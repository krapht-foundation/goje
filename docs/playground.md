# Run the Goje Playground

This repository includes a reference playground built on Goje's public scene runtime and graphical hosts. It is both a runnable example and a smoke fixture for desktop and web. You do not need to ship this command with your game.

The playground opens the authored isometric courtyard. **Left-click** the amber scout to select it, then **right-click** a ground tile on the other side of the wall. The yellow bar shows selection; a green marker shows the accepted destination. The scout follows the shortest walkable route and the marker disappears on arrival. Walls and the cyan lookout block movement. An invalid command leaves the current route intact.

Use the arrow keys to pan and **W/S** to zoom around the view center. **R** restores the authored spawns, camera, and empty selection. In the browser, click the canvas to focus it. See the [level authoring guide](levels.md) to edit the courtyard and play the result.

## Prerequisites

- Go at the version in [`go.mod`](../go.mod) or newer, and GNU Make.
- CGO and a graphical desktop for native runs. On macOS, install the Xcode Command Line Tools. On Linux, install the X11 and OpenGL development libraries and use X11 or XWayland. See the [tested prerequisites](../rfcs/0002-graphics-feasibility.md#build-and-distribution-notes). An Xvfb session with Mesa can run the Linux desktop smoke check.
- Node.js 24.2 or newer and npm for serving and testing the browser build. Python 3 is used for the native smoke timeout and Markdown link check.

## Run

```sh
make run-desktop
```

For the browser, start `make serve-web` and open `http://127.0.0.1:8080/`. Set `PORT` to use a different port. `make build-web` alone generates a static site in `.build/web/`; serve `playground.wasm` as `application/wasm` and keep its generated `wasm_exec.js` alongside it.

The original pickup encounter remains available through `GOJE_PLAYGROUND=pickup make run-desktop` or `http://127.0.0.1:8080/?fixture=pickup`. Move with the arrow keys or **WASD** to collect the cyan token, then press **R** to reset.

The M2 patrol remains available through `GOJE_PLAYGROUND=courtyard make run-desktop` or `http://127.0.0.1:8080/?fixture=courtyard`. Click either visible unit to select it, **Space** to start or pause patrol, and **R** to restore the original overlap. Click the exposed wall to see it block selection of the unit behind it.

## Use the playground in your game

Implement `engine.Scene` and pass a factory to `platform.Run`. Let the factory create fresh encounter state, and keep reusable assets such as the marker image in the host configuration. Read input in `Update` and return `engine.Restart()` to re-enter the same factory without reopening the host or uploading the image again. See the [runtime guide](runtime.md) for the lifecycle and headless testing API.

The [reference command](../cmd/playground/) uses the same Go source for desktop and web. To try it in another module before a Goje release, run these commands from this repository:

```sh
goje_root="$PWD"
go_version="$(go list -m -f '{{.GoVersion}}')"
mkdir ../my-playground
cp cmd/playground/main.go cmd/playground/game.go cmd/playground/courtyard.go \
  cmd/playground/authored.go ../my-playground/
cp -R cmd/playground/levels cmd/playground/art ../my-playground/
cd ../my-playground
export GOWORK=off
go mod init example.com/my-playground
go mod edit -go="$go_version" -require=github.com/krapht-foundation/goje@v0.0.0 \
  -replace=github.com/krapht-foundation/goje="$goje_root"
go mod tidy
go run .
```

The local `replace` selects the unreleased revision. When depending on a published Goje version, use the version in `go.mod` without the local replacement. The reference command imports only public packages.

To run that same module in a browser, build the Wasm binary and serve it alongside the reference page and the matching Go bootstrap:

```sh
mkdir -p web
GOOS=js GOARCH=wasm go build -o web/playground.wasm .
cp "$goje_root/web/playground/index.html" "$(go env GOROOT)/lib/wasm/wasm_exec.js" web/
python3 -m http.server 8080 --bind 127.0.0.1 --directory web
```

Open `http://127.0.0.1:8080/` and click the canvas if it does not have keyboard focus.

## Draw a ground tile in the browser

Run `make serve-tile-maker` with Go and Node installed, then open `http://127.0.0.1:8082/`. Set `TILE_MAKER_PORT` to use a different port. The app runs on loopback and does not upload your files.

1. Start with the blank tile or choose **Open PNG** to edit a 32 × 16 PNG up to 64 KiB.
2. Choose a color and use **Pencil** to paint opaque pixels or **Eraser** to clear them. Click or drag on the grid, or focus it and use the arrow keys and **Enter** or **Space**. **Undo** and **Redo** operate on whole strokes or individual keyboard edits.
3. Use the native-size tile and the isometric repeat to check the result. The optional diamond guide helps place transparent corners without limiting where you can draw.
4. Choose **Export PNG** to download a copy. The editor never overwrites the original and preserves intermediate alpha on untouched pixels. Reopen the download or validate it for the playground using the instructions below.

Run `make setup-web` once, then `make smoke-tile-maker` to exercise the controls and check the downloaded PNG in Chromium. The smoke reports stroke time (including browser input and two animation frames) and export time (from initiating the click to the download event), not Go CPU time.

## Validate custom ground art

Export a 32 × 16 PNG, at most 64 KiB, with transparent corners around the isometric diamond. From the repository root, run `go run ./cmd/tilecheck /path/to/tile.png`. This reads the file without rewriting it. To see the result, replace `art/ground.png` in a scratch copy of the reference command and rebuild as shown in [Play a level edit](levels.md#play-a-level-edit). The authored courtyard validates the tile at startup and draws it beneath the grid on walkable cells only. Pickup and patrol do not use the tile.

The [reference ground tile](../cmd/playground/art/ground.png) is original Goje artwork generated for this project. Its muted diamond, transparent corners, and offset highlight distinguish orientation without external art or dependencies. See the [M5 RFC](../rfcs/0006-m5-tile-art.md) for the milestone plan.

## Capture a local run

Run `GOJE_DIAGNOSTICS=1 make run-desktop` on desktop, or start `make serve-web` and open `http://127.0.0.1:8080/?diagnostics=1`. Move around and press **R** to see a scene transition in the lifecycle log. After ten seconds (or earlier if you close the native window), the run stops and prints the JSON report to the terminal or the browser developer console. The demo retains up to 512 phase records; the report still counts ticks and frames after the trace fills.

## Verify

```sh
make setup-tools
make check
make test-race
make vuln
make smoke-desktop
make check-example
make setup-web
make smoke-web
```

`make check` does not rewrite source. Use `make fmt` to apply formatting. The native smoke check scripts input snapshots and reads the OpenGL back buffer. The Chromium check uses real keyboard and mouse input and canvas screenshots. Both check the authored scout's selection, detour, arrival, and reset alongside the earlier patrol and pickup fixtures. `make check-example` builds the command from an independent module for the host and for WebAssembly.

The native smoke fixture is intended for macOS and Linux with X11, including Xvfb; the browser fixture uses Chromium with WebGL 2. Neither smoke check certifies GPU performance or every platform or browser. `make setup-web` installs its Chromium headless shell into `.build/playwright/`, separate from any user-wide cache. See the [playground specification](../specs/playground.md) for acceptance criteria.
