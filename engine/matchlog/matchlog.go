// Package matchlog records the command payloads and state checkpoints of a
// lockstep match. It owns the bounded binary format, not the commands or the
// simulation. See specs/match-log-v1.md.
package matchlog

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/krapht-foundation/goje/engine/lockstep"
	"github.com/krapht-foundation/goje/engine/replay"
)

const (
	// MaxEncodedSize is the largest complete log, in bytes.
	MaxEncodedSize = 2 << 20
	// MaxTicks is the largest number of ticks in a log.
	MaxTicks = 1 << 20
	// Interval is the interval between checkpoints, in ticks.
	Interval = 60

	magic       = "GML\x01"
	fixedHeader = 21
)

// ErrFull means the recorder kept the longest prefix that fits its limits.
var ErrFull = errors.New("match log is full")

// Mode identifies how players control teams.
type Mode uint8

const (
	Cooperative Mode = iota
	Versus
)

// Header identifies the exact content and options used to start the match.
type Header struct {
	replay.Meta
	ContentName string
	Mode        Mode
	Tactics     bool
	Players     int
}

func (h Header) validate() error {
	if err := h.Validate(); err != nil {
		return err
	}
	if h.ContentName == "" || len(h.ContentName) > 32 {
		return errors.New("invalid content name length")
	}
	for i, c := range []byte(h.ContentName) {
		alpha := c >= 'a' && c <= 'z'
		suffix := i > 0 && (c >= '0' && c <= '9' || c == '-')
		if !alpha && !suffix {
			return errors.New("invalid content name")
		}
	}
	if h.Mode > Versus || h.Mode == Versus && h.Tactics || h.Players < 2 || h.Players > 4 {
		return errors.New("invalid match options")
	}
	return nil
}

// Recorder holds a bounded log of completed ticks. Its zero value is invalid.
type Recorder struct {
	header   Header
	body     []byte
	empty    int // Offset of the current empty run, or -1.
	ticks    int
	hashes   []byte
	marks    []recordMark
	lastHash uint64
	full     bool
}

// New starts a recorder for the given initial match.
func New(header Header) (*Recorder, error) {
	if err := header.validate(); err != nil {
		return nil, fmt.Errorf("new match log: %w", err)
	}
	return &Recorder{header: header, empty: -1}, nil
}

// Append records one completed tick. It copies the payloads and does not
// retain the caller's slices. If the log fills, its recorded prefix is intact.
func (r *Recorder) Append(payloads [][]byte, hash uint64) error {
	if len(payloads) != r.header.Players {
		return errors.New("match log: wrong player count")
	}
	size := 0
	for _, payload := range payloads {
		if len(payload) > lockstep.MaxPayload {
			return errors.New("match log: oversized payload")
		}
		size += len(payload)
	}
	delta := 4
	if size != 0 {
		delta += 2*len(payloads) + size
	} else if r.empty >= 0 {
		delta = 0
	}
	ticks := r.ticks + 1
	headerSize := fixedHeader + len(r.header.Game) + len(r.header.ContentName)
	if r.full || ticks > MaxTicks ||
		headerSize+len(r.body)+delta+8*checkpointCount(ticks) > MaxEncodedSize {
		r.full = true
		return ErrFull
	}
	if size == 0 {
		if r.empty < 0 {
			r.empty = len(r.body)
			r.body = binary.LittleEndian.AppendUint32(r.body, 1)
		} else {
			run := r.body[r.empty:]
			binary.LittleEndian.PutUint32(run, binary.LittleEndian.Uint32(run)+1)
		}
	} else {
		r.empty = -1
		r.body = binary.LittleEndian.AppendUint32(r.body, 0)
		for _, payload := range payloads {
			r.body = binary.LittleEndian.AppendUint16(
				r.body,
				uint16(len(payload)),
			) // #nosec G115 -- bounded above.
			r.body = append(r.body, payload...)
		}
	}
	r.ticks, r.lastHash = ticks, hash
	if ticks%Interval == 0 {
		r.hashes = binary.LittleEndian.AppendUint64(r.hashes, hash)
		r.marks = append(r.marks, r.mark())
	}
	return nil
}

// Encode returns an independently owned log of the recorded prefix, including
// its final checkpoint even when the last tick was not a regular checkpoint.
func (r *Recorder) Encode() []byte {
	h := r.header
	size := fixedHeader + len(h.Game) + len(h.ContentName) + len(r.body) + 8*checkpointCount(r.ticks)
	data := make([]byte, 0, size)
	data = appendHeader(data, h, r.ticks)
	data = append(data, r.body...)
	data = append(data, r.hashes...)
	if r.ticks%Interval != 0 {
		data = binary.LittleEndian.AppendUint64(data, r.lastHash)
	}
	return data
}

func appendHeader(data []byte, h Header, ticks int) []byte {
	data = append(data, magic...)
	var flags byte
	if h.Tactics {
		flags = 1
	}
	data = append(
		data,
		byte(h.Mode),
		flags,
		byte(h.Players),
		byte(len(h.Game)),
		byte(len(h.ContentName)),
	) // #nosec G115 -- validated in New.
	data = binary.LittleEndian.AppendUint64(data, h.Content)
	data = binary.LittleEndian.AppendUint32(
		data,
		uint32(ticks),
	) // #nosec G115 -- bounded in Append.
	data = append(data, h.Game...)
	data = append(data, h.ContentName...)
	return data
}

func checkpointCount(ticks int) int { return (ticks + Interval - 1) / Interval }

// Log is a validated, immutable view of a match log.
type Log struct {
	header       Header
	ticks        int
	body, hashes []byte
}

// Header returns the match options and content check.
func (l Log) Header() Header { return l.header }

// Ticks returns the number of completed ticks.
func (l Log) Ticks() int { return l.ticks }

// Decode validates a log and borrows its bytes. The caller must not modify
// them while the log or its readers are in use.
func Decode(data []byte) (Log, error) {
	if len(data) < fixedHeader || len(data) > MaxEncodedSize || string(data[:4]) != magic {
		return Log{}, errors.New("match log: invalid size, magic, or version")
	}
	n, m := int(data[7]), int(data[8])
	end := fixedHeader + n + m
	ticks := binary.LittleEndian.Uint32(data[17:21])
	if ticks > MaxTicks || n < 1 || n > 32 || m < 1 || m > 32 || len(data) < end || data[5] > 1 {
		return Log{}, errors.New("match log: invalid header")
	}
	meta := replay.Meta{
		Game:    string(data[fixedHeader : fixedHeader+n]),
		Content: binary.LittleEndian.Uint64(data[9:17]),
	}
	h := Header{
		Meta:        meta,
		ContentName: string(data[fixedHeader+n : end]),
		Mode:        Mode(data[4]),
		Tactics:     data[5] == 1,
		Players:     int(data[6]),
	}
	if err := h.validate(); err != nil {
		return Log{}, fmt.Errorf("match log: %w", err)
	}
	hashStart := len(data) - 8*checkpointCount(int(ticks))
	if hashStart < end {
		return Log{}, errors.New("match log: truncated checkpoints")
	}
	body := data[end:hashStart]
	if !validBody(body, h.Players, ticks) {
		return Log{}, errors.New("match log: invalid tick records")
	}
	return Log{header: h, ticks: int(ticks), body: body, hashes: data[hashStart:]}, nil
}

func validBody(body []byte, players int, ticks uint32) bool {
	wasEmpty := false
	for ticks != 0 {
		if len(body) < 4 {
			return false
		}
		run := binary.LittleEndian.Uint32(body)
		body = body[4:]
		if run != 0 {
			if wasEmpty || run > ticks {
				return false
			}
			ticks -= run
			wasEmpty = true
			continue
		}
		wasEmpty = false
		size := 0
		for range players {
			if len(body) < 2 {
				return false
			}
			n := int(binary.LittleEndian.Uint16(body))
			if n > lockstep.MaxPayload || n > len(body)-2 {
				return false
			}
			body = body[2+n:]
			size += n
		}
		if size == 0 {
			return false
		}
		ticks--
	}
	return len(body) == 0
}

// Reader returns an independent cursor starting at the log's first tick.
func (l Log) Reader() Reader { return Reader{log: l, body: l.body} }

// Reader iterates over a log without allocating. Do not copy it while a
// payload view returned by Next is in use.
type Reader struct {
	log         Log
	body        []byte
	tick, empty int
	payloads    [4][]byte
}

// Next advances one tick and returns the players' payloads in player order,
// or nil at the end. Treat them as read-only, valid until the next call.
func (r *Reader) Next() [][]byte {
	if r.tick == r.log.ticks {
		return nil
	}
	r.tick++
	clear(r.payloads[:])
	if r.empty == 0 {
		r.empty = int(binary.LittleEndian.Uint32(r.body)) // #nosec G115 -- validated by Decode.
		r.body = r.body[4:]
		if r.empty == 0 {
			for p := range r.log.header.Players {
				n := int(binary.LittleEndian.Uint16(r.body))
				r.payloads[p] = r.body[2 : 2+n : 2+n]
				r.body = r.body[2+n:]
			}
			return r.payloads[:r.log.header.Players]
		}
	}
	r.empty--
	return r.payloads[:r.log.header.Players]
}

// Tick is the number of ticks that Next has returned.
func (r *Reader) Tick() int { return r.tick }

// Checkpoint returns the hash for the most recent tick when it is a regular
// checkpoint or the last tick of the match.
func (r *Reader) Checkpoint() (uint64, bool) {
	if r.tick == 0 || r.tick%Interval != 0 && r.tick != r.log.ticks {
		return 0, false
	}
	return binary.LittleEndian.Uint64(r.log.hashes[8*(checkpointCount(r.tick)-1):]), true
}
