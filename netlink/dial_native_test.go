//go:build !js

package netlink

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/krapht-foundation/goje/cmd/relay/relay"
	"github.com/krapht-foundation/goje/netlink/ws"
)

func startRelay(tb testing.TB) (s *relay.Server, url string) {
	tb.Helper()
	s = relay.New(relay.Config{Logger: slog.New(slog.DiscardHandler)})
	srv := httptest.NewServer(s)
	tb.Cleanup(func() {
		srv.Close()
		s.Close()
	})
	return s, "ws" + strings.TrimPrefix(srv.URL, "http")
}

func dialT(tb testing.TB, url string) *Conn {
	tb.Helper()
	c, err := Dial(tb.Context(), url)
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { c.Close() })
	return c
}

// recv polls c for up to five seconds.
func recv(tb testing.TB, c *Conn) ([]byte, error) {
	tb.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		msg, ok, err := c.Recv()
		if ok || err != nil {
			return msg, err
		}
		time.Sleep(time.Millisecond)
	}
	tb.Fatal("no message within five seconds")
	return nil, nil
}

func pairConns(tb testing.TB, url string) (h, g *Conn) {
	tb.Helper()
	h = dialT(tb, url+"/new")
	if h.Slot() != 0 || len(h.Code()) != 6 || h.PeerJoined() {
		tb.Fatalf("host slot %d, code %q, joined %v", h.Slot(), h.Code(), h.PeerJoined())
	}
	g = dialT(tb, url+"/join?room="+h.Code())
	if g.Slot() != 1 || g.Code() != h.Code() || !g.PeerJoined() {
		tb.Fatalf("guest slot %d, code %q, joined %v", g.Slot(), g.Code(), g.PeerJoined())
	}
	for deadline := time.Now().Add(5 * time.Second); !h.PeerJoined(); {
		if time.Now().After(deadline) {
			tb.Fatal("host never saw the guest join")
		}
		time.Sleep(time.Millisecond)
	}
	return h, g
}

func TestRelayRoundTrip(t *testing.T) {
	_, url := startRelay(t)
	h, g := pairConns(t, url)
	if err := h.Send(make([]byte, MaxMessage+1)); err == nil {
		t.Fatal("oversized Send succeeded")
	}
	for i := range 100 {
		size := []int{1, MaxMessage, 40}[i%3]
		for _, p := range [][2]*Conn{{h, g}, {g, h}} {
			msg := bytes.Repeat([]byte{byte(i)}, size)
			if err := p[0].Send(msg); err != nil {
				t.Fatal(err)
			}
			got, err := recv(t, p[1])
			if err != nil || !bytes.Equal(got, msg) {
				t.Fatalf("message %d: %d bytes, %v", i, len(got), err)
			}
		}
	}
}

func TestCloseFlushesAndPeerLeaves(t *testing.T) {
	baseline := runtime.NumGoroutine()
	_, url := startRelay(t)
	h, g := pairConns(t, url)
	if err := g.Send([]byte("bye")); err != nil {
		t.Fatal(err)
	}
	if err := g.Close(); err != nil {
		t.Fatal(err)
	}
	if err := g.Close(); err != nil {
		t.Fatal(err)
	}
	if m, err := recv(t, h); err != nil || string(m) != "bye" {
		t.Fatalf("host got %q, %v", m, err)
	}
	if _, err := recv(t, h); !errors.Is(err, ErrPeerLeft) {
		t.Fatalf("host after the guest left: %v", err)
	}
	if err := h.Send([]byte("x")); !errors.Is(err, ErrPeerLeft) {
		t.Fatalf("Send after the peer left = %v", err)
	}
	h.Close()
	for deadline := time.Now().Add(5 * time.Second); runtime.NumGoroutine() > baseline+4; {
		if time.Now().After(deadline) {
			t.Fatalf("%d goroutines remain, baseline %d", runtime.NumGoroutine(), baseline)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestRelayShutdown(t *testing.T) {
	s, url := startRelay(t)
	h, g := pairConns(t, url)
	s.Close()
	for _, c := range []*Conn{h, g} {
		if _, err := recv(t, c); err == nil || errors.Is(err, ErrPeerLeft) {
			t.Fatalf("Recv after the relay closed = %v", err)
		}
	}
	if err := h.Send([]byte("x")); err == nil {
		t.Fatal("Send after the relay closed succeeded")
	}
}

func TestDialErrors(t *testing.T) {
	_, url := startRelay(t)
	if _, err := Dial(t.Context(), url+"/join?room=AAAAAA"); err == nil {
		t.Fatal("joined a room that does not exist")
	}
	if _, err := Dial(t.Context(), "http://example.invalid/"); err == nil {
		t.Fatal("dialled an http URL")
	}
	// A server that upgrades but never sends a Welcome blocks Dial until the
	// context ends.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := ws.Accept(w, r, ws.Options{})
		if err != nil {
			return
		}
		defer c.Close()
		// Wait for the client to give up.
		c.ReadMessage(nil)
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	if _, err := Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")); !errors.Is(err,
		context.DeadlineExceeded) {
		t.Fatalf("Dial without a Welcome = %v", err)
	}
}

func TestErrReportsAClosedRelayWithoutTakingMessages(t *testing.T) {
	s, url := startRelay(t)
	h, g := pairConns(t, url)
	if err := g.Send([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	// Err must not consume the message that the host has not read yet.
	if err := h.Err(); err != nil {
		t.Fatalf("Err on an open connection = %v", err)
	}
	if m, err := recv(t, h); err != nil || string(m) != "hello" {
		t.Fatalf("host got %q, %v after calling Err", m, err)
	}
	s.Close()
	for deadline := time.Now().Add(5 * time.Second); h.Err() == nil; {
		if time.Now().After(deadline) {
			t.Fatal("Err stayed nil after the relay closed")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
