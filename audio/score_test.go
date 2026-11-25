package audio

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"
)

func testScore() Score {
	return Score{Version: ScoreVersion, Tempo: 120, Steps: 8, Channels: []Channel{
		{
			Waveform: Square,
			Notes: []Note{
				{Step: 0, Pitch: 69, Length: 2, Level: 100},
				{Step: 4, Pitch: 72, Length: 4, Level: 50},
			},
		},
		{Waveform: Sine},
	}}
}

func TestScoreRoundTripsCanonically(t *testing.T) {
	t.Parallel()
	data, err := EncodeScore(testScore())
	if err != nil {
		t.Fatal(err)
	}
	const want = `{
  "version": 1,
  "tempo": 120,
  "steps": 8,
  "channels": [
    {
      "waveform": "square",
      "notes": [
        {"step": 0, "pitch": 69, "length": 2, "level": 100},
        {"step": 4, "pitch": 72, "length": 4, "level": 50}
      ]
    },
    {
      "waveform": "sine",
      "notes": []
    }
  ]
}
`
	if string(data) != want {
		t.Fatalf("canonical score:\n%s\nwant:\n%s", data, want)
	}
	s, err := DecodeScore(data)
	if err != nil {
		t.Fatal(err)
	}
	again, err := EncodeScore(s)
	if err != nil || !bytes.Equal(again, data) {
		t.Fatalf("re-encoding gave %q, %v", again, err)
	}
	compact, err := DecodeScore(
		[]byte(
			`{"version":1,"tempo":120,"steps":8,"channels":[{"waveform":"square","notes":[{"step":0,"pitch":69,"length":2,"level":100},{"step":4,"pitch":72,"length":4,"level":50}]},{"waveform":"sine","notes":null}]}`,
		),
	)
	if err != nil {
		t.Fatal(err)
	}
	if again, _ := EncodeScore(compact); !bytes.Equal(again, data) {
		t.Fatalf("compact input encoded as %q", again)
	}
}

func TestDecodeScoreRejects(t *testing.T) {
	t.Parallel()
	valid, err := EncodeScore(testScore())
	if err != nil {
		t.Fatal(err)
	}
	edit := func(old, replacement string) string {
		return strings.Replace(string(valid), old, replacement, 1)
	}
	for _, tc := range []struct{ name, data, want string }{
		{"too large", string(valid) + strings.Repeat(" ", MaxScoreBytes), "exceeds"},
		{"not JSON", "{", "unexpected EOF"},
		{"unknown field", edit(`"tempo"`, `"swing": 1, "tempo"`), "unknown field"},
		{"trailing data", string(valid) + "{}", "after the score"},
		{"fraction", edit(`"tempo": 120`, `"tempo": 120.5`), "cannot unmarshal"},
		{"version", edit(`"version": 1`, `"version": 2`), "version 2"},
		{"slow", edit(`"tempo": 120`, `"tempo": 39`), "tempo"},
		{"fast", edit(`"tempo": 120`, `"tempo": 301`), "tempo"},
		{"no steps", edit(`"steps": 8`, `"steps": 0`), "steps"},
		{"no channels", `{"version":1,"tempo":120,"steps":8,"channels":[]}`, "channels"},
		{"waveform", edit(`"sine"`, `"saw"`), "saw"},
		{"overlap", edit(`"step": 4`, `"step": 1`), "previous note"},
		{"past loop", edit(`"length": 4`, `"length": 5`), "within the loop"},
		{"empty note", edit(`"length": 2`, `"length": 0`), "within the loop"},
		{"low pitch", edit(`"pitch": 69`, `"pitch": 23`), "pitch"},
		{"high pitch", edit(`"pitch": 72`, `"pitch": 109`), "pitch"},
		{"silent", edit(`"level": 50`, `"level": 0`), "level"},
		{"loud", edit(`"level": 100`, `"level": 101`), "level"},
	} {
		if _, err := DecodeScore(
			[]byte(tc.data),
		); err == nil ||
			!strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: error %v, want %q", tc.name, err, tc.want)
		}
	}
	long := testScore()
	long.Tempo, long.Steps = MinTempo, MaxSteps // 6.4 minutes.
	if _, err := RenderScore(long); err == nil || !strings.Contains(err.Error(), "64 seconds") {
		t.Errorf("long loop: error %v", err)
	}
	if _, err := EncodeScore(Score{}); err == nil {
		t.Error("encoded an invalid score")
	}
}

func TestRenderScoreIsDeterministicWithEnvelopes(t *testing.T) {
	t.Parallel()
	s := testScore()
	a, err := RenderScore(s)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := RenderScore(s)
	if !bytes.Equal(a.PCM(), b.PCM()) {
		t.Fatal("the same score rendered different samples")
	}
	step := s.StepFrames() // 6000 frames at 120 beats per minute.
	if step != 6000 || len(a.PCM()) != 2*8*step {
		t.Fatalf("rendered %d bytes with %d-frame steps", len(a.PCM()), step)
	}
	at := func(i int) int { return int(int16(binary.LittleEndian.Uint16(a.PCM()[2*i:]))) }
	quarter := 8192 // 0.25 of full scale: one channel at level 100.
	for _, tc := range []struct{ frame, want int }{
		{0, 0},                    // attack starts at zero
		{noteAttack + 1, quarter}, // square at full channel amplitude
		{2*step - 1, 0},           // release ends at zero
		{2 * step, 0},             // rest
		{3*step + 500, 0},         // rest
		{4*step + noteAttack + 1, quarter / 2},
	} {
		if got := at(tc.frame); got != tc.want && got != -tc.want {
			t.Errorf("frame %d is %d, want ±%d", tc.frame, got, tc.want)
		}
	}
}

func FuzzDecodeScore(f *testing.F) {
	valid, err := EncodeScore(testScore())
	if err != nil {
		f.Fatal(err)
	}
	f.Add(valid)
	f.Add(
		[]byte(
			`{"version":1,"tempo":300,"steps":1,"channels":[{"waveform":"sine","notes":[{"step":0,"pitch":24,"length":1,"level":1}]}]}`,
		),
	)
	f.Fuzz(func(t *testing.T, data []byte) {
		s, err := DecodeScore(data)
		if err != nil {
			return
		}
		canonical, err := EncodeScore(s)
		if err != nil {
			t.Fatalf("decoded score does not encode: %v", err)
		}
		again, err := DecodeScore(canonical)
		if err != nil {
			t.Fatalf("canonical score does not decode: %v", err)
		}
		if next, _ := EncodeScore(again); !bytes.Equal(next, canonical) {
			t.Fatal("canonical form is not stable")
		}
		clip, err := RenderScore(s)
		if err != nil || len(clip.PCM()) != 2*s.Steps*s.StepFrames() ||
			len(clip.PCM()) > maxMusicPCMBytes {
			t.Fatalf("render gave %d bytes, %v", len(clip.PCM()), err)
		}
	})
}
