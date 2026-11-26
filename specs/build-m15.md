# Goje build and package commands, M15 and M17

`cmd/goje` turns a Goje main package into files for players: an installable web directory that works offline, a desktop executable for the host system, and release archives with checksums and an optional signature. [RFC 0016](../rfcs/0016-m15-audio-mixing-vision-and-platform-reach.md) records why the commands exist and what they leave out, and [RFC 0018](../rfcs/0018-m17-versus-music-tools-and-reach.md) adds Windows, the offline web app, and signing. The [update specification](update-m17.md) defines the signed files and how a game verifies them. The [building guide](../docs/building.md) explains how to use them.

## Commands and flags

```text
goje build   [-o DIR] [-name NAME] [-version V] [-id BUNDLE_ID] [-tags TAGS] [-icon PNG] [-target web|desktop|all] [PACKAGE]
goje package [the same flags] [-sign KEYFILE] [PACKAGE]
goje keygen  [-o PREFIX]
```

| Flag | Default | Rule |
| --- | --- | --- |
| `-o` | `dist` | The output directory. It must not be empty. |
| `-name` | The last element of the package's import path | It matches `[a-z][a-z0-9-]*` and has at most 32 bytes. A default that does not match is an error that asks for `-name`. |
| `-version` | `0.0.0` | Three decimal numbers without leading zeros, each with at most 9 digits, separated by periods, optionally followed by `-` and 1 to 32 letters, digits, periods, or hyphens. A leading `v` and build metadata after `+` are invalid. |
| `-id` | `com.example.NAME` | The macOS bundle identifier: at least two period-separated parts of letters, digits, and hyphens. |
| `-tags` | None | Comma-separated Go build tags passed to every `go` command. Spaces are invalid. |
| `-icon` | The Goje brand icon | A complete PNG of exactly 512 × 512 pixels. Anything else is an error that names the problem. |
| `-target` | `all` | `web`, `desktop`, or `all`. |
| `-sign` | None | `package` only; with `build` it is an argument error. A PEM file holding a PKCS #8 Ed25519 private key, as `goje keygen` writes it. The key is read before anything is built, and a missing, malformed, or non-Ed25519 key is an error. |

`keygen` writes a new Ed25519 key pair: `PREFIX.key`, the PKCS #8 private key in a PEM `PRIVATE KEY` block with mode 0600, and `PREFIX.pub`, the PKIX public key in a PEM `PUBLIC KEY` block with mode 0644. `PREFIX` defaults to `goje-signing` and must not be empty. It never replaces a file: if either file exists it is an error, and a private key whose public key could not be written is removed.

`PACKAGE` is one package path or pattern as the `go` command accepts it, and defaults to `.`. It must name exactly one package, and the package must be `main`. If the package directory contains `goje.json`, it must pass [`project.Load`](project-v1.md) over an `os.Root` of that directory, as `projectcheck` checks it. Argument errors exit with status 2 and print the usage; other errors exit with status 1.

The `go` command found on `PATH` does the work, so its toolchain selection decides which Go version builds the game, and it refuses a module whose `go` line it cannot satisfy.

## Build output

Every Go build uses `go build -trimpath -buildvcs=false -ldflags=-s -w`, with `-H=windowsgui` added to the linker flags on Windows, so that the same sources, toolchain, and tags give the same bytes. `build` replaces `DIR/web` and `DIR/desktop/GOOS-GOARCH` before it writes them, so no stale file survives.

`DIR/web/`, built with `GOOS=js GOARCH=wasm`:

| File | Content |
| --- | --- |
| `index.html` | The generic page described below, with the name as its title. |
| `game.wasm` | The game. |
| `wasm_exec.js` | A copy of `$(go env GOROOT)/lib/wasm/wasm_exec.js`, which matches the toolchain that built `game.wasm`. |
| `manifest.webmanifest` | JSON with `name` and `short_name` set to the name, `start_url` and `scope` `./`, `display` `fullscreen`, `background_color` and `theme_color` `#141c29`, and two icon entries for `icon-512.png`, size `512x512`, with `purpose` `any` and `maskable`. |
| `icon-512.png` | The icon. A maskable icon keeps its content inside the central circle whose radius is 40% of the size; the default icon does. |
| `sw.js` | The service worker described below. |

The page has one canvas with the ID `goje` and `tabindex="0"`, which fills the page inside a container with safe-area padding and `touch-action: none`. Its viewport is `width=device-width, initial-scale=1, viewport-fit=cover, user-scalable=no`. Its script is dependency-free and under 4 KiB. It registers `sw.js` where the browser supports service workers, and only logs a warning if registration fails, for example outside a secure context. It starts the Go runtime once and shows a message if WebAssembly or WebGL 2 is missing, if `game.wasm` fails to download or instantiate, or if the game exits with an error. The page needs an HTTP server, because browsers do not fetch WebAssembly from `file:` URLs.

`sw.js` makes the directory an installable web app that works offline:

- The bundle hash is the SHA-256 digest, in lowercase hexadecimal, of the lines that `SHA256SUMS` would hold for every other file of `DIR/web/`, sorted by name. Any change to the bundle changes `sw.js`, so the browser installs the new worker.
- Its cache is named `goje SCOPE HASH`, where `SCOPE` is the registration scope URL. On install it fetches `./` and every other file of the directory into the cache, bypassing the HTTP cache. On activation it deletes every other cache whose name starts with `goje SCOPE `, and no other cache, so games at other paths of the same origin keep theirs.
- It answers each GET request from the cache, ignoring the query string, and falls back to the network for anything else. It does not take over open pages: a new version is used once every page of the old one is closed. Reloading a page does not count as closing it, so a player must close every tab or window of the game before the next one opens the new version. A page that was not yet controlled by the old worker, such as the very first visit, receives the new version on its next load.

`DIR/desktop/GOOS-GOARCH/`, built with cgo for the host:

| System | Content |
| --- | --- |
| macOS | `NAME.app/Contents/Info.plist`, `NAME.app/Contents/MacOS/NAME`, and `NAME.app/Contents/Resources/NAME.icns`. |
| Linux | `NAME`, the executable; `NAME.desktop`; and `NAME.png`, the icon. |
| Windows | `NAME.exe`, a GUI program, so Windows opens no console window beside it. It has no embedded icon. |

`Info.plist` is an XML property list with `CFBundleDevelopmentRegion` `en`, `CFBundleDisplayName`, `CFBundleName`, and `CFBundleExecutable` set to the name, `CFBundleIconFile` `NAME.icns`, `CFBundleIdentifier`, `CFBundleInfoDictionaryVersion` `6.0`, `CFBundlePackageType` `APPL`, `CFBundleShortVersionString` and `CFBundleVersion` set to the version without its pre-release suffix, because Apple accepts only numbers there, `LSApplicationCategoryType` `public.app-category.games`, `LSMinimumSystemVersion` `12.0`, and `NSHighResolutionCapable` true. The executable is built with the deployment target `12.0`, so its `LC_BUILD_VERSION` minimum matches the plist whichever macOS release builds it. The bundle is not signed beyond the ad hoc signature that the Go linker gives every Apple silicon executable.

`NAME.icns` is the bytes `icns`, the file length as a big-endian 32-bit integer, then one `ic09` chunk: the bytes `ic09`, the chunk length including its 8-byte header as a big-endian 32-bit integer, and the 512 × 512 PNG.

`NAME.desktop` is a freedesktop.org desktop entry:

```ini
[Desktop Entry]
Type=Application
Name=NAME
Exec=NAME
Icon=NAME
Categories=Game;
Terminal=false
```

`Exec` and `Icon` name the executable and icon without a directory, so the entry works after the player installs them as the building guide describes.

## Targets and systems

`-target web` builds on any system that runs Go. `-target desktop` builds only for the host system, because the desktop host uses cgo for GLFW, OpenGL, and miniaudio, and only on macOS, Linux, and Windows, which the desktop host supports. Windows needs a cgo C compiler such as MinGW-w64 `gcc` on `PATH`. On another host it is an error. If `GOOS` or `GOARCH`, from the environment or `go env -w`, differs from the host's `GOHOSTOS` or `GOHOSTARCH`, a desktop build is an error that says so. `-target all` builds both, or only the web on a host without a desktop build, and says so on standard error.

The Windows x64 job in the [check workflow](../.github/workflows/check.yml) tests the build and package logic, packages Beacon twice, checks the archives and the GUI subsystem of the executable, then requires the extracted game to open a visible window with Mesa software OpenGL.

## Packages

`package` runs `build` with the same flags and then replaces `DIR/release/` with:

| File | Content |
| --- | --- |
| `NAME-VERSION-web.zip` | Every file of `DIR/web/`, at the archive root. |
| `NAME-VERSION-GOOS-GOARCH.tar.gz` | Every directory and file of `DIR/desktop/GOOS-GOARCH/`, at the archive root, if a desktop build ran on macOS or Linux. |
| `NAME-VERSION-windows-GOARCH.zip` | Every file of `DIR/desktop/windows-GOARCH/`, at the archive root, if a desktop build ran on Windows. |
| `SHA256SUMS` | One line per archive, sorted by file name: 64 lowercase hexadecimal digits of the SHA-256 digest, two spaces, and the file name. `sha256sum -c` and `shasum -a 256 -c` read it. |
| `SHA256SUMS.sig` | With `-sign`: the 64-byte Ed25519 signature of the exact bytes of `SHA256SUMS`. |
| `update.json` | With `-sign`: the signed manifest of the [update specification](update-m17.md), naming the game, the version, and the size and digest of each archive. |

Archives are byte-for-byte reproducible from the same build output:

- Entries are in depth-first order, with the entries of each directory sorted by name in byte order, so a directory precedes its contents. Zip archives hold only files; tar archives also hold directories, whose names end in `/`.
- Every entry has the same modification time: `SOURCE_DATE_EPOCH` in UTC if it is set to a non-negative integer, otherwise 2020-01-01 00:00:00 UTC. Another value of `SOURCE_DATE_EPOCH` is an error.
- Directories and files with any execute bit have mode 0755, and other files 0644. Windows file systems have no execute bits, so every file packaged on Windows, including `game.wasm`, has mode 0644, and its web archive differs from one packaged on macOS or Linux in that mode. Tar entries have user and group ID 0 and no user or group names, and need no PAX records.
- Zip entries use Deflate at the best compression level and record the Unix mode. The gzip stream uses the best compression level and has no name or time in its header.
- Symbolic links and other special files in the build output are errors.

Ed25519 signatures are deterministic, so signing the same archives with the same key also gives the same `SHA256SUMS.sig` and `update.json`.

## Limits

- Desktop builds are for the host system only. The web bundle is the only Goje distribution target for phones.
- The [hosted channel](update-m18.md) does not install or run downloads. There is no Authenticode, macOS notarization, or installer.
- The Windows executable has no embedded icon or version resource.
- Reproducibility holds for the same toolchain, sources, tags, and, for desktop builds, the same C compiler and system libraries.
