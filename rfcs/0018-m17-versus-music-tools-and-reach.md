# RFC 0018: M17 - Versus, tactics, online replays, music, authored vision, a font editor, and reach

**Status:** Approved for implementation, with Windows verified on CI and reverted if it cannot pass, the mobile item delivered as an installable web app, and flanking included in the guards' tactics.

## Problem and goal

[M16](0017-m16-online-lockstep-play.md) let two players command the outpost's scouts together over a network. Its [next-milestone list](0017-m16-online-lockstep-play.md#next-milestone) named three groups of work that it did not do, and the project owner has asked for all of them in M17:

- **Versus.** The second player commands the guards. Each team has its own fog and its own win rule.
- **Depth.** Tactical guards, replays of online matches, music, vision that a level author controls, and an editor for the built-in bitmap font.
- **Reach.** A Windows package, a path to Android and iOS, signing, and updates.

These share one aim: after M17 a game made with Goje can be played against a person, watched afterwards, authored in the browser tools without editing Go constants, heard with music, and handed to players on the three desktop systems with a way to tell that a download is genuine. The milestone is large, so each item is a separate slice with its own exit check, and where an item cannot be fully verified on the machines available, this document says so before the work starts.

The owner asked for a "focused" milestone but ticked every item. This RFC takes the items as the scope and keeps each one narrow instead. Section [Out of scope](#out-of-scope) lists what each item leaves out.

## Decisions for review

| Decision | Recommended | Alternative and tradeoff |
| --- | --- | --- |
| Versus teams | Player 0 commands the scouts and player 1 the guards. The guards' built-in AI is off for a human-commanded team. Each team has its own explored set, fog, and a win rule that it wins when the other team is gone. The alarm zones have no meaning and are absent from the versus level. | Keep the guards' AI and add a second human only to the scouts. That is co-op, which exists. A mirrored team with the same units and no alarm needs a new level file, which is cheap. |
| Starting a versus or tactical match | The host chooses the mode and the guards' behavior in the title menu. When the guest joins, the host sends a one-message `Setup` before the lockstep session starts, and the guest builds the same match. A guest that receives an unknown setup ends the lobby. | Pass the mode as a launch parameter to both players. It works, but two players must agree out of band, and a disagreement shows only as a lockstep error. |
| Versus level | A new level file, `versus.json`, with a symmetric 11 × 9 board, six units per team, mirrored pillars, two hedges that block sight but not movement, and a sight radius set by the level. Its content name is `versus-1`. The outpost level `outpost.json` and `outpost-2` stay unchanged, so every existing save, replay, and pinned trace is unaffected. | Reuse the outpost level. It is not symmetric: the scouts start in a back column and the guards in a front one, and it has an alarm zone. |
| Tactical guards | An opt-in rule of the skirmish simulation, off by default, so every pinned trace is unchanged. When on, guards share a target by focusing the weakest scout in range, wake together within a radius when one is hurt, fall back out of weapon range when one hit from death while an ally still fights, and flank: when three or more guards chase one target, every second guard routes through a waypoint to the side of the line of approach before it closes in. The title menu has a Classic and Tactical choice that applies to a single-player and a co-op match. | Change the default behavior. It would retire every `outpost-2` recording, save, and pinned trace and force the recorded-fight smoke scripts to be rewritten. Patrols and healing are not included, because each needs new unit state and a larger balance pass. |
| Online replays | A new format, the match log, in a new package `engine/matchlog`. It records, for each simulation tick, the payload of each player, with the checkpoint hashes of `Sim.Trace`. Both players record locally and save when the match ends for any reason. A title entry plays the last online match back through the same session, with no network. | Extend replay version 1. Its runs hold only `engine.Input`, a run holds at most 232 bytes, and a command can hold 256 IDs, so a command stream does not fit without breaking the canonical form that the decoder enforces. |
| Spectating and rejoin | Not in this milestone. The match log makes both possible later, because a log can be streamed or replayed from a start state. | Include a spectator slot in the relay. It needs a third lockstep role that never submits and a relay rule for it, and it doubles the test matrix of M16. |
| Music | The mixer gets one music voice, separate from the 16 sound voices. A music track is a mono 48 kHz clip that loops, with a gain that changes smoothly. The outpost's track is rendered at start-up from a score of a few kilobytes, using the synthesizer of the Sound Maker, so the repository stays small. The Sound Maker gains a Music panel that edits a score and exports it. | Stream or decode a compressed file. It needs a decoder, a ring buffer fed off the audio thread, and an underrun policy on desktop, and a second path in the browser. A rendered loop of at most 64 seconds is 6 MiB and fits the current design. |
| Authored vision | Level version 3 adds an optional top-level `sight` object with the sight radius, and zones of kind `sight_block` block line of sight over their rectangle without blocking movement. Version 1 and 2 files decode and encode unchanged, and a file gets version 3 only when it has a sight object. The Level Builder gets a radius field and a Sight block preset. | Keep the radius a code constant and encode blockers as zones. It needs no format change, but the radius would stay in Go and an author could not set it. |
| Font editor | A browser editor for a monospaced font of one cell size, modeled on the Pixel Art Maker. It opens the built-in font or an existing sheet and sidecar, edits glyph cells, and exports the PNG and the JSON that `font.Decode` already reads. `specs/font-v1.md` does not change. | Add proportional advances and kerning. Advances are optional in the format, but an editor for them is a second interface and a second test matrix. A font that needs them can be written by hand. |
| Windows | Support Windows as a desktop target in the driver, `goje build`, and `goje package`, producing `NAME.exe` and a zip. The code is verified by tests on every host and by a build, package, and run check on a `windows-latest` runner in CI. This machine cannot build it, so the first runs are on CI. If the Windows job cannot be made to pass after a bounded effort, the Windows changes are reverted and the milestone records Windows as unsupported with the reason. | Skip Windows, or ship it unverified. A claim of support with no run behind it is worse than none. |
| Android and iOS | The web build becomes an installable, offline-capable web app: a manifest with the maskable icons, a service worker that precaches the bundle, and a registration step in the shell. The docs show how to wrap that app with a tool such as Capacitor or Bubblewrap. Goje does not generate native projects. The service worker and the manifest are verified in Chromium. | Generate Gradle and Xcode projects. They cannot be built here, no device is available, and they commit the project to maintaining two native toolchains. |
| Signing and updates | `goje package` can sign `SHA256SUMS` with an ed25519 key that the developer holds, and writes an `update.json` with the version, the file hashes, and the signature. A new package `update` verifies a manifest and a downloaded file against a public key and a current version, and a game can call it to learn that a newer signed build exists. Everything is tested with a generated key and a local server. Authenticode, Apple notarization, and store signing need accounts and are not done. | Skip signing. Checksums alone prove a download is intact, not that it is from the developer. |
| Order of work | Five independent slices first (music, the font editor, reach, authored vision, and the skirmish rules for versus and tactics), each owned by one agent with its own files. The online outpost changes and the match log follow, because they depend on the first wave. | One slice at a time. It is safer, and much slower, and the first wave shares no files. |

## Proposed design

### Versus and tactics in the skirmish simulation

- A simulation option makes a team human-commanded. `Sim.SetControl(team, human bool)` turns off `decide` for that team's units, and `Apply` accepts commands for them. Without the option, behavior, trace, and snapshot are byte for byte as they are.
- The explored set becomes one set per team. The scouts' set keeps its position in the trace and the snapshot. A second set is folded in only when the guards' vision is on, and the snapshot becomes version 3 only then, so versions 1 and 2 are unchanged.
- `Explored(team, x, y)` and `Visible(team, x, y)` take a team. The old scouts-only forms stay as thin wrappers for the code that has not changed.
- The simulation reports which team has won: a team wins when no unit of the other team is alive. The outpost's own rule, which lives in its session, stays as it is for the single-player and co-op matches.
- Tactics is a flag with four behaviors, focus fire, a shared wake, a fall back, and a flank. The flank waypoint is a per-unit cell that is part of the trace and the snapshot when tactics is on. Each behavior is a pass over the units in ascending ID order so the result is deterministic. Its fields are in the trace and snapshot only when the flag is on.

### Authored vision

- `level.Board` gains `Sight() (radius int, ok bool)` and `SightBlocked(x, y int) bool`. A radius is 1 to `vision.MaxRadius`. Decoding rejects a sight object without a radius, a radius out of range, and a `sight_block` zone with a role other than `trigger`, because a solid zone already blocks movement and an author should say which is meant.
- `VisionConfig` gains an optional blocked mask. The simulation combines it with the wall cells, so a sight-blocking zone and a wall cast the same shadow, and `Cast` stays symmetric because it receives one callback.
- The Level Builder gets a number field and a button. A zone kind is free text, so the editor itself needs little.

### The online outpost, versus, and the match log

- The title menu gains Host co-op, Host versus, and a Guards line. The `Setup` message is `0x53, mode, tactics` and is sent by the host once the guest has joined. It is outside lockstep and is specified with the outpost.
- Ownership is a function of the mode: by ID parity in co-op, and by team in versus. `checkOwnership` and the selection filters use it. The HUD, fog, sounds, and end screen take the local team.
- The match log's header names the game, the content name, the mode, the tactics flag, and the players. Its body holds each tick's payloads, run-length coded for the empty ticks, and then the checkpoint hashes. Limits: 2 MiB encoded, 1,048,576 ticks, 1,024 bytes per payload. The decoder is a trust boundary like the others and is fuzzed.
- Watching a log builds the same session with a source of payloads in place of the network. The camera is free, **Tab** switches which team's fog is shown, and a checkpoint that does not match ends the watch with a divergence report.

### Music

- `audio.Clip` gains a music limit: mono 48 kHz 16-bit PCM of at most 6 MiB. Sound clips keep their 3-second limit. `audio.Score` is a small JSON document of a tempo, a loop length in steps, and up to four channels of notes, each with a waveform, a pitch, a length, and a level. `audio.RenderScore` turns it into a clip with the Sound Maker's oscillators.
- The request queue carries a kind: sound, play music, stop music, set music gain. Playing music replaces the current track, with a fade in and a fade out measured in milliseconds.
- On desktop the mixer has one more voice that loops and ramps its gain per block. In the browser the voice is a looping `AudioBufferSourceNode` with a gain node whose value is ramped. Parity between the two is checked in the same way as the M15 mixer.
- The outpost plays its track in the title and the match, ducks it under the pause menu, and stops it when a match ends. The Sound entry in the menus sets the volume of music and sounds together.

### The font editor

- A package `internal/fonteditor` holds the document: a cell size, a first rune, a count, a baseline, and one bitmap per glyph. It opens a sheet and a sidecar through `font.Decode`, starts from the built-in font through a new exporter of its sheet, edits pixels with undo, and exports the PNG and the JSON. The editor limits are the decoder's, 256 KiB and 512 pixels per axis, so an export cannot fail `fontcheck`.
- `cmd/fonteditor`, `web/fonteditor`, and `scripts/font-editor-server.mjs` follow the other editors. A smoke edits a glyph, exports, validates with `fontcheck`, and renders the exported font in a scratch game through `font.Decode`.

### Reach

- The desktop driver's build tags and `platform.Run` accept Windows. `goje build -target desktop` on Windows produces `NAME.exe` linked as a GUI program, and `goje package` writes a zip with the same reproducible rules as the other archives. The host-equality check in `resolve` stays, so cross compiling is still refused.
- The shell page registers `sw.js`, which precaches the files of the bundle under a name that includes the build's content hash and removes older caches. The manifest gets `scope`, `start_url`, and a maskable icon.
- `goje package -sign KEYFILE` signs `SHA256SUMS` and writes `SHA256SUMS.sig` and `update.json`. `goje keygen` makes a key pair. The package `update` has `Verify(manifest, publicKey)`, `Newer(manifest, current)`, and a download that checks each hash while it reads and refuses more than a stated size.

## Implementation and acceptance plan

| Slice | Exit check |
| --- | --- |
| 1. Skirmish rules | Per-team explored sets, human-controlled teams, the winner, and tactics have tests for each rule and seeded histories that hold the invariants. Every pinned trace of M13 to M16 is unchanged with the new options off, and the snapshot of version 1 and 2 is byte for byte as before. Tactics has its own pinned trace, the same natively and under WebAssembly. |
| 2. Authored vision | `level` decodes and encodes version 3 and rejects each bad sight object. Fuzz tests find no panic, and versions 1 and 2 round trip byte for byte. The Level Builder edits the radius and blockers and a smoke authors the versus level from its bare terrain, and the exported file equals the shipped one. |
| 3. Music | Mixer tests cover a loop, wrap, fade, stop, and replacement without allocation. `make smoke-mixer` compares the Go music voice with the Web Audio graph. The score renderer is deterministic and the Sound Maker smoke edits, exports, and plays a score. The queue and the hosts handle the new request kinds. |
| 4. Font editor | The document tests cover opening, editing, undo, and every limit. The smoke edits a glyph in Chromium, and the exported pair passes `fontcheck` and renders the same pixels through `font.Decode`. |
| 5. Reach | Windows: tests of the build and package logic on every host, and a `windows-latest` job that builds, packages, and runs the result for its `--help` or a headless check. Web app: a Chromium smoke installs the service worker, goes offline, and reloads the game from the cache. Signing: tests with a generated key for a good signature, a changed file, a changed manifest, a wrong key, an old version, and an oversized download. |
| 6. Online outpost | Headless tests play versus through the in-memory network: the two teams see only their own fog, each commands only its own units, a forged command for the other team's unit ends the match, and the match ends with a winner. The title menu, the setup message, the Classic and Tactical choice, music, and the end screens work. The pinned single-player and co-op traces are unchanged. |
| 7. Match log | Encode and decode round trips, every rejection, a fuzz test, and a log of a played match that replays to the same checkpoint hashes. A match that ends by a desync, by a leave, and by a win each saves a log. |
| 8. Cross-play and release | `make smoke-online` extends to a versus match between a native host and a Chromium guest, and to watching its log in each. The documents, the specifications, and the security notes are updated, and measurements of the tactics pass, the music voice, and the match log are recorded. |

## Out of scope

Spectators and rejoin. Rollback, adaptive delay, and prediction. More than two players in the outpost. A versus matchmaking service, accounts, and ranking. Compressed or streamed music, a tracker with samples, and sound effects authored in the Score panel. Proportional fonts and kerning in the editor. Patrols, healing, and formations for the guards. Generated Android and iOS projects, Authenticode, Apple notarization, store packages, and a hosted update channel. Cross compiling to Windows from another system.

## Contracts this proposal changes

- [`specs/skirmish-m13.md`](../specs/skirmish-m13.md): human-controlled teams, per-team explored sets, the winner, tactics, and snapshot version 3.
- [`specs/level-v2.md`](../specs/level-v2.md) and a new `specs/level-v3.md`: the sight object and the `sight_block` zone.
- [`specs/vision-m15.md`](../specs/vision-m15.md): the blocked mask and per-team fields.
- [`specs/audio-mixer-m15.md`](../specs/audio-mixer-m15.md): the music voice, the request kinds, and the music clip limit. A new `specs/music-m17.md` describes the score.
- [`specs/outpost-m14.md`](../specs/outpost-m14.md): versus, the setup message, tactics, and watching a log.
- New specifications: the match log, the font editor, the update manifest and signing.
- [`specs/build-m15.md`](../specs/build-m15.md) and [`docs/building.md`](../docs/building.md): Windows, the web app, signing, and updates.
- [`SECURITY.md`](../SECURITY.md): the match log decoder, the score decoder, and the update verifier are new trust boundaries.
- A new module dependency is not expected. `crypto/ed25519` is in the standard library.

Each change is stated in the document it affects, not applied silently.

## Implementation results

- Native and WebAssembly tests cover the Classic and Tactical traces, old snapshots, versus ownership and per-team fog, final-tick confirmation, and saving and watching a match after a win, leave, or desync. The standalone viewer reports a changed checkpoint by tick.
- The native/Chromium relay smoke matched 15 co-op hashes. An injected hash mismatch stopped both peers. Versus matched 12 checkpoints, and both replays verified all 723 recorded ticks; Tab switched the spectator fog in Chromium. A separate versus test ends in a win and checks opposite results.
- Chromium authored the shipped versus level byte for byte, exported a font that passed `fontcheck` and rendered through `font.Decode`, and played and exported a score. Score, level, match log, and manifest decoders were fuzzed.
- Signed Beacon packages reproduced on macOS. Chromium reloaded the packaged game after the server stopped and the browser went offline. The mobile deliverable is this installable web app; it was not tested on a phone. Windows remains provisional until the CI runner builds, packages, and runs Beacon.

Measurements on Apple M4 Pro and Go 1.27.4, medians of three runs with `-benchmem`. The WebAssembly measurements use Node, not browser timing. All four benchmarks reported zero allocations per operation.

| Operation | Native | WebAssembly |
| --- | ---: | ---: |
| Classic simulation tick, 64 versus 64 | 53.4 µs | 179 µs |
| Tactical simulation tick, 64 versus 64 | 53.1 µs | 182 µs |
| Music voice, 512-frame stereo block | 862 ns | 13.5 µs |
| Match log, append one empty tick | 7.85 ns | 30.3 ns |

## Next milestone

M18 takes what remains: spectators and rejoin, generated native wrappers, store signing and a hosted update channel, compressed music, and the games that these features make possible. It gets its own RFC.
