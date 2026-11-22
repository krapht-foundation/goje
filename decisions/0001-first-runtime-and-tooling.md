# ADR 0001: One Go module and an Ebitengine playground

**Status:** Accepted for the first playable slice.

## Context

Goje needs to prove that a single Go game can run on desktop and in a browser before designing a general-purpose engine or editor API. Contributors also need the same quality commands locally and in CI.

## Decision

Use one module, `github.com/krapht-foundation/goje`, with Go 1.27 or newer. Put the consumer-facing package at the root, implementation under `internal/`, and thin executable entry points under `cmd/`. Keep the public projection math independent of graphics. Use Ebitengine in the playground for the frame loop, keyboard input, rendering, and platform integration; it is not part of the public API.

Use Make for shared commands and pin developer tool versions in `tools.mk`. Use the portable parts of SuperEngine's `golangci-lint` policy: `gci`, `gofumpt`, `goimports`, and `golines`, with additional correctness, security, and maintainability checks. Goje owns its dependency rules and does not inherit backend-specific build tags or exclusions. Formatting is an explicit mutation; `make check` must not rewrite source. Use native and browser smoke checks for the [playground contract](../specs/playground.md).

## Consequences

Desktop and web share the game code, but require different runtime verification. Ebitengine gives the first slice a working cross-platform runtime without committing the public API to it. Separate modules, a custom renderer, and editor-specific dependencies are not necessary for this slice.

The repository keeps the runnable playground to exercise both targets and reproduce integration failures. The demo and smoke harness are consumers, not dependencies of Goje's public package. The design of reusable playground tooling is a separate [proposal](../rfcs/0001-game-owned-playgrounds.md).
