package driver

// VertexShader converts logical coordinates to clip space, with y increasing
// downwards. Each host supplies its GLSL version prefix.
const VertexShader = `
layout (location = 0) in vec2 position;
layout (location = 1) in vec2 uv;
layout (location = 2) in vec4 color;
uniform vec2 logicalSize;
out vec2 texcoord;
out vec4 tint;
void main() {
    vec2 logical = position / logicalSize;
    gl_Position = vec4(logical.x * 2.0 - 1.0, 1.0 - logical.y * 2.0, 0.0, 1.0);
    texcoord = uv;
    tint = color;
}`

// FragmentShader uses negative U for solid geometry. Keeping that flag in the
// vertex stream allows shapes and sprites to share one painter-ordered draw.
const FragmentShader = `
in vec2 texcoord;
in vec4 tint;
uniform sampler2D atlas;
out vec4 pixel;
void main() {
    pixel = texcoord.x < 0.0 ? tint : tint * texture(atlas, texcoord);
}`
