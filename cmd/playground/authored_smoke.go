//go:build goje_smoke && cgo && (darwin || linux)

package main

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"time"

	"github.com/krapht-foundation/goje/engine"
	"github.com/krapht-foundation/goje/internal/platform/desktop"
	"github.com/krapht-foundation/goje/platform"
)

func runAuthoredSmoke(shortcut bool) error {
	loaded, err := loadAuthored(authoredFiles)
	if err != nil {
		return err
	}
	config, factory := loaded.config(), loaded.factory()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	groundAccent := color.NRGBA{R: 91, G: 133, B: 114, A: 255}
	background := color.NRGBA{R: 20, G: 28, B: 41, A: 255}
	gridLine := color.NRGBA{R: 63, G: 84, B: 108, A: 255}
	tomato := color.NRGBA{R: 215, G: 84, B: 73, A: 255}
	ink := color.NRGBA{R: 36, G: 38, B: 46, A: 255}
	eye := color.NRGBA{R: 244, G: 241, B: 235, A: 255}
	motionTicks := 0
	route := courtyardCheckpoint{name: "wall detour", check: func(v image.Rectangle) bool {
		return pixelIs(v, 128, 94, tomato) && pixelIs(v, 208, 108, destinationColor)
	}}
	wallAndLookout := func(v image.Rectangle) bool {
		return pixelIs(v, 176, 80, courtyardWallColor)
	}
	if shortcut {
		// Match the highlight drawn through the Tile Maker in the round-trip smoke.
		groundAccent = color.NRGBA{R: 222, G: 122, B: 66, A: 255}
		// The opening's projected scout anchor lies above the original detour.
		route = courtyardCheckpoint{name: "shortcut crossing", check: func(v image.Rectangle) bool {
			return pixelIs(v, 176, 85, tomato) && pixelIs(v, 208, 108, destinationColor)
		}}
		wallAndLookout = func(v image.Rectangle) bool {
			return !pixelIs(v, 176, 80, courtyardWallColor) && pixelIs(v, 176, 122, tokenColor) &&
				!pixelIs(v, 192, 114, tokenColor)
		}
	}
	spawn := func(v image.Rectangle) bool {
		return pixelIs(v, 144, 71, tomato) && pixelIs(v, 139, 70, eye) && wallAndLookout(v) &&
			pixelIs(v, 141, 41, groundAccent) && pixelIs(v, 130, 38, background) &&
			pixelIs(v, 152, 40, gridLine) &&
			!pixelIs(v, 144, 53, selectionColor)
	}
	checks := []courtyardCheckpoint{
		{name: "authored spawn", check: spawn},
		{name: "idle scout blinks", check: func(v image.Rectangle) bool {
			return pixelIs(v, 139, 70, ink) && pixelIs(v, 144, 71, tomato)
		}},
		{
			name:  "ground art and planted scout pivot under zoom",
			input: engine.Input{Pressed: engine.KeyW},
			check: func(v image.Rectangle) bool {
				return pixelIs(v, 136, 26, groundAccent) && pixelIs(v, 122, 22, background) &&
					pixelIs(v, 150, 25, gridLine) && pixelIs(v, 140, 64, tomato) &&
					pixelIs(v, 140, 69, ink) && !pixelIs(v, 140, 70, ink)
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
		{name: "moving scout hops above its ground anchor", check: func(v image.Rectangle) bool {
			if motionTicks < 5 || motionTicks > 12 {
				return false
			}
			x, y := 144+motionTicks, 76+motionTicks/2
			return pixelIs(v, x, y-7, tomato) && pixelIs(v, x, y-4, ink) &&
				!pixelIs(v, x, y-2, ink)
		}},
	}
	if !shortcut {
		checks = append(checks, courtyardCheckpoint{
			name: "wall occludes the moving scout",
			check: func(v image.Rectangle) bool {
				// At the end of the first edge the visible red top overlaps the
				// wall's upper edge. The wall must cover the sprite below it.
				return motionTicks >= 14 && motionTicks <= 19 &&
					pixelIs(v, 159, 78, tomato) && pixelIs(v, 160, 81, courtyardWallColor)
			},
		})
	}
	checks = append(
		checks,
		route,
		courtyardCheckpoint{
			name: "arrival without destination marker",
			check: func(v image.Rectangle) bool {
				return pixelIs(v, 208, 103, tomato) && !pixelIs(v, 208, 108, destinationColor)
			},
		},
		courtyardCheckpoint{
			name:  "restored authored spawn",
			input: engine.Input{Pressed: engine.KeyR},
			check: spawn,
		},
	)
	index, issued := 0, false
	input := func() engine.Input {
		if motionTicks != 0 {
			motionTicks++
		}
		if index >= len(checks) || issued {
			return engine.Input{}
		}
		issued = true
		next := checks[index].input
		if next.SecondaryPress.Valid {
			motionTicks = 1
		}
		return next
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
		return fmt.Errorf("native authored smoke ended before checkpoint %q", checks[index].name)
	}
	return nil
}
