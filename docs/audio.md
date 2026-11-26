# Play sounds

Goje plays short prepared clips and one looping music track on desktop and in the browser. A game gives the host its clips and tracks at startup and requests them from `Update` by number, sounds with a gain and a stereo pan. Sound requests overlap: each one becomes a voice, and up to 16 voices play at once. The exact rules are in the [audio mixer specification](../specs/audio-mixer-m15.md) and the [music specification](../specs/music-m17.md).

## Prepare clips

Use mono, 48 kHz, signed 16-bit PCM WAV files of at most three seconds; the [Sound Maker](playground.md#make-a-sound-effect-in-the-browser) produces them. Decode each file once at startup with `audio.DecodeWAV` and pass the clips, at most 16, in `platform.Config.Sounds`. The index of a clip in that slice is its cue number. Leaving `Sounds` empty disables audio, and the game runs the same way.

```go
config.Sounds = []audio.Clip{bolt, hit, click} // cues 0, 1, and 2
```

## Play overlapping and positional sounds

`tick.PlaySound(cue)` plays a cue at full gain in the center. `tick.PlaySoundAt(cue, gain, pan)` sets the gain, from 0 to 1, and the pan, from -1 (left) to 1 (right). Values outside those ranges are clamped and NaN is treated as 0. Both return at once and never fail; call them only from `Update`.

For a sound that comes from a place, `audio.Spatialize` turns the listener's and the source's positions into a gain and a pan. The gain falls linearly from 1 at the listener to 0 at `hearing` units away, and the pan is the horizontal offset divided by half of `hearing`. Goje does not know about cameras or worlds, so use whatever coordinates make sense to the player, usually the screen position of the source and the center of the view:

```go
gain, pan := audio.Spatialize(viewCenterX, viewCenterY, boltScreenX, boltScreenY, 400)
tick.PlaySoundAt(cueBolt, gain*s.volume, pan)
```

The pan follows an equal-power law, the one the browser's stereo panner uses, so a sound keeps the same loudness as it moves across. A centered sound plays at about 0.707 of its amplitude in each speaker, so `PlaySound` cues are about 3 dB quieter than in M11, which sent each cue at full amplitude to a mono output.

## Voice limits and what to rate-limit

The host plays at most 16 voices. A 17th request stops the oldest voice. At most 64 requests wait between two host passes, and further requests are dropped. Nothing reports a dropped request or a replaced voice to the game.

Those limits protect the device, not the mix. A fight that requests the same bolt on every tick of every guard fills the voices with copies of one sound and cuts off the alarm that matters. Decide in the game what is too frequent. The outpost, for example, plays at most one voice per cue every three ticks. Count ticks in your scene's own state rather than reading `Tick.Number`, so that replays behave the same.

Overlapping voices add up. Keep the sum of the gains of sounds that often play together near 1 or below. Desktop output clamps a louder sum to full scale, and browsers clip it in their own way.

## Volume and mute

Mute and volume belong to the game. Multiply the gain you pass by your volume setting, and pass 0, or skip the request, when muted. Goje has no master volume. Keep these settings out of replayed state: they do not change the match, so they belong next to the other presentation settings, not in the scene's hash or snapshot.

## Play music

A music track is a prepared mono 48 kHz clip that the host loops. You can render a score at startup as shown below, or load a mono 48 kHz, 16-bit FLAC with `audio.DecodeFLAC`. FLAC saves space in the bundle; after decode it uses the same memory and playback path as a score. The [FLAC specification](../specs/music-flac-m18.md) gives the limits and the Beacon conversion recipe. Pass at most 16 tracks in `platform.Config.Music`; the index is the track number.

Write a score in the Sound Maker's Music panel, or by hand following the [score specification](../specs/music-m17.md):

```go
//go:embed theme.json
var themeScore []byte

score, err := audio.DecodeScore(themeScore)
if err != nil {
	return err
}
theme, err := audio.RenderScore(score)
if err != nil {
	return err
}
config.Music = []audio.Clip{theme} // track 0
```

From `Update`, `tick.PlayMusic(track, gain, fadeMS)` starts a track, fading it in. Asking for another track fades the current one out and the new one in; asking for the track that is already playing only changes its gain, so a title screen and a match can both ask for the same theme without restarting it. `tick.SetMusicGain(gain, fadeMS)` ducks or restores it, for example under a pause menu, and `tick.StopMusic(fadeMS)` fades it out. Fades are in milliseconds, up to 10 seconds.

```go
tick.PlayMusic(0, 0.6*s.volume, 500)
tick.SetMusicGain(0.2*s.volume, 200) // pause menu opened
tick.StopMusic(1000)                  // match over
```

Music is not cleared with sounds. It keeps playing across scene changes, restarts, and focus loss, and stops only when asked or when the game ends. Requests made before the browser's first click are kept and take effect then. Music requests share the 64-request queue with sounds, so request music when it should change, not on every update. The music track plays centered at its gain, on top of the sounds, so leave room for both: keep the music gain well below 1.

## Lifecycle

- Browsers start audio only after a trusted click, tap, or key press. Sound requests made before that are dropped and are not played later; music requests wait.
- Focus loss, page suspension, and scene exit, including a restart, clear pending sound requests and stop every sound voice. Nothing resumes when the game comes back. Music is not cleared; a suspended browser page pauses it.
- Without an audio device, or when the browser lacks Web Audio, the host logs once and the game runs silently.

## Nested runtimes and replays

A sound request is output. A replay records input, so playing a recording through the same scene requests the same sounds as the original session. A scene that runs another `engine.Runtime` inside it, such as a replay viewer, passes its own output on before each step, so the nested scenes are heard:

```go
v.playback.SetSoundOutput(tick.Sound())
err := v.playback.Step(engine.Input{})
```

The nested runtime shares the queue. When its scene exits, for example when the viewer stops it, every voice stops, including voices the outer scene started. Without `SetSoundOutput`, a nested runtime plays nothing. The [replay guide](replays.md#play-a-recording) shows a complete viewer.

## Check your change

- `go test ./audio ./internal/sound ./engine` runs the mixer, music, score, queue, and runtime tests. Add `GOOS=js GOARCH=wasm` and `-exec="$(go env GOROOT)/lib/wasm/go_js_wasm_exec"` to run them as WebAssembly under Node. `go test -fuzz FuzzDecodeScore ./audio` fuzzes the score decoder.
- `node scripts/smoke-mixer.mjs` compares the Go mixer with the browser's Web Audio graphs for a fixed script of overlapping voices and one of music requests in Chromium. Run it with `PLAYWRIGHT_BROWSERS_PATH` set to the repository's Playwright cache.
- `make smoke-beacon-audio` checks the mixed output of the desktop device callback and the browser's playback in Beacon.
- `go test -run '^$' -bench MixerRender ./audio` reports the time to mix 16 voices into one 512-frame buffer, which lasts 10.7 ms.
