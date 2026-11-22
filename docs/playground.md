# Run the Goje Playground

This repository includes a reference playground built with Goje's experimental, game-owned playground API. It is both a runnable example and a smoke fixture for desktop and web. You do not need to ship this command with your game.

The scene shows an isometric grid, an elevated player marker, and a cyan token. Hold the right arrow key or **D** to move through the token and collect it. Press **R** to restore the player and token to their starting positions without restarting. In the browser, click the canvas first if the page does not have keyboard focus.

## Prerequisites

- Go at the version in [`go.mod`](../go.mod) or newer, and GNU Make.
- A graphical desktop for native runs. Linux requires an X11 or XWayland session and [Ebitengine's runtime libraries](https://ebitengine.org/en/documents/install.html#runtime-libraries). An Xvfb session with Mesa can run the desktop smoke check on Linux CI.
- Node.js 24.2 or newer and npm for serving and testing the browser build. Python 3 is used for the native smoke timeout and Markdown link check.

## Run

```sh
make run-desktop
```

For the browser, start `make serve-web` and open `http://127.0.0.1:8080/`. Set `PORT` to use a different port. `make build-web` alone generates a static site in `.build/web/`; serve `playground.wasm` as `application/wasm` and keep its generated `wasm_exec.js` alongside it.

## Use the playground in your game

Pass your game's scene loader to `goje.NewPlayground`. Configure the starting encounter in that loader, and have it create fresh mutable state each time it runs. Keep reusable assets outside the scene. In your normal game loop, use `Scene()` to update and render the current scene, and call `Reset()` when the player requests a restart. Get the current scene again after resetting instead of retaining references from the previous run.

See the [reference command](../cmd/playground/) for a working integration. It uses Ebitengine for input, drawing, and the game loop; those are not dependencies of the playground API. To try the reference in another module before a Goje release, run the following from this repository:

```sh
goje_root="$PWD"
go_version="$(go list -m -f '{{.GoVersion}}')"
mkdir ../my-playground
cp cmd/playground/*.go ../my-playground/
cd ../my-playground
export GOWORK=off
go mod init example.com/my-playground
go mod edit -go="$go_version" -require=github.com/krapht-foundation/goje@v0.0.0 \
  -replace=github.com/krapht-foundation/goje="$goje_root"
go mod tidy
go run .
```

The local `replace` selects the unreleased revision. When depending on a published Goje version, use the version in `go.mod` without the local replacement. The reference command imports only public packages.

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

`make check` does not rewrite source. Use `make fmt` to apply formatting. The native smoke check scripts input actions and reads rendered frames before pickup, after pickup, and after reset. The browser smoke check uses Playwright Chromium to check the same states with real keyboard input and also verifies that resizing preserves encounter state. `make check-example` copies the command to an independent module and builds it for the host and for WebAssembly. Neither smoke check certifies GPU performance or every platform or browser. See the [playground specification](../specs/playground.md) for acceptance criteria.

If the default graphics backend cannot present a window on macOS, try `EBITENGINE_GRAPHICS_LIBRARY=opengl make run-desktop` or use the same override for `make smoke-desktop`. `make setup-web` installs its Chromium headless shell into `.build/playwright/`, separate from any user-wide cache.
