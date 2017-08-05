package main

import (
	"github.com/redrabbit/couchpotatoe/musiccast"
	"log"
)

func main() {
	musiccast.ListenAndDispatch()
	devices, err := musiccast.Discover()
	if err != nil {
		log.Fatal(err)
	}

	device := devices[0]

	log.Println(device.GetDeviceID(), "is available")
	changes := device.Subscribe()
	for {
		log.Println(<-changes)
	}
}
