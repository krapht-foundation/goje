package relay

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/krapht-foundation/goje/netlink/ws"
)

// clientOpts makes a stuck test fail instead of hanging.
var clientOpts = ws.Options{Limit: 4096, ReadTimeout: 5 * time.Second}

type testRelay struct {
	*Server
	url string // ws://host:port
}

func start(tb testing.TB, cfg Config) testRelay {
	tb.Helper()
	cfg.Logger = slog.New(slog.DiscardHandler)
	s := New(cfg)
	srv := httptest.NewServer(s)
	tb.Cleanup(func() {
		srv.Close()
		s.Close()
	})
	return testRelay{s, "ws" + strings.TrimPrefix(srv.URL, "http")}
}

func (r testRelay) rooms() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.Server.rooms)
}

func dial(tb testing.TB, url string) *ws.Conn {
	tb.Helper()
	c, err := ws.Dial(context.Background(), url, clientOpts)
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { c.Close() })
	return c
}

func read(tb testing.TB, c *ws.Conn) []byte {
	tb.Helper()
	msg, err := c.ReadMessage(nil)
	if err != nil {
		tb.Fatal(err)
	}
	return msg
}

func send(tb testing.TB, c *ws.Conn, msg []byte) {
	tb.Helper()
	if err := c.WriteMessage(msg); err != nil {
		tb.Fatal(err)
	}
}

// host opens a room and returns the host connection and the room code.
func host(tb testing.TB, r testRelay) (c *ws.Conn, code string) {
	tb.Helper()
	c = dial(tb, r.url+"/new")
	w := read(tb, c)
	if len(w) != 8 || w[0] != msgWelcome || w[1] != 0 {
		tb.Fatalf("host welcome % x", w)
	}
	for _, b := range w[2:] {
		if !strings.ContainsRune(codeAlphabet, rune(b)) {
			tb.Fatalf("room code %q outside the alphabet", w[2:])
		}
	}
	return c, string(w[2:])
}

// pair opens a room and joins it, checking the Welcome and PeerJoined.
func pair(tb testing.TB, r testRelay) (h, g *ws.Conn) {
	tb.Helper()
	h, code := host(tb, r)
	g = dial(tb, r.url+"/join?room="+code)
	if w := read(tb, g); !bytes.Equal(w, append([]byte{msgWelcome, 1}, code...)) {
		tb.Fatalf("guest welcome % x for room %s", w, code)
	}
	if m := read(tb, h); !bytes.Equal(m, []byte{msgPeerJoined}) {
		tb.Fatalf("host got % x instead of PeerJoined", m)
	}
	return h, g
}

// expectLeft reads PeerLeft and then the end of the connection.
func expectLeft(tb testing.TB, c *ws.Conn) {
	tb.Helper()
	if m := read(tb, c); !bytes.Equal(m, []byte{msgPeerLeft}) {
		tb.Fatalf("got % x instead of PeerLeft", m)
	}
	expectClosed(tb, c)
}

func expectClosed(tb testing.TB, c *ws.Conn) {
	tb.Helper()
	for {
		msg, err := c.ReadMessage(nil)
		if err == nil {
			tb.Fatalf("got % x instead of the end of the connection", msg)
		}
		if errors.Is(err, ws.ErrClosed) || errors.Is(err, io.EOF) {
			return
		}
		tb.Fatalf("connection ended with %v, not a close", err)
	}
}

// waitFor polls cond for up to five seconds.
func waitFor(tb testing.TB, what string, cond func() bool) {
	tb.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	tb.Fatalf("timed out waiting for %s", what)
}

func status(tb testing.TB, url string, header http.Header) int {
	tb.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet,
		strings.Replace(url, "ws:", "http:", 1), http.NoBody)
	if err != nil {
		tb.Fatal(err)
	}
	maps.Copy(req.Header, header)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		tb.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

func payload(i, n int) []byte {
	p := make([]byte, n)
	for j := range p {
		p[j] = byte(i + j)
	}
	return p
}

func TestForwarding(t *testing.T) {
	r := start(t, Config{})
	h, g := pair(t, r)
	const count = 200
	sizes := func(i int) int { return []int{1, 2048, 7, 125, 126, 1000}[i%6] }
	var wg sync.WaitGroup
	for _, pair := range [][2]*ws.Conn{{h, g}, {g, h}} {
		from, to := pair[0], pair[1]
		wg.Go(func() {
			for i := range count {
				if err := from.WriteMessage(payload(i, sizes(i))); err != nil {
					t.Error(err)
					return
				}
			}
		})
		wg.Go(func() {
			for i := range count {
				msg, err := to.ReadMessage(nil)
				if err != nil {
					t.Error(err)
					return
				}
				want := append([]byte{msgData}, payload(i, sizes(i))...)
				if !bytes.Equal(msg, want) {
					t.Errorf("message %d: %d bytes, want %d", i, len(msg), len(want))
					return
				}
			}
		})
	}
	wg.Wait()
}

func TestHostMessagesBeforePairingAreDropped(t *testing.T) {
	r := start(t, Config{})
	ctx, cancel := context.WithTimeout(t.Context(), clientOpts.ReadTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		strings.Replace(r.url, "ws:", "http:", 1)+"/new", http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Sec-WebSocket-Version", "13")
	req.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	h, ok := resp.Body.(io.ReadWriteCloser)
	if resp.StatusCode != http.StatusSwitchingProtocols || !ok {
		t.Fatalf("host upgrade: %s", resp.Status)
	}
	stop := context.AfterFunc(ctx, func() { _ = h.Close() })
	defer stop()
	recv := func(want string) {
		t.Helper()
		buf := make([]byte, len(want))
		if _, err := io.ReadFull(h, buf); err != nil || string(buf) != want {
			t.Fatalf("host got % x, want % x: %v", buf, want, err)
		}
	}
	write := func(frame string) {
		t.Helper()
		if _, err := io.WriteString(h, frame); err != nil {
			t.Fatal(err)
		}
	}
	recv("\x82\x08\x01\x00")
	var room [6]byte
	if _, err := io.ReadFull(h, room[:]); err != nil {
		t.Fatal(err)
	}
	code := string(room[:])
	// Pong proves the relay processed the previous data frame before the join.
	write("\x82\x85\x00\x00\x00\x00early\x89\x80\x00\x00\x00\x00")
	recv("\x8a\x00")
	g := dial(t, r.url+"/join?room="+strings.ToLower(code))
	read(t, g)
	recv("\x82\x01\x02")
	write("\x82\x84\x00\x00\x00\x00late")
	if m := read(t, g); string(m) != "\x04late" {
		t.Fatalf("guest got %q", m)
	}
}

func TestPeerLeft(t *testing.T) {
	r := start(t, Config{})
	for leaver := range 2 {
		h, g := pair(t, r)
		conns := [2]*ws.Conn{h, g}
		conns[leaver].Close()
		expectLeft(t, conns[1-leaver])
	}
	waitFor(t, "rooms to close", func() bool { return r.rooms() == 0 })
}

func TestJoinStatus(t *testing.T) {
	r := start(t, Config{})
	if got := status(t, r.url+"/join?room=ABCDEF", nil); got != http.StatusNotFound {
		t.Errorf("unknown room: status %d", got)
	}
	if got := status(t, r.url+"/join", nil); got != http.StatusNotFound {
		t.Errorf("no room: status %d", got)
	}
	_, code := host(t, r)
	dial(t, r.url+"/join?room="+code)
	if got := status(t, r.url+"/join?room="+code, nil); got != http.StatusConflict {
		t.Errorf("full room: status %d", got)
	}
	// A join that fails to upgrade gives the slot back.
	_, code = host(t, r)
	if got := status(t, r.url+"/join?room="+code, nil); got != http.StatusBadRequest {
		t.Errorf("join without upgrade: status %d", got)
	}
	g := dial(t, r.url+"/join?room="+code)
	if w := read(t, g); w[0] != msgWelcome {
		t.Fatalf("guest after a failed join got % x", w)
	}
}

func TestOrigins(t *testing.T) {
	r := start(t, Config{Origins: []string{"https://play.example"}})
	h := http.Header{
		"Upgrade":               {"websocket"},
		"Connection":            {"Upgrade"},
		"Sec-Websocket-Version": {"13"},
		"Sec-Websocket-Key":     {"dGhlIHNhbXBsZSBub25jZQ=="},
		"Origin":                {"https://evil.example"},
	}
	if got := status(t, r.url+"/new", h); got != http.StatusForbidden {
		t.Fatalf("foreign origin: status %d", got)
	}
	if r.rooms() != 0 {
		t.Fatal("a rejected upgrade left a room open")
	}
	host(t, r) // Programs send no Origin and are allowed.
}

func TestMaxRooms(t *testing.T) {
	r := start(t, Config{MaxRooms: 2})
	h, _ := host(t, r)
	host(t, r)
	if got := status(t, r.url+"/new", nil); got != http.StatusServiceUnavailable {
		t.Fatalf("room beyond the cap: status %d", got)
	}
	h.Close()
	waitFor(t, "a room to free", func() bool { return r.rooms() == 1 })
	host(t, r)
}

func TestMessageLimits(t *testing.T) {
	r := start(t, Config{MaxMessage: 100})
	for _, msg := range [][]byte{nil, make([]byte, 101)} {
		h, g := pair(t, r)
		send(t, g, make([]byte, 100))
		if m := read(t, h); len(m) != 101 {
			t.Fatalf("largest message forwarded as %d bytes", len(m))
		}
		send(t, g, msg)
		expectClosed(t, g)
		expectLeft(t, h)
	}
}

func TestRateLimit(t *testing.T) {
	r := start(t, Config{Rate: 20})
	h, g := pair(t, r)
	// The bucket holds one second's worth, so a fast burst of 40 breaks the
	// limit after about 20 messages.
	for i := range 40 {
		if err := g.WriteMessage([]byte{byte(i)}); err != nil {
			break
		}
	}
	forwarded := 0
	for m := read(t, h); m[0] == msgData; m = read(t, h) {
		forwarded++
	}
	if forwarded < 20 || forwarded >= 40 {
		t.Fatalf("%d of 40 burst messages forwarded at a rate of 20", forwarded)
	}
	expectClosed(t, h)
	expectClosed(t, g)
}

func TestRateRefills(t *testing.T) {
	r := start(t, Config{Rate: 50})
	h, g := pair(t, r)
	// 80 messages at 40 per second stay within a rate of 50.
	for i := range 80 {
		send(t, g, []byte{byte(i)})
		if m := read(t, h); m[1] != byte(i) {
			t.Fatalf("message %d arrived as % x", i, m)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func TestExpiry(t *testing.T) {
	r := start(t, Config{Idle: 150 * time.Millisecond, JoinWindow: 100 * time.Millisecond})
	h, _ := host(t, r)
	start := time.Now()
	expectClosed(t, h)
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("unjoined room closed after %v", d)
	}

	// Traffic keeps a room open past its idle time, and silence closes it.
	h, g := pair(t, r)
	for range 6 {
		time.Sleep(50 * time.Millisecond)
		send(t, h, []byte("x"))
		read(t, g)
	}
	expectClosed(t, g)
	expectClosed(t, h)
	waitFor(t, "rooms to close", func() bool { return r.rooms() == 0 })
}

func TestConcurrentRoomsAndCleanup(t *testing.T) {
	baseline := runtime.NumGoroutine()
	r := start(t, Config{})
	var wg sync.WaitGroup
	for room := range 20 {
		wg.Go(func() {
			h, g := pair(t, r)
			for i := range 10 {
				send(t, h, []byte(fmt.Sprint(room, i)))
				send(t, g, []byte(fmt.Sprint(i, room)))
				if m := read(t, g); string(m[1:]) != fmt.Sprint(room, i) {
					t.Errorf("room %d: guest got %q", room, m)
				}
				if m := read(t, h); string(m[1:]) != fmt.Sprint(i, room) {
					t.Errorf("room %d: host got %q", room, m)
				}
			}
			g.Close()
			expectLeft(t, h)
		})
	}
	wg.Wait()
	waitFor(t, "rooms to close", func() bool { return r.rooms() == 0 })
	h, _ := host(t, r)
	r.Close()
	expectClosed(t, h)
	if got := status(t, r.url+"/new", nil); got != http.StatusServiceUnavailable {
		t.Errorf("new room after Close: status %d", got)
	}
	// Every connection goroutine has returned once the count is back near the
	// baseline; the HTTP client keeps a few idle connections.
	waitFor(t, "connection goroutines to exit", func() bool {
		http.DefaultClient.CloseIdleConnections()
		return runtime.NumGoroutine() <= baseline+2
	})
}
