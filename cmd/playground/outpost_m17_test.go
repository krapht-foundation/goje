package main

import (
	"bytes"
	"fmt"
	"io/fs"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/krapht-foundation/goje/engine"
	"github.com/krapht-foundation/goje/engine/lockstep"
	"github.com/krapht-foundation/goje/engine/matchlog"
	"github.com/krapht-foundation/goje/engine/replay"
	"github.com/krapht-foundation/goje/engine/save"
	"github.com/krapht-foundation/goje/examples/skirmish"
)

// duelLevel is a versus board that ends the same way every time: one scout and
// one guard in the open, in each other's sight but out of weapon range.
const duelLevel = `{
  "version": 3,
  "rows": [".......", ".......", "......."],
  "placements": [
    {"id": 1, "kind": "scout", "x": 0, "y": 1},
    {"id": 2, "kind": "guard", "x": 6, "y": 1}
  ],
  "zones": [],
  "sight": {"radius": 8}
}`

func duelFiles() fs.FS {
	return fstest.MapFS{"levels/versus.json": &fstest.MapFile{Data: []byte(duelLevel)}}
}

var versusSetup = matchSetup{mode: matchlog.Versus}

// versusPair is two whole games joined by an in-memory net in a versus match,
// started the way beginOnline starts one, without a relay connection. Each
// game has its own store. sessions keeps each match scene after the app stops it.
type versusPair struct {
	tb       testing.TB
	net      *lockstep.MemoryNet
	apps     [2]*outpostApp
	sessions [2]*outpostSession
}

func newVersusPair(tb testing.TB, root fs.FS) *versusPair {
	tb.Helper()
	p := &versusPair{tb: tb, net: lockstep.NewMemoryNet(2)}
	for i := range p.apps {
		world := newOutpostWorld(tb)
		var err error
		world.versusBoard, world.versusMeta, err = loadOutpostLevel(
			root,
			"levels/versus.json",
			versusContent,
		)
		if err != nil {
			tb.Fatal(err)
		}
		app := newOutpostApp(world)
		if err = app.Enter(); err != nil {
			tb.Fatal(err)
		}
		tb.Cleanup(app.Exit)
		header := world.matchHeader(versusSetup)
		lock, err := lockstep.New(lockstep.Config{
			Game: onlineGame, Content: header.Content, Players: header.Players, Local: i,
			Delay: onlineDelay, HashInterval: onlineHashInterval, StallLimit: onlineStallLimit,
		}, p.net.Link(i))
		if err != nil {
			tb.Fatal(err)
		}
		sync := newOnlineSync(lock, i)
		if sync.recorder, err = matchlog.New(header); err != nil {
			tb.Fatal(err)
		}
		match, err := engine.Start(func() engine.Scene {
			s := world.onlineSession(versusSetup, i)
			s.sync, s.issuer = sync, sync.issue
			app.session, p.sessions[i] = s, s
			return s
		})
		if err != nil {
			tb.Fatal(err)
		}
		app.match, app.osync, app.state = match, sync, stateOnline
		p.apps[i] = app
	}
	return p
}

func updateApp(tb testing.TB, a *outpostApp, in engine.Input) {
	tb.Helper()
	if _, err := a.Update(engine.Tick{Input: in}); err != nil {
		tb.Fatal(err)
	}
}

// update runs one frame of player i's game while its match is still on.
func (p *versusPair) update(i int, in engine.Input) {
	p.tb.Helper()
	if p.apps[i].state == stateOnline {
		updateApp(p.tb, p.apps[i], in)
	}
}

func (p *versusPair) ended() bool {
	return p.apps[0].state == stateOnlineEnded && p.apps[1].state == stateOnlineEnded
}

// runToEnd steps both games with no input until both show their end screen.
func (p *versusPair) runToEnd(limit int) {
	p.tb.Helper()
	for n := 0; !p.ended(); n++ {
		if n > limit {
			p.tb.Fatalf("the match did not end in %d frames: states %d and %d",
				limit, p.apps[0].state, p.apps[1].state)
		}
		p.update(0, engine.Input{})
		p.update(1, engine.Input{})
	}
}

// watchToEnd plays the log that the app is watching to its last tick, pressing
// Tab on frame tabAt, and returns the watched scene.
func watchToEnd(tb testing.TB, a *outpostApp, tabAt int) *outpostSession {
	tb.Helper()
	if a.state != stateMatchWatch {
		tb.Fatalf("the watch did not start: state %d, notice %q", a.state, a.notice)
	}
	for n := 0; !a.matchPlayer.done(); n++ {
		if n > a.matchPlayer.ticks {
			tb.Fatalf("the watch passed %d ticks without ending", a.matchPlayer.ticks)
		}
		in := engine.Input{}
		if n == tabAt {
			in.Pressed = engine.KeyTab
		}
		updateApp(tb, a, in)
		if a.state != stateMatchWatch {
			tb.Fatalf("the watch stopped at tick %d: %q", a.matchPlayer.ticks, a.notice)
		}
	}
	return a.session
}

// checkLog requires that the app kept and stored a log of the ticks its match
// ran, and that the log plays back to the match's final state.
func checkLog(tb testing.TB, a *outpostApp, played *outpostSession) matchlog.Log {
	tb.Helper()
	if a.lastMatch == nil {
		tb.Fatal("the match kept no log")
	}
	stored, err := a.world.store.Get(outpostMatchKey)
	if err != nil || !bytes.Equal(stored, a.lastMatch) {
		tb.Fatalf("the stored log differs from the kept one: %v", err)
	}
	log, err := matchlog.Decode(a.lastMatch)
	if err != nil {
		tb.Fatal(err)
	}
	if h := log.Header(); h != a.world.matchHeader(versusSetup) {
		tb.Errorf("the log's header is %+v, want the versus match's", h)
	}
	if log.Ticks() != int(played.sim.Ticks()) {
		tb.Errorf("the log has %d ticks, the match ran %d", log.Ticks(), played.sim.Ticks())
	}
	a.watchMatch()
	if watched := watchToEnd(tb, a, -1); watched.sim.Trace() != played.sim.Trace() {
		tb.Errorf(
			"the watched log ends at trace %d, the match at %d",
			watched.sim.Trace(),
			played.sim.Trace(),
		)
	}
	return log
}

func TestOnlineVersusTeamsSeeTheirOwnFogAndCommandTheirOwnUnits(t *testing.T) {
	p := newVersusPair(t, authoredFiles)
	s0, s1 := p.sessions[0], p.sessions[1]
	if s0.fogTeam != skirmish.Scouts || s1.fogTeam != skirmish.Guards {
		t.Fatalf(
			"the players view teams %d and %d, want the scouts and the guards",
			s0.fogTeam,
			s1.fogTeam,
		)
	}
	start := map[uint64]float64{}
	for _, u := range s0.sim.Units() {
		start[u.ID] = u.Center().X.Float64()
	}

	// Each player drags a box over the whole screen, then sends the selection to the middle.
	press, release := drag(0, 0, width-1, height-1, 0)
	inputs := func(s *outpostSession, n int) engine.Input {
		switch n {
		case 0:
			return press
		case 1:
			return release
		case 2:
			x, y := s.logical(5.5, 4.5, 0)
			return engine.Input{SecondaryPress: engine.MousePress{X: x, Y: y, Valid: true}}
		}
		return engine.Input{}
	}
	for n := 0; s0.sim.Ticks() < 40 || s1.sim.Ticks() != s0.sim.Ticks(); n++ {
		if n > 200 {
			t.Fatalf("the match reached only ticks %d and %d", s0.sim.Ticks(), s1.sim.Ticks())
		}
		p.update(0, inputs(s0, n))
		p.update(1, inputs(s1, n))
		if p.ended() {
			t.Fatalf("the match ended: %q", p.apps[0].endLines)
		}
	}
	if want := []uint64{1, 2, 3, 4, 5, 6}; !slices.Equal(s0.selection, want) {
		t.Errorf("the scouts' player selected %v, want %v", s0.selection, want)
	}
	if want := []uint64{11, 12, 13, 14, 15, 16}; !slices.Equal(s1.selection, want) {
		t.Errorf("the guards' player selected %v, want %v", s1.selection, want)
	}
	if s0.sim.Trace() != s1.sim.Trace() {
		t.Fatalf("the copies differ at tick %d", s0.sim.Ticks())
	}
	// Each player's order reached the other player's copy through the net.
	for _, view := range []*outpostSession{s0, s1} {
		for _, u := range view.sim.Units() {
			if u.Center().X.Float64() == start[u.ID] && !u.Moving() {
				t.Errorf("player %d's copy: unit %d never took its order", view.fogTeam-1, u.ID)
			}
		}
	}
	// Neither team has seen the other's half yet, and each player sees only their own fog.
	for _, tc := range []struct {
		s          *outpostSession
		own, enemy string
		home, far  float64
	}{{s0, "Scouts ", "Guards", 0.5, 10.5}, {s1, "Guards ", "Scouts", 10.5, 0.5}} {
		if tc.s.hud.friends != 6 || tc.s.hud.foes != 0 ||
			!strings.HasPrefix(tc.s.hud.counts.Text, tc.own) {
			t.Errorf(
				"the %sHUD shows %d friends, %d foes: %q",
				tc.own,
				tc.s.hud.friends,
				tc.s.hud.foes,
				tc.s.hud.counts.Text,
			)
		}
		if !tc.s.explored(tc.home, 4.5) || tc.s.explored(tc.far, 4.5) {
			t.Errorf("the %splayer's fog shows home %v, the %s' side %v",
				tc.own, tc.s.explored(tc.home, 4.5), tc.enemy, tc.s.explored(tc.far, 4.5))
		}
	}
}

func TestOnlineVersusForgedCommandEndsTheMatch(t *testing.T) {
	for _, tc := range []struct {
		sender int
		id     uint64
	}{{1, 1}, {0, 11}, {1, 99}} {
		p := newVersusPair(t, authoredFiles)
		forged := skirmish.Command{
			Kind: skirmish.CommandOrder, IDs: []uint64{tc.id},
			Order: skirmish.Order{Kind: skirmish.Move},
		}
		if err := p.apps[tc.sender].osync.lock.Submit(forged.AppendTo(nil)); err != nil {
			t.Fatal(err)
		}
		p.runToEnd(onlineDelay + 10)
		reason := fmt.Sprintf("player %d commanded unit %d", tc.sender, tc.id)
		rejected := false
		for i, app := range p.apps {
			switch {
			case len(app.endLines) == 2 && app.endLines[0] == "The other player sent invalid data." &&
				strings.Contains(app.endLines[1], reason):
				rejected = true
			case slices.Equal(app.endLines, describeOnlineEnd(lockstep.ErrPeerLeft)):
				// The first detector can close before the other side processes this tick.
			default:
				t.Errorf(
					"player %d sent unit %d; player %d's end screen is %q",
					tc.sender,
					tc.id,
					i,
					app.endLines,
				)
			}
			if p.sessions[i].sim.Ticks() > onlineDelay+1 {
				t.Errorf("player %d advanced past the forged tick: %d", i, p.sessions[i].sim.Ticks())
			}
		}
		if !rejected {
			t.Errorf("no player rejected the forged command: %s", reason)
		}
	}
}

func TestOnlineVersusResultWaitsForBothPlayersAndIsLogged(t *testing.T) {
	p := newVersusPair(t, duelFiles())
	p.net.SetLatency(2)
	s0, s1 := p.sessions[0], p.sessions[1]
	// The scout's player focuses the guard every half second; the guard's player holds fire.
	press, release := drag(0, 0, width-1, height-1, 0)
	script := func(i, n int) engine.Input {
		switch {
		case n == 0:
			return press
		case n == 1:
			return release
		case i == 0 && n%30 == 2:
			return engine.Input{Pressed: engine.KeyF}
		case i == 1 && n == 2:
			return engine.Input{Pressed: engine.KeyH}
		}
		return engine.Input{}
	}
	var frames [2]int
	waited := false
	for n := 0; !p.ended(); n++ {
		if n > 6000 {
			t.Fatalf("the duel did not end: ticks %d and %d, outcomes %d and %d",
				s0.sim.Ticks(), s1.sim.Ticks(), s0.outcome, s1.outcome)
		}
		// The guard's game runs two frames for every three of the scout's.
		for i := range p.apps {
			if (i == 0 || n%3 != 0) && p.apps[i].state == stateOnline {
				p.update(i, script(i, frames[i]))
				frames[i]++
			}
		}
		if s0.outcome != outcomePlaying && s1.sim.Ticks() < s0.sim.Ticks() {
			if p.apps[0].state != stateOnline {
				t.Fatalf("player 0 showed the result of tick %d while player 1 was at tick %d",
					s0.sim.Ticks(), s1.sim.Ticks())
			}
			waited = true
		}
	}
	if !waited {
		t.Error("player 0 never waited for player 1 to reach the final tick")
	}
	for i, want := range []string{"Match complete: victory", "Match complete: defeat"} {
		if !slices.Equal(p.apps[i].endLines, []string{want}) {
			t.Errorf("player %d's end screen is %q, want %q", i, p.apps[i].endLines, want)
		}
	}
	if s0.sim.Ticks() != s1.sim.Ticks() || s0.sim.Trace() != s1.sim.Trace() {
		t.Fatalf("the players finished at ticks %d and %d with traces %d and %d",
			s0.sim.Ticks(), s1.sim.Ticks(), s0.sim.Trace(), s1.sim.Trace())
	}

	// Both players logged the same ticks. A fresh game finds the log in its store
	// and plays it back; Tab shows the guards' fog without changing the match.
	if !bytes.Equal(p.apps[0].lastMatch, p.apps[1].lastMatch) {
		t.Error("the two players kept different logs of the same match")
	}
	viewer := newOutpostApp(p.apps[0].world)
	if err := viewer.Enter(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(viewer.Exit)
	viewer.watchMatch()
	watched := watchToEnd(t, viewer, int(s0.sim.Ticks())/2)
	if watched.sim.Trace() != s0.sim.Trace() {
		t.Errorf(
			"the watched log ends at trace %d, the match at %d",
			watched.sim.Trace(),
			s0.sim.Trace(),
		)
	}
	if watched.fogTeam != skirmish.Guards || watched.outcome != outcomeDefeat {
		t.Errorf("after Tab the watch shows team %d with outcome %d, want the guards' defeat",
			watched.fogTeam, watched.outcome)
	}
	for i, app := range p.apps {
		checkLog(t, app, p.sessions[i])
	}
}

func TestOnlineVersusLogsAMatchThatAPlayerLeft(t *testing.T) {
	p := newVersusPair(t, authoredFiles)
	for range 90 {
		p.update(0, engine.Input{})
		p.update(1, engine.Input{})
	}
	// Player 0 opens the menu and picks the last entry, Leave the match.
	for _, key := range []engine.Keys{engine.KeyEscape, engine.KeyArrowUp, engine.KeyEnter} {
		p.update(0, engine.Input{Pressed: key})
		p.update(1, engine.Input{})
	}
	if p.apps[0].state != stateTitle {
		t.Fatalf("leaving went to state %d, want the title", p.apps[0].state)
	}
	for n := 0; p.apps[1].state == stateOnline; n++ {
		if n > onlineStallLimit {
			t.Fatal("the other player's match did not end")
		}
		p.update(1, engine.Input{})
	}
	if want := describeOnlineEnd(lockstep.ErrPeerLeft); !slices.Equal(p.apps[1].endLines, want) {
		t.Errorf("the remaining player's end screen is %q, want %q", p.apps[1].endLines, want)
	}
	for i, app := range p.apps {
		if log := checkLog(t, app, p.sessions[i]); log.Ticks() < 90 {
			t.Errorf("player %d logged %d ticks of a match that ran at least 90", i, log.Ticks())
		}
	}
}

func TestOnlineVersusLogsADesyncAndCatchesACorruptedLog(t *testing.T) {
	p := newVersusPair(t, authoredFiles)
	p.apps[1].osync.fault = func(tick, hash uint64) uint64 {
		if tick >= 100 {
			return hash ^ 1
		}
		return hash
	}
	p.runToEnd(400)
	desync := describeOnlineEnd(&lockstep.DesyncError{})[0]
	for i, app := range p.apps {
		if len(app.endLines) == 0 || app.endLines[0] != desync {
			t.Errorf("player %d's end screen is %q, want a desync", i, app.endLines)
		}
	}
	// The logs keep the true hashes, so both play back up to the divergence.
	var log matchlog.Log
	for i, app := range p.apps {
		log = checkLog(t, app, p.sessions[i])
		if log.Ticks() < 120 {
			t.Errorf("player %d logged %d ticks, want the desync at tick 120", i, log.Ticks())
		}
	}

	// A changed first checkpoint stops the watch at tick 60 with a divergence.
	a := p.apps[1]
	corrupt := slices.Clone(a.lastMatch)
	corrupt[len(corrupt)-8*((log.Ticks()+matchlog.Interval-1)/matchlog.Interval)] ^= 1
	if err := a.startMatchWatch(corrupt); err != nil {
		t.Fatal(err)
	}
	for n := 0; a.state == stateMatchWatch; n++ {
		if n > log.Ticks() {
			t.Fatal("a corrupted log played to its end")
		}
		updateApp(t, a, engine.Input{})
	}
	want, _, _ := strings.Cut((&replay.DivergenceError{Tick: matchlog.Interval}).Error(), ":")
	if a.state != stateTitle || !strings.HasPrefix(a.notice, want) {
		t.Errorf(
			"the corrupted log ended in state %d with %q, want the title and %q",
			a.state,
			a.notice,
			want,
		)
	}
}

func TestOutpostTacticalMatchSavesLoadsAndReplays(t *testing.T) {
	h := newAppHarness(t)
	tactical := h.world.soloMeta(true)
	h.step(engine.KeyArrowDown)
	h.step(engine.KeyArrowDown)
	h.step(engine.KeyArrowDown)
	h.step(engine.KeyEnter) // Guards: Tactical.
	if !h.app.tactics || h.app.menu.Items[titleGuards] != "Guards: Tactical" {
		t.Fatalf(
			"the Guards entry left tactics %v, label %q",
			h.app.tactics,
			h.app.menu.Items[titleGuards],
		)
	}
	h.step(engine.KeyArrowUp)
	h.step(engine.KeyArrowUp)
	h.step(engine.KeyArrowUp)
	h.step(engine.KeyEnter) // Start.
	if h.app.state != statePlaying || !h.app.session.tactics {
		t.Fatalf(
			"Start left state %d, tactics %v",
			h.app.state,
			h.app.session != nil && h.app.session.tactics,
		)
	}
	h.run(100)
	ticks := h.app.session.sim.Ticks()

	h.step(engine.KeyEscape)
	h.step(engine.KeyArrowDown)
	h.step(engine.KeyEnter) // Save.
	data, err := h.world.store.Get(outpostSaveKey)
	if err != nil {
		t.Fatal(err)
	}
	meta, _, err := save.Decode(data)
	if err != nil || meta != tactical || meta == h.world.meta {
		t.Fatalf("the save is %+v, %v; want a tactical save", meta, err)
	}
	h.step(engine.KeyEscape) // Resume.
	h.run(50)

	h.step(engine.KeyEscape)
	h.step(engine.KeyArrowDown)
	h.step(engine.KeyArrowDown)
	h.step(engine.KeyEnter) // Load.
	if h.app.state != statePlaying || !h.app.session.tactics || h.app.session.sim.Ticks() != ticks {
		t.Fatalf("Load left state %d, tactics %v, tick %d; want a tactical match at %d",
			h.app.state, h.app.session.tactics, h.app.session.sim.Ticks(), ticks)
	}
	h.run(40)
	final := h.app.session.sim.Trace()
	h.step(engine.KeyEscape)
	h.step(engine.KeyArrowUp)
	h.step(engine.KeyEnter) // Quit to title.
	data, err = h.world.store.Get(outpostReplayKey)
	if err != nil {
		t.Fatal(err)
	}
	if recording, err := replay.Decode(data); err != nil || recording.Meta != tactical {
		t.Fatalf("the replay is %v; want a tactical recording", err)
	}

	h.step(engine.KeyArrowDown)
	h.step(engine.KeyArrowDown)
	h.step(engine.KeyEnter) // Watch last replay.
	if h.app.state != stateWatching || !h.app.session.tactics {
		t.Fatalf("Watch left state %d, notice %q", h.app.state, h.app.notice)
	}
	for !h.app.player.Done() {
		h.step()
	}
	if h.app.state != stateWatching || h.app.session.sim.Trace() != final {
		t.Fatalf("the replay ended in state %d at trace %d, want %d; notice %q",
			h.app.state, h.app.session.sim.Trace(), final, h.app.notice)
	}
}

type outpostMusicLog struct {
	voiceLog
	starts, stops int
	gain          float32
}

func (m *outpostMusicLog) PlayMusic(_ int, gain float32, _ int) {
	m.starts++
	m.gain = gain
}

func (m *outpostMusicLog) SetMusicGain(gain float32, _ int) { m.gain = gain }
func (m *outpostMusicLog) StopMusic(_ int)                  { m.stops++ }

func TestOutpostMusicContinuesDucksAndStops(t *testing.T) {
	h := newAppHarness(t)
	music := &outpostMusicLog{}
	h.runtime.SetSoundOutput(music)
	h.step()
	titleGain := music.gain
	h.step(engine.KeyEnter)
	if music.starts != 1 || titleGain <= 0 || music.gain != titleGain {
		t.Fatal("starting a match did not continue the title music")
	}
	h.step(engine.KeyEscape)
	ducked := music.gain
	if ducked >= titleGain || ducked <= 0 {
		t.Fatalf("the pause menu did not duck the music: %v, title %v", ducked, titleGain)
	}
	h.step(engine.KeyArrowUp)
	h.step(engine.KeyArrowUp)
	h.step(engine.KeyEnter) // Sound: half.
	if music.gain != ducked/2 {
		t.Fatalf("Sound changed the cue volume but left music at %v, want %v", music.gain, ducked/2)
	}
	h.step(engine.KeyEscape)
	if music.starts != 1 || music.gain != titleGain/2 || music.stops != 0 {
		t.Fatal("resuming did not restore the loop to the chosen volume")
	}
	for _, in := range outpostScript(t) {
		if err := h.runtime.Step(in); err != nil {
			t.Fatal(err)
		}
	}
	if h.app.session.outcome != outcomeVictory || music.stops != 1 {
		t.Fatal("winning the match did not stop the loop")
	}
}
