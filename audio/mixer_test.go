package audio

import (
	"math"
	"slices"
	"testing"
)

func TestMixerPanLaw(t *testing.T) {
	t.Parallel()
	clip := Clip{pcm: samples(16384)} // 0.5
	center := float32(0.5 * math.Sqrt2 / 2)
	for _, tc := range []struct {
		pan, left, right float32
	}{
		{-1, 0.5, 0},
		{0, center, center},
		{1, 0, 0.5},
	} {
		var m Mixer
		m.Start(clip, 1, tc.pan)
		dst := make([]float32, 2)
		m.Render(dst)
		if !near(dst[0], tc.left) || !near(dst[1], tc.right) {
			t.Errorf("pan %v rendered %v, want [%v %v]", tc.pan, dst, tc.left, tc.right)
		}
	}
	for pan := float32(-1); pan <= 1; pan += 0.125 {
		var m Mixer
		m.Start(clip, 0.8, pan)
		dst := make([]float32, 2)
		m.Render(dst)
		if power := dst[0]*dst[0] + dst[1]*dst[1]; !near(power, 0.4*0.4) {
			t.Errorf("pan %v has power %v, want %v", pan, power, 0.4*0.4)
		}
	}
}

func TestMixerSanitizesGainAndPan(t *testing.T) {
	t.Parallel()
	clip := Clip{pcm: samples(16384)}
	nan := float32(math.NaN())
	for _, tc := range []struct {
		gain, pan, left, right float32
	}{
		{nan, 0, 0, 0},
		{-2, 0, 0, 0},
		{2, -3, 0.5, 0},
		{1, 3, 0, 0.5},
		{1, nan, 0.5 * math.Sqrt2 / 2, 0.5 * math.Sqrt2 / 2},
	} {
		var m Mixer
		m.Start(clip, tc.gain, tc.pan)
		dst := make([]float32, 2)
		m.Render(dst)
		if !near(dst[0], tc.left) || !near(dst[1], tc.right) {
			t.Errorf("gain %v pan %v rendered %v, want [%v %v]",
				tc.gain, tc.pan, dst, tc.left, tc.right)
		}
	}
}

func TestMixerSumsAndClamps(t *testing.T) {
	t.Parallel()
	var m Mixer
	m.Start(Clip{pcm: samples(16384, 30000, -30000)}, 1, -1)
	m.Start(Clip{pcm: samples(8192, 30000, -30000)}, 0.5, -1)
	dst := make([]float32, 6)
	m.Render(dst)
	if want := []float32{0.625, 0, 1, 0, -1, 0}; !slices.EqualFunc(dst, want, near) {
		t.Fatalf("rendered %v, want %v", dst, want)
	}
}

func TestMixerReplacesOldestVoice(t *testing.T) {
	t.Parallel()
	var m Mixer
	clip := Clip{pcm: samples(32767)}
	for i := range MaxVoices + 1 {
		m.Start(clip, float32(i+1)/32, -1)
	}
	if m.playing != MaxVoices || !near(m.voices[0].left, 2.0/32) ||
		!near(m.voices[MaxVoices-1].left, float32(MaxVoices+1)/32) {
		t.Fatalf("pool after overflow starts at gain %v with %d voices",
			m.voices[0].left*32, m.playing)
	}
}

func TestMixerEndsVoicesAtClipEnd(t *testing.T) {
	t.Parallel()
	var m Mixer
	m.Start(Clip{pcm: samples(100, 200, 300)}, 1, -1)
	m.Start(Clip{pcm: samples(1000)}, 1, -1)
	dst := []float32{9, 9, 9, 9}
	m.Render(dst)
	if m.playing != 1 || !near(dst[0], 1100.0/32768) || !near(dst[2], 200.0/32768) {
		t.Fatalf("first render %v left %d voices", dst, m.playing)
	}
	m.Render(dst[:2])
	if m.playing != 0 || !near(dst[0], 300.0/32768) || m.voices[0].pcm != nil {
		t.Fatalf("voice did not end on its last sample: %v, %d voices", dst, m.playing)
	}
	m.Render(dst)
	if !slices.Equal(dst, []float32{0, 0, 0, 0}) {
		t.Fatalf("empty mixer rendered %v", dst)
	}
	m.Start(Clip{pcm: samples(1000)}, 1, 0)
	m.Reset()
	m.Render(dst[:3])
	if m.playing != 0 || !slices.Equal(dst, []float32{0, 0, 0, 0}) {
		t.Fatalf("reset or odd length rendered %v", dst)
	}
	m.Start(Clip{}, 1, 0)
	if m.playing != 0 {
		t.Fatal("a zero clip took a voice")
	}
}

func TestMixerIsDeterministicAndAllocationFree(t *testing.T) {
	clip := Clip{pcm: samples(100, -2000, 30000, 7, -32768)}
	script := func(m *Mixer, dst []float32) {
		for i := range MaxVoices + 3 {
			m.Start(clip, float32(i)/MaxVoices, float32(i%5)/2-1)
			m.Render(dst[:2*(i%3)])
		}
		m.Render(dst)
	}
	var a, b Mixer
	da, db := make([]float32, 8), make([]float32, 8)
	if allocs := testing.AllocsPerRun(100, func() { script(&a, da) }); allocs != 0 {
		t.Fatalf("mixer allocated %v times per run", allocs)
	}
	a.Reset()
	script(&a, da)
	script(&b, db)
	if !slices.Equal(da, db) {
		t.Fatalf("the same script rendered %v and %v", da, db)
	}
}

func TestSpatialize(t *testing.T) {
	nan, inf := math.NaN(), math.Inf(1)
	for _, tc := range []struct {
		lx, ly, sx, sy, hearing float64
		gain, pan               float32
	}{
		{10, 10, 10, 10, 100, 1, 0},
		{0, 0, 30, 40, 100, 0.5, 0.6},
		{0, 0, -25, 0, 100, 0.75, -0.5},
		{0, 0, 80, 0, 100, 0.2, 1},
		{0, 0, 100, 0, 100, 0, 0},
		{0, 0, 0, -200, 100, 0, 0},
		{0, 0, 1, 0, 0, 0, 0},
		{0, 0, 1, 0, -5, 0, 0},
		{0, 0, 1, 0, nan, 0, 0},
		{nan, 0, 1, 0, 100, 0, 0},
		{0, 0, 1, nan, 100, 0, 0},
		{0, 0, inf, 0, 100, 0, 0},
		{0, 0, 5, 0, inf, 1, 0},
	} {
		gain, pan := Spatialize(tc.lx, tc.ly, tc.sx, tc.sy, tc.hearing)
		if !near(gain, tc.gain) || !near(pan, tc.pan) {
			t.Errorf("Spatialize(%v, %v, %v, %v, %v) = %v, %v; want %v, %v",
				tc.lx, tc.ly, tc.sx, tc.sy, tc.hearing, gain, pan, tc.gain, tc.pan)
		}
	}
	if allocs := testing.AllocsPerRun(100, func() { Spatialize(1, 2, 3, 4, 5) }); allocs != 0 {
		t.Fatalf("Spatialize allocated %v times", allocs)
	}
}

// BenchmarkMixerRender mixes a full pool into one 512-frame buffer, which
// lasts 10.7 ms at 48 kHz.
func BenchmarkMixerRender(b *testing.B) {
	pcm := make([]int16, 2*SampleRate)
	for i := range pcm {
		pcm[i] = int16(i*37%65536 - 32768)
	}
	clip := Clip{pcm: samples(pcm...)}
	dst := make([]float32, 2*512)
	var m Mixer
	b.ReportAllocs()
	for b.Loop() {
		if m.playing < MaxVoices {
			m.Reset()
			for i := range MaxVoices {
				m.Start(clip, 0.5, float32(i)/MaxVoices*2-1)
			}
		}
		m.Render(dst)
	}
}

func near(a, b float32) bool { return math.Abs(float64(a-b)) < 1e-6 }
