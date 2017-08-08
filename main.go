package main

import (
	"github.com/redrabbit/couchpotatoe/loxone"
	"log"
)

func main() {
	conn, err := loxone.Connect()
	if err != nil {
		log.Fatal(err)
	}

	err = conn.Authenticate("admin", "admin")
	if err != nil {
		log.Fatal(err)
	}

	app, err := conn.LoxAPP3()
	if err != nil {
		log.Fatal(err)
	}

	log.Println(app)

	select {}
}
