package main

import (
	"math"
	"math/bits"
	"testing"

	"github.com/krapht-foundation/goje/engine"
	"github.com/krapht-foundation/goje/engine/fixed"
)

// yardHistory is a recorded input history. It walks the scout into the wall and
// along it, through the doorway, back across the board, and under the deck.
var yardHistory = []struct {
	held  engine.Keys
	ticks int
}{
	{engine.KeyArrowRight, 70}, // Slide north along the wall and through the doorway.
	{engine.KeyArrowDown, 50},  // Run along the board's north-east edge.
	{engine.KeyArrowLeft, 80},  // Cross back; the wall stops the scout on the way.
	{engine.KeyW | engine.KeyD, 20},
	{0, 5},
	{engine.KeyArrowDown, 120}, // Slide south past the wall's end and under the deck.
	{engine.KeyArrowDown | engine.KeyArrowLeft, 40},
}

// wantTrace is the hash of the scout's position after every tick of the
// history. It depends on the whole path, so one differing step on any target
// changes it. Update it only after an intended change to the movement rules.
const wantTrace uint64 = 13227287839575903946

type yardReplay struct {
	final                      fixed.Rect
	trace                      uint64
	doorEnters, doorExits      int
	flushTicks, underDeckTicks int
}

func replayYard(t *testing.T) yardReplay {
	t.Helper()
	project, err := loadAuthored(authoredFiles)
	if err != nil {
		t.Fatal(err)
	}
	sim, err := newYardSim(project.layout)
	if err != nil {
		t.Fatal(err)
	}
	var out yardReplay
	wallFace := fixed.FromInt(4) // The authored wall column occupies x in [4, 5).
	lit := false
	for _, segment := range yardHistory {
		for range segment.ticks {
			before := sim.bodies[0].Footprint
			if err := sim.step(segment.held); err != nil {
				t.Fatal(err)
			}
			fp := sim.bodies[0].Footprint
			for _, v := range []fixed.Num{fp.Min.X, fp.Min.Y} {
				// A Num converts to float64 exactly, so its bits identify the value.
				out.trace = bits.RotateLeft64(out.trace^math.Float64bits(v.Float64()), 27) * 1099511628211
			}
			if sim.doorLit != lit {
				if sim.doorLit {
					out.doorEnters++
				} else {
					out.doorExits++
				}
				lit = sim.doorLit
			}
			if fp.Max.X == wallFace && fp != before {
				out.flushTicks++
			}
			if fp.Overlaps(sim.deck) {
				out.underDeckTicks++
			}
		}
	}
	out.final = sim.bodies[0].Footprint
	return out
}

// The replay runs on every target in CI. The expected values are exact
// integers, so native and WebAssembly must agree.
func TestYardReplayIsDeterministicAndShowsEachRule(t *testing.T) {
	t.Parallel()

	got := replayYard(t)
	if again := replayYard(t); again != got {
		t.Fatalf("two replays differ: %+v and %+v", got, again)
	}
	if got.doorEnters != 1 || got.doorExits != 1 {
		t.Errorf(
			"doorway events: %d enters and %d exits, want one of each",
			got.doorEnters,
			got.doorExits,
		)
	}
	if got.flushTicks == 0 {
		t.Error("the scout never moved while flush against the wall, so it did not slide along it")
	}
	if got.underDeckTicks == 0 {
		t.Error("the scout never walked under the deck")
	}
	if got.trace != wantTrace {
		t.Errorf("trajectory hash = %d, want %d", got.trace, wantTrace)
	}
}

func TestYardRestartRestoresTheSpawnAndDoorway(t *testing.T) {
	t.Parallel()

	project, err := loadAuthored(authoredFiles)
	if err != nil {
		t.Fatal(err)
	}
	factory := project.yardFactory()
	var scene *yardCourtyard
	runtime, err := engine.Start(func() engine.Scene {
		scene = factory().(*yardCourtyard)
		return scene
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := runtime.Stop(); err != nil {
			t.Error(err)
		}
	})
	spawn := scene.sim.bodies[0].Footprint
	// Walk until the doorway lamp is lit, so the restart has state to clear.
	for range 200 {
		if err := runtime.Step(engine.Input{Held: engine.KeyArrowRight}); err != nil {
			t.Fatal(err)
		}
		if scene.sim.doorLit {
			break
		}
	}
	if !scene.sim.doorLit || scene.sim.bodies[0].Footprint == spawn {
		t.Fatal("the scout did not reach the doorway")
	}
	previous := scene
	if err := runtime.Step(
		engine.Input{Pressed: engine.KeyR, Held: engine.KeyArrowRight},
	); err != nil {
		t.Fatal(err)
	}
	if scene == previous || scene.sim.bodies[0].Footprint != spawn || scene.sim.doorLit {
		t.Errorf("restart did not restore the spawn and an unlit doorway: %+v", scene.sim)
	}
	if !previous.sim.doorLit {
		t.Error("the restart tick moved the previous scene")
	}
}
