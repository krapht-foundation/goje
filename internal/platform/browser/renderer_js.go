//go:build wasm

package browser

import (
	"fmt"
	"image"
	"math"
	"syscall/js"

	"github.com/krapht-foundation/goje/internal/platform/driver"
)

// WebGL enums used while presenting are constants to avoid repeated JS property reads.
const (
	arrayBuffer    = 0x8892
	colorBufferBit = 0x4000
	contextLost    = 0x9242
	scissorTest    = 0x0C11
	streamDraw     = 0x88E0
	triangles      = 0x0004
)

type renderer struct {
	gl, program, vao, buffer, texture js.Value
	byteArray, staging                js.Value
	capacity                          int
	logicalW, logicalH                int
	backingW, backingH                int
	framebuffer                       image.Point
	viewport                          image.Rectangle
}

func newRenderer(gl js.Value, config driver.Config) (_ *renderer, err error) {
	if gl.Call("isContextLost").Bool() {
		return nil, errContextLost
	}
	r := &renderer{
		gl: gl, byteArray: js.Global().Get("Uint8Array"),
		logicalW: config.Width, logicalH: config.Height,
	}
	defer func() {
		if err != nil {
			r.close()
		}
	}()

	vertex, err := compile(
		gl,
		"vertex",
		gl.Get("VERTEX_SHADER"),
		"#version 300 es\n"+driver.VertexShader,
	)
	if err != nil {
		return nil, err
	}
	defer gl.Call("deleteShader", vertex)
	fragment, err := compile(
		gl,
		"fragment",
		gl.Get("FRAGMENT_SHADER"),
		"#version 300 es\nprecision highp float;\n"+driver.FragmentShader,
	)
	if err != nil {
		return nil, err
	}
	defer gl.Call("deleteShader", fragment)
	r.program = gl.Call("createProgram")
	if !r.program.Truthy() {
		return nil, graphicsError(gl, "create shader program")
	}
	gl.Call("attachShader", r.program, vertex)
	gl.Call("attachShader", r.program, fragment)
	gl.Call("linkProgram", r.program)
	gl.Call("detachShader", r.program, vertex)
	gl.Call("detachShader", r.program, fragment)
	if !gl.Call("getProgramParameter", r.program, gl.Get("LINK_STATUS")).Truthy() {
		return nil, graphicsError(
			gl,
			"link shader program: "+gl.Call("getProgramInfoLog", r.program).String(),
		)
	}
	gl.Call("useProgram", r.program)
	gl.Call(
		"uniform2f",
		gl.Call("getUniformLocation", r.program, "logicalSize"),
		config.Width,
		config.Height,
	)
	gl.Call("uniform1i", gl.Call("getUniformLocation", r.program, "atlas"), 0)

	r.vao = gl.Call("createVertexArray")
	r.buffer = gl.Call("createBuffer")
	if !r.vao.Truthy() || !r.buffer.Truthy() {
		return nil, graphicsError(gl, "create vertex batch buffers")
	}
	gl.Call("bindVertexArray", r.vao)
	gl.Call("bindBuffer", arrayBuffer, r.buffer)
	for i, components := range [...]int{2, 2, 4} {
		kind, offset := gl.Get("FLOAT"), i*8
		if i == 2 {
			kind = gl.Get("UNSIGNED_BYTE")
		}
		gl.Call("enableVertexAttribArray", i)
		gl.Call("vertexAttribPointer", i, components, kind, i == 2, driver.VertexStride, offset)
	}

	img := config.Texture
	w, h := img.Rect.Dx(), img.Rect.Dy()
	maxTextureSize := gl.Call("getParameter", gl.Get("MAX_TEXTURE_SIZE"))
	if !maxTextureSize.Truthy() {
		return nil, graphicsError(gl, "read texture size limit")
	}
	if limit := maxTextureSize.Int(); w > limit || h > limit {
		return nil, fmt.Errorf(
			"browser: image %d×%d exceeds the WebGL 2 texture size limit of %d",
			w,
			h,
			limit,
		)
	}
	r.texture = gl.Call("createTexture")
	if !r.texture.Truthy() {
		return nil, graphicsError(gl, "create texture")
	}
	gl.Call("activeTexture", gl.Get("TEXTURE0"))
	target := gl.Get("TEXTURE_2D")
	gl.Call("bindTexture", target, r.texture)
	nearest, clamp := gl.Get("NEAREST"), gl.Get("CLAMP_TO_EDGE")
	gl.Call("texParameteri", target, gl.Get("TEXTURE_MIN_FILTER"), nearest)
	gl.Call("texParameteri", target, gl.Get("TEXTURE_MAG_FILTER"), nearest)
	gl.Call("texParameteri", target, gl.Get("TEXTURE_WRAP_S"), clamp)
	gl.Call("texParameteri", target, gl.Get("TEXTURE_WRAP_T"), clamp)
	pixels := img.Pix[:4*w*h]
	upload := r.byteArray.New(len(pixels))
	js.CopyBytesToJS(upload, pixels)
	gl.Call(
		"texImage2D",
		target,
		0,
		gl.Get("RGBA8"),
		w,
		h,
		0,
		gl.Get("RGBA"),
		gl.Get("UNSIGNED_BYTE"),
		upload,
	)
	gl.Call("enable", gl.Get("BLEND"))
	gl.Call("blendFunc", gl.Get("SRC_ALPHA"), gl.Get("ONE_MINUS_SRC_ALPHA"))
	if code := gl.Call("getError").Int(); code != 0 {
		return nil, webGLError("initialize renderer", code)
	}
	return r, nil
}

func compile(gl js.Value, stage string, kind js.Value, source string) (js.Value, error) {
	shader := gl.Call("createShader", kind)
	if !shader.Truthy() {
		return js.Undefined(), graphicsError(gl, "create "+stage+" shader")
	}
	gl.Call("shaderSource", shader, source)
	gl.Call("compileShader", shader)
	if !gl.Call("getShaderParameter", shader, gl.Get("COMPILE_STATUS")).Truthy() {
		message := gl.Call("getShaderInfoLog", shader).String()
		gl.Call("deleteShader", shader)
		return js.Undefined(), graphicsError(gl, "compile "+stage+" shader: "+message)
	}
	return shader, nil
}

func graphicsError(gl js.Value, operation string) error {
	if gl.Call("isContextLost").Bool() {
		return errContextLost
	}
	return fmt.Errorf("browser: WebGL 2 cannot %s", operation)
}

func webGLError(operation string, code int) error {
	if code == contextLost {
		return errContextLost
	}
	return fmt.Errorf("browser: WebGL 2 %s: error 0x%04x", operation, code)
}

func (r *renderer) close() {
	if r.vao.Truthy() {
		r.gl.Call("deleteVertexArray", r.vao)
	}
	if r.buffer.Truthy() {
		r.gl.Call("deleteBuffer", r.buffer)
	}
	if r.texture.Truthy() {
		r.gl.Call("deleteTexture", r.texture)
	}
	if r.program.Truthy() {
		// A current WebGL program is only destroyed after it is unbound.
		r.gl.Call("useProgram", js.Null())
		r.gl.Call("deleteProgram", r.program)
	}
}

func (r *renderer) present(window, canvas js.Value, frame *driver.Frame) error {
	dpr := window.Get("devicePixelRatio").Float()
	w := int(math.Round(float64(canvas.Get("clientWidth").Int()) * dpr))
	h := int(math.Round(float64(canvas.Get("clientHeight").Int()) * dpr))
	if w <= 0 || h <= 0 {
		return nil
	}
	gl := r.gl
	if r.backingW != w || r.backingH != h {
		r.backingW, r.backingH = w, h
		// Setting a dimension resets the contents, even to the same value.
		if canvas.Get("width").Int() != w {
			canvas.Set("width", w)
		}
		if canvas.Get("height").Int() != h {
			canvas.Set("height", h)
		}
		r.framebuffer = image.Pt(
			gl.Get("drawingBufferWidth").Int(),
			gl.Get("drawingBufferHeight").Int(),
		)
		r.viewport = driver.Viewport(r.framebuffer.X, r.framebuffer.Y, r.logicalW, r.logicalH)
		v := r.viewport
		gl.Call("viewport", v.Min.X, v.Min.Y, v.Dx(), v.Dy())
		gl.Call("scissor", v.Min.X, v.Min.Y, v.Dx(), v.Dy())
	}
	if r.viewport.Empty() {
		return nil
	}
	gl.Call("clearColor", 0, 0, 0, 1)
	gl.Call("clear", colorBufferBit)
	background := frame.Background
	gl.Call("enable", scissorTest)
	gl.Call(
		"clearColor",
		float64(background.R)/255,
		float64(background.G)/255,
		float64(background.B)/255,
		float64(background.A)/255,
	)
	gl.Call("clear", colorBufferBit)
	gl.Call("disable", scissorTest)

	if n := len(frame.Vertices); n != 0 {
		// Grow CPU staging and GPU storage together. A steady frame copies once
		// and uploads only its live bytes, without creating a JS subarray view.
		if n > r.capacity {
			r.capacity = max(n, 2*r.capacity)
			gl.Call("bufferData", arrayBuffer, r.capacity, streamDraw)
			if code := gl.Call("getError").Int(); code != 0 {
				return webGLError("allocate vertex storage", code)
			}
			r.staging = r.byteArray.New(r.capacity)
		}
		js.CopyBytesToJS(r.staging, frame.Vertices)
		gl.Call("bufferSubData", arrayBuffer, 0, r.staging, 0, n)
		gl.Call("drawArrays", triangles, 0, n/driver.VertexStride)
	}
	return nil
}

func pinIntrinsicSize(canvas js.Value) {
	if canvas.Get("computedStyleMap").Type() != js.TypeFunction {
		return
	}
	styles := canvas.Call("computedStyleMap")
	if styles.Call("get", "width").Get("value").String() != "auto" ||
		styles.Call("get", "height").Get("value").String() != "auto" {
		return
	}
	// If the canvas uses its intrinsic size, changes to its backing dimensions
	// would otherwise feed back into the next client size calculation on HiDPI.
	style := canvas.Get("style")
	style.Set("width", fmt.Sprintf("%dpx", canvas.Get("width").Int()))
	style.Set("height", fmt.Sprintf("%dpx", canvas.Get("height").Int()))
}
