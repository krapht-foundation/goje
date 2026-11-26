//go:build !js

package netlink

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/krapht-foundation/goje/cmd/relay/relay"
)

func startRelay2(tb testing.TB, cfg relay.Config) string {
	tb.Helper()
	cfg.Logger = slog.New(slog.DiscardHandler)
	s := relay.New(cfg)
	srv := httptest.NewServer(s)
	tb.Cleanup(func() {
		srv.Close()
		s.Close()
	})
	return "ws" + strings.TrimPrefix(srv.URL, "http")
}

// eventually polls cond for up to five seconds.
func eventually(tb testing.TB, what string, cond func() bool) {
	tb.Helper()
	for deadline := time.Now().Add(5 * time.Second); !cond(); {
		if time.Now().After(deadline) {
			tb.Fatal("timed out waiting for " + what)
		}
		time.Sleep(time.Millisecond)
	}
}

func pair2(tb testing.TB, url string) (h, g *Conn) {
	tb.Helper()
	h = dialT(tb, url+"/v2/new")
	g = dialT(tb, url+"/v2/join?room="+strings.ToLower(h.Code()))
	eventually(tb, "epoch 1", func() bool { return h.Epoch() == 1 && g.Epoch() == 1 })
	return h, g
}

// recvEpoch polls RecvEpoch for up to five seconds.
func recvEpoch(tb testing.TB, c *Conn, epoch uint64) string {
	tb.Helper()
	var msg []byte
	eventually(tb, "a message", func() bool {
		m, ok, err := c.RecvEpoch(epoch)
		if err != nil {
			tb.Fatalf("RecvEpoch(%d): %v", epoch, err)
		}
		msg = m
		return ok
	})
	return string(msg)
}

func exchange(tb testing.TB, a, b *Conn, epoch uint64, msg string) {
	tb.Helper()
	if err := a.SendEpoch(epoch, []byte(msg)); err != nil {
		tb.Fatal(err)
	}
	if got := recvEpoch(tb, b, epoch); got != msg {
		tb.Fatalf("got %q, want %q", got, msg)
	}
}

// cut drops c's transport as a network failure would, without a normal close.
func cut(tb testing.TB, c *Conn) {
	tb.Helper()
	if err := c.tr.(wsTransport).c.CloseWith(closeLost); err != nil {
		tb.Fatal(err)
	}
	eventually(tb, "ErrLost", func() bool { return errors.Is(c.Err(), ErrLost) })
}

func TestV2PairWatchAndEnd(t *testing.T) {
	url := startRelay2(t, relay.Config{})
	h, g := pair2(t, url)
	if h.Slot() != 0 || g.Slot() != 1 || g.Code() != h.Code() || g.ViewCode() != h.ViewCode() ||
		h.ViewCode() == h.Code() || len(h.ViewCode()) != 6 {
		t.Fatalf("host %d %q %q, guest %d %q %q", h.Slot(), h.Code(), h.ViewCode(), g.Slot(), g.Code(),
			g.ViewCode())
	}
	if h.ResumeToken() == g.ResumeToken() || h.ResumeToken() == ([32]byte{}) {
		t.Fatal("resume tokens are not distinct and nonzero")
	}
	exchange(t, h, g, 1, "hello")
	exchange(t, g, h, 1, "back")

	if _, err := Dial(t.Context(), url+"/v2/join?room="+h.ViewCode()); err == nil {
		t.Fatal("joined as a player with the spectator code")
	}
	if _, err := Dial(t.Context(), url+"/v2/watch?room="+h.Code()); err == nil {
		t.Fatal("watched with the invitation code")
	}
	w := dialT(t, url+"/v2/watch?room="+h.ViewCode())
	if w.Slot() != watchSlot || w.Code() != "" || w.ViewCode() != h.ViewCode() ||
		w.ResumeToken() != ([32]byte{}) {
		t.Fatalf("spectator %d %q %q", w.Slot(), w.Code(), w.ViewCode())
	}
	if _, err := Dial(t.Context(), url+"/v2/watch?room="+h.ViewCode()); err == nil {
		t.Fatal("a second spectator joined")
	}
	eventually(
		t,
		"the spectator id",
		func() bool { return h.Watcher() != 0 && g.Watcher() == h.Watcher() },
	)
	if err := w.Send([]byte("order")); err == nil {
		t.Fatal("the spectator sent")
	}
	// Views sent just before the end reach the spectator before PeerLeft.
	for i := range viewQueue {
		if err := h.SendView(h.Watcher(), fmt.Appendf(nil, "view %d", i)); err != nil {
			t.Fatal(err)
		}
	}
	if err := h.EndRoom(); err != nil {
		t.Fatal(err)
	}
	h.Close()
	for i := range viewQueue {
		if m, err := recv(t, w); err != nil || string(m) != fmt.Sprintf("view %d", i) {
			t.Fatalf("view %d: %q, %v", i, m, err)
		}
	}
	if _, err := recv(t, w); !errors.Is(err, ErrPeerLeft) {
		t.Fatalf("spectator after the end: %v", err)
	}
	if _, err := recv(t, g); !errors.Is(err, ErrPeerLeft) {
		t.Fatalf("guest after the end: %v", err)
	}
	if _, err := Resume(
		t.Context(),
		url+"/v2/resume",
		g.ResumeToken(),
	); !errors.Is(
		err,
		ErrPeerLeft,
	) {
		t.Fatalf("Resume after the end = %v", err)
	}
}

func TestV2Resume(t *testing.T) {
	url := startRelay2(t, relay.Config{})
	h, g := pair2(t, url)
	exchange(t, h, g, 1, "before")
	if err := g.SendEpoch(1, []byte("in flight")); err != nil {
		t.Fatal(err)
	}
	cut(t, g)
	eventually(t, "the pause", func() bool { return h.Epoch() == 0 })
	if err := h.SendEpoch(1, []byte("x")); !errors.Is(err, ErrEpochChanged) {
		t.Fatalf("SendEpoch while paused = %v", err)
	}
	if _, err := Resume(t.Context(), url+"/v2/resume", [32]byte{1}); !errors.Is(err, ErrPeerLeft) {
		t.Fatalf("Resume with a wrong token = %v", err)
	}
	if _, err := Resume(t.Context(), url+"/v2/resume", h.ResumeToken()); !errors.Is(err, ErrLost) {
		t.Fatalf("Resume of a connected slot = %v", err)
	}
	if h.Err() != nil {
		t.Fatalf("the connected player was disturbed: %v", h.Err())
	}
	g2, err := Resume(t.Context(), url+"/v2/resume", g.ResumeToken())
	if err != nil {
		t.Fatal(err)
	}
	defer g2.Close()
	if g2.Slot() != 1 || g2.Code() != h.Code() || g2.ResumeToken() != g.ResumeToken() {
		t.Fatalf("resumed slot %d code %q", g2.Slot(), g2.Code())
	}
	eventually(t, "epoch 2", func() bool { return h.Epoch() == 2 && g2.Epoch() == 2 })
	exchange(t, h, g2, 2, "after")
	exchange(t, g2, h, 2, "after too")
	// Nothing of epoch 1 was delivered after the pause.
	if m, ok, _ := h.RecvEpoch(2); ok {
		t.Fatalf("stale message %q", m)
	}
}

func TestV2BothLostAndConcurrentResumes(t *testing.T) {
	url := startRelay2(t, relay.Config{})
	h, g := pair2(t, url)
	cut(t, h)
	cut(t, g)
	var mu sync.Mutex
	won := map[int][]*Conn{}
	var wg sync.WaitGroup
	for range 4 {
		for _, old := range []*Conn{h, g} {
			wg.Go(func() {
				c, err := Resume(t.Context(), url+"/v2/resume", old.ResumeToken())
				if err != nil {
					if !errors.Is(err, ErrLost) {
						t.Error(err)
					}
					return
				}
				mu.Lock()
				won[c.Slot()] = append(won[c.Slot()], c)
				mu.Unlock()
			})
		}
	}
	wg.Wait()
	if len(won[0]) != 1 || len(won[1]) != 1 {
		t.Fatalf("%d hosts and %d guests resumed", len(won[0]), len(won[1]))
	}
	h2, g2 := won[0][0], won[1][0]
	defer h2.Close()
	defer g2.Close()
	eventually(t, "epoch 2", func() bool { return h2.Epoch() == 2 && g2.Epoch() == 2 })
	exchange(t, h2, g2, 2, "together")
}

func TestV2ReservationExpires(t *testing.T) {
	url := startRelay2(t, relay.Config{Rejoin: 200 * time.Millisecond})
	h, g := pair2(t, url)
	cut(t, g)
	if _, err := recv(t, h); !errors.Is(err, ErrPeerLeft) {
		t.Fatalf("host after the reservation expired: %v", err)
	}
	if _, err := Resume(
		t.Context(),
		url+"/v2/resume",
		g.ResumeToken(),
	); !errors.Is(
		err,
		ErrPeerLeft,
	) {
		t.Fatalf("Resume after expiry = %v", err)
	}
}

func TestV2CloseLeavesForGood(t *testing.T) {
	url := startRelay2(t, relay.Config{})
	h, g := pair2(t, url)
	g.Close()
	// The 30-second hold would outlast recv's five seconds.
	if _, err := recv(t, h); !errors.Is(err, ErrPeerLeft) {
		t.Fatalf("host after the guest closed: %v", err)
	}
}
