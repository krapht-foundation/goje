package relay

import (
	"bytes"
	"net/http"
	"testing"
	"time"

	"github.com/krapht-foundation/goje/netlink/ws"
)

type v2Room struct {
	h, g       *ws.Conn
	code, view string
	hTok, gTok []byte
}

// host2 opens a v2 room and checks the host's Welcome.
func host2(tb testing.TB, r testRelay) (c *ws.Conn, code, view string, token []byte) {
	tb.Helper()
	c = dial(tb, r.url+"/v2/new")
	w := read(tb, c)
	if len(w) != 2+6+6+tokenLen || w[0] != msgWelcome2 || w[1] != 0 {
		tb.Fatalf("v2 host welcome % x", w)
	}
	return c, string(w[2:8]), string(w[8:14]), w[14:]
}

func pair2(tb testing.TB, r testRelay) v2Room {
	tb.Helper()
	var rm v2Room
	rm.h, rm.code, rm.view, rm.hTok = host2(tb, r)
	rm.g = dial(tb, r.url+"/v2/join?room="+rm.code)
	w := read(tb, rm.g)
	if len(w) != 2+6+6+tokenLen || w[1] != 1 || string(w[2:8]) != rm.code ||
		string(w[8:14]) != rm.view {
		tb.Fatalf("v2 guest welcome % x", w)
	}
	rm.gTok = w[14:]
	if bytes.Equal(rm.hTok, rm.gTok) || rm.code == rm.view {
		tb.Fatal("tokens or codes collide")
	}
	expect(tb, rm.h, idMessage(msgEpoch, 1))
	expect(tb, rm.g, idMessage(msgEpoch, 1))
	return rm
}

func expect(tb testing.TB, c *ws.Conn, want []byte) {
	tb.Helper()
	if got := read(tb, c); !bytes.Equal(got, want) {
		tb.Fatalf("got % x, want % x", got, want)
	}
}

func data2(epoch uint64, payload string) []byte {
	return append(idMessage(msgData, epoch), payload...)
}

func view2(id uint64, payload string) []byte {
	return append(idMessage(msgView, id), payload...)
}

func TestV2StaleEpochIsDropped(t *testing.T) {
	r := start(t, Config{})
	rm := pair2(t, r)
	send(t, rm.h, data2(0, "old"))
	send(t, rm.h, data2(2, "future"))
	send(t, rm.h, data2(1, "now"))
	expect(t, rm.g, data2(1, "now"))

	// The guest's transport fails without a close frame of 1000.
	rm.g.CloseWith(4000)
	expect(t, rm.h, []byte{msgPaused})
	send(t, rm.h, data2(1, "while paused"))
	g := dial(t, r.url+"/v2/resume")
	send(t, g, append([]byte{msgResume}, rm.gTok...))
	if w := read(t, g); w[0] != msgWelcome2 || w[1] != 1 || !bytes.Equal(w[14:], rm.gTok) {
		t.Fatalf("resume welcome % x", w)
	}
	expect(t, g, idMessage(msgEpoch, 2))
	expect(t, rm.h, idMessage(msgEpoch, 2))
	send(t, rm.h, data2(1, "stale"))
	send(t, rm.h, data2(2, "fresh"))
	expect(t, g, data2(2, "fresh"))
}

func TestV2Authorization(t *testing.T) {
	r := start(t, Config{MaxPending: 1})
	rm := pair2(t, r)
	// A connected slot is never replaced.
	c := dial(t, r.url+"/v2/resume")
	send(t, c, append([]byte{msgResume}, rm.hTok...))
	expectClosed(t, c)
	// An unknown token ends the attempt with PeerLeft.
	c = dial(t, r.url+"/v2/resume")
	send(t, c, append([]byte{msgResume}, make([]byte, tokenLen)...))
	expectLeft(t, c)
	// A malformed first message is refused.
	c = dial(t, r.url+"/v2/resume")
	send(t, c, []byte{msgResume, 1, 2})
	expectClosed(t, c)
	// Unauthenticated resumes are bounded.
	idle := dial(t, r.url+"/v2/resume")
	waitFor(t, "the pending resume", func() bool {
		r.mu.Lock()
		defer r.mu.Unlock()
		return r.pending == 1
	})
	if s := status(t, r.url+"/v2/resume", nil); s != http.StatusServiceUnavailable {
		t.Fatalf("resume beyond the cap = %d", s)
	}
	idle.Close()
	waitFor(t, "the pending slot", func() bool {
		r.mu.Lock()
		defer r.mu.Unlock()
		return r.pending == 0
	})
	// Codes are not interchangeable, and each room has one guest.
	for path, want := range map[string]int{
		"/v2/join?room=" + rm.view:  http.StatusNotFound,
		"/v2/watch?room=" + rm.code: http.StatusNotFound,
		"/v2/join?room=" + rm.code:  http.StatusConflict,
		"/join?room=" + rm.code:     http.StatusNotFound,
	} {
		if s := status(t, r.url+path, nil); s != want {
			t.Errorf("%s = %d, want %d", path, s, want)
		}
	}
}

func TestV2RoomCapIsShared(t *testing.T) {
	r := start(t, Config{MaxRooms: 2})
	host(t, r)
	host2(t, r)
	for _, path := range []string{"/new", "/v2/new"} {
		if s := status(t, r.url+path, nil); s != http.StatusServiceUnavailable {
			t.Errorf("%s beyond the cap = %d", path, s)
		}
	}
}

func TestV2SpectatorGenerations(t *testing.T) {
	r := start(t, Config{})
	rm := pair2(t, r)
	w1 := dial(t, r.url+"/v2/watch?room="+rm.view)
	expect(t, w1, append([]byte{msgWelcome2, 2}, rm.view...))
	expect(t, rm.h, idMessage(msgWatcher, 1))
	expect(t, rm.g, idMessage(msgWatcher, 1))
	if s := status(t, r.url+"/v2/watch?room="+rm.view, nil); s != http.StatusConflict {
		t.Fatalf("second spectator = %d", s)
	}
	send(t, rm.g, view2(1, "from the guest"))
	send(t, rm.h, view2(1, "seen"))
	expect(t, w1, []byte("\x09seen"))
	// A spectator that sends anything but a keepalive loses its seat.
	send(t, w1, []byte{msgKeepAlive})
	expect(t, w1, []byte{msgKeepAlive})
	send(t, w1, data2(1, "order"))
	expectClosed(t, w1)
	expect(t, rm.h, idMessage(msgWatcher, 0))
	expect(t, rm.g, idMessage(msgWatcher, 0))

	w2 := dial(t, r.url+"/v2/watch?room="+rm.view)
	expect(t, w2, append([]byte{msgWelcome2, 2}, rm.view...))
	expect(t, rm.h, idMessage(msgWatcher, 2))
	send(t, rm.h, view2(1, "for the old spectator"))
	send(t, rm.h, view2(2, "current"))
	expect(t, w2, []byte("\x09current"))
	// Play was never disturbed.
	send(t, rm.g, data2(1, "still playing"))
	expect(t, rm.h, data2(1, "still playing"))
}

func TestV2SlowSpectatorIsEvicted(t *testing.T) {
	r := start(t, Config{Rate: 1 << 20, ViewQueue: 4})
	rm := pair2(t, r)
	dial(t, r.url+"/v2/watch?room="+rm.view) // Never read.
	expect(t, rm.h, idMessage(msgWatcher, 1))
	expect(t, rm.g, idMessage(msgWatcher, 1))
	big := view2(1, string(make([]byte, 2048)))
	start := time.Now()
	evicted := false
	for i := 0; i < 20000 && !evicted; i++ {
		send(t, rm.h, big)
		// The players keep exchanging promptly while the spectator stalls.
		if i%500 == 0 {
			send(t, rm.g, data2(1, "ping"))
			for {
				m := read(t, rm.h)
				if bytes.Equal(m, idMessage(msgWatcher, 0)) {
					evicted = true
					continue
				}
				if !bytes.Equal(m, data2(1, "ping")) {
					t.Fatalf("host got % x", m[:min(len(m), 16)])
				}
				break
			}
		}
	}
	if !evicted {
		expect(t, rm.h, idMessage(msgWatcher, 0))
	}
	if d := time.Since(start); d > 10*time.Second {
		t.Fatalf("players waited %v for the slow spectator", d)
	}
	expect(t, rm.g, idMessage(msgWatcher, 0))
}

func TestV2EndReachesEveryone(t *testing.T) {
	r := start(t, Config{})
	rm := pair2(t, r)
	w := dial(t, r.url+"/v2/watch?room="+rm.view)
	read(t, w)
	expect(t, rm.h, idMessage(msgWatcher, 1))
	expect(t, rm.g, idMessage(msgWatcher, 1))
	send(t, rm.h, view2(1, "last"))
	send(t, rm.h, []byte{msgEnd})
	expect(t, w, []byte("\x09last"))
	for _, c := range []*ws.Conn{w, rm.h, rm.g} {
		expectLeft(t, c)
	}
	waitFor(t, "the room to close", func() bool { return r.rooms2Len() == 0 })
}

func (r testRelay) rooms2Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.rooms2) + len(r.views) + len(r.tokens)
}

func TestV2ExpiryWhileHeld(t *testing.T) {
	r := start(t, Config{Rejoin: 100 * time.Millisecond, Idle: time.Hour})
	rm := pair2(t, r)
	rm.h.CloseWith(4000)
	expect(t, rm.g, []byte{msgPaused})
	expectLeft(t, rm.g)
	waitFor(t, "the room to close", func() bool { return r.rooms2Len() == 0 })
}

func TestV2HostLostBeforePairing(t *testing.T) {
	r := start(t, Config{})
	h, code, _, tok := host2(t, r)
	h.CloseWith(4000)
	waitFor(t, "the server to reserve the host's seat", func() bool {
		r.mu.Lock()
		defer r.mu.Unlock()
		rm := r.rooms2[code]
		return rm != nil && rm.held[0]
	})
	g := dial(t, r.url+"/v2/join?room="+code)
	if w := read(t, g); w[1] != 1 {
		t.Fatalf("guest welcome % x", w)
	}
	h2 := dial(t, r.url+"/v2/resume")
	send(t, h2, append([]byte{msgResume}, tok...))
	if w := read(t, h2); w[1] != 0 {
		t.Fatalf("host welcome % x", w)
	}
	expect(t, h2, idMessage(msgEpoch, 1))
	expect(t, g, idMessage(msgEpoch, 1))
}
