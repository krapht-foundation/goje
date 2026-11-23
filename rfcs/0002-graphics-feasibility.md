# M1 graphics feasibility spike

This spike checks the graphics candidates in the [M1 runtime plan](0002-m1-runtime-foundation.md). It used disposable probes in a standalone Go 1.27.4 module. Neither probe imports Ebitengine or changes the reference playground.

## Verification

Each probe cleared a 160×160 logical surface to blue and drew a red triangle using a core-profile shader pair and a vertex array. The checks compared exact RGBA values at the center and outside the triangle, so a successful build, context creation, or clear alone could not pass.

| Target | Observed result |
| --- | --- |
| macOS 15.5, arm64, Apple M4 Pro | Requested an OpenGL 3.3 core context through GLFW 3.3.10; received OpenGL 4.1 (`Metal - 89.4`). Both pixels matched in the 320×320 window back buffer. The probe swapped buffers, destroyed the window, and terminated GLFW. |
| Debian 12.15, arm64, Xvfb in an OrbStack container | GLFW 3.3.10 used X11/GLX and received OpenGL 4.5 core from Mesa 22.3.6 (`llvmpipe`). Both pixels matched in the 160×160 window back buffer. The probe swapped buffers, destroyed the window, and terminated GLFW. |
| Chromium 159.0.8154.6 headless, Go `js/wasm` | WebGL 2 reported OpenGL ES 3.0. The Go animation callback drew the triangle, and both pixels matched in the 160×160 Chromium canvas screenshot. Pressing **Stop** removed the probe's listener, released its Go callbacks and graphics objects, and returned from the Wasm program. |

## Build and distribution notes

- The native Go bindings were `github.com/go-gl/gl` at `v0.0.0-20260331235117-4566fea9a276` (MIT) and `github.com/go-gl/glfw/v3.3/glfw` at `v0.0.0-20260823155953-d41da22a9587` (BSD-3-Clause). The GLFW binding bundles GLFW under its zlib/libpng license. Retain the applicable notices when redistributing. These are the tested versions, not dependencies added to Goje.
- The native build requires CGO and a C toolchain. On macOS, Xcode Command Line Tools supplied the compiler and SDK. Keep GLFW calls and the active graphics context on the process main thread. OpenGL is [deprecated on macOS](https://www.glfw.org/docs/latest/compat_guide.html#compat_osx), so passing this spike is not a long-term backend commitment.
- For Debian 12, the probe environment used `gcc`, `pkg-config`, `xorg-dev`, and `libgl1-mesa-dev` to build, plus `libgl1-mesa-dri`, `xvfb`, and `xauth` to run headlessly. `LIBGL_ALWAYS_SOFTWARE=1 xvfb-run -a` exercises the X11/Mesa software path rather than relying on GPU passthrough.
- The browser build uses the standard Go toolchain for `GOOS=js GOARCH=wasm`, its matching `wasm_exec.js`, and an HTTP server that serves Wasm as `application/wasm`. The probe preserved the drawing buffer only so the screenshot could be taken after the animation callback. M1 should not inherit that setting by default.

All three candidate paths passed the spike. Proceed with the headless runtime; keep these graphics paths as M1 candidates. This validates a small shader draw and the probe's cleanup, not frame pacing, throughput, context loss recovery, GPU performance, Linux x86-64, or other browsers and window systems.
