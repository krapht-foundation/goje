//go:build cgo && (darwin || linux || windows)

package desktop

import (
	"context"
	"encoding/binary"
	"log/slog"
	"math"

	"github.com/gen2brain/malgo"

	"github.com/krapht-foundation/goje/audio"
	"github.com/krapht-foundation/goje/internal/sound"
)

// speaker owns one miniaudio context and playback device.
type speaker struct {
	context *malgo.AllocatedContext
	device  *malgo.Device
	ctx     context.Context
	logger  *slog.Logger
}

// mix is touched only by the miniaudio callback thread after device start.
type mix struct {
	queue      *sound.Queue
	clips      []audio.Clip
	music      []audio.Clip
	generation uint64
	mixer      audio.Mixer
	frames     []float32 // grows to the largest callback, then is reused
}

// playbackPeriodMS is the device period. miniaudio's 10 ms default crackled on the built-in
// speakers of a MacBook even though every callback arrived on time and took under 150 µs, so
// the cause lies below the mixer. A 40 ms period played cleanly there, at the cost of up to
// about 40 ms more delay before a sound starts.
const playbackPeriodMS = 40

// openSpeaker starts default output for queue. Without a usable device it logs
// and returns nil; the queue stays disabled so the game runs silently.
func openSpeaker(ctx context.Context, logger *slog.Logger, queue *sound.Queue) *speaker {
	backends := audioBackends()
	audioContext, err := malgo.InitContext(backends, malgo.ContextConfig{}, nil)
	if err != nil {
		logAudio(ctx, logger, "audio unavailable", err)
		return nil
	}
	s := &speaker{context: audioContext, ctx: ctx, logger: logger}
	config := malgo.DefaultDeviceConfig(malgo.Playback)
	config.SampleRate = audio.SampleRate
	config.Playback.Format = malgo.FormatF32
	config.Playback.Channels = 2
	config.PeriodSizeInMilliseconds = playbackPeriodMS
	m := &mix{
		queue:      queue,
		clips:      queue.Clips(),
		music:      queue.Music(),
		generation: queue.Generation(),
	}
	s.device, err = malgo.InitDevice(audioContext.Context, config, malgo.DeviceCallbacks{
		Data: func(out, _ []byte, _ uint32) {
			probeAudio(m.fill(out))
		},
	})
	if err != nil {
		logAudio(ctx, logger, "audio unavailable", err)
		s.close()
		return nil
	}
	if err := s.device.Start(); err != nil {
		logAudio(ctx, logger, "audio unavailable", err)
		s.close()
		return nil
	}
	queue.Enable()
	if logger != nil {
		logger.InfoContext(ctx, "audio started")
	}
	return s
}

// close stops callbacks before releasing the context that owns the device.
func (s *speaker) close() {
	if s == nil {
		return
	}
	if s.device != nil {
		s.device.Uninit()
	}
	if err := s.context.Uninit(); err != nil {
		logAudio(s.ctx, s.logger, "audio shutdown failed", err)
	}
	s.context.Free()
}

// fill applies the pending requests, mixes the next frames, and writes them to
// out as little-endian float32 stereo. A generation change (suspension,
// restart) stops every sound voice before taking new requests; the music
// voice continues. It returns the mixed frames, which stay valid until the
// next call.
func (m *mix) fill(out []byte) []float32 {
	if generation := m.queue.Generation(); generation != m.generation {
		m.generation = generation
		m.mixer.Reset()
	}
	for range sound.Capacity {
		r, ok := m.queue.Next()
		if !ok {
			break
		}
		switch r.Kind {
		case sound.KindPlayMusic:
			m.mixer.PlayMusic(m.music[r.Cue], r.Gain, r.FadeMS)
		case sound.KindStopMusic:
			m.mixer.StopMusic(r.FadeMS)
		case sound.KindMusicGain:
			m.mixer.SetMusicGain(r.Gain, r.FadeMS)
		default:
			m.mixer.Start(m.clips[r.Cue], r.Gain, r.Pan)
		}
	}
	n := len(out) / 4
	if cap(m.frames) < n {
		m.frames = make([]float32, n)
	}
	frames := m.frames[:n]
	m.mixer.Render(frames)
	for i, sample := range frames {
		binary.LittleEndian.PutUint32(out[4*i:], math.Float32bits(sample))
	}
	return frames
}

func logAudio(ctx context.Context, logger *slog.Logger, message string, err error) {
	if logger != nil {
		logger.WarnContext(ctx, message, "error", err)
	}
}

// Real output backends only: miniaudio's default list ends with Null, which
// would hide a missing device behind a silent timer.
func platformBackends() []malgo.Backend {
	return []malgo.Backend{
		malgo.BackendWasapi,
		malgo.BackendDsound,
		malgo.BackendWinmm,
		malgo.BackendCoreaudio,
		malgo.BackendPulseaudio,
		malgo.BackendAlsa,
		malgo.BackendJack,
	}
}
