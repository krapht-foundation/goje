package main

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"io/fs"

	"github.com/krapht-foundation/goje/engine"
	"github.com/krapht-foundation/goje/engine/replay"
	"github.com/krapht-foundation/goje/font"
	"github.com/krapht-foundation/goje/level"
	"github.com/krapht-foundation/goje/platform"
)

// The outpost is the reference scene for RFC 0015: a title menu, a pause menu, a
// HUD with text, group selection and orders, saving and loading, and a replay of
// the last match. The simulation is the skirmish's, in examples/skirmish; this
// file loads the level and packs the font, outpost_session.go plays one match,
// and outpost_app.go owns the menus and the recording. See specs/outpost-m14.md.

const (
	outpostGame = "outpost"
	// outpostContent is part of the content check, so a change to the rules the
	// level cannot express can retire old replays and saves by changing it.
	outpostContent = "outpost-1"
	// outpostRecordTicks is the room reserved for one recording: five minutes.
	outpostRecordTicks = 5 * 60 * 60
)

// outpostWorld is what every match shares: the decoded level, the content check
// for recordings and saves, the font in the launch atlas, and the store. The
// store is nil when the host has none, and the game then plays without saving.
type outpostWorld struct {
	board level.Board
	meta  replay.Meta
	face  font.Face
	store platform.Store
}

// loadOutpost reads the outpost level and computes the content check from its bytes.
func loadOutpost(root fs.FS) (level.Board, replay.Meta, error) {
	data, err := fs.ReadFile(root, "levels/outpost.json")
	if err != nil {
		return level.Board{}, replay.Meta{}, fmt.Errorf("read outpost level: %w", err)
	}
	board, err := level.Decode(bytes.NewReader(data))
	if err != nil {
		return level.Board{}, replay.Meta{}, fmt.Errorf("decode outpost level: %w", err)
	}
	return board, replay.Meta{
		Game:    outpostGame,
		Content: replay.ContentHash(data, []byte(outpostContent)),
	}, nil
}

// packOutpostAtlas places the built-in font below the project's atlas, so the
// courtyard's ground and sprite regions keep their coordinates.
func packOutpostAtlas(base image.Image, face font.Face) (*image.NRGBA, font.Face) {
	bounds, sheet := base.Bounds(), face.Image().Bounds()
	offset := image.Pt(0, bounds.Dy()+1)
	size := image.Pt(max(bounds.Dx(), sheet.Dx()), offset.Y+sheet.Dy())
	atlas := image.NewNRGBA(image.Rectangle{Max: size})
	draw.Draw(atlas, image.Rectangle{Max: bounds.Size()}, base, bounds.Min, draw.Src)
	draw.Draw(
		atlas,
		image.Rectangle{Min: offset, Max: offset.Add(sheet.Size())},
		face.Image(),
		sheet.Min,
		draw.Src,
	)
	return atlas, face.InAtlas(offset)
}

// newOutpost loads the level and the font for the project. The store may be nil.
func newOutpost(
	root fs.FS,
	project authoredProject,
	store platform.Store,
) (*outpostWorld, platform.Config, error) {
	board, meta, err := loadOutpost(root)
	if err != nil {
		return nil, platform.Config{}, err
	}
	atlas, face := packOutpostAtlas(project.atlas, font.Builtin())
	config := project.config()
	config.Image = atlas
	config.Title = "Goje Outpost · Drag: select · Right click: move or attack · A: attack-move · H: hold · Esc: menu"
	return &outpostWorld{board: board, meta: meta, face: face, store: store}, config, nil
}

// errOutpostSaving is shown when a save or a load is asked for but the host has no store.
var errOutpostSaving = errors.New("saving is unavailable")

// factory returns the factory of the whole game: the title menu, which starts matches.
func (w *outpostWorld) factory() engine.SceneFactory {
	return func() engine.Scene { return newOutpostApp(w) }
}
