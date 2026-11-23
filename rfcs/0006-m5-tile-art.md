# RFC 0006: M5 — Custom tile art

**Status:** Implemented and verified for M5. Custom ground art builds on the [M4 editor-to-playground round trip](0005-m4-level-builder.md#acceptance-results) without changing the [Goje v1 level schema](../specs/level-v1.md).

## Goal

A developer can make a ground tile in an existing pixel art tool, validate it, and play the edited courtyard with that artwork on desktop and web. The scout still takes the opening made in M4. The art changes presentation, not walkability, picking, placement, or routing.

## Selected direction

| Decision | Selected | Alternative and tradeoff |
| --- | --- | --- |
| Import workflow | Accept one PNG from an existing art tool. Validate it with a Go CLI and embed that same file when building the reference game. | Build a browser Tile Maker first. That improves authoring convenience but commits to an editor workflow before the runtime art contract is proven. |
| Tile geometry | Use one 32 × 16 orthographic isometric diamond image for the courtyard, with its rectangle centered at ground elevation zero. | Allow arbitrary dimensions and per-tile pivots. That supports overhangs and other projections but requires an authored metadata contract that this slice does not need. |
| Art assignment | The reference game assigns one tile to every `.` cell. Keep art out of the v1 level file. | Put tile IDs and image references in the level. That enables per-cell painting but requires schema evolution and a project asset resolver. |
| Renderer | Extend the existing startup texture to support drawing source regions. Pack the tile and existing scout marker into one texture at startup. | Bind separate textures as draw order changes. That scales to more art but adds resource and batching policy before there is a workload that needs it. |

## First tile contract

- The reference game accepts one PNG, at most 64 KiB encoded and exactly 32 × 16 pixels. PNG rows run top to bottom; the center of the image rectangle is `(16, 8)` measured from its top-left edge. Authors use transparent pixels around the diamond. The importer does not infer a shape or generate an alpha mask.
- Project the center of cell `(x, y)` from `(x+0.5, y+0.5, 0)`, then apply the existing camera. At zoom 1 the destination rectangle extends 16 projected pixels left and right and 8 pixels up and down from that center. Zoom scales those offsets uniformly; the host still owns letterboxing and framebuffer scaling. See the [world-view specification](../specs/world-view-m2.md).
- Draw walkable tile art after the background and before the grid, destination indicator, and depth-sorted walls and placements. Use nearest-neighbor sampling, no mipmaps, and the same straight-alpha blending policy on desktop and web. Art remains outside hit testing. Blocked cells do not receive the walkable tile.
- The game supplies a reference tile and owns its art assignment. A developer can use `tilecheck` on a custom file, then substitute that file in a scratch copy of the reference command. The checker never rewrites it, and the build embeds the original bytes. A bad substitution fails at startup before entering the scene rather than silently using the reference tile. The Level Builder continues to edit and export v1 geometry only.

## Ownership and resource limits

The tile checker and reference game share one bounded Go PNG loader using the standard library. Read at most the byte limit plus one byte to detect overflow, inspect the dimensions before allocating decoded pixels, and decode from the same bounded bytes. Reject malformed, truncated, oversized, or wrong-size inputs. Neither the loader nor a level file resolves URLs, follows references, or opens another file.

The reference game packs the validated tile and existing marker into known rectangles in a small texture before launching the host. Do not introduce a public atlas file format or a general atlas packer. The draw API should take a source rectangle in launch-texture pixels, with a top-left origin and exclusive max edges, and a separate destination in logical pixels. Define invalid regions as no draw (not clipping or wrapping), and migrate the existing marker draws to explicit regions. Use the same frame vertices and paint order on both hosts; test neighboring regions for sampling bleed. The host still owns upload and disposal, and restart must not decode or upload the texture again. This extends the [M1 presentation contract](../specs/runtime-m1.md#presentation-and-resources) without changing scene lifecycle.

Compute the authored courtyard's walkable draw positions once per scene entry and reuse them. Draw code does not decode, repack, allocate per tile, or write gameplay state. Start with row-major ground draws in the existing batch. Measure before adding culling, a second batch, or a different representation.

## Delivery and acceptance

| Slice | Exit check |
| --- | --- |
| 1. PNG import | Validate a custom non-symmetric tile through `tilecheck` and the shared runtime loader. Test wrong dimensions, malformed and truncated data, and the byte limit. Add an original reference tile and document its provenance. |
| 2. Texture regions and ground layer | Test source-rectangle bounds and UV orientation headlessly, then check rendered pixels on native OpenGL and Chromium WebGL. Distinguish the custom tile from its transparent corners and the neighboring atlas region; verify it aligns under the grid while zooming. Keep existing pickup and scout rendering intact. |
| 3. Art-to-play round trip | Embed the validated PNG and the exact M4 browser download in a scratch copy of the same command. In both targets, observe the new ground art, relocated lookout, shortcut crossing, arrival, and reset. Preserve the original wall-detour smoke for the unedited level. Document how to repeat this with a developer's own PNG. |

Measure PNG decode, packing, and upload separately from steady-state drawing. Compare ground drawing with and without tile art on the 9 × 7 courtyard and a dense 128 × 128 board using the same camera and viewport. Record allocation counts, submitted vertices and bytes, frame timing percentiles, and dropped ticks on desktop and web. Require no additional steady-state allocation after frame buffers are warm; use the measurements to decide whether viewport culling is needed, not a machine-specific CI timer.

## Acceptance results

`make smoke-level-roundtrip` passed on native macOS OpenGL and Chromium WebGL. The check validated an original purple [test tile](../testdata/custom-ground.png) through `tilecheck`, reopened the exact browser level download, and validated it through `levelcheck`. It embedded both files in a scratch copy of the reference command. Rendered frames showed the new tile under the grid at both zoom levels, the moved lookout, the scout crossing the opening, arrival, and reset. The ordinary native and Chromium smokes still passed with the reference art and the original wall detour.

The [ground-art measurements](../docs/ground-art-performance.md#results) report decode, atlas packing, upload, steady-state allocations, and batch size on both targets. No measured graphical run dropped a simulation tick. The dense board submitted nearly 2 MB of vertices per frame without culling. Viewport culling is not needed for the courtyard but is worth measuring before using this path for larger, busier maps.

## Out of scope

A Tile Maker or Pixel Art Maker UI, project manifests, external level references, multiple terrain textures per cell, atlas serialization, dynamic texture loading, hot reload, animation, autotiling, overhangs, occlusion masks, and changes to collision or pathfinding.
