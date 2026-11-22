# Run the Goje Playground

The playground in this repository is a runnable test scene for Goje's desktop and web builds. It is not a tool for building a playground for your own game. That tooling is described in the [game-owned playground proposal](../rfcs/0001-game-owned-playgrounds.md).

The scene shows an isometric grid with an elevated marker and its ground shadow. Use the arrow keys or WASD to move the marker. In the browser, click the canvas first if the page does not have keyboard focus.

## Prerequisites

- Go at the version in [`go.mod`](../go.mod) or newer, and GNU Make.
- A graphical desktop for native runs. Linux requires an X11 or XWayland session and [Ebitengine's runtime libraries](https://ebitengine.org/en/documents/install.html#runtime-libraries). An Xvfb session with Mesa can run the desktop smoke check on Linux CI.
- Node.js 24.2 or newer and npm for serving and testing the browser build. Python 3 is used for the native smoke timeout and Markdown link check.

## Run

```sh
make run-desktop
```

For the browser, start `make serve-web` and open `http://127.0.0.1:8080/`. Set `PORT` to use a different port. `make build-web` alone generates a static site in `.build/web/`; serve `playground.wasm` as `application/wasm` and keep its generated `wasm_exec.js` alongside it.

## Verify

```sh
make setup-tools
make check
make test-race
make vuln
make smoke-desktop
make setup-web
make smoke-web
```

`make check` does not rewrite source. Use `make fmt` to apply formatting. The desktop smoke check opens a real window, applies scripted movement, and reads two rendered frames. The browser smoke check starts its own local server and uses Playwright Chromium to press a real arrow key, check the rendered movement, and verify the marker stays visible after resize. Neither check certifies GPU performance or every platform or browser. See the [playground spec](../specs/playground.md) for behavior and acceptance criteria.

If the default graphics backend cannot present a window on macOS, try `EBITENGINE_GRAPHICS_LIBRARY=opengl make run-desktop` or use the same override for `make smoke-desktop`. `make setup-web` installs its Chromium headless shell into `.build/playwright/`, separate from any user-wide cache.
