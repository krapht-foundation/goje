# Goje documentation

Goje is a Go game framework and toolchain in development. The project is focused on 2.5D action and real-time strategy games distributed for desktop and web. Start with the [playground guide](playground.md) to run the first playable slice.

The [runtime guide](runtime.md) explains how to step a scene headlessly, launch it on desktop or web, and collect a local diagnostics report. The [M1 performance baseline](runtime-baseline.md) documents how to measure the cost of diagnostics on both hosts.

The [level authoring guide](levels.md) shows how to open, edit, and export a Goje v1 level in the browser Level Builder, then validate and play it.

The [brand asset guide](brand/) covers the Goje logo and the Android, iOS, and web icon sets.

## What Goje aims to provide

- A game framework and reusable tools for building a playground inside your own game. The first [playground prototype](playground.md#use-the-playground-in-your-game) supports replaying a configured encounter.
- A [Level Builder](levels.md) for v1 maps and a [Tile Maker](playground.md#draw-a-ground-tile-in-the-browser) for ground art.
- A browser [Sprite Clip Editor](playground.md#author-sprite-clips-in-the-browser) for authoring and previewing clips from existing PNG sprite sheets. Sound, video, and sprite painting tools are planned.

The full framework and remaining editors are still in development. See [Contributing to Goje](../CONTRIBUTING.md) to discuss the design or submit a change.
