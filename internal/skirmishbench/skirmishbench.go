// Package skirmishbench measures the skirmish simulation on a large, repeatable
// battle: 128 scouts against 128 guards on a 64 × 64 board with pillars and an
// alarm line. It is shared by the native and WebAssembly benchmark programs so
// both targets run the same battle, and its trace hash shows whether they
// produced the same state. See RFC 0014 for the acceptance criteria, and RFC
// 0016 for the vision mode.
package skirmishbench

import (
	"fmt"
	"math/rand/v2"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/krapht-foundation/goje/engine/fixed"
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

// Squads and OrderInterval describe the orders mode: the scouts split into
// Squads groups, and every OrderInterval ticks each group gets a new group order.
const (
	Squads        = 6
	OrderInterval = 120
	// SightRadius is the sight radius in cells of the vision mode, the
	// outpost's radius.
	SightRadius = 7
)

// Scene is a battle. Create it with New or NewOrders and advance it with Step.
type Scene struct {
	*skirmish.Sim
	squads [][]uint64 // The scouts of each squad; nil outside the orders mode.
	ticks  int
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
	return &Scene{Sim: sim}, nil
}

// NewOrders builds the same battle but gives no scout a standing order. Instead,
// the scouts form six squads that receive a group order every OrderInterval
// ticks: an attack-move to a cell that sweeps across the board, a move, or an
// attack on the nearest guard. It measures the cost of the commands a player
// issues, on top of the crowd's own movement and fighting.
func NewOrders() (*Scene, error) {
	sim, err := build()
	if err != nil {
		return nil, err
	}
	squads := make([][]uint64, Squads)
	for i := range squads {
		squads[i] = make([]uint64, 0, PerSide/Squads+1)
	}
	n := 0
	for _, u := range sim.Units() {
		if u.Team == skirmish.Scouts {
			squads[n%Squads] = append(squads[n%Squads], u.ID)
			n++
		}
	}
	return &Scene{Sim: sim, squads: squads}, nil
}

// issue gives every squad its next order. The cells sweep through the open
// bands and the pillared middle, so some orders are refused or only partly assigned.
func (s *Scene) issue() {
	round := s.ticks / OrderInterval
	for i, ids := range s.squads {
		switch (round + i) % 3 {
		case 0, 1:
			kind := skirmish.AttackMove
			if (round+i)%3 == 1 {
				kind = skirmish.Move
			}
			goal := grid.Cell{X: 12 + (round*7+i*9)%40, Y: (i*11 + round*13) % Side}
			s.OrderGroup(ids, skirmish.Order{Kind: kind, Goal: goal})
		default:
			if target, ok := s.nearestGuard(ids); ok {
				s.OrderGroup(ids, skirmish.Order{Kind: skirmish.Attack, Target: target})
			}
		}
	}
}

// nearestGuard returns the living guard nearest to the first living scout of ids.
func (s *Scene) nearestGuard(ids []uint64) (uint64, bool) {
	var from fixed.Vec2
	found := false
	for _, id := range ids {
		if u, ok := s.Unit(id); ok && u.Alive {
			from, found = u.Center(), true
			break
		}
	}
	if !found {
		return 0, false
	}
	var best uint64
	var bestDistance int64 = -1
	for i := range s.Units() {
		u := &s.Units()[i]
		if !u.Alive || u.Team != skirmish.Guards {
			continue
		}
		c := u.Center()
		dx, dy := int64(c.X.Sub(from.X)), int64(c.Y.Sub(from.Y))
		if d := dx*dx + dy*dy; bestDistance < 0 || d < bestDistance {
			best, bestDistance = u.ID, d
		}
	}
	return best, best != 0
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
	if s.squads != nil && s.ticks%OrderInterval == 0 {
		s.issue()
	}
	s.ticks++
	err := s.Sim.Step()
	return time.Since(start), err
}

// Stats holds sorted-sample percentiles in microseconds.
type Stats struct{ Median, P95, P99, Max float64 }

// Report is the result of Run, RunOrders, or RunVision.
type Report struct {
	// Mode is "battle" for Run, "orders" for RunOrders, and "vision" for RunVision.
	Mode         string
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
	return measure("battle", scene, warmup, ticks)
}

// RunOrders is Run for the orders mode of NewOrders.
func RunOrders(warmup, ticks int) (Report, error) {
	scene, err := NewOrders()
	if err != nil {
		return Report{}, err
	}
	return measure("orders", scene, warmup, ticks)
}

// NewVision is NewOrders with vision on at SightRadius, so the squads' group
// orders, the scouts' fire, and the guards' waking all depend on line of
// sight, and every unit that changes cell recasts its view.
func NewVision() (*Scene, error) {
	scene, err := NewOrders()
	if err != nil {
		return nil, err
	}
	if err := scene.EnableVision(skirmish.VisionConfig{Radius: SightRadius}); err != nil {
		return nil, err
	}
	return scene, nil
}

// RunVision is Run for the vision mode of NewVision.
func RunVision(warmup, ticks int) (Report, error) {
	scene, err := NewVision()
	if err != nil {
		return Report{}, err
	}
	return measure("vision", scene, warmup, ticks)
}

func measure(mode string, scene *Scene, warmup, ticks int) (Report, error) {
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
		Mode: mode, Units: len(scene.Units()), Ticks: ticks, Tick: percentiles(samples),
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
