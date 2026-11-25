package lockstep

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"testing"
)

func testConfig(players, local, delay int) Config {
	return Config{
		Game: "test", Content: 0xfeed, Players: players, Local: local,
		Delay: delay, HashInterval: 7, StallLimit: 100000,
	}
}

func sessions(tb testing.TB, net *MemoryNet, cfg Config) []*Session {
	tb.Helper()
	ss := make([]*Session, cfg.Players)
	for p := range ss {
		cfg.Local = p
		s, err := New(cfg, net.Link(p))
		if err != nil {
			tb.Fatalf("New(%d): %v", p, err)
		}
		ss[p] = s
	}
	return ss
}

// mix folds a payload into a running state, standing in for a deterministic
// simulation step.
func mix(state uint64, b []byte) uint64 {
	for _, c := range b {
		state = (state ^ uint64(c)) * 0x100000001b3
	}
	return state*0x9e3779b97f4a7c15 + 1
}

// stamp is the payload a test player submits during a tick.
func stamp(p int, tick uint64) []byte {
	return binary.LittleEndian.AppendUint64([]byte{byte(p)}, tick)
}

// play runs every session until it reaches the tick limit. Each player
// submits a stamp after each Done, and every applied payload is checked
// against the schedule: what was submitted during tick t is applied at tick
// t+Delay+1. flip, when set, changes a player's state after a tick. It returns
// each player's first error and the states.
func play(
	tb testing.TB,
	ss []*Session,
	limit uint64,
	flip func(p int, tick uint64) uint64,
) (errs []error, states []uint64) {
	tb.Helper()
	errs = make([]error, len(ss))
	states = make([]uint64, len(ss))
	for range 100000 {
		busy := false
		for p, s := range ss {
			if errs[p] != nil || s.Tick() >= limit {
				continue
			}
			busy = true
			if errs[p] = s.Poll(); errs[p] != nil {
				continue
			}
			payloads, ok := s.Ready()
			if !ok {
				continue
			}
			t := s.Tick()
			delay := uint64(s.cfg.Delay)
			for q, b := range payloads {
				want := []byte(nil)
				if t > delay+1 { // nothing is submitted during tick 0
					want = stamp(q, t-delay-1)
				}
				if !bytes.Equal(b, want) {
					tb.Fatalf("player %d tick %d: payload of %d = %v, want %v", p, t, q, b, want)
				}
				states[p] = mix(states[p], b)
			}
			if flip != nil {
				states[p] ^= flip(p, t)
			}
			if errs[p] = s.Done(states[p]); errs[p] != nil {
				continue
			}
			if err := s.Submit(stamp(p, s.Tick())); err != nil {
				tb.Fatal(err)
			}
		}
		if !busy {
			return errs, states
		}
	}
	tb.Fatal("the match did not finish")
	return nil, nil
}

func TestPlayersDelaysAndFaults(t *testing.T) {
	faults := []struct {
		name           string
		latency, burst int
	}{{"reliable", 0, 0}, {"latency", 5, 0}, {"bursts", 0, 4}, {"both", 3, 3}}
	for players := 2; players <= MaxPlayers; players++ {
		for _, delay := range []int{0, 1, 6, 20, 30} {
			for _, f := range faults {
				t.Run(fmt.Sprintf("%dp/delay%d/%s", players, delay, f.name), func(t *testing.T) {
					net := NewMemoryNet(players)
					net.SetLatency(f.latency)
					net.SetBurst(f.burst)
					ss := sessions(t, net, testConfig(players, 0, delay))
					errs, states := play(t, ss, 150, nil)
					for p, err := range errs {
						if err != nil {
							t.Fatalf("player %d: %v", p, err)
						}
						if states[p] != states[0] {
							t.Fatalf("player %d state %x, player 0 state %x", p, states[p], states[0])
						}
					}
				})
			}
		}
	}
}

func TestDivergenceIsReportedAtTheNextCheckpointOnEverySide(t *testing.T) {
	for _, players := range []int{2, 3} {
		net := NewMemoryNet(players)
		net.SetLatency(2)
		cfg := testConfig(players, 0, 6)
		cfg.HashInterval = 10
		ss := sessions(t, net, cfg)
		// Player 1 flips one bit of its state while applying tick 13, so the
		// state after tick 13, hashed at checkpoint 20, differs.
		errs, _ := play(t, ss, 100, func(p int, tick uint64) uint64 {
			if p == 1 && tick == 13 {
				return 1 << 17
			}
			return 0
		})
		for p, err := range errs {
			d, ok := errors.AsType[*DesyncError](err)
			if !ok {
				t.Fatalf("%d players: player %d: %v, want a DesyncError", players, p, err)
			}
			if d.Tick != 20 || d.Local == d.Remote {
				t.Fatalf("%d players: player %d: %+v", players, p, d)
			}
			if want := 1; p != 1 && d.Player != want {
				t.Fatalf("player %d blames player %d, want %d", p, d.Player, want)
			}
			if p == 1 && d.Player == 1 {
				t.Fatalf("player 1 blames itself")
			}
		}
	}
}

func TestStoppedPlayerStallsTheMatch(t *testing.T) {
	net := NewMemoryNet(2)
	cfg := testConfig(2, 0, 3)
	cfg.StallLimit = 5
	ss := sessions(t, net, cfg)
	if errs, _ := play(t, ss, 20, nil); errs[0] != nil || errs[1] != nil {
		t.Fatal(errs)
	}
	net.Cut(1)
	// Player 1 already sent its frames up to tick 23, so player 0 runs on
	// to tick 24 and then waits.
	s := ss[0]
	for s.Tick() < 24 {
		if err := s.Poll(); err != nil {
			t.Fatal(err)
		}
		if _, ok := s.Ready(); !ok {
			t.Fatalf("not ready at tick %d", s.Tick())
		}
		if err := s.Done(0); err != nil {
			t.Fatal(err)
		}
	}
	for i := range cfg.StallLimit {
		if err := s.Poll(); err != nil {
			t.Fatalf("Poll after %d stalls: %v", i, err)
		}
		if _, ok := s.Ready(); ok {
			t.Fatal("ready without player 1's frame")
		}
	}
	if err := s.Poll(); !errors.Is(err, ErrStalled) {
		t.Fatalf("Poll = %v, want ErrStalled", err)
	}
	if err := s.Done(0); !errors.Is(err, ErrStalled) {
		t.Fatalf("Done = %v, want ErrStalled", err)
	}
}

func TestByeEndsTheMatchForTheOthers(t *testing.T) {
	net := NewMemoryNet(3)
	ss := sessions(t, net, testConfig(3, 0, 2))
	play(t, ss, 10, nil)
	if err := ss[2].Leave(); err != nil {
		t.Fatal(err)
	}
	for _, s := range ss[:2] {
		if err := s.Poll(); !errors.Is(err, ErrPeerLeft) {
			t.Fatalf("Poll = %v, want ErrPeerLeft", err)
		}
		if _, ok := s.Ready(); ok {
			t.Fatal("ready after the session ended")
		}
	}
}

func TestSubmitRejectsAnOversizedPayload(t *testing.T) {
	s, err := New(testConfig(2, 0, 0), NewMemoryNet(2).Link(0))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Submit(make([]byte, MaxPayload-1)); err != nil {
		t.Fatal(err)
	}
	if err := s.Submit([]byte{1, 2}); !errors.Is(err, ErrPayloadTooLarge) {
		t.Fatalf("Submit = %v, want ErrPayloadTooLarge", err)
	}
	if err := s.Submit([]byte{1}); err != nil || len(s.pending) != MaxPayload {
		t.Fatalf("Submit = %v with %d pending bytes", err, len(s.pending))
	}
}

func TestNewValidatesTheConfig(t *testing.T) {
	bad := []func(*Config){
		func(c *Config) { c.Players = 1 },
		func(c *Config) { c.Players = MaxPlayers + 1 },
		func(c *Config) { c.Local = 2 },
		func(c *Config) { c.Local = -1 },
		func(c *Config) { c.Delay = -1 },
		func(c *Config) { c.Delay = MaxDelay + 1 },
		func(c *Config) { c.HashInterval = 0 },
		func(c *Config) { c.StallLimit = 0 },
		func(c *Config) { c.Game = "" },
		func(c *Config) { c.Game = string(make([]byte, 33)) },
	}
	for i, change := range bad {
		cfg := testConfig(2, 0, 1)
		change(&cfg)
		if _, err := New(cfg, NewMemoryNet(2).Link(0)); err == nil {
			t.Fatalf("case %d: New accepted %+v", i, cfg)
		}
	}
}

// scriptLink delivers scripted messages and records what is sent.
type scriptLink struct {
	in   [][]byte
	sent int
}

func (l *scriptLink) Send([]byte) error { l.sent++; return nil }

func (l *scriptLink) Recv() (msg []byte, ok bool, err error) {
	if len(l.in) == 0 {
		return nil, false, nil
	}
	m := l.in[0]
	l.in = l.in[1:]
	return m, true, nil
}

func helloMsg(version byte, game string, content uint64, players, player byte) []byte {
	m := append([]byte{msgHello, version, byte(len(game))}, game...)
	m = binary.LittleEndian.AppendUint64(m, content)
	return append(m, players, player)
}

func frameMsg(p byte, tick uint64, payload []byte) []byte {
	m := binary.LittleEndian.AppendUint64([]byte{msgFrame, p}, tick)
	m = binary.LittleEndian.AppendUint16(m, uint16(len(payload)))
	return append(m, payload...)
}

func hashMsg(p byte, tick, hash uint64) []byte {
	m := binary.LittleEndian.AppendUint64([]byte{msgHash, p}, tick)
	return binary.LittleEndian.AppendUint64(m, hash)
}

func TestProtocolViolations(t *testing.T) {
	good := helloMsg(Version, "test", 0xfeed, 2, 1)
	cases := []struct {
		name     string
		interval int
		msgs     [][]byte
	}{
		{"empty message", 1, [][]byte{{}}},
		{"short message", 1, [][]byte{{msgFrame}}},
		{"oversized message", 1, [][]byte{good, frameMsg(1, 2, make([]byte, MaxPayload+1))}},
		{"unknown type", 1, [][]byte{good, {9, 1}}},
		{"other version", 1, [][]byte{helloMsg(Version+1, "test", 0xfeed, 2, 1)}},
		{"other game", 1, [][]byte{helloMsg(Version, "tess", 0xfeed, 2, 1)}},
		{"other content", 1, [][]byte{helloMsg(Version, "test", 0xfeee, 2, 1)}},
		{"other players", 1, [][]byte{helloMsg(Version, "test", 0xfeed, 3, 1)}},
		{"player out of range", 1, [][]byte{helloMsg(Version, "test", 0xfeed, 2, 2)}},
		{"local player index", 1, [][]byte{helloMsg(Version, "test", 0xfeed, 2, 0)}},
		{"second hello", 1, [][]byte{good, good}},
		{"truncated hello", 1, [][]byte{good[:len(good)-1]}},
		{"hello without a name", 1, [][]byte{{msgHello, Version}}},
		{"frame before hello", 1, [][]byte{frameMsg(1, 2, nil)}},
		{"frame naming the local player", 1, [][]byte{good, frameMsg(0, 2, nil)}},
		{"frame out of range player", 1, [][]byte{good, frameMsg(7, 2, nil)}},
		{"frame for a later tick", 1, [][]byte{good, frameMsg(1, 3, nil)}},
		{"frame repeated", 1, [][]byte{good, frameMsg(1, 2, nil), frameMsg(1, 2, nil)}},
		{"frame before its hash", 1, [][]byte{good, frameMsg(1, 2, nil), frameMsg(1, 3, nil)}},
		{"frame overflowing the ring", 100, [][]byte{
			good, frameMsg(1, 2, nil), frameMsg(1, 3, nil), frameMsg(1, 4, nil),
			frameMsg(1, 5, nil), frameMsg(1, 6, nil),
		}},
		{"frame length too long", 1, [][]byte{good, frameMsg(1, 2, []byte{1, 2, 3})[:14]}},
		{"frame header cut", 1, [][]byte{good, frameMsg(1, 2, nil)[:11]}},
		{"hash for a skipped checkpoint", 1, [][]byte{good, hashMsg(1, 2, 0)}},
		{"hash too far ahead", 1, [][]byte{
			good, hashMsg(1, 1, 0), hashMsg(1, 2, 0), hashMsg(1, 3, 0), hashMsg(1, 4, 0),
		}},
		{"hash cut", 1, [][]byte{good, hashMsg(1, 1, 0)[:17]}},
		{"hash before hello", 1, [][]byte{hashMsg(1, 1, 0)}},
		{"hash naming the local player", 1, [][]byte{good, hashMsg(0, 1, 0)}},
		{"long bye", 1, [][]byte{good, {msgBye, 1, 0}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := testConfig(2, 0, 2)
			cfg.HashInterval = c.interval
			s, err := New(cfg, &scriptLink{in: c.msgs})
			if err != nil {
				t.Fatal(err)
			}
			err = s.Poll()
			if _, ok := errors.AsType[*ProtocolError](err); !ok {
				t.Fatalf("Poll = %v, want a ProtocolError", err)
			}
			if again := s.Poll(); !errors.Is(again, err) {
				t.Fatalf("second Poll = %v, want the same error", again)
			}
		})
	}
}

func TestFramesBeforeTheLocalAgreementAreKept(t *testing.T) {
	// Player 1 sees every hello, starts, and sends frames for ticks 2 and 3
	// before player 2's Hello reaches player 0.
	link := &scriptLink{in: [][]byte{
		helloMsg(Version, "test", 0xfeed, 3, 1),
		frameMsg(1, 2, []byte{7}),
		hashMsg(1, 1, 0),
		frameMsg(1, 3, []byte{8}),
	}}
	cfg := testConfig(3, 0, 2)
	cfg.HashInterval = 1
	s, err := New(cfg, link)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Poll(); err != nil || s.Running() || link.sent != 1 {
		t.Fatalf("Poll = %v, running %v, %d sent", err, s.Running(), link.sent)
	}
	link.in = append(link.in, helloMsg(Version, "test", 0xfeed, 3, 2))
	if err := s.Poll(); err != nil || !s.Running() || link.sent != 2 {
		t.Fatalf("Poll = %v, running %v, %d sent", err, s.Running(), link.sent)
	}
	if p, ok := s.Ready(); !ok || p[1] != nil {
		t.Fatalf("Ready = %v, %v", p, ok)
	}
}

func TestDoneBeforeReadyFails(t *testing.T) {
	s, err := New(testConfig(2, 1, 1), NewMemoryNet(2).Link(1))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Done(0); err == nil {
		t.Fatal("Done before the match started succeeded")
	}
}

// tick runs one tick of every session and fails if one is not ready.
func tick(tb testing.TB, ss []*Session, payload []byte) {
	tb.Helper()
	for _, s := range ss {
		if err := s.Poll(); err != nil {
			tb.Fatal(err)
		}
		if _, ok := s.Ready(); !ok {
			tb.Fatalf("not ready at tick %d", s.Tick())
		}
		if err := s.Submit(payload); err != nil {
			tb.Fatal(err)
		}
		if err := s.Done(s.Tick()); err != nil {
			tb.Fatal(err)
		}
	}
}

func TestSteadyTicksDoNotAllocate(t *testing.T) {
	ss := sessions(t, NewMemoryNet(2), testConfig(2, 0, 6))
	for _, s := range ss {
		if err := s.Poll(); err != nil {
			t.Fatal(err)
		}
	}
	payload := make([]byte, 40)
	for range 100 {
		tick(t, ss, payload)
	}
	if allocs := testing.AllocsPerRun(200, func() { tick(t, ss, payload) }); allocs != 0 {
		t.Fatalf("a tick allocates %v times", allocs)
	}
}

// BenchmarkTwoPlayerTick measures one tick of both players of a match with a
// 40-byte payload each, through the in-memory link.
func BenchmarkTwoPlayerTick(b *testing.B) {
	ss := sessions(b, NewMemoryNet(2), testConfig(2, 0, 6))
	for _, s := range ss {
		if err := s.Poll(); err != nil {
			b.Fatal(err)
		}
	}
	payload := make([]byte, 40)
	b.ReportAllocs()
	for b.Loop() {
		tick(b, ss, payload)
	}
}

func FuzzSessionMessages(f *testing.F) {
	hello := helloMsg(Version, "test", 0xfeed, 2, 1)
	seed := func(msgs ...[]byte) []byte {
		var b []byte
		for _, m := range msgs {
			b = binary.LittleEndian.AppendUint16(b, uint16(len(m)))
			b = append(b, m...)
		}
		return b
	}
	f.Add(seed(hello, frameMsg(1, 2, []byte{1}), hashMsg(1, 1, 5), frameMsg(1, 3, nil)))
	f.Add(seed(hello, hashMsg(1, 1, 0), hashMsg(1, 2, 0), []byte{msgBye, 1}))
	f.Add(seed(helloMsg(Version, "test", 0xfeed, 2, 0), []byte{9}))
	f.Fuzz(func(t *testing.T, data []byte) {
		// The input is a list of messages, each with a 2-byte length.
		link := &scriptLink{}
		for len(data) >= 2 {
			n := min(int(binary.LittleEndian.Uint16(data)), len(data)-2)
			link.in = append(link.in, data[2:2+n])
			data = data[2+n:]
		}
		cfg := testConfig(2, 0, 2)
		cfg.HashInterval = 1
		s, err := New(cfg, link)
		if err != nil {
			t.Fatal(err)
		}
		for range 2*len(link.in) + 1 {
			if s.Poll() != nil {
				return
			}
			if _, ok := s.Ready(); ok {
				if s.Done(s.Tick()) != nil {
					return
				}
			}
		}
	})
}
