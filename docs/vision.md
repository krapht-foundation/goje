# See and hide in your game

Goje's vision package answers one question: which cells can a unit standing in this cell see? It uses integer arithmetic, so a game gets the same answer on desktop and in the browser, and the answer is symmetric: if a unit in one open cell sees another open cell, a unit there sees it back. It does not draw fog, remember what a team has seen, or decide what blocks sight; your game does. See the [behavior specification](../specs/vision-m15.md) for the full rules.

## Cast from one cell

`vision.Cast` calls a function for each cell in line of sight within a radius, counted as `dx² + dy² ≤ radius²`. The origin comes first, and a blocking cell is reported when sight reaches it, as a wall is seen from in front. Cells outside your grid must count as blocking; `Cast` may report them, so ignore them in `visit`.

```go
walls := func(x, y int) bool {
	return x < 0 || y < 0 || x >= width || y >= height || !board.Walkable(x, y)
}
vision.Cast(grid.Cell{X: 4, Y: 2}, 7, walls, func(x, y int) {
	if x >= 0 && y >= 0 && x < width && y < height {
		lit[y*width+x] = true
	}
})
```

`Cast` allocates nothing, and the order of its calls is fixed, so a game can fold the result into a replay hash.

## Keep a team's sight

A `vision.Field` counts how many viewers see each cell and remembers what each viewer saw, so moving a viewer costs work only when its cell changes. Size it once, give each unit a viewer number, and move the viewer whenever the unit may have moved.

```go
field, err := vision.NewField(width, height, len(units), 7, func(x, y int) bool {
	return !board.Walkable(x, y) // Called only for cells on the board.
})
if err != nil {
	return err
}
for i, u := range units {
	if u.Alive {
		field.Move(i, u.Cell) // Does nothing if the cell did not change.
	} else {
		field.Remove(i)
	}
}
if field.Visible(x, y) {
	// Draw the enemy standing in (x, y).
}
```

`Move` reports whether the viewer's sight changed, and `Cells` lists what it now sees, which is how a game keeps an explored set that only grows. A Field never allocates after `NewField`.

## Fog in the skirmish

The [skirmish simulation](../specs/skirmish-m13.md) uses a Field per team when a game calls `EnableVision` before the first `Step`:

```go
sim, err := skirmish.New(board)
if err != nil {
	return err
}
if err := sim.EnableVision(skirmish.VisionConfig{Radius: 7}); err != nil {
	return err
}
```

From then on, only walls block sight; a raised deck and other zones do not. Units fire only at enemies their team sees, an attack-move walks past a hidden enemy, an attack order needs its target in sight and gives up at the cell where it lost it, and guards wake when their team sees a scout. The alarm still wakes every guard, who then hunt without needing sight. To draw fog, ask `sim.Explored(cell)` whether the scouts have ever seen a cell and `sim.Visible(skirmish.Scouts, cell)` whether they see it now, and draw guards only in visible cells. The explored set is part of the trace and of the snapshot, so a save restores it, and sight is recomputed from the restored units. Without `EnableVision`, every cell is visible and explored, and the simulation behaves exactly as before.

## Hear what happened

`sim.Events()` lists what happened during the last `Step`: bolts fired, bolts that hurt a unit, units that fell, and the alarm. Each event names a unit and its ground position, so a scene can turn it into a sound placed where it happened. Events are output only: reading them never changes the match, and they are not saved or hashed. The slice is reused by the next `Step`.

```go
for _, e := range sim.Events() {
	switch e.Kind {
	case skirmish.EventBoltFired:
		// Play the bolt sound from e.At.
	case skirmish.EventAlarm:
		// Play the alarm.
	}
}
```

## Cost

On the 64 × 64 benchmark board with 256 units and squad orders, vision at a radius of 7 adds about 0.15 milliseconds to a median tick of about one millisecond on an Apple M-series machine, and the worst tick stayed under 4 milliseconds natively and under 8 milliseconds in Node WebAssembly. Run `make benchmark-skirmish` for the `vision` report on your machine; the [specification](../specs/vision-m15.md#measurements) records the measured values.

## Limits

Sight is flat: height does not change what a unit sees, and only cells block sight, not units. The guards' team does not remember what it has seen, and an enemy that leaves sight is not remembered, except as the cell where an attack order last saw its target. A radius is at most 181 cells, and a Field covers at most 128 × 128 cells.
