# Audio mixer, M15

## Scope and ownership

This contract defines how a Goje game requests overlapping and positional sounds and how the desktop and browser hosts mix them. It replaces the one-active-cue rule of [RFC 0012](../rfcs/0012-m11-audio-and-sound-maker.md): a new cue no longer stops the cue that is playing, and every accepted request becomes its own voice. The [M15 RFC](../rfcs/0016-m15-audio-mixing-vision-and-platform-reach.md) records the decisions and alternatives.

M17 adds one looping music voice, three music request kinds, and a music clip limit, as [RFC 0018](../rfcs/0018-m17-versus-music-tools-and-reach.md) decided. With no music tracks and no music requests, every rule below behaves as in M15. The score format and the game's music requests are in the [music specification](music-m17.md).

Goje owns the request interface in [`engine`](../engine/runtime.go), the bounded queue in [`internal/sound`](../internal/sound/queue.go), the voice pool, the music voice, and the pan law in [`audio.Mixer`](../audio/mixer.go), the [`audio.Spatialize`](../audio/spatial.go) helper, and the two host players. The game owns which cues it requests, how often, how loud, and from where: rate limits, priorities, mute, volume, and the meaning of positions are game policy. Sound clip format and decoding stay as M11 defines them in [`audio.DecodeWAV`](../audio/wav.go): mono, signed 16-bit PCM at 48 kHz, at most three seconds (`audio.MaxSoundSeconds`), and at most 16 clips per bank. A music track is a clip of the same format of at most 64 seconds (`audio.MaxMusicSeconds`), from `audio.RenderScore`, and a bank holds at most 16 tracks. The queue rejects a sound clip longer than three seconds.

## Contract

### Requests

- `engine.SoundOutput` is `Play(cue int, gain, pan float32)` and `Clear()`. `Tick.PlaySoundAt(cue, gain, pan)` calls `Play` when the runtime has an output and does nothing otherwise. `Tick.PlaySound(cue)` is `PlaySoundAt(cue, 1, 0)`.
- `Tick.Sound()` returns the runtime's output, which may be nil. A scene that runs a nested `engine.Runtime` passes it to the nested runtime's `SetSoundOutput`, so the nested scenes request sound through the same queue. When a nested runtime's scene exits, it calls `Clear` on that shared output.
- Gain is linear amplitude from 0 to 1. Pan runs from -1, fully left, to 1, fully right. The queue clamps both and replaces NaN with 0 before a host sees them, so both hosts receive the same values.
- Sound is output. A replay records input only, and playing a recording through the same scene requests the same sounds. Requests never change simulation state, and a missing device or a dropped request never affects gameplay.

### Queue

- The queue has one producer, the game loop, and one consumer, the host player, which on desktop is the device callback thread.
- Each request has a kind: sound, play music, stop music, or set music gain. A music request carries a track, for play, a gain, for play and set, and a fade in milliseconds clamped to 0 through 10,000 (`audio.MaxFadeMS`).
- It holds at most 64 pending requests of all kinds. Enqueueing never blocks, allocates, or calls a device; a request that does not fit is dropped, and accepted requests keep their order.
- Sound requests are dropped while the device is not ready (before the first browser unlock, after a suspension, or without a device) and when the cue is outside the bank. Music requests are accepted while the device is not ready, and wait; a play request for a track outside the music bank, and any music request without a music bank, is dropped.
- `Clear` increments a generation and tells the player to stop every sound voice. It removes nothing: the consumer skips sound requests from an older generation, so a sound request accepted before a clear is never started after it. Music requests and the music voice are not affected. The runtime clears on every scene exit, and the hosts clear on focus loss and suspension.
- When the browser host's context leaves the running state, it disables the queue, which clears and also removes the pending sound requests, keeping the music requests in order. Only a host whose consumer is not running at the same time does this.
- The consumer takes the pending requests in order, at most 64 per pass, so a producer that keeps adding requests cannot extend a device callback.

### Mixer

- `audio.Mixer` holds up to `audio.MaxVoices`, 16, sound voices and one music voice. Each sound voice is a clip, its position, and two channel gains. A Mixer allocates nothing, including in `Start`, `Render`, `Reset`, and the music methods.
- `Start(clip, gain, pan)` adds a voice at the clip's first sample. When all 16 voices are playing, it first removes the voice that started earliest. It clamps gain and pan as the queue does, and it ignores a zero clip.
- The pan law is equal power. A voice's left gain is `gain × cos((pan + 1) × π/4)` and its right gain is `gain × sin((pan + 1) × π/4)`. This is the law the Web Audio `StereoPannerNode` applies to a mono input, so a centered voice plays at about 0.707 of its amplitude on each channel, and the squared channel gains always sum to the squared gain.
- `Render(dst)` overwrites `dst` with `len(dst)/2` interleaved stereo frames at 48 kHz, left first. Each sample is the 16-bit value divided by 32768. The sound voices are added in start order, then the music voice, then each output sample is clamped to [-1, 1]. Every voice advances by the frames rendered, and a sound voice ends, freeing its slot, as soon as its last sample has been rendered. An odd final element of `dst` is set to 0.
- `Reset` stops every sound voice. The music voice continues.

### Music voice

- The music voice plays one track, centered and unpanned: each frame adds `sample × gain` to both channels. This is what a mono Web Audio source connected through a `GainNode` to a stereo destination plays. The track loops: after its last sample it continues from its first.
- The gain changes along linear ramps measured in frames, 48 per millisecond. Each frame is rendered with the current gain, and then the gain moves `(target − gain) / remaining` toward the target, so after `n` frames of an `n`-frame ramp it equals the target exactly. A new ramp starts from the current gain. A fade of zero sets the gain at once.
- `PlayMusic(clip, gain, fadeMS)`: with no track playing, the clip starts at its first sample with gain 0 and ramps to `gain`. If the clip is the playing track, sharing its samples, and the track is not stopping, only the gain ramps to `gain`, and the position is kept. Otherwise the current track ramps to 0, and on the frame after the ramp ends the clip starts at its first sample with gain 0 and ramps to `gain`, both over `fadeMS`. A later play request during that fade replaces the pending clip and restarts the fade out from the current gain. A zero clip is ignored.
- `StopMusic(fadeMS)` drops a pending clip, ramps the gain to 0, and then stops the track. It does nothing when no track plays.
- `SetMusicGain(gain, fadeMS)` ramps the playing track to `gain`. During a replacement it sets the gain the pending clip will ramp to and keeps the fade out. It does nothing while no track plays or the track is stopping.
- Gain is clamped as for `Start`, and fades as for the queue.

### Spatial helper

`audio.Spatialize(listenerX, listenerY, sourceX, sourceY, hearing)` returns `gain = 1 − distance / hearing` and `pan = 2 × (sourceX − listenerX) / hearing` clamped to [-1, 1]. A source at or beyond `hearing`, a `hearing` that is not positive, and any NaN argument give a gain and pan of 0. The function is pure and allocation free, and the game chooses the units, usually logical screen pixels from the camera's view.

### Hosts

- The desktop host opens a 48 kHz, two-channel, 32-bit float playback device with a 40 ms period. miniaudio's default 10 ms period crackled on a MacBook's built-in speakers although each callback arrived on time and took under 150 µs. Its callback applies a changed generation by resetting the mixer, applies the pending requests in order, sound requests with `Mixer.Start` and music requests with the music method of the same name, renders with `Mixer.Render` into a buffer that it reuses, and writes the frames to the device. After the buffer has grown to the largest callback size, the callback allocates nothing. A sound can therefore start up to one period after the request that asked for it.
- The browser host keeps one `AudioBuffer` per clip and per music track, created at startup. For each sound request it creates an `AudioBufferSourceNode`, a `GainNode` with the request's gain, and a `StereoPannerNode` with its pan, connects them in that order to the context's destination, and starts the source at once. It keeps the 16 most recent voices in a ring and stops the oldest before starting a 17th. A changed generation, a suspension, focus loss, and shutdown stop every sound voice. Ended sources stay in the ring until they are replaced or cleared, because stopping an ended source is harmless.
- The browser music voice is a looping `AudioBufferSourceNode` connected through a `GainNode` to the destination. The host applies music requests at the context's current time with the same rules as the mixer: a ramp is `cancelScheduledValues`, `setValueAtTime` with the ramp's current value, and `linearRampToValueAtTime`; a replacement ramps the current track to 0 and schedules the next track's source to start, at gain 0, when that ramp ends. The host releases a faded-out track at its next frame after the fade ends. A suspended context pauses the music with it, and the music resumes when the context runs again. Shutdown stops it.
- The browser host requires `AudioContext`, `AudioBuffer`, and `StereoPannerNode`. Without them, or after a Web Audio exception, it logs once and the game runs silently. The host takes no requests while the context is not running, so music requests wait for the first unlock. The M11 rules for unlock in a trusted gesture, dropping sound requests before unlock, and not replaying cleared sound requests are unchanged.
- Both hosts log availability and failures outside the hot path and never log individual requests.

## Errors and limits

- At most 16 clips per bank, 16 music tracks, 16 sound voices, one music voice, and 64 pending requests. Requests beyond these limits are dropped or replace the oldest voice as described above; none is an error. A game that requests music on every update can fill the queue; request music when it should change.
- Both hosts mix in floating point. The browser renders each voice through its own nodes, so the two hosts can differ by rounding and by the browser's resampler when the device rate is not 48 kHz. The browser destination does not clamp the way `Render` does, so a sum above 1 can clip differently in the device. Games should keep the sum of overlapping gains moderate.

## Verification

- `go test ./audio ./internal/sound ./engine` covers the pan law at -1, 0, and 1, equal power, clamping, gain and pan sanitizing, replacement of the oldest voice, voices that end exactly at the clip's end, silence, determinism, zero allocations of the mixer, `Spatialize`, and the queue, and a nested runtime that forwards `Tick.Sound`. For music it covers the loop and its wrap, a fade in, a replacement on the exact frame, a gain set during a replacement, a stop, a same-track request that keeps the position, clamping, zero allocations, music requests kept across a clear and an unready device in order, and the engine's forwarding to `MusicOutput`. The same tests pass under WebAssembly with `GOOS=js GOARCH=wasm go test -exec="$(go env GOROOT)/lib/wasm/go_js_wasm_exec"`.
- `node scripts/smoke-mixer.mjs` runs `go run ./cmd/mixerref`, which prints a fixed script of seven overlapping requests with different gains and pans and the Go mixer's output for it, renders the same script in Chromium through an `OfflineAudioContext` with the browser host's per-voice graph, and requires the two to agree within 2% RMS. The graph in the script mirrors `webAudio.start` in [the browser host](../internal/platform/browser/audio_js.go); it does not run the host's Go code. On an Apple M4 Pro with Chromium 159 the difference was 6.2 × 10⁻⁶ % RMS. The same command compares a music script, which fades in a track rendered from a score, ducks it, replaces it with a second track while changing the pending gain, ramps it again, and stops it, with the scheduling of [`webMusic`](../internal/platform/browser/music_js.go) mirrored in the script, within 2% RMS; it also requires silence after the stop and that a reference shifted by 0.1 s fails the comparison. On the same machine the music difference was 1.4 × 10⁻³ % RMS.
- The native Beacon audio smoke (`beacon --audio`, built with `-tags goje_smoke,goje_beacon_smoke`) plays Beacon's centered cue and a second voice at gain 0.5 panned right in the same tick, through miniaudio's Null device or the real device, and requires the device callback's frames to equal a reference `Mixer`'s output exactly, followed by silence after reset. The Chromium Beacon smoke still checks the cue's samples at the source node, unlock, focus loss, and the silent fallbacks.
- `go test -bench MixerRender ./audio` mixes 16 voices into one 512-frame buffer, which lasts 10.7 ms. On an Apple M4 Pro with Go 1.27.4 it took 6.7 µs natively and 32 µs as WebAssembly under Node 24, with no allocations on either.

## References

- [RFC 0016](../rfcs/0016-m15-audio-mixing-vision-and-platform-reach.md) and [RFC 0012](../rfcs/0012-m11-audio-and-sound-maker.md).
- [Audio guide](../docs/audio.md).
- [`audio/mixer.go`](../audio/mixer.go), [`audio/spatial.go`](../audio/spatial.go), [`internal/sound/queue.go`](../internal/sound/queue.go), [`internal/platform/desktop/audio.go`](../internal/platform/desktop/audio.go), [`internal/platform/browser/audio_js.go`](../internal/platform/browser/audio_js.go), and [`internal/platform/browser/music_js.go`](../internal/platform/browser/music_js.go).
- [Music specification](music-m17.md) and [RFC 0018](../rfcs/0018-m17-versus-music-tools-and-reach.md).
- [Web Audio `StereoPannerNode` panning algorithm](https://webaudio.github.io/web-audio-api/#stereopanner-algorithm).
