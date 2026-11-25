//go:build cgo && (darwin || linux || windows)

package desktop

import (
	"errors"
	"fmt"
	"image"
	"strings"

	"github.com/go-gl/gl/v3.3-core/gl"

	"github.com/krapht-foundation/goje/internal/platform/driver"
)

// A renderer owns the GL objects for one current context. It keeps the atlas
// and vertex array bound because the desktop host is the only GL caller.
type renderer struct {
	program  uint32
	array    uint32
	buffer   uint32
	atlas    uint32
	capacity int
	viewport image.Rectangle
}

func newRenderer(config driver.Config) (*renderer, error) {
	texture := config.Texture
	width, height := texture.Rect.Dx(), texture.Rect.Dy()
	var limit int32
	gl.GetIntegerv(gl.MAX_TEXTURE_SIZE, &limit)
	if width > int(limit) || height > int(limit) {
		return nil, fmt.Errorf("upload OpenGL atlas: %dx%d exceeds the texture limit %d",
			width, height, limit)
	}

	program, err := linkProgram()
	if err != nil {
		return nil, err
	}
	r := &renderer{program: program}
	gl.UseProgram(r.program)
	gl.Uniform2f(gl.GetUniformLocation(r.program, gl.Str("logicalSize\x00")),
		float32(config.Width), float32(config.Height))
	gl.Uniform1i(gl.GetUniformLocation(r.program, gl.Str("atlas\x00")), 0)

	gl.GenVertexArrays(1, &r.array)
	gl.BindVertexArray(r.array)
	gl.GenBuffers(1, &r.buffer)
	gl.BindBuffer(gl.ARRAY_BUFFER, r.buffer)
	gl.VertexAttribPointerWithOffset(0, 2, gl.FLOAT, false, driver.VertexStride, 0)
	gl.VertexAttribPointerWithOffset(1, 2, gl.FLOAT, false, driver.VertexStride, 8)
	gl.VertexAttribPointerWithOffset(2, 4, gl.UNSIGNED_BYTE, true, driver.VertexStride, 16)
	gl.EnableVertexAttribArray(0)
	gl.EnableVertexAttribArray(1)
	gl.EnableVertexAttribArray(2)

	gl.ActiveTexture(gl.TEXTURE0)
	gl.GenTextures(1, &r.atlas)
	gl.BindTexture(gl.TEXTURE_2D, r.atlas)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.NEAREST)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.NEAREST)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE)
	uploadStart := driver.BenchUploadStart()
	//nolint:gosec // G115: texture dimensions were checked against the GL int32 limit above.
	gl.TexImage2D(gl.TEXTURE_2D, 0, gl.RGBA8, int32(width), int32(height), 0,
		gl.RGBA, gl.UNSIGNED_BYTE, gl.Ptr(&texture.Pix[0]))
	driver.BenchUploadEnd(uploadStart)

	gl.Enable(gl.BLEND)
	gl.BlendFuncSeparate(gl.SRC_ALPHA, gl.ONE_MINUS_SRC_ALPHA, gl.ONE, gl.ONE_MINUS_SRC_ALPHA)
	if err := graphicsError("configure OpenGL renderer"); err != nil {
		r.close()
		return nil, err
	}
	return r, nil
}

func (r *renderer) close() {
	gl.DeleteTextures(1, &r.atlas)
	gl.DeleteVertexArrays(1, &r.array)
	gl.DeleteBuffers(1, &r.buffer)
	gl.UseProgram(0)
	gl.DeleteProgram(r.program)
}

// draw submits a complete back buffer. Buffer swapping is the host's job, so
// a test can inspect this buffer with its own GL readback before presenting it.
func (r *renderer) draw(frame *driver.Frame, viewport image.Rectangle) error {
	if r.viewport != viewport {
		r.viewport = viewport
		//nolint:gosec // G115: native framebuffer dimensions come from GLFW C int values.
		x, y := int32(viewport.Min.X), int32(viewport.Min.Y)
		//nolint:gosec // G115: the viewport dimensions are bounded by the framebuffer.
		width, height := int32(viewport.Dx()), int32(viewport.Dy())
		gl.Viewport(x, y, width, height)
		gl.Scissor(x, y, width, height)
	}

	// A viewport does not limit Clear; scissor only the second clear so the
	// drawable around it always contains black bars, even after a resize.
	gl.ClearColor(0, 0, 0, 1)
	gl.Clear(gl.COLOR_BUFFER_BIT)
	gl.Enable(gl.SCISSOR_TEST)
	background := frame.Background
	const channel = 1.0 / 255
	gl.ClearColor(float32(background.R)*channel, float32(background.G)*channel,
		float32(background.B)*channel, float32(background.A)*channel)
	gl.Clear(gl.COLOR_BUFFER_BIT)
	gl.Disable(gl.SCISSOR_TEST)

	if size := len(frame.Vertices); size != 0 {
		count := size / driver.VertexStride
		if count > 1<<31-1 {
			return errors.New("OpenGL frame exceeds the vertex count limit")
		}
		if size > r.capacity {
			r.capacity = max(size, 2*r.capacity)
			gl.BufferData(gl.ARRAY_BUFFER, r.capacity, nil, gl.STREAM_DRAW)
		}
		gl.BufferSubData(gl.ARRAY_BUFFER, 0, size, gl.Ptr(&frame.Vertices[0]))
		gl.DrawArrays(gl.TRIANGLES, 0, int32(count))
	}
	return graphicsError("draw OpenGL frame")
}

func graphicsError(operation string) error {
	switch code := gl.GetError(); code {
	case gl.NO_ERROR:
		return nil
	case gl.CONTEXT_LOST:
		return fmt.Errorf("%s: graphics context lost", operation)
	default:
		return fmt.Errorf("%s: OpenGL error %#x", operation, code)
	}
}

func linkProgram() (uint32, error) {
	vertex, err := compileShader(gl.VERTEX_SHADER, driver.VertexShader)
	if err != nil {
		return 0, fmt.Errorf("compile OpenGL vertex shader: %w", err)
	}
	defer gl.DeleteShader(vertex)
	fragment, err := compileShader(gl.FRAGMENT_SHADER, driver.FragmentShader)
	if err != nil {
		return 0, fmt.Errorf("compile OpenGL fragment shader: %w", err)
	}
	defer gl.DeleteShader(fragment)

	program := gl.CreateProgram()
	gl.AttachShader(program, vertex)
	gl.AttachShader(program, fragment)
	gl.LinkProgram(program)
	var status int32
	gl.GetProgramiv(program, gl.LINK_STATUS, &status)
	if status == gl.FALSE {
		var size int32
		gl.GetProgramiv(program, gl.INFO_LOG_LENGTH, &size)
		log := make([]byte, max(size, 1))
		gl.GetProgramInfoLog(program, max(size, 1), nil, &log[0])
		gl.DeleteProgram(program)
		return 0, fmt.Errorf("link OpenGL program: %w", shaderError(log))
	}
	gl.DetachShader(program, vertex)
	gl.DetachShader(program, fragment)
	return program, nil
}

func compileShader(kind uint32, source string) (uint32, error) {
	shader := gl.CreateShader(kind)
	text, freeText := gl.Strs("#version 330 core\n" + source + "\x00")
	gl.ShaderSource(shader, 1, text, nil)
	freeText()
	gl.CompileShader(shader)
	var status int32
	gl.GetShaderiv(shader, gl.COMPILE_STATUS, &status)
	if status == gl.FALSE {
		var size int32
		gl.GetShaderiv(shader, gl.INFO_LOG_LENGTH, &size)
		log := make([]byte, max(size, 1))
		gl.GetShaderInfoLog(shader, max(size, 1), nil, &log[0])
		gl.DeleteShader(shader)
		return 0, shaderError(log)
	}
	return shader, nil
}

func shaderError(log []byte) error {
	message := strings.TrimSpace(strings.TrimRight(string(log), "\x00"))
	if message == "" {
		return errors.New("driver did not provide a shader log")
	}
	return errors.New(message)
}
