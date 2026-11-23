package engine

// Isometric projects a square ground grid onto a diamond grid. HalfTileWidth
// and HalfTileHeight are the positive screen-pixel half-extents of a tile.
type Isometric struct {
	HalfTileWidth  float64
	HalfTileHeight float64
}

// Project converts ground coordinates in tiles and elevation in screen pixels
// to offsets from the screen origin. Positive elevation moves the result up.
func (p Isometric) Project(x, y, elevation float64) (screenX, screenY float64) {
	return (x - y) * p.HalfTileWidth, (x+y)*p.HalfTileHeight - elevation
}

// Unproject maps projected pixels back to tile coordinates on the given
// elevation plane. Pass zero to find the ground point; the projection alone
// cannot tell which elevation produced the input point.
func (p Isometric) Unproject(projectedX, projectedY, elevation float64) (x, y float64) {
	difference := projectedX / p.HalfTileWidth
	sum := (projectedY + elevation) / p.HalfTileHeight
	return 0.5*sum + 0.5*difference, 0.5*sum - 0.5*difference
}
