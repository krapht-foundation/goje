package audio

import (
	"bufio"
	"crypto/md5" //nolint:gosec // G501: FLAC defines its integrity check as MD5; it is not used for security.
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"

	"github.com/mewkiz/flac/frame"
)

const (
	// MaxFLACBytes limits the encoded file, including metadata.
	MaxFLACBytes = 4 << 20

	// maxFLACMetaBytes and maxFLACMetaBlocks bound the metadata skipped
	// after STREAMINFO. Their contents are never parsed.
	maxFLACMetaBytes  = 64 << 10
	maxFLACMetaBlocks = 16
	streamInfoBytes   = 34
)

// DecodeFLAC reads one native FLAC stream of mono signed 16-bit samples at
// 48 kHz, at most 64 seconds (MaxMusicSeconds) and MaxFLACBytes. STREAMINFO
// must give the total sample count; the frames must decode to exactly that
// many samples, and a nonzero MD5 signature must match. Other metadata blocks
// are skipped unread. It does not close r. On error the returned clip is zero.
func DecodeFLAC(r io.Reader) (Clip, error) {
	lr := &io.LimitedReader{R: r, N: MaxFLACBytes + 1}
	pcm, err := decodeFLAC(bufio.NewReader(lr))
	if lr.N == 0 {
		err = fmt.Errorf("file exceeds %d bytes", MaxFLACBytes)
	}
	if err != nil {
		return Clip{}, fmt.Errorf("decode FLAC: %w", err)
	}
	return Clip{pcm: pcm}, nil
}

type flacInfo struct {
	minBlock, maxBlock int
	samples            int
	md5                [md5.Size]byte
}

func decodeFLAC(r *bufio.Reader) ([]byte, error) {
	info, err := readFLACMetadata(r)
	if err != nil {
		return nil, err
	}
	// STREAMINFO bounds the allocation; the frame loop enforces it again.
	pcm := make([]byte, 0, 2*info.samples)
	fixed := false
	for n := 0; len(pcm) < cap(pcm); n++ {
		f, err := frame.New(r)
		if err != nil {
			return nil, flacErr(err)
		}
		done := len(pcm) / 2
		bs := int(f.BlockSize)
		switch {
		case n == 0:
			fixed = f.HasFixedBlockSize
		case f.HasFixedBlockSize != fixed:
			return nil, fmt.Errorf("frame %d changes the blocking strategy", n)
		}
		pos := uint64(done)
		if fixed {
			pos = uint64(n)
		}
		switch {
		case f.Channels != frame.ChannelsMono:
			return nil, fmt.Errorf("frame %d has %d channels, want mono", n, f.Channels.Count())
		case f.SampleRate != 0 && f.SampleRate != SampleRate:
			return nil, fmt.Errorf("frame %d sample rate %d, want %d", n, f.SampleRate, SampleRate)
		case f.BitsPerSample != 0 && f.BitsPerSample != 16:
			return nil, fmt.Errorf("frame %d has %d bits per sample, want 16", n, f.BitsPerSample)
		case f.Num != pos:
			return nil, fmt.Errorf("frame %d is out of order", n)
		case bs == 0 || bs > info.maxBlock:
			return nil, fmt.Errorf("frame %d block size %d exceeds the maximum %d", n, bs, info.maxBlock)
		case done+bs > info.samples:
			return nil, errors.New("frames exceed the STREAMINFO sample count")
		case bs < info.minBlock && done+bs != info.samples:
			return nil, fmt.Errorf("frame %d block size %d is below the minimum %d", n, bs, info.minBlock)
		}
		// Parse allocates one block of int32 samples, bounded by maxBlock above.
		if err := f.Parse(); err != nil {
			return nil, flacErr(err)
		}
		s := f.Subframes[0].Samples
		if len(s) != bs {
			return nil, fmt.Errorf("frame %d decodes %d samples, want %d", n, len(s), bs)
		}
		for _, v := range s {
			if v < math.MinInt16 || v > math.MaxInt16 {
				return nil, fmt.Errorf("frame %d sample %d exceeds 16 bits", n, v)
			}
			//nolint:gosec // G115: range checked above.
			pcm = binary.LittleEndian.AppendUint16(pcm, uint16(int16(v)))
		}
	}
	if _, err := r.ReadByte(); err == nil {
		return nil, errors.New("unexpected data after the last frame")
	} else if !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("check end of FLAC: %w", err)
	}
	//nolint:gosec // G401: see import.
	if info.md5 != ([md5.Size]byte{}) && md5.Sum(pcm) != info.md5 {
		return nil, errors.New("decoded samples do not match the STREAMINFO MD5 signature")
	}
	return pcm, nil
}

// readFLACMetadata reads the "fLaC" marker and every metadata block, leaving
// r at the first frame. An ID3 prefix is not accepted.
func readFLACMetadata(r *bufio.Reader) (flacInfo, error) {
	var info flacInfo
	var b [4 + 4 + streamInfoBytes]byte
	if err := readFull(r, b[:]); err != nil {
		return info, err
	}
	if string(b[0:4]) != "fLaC" {
		return info, errors.New("not a native FLAC stream")
	}
	last := b[4]&0x80 != 0
	if b[4]&0x7f != 0 || int(b[5])<<16|int(b[6])<<8|int(b[7]) != streamInfoBytes {
		return info, errors.New("first metadata block is not a 34-byte STREAMINFO")
	}
	si := b[8:]
	be := binary.BigEndian
	info.minBlock = int(be.Uint16(si[0:2]))
	info.maxBlock = int(be.Uint16(si[2:4]))
	packed := be.Uint64(si[10:18]) // rate:20, channels-1:3, bits-1:5, samples:36
	rate := packed >> 44
	channels := packed>>41&0x7 + 1
	bits := packed>>36&0x1f + 1
	samples := packed & (1<<36 - 1)
	copy(info.md5[:], si[18:34])
	switch {
	case rate != SampleRate:
		return info, fmt.Errorf("sample rate %d, want %d", rate, SampleRate)
	case channels != 1:
		return info, fmt.Errorf("%d channels, want mono", channels)
	case bits != 16:
		return info, fmt.Errorf("%d bits per sample, want 16", bits)
	case samples == 0:
		return info, errors.New("STREAMINFO does not give the sample count")
	case samples > MaxMusicSeconds*SampleRate:
		return info, fmt.Errorf("track exceeds %d seconds", MaxMusicSeconds)
	case info.minBlock < 16 || info.maxBlock < info.minBlock:
		return info, fmt.Errorf(
			"STREAMINFO block sizes %d to %d are invalid",
			info.minBlock,
			info.maxBlock,
		)
	}
	info.samples = int(samples)
	skipped := 0
	for blocks := 1; !last; blocks++ {
		if blocks > maxFLACMetaBlocks {
			return info, fmt.Errorf("more than %d metadata blocks", maxFLACMetaBlocks)
		}
		var h [4]byte
		if err := readFull(r, h[:]); err != nil {
			return info, err
		}
		last = h[0]&0x80 != 0
		size := int(h[1])<<16 | int(h[2])<<8 | int(h[3])
		switch kind := h[0] & 0x7f; {
		case kind == 0:
			return info, errors.New("duplicate STREAMINFO block")
		case kind > 6:
			return info, fmt.Errorf("unsupported metadata block type %d", kind)
		}
		if skipped += size; skipped > maxFLACMetaBytes {
			return info, fmt.Errorf("metadata exceeds %d bytes", maxFLACMetaBytes)
		}
		if _, err := r.Discard(size); err != nil {
			return info, readErr(err)
		}
	}
	return info, nil
}

// flacErr reports the end of input as truncation, like the WAV decoder, and
// other decoder errors without wrapping the dependency's error values.
func flacErr(err error) error {
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return readErr(err)
	}
	//nolint:errorlint // Callers must not depend on the dependency's errors.
	return fmt.Errorf("frame: %v", err)
}
