# RFC 0012: M11 — Game audio and Sound Maker

**Status:** Implemented and verified for M11. The native output backend passed the playback feasibility check.

## Problem and goal

Goje can take painted and animated assets from its browser editors into [Beacon and the authored courtyard](../docs/playground.md#paint-animate-and-play-a-sprite), but neither game can play a sound. A developer should be able to make a short cue, download it as a WAV, and use that exact file in a desktop and web game. Audio is optional: the game must behave the same when playback is unavailable, muted, or locked by the browser.

## Decisions for review

| Decision | Recommended | Alternative and tradeoff |
| --- | --- | --- |
| Delivery | Prove one-shot playback in Beacon before writing the Sound Maker. | Build the editor first. A preview would be usable sooner, but it would not prove that Goje can ship its exports on either target. |
| Asset format | Use mono signed 16-bit PCM RIFF/WAVE at 48 kHz, at most three seconds per clip and 300 KiB per input. | Accept several codecs, formats, and rates. This would handle more existing assets but expand decoding, validation, and conversion before we have a playback path. |
| Runtime | Use optional host-owned playback with a bounded queue and one active cue; a new accepted cue replaces the current one. | Ship a general-purpose mixer. Overlapping sounds are useful, but a first Beacon cue does not need voice allocation or mix policy. |
| Authoring | Build a standalone browser editor with waveform, start and end pitch, duration, and an attack/release envelope. | Add multitrack editing or import recorded samples. Those workflows need a durable project format and different controls. |

## Proposed design

### Asset and authoring

- Go owns WAV validation, PCM generation, and encoding; desktop and web use the same decoder. Validate the encoded size before allocating samples; reject unsupported format fields, out-of-bounds chunk lengths, missing or duplicate format/data chunks, partial samples, empty clips, and clips beyond the duration limit. Validate and prepare configured assets before steady-state ticks. Invalid assets return a clear startup error; an unavailable output device does not.
- Sound Maker previews the same generated samples it encodes. Waveform choices start with sine, triangle, and square. Pitch is measured in hertz and durations in milliseconds. Bound duration, pitch, and envelope controls; the attack and release must fit in the clip. A short envelope fades the sound in and out rather than ending at a discontinuity. Repeated export of unchanged settings produces the same bytes within the same build.
- The browser editor does not upload files or overwrite an existing one. WAV is a playback artifact, not an editable preset; opening a WAV to reconstruct synth controls is not part of this slice. Revoke temporary download URLs and dispose of the preview audio context when it is no longer needed.

### Playback and lifecycle

- A game opts in when it launches the host, like an optional presentation resource. The host owns device startup, sample transfer, and cleanup. Games request playback by a prepared cue identifier, never by opening a file or calling a device from `Update`. Beacon requests one cue on its existing click-or-Space toggle, including when both inputs arrive in the same tick. Reset does not produce a cue.
- Enqueueing an enabled cue must not allocate, block, decode, or call a platform device from the simulation tick. Playback runs outside the tick and draw paths. When the bounded queue fills, drop the incoming request rather than waiting. Disabled audio does no playback setup or per-tick audio work. Audio completion, playback failure, and device state never affect gameplay state or timing.
- On web, request audio unlock from a trusted pointer or keyboard event, not from a later simulation tick. Drop requests before unlock succeeds; do not play a burst of stale cues after a gesture. Clear pending cues and stop the active cue on focus loss or page suspension; do not replay them on resume. Clear old scene cues on restart and release resources on shutdown. Keep these rules consistent with the [runtime lifecycle](../specs/runtime-m1.md#host-timing-and-input).
- Start the playback spike with the browser's Web Audio API and evaluate narrowly scoped native output libraries for macOS and Linux. Leave the native choice open until the spike confirms device lifecycle, runtime dependencies, and behavior with no device. Do not add an engine framework or use speaker output as a CI dependency.
- Report audio availability and lifecycle failures through the existing optional host logger outside the hot path. Do not log every cue or dropped request. In Beacon, compare repeated disabled, enabled-idle, and burst runs on the same host and browser. Record tick/frame percentiles, dropped ticks, and request-to-output latency at the measured output boundary. Do not set hardware-specific CI limits or label software capture timing as speaker latency.

## Delivery and acceptance

| Slice | Exit check |
| --- | --- |
| 1. Playback foundation | Prove one prepared cue in Beacon on desktop and Chromium. Verify PCM limits, one cue per toggle, bounded overflow, reset and suspension, the pre-unlock drop, and silent operation without a usable device. Demonstrate that the real backend submits non-silent samples after the toggle; no hardware speaker is required in CI. Record the backend choice and the performance observations. |
| 2. Browser Sound Maker | Use keyboard-accessible controls to produce and preview a short effect in Chromium. Download it twice and compare the actual files; decode one to verify its waveform, envelope, duration, and format. Confirm invalid settings do not discard the current sound. |
| 3. Sound-to-play round trip | Embed the exact WAV download in a scratch copy of Beacon. On desktop and Chromium, trigger it with real input and compare captured output against the downloaded samples, accounting for device sample-rate conversion where applicable. Verify no cue on reset or resume and leave checked-in assets unchanged. Document the authoring and playback workflow. |

The second slice is implemented in the [Sound Maker](../docs/playground.md#make-a-sound-effect-in-the-browser). Go owns settings validation, deterministic synthesis, and WAV encoding ([`internal/soundmaker`](../internal/soundmaker/document.go) and [`audio.EncodeWAV`](../audio/encode.go)); the page only edits a draft, draws the waveform, and plays and downloads the same bytes. `make smoke-sound-maker` applies settings, downloads twice and compares the files, decodes one to check format, envelope, duration, and pitch phase, compares the preview buffer with the WAV samples, and confirms rejected settings and a missing Web Audio API change nothing else.

The third slice is implemented in [Play a Sound Maker download in Beacon](../docs/playground.md#play-a-sound-maker-download-in-beacon). `make smoke-sound-roundtrip` exports a square-wave effect from the Sound Maker, embeds that exact file in scratch copies of Beacon, and plays it with real input. On macOS with CoreAudio's callback path and miniaudio's Null device, and in Chromium WebGL with Web Audio at 48 kHz, the rendered samples equal the downloaded samples exactly, and the reset and focus-loss checks from the foundation slice still pass. A one-bit change to the expected file fails the Chromium check. The resampling tolerance for browsers that run at another rate, a real Linux audio device, and speaker latency were not exercised.

## Playback foundation results

Desktop uses [malgo](https://github.com/gen2brain/malgo) v0.11.28, a narrow cgo binding to miniaudio. Goje uses its device callback, not its higher-level engine or mixer. The runtime selects only real backends (CoreAudio on macOS; PulseAudio, ALSA, or JACK on Linux) and degrades to silent gameplay if device initialization fails. The smoke can explicitly select miniaudio's Null backend, so CI exercises PCM delivery without needing a speaker or sound server. Chromium uses Web Audio and prepares buffers before the first gesture.

`make benchmark-beacon-audio` sampled three runs per mode on an Apple M4 Pro (Go 1.27.4, Chromium 159.0.8154.6). Each run discarded 30 warm-up frames and measured 150 frames; burst requested 20 cues per simulation second. The table gives the median across runs of each p95 and p99, in microseconds. Tick and frame columns measure Go work; frame interval is the elapsed time between frames. Chromium's clock resolution was about 100 µs, so zeros are below its resolution, not proof of zero work.

| Host | Mode | Tick p95 / p99 (µs) | Frame p95 / p99 (µs) | Interval p95 / p99 (µs) | Dropped ticks (all runs) |
| --- | --- | ---: | ---: | ---: | ---: |
| macOS | Off | 1.8 / 60.5 | 149.4 / 205.5 | 16,938 / 17,052 | 0 |
| macOS | Idle | 27.7 / 44.8 | 123.6 / 236.3 | 16,826 / 17,206 | 0 |
| macOS | Burst | 2.8 / 53.4 | 96.6 / 175.9 | 16,830 / 17,011 | 0 |
| Chromium | Off | 0 / 100.1 | 199.9 / 199.9 | 17,400 / 17,600 | 0 |
| Chromium | Idle | 0 / 100.1 | 199.9 / 299.8 | 17,500 / 17,600 | 0 |
| Chromium | Burst | 0 / 100.1 | 199.9 / 299.8 | 17,300 / 17,700 | 0 |

The native runs recorded no Go heap allocations during the sample windows. Chromium recorded the same Go allocations in off and idle; burst added three small Go allocations per cue from the Web Audio bridge, outside the tick. The isolated queue benchmark measured 19.7–19.9 ns per enqueue and drain with zero Go allocations.

The PCM smoke observed the cue at the CoreAudio callback after 8.3 ms and at Chromium's software analyser after 18–21 ms. These are sample handoff observations, not speaker latency or CI limits. This run did not exercise a real Linux device. See [Play a sound in Beacon](../docs/playground.md#play-a-sound-in-beacon) for the smoke and usage commands.

## Out of scope

Music sequencing and streaming, overlapping cues and a general mixer, positional audio, microphone recording, importing samples into the editor, project format changes, additional codecs, and native editor packaging. Movement and collision remain a separate engine milestone after audio.
