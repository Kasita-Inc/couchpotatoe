package main

import (
	"github.com/redrabbit/couchpotatoe/loxone"
	"log"
)

func main() {
	ws, err := loxone.Connect("172.16.2.59")
	if err != nil {
		log.Fatal(err)
	}

	log.Println("connected")

	err = ws.Authenticate("admin", "TdtuPMJjZTTutWetWMoPXy9V")
	if err != nil {
		log.Fatal(err)
	}

	log.Println("authenticated")

	app3, err := ws.LoxAPP3()
	if err != nil {
		log.Fatal(err)
	}

	log.Println("app3 last modified:", app3["lastModified"])

	ch := ws.Subscribe("106e6773-02a9-e641-ffff20df2fc4e78a")

	err = ws.EnableStatusUpdate()
	if err != nil {
		log.Fatal(err)
	}

	for {
		log.Println(<-ch)
	}
}
