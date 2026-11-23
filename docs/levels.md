# Author a Goje level

## Edit a level in the browser

Run `make serve-level-editor` with Go and Node installed, then open `http://127.0.0.1:8081/`. Use `EDITOR_PORT` to change the port. The server serves the application on loopback; the selected level stays in the browser.

1. Choose **Open level** and select the [authored courtyard](../cmd/playground/levels/courtyard.json).
2. Select **Floor** and paint cell `(4, 2)` to cut a hole in the wall. Click or drag to paint. To edit with the keyboard, focus the grid, move with the arrow keys, and press **Enter** or **Space**.
3. To move the lookout, select the placement tool, then activate `(7, 4)` and a free floor such as `(7, 5)`. Use **Undo** or **Redo** to step through completed edits. Press **Escape** to cancel a stroke or placement selection in progress.
4. Choose **Export** to download a validated copy. The editor does not overwrite the open file. You can reopen the download or pass it to `go run ./cmd/levelcheck` before playing it.

Run `make setup-web` once to install the test browser, then `make smoke-level-editor` to exercise local file selection, editing, and export in Chromium.

The smoke test reports edit and export timings separately for a 128 × 128 level with 1,024 placements. One run on an Apple M4 Pro with Go 1.27.4 and Chromium 159 measured 59 ms for a 41-cell stroke and 33 ms from clicking **Export** to the download event. The stroke timing includes Playwright input and two animation frames. These are browser workflow measurements, not Go CPU timings or performance thresholds.

## Validate a level from the command line

A Goje v1 level is a JSON document with walkable cells, walls, and named placements. Edit the [authored courtyard](../cmd/playground/levels/courtyard.json) in a text editor, then validate it from the repository root:

```sh
go run ./cmd/levelcheck cmd/playground/levels/courtyard.json
```

Replace a `#` with `.` in one of the courtyard rows to open that ground cell. `levelcheck` reports the board size and placement count, or points to a malformed field or invalid cell. The reference game needs one `scout` and one `lookout`; the validator checks the same rule and never rewrites the file.

Other games can use `level.Decode` with a reader they supply. The decoder validates geometry and identifier syntax; the game must decide which placement kinds it can instantiate. See the [v1 format specification](../specs/level-v1.md) for coordinates, bounds, and compatibility.

Tools that already hold terrain rows and placements can call `level.Encode`. It validates the same v1 constraints as `Decode`, preserves row and placement order, and returns deterministic JSON bytes. It does not open or overwrite a file. The [Level Builder RFC](../rfcs/0005-m4-level-builder.md) describes the browser editor workflow.

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

Export the edited courtyard from the Level Builder. The playground embeds its level at build time, so test the download in a scratch copy of the command rather than replacing the repository fixture. From the repository root, set `download` to the actual path:

```sh
download="$HOME/Downloads/courtyard-edited.json"
go run ./cmd/levelcheck "$download"

mkdir -p .build
work=$(mktemp -d "$PWD/.build/play-edited.XXXXXX")
mkdir -p "$work/playground/levels" "$work/web"
cp cmd/playground/*.go "$work/playground/"
cp -R cmd/playground/art "$work/playground/"
cp "$download" "$work/playground/levels/courtyard.json"
```

To try your own ground art in the same build, validate and copy it before launching either target:

```sh
tile="$HOME/Downloads/my-ground.png"
go run ./cmd/tilecheck "$tile"
cp "$tile" "$work/playground/art/ground.png"
```

The PNG changes how walkable cells look, not which cells are walkable. See [ground art requirements](playground.md#validate-custom-ground-art).

- **Desktop:** Run `go run "$work/playground"`.
- **Browser:** Build the Wasm bundle and serve it on loopback:

  ```sh
  GOOS=js GOARCH=wasm go build -o "$work/web/playground.wasm" "$work/playground"
  cp web/playground/index.html "$(go env GOROOT)/lib/wasm/wasm_exec.js" "$work/web/"
  node scripts/playground-server.mjs 8080 "$work/web"
  ```

  Open `http://127.0.0.1:8080/`. The browser build does not need to load the JSON separately.

Select the scout and right-click the center of ground cell `(6, 2)`, two columns beyond the opening. The scout crosses the opening without going around the wall. Press **R** to restore the scout's spawn and the lookout's edited position. Stop the app before removing the scratch directory.

To automate the workflow, run `make setup-web` once and then `make smoke-level-roundtrip`. The test validates a [custom PNG](../testdata/custom-ground.png) with `tilecheck`, reopens and validates the browser download with `levelcheck`, and checks the custom ground pixels, lookout, shortcut crossing, arrival, and reset on both targets. It requires a working native graphics session; on headless Linux with the native smoke dependencies installed, use `xvfb-run -a make smoke-level-roundtrip`.
