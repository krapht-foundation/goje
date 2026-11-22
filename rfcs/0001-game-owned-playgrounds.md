# RFC 0001: Game-owned playgrounds

**Status:** The [first workflow](../specs/playground.md) is implemented as an experimental prototype. The broader design remains open.

## Problem

Game developers need a way to assemble a playable test environment from their own scenes, entities, and assets. The original Goje demo exercised one built-in scene on desktop and web but did not give another game a way to create its own playground.

## Proposed direction

| Piece | Purpose |
| --- | --- |
| Developer tooling | Give developers reusable public Go APIs to assemble and run a playground inside their own game. |
| Reference playground | Show how to build one using the same public API available to any game. The example should build when copied into a separate Go module, without importing `internal/` or relying on a Goje checkout. |
| Smoke scenario | Keep a small runnable scenario in this repository to check input, simulation, and rendering on desktop and web. It may share an executable with the reference playground, but its assertions do not belong in the shipped API. |

Goje's runtime and developer tooling must not depend on the example or its test assets. The reference command now exercises the public API and also serves as the smoke fixture.

The [M1 runtime foundation RFC](0002-m1-runtime-foundation.md) proposes moving scene restart into the engine lifecycle. This RFC continues to track the broader playground tooling.

## First workflow to validate

Use a sample game's existing scene and assets to configure a repeatable encounter. Run it, alter gameplay state, and reset to the configured starting state without restarting the game. Verify the workflow on desktop and in a browser, including that the reset restores the scenario's starting state. Use the same public API in the sample and in a separate-module build check.

## Open questions

- The prototype calls the same game-owned scene loader for initialization and reset. Is that sufficient when the scene holds resources that need explicit teardown or asynchronous loading?
- What inspection or encounter-editing features are needed after the first workflow works?
- Would a standalone launcher or editor add value beyond an in-game playground? Decide after testing the embedded workflow.
