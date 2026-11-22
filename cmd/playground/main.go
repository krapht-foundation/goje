package main

import (
	"flag"
	"fmt"
	"log"
)

func main() {
	smoke := flag.Bool("smoke", false, "run the native rendering smoke scenario and exit")
	flag.Parse()
	if err := run(*smoke); err != nil {
		log.Fatal(err)
	}
	if *smoke {
		fmt.Println("Desktop smoke passed: rendered movement, pickup, and reset")
	}
}
