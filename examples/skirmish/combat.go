package skirmish

import (
	"errors"

	"github.com/krapht-foundation/goje/engine/collide"
	"github.com/krapht-foundation/goje/engine/fixed"
)

// Every unit carries the same weapon, so a fight is decided by numbers, health, and position.
const (
	scoutHP        = 40
	guardHP        = 30
	weaponRange    = 4 * fixed.One // Center to center.
	weaponDamage   = 10
	weaponCooldown = 30 // Ticks between shots.

	// A guard at its post notices a scout this close, and keeps hunting it until it is
	// farther than leash. An alarm sends guards after the nearest scout at any distance.
	sight = 6 * fixed.One
	leash = 12 * fixed.One
	// A hunting guard routes again to its target this often, offset by its ID.
	chasePeriod = 15

	boltSize   fixed.Num = 32 // Footprint side, 1/8 tile.
	boltSpeed  fixed.Num = 64 // Travel per tick, 1/4 tile.
	boltBottom           = 8
	boltTop              = 12 // A bolt flies at chest height: under a raised deck, into a unit.
	boltLife             = 24 // Ticks before a bolt that hit nothing fades.
	// The bolt starts this far from the shooter's center along the aim. The distance is
	// at least half a unit plus half a bolt on one axis for every direction, so a new
	// bolt never overlaps its shooter.
	muzzle fixed.Num = 96

	boltIDBase = 1 << 40
	fnvOffset  = 14695981039346656037
	fnvPrime   = 1099511628211
)

// Bolt is a projectile. It is swept through the world each tick, so a fast bolt
// cannot skip over a thin wall or unit. It is not stored in the world's bodies,
// so units walk through bolts and bolts do not block each other.
type Bolt struct {
	ID       uint64
	Body     collide.Shape
	Velocity fixed.Vec2
	Life     int
}

// Bolts returns the bolts in flight in the order they were fired. The slice is
// valid until the next Step and must not be modified.
func (s *Sim) Bolts() []Bolt { return s.bolts }

// nearest finds the living enemy of u whose center is within radius, preferring
// the closest and then the lowest ID. It returns the squared distance in raw
// units. With sighted, and vision on, it ignores enemies u's team does not see.
func (s *Sim) nearest(u *Unit, radius fixed.Num, sighted bool) (*Unit, int64, bool) {
	c := u.Center()
	query := fixed.Rect{
		Min: fixed.Vec2{X: c.X.Sub(radius), Y: c.Y.Sub(radius)},
		Max: fixed.Vec2{X: c.X.Add(radius), Y: c.Y.Add(radius)},
	}
	s.ids = s.bodies.Overlaps(s.ids[:0], query, collide.Interval{Bottom: 0, Top: BodyTop})
	var best *Unit
	var bestD2 int64
	limit := int64(radius) * int64(radius)
	for _, id := range s.ids {
		t := &s.units[s.index[id]]
		if t.Team == u.Team || t.HP <= 0 || (sighted && !s.sees(u, t)) {
			continue
		}
		tc := t.Center()
		dx, dy := int64(tc.X.Sub(c.X)), int64(tc.Y.Sub(c.Y))
		if d2 := dx*dx + dy*dy; d2 <= limit && (best == nil || d2 < bestD2) {
			best, bestD2 = t, d2
		}
	}
	return best, bestD2, best != nil
}

// decide runs the guard AI: a guard at its post wakes when a scout comes within
// sight or the alarm rings, then walks toward the nearest scout until that
// scout is in weapon range, and goes back to its post when none is within the
// leash. A guard carrying an attack or attack-move order follows the order
// instead and returns to the AI when the order ends.
//
// With vision on, a guard that is not alarmed considers only scouts within the
// leash in cells its team sees, so it wakes when its team sees a scout, and
// any guard stops walking for a scout in range only while it is in sight. An
// alarmed guard still hunts the nearest scout at any distance without sight.
func (s *Sim) decide() {
	whole := fixed.FromInt(max(s.width, s.height))
	for i := range s.units {
		u := &s.units[i]
		if !u.Alive || u.Team != Guards || u.order != Move {
			continue
		}
		radius := sight
		switch {
		case u.alarmed:
			radius = whole
		case u.chasing || s.radius != 0:
			radius = leash
		}
		target, d2, ok := s.nearest(u, radius, !u.alarmed)
		if !ok {
			if u.chasing {
				s.finish(u)
			}
			u.chasing, u.alarmed = false, false
			continue
		}
		u.chasing = true
		if d2 <= int64(weaponRange)*int64(weaponRange) && s.sees(u, target) {
			s.finish(u)
			continue
		}
		if !u.Moving() || (s.tick+u.ID)%chasePeriod == 0 {
			// A new route must not hide how long the guard has been stuck behind others.
			blocked, repaths := u.blocked, u.repaths
			if s.order(u, s.cellOf(target.Center()), false) {
				u.blocked, u.repaths = blocked, repaths
			}
		}
	}
}

// reportTriggers feeds the scouts to the trigger tracker and wakes every guard
// the first time one enters an alarm zone.
func (s *Sim) reportTriggers() {
	s.shapes = s.shapes[:0]
	for i := range s.units {
		if u := &s.units[i]; u.Alive && u.Team == Scouts {
			s.shapes = append(s.shapes, u.Body)
		}
	}
	s.events = s.tracker.Update(s.events[:0], s.shapes)
	for _, e := range s.events {
		if !e.Enter || !s.alarmZones[e.Trigger] || s.alarm {
			continue
		}
		s.alarm = true
		s.report(EventAlarm, &s.units[s.index[e.Body]])
		for i := range s.units {
			if u := &s.units[i]; u.Alive && u.Team == Guards {
				u.alarmed = true
			}
		}
	}
}

// fire lets every ready unit shoot, in ascending ID order: a unit attacking a
// target shoots only that target, every other unit its nearest enemy in range,
// and a unit that holds its fire does not shoot. A held weapon still cools down.
func (s *Sim) fire() {
	for i := range s.units {
		u := &s.units[i]
		if !u.Alive || u.HP <= 0 {
			continue
		}
		if u.cooldown > 0 {
			u.cooldown--
			continue
		}
		if len(s.bolts) == MaxBolts || u.hold {
			continue
		}
		if target, ok := s.weaponTarget(u); ok {
			s.spawn(u, target)
			u.cooldown = weaponCooldown
		}
	}
}

// weaponTarget picks whom u shoots at this tick. With vision on, a target must
// stand in a cell u's team sees.
func (s *Sim) weaponTarget(u *Unit) (*Unit, bool) {
	if u.order != Attack {
		t, _, ok := s.nearest(u, weaponRange, true)
		return t, ok
	}
	t := &s.units[s.index[u.target]]
	return t, t.Alive && t.HP > 0 && inRange(u, t) && s.sees(u, t)
}

// spawn fires a bolt from u toward target. A bolt that starts on a wall or unit
// is spent at once, and one that would start off the board is dropped.
func (s *Sim) spawn(u, target *Unit) {
	aim := target.Center().Sub(u.Center())
	velocity := aim.WithLength(boltSpeed)
	if velocity == (fixed.Vec2{}) {
		return
	}
	at := u.Center().Add(aim.WithLength(muzzle))
	half := boltSize / 2
	s.boltSeq++
	s.report(EventBoltFired, u)
	b := Bolt{
		Velocity: velocity,
		Life:     boltLife,
		Body: collide.Shape{
			ID: boltIDBase + s.boltSeq,
			Footprint: fixed.Rect{
				Min: fixed.Vec2{X: at.X.Sub(half), Y: at.Y.Sub(half)},
				Max: fixed.Vec2{X: at.X.Add(half), Y: at.Y.Add(half)},
			},
			Vertical: collide.Interval{Bottom: boltBottom, Top: boltTop},
		},
	}
	b.ID = b.Body.ID
	if f := b.Body.Footprint; f.Min.X < 0 || f.Min.Y < 0 ||
		f.Max.X > fixed.FromInt(s.width) || f.Max.Y > fixed.FromInt(s.height) {
		return
	}
	if !s.settle(&b) {
		s.bolts = append(s.bolts, b)
	}
}

// settle reports whether a bolt already overlaps a wall or a unit, as it does
// when it starts there or a unit walks into it. A unit it overlaps takes the
// damage, the lowest ID first.
func (s *Sim) settle(b *Bolt) bool {
	for _, h := range s.world.Overlaps(s.hits[:0], b.Body.Footprint, b.Body.Vertical) {
		if !h.Trigger {
			return true
		}
	}
	s.ids = s.bodies.Overlaps(s.ids[:0], b.Body.Footprint, b.Body.Vertical)
	if len(s.ids) == 0 {
		return false
	}
	s.hurt(s.ids[0])
	return true
}

// flyBolts moves every bolt, in the order they were fired. A bolt that is
// stopped by anything, or runs out of life, is removed, and a unit it stopped at takes damage.
func (s *Sim) flyBolts() error {
	live := s.bolts[:0]
	for _, b := range s.bolts {
		result, err := s.world.MoveAmong(s.bodies, b.Body, b.Velocity.X, b.Velocity.Y)
		if err != nil {
			if !errors.Is(err, collide.ErrEmbedded) {
				return err
			}
			s.settle(&b)
			continue
		}
		b.Body = result.Body
		b.Life--
		if result.X.Blocked || result.Y.Blocked {
			s.hurt(max(result.X.Body, result.Y.Body))
			continue
		}
		if b.Life > 0 {
			live = append(live, b)
		}
	}
	s.bolts = live
	return nil
}

// hurt damages the unit with the given ID. Any other ID, such as a wall, is ignored.
func (s *Sim) hurt(id uint64) {
	if i, ok := s.index[id]; ok {
		s.units[i].HP -= weaponDamage
		s.report(EventBoltHit, &s.units[i])
	}
}

// reap removes units whose health ran out, after everything else this tick, so
// that the order of the loops above never decides who got to act.
func (s *Sim) reap() {
	for i := range s.units {
		u := &s.units[i]
		if !u.Alive || u.HP > 0 {
			continue
		}
		u.Alive = false
		s.report(EventUnitFell, u)
		s.bodies.Remove(u.ID)
		u.route, u.next = u.route[:0], 0
		u.order, u.target = Move, 0
	}
}

// fold mixes the tick's state into the trace.
func (s *Sim) fold() {
	h := mix(s.trace, s.tick)
	for i := range s.units {
		u := &s.units[i]
		flags := int64(0)
		if u.Alive {
			flags |= 1
		}
		if u.chasing {
			flags |= 2
		}
		if u.alarmed {
			flags |= 4
		}
		h = mix(h, u.ID)
		h = mixInt(h, int64(u.Body.Footprint.Min.X))
		h = mixInt(h, int64(u.Body.Footprint.Min.Y))
		h = mixInt(h, int64(u.HP))
		h = mixInt(h, int64(u.goal))
		h = mixInt(h, int64(u.cooldown))
		h = mixInt(h, flags)
		// Order state joins the trace only when it differs from the default,
		// so histories without the player's orders keep their M13 traces.
		if u.order != Move || u.target != 0 || u.hold {
			hold := int64(0)
			if u.hold {
				hold = 1
			}
			h = mixInt(h, int64(u.order))
			h = mix(h, u.target)
			h = mixInt(h, hold)
		}
	}
	for i := range s.bolts {
		b := &s.bolts[i]
		h = mix(h, b.ID)
		h = mixInt(h, int64(b.Body.Footprint.Min.X))
		h = mixInt(h, int64(b.Body.Footprint.Min.Y))
		h = mixInt(h, int64(b.Life))
	}
	// Vision state joins the trace only when vision is on, so histories
	// without it keep their M13 and M14 traces.
	if s.radius != 0 {
		for _, w := range s.explored {
			h = mix(h, w)
		}
		for i := range s.units {
			if u := &s.units[i]; u.order == Attack {
				h = mixInt(h, int64(u.lastSeen))
			}
		}
	}
	s.trace = h
}

// mix folds one value into an FNV-1a style hash.
func mix(h, v uint64) uint64 {
	return (h ^ v) * fnvPrime
}

// mixInt folds a signed value by its bit pattern, so negative values hash distinctly.
func mixInt(h uint64, v int64) uint64 {
	return mix(h, uint64(v)) // #nosec G115 -- only the bit pattern matters.
}
