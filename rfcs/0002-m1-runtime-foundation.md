# RFC 0002: M1 — Goje runtime foundation

**Status:** Accepted for M1. The [runtime specification](../specs/runtime-m1.md) defines the contract, and [ADR 0002](../decisions/0002-runtime-host-and-diagnostics.md) records the architecture decisions. The candidate graphics paths remain subject to the feasibility check below.

## Goal

A game developer supplies a scene written in Go and runs it through Goje on desktop and the web without writing platform glue. The same scene can be stepped headlessly. M1 replaces the platform and lifecycle code in the existing [reference playground](../specs/playground.md); it does not add a new game or an editor.

Phaser's [Game](https://docs.phaser.io/phaser/concepts/game) and [Scenes](https://docs.phaser.io/phaser/concepts/scenes) establish a useful ownership split: the game runtime runs the lifecycle and shares platform services, while a scene owns local state and behavior. Goje takes that split without adopting Phaser's class hierarchy, injected plugins, or support for multiple active scenes in M1.

## Selected direction

| Decision | M1 direction | Consequence |
| --- | --- | --- |
| Scope | Build the runtime foundation. Reuse the current encounter as the acceptance scenario. | Camera, depth sorting, and picking follow M1. |
| Backend | Own the desktop and web hosts in Goje. Do not build new runtime work on Ebitengine or another game framework. Remove Ebitengine when the reference playground switches hosts. | Use low-level libraries for windowing, graphics bindings, and other primitives. Goje owns the integration and game-facing behavior. |
| Clock | The host schedules fixed ticks. The core executes one tick when asked and never reads wall time. | Running a scene headlessly does not require a window, a browser, or a second scheduler. |
| Repository | Keep the single Go module and put the public core in `engine/`, per [ADR 0002](../decisions/0002-runtime-host-and-diagnostics.md). | Keep backend-specific dependencies out of the engine's import graph, even if the module lists them. |

## Design priorities

- **Developer experience and batteries included.** Provide one documented launch path with sensible defaults for timing, keyboard input, viewport scaling, drawing, and scene lifecycle. The same game source should run on both targets without game-specific build tags, platform loops, or backend setup code. Supply a copyable example, a browser bootstrap, documented build and run commands, and errors that explain missing platform requirements or failed initialization. Keep the headless stepping API available for tests without forcing graphical applications to use it directly. Batteries included means Goje supplies the pieces needed for the supported workflow; it does not mean implementing every engine subsystem in M1.
- **Use low-level dependencies deliberately.** Prefer maintained libraries with suitable licenses and target support over writing platform primitives from scratch. Verify their build requirements and measure costs at hot boundaries such as cgo and Wasm-to-JavaScript calls. Keep their types behind the host boundary. Do not delegate the game loop, scene model, or game-facing API to a dependency.
- **Measure performance from the first slice.** Keep steady-state engine paths free of avoidable allocations and copies. Reuse frame buffers and uploaded textures; batch drawing across a host boundary where measurements show that individual submissions are costly. Treat startup and asset upload separately from steady state. Do not add pools, parallelism, or specialized storage without a measured need.

## Package and ownership boundaries

| Location | Owns |
| --- | --- |
| Public `engine/` package | The game lifecycle, one active scene, single-tick stepping, neutral input and drawing contracts, and the existing isometric projection. No OS handles, GPU handles, JavaScript, cgo, or graphics bindings. |
| Public `platform/` package | One launch entry point for consumers. It selects the desktop or web implementation using Go build constraints; game code does not branch by OS. |
| `internal/platform/` | Platform loops, a shared host-side tick scheduler, input buffering, window or canvas setup, rendering, and native resource cleanup. |
| `cmd/playground/` and `web/playground/` | The reference encounter and browser bootstrap. The example calls public Goje packages; smoke assertions remain outside the runtime API. |

Dependencies point from the app and host toward the core, never the reverse. The headless driver uses the same lifecycle and tick path as the graphical hosts.

## Accepted contracts

The [M1 runtime specification](../specs/runtime-m1.md) defines lifecycle, timing, input, presentation, and local observability. In particular, diagnostics are off by default, and their overhead must be measured separately from the engine baseline.

The first candidates are [GLFW](https://www.glfw.org/docs/latest/intro.html) and OpenGL 3.3 core on desktop, and Go `js/wasm` with [WebGL 2](https://developer.mozilla.org/en-US/docs/Web/API/WebGL2RenderingContext) in the browser. The proposed verification matrix is macOS, Linux with X11 under Xvfb/Mesa, and Chromium. Windows, Wayland-native support, Safari and Firefox certification, and signed desktop installers are not M1 acceptance targets. This is a verification boundary, not a change to the desktop and web product goals.

## Implementation plan

| Order | Deliverable | Exit check |
| --- | --- | --- |
| 1 | Use the accepted spec and ADR to guide a feasibility spike. Check low-level dependencies, licenses, native thread requirements, and browser graphics availability with a window and a canvas that each draw one primitive. | The chosen hosts present pixels on the verification matrix. If a primitive fails, bring the alternatives back to review before building a renderer. |
| 2 | Implement the headless runtime, scene entry and exit, restart and replacement, and single-tick input. Implement the host scheduling policy independently of the runtime. | Deterministic tests cover hook ordering, exactly-once cleanup, failure paths, bounded catch-up, resume, and input edges across zero or multiple ticks per presentation. |
| 3 | Implement the native and browser hosts and the minimal drawing path. Add logical scaling, input mapping, and shared image upload. | Both hosts launch a scene, render it, react to input and resize, and shut down cleanly. Startup failure is visible to the caller or browser user. |
| 4 | Port the reference encounter to the public Goje runtime and host. Move restart onto the lifecycle path so that the experimental playground wrapper does not maintain a competing reset mechanism. Replace the old host smoke path, remove Ebitengine and its unused dependencies, and update contributor and public guides. | Native-smoke and Chromium runs show the player and token at spawn, after pickup, and after reset. The browser also proves resize preserves state and focus loss does not leave a stuck key. The copy of the reference command outside Goje's module builds for desktop and `js/wasm` using public APIs. |
| 5 | Verify the developer workflow and capture a performance baseline using the host diagnostics. Document build prerequisites, a minimal launch example for each target, and how to reproduce the measurements. | Follow the guide from a fresh external module on desktop and web without writing platform glue. Record allocation counts for steady-state runtime steps and tick and frame timing for the reference encounter and a larger repeatable sprite workload on both hosts. |

## M1 acceptance

- `make check`, `make test-race`, `make check-example`, `make smoke-desktop`, and `make smoke-web` pass on their designated targets. The smoke checks inspect actual pixels; compiling the targets alone is not sufficient.
- A headless test can start, step, restart, replace, and stop a scene with no platform libraries loaded. The core passes with `CGO_ENABLED=0`, and the module no longer lists Ebitengine. Documentation names the verified targets and the requirements for local runs.
- The same reference scene and inputs work on both hosts. A restart restores encounter state and reuses the shared image and active host.
- The performance report records workload size, hardware, target, toolchain, and build settings. The headless no-op scene step performs zero steady-state allocations; report tick and frame duration percentiles separately and record when the host drops simulation debt. The baseline is not a performance guarantee or a cross-machine CI timing threshold.
- Engine packages and cross-platform subsystems meet the [maintainer documentation standard](../CONTRIBUTING.md#document-code-for-maintainers). A contributor can find the runtime entry point and trace a tick, a scene restart, and resource cleanup across the core and hosts.
- Verify the [diagnostics contract and overhead](../specs/runtime-m1.md#diagnostics-verification) on both hosts. The reference app demonstrates how developers supply a logger and inspect a local capture.

## Outside M1

Scene stacks, a general-purpose render graph, camera and picking APIs, ECS, collision and physics, RTS navigation, audio, networking, editor applications, a versioned asset pipeline, and remote telemetry or vendor-specific exporters. The next milestone can use an overlapping-sprite scene to validate 2.5D camera transforms, render ordering, and selection together. The broader game-owned playground product remains tracked in [RFC 0001](0001-game-owned-playgrounds.md).
