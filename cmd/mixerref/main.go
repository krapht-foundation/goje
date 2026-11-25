// Command mixerref prints fixed sound and music request scripts and the
// audio.Mixer output for each as JSON. scripts/smoke-mixer.mjs renders the
// same scripts through the browser host's Web Audio graphs and compares them.
package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"os"

	"github.com/krapht-foundation/goje/audio"
)

// block is the Web Audio render quantum. Requests start on block boundaries,
// where the Go mixer can start them between two Render calls.
const block = 128

type request struct {
	Frame int     `json:"frame"`
	Cue   int     `json:"cue"`
	Gain  float32 `json:"gain"`
	Pan   float32 `json:"pan"`
}

type reference struct {
	SampleRate int       `json:"sampleRate"`
	Frames     int       `json:"frames"`
	Clips      [][]int16 `json:"clips"`
	Requests   []request `json:"requests"`
	// Mixed holds interleaved stereo frames, left first.
	Mixed []float32      `json:"mixed"`
	Music musicReference `json:"music"`
}

// musicRequest drives the music voice: kind is play, stop, or gain.
type musicRequest struct {
	Frame  int     `json:"frame"`
	Kind   string  `json:"kind"`
	Track  int     `json:"track"`
	Gain   float32 `json:"gain"`
	FadeMS int     `json:"fadeMS"`
}

type musicReference struct {
	Frames   int            `json:"frames"`
	Tracks   [][]int16      `json:"tracks"`
	Requests []musicRequest `json:"requests"`
	Mixed    []float32      `json:"mixed"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "mixerref:", err)
		os.Exit(1)
	}
}

func run() error {
	ref := reference{
		SampleRate: audio.SampleRate,
		Frames:     audio.SampleRate / 2,
		Clips: [][]int16{
			synth(audio.SampleRate/4, func(i int, t float64) float64 {
				return 0.6 * (1 - float64(i)/(audio.SampleRate/4)) * math.Sin(2*math.Pi*440*t)
			}),
			synth(audio.SampleRate*3/20, func(_ int, t float64) float64 {
				return 0.4 * math.Copysign(1, math.Sin(2*math.Pi*220*t))
			}),
			noise(audio.SampleRate * 3 / 10),
		},
		// Overlapping voices with different gains and pans, including the
		// pan limits, so both channels of the law are compared.
		Requests: []request{
			{0, 0, 1, 0},
			{10 * block, 1, 0.7, -0.6},
			{20 * block, 2, 0.5, 0.8},
			{30 * block, 0, 0.4, -1},
			{50 * block, 1, 0.9, 1},
			{60 * block, 2, 0.3, -0.25},
			{62 * block, 0, 0.8, 0.5},
		},
	}
	clips := make([]audio.Clip, len(ref.Clips))
	for i, samples := range ref.Clips {
		pcm := make([]byte, 0, 2*len(samples))
		for _, s := range samples {
			pcm = binary.LittleEndian.AppendUint16(pcm, uint16(s)) // #nosec G115 -- PCM bit pattern.
		}
		wav, err := audio.EncodeWAV(pcm)
		if err != nil {
			return err
		}
		clip, err := audio.DecodeWAV(bytes.NewReader(wav))
		if err != nil {
			return err
		}
		clips[i] = clip
	}
	ref.Mixed = make([]float32, 2*ref.Frames)
	var mixer audio.Mixer
	next := 0
	for at := 0; at < ref.Frames; at += block {
		for next < len(ref.Requests) && ref.Requests[next].Frame == at {
			r := ref.Requests[next]
			mixer.Start(clips[r.Cue], r.Gain, r.Pan)
			next++
		}
		mixer.Render(ref.Mixed[2*at : 2*min(at+block, ref.Frames)])
	}
	var err error
	if ref.Music, err = music(); err != nil {
		return err
	}
	if err := json.NewEncoder(os.Stdout).Encode(ref); err != nil {
		return fmt.Errorf("write reference: %w", err)
	}
	return nil
}

// music renders a script that fades a looping track in, ducks it, replaces it
// with a second track while the gain of the pending track changes, ramps the
// second track, and stops it, with silence after the end.
func music() (musicReference, error) {
	scores := []audio.Score{
		{Version: audio.ScoreVersion, Tempo: 300, Steps: 2, Channels: []audio.Channel{
			{Waveform: audio.Sine, Notes: []audio.Note{{Step: 0, Pitch: 57, Length: 2, Level: 100}}},
		}},
		{Version: audio.ScoreVersion, Tempo: 300, Steps: 4, Channels: []audio.Channel{
			{Waveform: audio.Triangle, Notes: []audio.Note{{Step: 0, Pitch: 64, Length: 3, Level: 90}}},
			{Waveform: audio.Square, Notes: []audio.Note{{Step: 1, Pitch: 52, Length: 3, Level: 40}}},
		}},
	}
	ref := musicReference{
		Frames: 470 * block,
		Requests: []musicRequest{
			{Frame: 0, Kind: "play", Track: 0, Gain: 0.8, FadeMS: 100},
			{Frame: 100 * block, Kind: "gain", Gain: 0.3, FadeMS: 50},
			{Frame: 180 * block, Kind: "play", Track: 1, Gain: 0.6, FadeMS: 100},
			{Frame: 200 * block, Kind: "gain", Gain: 0.9},
			{Frame: 300 * block, Kind: "play", Track: 1, Gain: 0.5, FadeMS: 50},
			{Frame: 380 * block, Kind: "stop", FadeMS: 200},
		},
	}
	tracks := make([]audio.Clip, len(scores))
	for i, s := range scores {
		clip, err := audio.RenderScore(s)
		if err != nil {
			return musicReference{}, err
		}
		tracks[i] = clip
		samples := make([]int16, len(clip.PCM())/2)
		for j := range samples {
			samples[j] = int16(
				binary.LittleEndian.Uint16(clip.PCM()[2*j:]),
			) // #nosec G115 -- PCM bit pattern.
		}
		ref.Tracks = append(ref.Tracks, samples)
	}
	ref.Mixed = make([]float32, 2*ref.Frames)
	var mixer audio.Mixer
	next := 0
	for at := 0; at < ref.Frames; at += block {
		for next < len(ref.Requests) && ref.Requests[next].Frame == at {
			switch r := ref.Requests[next]; r.Kind {
			case "play":
				mixer.PlayMusic(tracks[r.Track], r.Gain, r.FadeMS)
			case "gain":
				mixer.SetMusicGain(r.Gain, r.FadeMS)
			default:
				mixer.StopMusic(r.FadeMS)
			}
			next++
		}
		mixer.Render(ref.Mixed[2*at : 2*min(at+block, ref.Frames)])
	}
	return ref, nil
}

// synth samples f(i, seconds) for n frames as 16-bit PCM.
func synth(n int, f func(i int, t float64) float64) []int16 {
	out := make([]int16, n)
	for i := range out {
		out[i] = int16(math.Round(32767 * f(i, float64(i)/audio.SampleRate)))
	}
	return out
}

// noise is a decaying burst from a fixed linear congruential generator.
func noise(n int) []int16 {
	seed := uint32(1)
	return synth(n, func(i int, _ float64) float64 {
		seed = seed*1664525 + 1013904223
		return 0.5 * (1 - float64(i)/float64(n)) * (float64(seed>>8)/(1<<23) - 1)
	})
}
