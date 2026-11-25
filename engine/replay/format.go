package replay

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"

	"github.com/krapht-foundation/goje/engine"
)

// The byte layout is defined in specs/replay-v1.md. All integers are
// little-endian; floats are their IEEE 754 bits.
const (
	magic   = "GOJERPLY"
	version = 1
	// headerSize counts every fixed-size field: magic, version, game length,
	// content, interval, start length, tick count, run count, and checkpoint count.
	headerSize = len(magic) + 2 + 1 + 8 + 2 + 4 + 4 + 4 + 4
	minRunSize = 2 + 4
	// touchSize is an ID and two coordinates; padSize is a slot, the button
	// bits, four stick bytes, and two trigger bytes.
	touchSize  = 4 + 16
	padSize    = 1 + 2 + 6
	maxRunSize = minRunSize + 4 + 4 + 5*16 +
		1 + engine.MaxTouches*touchSize + 1 + engine.MaxGamepads*padSize
	checkpointSize = 4 + 8
)

// Run flag bits. A coordinate pair is present when either value has nonzero
// bits. The touch and gamepad bits were added in M15; a run without an active
// touch or a connected pad sets neither, so it keeps its earlier bytes.
const (
	flagHeld uint16 = 1 << iota
	flagPressed
	flagPointer
	flagInside
	flagPrimaryPress
	flagPrimaryPressValid
	flagSecondaryPress
	flagSecondaryPressValid
	flagPrimaryRelease
	flagPrimaryReleaseValid
	flagSecondaryRelease
	flagSecondaryReleaseValid
	flagPrimaryHeld
	flagSecondaryHeld
	flagTouches
	flagGamepads
)

// Encode returns the version 1 encoding of rec. It rejects a recording that
// breaks a rule of the format, including one larger than MaxEncodedSize.
func Encode(rec *Recording) ([]byte, error) {
	ticks, err := rec.validate()
	if err != nil {
		return nil, fmt.Errorf("encode replay: %w", err)
	}
	size := headerSize + len(rec.Game) + len(rec.Start) + checkpointSize*len(rec.Checkpoints)
	for _, run := range rec.Runs {
		size += runSize(run.Input)
	}
	if size > MaxEncodedSize {
		return nil, fmt.Errorf("encode replay: %d bytes exceeds %d", size, MaxEncodedSize)
	}
	le := binary.LittleEndian
	data := make([]byte, 0, size)
	data = append(data, magic...)
	data = le.AppendUint16(data, version)
	data = append(data, byte(len(rec.Game))) // #nosec G115 -- validate bounded the name to 32 bytes.
	data = append(data, rec.Game...)
	data = le.AppendUint64(data, rec.Content)
	data = le.AppendUint16(data, uint16(rec.Interval)) // #nosec G115 -- validate bounded it.
	data = le.AppendUint32(data, u32(len(rec.Start)))
	data = append(data, rec.Start...)
	data = le.AppendUint32(data, u32(ticks))
	data = le.AppendUint32(data, u32(len(rec.Runs)))
	for _, run := range rec.Runs {
		data = appendRun(data, run)
	}
	data = le.AppendUint32(data, u32(len(rec.Checkpoints)))
	for _, c := range rec.Checkpoints {
		data = le.AppendUint32(data, u32(c.Tick))
		data = le.AppendUint64(data, c.Hash)
	}
	return data, nil
}

// u32 converts a count or size that validate bounded to at most MaxTicks or
// MaxStartSize.
func u32(n int) uint32 { return uint32(n) } // #nosec G115 -- validate bounded n.

// Decode reads a version 1 recording. It checks every count against the
// remaining bytes and the limits before allocating from it. The returned
// recording's Start aliases data.
func Decode(data []byte) (*Recording, error) {
	rec, err := decode(data)
	if err != nil {
		return nil, fmt.Errorf("decode replay: %w", err)
	}
	return rec, nil
}

func decode(data []byte) (*Recording, error) {
	if len(data) > MaxEncodedSize {
		return nil, fmt.Errorf("%d bytes exceeds %d", len(data), MaxEncodedSize)
	}
	r := reader{data: data}
	if string(r.bytes(len(magic))) != magic {
		return nil, errors.New("bad magic")
	}
	if v := r.u16(); r.ok() && v != version {
		return nil, fmt.Errorf("unsupported version %d (want %d)", v, version)
	}
	rec := &Recording{}
	rec.Game = string(r.bytes(int(r.u8())))
	if r.ok() && !validGame(rec.Game) {
		return nil, rec.Validate()
	}
	rec.Content = r.u64()
	interval := r.u16()
	rec.Interval = int(interval)
	if size := r.u32(); size > MaxStartSize {
		return nil, fmt.Errorf("start state of %d bytes exceeds %d", size, MaxStartSize)
	} else if size > 0 {
		rec.Start = r.bytes(int(size))
	}
	ticks, runs := r.u32(), r.u32()
	if !r.ok() {
		return nil, errTruncated
	}
	if ticks > MaxTicks || runs > ticks || int(runs) > len(r.data)/minRunSize {
		return nil, fmt.Errorf("%d runs of %d ticks exceed the limits or the data", runs, ticks)
	}
	rec.Runs = make([]Run, runs)
	for i := range rec.Runs {
		if err := r.run(&rec.Runs[i]); err != nil {
			return nil, fmt.Errorf("run %d: %w", i, err)
		}
	}
	checkpoints := r.u32()
	if !r.ok() {
		return nil, errTruncated
	}
	if rec.Interval < 1 {
		return nil, errors.New("checkpoint interval is zero")
	}
	if checkpoints > ticks/uint32(interval)+1 || int(checkpoints) > len(r.data)/checkpointSize {
		return nil, fmt.Errorf("%d checkpoints exceed the ticks or the data", checkpoints)
	}
	if checkpoints > 0 {
		rec.Checkpoints = make([]Checkpoint, checkpoints)
	}
	for i := range rec.Checkpoints {
		rec.Checkpoints[i] = Checkpoint{Tick: int(r.u32()), Hash: r.u64()}
	}
	if !r.ok() {
		return nil, errTruncated
	}
	if len(r.data) != 0 {
		return nil, fmt.Errorf("%d bytes of trailing data", len(r.data))
	}
	total, err := rec.validate()
	if err != nil {
		return nil, err
	}
	if total != int(ticks) {
		return nil, fmt.Errorf("runs hold %d ticks, header says %d", total, ticks)
	}
	return rec, nil
}

var errTruncated = errors.New("truncated data")

func runSize(in engine.Input) int {
	size := minRunSize
	if in.Held != 0 {
		size += 4
	}
	if in.Pressed != 0 {
		size += 4
	}
	for _, p := range [...]struct{ x, y float64 }{
		{in.Pointer.X, in.Pointer.Y},
		{in.PrimaryPress.X, in.PrimaryPress.Y},
		{in.SecondaryPress.X, in.SecondaryPress.Y},
		{in.PrimaryRelease.X, in.PrimaryRelease.Y},
		{in.SecondaryRelease.X, in.SecondaryRelease.Y},
	} {
		if present(p.x, p.y) {
			size += 16
		}
	}
	if n := touchCount(&in); n > 0 {
		size += 1 + n*touchSize
	}
	if n := padCount(&in); n > 0 {
		size += 1 + n*padSize
	}
	return size
}

// touchCount returns the number of active touches, which validate has checked
// come first.
func touchCount(in *engine.Input) int {
	n := 0
	for _, t := range in.Touches {
		if t.Active {
			n++
		}
	}
	return n
}

func padCount(in *engine.Input) int {
	n := 0
	for _, p := range in.Gamepads {
		if p.Connected {
			n++
		}
	}
	return n
}

func present(x, y float64) bool { return math.Float64bits(x)|math.Float64bits(y) != 0 }

func appendRun(data []byte, run Run) []byte {
	in := run.Input
	var flags uint16
	set := func(bit uint16, on bool) {
		if on {
			flags |= bit
		}
	}
	set(flagHeld, in.Held != 0)
	set(flagPressed, in.Pressed != 0)
	set(flagPointer, present(in.Pointer.X, in.Pointer.Y))
	set(flagInside, in.Pointer.Inside)
	set(flagPrimaryPress, present(in.PrimaryPress.X, in.PrimaryPress.Y))
	set(flagPrimaryPressValid, in.PrimaryPress.Valid)
	set(flagSecondaryPress, present(in.SecondaryPress.X, in.SecondaryPress.Y))
	set(flagSecondaryPressValid, in.SecondaryPress.Valid)
	set(flagPrimaryRelease, present(in.PrimaryRelease.X, in.PrimaryRelease.Y))
	set(flagPrimaryReleaseValid, in.PrimaryRelease.Valid)
	set(flagSecondaryRelease, present(in.SecondaryRelease.X, in.SecondaryRelease.Y))
	set(flagSecondaryReleaseValid, in.SecondaryRelease.Valid)
	set(flagPrimaryHeld, in.PrimaryHeld)
	set(flagSecondaryHeld, in.SecondaryHeld)
	touches, pads := touchCount(&in), padCount(&in)
	set(flagTouches, touches > 0)
	set(flagGamepads, pads > 0)
	data = binary.LittleEndian.AppendUint16(data, flags)
	data = binary.LittleEndian.AppendUint32(data, u32(run.Ticks))
	if flags&flagHeld != 0 {
		data = binary.LittleEndian.AppendUint32(data, uint32(in.Held))
	}
	if flags&flagPressed != 0 {
		data = binary.LittleEndian.AppendUint32(data, uint32(in.Pressed))
	}
	for _, p := range [...]struct {
		bit  uint16
		x, y float64
	}{
		{flagPointer, in.Pointer.X, in.Pointer.Y},
		{flagPrimaryPress, in.PrimaryPress.X, in.PrimaryPress.Y},
		{flagSecondaryPress, in.SecondaryPress.X, in.SecondaryPress.Y},
		{flagPrimaryRelease, in.PrimaryRelease.X, in.PrimaryRelease.Y},
		{flagSecondaryRelease, in.SecondaryRelease.X, in.SecondaryRelease.Y},
	} {
		if flags&p.bit != 0 {
			data = binary.LittleEndian.AppendUint64(data, math.Float64bits(p.x))
			data = binary.LittleEndian.AppendUint64(data, math.Float64bits(p.y))
		}
	}
	if touches > 0 {
		data = append(data, byte(touches)) // #nosec G115 -- at most engine.MaxTouches.
		for _, t := range in.Touches[:touches] {
			data = binary.LittleEndian.AppendUint32(data, t.ID)
			data = binary.LittleEndian.AppendUint64(data, math.Float64bits(t.X))
			data = binary.LittleEndian.AppendUint64(data, math.Float64bits(t.Y))
		}
	}
	if pads > 0 {
		data = append(data, byte(pads)) // #nosec G115 -- at most engine.MaxGamepads.
		for slot, p := range in.Gamepads {
			if !p.Connected {
				continue
			}
			data = append(data, byte(slot)) // #nosec G115 -- below engine.MaxGamepads.
			data = binary.LittleEndian.AppendUint16(data, uint16(p.Buttons))
			for _, axis := range [...]int8{p.LeftX, p.LeftY, p.RightX, p.RightY} {
				data = append(data, byte(axis)) // #nosec G115 -- stored as the two's complement byte.
			}
			data = append(data, p.LeftTrigger, p.RightTrigger)
		}
	}
	return data
}

// reader consumes little-endian fields. After the data runs out it returns
// zero values and ok reports false, so callers check once per group of fields.
type reader struct {
	data      []byte
	truncated bool
}

func (r *reader) ok() bool { return !r.truncated }

func (r *reader) bytes(n int) []byte {
	if r.truncated || n > len(r.data) {
		r.truncated = true
		return nil
	}
	b := r.data[:n:n]
	r.data = r.data[n:]
	return b
}

func (r *reader) u8() uint8 {
	if b := r.bytes(1); b != nil {
		return b[0]
	}
	return 0
}

func (r *reader) u16() uint16 {
	if b := r.bytes(2); b != nil {
		return binary.LittleEndian.Uint16(b)
	}
	return 0
}

func (r *reader) u32() uint32 {
	if b := r.bytes(4); b != nil {
		return binary.LittleEndian.Uint32(b)
	}
	return 0
}

func (r *reader) u64() uint64 {
	if b := r.bytes(8); b != nil {
		return binary.LittleEndian.Uint64(b)
	}
	return 0
}

// run reads one run and rejects present fields whose value is zero and lists
// that the format does not allow, so that every recording has exactly one
// encoding. All 16 flag bits are defined.
func (r *reader) run(run *Run) error {
	flags := r.u16()
	ticks := r.u32()
	if !r.ok() {
		return errTruncated
	}
	run.Ticks = int(ticks)
	in := &run.Input
	if flags&flagHeld != 0 {
		if in.Held = engine.Keys(r.u32()); r.ok() && in.Held == 0 {
			return errors.New("held keys flagged but empty")
		}
	}
	if flags&flagPressed != 0 {
		if in.Pressed = engine.Keys(r.u32()); r.ok() && in.Pressed == 0 {
			return errors.New("pressed keys flagged but empty")
		}
	}
	for _, p := range [...]struct {
		bit  uint16
		x, y *float64
	}{
		{flagPointer, &in.Pointer.X, &in.Pointer.Y},
		{flagPrimaryPress, &in.PrimaryPress.X, &in.PrimaryPress.Y},
		{flagSecondaryPress, &in.SecondaryPress.X, &in.SecondaryPress.Y},
		{flagPrimaryRelease, &in.PrimaryRelease.X, &in.PrimaryRelease.Y},
		{flagSecondaryRelease, &in.SecondaryRelease.X, &in.SecondaryRelease.Y},
	} {
		if flags&p.bit == 0 {
			continue
		}
		*p.x, *p.y = math.Float64frombits(r.u64()), math.Float64frombits(r.u64())
		if r.ok() && !present(*p.x, *p.y) {
			return errors.New("coordinates flagged but zero")
		}
	}
	if flags&flagTouches != 0 {
		if err := r.touches(in); err != nil {
			return err
		}
	}
	if flags&flagGamepads != 0 {
		if err := r.gamepads(in); err != nil {
			return err
		}
	}
	in.Pointer.Inside = flags&flagInside != 0
	in.PrimaryPress.Valid = flags&flagPrimaryPressValid != 0
	in.SecondaryPress.Valid = flags&flagSecondaryPressValid != 0
	in.PrimaryRelease.Valid = flags&flagPrimaryReleaseValid != 0
	in.SecondaryRelease.Valid = flags&flagSecondaryReleaseValid != 0
	in.PrimaryHeld = flags&flagPrimaryHeld != 0
	in.SecondaryHeld = flags&flagSecondaryHeld != 0
	if !r.ok() {
		return errTruncated
	}
	return nil
}

// touches reads a touch list. The count is checked before any entry is read,
// and an entry with ID 0, which is how an inactive touch would read, is refused.
func (r *reader) touches(in *engine.Input) error {
	n := int(r.u8())
	if !r.ok() {
		return errTruncated
	}
	if n < 1 || n > engine.MaxTouches {
		return fmt.Errorf("touch count %d is outside 1-%d", n, engine.MaxTouches)
	}
	for i := range n {
		t := &in.Touches[i]
		t.ID, t.Active = r.u32(), true
		t.X, t.Y = math.Float64frombits(r.u64()), math.Float64frombits(r.u64())
		if r.ok() && t.ID == 0 {
			return fmt.Errorf("touch %d is inactive", i)
		}
	}
	return nil
}

// gamepads reads a pad list, whose slots must be in increasing order, so that a
// slot cannot be listed twice.
func (r *reader) gamepads(in *engine.Input) error {
	n := int(r.u8())
	if !r.ok() {
		return errTruncated
	}
	if n < 1 || n > engine.MaxGamepads {
		return fmt.Errorf("gamepad count %d is outside 1-%d", n, engine.MaxGamepads)
	}
	previous := -1
	for range n {
		slot := int(r.u8())
		buttons := engine.GamepadButtons(r.u16())
		axes := r.bytes(6)
		if !r.ok() {
			return errTruncated
		}
		if slot >= engine.MaxGamepads || slot <= previous {
			return fmt.Errorf("gamepad slot %d is out of range, repeated, or out of order", slot)
		}
		previous = slot
		in.Gamepads[slot] = engine.Gamepad{
			Connected: true, Buttons: buttons,
			LeftX: int8(axes[0]), LeftY: int8(axes[1]), // #nosec G115 -- two's complement bytes.
			RightX: int8(axes[2]), RightY: int8(axes[3]), // #nosec G115 -- two's complement bytes.
			LeftTrigger: axes[4], RightTrigger: axes[5],
		}
	}
	return nil
}
