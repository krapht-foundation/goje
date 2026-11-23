//go:build !goje_smoke && !goje_bench

package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"log/slog"
	"os"
	"time"

	"github.com/krapht-foundation/goje/engine"
	"github.com/krapht-foundation/goje/platform"
)

func main() {
	if err := runPlayground(); err != nil {
		log.Fatal(err)
	}
}

func runPlayground() error {
	ctx := context.Background()
	config := playgroundConfig()
	var factory engine.SceneFactory
	switch os.Getenv("GOJE_PLAYGROUND") {
	case "pickup":
		factory = newScene
	case "courtyard":
		factory = newCourtyard
		config.Title = "Goje Patrol · Click: select · Arrows: pan · W/S: zoom · Space: patrol · R: reset"
	default:
		var err error
		factory, err = authoredFactory()
		if err != nil {
			return err
		}
		config.Title = "Goje Courtyard · Left click: select · Right click: move · Arrows: pan · W/S: zoom · R: reset"
	}
	if os.Getenv("GOJE_DIAGNOSTICS") == "1" {
		// Keep the demo finite so native and browser users can both inspect a
		// finished capture without reading from the live game loop.
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		config.Logger = slog.New(slog.NewTextHandler(os.Stderr, nil))
		config.Diagnostics = &platform.Diagnostics{TraceCapacity: 512}
	}

	runErr := platform.Run(ctx, config, factory)
	if config.Diagnostics != nil {
		return errors.Join(runErr, json.NewEncoder(os.Stdout).Encode(config.Diagnostics.Result))
	}
	return runErr
}
