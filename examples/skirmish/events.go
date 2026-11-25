package skirmish

import "github.com/krapht-foundation/goje/engine/fixed"

// EventKind says what happened in an Event.
type EventKind uint8

const (
	// EventBoltFired is a unit firing a bolt, including one that is spent at
	// once because it starts on a wall or a unit and one that is dropped
	// because it would start off the board. Unit is the shooter. Each event
	// takes the next bolt ID, so the nth this tick names the nth new bolt.
	EventBoltFired EventKind = iota + 1
	// EventBoltHit is a bolt hurting a unit. Unit is the unit hit. A bolt
	// stopped by a wall or fading out makes no event.
	EventBoltHit
	// EventUnitFell is a unit being removed because its health ran out. Unit
	// is the unit that fell.
	EventUnitFell
	// EventAlarm is the alarm ringing, once per simulation. Unit is the scout
	// that entered the alarm zone.
	EventAlarm
)

// Event is something that happened during a Step, for sound and effects. At
// is the center of Unit, in ground tiles, at the moment of the event.
type Event struct {
	Kind EventKind
	Unit uint64
	At   fixed.Vec2
}

// maxEvents bounds the events of one tick: every unit fires once, every bolt
// in flight and every new one hits at most once, every unit falls once, and
// the alarm rings once.
const maxEvents = 3*MaxUnits + MaxBolts + 1

// Events returns what happened during the last Step, in the order it
// happened. Events are output only: they are not part of the trace, a
// snapshot, or a hash, and reading them changes nothing. The slice is reused
// by the next Step and must not be modified.
func (s *Sim) Events() []Event { return s.happened }

// report records an event about u.
func (s *Sim) report(kind EventKind, u *Unit) {
	s.happened = append(s.happened, Event{Kind: kind, Unit: u.ID, At: u.Center()})
}
