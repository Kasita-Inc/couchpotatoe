package main

import (
	"github.com/redrabbit/couchpotatoe/musiccast"
	"log"
)

func main() {
	devices, err := musiccast.Discover()
	if err != nil {
		log.Fatal(err)
	}

	for _, d := range devices {
		log.Println(d.DeviceID, "available")
	}

	go musiccast.Listen()
	select {}
}
