// Package save wraps a scene snapshot in a versioned envelope that names the
// game and its content and carries a checksum, so a damaged, truncated, or
// foreign file is refused instead of loaded. The byte layout is defined in
// specs/save-v1.md.
package save

import (
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc64"

	"github.com/krapht-foundation/goje/engine/replay"
)

// MaxPayloadSize is the largest snapshot a save holds, in bytes.
const MaxPayloadSize = 1 << 20

const (
	magic   = "GOJESAVE"
	version = 1
	// headerSize counts magic, version, game length, content, and payload length.
	headerSize = len(magic) + 2 + 1 + 8 + 4
	sumSize    = 8
)

// crc is CRC-64/ECMA, which detects every error burst of up to 64 bits.
var crc = crc64.MakeTable(crc64.ECMA)

// Encode returns the version 1 save of payload for the game and content in
// meta. Compare the decoded Meta with the running game's using Meta.Check.
func Encode(meta replay.Meta, payload []byte) ([]byte, error) {
	if err := meta.Validate(); err != nil {
		return nil, fmt.Errorf("encode save: %w", err)
	}
	if len(payload) > MaxPayloadSize {
		return nil, fmt.Errorf(
			"encode save: payload of %d bytes exceeds %d",
			len(payload),
			MaxPayloadSize,
		)
	}
	data := make([]byte, 0, headerSize+len(meta.Game)+len(payload)+sumSize)
	data = append(data, magic...)
	data = binary.LittleEndian.AppendUint16(data, version)
	data = append(data, byte(len(meta.Game))) // #nosec G115 -- Validate bounded the name to 32 bytes.
	data = append(data, meta.Game...)
	data = binary.LittleEndian.AppendUint64(data, meta.Content)
	size := uint32(len(payload)) // #nosec G115 -- at most MaxPayloadSize.
	data = binary.LittleEndian.AppendUint32(data, size)
	data = append(data, payload...)
	return binary.LittleEndian.AppendUint64(data, crc64.Checksum(data, crc)), nil
}

// Decode reads a version 1 save. It refuses data with a bad magic number,
// version, game name, or checksum, a payload over MaxPayloadSize, and
// truncated or trailing bytes. The payload aliases data.
func Decode(data []byte) (replay.Meta, []byte, error) {
	meta, payload, err := decode(data)
	if err != nil {
		return replay.Meta{}, nil, fmt.Errorf("decode save: %w", err)
	}
	return meta, payload, nil
}

func decode(data []byte) (replay.Meta, []byte, error) {
	var meta replay.Meta
	if len(data) > headerSize+32+MaxPayloadSize+sumSize {
		return meta, nil, fmt.Errorf("%d bytes is larger than any save", len(data))
	}
	if len(data) < headerSize+sumSize || string(data[:len(magic)]) != magic {
		return meta, nil, errors.New("not a save")
	}
	// The checksum covers every byte before it, so check it before trusting any field.
	body := data[:len(data)-sumSize]
	if crc64.Checksum(body, crc) != binary.LittleEndian.Uint64(data[len(body):]) {
		return meta, nil, errors.New("checksum mismatch")
	}
	at := len(magic)
	if v := binary.LittleEndian.Uint16(data[at:]); v != version {
		return meta, nil, fmt.Errorf("unsupported version %d (want %d)", v, version)
	}
	at += 2
	gameEnd := at + 1 + int(data[at])
	if gameEnd+8+4 > len(body) {
		return meta, nil, errors.New("truncated header")
	}
	meta.Game = string(data[at+1 : gameEnd])
	if err := meta.Validate(); err != nil {
		return meta, nil, err
	}
	meta.Content = binary.LittleEndian.Uint64(data[gameEnd:])
	size := binary.LittleEndian.Uint32(data[gameEnd+8:])
	payload := body[gameEnd+12 : len(body) : len(body)]
	if size > MaxPayloadSize || int(size) != len(payload) {
		return meta, nil, fmt.Errorf(
			"payload length %d does not match the %d bytes present",
			size,
			len(payload),
		)
	}
	return meta, payload, nil
}
