# Goje build and package commands, M15

`cmd/goje` turns a Goje main package into files for players: a web directory, a desktop executable for the host system, and release archives with checksums. [RFC 0016](../rfcs/0016-m15-audio-mixing-vision-and-platform-reach.md) records why the commands exist and what they leave out. The [building guide](../docs/building.md) explains how to use them.

## Commands and flags

```text
goje build   [-o DIR] [-name NAME] [-version V] [-id BUNDLE_ID] [-tags TAGS] [-icon PNG] [-target web|desktop|all] [PACKAGE]
goje package [the same flags and PACKAGE]
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

`PACKAGE` is one package path or pattern as the `go` command accepts it, and defaults to `.`. It must name exactly one package, and the package must be `main`. If the package directory contains `goje.json`, it must pass [`project.Load`](project-v1.md) over an `os.Root` of that directory, as `projectcheck` checks it. Argument errors exit with status 2 and print the usage; other errors exit with status 1.

The `go` command found on `PATH` does the work, so its toolchain selection decides which Go version builds the game, and it refuses a module whose `go` line it cannot satisfy.

## Build output

Every Go build uses `go build -trimpath -buildvcs=false -ldflags=-s -w`, so that the same sources, toolchain, and tags give the same bytes. `build` replaces `DIR/web` and `DIR/desktop/GOOS-GOARCH` before it writes them, so no stale file survives.

`DIR/web/`, built with `GOOS=js GOARCH=wasm`:

| File | Content |
| --- | --- |
| `index.html` | The generic page described below, with the name as its title. |
| `game.wasm` | The game. |
| `wasm_exec.js` | A copy of `$(go env GOROOT)/lib/wasm/wasm_exec.js`, which matches the toolchain that built `game.wasm`. |
| `manifest.webmanifest` | JSON with `name` and `short_name` set to the name, `start_url` `./`, `display` `fullscreen`, `background_color` and `theme_color` `#141c29`, and one icon entry for `icon-512.png`. |
| `icon-512.png` | The icon. |

The page has one canvas with the ID `goje` and `tabindex="0"`, which fills the page inside a container with safe-area padding and `touch-action: none`. Its viewport is `width=device-width, initial-scale=1, viewport-fit=cover, user-scalable=no`. Its script is dependency-free and under 4 KiB. It starts the Go runtime once and shows a message if WebAssembly or WebGL 2 is missing, if `game.wasm` fails to download or instantiate, or if the game exits with an error. The page needs an HTTP server, because browsers do not fetch WebAssembly from `file:` URLs.

`DIR/desktop/GOOS-GOARCH/`, built with cgo for the host:

| System | Content |
| --- | --- |
| macOS | `NAME.app/Contents/Info.plist`, `NAME.app/Contents/MacOS/NAME`, and `NAME.app/Contents/Resources/NAME.icns`. |
| Linux | `NAME`, the executable; `NAME.desktop`; and `NAME.png`, the icon. |

`Info.plist` is an XML property list with `CFBundleDevelopmentRegion` `en`, `CFBundleDisplayName`, `CFBundleName`, and `CFBundleExecutable` set to the name, `CFBundleIconFile` `NAME.icns`, `CFBundleIdentifier`, `CFBundleInfoDictionaryVersion` `6.0`, `CFBundlePackageType` `APPL`, `CFBundleShortVersionString` and `CFBundleVersion` set to the version without its pre-release suffix, because Apple accepts only numbers there, `LSApplicationCategoryType` `public.app-category.games`, `LSMinimumSystemVersion` `12.0`, and `NSHighResolutionCapable` true. The bundle is not signed beyond the ad hoc signature that the Go linker gives every Apple silicon executable.

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

`-target web` builds on any system that runs Go. `-target desktop` builds only for the host system, because the desktop host uses cgo for GLFW, OpenGL, and miniaudio, and only on macOS and Linux, which the desktop host supports. On another host it is an error. If `GOOS` or `GOARCH`, from the environment or `go env -w`, differs from the host's `GOHOSTOS` or `GOHOSTARCH`, a desktop build is an error that says so. `-target all` builds both, or only the web on a host without a desktop build, and says so on standard error.

## Packages

`package` runs `build` with the same flags and then replaces `DIR/release/` with:

| File | Content |
| --- | --- |
| `NAME-VERSION-web.zip` | Every file of `DIR/web/`, at the archive root. |
| `NAME-VERSION-GOOS-GOARCH.tar.gz` | Every directory and file of `DIR/desktop/GOOS-GOARCH/`, at the archive root, if a desktop build ran. |
| `SHA256SUMS` | One line per archive, sorted by file name: 64 lowercase hexadecimal digits of the SHA-256 digest, two spaces, and the file name. `sha256sum -c` and `shasum -a 256 -c` read it. |

Archives are byte-for-byte reproducible from the same build output:

- Entries are in depth-first order, with the entries of each directory sorted by name in byte order, so a directory precedes its contents. Zip archives hold only files; tar archives also hold directories, whose names end in `/`.
- Every entry has the same modification time: `SOURCE_DATE_EPOCH` in UTC if it is set to a non-negative integer, otherwise 2020-01-01 00:00:00 UTC. Another value of `SOURCE_DATE_EPOCH` is an error.
- Directories and files with any execute bit have mode 0755, and other files 0644. Tar entries have user and group ID 0 and no user or group names, and need no PAX records.
- Zip entries use Deflate at the best compression level and record the Unix mode. The gzip stream uses the best compression level and has no name or time in its header.
- Symbolic links and other special files in the build output are errors.

## Limits

- Desktop builds are for the host system only. There are no Windows, Android, or iOS outputs.
- Nothing is signed or notarized, and there are no installers, update channels, or app store files.
- The web output has no service worker, so it does not work offline.
- Reproducibility holds for the same toolchain, sources, tags, and, for desktop builds, the same C compiler and system libraries.
