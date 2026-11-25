# goje

![Goje logo: a pixel tomato mascot looking toward the GOJE wordmark](docs/brand/goje.svg)

Goje is a Go game framework and toolchain for desktop and web. It supports 2D and aims to make 2.5D action and RTS-style games especially practical. Goje (/ɡoʊˈdʒe/, roughly “go-JEH”) means “tomato” in Persian (گوجه).

The reference [playground](docs/playground.md) runs an isometric courtyard on desktop and web. Use the browser [Level Builder](docs/levels.md#edit-a-level-in-the-browser) to open a shortcut, the [Tile Maker](docs/playground.md#draw-a-ground-tile-in-the-browser) to draw new ground art, the [Pixel Art Maker](docs/playground.md#draw-pixel-art-in-the-browser) to paint sprite sheets, the [Sprite Clip Editor](docs/playground.md#author-sprite-clips-in-the-browser) to author and preview animations, or the [Sound Maker](docs/playground.md#make-a-sound-effect-in-the-browser) to make short sound effects. The full engine and remaining editors are still in development.

To move characters against walls, trigger zones, and each other, see the [collision guide](docs/collision.md). The [skirmish](docs/playground.md#the-skirmish) shows squads that block each other, fight with projectiles, and wake on a trigger placed in the Level Builder. The [outpost](docs/playground.md#the-outpost) adds a menu and HUD with [text](docs/text-and-ui.md), commands for groups of units, and [recordings and saved games](docs/replays.md), with [fog of war](docs/vision.md), [positional sound](docs/audio.md), and touch and gamepad control. Two players can also command the outpost together over the network; see [online play](docs/online.md). To ship a game, see [building and packaging](docs/building.md).

- [Contributing](CONTRIBUTING.md)
- [Security policy](SECURITY.md)
- [Code of conduct](CODE_OF_CONDUCTS.md)
- [Agent instructions](AGENTS.md) and [workspace map](.ai/README.md)
