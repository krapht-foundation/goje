package sprite_test

import (
	"bytes"
	_ "embed"
	"fmt"
	"image"
	"image/png"
	"strings"
	"testing"

	"github.com/krapht-foundation/goje/sprite"
)

//go:embed testdata/tomato.png
var tomatoPNG []byte

//go:embed testdata/tomato.json
var tomatoJSON string

func TestTomatoSheet(t *testing.T) {
	t.Parallel()
	sheet, err := sprite.Decode(bytes.NewReader(tomatoPNG), strings.NewReader(tomatoJSON))
	if err != nil {
		t.Fatal(err)
	}
	move, found := sheet.Clip("move")
	if !found || move.Len() != 3 {
		t.Fatal("expected a three-frame move clip")
	}
	hop := move.Frame(1)
	want := sprite.Frame{Source: image.Rect(74, 0, 98, 24), Pivot: image.Pt(12, 24), Ticks: 8}
	if hop != want {
		t.Fatalf("hop frame = %+v, want %+v", hop, want)
	}
	_, _, _, body := sheet.Image().At(hop.Source.Min.X+12, 14).RGBA()
	_, _, _, gap := sheet.Image().At(hop.Source.Min.X+12, 22).RGBA()
	if body == 0 || gap != 0 {
		t.Fatal("the airborne frame must have visible art above a transparent ground gap")
	}
}

func TestDecodeInvalidClips(t *testing.T) {
	t.Parallel()
	const frame = `{"x":0,"y":0,"width":24,"height":20,"pivotX":12,"pivotY":20,"ticks":1}`
	const clip = `{"name":"idle","frames":[` + frame + `]}`
	const doc = `{"version":1,"clips":[` + clip + `]}`
	cases := map[string]string{
		"missing field":        strings.Replace(doc, `,"pivotY":20`, "", 1),
		"unknown field":        strings.Replace(doc, `"x":0`, `"X":0`, 1),
		"duplicate field":      strings.Replace(doc, `"x":0`, `"x":0,"x":0`, 1),
		"null value":           strings.Replace(doc, `"x":0`, `"x":null`, 1),
		"unsupported version":  strings.Replace(doc, `"version":1`, `"version":2`, 1),
		"overflowing endpoint": strings.Replace(doc, `"x":0`, `"x":9223372036854775807`, 1),
		"outside sheet":        strings.Replace(doc, `"width":24`, `"width":99`, 1),
		"outside pivot":        strings.Replace(doc, `"pivotY":20`, `"pivotY":21`, 1),
		"decimal duration":     strings.Replace(doc, `"ticks":1`, `"ticks":1.0`, 1),
		"zero duration":        strings.Replace(doc, `"ticks":1`, `"ticks":0`, 1),
		"empty clips":          `{"version":1,"clips":[]}`,
		"empty frames":         strings.Replace(doc, frame, "", 1),
		"duplicate clip":       `{"version":1,"clips":[` + clip + "," + clip + `]}`,
		"trailing JSON":        doc + `null`,
		"invalid UTF-8":        doc + "\xff",
		"byte budget":          doc + strings.Repeat(" ", 64<<10),
		"total frame budget": `{"version":1,"clips":[{"name":"idle","frames":[` +
			strings.Repeat(frame+",", 128) + frame + `]},{"name":"move","frames":[` +
			strings.Repeat(frame+",", 127) + frame + `]}]}`,
	}
	for name, metadata := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if sheet, err := sprite.Decode(
				bytes.NewReader(tomatoPNG),
				strings.NewReader(metadata),
			); err == nil ||
				sheet != nil {
				t.Fatalf("invalid metadata returned sheet %v, error %v", sheet, err)
			}
		})
	}
}

func TestDecodeInvalidPNG(t *testing.T) {
	t.Parallel()
	var wide bytes.Buffer
	if err := png.Encode(&wide, image.NewGray(image.Rect(0, 0, 513, 1))); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{
		"malformed":   []byte("not a PNG"),
		"truncated":   tomatoPNG[:len(tomatoPNG)-12],
		"wide axis":   wide.Bytes(),
		"byte budget": append(bytes.Clone(tomatoPNG), make([]byte, 1<<20)...),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if sheet, err := sprite.Decode(
				bytes.NewReader(data),
				strings.NewReader(tomatoJSON),
			); err == nil ||
				sheet != nil {
				t.Fatalf("invalid PNG returned sheet %v, error %v", sheet, err)
			}
		})
	}
}

func ExampleDecode() {
	sheet, err := sprite.Decode(bytes.NewReader(tomatoPNG), strings.NewReader(tomatoJSON))
	if err != nil {
		fmt.Println(err)
		return
	}
	if idle, found := sheet.Clip("idle"); found {
		fmt.Printf("%d idle frames; first lasts %d ticks\n", idle.Len(), idle.Frame(0).Ticks)
	}
	// Output: 2 idle frames; first lasts 114 ticks
}
