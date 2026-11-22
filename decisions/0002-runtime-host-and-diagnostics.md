# ADR 0002: Goje-owned hosts and opt-in diagnostics

**Status:** Accepted for M1. Specific low-level graphics libraries remain subject to the [feasibility spike](../rfcs/0002-m1-runtime-foundation.md#implementation-plan).

## Context

The first playable slice used Ebitengine to validate desktop and browser targets. M1 must give developers a working launch path without asking them to write platform glue, while keeping Goje in control of game-facing behavior. Contributors also need to diagnose runtime behavior without putting observability work in the default game loop.

## Decision

- Put the public engine core in `engine/`, separate from commands and tooling, and leave no Go package at the module root. Games import `github.com/krapht-foundation/goje/engine`. The planned `platform/` launch package depends on the engine, never the reverse. This replaces the root package layout from [ADR 0001](0001-first-runtime-and-tooling.md) without changing the module path.
- Keep one Go module. Goje owns scene lifecycle, host integration, input translation, and drawing. Use maintained low-level libraries for platform and graphics primitives, but do not build on a game framework. Remove Ebitengine when the reference playground moves to the Goje hosts. This supersedes the playground backend choice in [ADR 0001](0001-first-runtime-and-tooling.md) for M1, not the single-module layout.
- The hosts schedule fixed updates using a shared policy. The core executes one tick per step and does not own a wall clock. That keeps headless tests independent of the window, canvas, and display refresh rate. A core scheduler would need to account for pause and resumption across hosts while duplicating responsibilities already owned by the platform loops.
- Use `log/slog` for infrequent structured lifecycle logging. Collect counters and phase traces locally and only when requested. The default path does no diagnostics work beyond selecting that path; enabled capture uses bounded storage and may lose records instead of making the game wait. Remote telemetry, vendor SDKs, and general-purpose hot-loop hooks are outside M1.

## Consequences

The team owns host pacing, shutdown, and the cross-platform test matrix instead of inheriting those behaviors from a game framework. The core remains usable without graphics dependencies. Diagnostics are designed to explain game behavior, not change it; capture may cost CPU time, so M1 verifies and reports its cost separately from the default baseline. The [runtime specification](../specs/runtime-m1.md) defines the observable guarantees and checks.
