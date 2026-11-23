# ADR 0004: Keep level data separate from game rules

**Status:** Accepted for M3. See the [M3 RFC](../rfcs/0004-m3-levels-and-unit-commands.md) and [level format](../specs/level-v1.md).

## Decision

Use a bounded, versioned Goje JSON format for the first authoring workflow. The public `level` package reads bytes through an `io.Reader` without owning a filesystem, network source, or graphical host. The validator and runtime use the same decoder. The reference game embeds its authored bytes for both builds and recreates mutable state from the validated level on restart.

Goje owns grid geometry and reusable routing. The game maps placement kinds to visuals and behavior and decides what a move command means. A level file does not refer to executable code or give the decoder authority to open other files.

## Consequences

The authoring and runtime representations are the same in M3, so an edit needs no exporter. An existing editor format would provide authoring tools sooner but introduce layer, asset, and coordinate conventions before Goje has validated a complete workflow. Revisit import and export once real projects need those conventions. The first format does not promise to model every isometric game.
