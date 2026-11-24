//go:build goje_smoke && cgo && (darwin || linux)

package main

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"

	"github.com/go-gl/gl/v3.3-core/gl"

	"github.com/krapht-foundation/goje/engine"
	"github.com/krapht-foundation/goje/engine/grid"
	"github.com/krapht-foundation/goje/examples/skirmish"
	"github.com/krapht-foundation/goje/internal/platform/desktop"
	"github.com/krapht-foundation/goje/platform"
)

// skirmishScreen converts a ground position in tiles and a height in projected
// pixels to logical viewport pixels. The camera is centered on the 10 × 8
// board, so the origin is fixed.
func skirmishScreen(x, y, elevation float64) (screenX, screenY int) {
	return int(16*(x-y) + 144), int(8*(x+y) + 28 - elevation)
}

const (
	skirmishStageSpawn = iota
	skirmishStageSelect
	skirmishStageOrder
	skirmishStageFight
	skirmishStageReset
	skirmishStageDone
)

// skirmishScript sends real selection and order presses through the scene's
// normal Update, then lets the fight play. It runs one tick per presented
// frame: the wrapper skips ticks until the probe has inspected the frame that
// the previous tick produced, so no bolt or health change goes unseen.
type skirmishScript struct {
	scene   *skirmishScene // The live scene; a restart replaces it.
	stage   int
	waiting bool // A tick ran whose frame the probe has not inspected yet.
	sent    bool // The press for the current stage went out.
	// Facts the fight must show at least once, each from a presented frame.
	alarmLit, boltDrawn, damageDrawn bool
}

type skirmishSmokeScene struct {
	engine.Scene
	script *skirmishScript
}

func (s *skirmishSmokeScene) Update(tick engine.Tick) (engine.Transition, error) {
	script := s.script
	if script.waiting {
		return engine.Transition{}, nil
	}
	script.waiting = true
	tick.Input = script.input()
	return s.Scene.Update(tick)
}

// input returns this tick's presses. Mouse positions are logical pixels.
func (k *skirmishScript) input() engine.Input {
	switch {
	case k.stage == skirmishStageSelect && !k.sent:
		k.sent = true
		x, y := skirmishScreen(1.5, 2.5, 0)
		return engine.Input{
			PrimaryPress: engine.MousePress{X: float64(x), Y: float64(y - 7), Valid: true},
		}
	case k.stage == skirmishStageOrder && !k.sent:
		k.sent = true
		x, y := skirmishScreen(6.5, 2.5, 0)
		return engine.Input{
			SecondaryPress: engine.MousePress{X: float64(x), Y: float64(y), Valid: true},
		}
	case k.stage == skirmishStageReset && !k.sent:
		k.sent = true
		return engine.Input{Pressed: engine.KeyR}
	}
	return engine.Input{}
}

// check validates the frame just drawn and reports whether the stage is complete.
func (k *skirmishScript) check(viewport image.Rectangle) (bool, error) {
	scene := k.scene
	sim := scene.sim
	switch k.stage {
	case skirmishStageSpawn:
		return true, skirmishAtSpawn(viewport, scene)
	case skirmishStageSelect:
		if !k.sent {
			return false, nil
		}
		x, y := skirmishScreen(1.5, 2.5, 0)
		if scene.selected != 1 || !pixelIs(viewport, x, y-skirmishUnitHeight-7, selectionColor) {
			return false, errors.New("frame did not show the selection marker over scout 1")
		}
		return true, nil
	case skirmishStageOrder:
		if !k.sent {
			return false, nil
		}
		x, y := skirmishScreen(6.5, 2.5, 0)
		u, _ := sim.Unit(1)
		if goal, moving := sim.Goal(u); !moving || goal != (grid.Cell{X: 6, Y: 2}) ||
			!pixelIs(viewport, x, y, destinationColor) {
			return false, errors.New("frame did not show the destination marker at 6, 2")
		}
		return true, nil
	case skirmishStageFight:
		return k.checkFight(viewport)
	case skirmishStageReset:
		if !k.sent || sim.Ticks() != 0 {
			return false, nil // Wait for the restarted scene to reach the screen.
		}
		return true, skirmishAtSpawn(viewport, scene)
	}
	return false, nil
}

// checkFight inspects every frame of the fight. It is complete when both guards
// and scout 1 are gone: the alarm rang, bolts were drawn, health bars showed
// damage, and dead units and the selection left the screen.
func (k *skirmishScript) checkFight(viewport image.Rectangle) (bool, error) {
	sim := k.scene.sim
	// A unit or bolt may cover a cell of the alarm line, so count the line's color over the whole
	// frame.
	loud, quiet := countPixels(viewport, skirmishAlarmLoud), countPixels(viewport, skirmishAlarmQuiet)
	switch {
	case sim.Alarm() && (loud == 0 || quiet != 0):
		return false, errors.New("frame did not show the whole alarm line lit after a scout crossed it")
	case !sim.Alarm() && (quiet == 0 || loud != 0):
		return false, errors.New("frame did not show the quiet alarm line before the alarm")
	}
	k.alarmLit = k.alarmLit || loud != 0
	if bolts := sim.Bolts(); len(bolts) > 0 {
		fp := bolts[0].Body.Footprint
		x, y := skirmishScreen(fp.Min.X.Add((fp.Max.X-fp.Min.X)/2).Float64(),
			fp.Min.Y.Add((fp.Max.Y-fp.Min.Y)/2).Float64(), float64(bolts[0].Body.Vertical.Bottom))
		if !pixelIs(viewport, x, y, skirmishBoltColor) {
			return false, fmt.Errorf("frame did not show the bolt at %d, %d", x, y)
		}
		k.boltDrawn = true
	}
	for _, u := range sim.Units() {
		if !u.Alive || u.HP >= u.MaxHP {
			continue
		}
		c := u.Center()
		x, y := skirmishScreen(c.X.Float64(), c.Y.Float64(), 0)
		barTop := y - skirmishUnitHeight - 3 // Mid-bar, so a fractional position never lands on its edge.
		// The bar is 12 pixels wide. A unit behind the deck or the wall is rightly covered, so only a
		// bar that shows at either end is checked: its left end is fill and its right end is the
		// backing.
		shows := pixelIs(viewport, x-5, barTop, skirmishHealthColor) ||
			pixelIs(viewport, x+5, barTop, skirmishHealthBack)
		if !shows {
			continue
		}
		if !pixelIs(viewport, x+5, barTop, skirmishHealthBack) ||
			!pixelIs(viewport, x-5, barTop, skirmishHealthColor) {
			return false, fmt.Errorf(
				"frame did not show the damaged health bar of unit %d (health %d)",
				u.ID,
				u.HP,
			)
		}
		k.damageDrawn = true
	}
	scout1, _ := sim.Unit(1)
	guard11, _ := sim.Unit(11)
	guard12, _ := sim.Unit(12)
	if scout1.Alive || guard11.Alive || guard12.Alive {
		return false, nil
	}
	switch {
	case !k.alarmLit || !k.boltDrawn || !k.damageDrawn:
		return false, fmt.Errorf(
			"the fight ended without showing the alarm (%v), a bolt (%v), and damage (%v)",
			k.alarmLit,
			k.boltDrawn,
			k.damageDrawn,
		)
	case countPixels(viewport, skirmishGuardColor) != 0:
		return false, errors.New("frame still showed a guard after both were defeated")
	case countPixels(viewport, selectionColor) != 0:
		return false, errors.New("frame still showed the selection marker after its scout fell")
	case countPixels(viewport, skirmishScoutColor) == 0:
		return false, errors.New("frame did not show the surviving scout")
	}
	return true, nil
}

// skirmishAtSpawn checks that a frame shows the opening: four full-health
// units, a quiet alarm line, the wall, and the deck.
func skirmishAtSpawn(viewport image.Rectangle, scene *skirmishScene) error {
	for _, u := range scene.sim.Units() {
		c := u.Center()
		x, y := skirmishScreen(c.X.Float64(), c.Y.Float64(), 0)
		want := skirmishGuardColor
		if u.Team == skirmish.Scouts {
			want = skirmishScoutColor
		}
		barTop := y - skirmishUnitHeight - 3 // Mid-bar, so a fractional position never lands on its edge.
		if !pixelIs(viewport, x, y-7, want) || !pixelIs(viewport, x-5, barTop, skirmishHealthColor) ||
			!pixelIs(viewport, x+5, barTop, skirmishHealthColor) {
			return fmt.Errorf("frame did not show unit %d with a full health bar", u.ID)
		}
	}
	alarmX, alarmY := skirmishScreen(5.5, 0.5, 0)
	wallX, wallY := skirmishScreen(3.5, 2.5, 0)
	deckX, deckY := skirmishScreen(6.5, 3.5, skirmishDeckHeight)
	if !pixelIs(viewport, alarmX, alarmY, skirmishAlarmQuiet) ||
		!pixelIs(viewport, wallX, wallY-9, courtyardWallColor) ||
		!pixelIs(viewport, deckX, deckY, skirmishDeckColor) ||
		countPixels(viewport, selectionColor) != 0 {
		return errors.New(
			"frame did not show the quiet alarm line, the wall, the deck, and no selection",
		)
	}
	return nil
}

// countPixels counts the pixels of one exact color in the whole viewport.
func countPixels(viewport image.Rectangle, want color.NRGBA) int {
	pixels := make([]byte, viewport.Dx()*viewport.Dy()*4)
	//nolint:gosec // G115: the viewport lies inside the GLFW framebuffer's C int bounds.
	gl.ReadPixels(
		int32(viewport.Min.X),
		int32(viewport.Min.Y),
		int32(viewport.Dx()),
		int32(viewport.Dy()),
		gl.RGBA,
		gl.UNSIGNED_BYTE,
		gl.Ptr(&pixels[0]),
	)
	count := 0
	for i := 0; i+3 < len(pixels); i += 4 {
		if pixels[i] == want.R && pixels[i+1] == want.G && pixels[i+2] == want.B &&
			pixels[i+3] == want.A {
			count++
		}
	}
	return count
}

func runSkirmishSmoke() error {
	loaded, err := loadAuthored(authoredFiles)
	if err != nil {
		return err
	}
	board, err := loadSkirmish(authoredFiles)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	script := &skirmishScript{}
	desktop.FrameProbe = func(viewport image.Rectangle) error {
		if script.stage >= skirmishStageDone || script.scene == nil || script.scene.sim == nil {
			return nil
		}
		script.waiting = false
		complete, err := script.check(viewport)
		if err != nil {
			return fmt.Errorf("stage %d, tick %d: %w", script.stage, script.scene.sim.Ticks(), err)
		}
		if complete {
			script.stage, script.sent = script.stage+1, false
			if script.stage == skirmishStageDone {
				cancel()
			}
		}
		return nil
	}
	defer func() { desktop.FrameProbe = nil }()
	if err := platform.Run(ctx, loaded.config(), func() engine.Scene {
		script.scene = &skirmishScene{board: board}
		return &skirmishSmokeScene{Scene: script.scene, script: script}
	}); err != nil {
		return fmt.Errorf("native skirmish smoke: %w", err)
	}
	if script.stage != skirmishStageDone {
		return fmt.Errorf("native skirmish smoke ended during stage %d", script.stage)
	}
	return nil
}
