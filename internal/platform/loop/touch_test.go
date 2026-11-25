package loop_test

import (
	"math"
	"testing"

	"github.com/krapht-foundation/goje/engine"
	"github.com/krapht-foundation/goje/internal/platform/loop"
)

func touch(id uint32, x, y float64) engine.Touch {
	return engine.Touch{ID: id, X: x, Y: y, Active: true}
}

func TestFirstFingerDrivesThePointer(t *testing.T) {
	var host loop.Loop
	host.TouchDown(40, 10, 20)
	want := engine.Input{
		Pointer:      engine.Pointer{X: 10, Y: 20, Inside: true},
		PrimaryPress: engine.MousePress{X: 10, Y: 20, Valid: true},
		PrimaryHeld:  true,
	}
	want.Touches[0] = touch(1, 10, 20)
	if got := host.NextInput(); got != want {
		t.Fatalf("landing = %+v; want %+v", got, want)
	}

	host.TouchMove(40, 30, 25, true)
	want.PrimaryPress = engine.MousePress{}
	want.Pointer = engine.Pointer{X: 30, Y: 25, Inside: true}
	want.Touches[0] = touch(1, 30, 25)
	if got := host.NextInput(); got != want {
		t.Fatalf("drag = %+v; want %+v", got, want)
	}

	// Outside the viewport the pointer leaves, and the touch and the release
	// keep the last position inside.
	host.TouchMove(40, 500, 25, false)
	host.TouchUp(40)
	want = engine.Input{PrimaryRelease: engine.MousePress{X: 30, Y: 25, Valid: true}}
	if got := host.NextInput(); got != want {
		t.Fatalf("lift = %+v; want %+v", got, want)
	}
	if got := host.NextInput(); got != (engine.Input{}) {
		t.Fatalf("after lift = %+v; want zero", got)
	}
}

func TestTapBetweenTicksKeepsBothEdges(t *testing.T) {
	var host loop.Loop
	host.TouchDown(7, 5, 6)
	host.TouchUp(7)
	want := engine.Input{
		Pointer:        engine.Pointer{X: 5, Y: 6, Inside: true},
		PrimaryPress:   engine.MousePress{X: 5, Y: 6, Valid: true},
		PrimaryRelease: engine.MousePress{X: 5, Y: 6, Valid: true},
	}
	if got := host.NextInput(); got != want {
		t.Fatalf("tap = %+v; want %+v", got, want)
	}
}

func TestSecondToSixthFingers(t *testing.T) {
	var host loop.Loop
	host.TouchDown(100, 1, 1)
	host.NextInput()
	host.TouchDown(200, 2, 2)
	// A repeated landing of a tracked finger is ignored.
	host.TouchDown(200, 9, 9)
	host.TouchMove(200, 3, 3, true)
	got := host.NextInput()
	if got.Pointer != (engine.Pointer{X: 1, Y: 1, Inside: true}) || got.PrimaryPress.Valid ||
		!got.PrimaryHeld {
		t.Fatalf("second finger moved the pointer or pressed: %+v", got)
	}
	if got.Touches[0] != touch(1, 1, 1) || got.Touches[1] != touch(2, 3, 3) {
		t.Fatalf("touches = %+v", got.Touches)
	}
	for i := range 4 {
		host.TouchDown(300+i, float64(10+i), 0)
	}
	got = host.NextInput()
	for i, want := range [engine.MaxTouches]engine.Touch{
		touch(1, 1, 1), touch(2, 3, 3), touch(3, 10, 0), touch(4, 11, 0), touch(5, 12, 0),
	} {
		if got.Touches[i] != want {
			t.Fatalf("touch %d = %+v; want %+v (sixth finger must be ignored)", i, got.Touches[i], want)
		}
	}
	// The ignored sixth finger's motion and lifting do nothing.
	host.TouchMove(303, 50, 50, true)
	host.TouchUp(303)
	if again := host.NextInput(); again != got {
		t.Fatalf("sixth finger changed input: %+v", again)
	}
	// A finger in the middle lifts: the rest close the gap in landing order.
	host.TouchUp(300)
	got = host.NextInput()
	if got.Touches[2] != touch(4, 11, 0) || got.Touches[3] != touch(5, 12, 0) ||
		got.Touches[4] != (engine.Touch{}) || got.PrimaryRelease.Valid {
		t.Fatalf("after middle lift: %+v", got)
	}
}

func TestFirstFingerLiftsWhileOthersAreDown(t *testing.T) {
	var host loop.Loop
	host.TouchDown(1, 10, 10)
	host.TouchDown(2, 20, 20)
	host.NextInput()
	host.TouchUp(1)
	got := host.NextInput()
	if got.PrimaryRelease != (engine.MousePress{X: 10, Y: 10, Valid: true}) || got.PrimaryHeld {
		t.Fatalf("first lift = %+v; want a release at its last position", got)
	}
	if got.Touches[0] != touch(2, 20, 20) || got.Touches[1].Active {
		t.Fatalf("touches = %+v", got.Touches)
	}
	// The remaining finger and new ones drive nothing until all have lifted.
	host.TouchMove(2, 25, 25, true)
	host.TouchDown(3, 30, 30)
	got = host.NextInput()
	if got.Pointer != (engine.Pointer{X: 10, Y: 10, Inside: true}) || got.PrimaryPress.Valid ||
		got.PrimaryHeld {
		t.Fatalf("remaining fingers drove the pointer: %+v", got)
	}
	if got.Touches[0] != touch(2, 25, 25) || got.Touches[1] != touch(3, 30, 30) {
		t.Fatalf("touches = %+v", got.Touches)
	}
	host.TouchUp(2)
	host.TouchUp(3)
	if got = host.NextInput(); got.PrimaryRelease.Valid ||
		got.Touches != ([engine.MaxTouches]engine.Touch{}) {
		t.Fatalf("lifting the rest = %+v", got)
	}
	// After every finger has lifted, the next one drives the pointer again,
	// and IDs restart.
	host.TouchDown(4, 40, 40)
	got = host.NextInput()
	if !got.PrimaryPress.Valid || got.Touches[0] != touch(1, 40, 40) {
		t.Fatalf("new first finger = %+v", got)
	}
}

func TestTouchCancelEndsTheDragWithoutARelease(t *testing.T) {
	var host loop.Loop
	host.TouchDown(1, 10, 10)
	host.TouchDown(2, 20, 20)
	host.NextInput()
	host.TouchCancel(1)
	got := host.NextInput()
	if got.PrimaryRelease.Valid || got.PrimaryHeld || got.Touches[0] != touch(2, 20, 20) {
		t.Fatalf("cancel = %+v", got)
	}
}

func TestFocusLossClearsTouchesWithoutARelease(t *testing.T) {
	var host loop.Loop
	host.TouchDown(1, 10, 10)
	host.TouchDown(2, 20, 20)
	host.SetActive(false)
	host.TouchDown(3, 30, 30)
	host.SetActive(true)
	// Fingers from before the suspension are no longer tracked.
	host.TouchMove(1, 15, 15, true)
	host.TouchUp(1)
	host.TouchUp(2)
	if got := host.NextInput(); got != (engine.Input{}) {
		t.Fatalf("resumed input = %+v; want zero", got)
	}
}

func TestGamepadConnectAndDisconnect(t *testing.T) {
	var host loop.Loop
	pad := loop.NormalizeGamepad(engine.GamepadSouth, 1, 0, 0, 0, 0, 0)
	host.SetGamepad(2, pad)
	host.NextInput()
	// Pad state is held, not an edge: every tick sees it.
	got := host.NextInput()
	if got.Gamepads[2] != pad || got.Gamepads[0] != (engine.Gamepad{}) {
		t.Fatalf("connected = %+v", got.Gamepads)
	}
	host.SetGamepad(2, engine.Gamepad{})
	if got := host.NextInput(); got != (engine.Input{}) {
		t.Fatalf("disconnected = %+v", got)
	}
	host.SetGamepad(0, pad)
	host.SetActive(false)
	host.SetGamepad(1, pad)
	host.SetActive(true)
	if got := host.NextInput(); got != (engine.Input{}) {
		t.Fatalf("suspension kept pads: %+v", got)
	}
}

func TestNormalizeGamepad(t *testing.T) {
	// The radius at which the rescaled value reaches half of one step.
	half := 0.15 + 0.5*0.85/127
	for _, tc := range []struct {
		name         string
		x, y         float64
		wantX, wantY int8
	}{
		{"rest", 0, 0, 0, 0},
		{"deadzone edge", 0.15, 0, 0, 0},
		{"deadzone edge diagonal", 0.15 * math.Sqrt2 / 2, -0.15 * math.Sqrt2 / 2, 0, 0},
		{"below half a step", half - 1e-9, 0, 0, 0},
		{"above half a step", half + 1e-9, 0, 1, 0},
		{"full right", 1, 0, 127, 0},
		{"full up", 0, -1, 0, -127},
		{"past full", -3, 0, -127, 0},
		{"diagonal corner", 1, 1, 90, 90},
		{"past half", 0.58, 0, 64, 0},
		{"NaN", math.NaN(), 0.5, 0, 0},
		{"infinite", math.Inf(1), 0, 0, 0},
	} {
		got := loop.NormalizeGamepad(0, tc.x, tc.y, tc.y, tc.x, 0, 0)
		if got.LeftX != tc.wantX || got.LeftY != tc.wantY || got.RightX != tc.wantY ||
			got.RightY != tc.wantX || !got.Connected {
			t.Errorf("%s: (%v, %v) -> %+v; want (%d, %d)", tc.name, tc.x, tc.y, got, tc.wantX, tc.wantY)
		}
	}
	for _, tc := range []struct {
		value float64
		want  uint8
		down  bool
	}{
		{-1, 0, false},
		{math.NaN(), 0, false},
		{0.001, 0, false},
		{0.499, 127, false},
		{0.5, 128, true},
		{1, 255, true},
		{2, 255, true},
	} {
		got := loop.NormalizeGamepad(engine.GamepadLeftTrigger, 0, 0, 0, 0, tc.value, tc.value)
		down := engine.GamepadLeftTrigger | engine.GamepadRightTrigger
		if got.LeftTrigger != tc.want || got.RightTrigger != tc.want ||
			got.Buttons.Has(down) != tc.down || (!tc.down && got.Buttons != 0) {
			t.Errorf("trigger %v -> %+v; want %d, down %v", tc.value, got, tc.want, tc.down)
		}
	}
	if got := loop.NormalizeGamepad(
		engine.GamepadUp|engine.GamepadStart,
		0,
		0,
		0,
		0,
		0,
		0,
	); got.Buttons !=
		engine.GamepadUp|engine.GamepadStart {
		t.Errorf("buttons = %#x", got.Buttons)
	}
	if n := testing.AllocsPerRun(100, func() {
		loop.NormalizeGamepad(engine.GamepadSouth, 0.3, -0.7, 0.9, 0.1, 0.4, 0.8)
	}); n != 0 {
		t.Errorf("NormalizeGamepad allocates %v times", n)
	}
}

// The pinned sweep shows that native and WebAssembly builds give the same bytes.
func TestNormalizeGamepadSweepIsPinned(t *testing.T) {
	hash := uint64(14695981039346656037)
	for i := -110; i <= 110; i++ {
		for j := -110; j <= 110; j++ {
			x, y := float64(i)/97, float64(j)/89
			pad := loop.NormalizeGamepad(0, x, y, y/3, x/7, x, y)
			for _, b := range [...]uint8{
				uint8(pad.LeftX), uint8(pad.LeftY), // #nosec G115 -- hashing the two's complement bytes.
				uint8(pad.RightX), uint8(pad.RightY), // #nosec G115 -- hashing the two's complement bytes.
				pad.LeftTrigger, pad.RightTrigger,
			} {
				hash = (hash ^ uint64(b)) * 1099511628211
			}
		}
	}
	if hash != 0x4369656bee071545 {
		t.Errorf("sweep hash = %#x", hash)
	}
}
