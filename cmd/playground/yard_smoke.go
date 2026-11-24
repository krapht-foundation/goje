//go:build goje_smoke && cgo && (darwin || linux)

package main

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"

	"github.com/krapht-foundation/goje/engine"
	"github.com/krapht-foundation/goje/engine/fixed"
	"github.com/krapht-foundation/goje/internal/platform/desktop"
	"github.com/krapht-foundation/goje/platform"
)

// yardScreen converts ground tiles and a height in projected pixels to logical
// viewport pixels. The camera is centered on the board, so the origin is fixed.
func yardScreen(x, y, elevation float64) (screenX, screenY int) {
	return int(16*(x-y) + 144), int(8*(x+y) - elevation + 36)
}

// yardScript feeds real held keys through the scene's normal Update and moves
// to the next stage only after the probe has seen the stage's pixels. Input
// stops as soon as a stage's goal holds, so catch-up ticks cannot overshoot it.
type yardScript struct {
	yard         *yardCourtyard // The live scene; a restart replaces it.
	stage        int
	flushY       fixed.Num // Zero means the scout has not touched the wall yet.
	resetPressed bool
	done         bool // The current stage's goal holds in the simulation.
}

type yardSmokeScene struct {
	engine.Scene
	script *yardScript
}

const (
	yardStageSpawn = iota
	yardStageSlide
	yardStageReset
	yardStageDoorLit
	yardStageDoorDark
	yardStageReset2
	yardStageDeck
	yardStageDone
)

func (s *yardSmokeScene) Update(tick engine.Tick) (engine.Transition, error) {
	tick.Input = s.script.input()
	return s.Scene.Update(tick)
}

// input returns this tick's keys and records whether the stage goal now holds.
func (y *yardScript) input() engine.Input {
	sim := y.yard.sim
	fp := sim.bodies[0].Footprint
	wallFace := fixed.FromInt(4) // The authored wall column starts at tile x = 4.
	switch y.stage {
	case yardStageSlide:
		if fp.Max.X == wallFace {
			if y.flushY == 0 {
				y.flushY = fp.Min.Y
			}
			y.done = fp.Min.Y.Sub(y.flushY) >= 40
		}
		if !y.done {
			return engine.Input{Held: engine.KeyArrowDown}
		}
	case yardStageReset, yardStageReset2:
		if !y.resetPressed {
			y.resetPressed = true
			return engine.Input{Pressed: engine.KeyR}
		}
	case yardStageDoorLit:
		y.done = sim.doorLit
		if !y.done {
			return engine.Input{Held: engine.KeyArrowRight}
		}
	case yardStageDoorDark:
		y.done = !sim.doorLit
		if !y.done {
			return engine.Input{Held: engine.KeyArrowRight}
		}
	case yardStageDeck:
		y.done = fp.Overlaps(sim.deck) && fp.Min.X > sim.deck.Min.X
		if !y.done {
			return engine.Input{Held: engine.KeyArrowDown}
		}
	}
	return engine.Input{}
}

// check validates the frame just drawn for the current stage and reports
// whether the stage is complete.
func (y *yardScript) check(viewport image.Rectangle) (bool, error) {
	sim := y.yard.sim
	pipX, pipY := sim.center()
	px, py := yardScreen(pipX, pipY, 0)
	lamp := func(want color.NRGBA) bool {
		x, yy := yardScreen(sim.door.Min.X.Float64()+0.5, sim.door.Min.Y.Float64()+0.5, 32)
		return pixelIs(viewport, x, yy, want)
	}
	atSpawn := func() bool {
		wallX, wallY := yardScreen(4.5, 1.5, 0)
		deckX, deckY := yardScreen(4.5, 5.5, yardDeckHeight)
		return pixelIs(viewport, px, py, yardFootprintInk) && lamp(yardDoorDark) &&
			pixelIs(viewport, wallX, wallY-9, courtyardWallColor) &&
			pixelIs(viewport, deckX, deckY, yardDeckColor) &&
			px == 144 && py == 76
	}
	switch y.stage {
	case yardStageSpawn, yardStageReset, yardStageReset2:
		if y.stage != yardStageSpawn && !y.resetPressed {
			return false, nil // Wait for the restart tick to reach the screen.
		}
		if !atSpawn() {
			return false, errors.New(
				"frame did not show the scout at spawn with a dark lamp, the wall, and the deck",
			)
		}
		return true, nil
	case yardStageSlide:
		if !y.done {
			return false, nil
		}
		// Flush against the wall, then moved south along it: Y changed, X did not.
		if sim.bodies[0].Footprint.Max.X != fixed.FromInt(4) ||
			!pixelIs(viewport, px, py, yardFootprintInk) {
			return false, fmt.Errorf("frame did not show the scout flush against the wall after "+
				"sliding along it: footprint %+v, marker at (%d, %d)", sim.bodies[0].Footprint, px, py)
		}
		return true, nil
	case yardStageDoorLit:
		if !y.done {
			return false, nil
		}
		if !lamp(yardDoorLit) {
			return false, errors.New(
				"frame did not show the doorway lamp lit while the scout overlapped it",
			)
		}
		return true, nil
	case yardStageDoorDark:
		if !y.done {
			return false, nil
		}
		if !lamp(yardDoorDark) {
			return false, errors.New("frame did not show the doorway lamp dark after the scout left")
		}
		return true, nil
	case yardStageDeck:
		if !y.done {
			return false, nil
		}
		// The footprint overlay stays visible while the scout is beneath the deck, which is
		// drawn at its own height above it.
		deckX, deckY := yardScreen(4.5, 5.5, yardDeckHeight)
		if !pixelIs(viewport, px, py, yardFootprintInk) ||
			!pixelIs(viewport, deckX, deckY, yardDeckColor) {
			return false, errors.New("frame did not show the scout's marker beneath the raised deck")
		}
		return true, nil
	}
	return false, nil
}

func runYardSmoke() error {
	loaded, err := loadAuthored(authoredFiles)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	script := &yardScript{}
	config := loaded.config()
	desktop.FrameProbe = func(viewport image.Rectangle) error {
		if script.stage >= yardStageDone || script.yard == nil || script.yard.sim == nil {
			return nil
		}
		complete, err := script.check(viewport)
		if err != nil {
			return fmt.Errorf("stage %d: %w", script.stage, err)
		}
		if complete {
			script.stage, script.done, script.resetPressed, script.flushY = script.stage+1, false, false, 0
			if script.stage == yardStageDone {
				cancel()
			}
		}
		return nil
	}
	defer func() { desktop.FrameProbe = nil }()
	if err := platform.Run(ctx, config, func() engine.Scene {
		script.yard = &yardCourtyard{layout: loaded.layout, idle: loaded.idle, move: loaded.move}
		return &yardSmokeScene{Scene: script.yard, script: script}
	}); err != nil {
		return fmt.Errorf("native yard smoke: %w", err)
	}
	if script.stage != yardStageDone {
		return fmt.Errorf("native yard smoke ended during stage %d", script.stage)
	}
	return nil
}
