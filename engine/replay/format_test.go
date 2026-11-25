package replay_test

import (
	"bytes"
	"encoding/binary"
	"math"
	"strings"
	"testing"

	"github.com/krapht-foundation/goje/engine"
	"github.com/krapht-foundation/goje/engine/replay"
)

// fullRecording uses every input field, a start state, and the bit patterns
// that ordinary equality would merge or lose: -0, NaN, and the last key.
func fullRecording() *replay.Recording {
	negZero := math.Copysign(0, -1)
	nan := math.Float64frombits(0x7ff8000000000001)
	return &replay.Recording{
		Meta:     meta,
		Interval: 2,
		Start:    []byte("start"),
		Runs: []replay.Run{
			{Ticks: 3},
			{Input: engine.Input{Pointer: engine.Pointer{X: negZero}}, Ticks: 1},
			{Input: engine.Input{
				Held:    engine.Key9 | engine.KeyArrowLeft,
				Pressed: engine.KeyShift,
				Pointer: engine.Pointer{X: 1.25, Y: nan, Inside: true},
				PrimaryPress: engine.MousePress{
					X:     0,
					Y:     0,
					Valid: true,
				},
				SecondaryPress:   engine.MousePress{X: 3, Y: 4},
				PrimaryRelease:   engine.MousePress{X: math.MaxFloat64, Y: -1, Valid: true},
				SecondaryRelease: engine.MousePress{X: 5e-324, Y: 6, Valid: true},
				PrimaryHeld:      true,
				SecondaryHeld:    true,
			}, Ticks: 2},
			{Input: engine.Input{SecondaryHeld: true}, Ticks: 1},
		},
		Checkpoints: []replay.Checkpoint{
			{Tick: 2, Hash: 1},
			{Tick: 6, Hash: math.MaxUint64},
			{Tick: 7, Hash: 0},
		},
	}
}

func TestFormatRoundTrip(t *testing.T) {
	want := fullRecording()
	data, err := replay.Encode(want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := replay.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	if got.Meta != want.Meta || got.Interval != want.Interval ||
		!bytes.Equal(got.Start, want.Start) ||
		len(got.Runs) != len(want.Runs) ||
		len(got.Checkpoints) != len(want.Checkpoints) {
		t.Fatalf("decoded %+v", got)
	}
	for i := range want.Runs {
		g, w := got.Runs[i], want.Runs[i]
		if g.Ticks != w.Ticks || !exactInput(g.Input, w.Input) {
			t.Errorf("run %d = %+v, want %+v", i, g, w)
		}
	}
	for i := range want.Checkpoints {
		if got.Checkpoints[i] != want.Checkpoints[i] {
			t.Errorf("checkpoint %d = %+v", i, got.Checkpoints[i])
		}
	}
	again, err := replay.Encode(got)
	if err != nil || !bytes.Equal(again, data) {
		t.Fatalf("re-encoding differs: %v", err)
	}

	empty := &replay.Recording{Meta: meta, Interval: replay.CheckpointInterval}
	data, err = replay.Encode(empty)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := replay.Decode(
		data,
	); err != nil || got.Ticks() != 0 || got.Start != nil ||
		got.Checkpoints != nil {
		t.Fatalf("empty recording: %+v, %v", got, err)
	}
}

func exactInput(a, b engine.Input) bool {
	bits := func(in engine.Input) [10]uint64 {
		return [10]uint64{
			math.Float64bits(in.Pointer.X), math.Float64bits(in.Pointer.Y),
			math.Float64bits(in.PrimaryPress.X), math.Float64bits(in.PrimaryPress.Y),
			math.Float64bits(in.SecondaryPress.X), math.Float64bits(in.SecondaryPress.Y),
			math.Float64bits(in.PrimaryRelease.X), math.Float64bits(in.PrimaryRelease.Y),
			math.Float64bits(in.SecondaryRelease.X), math.Float64bits(in.SecondaryRelease.Y),
		}
	}
	a2, b2 := a, b
	for _, in := range []*engine.Input{&a2, &b2} {
		in.Pointer.X, in.Pointer.Y = 0, 0
		in.PrimaryPress.X, in.PrimaryPress.Y = 0, 0
		in.SecondaryPress.X, in.SecondaryPress.Y = 0, 0
		in.PrimaryRelease.X, in.PrimaryRelease.Y = 0, 0
		in.SecondaryRelease.X, in.SecondaryRelease.Y = 0, 0
	}
	return a2 == b2 && bits(a) == bits(b)
}

func TestEncodeRejects(t *testing.T) {
	for name, edit := range map[string]func(*replay.Recording){
		"bad game":             func(r *replay.Recording) { r.Game = "Bad" },
		"zero interval":        func(r *replay.Recording) { r.Interval = 0 },
		"large interval":       func(r *replay.Recording) { r.Interval = 1 << 16 },
		"large start":          func(r *replay.Recording) { r.Start = make([]byte, replay.MaxStartSize+1) },
		"empty run":            func(r *replay.Recording) { r.Runs[0].Ticks = 0 },
		"too many ticks":       func(r *replay.Recording) { r.Runs[0].Ticks = replay.MaxTicks },
		"unknown held key":     func(r *replay.Recording) { r.Runs[0].Input.Held = engine.Key9 << 1 },
		"unknown pressed key":  func(r *replay.Recording) { r.Runs[0].Input.Pressed = 1 << 31 },
		"same adjacent runs":   func(r *replay.Recording) { r.Runs[1].Input = r.Runs[0].Input },
		"checkpoint order":     func(r *replay.Recording) { r.Checkpoints[1].Tick = 2 },
		"checkpoint zero":      func(r *replay.Recording) { r.Checkpoints[0].Tick = 0 },
		"checkpoint past end":  func(r *replay.Recording) { r.Checkpoints[2].Tick = 8 },
		"checkpoint off cycle": func(r *replay.Recording) { r.Checkpoints[1].Tick = 5 },
		"too large": func(r *replay.Recording) {
			r.Start = make([]byte, replay.MaxStartSize)
			r.Runs = nil
			for i := range 1 << 16 {
				r.Runs = append(r.Runs, replay.Run{Input: engine.Input{Pointer: engine.Pointer{X: float64(i + 1)}}, Ticks: 1})
			}
			r.Checkpoints = nil
		},
	} {
		rec := fullRecording()
		edit(rec)
		if _, err := replay.Encode(rec); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// header offsets in the encoding of fullRecording.
const (
	offVersion  = 8
	offGameLen  = 10
	offContent  = 11 + 9
	offInterval = offContent + 8
	offStartLen = offInterval + 2
	offTicks    = offStartLen + 4 + 5
	offRuns     = offTicks + 4
	offFirstRun = offRuns + 4
)

func TestDecodeRejects(t *testing.T) {
	valid, err := replay.Encode(fullRecording())
	if err != nil {
		t.Fatal(err)
	}
	checkpointsAt := len(valid) - 4 - 3*12
	put32 := func(at int, v uint32) func([]byte) []byte {
		return func(b []byte) []byte { binary.LittleEndian.PutUint32(b[at:], v); return b }
	}
	for name, tc := range map[string]struct {
		edit func([]byte) []byte
		want string
	}{
		"bad magic":            {func(b []byte) []byte { b[0] = 'X'; return b }, "bad magic"},
		"version":              {func(b []byte) []byte { b[offVersion] = 2; return b }, "unsupported version 2"},
		"empty game":           {func(b []byte) []byte { b[offGameLen] = 0; return b }, "game name"},
		"bad game":             {func(b []byte) []byte { b[offGameLen+1] = 'A'; return b }, "game name"},
		"zero interval":        {func(b []byte) []byte { b[offInterval] = 0; return b }, "interval is zero"},
		"large start":          {put32(offStartLen, replay.MaxStartSize+1), "start state of"},
		"start past end":       {put32(offStartLen, replay.MaxStartSize), "truncated"},
		"too many ticks":       {put32(offTicks, replay.MaxTicks+1), "exceed the limits"},
		"more runs than ticks": {put32(offRuns, 8), "exceed the limits"},
		"runs over data": {func([]byte) []byte {
			b := b0()
			binary.LittleEndian.PutUint32(b[len(b)-12:], 1<<20)
			binary.LittleEndian.PutUint32(b[len(b)-8:], 1<<20)
			return b
		}, "exceed the limits or the data"},
		"tick sum":          {put32(offTicks, 6), "header says 6"},
		"unknown flag":      {func(b []byte) []byte { b[offFirstRun+1] = 0x80; return b }, "unknown flags"},
		"empty run":         {put32(offFirstRun+2, 0), "tick count 0"},
		"checkpoints count": {put32(checkpointsAt, 5), "checkpoints exceed"},
		"checkpoint order":  {put32(checkpointsAt+4+12, 2), "out of order"},
		"trailing":          {func(b []byte) []byte { return append(b, 0) }, "trailing"},
		"oversized":         {func([]byte) []byte { return make([]byte, replay.MaxEncodedSize+1) }, "exceeds"},
	} {
		_, err := replay.Decode(tc.edit(bytes.Clone(valid)))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: got %v, want %q", name, err, tc.want)
		}
	}
	for n := range len(valid) {
		if _, err := replay.Decode(valid[:n]); err == nil {
			t.Errorf("accepted a %d byte prefix", n)
		}
	}
}

// b0 is the encoding of a recording without ticks, ending in the tick, run,
// and checkpoint counts.
func b0() []byte {
	data, _ := replay.Encode(&replay.Recording{Meta: meta, Interval: 1})
	return data
}

func TestDecodeRejectsNonCanonicalRuns(t *testing.T) {
	// One run of one tick with the given flags and field bytes.
	run := func(flags uint16, payload ...byte) []byte {
		data := b0()
		data = data[:len(data)-12]
		data = binary.LittleEndian.AppendUint32(data, 1)
		data = binary.LittleEndian.AppendUint32(data, 1)
		data = binary.LittleEndian.AppendUint16(data, flags)
		data = binary.LittleEndian.AppendUint32(data, 1)
		data = append(data, payload...)
		return binary.LittleEndian.AppendUint32(data, 0)
	}
	if _, err := replay.Decode(run(1 << 12)); err != nil {
		t.Fatalf("valid run rejected: %v", err)
	}
	for name, tc := range map[string]struct {
		data []byte
		want string
	}{
		"held zero":    {run(1, 0, 0, 0, 0), "held keys flagged but empty"},
		"pressed zero": {run(2, 0, 0, 0, 0), "pressed keys flagged but empty"},
		"pointer zero": {run(4, make([]byte, 16)...), "coordinates flagged but zero"},
		"unknown key":  {run(1, 0, 0, 0, 2), "unknown key bits"},
	} {
		if _, err := replay.Decode(tc.data); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: got %v, want %q", name, err, tc.want)
		}
	}
}

func FuzzDecode(f *testing.F) {
	valid, err := replay.Encode(fullRecording())
	if err != nil {
		f.Fatal(err)
	}
	f.Add(valid)
	f.Add(b0())
	f.Fuzz(func(t *testing.T, data []byte) {
		rec, err := replay.Decode(data)
		if err != nil {
			return
		}
		again, err := replay.Encode(rec)
		if err != nil {
			t.Fatalf("decoded recording does not encode: %v", err)
		}
		if !bytes.Equal(again, data) {
			t.Fatal("re-encoding differs")
		}
	})
}
