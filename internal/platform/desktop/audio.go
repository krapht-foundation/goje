//go:build cgo && (darwin || linux)

package desktop

import (
	"context"
	"log/slog"

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

// voice is touched only by the miniaudio callback thread after device start.
type voice struct {
	queue      *sound.Queue
	clips      []audio.Clip
	generation uint64
	active     []byte
}

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
	config.Playback.Format = malgo.FormatS16
	config.Playback.Channels = 1
	v := &voice{queue: queue, clips: queue.Clips(), generation: queue.Generation()}
	s.device, err = malgo.InitDevice(audioContext.Context, config, malgo.DeviceCallbacks{
		Data: func(out, _ []byte, _ uint32) {
			v.fill(out)
			probeAudio(out)
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

// fill copies the active cue into out and zero-fills the rest. A generation
// change (suspension, restart) stops the active cue before taking a new one.
func (v *voice) fill(out []byte) {
	if generation := v.queue.Generation(); generation != v.generation {
		v.generation = generation
		v.active = nil
	}
	if cue, ok := v.queue.Next(); ok {
		v.active = v.clips[cue].PCM()
	}
	n := copy(out, v.active)
	v.active = v.active[n:]
	clear(out[n:])
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
		malgo.BackendCoreaudio,
		malgo.BackendPulseaudio,
		malgo.BackendAlsa,
		malgo.BackendJack,
	}
}
