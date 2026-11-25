# Playground: game-owned encounter prototype

This specification covers the in-repository reference command and its use of the public scene runtime. See the [game-owned playground RFC](../rfcs/0001-game-owned-playgrounds.md) for the broader design.

## Behavior

- The command defaults to the [M3 authored courtyard](unit-commands-m3.md). A primary click selects the scout and a secondary click commands it to a ground tile. Arrow keys pan, **W/S** step through zoom levels, and **R** restores the authored layout, spawns, camera, and empty selection.
- The authored scout plays the [M8 sprite sheet](sprite-sheet-v1.md): it blinks when stationary and hops on ticks that move it. Its sprite pivot follows the ground position without changing route geometry, paint order, or the selection rectangle from frame to frame. Restart restores the first idle frame without reloading the sheet.
- The same host launches the M1 pickup encounter with `GOJE_PLAYGROUND=pickup` on desktop or `?fixture=pickup` in the browser. It launches the [M2 patrol](world-view-m2.md) with `GOJE_PLAYGROUND=courtyard` or `?fixture=courtyard`. The patrol starts paused; **Space** toggles it.
- It launches the [M12 yard](collision-m12.md#yard-fixture), where the scout walks freely through the courtyard's level with collision, with `GOJE_PLAYGROUND=yard` or `?fixture=yard`. Arrow keys or WASD walk and **R** restarts. The yard is a separate fixture because the courtyard assigns those keys to camera control. Its deck and doorway come from the zones of a [version 2 level](level-v2.md).
- It launches the [M13 skirmish](skirmish-m13.md), where scouts and guards block each other and fight with bolts, with `GOJE_PLAYGROUND=skirmish` or `?fixture=skirmish`. It keeps the courtyard's controls: left click selects a scout, right click orders it, arrow keys pan, **W/S** zoom, and **R** restarts.
- The encounter runs as an `engine.Scene` through `platform.Run`. Its factory supplies fresh mutable state; the host owns ticking, input, presentation, and the shared image. The scene requests `engine.Restart()` instead of reinitializing the host or maintaining a second reset mechanism.
- The pickup encounter begins with an amber player marker and a cyan token. Arrow keys or WASD move the marker in screen-relative directions. Moving through the token collects it; **R** restores both to their original positions before movement.
- The fixtures build for desktop and web and advance movement during simulation ticks, not draws.
- The isometric projection maps ground coordinates to screen offsets and applies elevation vertically without moving the ground position. The public Go package exposes this math without importing a windowing or rendering library.

## Verification

- A headless Go test covers ground axes and elevation independently.
- `make smoke-desktop` scripts simulation input snapshots and checks native OpenGL back buffers for the pickup, patrol, courtyard, yard, and skirmish fixtures, each in its own process with its own 20-second limit. The scripted framebuffer probe is independent of OS window focus and does not synthesize OS input events.
- `make smoke-web` drives real key and mouse events in Chromium and checks the same fixtures in canvas screenshots, including the authored wall detour, arrival, and reset, the yard's slide, doorway, and deck, and the skirmish's crossing of two scouts and its selection, order, alarm, bolts, damage, defeat, and reset.
- `make check-example` copies the reference command into a separate Go module with workspace mode disabled and builds it natively and for WebAssembly. It uses a local module replacement to select the revision under test, not an import into Goje's internal packages.

Editors, asset formats, general-purpose engine APIs, and support claims for untested platforms are outside this slice.
