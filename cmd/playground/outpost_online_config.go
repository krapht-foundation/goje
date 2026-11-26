//go:build !js

package main

import "os"

// onlineConfigFromEnvironment reads the relay and the room to join from
// GOJE_RELAY, such as ws://127.0.0.1:8080, and GOJE_ROOM or GOJE_WATCH.
func onlineConfigFromEnvironment() onlineConfig {
	if code := os.Getenv("GOJE_WATCH"); code != "" {
		return onlineConfig{relay: os.Getenv("GOJE_RELAY"), room: code, watch: true}
	}
	return onlineConfig{relay: os.Getenv("GOJE_RELAY"), room: os.Getenv("GOJE_ROOM")}
}
