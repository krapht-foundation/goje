# Playground: game-owned encounter prototype

This specification covers the in-repository reference command and its use of the public scene runtime. See the [game-owned playground RFC](../rfcs/0001-game-owned-playgrounds.md) for the broader design.

## Behavior

- The command defaults to the [M3 authored courtyard](unit-commands-m3.md). A primary click selects the scout and a secondary click commands it to a ground tile. Arrow keys pan, **W/S** step through zoom levels, and **R** restores the authored layout, spawns, camera, and empty selection.
- The same host launches the M1 pickup encounter with `GOJE_PLAYGROUND=pickup` on desktop or `?fixture=pickup` in the browser. It launches the [M2 patrol](world-view-m2.md) with `GOJE_PLAYGROUND=courtyard` or `?fixture=courtyard`. The patrol starts paused; **Space** toggles it.
- The encounter runs as an `engine.Scene` through `platform.Run`. Its factory supplies fresh mutable state; the host owns ticking, input, presentation, and the shared image. The scene requests `engine.Restart()` instead of reinitializing the host or maintaining a second reset mechanism.
- The pickup encounter begins with an amber player marker and a cyan token. Arrow keys or WASD move the marker in screen-relative directions. Moving through the token collects it; **R** restores both to their original positions before movement.
- The fixtures build for desktop and web and advance movement during simulation ticks, not draws.
- The isometric projection maps ground coordinates to screen offsets and applies elevation vertically without moving the ground position. The public Go package exposes this math without importing a windowing or rendering library.

## Verification

- A headless Go test covers ground axes and elevation independently.
- `make smoke-desktop` scripts simulation input snapshots and checks native OpenGL back buffers for all three fixtures. It does not synthesize OS input events.
- `make smoke-web` drives real key and mouse events in Chromium and checks the same fixtures in canvas screenshots, including the authored wall detour, arrival, and reset.
- `make check-example` copies the reference command into a separate Go module with workspace mode disabled and builds it natively and for WebAssembly. It uses a local module replacement to select the revision under test, not an import into Goje's internal packages.

Editors, asset formats, general-purpose engine APIs, and support claims for untested platforms are outside this slice.
