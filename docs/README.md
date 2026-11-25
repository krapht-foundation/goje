# Goje documentation

Goje is a Go game framework and toolchain in development. The project is focused on 2.5D action and real-time strategy games distributed for desktop and web. Start with the [playground guide](playground.md) to run the first playable slice.

The [runtime guide](runtime.md) explains how to step a scene headlessly, launch it on desktop or web, and collect a local diagnostics report. The [M1 performance baseline](runtime-baseline.md) documents how to measure the cost of diagnostics on both hosts.

The [level authoring guide](levels.md) shows how to open, edit, and export a Goje level, including its collision zones, in the browser Level Builder, then validate and play it.

The [brand asset guide](brand/) covers the Goje logo and the Android, iOS, and web icon sets.

The [collision guide](collision.md) explains how to move bodies against walls, trigger zones, and each other with exact, repeatable arithmetic, and how to build a world from a level. The [playground guide](playground.md#the-skirmish) describes the skirmish, a reference game in which squads of units block each other and fight.

The [text and HUD guide](text-and-ui.md) shows how to draw text and build menus, and the [replay guide](replays.md) how to record, replay, save, and load a game. The [playground guide](playground.md#the-outpost) describes the outpost, a reference game that uses both together with commands for groups of units.

The [audio guide](audio.md) explains how to play overlapping and positional sounds, and the [vision guide](vision.md) how to give units line of sight and draw fog of war. The [building guide](building.md) shows how to turn a game into a web bundle and a package for macOS or Linux with `goje build` and `goje package`.

## What Goje aims to provide

- A game framework and reusable tools for building a playground inside your own game. The first [playground prototype](playground.md#use-the-playground-in-your-game) supports replaying a configured encounter.
- A [Level Builder](levels.md) for maps with collision zones and a [Tile Maker](playground.md#draw-a-ground-tile-in-the-browser) for ground art.
- A browser [Sprite Clip Editor](playground.md#author-sprite-clips-in-the-browser) for authoring and previewing clips from existing PNG sprite sheets, a [Pixel Art Maker](playground.md#draw-pixel-art-in-the-browser) for sprite sheets, and a [Sound Maker](playground.md#make-a-sound-effect-in-the-browser) for short WAV effects. Video tools are planned.

The full framework and remaining editors are still in development. See [Contributing to Goje](../CONTRIBUTING.md) to discuss the design or submit a change.
