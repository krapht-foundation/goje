---
name: goje-content-toolchain
description: Design or change project files, asset import and export, editor workflows, or runtime integration for Goje levels, tiles, pixel art, sound, animation, video, and sprites.
---

# Content tools and assets

1. Identify authoring data and runtime artifacts. Record who owns the format and which readers and writers change. Define units and defaults for relevant fields such as tile origin, sprite pivot, frame time, or audio sample rate.
2. Preserve user work. Separate transient UI state from saved content; define edit, undo, redo, and save boundaries. If an existing format changes, document compatibility and migration. On failed save or migration, leave the original readable.
3. Make exports reproducible for identical authored inputs and settings wherever the format allows it. Use stable ordering and portable relative asset references. Treat imported content as untrusted and load the `goje-security` skill for filesystem, decoder, and resource limits.
4. Verify an editor-to-engine round trip using a small meaningful asset: author, save, reopen, export, and load in the runtime. Check what the player sees or hears, not just that the bytes parse. Exercise older assets if compatibility is promised and report the desktop/web paths verified.
