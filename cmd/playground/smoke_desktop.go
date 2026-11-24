//go:build goje_smoke && cgo && (darwin || linux)

package main

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"log"
	"os"

	"github.com/go-gl/gl/v3.3-core/gl"

	"github.com/krapht-foundation/goje/engine"
	"github.com/krapht-foundation/goje/internal/platform/desktop"
	"github.com/krapht-foundation/goje/platform"
)

const smokeMoveTicks = 20

type smokeStage uint8

const (
	smokeSpawn smokeStage = iota
	smokePickup
	smokeReset
	smokeDone
)

// nativeSmoke spans scene restarts. Each new wrapper embeds a fresh copy of
// the real encounter and sends scripted snapshots through its normal Update.
type nativeSmoke struct {
	cancel      context.CancelFunc
	stage       smokeStage
	moves       int
	resetIssued bool
}

type smokeScene struct {
	engine.Scene
	smoke *nativeSmoke
}

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--sprite" {
		if err := runSpriteSmoke(); err != nil {
			log.Fatal(err)
		}
		fmt.Println("Desktop smoke passed: exported scout clip airborne with its painted cyan stripe " +
			"on motion ticks 13 and 16, planted on tick 17, and reset")
		return
	}
	if len(os.Args) == 2 && os.Args[1] == "--shortcut" {
		if err := runAuthoredSmoke(true); err != nil {
			log.Fatal(err)
		}
		fmt.Println(
			"Desktop smoke passed: custom tile, moved lookout, shortcut crossing, arrival, and reset",
		)
		return
	}
	if err := runSmoke(); err != nil {
		log.Fatal(err)
	}
	fmt.Println("Desktop smoke passed: rendered movement, pickup, and reset")
	if err := runCourtyardSmoke(); err != nil {
		log.Fatal(err)
	}
	fmt.Println("Desktop smoke passed: courtyard overlap, selection, pan, zoom, resize, and reset")
	if err := runAuthoredSmoke(false); err != nil {
		log.Fatal(err)
	}
	fmt.Println("Desktop smoke passed: authored courtyard selection, wall detour, arrival, and reset")
	if err := runYardSmoke(); err != nil {
		log.Fatal(err)
	}
	fmt.Println(
		"Desktop smoke passed: yard slide along a wall, doorway lamp, walk beneath a raised deck, and reset",
	)
}

func runSmoke() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	smoke := &nativeSmoke{cancel: cancel}
	desktop.FrameProbe = smoke.inspect
	defer func() { desktop.FrameProbe = nil }()
	if err := platform.Run(ctx, playgroundConfig(nil), func() engine.Scene {
		return &smokeScene{Scene: newScene(), smoke: smoke}
	}); err != nil {
		return fmt.Errorf("native smoke: %w", err)
	}
	if smoke.stage != smokeDone {
		return errors.New("window closed before the native smoke scenario finished")
	}
	return nil
}

func (s *smokeScene) Update(tick engine.Tick) (engine.Transition, error) {
	tick.Input = engine.Input{}
	switch {
	case s.smoke.stage == smokePickup && s.smoke.moves < smokeMoveTicks:
		s.smoke.moves++
		tick.Input.Held = engine.KeyArrowRight
	case s.smoke.stage == smokeReset && !s.smoke.resetIssued:
		s.smoke.resetIssued = true
		tick.Input.Pressed = engine.KeyR
	}
	return s.Scene.Update(tick)
}

// Inspect the actual back buffer before swapping. The scene advances only
// after the current checkpoint has been rendered, regardless of frame rate.
func (s *nativeSmoke) inspect(viewport image.Rectangle) error {
	switch s.stage {
	case smokeSpawn:
		if !atStart(viewport) {
			return errors.New("initial native frame did not show the player and token at spawn")
		}
		s.stage = smokePickup
	case smokePickup:
		if s.moves != smokeMoveTicks {
			return nil
		}
		if !pixelIs(viewport, 240, 92, markerColor) || pixelIs(viewport, 192, 108, tokenColor) {
			return errors.New("native frame did not show the player after collecting the token")
		}
		s.stage = smokeReset
	case smokeReset:
		// A frame can be presented before the next tick processes the reset.
		if !s.resetIssued {
			return nil
		}
		if !atStart(viewport) {
			return errors.New("native frame did not show the player and token restored after reset")
		}
		if pixelIs(viewport, 240, 92, markerColor) {
			return errors.New("native frame retained the old player after reset")
		}
		s.stage = smokeDone
		s.cancel()
	case smokeDone:
	}
	return nil
}

func atStart(viewport image.Rectangle) bool {
	return pixelIs(viewport, 160, 92, markerColor) && pixelIs(viewport, 192, 108, tokenColor)
}

func pixelIs(viewport image.Rectangle, x, y int, want color.NRGBA) bool {
	x = viewport.Min.X + x*viewport.Dx()/width
	y = viewport.Max.Y - 1 - y*viewport.Dy()/height
	var pixel [4]byte
	//nolint:gosec // G115: sample points are inside the GLFW framebuffer's C int bounds.
	gl.ReadPixels(int32(x), int32(y), 1, 1, gl.RGBA, gl.UNSIGNED_BYTE, gl.Ptr(&pixel[0]))
	return pixel == [4]byte{want.R, want.G, want.B, want.A}
}
