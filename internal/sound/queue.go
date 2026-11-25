// Package sound connects fixed-tick cue and music requests to host-owned
// playback. See specs/audio-mixer-m15.md for the queue and host rules.
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

// Kind is what a Request asks the player to do.
type Kind uint8

// The request kinds. A sound request starts a voice; the others drive the
// single music voice with the audio.Mixer method of the same name.
const (
	KindSound Kind = iota
	KindPlayMusic
	KindStopMusic
	KindMusicGain
)

// Request is one accepted request. Cue is the sound cue or, for
// KindPlayMusic, the music track. Gain is in [0, 1], Pan in [-1, 1], and
// FadeMS in [0, audio.MaxFadeMS].
type Request struct {
	Kind       Kind
	Cue        int
	Gain, Pan  float32
	FadeMS     int
	generation uint64
}

// Queue has one game-loop producer and one playback consumer. The consumer may
// be a device callback on another thread. A nil queue represents disabled audio.
type Queue struct {
	clips      []audio.Clip
	music      []audio.Clip
	pending    chan Request
	generation atomic.Uint64
	ready      atomic.Bool
}

// New validates and retains the prepared sound clips and music tracks; two
// empty banks disable audio.
func New(clips, music []audio.Clip) (*Queue, error) {
	if len(clips) == 0 && len(music) == 0 {
		return nil, nil
	}
	if len(clips) > maxClips {
		return nil, fmt.Errorf("audio bank exceeds %d clips", maxClips)
	}
	if len(music) > maxClips {
		return nil, fmt.Errorf("music bank exceeds %d tracks", maxClips)
	}
	for i, clip := range clips {
		switch n := len(clip.PCM()); {
		case n == 0:
			return nil, fmt.Errorf("audio cue %d is empty", i)
		case n > audio.MaxSoundSeconds*audio.SampleRate*2:
			return nil, fmt.Errorf("audio cue %d exceeds %d seconds", i, audio.MaxSoundSeconds)
		}
	}
	for i, clip := range music {
		if len(clip.PCM()) == 0 {
			return nil, fmt.Errorf("music track %d is empty", i)
		}
	}
	return &Queue{
		clips: slices.Clone(clips), music: slices.Clone(music), pending: make(chan Request, Capacity),
	}, nil
}

// Clips lends the prepared bank. Do not modify its entries or PCM.
func (q *Queue) Clips() []audio.Clip { return q.clips }

// Music lends the prepared music tracks. Do not modify its entries or PCM.
func (q *Queue) Music() []audio.Clip { return q.music }

// Enable opens the queue after the device becomes ready. Earlier cues are lost.
func (q *Queue) Enable() { q.ready.Store(true) }

// Disable drops sound requests until the playback device is ready again. It
// also drops pending sound requests, keeping music requests in order, to free
// their space. Only a host whose consumer is not running calls Disable.
func (q *Queue) Disable() {
	q.ready.Store(false)
	q.Clear()
	for range len(q.pending) {
		select {
		case r := <-q.pending:
			if r.Kind != KindSound {
				q.push(r) // Space is free: this pass just took a request.
			}
		default:
		}
	}
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
	q.push(Request{Cue: cue, Gain: clamp(gain, 0), Pan: clamp(pan, -1), generation: q.Generation()})
}

// PlayMusic asks for track to loop at gain, replacing the current track with
// a fade of fadeMS. Music requests are kept while the device is not ready and
// across clears, so the music reflects the last request once it plays. An
// invalid track and overflow are dropped.
func (q *Queue) PlayMusic(track int, gain float32, fadeMS int) {
	if track < 0 || track >= len(q.music) {
		return
	}
	q.push(Request{Kind: KindPlayMusic, Cue: track, Gain: clamp(gain, 0), FadeMS: fade(fadeMS)})
}

// StopMusic asks for the music to fade out over fadeMS and stop.
func (q *Queue) StopMusic(fadeMS int) {
	if len(q.music) > 0 {
		q.push(Request{Kind: KindStopMusic, FadeMS: fade(fadeMS)})
	}
}

// SetMusicGain asks for the music's gain to move to gain over fadeMS.
func (q *Queue) SetMusicGain(gain float32, fadeMS int) {
	if len(q.music) > 0 {
		q.push(Request{Kind: KindMusicGain, Gain: clamp(gain, 0), FadeMS: fade(fadeMS)})
	}
}

func (q *Queue) push(r Request) {
	select {
	case q.pending <- r:
	default:
	}
}

// Next takes the oldest pending request. It skips sound requests from before
// a clear; music requests never expire. Only the playback consumer calls
// Next, at most Capacity times per pass.
func (q *Queue) Next() (Request, bool) {
	for {
		select {
		case r := <-q.pending:
			if r.Kind != KindSound || r.generation == q.Generation() {
				return r, true
			}
		default:
			return Request{}, false
		}
	}
}

// Clear invalidates the pending sound requests and the playing sound voices;
// the consumer skips the stale requests. Music requests are unaffected. It
// never removes a request, so a consumer on another thread still sees the
// music requests in order.
func (q *Queue) Clear() { q.generation.Add(1) }

// clamp limits v to [low, 1] and maps NaN to 0.
func clamp(v, low float32) float32 {
	if math.IsNaN(float64(v)) {
		return 0
	}
	return max(low, min(1, v))
}

func fade(ms int) int { return max(0, min(ms, audio.MaxFadeMS)) }
