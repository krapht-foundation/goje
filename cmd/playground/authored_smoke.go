//go:build goje_smoke && cgo && (darwin || linux)

package main

import (
	"context"
	"fmt"
	"image"

	"github.com/krapht-foundation/goje/engine"
	"github.com/krapht-foundation/goje/internal/platform/desktop"
	"github.com/krapht-foundation/goje/platform"
)

func runAuthoredSmoke(shortcut bool) error {
	factory, err := authoredFactory()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	route := courtyardCheckpoint{name: "wall detour", check: func(v image.Rectangle) bool {
		return pixelIs(v, 128, 88, markerColor) && pixelIs(v, 208, 108, destinationColor)
	}}
	wallAndLookout := func(v image.Rectangle) bool {
		return pixelIs(v, 176, 80, courtyardWallColor)
	}
	if shortcut {
		// The opening's projected scout anchor lies above the original detour.
		route = courtyardCheckpoint{name: "shortcut crossing", check: func(v image.Rectangle) bool {
			return pixelIs(v, 176, 80, markerColor) && pixelIs(v, 208, 108, destinationColor)
		}}
		wallAndLookout = func(v image.Rectangle) bool {
			return !pixelIs(v, 176, 80, courtyardWallColor) && pixelIs(v, 176, 122, tokenColor) &&
				!pixelIs(v, 192, 114, tokenColor)
		}
	}
	spawn := func(v image.Rectangle) bool {
		return pixelIs(v, 144, 64, markerColor) && wallAndLookout(v) && !pixelIs(v, 144, 53, selectionColor)
	}
	checks := []courtyardCheckpoint{
		{name: "authored spawn", check: spawn},
		{name: "selection and destination", input: engine.Input{
			PrimaryPress:   engine.MousePress{X: 144, Y: 64, Valid: true},
			SecondaryPress: engine.MousePress{X: 208, Y: 108, Valid: true},
		}, check: func(v image.Rectangle) bool {
			return pixelIs(v, 208, 108, destinationColor) &&
				(pixelIs(v, 144, 53, selectionColor) || pixelIs(v, 146, 54, selectionColor))
		}},
		route,
		{name: "arrival without destination marker", check: func(v image.Rectangle) bool {
			return pixelIs(v, 208, 96, markerColor) && !pixelIs(v, 208, 108, destinationColor)
		}},
		{name: "restored authored spawn", input: engine.Input{Pressed: engine.KeyR}, check: spawn},
	}
	index, issued := 0, false
	input := func() engine.Input {
		if index >= len(checks) || issued {
			return engine.Input{}
		}
		issued = true
		return checks[index].input
	}
	desktop.FrameProbe = func(viewport image.Rectangle) error {
		if index == len(checks) {
			return nil
		}
		check := checks[index]
		if (check.input == (engine.Input{}) || issued) && check.check(viewport) {
			index, issued = index+1, false
			if index == len(checks) {
				cancel()
			}
		}
		return nil
	}
	defer func() { desktop.FrameProbe = nil }()
	if err := platform.Run(ctx, playgroundConfig(), func() engine.Scene {
		return &courtyardSmokeScene{Scene: factory(), input: input}
	}); err != nil {
		return fmt.Errorf("native authored smoke: %w", err)
	}
	if index != len(checks) {
		return fmt.Errorf("window closed before checkpoint %q", checks[index].name)
	}
	return nil
}
