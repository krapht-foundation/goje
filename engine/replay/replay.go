// Package replay records the per-tick input of Goje scenes, plays it back, and
// checks that playback reaches the recorded state. Recording and playback are
// scene wrappers, so hosts need no change, and Verify runs a recording without
// a host. The binary format and the checkpoint rules are defined in
// specs/replay-v1.md; docs/replays.md explains what makes a scene replayable.
package replay

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"

	"github.com/krapht-foundation/goje/engine"
)

const (
	// MaxEncodedSize is the largest encoded recording in bytes.
	MaxEncodedSize = 2 << 20
	// MaxTicks is the most ticks a recording holds, about 4.8 hours at 60 Hz.
	MaxTicks = 1 << 20
	// MaxStartSize is the largest start state in bytes.
	MaxStartSize = 1 << 20
	// CheckpointInterval is the number of ticks between the state hashes that a
	// Recorder takes.
	CheckpointInterval = 60
	maxInterval        = 1<<16 - 1
	// knownKeys has a bit for every engine key, from KeyArrowLeft to KeyTab.
	knownKeys = engine.KeyTab<<1 - 1
)

// ErrContentMismatch reports a recording or save made for another game or with
// other content. Test for it with errors.Is.
var ErrContentMismatch = errors.New("made for different game content")

// Meta identifies the game and content that a recording or save belongs to.
// Game is a 1–32 byte name that starts with a lowercase ASCII letter followed by
// lowercase letters, digits, and underscores. Content is the game's check value
// over the bytes that define its content; see ContentHash.
type Meta struct {
	Game    string
	Content uint64
}

// Validate reports whether the game name has the required shape.
func (m Meta) Validate() error {
	if !validGame(m.Game) {
		return fmt.Errorf("game name %q must be 1-32 bytes of [a-z][a-z0-9_]*", m.Game)
	}
	return nil
}

// Check returns an error wrapping ErrContentMismatch unless m equals expected.
func (m Meta) Check(expected Meta) error {
	if m != expected {
		return fmt.Errorf("%w: have game %q content %016x, want game %q content %016x",
			ErrContentMismatch, m.Game, m.Content, expected.Game, expected.Content)
	}
	return nil
}

// ContentHash returns the 64-bit FNV-1a hash of the parts, each preceded by its
// length as a little-endian uint64, so moving bytes between parts changes the
// result. Pass the bytes that define the game's content, such as its level and
// art, in a fixed order.
func ContentHash(parts ...[]byte) uint64 {
	hash := uint64(14695981039346656037)
	write := func(data []byte) {
		for _, b := range data {
			hash = (hash ^ uint64(b)) * 1099511628211
		}
	}
	var size [8]byte
	for _, part := range parts {
		n := uint64(len(part)) // #nosec G115 -- lengths are never negative.
		binary.LittleEndian.PutUint64(size[:], n)
		write(size[:])
		write(part)
	}
	return hash
}

// Run is Ticks consecutive ticks that share one input.
type Run struct {
	Input engine.Input
	Ticks int
}

// Checkpoint is the state hash of the scene that ran tick Tick, taken right
// after that update. Ticks count from 1 at the start of the recording.
type Checkpoint struct {
	Tick int
	Hash uint64
}

// Recording is a recorded session. Runs hold the input of every tick in order,
// and adjacent runs differ, so each recording has one encoding. Checkpoints are
// in increasing tick order and fall on multiples of Interval or on the last
// tick. Start, when not empty, is the snapshot that playback restores into the
// first scene. Inputs compare by exact bits, so -0 and +0 differ.
type Recording struct {
	Meta
	Interval    int
	Start       []byte
	Runs        []Run
	Checkpoints []Checkpoint
}

// Ticks returns the number of recorded ticks.
func (r *Recording) Ticks() int {
	total := 0
	for _, run := range r.Runs {
		total += run.Ticks
	}
	return total
}

// validate checks every rule that Encode and Decode enforce except the encoded
// size, and returns the number of ticks.
func (r *Recording) validate() (int, error) {
	if err := r.Validate(); err != nil {
		return 0, err
	}
	if r.Interval < 1 || r.Interval > maxInterval {
		return 0, fmt.Errorf("checkpoint interval %d is outside 1-%d", r.Interval, maxInterval)
	}
	if len(r.Start) > MaxStartSize {
		return 0, fmt.Errorf("start state of %d bytes exceeds %d", len(r.Start), MaxStartSize)
	}
	total := 0
	for i, run := range r.Runs {
		if run.Ticks < 1 || run.Ticks > MaxTicks-total {
			return 0, fmt.Errorf("run %d: tick count %d is outside 1-%d in total", i, run.Ticks, MaxTicks)
		}
		if run.Input.Held&^knownKeys != 0 || run.Input.Pressed&^knownKeys != 0 {
			return 0, fmt.Errorf("run %d: unknown key bits", i)
		}
		if err := encodableLists(&run.Input); err != nil {
			return 0, fmt.Errorf("run %d: %w", i, err)
		}
		if i > 0 && sameInput(r.Runs[i-1].Input, run.Input) {
			return 0, fmt.Errorf("run %d: same input as the run before it", i)
		}
		total += run.Ticks
	}
	previous := 0
	for i, c := range r.Checkpoints {
		if c.Tick <= previous || c.Tick > total || (c.Tick%r.Interval != 0 && c.Tick != total) {
			return 0, fmt.Errorf("checkpoint %d: tick %d is out of order or off the interval", i, c.Tick)
		}
		previous = c.Tick
	}
	return total, nil
}

// sameInput compares inputs by the exact bits of their coordinates, so runs
// never merge ticks whose pointer positions differ only in the sign of zero.
func sameInput(a, b engine.Input) bool {
	return a.Held == b.Held && a.Pressed == b.Pressed &&
		samePoint(
			a.Pointer.X,
			a.Pointer.Y,
			b.Pointer.X,
			b.Pointer.Y,
		) && a.Pointer.Inside == b.Pointer.Inside &&
		samePress(a.PrimaryPress, b.PrimaryPress) && samePress(a.SecondaryPress, b.SecondaryPress) &&
		samePress(
			a.PrimaryRelease,
			b.PrimaryRelease,
		) && samePress(a.SecondaryRelease, b.SecondaryRelease) &&
		a.PrimaryHeld == b.PrimaryHeld && a.SecondaryHeld == b.SecondaryHeld &&
		sameTouches(&a.Touches, &b.Touches) && a.Gamepads == b.Gamepads
}

func sameTouches(a, b *[engine.MaxTouches]engine.Touch) bool {
	for i := range a {
		if a[i].ID != b[i].ID || a[i].Active != b[i].Active ||
			!samePoint(a[i].X, a[i].Y, b[i].X, b[i].Y) {
			return false
		}
	}
	return true
}

// encodableLists checks the rules that let the format store touches and pads
// as lists: active touches come first and have a nonzero ID, and inactive
// touches and disconnected pads are all zero.
func encodableLists(in *engine.Input) error {
	ended := false
	for i, t := range in.Touches {
		switch {
		case !t.Active:
			if t.ID != 0 || present(t.X, t.Y) {
				return fmt.Errorf("inactive touch %d is not zero", i)
			}
			ended = true
		case ended:
			return fmt.Errorf("active touch %d follows an inactive one", i)
		case t.ID == 0:
			return fmt.Errorf("active touch %d has ID 0", i)
		}
	}
	for i, p := range in.Gamepads {
		if !p.Connected && p != (engine.Gamepad{}) {
			return fmt.Errorf("disconnected gamepad %d is not zero", i)
		}
	}
	return nil
}

func samePress(a, b engine.MousePress) bool {
	return a.Valid == b.Valid && samePoint(a.X, a.Y, b.X, b.Y)
}

func samePoint(ax, ay, bx, by float64) bool {
	return math.Float64bits(ax) == math.Float64bits(bx) &&
		math.Float64bits(ay) == math.Float64bits(by)
}

func validGame(name string) bool {
	if name == "" || len(name) > 32 || name[0] < 'a' || name[0] > 'z' {
		return false
	}
	for i := 1; i < len(name); i++ {
		c := name[i]
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '_' {
			return false
		}
	}
	return true
}
