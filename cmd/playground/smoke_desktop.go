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

// smokeCase is one self-contained native scenario. Each runs in its own
// process when named by its flag, so every scenario gets its own time budget
// in make smoke-desktop; running with no flag runs them all in order.
type smokeCase struct {
	flag   string
	run    func() error
	passed string
}

func smokeCases() []smokeCase {
	return []smokeCase{
		{"--pickup", runSmoke, "rendered movement, pickup, and reset"},
		{
			"--courtyard",
			runCourtyardSmoke,
			"courtyard overlap, selection, pan, zoom, resize, and reset",
		},
		{
			"--authored", func() error { return runAuthoredSmoke(false) },
			"authored courtyard selection, wall detour, arrival, and reset",
		},
		{
			"--yard",
			runYardSmoke,
			"yard slide along a wall, doorway lamp, walk beneath a raised deck, and reset",
		},
		{
			"--skirmish",
			runSkirmishSmoke,
			"skirmish selection, order, alarm, bolts, damage, defeat, and reset",
		},
		{
			"--outpost",
			runOutpostSmoke,
			"outpost text, drag selection, orders, hold, pause, save, load, victory, and replay",
		},
	}
}

// extraSmokeCases are only run by name: the exported-asset scenarios need
// files that the plain playground build does not contain.
func extraSmokeCases() []smokeCase {
	return []smokeCase{
		{"--sprite", runSpriteSmoke, "exported scout clip airborne with its painted cyan stripe " +
			"on motion ticks 13 and 16, planted on tick 17, and reset"},
		{
			"--shortcut", func() error { return runAuthoredSmoke(true) },
			"custom tile, moved lookout, shortcut crossing, arrival, and reset",
		},
	}
}

func main() {
	cases := smokeCases()
	if len(os.Args) == 2 {
		cases = nil
		for _, c := range append(smokeCases(), extraSmokeCases()...) {
			if c.flag == os.Args[1] {
				cases = []smokeCase{c}
			}
		}
		if cases == nil {
			log.Fatal("unknown smoke scenario flag; see smokeCases and extraSmokeCases")
		}
	}
	for _, c := range cases {
		if err := c.run(); err != nil {
			log.Fatal(err)
		}
		fmt.Println("Desktop smoke passed: " + c.passed)
	}
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
