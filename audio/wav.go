// Package audio decodes the short sound clips Goje games play, renders music
// scores to looping clips, and mixes both. It does not open files or devices;
// desktop and web hosts share the same decoder, renderer, and mixer rules.
package audio

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

const (
	// SampleRate is the only accepted clip rate, in frames per second.
	SampleRate = 48000
	// MaxWAVBytes limits the encoded file, including ancillary chunks.
	MaxWAVBytes = 300 << 10
	// MaxSoundSeconds limits a sound clip, from DecodeWAV or for EncodeWAV.
	MaxSoundSeconds = 3
	// MaxMusicSeconds limits a music clip from RenderScore.
	MaxMusicSeconds = 64

	maxSeconds       = MaxSoundSeconds
	maxPCMBytes      = MaxSoundSeconds * SampleRate * 2
	maxMusicPCMBytes = MaxMusicSeconds * SampleRate * 2
)

// Clip is a decoded or rendered mono clip. A zero Clip is invalid; DecodeWAV
// and RenderScore never return an empty Clip without an error.
type Clip struct {
	pcm []byte
}

// PCM returns packed little-endian signed 16-bit mono samples at SampleRate.
// The slice is shared and MUST NOT be modified.
func (c Clip) PCM() []byte { return c.pcm }

// DecodeWAV reads one RIFF/WAVE file of mono signed 16-bit PCM at 48 kHz,
// at most three seconds and 300 KiB. Well-formed ancillary chunks are skipped.
// It does not close r. On error the returned clip is zero.
func DecodeWAV(r io.Reader) (Clip, error) {
	pcm, err := decodeWAV(&io.LimitedReader{R: r, N: MaxWAVBytes + 1})
	if err != nil {
		return Clip{}, fmt.Errorf("decode WAV: %w", err)
	}
	return Clip{pcm: pcm}, nil
}

func decodeWAV(r *io.LimitedReader) ([]byte, error) {
	var head [12]byte
	if err := readFull(r, head[:]); err != nil {
		return nil, err
	}
	if string(head[0:4]) != "RIFF" || string(head[8:12]) != "WAVE" {
		return nil, errors.New("not a RIFF/WAVE file")
	}
	riffSize := int64(binary.LittleEndian.Uint32(head[4:8]))
	if riffSize+8 > MaxWAVBytes {
		return nil, fmt.Errorf("file exceeds %d bytes", MaxWAVBytes)
	}
	remaining := riffSize - 4
	if remaining < 0 {
		return nil, errors.New("RIFF size is smaller than its header")
	}
	var pcm []byte
	var haveFmt, haveData bool
	var chunk [8]byte
	for remaining > 0 {
		if remaining < int64(len(chunk)) {
			return nil, errors.New("partial chunk header at end of RIFF")
		}
		if err := readFull(r, chunk[:]); err != nil {
			return nil, err
		}
		id := string(chunk[0:4])
		size := int64(binary.LittleEndian.Uint32(chunk[4:8]))
		padded := size + size&1
		remaining -= int64(len(chunk))
		if padded > remaining {
			return nil, fmt.Errorf("%q chunk exceeds RIFF bounds", id)
		}
		remaining -= padded
		switch id {
		case "fmt ":
			if haveFmt {
				return nil, errors.New(`duplicate "fmt " chunk`)
			}
			haveFmt = true
			if err := readFormat(r, size); err != nil {
				return nil, err
			}
		case "data":
			if !haveFmt {
				return nil, errors.New(`"data" chunk before "fmt " chunk`)
			}
			if haveData {
				return nil, errors.New(`duplicate "data" chunk`)
			}
			haveData = true
			switch {
			case size == 0:
				return nil, errors.New("clip is empty")
			case size&1 != 0:
				return nil, errors.New("data ends with a partial sample")
			case size > maxPCMBytes:
				return nil, fmt.Errorf("clip exceeds %d seconds", maxSeconds)
			}
			// The size is bounded above, so allocating the exact chunk is safe
			// and avoids retaining ancillary bytes or copying PCM twice.
			pcm = make([]byte, size)
			if err := readFull(r, pcm); err != nil {
				return nil, err
			}
		default:
			if _, err := io.CopyN(io.Discard, r, size); err != nil {
				return nil, readErr(err)
			}
		}
		if size&1 != 0 {
			var pad [1]byte
			if err := readFull(r, pad[:]); err != nil {
				return nil, err
			}
			if pad[0] != 0 {
				return nil, fmt.Errorf("%q chunk has a nonzero pad byte", id)
			}
		}
	}
	if !haveFmt {
		return nil, errors.New(`missing "fmt " chunk`)
	}
	if !haveData {
		return nil, errors.New(`missing "data" chunk`)
	}
	var extra [1]byte
	if n, err := io.ReadFull(r, extra[:]); n != 0 {
		return nil, errors.New("unexpected data after RIFF chunk")
	} else if !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("check end of WAV: %w", err)
	}
	return pcm, nil
}

func readFormat(r io.Reader, size int64) error {
	if size != 16 {
		return fmt.Errorf(`"fmt " chunk is %d bytes, want 16`, size)
	}
	var f [16]byte
	if err := readFull(r, f[:]); err != nil {
		return err
	}
	le := binary.LittleEndian
	switch {
	case le.Uint16(f[0:2]) != 1:
		return fmt.Errorf("format tag %d is not PCM", le.Uint16(f[0:2]))
	case le.Uint16(f[2:4]) != 1:
		return fmt.Errorf("%d channels, want mono", le.Uint16(f[2:4]))
	case le.Uint32(f[4:8]) != SampleRate:
		return fmt.Errorf("sample rate %d, want %d", le.Uint32(f[4:8]), SampleRate)
	case le.Uint16(f[14:16]) != 16:
		return fmt.Errorf("%d bits per sample, want 16", le.Uint16(f[14:16]))
	case le.Uint32(f[8:12]) != SampleRate*2 || le.Uint16(f[12:14]) != 2:
		return errors.New("byte rate or block align does not match mono 16-bit PCM")
	}
	return nil
}

func readFull(r io.Reader, b []byte) error {
	_, err := io.ReadFull(r, b)
	return readErr(err)
}

func readErr(err error) error {
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return errors.New("file is truncated")
	}
	if err != nil {
		return fmt.Errorf("read: %w", err)
	}
	return nil
}
