// Package skirmishbench measures the skirmish simulation on a large, repeatable
// battle: 128 scouts against 128 guards on a 64 × 64 board with pillars and an
// alarm line. It is shared by the native and WebAssembly benchmark programs so
// both targets run the same battle, and its trace hash shows whether they
// produced the same state. See RFC 0014 for the acceptance criteria.
package skirmishbench

import (
	"fmt"
	"math/rand/v2"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/krapht-foundation/goje/engine/grid"
	"github.com/krapht-foundation/goje/examples/skirmish"
	"github.com/krapht-foundation/goje/level"
)

const (
	// Side is the board size in tiles.
	Side = 64
	// PerSide is the number of units on each team.
	PerSide = 128

	pillarEvery = 12 // about one tile in twelve holds a wall
	alarmColumn = 16
	rallyColumn = 24
)

// Scene is a battle. Create it with New and advance it with Step.
type Scene struct {
	*skirmish.Sim
}

// New builds the battle from a fixed seed and orders every scout toward the
// middle of the board, across the alarm line.
func New() (*Scene, error) {
	sim, err := build()
	if err != nil {
		return nil, err
	}
	rally := 0
	for _, u := range sim.Units() {
		if u.Team != skirmish.Scouts {
			continue
		}
		// Two columns of scouts rally in the open band; every scout gets its own row.
		goal := grid.Cell{X: rallyColumn + rally/Side, Y: rally % Side}
		rally++
		if !sim.Command(u.ID, goal) {
			return nil, fmt.Errorf("scout %d cannot reach %v", u.ID, goal)
		}
	}
	return &Scene{sim}, nil
}

// build creates the battle's simulation without giving any orders.
func build() (*skirmish.Sim, error) {
	// A fixed seed makes the scene repeatable; this is not a security use.
	rng := rand.New(rand.NewPCG(2026, 13)) //nolint:gosec // see above
	rows := make([]string, Side)
	for y := range rows {
		var row strings.Builder
		for x := range Side {
			// Keep both armies' edges and the rally columns open.
			open := x < 8 || x >= Side-8 || (x >= rallyColumn-2 && x <= rallyColumn+3) ||
				rng.IntN(pillarEvery) != 0
			row.WriteByte(map[bool]byte{true: '.', false: '#'}[open])
		}
		rows[y] = row.String()
	}
	placements := make([]level.Placement, 0, 2*PerSide)
	for i := range PerSide {
		// A lattice with every other row and column free, so idle units never wall each other in.
		x, y := 2*(i/(Side/2)), 2*(i%(Side/2))
		placements = append(
			placements,
			level.Placement{
				Kind: "scout",
				ID:   uint32(i + 1),
				X:    x,
				Y:    y,
			}, // #nosec G115 -- small.
			level.Placement{
				Kind: "guard",
				ID:   uint32(i + 1001),
				X:    Side - 1 - x,
				Y:    y,
			},
		) // #nosec G115 -- small.
	}
	zones := []level.Zone{{
		Kind: "alarm", ID: 1, Role: level.RoleTrigger,
		X: alarmColumn, Y: 0, W: 1, H: Side, Bottom: 0, Top: 32,
	}}
	data, err := level.Encode(rows, placements, zones)
	if err != nil {
		return nil, fmt.Errorf("encode benchmark level: %w", err)
	}
	board, err := level.Decode(strings.NewReader(string(data)))
	if err != nil {
		return nil, fmt.Errorf("decode benchmark level: %w", err)
	}
	return skirmish.New(board)
}

// Step advances the battle one tick and returns the time it took.
func (s *Scene) Step() (time.Duration, error) {
	start := time.Now()
	err := s.Sim.Step()
	return time.Since(start), err
}

// Stats holds sorted-sample percentiles in microseconds.
type Stats struct{ Median, P95, P99, Max float64 }

// Report is the result of Run.
type Report struct {
	Units, Ticks int
	// Tick is the per-tick cost of Step.
	Tick Stats
	// Scouts and Guards are the units left alive after the last tick.
	Scouts, Guards int
	// Mallocs counts heap allocations during the measured ticks.
	Mallocs uint64
	// Trace is the simulation's trace hash after the last tick. The ",string"
	// option keeps all 64 bits when a browser parses the JSON.
	Trace uint64 `json:",string"`
}

// Run steps the battle for warmup ticks unmeasured and then measures ticks ticks.
func Run(warmup, ticks int) (Report, error) {
	scene, err := New()
	if err != nil {
		return Report{}, err
	}
	for range warmup {
		if _, err := scene.Step(); err != nil {
			return Report{}, err
		}
	}
	samples := make([]float64, ticks)
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	for i := range ticks {
		d, err := scene.Step()
		if err != nil {
			return Report{}, err
		}
		samples[i] = float64(d.Nanoseconds()) / 1000
	}
	runtime.ReadMemStats(&after)
	report := Report{
		Units: len(scene.Units()), Ticks: ticks, Tick: percentiles(samples),
		Mallocs: after.Mallocs - before.Mallocs, Trace: scene.Trace(),
	}
	for _, u := range scene.Units() {
		switch {
		case !u.Alive:
		case u.Team == skirmish.Scouts:
			report.Scouts++
		default:
			report.Guards++
		}
	}
	return report, nil
}

func percentiles(samples []float64) Stats {
	slices.Sort(samples)
	at := func(p float64) float64 { return samples[min(int(p*float64(len(samples))), len(samples)-1)] }
	return Stats{Median: at(0.5), P95: at(0.95), P99: at(0.99), Max: samples[len(samples)-1]}
}
