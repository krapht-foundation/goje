# Run the Goje Playground

This repository includes a reference playground built on Goje's public scene runtime and graphical hosts. It is both a runnable example and a smoke fixture for desktop and web. You do not need to ship this command with your game.

The playground opens on a paused isometric courtyard so you can repeat the same clicks. Click the center of the cyan unit to select it. Click the exposed wall just above it to see the wall block selection of the amber unit behind it. Click the amber unit's exposed left edge to select that unit instead. A yellow bar marks the selected unit.

Use the arrow keys to pan the camera and **W** and **S** to zoom in and out around the view center. **Space** starts or pauses the patrol; both units pass in front of and behind the wall. **R** restores the original overlap, camera, and empty selection. In the browser, click the canvas if it does not have keyboard focus.

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

## Use the playground in your game

Implement `engine.Scene` and pass a factory to `platform.Run`. Let the factory create fresh encounter state, and keep reusable assets such as the marker image in the host configuration. Read input in `Update` and return `engine.Restart()` to re-enter the same factory without reopening the host or uploading the image again. See the [runtime guide](runtime.md) for the lifecycle and headless testing API.

The [reference command](../cmd/playground/) uses the same Go source for desktop and web. To try it in another module before a Goje release, run these commands from this repository:

```sh
goje_root="$PWD"
go_version="$(go list -m -f '{{.GoVersion}}')"
mkdir ../my-playground
cp cmd/playground/main.go cmd/playground/game.go cmd/playground/courtyard.go ../my-playground/
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

`make check` does not rewrite source. Use `make fmt` to apply formatting. The native smoke check scripts input snapshots and reads the OpenGL back buffer for both scenes. The browser smoke check uses Playwright Chromium, real keyboard and mouse input, and canvas screenshots. It checks pickup and focus loss, as well as courtyard overlap, wall blocking, selection, pan, zoom, letterboxed resize, patrol, and reset. `make check-example` builds the command from an independent module for the host and for WebAssembly.

The native smoke fixture is intended for macOS and Linux with X11, including Xvfb; the browser fixture uses Chromium with WebGL 2. Neither smoke check certifies GPU performance or every platform or browser. `make setup-web` installs its Chromium headless shell into `.build/playwright/`, separate from any user-wide cache. See the [playground specification](../specs/playground.md) for acceptance criteria.
