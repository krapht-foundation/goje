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
	bits := func(in engine.Input) [20]uint64 {
		b := [20]uint64{
			math.Float64bits(in.Pointer.X), math.Float64bits(in.Pointer.Y),
			math.Float64bits(in.PrimaryPress.X), math.Float64bits(in.PrimaryPress.Y),
			math.Float64bits(in.SecondaryPress.X), math.Float64bits(in.SecondaryPress.Y),
			math.Float64bits(in.PrimaryRelease.X), math.Float64bits(in.PrimaryRelease.Y),
			math.Float64bits(in.SecondaryRelease.X), math.Float64bits(in.SecondaryRelease.Y),
		}
		for i, touch := range in.Touches {
			b[10+2*i], b[11+2*i] = math.Float64bits(touch.X), math.Float64bits(touch.Y)
		}
		return b
	}
	a2, b2 := a, b
	for _, in := range []*engine.Input{&a2, &b2} {
		in.Pointer.X, in.Pointer.Y = 0, 0
		in.PrimaryPress.X, in.PrimaryPress.Y = 0, 0
		in.SecondaryPress.X, in.SecondaryPress.Y = 0, 0
		in.PrimaryRelease.X, in.PrimaryRelease.Y = 0, 0
		in.SecondaryRelease.X, in.SecondaryRelease.Y = 0, 0
		for i := range in.Touches {
			in.Touches[i].X, in.Touches[i].Y = 0, 0
		}
	}
	return a2 == b2 && bits(a) == bits(b)
}

// listRecording uses touches and gamepads, with the touch coordinates that
// ordinary equality would merge or lose.
func listRecording() *replay.Recording {
	negZero := math.Copysign(0, -1)
	nan := math.Float64frombits(0x7ff8000000000001)
	touches := engine.Input{PrimaryHeld: true}
	touches.Touches[0] = engine.Touch{ID: 7, X: negZero, Y: nan, Active: true}
	touches.Touches[1] = engine.Touch{ID: 1 << 31, Active: true}
	pads := engine.Input{Held: engine.KeyA}
	pads.Gamepads[1] = engine.Gamepad{Connected: true}
	pads.Gamepads[3] = engine.Gamepad{
		Connected: true, Buttons: engine.GamepadSouth | engine.GamepadRight,
		LeftX: -127, LeftY: 127, RightX: -1, RightY: 1, LeftTrigger: 255, RightTrigger: 1,
	}
	both := pads
	var id uint32
	for i := range engine.MaxTouches {
		id++
		both.Touches[i] = engine.Touch{ID: id, X: float64(i), Y: 2, Active: true}
	}
	both.Gamepads[0] = engine.Gamepad{Connected: true, LeftY: -64}
	return &replay.Recording{
		Meta:     meta,
		Interval: replay.CheckpointInterval,
		Runs: []replay.Run{
			{Input: touches, Ticks: 2}, {Input: pads, Ticks: 1}, {Input: both, Ticks: 3}, {Ticks: 1},
		},
		Checkpoints: []replay.Checkpoint{{Tick: 7, Hash: 3}},
	}
}

func TestTouchAndGamepadRoundTrip(t *testing.T) {
	want := listRecording()
	data, err := replay.Encode(want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := replay.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Runs) != len(want.Runs) {
		t.Fatalf("decoded %d runs", len(got.Runs))
	}
	for i := range want.Runs {
		g, w := got.Runs[i], want.Runs[i]
		if g.Ticks != w.Ticks || !exactInput(g.Input, w.Input) {
			t.Errorf("run %d = %+v, want %+v", i, g, w)
		}
	}
	again, err := replay.Encode(got)
	if err != nil || !bytes.Equal(again, data) {
		t.Fatalf("re-encoding differs: %v", err)
	}
	for n := range len(data) {
		if _, err := replay.Decode(data[:n]); err == nil {
			t.Errorf("accepted a %d byte prefix", n)
		}
	}
	// A touch differing only in the sign of zero starts a new run.
	changed := listRecording()
	changed.Runs[0].Input.Touches[0].X = 0
	changed.Runs = append(changed.Runs[:1], replay.Run{Input: want.Runs[0].Input, Ticks: 1})
	changed.Checkpoints = nil
	if _, err := replay.Encode(changed); err != nil {
		t.Errorf("touches that differ in the sign of zero were merged: %v", err)
	}
}

func TestEncodeRejects(t *testing.T) {
	for name, edit := range map[string]func(*replay.Recording){
		"bad game":            func(r *replay.Recording) { r.Game = "Bad" },
		"zero interval":       func(r *replay.Recording) { r.Interval = 0 },
		"large interval":      func(r *replay.Recording) { r.Interval = 1 << 16 },
		"large start":         func(r *replay.Recording) { r.Start = make([]byte, replay.MaxStartSize+1) },
		"empty run":           func(r *replay.Recording) { r.Runs[0].Ticks = 0 },
		"too many ticks":      func(r *replay.Recording) { r.Runs[0].Ticks = replay.MaxTicks },
		"unknown held key":    func(r *replay.Recording) { r.Runs[0].Input.Held = engine.Key9 << 1 },
		"unknown pressed key": func(r *replay.Recording) { r.Runs[0].Input.Pressed = 1 << 31 },
		"same adjacent runs":  func(r *replay.Recording) { r.Runs[1].Input = r.Runs[0].Input },
		"checkpoint order":    func(r *replay.Recording) { r.Checkpoints[1].Tick = 2 },
		"inactive touch with data": func(r *replay.Recording) {
			r.Runs[0].Input.Touches[2].X = math.Copysign(0, -1)
		},
		"active touch after inactive": func(r *replay.Recording) {
			r.Runs[0].Input.Touches[1] = engine.Touch{ID: 1, Active: true}
		},
		"active touch without ID": func(r *replay.Recording) {
			r.Runs[0].Input.Touches[0] = engine.Touch{X: 1, Active: true}
		},
		"disconnected pad with data": func(r *replay.Recording) {
			r.Runs[0].Input.Gamepads[3].Buttons = engine.GamepadStart
		},
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
	const touches, pads = 1 << 14, 1 << 15
	// pad is a resting pad entry in a slot; touch is a touch entry at (0, 0).
	pad := func(slot byte) []byte { return []byte{slot, 0, 0, 0, 0, 0, 0, 0, 0} }
	touch := func(id byte) []byte { return append([]byte{id, 0, 0, 0}, make([]byte, 16)...) }
	list := func(count byte, entries ...[]byte) []byte {
		return append([]byte{count}, bytes.Join(entries, nil)...)
	}
	for _, data := range [][]byte{
		run(1 << 12),
		run(touches, list(1, touch(1))...),
		run(pads, list(2, pad(0), pad(3))...),
		run(touches|pads, append(list(1, touch(9)), list(1, pad(2))...)...),
	} {
		if _, err := replay.Decode(data); err != nil {
			t.Fatalf("valid run rejected: %v", err)
		}
	}
	for name, tc := range map[string]struct {
		data []byte
		want string
	}{
		"held zero":        {run(1, 0, 0, 0, 0), "held keys flagged but empty"},
		"pressed zero":     {run(2, 0, 0, 0, 0), "pressed keys flagged but empty"},
		"pointer zero":     {run(4, make([]byte, 16)...), "coordinates flagged but zero"},
		"unknown key":      {run(1, 0, 0, 0, 2), "unknown key bits"},
		"no touches":       {run(touches, 0), "touch count 0"},
		"too many touches": {run(touches, 6), "touch count 6"},
		"inactive touch":   {run(touches, list(2, touch(1), touch(0))...), "touch 1 is inactive"},
		"no pads":          {run(pads, 0), "gamepad count 0"},
		"too many pads":    {run(pads, 5), "gamepad count 5"},
		"pad out of range": {run(pads, list(1, pad(4))...), "gamepad slot 4"},
		"pad listed twice": {run(pads, list(2, pad(1), pad(1))...), "gamepad slot 1"},
		"pads out of order": {
			run(pads, list(2, pad(2), pad(1))...), "gamepad slot 1",
		},
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
	lists, err := replay.Encode(listRecording())
	if err != nil {
		f.Fatal(err)
	}
	f.Add(lists)
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
