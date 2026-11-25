# Playground: game-owned encounter prototype

This specification covers the in-repository reference command and its use of the public scene runtime. See the [game-owned playground RFC](../rfcs/0001-game-owned-playgrounds.md) for the broader design.

## Behavior

- The command defaults to the [M3 authored courtyard](unit-commands-m3.md). A primary click selects the scout and a secondary click commands it to a ground tile. Arrow keys pan, **W/S** step through zoom levels, and **R** restores the authored layout, spawns, camera, and empty selection.
- The authored scout plays the [M8 sprite sheet](sprite-sheet-v1.md): it blinks when stationary and hops on ticks that move it. Its sprite pivot follows the ground position without changing route geometry, paint order, or the selection rectangle from frame to frame. Restart restores the first idle frame without reloading the sheet.
- The same host launches the M1 pickup encounter with `GOJE_PLAYGROUND=pickup` on desktop or `?fixture=pickup` in the browser. It launches the [M2 patrol](world-view-m2.md) with `GOJE_PLAYGROUND=courtyard` or `?fixture=courtyard`. The patrol starts paused; **Space** toggles it.
- It launches the [M12 yard](collision-m12.md#yard-fixture), where the scout walks freely through the courtyard's level with collision, with `GOJE_PLAYGROUND=yard` or `?fixture=yard`. Arrow keys or WASD walk and **R** restarts. The yard is a separate fixture because the courtyard assigns those keys to camera control. Its deck and doorway come from the zones of a [version 2 level](level-v2.md).
- It launches the [M13 skirmish](skirmish-m13.md), where scouts and guards block each other and fight with bolts, with `GOJE_PLAYGROUND=skirmish` or `?fixture=skirmish`. It keeps the courtyard's controls: left click selects a scout, right click orders it, arrow keys pan, **W/S** zoom, and **R** restarts.
- It launches the [M14 outpost](outpost-m14.md), a battle with a title and pause menu, drag and group selection, attack-move, hold, and focus orders, a HUD in the built-in font, saving and loading, and a replay of the last match, with `GOJE_PLAYGROUND=outpost` or `?fixture=outpost`. Milestone 15 added fog of war, positional sound from eight synthesized cues, and touch and gamepad control to it.
- With a relay configured through `GOJE_RELAY` or the `relay` query parameter, two outpost players can host co-op or versus across desktop and web, then play back the last match without the relay. See [online play](outpost-m14.md#online-play-m16).
- It launches an input probe with `GOJE_PLAYGROUND=input` or `?fixture=input`, which draws a marker for the pointer, one for each touch, and the sticks and buttons of each connected pad. `make smoke-input` drives it in Chromium with real touch events and a fake gamepad.
- The encounter runs as an `engine.Scene` through `platform.Run`. Its factory supplies fresh mutable state; the host owns ticking, input, presentation, and the shared image. The scene requests `engine.Restart()` instead of reinitializing the host or maintaining a second reset mechanism.
- The pickup encounter begins with an amber player marker and a cyan token. Arrow keys or WASD move the marker in screen-relative directions. Moving through the token collects it; **R** restores both to their original positions before movement.
- The fixtures build for desktop and web and advance movement during simulation ticks, not draws.
- The isometric projection maps ground coordinates to screen offsets and applies elevation vertically without moving the ground position. The public Go package exposes this math without importing a windowing or rendering library.

## Verification

- A headless Go test covers ground axes and elevation independently.
- `make smoke-desktop` scripts simulation input snapshots and checks native OpenGL back buffers for the pickup, patrol, courtyard, yard, skirmish, and outpost fixtures, each in its own process with its own 30-second limit. The scripted framebuffer probe is independent of OS window focus and does not synthesize OS input events.
- `make smoke-web` drives real key and mouse events in Chromium and checks the same fixtures in canvas screenshots, including the authored wall detour, arrival, and reset, the yard's slide, doorway, and deck, the skirmish's crossing of two scouts and its selection, order, alarm, bolts, damage, defeat, and reset, and the outpost's drag selection, groups, orders, pause, save, load, and replay through a real `localStorage`, its fog of war, a real touch tap that shows the command buttons, and a fake gamepad's Start button that opens and closes the pause menu.
- `make smoke-online` plays co-op and versus between a native host and Chromium through a real relay. It checks agreement and both online replays, and that an injected divergence ends the match on both sides.
- `make check-example` copies the reference command into a separate Go module with workspace mode disabled and builds it natively and for WebAssembly. It uses a local module replacement to select the revision under test, not an import into Goje's internal packages.

Editors, asset formats, general-purpose engine APIs, and support claims for untested platforms are outside this slice.
