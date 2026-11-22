package main

import (
	"flag"
	"fmt"
	"log"

	"github.com/krapht-foundation/goje/internal/playground"
)

func main() {
	smoke := flag.Bool("smoke", false, "run the native rendering smoke scenario and exit")
	flag.Parse()
	if err := playground.Run(*smoke); err != nil {
		log.Fatal(err)
	}
	if *smoke {
		fmt.Println("Desktop smoke passed: marker moved right in rendered frames")
	}
}
