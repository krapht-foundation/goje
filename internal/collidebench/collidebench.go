// Package collidebench measures the collision package on a dense, repeatable
// scene. It is shared by the native and WebAssembly benchmark programs so both
// targets run the same simulation, and its result hash shows whether they
// produced the same state. See RFC 0013 for the acceptance criteria.
package collidebench

import (
	"fmt"
	"math"
	"math/rand/v2"
	"runtime"
	"slices"
	"time"

	"github.com/krapht-foundation/goje/engine/collide"
	"github.com/krapht-foundation/goje/engine/fixed"
)

const (
	// Side is the board size in tiles.
	Side = 64
	// Bodies is the number of moving bodies.
	Bodies = 2048
	// Triggers is the number of trigger zones.
	Triggers = 64

	pillarEvery = 6  // about one tile in six holds a solid pillar
	bodySize    = 96 // 3/8 of a tile
	maxSpeed    = 24 // units per tick, under a tenth of a tile
)

// Scene is a world with moving bodies. Create it with New and advance it with
// Step. It is deterministic: the same seed produces the same history on every
// target.
type Scene struct {
	world   *collide.World
	tracker *collide.Tracker
	bodies  []collide.Shape
	speeds  []fixed.Vec2
	events  []collide.Event
	hits    uint64
	// Solids is the number of solid pillars in the scene.
	Solids int
}

// New builds the benchmark scene from a fixed seed.
func New() (*Scene, error) {
	// A fixed seed makes the scene repeatable; this is not a security use.
	rng := rand.New(rand.NewPCG(2026, 12)) //nolint:gosec // see above
	height := collide.Interval{Bottom: 0, Top: 32}
	var solids []collide.Shape
	blocked := make([]bool, Side*Side)
	for cell := range blocked {
		if rng.IntN(pillarEvery) != 0 {
			continue
		}
		blocked[cell] = true
		x, y := cell%Side, cell/Side
		solids = append(solids, collide.Shape{
			ID:        uint64(cell) + 1,
			Footprint: tileRect(x, y, x+1, y+1),
			Vertical:  height,
		})
	}
	triggers := make([]collide.Shape, Triggers)
	for i := range triggers {
		x, y := rng.IntN(Side-2), rng.IntN(Side-2)
		triggers[i] = collide.Shape{
			ID:        uint64(i) + 1,
			Footprint: tileRect(x, y, x+2, y+2),
			Vertical:  height,
		}
	}
	world, err := collide.New(Side, Side, solids, triggers)
	if err != nil {
		return nil, fmt.Errorf("build benchmark world: %w", err)
	}

	scene := &Scene{
		world: world, tracker: collide.NewTracker(world),
		bodies: make([]collide.Shape, 0, Bodies), speeds: make([]fixed.Vec2, 0, Bodies),
		events: make([]collide.Event, 0, 4*Bodies), Solids: len(solids),
	}
	for len(scene.bodies) < Bodies {
		cell := rng.IntN(Side * Side)
		if blocked[cell] {
			continue
		}
		// Center the body in a free tile so it never starts inside a pillar.
		x, y := fixed.FromInt(
			cell%Side,
		)+(fixed.One-bodySize)/2, fixed.FromInt(
			cell/Side,
		)+(fixed.One-bodySize)/2
		scene.bodies = append(scene.bodies, collide.Shape{
			ID: uint64(len(scene.bodies)) + 1,
			Footprint: fixed.Rect{
				Min: fixed.Vec2{X: x, Y: y},
				Max: fixed.Vec2{X: x + bodySize, Y: y + bodySize},
			},
			Vertical: collide.Interval{Bottom: 0, Top: 20},
		})
		scene.speeds = append(scene.speeds, fixed.Vec2{
			X: fixed.Num(rng.Int32N(2*maxSpeed+1) - maxSpeed),
			Y: fixed.Num(rng.Int32N(2*maxSpeed+1) - maxSpeed),
		})
	}
	return scene, nil
}

func tileRect(x0, y0, x1, y1 int) fixed.Rect {
	return fixed.Rect{
		Min: fixed.Vec2{X: fixed.FromInt(x0), Y: fixed.FromInt(y0)},
		Max: fixed.Vec2{X: fixed.FromInt(x1), Y: fixed.FromInt(y1)},
	}
}

// Step moves every body once and then reports trigger events. A body that is
// stopped on an axis reverses that axis, so bodies keep bouncing between walls.
// It returns the time spent moving and the time spent on events.
func (s *Scene) Step() (move, trigger time.Duration, err error) {
	start := time.Now()
	for i := range s.bodies {
		result, moveErr := s.world.Move(s.bodies[i], s.speeds[i].X, s.speeds[i].Y)
		if moveErr != nil {
			return 0, 0, fmt.Errorf("move body %d: %w", s.bodies[i].ID, moveErr)
		}
		s.bodies[i] = result.Body
		if result.X.Blocked {
			s.speeds[i].X = s.speeds[i].X.Neg()
		}
		if result.Y.Blocked {
			s.speeds[i].Y = s.speeds[i].Y.Neg()
		}
	}
	moved := time.Now()
	s.events = s.tracker.Update(s.events[:0], s.bodies)
	for _, event := range s.events {
		s.hits = s.hits*1099511628211 ^ event.Trigger<<33 ^ event.Body<<1
		if event.Enter {
			s.hits ^= 1
		}
	}
	return moved.Sub(start), time.Since(moved), nil
}

// Hash summarizes every body position and every trigger event so far. Equal
// hashes after equal tick counts mean two runs produced the same state.
func (s *Scene) Hash() uint64 {
	hash := s.hits
	for _, body := range s.bodies {
		for _, v := range []fixed.Num{body.Footprint.Min.X, body.Footprint.Min.Y} {
			// Float64 is exact for a Num, so its bits identify the value.
			hash = hash*1099511628211 ^ math.Float64bits(v.Float64())
		}
	}
	return hash
}

// Stats holds sorted-sample percentiles in microseconds.
type Stats struct{ Median, P95, P99 float64 }

// Report is the result of Run.
type Report struct {
	Bodies, Solids, Triggers, Ticks int
	// Move, Trigger, and Tick are per-tick costs of moving, of events, and of both.
	Move, Trigger, Tick Stats
	// Mallocs counts heap allocations during the measured ticks.
	Mallocs uint64
	// Hash is Scene.Hash after warm-up and the measured ticks.
	// The ",string" option keeps all 64 bits when a browser parses the JSON.
	Hash uint64 `json:",string"`
}

// Run warms the scene up for warmup ticks and then measures ticks ticks.
func Run(warmup, ticks int) (Report, error) {
	scene, err := New()
	if err != nil {
		return Report{}, err
	}
	for range warmup {
		if _, _, err := scene.Step(); err != nil {
			return Report{}, err
		}
	}
	moves, triggers, totals := make([]float64, ticks), make([]float64, ticks), make([]float64, ticks)
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	for i := range ticks {
		move, trigger, err := scene.Step()
		if err != nil {
			return Report{}, err
		}
		moves[i], triggers[i], totals[i] = micros(move), micros(trigger), micros(move+trigger)
	}
	runtime.ReadMemStats(&after)
	return Report{
		Bodies: Bodies, Solids: scene.Solids, Triggers: Triggers, Ticks: ticks,
		Move: percentiles(moves), Trigger: percentiles(triggers), Tick: percentiles(totals),
		Mallocs: after.Mallocs - before.Mallocs, Hash: scene.Hash(),
	}, nil
}

func micros(d time.Duration) float64 { return float64(d.Nanoseconds()) / 1000 }

func percentiles(samples []float64) Stats {
	slices.Sort(samples)
	at := func(p float64) float64 { return samples[min(int(p*float64(len(samples))), len(samples)-1)] }
	return Stats{Median: at(0.5), P95: at(0.95), P99: at(0.99)}
}
