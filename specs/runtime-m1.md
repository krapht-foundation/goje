# M1 runtime specification

**Status:** Accepted. The headless lifecycle, shared timing and input policy, graphical hosts, and local diagnostics are implemented and used by the [reference playground](playground.md). See the [runtime guide](../docs/runtime.md) for the API and the [performance baseline](../docs/runtime-baseline.md) for measurements.

## Scene lifecycle

- Start one active scene on the game loop thread. A game supplies a factory that creates fresh mutable state on each entry. If entry fails, the scene must unwind its own partial setup and is never updated or drawn. Only successfully entered scenes receive an exit callback.
- Each step delivers one input snapshot and executes one update. Accept at most one scene restart or replacement request per update. After a successful update and before the next update or draw, exit the old scene, then create and enter the new one. The replacement's first update runs on the next tick, even during catch-up. No lifecycle hooks are invoked recursively.
- Restart uses the same factory and encounter configuration without restarting the host, rewinding the runtime tick count, or reloading shared assets. In the reference playground, restart takes priority over movement for that tick.
- On shutdown or a fatal host or update error, exit the active scene exactly once before disposing host resources. If replacement fails after the old scene has exited, stop and report the error. The headless driver uses the same lifecycle and tick path as the graphical hosts.

## Host timing and input

- The host schedules 60 fixed ticks per second. The runtime executes one tick per step without reading wall time. A presentation can run zero to four updates; after four, discard remaining whole ticks and keep the fractional remainder. Rendering does not set the simulation rate. M1 does not interpolate draws or guarantee cross-machine replay determinism.
- The native host uses a monotonic clock and the browser host uses the `requestAnimationFrame` timestamp. Both use the same scheduling policy.
- Losing focus or hiding the page suspends simulation, clears held keys and pending press edges, and resets timing. Resume establishes a new baseline and does not catch up inactive time. Hold state persists across ticks while active. Buffer a key press until a tick consumes it, even if the key was released in the meantime. Do not repeat the edge on catch-up ticks or OS key repeat.
- **Pointer and buttons (M14).** Each snapshot carries the pointer position in logical viewport pixels with an `Inside` flag, the held state of the left and right buttons, and one release edge per button beside the press edges. The position is sampled once per tick, the latest value wins, and it is zero with `Inside` false when the pointer is outside the viewport or the window is not focused; after focus returns it is unknown until the pointer next moves. A release edge carries the last position the pointer had inside the viewport, because a drag can end outside it, and is emitted only for a press the host accepted in the viewport. Release edges buffer like press edges: the first one is kept until a tick consumes it. Losing focus clears all edges and held state without a release, so a scene must treat a drag that ends without a release as cancelled. The browser host captures the pointer during a press so the release and the drag's motion arrive outside the canvas.
- **Keys (M14).** The key set also includes Shift and Control, which are held while either physical key is held, Escape, Enter, H, F, and the digits 1 to 9 on the main row. The set is a 32-bit mask.
- **Canvas (M14).** `Canvas.ImageTinted` draws an image region with every texel multiplied by a color, so one white glyph sheet serves every text color. Opaque white draws the texels unchanged, as `Image` does.

## Presentation and resources

- Drawing does not mutate simulation state. Present a logical viewport with uniform scaling and letterboxing in the physical framebuffer. Resizing preserves scene state. Keep ground position, elevation, and logical screen coordinates separate.
- Support a background, colored geometry, and nearest-neighbor textured quads in paint order. The game chooses one optional launch image. Each quad samples a region in image-local pixels from its upper left, with exclusive maximum edges. An absent image or an empty or out-of-bounds source region draws nothing. The host uploads the image once, reuses it across scene restarts, and owns the GPU resource until shutdown. An asset manager and content format are out of scope.
- The native host honors the selected windowing library's thread requirements. The browser host cancels frame callbacks and releases its event handlers on shutdown. On graphics context loss, stop cleanly and surface the error; recovery is out of scope.

## Local diagnostics

- Launch configuration accepts a game-supplied `log/slog` logger without changing the process-wide default. Log infrequent lifecycle events such as startup, scene transitions, and suspension. Do not log every update or draw. Return fatal errors to the app boundary for presentation rather than logging the same failure at each layer.
- Local counters and phase capture are off by default. With diagnostics disabled, tick and frame paths do no diagnostic timing reads, allocations, buffer writes, locking, callback dispatch, or I/O. Ordinary scheduler timing and runtime state are not diagnostics. Lifecycle logging stays outside the steady-state hot path.
- When enabled, a snapshot reports completed ticks, presented frames, and whole ticks dropped by the catch-up limit. Snapshots expose values, not mutable engine state. Phase capture records update, draw, and host submission with tick or frame identifiers. These are CPU timings, not GPU timings.
- Capture storage is bounded and allocated before capture begins. Record into reusable storage without allocating, blocking, or invoking game or exporter callbacks per record. When full, omit records and count the omissions; never delay the engine to preserve diagnostics. Read or export the capture outside the hot loop. No automatic transmission, raw input, or game payloads.

### Diagnostics verification

- Test counter values, phase ordering, and capture overflow with a controlled clock and a small capacity. Verify that overflow does not change the number of ticks or frames processed. Verify that the disabled path takes no diagnostic timing reads and adds no allocations to steady-state steps.
- On the same hardware, build configuration, and workload, compare repeated native and browser runs with diagnostics disabled and enabled. Use the same external or benchmark timing in both modes so the disabled baseline does not rely on the tracing code being measured. Report tick and frame time percentiles, dropped simulation ticks, and capture capacity. Enabled diagnostics are not free; report their measured cost and investigate repeatable regressions in frame pacing before accepting the capture design. Do not use cross-machine timing thresholds as a substitute for measurement.
