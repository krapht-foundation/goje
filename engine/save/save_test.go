package save_test

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc64"
	"strings"
	"testing"

	"github.com/krapht-foundation/goje/engine/replay"
	"github.com/krapht-foundation/goje/engine/save"
)

var meta = replay.Meta{Game: "outpost", Content: 0xfedcba9876543210}

func TestRoundTrip(t *testing.T) {
	for _, payload := range [][]byte{nil, []byte("state"), bytes.Repeat([]byte{0xa5}, save.MaxPayloadSize)} {
		data, err := save.Encode(meta, payload)
		if err != nil {
			t.Fatal(err)
		}
		gotMeta, got, err := save.Decode(data)
		if err != nil {
			t.Fatal(err)
		}
		if gotMeta != meta || !bytes.Equal(got, payload) {
			t.Fatalf("decoded %+v and %d bytes", gotMeta, len(got))
		}
		if err := gotMeta.Check(meta); err != nil {
			t.Fatal(err)
		}
	}
}

func TestEncodeRejects(t *testing.T) {
	if _, err := save.Encode(replay.Meta{Game: "Bad"}, nil); err == nil {
		t.Error("accepted a bad game name")
	}
	if _, err := save.Encode(meta, make([]byte, save.MaxPayloadSize+1)); err == nil {
		t.Error("accepted an oversized payload")
	}
}

func TestDecodeRejectsEveryCorruptByte(t *testing.T) {
	valid, err := save.Encode(meta, []byte("snapshot bytes"))
	if err != nil {
		t.Fatal(err)
	}
	// Covers the magic, version, game, content, length, payload, and checksum.
	for i := range valid {
		for _, mask := range []byte{0x01, 0x80, 0xff} {
			data := bytes.Clone(valid)
			data[i] ^= mask
			if _, _, err := save.Decode(data); err == nil {
				t.Errorf("accepted byte %d xor %#x", i, mask)
			}
		}
	}
	for n := range len(valid) {
		if _, _, err := save.Decode(valid[:n]); err == nil {
			t.Errorf("accepted a %d byte prefix", n)
		}
	}
	if _, _, err := save.Decode(append(bytes.Clone(valid), 0)); err == nil {
		t.Error("accepted trailing data")
	}
	if _, _, err := save.Decode(
		make([]byte, 2<<20),
	); err == nil ||
		!strings.Contains(err.Error(), "larger") {
		t.Errorf("oversized: got %v", err)
	}
}

// TestDecodeRejectsWellSummedFields checks the field rules that a matching
// checksum does not excuse.
func TestDecodeRejectsWellSummedFields(t *testing.T) {
	valid, err := save.Encode(meta, []byte("abc"))
	if err != nil {
		t.Fatal(err)
	}
	resum := func(edit func([]byte) []byte) []byte {
		body := edit(bytes.Clone(valid[:len(valid)-8]))
		return binary.LittleEndian.AppendUint64(body, crc64.Checksum(body, crc64.MakeTable(crc64.ECMA)))
	}
	lengthAt := 8 + 2 + 1 + len(meta.Game) + 8
	for name, tc := range map[string]struct {
		data []byte
		want string
	}{
		"version": {resum(func(b []byte) []byte { b[8] = 2; return b }), "unsupported version 2"},
		"game":    {resum(func(b []byte) []byte { b[11] = 'X'; return b }), "game name"},
		"long game length": {
			resum(func(b []byte) []byte { b[10] = 200; return b }), "truncated header",
		},
		"short payload": {
			resum(func(b []byte) []byte { binary.LittleEndian.PutUint32(b[lengthAt:], 4); return b }), "does not match",
		},
		"huge payload length": {
			resum(func(b []byte) []byte { binary.LittleEndian.PutUint32(b[lengthAt:], 1<<31); return b }), "does not match",
		},
		"trailing payload": {resum(func(b []byte) []byte { return append(b, 0) }), "does not match"},
	} {
		if _, _, err := save.Decode(tc.data); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: got %v, want %q", name, err, tc.want)
		}
	}
}

func TestCheckRefusesOtherContent(t *testing.T) {
	data, err := save.Encode(meta, []byte("x"))
	if err != nil {
		t.Fatal(err)
	}
	got, _, err := save.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []replay.Meta{{Game: "other", Content: meta.Content}, {Game: meta.Game}} {
		if err := got.Check(want); !errors.Is(err, replay.ErrContentMismatch) {
			t.Errorf("%+v: got %v", want, err)
		}
	}
}

func FuzzDecode(f *testing.F) {
	valid, err := save.Encode(meta, []byte("snapshot"))
	if err != nil {
		f.Fatal(err)
	}
	f.Add(valid)
	f.Fuzz(func(t *testing.T, data []byte) {
		meta, payload, err := save.Decode(data)
		if err != nil {
			return
		}
		again, err := save.Encode(meta, payload)
		if err != nil || !bytes.Equal(again, data) {
			t.Fatalf("decoded save does not re-encode to the same bytes: %v", err)
		}
	})
}
