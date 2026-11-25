//go:build js && wasm

package main

import (
	"net/url"
	"strings"
	"syscall/js"
)

// onlineConfigFromEnvironment reads the relay and the room to join from the
// page's `relay` and `room` query parameters.
func onlineConfigFromEnvironment() onlineConfig {
	query, err := url.ParseQuery(
		strings.TrimPrefix(js.Global().Get("location").Get("search").String(), "?"),
	)
	if err != nil {
		return onlineConfig{}
	}
	return onlineConfig{relay: query.Get("relay"), room: query.Get("room")}
}
