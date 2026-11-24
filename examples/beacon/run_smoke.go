//go:build goje_beacon_smoke

package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/krapht-foundation/goje/engine"
	"github.com/krapht-foundation/goje/platform"
)

// Run the actual host with repeatable input. The browser smoke additionally
// checks real device input and presented canvas pixels.
func run(config platform.Config, factory func() *scene) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var seen smokeProgress
	capture := &platform.Diagnostics{}
	config.Diagnostics = capture
	err := platform.Run(ctx, config, func() engine.Scene {
		return &smokeScene{scene: factory(), progress: &seen, cancel: cancel}
	})
	if err != nil {
		return fmt.Errorf("run native beacon smoke: %w", err)
	}
	if !seen.reset || capture.Result.Frames < 3 {
		return errors.New("the native host did not present the toggle and reset scenario")
	}
	slog.Info("Native Beacon smoke passed: authored placement, toggle, reset, and presented frames")
	return nil
}

type smokeProgress struct{ reset bool }

type smokeScene struct {
	*scene
	progress *smokeProgress
	cancel   context.CancelFunc
}

func (s *smokeScene) Update(tick engine.Tick) (engine.Transition, error) {
	tick.Input = engine.Input{}
	if s.beacon.X != 1 || s.beacon.Y != 0 {
		return engine.Transition{}, errors.New("expected the editor-moved beacon at (1, 0)")
	}
	switch tick.Number {
	case 1:
		tick.Input.PrimaryPress = engine.MousePress{X: 64, Y: 32, Valid: true}
	case 15:
		if !s.on {
			return engine.Transition{}, errors.New("beacon did not toggle on")
		}
		tick.Input.Pressed = engine.KeyR
	case 30:
		if s.on {
			return engine.Transition{}, errors.New("beacon did not reset")
		}
		s.progress.reset = true
		s.cancel()
	}
	return s.scene.Update(tick)
}
