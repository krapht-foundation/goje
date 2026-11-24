// Package sound connects fixed-tick cue requests to host-owned playback.
package sound

import (
	"fmt"
	"slices"
	"sync/atomic"

	"github.com/krapht-foundation/goje/audio"
)

const (
	maxClips = 16
	capacity = 8
)

type request struct {
	generation uint64
	cue        int
}

// Queue has one game-loop producer and one playback consumer. The consumer may
// be a device callback on another thread. A nil queue represents disabled audio.
type Queue struct {
	clips      []audio.Clip
	pending    chan request
	generation atomic.Uint64
	ready      atomic.Bool
}

// New validates and retains the prepared clips; an empty bank disables audio.
func New(clips []audio.Clip) (*Queue, error) {
	if len(clips) == 0 {
		return nil, nil
	}
	if len(clips) > maxClips {
		return nil, fmt.Errorf("audio bank exceeds %d clips", maxClips)
	}
	for i, clip := range clips {
		if len(clip.PCM()) == 0 {
			return nil, fmt.Errorf("audio cue %d is empty", i)
		}
	}
	return &Queue{clips: slices.Clone(clips), pending: make(chan request, capacity)}, nil
}

// Clips lends the prepared bank. Do not modify its entries or PCM.
func (q *Queue) Clips() []audio.Clip { return q.clips }

// Enable opens the queue after the device becomes ready. Earlier cues are lost.
func (q *Queue) Enable() { q.ready.Store(true) }

// Disable drops requests until the playback device is ready again.
func (q *Queue) Disable() {
	q.ready.Store(false)
	q.Clear()
}

// Generation changes whenever the host or runtime clears playback.
func (q *Queue) Generation() uint64 { return q.generation.Load() }

// Play enqueues a cue without waiting. Invalid cues and overflow are dropped.
func (q *Queue) Play(cue int) {
	if cue < 0 || cue >= len(q.clips) || !q.ready.Load() {
		return
	}
	select {
	case q.pending <- request{generation: q.Generation(), cue: cue}:
	default:
	}
}

// Next takes the latest pending cue. Only the playback consumer calls Next.
// Older requests in the same batch would be replaced before playing a sample.
func (q *Queue) Next() (int, bool) {
	var latest request
	var found bool
	// Do not let a concurrently refilling producer extend a device callback.
	for range len(q.pending) {
		select {
		case latest = <-q.pending:
			found = true
		default:
		}
	}
	return latest.cue, found && latest.generation == q.Generation()
}

// Clear drops pending cues and invalidates the currently playing one. Only the
// game-loop producer calls Clear, so it cannot drain a new generation's cues.
func (q *Queue) Clear() {
	q.generation.Add(1)
	for range len(q.pending) {
		select {
		case <-q.pending:
		default:
		}
	}
}
