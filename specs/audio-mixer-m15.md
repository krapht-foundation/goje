# Audio mixer, M15

## Scope and ownership

This contract defines how a Goje game requests overlapping and positional sounds and how the desktop and browser hosts mix them. It replaces the one-active-cue rule of [RFC 0012](../rfcs/0012-m11-audio-and-sound-maker.md): a new cue no longer stops the cue that is playing, and every accepted request becomes its own voice. The [M15 RFC](../rfcs/0016-m15-audio-mixing-vision-and-platform-reach.md) records the decisions and alternatives.

Goje owns the request interface in [`engine`](../engine/runtime.go), the bounded queue in [`internal/sound`](../internal/sound/queue.go), the voice pool and pan law in [`audio.Mixer`](../audio/mixer.go), the [`audio.Spatialize`](../audio/spatial.go) helper, and the two host players. The game owns which cues it requests, how often, how loud, and from where: rate limits, priorities, mute, volume, and the meaning of positions are game policy. Clip format and decoding stay as M11 defines them in [`audio.DecodeWAV`](../audio/wav.go): mono, signed 16-bit PCM at 48 kHz, at most three seconds, and at most 16 clips per bank.

## Contract

### Requests

- `engine.SoundOutput` is `Play(cue int, gain, pan float32)` and `Clear()`. `Tick.PlaySoundAt(cue, gain, pan)` calls `Play` when the runtime has an output and does nothing otherwise. `Tick.PlaySound(cue)` is `PlaySoundAt(cue, 1, 0)`.
- `Tick.Sound()` returns the runtime's output, which may be nil. A scene that runs a nested `engine.Runtime` passes it to the nested runtime's `SetSoundOutput`, so the nested scenes request sound through the same queue. When a nested runtime's scene exits, it calls `Clear` on that shared output.
- Gain is linear amplitude from 0 to 1. Pan runs from -1, fully left, to 1, fully right. The queue clamps both and replaces NaN with 0 before a host sees them, so both hosts receive the same values.
- Sound is output. A replay records input only, and playing a recording through the same scene requests the same sounds. Requests never change simulation state, and a missing device or a dropped request never affects gameplay.

### Queue

- The queue has one producer, the game loop, and one consumer, the host player, which on desktop is the device callback thread.
- It holds at most 64 pending requests. Enqueueing never blocks, allocates, or calls a device; a request that does not fit is dropped, and accepted requests keep their order.
- Requests are dropped while the device is not ready (before the first browser unlock, after a suspension, or without a device) and when the cue is outside the bank.
- `Clear` increments a generation, drops pending requests, and tells the player to stop every voice. The runtime clears on every scene exit, and the hosts clear on focus loss and suspension. A request accepted before a clear is never started after it.
- The consumer takes the pending requests in order, at most 64 per pass, so a producer that keeps adding requests cannot extend a device callback.

### Mixer

- `audio.Mixer` holds up to `audio.MaxVoices`, 16, voices. Each voice is a clip, its position, and two channel gains. A Mixer allocates nothing, including in `Start`, `Render`, and `Reset`.
- `Start(clip, gain, pan)` adds a voice at the clip's first sample. When all 16 voices are playing, it first removes the voice that started earliest. It clamps gain and pan as the queue does, and it ignores a zero clip.
- The pan law is equal power. A voice's left gain is `gain × cos((pan + 1) × π/4)` and its right gain is `gain × sin((pan + 1) × π/4)`. This is the law the Web Audio `StereoPannerNode` applies to a mono input, so a centered voice plays at about 0.707 of its amplitude on each channel, and the squared channel gains always sum to the squared gain.
- `Render(dst)` overwrites `dst` with `len(dst)/2` interleaved stereo frames at 48 kHz, left first. Each sample is the 16-bit value divided by 32768. The voices are added in start order, then each output sample is clamped to [-1, 1]. Every voice advances by the frames rendered, and a voice ends, freeing its slot, as soon as its last sample has been rendered. An odd final element of `dst` is set to 0.
- `Reset` stops every voice.

### Spatial helper

`audio.Spatialize(listenerX, listenerY, sourceX, sourceY, hearing)` returns `gain = 1 − distance / hearing` and `pan = 2 × (sourceX − listenerX) / hearing` clamped to [-1, 1]. A source at or beyond `hearing`, a `hearing` that is not positive, and any NaN argument give a gain and pan of 0. The function is pure and allocation free, and the game chooses the units, usually logical screen pixels from the camera's view.

### Hosts

- The desktop host opens a 48 kHz, two-channel, 32-bit float playback device. Its callback applies a changed generation by resetting the mixer, starts the pending requests with `Mixer.Start`, renders with `Mixer.Render` into a buffer that it reuses, and writes the frames to the device. After the buffer has grown to the largest callback size, the callback allocates nothing.
- The browser host keeps one `AudioBuffer` per clip, created at startup. For each request it creates an `AudioBufferSourceNode`, a `GainNode` with the request's gain, and a `StereoPannerNode` with its pan, connects them in that order to the context's destination, and starts the source at once. It keeps the 16 most recent voices in a ring and stops the oldest before starting a 17th. A changed generation, a suspension, focus loss, and shutdown stop every voice. Ended sources stay in the ring until they are replaced or cleared, because stopping an ended source is harmless.
- The browser host requires `AudioContext`, `AudioBuffer`, and `StereoPannerNode`. Without them, or after a Web Audio exception, it logs once and the game runs silently. The M11 rules for unlock in a trusted gesture, dropping requests before unlock, and not replaying cleared requests are unchanged.
- Both hosts log availability and failures outside the hot path and never log individual requests.

## Errors and limits

- At most 16 clips per bank, 16 voices, and 64 pending requests. Requests beyond these limits are dropped or replace the oldest voice as described above; none is an error.
- Both hosts mix in floating point. The browser renders each voice through its own nodes, so the two hosts can differ by rounding and by the browser's resampler when the device rate is not 48 kHz. The browser destination does not clamp the way `Render` does, so a sum above 1 can clip differently in the device. Games should keep the sum of overlapping gains moderate.

## Verification

- `go test ./audio ./internal/sound ./engine` covers the pan law at -1, 0, and 1, equal power, clamping, gain and pan sanitizing, replacement of the oldest voice, voices that end exactly at the clip's end, silence, determinism, zero allocations of the mixer, `Spatialize`, and the queue, and a nested runtime that forwards `Tick.Sound`. The same tests pass under WebAssembly with `GOOS=js GOARCH=wasm go test -exec="$(go env GOROOT)/lib/wasm/go_js_wasm_exec"`.
- `node scripts/smoke-mixer.mjs` runs `go run ./cmd/mixerref`, which prints a fixed script of seven overlapping requests with different gains and pans and the Go mixer's output for it, renders the same script in Chromium through an `OfflineAudioContext` with the browser host's per-voice graph, and requires the two to agree within 2% RMS. The graph in the script mirrors `webAudio.start` in [the browser host](../internal/platform/browser/audio_js.go); it does not run the host's Go code. On an Apple M4 Pro with Chromium 159 the difference was 6.2 × 10⁻⁶ % RMS.
- The native Beacon audio smoke (`beacon --audio`, built with `-tags goje_smoke,goje_beacon_smoke`) plays Beacon's centered cue and a second voice at gain 0.5 panned right in the same tick, through miniaudio's Null device or the real device, and requires the device callback's frames to equal a reference `Mixer`'s output exactly, followed by silence after reset. The Chromium Beacon smoke still checks the cue's samples at the source node, unlock, focus loss, and the silent fallbacks.
- `go test -bench MixerRender ./audio` mixes 16 voices into one 512-frame buffer, which lasts 10.7 ms. On an Apple M4 Pro with Go 1.27.4 it took 6.7 µs natively and 32 µs as WebAssembly under Node 24, with no allocations on either.

## References

- [RFC 0016](../rfcs/0016-m15-audio-mixing-vision-and-platform-reach.md) and [RFC 0012](../rfcs/0012-m11-audio-and-sound-maker.md).
- [Audio guide](../docs/audio.md).
- [`audio/mixer.go`](../audio/mixer.go), [`audio/spatial.go`](../audio/spatial.go), [`internal/sound/queue.go`](../internal/sound/queue.go), [`internal/platform/desktop/audio.go`](../internal/platform/desktop/audio.go), and [`internal/platform/browser/audio_js.go`](../internal/platform/browser/audio_js.go).
- [Web Audio `StereoPannerNode` panning algorithm](https://webaudio.github.io/web-audio-api/#stereopanner-algorithm).
