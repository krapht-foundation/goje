//go:build goje_smoke && cgo && (darwin || linux)

package desktop

import "github.com/gen2brain/malgo"

// AudioNull selects miniaudio's Null backend before Run, so smoke runs get
// real callback output without a speaker. Otherwise the native backends run.
var AudioNull bool

// AudioProbe receives each mixed buffer of interleaved float32 stereo frames
// on the audio thread, whichever backend runs. The slice is read-only and
// valid only during the call; the probe must not block.
var AudioProbe func(frames []float32)

func audioBackends() []malgo.Backend {
	if AudioNull {
		return []malgo.Backend{malgo.BackendNull}
	}
	return platformBackends()
}

func probeAudio(frames []float32) {
	if AudioProbe != nil {
		AudioProbe(frames)
	}
}
