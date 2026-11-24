package soundmaker_test

import (
	"bytes"
	"encoding/binary"
	"math"
	"slices"
	"testing"

	"github.com/krapht-foundation/goje/audio"
	"github.com/krapht-foundation/goje/internal/soundmaker"
)

func sample(pcm []byte, i int) int {
	return int(int16(binary.LittleEndian.Uint16(pcm[2*i:])))
}

func TestExportDecodesToTheSynthesizedSamples(t *testing.T) {
	t.Parallel()
	for _, wave := range []soundmaker.Waveform{soundmaker.Sine, soundmaker.Triangle, soundmaker.Square} {
		doc := soundmaker.New()
		s := soundmaker.Default()
		s.Waveform, s.DurationMS = wave, 3000 // The longest legal clip.
		if err := doc.Apply(s); err != nil {
			t.Fatalf("%s: %v", wave, err)
		}
		first, err := doc.ExportWAV()
		if err != nil {
			t.Fatal(err)
		}
		second, _ := doc.ExportWAV()
		if !bytes.Equal(first, second) {
			t.Fatalf("%s: repeated export differs", wave)
		}
		clip, err := audio.DecodeWAV(bytes.NewReader(first))
		if err != nil || !bytes.Equal(clip.PCM(), doc.PCM()) ||
			len(clip.PCM()) != 2*3*audio.SampleRate {
			t.Fatalf("%s: decoded %d bytes, err %v", wave, len(clip.PCM()), err)
		}
	}
}

func TestEnvelopeAndPitch(t *testing.T) {
	t.Parallel()
	doc := soundmaker.New()
	s := soundmaker.Settings{
		Waveform:   soundmaker.Square,
		StartHz:    480,
		EndHz:      480,
		DurationMS: 100,
		AttackMS:   20,
		ReleaseMS:  30,
	}
	if err := doc.Apply(s); err != nil {
		t.Fatal(err)
	}
	pcm := doc.PCM()
	n := len(pcm) / 2
	if n != 100*48 {
		t.Fatalf("%d samples, want 4800", n)
	}
	peak := int(math.Round(0.8 * 32767))
	// The square is at full gain between the fades, and silent at both ends.
	if sample(pcm, 0) != 0 || sample(pcm, n-1) != 0 {
		t.Fatalf("clip does not fade from and to silence: %d, %d", sample(pcm, 0), sample(pcm, n-1))
	}
	if got := sample(pcm, 20*48+10); got != peak && got != -peak {
		t.Fatalf("sustain sample %d, want ±%d", got, peak)
	}
	// Halfway through the attack the gain is one half.
	if got := sample(pcm, 10*48); math.Abs(math.Abs(float64(got))-float64(peak)/2) > 1 {
		t.Fatalf("mid-attack sample %d, want ±%d", got, peak/2)
	}
	// 480 Hz at 48 kHz is exactly 100 samples per cycle: 50 high, 50 low.
	// Sample 1000 is a whole number of cycles in, past the attack.
	cycle := 1000
	if sample(pcm, cycle+10) != peak || sample(pcm, cycle+60) != -peak {
		t.Fatalf("square half cycles wrong: %d, %d", sample(pcm, cycle+10), sample(pcm, cycle+60))
	}
}

func TestInvalidSettingsKeepTheCurrentSound(t *testing.T) {
	t.Parallel()
	doc := soundmaker.New()
	before, want := slices.Clone(doc.PCM()), doc.Settings()
	valid := soundmaker.Default()
	bad := map[string]func(*soundmaker.Settings){
		"waveform":      func(s *soundmaker.Settings) { s.Waveform = "saw" },
		"low pitch":     func(s *soundmaker.Settings) { s.StartHz = soundmaker.MinHz - 1 },
		"high pitch":    func(s *soundmaker.Settings) { s.EndHz = soundmaker.MaxHz + 1 },
		"short":         func(s *soundmaker.Settings) { s.DurationMS = soundmaker.MinDurationMS - 1 },
		"long":          func(s *soundmaker.Settings) { s.DurationMS = soundmaker.MaxDurationMS + 1 },
		"negative":      func(s *soundmaker.Settings) { s.AttackMS = -1 },
		"envelope fit":  func(s *soundmaker.Settings) { s.AttackMS, s.ReleaseMS = 100, 51 },
		"envelope wrap": func(s *soundmaker.Settings) { s.AttackMS, s.ReleaseMS = math.MaxInt, math.MaxInt },
	}
	for name, mutate := range bad {
		s := valid
		mutate(&s)
		if err := doc.Apply(s); err == nil {
			t.Fatalf("%s: accepted %+v", name, s)
		}
		if doc.Settings() != want || !bytes.Equal(doc.PCM(), before) {
			t.Fatalf("%s: rejected apply changed the document", name)
		}
	}
}

func TestEncodeWAVRejectsUnplayableClips(t *testing.T) {
	t.Parallel()
	for name, pcm := range map[string][]byte{
		"empty": nil, "partial": make([]byte, 3), "too long": make([]byte, 2*3*audio.SampleRate+2),
	} {
		if _, err := audio.EncodeWAV(pcm); err == nil {
			t.Fatalf("%s: encoded", name)
		}
	}
}
