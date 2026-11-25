# Goje documentation

Goje is a Go game framework and toolchain in development. The project is focused on 2.5D action and real-time strategy games distributed for desktop and web. Start with the [playground guide](playground.md) to run the first playable slice.

The [runtime guide](runtime.md) explains how to step a scene headlessly, launch it on desktop or web, and collect a local diagnostics report. The [M1 performance baseline](runtime-baseline.md) documents how to measure the cost of diagnostics on both hosts.

The [level authoring guide](levels.md) shows how to edit, export, and play a Goje level with collision zones and authored sight.

The [brand asset guide](brand/) covers the Goje logo and the Android, iOS, and web icon sets.

The [collision guide](collision.md) explains how to move bodies against walls, trigger zones, and each other with exact, repeatable arithmetic, and how to build a world from a level. The [playground guide](playground.md#the-skirmish) describes the skirmish, a reference game in which squads of units block each other and fight.

The [text and HUD guide](text-and-ui.md) covers fonts, menus, and the Font Editor. The [replay guide](replays.md) covers local recordings and saved games.

The [audio guide](audio.md) covers overlapping sounds, positional cues, and music. The [vision guide](vision.md) explains line of sight and fog. The [building guide](building.md) covers desktop packages, installable web bundles, and signed updates.

The [online play guide](online.md) explains lockstep, match logs, and the relay. The [playground guide](playground.md#play-the-outpost-online) shows how to host, join, or watch an outpost match.

## What Goje aims to provide

- A game framework and reusable tools for building a playground inside your own game. The first [playground prototype](playground.md#use-the-playground-in-your-game) supports replaying a configured encounter.
- A [Level Builder](levels.md) for maps with collision zones and a [Tile Maker](playground.md#draw-a-ground-tile-in-the-browser) for ground art.
- A browser [Sprite Clip Editor](playground.md#author-sprite-clips-in-the-browser) for authoring and previewing clips from existing PNG sprite sheets, a [Pixel Art Maker](playground.md#draw-pixel-art-in-the-browser) for sprite sheets, and a [Sound Maker](playground.md#make-a-sound-effect-in-the-browser) for short WAV effects. Video tools are planned.

The full framework and remaining editors are still in development. See [Contributing to Goje](../CONTRIBUTING.md) to discuss the design or submit a change.
