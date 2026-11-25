package audio

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
)

// Waveform selects an oscillator shape. The Sound Maker's effects and
// RenderScore share these oscillators.
type Waveform string

// The supported oscillator shapes.
const (
	Sine     Waveform = "sine"
	Triangle Waveform = "triangle"
	Square   Waveform = "square"
)

// Valid reports whether w is one of the supported shapes.
func (w Waveform) Valid() bool { return w == Sine || w == Triangle || w == Square }

// Oscillate returns one sample in [-1, 1] for a phase in [0, 1), in cycles.
// An unsupported shape plays as a sine.
func (w Waveform) Oscillate(phase float64) float64 {
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

// Score limits. See specs/music-m17.md.
const (
	ScoreVersion     = 1
	MaxScoreBytes    = 64 << 10
	MinTempo         = 40
	MaxTempo         = 300
	MaxSteps         = 1024
	MaxScoreChannels = 4
	MinPitch         = 24  // C1, about 32.7 Hz
	MaxPitch         = 108 // C8, about 4186 Hz
	MaxLevel         = 100

	noteAttack  = SampleRate / 200 // 5 ms
	noteRelease = SampleRate / 50  // 20 ms
	// Each channel is monophonic and at most a quarter of full scale, so four
	// channels sum to at most full scale and never clip.
	channelAmplitude = 0.25
)

// Score is a looping piece of music: a tempo in beats per minute, a loop of
// Steps sixteenth-note steps, and up to four monophonic channels.
type Score struct {
	Version  int       `json:"version"`
	Tempo    int       `json:"tempo"`
	Steps    int       `json:"steps"`
	Channels []Channel `json:"channels"`
}

// Channel plays its notes, in step order and without overlap, on one waveform.
type Channel struct {
	Waveform Waveform `json:"waveform"`
	Notes    []Note   `json:"notes"`
}

// Note starts at a zero-based Step, plays a MIDI Pitch (69 is A4, 440 Hz) for
// Length steps, at a Level from 1 to 100 percent of the channel's amplitude.
type Note struct {
	Step   int `json:"step"`
	Pitch  int `json:"pitch"`
	Length int `json:"length"`
	Level  int `json:"level"`
}

// StepFrames is the length of one step in frames at SampleRate. A step is a
// sixteenth note; the division rounds down so every host loops alike.
func (s Score) StepFrames() int { return SampleRate * 15 / s.Tempo }

// Validate reports the first field outside the limits of specs/music-m17.md.
func (s Score) Validate() error {
	switch {
	case s.Version != ScoreVersion:
		return fmt.Errorf("score version %d, want %d", s.Version, ScoreVersion)
	case s.Tempo < MinTempo || s.Tempo > MaxTempo:
		return fmt.Errorf("tempo must be %d-%d beats per minute", MinTempo, MaxTempo)
	case s.Steps < 1 || s.Steps > MaxSteps:
		return fmt.Errorf("steps must be 1-%d", MaxSteps)
	case s.Steps*s.StepFrames() > MaxMusicSeconds*SampleRate:
		return fmt.Errorf("loop exceeds %d seconds", MaxMusicSeconds)
	case len(s.Channels) < 1 || len(s.Channels) > MaxScoreChannels:
		return fmt.Errorf("score needs 1-%d channels", MaxScoreChannels)
	}
	for c, ch := range s.Channels {
		if !ch.Waveform.Valid() {
			return fmt.Errorf("channel %d: waveform %q is not sine, triangle, or square", c, ch.Waveform)
		}
		end := 0
		for i, n := range ch.Notes {
			switch {
			case n.Step < end:
				return fmt.Errorf("channel %d note %d: starts before the previous note ends", c, i)
			case n.Length < 1 || n.Length > s.Steps-n.Step:
				return fmt.Errorf(
					"channel %d note %d: must last 1 step or more and end within the loop",
					c,
					i,
				)
			case n.Pitch < MinPitch || n.Pitch > MaxPitch:
				return fmt.Errorf("channel %d note %d: pitch must be %d-%d", c, i, MinPitch, MaxPitch)
			case n.Level < 1 || n.Level > MaxLevel:
				return fmt.Errorf("channel %d note %d: level must be 1-%d", c, i, MaxLevel)
			}
			end = n.Step + n.Length
		}
	}
	return nil
}

// DecodeScore reads one JSON score of at most MaxScoreBytes. Unknown fields,
// trailing data, and any value outside the limits are errors.
func DecodeScore(data []byte) (Score, error) {
	if len(data) > MaxScoreBytes {
		return Score{}, fmt.Errorf("decode score: exceeds %d bytes", MaxScoreBytes)
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	var s Score
	if err := d.Decode(&s); err != nil {
		return Score{}, fmt.Errorf("decode score: %w", err)
	}
	if _, err := d.Token(); !errors.Is(err, io.EOF) {
		return Score{}, errors.New("decode score: unexpected data after the score")
	}
	if err := s.Validate(); err != nil {
		return Score{}, fmt.Errorf("decode score: %w", err)
	}
	return s, nil
}

// EncodeScore writes a valid score in its canonical form: two-space
// indentation, one note per line, and a final newline. DecodeScore accepts
// the result, and the same score always gives the same bytes.
func EncodeScore(s Score) ([]byte, error) {
	if err := s.Validate(); err != nil {
		return nil, fmt.Errorf("encode score: %w", err)
	}
	b := fmt.Appendf(
		nil,
		"{\n  \"version\": %d,\n  \"tempo\": %d,\n  \"steps\": %d,\n  \"channels\": [",
		s.Version,
		s.Tempo,
		s.Steps,
	)
	for c, ch := range s.Channels {
		if c > 0 {
			b = append(b, ',')
		}
		b = append(b, "\n    {\n      \"waveform\": "...)
		b = strconv.AppendQuote(b, string(ch.Waveform)) // Valid shapes are plain ASCII.
		b = append(b, ",\n      \"notes\": ["...)
		for i, n := range ch.Notes {
			if i > 0 {
				b = append(b, ',')
			}
			b = fmt.Appendf(
				b,
				"\n        {\"step\": %d, \"pitch\": %d, \"length\": %d, \"level\": %d}",
				n.Step,
				n.Pitch,
				n.Length,
				n.Level,
			)
		}
		if len(ch.Notes) > 0 {
			b = append(b, "\n      "...)
		}
		b = append(b, "]\n    }"...)
	}
	b = append(b, "\n  ]\n}\n"...)
	if len(b) > MaxScoreBytes {
		return nil, fmt.Errorf("encode score: exceeds %d bytes", MaxScoreBytes)
	}
	return b, nil
}

// RenderScore renders one loop of a valid score to a music clip of
// Steps × StepFrames frames. Each note starts its oscillator at phase zero,
// with a 5 ms linear attack and a 20 ms linear release inside its length. The
// same score always gives the same samples.
func RenderScore(s Score) (Clip, error) {
	if err := s.Validate(); err != nil {
		return Clip{}, fmt.Errorf("render score: %w", err)
	}
	step := s.StepFrames()
	mix := make([]float64, s.Steps*step)
	for _, ch := range s.Channels {
		for _, n := range ch.Notes {
			out := mix[n.Step*step : (n.Step+n.Length)*step]
			// The shortest note, one step at MaxTempo, still fits both ramps.
			freq := 440 * math.Exp2(float64(n.Pitch-69)/12) / SampleRate
			amp := channelAmplitude * float64(n.Level) / MaxLevel
			var phase float64
			for i := range out {
				gain := 1.0
				if i < noteAttack {
					gain = float64(i) / noteAttack
				}
				if tail := len(out) - 1 - i; tail < noteRelease {
					gain = min(gain, float64(tail)/noteRelease)
				}
				out[i] += amp * gain * ch.Waveform.Oscillate(phase)
				phase += freq
				phase -= math.Floor(phase)
			}
		}
	}
	pcm := make([]byte, 2*len(mix))
	for i, v := range mix {
		//nolint:gosec // G115: |v| <= 1, so the rounded value fits int16.
		binary.LittleEndian.PutUint16(pcm[2*i:], uint16(int16(math.Round(v*32767))))
	}
	return Clip{pcm: pcm}, nil
}
