# RFC 0008: M7 — Game projects and reusable playgrounds

**Status:** Implemented and verified for M7. This milestone builds on the [M6 editor-to-playground round trip](0007-m6-tile-maker.md#acceptance-results) and continues [game-owned playgrounds](0001-game-owned-playgrounds.md).

## Problem

The Level Builder and Tile Maker produce assets that already run on desktop and web. The [current workflow](../docs/levels.md#play-a-level-edit) pairs those files by copying the reference command and replacing its hard-coded files. That proves the formats work, but it does not let a developer assemble a playground without inheriting the courtyard game.

## Goal

An independent Go game can pair its authored level and ground tile in a project directory, validate the pair before launch, and use the same project and game code on desktop and web. The game still owns what placements mean, how the scene behaves, and when to reset it.

## Selected direction

| Decision | Selected | Alternative and tradeoff |
| --- | --- | --- |
| Project shape | One versioned `goje.json` naming one required level and one required ground tile. | An asset catalog or multi-scene manifest would model more games, but neither is needed to prove this handoff. |
| Delivery | Use an ordinary directory for authoring and `embed.FS` for the first portable launch workflow. Let the calling game choose its filesystem. | A ZIP, browser file picker, or URL loader could support swapping projects after build, but adds a separate import and security model. |
| Playground API | Add a shared public `project` loader. Assemble the playground with the existing scene and host APIs; do not add a second launcher API. | A new `playground.Run` could reduce boilerplate if multiple games need the same setup, but would hide ownership before that common setup is known. |

## Project format and validation

The [Goje project v1 specification](../specs/project-v1.md) owns the format and filesystem boundary. The `projectcheck PROJECT_DIR` command validates a user-selected directory using the same loader as games. It reports project-format errors without applying courtyard rules; the existing `levelcheck` remains courtyard-specific.

## Ownership and trust boundary

The public `project` package loads from a caller-supplied, already rooted `fs.FS`. It validates all references before opening assets and returns the decoded level and tile only after both succeed. It does not discover directories, fetch URLs, or run game rules. Open only the manifest and its two referenced regular files, using bounded reads; do not recursively enumerate the project. The filesystem implementation is trusted to enforce its boundary: `embed.FS` is immutable and the native command uses `os.Root`, not `os.DirFS`, for untrusted project directories. In-root relative symlinks are allowed by the native adapter; escaping and absolute symlinks are rejected. Native validation does not isolate hostile mounts or promise a consistent snapshot of a directory being modified concurrently. Web launches use embedded files and do not rely on a browser filesystem emulation of `os.Root`.

The calling game owns startup and receives CPU-side data. It validates placement meaning, prepares the existing startup atlas, and supplies its own scene factory and host config to `platform.Run`. The host uploads that atlas before scene entry as it does today. Reset creates fresh mutable scene state from the loaded project; it does not reopen files, decode images, or re-upload the atlas. No file reads or allocations are added to the frame path by this workflow.

The repository's authored courtyard continues to be a reference game and regression fixture. Migrate its startup to the same loader and keep its existing art, levels, and game rules. The editor exports remain standalone files; neither editor needs to know about the manifest for M7.

## Delivery and acceptance

| Slice | Exit check |
| --- | --- |
| 1. Manifest and validator | Write the versioned format spec on acceptance, add the shared loader and `projectcheck`, and verify a valid project plus malformed, duplicate, and oversized manifests; unsafe paths; escaping symlinks on native; missing or non-regular files; and invalid level or tile contents. Verify that the shared loader works against an embedded filesystem in Wasm. |
| 2. Reference integration | Load the authored courtyard through the project manifest. Re-run the ordinary detour and the M6 exact-download shortcut on native OpenGL and Chromium WebGL. Confirm that changing the manifest's paths actually selects the new files and that reset reads nothing from the project filesystem. |
| 3. Independent playground | Build and run a small, independently written Go game from a separate module, using only public Goje packages and its own editor-exported assets and placement rule. Prove the same project renders on desktop and web, game input changes scene state, and reset restores it without rereading assets. Document the validate/build/run workflow without copying `cmd/playground` or importing `examples/courtyard`. |

## Acceptance results

The [shared project loader](../project/project.go) and [validator](../docs/levels.md#validate-a-project-from-the-command-line) are implemented. Native checks exercised a valid project, symlink confinement, and rejection of a named pipe. The loader tests also ran under Go's JavaScript/Wasm runner against an embedded project.

The reference courtyard now loads its embedded [manifest](../cmd/playground/goje.json) before opening the host. The ordinary detour and exact-download shortcut passed on macOS OpenGL and Chromium WebGL. The round trip selected the new export paths while leaving the original fixtures present. A headless test verified that running and resetting the scene did not reopen the project filesystem.

[`make smoke-beacon`](../docs/playground.md#use-the-playground-in-your-game) built the top-down 2D Beacon game in a separate Go module using only public Goje packages. The editors moved its game-defined `beacon` placement and drew its ground texture. `projectcheck` validated those exact downloads before the same project was embedded in both builds. Native OpenGL presented frames while scripted input toggled and reset the beacon. Chromium WebGL pixel checks verified the edited placement, ground accent, transparent pixels, wall, pointer and keyboard input, and reset. The scene factory retains decoded scene data, not the project filesystem.

## Out of scope

Project archives, remote loading, hot reload, editor integration, multi-scene catalogs, custom asset types, changes to level semantics, standalone launchers, and a new rendering or asset lifecycle API.
