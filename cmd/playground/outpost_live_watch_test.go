package main

import (
	"encoding/binary"
	"testing"

	"github.com/krapht-foundation/goje/engine/matchlog"
)

func TestLiveViewSegmentLimits(t *testing.T) {
	t.Parallel()
	header := matchlog.Header{
		Game:        "outpost",
		Content:     1,
		ContentName: "versus-1",
		Players:     2,
		Mode:        matchlog.Versus,
	}
	recorder, err := matchlog.New(header)
	if err != nil {
		t.Fatal(err)
	}
	for i := range 2*matchlog.Interval + 1 {
		if err := recorder.Append([][]byte{nil, nil}, uint64(i+1)); err != nil {
			t.Fatal(err)
		}
	}
	begin := func(first, size int) []byte {
		msg := binary.LittleEndian.AppendUint32([]byte{viewBegin}, uint32(first))
		return binary.LittleEndian.AppendUint32(msg, uint32(size))
	}
	var v viewInbox
	if err := v.receive(begin(0, matchlog.MaxEncodedSize+1)); err == nil || v.data != nil {
		t.Fatal("oversize announced segment was not rejected before allocation")
	}
	if err := v.receive([]byte{viewChunk, 1}); err == nil {
		t.Fatal("accepted data before the segment header")
	}
	for _, span := range [][2]int{{0, 60}, {60, 120}, {120, 121}} {
		data, ok := recorder.Segment(span[0], span[1])
		if !ok {
			t.Fatalf("cannot encode segment %v", span)
		}
		if err := v.receive(begin(span[0]+1, len(data))); err == nil {
			t.Fatalf("accepted a gap before segment %v", span)
		}
		if err := v.receive(begin(span[0], len(data))); err != nil {
			t.Fatal(err)
		}
		if err := v.receive(append([]byte{viewChunk}, data[:5]...)); err != nil {
			t.Fatal(err)
		}
		if err := v.receive(append([]byte{viewChunk}, data[5:]...)); err != nil {
			t.Fatal(err)
		}
	}
	if err := v.receive(begin(121, 1024)); err == nil {
		t.Fatal("accepted a segment after a nonregular final checkpoint")
	}
	for _, ticks := range []int{60, 60, 1} {
		if got := v.pop().log.Ticks(); got != ticks {
			t.Fatalf("segment has %d ticks, want %d", got, ticks)
		}
	}
}
