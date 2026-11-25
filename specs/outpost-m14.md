# Outpost: the reference game of M14 and M15

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

When the host is configured with a relay, the title menu gains **Host online game** and, when a room code is also given, **Join room CODE**. The relay is `GOJE_RELAY` on desktop and the `relay` query parameter in the browser, and the room is `GOJE_ROOM` or the `room` parameter. A value that is not a `ws://` or `wss://` address without a path, or a room that is not six characters of the code alphabet, is ignored. The [relay](relay-m16.md) pairs the players and the [lockstep protocol](lockstep-m16.md) keeps their matches in step. The design is in [RFC 0017](../rfcs/0017-m16-online-lockstep-play.md).

The outpost gains three states:

| State | What it shows | Keys and pointer |
| --- | --- | --- |
| Lobby | The connection being made, and for a host the room code and a wait for the guest. | **Escape** or a gamepad's Start button cancels and returns to the title. |
| Online | The match, with the partner's scouts in teal, a notice while it waits for the other player, and a menu: Resume, Sound, Leave the match. | The match controls, except that **R** does nothing. **Escape**, Start, or the Menu button opens the menu over a match that keeps running. |
| Ended | One or more lines that say why the match ended: the other player left, stopped responding, sent invalid data, the games no longer agree, or the connection was lost. | **Enter**, **Escape**, Start, or a tap returns to the title. |

A match has two players, and the guards stay under the simulation's control, so it is cooperative. The first player commands the scouts with odd IDs and the second those with even IDs. Selection, a box, and a control group contain only the player's own scouts. A command that a player gives is encoded as an [M16 command](skirmish-m13.md#commands-m16), sent through the session, and applied at the scheduled tick by every copy in player order, the player's own included, so each copy changes its units only through commands that every copy has. A received command that names a unit that does not exist, is not a scout, or is not the sender's ends the match, because an honest copy never sends one. A scout that fell after the command was issued is allowed.

The session runs at the lockstep delay of five, so a command takes effect six ticks, 100 ms, after it was issued. The match checks the hash of the simulation, `Sim.Trace`, every 60 ticks and ends when the limit of 600 ticks without the other player's commands passes. Selection, the camera, the volume, and the menus are local and are not in the hash. A match is not recorded, saved, or restored, and a match started with the **Start** entry is unchanged by any of this.

## What a recording and a save hold

A match is the `outpostSession` scene. It reads only the input it is given and its own state, so it is replayable under the rules in [Make a scene replayable](../docs/replays.md#make-a-scene-replayable). `StateHash` digests the simulation's trace, which includes the explored cells, the selection and groups, the pending mode, a drag in progress, the zoom, the camera center, and the state of the touch buttons and the gamepad cursor. The recorder samples it at its checkpoint interval, and playback fails with a divergence error at the first checkpoint that differs.

The content check of both formats is `replay.ContentHash` of the level file's bytes and the string `outpost-2`. A level edited in the Level Builder, or a change to the rules that the level cannot express, retires old recordings and saves. Changing the string `outpost-2` retires them deliberately.

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

- Headless tests in `cmd/playground` cover two players' copies of a match over an in-memory network with and without latency agreeing on every checked tick and selecting only their own scouts, a player who stops being stalled out, forged commands being rejected, a changed hash stopping both players, and the launch parameters.
- `make smoke-online` plays a match between a native host with no window and the WebAssembly build in Chromium through a real relay, requires fifteen hashes to be logged by both and to be equal, and then injects a changed hash into the host and requires both players to report it.
- Headless tests in `cmd/playground` cover box, click, Shift, and group selection, every order and mode, a drag that never releases, snapshot restore at several ticks continuing identically, damaged snapshots, the recorded fight with a pinned trace and a divergence from a changed input, the app flow with an in-memory store, and a draw that allocates nothing.
- Further headless tests cover fog hiding the guards until a scout sees them and never listing a guard the scouts cannot see, the sounds of the recorded fight, their range, rate limit, determinism, and independence from the volume, a gamepad moving a cursor and clicking, touch buttons giving commands without starting a drag, a match played by pad alone replaying and restoring exactly, and Start opening and closing the pause menu.
- `make smoke-desktop` runs `--outpost`, which scripts the title, the recorded fight to a victory, a restart, a drag selection with the box drawn, hold, pause, save, load, quitting, playing back the recording, a touch tap that shows the buttons, a gamepad cursor, and Start pausing, checking text, markers, bolts, and that the guards and their ground are not drawn at the start in the native back buffer.
- `make smoke-web` plays the same game in Chromium with real mouse and keyboard input and a real `localStorage`, including the Shift-click, control groups, attack-move, focus fire, and a replay whose final frame matches the match it recorded, and then a real touch tap through the Chrome DevTools protocol and a fake pad behind `navigator.getGamepads`.
- `make smoke-outpost-roundtrip` authors the level's placements and zones in the Level Builder from the bare terrain, then runs both smokes against the exported file.
