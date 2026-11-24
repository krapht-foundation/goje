package audio

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// EncodeWAV wraps packed little-endian signed 16-bit mono samples at
// SampleRate in a canonical 44-byte-header RIFF/WAVE file that DecodeWAV
// accepts. It rejects empty input, a partial sample, and clips longer than the
// decoder's limit, so every successful result decodes. The same samples always
// produce the same bytes.
func EncodeWAV(pcm []byte) ([]byte, error) {
	switch {
	case len(pcm) == 0:
		return nil, errors.New("encode WAV: clip is empty")
	case len(pcm)&1 != 0:
		return nil, errors.New("encode WAV: data ends with a partial sample")
	case len(pcm) > maxPCMBytes:
		return nil, fmt.Errorf("encode WAV: clip exceeds %d seconds", maxSeconds)
	}
	le := binary.LittleEndian
	out := make([]byte, 44, 44+len(pcm))
	copy(out[0:], "RIFF")
	le.PutUint32(out[4:], uint32(36+len(pcm))) //nolint:gosec // G115: bounded by maxPCMBytes above.
	copy(out[8:], "WAVEfmt ")
	le.PutUint32(out[16:], 16)
	le.PutUint16(out[20:], 1) // PCM
	le.PutUint16(out[22:], 1) // mono
	le.PutUint32(out[24:], SampleRate)
	le.PutUint32(out[28:], SampleRate*2)
	le.PutUint16(out[32:], 2)
	le.PutUint16(out[34:], 16)
	copy(out[36:], "data")
	le.PutUint32(out[40:], uint32(len(pcm))) //nolint:gosec // G115: bounded by maxPCMBytes above.
	return append(out, pcm...), nil
}
