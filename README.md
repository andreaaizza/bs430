## Intro
Some experiments to read Medisana "BS 430" data to own data store: automatically reads weight and all features (kCal, fat, ...) to a InfluxDB database. It currenlty runs on a RespberryPI Zero W. 

## Installation
This requires: 
* Go: https://golang.org/doc/install
* GATT: `go get github.com/andreaaizza/gatt`
* InfluxDB client: `go get github.com/influxdata/influxdb1-client/v2`
* A InfluxDB server with a writable database (set `host`, `db`, `user` and `pass` to `config.json`)
* A Medisana "BS 430" (might work on some other Medisana models, but could not test it). Get the Bluetooth MAC address (you can use your mobile phone and Bluetooth scan, while the Bluetooth logo is blinking on your Medisana "BS 430", i.e. after a weight measurement on a recognized profile -- see manual), and set the `config.json` appropriately

To build and install:
```
git clone github.com/andreaaizza/bs430
cd bs430
make 
```
Run it once, customize generated `config.json.template` to `config.json` and run it.

## How to read data from Medisana "BS 430"

### Bluetooth service
Service: `0x78b2`, includes measurements and commands
Characteristics:
* `0x8a21` -> indication, weight
* `0x8a22` -> indication, features (kCal, ...)
* `0x8a82` -> indication, customer (including profile number, ...)

### Procedure
1. Subscribe to all indications
`SetIndicateValue()`
2. Write current Epoch (2010) on `0x8a81`
`WriteCharacteristic(..., []byte{0x02}, epoch2010()...)`
3. Read and decode indications
Read indications and decode values (see INSTRUCTIONS)

## Instructions
See also: https://github.com/oliexdev/openScale/wiki/Medisana-BS444

From characteristic `0x8a21`:
```
byte 0: unknown
byte 1-2: weight in little endian / 100
byte 3-4: unknown
byte 5-8: unix date/time stamp in little endian starting from year 2010
byte 9-18: unknown
Second package is send from UUID 0x8a22 as follow:
```

From characteristic `0x8a22`:
```
byte 0: unknown
byte 1-4: unix date/time stamp in little endian starting from year 2010
byte 5: unknown
byte 6-7: kCal in little endian
byte 8-9: fat percentage in little endian / 10 and first byte is masked with 0xF0
byte 10-11: water percentage in little endian / 10 and first byte is masked with 0xF0
byte 12-13: muscle percentage in little endian / 10 and first byte is masked with 0xF0
byte 14-15: bone mass in little endian / 10 and first byte is masked with 0xF0
byte 16-18: reserved bytes
```
From characteristic `0x8a82`:
```
byte 0: unknown
byte 1: profile number (uint8)
```

### Sample weight decode
```
2019-07-15T21:38:39.114Z INFO Attribute value changed, handle: 0x1B, value (0x): 1D-7A-17-00-FE-44-B7-EF-11-A0-17-00-FF-03-09-00-00-00-00
```

`7A-17` -> `echo 'ibase=16;obase=A;177A/64' | bc` = 60.10

### Epoch 2010:
Epoch (2010) is the count of seconds from Jan 1 2010, little endian coded.

How to read it: `44-B7-EF-11`
``` 
date +%s -d 2010-01-01
1262300400
echo 'obase=16;1262300400' | bc 
4B3D2CF0
echo 'ibase=16;obase=A;11EFB744+4B3D2CF0' | bc | date -d @$( cat - ) 
Mon 15 Jul 2019 10:38:12 PM CEST
```

How to get current:
```
echo "obase=16;$(( $( date +%s ) - 1262300400 ))" | bc
```
