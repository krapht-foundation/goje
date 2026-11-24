// Package soundmaker owns the Sound Maker's effect settings, deterministic
// synthesis, and WAV export. It does not choose files or own preview state.
package soundmaker

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"

	"github.com/krapht-foundation/goje/audio"
)

// Waveform selects the oscillator shape.
type Waveform string

// The supported oscillator shapes.
const (
	Sine     Waveform = "sine"
	Triangle Waveform = "triangle"
	Square   Waveform = "square"
)

// Bounds on every control. Pitch stays well under the 24 kHz Nyquist limit.
const (
	MinHz         = 20
	MaxHz         = 10000
	MinDurationMS = 10
	MaxDurationMS = 3000 // Matches the three-second clip limit.

	samplesPerMS = audio.SampleRate / 1000
	// Square waves at full scale are harsh and clip easily when mixed later.
	amplitude = 0.8
)

// Settings describe one effect. Pitch glides linearly from StartHz to EndHz
// over the clip; attack and release are linear fades measured in milliseconds.
type Settings struct {
	Waveform   Waveform
	StartHz    int
	EndHz      int
	DurationMS int
	AttackMS   int
	ReleaseMS  int
}

// Default is the effect a new document starts with.
func Default() Settings {
	return Settings{
		Waveform:   Sine,
		StartHz:    880,
		EndHz:      440,
		DurationMS: 150,
		AttackMS:   5,
		ReleaseMS:  60,
	}
}

// Validate reports the first control outside its bounds.
func (s Settings) Validate() error {
	switch s.Waveform {
	case Sine, Triangle, Square:
	default:
		return fmt.Errorf("waveform %q is not sine, triangle, or square", s.Waveform)
	}
	for _, pitch := range []struct {
		name string
		hz   int
	}{{"start pitch", s.StartHz}, {"end pitch", s.EndHz}} {
		if pitch.hz < MinHz || pitch.hz > MaxHz {
			return fmt.Errorf("%s must be %d-%d Hz", pitch.name, MinHz, MaxHz)
		}
	}
	if s.DurationMS < MinDurationMS || s.DurationMS > MaxDurationMS {
		return fmt.Errorf("duration must be %d-%d ms", MinDurationMS, MaxDurationMS)
	}
	if s.AttackMS < 0 || s.ReleaseMS < 0 {
		return errors.New("attack and release must not be negative")
	}
	// Subtract rather than add so absurd inputs cannot overflow.
	if s.AttackMS > s.DurationMS-s.ReleaseMS {
		return errors.New("attack and release must fit within the duration")
	}
	return nil
}

// Synthesize renders the settings to packed little-endian signed 16-bit mono
// PCM at audio.SampleRate. The same settings give the same bytes in one build.
func Synthesize(s Settings) ([]byte, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	return render(s), nil
}

// render assumes validated settings.
func render(s Settings) []byte {
	n := s.DurationMS * samplesPerMS
	attack, release := s.AttackMS*samplesPerMS, s.ReleaseMS*samplesPerMS
	pcm := make([]byte, 2*n)
	var phase float64 // Cycles; integrating frequency keeps the glide continuous.
	for i := range n {
		gain := 1.0
		if i < attack {
			gain = float64(i) / float64(attack)
		}
		if tail := n - 1 - i; tail < release {
			gain = min(gain, float64(tail)/float64(release))
		}
		v := amplitude * gain * oscillate(s.Waveform, phase)
		//nolint:gosec // G115: |v| <= 0.8, so the rounded value fits int16.
		binary.LittleEndian.PutUint16(pcm[2*i:], uint16(int16(math.Round(v*32767))))
		freq := float64(s.StartHz) + float64(s.EndHz-s.StartHz)*float64(i)/float64(n)
		phase += freq / audio.SampleRate
		phase -= math.Floor(phase)
	}
	return pcm
}

// oscillate returns one sample in [-1, 1] for a phase in [0, 1).
func oscillate(w Waveform, phase float64) float64 {
	switch w {
	case Triangle:
		// Starts at zero like the sine: up, down, then back to zero.
		switch {
		case phase < 0.25:
			return 4 * phase
		case phase < 0.75:
			return 2 - 4*phase
		default:
			return 4*phase - 4
		}
	case Square:
		if phase < 0.5 {
			return 1
		}
		return -1
	default:
		return math.Sin(2 * math.Pi * phase)
	}
}

// Document holds the current effect. Its zero value is not usable; call New.
type Document struct {
	settings Settings
	pcm      []byte
}

// New returns a document holding the default effect.
func New() *Document {
	return &Document{settings: Default(), pcm: render(Default())}
}

// Settings returns the current effect.
func (d *Document) Settings() Settings { return d.settings }

// PCM returns the samples the preview plays and the export encodes. The slice
// is shared and MUST NOT be modified.
func (d *Document) PCM() []byte { return d.pcm }

// Apply replaces the effect. An invalid candidate leaves the current effect
// and its samples untouched.
func (d *Document) Apply(s Settings) error {
	pcm, err := Synthesize(s)
	if err != nil {
		return err
	}
	d.settings, d.pcm = s, pcm
	return nil
}

// ExportWAV encodes the current samples as a playable WAV file.
func (d *Document) ExportWAV() ([]byte, error) { return audio.EncodeWAV(d.pcm) }
