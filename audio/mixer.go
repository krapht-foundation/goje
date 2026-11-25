package audio

import (
	"encoding/binary"
	"math"
)

// MaxVoices is the number of sound clips a Mixer plays at once. The music
// voice is separate and does not count.
const MaxVoices = 16

// MaxFadeMS bounds a music fade. Longer fades are clamped.
const MaxFadeMS = 10_000

// Mixer is a fixed pool of sound voices and one looping music voice that
// mixes clips into interleaved stereo frames at SampleRate. Desktop hosts own
// one inside the device callback; see specs/audio-mixer-m15.md. A Mixer is
// not safe for concurrent use, and its zero value is ready. It allocates
// nothing.
type Mixer struct {
	// voices[:playing] are in start order, so voices[0] is the oldest.
	voices  [MaxVoices]voice
	playing int
	music   music
}

type voice struct {
	pcm         []byte // the samples not yet mixed
	left, right float32
}

// music is the looping voice. Its gain moves linearly to target over ramp
// frames. When a ramp to zero ends, a pending track replaces the current one
// and fades in, or a stopping track ends.
type music struct {
	pcm          []byte // the whole track; nil when silent
	pos          int    // byte offset of the next sample
	gain, target float32
	ramp         int
	next         []byte
	nextGain     float32
	nextFade     int
	stopping     bool
}

// Start plays clip from its first sample with a gain from 0 to 1 and a pan
// from -1 (left) to 1 (right). Other values are clamped and NaN is treated as
// 0. When every voice is playing, the oldest one stops to make room. A zero
// Clip is ignored.
func (m *Mixer) Start(clip Clip, gain, pan float32) {
	if len(clip.pcm) == 0 {
		return
	}
	if m.playing == MaxVoices {
		copy(m.voices[:], m.voices[1:])
		m.playing--
	}
	// The Web Audio StereoPannerNode applies this equal-power law to a mono
	// input, so both hosts pan alike: center is cos(π/4) ≈ 0.707 per side.
	angle := float64(clampUnit(pan, -1)+1) * math.Pi / 4
	g := float64(clampUnit(gain, 0))
	m.voices[m.playing] = voice{
		pcm:   clip.pcm,
		left:  float32(g * math.Cos(angle)),
		right: float32(g * math.Sin(angle)),
	}
	m.playing++
}

// Render overwrites dst with the next len(dst)/2 interleaved stereo frames,
// left first, and advances every voice. A sound voice ends after its last
// sample; the music voice loops. The mixed sum is clamped to [-1, 1]. An odd
// final element is set to 0.
func (m *Mixer) Render(dst []float32) {
	clear(dst)
	frames := len(dst) / 2
	kept := 0
	for _, v := range m.voices[:m.playing] {
		n := min(frames, len(v.pcm)/2)
		for i := range n {
			//nolint:gosec // G115: Reinterpret the signed PCM bit pattern.
			s := float32(int16(binary.LittleEndian.Uint16(v.pcm[2*i:]))) / 32768
			dst[2*i] += s * v.left
			dst[2*i+1] += s * v.right
		}
		if v.pcm = v.pcm[2*n:]; len(v.pcm) > 0 {
			m.voices[kept] = v
			kept++
		}
	}
	clear(m.voices[kept:m.playing]) // Drop clip references of ended voices.
	m.playing = kept
	m.music.render(dst[:2*frames])
	for i, s := range dst {
		dst[i] = max(-1, min(1, s))
	}
}

// Reset stops every sound voice. The music voice keeps playing; StopMusic
// stops it.
func (m *Mixer) Reset() {
	clear(m.voices[:m.playing])
	m.playing = 0
}

// PlayMusic loops clip, centered, at a gain from 0 to 1, clamped like Start's.
// fadeMS is clamped to [0, MaxFadeMS]. With no track playing, clip starts at
// once and fades in from 0. When clip is the playing track and it is not
// stopping, only its gain moves. Otherwise the current track fades out, then
// clip starts from its first sample and fades in. A zero Clip is ignored.
func (m *Mixer) PlayMusic(clip Clip, gain float32, fadeMS int) {
	if len(clip.pcm) == 0 {
		return
	}
	g, f := clampUnit(gain, 0), fadeFrames(fadeMS)
	mu := &m.music
	switch {
	case mu.pcm == nil:
		*mu = music{pcm: clip.pcm}
		mu.rampTo(g, f)
	case sameTrack(mu.pcm, clip.pcm) && !mu.stopping:
		mu.next = nil
		mu.rampTo(g, f)
	default:
		mu.next, mu.nextGain, mu.nextFade, mu.stopping = clip.pcm, g, f, false
		mu.rampTo(0, f)
	}
}

// StopMusic fades the music out over fadeMS and then stops it, dropping a
// track that was waiting to replace it.
func (m *Mixer) StopMusic(fadeMS int) {
	mu := &m.music
	if mu.pcm == nil {
		return
	}
	mu.next, mu.stopping = nil, true
	mu.rampTo(0, fadeFrames(fadeMS))
}

// SetMusicGain moves the music's gain linearly to gain over fadeMS. During a
// replacement it sets the gain the next track fades in to. It does nothing
// while the music is silent or stopping.
func (m *Mixer) SetMusicGain(gain float32, fadeMS int) {
	mu := &m.music
	switch g := clampUnit(gain, 0); {
	case mu.pcm == nil || mu.stopping:
	case mu.next != nil:
		mu.nextGain = g
	default:
		mu.rampTo(g, fadeFrames(fadeMS))
	}
}

// render adds the looping track, the same on both channels, and advances its
// gain one frame at a time.
func (mu *music) render(dst []float32) {
	for i := 0; i+1 < len(dst) && mu.pcm != nil; i += 2 {
		//nolint:gosec // G115: Reinterpret the signed PCM bit pattern.
		s := float32(int16(binary.LittleEndian.Uint16(mu.pcm[mu.pos:]))) / 32768 * mu.gain
		dst[i] += s
		dst[i+1] += s
		if mu.pos += 2; mu.pos == len(mu.pcm) {
			mu.pos = 0
		}
		if mu.ramp > 0 {
			mu.gain += (mu.target - mu.gain) / float32(mu.ramp)
			mu.ramp--
			if mu.ramp == 0 {
				mu.settle()
			}
		}
	}
}

func (mu *music) rampTo(target float32, frames int) {
	mu.target, mu.ramp = target, frames
	if frames == 0 {
		mu.settle()
	}
}

// settle ends a ramp exactly on its target and applies a pending replacement
// or stop once the gain is zero.
func (mu *music) settle() {
	mu.gain = mu.target
	switch {
	case mu.target != 0:
	case mu.next != nil:
		next, gain, fade := mu.next, mu.nextGain, mu.nextFade
		*mu = music{pcm: next}
		mu.rampTo(gain, fade)
	case mu.stopping:
		*mu = music{}
	}
}

func fadeFrames(ms int) int { return max(0, min(ms, MaxFadeMS)) * (SampleRate / 1000) }

func sameTrack(a, b []byte) bool { return len(a) == len(b) && &a[0] == &b[0] }

// clampUnit limits v to [low, 1] and maps NaN to 0.
func clampUnit(v, low float32) float32 {
	if math.IsNaN(float64(v)) {
		return 0
	}
	return max(low, min(1, v))
}
