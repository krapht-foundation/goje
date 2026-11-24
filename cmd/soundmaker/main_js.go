// Command soundmaker hosts the effect document for the browser Sound Maker.
package main

import (
	"math"
	"syscall/js"

	"github.com/krapht-foundation/goje/internal/soundmaker"
)

func main() {
	doc := soundmaker.New()
	uint8Array := js.Global().Get("Uint8Array")
	api := map[string]any{
		"limits": map[string]any{
			"minHz": soundmaker.MinHz, "maxHz": soundmaker.MaxHz,
			"minDurationMS": soundmaker.MinDurationMS, "maxDurationMS": soundmaker.MaxDurationMS,
		},
		"settings": callback(func(_ []js.Value) any {
			s := doc.Settings()
			return map[string]any{
				"waveform": string(s.Waveform), "startHz": s.StartHz, "endHz": s.EndHz,
				"durationMS": s.DurationMS, "attackMS": s.AttackMS, "releaseMS": s.ReleaseMS,
			}
		}),
		// apply(waveform, startHz, endHz, durationMS, attackMS, releaseMS) returns
		// "" on success or a diagnostic; a rejected call keeps the current sound.
		"apply": callback(func(args []js.Value) any {
			if len(args) != 6 || args[0].Type() != js.TypeString {
				return "Expected a waveform and five whole numbers."
			}
			var n [5]int
			for i := range n {
				v := args[i+1]
				if v.Type() != js.TypeNumber || v.Float() != math.Trunc(v.Float()) ||
					math.Abs(v.Float()) > 1<<31 {
					return "Pitch, duration, attack, and release must be whole numbers."
				}
				n[i] = v.Int()
			}
			return diagnostic(doc.Apply(soundmaker.Settings{
				Waveform: soundmaker.Waveform(args[0].String()),
				StartHz:  n[0], EndHz: n[1], DurationMS: n[2], AttackMS: n[3], ReleaseMS: n[4],
			}))
		}),
		// pcm returns a copy of the samples that the export encodes.
		"pcm": callback(func(_ []js.Value) any { return bytesToJS(uint8Array, doc.PCM()) }),
		"exportWAV": callback(func(_ []js.Value) any {
			data, err := doc.ExportWAV()
			if err != nil {
				return err.Error()
			}
			return bytesToJS(uint8Array, data)
		}),
	}
	js.Global().Set("gojeSoundMaker", api)
	// The page owns these callbacks for the lifetime of the Wasm instance.
	select {}
}

func callback(fn func([]js.Value) any) js.Func {
	return js.FuncOf(func(_ js.Value, args []js.Value) any { return fn(args) })
}

func bytesToJS(uint8Array js.Value, data []byte) js.Value {
	out := uint8Array.New(len(data))
	js.CopyBytesToJS(out, data)
	return out
}

func diagnostic(err error) string {
	if err != nil {
		return err.Error()
	}
	return ""
}
