# Playground: first playable slice

This specification covers the in-repository demo and smoke scenario. It does not define a reusable playground API; see the [game-owned playground proposal](../rfcs/0001-game-owned-playgrounds.md) for that work.

## Behavior

- The same playground command builds a desktop window and a browser game. Both present an isometric grid, an elevated marker and its ground shadow, and keyboard controls.
- Arrow keys or WASD move the marker in screen-relative directions. The marker stays on the board. The game advances movement during simulation ticks, not draws.
- The isometric projection maps ground coordinates to screen offsets and applies elevation vertically without moving the ground position. The public Go package exposes this math without importing a windowing or rendering library.

## Verification

- A headless Go test covers ground axes and elevation independently.
- `make smoke-desktop` opens a native window, moves the marker with a deterministic input sequence, and checks the rendered initial and final positions. It does not synthesize OS keyboard events.
- `make smoke-web` serves the built application on loopback, drives real key events in Chromium, and checks canvas screenshots before and after movement and after a viewport resize. Compilation alone does not satisfy either smoke check.

Editors, asset formats, general-purpose engine APIs, and support claims for untested platforms are outside this slice.
