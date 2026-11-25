package audio

import (
	"testing"
)

const fadeFrame = SampleRate / 1000 // frames in a 1 ms fade

// renderLeft renders n frames and returns the left channel, checking that the
// music voice is centered.
func renderLeft(t *testing.T, m *Mixer, n int) []float32 {
	t.Helper()
	dst := make([]float32, 2*n)
	m.Render(dst)
	left := make([]float32, n)
	for i := range left {
		if dst[2*i] != dst[2*i+1] {
			t.Fatalf("frame %d is %v left and %v right", i, dst[2*i], dst[2*i+1])
		}
		left[i] = dst[2*i]
	}
	return left
}

func TestMusicLoopsAndKeepsPositionOnSameTrack(t *testing.T) {
	t.Parallel()
	track := Clip{pcm: samples(8192, 16384, 24576)}
	var m Mixer
	m.PlayMusic(track, 1, 0)
	got := renderLeft(t, &m, 4)
	for i, want := range []float32{0.25, 0.5, 0.75, 0.25} {
		if !near(got[i], want) {
			t.Fatalf("frame %d is %v, want %v", i, got[i], want)
		}
	}
	m.Reset() // Stops sound voices only.
	m.PlayMusic(track, 0.5, 0)
	if got := renderLeft(t, &m, 1); !near(got[0], 0.25) {
		t.Fatalf("the same track restarted or kept its gain: %v, want 0.25", got[0])
	}
}

func TestMusicFadesReplacesAndStops(t *testing.T) {
	t.Parallel()
	up, down := Clip{pcm: samples(16384)}, Clip{pcm: samples(-16384)}
	var m Mixer
	m.PlayMusic(up, 1, 1)
	in := renderLeft(t, &m, fadeFrame+2)
	for k, v := range in {
		if want := 0.5 * float32(min(k, fadeFrame)) / fadeFrame; !near(v, want) {
			t.Fatalf("fade in frame %d is %v, want %v", k, v, want)
		}
	}
	m.PlayMusic(down, 0.8, 1)
	m.SetMusicGain(1, 0) // Sets the gain the pending track fades in to.
	out := renderLeft(t, &m, 2*fadeFrame+1)
	for k, v := range out {
		want := 0.5 * float32(fadeFrame-k) / fadeFrame
		if k >= fadeFrame {
			want = -0.5 * float32(k-fadeFrame) / fadeFrame
		}
		if !near(v, want) {
			t.Fatalf("replacement frame %d is %v, want %v", k, v, want)
		}
	}
	m.StopMusic(1)
	stop := renderLeft(t, &m, fadeFrame+3)
	if !near(stop[0], -0.5) || stop[fadeFrame] != 0 || stop[fadeFrame+2] != 0 || m.music.pcm != nil {
		t.Fatalf(
			"stop rendered %v ... %v and left %d bytes",
			stop[0],
			stop[fadeFrame:],
			len(m.music.pcm),
		)
	}
	m.SetMusicGain(1, 0)
	m.StopMusic(0)
	if got := renderLeft(t, &m, 1); got[0] != 0 {
		t.Fatalf("silent music rendered %v", got[0])
	}
}

func TestMusicClampsAndIgnoresZeroClip(t *testing.T) {
	t.Parallel()
	var m Mixer
	m.PlayMusic(Clip{}, 1, 0)
	if m.music.pcm != nil {
		t.Fatal("a zero clip started")
	}
	m.PlayMusic(Clip{pcm: samples(16384)}, 3, -5)
	if got := renderLeft(t, &m, 1); !near(got[0], 0.5) {
		t.Fatalf("gain 3 and fade -5 rendered %v, want 0.5", got[0])
	}
	m.SetMusicGain(1, 1<<30)
	if m.music.ramp != MaxFadeMS*fadeFrame {
		t.Fatalf("fade ramp is %d frames, want %d", m.music.ramp, MaxFadeMS*fadeFrame)
	}
}

func TestMusicDoesNotAllocate(t *testing.T) {
	a, b := Clip{pcm: samples(1, 2, 3)}, Clip{pcm: samples(-4, 5)}
	var m Mixer
	dst := make([]float32, 2*fadeFrame)
	if allocs := testing.AllocsPerRun(100, func() {
		m.PlayMusic(a, 1, 1)
		m.Render(dst)
		m.PlayMusic(b, 0.5, 1)
		m.SetMusicGain(0.7, 0)
		m.Render(dst)
		m.Render(dst)
		m.StopMusic(1)
		m.Render(dst)
	}); allocs != 0 {
		t.Fatalf("music allocated %v times per run", allocs)
	}
}
