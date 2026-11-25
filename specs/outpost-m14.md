# Outpost: the reference game of M14–M17

The outpost is the playground fixture that exercises the milestone 14 features together: [text and widgets](font-v1.md), pointer input and group selection, the [RTS orders](skirmish-m13.md#orders-and-stances-m14) of the skirmish simulation, [recordings](replay-v1.md), and [saved games](save-v1.md). Milestone 15 revised it, as the section [The M15 revision](#the-m15-revision) describes: line of sight and fog of war from [vision](vision-m15.md), positional sound from the [mixer](audio-mixer-m15.md), and touch and gamepad control. It is selected with `GOJE_PLAYGROUND=outpost` on desktop and `?fixture=outpost` in the browser. The skirmish fixture is unchanged and keeps its own pinned traces.

## The level

The level is `cmd/playground/levels/outpost.json`, a version 2 level of 11 × 9 cells. Six scouts with IDs 1 to 6 stand in the two left columns, and six guards with IDs 11 to 16 stand in the two right columns. Four wall cells make two short pillars in column 3. An alarm trigger zone fills column 5, and a solid deck zone covers two cells in the middle. The skirmish simulation reads these as it does for the skirmish: crossing the alarm wakes the guards, and bolts pass under the deck. The level is authored in the Level Builder; `make smoke-outpost-roundtrip` rebuilds its placements and zones from the bare terrain and plays the exported file.

The whole board fits the 320 × 200 logical viewport at the default zoom. The camera pans with the arrow keys and zooms with **W** and **S** as in the courtyard, and these are part of a match's state.

## States

The game is one scene, `outpostApp`, with four states.

| State | What it shows | Keys and pointer |
| --- | --- | --- |
| Title | The name and a menu: Start, Load, Watch last replay. | Arrow keys and **Enter**, the pointer, a gamepad's D-pad and south button, or a tap. |
| Playing | The match, a top bar with the unit counts and the alarm, a bottom bar with the selection and a hint, and a red recording clock. | The match controls below. **Escape** opens the pause menu unless the match has something to cancel. A gamepad's Start button always opens it, and a Menu button appears at the top right once a finger has touched the screen. |
| Paused | The match, dimmed, with a menu: Resume, Save, Load, Watch replay, Restart, Sound, Quit to title. The Sound entry steps the volume from full to half to off. | Arrow keys and **Enter**, the pointer, or a gamepad. **Escape** or a gamepad's Start button resumes. |
| Watching | The recording playing back, with a red label that reads REPLAY and then REPLAY ENDED. | **Escape**, a gamepad's Start button, or the Leave button returns to the title. Other live input is ignored. |

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

## The M15 revision

The content check string is now `outpost-2`, so every recording and save made before the revision is refused by the content check and none can be misread. The level file is unchanged.

### Fog of war

The match calls `Sim.EnableVision` with a radius of 7 cells, so the rules of [the vision specification](vision-m15.md) apply: a unit fires at an enemy only while its team sees it, the guards wake when their team sees a scout, and the alarm still wakes them all. The scene draws only what the scouts have learned. Ground is not drawn in cells they have never seen. Ground in cells they have seen but do not see now is drawn through a dark tint, and ground in sight is drawn as before. Walls, the deck, and the alarm line are drawn in explored cells. A guard is drawn, selectable, and a target of **F** only while the scouts see the cell that holds its center, and a bolt is drawn only while the scouts see its cell. The top bar counts the guards in sight, not the guards alive, and the end of the match is still decided by all of them. The grid lines are not drawn.

### Sound

The scene turns the simulation's events into cues: a bolt fired, a bolt that hit, a unit that fell, and the alarm. It also plays an acknowledgment when an order reaches at least one unit, and a victory or defeat sound once. The event position is converted to the screen with the camera and given to `audio.Spatialize` with the center of the screen as the listener and 260 logical pixels as the hearing distance, so the pan follows what is on screen. A cue plays at most once every three ticks. Menu choices play a click.

The eight sounds are `cmd/playground/sounds/*.wav`, made with the Sound Maker's synthesis from the settings in `outpost_sounds_test.go`, which also checks that the files match. Playback is output only. The volume is a setting of the player, not of the match: it is neither recorded, hashed, nor saved, and the Sound entry of the pause menu changes it. A replay is heard as it was played.

### Touch and gamepad

- **Touch.** The first finger is the pointer, so a tap selects and a drag draws the box. After the first touch, a row of four buttons appears at the bottom of the board: **Move** and **Attack** arm a move or an attack-move for the next tap on the ground, **Hold** holds fire, and **Focus** orders the selection to attack the nearest guard in sight. A press that lands on a button is consumed by it and never starts a drag. A mouse press hides the buttons again.
- **Gamepad.** The left stick moves a cursor at up to three logical pixels per tick, and the south button is the primary button and the east button the secondary button. The west button arms an attack-move, the north button holds fire, the shoulder buttons recall the next or previous non-empty control group, the triggers zoom, the right stick pans the camera, and Start opens the pause menu. Moving the mouse hands control back to the mouse.

Both are interpreted inside the scene from the recorded input, so a match played by touch or pad replays exactly. The scene's own state for this, the cursor, the previous buttons, the control group the shoulders are at, and whether the touch buttons are shown, is part of the hash and the snapshot.

## Online play (M16)

With a relay configured, the title menu adds **Host co-op**, **Host versus**, and **Join room CODE** when a room code is given. Set `GOJE_RELAY` and `GOJE_ROOM` on desktop or `relay` and `room` in the browser. Invalid addresses or room codes are ignored. The [relay](relay-m16.md) pairs players and the [lockstep protocol](lockstep-m16.md) keeps their matches in step.

M18 adds [live watch and rejoin](outpost-m18.md). It does not change the following setup message or ownership rules.

The title has a **Guards: Classic/Tactical** option that applies to new solo and hosted co-op matches. Tactical guards use the [opt-in skirmish rules](skirmish-m13.md#versus-and-tactics-m17). Versus ignores this option because the guest commands the guards.

| State | What it shows | Keys and pointer |
| --- | --- | --- |
| Lobby | The connection being made, and for a host the room code and a wait for the guest. | **Escape** or a gamepad's Start button cancels and returns to the title. |
| Online | The match, a notice while it waits for the other player, and a menu with Resume, Sound, and Leave the match. In co-op, the partner's scouts are teal. | The match controls, except that **R** does nothing. **Escape**, Start, or the Menu button opens the menu over a match that keeps running. |
| Ended | The result or the reason the game stopped: a leave, stall, invalid data, divergence, or a lost connection. | **Enter**, **Escape**, Start, or a tap returns to the title. |
| Watching an online match | The last recorded match reconstructed without a relay, with a free camera. The final state remains on screen. | **Tab** or the **Fog** button switches the viewing team. **Escape**, Start, or **Leave** returns to the title. |

### Match setup and ownership

After the guest joins, the host sends exactly three bytes before its lockstep Hello: `0x53`, mode (`0` co-op, `1` versus), and tactics (`0` Classic, `1` Tactical). Versus requires tactics `0`. The guest refuses any other setup. The Hello checks the content hash for the selected level and rules.

In co-op, the first player owns the scouts with odd IDs and the second owns the even IDs; the guards are still driven by the simulation. In versus, the first player owns all scouts and the second owns all guards. [`versus.json`](../cmd/playground/levels/versus.json) is an 11 × 9 [version 3 level](level-v3.md) with mirrored spawns and walls, two sight-blocking hedges, and no alarm. Each side sees and hears the events it can observe, and the HUD counts the opponents in sight. A side wins when only its units remain; no living units on either side is a draw.

Only units owned by the local player can be selected or put in a control group. Commands use the [M16 encoding](skirmish-m13.md#commands-m16). Both copies validate the full tick, apply its commands in player order, then step. A command that names an unknown unit or one its sender does not own ends the match. A fallen unit the sender owns is allowed.

The session runs with a delay of five, so an order takes effect after six ticks, or 100 ms at 60 Hz. The simulation trace is checked every 60 ticks. The original stall limit was 600 updates; M18 allows 1,200 in an active room so that transport recovery can begin. At a natural result, both copies stop stepping and confirm the final tick before closing. The camera, selection, volume, and menus are local and do not affect the trace.

### Recording and playback

Each player saves the [match log](match-log-v1.md) as `last-online` when the match exits, even after a disconnect, leave, or desync. It contains only completed ticks and local checkpoints, including a final hash; pending commands do not appear. If the log fills, the app keeps the recorded prefix and reports the limit. If storage is unavailable, the app keeps the last log for the current run.

**Watch last online match** checks the log's content and replays the same commands without a connection. At a mismatched checkpoint it stops and reports the divergent tick. Online matches cannot be saved as resumable snapshots.

### Music

The title and match share a [score](music-m17.md) rendered once on startup. The music plays across the title-to-match transition, ducks under either in-game menu, and fades out when the match ends. The **Sound** setting controls both the music and the cues. In a browser, the host waits for a user gesture before playing.

## What a recording and a save hold

A match is the `outpostSession` scene. It reads only the input it is given and its own state, so it is replayable under the rules in [Make a scene replayable](../docs/replays.md#make-a-scene-replayable). `StateHash` digests the simulation's trace, which includes the explored cells, the selection and groups, the pending mode, a drag in progress, the zoom, the camera center, and the state of the touch buttons and the gamepad cursor. The recorder samples it at its checkpoint interval, and playback fails with a divergence error at the first checkpoint that differs.

The Classic content check is `replay.ContentHash` of the level file's bytes and `outpost-2`. Tactical solo and co-op derive a distinct check from that value and `tactical-1`; solo playback and loading choose the rules from the check, not the current title option. Versus uses the hash of its level file and `versus-1`. A different level or rule version fails the content check.

A snapshot is one tag byte with the value 2, then the following in little-endian order:

1. A length of four bytes and the simulation snapshot of `Sim.Snapshot`.
2. The zoom level and the pending mode, one byte each.
3. The camera center, two IEEE 754 doubles.
4. The drag in progress: an active byte, an additive byte, and four doubles for its start and current points.
5. The selection and then the nine groups, each a two-byte count followed by that many eight-byte IDs.
6. The controls: the cursor and the last pointer position as four doubles, the previous gamepad buttons as two bytes, and one byte each for whether the pointer was inside, the pad is driving the cursor, the touch buttons are shown, and the control group the shoulders are at.

`Restore` reads and checks every field before it changes the scene, so a damaged snapshot leaves the match as it was. It refuses unknown tags, trailing bytes, a zoom level, mode, or group index out of range, a cursor or pointer position that is not a number or lies outside the viewport, a camera center that is not a number or lies far from the board, and more than `skirmish.MaxUnits` IDs in a list. The outcome and the HUD are recomputed from the restored simulation.

## Saving and the last replay

The store holds two keys. `save` is a saved game, written by the Save entry: a `save.Encode` envelope around the snapshot. `last` is the last finished recording. The app writes `last` when it leaves a match, which happens when the player quits to the title, loads a save, starts watching a replay, or when the game closes. Loading a save begins a new recording whose start state is the loaded snapshot. A recording stops at 18,000 ticks, which is five minutes of play.

A host without storage plays without saving: Save and Load report that saving is unavailable, and nothing else changes. Failures of the store, of decoding, and of the content check appear as a notice at the bottom of the screen for about two and a half seconds and never end the game.

## Verification

- Headless tests exercise command ownership and fog in co-op and versus, stalled and malicious peers, result confirmation, persistence, and replay checkpoint verification.
- `make smoke-online` runs co-op, an injected mismatch, and versus across a native host and Chromium. The versus run also plays back both locally recorded logs.
- Headless tests in `cmd/playground` cover box, click, Shift, and group selection, every order and mode, a drag that never releases, snapshot restore at several ticks continuing identically, damaged snapshots, the recorded fight with a pinned trace and a divergence from a changed input, the app flow with an in-memory store, and a draw that allocates nothing.
- Further headless tests cover fog hiding the guards until a scout sees them and never listing a guard the scouts cannot see, the sounds of the recorded fight, their range, rate limit, determinism, and independence from the volume, a gamepad moving a cursor and clicking, touch buttons giving commands without starting a drag, a match played by pad alone replaying and restoring exactly, and Start opening and closing the pause menu.
- `make smoke-desktop` runs `--outpost`, which scripts the title, the recorded fight to a victory, a restart, a drag selection with the box drawn, hold, pause, save, load, quitting, playing back the recording, a touch tap that shows the buttons, a gamepad cursor, and Start pausing, checking text, markers, bolts, and that the guards and their ground are not drawn at the start in the native back buffer.
- `make smoke-web` plays the same game in Chromium with real mouse and keyboard input and a real `localStorage`, including the Shift-click, control groups, attack-move, focus fire, and a replay whose final frame matches the match it recorded, and then a real touch tap through the Chrome DevTools protocol and a fake pad behind `navigator.getGamepads`.
- `make smoke-outpost-roundtrip` authors the level's placements and zones in the Level Builder from the bare terrain, then runs both smokes against the exported file.
