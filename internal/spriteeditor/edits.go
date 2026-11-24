package spriteeditor

import (
	"errors"
	"slices"

	"github.com/krapht-foundation/goje/internal/spriteformat"
)

// AddClip appends a named clip with one frame.
func (d *Document) AddClip(name string, first spriteformat.Frame) error {
	clips := slices.Clip(d.Clips())
	return d.apply(append(clips, spriteformat.Clip{Name: name, Frames: []spriteformat.Frame{first}}))
}

// RenameClip changes a clip's name without changing its position or frames.
func (d *Document) RenameClip(index int, name string) error {
	clips, err := d.editClip(index)
	if err != nil {
		return err
	}
	clips[index].Name = name
	return d.apply(clips)
}

// DeleteClip removes a clip, including all of its frames.
func (d *Document) DeleteClip(index int) error {
	clips, err := d.editClip(index)
	if err != nil {
		return err
	}
	return d.apply(slices.Delete(clips, index, index+1))
}

// AddFrame appends a frame to the selected clip.
func (d *Document) AddFrame(clip int, frame spriteformat.Frame) error {
	clips, err := d.editClip(clip)
	if err != nil {
		return err
	}
	clips[clip].Frames = append(slices.Clip(clips[clip].Frames), frame)
	return d.apply(clips)
}

// SetFrame replaces a frame's rectangle, pivot, and duration as one edit.
func (d *Document) SetFrame(clip, index int, frame spriteformat.Frame) error {
	clips, err := d.editFrames(clip, index)
	if err != nil {
		return err
	}
	clips[clip].Frames[index] = frame
	return d.apply(clips)
}

// DuplicateFrame inserts a copy immediately after the selected frame.
func (d *Document) DuplicateFrame(clip, index int) error {
	clips, err := d.editClip(clip)
	if err != nil {
		return err
	}
	frames := clips[clip].Frames
	if index < 0 || index >= len(frames) {
		return errors.New("frame index is outside the clip")
	}
	// Clamp capacity so insertion cannot overwrite a prior revision's backing array.
	clips[clip].Frames = slices.Insert(slices.Clip(frames), index+1, frames[index])
	return d.apply(clips)
}

// DeleteFrame removes the selected frame. Use DeleteClip for the last frame.
func (d *Document) DeleteFrame(clip, index int) error {
	clips, err := d.editFrames(clip, index)
	if err != nil {
		return err
	}
	clips[clip].Frames = slices.Delete(clips[clip].Frames, index, index+1)
	return d.apply(clips)
}

// MoveFrame moves an existing frame to another index within its clip.
func (d *Document) MoveFrame(clip, from, to int) error {
	clips, err := d.editFrames(clip, from)
	if err != nil {
		return err
	}
	frames := clips[clip].Frames
	if to < 0 || to >= len(frames) {
		return errors.New("frame destination is outside the clip")
	}
	frame := frames[from]
	if from < to {
		copy(frames[from:to], frames[from+1:to+1])
	} else {
		copy(frames[to+1:from+1], frames[to:from])
	}
	frames[to] = frame
	return d.apply(clips)
}

func (d *Document) editClip(index int) ([]spriteformat.Clip, error) {
	clips := d.Clips()
	if index < 0 || index >= len(clips) {
		return nil, errors.New("clip index is outside the document")
	}
	return slices.Clone(clips), nil
}

func (d *Document) editFrames(clip, index int) ([]spriteformat.Clip, error) {
	clips, err := d.editClip(clip)
	if err != nil {
		return nil, err
	}
	frames := clips[clip].Frames
	if index < 0 || index >= len(frames) {
		return nil, errors.New("frame index is outside the clip")
	}
	clips[clip].Frames = slices.Clone(frames)
	return clips, nil
}
