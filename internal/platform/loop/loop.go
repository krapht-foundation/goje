// Package loop implements the common pacing and keyboard buffering policy for
// Goje hosts. Hosts supply monotonic timestamps and native events; the engine
// receives one input snapshot per fixed tick without depending on host clocks.
package loop

import (
	"time"

	"github.com/krapht-foundation/goje/engine"
)

const (
	ticksPerSecond = 60
	maxUpdates     = 4
)

// Budget is the number of updates allowed before presenting and the number of
// overdue whole ticks discarded by the pacing limit.
type Budget struct {
	Updates int
	Dropped int64
}

// Loop buffers keyboard state and schedules fixed updates. Its zero value is
// active and uses the first frame's timestamp as its baseline. A host owns and
// calls it on the game loop; it is not safe for concurrent use.
type Loop struct {
	last      time.Duration
	remainder time.Duration
	input     engine.Input
	started   bool
	suspended bool
}

// Frame computes the update budget from a nonnegative monotonic timestamp
// expressed as a duration from a fixed origin. Call it once per presentation,
// including frames with no updates.
func (l *Loop) Frame(now time.Duration) Budget {
	if l.suspended {
		return Budget{}
	}
	if !l.started {
		l.started = true
		l.last = now
		return Budget{}
	}
	if now <= l.last {
		return Budget{}
	}
	elapsed := now - l.last
	l.last = now

	// Accumulate fractional ticks in scaled nanoseconds. Dividing a second by
	// 60 first truncates the period; scaling only the subsecond part avoids overflow.
	fraction := l.remainder + elapsed%time.Second*ticksPerSecond
	ticks := elapsed/time.Second*ticksPerSecond + fraction/time.Second
	l.remainder = fraction % time.Second
	updates := min(ticks, maxUpdates)
	return Budget{Updates: int(updates), Dropped: int64(ticks - updates)}
}

// SetActive suspends or resumes the loop. The host combines focus and page
// visibility before calling it. Suspension discards pending input and time
// debt so inactive time and keys cannot leak into the resumed simulation.
// It reports whether the state actually changed.
func (l *Loop) SetActive(active bool) bool {
	if l.suspended == !active {
		return false
	}
	*l = Loop{suspended: !active}
	return true
}

// KeyDown records a key press. A repeat must be flagged by the host so it
// cannot restore a key that was cleared during suspension.
func (l *Loop) KeyDown(key engine.Keys, repeat bool) {
	if l.suspended || repeat {
		return
	}
	l.input.Pressed |= key &^ l.input.Held
	l.input.Held |= key
}

// KeyUp releases a key without clearing any buffered press edge.
func (l *Loop) KeyUp(key engine.Keys) { l.input.Held &^= key }

// NextInput consumes the buffered press edges and returns the current snapshot.
// The host calls it once per update, not once per presentation.
func (l *Loop) NextInput() engine.Input {
	input := l.input
	l.input.Pressed = 0
	return input
}
