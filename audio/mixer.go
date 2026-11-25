package audio

import (
	"encoding/binary"
	"math"
)

// MaxVoices is the number of clips a Mixer plays at once.
const MaxVoices = 16

// Mixer is a fixed pool of voices that mixes clips into interleaved stereo
// frames at SampleRate. Desktop hosts own one inside the device callback; see
// specs/audio-mixer-m15.md. A Mixer is not safe for concurrent use, and its
// zero value is ready. It allocates nothing.
type Mixer struct {
	// voices[:playing] are in start order, so voices[0] is the oldest.
	voices  [MaxVoices]voice
	playing int
}

type voice struct {
	pcm         []byte // the samples not yet mixed
	left, right float32
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
// left first, and advances every voice. A voice ends after its last sample.
// The mixed sum is clamped to [-1, 1]. An odd final element is set to 0.
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
	for i, s := range dst {
		dst[i] = max(-1, min(1, s))
	}
}

// Reset stops every voice.
func (m *Mixer) Reset() {
	clear(m.voices[:m.playing])
	m.playing = 0
}

// clampUnit limits v to [low, 1] and maps NaN to 0.
func clampUnit(v, low float32) float32 {
	if math.IsNaN(float64(v)) {
		return 0
	}
	return max(low, min(1, v))
}
