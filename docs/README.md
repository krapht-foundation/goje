# Goje documentation

Goje is a Go game framework and toolchain in development. The project is focused on 2.5D action and real-time strategy games distributed for desktop and web. Start with the [playground guide](playground.md) to run the first playable slice.

The [runtime guide](runtime.md) explains how to step a scene headlessly, launch it on desktop or web, and collect a local diagnostics report. The [M1 performance baseline](runtime-baseline.md) documents how to measure the cost of diagnostics on both hosts.

The [level authoring guide](levels.md) shows how to edit and validate a Goje v1 level.

## What Goje aims to provide

- A game framework and reusable tools for building a playground inside your own game. The first [playground prototype](playground.md#use-the-playground-in-your-game) supports replaying a configured encounter.
- A level builder and tools for creating pixel art and tiles.
- Tools for making sounds and authoring animations, video, and sprites.

The planned framework and editors are not yet available. See [Contributing to Goje](../CONTRIBUTING.md) to discuss the design or submit a change.
