# Playground: game-owned encounter prototype

This specification covers the in-repository reference command and its use of the public scene runtime. See the [game-owned playground RFC](../rfcs/0001-game-owned-playgrounds.md) for the broader design.

## Behavior

- The command defaults to the [M2 courtyard](world-view-m2.md). On entry, the patrol is paused at a repeatable overlap. Arrow keys pan, **W/S** step through zoom levels, **Space** starts or pauses the patrol, and **R** restores the initial camera, patrol, and selection.
- The same host launches the M1 pickup encounter when `GOJE_PLAYGROUND=pickup` is set on desktop or `?fixture=pickup` is present in the browser URL.
- The encounter runs as an `engine.Scene` through `platform.Run`. Its factory supplies fresh mutable state; the host owns ticking, input, presentation, and the shared image. The scene requests `engine.Restart()` instead of reinitializing the host or maintaining a second reset mechanism.
- The reference encounter begins with a player marker at the center of the board and a cyan token to its right. Moving through the token collects it. Pressing **R** restores both to their initial positions without restarting the game and skips movement for that tick.
- Both fixtures build for desktop and web and present an isometric grid, elevated sprites, and ground shadows.
- Arrow keys or WASD move the marker in screen-relative directions. The marker stays on the board. The game advances movement during simulation ticks, not draws.
- The isometric projection maps ground coordinates to screen offsets and applies elevation vertically without moving the ground position. The public Go package exposes this math without importing a windowing or rendering library.

## Verification

- A headless Go test covers ground axes and elevation independently.
- `make smoke-desktop` opens native windows, scripts simulation input snapshots, and checks the actual OpenGL back buffers for both fixtures. It does not synthesize OS input events.
- `make smoke-web` drives real key and mouse events in Chromium and checks both fixtures in canvas screenshots, including resize and pickup focus loss. See the [M2 verification contract](world-view-m2.md#verification) for courtyard coverage.
- `make check-example` copies the reference command into a separate Go module with workspace mode disabled and builds it natively and for WebAssembly. It uses a local module replacement to select the revision under test, not an import into Goje's internal packages.

Editors, asset formats, general-purpose engine APIs, and support claims for untested platforms are outside this slice.
