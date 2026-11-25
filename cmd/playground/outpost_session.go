package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/krapht-foundation/goje/engine"
	"github.com/krapht-foundation/goje/engine/grid"
	"github.com/krapht-foundation/goje/examples/skirmish"
	"github.com/krapht-foundation/goje/font"
	"github.com/krapht-foundation/goje/ui"
)

const (
	outpostGroups = 9
	// outpostDragMin is the smallest drag, in logical pixels, that selects a box
	// instead of clicking.
	outpostDragMin     = 4.0
	outpostSnapshotTag = 2
)

type outpostMode uint8

const (
	modeNone outpostMode = iota
	// modeAttackMove waits for the click that gives the selection an attack-move order.
	modeAttackMove
	// modeMove waits for the tap that gives the selection a move order. It is
	// the touch buttons' stand-in for the secondary mouse button.
	modeMove
)

type outpostOutcome uint8

const (
	outcomePlaying outpostOutcome = iota
	outcomeVictory
	outcomeDefeat
)

// outpostDrag is a primary-button drag in progress, in logical pixels.
type outpostDrag struct {
	active, additive   bool
	startX, startY     float64
	currentX, currentY float64
}

// outpostSession is one match. It owns what a replay must reproduce: the
// simulation, the selection, the control groups, the pending command mode, a
// drag in progress, and the camera. It reads nothing but its input and its own
// history, so a recording plays back exactly. It implements engine.Hasher and
// engine.Snapshotter, which the app uses for checkpoints and saves.
type outpostSession struct {
	skirmishScene
	world   *outpostWorld
	restore []byte // A snapshot to apply when the scene is entered, or nil.

	selection []uint64 // Living scouts in ascending ID order.
	groups    [outpostGroups][]uint64
	mode      outpostMode
	drag      outpostDrag
	outcome   outpostOutcome
	scratch   []uint64

	hud      outpostHUD
	controls outpostControls

	// The rest is output. It decides what is heard, never what happens, so it is
	// not hashed or saved.
	ordered   bool                    // An order was given since the last tick's sounds.
	lastCue   [outpostCueCount]uint64 // The tick of the last voice of each cue, or zero.
	announced outpostOutcome          // The outcome whose sound has been played.
}

// outpostHUD holds the text of the HUD. The strings are rebuilt in Update when a
// number changes, so Draw allocates nothing.
type outpostHUD struct {
	key                 [8]int
	top, bottom         ui.Panel
	counts, alarm       ui.Label
	selection, hint     ui.Label
	result, resultHint  ui.Label
	resultPanel         ui.Panel
	scouts, guards      int
	selected, hp, maxHP int
	held                int
}

func (s *outpostSession) Enter() error {
	if err := s.skirmishScene.Enter(); err != nil {
		return err
	}
	s.marked = s.isSelected
	s.selection = make([]uint64, 0, skirmish.MaxUnits)
	for g := range s.groups {
		s.groups[g] = make([]uint64, 0, skirmish.MaxUnits)
	}
	s.scratch = make([]uint64, 0, skirmish.MaxUnits)
	s.layoutHUD()
	s.controls.layout()
	s.prune()
	if s.restore != nil {
		if err := s.Restore(s.restore); err != nil {
			return fmt.Errorf("outpost: restore snapshot: %w", err)
		}
		s.restore = nil
	}
	s.updateHUD()
	return nil
}

func (s *outpostSession) isSelected(id uint64) bool {
	_, found := slices.BinarySearch(s.selection, id)
	return found
}

// cancelable reports whether Escape would cancel something pending, in which
// case the app leaves the key to the match instead of opening the pause menu.
func (s *outpostSession) cancelable() bool { return s.mode != modeNone || s.drag.active }

func (s *outpostSession) Update(tick engine.Tick) (engine.Transition, error) {
	in := s.effective(tick.Input)
	if in.Pressed.Has(engine.KeyR) {
		return engine.Restart(), nil
	}
	s.keys(in)
	s.pointer(in)
	moveCourtyardCamera(&s.camera, &s.zoomLevel, in, s.homeX, s.homeY)
	if err := s.sim.Step(); err != nil {
		return engine.Transition{}, err
	}
	s.prune()
	s.refresh()
	s.updateHUD()
	s.play(tick)
	return engine.Transition{}, nil
}

// keys handles the keyboard commands. Escape cancels a pending mode or drag; the
// app opens the pause menu only when there is nothing to cancel.
func (s *outpostSession) keys(in engine.Input) {
	if in.Pressed.Has(engine.KeyEscape) {
		s.mode, s.drag = modeNone, outpostDrag{}
	}
	for i := range outpostGroups {
		if !in.Pressed.Has(engine.Key1 << i) {
			continue
		}
		switch {
		case in.Held.Has(engine.KeyControl):
			s.groups[i] = append(s.groups[i][:0], s.selection...)
		case len(s.groups[i]) > 0:
			s.selection = append(s.selection[:0], s.groups[i]...)
		}
	}
	if in.Pressed.Has(engine.KeyA) && len(s.selection) > 0 {
		s.mode = modeAttackMove
	}
	if in.Pressed.Has(engine.KeyH) {
		s.toggleHold()
	}
	if in.Pressed.Has(engine.KeyF) {
		s.focus(in.Pointer)
	}
}

// pointer handles presses, the drag, and releases. A press that gives a pending
// attack-move order is consumed, so its release does not select anything.
func (s *outpostSession) pointer(in engine.Input) {
	if in.SecondaryPress.Valid {
		s.mode = modeNone
		s.order(in.SecondaryPress, skirmish.Move)
	}
	if press := in.PrimaryPress; press.Valid {
		switch s.mode {
		case modeAttackMove:
			s.mode, s.drag = modeNone, outpostDrag{}
			s.order(press, skirmish.AttackMove)
		case modeMove:
			s.mode, s.drag = modeNone, outpostDrag{}
			s.order(press, skirmish.Move)
		default:
			s.drag = outpostDrag{
				active: true, additive: in.Held.Has(engine.KeyShift),
				startX: press.X, startY: press.Y, currentX: press.X, currentY: press.Y,
			}
		}
	}
	if !s.drag.active {
		return
	}
	if in.Pointer.Inside {
		s.drag.currentX, s.drag.currentY = in.Pointer.X, in.Pointer.Y
	}
	switch {
	case in.PrimaryRelease.Valid:
		s.drag.currentX, s.drag.currentY = in.PrimaryRelease.X, in.PrimaryRelease.Y
		s.endDrag()
		s.drag = outpostDrag{}
	case !in.PrimaryHeld:
		s.drag = outpostDrag{} // The release never arrived, for example after losing focus.
	}
}

// endDrag selects what the finished drag covers: a click picks one scout, and a
// longer drag selects every scout inside the box as drawn on the screen.
func (s *outpostSession) endDrag() {
	d := s.drag
	s.scratch = s.scratch[:0]
	if math.Hypot(d.currentX-d.startX, d.currentY-d.startY) < outpostDragMin {
		if id, hit := s.pick(d.startX, d.startY); hit {
			if u, ok := s.sim.Unit(id); ok && u.Alive && u.Team == skirmish.Scouts {
				s.scratch = append(s.scratch, id)
			}
		}
	} else {
		left, right := min(d.startX, d.currentX), max(d.startX, d.currentX)
		top, bottom := min(d.startY, d.currentY), max(d.startY, d.currentY)
		for i := range s.sim.Units() {
			u := &s.sim.Units()[i]
			if !u.Alive || u.Team != skirmish.Scouts {
				continue
			}
			c := u.Center()
			x, y := s.camera.ToLogical(
				courtyardProjection.Project(c.X.Float64(), c.Y.Float64(), skirmishUnitHeight/2),
			)
			if x >= left && x <= right && y >= top && y <= bottom {
				s.scratch = append(s.scratch, u.ID)
			}
		}
	}
	switch {
	case !d.additive:
		s.selection = append(s.selection[:0], s.scratch...)
	case len(s.scratch) == 1 && s.isSelected(s.scratch[0]):
		// Shift-clicking a selected scout removes it.
		i, _ := slices.BinarySearch(s.selection, s.scratch[0])
		s.selection = slices.Delete(s.selection, i, i+1)
	default:
		for _, id := range s.scratch {
			if i, found := slices.BinarySearch(s.selection, id); !found {
				s.selection = slices.Insert(s.selection, i, id)
			}
		}
	}
}

// pick returns the ID of the item under a logical point, if any.
func (s *outpostSession) pick(x, y float64) (uint64, bool) {
	px, py := s.camera.ToProjected(x, y)
	return engine.PickWorld(s.items, courtyardProjection, px, py)
}

// cellUnder returns the ground cell under a logical point.
func (s *outpostSession) cellUnder(x, y float64) (grid.Cell, bool) {
	px, py := s.camera.ToProjected(x, y)
	return s.sim.CellAt(courtyardProjection.Unproject(px, py, 0))
}

// order sends the selection to the point: an attack when a living guard is under
// it, and otherwise the given kind of order to a goal cell near it.
func (s *outpostSession) order(at engine.MousePress, kind skirmish.OrderKind) {
	if len(s.selection) == 0 {
		return
	}
	if id, hit := s.pick(at.X, at.Y); hit {
		if u, ok := s.sim.Unit(id); ok && u.Alive && u.Team == skirmish.Guards {
			s.ordered = s.sim.OrderGroup(
				s.selection,
				skirmish.Order{Kind: skirmish.Attack, Target: id},
			) > 0 ||
				s.ordered
			return
		}
	}
	if cell, ok := s.cellUnder(at.X, at.Y); ok {
		s.ordered = s.sim.OrderGroup(s.selection, skirmish.Order{Kind: kind, Goal: cell}) > 0 ||
			s.ordered
	}
}

// toggleHold holds fire for the whole selection, or releases it when every
// selected unit is already holding.
func (s *outpostSession) toggleHold() {
	all := len(s.selection) > 0
	for _, id := range s.selection {
		if u, ok := s.sim.Unit(id); ok && !u.Hold() {
			all = false
		}
	}
	for _, id := range s.selection {
		s.sim.SetHold(id, !all)
	}
}

// focus orders the selection to attack the guard under the pointer, or else the
// guard nearest to the pointer's ground position, or to the first selected
// scout when the pointer is outside the viewport.
func (s *outpostSession) focus(pointer engine.Pointer) {
	if len(s.selection) == 0 {
		return
	}
	var fx, fy float64
	switch first, ok := s.sim.Unit(s.selection[0]); {
	case pointer.Inside:
		if id, hit := s.pick(pointer.X, pointer.Y); hit {
			if target, found := s.sim.Unit(id); found && target.Alive && target.Team == skirmish.Guards {
				s.ordered = s.sim.OrderGroup(
					s.selection,
					skirmish.Order{Kind: skirmish.Attack, Target: id},
				) > 0 ||
					s.ordered
				return
			}
		}
		px, py := s.camera.ToProjected(pointer.X, pointer.Y)
		fx, fy = courtyardProjection.Unproject(px, py, 0)
	case ok:
		c := first.Center()
		fx, fy = c.X.Float64(), c.Y.Float64()
	default:
		return
	}
	best, bestDistance := uint64(0), math.Inf(1)
	for i := range s.sim.Units() {
		u := &s.sim.Units()[i]
		if !u.Alive || u.Team != skirmish.Guards || !s.seenUnit(u) {
			continue
		}
		c := u.Center()
		if d := math.Hypot(c.X.Float64()-fx, c.Y.Float64()-fy); d < bestDistance {
			best, bestDistance = u.ID, d
		}
	}
	if best != 0 {
		s.ordered = s.sim.OrderGroup(
			s.selection,
			skirmish.Order{Kind: skirmish.Attack, Target: best},
		) > 0 ||
			s.ordered
	}
}

// prune removes the fallen from the selection and the groups and recomputes the outcome.
func (s *outpostSession) prune() {
	s.selection = s.living(s.selection)
	for g := range s.groups {
		s.groups[g] = s.living(s.groups[g])
	}
	scouts, guards, inSight := 0, 0, 0
	for i := range s.sim.Units() {
		if u := &s.sim.Units()[i]; u.Alive {
			if u.Team == skirmish.Scouts {
				scouts++
			} else {
				guards++
				if s.seenUnit(u) {
					inSight++
				}
			}
		}
	}
	s.hud.scouts, s.hud.guards = scouts, inSight
	switch {
	case guards == 0:
		s.outcome = outcomeVictory
	case scouts == 0:
		s.outcome = outcomeDefeat
	default:
		s.outcome = outcomePlaying
	}
}

// living filters ids in place down to the units still alive.
func (s *outpostSession) living(ids []uint64) []uint64 {
	kept := ids[:0]
	for _, id := range ids {
		if u, ok := s.sim.Unit(id); ok && u.Alive {
			kept = append(kept, id)
		}
	}
	return kept
}

// StateHash digests the simulation and everything else that later updates read.
func (s *outpostSession) StateHash() uint64 {
	h := fnvOffset
	add := func(v uint64) {
		for range 8 {
			h = (h ^ (v & 0xff)) * fnvPrime
			v >>= 8
		}
	}
	add(s.sim.Trace())
	add(uint64(len(s.selection)))
	for _, id := range s.selection {
		add(id)
	}
	for _, g := range s.groups {
		add(uint64(len(g)))
		for _, id := range g {
			add(id)
		}
	}
	add(uint64(s.mode))
	s.controls.hash(add)
	add(uint64(s.zoomLevel)) // #nosec G115 -- the zoom level is a small index.
	add(math.Float64bits(s.camera.CenterX))
	add(math.Float64bits(s.camera.CenterY))
	if s.drag.active {
		add(1)
		add(uint64(boolByte(s.drag.additive)))
		add(math.Float64bits(s.drag.startX))
		add(math.Float64bits(s.drag.startY))
		add(math.Float64bits(s.drag.currentX))
		add(math.Float64bits(s.drag.currentY))
	}
	return h
}

const (
	fnvOffset uint64 = 14695981039346656037
	fnvPrime  uint64 = 1099511628211
)

// Snapshot writes the match: the simulation, then the scene's own state. See
// specs/outpost-m14.md for the layout.
func (s *outpostSession) Snapshot() ([]byte, error) {
	simState, err := s.sim.Snapshot()
	if err != nil {
		return nil, err
	}
	b := []byte{outpostSnapshotTag}
	b = binary.LittleEndian.AppendUint32(
		b,
		uint32(len(simState)),
	) // #nosec G115 -- a snapshot is far below 4 GiB.
	b = append(b, simState...)
	b = append(b, byte(s.zoomLevel), byte(s.mode)) // #nosec G115 -- the zoom level is a small index.
	b = binary.LittleEndian.AppendUint64(b, math.Float64bits(s.camera.CenterX))
	b = binary.LittleEndian.AppendUint64(b, math.Float64bits(s.camera.CenterY))
	b = append(b, boolByte(s.drag.active), boolByte(s.drag.additive))
	for _, v := range [...]float64{s.drag.startX, s.drag.startY, s.drag.currentX, s.drag.currentY} {
		b = binary.LittleEndian.AppendUint64(b, math.Float64bits(v))
	}
	for _, list := range append([][]uint64{s.selection}, s.groups[:]...) {
		b = binary.LittleEndian.AppendUint16(b, uint16(len(list))) // #nosec G115 -- at most MaxUnits.
		for _, id := range list {
			b = binary.LittleEndian.AppendUint64(b, id)
		}
	}
	b = s.controls.appendSnapshot(b)
	return b, nil
}

func boolByte(v bool) byte {
	if v {
		return 1
	}
	return 0
}

// errOutpostSnapshot is returned for a snapshot that is not a valid match.
var errOutpostSnapshot = errors.New("not a valid outpost snapshot")

// Restore replaces the match with a snapshot written by Snapshot. It parses and
// checks everything before it changes the scene, and leaves the scene unchanged
// when it returns an error.
func (s *outpostSession) Restore(data []byte) error {
	r := snapshotReader{data: data}
	if r.byte() != outpostSnapshotTag {
		return errOutpostSnapshot
	}
	simState := r.bytes(int(r.u32()))
	zoom, mode := int(r.byte()), outpostMode(r.byte())
	camX, camY := math.Float64frombits(r.u64()), math.Float64frombits(r.u64())
	var drag outpostDrag
	drag.active, drag.additive = r.byte() == 1, r.byte() == 1
	for _, v := range [...]*float64{&drag.startX, &drag.startY, &drag.currentX, &drag.currentY} {
		*v = math.Float64frombits(r.u64())
	}
	var lists [1 + outpostGroups][]uint64
	for i := range lists {
		n := int(r.u16())
		if n > skirmish.MaxUnits {
			return errOutpostSnapshot
		}
		for range n {
			lists[i] = append(lists[i], r.u64())
		}
	}
	controls := readControls(&r)
	if r.err || len(r.data) != 0 || zoom >= len(courtyardZoom) || mode > modeMove ||
		!controls.valid() ||
		!finiteWithin(camX, s.homeX, 128) ||
		!finiteWithin(camY, s.homeY, 64) {
		return errOutpostSnapshot
	}
	if err := s.sim.Restore(simState); err != nil {
		return err
	}
	s.zoomLevel, s.mode, s.drag = zoom, mode, drag
	controls.buttons = s.controls.buttons
	s.controls = controls
	s.camera.Zoom, s.camera.CenterX, s.camera.CenterY = courtyardZoom[zoom], camX, camY
	s.selection = append(s.selection[:0], lists[0]...)
	for g := range s.groups {
		s.groups[g] = append(s.groups[g][:0], lists[1+g]...)
	}
	s.prune()
	s.refresh()
	s.updateHUD()
	return nil
}

func finiteWithin(v, center, radius float64) bool {
	return !math.IsNaN(v) && math.Abs(v-center) <= radius
}

// snapshotReader reads little-endian values and remembers whether it ran out of data.
type snapshotReader struct {
	data []byte
	err  bool
}

func (r *snapshotReader) bytes(n int) []byte {
	if n < 0 || n > len(r.data) {
		r.err, r.data = true, nil
		return nil
	}
	out := r.data[:n]
	r.data = r.data[n:]
	return out
}

func (r *snapshotReader) byte() byte {
	if b := r.bytes(1); b != nil {
		return b[0]
	}
	return 0
}

func (r *snapshotReader) u16() uint16 {
	if b := r.bytes(2); b != nil {
		return binary.LittleEndian.Uint16(b)
	}
	return 0
}

func (r *snapshotReader) u32() uint32 {
	if b := r.bytes(4); b != nil {
		return binary.LittleEndian.Uint32(b)
	}
	return 0
}

func (r *snapshotReader) u64() uint64 {
	if b := r.bytes(8); b != nil {
		return binary.LittleEndian.Uint64(b)
	}
	return 0
}

// layoutHUD places the widgets once. The viewport is 320 × 200 logical pixels.
func (s *outpostSession) layoutHUD() {
	h := &s.hud
	panel := ui.DefaultStyle()
	panel.BorderWidth = 0
	label := ui.DefaultStyle()
	h.top = ui.Panel{Bounds: image.Rect(0, 0, width, 12), Style: panel}
	h.bottom = ui.Panel{Bounds: image.Rect(0, 180, width, height), Style: panel}
	h.counts = ui.Label{Bounds: image.Rect(0, 0, 160, 12), Style: label}
	h.alarm = ui.Label{Bounds: image.Rect(150, 0, 240, 12), Text: "ALARM", Style: label}
	h.alarm.Style.Text = skirmishAlarmLoud
	h.selection = ui.Label{Bounds: image.Rect(0, 180, width, 190), Style: label}
	h.hint = ui.Label{Bounds: image.Rect(0, 190, width, height), Style: label}
	h.hint.Style.Text = skirmishAlarmQuiet
	h.resultPanel = ui.Panel{Bounds: image.Rect(80, 78, 240, 122), Style: ui.DefaultStyle()}
	h.result = ui.Label{Bounds: image.Rect(80, 80, 240, 104), Style: label}
	h.result.Style.TextScale = 2
	h.resultHint = ui.Label{
		Bounds: image.Rect(80, 104, 240, 120),
		Text:   "Press R to restart",
		Style:  label,
	}
}

// updateHUD rebuilds the label strings when a number they show has changed.
func (s *outpostSession) updateHUD() {
	h := &s.hud
	h.selected, h.hp, h.maxHP, h.held = len(s.selection), 0, 0, 0
	for _, id := range s.selection {
		if u, ok := s.sim.Unit(id); ok {
			h.hp, h.maxHP = h.hp+u.HP, h.maxHP+u.MaxHP
			if u.Hold() {
				h.held++
			}
		}
	}
	groups := 0
	for g := range s.groups {
		groups = groups*257 + len(s.groups[g])
	}
	key := [8]int{h.scouts, h.guards, h.selected, h.hp, h.maxHP, h.held, int(s.mode), groups}
	if key == h.key && h.counts.Text != "" {
		return
	}
	h.key = key
	h.counts.Text = "Scouts " + strconv.Itoa(h.scouts) + "  Guards seen " + strconv.Itoa(h.guards)
	switch {
	case s.mode == modeMove:
		h.selection.Text = "Move: tap the destination."
	case s.mode == modeAttackMove:
		h.selection.Text = "Attack-move: click a target. Esc cancels."
	case h.selected == 0:
		h.selection.Text = "Nothing selected"
	default:
		h.selection.Text = strconv.Itoa(h.selected) + " selected  Health " + strconv.Itoa(h.hp) + "/" +
			strconv.Itoa(h.maxHP)
		if h.held > 0 {
			h.selection.Text += "  Hold " + strconv.Itoa(h.held)
		}
	}
	h.hint.Text = outpostHint
	var groupText strings.Builder
	for g := range s.groups {
		if n := len(s.groups[g]); n > 0 {
			groupText.WriteString(" " + strconv.Itoa(g+1) + ":" + strconv.Itoa(n))
		}
	}
	if groupText.Len() > 0 {
		h.hint.Text = "Groups" + groupText.String()
	}
	h.result.Text = "VICTORY"
	if s.outcome == outcomeDefeat {
		h.result.Text = "DEFEAT"
	}
}

func (s *outpostSession) Draw(canvas engine.Canvas) {
	s.drawWorld(canvas)
	zoom := float32(s.camera.Zoom)
	for _, id := range s.selection {
		if u, ok := s.sim.Unit(id); ok {
			if goal, moving := s.sim.Goal(u); moving {
				x, y := s.camera.ToLogical(
					courtyardProjection.Project(float64(goal.X)+0.5, float64(goal.Y)+0.5, 0),
				)
				canvas.Rect(float32(x)-4*zoom, float32(y)-2*zoom, 8*zoom, 4*zoom, destinationColor)
			}
		}
	}
	if d := s.drag; d.active &&
		math.Hypot(d.currentX-d.startX, d.currentY-d.startY) >= outpostDragMin {
		left, top := float32(min(d.startX, d.currentX)), float32(min(d.startY, d.currentY))
		w, h := float32(math.Abs(d.currentX-d.startX)), float32(math.Abs(d.currentY-d.startY))
		canvas.Rect(left, top, w, 1, selectionColor)
		canvas.Rect(left, top+h-1, w, 1, selectionColor)
		canvas.Rect(left, top, 1, h, selectionColor)
		canvas.Rect(left+w-1, top, 1, h, selectionColor)
	}
	s.drawHUD(canvas, s.world.face)
	s.controls.draw(canvas, s.world.face)
}

func (s *outpostSession) drawHUD(canvas engine.Canvas, face font.Face) {
	h := &s.hud
	h.top.Draw(canvas)
	h.bottom.Draw(canvas)
	h.counts.Draw(canvas, face)
	if s.sim.Alarm() {
		h.alarm.Draw(canvas, face)
	}
	h.selection.Draw(canvas, face)
	h.hint.Draw(canvas, face)
	if s.outcome != outcomePlaying {
		h.resultPanel.Draw(canvas)
		h.result.Draw(canvas, face)
		h.resultHint.Draw(canvas, face)
	}
}
