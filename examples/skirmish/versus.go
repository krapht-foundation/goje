package skirmish

import (
	"errors"

	"github.com/krapht-foundation/goje/engine/fixed"
)

// Tactics constants (M17).
const (
	// A hit AI unit wakes allies whose centers are this close to its own.
	wakeRadius = 6 * fixed.One
	// When this many AI units hunt one enemy, every second one flanks.
	flankers = 3
	// A flank waypoint is this many cells to the side of the line of approach.
	flankOffset = 3
	// A falling-back unit looks this many cells around itself for cover.
	fallBackReach = 6

	noWaypoint = -1
	flankDone  = -2
)

// SetControl sets whether a team is commanded by a player or run by the AI,
// as specs/skirmish-m13.md describes under versus (M17). By default the
// scouts are human and the guards are AI. A human-controlled team runs no AI,
// is never alarmed, and keeps every order it is given; human-controlled guards
// also get the scouts' health, so that a versus match is symmetric. It must be
// called before the first Step.
func (s *Sim) SetControl(team Team, human bool) error {
	switch {
	case s.tick != 0:
		return errors.New("skirmish: control must be set before the first Step")
	case team != Scouts && team != Guards:
		return errors.New("skirmish: unknown team")
	}
	s.human[team-1] = human
	if team == Guards {
		hp := guardHP
		if human {
			hp = scoutHP
		}
		for i := range s.units {
			if u := &s.units[i]; u.Team == Guards {
				u.HP, u.MaxHP = hp, hp
			}
		}
	}
	return nil
}

// SetTactics turns the tactical AI on or off (M17): focus fire, a shared
// wake, a fall back, and a flank, as specs/skirmish-m13.md describes. It is
// off by default and must be set before the first Step.
func (s *Sim) SetTactics(enabled bool) error {
	if s.tick != 0 {
		return errors.New("skirmish: tactics must be set before the first Step")
	}
	s.tactics = enabled
	return nil
}

// Winner reports whether the match is decided and who won: a team wins when
// no unit of the other team is alive, and when neither team has a living unit
// the result is a draw, reported as team 0.
func (s *Sim) Winner() (Team, bool) {
	var alive [2]bool
	for i := range s.units {
		if u := &s.units[i]; u.Alive {
			alive[u.Team-1] = true
		}
	}
	switch {
	case alive[0] && alive[1]:
		return 0, false
	case alive[0]:
		return Scouts, true
	case alive[1]:
		return Guards, true
	}
	return 0, true
}

// wake lets every AI unit hit this tick wake the allies near it, in the order
// the hits happened: each living ally within wakeRadius that runs the AI
// starts hunting.
func (s *Sim) wake() {
	limit := int64(wakeRadius) * int64(wakeRadius)
	for _, e := range s.happened {
		if e.Kind != EventBoltHit {
			continue
		}
		hit := &s.units[s.index[e.Unit]]
		if !s.ai(hit) {
			continue
		}
		for i := range s.units {
			a := &s.units[i]
			if !a.Alive || a.Team != hit.Team || a.order != Move {
				continue
			}
			d := a.Center().Sub(e.At)
			if dx, dy := int64(d.X), int64(d.Y); dx*dx+dy*dy <= limit {
				a.chasing = true
			}
		}
	}
}

// fallBack moves u, one hit from death while an ally still fights, out of
// weapon range of the enemy t it hunts, and reports whether it did or is
// already out of range. A unit with no free cell out of range fights on.
func (s *Sim) fallBack(u, t *Unit) bool {
	if u.HP > weaponDamage || !s.allyFights(u) {
		return false
	}
	if !inRange(u, t) {
		if u.Moving() && s.covers(u.goal, t) {
			s.finish(u) // It was chasing back into range.
		}
		return true
	}
	if u.Moving() && !s.covers(u.goal, t) {
		return true // Already on its way out.
	}
	s.mark(u, true)
	defer s.unmark()
	from, best := s.cellOf(u.Center()), -1
	var bestD2 int64
	fx, fy := from%s.width, from/s.width
	for y := max(fy-fallBackReach, 0); y <= min(fy+fallBackReach, s.height-1); y++ {
		for x := max(fx-fallBackReach, 0); x <= min(fx+fallBackReach, s.width-1); x++ {
			c := y*s.width + x
			if s.solid[c] || s.occ[c] || s.covers(c, t) {
				continue
			}
			d := s.centerOf(c).Sub(u.Center())
			if d2 := int64(d.X)*int64(d.X) + int64(d.Y)*int64(d.Y); best < 0 || d2 < bestD2 {
				best, bestD2 = c, d2
			}
		}
	}
	return best >= 0 && s.order(u, best, false)
}

// covers reports whether a unit standing on cell's center would have t in weapon range.
func (s *Sim) covers(cell int, t *Unit) bool {
	d := t.Center().Sub(s.centerOf(cell))
	dx, dy := int64(d.X), int64(d.Y)
	return dx*dx+dy*dy <= int64(weaponRange)*int64(weaponRange)
}

// allyFights reports whether another living unit of u's team hunts and has
// more than one hit of health left.
func (s *Sim) allyFights(u *Unit) bool {
	for i := range s.units {
		if a := &s.units[i]; a != u && a.Alive && a.Team == u.Team && a.chasing && a.HP > weaponDamage {
			return true
		}
	}
	return false
}

// flank returns the waypoint u walks through before closing in on t, or a
// negative value when it goes straight. rank counts the AI units of lower ID
// hunting t and count all of them. When count is at least flankers, the units
// of odd rank flank, alternately to the left and the right of the line from
// them to t. A waypoint is used once: when the unit stops walking, by arriving
// or giving up, it closes in directly until it stops hunting.
func (s *Sim) flank(u, t *Unit, rank, count int) int {
	switch {
	case u.waypoint >= 0 && !u.Moving():
		u.waypoint = flankDone
	case u.waypoint == noWaypoint && count >= flankers && rank%2 == 1:
		u.waypoint = s.waypointFor(u, t, rank/2%2 == 0)
	}
	return u.waypoint
}

// waypointFor picks the cell flankOffset cells to one side of the midpoint
// between u and t, across the main axis of the approach, clamped to the
// board. The other side is tried when that cell is solid; flankDone means
// neither will do.
func (s *Sim) waypointFor(u, t *Unit, left bool) int {
	from, to := s.cellOf(u.Center()), s.cellOf(t.Center())
	ux, uy, tx, ty := from%s.width, from/s.width, to%s.width, to/s.width
	mx, my := (ux+tx)/2, (uy+ty)/2
	off := flankOffset
	if !left {
		off = -off
	}
	for range 2 {
		x, y := mx, my+off
		if abs(tx-ux) < abs(ty-uy) {
			x, y = mx+off, my
		}
		x, y = min(max(x, 0), s.width-1), min(max(y, 0), s.height-1)
		if c := y*s.width + x; !s.solid[c] {
			return c
		}
		off = -off
	}
	return flankDone
}

func abs(v int) int { return max(v, -v) }
