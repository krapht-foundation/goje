# Goje runtime and graphical hosts

Goje's M1 runtime can step a scene headlessly or present it with a Goje-owned desktop or browser host. The [reference playground](playground.md) uses the same scene on both hosts. See the [M1 specification](../specs/runtime-m1.md) for the engine contracts.

## Run a scene without a window

Import `github.com/krapht-foundation/goje/engine`. Implement `engine.Scene` and pass a factory to `engine.Start`. The factory makes a new scene each time the runtime enters it. Give each `Runtime.Step` an `engine.Input` snapshot; the runtime supplies a tick number and does not read a clock. An empty `Input` means no keys are held or newly pressed.

Return `engine.Restart()` from an update to reload the current encounter or `engine.Replace(factory)` to load another scene. Return the zero `Transition` to keep the current scene. The current scene exits before the new factory runs, but the replacement does not update until the next step. Scene `Enter` must clean up any partial setup if it returns an error. Call `Runtime.Stop` before releasing resources the scenes share; it is safe to call again after a failed step or a normal stop.

See the [lifecycle tests](../engine/runtime_test.go) for a runnable example of restart and replacement without clocks or graphics.

## Launch a graphical scene

Call `platform.Run(ctx, config, factory)` from `main`, importing `github.com/krapht-foundation/goje/platform`. Set `Config.Width` and `Config.Height` in logical pixels. The scene's `Draw(engine.Canvas)` callback records rectangles, line strokes, and regions of the optional `Config.Image`. Call `canvas.Image(source, x, y, width, height)` with a source rectangle in image-local pixels and a destination in logical pixels. The source origin is the upper-left texel and the maximum edges are exclusive. Empty or out-of-bounds source rectangles draw nothing. Goje samples with nearest-neighbor filtering, uploads the image before scene entry, and reuses it across restarts. Close the desktop window or cancel the context to exit the scene and release the host.

- **Desktop:** Build and run the game with CGO on macOS or Linux. macOS requires the Xcode Command Line Tools. Linux requires X11 and OpenGL build headers and runtime libraries. See the [feasibility report](../rfcs/0002-graphics-feasibility.md#build-and-distribution-notes) for the tested package list. Call `Run` in the main goroutine because GLFW requires the startup thread.
- **Browser:** Build the same Go command with `GOOS=js GOARCH=wasm`. Serve it over HTTP as `application/wasm` with the matching `wasm_exec.js` and Go bootstrap. Include `<canvas id="goje" tabindex="0"></canvas>` in the page and give it the intended CSS display size. Focus the canvas to use the arrow keys or WASD and R. The host scales the drawing buffer for the device pixel ratio and stops with an error if WebGL 2 is unavailable or the context is lost.

## Play an animated sprite

Load the PNG and clip JSON with [`sprite.Decode`](../specs/sprite-sheet-v1.md) before launching the host. Copy the sheet into the game's launch image. Resolve each clip name once, check that it exists, then call `clip.InAtlas(offset)` with the sheet's upper-left position in that image. Share the returned clips across actors.

Give each actor a `sprite.Player`. Select the initial clip with `player.Step(idle)` in `Enter`. In each `Update`, call `player.Step` once with the clip the game chose for that tick. Changing clips starts at the first frame; passing the same clip advances it. Restart creates a fresh player, not a new sheet or launch image.

In `Draw`, read `player.Frame()` and skip the sprite if no frame is selected. Align the frame with the actor's logical anchor and pass the resulting rectangle to the existing canvas:

```go
if frame, ok := player.Frame(); ok {
    x, y, width, height := frame.Destination(anchorX, anchorY, scale)
    canvas.Image(frame.Source, x, y, width, height)
}
```

Use `scale = 1` for unscaled sprites or the camera zoom for a projected anchor. Keep the actor's ground sort key and hit rectangle independent of the frame's bounds and pivot.

## Inspect a local run

Give `platform.Config.Logger` your own `*slog.Logger` to receive host startup, shutdown, suspension and resumption, and scene transition events. Goje does not replace the process logger or log every tick. Fatal errors still return to the caller.

Pass a `*platform.Diagnostics` as `Config.Diagnostics` to collect completed ticks, presented frames, and whole ticks dropped by the catch-up limit. Leave it nil to disable diagnostics, or set `TraceCapacity` to zero for counters without phase timing. With a positive capacity, the report holds the first records that fit and counts those omitted afterward. Records identify their frame and, for updates, their tick. Start offsets and durations are CPU-side elapsed times in nanoseconds in JSON. They can include time blocked in a graphics call, but do not measure GPU execution or native swap wait.

```go
capture := &platform.Diagnostics{TraceCapacity: 4096}
config.Logger = slog.New(slog.NewTextHandler(os.Stderr, nil))
config.Diagnostics = capture
err := platform.Run(ctx, config, newScene)
// After Run returns, capture.Result is available even when a run fails.
```

Read the result only after `Run` returns. The host does not execute game exporters, record raw input or game data, or transmit a report. The caller owns the finished trace and decides whether to inspect or serialize it. The [reference playground](playground.md#capture-a-local-run) shows how to view a capture on desktop and web.

## Choose a view for the game

The scene runtime and canvas do not require a projection. A 2D game can draw and pick directly in logical pixels. A 2.5D game can keep gameplay on a flat ground plane, project it for presentation, and draw elevated or depth-sorted visuals. The same game can use different views for different scenes.

Goje aims to support games that mix 2D and 2.5D presentation, including the kind of variety seen in *Cult of the Lamb*. The current canvas can draw flat and isometric scenes with sprites. It does not yet provide a general mesh renderer or a ready-made pipeline for 2D characters in 3D environments.

## Build an isometric world view

`engine.Isometric.Project` maps tile coordinates and elevation to unzoomed projected pixels. Use `engine.Camera.ToLogical` to apply pan and zoom before drawing. For a logical point, `Camera.ToProjected` removes that transform. To find the underlying tile, pass the result to `Isometric.Unproject` with elevation zero.

Keep a reusable slice of `engine.WorldItem` separate from simulation storage. Call `engine.SortWorldItems` after changing ground anchors or adding or removing items. In `Draw`, traverse that slice from back to front and transform each item's `ProjectedAnchor` through the camera. To resolve a logical press, transform it back to projected pixels and call `engine.PickWorld` on the same slice. Hit rectangles are relative to each projected anchor; `HitBlock` stops selection without returning an object. Pan and zoom alone do not require sorting again.

In `Scene.Update`, check `tick.Input.PrimaryPress.Valid` and `tick.Input.SecondaryPress.Valid` before reading their logical coordinates. The desktop and browser hosts buffer the first press for each mouse button independently and ignore presses in letterbox bars. Resolve picking and ground commands against the tick-start camera before applying movement or camera controls. The [M3 command specification](../specs/unit-commands-m3.md) defines how the reference playground uses both edges.

## How a host drives the runtime

The shared [host loop](../internal/platform/loop/loop.go) takes a timestamp relative to a fixed origin, buffers input events, and returns a budget of fixed steps before each presentation. The host takes one snapshot per step, not once per frame: that preserves quick presses when no update ran yet and prevents them from firing again during catch-up. The host combines its focus and visibility state before suspending or resuming the loop. Suspension clears pending input and accumulated time, so neither stale presses nor inactive time affect the resumed scene.

The [desktop host](../internal/platform/desktop/desktop.go) and [browser host](../internal/platform/browser/browser_js.go) own window and canvas events, presentation, and GPU cleanup. Both call the [shared driver](../internal/platform/driver/driver.go), which steps the scene, then records one painter-ordered batch in reusable storage. The host clears the letterbox and submits that batch. Scene state stays independent of both the display refresh rate and GPU resource lifetime.

## Verify the contract

```sh
CGO_ENABLED=0 go test ./engine ./internal/platform/loop ./internal/platform/driver
GOOS=js GOARCH=wasm go test -exec="$(go env GOROOT)/lib/wasm/go_js_wasm_exec" \
  ./engine ./internal/platform/loop ./internal/platform/driver
```

The Wasm check uses Node through the Go toolchain's test runner. It verifies shared runtime behavior, not browser input, rendering, or focus events.

See the [M1 performance baseline](runtime-baseline.md) for the measured cost of enabling local capture and the commands to reproduce it.

See the [M2 world-view measurements](world-view-performance.md) for ordering and hit-query costs, the courtyard density comparison, and the pickup rerun against M1.

See the [M5 ground-art measurements](ground-art-performance.md) for PNG startup cost, warmed allocations, and submission costs on a courtyard and a dense board.

See the [M8 sprite measurements](sprite-performance.md) for sprite import, atlas packing, and the fixed-frame versus animated comparison.
