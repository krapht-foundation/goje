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
	var config platform.Config
	var factory engine.SceneFactory
	switch os.Getenv("GOJE_PLAYGROUND") {
	case "pickup":
		config = playgroundConfig(nil)
		factory = newScene
	case "courtyard":
		config = playgroundConfig(nil)
		factory = newCourtyard
		config.Title = "Goje Patrol · Click: select · Arrows: pan · W/S: zoom · Space: patrol · R: reset"
	case "yard":
		loaded, err := loadAuthored(authoredFiles)
		if err != nil {
			return err
		}
		config, factory = loaded.config(), loaded.yardFactory()
		config.Title = "Goje Yard · Arrows / WASD: walk · R: reset"
	default:
		loaded, err := loadAuthored(authoredFiles)
		if err != nil {
			return err
		}
		config, factory = loaded.config(), loaded.factory()
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
