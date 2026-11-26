# Music, M17

## Scope and ownership

This contract defines the music score format, how a score becomes a looping clip, and how a game requests music. The [M17 RFC](../rfcs/0018-m17-versus-music-tools-and-reach.md) records the decisions and alternatives. The mixer's music voice, the request kinds, and the host rules are in the [audio mixer specification](audio-mixer-m15.md#music-voice).

Goje owns [`audio.Score`](../audio/score.go), `audio.DecodeScore`, `audio.EncodeScore`, `audio.RenderScore`, the music requests on `engine.Tick`, and the Sound Maker's Music panel. The game owns which tracks exist, when they play, and how loud. Compressed or streamed music, samples, and per-note effects are out of scope.

## Score format, version 1

A score is one UTF-8 JSON object of at most 64 KiB (`audio.MaxScoreBytes`):

```json
{
  "version": 1,
  "tempo": 120,
  "steps": 8,
  "channels": [
    {
      "waveform": "square",
      "notes": [
        {"step": 0, "pitch": 69, "length": 2, "level": 100}
      ]
    }
  ]
}
```

| Field | Rule |
| --- | --- |
| `version` | 1. |
| `tempo` | Beats per minute, 40 to 300. A step is a sixteenth note, so one step lasts `floor(48000 × 15 / tempo)` frames. |
| `steps` | The loop length in steps, 1 to 1,024. The loop, `steps × step frames`, is at most 64 seconds. |
| `channels` | 1 to 4 channels. |
| `waveform` | `sine`, `triangle`, or `square`, the Sound Maker's oscillators. |
| `notes` | The channel's notes in step order. `null` and `[]` both mean no notes. A note starts at or after the end of the previous one, so a channel plays one note at a time. |
| `step` | The zero-based first step. |
| `pitch` | A MIDI note number from 24 (C1) to 108 (C8); 69 is A4 at 440 Hz. |
| `length` | 1 or more steps, ending within the loop: `step + length ≤ steps`. Notes do not wrap. |
| `level` | 1 to 100, a percentage of the channel's amplitude. |

All numbers are JSON integers. `DecodeScore` rejects a larger file, malformed JSON, unknown fields, data after the object, fractional numbers, and any value outside these rules. A decoded score is a trust boundary like the other decoders: limits are checked before rendering allocates, and the largest valid score renders at most 64 seconds.

`EncodeScore` writes the canonical form: the fields in the order above, two-space indentation, one note per line as `{"step": S, "pitch": P, "length": L, "level": V}`, `[]` for a channel without notes, and a final newline. Encoding a decoded canonical file gives the same bytes.

## Rendering

`RenderScore` returns a clip of exactly `steps × step frames` mono 16-bit samples at 48 kHz. For each note, in channel order and then note order:

- Its frequency is `440 × 2^((pitch − 69) / 12)` Hz. The oscillator starts at phase zero at the note's first frame and advances by `frequency / 48000` cycles per frame.
- Its amplitude is `0.25 × level / 100`. Because each channel plays one note at a time, four channels sum to at most full scale and never clip.
- Its envelope rises linearly over the first 240 frames (5 ms) from zero, and falls linearly to zero over the last 960 frames (20 ms), taking the smaller of the two where they overlap. The shortest note, one step at 300 beats per minute, is 2,400 frames, so both fit.
- The sum is quantized as `round(sum × 32767)`.

The same score gives the same samples on every host Goje supports; the computation is the one the Sound Maker's effects use.

## Requesting music

- `platform.Config.Music` holds up to 16 prepared tracks from `RenderScore` or [`DecodeFLAC`](music-flac-m18.md), identified by zero-based index. A game with only music and no sounds still gets an audio host.
- `Tick.PlayMusic(track, gain, fadeMS)`, `Tick.StopMusic(fadeMS)`, and `Tick.SetMusicGain(gain, fadeMS)` forward to the output when it implements `engine.MusicOutput`, and do nothing otherwise. The host's output always implements it. `engine.SoundOutput` is unchanged.
- Gain is linear, 0 to 1, clamped with NaN as 0. Fades are milliseconds, clamped to 0 through 10,000 (`audio.MaxFadeMS`).
- Music is output, like sound: replays and simulation never depend on it.
- Music continues across scene changes, `Clear`, focus loss, and suspension; only `StopMusic` and the end of `platform.Run` stop it. Requests made before the browser's first unlock are kept and take effect when audio starts.
- Music requests share the 64-entry queue with sound requests. Request music when it should change, not on every update.

## The Music panel

The Sound Maker's Music panel shows the canonical JSON of the current score in a text field. **Apply score** decodes and renders the text; a rejected score leaves the current one, and the diagnostic is shown. **Play loop** loops the rendered samples, **Stop loop** stops them, and **Download score** saves the canonical JSON as `score.json`.

## Verification

- `go test ./audio ./internal/sound ./internal/soundmaker ./engine` covers the canonical round trip, every rejection, rendering length, envelopes, and determinism, the mixer's loop, fades, replacement, stop, and zero allocations, the queue's music requests, and the engine's forwarding. `go test -fuzz FuzzDecodeScore ./audio` fuzzes the decoder, re-encoding, and rendering.
- `node scripts/smoke-mixer.mjs` compares the Go music voice with the browser's scheduling for a fixed script.
- `node scripts/smoke-sound-maker.mjs` edits, rejects, exports, and loops a score in Chromium.

## References

- [RFC 0018](../rfcs/0018-m17-versus-music-tools-and-reach.md)
- [Audio mixer specification](audio-mixer-m15.md)
- [Audio guide](../docs/audio.md#play-music)
