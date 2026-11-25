# Outpost: the M14 reference game

The outpost is the playground fixture that exercises the milestone 14 features together: [text and widgets](font-v1.md), pointer input and group selection, the [RTS orders](skirmish-m13.md#orders-and-stances-m14) of the skirmish simulation, [recordings](replay-v1.md), and [saved games](save-v1.md). It is selected with `GOJE_PLAYGROUND=outpost` on desktop and `?fixture=outpost` in the browser. The skirmish fixture is unchanged and keeps its own pinned traces.

## The level

The level is `cmd/playground/levels/outpost.json`, a version 2 level of 11 × 9 cells. Six scouts with IDs 1 to 6 stand in the two left columns, and six guards with IDs 11 to 16 stand in the two right columns. Four wall cells make two short pillars in column 3. An alarm trigger zone fills column 5, and a solid deck zone covers two cells in the middle. The skirmish simulation reads these as it does for the skirmish: crossing the alarm wakes the guards, and bolts pass under the deck. The level is authored in the Level Builder; `make smoke-outpost-roundtrip` rebuilds its placements and zones from the bare terrain and plays the exported file.

The whole board fits the 320 × 200 logical viewport at the default zoom. The camera pans with the arrow keys and zooms with **W** and **S** as in the courtyard, and these are part of a match's state.

## States

The game is one scene, `outpostApp`, with four states.

| State | What it shows | Keys and pointer |
| --- | --- | --- |
| Title | The name and a menu: Start, Load, Watch last replay. | Arrow keys and **Enter**, or the pointer. |
| Playing | The match, a top bar with the unit counts and the alarm, a bottom bar with the selection and a hint, and a red recording clock. | The match controls below. **Escape** opens the pause menu unless the match has something to cancel. |
| Paused | The match, dimmed, with a menu: Resume, Save, Load, Watch replay, Restart, Quit to title. | Arrow keys and **Enter**, or the pointer. **Escape** resumes. |
| Watching | The recording playing back, with a red label that reads REPLAY and then REPLAY ENDED. | **Escape** returns to the title. Live input is ignored. |

A match runs in its own `engine.Runtime`, which the app steps only while the state is Playing. A pause therefore is not a tick of the match, and a recording never contains one. The Restart entry steps the match once with **R** pressed, so the restart is an input that the recording holds.

## Match controls

- Dragging the primary button draws a box in logical screen pixels and selects the scouts whose centers are inside it when the button is released. A drag shorter than four logical pixels is a click: it selects the scout under the pointer, and a click on nothing or on a guard clears the selection. Holding **Shift** adds the scouts to the selection, and a Shift-click on a selected scout removes it. The box is in screen space, not on the ground plane, so it selects what the player sees inside it. Only scouts can be selected.
- **Ctrl+1** to **Ctrl+9** store the selection in a control group, and **1** to **9** recall it. An empty group changes nothing. Units that fall leave the selection and the groups.
- The secondary button orders the selection. On the ground it sends the group to the cell, spread over nearby cells by `Sim.OrderGroup`, and on a living guard it orders an attack on that guard.
- **A** arms an attack-move, and the next primary click on the ground gives the selection that order, or an attack when it lands on a guard. **Escape** cancels the armed order, as does any secondary click.
- **H** holds fire for the selection, or releases it when every selected unit is already holding.
- **F** orders the selection to attack the guard under the pointer, or the guard nearest to the pointer's ground position, or the guard nearest to the first selected scout when the pointer is outside the viewport.
- **R** restarts the match.
- A match ends in victory when no guard is alive and in defeat when no scout is alive. A panel then offers **R**.

The HUD text is rebuilt only when a number it shows changes, so drawing a frame allocates nothing.

## What a recording and a save hold

A match is the `outpostSession` scene. It reads only the input it is given and its own state, so it is replayable under the rules in [Make a scene replayable](../docs/replays.md#make-a-scene-replayable). `StateHash` digests the simulation's trace, the selection and groups, the pending mode, a drag in progress, the zoom, and the camera center. The recorder samples it at its checkpoint interval, and playback fails with a divergence error at the first checkpoint that differs.

The content check of both formats is `replay.ContentHash` of the level file's bytes and the string `outpost-1`. A level edited in the Level Builder, or a change to the rules that the level cannot express, retires old recordings and saves. Changing the string `outpost-1` retires them deliberately.

A snapshot is one tag byte with the value 1, then the following in little-endian order:

1. A length of four bytes and the simulation snapshot of `Sim.Snapshot`.
2. The zoom level and the pending mode, one byte each.
3. The camera center, two IEEE 754 doubles.
4. The drag in progress: an active byte, an additive byte, and four doubles for its start and current points.
5. The selection and then the nine groups, each a two-byte count followed by that many eight-byte IDs.

`Restore` reads and checks every field before it changes the scene, so a damaged snapshot leaves the match as it was. It refuses unknown tags, trailing bytes, a zoom level or mode out of range, a camera center that is not a number or lies far from the board, and more than `skirmish.MaxUnits` IDs in a list. The outcome and the HUD are recomputed from the restored simulation.

## Saving and the last replay

The store holds two keys. `save` is a saved game, written by the Save entry: a `save.Encode` envelope around the snapshot. `last` is the last finished recording. The app writes `last` when it leaves a match, which happens when the player quits to the title, loads a save, starts watching a replay, or when the game closes. Loading a save begins a new recording whose start state is the loaded snapshot. A recording stops at 18,000 ticks, which is five minutes of play.

A host without storage plays without saving: Save and Load report that saving is unavailable, and nothing else changes. Failures of the store, of decoding, and of the content check appear as a notice at the bottom of the screen for about two and a half seconds and never end the game.

## Verification

- Headless tests in `cmd/playground` cover box, click, Shift, and group selection, every order and mode, a drag that never releases, snapshot restore at several ticks continuing identically, damaged snapshots, the recorded fight with a pinned trace and a divergence from a changed input, the app flow with an in-memory store, and a draw that allocates nothing.
- `make smoke-desktop` runs `--outpost`, which scripts the title, the recorded fight to a victory, a restart, a drag selection with the box drawn, hold, pause, save, load, quitting, and playing back the recording, checking text, markers, and bolts in the native back buffer.
- `make smoke-web` plays the same game in Chromium with real mouse and keyboard input and a real `localStorage`, including the Shift-click, control groups, attack-move, focus fire, and a replay whose final frame matches the match it recorded.
- `make smoke-outpost-roundtrip` authors the level's placements and zones in the Level Builder from the bare terrain, then runs both smokes against the exported file.
