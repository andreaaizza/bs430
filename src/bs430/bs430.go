package main

import (
	"db"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
	"version"

	"github.com/andreaaizza/gatt"
)

func onStateChanged(d gatt.Device, s gatt.State) {
	fmt.Println("State:", s)
	switch s {
	case gatt.StatePoweredOn:
		fmt.Println("Scanning...")
		d.Scan([]gatt.UUID{}, false)
		return
	default:
		d.StopScanning()
	}
}

func onPeriphDiscovered(p gatt.Peripheral, a *gatt.Advertisement, rssi int) {
	// Filter on conf.uuid
	if strings.ToUpper(p.ID()) != C.Uuid {
		return
	}

	// stop scanning when a device is found
	p.Device().StopScanning()
	//Using Raspberrypi Z W, for some unknown reason, you need to wait a bit here!?!?  FIXME
	time.Sleep(100 * time.Millisecond)

	fmt.Printf("\nDiscovered dev ID:%s, NAME:(%s), RSSI:%v\n", p.ID(), p.Name(), a.TxPowerLevel)
	p.Device().Connect(p)
}

func epoch2010() []byte {
	// Epoch2010
	var t uint32 = uint32(time.Now().Unix()) - 1262300400
	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b, t)
	return b
}

func epoch2010ToTime(e []byte) time.Time {
	epoch := binary.LittleEndian.Uint32(e) + 1262300400
	return time.Unix(int64(epoch), 0)
}

const defProfile uint8 = 255

func makeTags() map[string]string {
	if connParam.profile == defProfile {
		return map[string]string{"sensor": C.Uuid, "profile": "UNKNOWN"}
	}
	return map[string]string{"sensor": C.Uuid, "profile": fmt.Sprintf("%d", connParam.profile)}
}

func sholdPushToDB(c string) (s string, b bool) {
	b = C.ReadLast <= 0 || connParam.count[c] <= C.ReadLast
	if b {
		s = "|P"
	} else {
		s = "|L"
	}
	return
}

func decode(c *gatt.Characteristic, b []byte) (ret string) {

	switch c.UUID().String() {
	case "8a21": // weight
		{
			tags := makeTags()
			connParam.count["8a21"] += 1

			weight := float32(binary.LittleEndian.Uint16(b[1:3])) / 100
			t := epoch2010ToTime(b[5:9])

			ret = fmt.Sprintf("%s, weight:%0.1f", t.String(), weight)

			toDBLogString, toDB := sholdPushToDB("8a21")
			ret += toDBLogString
			if toDB {
				db.PushToDB(db.DBArgs{Tags: tags, Measurement: "weight", T: t, Val: weight})
			}
			return
		}
	case "8a22": // features
		{
			tags := makeTags()
			connParam.count["8a22"] += 1

			t := epoch2010ToTime(b[1:5])
			kCal := float32(binary.LittleEndian.Uint16(b[6:8]))
			fat := float32(binary.LittleEndian.Uint16([]byte{b[8], b[9] & 0x0F})) / 10
			water := float32(binary.LittleEndian.Uint16([]byte{b[10], b[11] & 0x0F})) / 10
			muscle := float32(binary.LittleEndian.Uint16([]byte{b[12], b[13] & 0x0F})) / 10
			bone := float32(binary.LittleEndian.Uint16([]byte{b[14], b[15] & 0x0F})) / 10

			ret = fmt.Sprintf("%s, kCal:%0.1f fat:%0.1f water:%0.1f muscle:%0.1f bone:%0.1f", t.String(), kCal, fat, water, muscle, bone)

			s, b := sholdPushToDB("8a22")
			ret += s
			if b {
				db.PushToDB(db.DBArgs{Tags: tags, Measurement: "kCal", T: t, Val: kCal})
				db.PushToDB(db.DBArgs{Tags: tags, Measurement: "fat", T: t, Val: fat})
				db.PushToDB(db.DBArgs{Tags: tags, Measurement: "water", T: t, Val: water})
				db.PushToDB(db.DBArgs{Tags: tags, Measurement: "muscle", T: t, Val: muscle})
				db.PushToDB(db.DBArgs{Tags: tags, Measurement: "bone", T: t, Val: bone})
			}
			return
		}
	case "8a82":
		{
			connParam.profile = uint8(b[2])
			ret = fmt.Sprintf("profile=%d", connParam.profile)
			return
		}
	default:
		return "unknown"
	}
}

var connParam struct {
	profile uint8
	count   countType
}

type countType map[string]int

var defCount countType = countType{"8a21": 0, "8a22": 0}

func onPeriphConnected(p gatt.Peripheral, err error) {

	fmt.Println("Connected ", p.ID())
	defer p.Device().CancelConnection(p)

	// reset connection specific parameters
	connParam.profile = defProfile
	connParam.count = make(map[string]int)
	for s, c := range defCount {
		connParam.count[s] = c
	}

	if err := p.SetMTU(500); err != nil {
		fmt.Printf("Failed to set MTU, err: %s\n", err)
	}

	// Discovery services
	// BS430, Service 0x78b2 includes measurements and commands
	ss, err := p.DiscoverServices([]gatt.UUID{gatt.MustParseUUID("78b2")})
	if err != nil {
		fmt.Printf("Failed to discover services, err: %s\n", err)
		return
	}

	for _, s := range ss {
		if C.Debug {
			fmt.Println("Service: ", s.UUID().String())
		}

		// Discovery characteristics
		cs, err := p.DiscoverCharacteristics([]gatt.UUID{
			gatt.MustParseUUID("8a21"), // weight - indicate
			gatt.MustParseUUID("8a22"), // features - indicate
			gatt.MustParseUUID("8a81"), // cmd - write (0x02 to trigger)
			gatt.MustParseUUID("8a82"), // custom - indicate
		}, s)
		if err != nil {
			fmt.Printf("Failed to discover characteristics, err: %s\n", err)
			continue
		}

		// 1. subscribe to all indications
		for _, c := range cs {
			if C.Debug {
				fmt.Println("Characteristic:  ", c.UUID().String(), " Props: ", c.Properties().String())
			}

			// Discovery descriptors
			_, err := p.DiscoverDescriptors(nil, c)
			if err != nil {
				fmt.Printf("Failed to discover descriptors, err: %s\n", err)
				continue
			}

			// Subscribe the characteristic, if possible.
			if (c.Properties() & gatt.CharIndicate) != 0 {
				f := func(c *gatt.Characteristic, b []byte, err error) {
					if C.Debug {
						fmt.Printf("%s: % X | %s\n", c.UUID().String(), b, decode(c, b))
					} else {
						fmt.Printf("%s: | %s\n", c.UUID().String(), decode(c, b))
					}
				}
				if err := p.SetIndicateValue(c, f); err != nil {
					fmt.Printf("Failed to subscribe indicate %s, err: %s\n", c.UUID().String(), err)
					continue
				}
			}
		}

		// 2. write command to trigger indications
		for _, c := range cs {
			if C.Debug {
				fmt.Println("Characteristic:  ", c.UUID().String(), " Props: ", c.Properties().String())
			}

			// Write command: 0x02 <current_epoch2010>
			if (c.Properties() & gatt.CharWrite) != 0 {
				w := append([]byte{0x02}, epoch2010()...)
				if C.Debug {
					fmt.Printf("Writing cmd % 0X to %s\n", w, c.UUID().String())
				}
				err := p.WriteCharacteristic(c, w, false)
				if err != nil {
					fmt.Printf("Failed to write to characteristic %s, err: %s\n", c.UUID().String(), err)
					continue
				}
			}

		}
	}

	// Waiting for 5 seconds to get some notifiations, if any
	time.Sleep(5 * time.Second)
}

func onPeriphDisconnected(p gatt.Peripheral, err error) {
	fmt.Println("Disconnected ", p.ID())

	// wait to avoid rescanning... BS430 remains shutsdown in 45s
	time.Sleep(45 * time.Second)

	// reset profile
	connParam.profile = defProfile

	// restart Scanning...
	fmt.Println("Restarting scanning...")
	p.Device().Scan([]gatt.UUID{}, false)
}

type Conf struct {
	Debug    bool      `json:"debug,omitempty"`
	Uuid     string    `json:"uuid,omitempty"`
	Dev      int       `json:"dev,omitempty"`
	DbConf   db.Config `json:"db_conf,omitempty"`
	ReadLast int       `json:"read_last,omitempty"` // ReadLast Read at most this amount of latest measurements. <=0 implies all

	confFile string `json:"-"`
	showVer  bool   `json:"-"`
}

func (C Conf) dump() (s string) {
	b, err := json.Marshal(C)
	if err != nil {
		return "Error () dumping config"
	} else {
		return string(b)
	}
}

var C Conf

var DefConf Conf = Conf{Debug: false, Uuid: "00:11:22:33:44:55", Dev: -1, ReadLast: 1, DbConf: db.DefConf}

func loadConfig() {
	var defConfFn = "config.json.template"

	if !filepath.IsAbs(C.confFile) {
		cwd, err := os.Getwd()
		if err != nil {
			log.Panic("Conf file has relative path and cannot determine cwd")
		}
		C.confFile = filepath.Join(cwd, C.confFile)
		defConfFn = filepath.Join(cwd, defConfFn)
	} else {
		defConfFn = filepath.Join(filepath.Dir(C.confFile), defConfFn)
	}

	// save def config
	jsdata, err := json.Marshal(DefConf)
	if err != nil {
		log.Panic("Cannot marshal DefConf")
	}
	err = ioutil.WriteFile(defConfFn, jsdata, 0644)
	if err != nil {
		log.Panic("Cannot save DefConf to file '", defConfFn, "', ", err)
	}
	log.Print("Saved template config to: ", defConfFn)

	// load config
	b, err := ioutil.ReadFile(C.confFile)
	if err != nil {
		log.Panic("Cannot open conf file '", C.confFile, "', ", err)

	}
	err = json.Unmarshal(b, &C)
	if err != nil {
		log.Panic("Cannot unmarshal config file '", C.confFile, "', ", err)
	}
}

func main() {
	// log flags
	log.SetFlags(log.Ldate | log.Lshortfile)

	// init conf to def
	C = DefConf

	// flags override defConf
	flag.BoolVar(&C.showVer, "v", false, "Show version and quit")
	flag.BoolVar(&C.Debug, "debug", false, "Debug")
	flag.StringVar(&C.confFile, "c", "./config.json", "Config file")
	flag.IntVar(&C.ReadLast, "n", 1, "Read at most last 'n' measurements (-1 for all)")
	flag.Parse()

	// Version only?
	if C.showVer {
		fmt.Printf("This is version %s. Ciao!\n", version.Version)
		os.Exit(0)
	}

	// load config
	loadConfig()
	log.Printf("Loaded conf %v", C)

	// override Uuid from Args
	if len(flag.Args()) >= 1 {
		C.Uuid = strings.ToUpper(flag.Args()[0])
		log.Print("Override command line. Uuid=", C.Uuid)
	}

	// start
	log.Printf("Starting version '%s' with conf %s", version.Version, C.dump())
	db.Init(C.DbConf)

	// init BT
	d, err := gatt.NewDevice([]gatt.Option{gatt.LnxMaxConnections(1), gatt.LnxDeviceID(-1, true)}...)
	if err != nil {
		log.Panicf("Failed to open device, err: %s\n", err)
		return
	}

	// Register handlers.
	d.Handle(
		gatt.PeripheralDiscovered(onPeriphDiscovered),
		gatt.PeripheralConnected(onPeriphConnected),
		gatt.PeripheralDisconnected(onPeriphDisconnected),
	)

	d.Init(onStateChanged)

	for {
		select {}
	}
}
