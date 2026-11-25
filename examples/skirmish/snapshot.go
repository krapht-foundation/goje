package skirmish

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/krapht-foundation/goje/engine/collide"
	"github.com/krapht-foundation/goje/engine/fixed"
)

// snapshotVersion is the layout Snapshot writes and Restore reads. See the
// snapshot section of specs/skirmish-m13.md.
const snapshotVersion = 1

// Unit flag bits in a snapshot.
const (
	flagAlive = 1 << iota
	flagChasing
	flagAlarmed
	flagHold
	flagsKnown = flagAlive | flagChasing | flagAlarmed | flagHold
)

var errSnapshot = errors.New("skirmish: invalid snapshot")

// decoder reads little-endian values. A read past the end sets ok to false
// and returns zero, so callers check ok once after a group of reads.
type decoder struct {
	b  []byte
	ok bool
}

// Snapshot returns the simulation's whole dynamic state between ticks: every
// unit with its route and order, the bolts, the alarm, the counters, the trace,
// and the trigger tracker's overlaps. The level is not included; Restore needs
// a Sim built from the same level. The layout is little-endian and versioned,
// as specs/skirmish-m13.md describes. The error is always nil; it is there so
// that a scene can return Snapshot as its own.
func (s *Sim) Snapshot() ([]byte, error) {
	le := binary.LittleEndian
	b := make([]byte, 0, 64+len(s.units)*64+len(s.bolts)*25)
	b = le.AppendUint16(b, snapshotVersion)
	b = le.AppendUint16(
		b,
		uint16(s.width),
	) // #nosec G115 -- a board has at most 128 tiles on a side.
	b = le.AppendUint16(
		b,
		uint16(s.height),
	) // #nosec G115 -- a board has at most 128 tiles on a side.
	b = le.AppendUint16(b, uint16(len(s.units))) // #nosec G115 -- at most MaxUnits units.
	b = le.AppendUint64(b, s.tick)
	b = le.AppendUint64(b, s.trace)
	b = le.AppendUint64(b, s.boltSeq)
	b = append(b, boolByte(s.alarm))
	for i := range s.units {
		u := &s.units[i]
		flags := boolByte(u.Alive)*flagAlive | boolByte(u.chasing)*flagChasing |
			boolByte(u.alarmed)*flagAlarmed | boolByte(u.hold)*flagHold
		b = le.AppendUint64(b, u.ID)
		b = appendNum(b, u.Body.Footprint.Min.X)
		b = appendNum(b, u.Body.Footprint.Min.Y)
		b = le.AppendUint32(
			b,
			uint32(int32(u.HP)),
		) // #nosec G115 -- health stays far inside int32; the bits are kept.
		b = append(b, flags, byte(u.order))
		b = le.AppendUint64(b, u.target)
		b = le.AppendUint16(
			b,
			uint16(u.cell),
		) // #nosec G115 -- a board has at most 16,384 cells.
		b = le.AppendUint16(
			b,
			uint16(u.heading),
		) // #nosec G115 -- a board has at most 16,384 cells.
		b = le.AppendUint32(
			b,
			uint32(int32(u.goal)),
		) // #nosec G115 -- -1 or a cell index; the bits are kept.
		b = le.AppendUint16(
			b,
			uint16(u.blocked),
		) // #nosec G115 -- bounded by stuckTicks+repathPeriod.
		b = append(
			b,
			byte(u.repaths),
			byte(u.cooldown),
		) // #nosec G115 -- at most maxRepaths and weaponCooldown.
		b = le.AppendUint16(b, uint16(u.next))       // #nosec G115 -- at most the route length.
		b = le.AppendUint16(b, uint16(len(u.route))) // #nosec G115 -- at most 16,384 cells.
		for _, c := range u.route {
			b = le.AppendUint16(b, c)
		}
	}
	b = le.AppendUint16(b, uint16(len(s.bolts))) // #nosec G115 -- at most MaxBolts bolts.
	for i := range s.bolts {
		bolt := &s.bolts[i]
		b = le.AppendUint64(b, bolt.ID)
		b = appendNum(b, bolt.Body.Footprint.Min.X)
		b = appendNum(b, bolt.Body.Footprint.Min.Y)
		b = appendNum(b, bolt.Velocity.X)
		b = appendNum(b, bolt.Velocity.Y)
		b = append(b, byte(bolt.Life)) // #nosec G115 -- at most boltLife.
	}
	overlaps := s.tracker.AppendOverlaps(nil)
	b = le.AppendUint32(
		b,
		uint32(len(overlaps)),
	) // #nosec G115 -- at most one pair per trigger and scout.
	for _, o := range overlaps {
		b = le.AppendUint64(b, o.Trigger)
		b = le.AppendUint64(b, o.Body)
	}
	return b, nil
}

// Restore replaces the simulation's dynamic state with a snapshot taken from a
// Sim of the same level. Build the Sim with New first; walls, zones, unit IDs,
// teams, and maximum health come from the level. Restore rejects a snapshot of
// another version, board size, or unit set, a truncated or overlong one, and
// any state a run cannot reach: a count over its limit, a cell index off the
// board, a unit outside the board, inside a solid, or overlapping another unit,
// health out of range, an attack on an unknown unit or a friend, and a bolt
// outside the board. On error the Sim is unchanged.
func (s *Sim) Restore(data []byte) error {
	d := decoder{b: data, ok: true}
	cells := s.width * s.height
	if v := d.u16(); d.ok && v != snapshotVersion {
		return fmt.Errorf("%w: version %d, want %d", errSnapshot, v, snapshotVersion)
	}
	w, h, n := int(d.u16()), int(d.u16()), int(d.u16())
	if d.ok && (w != s.width || h != s.height || n != len(s.units)) {
		return fmt.Errorf(
			"%w: a %d × %d board with %d units does not match this level",
			errSnapshot,
			w,
			h,
			n,
		)
	}
	tick, trace, boltSeq := d.u64(), d.u64(), d.u64()
	alarm := d.u8()
	if d.ok && (alarm > 1 || boltSeq >= boltIDBase) {
		return fmt.Errorf("%w: bad alarm or bolt counter", errSnapshot)
	}

	bodies, err := collide.NewBodies(s.width, s.height)
	if err != nil {
		return fmt.Errorf("skirmish: %w", err)
	}
	units := make([]Unit, len(s.units))
	for i := range units {
		if !d.ok {
			break
		}
		u := &units[i]
		*u = s.units[i]
		u.route = nil
		if err := d.unit(u, cells); err != nil {
			return err
		}
		if !d.ok {
			break
		}
		if err := s.checkUnit(u, bodies); err != nil {
			return err
		}
	}
	for i := range units {
		u := &units[i]
		if u.order == Attack {
			t, ok := s.Unit(u.target)
			if !ok || t.Team == u.Team {
				return fmt.Errorf(
					"%w: unit %d attacks unit %d, which is not an enemy",
					errSnapshot,
					u.ID,
					u.target,
				)
			}
		}
	}

	nbolts := int(d.u16())
	if d.ok && nbolts > MaxBolts {
		return fmt.Errorf("%w: %d bolts exceed the limit of %d", errSnapshot, nbolts, MaxBolts)
	}
	bolts := make([]Bolt, 0, nbolts)
	for range nbolts {
		b, err := s.decodeBolt(&d, boltSeq, bolts)
		if err != nil {
			return err
		}
		if !d.ok {
			break
		}
		bolts = append(bolts, b)
	}

	count := int(d.u32())
	if d.ok && count > len(d.b)/16 {
		return fmt.Errorf("%w: %d tracker overlaps exceed the data", errSnapshot, count)
	}
	overlaps := make([]collide.Overlap, 0, count)
	for range count {
		o := collide.Overlap{Trigger: d.u64(), Body: d.u64()}
		if t, ok := s.Unit(o.Body); d.ok && (!ok || t.Team != Scouts) {
			return fmt.Errorf(
				"%w: tracker overlap names unit %d, which is not a scout",
				errSnapshot,
				o.Body,
			)
		}
		overlaps = append(overlaps, o)
	}
	if !d.ok {
		return fmt.Errorf("%w: truncated", errSnapshot)
	}
	if len(d.b) != 0 {
		return fmt.Errorf("%w: %d bytes of trailing data", errSnapshot, len(d.b))
	}
	if err := s.tracker.Restore(overlaps); err != nil {
		return fmt.Errorf("skirmish: %w", err)
	}

	// Everything is valid: commit. Routes keep their buffers, sized in New.
	for i := range units {
		route := s.units[i].route
		s.units[i] = units[i]
		s.units[i].route = route[:copy(route[:cap(route)], units[i].route)]
	}
	s.bodies = bodies
	s.bolts = append(s.bolts[:0], bolts...)
	s.tick, s.trace, s.boltSeq, s.alarm = tick, trace, boltSeq, alarm == 1
	return nil
}

// unit reads one unit's dynamic state into u, whose static fields are set.
func (d *decoder) unit(u *Unit, cells int) error {
	id := d.u64()
	x, y := d.num(), d.num()
	hp := int(d.i32())
	flags, order := d.u8(), OrderKind(d.u8())
	target := d.u64()
	cell, heading, goal := int(d.u16()), int(d.u16()), int(d.i32())
	blocked, repaths, cooldown := int(d.u16()), int(d.u8()), int(d.u8())
	next, length := int(d.u16()), int(d.u16())
	if !d.ok {
		return nil
	}
	switch {
	case id != u.ID:
		return fmt.Errorf("%w: unit %d where the level has unit %d", errSnapshot, id, u.ID)
	case flags&^flagsKnown != 0 || order > AttackMove || (order == Attack) == (target == 0):
		return fmt.Errorf("%w: unit %d has bad flags or order", errSnapshot, id)
	case cell >= cells || heading >= cells || goal < -1 || goal >= cells || length > cells || next > length:
		return fmt.Errorf("%w: unit %d has a cell off the board", errSnapshot, id)
	case blocked > stuckTicks+repathPeriod || repaths > maxRepaths || cooldown > weaponCooldown:
		return fmt.Errorf("%w: unit %d has a counter out of range", errSnapshot, id)
	case flags&flagAlive != 0 && (hp <= 0 || hp > u.MaxHP), flags&flagAlive == 0 && hp > 0:
		return fmt.Errorf("%w: unit %d has health %d", errSnapshot, id, hp)
	}
	u.route = make([]uint16, length)
	for i := range u.route {
		if u.route[i] = d.u16(); d.ok && int(u.route[i]) >= cells {
			return fmt.Errorf("%w: unit %d has a route cell off the board", errSnapshot, id)
		}
	}
	u.Body.Footprint = fixed.Rect{
		Min: fixed.Vec2{X: x, Y: y},
		Max: fixed.Vec2{X: x.Add(BodySize), Y: y.Add(BodySize)},
	}
	u.HP, u.Alive = hp, flags&flagAlive != 0
	u.chasing, u.alarmed, u.hold = flags&flagChasing != 0, flags&flagAlarmed != 0, flags&flagHold != 0
	u.order, u.target = order, target
	u.cell, u.heading, u.goal = cell, heading, goal
	u.blocked, u.repaths, u.cooldown = blocked, repaths, cooldown
	u.next = next
	return nil
}

// checkUnit rejects a living unit outside the board, inside a solid, or on
// another unit, and adds it to bodies.
func (s *Sim) checkUnit(u *Unit, bodies *collide.Bodies) error {
	if !u.Alive {
		return nil
	}
	f := u.Body.Footprint
	if f.Min.X < 0 || f.Min.Y < 0 || f.Min.X > fixed.FromInt(s.width)-BodySize ||
		f.Min.Y > fixed.FromInt(s.height)-BodySize {
		return fmt.Errorf("%w: unit %d is outside the board", errSnapshot, u.ID)
	}
	for _, hit := range s.world.Overlaps(s.hits[:0], f, u.Body.Vertical) {
		if !hit.Trigger {
			return fmt.Errorf("%w: unit %d is inside a solid", errSnapshot, u.ID)
		}
	}
	if len(bodies.Overlaps(s.ids[:0], f, u.Body.Vertical)) != 0 {
		return fmt.Errorf("%w: unit %d overlaps another unit", errSnapshot, u.ID)
	}
	if err := bodies.Set(u.Body); err != nil {
		return fmt.Errorf("skirmish: unit %d: %w", u.ID, err)
	}
	return nil
}

// decodeBolt reads one bolt. Bolts are stored in the order they were fired, so
// their IDs ascend and none is newer than the bolt counter.
func (s *Sim) decodeBolt(d *decoder, seq uint64, earlier []Bolt) (Bolt, error) {
	id := d.u64()
	x, y, vx, vy := d.num(), d.num(), d.num(), d.num()
	life := int(d.u8())
	if !d.ok {
		return Bolt{}, nil
	}
	fresh := id > boltIDBase && id <= boltIDBase+seq &&
		(len(earlier) == 0 || id > earlier[len(earlier)-1].ID)
	inside := x >= 0 && y >= 0 && x <= fixed.FromInt(s.width)-boltSize &&
		y <= fixed.FromInt(s.height)-boltSize
	if !fresh || !inside || life < 1 || life > boltLife || vx.Abs() > boltSpeed ||
		vy.Abs() > boltSpeed {
		return Bolt{}, fmt.Errorf("%w: bolt %d is out of range", errSnapshot, id)
	}
	return Bolt{
		ID:       id,
		Velocity: fixed.Vec2{X: vx, Y: vy},
		Life:     life,
		Body: collide.Shape{
			ID: id,
			Footprint: fixed.Rect{
				Min: fixed.Vec2{X: x, Y: y},
				Max: fixed.Vec2{X: x + boltSize, Y: y + boltSize},
			},
			Vertical: collide.Interval{Bottom: boltBottom, Top: boltTop},
		},
	}, nil
}

func appendNum(b []byte, n fixed.Num) []byte {
	return binary.LittleEndian.AppendUint32(b, uint32(n)) // #nosec G115 -- the bits are kept.
}

func boolByte(v bool) byte {
	if v {
		return 1
	}
	return 0
}

func (d *decoder) take(n int) []byte {
	if !d.ok || len(d.b) < n {
		d.ok = false
		return make([]byte, n)
	}
	p := d.b[:n]
	d.b = d.b[n:]
	return p
}

func (d *decoder) u8() uint8   { return d.take(1)[0] }
func (d *decoder) u16() uint16 { return binary.LittleEndian.Uint16(d.take(2)) }
func (d *decoder) u32() uint32 { return binary.LittleEndian.Uint32(d.take(4)) }
func (d *decoder) u64() uint64 { return binary.LittleEndian.Uint64(d.take(8)) }
func (d *decoder) i32() int32  { return int32(d.u32()) } // #nosec G115 -- the bits are kept.
func (d *decoder) num() fixed.Num {
	return fixed.Num(d.i32())
}
