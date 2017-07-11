package main

import (
	"github.com/redrabbit/couchpotatoe/musiccast"
	"log"
)

func main() {
	musiccast.Listen()
	devices, err := musiccast.Discover()
	if err != nil {
		log.Fatal(err)
	}

	for _, d := range devices {
		log.Println(d.GetDeviceID(), "is available")
	}

	select {}
}
