package sound

import (
	"math"
	"strings"
	"testing"

	"github.com/krapht-foundation/goje/audio"
)

func TestQueueDropsUnavailableAndOverflowAndClears(t *testing.T) {
	t.Parallel()
	q := twoCues(t)
	q.Play(1, 1, 0)
	if _, ok := q.Next(); ok {
		t.Fatal("unavailable output retained a cue")
	}
	q.Enable()
	for i := range Capacity {
		q.Play(i%2, 1, 0)
	}
	q.Play(1, 0.5, 0)
	for i := range Capacity {
		if r, ok := q.Next(); !ok || r.Cue != i%2 || r.Gain != 1 {
			t.Fatalf("request %d is %+v, %v; overflow must keep accepted requests in order", i, r, ok)
		}
	}
	if _, ok := q.Next(); ok {
		t.Fatal("overflow accepted a request beyond capacity")
	}
	q.Play(0, 1, 0)
	q.Play(2, 1, 0)
	q.Play(-1, 1, 0)
	generation := q.Generation()
	q.Clear()
	if _, ok := q.Next(); ok || generation == q.Generation() {
		t.Fatal("clear retained a queued cue or failed to invalidate the playing voices")
	}
	q.Play(1, 1, 0)
	if r, ok := q.Next(); !ok || r.Cue != 1 {
		t.Fatal("clear did not leave the player ready for a new cue")
	}
	q.Disable()
	q.Play(1, 1, 0)
	if _, ok := q.Next(); ok {
		t.Fatal("disabled output retained a cue")
	}
}

func TestQueueSanitizesGainAndPan(t *testing.T) {
	t.Parallel()
	q := twoCues(t)
	q.Enable()
	nan := float32(math.NaN())
	for _, tc := range []struct{ gain, pan, wantGain, wantPan float32 }{
		{0.25, -0.5, 0.25, -0.5},
		{2, 3, 1, 1},
		{-1, -3, 0, -1},
		{nan, nan, 0, 0},
	} {
		q.Play(0, tc.gain, tc.pan)
		if r, ok := q.Next(); !ok || r.Gain != tc.wantGain || r.Pan != tc.wantPan {
			t.Errorf("Play(0, %v, %v) queued %+v, want gain %v and pan %v",
				tc.gain, tc.pan, r, tc.wantGain, tc.wantPan)
		}
	}
}

func TestQueueSkipsRequestsFromBeforeAClear(t *testing.T) {
	t.Parallel()
	q := twoCues(t)
	q.Enable()
	q.Play(0, 1, 0)
	// A request accepted just before a concurrent clear can outlive its drain.
	q.generation.Add(1)
	q.Play(1, 1, 0)
	if r, ok := q.Next(); !ok || r.Cue != 1 {
		t.Fatalf("Next returned %+v, %v; want only the new generation's cue", r, ok)
	}
}

func TestQueueDoesNotAllocate(t *testing.T) {
	q := twoCues(t)
	q.Enable()
	if allocs := testing.AllocsPerRun(100, func() {
		q.Play(1, 0.5, 0.25)
		q.Next()
	}); allocs != 0 {
		t.Fatalf("enqueue and drain allocated %v times", allocs)
	}
}

func TestQueueKeepsMusicAcrossClearsAndUnreadyDevices(t *testing.T) {
	t.Parallel()
	q := twoCues(t)
	q.PlayMusic(0, 2, -5) // Before the device is ready.
	q.PlayMusic(1, 1, 0)  // No such track.
	q.Play(0, 1, 0)       // Dropped: not ready.
	q.SetMusicGain(0.5, 1<<30)
	q.Enable()
	q.Play(1, 1, 0)
	q.Clear()
	q.StopMusic(250)
	q.Disable()
	want := []Request{
		{Kind: KindPlayMusic, Cue: 0, Gain: 1},
		{Kind: KindMusicGain, Gain: 0.5, FadeMS: audio.MaxFadeMS},
		{Kind: KindStopMusic, FadeMS: 250},
	}
	for i, w := range want {
		if r, ok := q.Next(); !ok || r != w {
			t.Fatalf("request %d is %+v, %v; want %+v", i, r, ok, w)
		}
	}
	if r, ok := q.Next(); ok {
		t.Fatalf("queue kept %+v", r)
	}
}

func TestQueueRejectsLongSoundClips(t *testing.T) {
	t.Parallel()
	long, err := audio.RenderScore(audio.Score{
		Version: audio.ScoreVersion, Tempo: 120, Steps: 32,
		Channels: []audio.Channel{{Waveform: audio.Sine}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := New([]audio.Clip{long}, nil); err == nil {
		t.Fatal("a 4-second sound cue was accepted")
	}
	if q, err := New(nil, []audio.Clip{long}); err != nil || q == nil {
		t.Fatalf("a music-only bank gave %v, %v", q, err)
	}
}

func BenchmarkQueue(b *testing.B) {
	q := twoCues(b)
	q.Enable()
	b.ReportAllocs()
	for b.Loop() {
		q.Play(0, 1, 0)
		q.Next()
	}
}

func twoCues(tb testing.TB) *Queue {
	tb.Helper()
	clip, err := audio.DecodeWAV(strings.NewReader("RIFF\x26\x00\x00\x00WAVEfmt " +
		"\x10\x00\x00\x00\x01\x00\x01\x00\x80\xbb\x00\x00\x00\x77\x01\x00\x02\x00\x10\x00" +
		"data\x02\x00\x00\x00\x01\x00"))
	if err != nil {
		tb.Fatal(err)
	}
	q, err := New([]audio.Clip{clip, clip}, []audio.Clip{clip})
	if err != nil {
		tb.Fatal(err)
	}
	return q
}
