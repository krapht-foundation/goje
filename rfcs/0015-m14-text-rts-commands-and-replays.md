# RFC 0015: M14 - Text and HUD, RTS commands, and replays

**Status:** Implemented in milestone 14. The delivered milestone differs from this proposal in the ways listed under [Implementation notes and results](#implementation-notes-and-results); where they differ, the specifications are the contract.

## Problem and goal

[M13](0014-m13-crowds-authored-collision-and-combat.md) put several units on one board that block each other, fight, and wake on a trigger. Three gaps now stand between that and a game a player can sit down with.

1. Nothing can show text. `engine.Canvas` draws rectangles, lines, and regions of one image, so the skirmish shows health as bars and has no menu, no counts, and no messages. A game cannot tell the player what a mode is or who won.
2. The commands are those of a single-unit prototype. A player selects one scout at a time and orders it to one cell. An RTS-style game needs box and group selection, orders for a whole group, attack-move, focus fire, and holding fire.
3. A run cannot be saved, replayed, or compared. [M12](0013-m12-movement-and-collision.md) and M13 pin position and state hashes across targets in tests, but a player's session is not recorded, a game cannot resume, and a bug report cannot carry the input that caused it.

A developer should be able to open a new `outpost` fixture, see a menu and a readable HUD, drag a box around several scouts, give them an attack-move order that they carry out as a loose formation, hold fire, focus a guard, save the game, load it, and watch the same session again as a replay that ends in the same state. Desktop and web must agree exactly.

This milestone combines three ideas that were separate options: text and a HUD, an RTS command layer, and replays with saves. They share one scenario and one new input surface, so they are delivered as ordered slices and committed once, as M13 was. A following milestone, [M15](#next-milestone), takes audio, vision, and platform reach.

## Decisions for review

| Decision | Recommended | Alternative and tradeoff |
| --- | --- | --- |
| Delivery | Input and canvas primitives, then text and UI, then replays and saves, then RTS orders in the simulation, then Level Builder placements, then the playable outpost. Each slice has its own headless tests and exit check, and the milestone is committed and pushed once, at the end. | Three milestones. Each is smaller to review, but the outpost needs all three, and the command layer needs both text and the new pointer input. |
| Text | A bitmap font: a PNG sheet of equal cells and a small JSON description, drawn as one tinted image region per glyph. Goje ships a built-in 5 × 7 ASCII font drawn by its own table, so every game has text without an asset. | Vector fonts. They scale smoothly, but they need rasterization, shaping, and licensing decisions, and pixel-exact output that matches on every target. Bitmap text fits the pixel-art pipeline and replays exactly. |
| Tinted images | `Canvas` gains one method that draws an image region multiplied by a color. The shaders already multiply a vertex color by the texture, so both hosts need only to pass the tint through. | Ship one font sheet per color. That needs no renderer change, but a HUD with five colors would need five atlas copies. |
| UI | A small set of plain widgets over `Canvas`, `Input`, and the font: panel, label, button, vertical menu, and meter. Scenes lay them out by hand with rectangles. | A layout engine or an immediate-mode framework. Both are more general, but the milestone needs a menu and a HUD, and a layout contract fixed now would be guessed. |
| Input | `engine.Input` gains the pointer position, the held state of both buttons, release edges, and more keys, including Shift, Control, Escape, Enter, and the digits 1 to 9. `engine.Keys` widens to 32 bits. | A separate pointer event queue. That is more flexible, but it breaks the one-snapshot-per-tick rule that makes recording and replay simple. |
| Replays | A replay is a compact binary list of the per-tick `Input` values with a state hash every 60 ticks, an optional start state, and a check value for the game content. Recording and playing back are scene wrappers in a new `engine/replay` package, so neither host changes. | Record in the hosts. That captures even scenes that forget to opt in, but it ties the format to each host and is harder to test headlessly. |
| Saves | A scene may implement `engine.Snapshotter`, which writes and reads its whole state as bytes. A new `save` envelope adds a version, the game, a content check, and a checksum. Hosts provide a small key-value store, a file directory on desktop and local storage in the browser. | Save a replay prefix and replay it to load. It needs no per-game code, but loading takes as long as the game has run: ten minutes of a busy battle would take tens of seconds. |
| Commands | The simulation gains orders: move, attack a unit, attack-move, and a hold-fire stance. A group order assigns each selected unit a distinct goal cell near the clicked cell, so the group arrives spread out. | Persistent formations that keep shape and match speed. They are more faithful to RTS games, but they need a movement model that M13 deliberately does not have. |
| Where policy lives | Selection, orders, targeting, and group assignment remain reference-game code in `examples/skirmish`, shared with the existing fixture and the benchmark. The engine gains input, text, UI, replay, and save plumbing. | Move selection and commands into the engine now. Goje would fix rules before a second game shows which are general. |
| Level Builder placements | The editor adds, deletes, and edits the kind of placements, in addition to moving them, so an outpost with a dozen units can be authored without editing JSON. | Defer, as M13 did. The editor stays smaller, but the milestone's scenario is then written by hand again. |
| Scenario | A new `outpost` fixture, a larger authored level with six scouts, six guards, a deck, and an alarm, plus a menu, HUD, pause, and end states. The skirmish and its pinned traces stay unchanged. | Extend the skirmish. That avoids a second level, but it would change the recorded M13 history that other tests and documents depend on. |

## Proposed design

### Input and canvas

- **Pointer.** `Input` gains `Pointer`, the latest logical position, with a flag that is false when the pointer is outside the viewport or the window is not focused. It is sampled once per tick, like keys, so one tick has one value.
- **Buttons.** `PrimaryHeld` and `SecondaryHeld` report the buttons down at the start of the tick. `PrimaryRelease` and `SecondaryRelease` are `MousePress` edges with the position of the release. Every press that the host reports is followed by exactly one release edge unless focus is lost. Losing focus clears held state and emits no release, so a scene must treat a drag that ends without a release as cancelled. The existing press edges and their buffering rules do not change.
- **Keys.** `Keys` is 32 bits. It gains `KeyShift`, `KeyControl`, `KeyEscape`, `KeyEnter`, `KeyH`, `KeyF`, and `Key1` through `Key9`. The desktop and browser key tables map them, and repeat events are still ignored.
- **Tinted images.** `Canvas.ImageTinted(source, x, y, width, height, tint)` behaves like `Image` and multiplies each texel by the tint. `Image` is unchanged and uses a white tint.
- **Documented contract.** [`specs/runtime-m1.md`](../specs/runtime-m1.md) records the new fields, their buffering, and the focus behavior. The Beacon and other existing games compile and behave unchanged, because every new field is zero in an old input.

### Text

- **Font sheet.** A font is a PNG and a JSON document, in the style of the sprite sheet. The PNG is at most 256 KiB and 512 pixels on each side. The JSON gives the cell width and height, the baseline, the first code point, the number of glyphs, and an optional list of per-glyph advances for proportional text. Glyph `n` is in cell `n`, left to right and then top to bottom. Missing glyphs draw the glyph for `?` if there is one and are skipped otherwise. Names, unknown fields, duplicate fields, and bounds are validated as for the other formats, and `fontcheck` runs the same decoder from the command line. `specs/font-v1.md` will record the format.
- **Built-in font.** `font.Builtin()` returns a 5 × 7 pixel font for the ASCII range 32 to 126 in 6 × 8 cells, produced from a table in Go source. It is authored for this project and carries no outside license.
- **Drawing.** `Face.InAtlas(offset)` places a font in the game's launch atlas, as sprite clips do. `Face.Draw(canvas, text, x, y, scale, tint)` draws one glyph per `ImageTinted` call at an integer scale of at least 1, and `Face.Measure(text, scale)` returns the size. `\n` starts a new line. Text is UTF-8, each rune is one glyph, and invalid bytes draw the missing glyph. There is no wrapping, shaping, bidirectional text, or word processing. Drawing allocates nothing.
- **Authoring.** A font sheet is an ordinary PNG, so it can be painted in the existing Pixel Art Maker and described by hand. A dedicated font editor is not part of this milestone.

### UI

- **Widgets.** A package `ui` holds five plain structs, each with a rectangle, and `Update` and `Draw` methods. `Panel` is a filled, bordered rectangle. `Label` is text. `Button` has a label and reports a click when a primary press lands inside it, and `Hot` and `Down` for drawing from the pointer and held state. `Menu` is a vertical list of buttons that also responds to the arrow keys and Enter. `Meter` is a bar with a fraction, such as a health bar.
- **Input ownership.** A scene asks `Contains(x, y)` before treating a press as a world click, so a press on the HUD does not also order a unit.
- **Style.** Colors and borders are fields the scene sets. There is no theme, layout engine, scrolling, text entry, or focus system beyond the menu.

### Replays

- **Recording.** `replay.Record(factory, recorder)` returns a scene factory whose scenes record `tick.Input` before each update and, every 60 ticks and after the last, the state hash of the inner scene. A scene that implements `engine.Hasher` supplies the hash. A restart or a replacement creates a new inner scene and the recording continues, because the recorder is shared.
- **Playback.** `replay.Play(factory, recording)` returns a factory whose scenes ignore live input, feed the recorded input to the inner scene, and compare each checkpoint. The first mismatch ends playback with an error that names the tick. A finished playback leaves the scene on its last state. Pause, seeking, and speed changes are not part of this milestone.
- **Verification.** `replay.Verify(factory, recording)` runs a recording headlessly through the runtime and returns the first diverging tick. Games use it in tests, and the playground checks a recorded session on every target.
- **Format.** `specs/replay-v1.md` will define a compact little-endian binary format with a magic number and version, a game name, a content check value, the checkpoint interval, an optional start state of at most 1 MiB, and the inputs stored as runs of identical ticks. A replay is limited to 2 MiB and 1,048,576 ticks, about 4.8 hours. The decoder rejects bad magic, unknown versions, truncated or trailing data, and any count beyond the limits before allocating from it. Pointer positions are stored as exact IEEE 754 bits so that playback reproduces the same input on every target.
- **Content check.** The game supplies a 64-bit check value over the bytes that define its content, such as the level and art. A replay or save made with different content is refused with a clear error instead of diverging.
- **Determinism rule.** A recordable scene must derive all state from `Input` and its own history, and must not read `Tick.Number`, a clock, or randomness. The pointer-to-cell conversion uses floating-point arithmetic at the input boundary, as the M3 scenes do. The recorded cell for a click is therefore the same wherever the same Go compiler rules apply, and the cross-target replay check in CI is the test that this holds.

### Saves

- **Snapshotter.** `engine.Snapshotter` is an optional interface a scene may implement: `Snapshot() ([]byte, error)` and `Restore([]byte) error`. The scene owns the layout of its bytes. The skirmish simulation gains `Snapshot` and `Restore` that capture every unit, route, bolt, the alarm, counters, and the trace.
- **Trigger state.** `collide.Tracker` gains exact export and import of which triggers each body currently overlaps, so a restored game does not report entry events again for bodies that were already inside.
- **Envelope.** `save.Encode` and `save.Decode` wrap a snapshot with a magic number, a version, the game name, the content check, and a checksum, limited to 1 MiB of payload. Corrupt, truncated, oversized, or mismatched files are refused. `specs/save-v1.md` will define it.
- **Store.** `platform.NewStore(game)` returns a store with `Put`, `Get`, and `Delete` on short validated keys and values of at most 4 MiB. On desktop it keeps files in a per-game directory under the user's configuration directory, written by replacing a temporary file. In the browser it uses local storage with base64 values, and a failed write returns an error. A game that gets no store, for example because local storage is blocked, plays normally and offers no saving.
- **Restore equals continue.** Restoring a snapshot and running further must reach the same hashes as the uninterrupted run. This is a headless test on every target.
- **Replay from a save.** A recording started after a load stores that snapshot as its start state, so it can be played back.

### RTS commands

All of these live in `examples/skirmish` and the scene, and they change no existing behavior: the M13 skirmish and its pinned traces must be reproduced exactly.

- **Selection.** Click selects one unit, and Shift adds or removes it. Dragging with the primary button selects every living scout whose footprint center is inside the box on the ground plane, replacing the selection unless Shift is held. The box is drawn as a rectangle outline. A drag under 4 logical pixels counts as a click. **Control + 1** to **9** stores the selection in a control group and **1** to **9** recalls it. Dead units leave every selection and group.
- **Orders.** `Sim.Order(id, Order)` is the new entry point. `Move` is the existing `Command`. `Attack` names a target unit: the unit walks toward the target's current cell, asking again every 15 ticks, until it is in weapon range, and fires at that target instead of the nearest enemy while it lives. `AttackMove` walks to a goal and stops to fight whenever an enemy is in weapon range, resuming when none is. A hold stance stops a unit from firing at all until the stance is cleared.
- **Controls.** A right click on the ground orders a move and on an enemy unit orders an attack. **A** then a left click on the ground orders an attack-move and on a unit orders an attack. **H** toggles hold fire for the selection, **F** orders the selection to attack the enemy under the pointer or, with none, the nearest enemy, and **Escape** cancels a pending mode. The HUD shows the pending mode.
- **Group assignment.** A group order is turned into one goal per unit. Starting from the clicked cell, a breadth-first search over walkable cells that no idle unit holds yields cells in a fixed order, nearer cells first and row-major among equals. The selected units, sorted by distance to the clicked cell and then by ID, take those cells in that order. Each unit then receives an ordinary order to its goal. A unit with no reachable goal keeps its current order. The result is deterministic and spreads the group; it is not a minimum-cost assignment and it does not keep a shape while moving.
- **Existing rules apply.** Blocking, repathing, giving up, friendly fire, guards, and the alarm work as in M13. A unit with an order that ends, gives up, or loses its target stands idle.
- **Specification.** [`specs/skirmish-m13.md`](../specs/skirmish-m13.md) gains the order, stance, and assignment rules and the tick position of order handling, and a new `specs/outpost-m14.md` records the scene's controls, HUD, and states.

### Level Builder placements

- The editor adds a placement by choosing a kind, with a text field as for zones, and clicking a cell. It assigns the smallest unused ID above the largest in the level, or one the author types, within 1 to 4,294,967,295 as the format requires. It also deletes a placement and changes its kind. Each edit is one undoable transaction under the existing history limits, and invalid edits are rejected with visible text. Placements keep the one-per-cell and 1,024-placement rules of [`specs/level-v1.md`](../specs/level-v1.md), whose format does not change.
- The courtyard's `scout` and `lookout` pair and the existing exports keep working. `levelcheck` stays specific to the courtyard.

### The outpost scenario

- `GOJE_PLAYGROUND=outpost` or `?fixture=outpost` runs a 16 × 12 authored level with six scouts, six guards, walls, a raised deck, and an alarm trigger. Its units, walls, deck, and alarm are authored with the Level Builder in a Chromium round trip, as the yard's zones were.
- A title menu offers Start, Watch last replay, and Load. During play, **Escape** opens a pause menu with Resume, Save, Load, Watch replay, and Restart. A HUD shows the live count of scouts and guards, the selection size and health, control group badges, the pending mode and hold state, the alarm, a recording indicator, and a victory or defeat message when a side has no units left.
- The scene records every run, keeps the last finished one in the store, and plays it back through `replay.Play`. Playback shows the same fight and ends on the recorded hash.
- It reuses the courtyard's art and camera. It uses the built-in font and canvas rectangles for everything else.

### Allocation and budget

- Text drawing, widget updates, selection, group assignment, and recording allocate nothing in steady state after their buffers are sized at scene entry. A benchmark draws 1,000 glyphs and a full HUD per frame and reports the draw time on native and in Chromium. The skirmish benchmark gains a mode with six squads issuing attack-moves and group orders, and the worst tick must still stay under half of a 60 Hz frame on the development machine. A replay of 36,000 ticks must verify headlessly in under one second per 600 ticks of a busy battle on the development machine, or the cost is reported and a faster check is chosen.

## Implementation and acceptance plan

| Slice | Exit check |
| --- | --- |
| 1. Input and canvas | The loop tests cover pointer sampling, held state, release edges with and without a matching press, focus loss, and key repeat for the new fields. Both hosts deliver them. Chromium and native smokes with real input show a pointer-following marker, a drag box, and a tinted image whose color matches the tint. Existing smokes and tests pass unchanged. |
| 2. Text and UI | Font decoding has tests for every rejection above and a short fuzz run. The built-in font and a custom sheet render in a native and Chromium smoke that checks glyph pixels, scale, tint, and newlines. Widget tests cover hit testing, hover, click, held state, menu keys, and the Contains rule. Drawing is benchmarked and allocates nothing. |
| 3. Replays and saves | Format tests cover round trips, every rejection and limit, and a fuzz run for the replay and save decoders. A scene recorded from a scripted history replays with matching hashes natively and under WebAssembly, and a deliberately changed input is detected at the right tick. Restore equals continue holds for the skirmish simulation on both targets. The desktop and browser stores pass a round trip, refuse oversized or malformed keys, and survive a rejected write. |
| 4. RTS orders | Headless tests cover box selection, Shift, control groups, every order and stance, the attack interval, attack-move stopping and resuming, group assignment ties and unreachable goals, and orders that always end. Randomized histories from fixed seeds check that no footprints overlap and that every order ends. The M13 crowd, squad, fight, benchmark, and yard traces are unchanged. A new recorded history of group orders has a pinned trace that matches natively, under WebAssembly, and in Chromium. |
| 5. Level Builder placements | The editor core and browser tool add, delete, and re-kind placements with undo and redo and reject invalid edits. A Chromium round trip authors the outpost, exports it, reopens it, and plays it on desktop and web. |
| 6. Playable outpost | Native and Chromium smokes use real input to open the menu, start, drag-select a group, issue an attack-move, hold fire, focus a guard, save, load, finish the fight, and watch the replay end on the recorded hash, with the text and HUD checked in pixels. The documentation describes the scenario, controls, formats, and measurements and states the limits. The courtyard, yard, skirmish, and Beacon smokes still pass. |

## Out of scope

Persistent formations, shared formation speed, waypoints, queued orders, patrol, rally points, and automatic group assignment that minimizes travel. Production, resources, buildings, and technology. A minimap, edge scrolling, and camera bookmarks. Fog of war, line of sight, and AI that plans tactics. Vector or outline fonts, text wrapping, shaping, right-to-left and combining text, input methods, text entry, localization, and a font editor. Layout engines, scrolling lists, themes, and focus rings. Seeking, pausing, and changing the speed of a replay. Replays across different game content, networking, and cloud or shared saves. Several save slots, save thumbnails, and compression. Touch, gamepad, and other input devices. Audio. Any change to the grid router, the level formats, the M3 courtyard contract, or the yard's controls.

## Contracts this proposal changes

- [`specs/runtime-m1.md`](../specs/runtime-m1.md): the input snapshot, the key set, and the new canvas method.
- [`specs/collision-m12.md`](../specs/collision-m12.md): the trigger tracker's exported state.
- [`specs/skirmish-m13.md`](../specs/skirmish-m13.md): orders, stances, group assignment, and the snapshot. Existing behavior is not redefined.
- [`specs/playground.md`](../specs/playground.md): the new fixture and its smokes.
- New specifications: font version 1, replay version 1, save version 1, and the outpost.
- The Level Builder's placement behavior in [RFC 0005](0005-m4-level-builder.md) and its documentation changes from move-only. The level format does not change.

Each change is stated in the document it affects, not applied silently.

## Next milestone

M15 takes the remaining options from the M14 proposal. The contents are a plan, not a commitment, and M15 gets its own RFC.

- **Audio engine.** Overlapping cues, a mixer, positional audio, and combat sounds in the outpost. Replays record input, not sound, so playback sounds the same as play.
- **Vision.** Line of sight, fog of war, and guards and scouts that use them, using the state hash and replays from M14 as the regression test.
- **Platform reach.** Touch and gamepad input, added as new `Input` fields beside the pointer fields from this milestone, a `goje build` export for desktop and web, and release packaging.

## Implementation notes and results

The accepted behavior is recorded in the [runtime](../specs/runtime-m1.md), [font](../specs/font-v1.md), [replay](../specs/replay-v1.md), [save](../specs/save-v1.md), [collision](../specs/collision-m12.md), [skirmish](../specs/skirmish-m13.md), [level version 2](../specs/level-v2.md), [playground](../specs/playground.md), and [outpost](../specs/outpost-m14.md) specifications. Where this proposal and those documents differ, the specifications are the contract.

### Differences from the proposal

- **Outpost size.** The outpost level is 11 × 9 cells, not 16 × 12, so that the whole board fits the 320 × 200 logical viewport at the default zoom with room for the HUD bars. It has six scouts, six guards, four wall cells, a deck, and an alarm trigger as proposed.
- **Box selection.** The box is a rectangle in logical screen pixels, and a scout is selected when its center is inside it as drawn, not when its footprint center is inside a rectangle on the ground plane. A ground-plane box would not match what the player sees under a 2.5D projection. The proposed `Sim.AppendScoutsInRect` was written, found to have no user, and removed.
- **Widgets.** `ui.Panel`, `ui.Label`, and `ui.Meter` have no `Update` method, because they take no input. `Panel.Draw` and `Meter.Draw` take only the canvas, and `Label.Draw` takes the canvas and a face. `Menu.Update` returns the activated item and whether the player canceled, and `Button.Update` returns whether it was activated.
- **Replay API.** `replay.Play` returns a `*Player` with `Factory`, `Done`, `Played`, and `Ticks`, because a playing scene must tell the host when it has reached the end of the recording. `Play` and `Verify` take the expected `Meta`, and `Meta`, `ContentHash`, and `ErrContentMismatch` live in `replay`, which the save envelope reuses. `NewRecorder` takes the number of ticks to reserve room for, so recording never allocates, and a recording stops at 1,048,576 ticks or 2 MiB, whichever comes first. With a moving pointer the size limit comes first, at about 26 minutes. A playback scene cannot leave on its own, so a game that wants to return to a menu runs the playback in a nested `engine.Runtime` and steps it from a parent scene; the outpost does this.
- **Transition replacement.** `engine.Transition` gained `Replacement`, so the recording and playback wrappers can wrap the scene that a `Replace` transition creates, as they wrap the first scene.
- **Group assignment.** `AssignGoals` checks reachability only from the clicked cell. A unit that cannot reach the cell it was given keeps its previous order, and the cell is not offered to another unit. Range ignores walls, so a unit given an attack-move can wait indefinitely beside an enemy it cannot reach through a wall; the tests show this.
- **Level Builder.** The Zones and Placements panels moved into a left column, which changes the Tab order. The placement list is in row-major cell order, not insertion order. `level.Placement.Check` and `level.MaxPlacements` were added so that the editor and the decoder apply the same rule.
- **Smokes.** The smokes for the new input fields and for glyph pixels are part of the outpost smokes instead of separate ones: the native smoke checks the drag box, tinted glyphs, markers, and the menu in the back buffer, and the Chromium smoke does the same with real mouse and keyboard input. `make smoke-placement-editor` and `make smoke-outpost-roundtrip` are new, and the outpost round trip strips the shipped level to its terrain, rebuilds the placements and zones through the editor's forms, and plays the export on both targets.
- **Benchmarks.** The skirmish benchmark program now prints two reports, the battle and the same battle with six squads receiving group orders, and `scripts/benchmark-collide.mjs` reads both. The text and replay costs are Go benchmarks run natively and as WebAssembly under Node, not in Chromium.
- **Held modifiers.** `Input.Held` is sampled once per tick. A host that delivers a key down and up for Shift or Control between two ticks shows the scene neither, so a Shift-click or a Control+digit that is faster than a tick is read without its modifier. Real keyboards hold a modifier for far longer than a 16 millisecond tick; the Chromium smoke steps a frame while it holds the modifier for the same reason.

### Results

| Check | Result |
| --- | --- |
| Text, widgets, and font | Decoder rejection tests, a fuzz run, widget tests, and the glyph and tint pixel checks in the outpost smokes pass. Drawing 1,000 glyphs takes about 16 microseconds natively and 95 microseconds as WebAssembly under Node on the development machine, and drawing the outpost's whole HUD takes about 2 and 16 microseconds. Neither allocates. |
| Replays and saves | Round trip, rejection, and limit tests and fuzz runs pass for both formats. The outpost's recorded fight replays to the same state hashes natively and as WebAssembly, and a changed input is reported at the first checkpoint after the change. A snapshot restored at several ticks continues identically to the uninterrupted match. Verifying a 603-tick busy battle takes about 1.2 to 1.7 milliseconds natively. |
| Orders | The M13 crowd, squad, fight, benchmark, yard, and crossing traces are unchanged. The orders history has the pinned trace `16928502377189695761`, and the outpost's recorded fight has `2740933099236710724`, each the same natively and under WebAssembly. |
| Benchmark | With six squads re-ordered every 120 ticks, the 256-unit battle ended with the same trace on native, Node WebAssembly, and Chromium. The worst tick was 3.0 ms natively, about 18% of a 60 Hz frame, and 8.1 to 8.6 ms in Chromium, which touches the half-frame line in this stress case. See [Group orders](../docs/collision-performance.md#group-orders). |
| Smokes | `make smoke-desktop` with the new `--outpost` scenario, `make smoke-web`, `make smoke-placement-editor`, and `make smoke-outpost-roundtrip` pass on macOS. The Chromium smoke is the first run of the browser store in a real `localStorage`, of pointer capture during a drag, and of Control and Shift in real key events. |

### Not checked

Linux native smokes beyond what CI runs, the Windows configuration directory used by the desktop store, Firefox and Safari for the browser store and the pointer events, touch input, and a game with more than one nested runtime. The text and HUD drawing time in Chromium was not measured. A guard that cannot be reached through a wall, and two units that meet head-on in a one-tile corridor, still give up or wait as the M13 specification says.

