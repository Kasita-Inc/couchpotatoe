package main

import (
	"github.com/almightycouch/couchpotatoe/loxone"
	"github.com/brutella/hc"
	"github.com/brutella/hc/accessory"
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

	info := accessory.Info{
		Name: "Bett (links)",
	}

	acc := accessory.NewSwitch(info)

	acc.Switch.On.OnValueRemoteUpdate(func(power bool) {
		if power == true {
			ws.ControlCommand("106e6773-02a9-e657-ffff403fb0c34b9e/AI2", "on")
		} else {
			ws.ControlCommand("106e6773-02a9-e657-ffff403fb0c34b9e/AI2", "off")
		}
	})

	go func() {
		for {
			val := <-ch
			acc.Switch.On.SetValue(val.(float64) != 0)
		}
	}()

	config := hc.Config{Pin: "00102003"}
	t, err := hc.NewIPTransport(config, acc.Accessory)
	if err != nil {
		log.Fatal(err)
	}

	hc.OnTermination(func() {
		t.Stop()
	})

	t.Start()
}
