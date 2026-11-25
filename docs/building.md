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

`dist/web/` is a complete static site: `index.html`, `game.wasm`, `wasm_exec.js`, `manifest.webmanifest`, `icon-512.png`, and `sw.js`. Upload the directory to any static web host, or serve it locally with any HTTP server, such as `python3 -m http.server -d dist/web`. Opening `index.html` as a file does not work, because browsers do not load WebAssembly from `file:` URLs. Players open the page in a browser with WebGL 2. The canvas fills the window, and the page shows a message if the browser cannot run the game.

The web build is an installable web app. Served over HTTPS, or from `localhost`, the page registers the service worker `sw.js`, which stores every file of the game on the first visit, so the game starts again without a network. Players can install it from the browser's menu, or add it to the home screen on a phone, to start it full-screen like an app. When you upload a new build, browsers fetch the new files in the background and use them once the old version's pages are closed. The service worker caches only its own directory, so several games can share a site under different paths. The icon is also offered as a maskable icon, which Android crops to a circle or other shape, so keep a custom icon's subject inside the central 80% circle, as the default tomato does.

To publish to Google Play or the App Store, wrap the web app with a tool such as [Bubblewrap](https://github.com/GoogleChromeLabs/bubblewrap), which makes an Android Trusted Web Activity from the URL of your deployed `manifest.webmanifest`, or [Capacitor](https://capacitorjs.com/), which packages the files of `dist/web/` into Android and iOS projects. Goje does not generate or test those projects.

`dist/desktop/darwin-arm64/NAME.app` on a Mac is an application bundle. Players drag it into Applications and open it. It is not signed or notarized, so macOS refuses to open it the first time. Players then open **System Settings → Privacy & Security** and choose **Open Anyway**, once. If the app came from a downloaded archive, they can instead run `xattr -dr com.apple.quarantine NAME.app` in Terminal.

`dist/desktop/linux-amd64/` on Linux holds the executable `NAME`, a launcher `NAME.desktop`, and the icon `NAME.png`. Players can run `./NAME` directly. To add it to the desktop's application menu, they copy the executable into a directory on their `PATH`, such as `~/.local/bin`, the launcher into `~/.local/share/applications`, and the icon into `~/.local/share/icons/hicolor/512x512/apps`. The executable needs OpenGL 3.3 and the X11 libraries that GLFW uses.

`dist/desktop/windows-amd64/NAME.exe` on Windows is the whole game. It is a GUI program, so no console window opens beside it, and it has no icon of its own. Building it needs a C compiler for cgo, such as MinGW-w64 `gcc` on `PATH`. Players need a graphics driver with OpenGL 3.3. Because it is not Authenticode-signed, Windows SmartScreen warns on first start; players choose **More info → Run anyway**. Windows support is provisional and verified only on GitHub's `windows-latest` runner.

## Package

```sh
go run github.com/krapht-foundation/goje/cmd/goje package -name my-game -version 1.0.0
```

`package` builds as above and then writes `dist/release/`:

- `my-game-1.0.0-web.zip`, the web directory.
- `my-game-1.0.0-darwin-arm64.tar.gz`, `my-game-1.0.0-linux-amd64.tar.gz`, or `my-game-1.0.0-windows-amd64.zip`, the application bundle, the Linux files, or the Windows executable.
- `SHA256SUMS`, which players check with `sha256sum -c SHA256SUMS` on Linux or `shasum -a 256 -c SHA256SUMS` on macOS. On Windows, `Get-FileHash FILE` in PowerShell prints a digest to compare.

Packaging the same build twice gives identical files, and so does building the same sources again with the same Go and C toolchains. Set `SOURCE_DATE_EPOCH` to choose the time stored in the archives; it is 2020-01-01 otherwise.

To publish for macOS, Linux, and Windows, run `package` on each system. The repository's release workflow does this on GitHub-hosted runners for the playground and Beacon and drafts a release when a `v` tag is pushed; use it as a model for your own. Windows has no execute bits, so a web archive packaged there differs from the Unix one in one file mode; the workflow takes the web archive from Linux and macOS and only the desktop archive from Windows.

## Sign a release

Checksums show that a download is intact, not who made it. To let players and your game check that a release is yours, make a key pair once:

```sh
go run github.com/krapht-foundation/goje/cmd/goje keygen -o my-game
```

This writes `my-game.key`, the private key, which only you may read, and `my-game.pub`, the public key. Keep the private key out of the repository and back it up: anyone who has it can sign releases as you, and if you lose it, existing builds cannot trust your new key. The command never overwrites a key.

Then package with the key:

```sh
go run github.com/krapht-foundation/goje/cmd/goje package -name my-game -version 1.1.0 -sign my-game.key
```

`dist/release/` then also holds `SHA256SUMS.sig`, the signature of `SHA256SUMS`, and `update.json`, a signed manifest of the game's name, version, and archives. Publish `my-game.pub` where players can find it. With OpenSSL 3 they check the signature before the checksums:

```sh
openssl pkeyutl -verify -pubin -inkey my-game.pub -rawin -in SHA256SUMS -sigfile SHA256SUMS.sig
```

## Check for updates

Upload `update.json` and the archives to the same directory of your web host. Embed the public key in the game, and use the package `update` to learn whether a newer genuine build exists:

```go
//go:embed my-game.pub
var releaseKey []byte

func checkForUpdate(ctx context.Context, current string) (*update.Manifest, error) {
	key, err := update.ParsePublicKey(releaseKey)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://example.com/my-game/update.json", http.NoBody)
	if err != nil {
		return nil, err
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, update.MaxManifest+1))
	if err != nil {
		return nil, err
	}
	manifest, err := update.Verify(data, key)
	if err != nil || manifest.Name != "my-game" {
		return nil, errors.Join(err, errors.New("not a my-game release"))
	}
	if newer, err := update.Newer(manifest, current); err != nil || !newer {
		return nil, err
	}
	return &manifest, nil
}
```

`update.Verify` accepts only a manifest that your key signed, byte for byte, and `update.Newer` refuses the same or an older version, so a replayed old manifest is not an update. To fetch an archive, call `update.Download` with its entry from the manifest; it stops at the signed size and fails if the digest differs, and you discard whatever it wrote if it fails. The [update specification](../specs/update-m17.md) states every rule. Goje does not install the download: show the player where it is, or open your release page.

## Limits

- The desktop build is for the system you run the command on. It needs cgo and works on macOS, Linux, and Windows; elsewhere use `-target web`. Cross-compiling the desktop build is not supported.
- Signing uses your own Ed25519 key. There is no Authenticode, Apple notarization, store signing, installer, automatic installation, or hosted update channel.
- There are no native Android or iOS builds. Phones and tablets use the installable web app, which you can wrap as described above.
