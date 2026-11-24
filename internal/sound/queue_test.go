package sound

import (
	"strings"
	"testing"

	"github.com/krapht-foundation/goje/audio"
)

func TestQueueDropsUnavailableAndOverflowAndClears(t *testing.T) {
	t.Parallel()
	q := twoCues(t)
	q.Play(1)
	if _, ok := q.Next(); ok {
		t.Fatal("unavailable output retained a cue")
	}
	q.Enable()
	for range capacity {
		q.Play(0)
	}
	q.Play(1)
	if cue, ok := q.Next(); !ok || cue != 0 {
		t.Fatal("overflow replaced an already accepted cue")
	}
	q.Play(0)
	q.Play(1)
	q.Play(2)
	if cue, ok := q.Next(); !ok || cue != 1 {
		t.Fatal("playback did not take the latest valid request")
	}
	q.Play(0)
	generation := q.Generation()
	q.Clear()
	if _, ok := q.Next(); ok || generation == q.Generation() {
		t.Fatal("clear retained a queued cue or failed to invalidate the active voice")
	}
	q.Play(1)
	if cue, ok := q.Next(); !ok || cue != 1 {
		t.Fatal("clear did not leave the player ready for a new cue")
	}
}

func BenchmarkQueue(b *testing.B) {
	q := twoCues(b)
	q.Enable()
	for b.Loop() {
		q.Play(0)
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
	q, err := New([]audio.Clip{clip, clip})
	if err != nil {
		tb.Fatal(err)
	}
	return q
}
