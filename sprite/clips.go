package sprite

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"io"
	"slices"
	"strconv"
	"unicode/utf8"
)

const (
	maxJSONBytes = 64 << 10
	maxClips     = 32
	maxFrames    = 256
	maxTicks     = 3600
)

func decodeClips(r io.Reader, size image.Point) (map[string]Clip, error) {
	data, readErr := readBounded(r, maxJSONBytes)
	if readErr != nil {
		return nil, readErr
	}
	if !utf8.Valid(data) {
		return nil, errors.New("metadata must be UTF-8")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	var clips map[string]Clip
	parseErr := readObject(d, []string{"version", "clips"}, func(field int) error {
		if field == 0 {
			version, err := readInt(d)
			if err != nil {
				return err
			}
			if version != 1 {
				return fmt.Errorf("unsupported version %d", version)
			}
			return nil
		}
		var err error
		clips, err = readClips(d, size)
		return err
	})
	if parseErr != nil {
		return nil, parseErr
	}
	if _, err := d.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("unexpected data after sprite metadata")
	}
	return clips, nil
}

func readClips(d *json.Decoder, size image.Point) (map[string]Clip, error) {
	if err := delimiter(d, '['); err != nil {
		return nil, err
	}
	clips := make(map[string]Clip)
	frameCount := 0
	for d.More() {
		if len(clips) == maxClips {
			return nil, fmt.Errorf("asset exceeds %d clips", maxClips)
		}
		var name string
		var frames []Frame
		err := readObject(d, []string{"name", "frames"}, func(field int) error {
			if field == 0 {
				token, err := d.Token()
				if err != nil {
					return fmt.Errorf("read name: %w", err)
				}
				var ok bool
				name, ok = token.(string)
				if !ok || !validName(name) {
					return errors.New("invalid clip name")
				}
				if _, exists := clips[name]; exists {
					return fmt.Errorf("duplicate clip %q", name)
				}
				return nil
			}
			var err error
			frames, err = readFrames(d, size, maxFrames-frameCount)
			return err
		})
		if err != nil {
			return nil, fmt.Errorf("clip %d: %w", len(clips)+1, err)
		}
		clips[name] = Clip{frames: frames}
		frameCount += len(frames)
	}
	if len(clips) == 0 {
		return nil, errors.New("asset must contain a clip")
	}
	if err := delimiter(d, ']'); err != nil {
		return nil, err
	}
	return clips, nil
}

func readFrames(d *json.Decoder, size image.Point, remaining int) ([]Frame, error) {
	if err := delimiter(d, '['); err != nil {
		return nil, err
	}
	var frames []Frame
	for d.More() {
		if len(frames) == remaining {
			return nil, fmt.Errorf("asset exceeds %d frame entries", maxFrames)
		}
		frame, err := readFrame(d, size)
		if err != nil {
			return nil, fmt.Errorf("frame %d: %w", len(frames)+1, err)
		}
		frames = append(frames, frame)
	}
	if len(frames) == 0 {
		return nil, errors.New("clip must contain a frame")
	}
	if err := delimiter(d, ']'); err != nil {
		return nil, err
	}
	return frames, nil
}

func readFrame(d *json.Decoder, size image.Point) (Frame, error) {
	var values [7]int
	fields := []string{"x", "y", "width", "height", "pivotX", "pivotY", "ticks"}
	err := readObject(d, fields, func(field int) error {
		var err error
		values[field], err = readInt(d)
		return err
	})
	if err != nil {
		return Frame{}, err
	}
	x, y, w, h := values[0], values[1], values[2], values[3]
	// Subtract dimensions only after checking the origin, before adding endpoints.
	if x < 0 || x >= size.X || y < 0 || y >= size.Y || w < 1 || w > size.X-x || h < 1 ||
		h > size.Y-y {
		return Frame{}, errors.New("source rectangle is outside the sprite sheet or empty")
	}
	px, py, ticks := values[4], values[5], values[6]
	if px < 0 || px > w || py < 0 || py > h {
		return Frame{}, errors.New("pivot is outside frame edges")
	}
	if ticks < 1 || ticks > maxTicks {
		return Frame{}, fmt.Errorf("ticks must be 1–%d", maxTicks)
	}
	return Frame{Source: image.Rect(x, y, x+w, y+h), Pivot: image.Pt(px, py), Ticks: ticks}, nil
}

// Read only schema-shaped tokens. An unknown key cannot introduce a subtree,
// and the field mask rejects duplicates before they can overwrite data.
func readObject(d *json.Decoder, fields []string, readField func(int) error) error {
	if err := delimiter(d, '{'); err != nil {
		return err
	}
	var seen uint
	for d.More() {
		token, err := d.Token()
		if err != nil {
			return fmt.Errorf("read field name: %w", err)
		}
		name, ok := token.(string)
		if !ok {
			return errors.New("expected field name")
		}
		field := slices.Index(fields, name)
		if field < 0 {
			return fmt.Errorf("unknown field %q", name)
		}
		if seen&(1<<field) != 0 {
			return fmt.Errorf("duplicate field %q", name)
		}
		seen |= 1 << field
		if err := readField(field); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	if err := delimiter(d, '}'); err != nil {
		return err
	}
	for i, name := range fields {
		if seen&(1<<i) == 0 {
			return fmt.Errorf("missing field %q", name)
		}
	}
	return nil
}

func delimiter(d *json.Decoder, want json.Delim) error {
	token, err := d.Token()
	if err != nil {
		return fmt.Errorf("read %q: %w", want, err)
	}
	if token != want {
		return fmt.Errorf("expected %q", want)
	}
	return nil
}

func readInt(d *json.Decoder) (int, error) {
	token, err := d.Token()
	if err != nil {
		return 0, fmt.Errorf("read integer: %w", err)
	}
	number, ok := token.(json.Number)
	if !ok {
		return 0, errors.New("expected integer")
	}
	value, err := strconv.Atoi(string(number))
	if err != nil {
		return 0, errors.New("expected integer in range")
	}
	return value, nil
}

func validName(name string) bool {
	if name == "" || len(name) > 32 || name[0] < 'a' || name[0] > 'z' {
		return false
	}
	for i := 1; i < len(name); i++ {
		c := name[i]
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '_' {
			return false
		}
	}
	return true
}
