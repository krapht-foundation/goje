# RFC 0001: Game-owned playgrounds

**Status:** Proposed.

## Problem

Game developers need a way to assemble a playable test environment from their own scenes, entities, and assets. Goje's [current demo](../specs/playground.md) exercises one built-in scene on desktop and web, but it does not give another game a way to create its own playground.

## Proposed direction

| Piece | Purpose |
| --- | --- |
| Developer tooling | Give developers reusable public Go APIs to assemble and run a playground inside their own game. |
| Reference playground | Show how to build one using the same public API available to any game. The example should build when copied into a separate Go module, without importing `internal/` or relying on a Goje checkout. |
| Smoke scenario | Keep a small runnable scenario in this repository to check input, simulation, and rendering on desktop and web. It may share an executable with the reference playground, but its assertions do not belong in the shipped API. |

Goje's runtime and developer tooling must not depend on the example or its test assets. Keep the existing demo as a smoke fixture until a reference app can exercise the public API. Do not generalize the demo into the product by default.

## First workflow to validate

Use a sample game's existing scene and assets to configure a repeatable encounter. Run it, alter gameplay state, and reset to the configured starting state without restarting the game. Verify the workflow on desktop and in a browser, including that the reset restores the scenario's starting state. Use the same public API in the sample and in a separate-module build check.

## Open questions

- How should a game supply scenario setup and reset logic without duplicating its scene-loading code?
- What inspection or encounter-editing features are needed after the first workflow works?
- Would a standalone launcher or editor add value beyond an in-game playground? Decide after testing the embedded workflow.
