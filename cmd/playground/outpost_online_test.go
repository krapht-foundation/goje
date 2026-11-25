package main

import (
	"cmp"
	"errors"
	"testing"

	"github.com/krapht-foundation/goje/engine"
	"github.com/krapht-foundation/goje/engine/lockstep"
	"github.com/krapht-foundation/goje/examples/skirmish"
)

// onlinePair is two players' copies of one match joined by an in-memory net.
type onlinePair struct {
	tb       testing.TB
	net      *lockstep.MemoryNet
	sessions [2]*outpostSession
	syncs    [2]*onlineSync
}

func newOnlinePair(tb testing.TB) *onlinePair {
	tb.Helper()
	world := newOutpostWorld(tb)
	p := &onlinePair{tb: tb, net: lockstep.NewMemoryNet(2)}
	for i := range p.sessions {
		lock, err := lockstep.New(lockstep.Config{
			Game: onlineGame, Content: world.meta.Content, Players: 2, Local: i,
			Delay: onlineDelay, HashInterval: onlineHashInterval, StallLimit: onlineStallLimit,
		}, p.net.Link(i))
		if err != nil {
			tb.Fatal(err)
		}
		sync := newOnlineSync(lock, i)
		s := world.newSession(nil)
		s.sync, s.issuer = sync, sync.issue
		s.owns = func(id uint64) bool { return onlineOwner(id) == i }
		if err := s.Enter(); err != nil {
			tb.Fatal(err)
		}
		p.sessions[i], p.syncs[i] = s, sync
	}
	return p
}

// step runs one tick of both players, each with its own input, and returns the errors.
func (p *onlinePair) step(in0, in1 engine.Input) (err0, err1 error) {
	_, err0 = p.sessions[0].Update(engine.Tick{Input: in0})
	_, err1 = p.sessions[1].Update(engine.Tick{Input: in1})
	return err0, err1
}

func (p *onlinePair) mustStep(in0, in1 engine.Input) {
	p.tb.Helper()
	if err0, err1 := p.step(in0, in1); err0 != nil || err1 != nil {
		p.tb.Fatalf("step failed: %v, %v", err0, err1)
	}
}

// script is what a player does: select everything they own, send it to the middle,
// and then, every second, focus the nearest guard in sight.
func (p *onlinePair) script(i int) []engine.Input {
	press, release := drag(0, 0, width-1, height-1, 0)
	gx, gy := p.sessions[i].logical(3.5, 4.5, 0)
	inputs := make([]engine.Input, 0, 903)
	inputs = append(inputs, press, release,
		engine.Input{SecondaryPress: engine.MousePress{X: gx, Y: gy, Valid: true}})
	for n := range 900 {
		in := engine.Input{}
		if n > 60 && n%60 == 0 {
			in.Pressed = engine.KeyF
		}
		inputs = append(inputs, in)
	}
	return inputs
}

// checkOwnSelection requires that each player has selected all of their scouts and
// none of the other player's.
func (p *onlinePair) checkOwnSelection(tb testing.TB) {
	tb.Helper()
	for i, s := range p.sessions {
		if len(s.selection) != 3 {
			tb.Errorf("player %d selected %v, want their three scouts", i, s.selection)
		}
		for _, id := range s.selection {
			if onlineOwner(id) != i {
				tb.Errorf("player %d selected scout %d, which is the other player's", i, id)
			}
		}
	}
}

func TestOnlineCoopPlayersStayInSyncAndOwnTheirScouts(t *testing.T) {
	for _, latency := range []int{0, 3} {
		p := newOnlinePair(t)
		p.net.SetLatency(latency)
		a, b := p.script(0), p.script(1)
		steps := 0
		for i := range a {
			p.mustStep(a[i], b[i])
			if i == 10 {
				p.checkOwnSelection(t)
			}
			if p.sessions[0].sim.Ticks() != p.sessions[1].sim.Ticks() {
				continue // One player's tick waited for the other's commands.
			}
			if t0, t1 := p.sessions[0].sim.Trace(), p.sessions[1].sim.Trace(); t0 != t1 {
				t.Fatalf("latency %d: traces differ at tick %d: %d and %d",
					latency, p.sessions[0].sim.Ticks(), t0, t1)
			}
			steps++
		}
		if steps < len(a)/2 {
			t.Errorf("latency %d: the players agreed on only %d of %d ticks", latency, steps, len(a))
		}
		if p.sessions[0].sim.Ticks() < 400 {
			t.Errorf("latency %d: the match reached only tick %d", latency, p.sessions[0].sim.Ticks())
		}
	}
}

func TestOnlineMatchWaitsForAPlayerWhoStops(t *testing.T) {
	p := newOnlinePair(t)
	for range 30 {
		p.mustStep(engine.Input{}, engine.Input{})
	}
	p.net.Cut(1)
	ticks := p.sessions[0].sim.Ticks()
	var err error
	for i := 0; i < onlineStallLimit+60 && err == nil; i++ {
		_, err = p.sessions[0].Update(engine.Tick{})
		_, _ = p.sessions[1].Update(engine.Tick{})
	}
	if _, ok := errors.AsType[*onlineEnd](err); !ok || !errors.Is(err, lockstep.ErrStalled) {
		t.Fatalf("a player who stopped ended the match with %v, want a stall", err)
	}
	if p.sessions[0].sim.Ticks() > ticks+onlineDelay+1 {
		t.Errorf(
			"the match ran from tick %d to %d without the other player",
			ticks,
			p.sessions[0].sim.Ticks(),
		)
	}
	if !p.syncs[0].waiting() {
		t.Error("the match was not waiting")
	}
}

func TestOnlineRejectsCommandsForUnitsTheSenderDoesNotOwn(t *testing.T) {
	for name, id := range map[string]uint64{"the other player's scout": 1, "a guard": 12, "no unit": 99} {
		p := newOnlinePair(t)
		forged := skirmish.Command{
			Kind: skirmish.CommandOrder, IDs: []uint64{id},
			Order: skirmish.Order{Kind: skirmish.Move},
		}
		// Player 1 sends what only player 0 could send, or a unit that is not a scout.
		if err := p.syncs[1].lock.Submit(forged.AppendTo(nil)); err != nil {
			t.Fatal(err)
		}
		var err0 error
		for range onlineDelay + 4 {
			if err0, _ = p.step(engine.Input{}, engine.Input{}); err0 != nil {
				break
			}
		}
		if _, ok := errors.AsType[*onlineEnd](err0); !ok {
			t.Errorf("%s: a forged command ended the match with %v, want an online end", name, err0)
		}
	}
}

func TestOnlineDivergenceStopsBothPlayers(t *testing.T) {
	p := newOnlinePair(t)
	p.syncs[1].fault = func(tick, hash uint64) uint64 {
		if tick >= 100 {
			return hash ^ 1
		}
		return hash
	}
	var err0, err1 error
	for i := 0; i < 400 && err0 == nil && err1 == nil; i++ {
		err0, err1 = p.step(engine.Input{}, engine.Input{})
	}
	// The player who compared first stops a tick before the other one hears.
	for range 2 {
		if err0 == nil || err1 == nil {
			e0, e1 := p.step(engine.Input{}, engine.Input{})
			err0, err1 = cmp.Or(err0, e0), cmp.Or(err1, e1)
		}
	}
	for i, err := range []error{err0, err1} {
		desync, ok := errors.AsType[*lockstep.DesyncError](err)
		if !ok {
			t.Fatalf("player %d: err = %v, want a desync", i, err)
		}
		if desync.Tick != 120 {
			t.Errorf(
				"player %d: desync at tick %d, want the first checkpoint after the change, 120",
				i,
				desync.Tick,
			)
		}
	}
}

func TestCleanOnlineConfigKeepsOnlyUsableValues(t *testing.T) {
	for _, tc := range []struct{ in, want onlineConfig }{
		{onlineConfig{"ws://127.0.0.1:8080", "ABC234"}, onlineConfig{"ws://127.0.0.1:8080", "ABC234"}},
		{onlineConfig{"ws://host/", "ABC234"}, onlineConfig{"ws://host", "ABC234"}},
		{onlineConfig{"wss://relay.example", "ABC23"}, onlineConfig{"wss://relay.example", ""}},
		{onlineConfig{"wss://relay.example", "abc234"}, onlineConfig{"wss://relay.example", ""}},
		{onlineConfig{"http://host", "ABC234"}, onlineConfig{}},
		{onlineConfig{"ws://host/path", "ABC234"}, onlineConfig{}},
		{onlineConfig{"", "ABC234"}, onlineConfig{}},
	} {
		if got := cleanOnlineConfig(tc.in); got != tc.want {
			t.Errorf("cleanOnlineConfig(%+v) = %+v, want %+v", tc.in, got, tc.want)
		}
	}
}
