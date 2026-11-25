package ws

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"os"
	"testing"
	"time"
)

// fakeConn reads from a fixed input, records what is written, and counts
// deadlines, so frame tests need no peer.
type fakeConn struct {
	in             io.Reader
	out            bytes.Buffer
	readDeadlines  int
	writeDeadlines int
	closed         bool
}

func (f *fakeConn) Read(p []byte) (int, error)  { return f.in.Read(p) }
func (f *fakeConn) Write(p []byte) (int, error) { return f.out.Write(p) }
func (f *fakeConn) Close() error                { f.closed = true; return nil }
func (*fakeConn) LocalAddr() net.Addr           { return nil }
func (*fakeConn) RemoteAddr() net.Addr          { return nil }
func (*fakeConn) SetDeadline(time.Time) error   { return nil }
func (f *fakeConn) SetReadDeadline(time.Time) error {
	f.readDeadlines++
	return nil
}

func (f *fakeConn) SetWriteDeadline(time.Time) error {
	f.writeDeadlines++
	return nil
}

func newFake(input []byte, client bool, opts Options) (*Conn, *fakeConn) {
	fc := &fakeConn{in: bytes.NewReader(input)}
	return newConn(fc, bufio.NewReader(fc), client, opts), fc
}

var testKey = [4]byte{0x37, 0xFA, 0x21, 0x3D}

// frame encodes a frame with the minimal length form, masked with testKey
// when masked is set.
func frame(b0 byte, masked bool, payload []byte) []byte {
	return frameLen(b0, masked, payload, -1)
}

// frameLen encodes a frame whose length uses the form of form bytes (0, 2,
// or 8), or the minimal form when form is negative.
func frameLen(b0 byte, masked bool, payload []byte, form int) []byte {
	n := len(payload)
	if form < 0 {
		switch {
		case n < 126:
			form = 0
		case n <= 0xFFFF:
			form = 2
		default:
			form = 8
		}
	}
	var maskBit byte
	if masked {
		maskBit = 0x80
	}
	b := []byte{b0}
	switch form {
	case 0:
		b = append(b, maskBit|byte(n))
	case 2:
		b = binary.BigEndian.AppendUint16(append(b, maskBit|126), uint16(n))
	default:
		b = binary.BigEndian.AppendUint64(append(b, maskBit|127), uint64(n))
	}
	if !masked {
		return append(b, payload...)
	}
	b = append(b, testKey[:]...)
	start := len(b)
	b = append(b, payload...)
	mask(b[start:], testKey)
	return b
}

// header encodes only a frame header with an explicit length field.
func header(b0, b1 byte, ext ...byte) []byte { return append([]byte{b0, b1}, ext...) }

// closeStatus returns the status of the close frame a conn wrote, or -1.
func closeStatus(tb testing.TB, out []byte, masked bool) int {
	tb.Helper()
	r, _ := newFake(out, !masked, Options{})
	for {
		op, n, err := r.readHeader()
		if err != nil {
			return -1
		}
		p := make([]byte, n)
		if err := r.readPayload(p); err != nil {
			return -1
		}
		if op == opClose && n >= 2 {
			return int(binary.BigEndian.Uint16(p))
		}
	}
}

func TestAcceptKeyRFCExample(t *testing.T) {
	// RFC 6455 section 1.3.
	if got := AcceptKey("dGhlIHNhbXBsZSBub25jZQ=="); got != "s3pPLMBiTxaQ9kYGzzhZRbK+xOo=" {
		t.Fatalf("AcceptKey = %q", got)
	}
}

func TestReadRejects(t *testing.T) {
	big := make([]byte, 126)
	cases := []struct {
		name   string
		client bool // Read as the client, so frames must be unmasked.
		limit  int
		input  []byte
		want   error
		status int
	}{
		{"rsv1", false, 0, frame(0xC2, true, nil), ErrProtocol, closeProtocol},
		{"rsv2", false, 0, frame(0xA2, true, nil), ErrProtocol, closeProtocol},
		{"rsv3", false, 0, frame(0x92, true, nil), ErrProtocol, closeProtocol},
		{"text", false, 0, frame(0x81, true, []byte("hi")), ErrProtocol, closeProtocol},
		{"continuation", false, 0, frame(0x80, true, nil), ErrProtocol, closeProtocol},
		{"opcode 3", false, 0, frame(0x83, true, nil), ErrProtocol, closeProtocol},
		{"opcode 7", false, 0, frame(0x87, true, nil), ErrProtocol, closeProtocol},
		{"opcode 11", false, 0, frame(0x8B, true, nil), ErrProtocol, closeProtocol},
		{"opcode 15", false, 0, frame(0x8F, true, nil), ErrProtocol, closeProtocol},
		{"fragment", false, 0, frame(0x02, true, []byte("a")), ErrProtocol, closeProtocol},
		{"fragmented ping", false, 0, frame(0x09, true, nil), ErrProtocol, closeProtocol},
		{
			"unmasked to server", false, 0, frame(0x82, false, []byte("a")), ErrProtocol,
			closeProtocol,
		},
		{"masked to client", true, 0, frame(0x82, true, []byte("a")), ErrProtocol, closeProtocol},
		{"long ping", false, 0, frame(0x89, true, big), ErrProtocol, closeProtocol},
		{"long close", true, 0, frame(0x88, false, big), ErrProtocol, closeProtocol},
		{"one-byte close", false, 0, frame(0x88, true, []byte{3}), ErrProtocol, closeProtocol},
		{
			"7-bit over limit", false, 10, frame(0x82, true, make([]byte, 11)), ErrTooBig,
			closeTooBig,
		},
		{"16-bit over limit", false, 0, header(0x82, 0xFE, 0x08, 0x01), ErrTooBig, closeTooBig},
		{"16-bit max", true, 0, header(0x82, 0x7E, 0xFF, 0xFF), ErrTooBig, closeTooBig},
		{
			"64-bit over limit", true, 0, header(0x82, 0x7F, 0, 0, 0, 0, 0, 1, 0, 0), ErrTooBig,
			closeTooBig,
		},
		{
			"64-bit max", true, 0, header(0x82, 0x7F, 0x7F, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF),
			ErrTooBig, closeTooBig,
		},
		{
			"64-bit sign bit", true, 0, header(0x82, 0x7F, 0x80, 0, 0, 0, 0, 0, 0, 1), ErrProtocol,
			closeProtocol,
		},
		{"16-bit not minimal", true, 0, header(0x82, 0x7E, 0, 125), ErrProtocol, closeProtocol},
		{
			"64-bit not minimal", true, 0, header(0x82, 0x7F, 0, 0, 0, 0, 0, 0, 0xFF, 0xFF),
			ErrProtocol, closeProtocol,
		},
		{
			"64-bit close", true, 0, header(0x88, 0x7F, 0, 0, 0, 0, 0, 1, 0, 0), ErrProtocol,
			closeProtocol,
		},
		{"truncated header", true, 0, []byte{0x82}, io.ErrUnexpectedEOF, closeGoingAway},
		{"truncated length", true, 0, header(0x82, 0x7E, 1), io.ErrUnexpectedEOF, closeGoingAway},
		{
			"truncated mask", false, 0, header(0x82, 0x81, 1, 2), io.ErrUnexpectedEOF,
			closeGoingAway,
		},
		{"truncated payload", true, 0, header(0x82, 3, 1), io.ErrUnexpectedEOF, closeGoingAway},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, fc := newFake(tc.input, tc.client, Options{Limit: tc.limit})
			buf := make([]byte, 0, 16)
			msg, err := c.ReadMessage(buf)
			if !errors.Is(err, tc.want) {
				t.Fatalf("ReadMessage = %d bytes, %v; want %v", len(msg), err, tc.want)
			}
			if got := closeStatus(t, fc.out.Bytes(), tc.client); got != tc.status {
				t.Errorf("close status %d, want %d", got, tc.status)
			}
			if !fc.closed {
				t.Error("connection left open")
			}
		})
	}
}

func TestReadLengthForms(t *testing.T) {
	cases := []struct {
		n, form, limit int
	}{
		{0, 0, 0},
		{1, 0, 0},
		{125, 0, 0},
		{126, 2, 0},
		{127, 2, 0},
		{2048, 2, 0},
		{0xFFFF, 2, 0xFFFF},
		{0x10000, 8, 0x10000},
	}
	for _, tc := range cases {
		for _, client := range []bool{false, true} {
			payload := make([]byte, tc.n)
			for i := range payload {
				payload[i] = byte(i * 7)
			}
			c, _ := newFake(frameLen(0x82, !client, payload, tc.form), client,
				Options{Limit: tc.limit})
			buf := make([]byte, 0, 2048)
			msg, err := c.ReadMessage(buf)
			if err != nil || !bytes.Equal(msg, payload) {
				t.Fatalf("%d bytes in form %d, client %v: %d bytes, %v", tc.n, tc.form, client,
					len(msg), err)
			}
			if tc.n > 0 && tc.n <= cap(buf) && &msg[0] != &buf[:1][0] {
				t.Errorf("%d bytes: payload not read into the caller's buffer", tc.n)
			}
		}
	}
}

func TestWriteMasking(t *testing.T) {
	payload := []byte("goje lockstep")
	for _, client := range []bool{false, true} {
		w, fc := newFake(nil, client, Options{})
		if err := w.WriteMessage(payload); err != nil {
			t.Fatal(err)
		}
		raw := fc.out.Bytes()
		if masked := raw[1]&0x80 != 0; masked != client {
			t.Fatalf("client %v: mask bit %v", client, masked)
		}
		if client && bytes.Contains(raw, payload) {
			t.Error("client payload sent unmasked")
		}
		r, _ := newFake(raw, !client, Options{})
		msg, err := r.ReadMessage(nil)
		if err != nil || !bytes.Equal(msg, payload) {
			t.Fatalf("client %v: read back %q, %v", client, msg, err)
		}
		if fc.writeDeadlines != 1 {
			t.Errorf("client %v: %d write deadlines for one frame", client, fc.writeDeadlines)
		}
	}
	// Each length form is encoded minimally.
	for _, n := range []int{125, 126, 0xFFFF, 0x10000} {
		w, fc := newFake(nil, false, Options{})
		if err := w.WriteMessage(make([]byte, n)); err != nil {
			t.Fatal(err)
		}
		if want := frame(0x82, false, make([]byte, n)); !bytes.Equal(fc.out.Bytes(), want) {
			t.Errorf("%d bytes: frame header % x", n, fc.out.Bytes()[:4])
		}
	}
}

// pipePair returns a server Conn on one end of a pipe and the raw other end.
func pipePair(tb testing.TB, opts Options) (server *Conn, raw net.Conn) {
	tb.Helper()
	a, b := net.Pipe()
	tb.Cleanup(func() {
		a.Close()
		b.Close()
	})
	return newConn(a, bufio.NewReader(a), false, opts), b
}

// readRaw reads one unmasked frame from a raw pipe end.
func readRaw(tb testing.TB, raw net.Conn) (op byte, payload []byte) {
	tb.Helper()
	c := newConn(raw, bufio.NewReader(raw), true, Options{Limit: 1 << 20})
	op, n, err := c.readHeader()
	if err != nil {
		tb.Fatal(err)
	}
	p := make([]byte, n)
	if err := c.readPayload(p); err != nil {
		tb.Fatal(err)
	}
	return op, p
}

type result struct {
	msg []byte
	err error
}

func readAsync(c *Conn) chan result {
	ch := make(chan result, 1)
	go func() {
		msg, err := c.ReadMessage(nil)
		ch <- result{msg, err}
	}()
	return ch
}

func TestPingPongClose(t *testing.T) {
	s, raw := pipePair(t, Options{})
	got := readAsync(s)
	if _, err := raw.Write(frame(0x89, true, []byte("tick"))); err != nil {
		t.Fatal(err)
	}
	if op, p := readRaw(t, raw); op != opPong || string(p) != "tick" {
		t.Fatalf("answer to ping: opcode %d %q", op, p)
	}
	if _, err := raw.Write(frame(0x8A, true, []byte("ignored"))); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Write(frame(0x82, true, []byte("data"))); err != nil {
		t.Fatal(err)
	}
	if r := <-got; r.err != nil || string(r.msg) != "data" {
		t.Fatalf("ReadMessage = %q, %v", r.msg, r.err)
	}

	got = readAsync(s)
	if _, err := raw.Write(frame(0x88, true, []byte{0x0F, 0xA0, 'b', 'y', 'e'})); err != nil {
		t.Fatal(err)
	}
	if op, p := readRaw(t, raw); op != opClose || !bytes.Equal(p, []byte{0x0F, 0xA0}) {
		t.Fatalf("answer to close: opcode %d % x", op, p)
	}
	if r := <-got; !errors.Is(r.err, ErrClosed) {
		t.Fatalf("ReadMessage after close = %v", r.err)
	}
	if _, err := raw.Read(make([]byte, 1)); !errors.Is(err, io.EOF) {
		t.Fatalf("connection still open after close: %v", err)
	}
	if err := s.WriteMessage([]byte("late")); err == nil {
		t.Fatal("write after close succeeded")
	}
}

func TestEmptyClose(t *testing.T) {
	s, raw := pipePair(t, Options{})
	got := readAsync(s)
	if _, err := raw.Write(frame(0x88, true, nil)); err != nil {
		t.Fatal(err)
	}
	if op, p := readRaw(t, raw); op != opClose || binary.BigEndian.Uint16(p) != closeNormal {
		t.Fatalf("answer to empty close: opcode %d % x", op, p)
	}
	if r := <-got; !errors.Is(r.err, ErrClosed) {
		t.Fatalf("ReadMessage = %v", r.err)
	}
}

func TestDeadlines(t *testing.T) {
	c, fc := newFake(frame(0x82, true, []byte("x")), false, Options{ReadTimeout: time.Second})
	if _, err := c.ReadMessage(nil); err != nil {
		t.Fatal(err)
	}
	if fc.readDeadlines != 1 {
		t.Errorf("%d read deadlines, want 1", fc.readDeadlines)
	}

	s, _ := pipePair(t, Options{ReadTimeout: 20 * time.Millisecond})
	start := time.Now()
	if _, err := s.ReadMessage(nil); !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("idle read = %v", err)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("read deadline took %v", d)
	}

	// Nobody reads the pipe, so the write must time out.
	w, _ := pipePair(t, Options{WriteTimeout: 20 * time.Millisecond})
	if err := w.WriteMessage([]byte("x")); !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("blocked write = %v", err)
	}
	// Close does not wait for the full write timeout of a peer that reads
	// nothing.
	w2, _ := pipePair(t, Options{})
	start = time.Now()
	if err := w2.Close(); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > closeTimeout+time.Second {
		t.Errorf("Close took %v", d)
	}
}

func FuzzReadFrame(f *testing.F) {
	f.Add(frame(0x82, true, []byte("seed")), false)
	f.Add(frame(0x82, false, []byte("seed")), true)
	f.Add(append(frame(0x89, true, []byte("p")), frame(0x82, true, nil)...), false)
	f.Add(frame(0x88, true, []byte{3, 232}), false)
	f.Add(header(0x82, 0xFF, 0, 0, 0, 0, 0, 0, 0, 64), false)
	f.Add(header(0x82, 0x7E, 0, 64), true)
	f.Fuzz(func(t *testing.T, input []byte, client bool) {
		const limit = 64
		c, fc := newFake(input, client, Options{Limit: limit})
		buf := make([]byte, 0, limit)
		for range 4 {
			msg, err := c.ReadMessage(buf)
			if err != nil {
				if !fc.closed {
					t.Fatal("error left the connection open")
				}
				return
			}
			if len(msg) > limit || (len(msg) > 0 && &msg[0] != &buf[:1][0]) {
				t.Fatalf("message of %d bytes outside the caller's buffer", len(msg))
			}
		}
	})
}
