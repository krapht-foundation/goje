package driver

import (
	"image"
	"testing"
)

func TestLogicalPointerUsesPresentationViewport(t *testing.T) {
	logical := image.Pt(320, 200)
	tall := image.Pt(800, 503) // Two framebuffer pixels above the viewport, one below.
	wide := image.Pt(1003, 400)
	for _, tc := range []struct {
		name             string
		framebuffer      image.Point
		sourceW, sourceH float64
		x, y             float64
		wantX, wantY     float64
		ok               bool
	}{
		{name: "retina top left", framebuffer: tall, sourceW: 400, sourceH: 251.5, y: 1, ok: true},
		{
			name: "fractional retina position", framebuffer: tall, sourceW: 400, sourceH: 251.5,
			x: 100.625, y: 50.6875, wantX: 80.5, wantY: 39.75, ok: true,
		},
		{name: "extra top pixel", framebuffer: tall, sourceW: 400, sourceH: 251.5, x: 1, y: 0.75},
		{name: "bottom edge is outside", framebuffer: tall, sourceW: 400, sourceH: 251.5, x: 1, y: 251},
		{name: "left bar", framebuffer: wide, sourceW: 1003, sourceH: 400, x: 180.75, y: 30},
		{
			name: "left edge is inside", framebuffer: wide, sourceW: 1003, sourceH: 400, x: 181, y: 30,
			wantY: 15, ok: true,
		},
		{name: "right edge is outside", framebuffer: wide, sourceW: 1003, sourceH: 400, x: 821, y: 30},
		{name: "empty framebuffer", sourceW: 320, sourceH: 200, x: 1, y: 1},
		{name: "empty CSS box", framebuffer: wide, sourceH: 400, x: 1, y: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			viewport := Viewport(tc.framebuffer.X, tc.framebuffer.Y, logical.X, logical.Y)
			x, y, ok := LogicalPointer(
				tc.x,
				tc.y,
				tc.sourceW,
				tc.sourceH,
				tc.framebuffer,
				logical,
				viewport,
			)
			if x != tc.wantX || y != tc.wantY || ok != tc.ok {
				t.Fatalf(
					"LogicalPointer() = (%v, %v, %t); want (%v, %v, %t)",
					x,
					y,
					ok,
					tc.wantX,
					tc.wantY,
					tc.ok,
				)
			}
		})
	}
}
