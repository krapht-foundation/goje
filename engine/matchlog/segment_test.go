package matchlog

import (
	"bytes"
	"testing"
)

func TestSegmentAndRewind(t *testing.T) {
	header := Header{
		Game:        "outpost",
		Content:     1,
		ContentName: "versus-1",
		Players:     2,
		Mode:        Versus,
	}
	newLog := func() *Recorder {
		r, err := New(header)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	appendTicks := func(r *Recorder, first, last int, alternate bool) {
		for tick := first + 1; tick <= last; tick++ {
			var payload [2][]byte
			if tick == 153 || alternate && tick == 61 {
				payload[0] = []byte{byte(tick)}
			}
			if err := r.Append(payload[:], uint64(tick)); err != nil {
				t.Fatal(err)
			}
		}
	}
	original := newLog()
	appendTicks(original, 0, 185, false)
	for _, region := range [][2]int{{0, 0}, {0, 60}, {60, 120}, {120, 120}, {60, 180}, {120, 185}, {180, 185}, {0, 185}} {
		first, last := region[0], region[1]
		want := newLog()
		appendTicks(want, first, last, false)
		got, ok := original.Segment(first, last)
		if !ok || !bytes.Equal(got, want.Encode()) {
			t.Fatalf("range %v encoded differently: got %x, want %x", region, got, want.Encode())
		}
	}
	if !original.Rewind(60) || original.Rewind(59) {
		t.Fatal("rewind must accept only existing checkpoint boundaries")
	}
	appendTicks(original, 60, 125, true)
	want := newLog()
	appendTicks(want, 0, 125, true)
	if got := original.Encode(); !bytes.Equal(got, want.Encode()) {
		t.Fatalf("after rewind and new orders: got %x, want %x", got, want.Encode())
	}
}
