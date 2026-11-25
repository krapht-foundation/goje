# Run the Goje Playground

This repository includes a reference playground built on Goje's public scene runtime and graphical hosts. It is both a runnable example and a smoke fixture for desktop and web. You do not need to ship this command with your game.

The playground opens the authored isometric courtyard. **Left-click** the tomato scout to select it, then **right-click** a ground tile on the other side of the wall. The yellow bar shows selection; a green marker shows the accepted destination. The scout blinks while idle and hops along the shortest walkable route. The marker disappears on arrival. Walls and the cyan lookout block movement. An invalid command leaves the current route intact.

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

The M12 yard is available through `GOJE_PLAYGROUND=yard make run-desktop` or `http://127.0.0.1:8080/?fixture=yard`. Walk the scout with the arrow keys or **WASD**, and press **R** to restart. The scout stops against walls and slides along them, lights a lamp in the north doorway, and walks beneath the raised deck in the south. See [Move and collide in your game](collision.md) to use the same packages. The yard's deck and doorway are zones in a [version 2 level](levels.md#draw-zones).

## The skirmish

The M13 skirmish is available through `GOJE_PLAYGROUND=skirmish make run-desktop` or `http://127.0.0.1:8080/?fixture=skirmish`. Two blue scouts face two red guards on a small board with a wall, a raised deck, and a line of purple cells that is an alarm. It keeps the courtyard's controls: **left-click** a scout to select it, **right-click** a cell to order it there, the arrow keys pan, **W/S** zoom, and **R** restarts. Scouts fire automatically at the nearest guard within four tiles, and guards wake when a scout comes within six tiles or crosses the alarm line. Bolts are the small pale squares, health bars show green for health and dark red for what is gone, and a defeated unit disappears. Bolts hurt friends too, pass under the deck, and stop at the wall.

Try it: select the upper-left scout and order it one cell toward the other scout, then select the other scout and order it to the cell the first one left. The first scout stops in the second scout's way, so the second scout waits behind it, touching, and after about a second asks for a new route and walks around through the next column over. Nobody is hurt. Units cannot share ground, so when they meet they stop, wait, and then look for a way around; a unit that finds none gives up and stays. Then press **R**, select the upper-left scout, and order it to the cell just past the alarm line, in the same row. The scout crosses the line, both guards wake, and the fight plays out. In the recorded history the ordered scout and both guards fall and the second scout survives with 20 health. The level, including the deck and the alarm, is [`levels/skirmish.json`](../cmd/playground/levels/skirmish.json), a version 2 level whose zones the Level Builder can edit. The rules live in [`examples/skirmish`](../examples/skirmish/sim.go) and are specified in the [skirmish specification](../specs/skirmish-m13.md).

## The outpost

The M14 outpost is available through `GOJE_PLAYGROUND=outpost make run-desktop` or `http://127.0.0.1:8080/?fixture=outpost`. It is a small real-time battle that shows text, a HUD, commands for groups of units, recordings, and saved games. Six blue scouts stand at the left of an 11 × 9 board and six red guards at the right. Two pairs of pillars stand in the way, a purple line of cells in the middle is an alarm, and a raised deck sits beyond it. A title menu starts a match, loads a saved game, or watches the last recording; **Escape** pauses a match and opens a menu to resume, save, load, watch the replay of the match so far, restart, or quit to the title. Watching a replay ends the match.

In a match, drag with the left button to select the scouts inside a box, click one scout to select it, and hold **Shift** to add to the selection or to remove a scout from it. **Ctrl+1** to **Ctrl+9** store the selection in a group, and **1** to **9** bring it back. **Right-click** the ground to send the selection there, spread over nearby cells, and right-click a guard to attack it. Press **A** and then left-click to attack-move, so the group fights what it meets on the way and then goes on. **H** makes the group hold fire, **F** focuses it on the guard under the pointer or the one nearest to it, and **R** restarts. The top bar counts the units and lights an alarm label, the bottom bar shows the selection and its health, and a red clock shows that the match is being recorded.

Try it: drag a box over the whole board to select every scout, right-click the gap between the two pairs of pillars, which is the fourth column and fifth row, to gather them there, wait a moment for the guards to notice, and then press **F** about once a second. The scouts focus one guard at a time, and in the recorded history they win with two scouts left. Press **Escape**, choose **Save**, play on, and choose **Load** to return to the saved moment. Choose **Quit to title** and then **Watch last replay** to see the match you just played run by itself; the label turns to REPLAY ENDED at its last tick. A save or a recording made with a different level file or different rules is refused with a notice instead of being played.

The browser keeps the save and the last recording in `localStorage`, and the desktop build keeps them as files in the `goje/outpost` folder of your user configuration directory. See [Put text and a HUD in your game](text-and-ui.md) and [Record, replay, save, and load](replays.md) to use the same packages, and the [outpost specification](../specs/outpost-m14.md) for the exact controls and formats. The outpost's placements and zones come from a [version 2 level](levels.md#add-and-remove-placements) that you can rebuild in the Level Builder with `make smoke-outpost-roundtrip`.

## Use the playground in your game

The [Beacon example](../examples/beacon/game.go) is a small, top-down 2D game built on the public `project`, `level`, `engine`, `platform`, and `sprite` packages. It owns its layout, rendering, and input rules, and it does not import the reference courtyard. Try the included project with `go run ./examples/beacon`.

To start a separate module, run these commands from the Goje checkout:

```sh
goje_root="$PWD"
go_version="$(go list -m -f '{{.GoVersion}}')"
mkdir ../my-playground
cp examples/beacon/game.go examples/beacon/run.go ../my-playground/
cp -R examples/beacon/assets examples/beacon/web ../my-playground/
cd ../my-playground
export GOWORK=off
go mod init example.com/my-playground
go mod edit -go="$go_version" -require=github.com/krapht-foundation/goje@v0.0.0 \
  -replace=github.com/krapht-foundation/goje="$goje_root"
go mod tidy
go run .
```

The local `replace` selects the unreleased Goje revision. Use a published version without `replace` once one is available. Click the beacon or press **Space** to toggle it. Use the arrow keys or **WASD** to move the tomato in the lane below the board, and press **R** to reset both. The game validates the placement and loads the atlas before opening a window. Reset recreates only scene state.

To use your own assets, open `assets/arena.json` in Level Builder and move the `beacon` to another walkable cell. Save the export as `assets/arena.json` in your module. Use the Tile Maker to draw a PNG and save it as `assets/floor.png`. You can rename either file if you update [`assets/goje.json`](../examples/beacon/assets/goje.json). The example requires exactly one `beacon` and a board of at most 4 × 2 cells. Validate the pair before rebuilding:

```sh
(cd "$goje_root" && go run ./cmd/projectcheck ../my-playground/assets)
```

To run the same module in a browser, build the Wasm binary and add the matching Go bootstrap to the included page:

```sh
GOOS=js GOARCH=wasm go build -o web/playground.wasm .
cp "$(go env GOROOT)/lib/wasm/wasm_exec.js" web/
node "$goje_root/scripts/playground-server.mjs" 8080 "$PWD/web"
```

Open `http://127.0.0.1:8080/` and click the canvas to focus it. Both builds embed the same project.

From the Goje checkout, `make setup-web` and `make smoke-beacon` automate the author-to-play workflow using exact editor downloads. The smoke builds Beacon from a separate Go module and checks rendered pixels for the edited board, blink, hop, toggle, and reset. On headless Linux with the native graphics dependencies, use `xvfb-run -a make smoke-beacon`.

Both examples embed the same [tomato sheet and clips](../specs/sprite-sheet-v1.md#loading-and-ownership). To use different art, run `go run ./cmd/spritecheck SHEET.png CLIPS.json`, then replace `tomato.png` and `tomato.json` under `cmd/playground/art/` or Beacon's `assets/` directory. The games expect `idle` and `move` clips. No `goje.json` change is needed.

### Play a sound in Beacon

Click the beacon or press **Space** to play its short, synthesized [`toggle.wav`](../examples/beacon/assets/toggle.wav). Reset does not play the cue. Beacon decodes the file at startup with [`audio.DecodeWAV`](../audio/wav.go), passes the clip in `platform.Config.Sounds`, and requests playback from the scene with `Tick.PlaySound`. Use mono, 48 kHz, signed 16-bit PCM WAV of at most three seconds for your own cues. Leaving `Sounds` empty disables audio.

On desktop, Goje uses the available native audio device. In the browser, a trusted click, tap, or key press unlocks Web Audio; input that arrives before unlock is not queued for later. Sound is optional: Beacon still works when audio is unavailable. Run `make setup-web` once, then `make smoke-beacon-audio` to verify the desktop callback and Chromium output. The desktop smoke uses a silent device by default; set `GOJE_AUDIO_DEVICE=default` to exercise the system device. Run `make benchmark-beacon-audio` to compare audio disabled, idle, and burst frame pacing.

## Make a sound effect in the browser

Run `make serve-sound-maker` with Go and Node installed, then open `http://127.0.0.1:8085/`. Set `SOUND_MAKER_PORT` to use a different port. The editor runs on loopback and never uploads your files.

1. Choose a waveform (sine, triangle, or square), a start and end pitch from 20 to 10,000 Hz, a duration from 10 to 3,000 ms, and an attack and release in milliseconds. The pitch glides in a straight line from start to end, and the attack and release must fit within the duration.
2. Press **Apply** or **Enter**. The controls are a draft until you apply them. If a value is out of range, the status explains why, your draft stays in the fields so you can correct it, and the current sound is not changed.
3. Choose **Play** to preview the sound. The preview plays the same samples that the download contains. If the browser has no Web Audio, the preview reports that it is unavailable and everything else still works.
4. Choose **Download WAV**. The file is mono, 48 kHz, signed 16-bit PCM, so Beacon and your own games can load it with [`audio.DecodeWAV`](../audio/wav.go). The same settings always produce the same bytes. The editor cannot reopen a WAV to recover its settings, so note the settings you want to keep.

Run `make setup-web` once, then `make smoke-sound-maker` to exercise the keyboard controls, preview, and downloads in Chromium. The smoke decodes the WAV and compares it with the preview samples, and checks that rejected settings leave the sound unchanged.

### Play a Sound Maker download in Beacon

Download a WAV from the Sound Maker, then replace Beacon's `assets/toggle.wav` with it in your own module and rebuild. The file must stay mono, 48 kHz, signed 16-bit PCM and at most three seconds, and Beacon reports a clear startup error for any other file. Nothing else needs to change: the toggle plays whatever clip is in `platform.Config.Sounds`.

From the Goje checkout, `make smoke-sound-roundtrip` automates this on scratch copies. It makes a short square-wave effect in the Sound Maker, checks that the download differs from the shipped cue, embeds it unchanged in a separate Beacon build, and plays it with real input on both targets. On desktop, the device callback must receive exactly the downloaded samples once, then silence, and reset must not play it. In Chromium, the samples that Web Audio renders must equal the download bit for bit when the context runs at 48 kHz. The browser smoke also forces a 44.1 kHz context, where the browser resamples, and requires the rendered cue to stay within 10% RMS of an interpolated copy of the download after aligning for filter delay. The checked-in assets are never modified. On headless Linux, use `xvfb-run -a make smoke-sound-roundtrip`.

## Draw pixel art in the browser

Run `make serve-pixel-art` with Go and Node installed, then open `http://127.0.0.1:8084/`. Set `PIXEL_ART_PORT` to use a different port. The editor runs on loopback and never uploads your files.

1. Enter a width and height and choose **New sheet**, or choose **Open PNG**. Each axis can be 1–512 pixels. Imports and downloads must fit within 1 MiB. Opening a file or creating a sheet never overwrites the original.
2. Choose a palette color or set your own. Click or drag to draw with the **Pencil** or clear pixels with the **Eraser**. To work by keyboard, focus the sheet and use the arrow keys and **Enter** or **Space**. **Undo** and **Redo** operate on whole strokes or individual keyboard edits.
3. Inspect the zoomed sheet and the 1× preview, then choose **Download PNG**. The download contains only the artwork, without the checkerboard or pixel grid. To animate it, open the exact PNG in the [Sprite Clip Editor](#author-sprite-clips-in-the-browser).

Run `make setup-web` once, then `make smoke-pixel-art` to edit, download, and reopen artwork in Chromium. The smoke reports edit-to-preview and download-handoff timings for a small sheet and a 512 × 512 sheet; these are observations, not CI limits.

## Author sprite clips in the browser

Run `make serve-sprite-editor` with Go and Node installed, then open `http://127.0.0.1:8083/`. Set `SPRITE_EDITOR_PORT` to use a different port. The editor runs on loopback and does not upload your files.

1. Choose a sprite sheet PNG and optionally its v1 clip sidecar, then press **Open**. Try the [tomato PNG](../sprite/testdata/tomato.png) and [its sidecar](../sprite/testdata/tomato.json). A PNG without a sidecar starts with no clips.
2. Select a clip and frame, or name a new clip. Drag a rectangle on the sheet or type exact coordinates. Set a frame-relative pivot, such as the bottom center, and a duration in simulation ticks (60 per second). Press **Apply frame** to commit, or **Add frame** or **Add clip** to create a new entry. The frame list supports duplicate, delete, and move actions. Use **Undo** and **Redo** to recover committed edits.
3. Select **Play** to loop the clip, or **Step one tick** while paused. The preview places every frame's pivot on the same anchor so you can spot unwanted movement between poses. The preview and its zoom never change the exported metadata.
4. Choose **Download JSON** and keep the source PNG. Reopen the download with that PNG to keep editing, or run `go run ./cmd/spritecheck SHEET.png CLIPS.json` before embedding the pair. The editor never overwrites either input file.

Run `make setup-web` once, then `make smoke-sprite-editor` to exercise keyboard and pointer editing, preview timing and alignment, and download and reopen in Chromium.

## Paint, animate, and play a sprite

1. In the **Pixel Art Maker**, open the [tomato PNG](../sprite/testdata/tomato.png), repaint part of the sheet, and choose **Download PNG**.
2. In the **Sprite Clip Editor**, open the downloaded PNG with the [original sidecar](../sprite/testdata/tomato.json). Edit the clip frames or their timing and choose **Download JSON**.
3. Validate the two downloads together with `go run ./cmd/spritecheck SHEET.png CLIPS.json`. To see them in a game, copy the files as `tomato.png` and `tomato.json` into a scratch copy of the courtyard's `cmd/playground/art/` or Beacon's `examples/beacon/assets/` and rebuild.

Run `make smoke-sprite-roundtrip` to exercise this workflow in Chromium and play both exact downloads in scratch builds of Beacon and the authored courtyard on desktop OpenGL and Chromium WebGL. On headless Linux, use `xvfb-run -a make smoke-sprite-roundtrip`. The smoke paints a cyan stripe on the tomato's hop, moves the hop to the first slot of `move`, and extends it from 8 to 16 ticks. Both games show the stripe and the clear space below the actor at motion ticks 13 and 16, land on tick 17, and return to the standing pose on reset. The checked-in assets are not overwritten.

## Draw a ground tile in the browser

Run `make serve-tile-maker` with Go and Node installed, then open `http://127.0.0.1:8082/`. Set `TILE_MAKER_PORT` to use a different port. The app runs on loopback and does not upload your files.

1. Start with the blank tile or choose **Open PNG** to edit a 32 × 16 PNG up to 64 KiB.
2. Choose a color and use **Pencil** to paint opaque pixels or **Eraser** to clear them. Click or drag on the grid, or focus it and use the arrow keys and **Enter** or **Space**. **Undo** and **Redo** operate on whole strokes or individual keyboard edits.
3. Use the native-size tile and the isometric repeat to check the result. The optional diamond guide helps place transparent corners without limiting where you can draw.
4. Choose **Export PNG** to download a copy. The editor never overwrites the original and preserves intermediate alpha on untouched pixels. Reopen the download or validate it for the playground using the instructions below.

Run `make setup-web` once, then `make smoke-tile-maker` to exercise the controls and check the downloaded PNG in Chromium. The smoke reports stroke time (including browser input and two animation frames) and export time (from initiating the click to the download event), not Go CPU time.

## Validate custom ground art

Export a 32 × 16 PNG, at most 64 KiB, with transparent corners around the isometric diamond. From the repository root, run `go run ./cmd/tilecheck /path/to/tile.png`. This reads the file without rewriting it. To see the result, point the `ground` field of `goje.json` at the PNG in a scratch copy of the reference command and rebuild as shown in [Play a level edit](levels.md#play-a-level-edit). The authored courtyard validates the tile at startup and draws it beneath the grid on walkable cells only. Pickup and patrol do not use the tile.

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

`make check` does not rewrite source. Use `make fmt` to apply formatting. The native smoke check scripts input snapshots and reads the OpenGL back buffer. The Chromium check uses real keyboard and mouse input and canvas screenshots. Both check the authored scout's blink, hop, ground pivot under zoom, wall occlusion, selection, detour, arrival, and reset alongside the earlier patrol and pickup fixtures, the yard, and the skirmish's scout held up by another scout and going around it, selection, order, alarm line, bolts, health bars, defeat, and reset, and the outpost's drag selection, control groups, orders, pause, save, load, and replay, which the Chromium check plays through a real `localStorage`. `make test-wasm` runs the simulation tests as WebAssembly, and `make benchmark-skirmish` measures a 128 against 128 battle, with and without group orders. `make check-example` builds the command from an independent module for the host and for WebAssembly.

The native smoke fixture is intended for macOS and Linux with X11, including Xvfb; the browser fixture uses Chromium with WebGL 2. Neither smoke check certifies GPU performance or every platform or browser. `make setup-web` installs its Chromium headless shell into `.build/playwright/`, separate from any user-wide cache. See the [playground specification](../specs/playground.md) for acceptance criteria.
