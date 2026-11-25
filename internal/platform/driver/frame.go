package driver

import (
	"encoding/binary"
	"image"
	"image/color"
	"math"
)

// VertexStride is the size in bytes of a packed position, UV, and RGBA vertex.
const VertexStride = 20

// Frame holds one painter-ordered batch for either graphics API. Vertices use
// packed little-endian floats and normalized color bytes, so the browser can
// upload the same byte stream the native host uses.
type Frame struct {
	Background color.RGBA
	Vertices   []byte
	imageSize  image.Point
}

func (f *Frame) reset() {
	f.Background = color.RGBA{A: 255}
	f.Vertices = f.Vertices[:0]
}

func (f *Frame) Clear(tint color.NRGBA) { f.Background = color.RGBA(tint) }

func (f *Frame) Rect(x, y, w, h float32, tint color.NRGBA) {
	if w > 0 && h > 0 {
		f.quad(point{x, y}, point{x + w, y}, point{x + w, y + h}, point{x, y + h}, tint, solidUV)
	}
}

func (f *Frame) Line(x1, y1, x2, y2, width float32, tint color.NRGBA) {
	dx, dy := x2-x1, y2-y1
	if width <= 0 || dx == 0 && dy == 0 {
		return
	}
	scale := width / (2 * float32(math.Hypot(float64(dx), float64(dy))))
	x, y := -dy*scale, dx*scale
	f.quad(point{x1 + x, y1 + y}, point{x2 + x, y2 + y},
		point{x2 - x, y2 - y}, point{x1 - x, y1 - y}, tint, solidUV)
}

func (f *Frame) Image(source image.Rectangle, x, y, w, h float32) {
	f.ImageTinted(source, x, y, w, h, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
}

func (f *Frame) ImageTinted(source image.Rectangle, x, y, w, h float32, tint color.NRGBA) {
	if !(w > 0 && h > 0) || source.Empty() || !source.In(image.Rectangle{Max: f.imageSize}) {
		return
	}
	iw, ih := float32(f.imageSize.X), float32(f.imageSize.Y)
	uv := uvRect{
		left: float32(source.Min.X) / iw, right: float32(source.Max.X) / iw,
		top: float32(source.Min.Y) / ih, bottom: float32(source.Max.Y) / ih,
	}
	f.quad(point{x, y}, point{x + w, y}, point{x + w, y + h}, point{x, y + h}, tint, uv)
}

type point struct{ x, y float32 }

type uvRect struct{ left, right, top, bottom float32 }

// The shared shader uses negative U for untextured vertices.
var solidUV = uvRect{left: -1, right: -1}

func (f *Frame) quad(a, b, c, d point, tint color.NRGBA, uv uvRect) {
	f.vertex(a, uv.left, uv.top, tint)
	f.vertex(b, uv.right, uv.top, tint)
	f.vertex(c, uv.right, uv.bottom, tint)
	f.vertex(a, uv.left, uv.top, tint)
	f.vertex(c, uv.right, uv.bottom, tint)
	f.vertex(d, uv.left, uv.bottom, tint)
}

func (f *Frame) vertex(p point, u, v float32, tint color.NRGBA) {
	b := f.Vertices
	b = binary.LittleEndian.AppendUint32(b, math.Float32bits(p.x))
	b = binary.LittleEndian.AppendUint32(b, math.Float32bits(p.y))
	b = binary.LittleEndian.AppendUint32(b, math.Float32bits(u))
	b = binary.LittleEndian.AppendUint32(b, math.Float32bits(v))
	b = append(b, tint.R, tint.G, tint.B, tint.A)
	f.Vertices = b
}
