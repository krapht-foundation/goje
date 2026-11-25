# Author a Goje level

## Edit a level in the browser

Run `make serve-level-editor` with Go and Node installed, then open `http://127.0.0.1:8081/`. Use `EDITOR_PORT` to change the port. The server serves the application on loopback; the selected level stays in the browser.

1. Choose **Open level** and select the [authored courtyard](../cmd/playground/levels/courtyard.json).
2. Select **Floor** and paint cell `(4, 2)` to cut a hole in the wall. Click or drag to paint. To edit with the keyboard, focus the grid, move with the arrow keys, and press **Enter** or **Space**.
3. To move the lookout, select **Move placement**, then activate `(7, 4)` and a free floor such as `(7, 5)`. Use **Undo** or **Redo** to step through completed edits. Press **Escape** to cancel a stroke or placement selection in progress.
4. Choose **Export** to download a validated copy. The editor does not overwrite the open file. You can reopen the download or pass it to `go run ./cmd/levelcheck` before playing it.

Run `make setup-web` once to install the test browser, then `make smoke-level-editor` to exercise local file selection, editing, and export in Chromium.

The smoke test reports edit and export timings separately for a 128 × 128 level with 1,024 placements. One run on an Apple M4 Pro with Go 1.27.4 and Chromium 159 measured 59 ms for a 41-cell stroke and 33 ms from clicking **Export** to the download event. The stroke timing includes Playwright input and two animation frames. These are browser workflow measurements, not Go CPU timings or performance thresholds.

## Draw zones

A zone is a tile-aligned rectangle with a game-defined kind, a role, and a vertical extent. A **solid** zone blocks bodies whose height overlaps its own, such as a raised deck that characters walk beneath. A **trigger** zone blocks nothing and reports when a body enters or leaves, such as a doorway or an alarm line. A level that has zones is a [version 2 level](../specs/level-v2.md). The Level Builder reads both versions, writes version 1 when a level has no zones, and keeps zones you do not touch.

1. Open a level, such as the [courtyard](../cmd/playground/levels/courtyard.json) or the [yard](../cmd/playground/levels/yard.json).
2. In the **Zones** panel, set the kind, the role, and the **Bottom** and **Top** heights in projected pixels. A deck that characters walk under might be `deck`, solid, 24 to 32. A doorway is `door`, trigger, 0 to 32.
3. Select **Draw zone**. Drag a rectangle on the grid, or click one corner and then the opposite corner. With the keyboard, press **Enter** or **Space** on one corner and again on the other. **Escape** cancels a pending corner. You can also type the column, row, width, and height and choose **Add zone**.
4. Choose a zone in the list to see its outline and values. Change the fields and choose **Apply to selected zone** to replace it, or **Delete selected zone** to remove it. Every add, apply, and delete is one action that **Undo** and **Redo** step through. A zone that leaves the board, has no cells, or has an empty height range is rejected with a message, and nothing changes.
5. Solids appear hatched with an `S` and the zone's ID, and triggers appear with a dotted border and a `T`, so the role does not depend on color.

To use zones in a game, call `collide.FromLevel` or `collide.LevelShapes`, then read `board.Zones()` to give each zone ID a meaning. See [Move and collide in your game](collision.md#build-a-world-from-a-level).

Run `make smoke-zone-roundtrip` to author the yard's deck and doorway in Chromium, export them, reopen the download, check it with `levelcheck`, and play it in scratch builds on desktop and web. It needs a working native graphics session; on headless Linux, use `xvfb-run -a make smoke-zone-roundtrip`.

Run `make smoke-placement-editor` to exercise adding, rejecting, changing, deleting, undoing, and exporting placements in Chromium. Run `make smoke-outpost-roundtrip` to strip the outpost level down to its terrain, add its twelve placements and two zones through the Level Builder's forms, export the result, and play it on desktop and web. It needs the same graphics session as the zone round trip.

## Set what units can see

A level can set how far units see and which cells block their sight. A level with a sight radius is a [version 3 level](../specs/level-v3.md); the Level Builder writes version 1 or 2, as before, when the radius is 0.

1. Open a level, such as the [versus level](../cmd/playground/levels/versus.json).
2. In the **Vision** panel, type the **Sight radius** in cells, from 1 to 181, and choose **Set sight radius**. Type 0 to remove it. Each change is one action that **Undo** and **Redo** step through, and the field follows them.
3. To add a hedge, choose **Sight block preset** in the **Zones** panel. It sets the kind to `sight_block`, the role to trigger, and the heights to 0 to 32, and keeps the column, row, width, and height. Then draw the zone with **Draw zone** or type its cells and choose **Add zone**. A sight block hides what is behind it but does not stop movement, so units can walk through a hedge.
4. Export the level. A `sight_block` zone whose role you changed to solid is reported and not written, because a solid already blocks movement.

A game reads the radius from `board.Sight()` and builds its blocked mask once from `board.SightBlocked(x, y)`, then adds its walls to that mask.

After `make setup-web` and `make build-level-editor`, run `PLAYWRIGHT_BROWSERS_PATH=.build/playwright node scripts/smoke-versus-editor.mjs /tmp/versus.json`. It strips the shipped versus level to its terrain, sets the radius, undoes and redoes it, adds the twelve units and both hedges through the forms, and checks that the export equals the shipped file byte for byte.

## Add and remove placements

A placement is a game-defined object, such as a unit, on one floor cell. Each has an ID that is unique in the level and a kind, such as `scout` or `guard`. A kind starts with a lowercase letter, continues with lowercase letters, digits, or underscores, and has at most 32 characters. The decoder checks only that a kind is well formed, so a game must reject the kinds it does not support.

1. Open a level, then set the kind in the **Placements** panel. Leave **ID** blank to use one more than the largest ID in the level, or type an unused ID from 1 to 4,294,967,295.
2. Select **Add placement** and click a free floor cell. With the keyboard, move the cursor and press **Enter** or **Space**. You can also type the column and row in **X** and **Y** and choose **Add at X, Y**. A typed ID applies to one addition only.
3. Choose a placement in the list to select it. Type a new kind and choose **Change kind of selected placement** to change it; its ID and cell stay the same. Choose **Delete selected placement**, or press **Delete** in the list or on the grid, to remove it.

Every addition, change of kind, and deletion is one action that **Undo** and **Redo** step through. A level holds at most 1,024 placements and one placement per cell. A wall cell, an occupied cell, a cell outside the board, a duplicate ID, or a malformed kind is rejected with a message, and nothing changes.

The courtyard still needs exactly one `scout` and one `lookout`, and `levelcheck` still checks that rule, so it rejects a courtyard with added units. Other games, such as the [skirmish](../cmd/playground/levels/skirmish.json), list as many scouts and guards as they need.

After `make setup-web`, run `make build-level-editor` and then `PLAYWRIGHT_BROWSERS_PATH=.build/playwright node scripts/smoke-placement-editor.mjs` to add scouts and guards with the pointer, the keyboard, and typed cells in Chromium, change a kind, delete a placement, undo and redo, and export and reopen the level.

## Validate a level from the command line

A Goje level is a JSON document with walkable cells, walls, named placements, from version 2, collision zones, and, from version 3, a sight radius. Edit the [authored courtyard](../cmd/playground/levels/courtyard.json) in a text editor, then validate it from the repository root:

```sh
go run ./cmd/levelcheck cmd/playground/levels/courtyard.json
```

Replace a `#` with `.` in one of the courtyard rows to open that ground cell. `levelcheck` reports the board size, placement count, and zone count, or points to a malformed field or invalid cell. The courtyard needs one `scout` and one `lookout`; the validator checks the same rule and never rewrites the file, so it rejects the skirmish level, which lists several scouts and guards.

Other games can use `level.Decode` with a reader they supply. The decoder validates geometry and identifier syntax; the game must decide which placement kinds it can instantiate. See the [v1](../specs/level-v1.md), [v2](../specs/level-v2.md), and [v3](../specs/level-v3.md) format specifications for coordinates, bounds, and compatibility.

Tools that already hold terrain rows, placements, and zones can call `level.Encode`. It validates the same constraints as `Decode`, preserves row, placement, and zone order, writes version 1 when there are no zones and version 2 otherwise, and returns deterministic JSON bytes. `level.EncodeSight` takes a sight radius as well and writes version 3 when it is not 0. Neither opens or overwrites a file. The [Level Builder RFC](../rfcs/0005-m4-level-builder.md) describes the browser editor workflow.

## Validate a project from the command line

Put a Level Builder export and a Tile Maker PNG in the same project directory. Name them in a [`goje.json` manifest](../specs/project-v1.md), then run:

```sh
go run ./cmd/projectcheck path/to/project
```

`projectcheck` validates the paths and both assets without rewriting them. Unlike `levelcheck`, it does not enforce the courtyard's placement rules. A Go game can call `project.Load` with a filesystem rooted at its `goje.json` and then validate the placement kinds it supports.

## Route a unit on the ground grid

Create an `engine/grid.Router` with the board dimensions and keep it for the life of the scene. Pass the ground coordinates from your inverse projection to `router.CellAt`; it rejects points outside the board but does not test for walls.

Call `router.AppendRoute` when a command arrives, not on every tick. Pass `board.Walkable` for terrain alone, or a predicate that also checks game-owned blockers such as a stationary unit. Routes exclude the start and include the destination. If no route exists, the call leaves the supplied slice unchanged. Keep a reusable route buffer with room for `width*height-1` cells to avoid allocations during commands. Your game still decides whether to replace an order and how to move the unit along the route.

### Measure routing cost

The headless benchmark searches a 128 × 128 board from the top-left cell to the opposite corner. In the reachable case the board is open and the route has 254 steps. In the unreachable case two walls isolate the destination, forcing the search to exhaust the connected area. Both cases reuse a router and a route buffer; timings exclude setup, projection, movement, and rendering.

```sh
GOMAXPROCS=1 go test ./engine/grid -run '^$' -bench '^BenchmarkRoute$' \
  -benchmem -benchtime=100ms -count=10
GOMAXPROCS=1 GOOS=js GOARCH=wasm go test \
  -exec="$(go env GOROOT)/lib/wasm/go_js_wasm_exec" ./engine/grid \
  -run '^$' -bench '^BenchmarkRoute$' -benchmem -benchtime=100ms -count=10
```

Median of ten runs per case, using Go 1.27.4 on an Apple M4 Pro with macOS 15.5 (Node 24.20.0 for Wasm):

| Target | Reachable | Unreachable |
| --- | ---: | ---: |
| Native arm64 | 80 µs | 82 µs |
| Wasm under Node | 302 µs | 321 µs |

Every run measured 0 B/op and 0 allocations per query after setup. These are CPU-side search timings, not browser input or frame latency.

### Measure moving frames

The authored courtyard's moving-frame benchmark alternates commands between the scout's spawn and the far side of the wall. It measures one fixed tick and the recording of one draw batch, including the occasional search on a new order. It does not submit work to a GPU. On the same machine, the median of ten runs was 0.53 µs native and 2.35 µs under Node Wasm, with zero allocations per frame after warmup.

```sh
GOMAXPROCS=1 go test -tags goje_bench ./cmd/playground -run '^$' \
  -bench '^BenchmarkAuthoredAdvance$' -benchmem -benchtime=100ms -count=10
GOMAXPROCS=1 GOOS=js GOARCH=wasm go test -tags goje_bench \
  -exec="$(go env GOROOT)/lib/wasm/go_js_wasm_exec" ./cmd/playground \
  -run '^$' -bench '^BenchmarkAuthoredAdvance$' -benchmem -benchtime=100ms -count=10
```

## Play a level edit

Export the edited courtyard from the Level Builder. The reference command embeds its project at build time, so use a scratch copy rather than replacing the repository fixture. From the repository root, set `download` to the actual path:

```sh
download="$HOME/Downloads/courtyard-edited.json"
go run ./cmd/levelcheck "$download"

mkdir -p .build
work=$(mktemp -d "$PWD/.build/play-edited.XXXXXX")
mkdir -p "$work/playground" "$work/web"
cp cmd/playground/*.go cmd/playground/goje.json "$work/playground/"
cp -R cmd/playground/levels cmd/playground/art "$work/playground/"
cp "$download" "$work/playground/levels/edited.json"
```

In the scratch copy of `goje.json`, set `level` to `levels/edited.json`. Keep `ground` as `art/ground.png` to use the original art, or [draw and export a tile in the Tile Maker](playground.md#draw-a-ground-tile-in-the-browser). Leave the corners transparent so the tiles fit the isometric grid. Set `tile` to the actual download path:

```sh
tile="$HOME/Downloads/tile.png"
go run ./cmd/tilecheck "$tile"
cp "$tile" "$work/playground/art/edited.png"
```

If you copied a custom tile, set `ground` to `art/edited.png` in the scratch manifest. The PNG changes how walkable cells look, not which cells are walkable. Validate the assembled project before launching either target:

```sh
go run ./cmd/projectcheck "$work/playground"
```

- **Desktop:** Run `go run "$work/playground"`.
- **Browser:** Build the Wasm bundle and serve it on loopback:

  ```sh
  GOOS=js GOARCH=wasm go build -o "$work/web/playground.wasm" "$work/playground"
  cp web/playground/index.html "$(go env GOROOT)/lib/wasm/wasm_exec.js" "$work/web/"
  node scripts/playground-server.mjs 8080 "$work/web"
  ```

  Open `http://127.0.0.1:8080/`. The browser build embeds the same project as the desktop build.

Select the scout and right-click the center of ground cell `(6, 2)`, two columns beyond the opening. The scout crosses the opening without going around the wall. Press **R** to restore the scout's spawn and the lookout's edited position. Stop the app before removing the scratch directory.

To automate the workflow, run `make setup-web` once and then `make smoke-level-roundtrip`. The smoke test authors and reopens both files in the editors, validates the exact downloads with `tilecheck` and `levelcheck`, and points the scratch manifest at their new paths while leaving the original fixtures in place. `projectcheck` validates the assembled project before both builds check the rendered accent, transparent corners, moved lookout, shortcut crossing, arrival, and reset. It requires a working native graphics session; on headless Linux with the native smoke dependencies installed, use `xvfb-run -a make smoke-level-roundtrip`.
