//go:build !js

package ws

import (
	"bytes"
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// echoServer upgrades every request and echoes binary messages until the
// client closes.
func echoServer(tb testing.TB) string {
	tb.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := Accept(w, r, Options{Limit: 1 << 16})
		if err != nil {
			return
		}
		defer c.Close()
		buf := make([]byte, 1<<16)
		for {
			msg, err := c.ReadMessage(buf)
			if err != nil {
				return
			}
			if err := c.WriteMessage(msg); err != nil {
				return
			}
		}
	}))
	tb.Cleanup(srv.Close)
	return "ws" + strings.TrimPrefix(srv.URL, "http")
}

func TestLoopback(t *testing.T) {
	c, err := Dial(t.Context(), echoServer(t)+"/echo?x=1", Options{Limit: 1 << 16})
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 1<<16)
	for _, n := range []int{0, 1, 125, 126, 2048, 0xFFFF, 0x10000} {
		msg := bytes.Repeat([]byte{byte(n)}, n)
		if err := c.WriteMessage(msg); err != nil {
			t.Fatal(err)
		}
		got, err := c.ReadMessage(buf)
		if err != nil || !bytes.Equal(got, msg) {
			t.Fatalf("%d bytes echoed as %d, %v", n, len(got), err)
		}
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if err := c.WriteMessage([]byte("x")); err == nil {
		t.Fatal("write after Close succeeded")
	}
}

func TestDialRejects(t *testing.T) {
	url := echoServer(t)
	for _, bad := range []string{"http://x/", "ws://%zz", strings.Replace(url, "ws:", "wss:", 1)} {
		if c, err := Dial(t.Context(), bad, Options{}); err == nil {
			c.Close()
			t.Errorf("Dial(%q) succeeded", bad)
		}
	}
	// A plain HTTP server answers 404 instead of upgrading.
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	if _, err := Dial(
		t.Context(),
		"ws"+strings.TrimPrefix(srv.URL, "http"),
		Options{},
	); !errors.Is(
		err,
		ErrProtocol,
	) {
		t.Fatalf("Dial to a non-WebSocket server = %v", err)
	}
}

func TestDialCancel(t *testing.T) {
	// The listener accepts connections but never answers the handshake.
	var lc net.ListenConfig
	ln, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	// The one connection Dial makes stays open, unanswered, until the test ends.
	go func() {
		nc, err := ln.Accept()
		if err != nil {
			return
		}
		defer nc.Close()
		<-t.Context().Done()
	}()
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := Dial(
		ctx,
		"ws://"+ln.Addr().String()+"/",
		Options{},
	); !errors.Is(
		err,
		context.DeadlineExceeded,
	) {
		t.Fatalf("Dial = %v", err)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("cancelled Dial took %v", d)
	}
}

func TestSteadyStateAllocations(t *testing.T) {
	c, err := Dial(t.Context(), echoServer(t), Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	msg := make([]byte, 64)
	buf := make([]byte, DefaultLimit)
	roundTrip := func() {
		if err := c.WriteMessage(msg); err != nil {
			t.Fatal(err)
		}
		if _, err := c.ReadMessage(buf); err != nil {
			t.Fatal(err)
		}
	}
	roundTrip()
	// The count covers the echoing server goroutine and the runtime's network poller.
	// A collection during a run empties pools that the runtime refills, which shows as
	// an allocation, so the steady state is shown by any one clean run out of five.
	var n float64
	for range 5 {
		if n = testing.AllocsPerRun(200, roundTrip); n == 0 {
			return
		}
	}
	t.Errorf("%v allocations per round trip in each of five runs", n)
}
