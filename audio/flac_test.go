package audio

import (
	"bytes"
	"cmp"
	"crypto/md5"
	"encoding/binary"
	"math/rand/v2"
	"os"
	"strings"
	"testing"

	"github.com/mewkiz/flac"
	"github.com/mewkiz/flac/frame"
	"github.com/mewkiz/flac/meta"
)

const beaconMusic = "../examples/beacon/assets/music.flac"

// flacFrame is one mono frame for encodeFLAC. A zero rate or bits copies the
// stream's; pred defaults to verbatim, and the encoder may pick a predictor.
type flacFrame struct {
	samples      []int32
	rate         uint32
	bits         uint8
	variable     bool
	overflowFrom int32 // nonzero: a first-order fixed predictor jumping by this residual
}

// encodeFLAC writes a stream whose STREAMINFO is valid for the frames, then
// lets edit change it before encoding.
func encodeFLAC(
	tb testing.TB,
	edit func(*meta.StreamInfo),
	blocks []*meta.Block,
	frames ...flacFrame,
) []byte {
	tb.Helper()
	info := meta.StreamInfo{
		SampleRate:    SampleRate,
		NChannels:     1,
		BitsPerSample: 16,
		BlockSizeMin:  0xffff,
	}
	sum := md5.New()
	for i, f := range frames {
		n := uint16(len(f.samples))
		if i < len(frames)-1 || len(frames) == 1 {
			info.BlockSizeMin = min(info.BlockSizeMin, n)
		}
		info.BlockSizeMax = max(info.BlockSizeMax, n)
		info.NSamples += uint64(len(f.samples))
		for _, v := range f.samples {
			_ = binary.Write(sum, binary.LittleEndian, int16(v))
		}
	}
	info.BlockSizeMin = max(info.BlockSizeMin, 16)
	copy(info.MD5sum[:], sum.Sum(nil))
	if edit != nil {
		edit(&info)
	}
	var buf bytes.Buffer
	enc, err := flac.NewEncoder(&buf, &info, blocks...)
	if err != nil {
		tb.Fatal(err)
	}
	for _, f := range frames {
		h := frame.Header{
			HasFixedBlockSize: !f.variable,
			BlockSize:         uint16(len(f.samples)),
			SampleRate:        cmp.Or(f.rate, SampleRate),
			Channels:          frame.ChannelsMono,
			BitsPerSample:     cmp.Or(f.bits, 16),
		}
		sub := &frame.Subframe{
			Pred:     frame.PredVerbatim,
			NSamples: len(f.samples),
			Samples:  append([]int32(nil), f.samples...),
		}
		if f.overflowFrom != 0 {
			enc.EnablePredictionAnalysis(false)
			sub.SubHeader = frame.SubHeader{Pred: frame.PredFixed, Order: 1}
			sub.RiceSubframe = &frame.RiceSubframe{Partitions: []frame.RicePartition{{Param: 14}}}
			for i := range sub.Samples {
				sub.Samples[i] = f.overflowFrom * int32(i)
			}
		}
		fr := &frame.Frame{Header: h, Subframes: []*frame.Subframe{sub}}
		if err := enc.WriteFrame(fr); err != nil {
			tb.Fatal(err)
		}
	}
	if err := enc.Close(); err != nil {
		tb.Fatal(err)
	}
	return buf.Bytes()
}

func ramp(n int, from int32) []int32 {
	s := make([]int32, n)
	for i := range s {
		s[i] = from + int32(i*37%2000)
	}
	return s
}

func le16(s ...[]int32) []byte {
	var out []byte
	for _, part := range s {
		for _, v := range part {
			out = binary.LittleEndian.AppendUint16(out, uint16(int16(v)))
		}
	}
	return out
}

func TestDecodeFLACAccepts(t *testing.T) {
	t.Parallel()
	a, b, c := ramp(4096, -1000), ramp(4096, 5), ramp(100, -32768)
	padding := &meta.Block{Type: meta.TypePadding, Length: 64}
	comment := &meta.Block{
		Type: meta.TypeVorbisComment,
		Body: &meta.VorbisComment{Vendor: "goje"},
	}
	extreme := []int32{32767, -32768, 0, 1, -1, 32767, -32768, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	for name, tc := range map[string]struct {
		data []byte
		want []byte
	}{
		"fixed blocks with short last": {encodeFLAC(t, nil, nil, flacFrame{samples: a}, flacFrame{samples: b}, flacFrame{samples: c}), le16(a, b, c)},
		"variable blocks":              {encodeFLAC(t, nil, nil, flacFrame{samples: c, variable: true}, flacFrame{samples: a, variable: true}), le16(c, a)},
		"skipped metadata":             {encodeFLAC(t, nil, []*meta.Block{comment, padding}, flacFrame{samples: b}), le16(b)},
		"full scale":                   {encodeFLAC(t, nil, nil, flacFrame{samples: extreme}), le16(extreme)},
		"header rate and bits":         {encodeFLAC(t, nil, nil, flacFrame{samples: b, rate: SampleRate, bits: 16}), le16(b)},
		"no MD5":                       {encodeFLAC(t, func(i *meta.StreamInfo) { i.MD5sum = [16]byte{} }, nil, flacFrame{samples: b}), le16(b)},
	} {
		clip, err := DecodeFLAC(bytes.NewReader(tc.data))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !bytes.Equal(clip.PCM(), tc.want) {
			t.Fatalf(
				"%s: decoded %d bytes that differ from the %d input bytes",
				name,
				len(clip.PCM()),
				len(tc.want),
			)
		}
	}
}

func TestDecodeFLACRejects(t *testing.T) {
	t.Parallel()
	block := ramp(4096, 0)
	valid := encodeFLAC(t, nil, nil, flacFrame{samples: block}, flacFrame{samples: block})
	withInfo := func(edit func(*meta.StreamInfo)) []byte { return encodeFLAC(t, edit, nil, flacFrame{samples: block}) }
	withBlock := func(b *meta.Block) []byte { return encodeFLAC(t, nil, []*meta.Block{b}, flacFrame{samples: block}) }
	pad := func(n uint32) *meta.Block {
		return &meta.Block{Type: meta.TypePadding, Length: int64(n)}
	}
	many := make([]*meta.Block, maxFLACMetaBlocks+1)
	for i := range many {
		many[i] = pad(0)
	}
	streamInfoAgain := append([]byte(nil), valid...)
	streamInfoAgain[4] &^= 0x80 // Clear "last" and append a second STREAMINFO after it.
	streamInfoAgain = append(
		streamInfoAgain[:42:42],
		append([]byte{0x80, 0, 0, 34}, streamInfoAgain[8:]...)...)
	reserved := withBlock(pad(0))
	reserved[42] = 0x80 | 7
	crc := append([]byte(nil), valid...)
	crc[len(crc)-100] ^= 0xff
	md5Bad := append([]byte(nil), valid...)
	md5Bad[8+18] ^= 1
	stereo := append([]byte(nil), valid...)
	stereo[8+12] |= 1 << 1 // STREAMINFO channels − 1 = 1; the encoder refuses to write it.
	// Noise does not compress, so 530 full blocks pass MaxFLACBytes before the
	// 64 seconds STREAMINFO promises.
	rng := rand.New(rand.NewPCG(1, 2))
	noise := make([]flacFrame, 530)
	for i := range noise {
		noise[i].samples = make([]int32, 4096)
		for j := range noise[i].samples {
			noise[i].samples[j] = rng.Int32N(65536) - 32768
		}
	}
	oversize := encodeFLAC(
		t,
		func(i *meta.StreamInfo) { i.NSamples = MaxMusicSeconds * SampleRate },
		nil,
		noise...)

	for name, tc := range map[string]struct {
		data []byte
		want string
	}{
		"empty":            {nil, "truncated"},
		"WAV":              {[]byte("RIFF\x00\x00\x00\x00WAVEfmt " + strings.Repeat("\x00", 40)), "not a native FLAC"},
		"ID3":              {append([]byte("ID3\x04\x00\x00\x00\x00\x00\x00"), valid...), "not a native FLAC"},
		"no STREAMINFO":    {append([]byte("fLaC\x81\x00\x00\x22"), valid[8:]...), "34-byte STREAMINFO"},
		"stereo":           {stereo, "2 channels"},
		"44.1 kHz":         {withInfo(func(i *meta.StreamInfo) { i.SampleRate = 44100 }), "sample rate 44100"},
		"24-bit":           {withInfo(func(i *meta.StreamInfo) { i.BitsPerSample = 24 }), "24 bits"},
		"unknown length":   {withInfo(func(i *meta.StreamInfo) { i.NSamples = 0 }), "sample count"},
		"over 64 seconds":  {withInfo(func(i *meta.StreamInfo) { i.NSamples = MaxMusicSeconds*SampleRate + 1 }), "64 seconds"},
		"small min block":  {withInfo(func(i *meta.StreamInfo) { i.BlockSizeMin = 15 }), "block sizes"},
		"max below min":    {withInfo(func(i *meta.StreamInfo) { i.BlockSizeMax = 100 }), "block sizes"},
		"block over max":   {withInfo(func(i *meta.StreamInfo) { i.BlockSizeMax, i.BlockSizeMin = 4095, 16 }), "exceeds the maximum"},
		"short inner":      {encodeFLAC(t, func(i *meta.StreamInfo) { i.BlockSizeMin = 4096 }, nil, flacFrame{samples: block[:100]}, flacFrame{samples: block}), "below the minimum"},
		"duplicate info":   {streamInfoAgain, "duplicate STREAMINFO"},
		"reserved block":   {reserved, "unsupported metadata block type 7"},
		"metadata size":    {withBlock(pad(maxFLACMetaBytes + 1)), "metadata exceeds"},
		"metadata blocks":  {encodeFLAC(t, nil, many, flacFrame{samples: block}), "more than 16 metadata"},
		"truncated meta":   {valid[:30], "truncated"},
		"truncated frame":  {valid[:len(valid)-1], "truncated"},
		"missing frame":    {withInfo(func(i *meta.StreamInfo) { i.NSamples = 8192 }), "truncated"},
		"extra samples":    {withInfo(func(i *meta.StreamInfo) { i.NSamples = 4000 }), "exceed the STREAMINFO sample count"},
		"trailing data":    {append(append([]byte(nil), valid...), 0), "after the last frame"},
		"oversize":         {oversize, "exceeds 4194304 bytes"},
		"frame CRC":        {crc, "frame:"},
		"MD5":              {md5Bad, "MD5"},
		"frame rate":       {encodeFLAC(t, nil, nil, flacFrame{samples: block, rate: 44100}), "frame 0 sample rate 44100"},
		"frame bits":       {encodeFLAC(t, nil, nil, flacFrame{samples: block, bits: 24}), "24 bits per sample"},
		"blocking changes": {encodeFLAC(t, nil, nil, flacFrame{samples: block}, flacFrame{samples: block, variable: true}), "blocking strategy"},
		"out of 16 bits":   {encodeFLAC(t, nil, nil, flacFrame{samples: make([]int32, 16), overflowFrom: 3000}), "exceeds 16 bits"},
	} {
		clip, err := DecodeFLAC(bytes.NewReader(tc.data))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: error %v, want %q", name, err, tc.want)
		}
		if clip.PCM() != nil {
			t.Errorf("%s: returned a nonzero clip", name)
		}
	}
}

// TestDecodeFLACBeaconMusic checks that the Beacon asset decodes to exactly
// the samples its source score renders. See specs/music-flac-m18.md.
func TestDecodeFLACBeaconMusic(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(beaconMusic)
	if err != nil {
		t.Fatal(err)
	}
	src, err := os.ReadFile("testdata/beacon-music.json")
	if err != nil {
		t.Fatal(err)
	}
	score, err := DecodeScore(src)
	if err != nil {
		t.Fatal(err)
	}
	want, err := RenderScore(score)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeFLAC(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.PCM(), want.PCM()) {
		t.Fatal("music.flac does not decode to the rendered score")
	}
	t.Logf(
		"FLAC %d bytes, WAV %d bytes, %.1f%%",
		len(data),
		44+len(want.PCM()),
		100*float64(len(data))/float64(44+len(want.PCM())),
	)
}

func BenchmarkDecodeFLACBeaconMusic(b *testing.B) {
	data, err := os.ReadFile(beaconMusic)
	if err != nil {
		b.Fatal(err)
	}
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	for b.Loop() {
		if _, err := DecodeFLAC(bytes.NewReader(data)); err != nil {
			b.Fatal(err)
		}
	}
}

func FuzzDecodeFLAC(f *testing.F) {
	f.Add(encodeFLAC(f, nil, nil, flacFrame{samples: ramp(64, 0)}, flacFrame{samples: ramp(20, 9)}))
	f.Add(
		encodeFLAC(
			f,
			func(i *meta.StreamInfo) { i.MD5sum = [16]byte{} },
			nil,
			flacFrame{samples: ramp(32, -5), variable: true},
		),
	)
	f.Fuzz(func(t *testing.T, data []byte) {
		clip, err := DecodeFLAC(bytes.NewReader(data))
		pcm := clip.PCM()
		switch {
		case err != nil && pcm != nil:
			t.Fatal("an error returned a nonzero clip")
		case err == nil && (len(pcm) == 0 || len(pcm)&1 != 0 || len(pcm) > maxMusicPCMBytes):
			t.Fatalf("decoded %d PCM bytes", len(pcm))
		}
	})
}
