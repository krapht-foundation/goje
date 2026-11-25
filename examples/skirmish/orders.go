package skirmish

import (
	"slices"

	"github.com/krapht-foundation/goje/engine/fixed"
	"github.com/krapht-foundation/goje/engine/grid"
)

// OrderKind says what an order asks a unit to do.
type OrderKind uint8

const (
	// Move walks to Goal, exactly as Command does.
	Move OrderKind = iota
	// Attack pursues the unit named by Target until it is within weapon range
	// and shoots only at it while it lives.
	Attack
	// AttackMove walks to Goal and stops to fight whenever an enemy is within
	// weapon range, resuming the route when none is.
	AttackMove
)

// Order is an instruction for one unit. Move and AttackMove use Goal, and
// Attack uses Target.
type Order struct {
	Kind   OrderKind
	Goal   grid.Cell
	Target uint64
}

// Assignment is one unit's goal in a group order.
type Assignment struct {
	ID   uint64
	Goal grid.Cell
}

// member is a unit taking part in a group order, with its squared distance in
// raw units from the clicked cell's center.
type member struct {
	index int
	d2    int64
}

// Order gives a unit a new order and reports whether it was accepted. A refused
// order leaves the unit's current order alone. Move and AttackMove follow the
// Command rules. Attack is refused for a dead or unknown target, a target on
// the unit's own team, and a target out of weapon range that no route reaches.
// That route, like every later one, treats idle units other than the target
// as walls. Any order replaces the current one; the hold stance is kept.
func (s *Sim) Order(id uint64, o Order) bool {
	u, ok := s.Unit(id)
	if !ok || !u.Alive {
		return false
	}
	switch o.Kind {
	case Move, AttackMove:
		if !s.routeTo(u, o.Goal) {
			return false
		}
		u.order, u.target = o.Kind, 0
		return true
	case Attack:
		t, ok := s.Unit(o.Target)
		if !ok || !t.Alive || t.Team == u.Team || (!inRange(u, t) && !s.chase(u, t)) {
			return false
		}
		u.order, u.target = Attack, o.Target
		return true
	}
	return false
}

// SetHold sets or clears a unit's hold-fire stance and reports whether the unit
// is alive. A unit that holds its fire never shoots, and an attack-move does not
// stop for enemies, until the stance is cleared. Orders do not change it.
func (s *Sim) SetHold(id uint64, hold bool) bool {
	u, ok := s.Unit(id)
	if !ok || !u.Alive {
		return false
	}
	u.hold = hold
	return true
}

// pursue runs attack orders before anyone moves, in ascending ID order. An
// attack whose target is gone ends, a unit within weapon range of its target
// stops, and one out of range routes toward the target's cell when it has no
// route or every chasePeriod ticks, offset by its ID. A unit that has no route
// and finds none gives up and stands idle.
func (s *Sim) pursue() {
	for i := range s.units {
		u := &s.units[i]
		if !u.Alive || u.order != Attack {
			continue
		}
		t := &s.units[s.index[u.target]]
		switch {
		case !t.Alive:
			s.stand(u)
		case inRange(u, t):
			if u.Moving() {
				s.finish(u)
			}
		case !u.Moving() || (s.tick+u.ID)%chasePeriod == 0:
			// A new route must not hide how long the unit has been stuck behind others.
			blocked, repaths := u.blocked, u.repaths
			if s.chase(u, t) {
				u.blocked, u.repaths = blocked, repaths
			} else if !u.Moving() {
				s.stand(u)
			}
		}
	}
}

// chase routes u toward t's cell, treating idle units other than t as walls.
func (s *Sim) chase(u, t *Unit) bool {
	goal := s.cellOf(t.Center())
	s.mark(u, true)
	s.occ[goal] = false
	ok := s.order(u, goal, false)
	s.unmark()
	return ok
}

// inRange reports whether t's center is within weapon range of u's.
func inRange(u, t *Unit) bool {
	d := t.Center().Sub(u.Center())
	dx, dy := int64(d.X), int64(d.Y)
	return dx*dx+dy*dy <= int64(weaponRange)*int64(weaponRange)
}

// AssignGoals turns a group order at click into one goal per unit and appends
// the assignments to dst, in the order the units take their cells. Unknown,
// dead, and repeated IDs are skipped.
//
// A breadth-first search from click over cells that are walkable and held by no
// idle unit lists cells nearer in steps first, row-major among cells at the
// same distance. The units, sorted by squared distance from their center to the
// center of click and then by ID, take those cells in that order. When there
// are more units than reachable cells, the farthest units get no assignment; a
// click outside the board, on a wall, or on an idle unit yields none. The search
// does not check that each unit can reach its cell, so an order to the cell may
// still be refused. The caller owns dst; with room for len(ids) assignments, or
// MaxUnits, AssignGoals never allocates.
func (s *Sim) AssignGoals(dst []Assignment, ids []uint64, click grid.Cell) []Assignment {
	if click.X < 0 || click.Y < 0 || click.X >= s.width || click.Y >= s.height {
		return dst
	}
	s.collect(ids, s.centerOf(click.Y*s.width+click.X))
	defer s.release()
	s.mark(nil, true)
	defer s.unmark()
	start := click.Y*s.width + click.X
	if s.solid[start] || s.occ[start] {
		return dst
	}
	s.queue = append(s.queue[:0], uint16(start)) // #nosec G115 -- a board has at most 16,384 cells.
	s.seen[start] = true
	taken := 0
	for head := 0; head < len(s.queue) && taken < len(s.group); {
		layer := s.queue[head:]
		slices.Sort(layer)
		for _, c := range layer {
			if taken == len(s.group) {
				break
			}
			cell := int(c)
			dst = append(dst, Assignment{
				ID:   s.units[s.group[taken].index].ID,
				Goal: grid.Cell{X: cell % s.width, Y: cell / s.width},
			})
			taken++
		}
		head += len(layer)
		for _, c := range layer {
			s.expand(int(c))
		}
	}
	return dst
}

// expand queues the unseen open neighbors of cell.
func (s *Sim) expand(cell int) {
	x, y := cell%s.width, cell/s.width
	for _, n := range [4][2]int{{x + 1, y}, {x, y + 1}, {x - 1, y}, {x, y - 1}} {
		if n[0] < 0 || n[1] < 0 || n[0] >= s.width || n[1] >= s.height {
			continue
		}
		c := n[1]*s.width + n[0]
		if !s.seen[c] && !s.solid[c] && !s.occ[c] {
			s.seen[c] = true
			s.queue = append(s.queue, uint16(c)) // #nosec G115 -- a board has at most 16,384 cells.
		}
	}
}

// collect fills s.group with the living units in ids, each once, sorted by
// squared distance to target and then by ID.
func (s *Sim) collect(ids []uint64, target fixed.Vec2) {
	s.group = s.group[:0]
	for _, id := range ids {
		i, ok := s.index[id]
		if !ok || s.grouped[i] || !s.units[i].Alive {
			continue
		}
		s.grouped[i] = true
		d := s.units[i].Center().Sub(target)
		dx, dy := int64(d.X), int64(d.Y)
		s.group = append(s.group, member{index: i, d2: dx*dx + dy*dy})
	}
	// Insertion sort: groups are small, and it avoids an allocation. Units are
	// stored in ID order, so a lower index is a lower ID.
	for i := 1; i < len(s.group); i++ {
		for j := i; j > 0 && (s.group[j].d2 < s.group[j-1].d2 ||
			s.group[j].d2 == s.group[j-1].d2 && s.group[j].index < s.group[j-1].index); j-- {
			s.group[j], s.group[j-1] = s.group[j-1], s.group[j]
		}
	}
}

// release clears the search state that AssignGoals left behind.
func (s *Sim) release() {
	for _, m := range s.group {
		s.grouped[m.index] = false
	}
	for _, c := range s.queue {
		s.seen[c] = false
	}
	s.queue = s.queue[:0]
}

// OrderGroup gives o to every unit in ids and returns how many accepted it. An
// Attack order goes to each unit as given. A Move or AttackMove order is first
// spread by AssignGoals around o.Goal, and each assigned unit then gets an
// ordinary order to its own goal, in assignment order. A unit without an
// assignment, or whose order is refused, keeps its current order. OrderGroup
// does not allocate.
func (s *Sim) OrderGroup(ids []uint64, o Order) int {
	n := 0
	if o.Kind == Attack {
		for _, id := range ids {
			if s.Order(id, o) {
				n++
			}
		}
		return n
	}
	s.assigned = s.AssignGoals(s.assigned[:0], ids, o.Goal)
	for _, a := range s.assigned {
		o.Goal = a.Goal
		if s.Order(a.ID, o) {
			n++
		}
	}
	return n
}
