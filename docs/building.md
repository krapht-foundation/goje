# Build and package a game

`goje build` turns your game's main package into a web directory and a desktop application for the system you build on. `goje package` also writes archives and a checksum file that you can hand to players or attach to a release. The [build specification](../specs/build-m15.md) states every rule exactly.

## Build

Run the command from your game's module, which requires Goje, so that the tool and the engine come from the same version:

```sh
go run github.com/krapht-foundation/goje/cmd/goje build -name my-game -version 1.0.0
```

The package is the current directory unless you name another one, such as `./cmd/my-game`. Useful flags:

- `-o DIR` chooses the output directory, which is `dist` by default.
- `-name` sets the file, bundle, and page name. It defaults to the last element of the package's import path and must use lowercase letters, digits, and hyphens.
- `-version` sets the version, such as `1.2.0` or `1.2.0-beta.1`. It defaults to `0.0.0`.
- `-icon icon.png` uses your 512 × 512 PNG instead of the Goje tomato.
- `-id com.yourstudio.my-game` sets the macOS bundle identifier. Set it before you publish, because the default, `com.example.NAME`, belongs to nobody.
- `-target web` or `-target desktop` builds only one of the two.
- `-tags` passes Go build tags, for example to build a smoke-test variant.

If the package directory has a `goje.json`, the command checks it as `projectcheck` does before it builds.

## What you get

`dist/web/` is a complete static site: `index.html`, `game.wasm`, `wasm_exec.js`, `manifest.webmanifest`, and `icon-512.png`. Upload the directory to any static web host, or serve it locally with any HTTP server, such as `python3 -m http.server -d dist/web`. Opening `index.html` as a file does not work, because browsers do not load WebAssembly from `file:` URLs. Players open the page in a browser with WebGL 2. The canvas fills the window, and the page shows a message if the browser cannot run the game. On a phone, players can add the page to the home screen to start it full-screen.

`dist/desktop/darwin-arm64/NAME.app` on a Mac is an application bundle. Players drag it into Applications and open it. It is not signed or notarized, so macOS refuses to open it the first time. Players then open **System Settings → Privacy & Security** and choose **Open Anyway**, once. If the app came from a downloaded archive, they can instead run `xattr -dr com.apple.quarantine NAME.app` in Terminal.

`dist/desktop/linux-amd64/` on Linux holds the executable `NAME`, a launcher `NAME.desktop`, and the icon `NAME.png`. Players can run `./NAME` directly. To add it to the desktop's application menu, they copy the executable into a directory on their `PATH`, such as `~/.local/bin`, the launcher into `~/.local/share/applications`, and the icon into `~/.local/share/icons/hicolor/512x512/apps`. The executable needs OpenGL 3.3 and the X11 libraries that GLFW uses.

## Package

```sh
go run github.com/krapht-foundation/goje/cmd/goje package -name my-game -version 1.0.0
```

`package` builds as above and then writes `dist/release/`:

- `my-game-1.0.0-web.zip`, the web directory.
- `my-game-1.0.0-darwin-arm64.tar.gz` or `my-game-1.0.0-linux-amd64.tar.gz`, the application bundle or the Linux files.
- `SHA256SUMS`, which players check with `sha256sum -c SHA256SUMS` on Linux or `shasum -a 256 -c SHA256SUMS` on macOS.

Packaging the same build twice gives identical files, and so does building the same sources again with the same Go and C toolchains. Set `SOURCE_DATE_EPOCH` to choose the time stored in the archives; it is 2020-01-01 otherwise.

To publish for both macOS and Linux, run `package` on each system. The repository's release workflow does this on GitHub-hosted runners for the playground and Beacon and drafts a release when a `v` tag is pushed; use it as a model for your own.

## Limits

- The desktop build is for the system you run the command on. It needs cgo and works on macOS and Linux only; elsewhere use `-target web`. Cross-compiling the desktop build is not supported.
- Nothing is signed or notarized, and there are no installers or automatic updates.
- There are no Windows, Android, or iOS builds. Phones and tablets use the web build.
- The web build has no service worker, so it does not work offline.
