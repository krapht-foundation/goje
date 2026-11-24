# RFC 0009: M8 — Animated sprites

**Status:** Implemented and verified for M8. See the [sprite sheet v1 specification](../specs/sprite-sheet-v1.md) for the asset format and validation contract.

## Problem and goal

[M7](0008-m7-game-projects.md) proved that a separate Go module can load Goje project assets and run its own 2D game on desktop and web. The [runtime](../docs/runtime.md#choose-a-view-for-the-game) also draws depth-sorted isometric sprites, but both kinds of games must currently supply their own frame coordinates and playback logic.

M8 should let a developer draw a character in an existing art tool, describe its idle and movement clips, and play the same asset in the independent top-down Beacon game and the isometric courtyard. The game decides why a character moves and when to change clips. Goje supplies validated frame data and deterministic playback.

Use Goje's [tomato mascot](../docs/brand/goje.svg) for the first sprite sheet. The idle clip blinks and the movement clip hops. Include frames with different bounds and pivots so the demos can show whether the sprite stays anchored. The runtime still accepts a game developer's own sheet; it does not depend on Goje's branding.

## Decisions for review

| Decision | Recommended | Alternative and tradeoff |
| --- | --- | --- |
| Authoring | Accept a PNG sheet and a small Goje JSON sidecar. Use the exported PNG directly and hand-author the metadata for this milestone. | Import a specific editor's format or build an editor now. Either reduces manual entry but commits to editor-specific or unproven conventions before the runtime contract is tested. |
| Project integration | The game explicitly loads the pair at startup. Keep `project.Load` and the v1 manifest unchanged. | Add sprites to a new manifest version. That gives tools asset discovery, but introduces schema migration and library-managed asset selection before games need either. |
| Rendering | Pack the sheet alongside existing art in the game's single launch image. Reuse `Canvas.Image` and both existing hosts. | Add multiple textures or a public atlas manager. That supports larger art sets, but introduces GPU resource and batching policy without evidence that this milestone needs it. |
| Playback | Loop named clips on fixed ticks, with one playback state per actor. | Start with one-shot clips, events, or blending as well. Those support more gameplay, but require completion and interruption rules not needed for idle and movement. |

## Proposed design

### Asset format and loading

- Add a shared Go decoder that takes separate readers for the PNG and the JSON. Games choose the files, and a read-only command-line validator uses the same decoder. The sidecar never names another file. The decoder cannot open paths or fetch URLs. Embedded assets work on both hosts. Keep the [version 1 project manifest](../specs/project-v1.md) strict and unchanged: existing projects still load, and `projectcheck` does not claim to validate extra game-owned files.
- Use a versioned JSON object with a `version` integer and a `clips` array. Each clip has a unique, case-sensitive `name` and a nonempty, ordered `frames` array. A frame contains integer `x`, `y`, `width`, and `height` in sheet pixels, integer `pivotX` and `pivotY` relative to the frame's upper-left edge, and a positive integer `ticks`. The rectangle uses exclusive maximum edges. Pivots may be on the frame edges, so a character can anchor at its bottom edge. No fields are implicit; a game selects its initial clip explicitly. Reused or overlapping frame rectangles are valid.
- Initial v1 limits: 1 MiB encoded PNG, 64 KiB UTF-8 JSON, sheet dimensions of 1–512 pixels per axis, up to 32 clips and 256 frame entries across the asset, and 1–3,600 ticks per frame. Names are 1–32 ASCII bytes, begin with a lowercase letter, and thereafter contain only lowercase letters, digits, or underscores. These are import limits for this workflow, not GPU limits.
- Reject missing, unknown, duplicate, or null fields, duplicate clip names, unsupported versions, invalid or out-of-sheet rectangles, out-of-frame pivots, trailing JSON values, and malformed or oversized inputs. Inspect PNG dimensions before decoding pixels from the same bounded bytes. Validate integers and bounds before computing endpoints. Return no partial asset on failure and do not rewrite source files. A game must report startup failures rather than substituting default art.

### Playback and rendering

- Share immutable clip definitions across actors. Resolve clip names once during setup; game updates select the resolved idle or move clip based on actual movement. On scene entry or a clip change, select the first frame and leave it at elapsed tick zero. Selecting the same clip again does not reset it. On each subsequent tick spent in the same clip, advance elapsed time; move to the next frame at its duration boundary and loop from the last frame to the first. Draw only reads the current frame. Catch-up advances once per executed tick, and paused or discarded ticks do not advance playback, matching the [runtime timing contract](../specs/runtime-m1.md#host-timing-and-input).
- Pack the validated sheet next to existing game art before host launch. Translate frame source bounds by the sheet's atlas offset once during startup. Keep the [one launch image](../specs/runtime-m1.md#presentation-and-resources), nearest-neighbor sampling, and painter order. No decoding, packing, name lookup, or GPU upload happens in tick or draw. Restart restores the initial idle frame through a fresh scene without reloading the asset or uploading a new texture.
- Place the frame by aligning its pivot with the actor's visual anchor. Subtract the pivot offsets from the anchor and scale both offsets and frame size by the same zoom. A flat game can use logical coordinates; an isometric game projects the anchor and then applies the camera. The ground position and stable item ID still determine world order; authored hit rectangles stay independent of frame dimensions and PNG alpha. See the [world-view specification](../specs/world-view-m2.md#depth-and-hit-geometry).

## Delivery and acceptance

| Slice | Exit check |
| --- | --- |
| 1. Import and validation | After acceptance, write the versioned asset spec and use the same decoder in the CLI and games. Author the mascot sheet in an existing tool, pair it with the sidecar, validate it, and load those exact files in native and Wasm builds. Cover malformed, oversized, and out-of-bounds files. Document provenance and how to substitute a developer's own pair. |
| 2. Tick playback and composition | Verify exact frame and loop boundaries, same-clip selection, clip changes, and restart headlessly. Use asymmetric frames with different bounds and pivots to expose atlas-offset or pivot bugs. Exercise native and Wasm shared code. |
| 3. Two playable views | Use the same source pair in Beacon, built as a separate module from public Goje packages, and in the courtyard. On native OpenGL and Chromium WebGL, inspect pixels for idle and movement frames, stable ground anchoring, and reset. In the courtyard, verify correct overlap with another world item across frame changes and zoom. In Beacon, demonstrate movement without replacing the existing toggle-and-reset workflow. Verify that restart performs no asset reads or uploads. |
| 4. Performance | Measure decode and atlas packing at startup separately from warmed playback and rendering. Compare fixed-frame sprites with animated sprites at the same actor count and quad count, in a representative scene and a dense 2,048-actor fixture. Record warmed allocations, tick and frame time percentiles, and dropped ticks on native and Chromium. Require no additional steady-state allocations; investigate repeatable pacing regressions rather than setting a machine-specific CI timer. |

### Playable views verification

`make smoke-beacon`, `make smoke-desktop`, `make smoke-web`, and `make smoke-level-roundtrip` passed on macOS OpenGL and Chromium WebGL. Both games showed the same tomato blinking, hopping above its ground anchor, and returning to its initial pose on reset. The courtyard also preserved the planted pivot under zoom, wall occlusion during movement, and the shortcut from the exact level editor export.

The courtyard's reset test observed no asset reads after startup. Beacon's scene factory likewise has no filesystem dependency. Both hosts upload the launch atlas when creating the renderer, outside the scene restart path.

### Performance verification

The [sprite measurements](../docs/sprite-performance.md#results) compare fixed and animated sprites at one and 2,048 actors, with matching quad counts. Headless native and Wasm runs allocated nothing after warmup. Animation added no Go heap allocations in the graphical runs, and all 24 runs on macOS OpenGL and Chromium WebGL reported zero dropped ticks. The dense workload showed additional CPU work without an increase in measured frame interval p95.

## Out of scope

A Goje sprite editor, third-party metadata import, a project asset catalog, multiple runtime textures, hot reload, one-shot clips, animation events, blending, skeletal animation, video, 3D meshes, and changes to collision or picking rules. This milestone supports animated sprites in flat and projected scenes; it does not provide a pipeline for 2D actors in fully 3D environments.
