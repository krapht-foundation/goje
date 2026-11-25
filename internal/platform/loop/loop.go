// Package loop implements the common pacing and input buffering policy for
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

// Loop buffers input and schedules fixed updates. Its zero value is
// active and uses the first frame's timestamp as its baseline. A host owns and
// calls it on the game loop; it is not safe for concurrent use.
type Loop struct {
	last      time.Duration
	remainder time.Duration
	input     engine.Input
	// at is the last position the pointer had inside the viewport. Release
	// edges report it, because a drag can end outside the viewport.
	at engine.MousePress
	// hosts holds the host's identifier of each finger in input.Touches.
	hosts [engine.MaxTouches]int
	// pointerTouch is the ID of the finger that drives the pointer, or 0.
	// lastTouch is the ID given to the latest finger; it restarts when every
	// finger has lifted.
	pointerTouch, lastTouch uint32
	started                 bool
	suspended               bool
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
// debt so inactive time and input cannot leak into the resumed simulation.
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

// MovePointer records the pointer position for the next tick. Inside is false
// when the pointer is outside the viewport, and x and y are then ignored.
func (l *Loop) MovePointer(x, y float64, inside bool) {
	if l.suspended {
		return
	}
	if !inside {
		l.input.Pointer = engine.Pointer{}
		return
	}
	l.at = engine.MousePress{X: x, Y: y, Valid: true}
	l.input.Pointer = engine.Pointer{X: x, Y: y, Inside: true}
}

// PressPrimary buffers an in-viewport left mouse press until the next tick and
// marks the button held. Later presses cannot replace the coordinates of an
// unconsumed press.
func (l *Loop) PressPrimary(x, y float64) {
	if l.suspended {
		return
	}
	l.MovePointer(x, y, true)
	l.input.PrimaryHeld = true
	if !l.input.PrimaryPress.Valid {
		l.input.PrimaryPress = engine.MousePress{X: x, Y: y, Valid: true}
	}
}

// PressSecondary buffers an in-viewport right mouse press until the next tick
// and marks the button held. Later presses cannot replace the coordinates of an
// unconsumed press.
func (l *Loop) PressSecondary(x, y float64) {
	if l.suspended {
		return
	}
	l.MovePointer(x, y, true)
	l.input.SecondaryHeld = true
	if !l.input.SecondaryPress.Valid {
		l.input.SecondaryPress = engine.MousePress{X: x, Y: y, Valid: true}
	}
}

// ReleasePrimary ends a left button press that PressPrimary accepted. It
// buffers a release edge at the last position inside the viewport and is
// ignored when the button is not held, for example when the press began
// outside the viewport or focus was lost since.
func (l *Loop) ReleasePrimary() {
	if l.suspended || !l.input.PrimaryHeld {
		return
	}
	l.input.PrimaryHeld = false
	if !l.input.PrimaryRelease.Valid {
		l.input.PrimaryRelease = l.at
	}
}

// ReleaseSecondary is ReleasePrimary for the right button.
func (l *Loop) ReleaseSecondary() {
	if l.suspended || !l.input.SecondaryHeld {
		return
	}
	l.input.SecondaryHeld = false
	if !l.input.SecondaryRelease.Valid {
		l.input.SecondaryRelease = l.at
	}
}

// TouchDown records a finger that landed inside the viewport. host is the
// host's identifier for the finger, such as a pointer ID, and x and y are
// logical viewport pixels. A finger that lands while no other finger is down
// drives the pointer and presses the primary button like PressPrimary. A finger
// beyond engine.MaxTouches, or one the loop already tracks, is ignored.
func (l *Loop) TouchDown(host int, x, y float64) {
	n := l.touchCount()
	if l.suspended || n == engine.MaxTouches || l.touch(host) >= 0 {
		return
	}
	if n == 0 {
		l.lastTouch = 0
	}
	l.lastTouch++
	l.input.Touches[n] = engine.Touch{ID: l.lastTouch, X: x, Y: y, Active: true}
	l.hosts[n] = host
	if n == 0 {
		l.pointerTouch = l.lastTouch
		l.PressPrimary(x, y)
	}
}

// TouchMove records the motion of a tracked finger. Inside is false when the
// finger is outside the viewport; the touch then keeps its last position
// inside, and the pointer, if the finger drives it, leaves the viewport as
// MovePointer describes.
func (l *Loop) TouchMove(host int, x, y float64, inside bool) {
	i := l.touch(host)
	if i < 0 {
		return
	}
	t := &l.input.Touches[i]
	if inside {
		t.X, t.Y = x, y
	}
	if t.ID == l.pointerTouch {
		l.MovePointer(x, y, inside)
	}
}

// TouchUp removes a lifted finger. If it drives the pointer, it releases the
// primary button like ReleasePrimary, and the fingers still down drive nothing
// until every finger has lifted.
func (l *Loop) TouchUp(host int) { l.lift(host, true) }

// TouchCancel removes a finger whose gesture the platform took over. It is
// TouchUp without a release edge, so a drag it drives is cancelled.
func (l *Loop) TouchCancel(host int) { l.lift(host, false) }

func (l *Loop) lift(host int, release bool) {
	i := l.touch(host)
	if i < 0 {
		return
	}
	if l.input.Touches[i].ID == l.pointerTouch {
		l.pointerTouch = 0
		if release {
			l.ReleasePrimary()
		} else {
			l.input.PrimaryHeld = false
		}
	}
	// Close the gap so active touches stay first and in landing order.
	copy(l.input.Touches[i:], l.input.Touches[i+1:])
	copy(l.hosts[i:], l.hosts[i+1:])
	l.input.Touches[engine.MaxTouches-1] = engine.Touch{}
}

func (l *Loop) touchCount() int {
	for i, t := range l.input.Touches {
		if !t.Active {
			return i
		}
	}
	return engine.MaxTouches
}

// touch returns the index of the tracked finger with the host identifier, or -1.
func (l *Loop) touch(host int) int {
	for i := range l.touchCount() {
		if l.hosts[i] == host {
			return i
		}
	}
	return -1
}

// SetGamepad records the state of the pad in a slot from 0 to
// engine.MaxGamepads-1, as NormalizeGamepad returns it, or the zero value for
// a slot without a standard pad. Hosts call it for every slot once per
// presentation, before the updates of that frame run.
func (l *Loop) SetGamepad(slot int, pad engine.Gamepad) {
	if !l.suspended {
		l.input.Gamepads[slot] = pad
	}
}

// NextInput consumes the buffered press and release edges and returns the
// current snapshot. The host calls it once per update, not once per presentation.
func (l *Loop) NextInput() engine.Input {
	input := l.input
	l.input.Pressed = 0
	l.input.PrimaryPress = engine.MousePress{}
	l.input.SecondaryPress = engine.MousePress{}
	l.input.PrimaryRelease = engine.MousePress{}
	l.input.SecondaryRelease = engine.MousePress{}
	return input
}
