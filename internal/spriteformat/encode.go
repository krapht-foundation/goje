package spriteformat

import (
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"slices"
)

// Frame holds sheet coordinates, a frame-local pivot, and fixed-tick duration.
type Frame struct {
	Source image.Rectangle
	Pivot  image.Point
	Ticks  int
}

// Clip is a named frame sequence in authored order.
type Clip struct {
	Name   string
	Frames []Frame
}

// Validate checks typed authoring data. An empty draft is valid here, but not on disk.
func Validate(size image.Point, clips []Clip) error {
	if len(clips) > MaxClips {
		return fmt.Errorf("asset exceeds %d clips", MaxClips)
	}
	var names [MaxClips]string
	remaining := MaxFrames
	for i, clip := range clips {
		if !validName(clip.Name) {
			return fmt.Errorf("clip %d: invalid clip name", i+1)
		}
		if slices.Contains(names[:i], clip.Name) {
			return fmt.Errorf("duplicate clip %q", clip.Name)
		}
		names[i] = clip.Name
		if len(clip.Frames) == 0 {
			return fmt.Errorf("clip %q must contain a frame", clip.Name)
		}
		if len(clip.Frames) > remaining {
			return fmt.Errorf("asset exceeds %d frame entries", MaxFrames)
		}
		remaining -= len(clip.Frames)
		for j, frame := range clip.Frames {
			if err := frame.validate(size); err != nil {
				return fmt.Errorf("clip %q frame %d: %w", clip.Name, j+1, err)
			}
		}
	}
	return nil
}

func (f Frame) validate(size image.Point) error {
	r := f.Source
	if r.Min.X < 0 || r.Min.Y < 0 || r.Max.X > size.X || r.Max.Y > size.Y || r.Empty() {
		return errors.New("source rectangle is outside the sprite sheet or empty")
	}
	return f.validatePose(r.Dx(), r.Dy())
}

type wireClip struct {
	Name   string      `json:"name"`
	Frames []wireFrame `json:"frames"`
}

type wireFrame struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
	PivotX int `json:"pivotX"`
	PivotY int `json:"pivotY"`
	Ticks  int `json:"ticks"`
}

// Encode writes the ordered v1 fields deterministically without touching the PNG.
func Encode(size image.Point, clips []Clip) ([]byte, error) {
	if len(clips) == 0 {
		return nil, errors.New("asset must contain a clip")
	}
	if err := Validate(size, clips); err != nil {
		return nil, err
	}
	count := 0
	for _, clip := range clips {
		count += len(clip.Frames)
	}
	frames := make([]wireFrame, 0, count)
	ordered := make([]wireClip, len(clips))
	for i, clip := range clips {
		start := len(frames)
		for _, f := range clip.Frames {
			frames = append(frames, wireFrame{
				X: f.Source.Min.X, Y: f.Source.Min.Y, Width: f.Source.Dx(), Height: f.Source.Dy(),
				PivotX: f.Pivot.X, PivotY: f.Pivot.Y, Ticks: f.Ticks,
			})
		}
		ordered[i] = wireClip{Name: clip.Name, Frames: frames[start:]}
	}
	data, err := json.Marshal(struct {
		Version int        `json:"version"`
		Clips   []wireClip `json:"clips"`
	}{Version: 1, Clips: ordered})
	if err != nil {
		return nil, fmt.Errorf("encode sprite clips: %w", err)
	}
	return data, nil
}
