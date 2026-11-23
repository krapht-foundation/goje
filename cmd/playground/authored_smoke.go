//go:build goje_smoke && cgo && (darwin || linux)

package main

import (
	"context"
	"fmt"
	"image"
	"image/color"

	"github.com/krapht-foundation/goje/engine"
	"github.com/krapht-foundation/goje/internal/platform/desktop"
	"github.com/krapht-foundation/goje/platform"
)

func runAuthoredSmoke(shortcut bool) error {
	factory, err := authoredFactory()
	if err != nil {
		return err
	}
	config, err := authoredConfig()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	groundAccent := color.NRGBA{R: 91, G: 133, B: 114, A: 255}
	background := color.NRGBA{R: 20, G: 28, B: 41, A: 255}
	gridLine := color.NRGBA{R: 63, G: 84, B: 108, A: 255}
	route := courtyardCheckpoint{name: "wall detour", check: func(v image.Rectangle) bool {
		return pixelIs(v, 128, 88, markerColor) && pixelIs(v, 208, 108, destinationColor)
	}}
	wallAndLookout := func(v image.Rectangle) bool {
		return pixelIs(v, 176, 80, courtyardWallColor)
	}
	if shortcut {
		// Match the highlight drawn through the Tile Maker in the round-trip smoke.
		groundAccent = color.NRGBA{R: 222, G: 122, B: 66, A: 255}
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
		return pixelIs(v, 144, 64, markerColor) && wallAndLookout(v) &&
			pixelIs(v, 141, 41, groundAccent) && pixelIs(v, 130, 38, background) &&
			pixelIs(v, 152, 40, gridLine) &&
			!pixelIs(v, 144, 53, selectionColor)
	}
	checks := []courtyardCheckpoint{
		{name: "authored spawn", check: spawn},
		{
			name:  "ground art under zoom",
			input: engine.Input{Pressed: engine.KeyW},
			check: func(v image.Rectangle) bool {
				return pixelIs(v, 136, 26, groundAccent) && pixelIs(v, 122, 22, background) &&
					pixelIs(v, 150, 25, gridLine)
			},
		},
		{name: "reset camera", input: engine.Input{Pressed: engine.KeyR}, check: spawn},
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
	if err := platform.Run(ctx, config, func() engine.Scene {
		return &courtyardSmokeScene{Scene: factory(), input: input}
	}); err != nil {
		return fmt.Errorf("native authored smoke: %w", err)
	}
	if index != len(checks) {
		return fmt.Errorf("window closed before checkpoint %q", checks[index].name)
	}
	return nil
}
