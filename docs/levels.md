# Author a Goje level

A Goje v1 level is a JSON document with walkable cells, walls, and named placements. Edit the [authored courtyard](../cmd/playground/levels/courtyard.json) in a text editor, then validate it from the repository root:

```sh
go run ./cmd/levelcheck cmd/playground/levels/courtyard.json
```

Replace a `#` with `.` in one of the courtyard rows to open that ground cell. `levelcheck` reports the board size and placement count, or points to a malformed field or invalid cell. The reference game needs one `scout` and one `lookout`; the validator checks the same rule and never rewrites the file.

Other games can use `level.Decode` with a reader they supply. The decoder validates geometry and identifier syntax; the game must decide which placement kinds it can instantiate. See the [v1 format specification](../specs/level-v1.md) for coordinates, bounds, and compatibility.

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

Open row `y=2` at column `x=4` (the middle cell of the wall), run `levelcheck`, and rebuild the [playground](playground.md) with `make run-desktop` or `make serve-web`. The file is embedded at build time on both hosts. Select the scout and right-click the center of the tile two cells beyond the wall, in the same ground row. The scout now crosses the new opening instead of detouring around the wall. Restore the `#` and rebuild to see the original detour again.
