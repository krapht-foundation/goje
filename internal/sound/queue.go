// Package sound connects fixed-tick cue requests to host-owned playback.
// See specs/audio-mixer-m15.md for the queue and host rules.
package sound

import (
	"fmt"
	"math"
	"slices"
	"sync/atomic"

	"github.com/krapht-foundation/goje/audio"
)

const (
	maxClips = 16
	// Capacity bounds pending requests. A consumer takes at most this many per
	// pass, so a producer that refills the queue cannot extend a device callback.
	Capacity = 64
)

// Request is one accepted cue with a gain in [0, 1] and a pan in [-1, 1].
type Request struct {
	Cue        int
	Gain, Pan  float32
	generation uint64
}

// Queue has one game-loop producer and one playback consumer. The consumer may
// be a device callback on another thread. A nil queue represents disabled audio.
type Queue struct {
	clips      []audio.Clip
	pending    chan Request
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
	return &Queue{clips: slices.Clone(clips), pending: make(chan Request, Capacity)}, nil
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
// The gain is clamped to [0, 1] and the pan to [-1, 1], and NaN becomes 0, so
// both hosts receive the same valid values.
func (q *Queue) Play(cue int, gain, pan float32) {
	if cue < 0 || cue >= len(q.clips) || !q.ready.Load() {
		return
	}
	select {
	case q.pending <- Request{
		Cue: cue, Gain: clamp(gain, 0), Pan: clamp(pan, -1), generation: q.Generation(),
	}:
	default:
	}
}

// Next takes the oldest pending request of the current generation and skips
// requests from before a clear. Only the playback consumer calls Next, at most
// Capacity times per pass.
func (q *Queue) Next() (Request, bool) {
	for {
		select {
		case r := <-q.pending:
			if r.generation == q.Generation() {
				return r, true
			}
		default:
			return Request{}, false
		}
	}
}

// Clear drops pending cues and invalidates the playing voices. Only the
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

// clamp limits v to [low, 1] and maps NaN to 0.
func clamp(v, low float32) float32 {
	if math.IsNaN(float64(v)) {
		return 0
	}
	return max(low, min(1, v))
}
