package engine

// Camera pans and uniformly scales a projected isometric view. CenterX and
// CenterY are unzoomed projected pixels. Width and Height are logical viewport
// pixels. Dimensions and Zoom must be positive, and all fields must be finite.
// The camera does not know about the framebuffer or letterboxing.
type Camera struct {
	CenterX, CenterY float64
	Width, Height    float64
	Zoom             float64
}

// ToLogical maps an unzoomed projected point to logical viewport pixels.
// The camera center always maps to the middle of the viewport.
func (c Camera) ToLogical(x, y float64) (logicalX, logicalY float64) {
	return (x-c.CenterX)*c.Zoom + 0.5*c.Width, (y-c.CenterY)*c.Zoom + 0.5*c.Height
}

// ToProjected undoes ToLogical. It does not clip to the viewport or infer an
// elevation. Hosts reject presses outside the viewport before using this mapping.
func (c Camera) ToProjected(x, y float64) (projectedX, projectedY float64) {
	return (x-0.5*c.Width)/c.Zoom + c.CenterX, (y-0.5*c.Height)/c.Zoom + c.CenterY
}
