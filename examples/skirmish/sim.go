// Package skirmish is the reference game simulation for RFC 0014: units that follow
// grid routes through a collision world and block each other, fight with
// projectiles, and are driven by a small guard AI. It owns game rules, not
// engine contracts: Goje supplies exact arithmetic, collision, and routing, and
// this package decides what a unit is and what an order means. It draws nothing
// and reads no clock, so one input history gives one result on every target.
//
// A Sim is for one goroutine. Call Command between ticks and Step once per tick.
package skirmish

import (
	"errors"
	"fmt"

	"github.com/krapht-foundation/goje/engine/collide"
	"github.com/krapht-foundation/goje/engine/fixed"
	"github.com/krapht-foundation/goje/engine/grid"
	"github.com/krapht-foundation/goje/level"
)

const (
	// MaxUnits is the largest number of units in a level.
	MaxUnits = 256
	// MaxBolts is the largest number of bolts in flight. A unit that would fire
	// while the pool is full keeps its shot until a later tick.
	MaxBolts = 512

	// BodySize is a unit's footprint side, 3/8 tile, in 1/256 tile.
	BodySize fixed.Num = 96
	// BodyTop is the height of a unit in projected pixels.
	BodyTop = 20
	// WallTop is the height of walls in projected pixels.
	WallTop = 32
	// Speed is how far a unit walks along a route each tick, in 1/256 tile.
	Speed fixed.Num = 16

	// A unit that has been blocked this many ticks asks for a new route. The
	// request is honored on ticks chosen by the unit's ID, every repathPeriod
	// ticks, so a jam does not search for every unit at once.
	stuckTicks   = 30
	repathPeriod = 30
	// An order gives up after this many new routes, so every order ends.
	maxRepaths = 3
)

// Team says which side a unit fights for.
type Team uint8

const (
	// Scouts are the player's units. They move only when commanded.
	Scouts Team = iota + 1
	// Guards are driven by the guard AI.
	Guards
)

// Unit is one body in the simulation. Callers read it; only the Sim changes it.
type Unit struct {
	ID    uint64
	Team  Team
	Body  collide.Shape
	HP    int // Health left; a unit with none is removed at the end of the tick.
	MaxHP int
	Alive bool

	route            []uint16 // Cell indices; route[next:] is still to walk.
	next             int
	cell             int // The last cell center the unit reached.
	heading          int // The cell center the unit walks toward when off center.
	goal             int // The final cell of the current order, or -1.
	blocked          int // Consecutive ticks the unit could not move.
	repaths          int // New routes asked for during the current order.
	cooldown         int // Ticks until the weapon is ready.
	chasing, alarmed bool
}

// Center returns the middle of the unit's footprint in ground tiles.
func (u *Unit) Center() fixed.Vec2 { return center(u.Body.Footprint) }

// Moving reports whether the unit has an order.
func (u *Unit) Moving() bool { return u.next < len(u.route) }

// Sim is the whole simulation. Create it with New.
type Sim struct {
	width, height int
	world         *collide.World
	bodies        *collide.Bodies
	tracker       *collide.Tracker
	router        *grid.Router
	units         []Unit
	index         map[uint64]int
	bolts         []Bolt
	boltSeq       uint64
	alarmZones    map[uint64]bool
	alarm         bool
	tick          uint64
	trace         uint64

	solid   []bool // Cells a unit can never enter: walls and solids that reach its height.
	occ     []bool // Cells held by units, for the plan in progress.
	marked  []int
	scratch []grid.Cell
	walk    func(x, y int) bool

	ids    []uint64
	shapes []collide.Shape
	events []collide.Event
	hits   []collide.Hit
}

// Goal returns the cell the unit's current order ends at.
func (s *Sim) Goal(u *Unit) (grid.Cell, bool) {
	if !u.Moving() || u.goal < 0 {
		return grid.Cell{}, false
	}
	return grid.Cell{X: u.goal % s.width, Y: u.goal / s.width}, true
}

// New builds a simulation from a decoded level. Placements of kind "scout" and
// "guard" become units, in ascending ID order. A solid zone blocks routes if its
// vertical extent overlaps a unit's; a trigger zone of kind "alarm" wakes every
// guard when a scout enters it. Any other placement kind is an error.
func New(board level.Board) (*Sim, error) {
	w, h := board.Size()
	if w == 0 {
		return nil, errors.New("skirmish: the level is empty")
	}
	solids, triggers, err := collide.LevelShapes(board, collide.Interval{Bottom: 0, Top: WallTop})
	if err != nil {
		return nil, fmt.Errorf("skirmish: %w", err)
	}
	s := &Sim{
		width: w, height: h,
		index:      make(map[uint64]int),
		alarmZones: make(map[uint64]bool),
		solid:      make([]bool, w*h),
		occ:        make([]bool, w*h),
		marked:     make([]int, 0, MaxUnits),
		scratch:    make([]grid.Cell, 0, w*h),
		bolts:      make([]Bolt, 0, MaxBolts),
		ids:        make([]uint64, 0, MaxUnits),
		shapes:     make([]collide.Shape, 0, MaxUnits),
		events:     make([]collide.Event, 0, MaxUnits),
		hits:       make([]collide.Hit, 0, 16),
		trace:      fnvOffset,
	}
	s.walk = func(x, y int) bool { c := y*s.width + x; return !s.solid[c] && !s.occ[c] }
	for _, shape := range solids {
		if shape.Vertical.Overlaps(collide.Interval{Bottom: 0, Top: BodyTop}) {
			markCells(s.solid, w, shape.Footprint)
		}
	}
	for z := range board.Zones() {
		if z.Kind == "alarm" && z.Role == level.RoleTrigger {
			s.alarmZones[uint64(z.ID)] = true
		}
	}
	if s.world, err = collide.New(w, h, solids, triggers); err != nil {
		return nil, fmt.Errorf("skirmish: %w", err)
	}
	if s.bodies, err = collide.NewBodies(w, h); err != nil {
		return nil, fmt.Errorf("skirmish: %w", err)
	}
	s.tracker = collide.NewTracker(s.world)
	if s.router, err = grid.NewRouter(w, h); err != nil {
		return nil, fmt.Errorf("skirmish: %w", err)
	}
	if err := s.addUnits(board); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Sim) addUnits(board level.Board) error {
	for p := range board.Placements() {
		var team Team
		var hp int
		switch p.Kind {
		case "scout":
			team, hp = Scouts, scoutHP
		case "guard":
			team, hp = Guards, guardHP
		default:
			return fmt.Errorf("skirmish: placement ID %d: unknown kind %q", p.ID, p.Kind)
		}
		if len(s.units) == MaxUnits {
			return fmt.Errorf("skirmish: at most %d units are allowed", MaxUnits)
		}
		cell := p.Y*s.width + p.X
		if s.solid[cell] {
			return fmt.Errorf("skirmish: placement ID %d starts inside a solid", p.ID)
		}
		c := s.centerOf(cell)
		s.units = append(s.units, Unit{
			ID: uint64(p.ID), Team: team, HP: hp, MaxHP: hp, Alive: true,
			Body: collide.Shape{
				ID:        uint64(p.ID),
				Footprint: bodyAt(c),
				Vertical:  collide.Interval{Bottom: 0, Top: BodyTop},
			},
			route: make([]uint16, 0, s.width*s.height),
			cell:  cell, heading: cell, goal: -1,
		})
	}
	// Placements are in file order, so sort by ID to make every loop independent of it.
	sortUnits(s.units)
	for i := range s.units {
		u := &s.units[i]
		s.index[u.ID] = i
		if err := s.bodies.Set(u.Body); err != nil {
			return fmt.Errorf("skirmish: unit %d: %w", u.ID, err)
		}
	}
	return nil
}

// Units returns every unit, dead ones included, in ascending ID order. The slice
// is valid until the next Step and must not be modified.
func (s *Sim) Units() []Unit { return s.units }

// Unit returns the unit with the given ID.
func (s *Sim) Unit(id uint64) (*Unit, bool) {
	i, ok := s.index[id]
	if !ok {
		return nil, false
	}
	return &s.units[i], true
}

// Alarm reports whether a scout has entered an alarm zone.
func (s *Sim) Alarm() bool { return s.alarm }

// Ticks returns the number of Step calls so far.
func (s *Sim) Ticks() uint64 { return s.tick }

// CellAt returns the cell that contains ground position (x, y) in tiles.
func (s *Sim) CellAt(x, y float64) (grid.Cell, bool) { return s.router.CellAt(x, y) }

// Trace is a hash of every unit's position, health, and orders, and of every
// bolt, after each tick so far. It depends on the whole history, so a single
// differing step on any target changes it.
func (s *Sim) Trace() uint64 { return s.trace }

// Command orders a unit to walk to goal. It reports whether the order was
// accepted. An unknown or dead unit, a goal outside the board or inside a solid,
// a goal held by a unit that has no order, and an unreachable goal leave the unit's
// current order alone. Cells held by units that have no order block the route;
// cells held by walking units do not, because those units will move on. A unit
// between cell centers finishes its current edge first. Commanding a stationary
// unit to its own cell does nothing.
func (s *Sim) Command(id uint64, goal grid.Cell) bool {
	u, ok := s.Unit(id)
	if !ok || !u.Alive || goal.X < 0 || goal.Y < 0 || goal.X >= s.width || goal.Y >= s.height {
		return false
	}
	g := goal.Y*s.width + goal.X
	if !s.offCenter(u) && g == u.cell && !u.Moving() {
		return false
	}
	s.mark(u, true)
	defer s.unmark()
	return s.order(u, g, false)
}

// order replaces u's route with one to goal, or leaves the old route if there is
// none. The unit finishes the edge it is on first, unless back is true, which
// sends an off-center unit back to the last center it reached.
func (s *Sim) order(u *Unit, goal int, back bool) bool {
	if s.solid[goal] || s.occ[goal] {
		return false
	}
	start, prefix := u.cell, false
	if s.offCenter(u) {
		prefix = true
		if !back {
			start = u.heading
			if u.Moving() {
				start = int(u.route[u.next])
			}
		}
	}
	path, ok := s.router.AppendRoute(
		s.scratch[:0], grid.Cell{X: start % s.width, Y: start / s.width},
		grid.Cell{X: goal % s.width, Y: goal / s.width}, s.walk)
	if !ok {
		return false
	}
	u.route, u.next = u.route[:0], 0
	if prefix {
		u.route = append(u.route, uint16(start)) // #nosec G115 -- a board has at most 16,384 cells.
	}
	for _, c := range path {
		u.route = append(
			u.route,
			uint16(c.Y*s.width+c.X),
		) // #nosec G115 -- a board has at most 16,384 cells.
	}
	u.goal, u.blocked, u.repaths = goal, 0, 0
	return true
}

// mark records the cells held by other units so the next route avoids them.
// With idleOnly, only units without an order count.
func (s *Sim) mark(skip *Unit, idleOnly bool) {
	for i := range s.units {
		u := &s.units[i]
		if !u.Alive || u == skip || (idleOnly && u.Moving()) {
			continue
		}
		c := s.cellOf(u.Center())
		s.occ[c] = true
		s.marked = append(s.marked, c)
	}
}

func (s *Sim) unmark() {
	for _, c := range s.marked {
		s.occ[c] = false
	}
	s.marked = s.marked[:0]
}

// Step advances the simulation one tick: guards decide, units move in ascending
// ID order, triggers report, weapons fire, bolts fly, and the dead are removed.
// An error means a rule was broken, such as two bodies overlapping, and the
// simulation must be discarded.
func (s *Sim) Step() error {
	s.tick++
	s.decide()
	for i := range s.units {
		if u := &s.units[i]; u.Alive {
			if err := s.walkUnit(u); err != nil {
				return err
			}
		}
	}
	s.reportTriggers()
	s.fire()
	if err := s.flyBolts(); err != nil {
		return err
	}
	s.reap()
	s.fold()
	return nil
}

// walkUnit moves a unit one step along its route and counts how long it has been blocked.
func (s *Sim) walkUnit(u *Unit) error {
	if !u.Moving() {
		u.blocked = 0
		return nil
	}
	c := u.Center()
	for u.Moving() && s.centerOf(int(u.route[u.next])) == c {
		u.cell, u.next = int(u.route[u.next]), u.next+1
	}
	if !u.Moving() {
		s.finish(u)
		return nil
	}
	u.heading = int(u.route[u.next])
	target := s.centerOf(u.heading)
	var dx, dy fixed.Num
	if d := target.X.Sub(c.X); d != 0 {
		dx = clamp(d)
	} else {
		dy = clamp(target.Y.Sub(c.Y))
	}
	result, err := s.world.MoveAmong(s.bodies, u.Body, dx, dy)
	if err != nil {
		return fmt.Errorf("skirmish: move unit %d: %w", u.ID, err)
	}
	u.Body = result.Body
	if err := s.bodies.Set(u.Body); err != nil {
		return fmt.Errorf("skirmish: store unit %d: %w", u.ID, err)
	}
	if result.Moved == (fixed.Vec2{}) {
		u.blocked++
		if u.blocked >= stuckTicks && (s.tick+u.ID)%repathPeriod == 0 {
			s.repath(u)
		}
		return nil
	}
	u.blocked = 0
	if u.Center() == target {
		u.cell, u.next = u.heading, u.next+1
		if !u.Moving() {
			s.finish(u)
		}
	}
	return nil
}

// repath looks for a way around whatever blocks a unit, treating every other
// unit as a wall. If there is none, or the order has already been retried too
// often, the unit gives up and stays where it is.
func (s *Sim) repath(u *Unit) {
	repaths := u.repaths + 1
	s.mark(u, false)
	ok := repaths <= maxRepaths && s.order(u, u.goal, true)
	s.unmark()
	if !ok {
		s.finish(u)
		return
	}
	u.repaths = repaths
}

// finish ends the unit's order, on arrival or when it gives up.
func (s *Sim) finish(u *Unit) {
	u.route, u.next = u.route[:0], 0
	u.goal, u.blocked, u.repaths = -1, 0, 0
}

func (s *Sim) offCenter(u *Unit) bool { return u.Center() != s.centerOf(u.cell) }

// centerOf is the middle of a cell in ground tiles.
func (s *Sim) centerOf(cell int) fixed.Vec2 {
	return fixed.Vec2{
		X: fixed.FromInt(cell%s.width) + fixed.One/2,
		Y: fixed.FromInt(cell/s.width) + fixed.One/2,
	}
}

// cellOf is the cell that contains a ground position.
func (s *Sim) cellOf(p fixed.Vec2) int { return p.Y.Floor()*s.width + p.X.Floor() }

func center(r fixed.Rect) fixed.Vec2 {
	return fixed.Vec2{X: r.Min.X.Add(BodySize / 2), Y: r.Min.Y.Add(BodySize / 2)}
}

func bodyAt(c fixed.Vec2) fixed.Rect {
	half := BodySize / 2
	return fixed.Rect{
		Min: fixed.Vec2{X: c.X.Sub(half), Y: c.Y.Sub(half)},
		Max: fixed.Vec2{X: c.X.Add(half), Y: c.Y.Add(half)},
	}
}

// clamp limits a step to the unit's speed, keeping its sign.
func clamp(d fixed.Num) fixed.Num {
	return max(-Speed, min(Speed, d))
}

func markCells(cells []bool, width int, r fixed.Rect) {
	x0, y0 := r.Min.X.Floor(), r.Min.Y.Floor()
	x1, y1 := r.Max.X.Sub(1).Floor(), r.Max.Y.Sub(1).Floor()
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			cells[y*width+x] = true
		}
	}
}

func sortUnits(units []Unit) {
	// Insertion sort: levels are small, and it avoids an allocation.
	for i := 1; i < len(units); i++ {
		for j := i; j > 0 && units[j].ID < units[j-1].ID; j-- {
			units[j], units[j-1] = units[j-1], units[j]
		}
	}
}
