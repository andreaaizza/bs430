package db

///
import (
	"log"
	"sync"
	"time"

	client "github.com/influxdata/influxdb1-client/v2"
)

// conf local var holding all configs
var conf = Config{}

// Config holds all configs
type Config struct {
	DB    *DBConf    `json:"db"`
	Buff  *BuffConf  `json:"-"`
	Debug *DebugConf `json:"-"`
}

var DefConf = Config{DB: &DefDBConf, Buff: &DefBuffConf, Debug: &DefDebugConf}

// DBConf holds database (InfluxDB) configs
type DBConf struct {
	Host string `json:"host"`
	DB   string `json:"db"`
	User string `json:"user"`
	Pass string `json:"pass"`
}

var DefDBConf = DBConf{Host: "http://localhost:8086", DB: "test", User: "user", Pass: "pass"}

func (dBConf DBConf) check() bool {
	return len(dBConf.Host) > 0 && len(dBConf.DB) > 0 && len(dBConf.User) > 0 && len(dBConf.Pass) > 0
}

// BuffConf holds buffering configs
type BuffConf struct {
	MaxItemsInBuffer int
	MaxTimeWaitSec   int
}

func (buffConf BuffConf) check() bool {
	return buffConf.MaxItemsInBuffer > 0 && buffConf.MaxTimeWaitSec > 0
}

const maxItemsInBuffer = 1000
const maxTimeWaitSec = 5

var DefBuffConf BuffConf = BuffConf{MaxItemsInBuffer: maxItemsInBuffer, MaxTimeWaitSec: maxTimeWaitSec}

type DebugConf struct {
	Debug bool
}

func (debugConf DebugConf) debug() bool {
	return debugConf.Debug
}

func (debugConf DebugConf) check() bool {
	return true
}

var DefDebugConf DebugConf = DebugConf{Debug: false}

// saveConf checks args and save to make conf effective
func (c Config) saveConf() {
	//DB
	if c.DB != nil && !c.DB.check() {
		log.Panic("DB configuration error [%v]", c)
	}
	conf.DB = c.DB

	//Buff
	if c.Buff == nil {
		conf.Buff = &DefBuffConf
	} else {
		if !c.Buff.check() {
			log.Panic("Buff configuration error [%v]", c)
		}
		conf.Buff = c.Buff
	}
	//Debug
	if c.Debug == nil {
		conf.Debug = &DefDebugConf
	} else {
		conf.Debug = c.Debug
	}
}

func Init(c Config) {
	// save to local conf, or panic
	c.saveConf()

	// start buff handle
	go buffHandle()
}

// DBAgs data item to be pushed to DB
type DBArgs struct {
	T           time.Time
	Measurement string
	Val         float32
	Tags        map[string]string
}

var dbArgsBuff = make([]DBArgs, 0)
var lastUpload = time.Unix(0, 0)
var mu = &sync.Mutex{}

func PushToDB(d DBArgs) {
	// add new item
	mu.Lock()
	defer mu.Unlock()

	dbArgsBuff = append(dbArgsBuff, d)
}

func buffHandle() {
	for {
		select {
		case <-time.After(2 * time.Second):
			if len(dbArgsBuff) > conf.Buff.MaxItemsInBuffer {
				if conf.Debug.debug() {
					log.Printf("Buffer has %d items (more than %d). Flushing (buffer)...", len(dbArgsBuff), conf.Buff.MaxItemsInBuffer)
				}
				flushData()
			} else {
				t := time.Now()
				if len(dbArgsBuff) > 0 && t.After(lastUpload.Add(time.Duration(maxTimeWaitSec)*time.Second)) {
					if conf.Debug.debug() {
						log.Printf("Buffer has been last flushed at %s. Flushing (time)...", lastUpload.String())
					}
					lastUpload = t
					flushData()
				}
			}
		}
	}
}

func flushData() (err error) {
	// Create a new HTTPClient
	c, err := client.NewHTTPClient(client.HTTPConfig{
		Addr:               conf.DB.Host,
		Username:           conf.DB.User,
		Password:           conf.DB.Pass,
		InsecureSkipVerify: false,
	})
	defer c.Close()
	if err != nil {
		log.Printf("Cannot connect to the database [%v]", err)
		return
	}

	// Create a new point batch
	bp, err := client.NewBatchPoints(client.BatchPointsConfig{
		Database:  conf.DB.DB,
		Precision: "ms",
	})
	if err != nil {
		log.Printf("Cannot add new batch point [%v] with error [%v]", bp, err)
		return
	}

	mu.Lock()
	defer mu.Unlock()
	for _, a := range dbArgsBuff {
		// Create a point and add to batch
		tags := a.Tags
		fields := map[string]interface{}{
			"value": a.Val,
		}

		pt, pterr := client.NewPoint(a.Measurement, tags, fields, a.T)
		if pterr != nil {
			log.Printf("Cannot add new point [%v] with error [%v]", pt, pterr)
			return
		}
		bp.AddPoint(pt)
	}

	// Write the batch
	if err := c.Write(bp); err != nil {
		log.Printf("%v", err)
	}

	// Close client resources
	if err = c.Close(); err != nil {
		log.Printf("Cannot close connection [%v]", err)
		return
	}

	// remove data from buffer
	dbArgsBuff = dbArgsBuff[:0]

	return
}

//
//
