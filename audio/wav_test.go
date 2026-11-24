package audio

import (
	"bytes"
	"encoding/binary"
	"slices"
	"strings"
	"testing"
)

type chunk struct {
	id   string
	body []byte
}

func fmtBody() []byte {
	b := make([]byte, 16)
	le := binary.LittleEndian
	le.PutUint16(b[0:], 1)
	le.PutUint16(b[2:], 1)
	le.PutUint32(b[4:], SampleRate)
	le.PutUint32(b[8:], SampleRate*2)
	le.PutUint16(b[12:], 2)
	le.PutUint16(b[14:], 16)
	return b
}

// wav builds a RIFF file with correct sizes and zero pad bytes.
func wav(chunks ...chunk) []byte {
	var body []byte
	for _, c := range chunks {
		body = append(body, c.id...)
		body = binary.LittleEndian.AppendUint32(body, uint32(len(c.body)))
		body = append(body, c.body...)
		if len(c.body)&1 != 0 {
			body = append(body, 0)
		}
	}
	out := []byte("RIFF")
	out = binary.LittleEndian.AppendUint32(out, uint32(len(body)+4))
	out = append(out, "WAVE"...)
	return append(out, body...)
}

func samples(values ...int16) []byte {
	var b []byte
	for _, v := range values {
		b = binary.LittleEndian.AppendUint16(b, uint16(v))
	}
	return b
}

func TestDecodeWAV(t *testing.T) {
	pcm := samples(0, 1, -1, 32767, -32768)
	file := wav(
		chunk{"LIST", []byte("odd")},
		chunk{"fmt ", fmtBody()},
		chunk{"fact", []byte{1, 2, 3, 4}},
		chunk{"data", pcm},
		chunk{"cue ", nil},
	)
	clip, err := DecodeWAV(bytes.NewReader(file))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(clip.PCM(), pcm) {
		t.Fatalf("PCM = %v, want %v", clip.PCM(), pcm)
	}
}

func TestDecodeWAVAcceptsMaxDuration(t *testing.T) {
	clip, err := DecodeWAV(
		bytes.NewReader(wav(chunk{"fmt ", fmtBody()}, chunk{"data", make([]byte, maxPCMBytes)})),
	)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(clip.PCM()) / 2; got != 3*SampleRate {
		t.Fatalf("samples = %d, want %d", got, 3*SampleRate)
	}
}

func TestDecodeWAVRejects(t *testing.T) {
	data := chunk{"data", samples(1, 2)}
	format := chunk{"fmt ", fmtBody()}
	withFmt := func(at int, v uint32, width int) chunk {
		b := fmtBody()
		if width == 2 {
			binary.LittleEndian.PutUint16(b[at:], uint16(v))
		} else {
			binary.LittleEndian.PutUint32(b[at:], v)
		}
		return chunk{"fmt ", b}
	}
	valid := wav(format, data)
	edit := func(f func([]byte) []byte) []byte { return f(slices.Clone(valid)) }

	tests := []struct {
		name string
		file []byte
		want string
	}{
		{"empty input", nil, "truncated"},
		{"short header", valid[:10], "truncated"},
		{"not RIFF", edit(func(b []byte) []byte { copy(b, "RIFX"); return b }), "not a RIFF/WAVE"},
		{"not WAVE", edit(func(b []byte) []byte { copy(b[8:], "AVI "); return b }), "not a RIFF/WAVE"},
		{
			"RIFF size too small",
			edit(func(b []byte) []byte { binary.LittleEndian.PutUint32(b[4:], 2); return b }),
			"smaller than its header",
		},
		{
			"RIFF size over limit",
			edit(func(b []byte) []byte { binary.LittleEndian.PutUint32(b[4:], MaxWAVBytes); return b }),
			"exceeds 307200 bytes",
		},
		{
			"input over limit",
			wav(format, chunk{"junk", make([]byte, MaxWAVBytes)}, data),
			"exceeds 307200 bytes",
		},
		{"truncated data", valid[:len(valid)-1], "truncated"},
		{"trailing data", append(slices.Clone(valid), 0), "after RIFF"},
		{
			"RIFF size cuts chunk",
			edit(
				func(b []byte) []byte { binary.LittleEndian.PutUint32(b[4:], uint32(len(b)-9)); return b },
			),
			"exceeds RIFF bounds",
		},
		{"partial chunk header", edit(func(b []byte) []byte {
			b = append(b, 0, 0, 0, 0)
			binary.LittleEndian.PutUint32(b[4:], uint32(len(b)-8))
			return b
		}), "partial chunk header"},
		{
			"chunk size past RIFF",
			edit(func(b []byte) []byte { binary.LittleEndian.PutUint32(b[40:], 1000); return b }),
			"exceeds RIFF bounds",
		},
		{"nonzero pad", func() []byte {
			b := wav(chunk{"junk", []byte{9}}, format, data)
			b[21] = 1 // pad after the one-byte junk body
			return b
		}(), "nonzero pad"},
		{"missing fmt", wav(chunk{"junk", nil}), `missing "fmt "`},
		{"missing data", wav(format), `missing "data"`},
		{"data before fmt", wav(data, format), `before "fmt "`},
		{"duplicate fmt", wav(format, format, data), `duplicate "fmt "`},
		{"duplicate data", wav(format, data, data), `duplicate "data"`},
		{"empty data", wav(format, chunk{"data", nil}), "empty"},
		{"partial sample", wav(format, chunk{"data", []byte{1, 2, 3}}), "partial sample"},
		{"too long", wav(format, chunk{"data", make([]byte, maxPCMBytes+2)}), "exceeds 3 seconds"},
		{"fmt size", wav(chunk{"fmt ", append(fmtBody(), 0, 0)}, data), "want 16"},
		{"format tag", wav(withFmt(0, 3, 2), data), "not PCM"},
		{"stereo", wav(withFmt(2, 2, 2), data), "want mono"},
		{"rate", wav(withFmt(4, 44100, 4), data), "sample rate 44100"},
		{"byte rate", wav(withFmt(8, 1, 4), data), "byte rate"},
		{"block align", wav(withFmt(12, 4, 2), data), "block align"},
		{"bits", wav(withFmt(14, 8, 2), data), "8 bits"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clip, err := DecodeWAV(bytes.NewReader(tt.file))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want containing %q", err, tt.want)
			}
			if clip.PCM() != nil {
				t.Fatal("clip not zero on error")
			}
		})
	}
}
