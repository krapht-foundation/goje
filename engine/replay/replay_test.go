package replay_test

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/krapht-foundation/goje/engine"
	"github.com/krapht-foundation/goje/engine/replay"
)

var meta = replay.Meta{Game: "test_game", Content: 0x0123456789abcdef}

// counter is a deterministic scene: its state depends only on its input
// history. KeyR restarts it, KeyEnter replaces it with next, and a press of
// Key1 makes Update fail.
type counter struct {
	state uint64
	next  engine.SceneFactory
	log   *[]string
	name  string
}

func (c *counter) Enter() error {
	if c.log != nil {
		*c.log = append(*c.log, "enter "+c.name)
	}
	return nil
}

func (c *counter) Update(tick engine.Tick) (engine.Transition, error) {
	in := tick.Input
	// The touch and pad terms are zero without those fields, so recordings
	// made before them keep their checkpoint hashes.
	c.state = c.state*1099511628211 ^ uint64(in.Held)<<32 ^ uint64(in.Pressed) ^
		math.Float64bits(in.Pointer.X) ^ math.Float64bits(in.PrimaryRelease.Y)>>1 ^
		math.Float64bits(in.Touches[0].Y)>>2 ^ uint64(in.Gamepads[1].LeftTrigger)<<40
	switch {
	case in.Pressed.Has(engine.Key1):
		return engine.Transition{}, errors.New("scripted failure")
	case in.Pressed.Has(engine.KeyR):
		return engine.Restart(), nil
	case in.Pressed.Has(engine.KeyEnter) && c.next != nil:
		return engine.Replace(c.next), nil
	}
	return engine.Transition{}, nil
}

func (*counter) Draw(engine.Canvas) {}

func (c *counter) Exit() {
	if c.log != nil {
		*c.log = append(*c.log, "exit "+c.name)
	}
}

// hashed adds engine.Hasher, and saved adds engine.Snapshotter as well.
type hashed struct{ counter }

func (h *hashed) StateHash() uint64 { return h.state }

type saved struct{ hashed }

func (s *saved) Snapshot() ([]byte, error) {
	return binary.LittleEndian.AppendUint64(nil, s.state), nil
}

func (s *saved) Restore(data []byte) error {
	if len(data) != 8 {
		return errors.New("bad snapshot")
	}
	s.state = binary.LittleEndian.Uint64(data)
	return nil
}

type snapOnly struct{ counter }

func (s *snapOnly) Snapshot() ([]byte, error) { return nil, nil }
func (s *snapOnly) Restore([]byte) error      { return nil }

// script returns a scripted input for each tick, with pointer motion, keys,
// and release edges.
func script(ticks int) []engine.Input {
	inputs := make([]engine.Input, ticks)
	for i := range inputs {
		in := &inputs[i]
		if i%7 < 3 {
			in.Held = engine.KeyArrowLeft | engine.KeyW
		}
		if i%11 == 0 {
			in.Pointer = engine.Pointer{X: float64(i) / 3, Y: 0.1 * float64(i), Inside: true}
		}
		if i%13 == 5 {
			in.PrimaryRelease = engine.MousePress{X: 1.5, Y: float64(i), Valid: true}
			in.PrimaryHeld = true
		}
	}
	return inputs
}

// record steps factory through the inputs under a recorder and finishes it.
func record(
	t *testing.T,
	factory engine.SceneFactory,
	inputs []engine.Input,
	start []byte,
) *replay.Recording {
	t.Helper()
	rec, err := replay.NewRecorder(meta, start, len(inputs))
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := engine.Start(replay.Record(factory, rec))
	if err != nil {
		t.Fatal(err)
	}
	for _, in := range inputs {
		if err = runtime.Step(in); err != nil {
			t.Fatal(err)
		}
	}
	recording := rec.Finish()
	if err = runtime.Stop(); err != nil {
		t.Fatal(err)
	}
	// Every recording a recorder makes must survive the format.
	data, err := replay.Encode(recording)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := replay.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	return decoded
}

// withInputs returns a copy of rec whose runs hold inputs.
func withInputs(rec *replay.Recording, inputs []engine.Input) *replay.Recording {
	out := *rec
	out.Runs = nil
	for _, in := range inputs {
		if n := len(out.Runs); n > 0 && out.Runs[n-1].Input == in {
			out.Runs[n-1].Ticks++
			continue
		}
		out.Runs = append(out.Runs, replay.Run{Input: in, Ticks: 1})
	}
	return &out
}

func checkpointTicks(rec *replay.Recording) []int {
	ticks := make([]int, 0, len(rec.Checkpoints))
	for _, c := range rec.Checkpoints {
		ticks = append(ticks, c.Tick)
	}
	return ticks
}

func TestRecordPlayAndDetectChangedInput(t *testing.T) {
	factory := func() engine.Scene { return &hashed{} }
	inputs := script(150)
	rec := record(t, factory, inputs, nil)
	if got := checkpointTicks(rec); !slices.Equal(got, []int{60, 120, 150}) {
		t.Fatalf("checkpoint ticks = %v", got)
	}
	// Pinned so that native and WebAssembly runs record the same bytes.
	data, err := replay.Encode(rec)
	if err != nil {
		t.Fatal(err)
	}
	if got := replay.ContentHash(data); got != 0x71c9db858885773e {
		t.Errorf("recording hash = %#x", got)
	}
	if rec.Ticks() != 150 || len(rec.Runs) >= 150 {
		t.Fatalf("%d ticks in %d runs", rec.Ticks(), len(rec.Runs))
	}
	if err = replay.Verify(factory, rec, meta); err != nil {
		t.Fatal(err)
	}

	inputs[69].Held |= engine.KeyS
	var divergence *replay.DivergenceError
	err = replay.Verify(factory, withInputs(rec, inputs), meta)
	if !errors.As(err, &divergence) || divergence.Tick != 120 {
		t.Fatalf("changed input at tick 70: got %v, want divergence at tick 120", err)
	}
}

// listScript is script with touches and a gamepad.
func listScript(ticks int) []engine.Input {
	inputs := script(ticks)
	for i := range inputs {
		in := &inputs[i]
		if i%9 < 4 {
			in.Touches[0] = engine.Touch{ID: 1, X: float64(i), Y: float64(i) / 2, Active: true}
		}
		if i%9 == 2 {
			in.Touches[1] = engine.Touch{ID: 2, X: 0.25, Y: -1, Active: true}
		}
		if i%17 < 8 {
			in.Gamepads[1] = engine.Gamepad{Connected: true, LeftY: -100, RightTrigger: 200}
			if i%5 == 0 {
				in.Gamepads[1].Buttons = engine.GamepadSouth | engine.GamepadUp
			}
		}
	}
	return inputs
}

func TestRecordTouchesAndGamepadsIsPinned(t *testing.T) {
	factory := func() engine.Scene { return &hashed{} }
	inputs := listScript(150)
	rec := record(t, factory, inputs, nil)
	// Pinned so that native and WebAssembly runs record the same bytes.
	data, err := replay.Encode(rec)
	if err != nil {
		t.Fatal(err)
	}
	if got := replay.ContentHash(data); got != 0xcdfcb7338151b3f6 {
		t.Errorf("recording hash = %#x", got)
	}
	if err = replay.Verify(factory, rec, meta); err != nil {
		t.Fatal(err)
	}
	inputs[69].Gamepads[1].LeftTrigger = 1
	var divergence *replay.DivergenceError
	err = replay.Verify(factory, withInputs(rec, inputs), meta)
	if !errors.As(err, &divergence) || divergence.Tick != 120 {
		t.Fatalf("changed pad at tick 70: got %v, want divergence at tick 120", err)
	}
}

func TestPlayerIgnoresLiveInputAndStopsAtEnd(t *testing.T) {
	var scene *hashed
	factory := func() engine.Scene { scene = &hashed{}; return scene }
	rec := record(t, factory, script(10), nil)
	want := rec.Checkpoints[len(rec.Checkpoints)-1].Hash

	player, err := replay.Play(factory, rec, meta)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := engine.Start(player.Factory())
	if err != nil {
		t.Fatal(err)
	}
	for range 20 {
		if err := runtime.Step(engine.Input{Held: engine.KeyD, Pressed: engine.KeyR}); err != nil {
			t.Fatal(err)
		}
	}
	if !player.Done() || player.Played() != 10 || player.Ticks() != 10 {
		t.Fatalf("done %v after %d of %d ticks", player.Done(), player.Played(), player.Ticks())
	}
	if scene.state != want {
		t.Fatalf("final state %016x, want %016x", scene.state, want)
	}
}

func TestRecordAcrossRestartAndReplace(t *testing.T) {
	var log []string
	var plain, second, first engine.SceneFactory
	plain = func() engine.Scene { return &counter{log: &log, name: "plain"} }
	second = func() engine.Scene { return &hashed{counter{next: plain, log: &log, name: "second"}} }
	first = func() engine.Scene { return &saved{hashed{counter{next: second, log: &log, name: "first"}}} }

	inputs := script(150)
	inputs[29].Pressed = engine.KeyR      // restart first
	inputs[89].Pressed = engine.KeyEnter  // replace with second
	inputs[129].Pressed = engine.KeyEnter // replace with plain, which has no hash
	rec := record(t, first, inputs, nil)
	if got := checkpointTicks(rec); !slices.Equal(got, []int{60, 120}) {
		t.Fatalf("checkpoint ticks = %v", got)
	}
	wantLog := "enter first,exit first,enter first,exit first,enter second,exit second,enter plain,exit plain"
	if got := strings.Join(log, ","); got != wantLog {
		t.Fatalf("scene log = %s", got)
	}
	if err := replay.Verify(first, rec, meta); err != nil {
		t.Fatal(err)
	}

	// The final scene has no hash, so a playback that ends on a hashed scene
	// diverges at the last tick.
	inputs[129].Pressed = 0
	err := replay.Verify(first, withInputs(rec, inputs), meta)
	var divergence *replay.DivergenceError
	if !errors.As(err, &divergence) || divergence.Tick != 150 {
		t.Fatalf("got %v, want divergence at tick 150", err)
	}
}

func TestFinalHashOnTransitionTick(t *testing.T) {
	var log []string
	factory := func() engine.Scene { return &hashed{counter{log: &log, name: "a"}} }
	inputs := script(61)
	inputs[60].Pressed = engine.KeyR
	rec := record(t, factory, inputs, nil)
	if got := checkpointTicks(rec); !slices.Equal(got, []int{60, 61}) {
		t.Fatalf("checkpoint ticks = %v", got)
	}
	if err := replay.Verify(factory, rec, meta); err != nil {
		t.Fatal(err)
	}
}

func TestUpdateErrorIsReturned(t *testing.T) {
	factory := func() engine.Scene { return &hashed{} }
	rec := record(t, factory, script(5), nil)
	inputs := script(5)
	inputs[2].Pressed = engine.Key1
	if err := replay.Verify(factory, withInputs(rec, inputs), meta); err == nil ||
		!strings.Contains(err.Error(), "scripted failure") {
		t.Fatalf("got %v, want the scene's error", err)
	}
}

func TestStartStateRestore(t *testing.T) {
	start := binary.LittleEndian.AppendUint64(nil, 0xfeed)
	// The recorded game restores the loaded save itself before the first tick.
	recordFactory := func() engine.Scene {
		s := &saved{}
		if err := s.Restore(start); err != nil {
			t.Fatal(err)
		}
		return s
	}
	rec := record(t, recordFactory, script(70), start)
	if !bytes.Equal(rec.Start, start) {
		t.Fatalf("start = %x", rec.Start)
	}
	factory := func() engine.Scene { return &saved{} }
	if err := replay.Verify(factory, rec, meta); err != nil {
		t.Fatal(err)
	}
	if err := replay.Verify(func() engine.Scene { return &hashed{} }, rec, meta); err == nil ||
		!strings.Contains(err.Error(), "Snapshotter") {
		t.Fatalf("scene without Restore: got %v", err)
	}
	bad := *rec
	bad.Start = []byte{1}
	if err := replay.Verify(
		factory,
		&bad,
		meta,
	); err == nil ||
		!strings.Contains(err.Error(), "bad snapshot") {
		t.Fatalf("rejected start state: got %v", err)
	}
	// Without the start state, the first checkpoint differs.
	bad.Start = nil
	var divergence *replay.DivergenceError
	if err := replay.Verify(
		factory,
		&bad,
		meta,
	); !errors.As(err, &divergence) ||
		divergence.Tick != 60 {
		t.Fatalf("missing start state: got %v", err)
	}
}

func TestContentMismatch(t *testing.T) {
	factory := func() engine.Scene { return &hashed{} }
	rec := record(t, factory, script(3), nil)
	for _, other := range []replay.Meta{{Game: "other", Content: meta.Content}, {Game: meta.Game, Content: 1}} {
		if err := replay.Verify(factory, rec, other); !errors.Is(err, replay.ErrContentMismatch) {
			t.Errorf("%+v: got %v", other, err)
		}
	}
	if err := meta.Check(meta); err != nil {
		t.Error(err)
	}
}

func TestWrappersExposeInnerInterfaces(t *testing.T) {
	rec, err := replay.NewRecorder(meta, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	empty := &replay.Recording{Meta: meta, Interval: replay.CheckpointInterval}
	for _, tc := range []struct {
		scene            engine.Scene
		hasher, snapshot bool
	}{
		{&counter{}, false, false},
		{&hashed{}, true, false},
		{&snapOnly{}, false, true},
		{&saved{}, true, true},
	} {
		player, err := replay.Play(func() engine.Scene { return tc.scene }, empty, meta)
		if err != nil {
			t.Fatal(err)
		}
		for _, wrapped := range []engine.Scene{replay.Record(func() engine.Scene { return tc.scene }, rec)(), player.Factory()()} {
			_, hasher := wrapped.(engine.Hasher)
			_, snapshot := wrapped.(engine.Snapshotter)
			if hasher != tc.hasher || snapshot != tc.snapshot {
				t.Errorf("%T wrapped as %T: Hasher %v, Snapshotter %v", tc.scene, wrapped, hasher, snapshot)
			}
		}
	}
	s := &saved{}
	s.state = 42
	wrapped := replay.Record(func() engine.Scene { return s }, rec)()
	if wrapped.(engine.Hasher).StateHash() != 42 {
		t.Error("StateHash is not forwarded")
	}
	if err := wrapped.(engine.Snapshotter).Restore(
		binary.LittleEndian.AppendUint64(nil, 7),
	); err != nil ||
		s.state != 7 {
		t.Errorf("Restore is not forwarded: %v", err)
	}
	if data, err := wrapped.(engine.Snapshotter).Snapshot(); err != nil || len(data) != 8 {
		t.Errorf("Snapshot is not forwarded: %v", err)
	}
}

func TestRecorderStopsAtTickLimit(t *testing.T) {
	rec, err := replay.NewRecorder(meta, nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := engine.Start(replay.Record(func() engine.Scene { return &hashed{} }, rec))
	if err != nil {
		t.Fatal(err)
	}
	for range replay.MaxTicks + 10 {
		if err := runtime.Step(engine.Input{}); err != nil {
			t.Fatal(err)
		}
	}
	if !rec.Full() || rec.Ticks() != replay.MaxTicks {
		t.Fatalf("full %v after %d ticks", rec.Full(), rec.Ticks())
	}
	recording := rec.Finish()
	if last := recording.Checkpoints[len(recording.Checkpoints)-1].Tick; last != replay.MaxTicks {
		t.Fatalf("last checkpoint at %d", last)
	}
	if _, err := replay.Encode(recording); err != nil {
		t.Fatal(err)
	}
}

func TestRecorderStopsAtSizeLimit(t *testing.T) {
	rec, err := replay.NewRecorder(meta, make([]byte, replay.MaxStartSize), 0)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := engine.Start(replay.Record(func() engine.Scene { return &hashed{} }, rec))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; !rec.Full(); i++ {
		// Every field present: the largest run there is.
		p := engine.MousePress{X: float64(i), Y: 1, Valid: true}
		in := engine.Input{
			Held:             engine.KeyA,
			Pressed:          engine.KeyD,
			Pointer:          engine.Pointer{X: float64(i), Y: 2, Inside: true},
			PrimaryPress:     p,
			SecondaryPress:   p,
			PrimaryRelease:   p,
			SecondaryRelease: p,
		}
		for j := range in.Touches {
			in.Touches[j] = engine.Touch{ID: 9, X: float64(i), Y: 3, Active: true}
		}
		for j := range in.Gamepads {
			in.Gamepads[j] = engine.Gamepad{Connected: true, Buttons: engine.GamepadSouth}
		}
		if err = runtime.Step(in); err != nil {
			t.Fatal(err)
		}
	}
	ticks := rec.Ticks()
	if err = runtime.Step(engine.Input{}); err != nil || rec.Ticks() != ticks {
		t.Fatalf("recorded past the limit: %v, %d ticks", err, rec.Ticks())
	}
	data, err := replay.Encode(rec.Finish())
	if err != nil {
		t.Fatal(err)
	}
	// The recorder reserves one largest run and two checkpoints, 256 bytes.
	if len(data) > replay.MaxEncodedSize || len(data) < replay.MaxEncodedSize-256 {
		t.Fatalf("encoded %d bytes, want just under %d", len(data), replay.MaxEncodedSize)
	}
}

func TestRecordingAllocatesNothing(t *testing.T) {
	rec, err := replay.NewRecorder(meta, nil, 4000)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := engine.Start(replay.Record(func() engine.Scene { return &hashed{} }, rec))
	if err != nil {
		t.Fatal(err)
	}
	inputs := script(64)
	i := 0
	allocs := testing.AllocsPerRun(3000, func() {
		i++
		if err := runtime.Step(inputs[i%len(inputs)]); err != nil {
			t.Fatal(err)
		}
	})
	if allocs != 0 {
		t.Fatalf("recording allocated %v times per tick", allocs)
	}
}

func TestNewRecorderRejects(t *testing.T) {
	for _, tc := range []struct {
		meta     replay.Meta
		start    []byte
		capacity int
	}{
		{replay.Meta{Game: ""}, nil, 0},
		{replay.Meta{Game: "Upper"}, nil, 0},
		{replay.Meta{Game: "1st"}, nil, 0},
		{replay.Meta{Game: "has-dash"}, nil, 0},
		{replay.Meta{Game: strings.Repeat("a", 33)}, nil, 0},
		{meta, make([]byte, replay.MaxStartSize+1), 0},
		{meta, nil, -1},
		{meta, nil, replay.MaxTicks + 1},
	} {
		if _, err := replay.NewRecorder(tc.meta, tc.start, tc.capacity); err == nil {
			t.Errorf("accepted %q, %d start bytes, capacity %d", tc.meta.Game, len(tc.start), tc.capacity)
		}
	}
	if _, err := replay.NewRecorder(replay.Meta{Game: strings.Repeat("z", 32)}, nil, 0); err != nil {
		t.Error(err)
	}
}

func TestContentHash(t *testing.T) {
	if replay.ContentHash(
		[]byte("ab"),
		[]byte("c"),
	) == replay.ContentHash(
		[]byte("a"),
		[]byte("bc"),
	) {
		t.Error("moving bytes between parts kept the hash")
	}
	if replay.ContentHash() == replay.ContentHash(nil) {
		t.Error("an empty part kept the hash")
	}
	// Pinned so that the value is stable across releases and targets.
	if got := replay.ContentHash([]byte("level"), []byte("art")); got != 0x9d8f472d11570b8a {
		t.Errorf("ContentHash = %#x", got)
	}
}
