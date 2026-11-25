package matchlog

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"

	"github.com/krapht-foundation/goje/engine/lockstep"
	"github.com/krapht-foundation/goje/engine/replay"
)

var (
	testMeta   = replay.Meta{Game: "outpost", Content: 42}
	testHeader = Header{
		Meta:        testMeta,
		ContentName: "versus-1",
		Mode:        Versus,
		Players:     2,
	}
)

func testLog(tb testing.TB, ticks int) []byte {
	tb.Helper()
	r, err := New(testHeader)
	if err != nil {
		tb.Fatal(err)
	}
	var payload [2][]byte
	for i := 1; i <= ticks; i++ {
		if i%10 == 0 {
			payload[1] = []byte{byte(i % 256), 255}
		} else {
			payload[1] = nil
		}
		if err := r.Append(payload[:], uint64(i)*7); err != nil {
			tb.Fatal(err)
		}
		if len(payload[1]) > 0 {
			payload[1][1] = 0 // The writer must not retain lockstep's ring buffer.
		}
	}
	return r.Encode()
}

func TestLogRoundTripWrapsEmptyRunsAcrossCheckpoints(t *testing.T) {
	const ticks = Interval*2 + 5
	data := testLog(t, ticks)
	l, err := Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	if l.Header() != testHeader || l.Ticks() != ticks {
		t.Fatalf("got header %+v, %d ticks", l.Header(), l.Ticks())
	}
	r := l.Reader()
	if _, ok := r.Checkpoint(); ok {
		t.Fatal("found a checkpoint at tick zero")
	}
	for i := 1; i <= ticks; i++ {
		got := r.Next()
		var want []byte
		if i%10 == 0 {
			want = []byte{byte(i % 256), 255}
		}
		if len(got) != 2 || len(got[0]) != 0 || !bytes.Equal(got[1], want) {
			t.Fatalf("tick %d: payloads %v, want no player 0 payload and %v for player 1", i, got, want)
		}
		if hash, ok := r.Checkpoint(); ok != (i%Interval == 0 || i == ticks) ||
			ok && hash != uint64(i)*7 {
			t.Fatalf("tick %d: checkpoint %d present=%v", i, hash, ok)
		}
	}
	if r.Next() != nil || r.Tick() != ticks {
		t.Fatal("reader advanced past the last tick")
	}
}

func TestDecodeRejectsNoncanonicalLogs(t *testing.T) {
	data := testLog(t, 11)
	body := fixedHeader + len(testHeader.Game) + len(testHeader.ContentName)
	for name, change := range map[string]func([]byte) []byte{
		"unknown version":                func(b []byte) []byte { b[3]++; return b },
		"unknown mode":                   func(b []byte) []byte { b[4] = 2; return b },
		"unknown flags":                  func(b []byte) []byte { b[5] = 2; return b },
		"versus with tactics":            func(b []byte) []byte { b[5] = 1; return b },
		"one player":                     func(b []byte) []byte { b[6] = 1; return b },
		"too many players":               func(b []byte) []byte { b[6] = 5; return b },
		"empty game name":                func(b []byte) []byte { b[7] = 0; return b },
		"oversize content name":          func(b []byte) []byte { b[8] = 33; return b },
		"invalid game name":              func(b []byte) []byte { b[fixedHeader] = '/'; return b },
		"invalid content name":           func(b []byte) []byte { b[fixedHeader+len(testHeader.Game)] = '/'; return b },
		"too many ticks":                 func(b []byte) []byte { binary.LittleEndian.PutUint32(b[17:], MaxTicks+1); return b },
		"invalid empty run":              func(b []byte) []byte { b[body] = 12; return b },
		"no payload for a nonempty tick": func(b []byte) []byte { b[body] = 0; return b },
		"oversized payload": func(b []byte) []byte {
			binary.LittleEndian.PutUint16(b[body+8:], lockstep.MaxPayload+1)
			return b
		},
		"split empty runs": func(b []byte) []byte {
			b[body] = 8
			return append(append(b[:body+4:body+4], 1, 0, 0, 0), b[body+4:]...)
		},
		"trailing byte": func(b []byte) []byte { return append(b, 0) },
		"oversized log": func([]byte) []byte { return make([]byte, MaxEncodedSize+1) },
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Decode(change(bytes.Clone(data))); err == nil {
				t.Fatal("accepted malformed log")
			}
		})
	}
	for i := range len(data) {
		if _, err := Decode(data[:i]); err == nil {
			t.Fatalf("accepted prefix of %d/%d bytes", i, len(data))
		}
	}
}

func TestFullLogPreservesTheCompletedPrefix(t *testing.T) {
	for _, payload := range [][][]byte{{nil, nil}, {make([]byte, lockstep.MaxPayload), make([]byte, lockstep.MaxPayload)}} {
		r, err := New(testHeader)
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for ; n <= MaxTicks; n++ {
			if err = r.Append(payload, uint64(n+1)); err != nil {
				break
			}
		}
		if !errors.Is(err, ErrFull) || n == 0 || len(payload[0]) == 0 && n != MaxTicks {
			t.Fatalf("unexpected limit after %d ticks: %v", n, err)
		}
		if err = r.Append([][]byte{nil, nil}, 999); !errors.Is(err, ErrFull) {
			t.Fatalf("a later empty tick was accepted: %v", err)
		}
		data := r.Encode()
		l, err := Decode(data)
		if err != nil || l.Ticks() != n {
			t.Fatalf("recorded prefix: %d ticks in %d bytes, %v", l.Ticks(), len(data), err)
		}
		reader := l.Reader()
		for range n {
			reader.Next()
		}
		if hash, ok := reader.Checkpoint(); !ok || hash != uint64(n) {
			t.Fatalf("final checkpoint: %d, %v; want %d", hash, ok, n)
		}
	}
}

func FuzzDecode(f *testing.F) {
	f.Add(testLog(f, 0))
	f.Add(testLog(f, 61))
	f.Add([]byte("GML\x01"))
	f.Fuzz(func(t *testing.T, data []byte) {
		l, err := Decode(data)
		if err != nil {
			return
		}
		r, err := New(l.Header())
		if err != nil {
			t.Fatal(err)
		}
		reader := l.Reader()
		for payload := reader.Next(); payload != nil; payload = reader.Next() {
			hash, _ := reader.Checkpoint()
			if err = r.Append(payload, hash); err != nil {
				t.Fatal(err)
			}
		}
		if !bytes.Equal(r.Encode(), data) {
			t.Fatal("accepted a noncanonical log")
		}
	})
}

func BenchmarkAppendEmpty(b *testing.B) {
	var payload [2][]byte
	r, err := New(testHeader)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if r.ticks == MaxTicks {
			r.body, r.hashes = r.body[:0], r.hashes[:0]
			r.ticks, r.empty = 0, -1
		}
		if err := r.Append(payload[:], 0); err != nil {
			b.Fatal(err)
		}
	}
}
