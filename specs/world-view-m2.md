# M2 world-view specification

**Status:** Accepted for M2. The headless world view, host mouse input, and [reference courtyard](../docs/playground.md) are implemented. The [M2 performance report](../docs/world-view-performance.md) records the native and Chromium measurements and comparison against M1. See the [delivery plan](../rfcs/0003-m2-camera-depth-and-picking.md) and the [picking decision](../decisions/0003-tick-based-world-picking.md) for rationale. The [M1 runtime specification](runtime-m1.md) still governs the host and runtime foundation.

## Coordinates and camera

- Ground position is measured in tiles. The isometric projection yields unzoomed projected pixels; positive elevation, also in unzoomed pixels, moves a sprite up without changing its ground anchor. The orthographic camera converts the projected point to logical viewport pixels: subtract the camera center in projected pixels, multiply by a positive uniform zoom, and add the logical viewport center. Preserve fractional coordinates through the transform.
- Inverting the camera removes zoom and pan. Inverting the isometric projection requires a chosen elevation plane; a ground query uses elevation zero and cannot infer elevation from a screen point. Picking sprites uses their projected hit bounds instead of guessing an elevation plane.
- Hosts convert window or CSS positions through framebuffer scaling and the actual letterboxed viewport into logical pixels, including HiDPI and browser device pixel ratio. Use the same viewport and origin convention as presentation. Letterbox bars and the right and bottom viewport edges are outside. Resize does not mutate the scene camera or selection.

## Depth and hit geometry

- Scene code owns stable object IDs and ground anchors. Engine presentation items do not reorder simulation storage. Within the world layer, smaller `groundX + groundY` draws first; lower numeric ID draws first on ties. IDs are unique within the list, and coordinates and zoom must be finite. Texture, elevation, camera pan, and camera zoom do not affect this order. Ground decoration and screen UI stay outside the world sort. Hosts preserve paint order when batching.
- World items can carry a projected hit rectangle anchored to the same projected sprite position as drawing. Rectangles include left and top and exclude right and bottom. Scan the world list in reverse paint order. A selectable hit returns the object, a blocker ends the query without a selection, and a pass-through item does not stop the scan. Hit rectangles are authored interaction geometry, not texture-alpha tests.

## Input and tick order

- Buffer the first valid primary mouse-button press at its event position until the next fixed update. Movement and release do not relocate or erase it; another press before consumption does not replace it. A press outside the viewport is ignored. Consume the buffered press once during catch-up; focus loss clears it.
- Pick against the world and camera at the **start of the consuming tick**, before applying that tick's movement or camera changes. Picking and drawing use the same presentation items and ordering for a given state. Later catch-up ticks can advance the world before it is drawn; picking does not refer to an earlier displayed frame. Drawing neither mutates gameplay state nor builds a separate order.
- In the reference courtyard, clicking a blocker or empty world clears selection; another unit can be selected where its hit rectangle is not blocked. Restart takes precedence over picking and resets the camera, units, and selection. A resize or click in a letterbox bar does not clear selection.

## Verification

- Headless tests cover fractional camera round-trips, the chosen elevation plane, depth ties independent of list input order, and select, block, and pass-through behavior for overlapping items. They distinguish picking at tick start from picking after object or camera movement.
- Host tests cover asymmetric letterboxing, HiDPI, viewport edges, event-position retention, focus loss, and consumption across frames with zero or multiple ticks.
- Native and Chromium smoke checks verify actual overlap and selection pixels, pan, zoom, letterboxed resize, and reset. Chromium uses real pointer events. The native framebuffer fixture can use scripted input snapshots; test native pointer mapping separately. The M1 pickup smoke scenario still passes.
- Once buffers hold the measured workload, the order and hit-query paths perform no steady-state allocation. Measure the courtyard and a dense fixture starting at 2,048 world items, report ordering and hit-query costs, tick and frame percentiles, allocations, and dropped ticks, and compare the unchanged M1 workload with its [baseline](../docs/runtime-baseline.md).
