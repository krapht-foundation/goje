//go:build wasm

package browser

import (
	"context"
	"encoding/binary"
	"fmt"
	"log/slog"
	"math"
	"slices"
	"syscall/js"

	"github.com/krapht-foundation/goje/audio"
	"github.com/krapht-foundation/goje/internal/sound"
)

// webAudio plays up to audio.MaxVoices prepared cues at once through Web
// Audio. Buffers exist from boot; the AudioContext starts only inside a trusted
// user gesture, so the queue stays disabled and drops requests until the
// browser allows output. See specs/audio-mixer-m15.md.
type webAudio struct {
	queue   *sound.Queue
	logger  *slog.Logger
	ctx     context.Context
	buffers []js.Value // indexed by cue; undefined for an unusable clip
	// Boolean is a native no-op that observes rejections without a Go callback,
	// so a promise that settles after shutdown cannot call a released function.
	observe js.Value
	context js.Value
	state   js.Func
	// voices is a ring in start order: voices[next] is the oldest voice and
	// the one the next request replaces. Ended sources stay until replaced or
	// cleared, because stopping them is harmless and tracking their end would
	// need a Go callback per voice.
	voices  [audio.MaxVoices]webVoice
	next    int
	gen     uint64
	running bool
	failed  bool
}

// webVoice is a source and the last node of its graph, which connects to the
// context's destination.
type webVoice struct {
	source, output js.Value
}

// newWebAudio prepares every clip outside the tick. It returns nil when sound
// is disabled or Web Audio is missing; audio never fails the game.
func newWebAudio(
	ctx context.Context,
	window js.Value,
	queue *sound.Queue,
	logger *slog.Logger,
) *webAudio {
	if queue == nil {
		return nil
	}
	if !window.Get("AudioContext").Truthy() || !window.Get("AudioBuffer").Truthy() ||
		!window.Get("StereoPannerNode").Truthy() {
		logAudio(ctx, logger, "audio unavailable", "reason", "Web Audio API missing")
		return nil
	}
	a := &webAudio{
		queue:   queue,
		logger:  logger,
		ctx:     ctx,
		observe: window.Get("Boolean"),
		gen:     queue.Generation(),
	}
	if err := catchJS(func() { a.prepare(window, queue.Clips()) }); err != nil {
		logAudio(ctx, logger, "audio unavailable", "error", err)
		return nil
	}
	return a
}

func (a *webAudio) prepare(window js.Value, clips []audio.Clip) {
	constructor := window.Get("AudioBuffer")
	bytes := window.Get("Uint8Array")
	floats := window.Get("Float32Array")
	a.buffers = make([]js.Value, len(clips))
	var scratch []byte
	for i, clip := range clips {
		pcm := clip.PCM()
		n := len(pcm) / 2
		if n == 0 {
			continue
		}
		// Convert in Go and cross the JS boundary once per clip. Typed arrays
		// use the host byte order, which is little-endian for wasm browsers.
		scratch = slices.Grow(scratch[:0], 4*n)[:4*n]
		for s := range n {
			//nolint:gosec // G115: Reinterpret the signed PCM bit pattern.
			v := float32(int16(binary.LittleEndian.Uint16(pcm[2*s:]))) / 32768
			binary.LittleEndian.PutUint32(scratch[4*s:], math.Float32bits(v))
		}
		staging := bytes.New(4 * n)
		js.CopyBytesToJS(staging, scratch)
		buffer := constructor.New(map[string]any{
			"length": n, "numberOfChannels": 1, "sampleRate": audio.SampleRate,
		})
		buffer.Call("copyToChannel", floats.New(staging.Get("buffer")), 0)
		a.buffers[i] = buffer
	}
}

// unlock runs only from a trusted gesture handler, never from a tick.
func (a *webAudio) unlock(window js.Value) {
	if a == nil || a.running || a.failed {
		return
	}
	if a.context.IsUndefined() {
		if err := catchJS(func() {
			a.context = window.Get("AudioContext").New(map[string]any{"latencyHint": "interactive"})
		}); err != nil {
			a.failed = true
			logAudio(a.ctx, a.logger, "audio unavailable", "error", err)
			return
		}
		a.state = js.FuncOf(func(js.Value, []js.Value) any {
			a.sync()
			return nil
		})
		a.context.Call("addEventListener", "statechange", a.state)
	}
	if a.context.Get("state").String() != "running" {
		if err := catchJS(func() {
			a.context.Call("resume").Call("catch", a.observe)
		}); err != nil {
			a.disable(err)
			return
		}
	}
	a.sync()
}

// sync follows context state changes. Leaving "running" (suspension or an OS
// interruption) stops the voice so it is not resumed later.
func (a *webAudio) sync() {
	if a.failed {
		return
	}
	running := a.context.Get("state").String() == "running"
	if running == a.running {
		return
	}
	a.running = running
	if running {
		a.queue.Enable()
		logAudio(a.ctx, a.logger, "audio unlocked")
	} else {
		a.queue.Disable()
		a.stopAll()
	}
}

// poll applies a cleared queue and starts the pending cues. It runs once per
// frame on the host loop and after focus changes, when frames may stop.
func (a *webAudio) poll() {
	if a == nil {
		return
	}
	if g := a.queue.Generation(); g != a.gen {
		a.gen = g
		a.stopAll()
	}
	for range sound.Capacity {
		r, ok := a.queue.Next()
		if !ok || !a.running {
			return
		}
		if a.buffers[r.Cue].IsUndefined() {
			continue
		}
		if err := catchJS(func() { a.start(r) }); err != nil {
			a.disable(err)
			return
		}
	}
}

// start replaces the oldest voice with source → gain → stereo panner →
// destination. The StereoPannerNode applies the same equal-power law to a
// mono buffer as audio.Mixer, so both hosts mix alike. scripts/smoke-mixer.mjs
// mirrors this graph; keep the two in step.
func (a *webAudio) start(r sound.Request) {
	source := a.context.Call("createBufferSource")
	source.Set("buffer", a.buffers[r.Cue])
	gain := a.context.Call("createGain")
	gain.Get("gain").Set("value", r.Gain)
	panner := a.context.Call("createStereoPanner")
	panner.Get("pan").Set("value", r.Pan)
	source.Call("connect", gain)
	gain.Call("connect", panner)
	panner.Call("connect", a.context.Get("destination"))
	a.voices[a.next].stop()
	source.Call("start")
	a.voices[a.next] = webVoice{source: source, output: panner}
	a.next = (a.next + 1) % len(a.voices)
}

func (a *webAudio) disable(err error) {
	a.failed = true
	a.running = false
	a.queue.Disable()
	logAudio(a.ctx, a.logger, "audio unavailable", "error", err)
}

func (a *webAudio) stopAll() {
	for i := range a.voices {
		a.voices[i].stop()
	}
}

func (v *webVoice) stop() {
	if v.source.IsUndefined() {
		return
	}
	v.source.Call("stop") // Harmless on a source that already ended.
	v.output.Call("disconnect")
	*v = webVoice{}
}

func (a *webAudio) close() {
	if a == nil {
		return
	}
	a.stopAll()
	if !a.context.IsUndefined() {
		a.context.Call("removeEventListener", "statechange", a.state)
		a.context.Call("close").Call("catch", a.observe)
		a.state.Release()
	}
}

func catchJS(f func()) (err error) {
	defer func() {
		if failure := recover(); failure != nil {
			exception, ok := failure.(js.Error)
			if !ok {
				panic(failure) //nolint:forbidigo // Unexpected Go panics are not Web Audio errors.
			}
			err = fmt.Errorf("browser: Web Audio: %w", exception)
		}
	}()
	f()
	return nil
}

func logAudio(ctx context.Context, logger *slog.Logger, msg string, args ...any) {
	if logger != nil {
		logger.InfoContext(ctx, msg, args...)
	}
}
