package netlink

import (
	"bytes"
	"encoding/binary"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func v2Conn(tr *fakeTransport, slot int) *Conn {
	return newConn(tr, welcome{slot: slot, code: "ABCDEF", view: "GHJKMN", v2: true})
}

func epochMsg(e uint64) []byte { return binary.BigEndian.AppendUint64([]byte{msgEpoch}, e) }

func dataMsg(e uint64, payload string) []byte {
	return append(binary.BigEndian.AppendUint64([]byte{msgData}, e), payload...)
}

func mustReceive(tb testing.TB, c *Conn, msgs ...[]byte) {
	tb.Helper()
	for _, m := range msgs {
		if err := c.receive(m); err != nil {
			tb.Fatalf("receive(% x): %v", m, err)
		}
	}
}

func TestParseWelcome2(t *testing.T) {
	var token [tokenLen]byte
	token[0], token[31] = 7, 9
	msg := append([]byte("\x05\x01ABC234DEF567"), token[:]...)
	w, err := parseWelcome(msg)
	if err != nil || w != (welcome{slot: 1, code: "ABC234", view: "DEF567", token: token, v2: true}) {
		t.Fatalf("player welcome = %+v, %v", w, err)
	}
	w, err = parseWelcome([]byte("\x05\x02DEF567"))
	if err != nil || w != (welcome{slot: 2, view: "DEF567", v2: true}) {
		t.Fatalf("spectator welcome = %+v, %v", w, err)
	}
	for _, bad := range [][]byte{
		[]byte("\x05\x02DEF56"), []byte("\x05\x00DEF567"), []byte("\x05\x03DEF567"),
		append([]byte("\x05\x02ABC234DEF567"), token[:]...), append([]byte("\x05\x00ABC234DEF56O"), token[:]...),
	} {
		if _, err := parseWelcome(bad); !errors.Is(err, ErrProtocol) {
			t.Errorf("parseWelcome(%q) = %v", bad, err)
		}
	}
	if _, err := parseFirst([]byte{msgPeerLeft}, true); !errors.Is(err, ErrPeerLeft) {
		t.Errorf("PeerLeft as first message = %v", err)
	}
	if _, err := parseFirst([]byte("\x05\x02DEF567"), true); !errors.Is(err, ErrProtocol) {
		t.Errorf("resume answered by a spectator welcome = %v", err)
	}
	if _, err := parseFirst([]byte("\x01\x00ABC234"), true); !errors.Is(err, ErrProtocol) {
		t.Errorf("resume answered by a v1 welcome = %v", err)
	}
}

func TestEpochs(t *testing.T) {
	tr := newFake()
	c := v2Conn(tr, 0)
	// Before pairing, Send drops and the pinned calls report the change.
	if err := c.Send([]byte("early")); err != nil {
		t.Fatal(err)
	}
	if c.out.n != 0 || c.Epoch() != 0 || c.PeerJoined() {
		t.Fatalf("unpaired: queued %d, epoch %d, joined %v", c.out.n, c.Epoch(), c.PeerJoined())
	}
	if err := c.SendEpoch(1, []byte("x")); !errors.Is(err, ErrEpochChanged) {
		t.Fatalf("SendEpoch before pairing = %v", err)
	}
	if err := c.SendEpoch(0, []byte("x")); err == nil {
		t.Fatal("SendEpoch(0) succeeded")
	}

	mustReceive(
		t,
		c,
		epochMsg(1),
		dataMsg(1, "a"),
		dataMsg(2, "future"),
		dataMsg(0, "past"),
		dataMsg(1, "b"),
	)
	if c.Epoch() != 1 || !c.PeerJoined() {
		t.Fatalf("epoch %d, joined %v", c.Epoch(), c.PeerJoined())
	}
	if m, ok, err := c.RecvEpoch(1); !ok || err != nil || string(m) != "a" {
		t.Fatalf("RecvEpoch = %q %v %v", m, ok, err)
	}
	if _, _, err := c.RecvEpoch(2); !errors.Is(err, ErrEpochChanged) {
		t.Fatalf("RecvEpoch for a later epoch = %v", err)
	}
	if err := c.SendEpoch(1, []byte("x")); err != nil {
		t.Fatal(err)
	}
	if f := c.out.front(); !bytes.Equal(f, dataMsg(1, "x")) {
		t.Fatalf("queued % x", f)
	}

	// A pause empties the queue, ends the epoch, and drops its data.
	mustReceive(t, c, []byte{msgPaused}, dataMsg(1, "late"))
	if c.Epoch() != 0 {
		t.Fatalf("paused epoch %d", c.Epoch())
	}
	if _, ok, err := c.Recv(); ok || err != nil {
		t.Fatalf("Recv while paused = %v %v", ok, err)
	}
	if _, _, err := c.RecvEpoch(1); !errors.Is(err, ErrEpochChanged) {
		t.Fatalf("RecvEpoch after a pause = %v", err)
	}
	if err := c.SendEpoch(1, []byte("x")); !errors.Is(err, ErrEpochChanged) {
		t.Fatalf("SendEpoch after a pause = %v", err)
	}
	mustReceive(t, c, epochMsg(2), dataMsg(2, "c"))
	if m, ok, err := c.RecvEpoch(2); !ok || err != nil || string(m) != "c" {
		t.Fatalf("RecvEpoch(2) = %q %v %v", m, ok, err)
	}
	// The relay never repeats or lowers an epoch.
	if err := c.receive(epochMsg(2)); !errors.Is(err, ErrProtocol) {
		t.Fatalf("repeated epoch = %v", err)
	}
}

func TestEpochKeepsHeldMessage(t *testing.T) {
	c := v2Conn(newFake(), 1)
	mustReceive(t, c, epochMsg(1), dataMsg(1, "held"), dataMsg(1, "dropped"))
	m, _, _ := c.Recv()
	mustReceive(t, c, epochMsg(2), dataMsg(2, "new"))
	if string(m) != "held" {
		t.Fatalf("held message overwritten with %q", m)
	}
	if m, ok, err := c.Recv(); !ok || err != nil || string(m) != "new" {
		t.Fatalf("Recv after the epoch = %q %v %v", m, ok, err)
	}
}

func TestStaleSendsAreNotWritten(t *testing.T) {
	tr := newFake()
	c := v2Conn(tr, 0)
	mustReceive(t, c, epochMsg(1))
	for _, m := range []string{"1a", "1b", "1c"} {
		if err := c.SendEpoch(1, []byte(m)); err != nil {
			t.Fatal(err)
		}
	}
	mustReceive(t, c, []byte{msgPaused}, epochMsg(2))
	if err := c.SendEpoch(2, []byte("2a")); err != nil {
		t.Fatal(err)
	}
	for i := range viewQueue {
		if err := c.SendView(5, []byte{byte(i)}); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.SendView(5, []byte("x")); !errors.Is(err, ErrQueueFull) {
		t.Fatalf("SendView beyond its share = %v", err)
	}
	if err := c.EndRoom(); err != nil {
		t.Fatal(err)
	}
	c.Close()
	go c.writeLoop()
	waitClosed(t, tr)
	w := tr.writes()
	want := make([]string, 0, viewQueue+2)
	want = append(want, string(dataMsg(2, "2a")))
	for i := range viewQueue {
		want = append(want, "\x09\x00\x00\x00\x00\x00\x00\x00\x05"+string(rune(i)))
	}
	want = append(want, "\x0A")
	if len(w) != len(want) {
		t.Fatalf("wrote %q, want %q", w, want)
	}
	for i := range w {
		if string(w[i]) != want[i] {
			t.Fatalf("write %d = %q, want %q", i, w[i], want[i])
		}
	}
	if c.views != 0 {
		t.Errorf("%d views still counted", c.views)
	}
	if tr.lost {
		t.Error("Close closed as a lost transport")
	}
}

func TestSpectatorAndViews(t *testing.T) {
	c := v2Conn(newFake(), watchSlot)
	if err := c.Send([]byte("x")); err == nil {
		t.Fatal("a spectator sent")
	}
	if err := c.SendView(1, []byte("x")); err == nil {
		t.Fatal("a spectator sent a view")
	}
	if err := c.EndRoom(); err == nil {
		t.Fatal("a spectator ended the room")
	}
	mustReceive(t, c, []byte("\x09view"), []byte{msgKeepAlive})
	if m, ok, _ := c.Recv(); !ok || string(m) != "view" {
		t.Fatalf("view %q", m)
	}
	for _, bad := range [][]byte{dataMsg(1, "x"), epochMsg(1), {msgPaused}, {msgView}} {
		if err := c.receive(bad); !errors.Is(err, ErrProtocol) {
			t.Errorf("spectator receive(% x) = %v", bad, err)
		}
	}
	host := v2Conn(newFake(), 0)
	mustReceive(t, host, binary.BigEndian.AppendUint64([]byte{msgWatcher}, 3))
	if host.Watcher() != 3 {
		t.Fatalf("Watcher() = %d", host.Watcher())
	}
	if err := host.SendView(0, []byte("x")); err == nil {
		t.Fatal("SendView to spectator 0 succeeded")
	}
	if err := v2Conn(newFake(), 1).SendView(3, []byte("x")); err == nil {
		t.Fatal("the guest sent a view")
	}
	if err := host.receive([]byte("\x09view")); !errors.Is(err, ErrProtocol) {
		t.Fatalf("player received a view: %v", err)
	}
}

func TestKeepAliveSilence(t *testing.T) {
	tr := newFake()
	c := v2Conn(tr, 0)
	if err := c.keepAlive(); err != nil || string(tr.writes()[0]) != "\x0D" {
		t.Fatalf("keepAlive = %v, wrote %q", err, tr.writes())
	}
	c.lastRecv.Store(time.Now().Add(-keepAliveTimeout - time.Second).UnixNano())
	if err := c.keepAlive(); !errors.Is(err, ErrLost) {
		t.Fatalf("keepAlive after silence = %v", err)
	}
	mustReceive(t, c, []byte{msgKeepAlive})
	if err := c.keepAlive(); err != nil {
		t.Fatalf("keepAlive after the relay spoke = %v", err)
	}
}

// TestEpochRace checks under -race that a reader pinned to an epoch only
// ever takes that epoch's messages while epochs change underneath it.
func TestEpochRace(t *testing.T) {
	c := v2Conn(newFake(), 1)
	const epochs = 200
	var wg sync.WaitGroup
	var stopped atomic.Bool
	wg.Go(func() {
		defer stopped.Store(true)
		for e := uint64(1); e <= epochs; e++ {
			if err := c.receive(epochMsg(e)); err != nil {
				t.Error(err)
				return
			}
			for i := range 5 {
				p := binary.BigEndian.AppendUint64(nil, e)
				if err := c.receive(dataMsg(e, string(append(p, byte(i))))); err != nil {
					t.Error(err)
					return
				}
			}
			if err := c.receive([]byte{msgPaused}); err != nil {
				t.Error(err)
				return
			}
		}
	})
	wg.Go(func() {
		for {
			e := c.Epoch()
			if e == 0 {
				if stopped.Load() {
					return
				}
				continue
			}
			for {
				m, ok, err := c.RecvEpoch(e)
				if err != nil {
					if !errors.Is(err, ErrEpochChanged) {
						t.Error(err)
					}
					break
				}
				if !ok {
					continue
				}
				if binary.BigEndian.Uint64(m) != e {
					t.Errorf("epoch %d reader took a message of epoch %d", e, binary.BigEndian.Uint64(m))
					return
				}
				if err := c.SendEpoch(e, m); err != nil && !errors.Is(err, ErrEpochChanged) {
					t.Error(err)
				}
				c.mu.Lock()
				c.out.n = 0 // No writer runs; keep the queue from filling.
				c.mu.Unlock()
			}
		}
	})
	wg.Wait()
}
