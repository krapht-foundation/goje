# Record, replay, save, and load

Goje can record a session as the input of every tick, play it back, check that playback ends in the same state, and save and load a game. Recording and playback wrap your scene factory, so the desktop and browser hosts need no change, and the same recording plays on every target. See the [replay format](../specs/replay-v1.md) and the [save format](../specs/save-v1.md) for the exact rules.

## Make a scene replayable

A replay stores input, not results, so playback reproduces a session only if the scene does the same thing with the same input every time. A replayable scene derives all of its state from `tick.Input` and its own history:

- Do not read `Tick.Number`. Count your own updates if you need time.
- Do not read a clock, such as `time.Now`, or measure frame time.
- Do not use randomness, unless the generator's seed and state are part of the scene's own state, so that they are recorded in its history and snapshot.
- Do not depend on map iteration order, goroutine scheduling, or anything the host reports outside `Input`.

Pointer coordinates are recorded exactly, so converting them to cells with floating-point arithmetic is fine.

Two optional interfaces let Goje check and save a scene:

```go
// StateHash digests every piece of state that later updates can read.
func (s *Scene) StateHash() uint64 { ... }

// Snapshot and Restore write and read the whole state.
func (s *Scene) Snapshot() ([]byte, error) { ... }
func (s *Scene) Restore(data []byte) error { ... }
```

A scene that implements `engine.Hasher` gets a checkpoint every 60 ticks and on the last tick, so playback detects where a session first went differently. A scene that implements `engine.Snapshotter` can be saved, and its replays can start from a loaded save. Without `StateHash`, a replay still plays, but nothing checks it.

## Identify your content

A replay or save only makes sense for the content it was made with. Compute a check value once, from the bytes that define your game, and use it everywhere:

```go
content := replay.Meta{
	Game:    "outpost",
	Content: replay.ContentHash(levelJSON, atlasPNG, clipsJSON),
}
```

The game name is 1–32 bytes of lowercase ASCII letters, digits, and underscores, starting with a letter. Playing a replay or loading a save made for another name or content fails with an error wrapping `replay.ErrContentMismatch`, instead of diverging later.

## Record a session

Wrap the factory you pass to the host. Restarts and scene replacements keep recording into the same recorder:

```go
recorder, err := replay.NewRecorder(content, nil, 36_000) // room for ten minutes at 60 Hz
if err != nil {
	return err
}
if err := platform.Run(ctx, config, replay.Record(newScene, recorder)); err != nil {
	return err
}
data, err := replay.Encode(recorder.Finish())
```

`Finish` takes the last state hash and returns the recording. A recording holds at most 2 MiB and 1,048,576 ticks, about 4.8 hours. When it reaches either limit, the recorder stops recording, `recorder.Full()` reports true, and the game keeps running; show it in your HUD if it matters. The size limit comes first when the pointer moves on every tick, after roughly 25 minutes of continuous movement. Recording allocates nothing per tick while the session stays within the capacity you pass, about 150 bytes per tick.

## Play a recording

```go
rec, err := replay.Decode(data)
if err != nil {
	return err
}
player, err := replay.Play(newScene, rec, content)
if err != nil {
	return err // including replay.ErrContentMismatch
}
return engine.Replace(player.Factory()), nil
```

Playback ignores live input. When it finishes, `player.Done()` reports true, and the scene stays on its final state. If a checkpoint does not match, the update returns a `*replay.DivergenceError` naming the tick, and the runtime stops. Pausing, seeking, and changing speed are not part of the package.

Because the playback scene never reads live input, it cannot leave on its own. To let the player stop watching, run playback in a runtime owned by a viewer scene, which reads the live input itself:

```go
func (v *viewer) Enter() (err error) {
	v.playback, err = engine.Start(v.player.Factory())
	return err
}

func (v *viewer) Update(tick engine.Tick) (engine.Transition, error) {
	if tick.Input.Pressed.Has(engine.KeyEscape) {
		return engine.Replace(newMenu), nil
	}
	if v.player.Done() {
		return engine.Transition{}, nil // Show "replay finished" in Draw.
	}
	v.playback.SetSoundOutput(tick.Sound()) // Hear the recording, as in play.
	return engine.Transition{}, v.playback.Step(engine.Input{})
}

func (v *viewer) Draw(canvas engine.Canvas) { _ = v.playback.Draw(canvas) }

func (v *viewer) Exit() { _ = v.playback.Stop() }
```

A playback error from `Step` then ends the viewer scene, and the outer runtime reports it. A player plays once, so create a new one with `replay.Play` for each viewing. Sound is output, so a replay does not record it; playback requests the same sounds as play because it runs the same scene. The inner runtime plays them only through the output that the viewer passes from `Tick.Sound`, and stopping it clears that shared output, as described in the [audio guide](audio.md#nested-runtimes-and-replays).

## Verify a recording in a test

`replay.Verify` plays a recording without a window, natively or under WebAssembly:

```go
err := replay.Verify(newScene, rec, content)
var divergence *replay.DivergenceError
if errors.As(err, &divergence) {
	t.Fatalf("the session diverged at tick %d", divergence.Tick)
}
```

Keep a recorded session as a test fixture and verify it on every target to catch changes in behavior and differences between targets.

## Save and load

Wrap a snapshot in a save, which adds the game, the content check, and a checksum:

```go
snapshot, err := scene.Snapshot()
if err != nil {
	return err
}
data, err := save.Encode(content, snapshot)
if err != nil {
	return err
}
err = store.Put("save", data)
```

To load, decode it, check it, and restore it:

```go
data, err := store.Get("save")
if errors.Is(err, platform.ErrNotFound) {
	// No save yet.
}
meta, snapshot, err := save.Decode(data)
if err == nil {
	err = meta.Check(content)
}
if err == nil {
	err = scene.Restore(snapshot)
}
```

A damaged, truncated, or foreign save is refused. A save is limited to 1 MiB of snapshot.

To record a session that begins from a loaded save, pass the snapshot to the recorder and restore the same snapshot into the first recorded scene before its first update. Playback restores it into the first playback scene. The start state applies only to the first scene, so a restart within the recording starts from your factory's fresh state.

```go
recorder, err := replay.NewRecorder(content, snapshot, 36_000)
```

Loading a save in the middle of a recording changes the state without input, so the rest of that recording no longer replays. Start a new recording after a load.

## Keep saves and replays

`platform.NewStore(game)` opens a small key-value store for your game:

```go
store, err := platform.NewStore("outpost")
if err != nil {
	// No storage, for example because the browser blocks site data. Play without saving.
}
```

Keys are 1–32 bytes of lowercase letters, digits, underscores, and hyphens, starting with a letter, and values are at most 4 MiB. A failed `Put` keeps the previous value. On desktop, values are files in a per-game directory under your user configuration directory, such as `~/Library/Application Support/goje/outpost` on macOS or `~/.config/goje/outpost` on Linux, written by replacing a temporary file. In the browser, values are base64 text in local storage under `goje/<game>/`. Browsers allow about 5 million characters per site, so a value near 4 MiB may not fit; `Put` then returns an error.

## Cost

Recording adds one comparison per tick and, at each checkpoint, one call to your scene's `StateHash`. `go test -bench OutpostReplayVerify ./cmd/playground` verifies a recording of the outpost's scripted fight, a busy battle of 603 ticks, in about 1.2 to 1.7 milliseconds natively and 3.8 milliseconds as WebAssembly under Node on an Apple M4 Pro. A five-minute recording of 18,000 ticks would take about 40 to 50 milliseconds natively, far below the one second per 600 ticks that RFC 0015 allowed. The first verification of a scene allocates about 170 KiB for the new match.
