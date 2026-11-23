# RFC 0003: M2 — Camera, depth, and picking

**Status:** Accepted for M2. The [M2 world-view specification](../specs/world-view-m2.md) defines the contract, and [ADR 0003](../decisions/0003-tick-based-world-picking.md) records why picking uses the start of the consuming tick. The [M1 runtime specification](../specs/runtime-m1.md) still governs the host and runtime foundation.

## Goal

A game developer can show an isometric world, pan and zoom it, and select a visible unit with the mouse on desktop and web. The same Go scene and pure geometry code work in both hosts and in headless tests.

Use an isometric courtyard in the existing [reference playground](../specs/playground.md). Two units overlap each other and move in front of and behind a short wall along scripted paths; the player can select a unit and see a marker for that selection. Provide a repeatable overlap where one unit is elevated, plus keyboard pan, discrete zoom around the view center, and reset to the starting state. Keep the existing pickup encounter as an M1 regression fixture. The courtyard is example code, not an engine-owned level or editor.

[Phaser's cameras](https://docs.phaser.io/phaser/concepts/cameras) distinguish the logical viewport from the world it shows. Phaser also exposes [painter depth](https://docs.phaser.io/phaser/concepts/gameobjects/components#depth-component) and [explicit hit areas and top-only input](https://docs.phaser.io/phaser/concepts/input#top-only-game-object-input). Goje borrows those boundaries, not Phaser's display-object hierarchy or event dispatch model.

## Selected direction

| Decision | M2 direction | Alternative and tradeoff |
| --- | --- | --- |
| Camera | One orthographic isometric view with pan and uniform zoom around its center. | Cursor-anchored zoom feels natural in an RTS; it adds pointer dependencies and anchoring rules before basic transforms are proven. |
| Depth | Paint from back to front by ground-anchor depth, then stable object ID. Split long occluders into pieces only when a single anchor fails. | Footprint-based ordering can handle more complex overlap, but requires explicit cycle and split rules. Do not attempt it without scenes that need it. |
| Picking | At the start of the tick that consumes the press, search the same ordered items from front to back using explicit rectangles. A wall can block without being selectable. | Picking the last presented frame can better match fast motion, but requires retaining a presentation snapshot across catch-up, resize, and scene transitions. |
| Assets | Reuse M1's shared image and colored geometry to prove overlap. | An atlas makes a richer demo possible, but adds a resource API that this acceptance scene does not need. |

## Ownership and delivery order

| Step | Owner | Exit check |
| --- | --- | --- |
| 1. World view | `engine/`: pure camera transforms and an ordered world view with hit testing; keep `Canvas` in logical pixels. | Headless tests cover a fractional camera round-trip, elevated sprites, depth ties under reordered input, and select/block/pass-through. |
| 2. Mouse input | `internal/platform/`: shared viewport conversion and press buffering; GLFW and browser adapters supply events. The scene reads the normalized value from its tick input. | Test viewport edges, asymmetric bars, HiDPI, event-position retention, focus loss, and consumption across zero or multiple ticks per frame. |
| 3. Courtyard | `cmd/playground/` and `web/playground/`: one copyable scene built only on public APIs; fixture switching and smoke assertions stay outside the engine. | Show actual overlap pixels and the resulting selection on macOS/Linux and Chromium. Browser smoke uses real pointer events; the existing native framebuffer probe can drive deterministic snapshots, with a separate native check of host pointer mapping. Pan, zoom, resize into letterbox, and reset are exercised. The existing M1 smoke paths still pass. |
| 4. Performance and docs | Compare the courtyard with a dense fixture, starting at 2,048 world items, on the M1 host matrix. Document the game-facing API and how to reproduce the measurements. | Report warmed-up allocations, ordering and hit-query costs, frame and tick percentiles, and dropped ticks. Compare the unchanged M1 workload to its [baseline](../docs/runtime-baseline.md) on equivalent settings. |

Start with sorting reusable compact items and a reverse scan on a press. Require no steady-state allocation after representative buffers have grown. Reuse a prepared order when only selection or the camera changes; rebuild when world-item data changes. Measure before introducing a spatial index, parallel sorting, or a different renderer. Diagnostics remain opt-in.

## Out of scope

Camera rotation, perspective, multiple cameras, arbitrary footprint intersections, height-aware occlusion graphs, alpha picking, drag-box selection, touch, navigation, collision, asset formats, multiple textures, editor applications, and new host targets. Revisit these only after a concrete scenario exposes a limit of the proposed model.

The [M3 proposal](0004-m3-levels-and-unit-commands.md) uses the existing world view to load an authored level and command a unit to move around a wall. The M2 courtyard remains a regression fixture.
