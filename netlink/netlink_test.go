package netlink

import (
	"bytes"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// link is a copy of engine/lockstep's Link, which netlink does not import.
type link interface {
	Send(msg []byte) error
	Recv() (msg []byte, ok bool, err error)
}

var _ link = (*Conn)(nil)

// fakeTransport records writes. While gate is open, writes wait for a value
// on it.
type fakeTransport struct {
	mu      sync.Mutex
	written [][]byte
	gate    chan struct{}
	err     error
	closed  chan struct{}
	lost    bool
}

func newFake() *fakeTransport { return &fakeTransport{closed: make(chan struct{})} }

func (f *fakeTransport) write(msg []byte) error {
	if f.gate != nil {
		<-f.gate
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.written = append(f.written, bytes.Clone(msg))
	return f.err
}

func (f *fakeTransport) close(lost bool) {
	f.lost = lost
	close(f.closed)
}

func (f *fakeTransport) writes() [][]byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.written
}

func startConn(tr *fakeTransport, slot int) *Conn {
	c := newConn(tr, welcome{slot: slot, code: "ABCDEF"})
	go c.writeLoop()
	return c
}

func waitClosed(tb testing.TB, tr *fakeTransport) {
	tb.Helper()
	select {
	case <-tr.closed:
	case <-time.After(5 * time.Second):
		tb.Fatal("transport not closed")
	}
}

func TestRing(t *testing.T) {
	r := newRing(3)
	for i := range 3 {
		if !r.push(nil, []byte{byte(i)}) {
			t.Fatalf("push %d failed", i)
		}
	}
	if r.push(nil, []byte{9}) {
		t.Fatal("push into a full ring succeeded")
	}
	for round := range 5 {
		if got := r.front()[0]; int(got) != round {
			t.Fatalf("front %d, want %d", got, round)
		}
		r.pop()
		r.push(nil, []byte{byte(round + 3)})
	}
	msg := make([]byte, 100)
	if n := testing.AllocsPerRun(100, func() {
		r.pop()
		r.push(nil, msg)
	}); n != 0 {
		t.Errorf("%v allocations per push once warm", n)
	}
}

func TestParseWelcome(t *testing.T) {
	w, err := parseWelcome([]byte("\x01\x01ABC234"))
	if err != nil || w != (welcome{slot: 1, code: "ABC234"}) {
		t.Fatalf("parseWelcome = %+v %v", w, err)
	}
	for _, bad := range []string{
		"", "\x01\x00ABC23", "\x01\x00ABC2345", "\x02\x00ABC234",
		"\x01\x02ABC234", "\x01\x00ABC10O", "\x01\x00abc234",
	} {
		if _, err := parseWelcome([]byte(bad)); !errors.Is(err, ErrProtocol) {
			t.Errorf("parseWelcome(%q) = %v", bad, err)
		}
	}
}

func TestReceive(t *testing.T) {
	tr := newFake()
	c := startConn(tr, 0)
	if c.PeerJoined() {
		t.Fatal("host starts joined")
	}
	if _, ok, err := c.Recv(); ok || err != nil {
		t.Fatalf("empty Recv = %v, %v", ok, err)
	}
	for _, m := range []string{"\x02", "\x04one", "\x04two"} {
		if err := c.receive([]byte(m)); err != nil {
			t.Fatal(err)
		}
	}
	if !c.PeerJoined() {
		t.Fatal("PeerJoined not recorded")
	}
	first, ok, err := c.Recv()
	if !ok || err != nil || string(first) != "one" {
		t.Fatalf("Recv = %q, %v, %v", first, ok, err)
	}
	// The held message survives new arrivals until the next Recv.
	for range recvQueue - 2 {
		if err := c.receive([]byte("\x04more")); err != nil {
			t.Fatal(err)
		}
	}
	if string(first) != "one" {
		t.Fatalf("held message overwritten with %q", first)
	}
	if err := c.receive([]byte("\x04over")); err == nil {
		t.Fatal("receive into a full queue succeeded")
	}
	if err := c.receive([]byte{msgPeerLeft}); err != nil {
		t.Fatal(err)
	}
	if m, _, _ := c.Recv(); string(m) != "two" {
		t.Fatalf("second message %q", m)
	}
	for range recvQueue - 2 {
		if _, ok, err := c.Recv(); !ok || err != nil {
			t.Fatalf("queued message lost: %v %v", ok, err)
		}
	}
	if _, ok, err := c.Recv(); ok || !errors.Is(err, ErrPeerLeft) {
		t.Fatalf("Recv after PeerLeft = %v, %v", ok, err)
	}
	if err := c.Send([]byte("x")); !errors.Is(err, ErrPeerLeft) {
		t.Fatalf("Send after PeerLeft = %v", err)
	}
	waitClosed(t, tr)
}

func TestReceiveRejects(t *testing.T) {
	for _, m := range []string{"", "\x04", "\x01\x00ABCDEF", "\x02x", "\x03x", "\x05", "\xFF"} {
		c := newConn(newFake(), welcome{slot: 1, code: "ABCDEF"})
		if err := c.receive([]byte(m)); !errors.Is(err, ErrProtocol) {
			t.Errorf("receive(%q) = %v", m, err)
		}
	}
}

func TestSendQueue(t *testing.T) {
	tr := newFake()
	tr.gate = make(chan struct{})
	c := startConn(tr, 1)
	for _, bad := range [][]byte{nil, make([]byte, MaxMessage+1)} {
		if err := c.Send(bad); err == nil {
			t.Errorf("Send of %d bytes succeeded", len(bad))
		}
	}
	// The message being written still holds its slot.
	for i := range sendQueue {
		if err := c.Send([]byte{byte(i)}); err != nil {
			t.Fatalf("Send %d: %v", i, err)
		}
	}
	if err := c.Send([]byte("x")); !errors.Is(err, ErrQueueFull) {
		t.Fatalf("Send into a full queue = %v", err)
	}
	// Close still writes everything queued, in order, then closes.
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if err := c.Send([]byte("x")); !errors.Is(err, ErrClosed) {
		t.Fatalf("Send after Close = %v", err)
	}
	if _, _, err := c.Recv(); !errors.Is(err, ErrClosed) {
		t.Fatalf("Recv after Close = %v", err)
	}
	close(tr.gate)
	waitClosed(t, tr)
	w := tr.writes()
	if len(w) != sendQueue {
		t.Fatalf("%d messages written, want %d", len(w), sendQueue)
	}
	for i, m := range w {
		if m[0] != byte(i) {
			t.Fatalf("message %d written as %d", i, m[0])
		}
	}
}

func TestWriteError(t *testing.T) {
	tr := newFake()
	tr.err = errors.New("broken pipe")
	c := startConn(tr, 1)
	if err := c.Send([]byte("x")); err != nil {
		t.Fatal(err)
	}
	waitClosed(t, tr)
	if !tr.lost {
		t.Error("a lost transport closed as a deliberate leave")
	}
	if err := c.Send(
		[]byte("x"),
	); !errors.Is(err, ErrLost) ||
		!strings.Contains(err.Error(), "broken pipe") {
		t.Fatalf("Send after a write error = %v", err)
	}
	if _, _, err := c.Recv(); err == nil {
		t.Fatal("Recv after a write error reports no error")
	}
}

func TestSteadyStateAllocations(t *testing.T) {
	tr := newFake()
	c := newConn(tr, welcome{slot: 1, code: "ABCDEF"})
	msg := append([]byte{msgData}, make([]byte, 100)...)
	cycle := func() {
		if err := c.receive(msg); err != nil {
			t.Fatal(err)
		}
		if _, ok, _ := c.Recv(); !ok {
			t.Fatal("message lost")
		}
		if err := c.Send(msg[1:]); err != nil {
			t.Fatal(err)
		}
		// Drain the send queue as the writer would.
		c.mu.Lock()
		c.out.pop()
		c.mu.Unlock()
	}
	for range recvQueue {
		cycle()
	}
	if n := testing.AllocsPerRun(100, cycle); n != 0 {
		t.Errorf("%v allocations per message once warm", n)
	}
}
