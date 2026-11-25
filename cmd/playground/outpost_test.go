package main

import (
	"errors"
	"image"
	"image/color"
	"slices"
	"testing"

	"github.com/krapht-foundation/goje/engine"
	"github.com/krapht-foundation/goje/engine/replay"
	"github.com/krapht-foundation/goje/engine/save"
	"github.com/krapht-foundation/goje/font"
	"github.com/krapht-foundation/goje/platform"
)

// memoryStore is a platform.Store that keeps values in a map.
type memoryStore map[string][]byte

func (m memoryStore) Put(
	key string,
	value []byte,
) error {
	m[key] = slices.Clone(value)
	return nil
}

func (m memoryStore) Get(key string) ([]byte, error) {
	if v, ok := m[key]; ok {
		return slices.Clone(v), nil
	}
	return nil, platform.ErrNotFound
}

func (m memoryStore) Delete(key string) error { delete(m, key); return nil }

func newOutpostWorld(tb testing.TB) *outpostWorld {
	tb.Helper()
	board, meta, err := loadOutpost(authoredFiles)
	if err != nil {
		tb.Fatal(err)
	}
	return &outpostWorld{
		board:  board,
		meta:   meta,
		face:   font.Builtin(),
		store:  memoryStore{},
		volume: 1,
	}
}

func newOutpostSession(tb testing.TB) *outpostSession {
	tb.Helper()
	world := newOutpostWorld(tb)
	s := world.newSession(nil)
	if err := s.Enter(); err != nil {
		tb.Fatal(err)
	}
	return s
}

func (s *outpostSession) update(tb testing.TB, in engine.Input) {
	tb.Helper()
	if _, err := s.Update(engine.Tick{Input: in}); err != nil {
		tb.Fatal(err)
	}
}

// drag returns the input of the tick that presses at one point and the input
// of the tick that releases at another, the way the hosts report a drag.
func drag(fromX, fromY, toX, toY float64, held engine.Keys) (press, release engine.Input) {
	press = engine.Input{
		Held:         held,
		PrimaryPress: engine.MousePress{X: fromX, Y: fromY, Valid: true}, PrimaryHeld: true,
		Pointer: engine.Pointer{X: fromX, Y: fromY, Inside: true},
	}
	release = engine.Input{
		Held:           held,
		PrimaryRelease: engine.MousePress{X: toX, Y: toY, Valid: true},
		Pointer:        engine.Pointer{X: toX, Y: toY, Inside: true},
	}
	return press, release
}

func (s *outpostSession) dragBox(tb testing.TB, fromX, fromY, toX, toY float64, held engine.Keys) {
	tb.Helper()
	press, release := drag(fromX, fromY, toX, toY, held)
	s.update(tb, press)
	s.update(tb, release)
}

// selectAll drags a box over the whole screen.
func (s *outpostSession) selectAll(tb testing.TB) {
	tb.Helper()
	s.dragBox(tb, 0, 0, width-1, height-1, 0)
}

func (s *outpostSession) rightClick(tb testing.TB, x, y, elevation float64) {
	tb.Helper()
	px, py := s.logical(x, y, elevation)
	s.update(tb, engine.Input{SecondaryPress: engine.MousePress{X: px, Y: py, Valid: true}})
}

func TestOutpostBoxAndClickSelection(t *testing.T) {
	s := newOutpostSession(t)
	// Scouts stand at (0.5, 1.5), (0.5, 3.5), (0.5, 5.5), (0.5, 7.5), (1.5, 2.5), (1.5, 6.5).
	around := func(id uint64) (x, y float64) {
		u, _ := s.sim.Unit(id)
		c := u.Center()
		return s.logical(c.X.Float64(), c.Y.Float64(), skirmishUnitHeight/2)
	}
	x1, y1 := around(1)
	x2, y2 := around(2)

	s.dragBox(t, x1-3, y1-3, x1+3, y1+3, 0)
	if !slices.Equal(s.selection, []uint64{1}) {
		t.Fatalf("a small box around scout 1 selected %v", s.selection)
	}
	s.dragBox(t, min(x1, x2)-3, y1-3, max(x1, x2)+3, y2+3, 0)
	if !slices.Equal(s.selection, []uint64{1, 2}) && !slices.Equal(s.selection, []uint64{1, 2, 5}) {
		t.Fatalf("a box over scouts 1 and 2 selected %v", s.selection)
	}
	s.selectAll(t)
	if !slices.Equal(s.selection, []uint64{1, 2, 3, 4, 5, 6}) {
		t.Fatalf("a box over the screen selected %v, want every scout and no guard", s.selection)
	}

	// A click selects one scout, and Shift toggles scouts in and out.
	s.dragBox(t, x1, y1, x1, y1, 0)
	if !slices.Equal(s.selection, []uint64{1}) {
		t.Fatalf("a click on scout 1 selected %v", s.selection)
	}
	s.dragBox(t, x2, y2, x2, y2, engine.KeyShift)
	if !slices.Equal(s.selection, []uint64{1, 2}) {
		t.Fatalf("a shift-click added scout 2 and gave %v", s.selection)
	}
	s.dragBox(t, x1, y1, x1, y1, engine.KeyShift)
	if !slices.Equal(s.selection, []uint64{2}) {
		t.Fatalf("a shift-click on a selected scout removed it and gave %v", s.selection)
	}
	// A click on a guard or empty ground clears the selection.
	gx, gy := s.logical(9.5, 1.5, skirmishUnitHeight/2)
	s.dragBox(t, gx, gy, gx, gy, 0)
	if len(s.selection) != 0 {
		t.Fatalf("a click on a guard left %v selected", s.selection)
	}
}

func TestOutpostDragThatNeverReleasesIsCancelled(t *testing.T) {
	s := newOutpostSession(t)
	press, _ := drag(0, 0, 100, 100, 0)
	s.update(t, press)
	if !s.drag.active {
		t.Fatal("the press did not start a drag")
	}
	s.update(t, engine.Input{}) // The host lost focus: no button held and no release.
	if s.drag.active || len(s.selection) != 0 {
		t.Fatalf("the drag survived without a release: %+v, selection %v", s.drag, s.selection)
	}
}

func TestOutpostControlGroups(t *testing.T) {
	s := newOutpostSession(t)
	x1, y1 := s.logical(0.5, 1.5, skirmishUnitHeight/2)
	s.dragBox(t, x1, y1, x1, y1, 0)
	s.update(t, engine.Input{Held: engine.KeyControl, Pressed: engine.Key2})
	s.selectAll(t)
	s.update(t, engine.Input{Held: engine.KeyControl, Pressed: engine.Key1})
	s.update(t, engine.Input{Pressed: engine.Key2})
	if !slices.Equal(s.selection, []uint64{1}) {
		t.Fatalf("recalling group 2 selected %v", s.selection)
	}
	s.update(t, engine.Input{Pressed: engine.Key1})
	if len(s.selection) != 6 {
		t.Fatalf("recalling group 1 selected %v", s.selection)
	}
	s.update(t, engine.Input{Pressed: engine.Key9}) // An empty group changes nothing.
	if len(s.selection) != 6 {
		t.Fatalf("recalling an empty group changed the selection to %v", s.selection)
	}
}

func TestOutpostOrdersAndModes(t *testing.T) {
	s := newOutpostSession(t)
	s.selectAll(t)

	// A group move gives each scout a different goal near the click.
	s.rightClick(t, 3.5, 4.5, 0)
	goals := map[[2]int]uint64{}
	for _, id := range s.selection {
		u, _ := s.sim.Unit(id)
		goal, ok := s.sim.Goal(u)
		if !ok {
			t.Fatalf("scout %d has no order after a group move", id)
		}
		if other, dup := goals[[2]int{goal.X, goal.Y}]; dup {
			t.Fatalf("scouts %d and %d were sent to the same cell %v", other, id, goal)
		}
		goals[[2]int{goal.X, goal.Y}] = id
	}

	// Hold fire toggles for the whole selection.
	s.update(t, engine.Input{Pressed: engine.KeyH})
	for _, id := range s.selection {
		if u, _ := s.sim.Unit(id); !u.Hold() {
			t.Fatalf("scout %d is not holding fire", id)
		}
	}
	s.update(t, engine.Input{Pressed: engine.KeyH})
	if u, _ := s.sim.Unit(1); u.Hold() {
		t.Fatal("the second H did not release the hold")
	}

	// A makes the next click an attack-move, and Escape cancels the mode.
	s.update(t, engine.Input{Pressed: engine.KeyA})
	if s.mode != modeAttackMove || !s.cancelable() {
		t.Fatalf("A left mode %d, cancelable %v", s.mode, s.cancelable())
	}
	s.update(t, engine.Input{Pressed: engine.KeyEscape})
	if s.mode != modeNone || s.cancelable() {
		t.Fatalf("Escape left mode %d", s.mode)
	}
	s.update(t, engine.Input{Pressed: engine.KeyA})
	px, py := s.logical(2.5, 4.5, 0)
	s.update(
		t,
		engine.Input{PrimaryPress: engine.MousePress{X: px, Y: py, Valid: true}, PrimaryHeld: true},
	)
	if s.mode != modeNone || s.drag.active {
		t.Fatalf("the attack-move click left mode %d and drag %v", s.mode, s.drag.active)
	}
	s.update(t, engine.Input{PrimaryRelease: engine.MousePress{X: px, Y: py, Valid: true}})
	if len(s.selection) != 6 {
		t.Fatalf("the release of an attack-move click changed the selection to %v", s.selection)
	}

	// A right click on a guard is an attack on it, and so is F with the pointer over one.
	gx, gy := s.logical(9.5, 1.5, skirmishUnitHeight/2)
	s.update(t, engine.Input{SecondaryPress: engine.MousePress{X: gx, Y: gy, Valid: true}})
	s.update(
		t,
		engine.Input{Pressed: engine.KeyF, Pointer: engine.Pointer{X: gx, Y: gy, Inside: true}},
	)
	attacks := 0
	for _, id := range s.selection {
		if u, _ := s.sim.Unit(id); u.Alive {
			attacks++
		}
	}
	if attacks != 6 {
		t.Fatalf("only %d of 6 scouts are alive after the orders", attacks)
	}
}

// outpostScript is the recorded match. All six scouts are selected with a box,
// sent to the middle of the board, and then told every second to focus the guard
// nearest to the first of them. The scouts win and two of them live.
func outpostScript(tb testing.TB) []engine.Input {
	tb.Helper()
	s := newOutpostSession(tb)
	press, release := drag(0, 0, width-1, height-1, 0)
	gx, gy := s.logical(3.5, 4.5, 0)
	inputs := make([]engine.Input, 0, 603)
	inputs = append(
		inputs,
		press,
		release,
		engine.Input{SecondaryPress: engine.MousePress{X: gx, Y: gy, Valid: true}},
	)
	for i := range 600 {
		in := engine.Input{}
		if i > 60 && i%60 == 0 {
			in.Pressed = engine.KeyF
		}
		inputs = append(inputs, in)
	}
	return inputs
}

func runOutpost(tb testing.TB, factory engine.SceneFactory, inputs []engine.Input) {
	tb.Helper()
	runtime, err := engine.Start(factory)
	if err != nil {
		tb.Fatal(err)
	}
	for _, in := range inputs {
		if err := runtime.Step(in); err != nil {
			tb.Fatal(err)
		}
	}
	if err := runtime.Stop(); err != nil {
		tb.Fatal(err)
	}
}

func TestOutpostRecordedMatchWinsAndReplays(t *testing.T) {
	const wantTrace uint64 = 3609343579101645234
	world := newOutpostWorld(t)
	inputs := outpostScript(t)
	var last *outpostSession
	factory := func() engine.Scene {
		last = world.newSession(nil)
		return last
	}
	recorder, err := replay.NewRecorder(world.meta, nil, len(inputs))
	if err != nil {
		t.Fatal(err)
	}
	runOutpost(t, replay.Record(factory, recorder), inputs)
	if last.outcome != outcomeVictory {
		t.Fatalf("outcome = %d after %d ticks, want a victory", last.outcome, last.sim.Ticks())
	}
	if scouts := last.hud.scouts; scouts != 2 {
		t.Errorf("%d scouts survived, want the recorded 2", scouts)
	}
	if got := last.sim.Trace(); got != wantTrace {
		t.Errorf("trace = %d, want %d", got, wantTrace)
	}

	// The recording plays back to the same state on this target, and a changed
	// input is caught at the tick where the history first differs.
	recording := recorder.Finish()
	data, err := replay.Encode(recording)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := replay.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	if err := replay.Verify(factory, decoded, world.meta); err != nil {
		t.Fatalf("the recorded match does not replay: %v", err)
	}
	// A changed input is caught at the first checkpoint after the tick where the
	// history differs: a hold-fire order at tick 200 stops the scouts shooting.
	tampered := *decoded
	tampered.Runs = slices.Clone(decoded.Runs)
	const tamperAt = 200
	tick := 0
	for i, run := range tampered.Runs {
		if tick >= tamperAt && run.Input == (engine.Input{}) {
			tampered.Runs[i].Input.Pressed = engine.KeyH
			break
		}
		tick += run.Ticks
	}
	var divergence *replay.DivergenceError
	if err := replay.Verify(factory, &tampered, world.meta); !errors.As(err, &divergence) {
		t.Fatalf("a recording with a changed input verified: %v", err)
	} else if divergence.Tick <= tamperAt || divergence.Tick > tamperAt+2*replay.CheckpointInterval {
		t.Errorf(
			"the divergence was reported at tick %d for a change at tick about %d",
			divergence.Tick,
			tamperAt,
		)
	}
}

func TestOutpostSnapshotRestoreContinuesIdentically(t *testing.T) {
	inputs := outpostScript(t)
	for _, at := range []int{3, 80, 200, 330} {
		a := newOutpostSession(t)
		for _, in := range inputs[:at] {
			a.update(t, in)
		}
		snapshot, err := a.Snapshot()
		if err != nil {
			t.Fatal(err)
		}
		b := newOutpostSession(t)
		if err := b.Restore(snapshot); err != nil {
			t.Fatalf("restore at tick %d: %v", at, err)
		}
		if a.StateHash() != b.StateHash() {
			t.Fatalf("restoring at tick %d changed the state hash", at)
		}
		for i, in := range inputs[at : at+150] {
			a.update(t, in)
			b.update(t, in)
			if a.StateHash() != b.StateHash() {
				t.Fatalf("restored match diverged %d ticks after tick %d", i+1, at)
			}
		}
	}
}

func TestOutpostRestoreRefusesDamagedSnapshots(t *testing.T) {
	s := newOutpostSession(t)
	s.selectAll(t)
	snapshot, err := s.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	before := s.StateHash()
	for _, bad := range [][]byte{nil, {0}, snapshot[:len(snapshot)-1], append(slices.Clone(snapshot), 0), snapshot[:5]} {
		if err := s.Restore(bad); err == nil {
			t.Errorf("a damaged snapshot of %d bytes was accepted", len(bad))
		}
	}
	if s.StateHash() != before {
		t.Error("a refused snapshot changed the match")
	}
}

// appHarness drives the whole game through a runtime, as a host does.
type appHarness struct {
	t       *testing.T
	world   *outpostWorld
	app     *outpostApp
	runtime *engine.Runtime
}

func newAppHarness(t *testing.T) *appHarness {
	t.Helper()
	h := &appHarness{t: t, world: newOutpostWorld(t)}
	runtime, err := engine.Start(func() engine.Scene {
		h.app = newOutpostApp(h.world)
		return h.app
	})
	if err != nil {
		t.Fatal(err)
	}
	h.runtime = runtime
	t.Cleanup(func() {
		if err := runtime.Stop(); err != nil {
			t.Error(err)
		}
	})
	return h
}

func (h *appHarness) step(keys ...engine.Keys) {
	h.t.Helper()
	var pressed engine.Keys
	for _, k := range keys {
		pressed |= k
	}
	if err := h.runtime.Step(engine.Input{Pressed: pressed}); err != nil {
		h.t.Fatal(err)
	}
}

func (h *appHarness) run(ticks int) {
	h.t.Helper()
	for range ticks {
		h.step()
	}
}

func TestOutpostAppSavesLoadsAndReplays(t *testing.T) {
	h := newAppHarness(t)
	store := h.world.store.(memoryStore)
	if h.app.state != stateTitle {
		t.Fatalf("the game opened in state %d, want the title", h.app.state)
	}
	h.step(engine.KeyArrowDown) // Load, with nothing saved yet.
	h.step(engine.KeyEnter)
	if h.app.state != stateTitle || h.app.noticeLeft == 0 {
		t.Fatalf("loading with no save gave state %d, notice %q", h.app.state, h.app.notice)
	}
	h.step(engine.KeyArrowUp)
	h.step(engine.KeyEnter) // Start.
	if h.app.state != statePlaying {
		t.Fatalf("Start left state %d", h.app.state)
	}
	h.run(100)
	ticks := h.app.session.sim.Ticks()
	if ticks != 100 {
		t.Fatalf("the match ran %d ticks, want 100 (the Start tick does not step the match)", ticks)
	}

	// Pausing stops the match, and the pause menu saves.
	h.step(engine.KeyEscape)
	if h.app.state != statePaused {
		t.Fatalf("Escape left state %d", h.app.state)
	}
	h.run(30)
	if got := h.app.session.sim.Ticks(); got != ticks {
		t.Fatalf("the match advanced from %d to %d ticks while paused", ticks, got)
	}
	h.step(engine.KeyArrowDown)
	h.step(engine.KeyEnter) // Save.
	data, ok := store[outpostSaveKey]
	if !ok {
		t.Fatal("Save wrote nothing to the store")
	}
	meta, _, err := save.Decode(data)
	if err != nil || meta != h.world.meta {
		t.Fatalf("the save is %+v, %v; want a save for this game", meta, err)
	}
	h.step(engine.KeyEscape) // Resume.
	h.run(60)
	if h.app.session.sim.Ticks() <= ticks {
		t.Fatal("the match did not continue after resuming")
	}

	// Loading goes back to the saved tick, and quitting keeps the recording.
	h.step(engine.KeyEscape)
	h.step(engine.KeyArrowDown)
	h.step(engine.KeyArrowDown)
	h.step(engine.KeyEnter) // Load.
	if h.app.state != statePlaying || h.app.session.sim.Ticks() != ticks {
		t.Fatalf(
			"Load left state %d at tick %d, want playing at %d",
			h.app.state,
			h.app.session.sim.Ticks(),
			ticks,
		)
	}
	h.run(40)
	h.step(engine.KeyEscape)
	h.step(engine.KeyArrowUp) // Wraps to Quit to title.
	h.step(engine.KeyEnter)
	if h.app.state != stateTitle {
		t.Fatalf("Quit left state %d", h.app.state)
	}
	if _, ok := store[outpostReplayKey]; !ok {
		t.Fatal("quitting did not keep the recording")
	}

	// The kept recording starts from the loaded save and plays to its end.
	h.step(engine.KeyArrowDown)
	h.step(engine.KeyArrowDown)
	h.step(engine.KeyEnter) // Watch last replay.
	if h.app.state != stateWatching {
		t.Fatalf("Watch left state %d, notice %q", h.app.state, h.app.notice)
	}
	for !h.app.player.Done() {
		h.step()
	}
	if h.app.state != stateWatching || h.app.player.Played() != h.app.player.Ticks() {
		t.Fatalf("the replay ended in state %d after %d of %d ticks, notice %q",
			h.app.state, h.app.player.Played(), h.app.player.Ticks(), h.app.notice)
	}
	h.step(engine.KeyEscape)
	if h.app.state != stateTitle {
		t.Fatalf("Escape left the replay for state %d", h.app.state)
	}
}

func TestOutpostAppRefusesASaveFromOtherContent(t *testing.T) {
	h := newAppHarness(t)
	other := h.world.meta
	other.Content++
	data, err := save.Encode(other, []byte{outpostSnapshotTag})
	if err != nil {
		t.Fatal(err)
	}
	h.world.store.Put(outpostSaveKey, data)
	h.step(engine.KeyArrowDown)
	h.step(engine.KeyEnter)
	if h.app.state != stateTitle || h.app.noticeLeft == 0 {
		t.Fatalf("a foreign save gave state %d, notice %q", h.app.state, h.app.notice)
	}
}

func TestOutpostAppWithoutAStorePlaysWithoutSaving(t *testing.T) {
	h := newAppHarness(t)
	h.world.store = nil
	h.step(engine.KeyEnter)
	h.run(5)
	h.step(engine.KeyEscape)
	h.step(engine.KeyArrowDown)
	h.step(engine.KeyEnter) // Save.
	if h.app.state != statePaused || h.app.notice != errOutpostSaving.Error() {
		t.Fatalf("saving without a store gave state %d, notice %q", h.app.state, h.app.notice)
	}
}

// countingCanvas counts what a frame draws.
type countingCanvas struct{ images, rects int }

func (*countingCanvas) Clear(color.NRGBA)                                         {}
func (c *countingCanvas) Rect(_, _, _, _ float32, _ color.NRGBA)                  { c.rects++ }
func (*countingCanvas) Line(_, _, _, _, _ float32, _ color.NRGBA)                 {}
func (*countingCanvas) Image(image.Rectangle, float32, float32, float32, float32) {}

func (c *countingCanvas) ImageTinted(
	image.Rectangle,
	float32,
	float32,
	float32,
	float32,
	color.NRGBA,
) {
	c.images++
}

func TestOutpostDrawsWithoutAllocating(t *testing.T) {
	s := newOutpostSession(t)
	s.selectAll(t)
	canvas := &countingCanvas{}
	s.Draw(canvas) // Warm up any lazy state.
	if allocs := testing.AllocsPerRun(20, func() { s.Draw(canvas) }); allocs != 0 {
		t.Errorf("Draw allocated %v times per frame", allocs)
	}
	if canvas.images == 0 || canvas.rects == 0 {
		t.Errorf("the HUD drew %d glyphs and %d rectangles", canvas.images, canvas.rects)
	}
}

func BenchmarkOutpostDrawFullHUD(b *testing.B) {
	s := newOutpostSession(b)
	s.selectAll(b)
	canvas := &countingCanvas{}
	b.ReportAllocs()
	for b.Loop() {
		s.Draw(canvas)
	}
}

// BenchmarkOutpostReplayVerify measures verifying a recording of the scripted
// fight, which is a busy battle: the reported time covers the whole recording.
func BenchmarkOutpostReplayVerify(b *testing.B) {
	world := newOutpostWorld(b)
	inputs := outpostScript(b)
	factory := func() engine.Scene {
		return world.newSession(nil)
	}
	recorder, err := replay.NewRecorder(world.meta, nil, len(inputs))
	if err != nil {
		b.Fatal(err)
	}
	runOutpost(b, replay.Record(factory, recorder), inputs)
	recording := recorder.Finish()
	b.ReportAllocs()
	b.ReportMetric(float64(len(inputs)), "ticks/op")
	for b.Loop() {
		if err := replay.Verify(factory, recording, world.meta); err != nil {
			b.Fatal(err)
		}
	}
}

func TestOutpostFactoryOpensTheTitle(t *testing.T) {
	runtime, err := engine.Start(newOutpostWorld(t).factory())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := runtime.Stop(); err != nil {
			t.Error(err)
		}
	})
	if err := runtime.Draw(&countingCanvas{}); err != nil {
		t.Fatal(err)
	}
}
