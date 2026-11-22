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
