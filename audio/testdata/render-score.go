//go:build ignore

// Render-score writes the samples of the score on standard input to standard
// output as raw little-endian signed 16-bit mono PCM at 48 kHz. It is the
// first step of the Beacon music recipe in specs/music-flac-m18.md.
package main

import (
	"io"
	"log"
	"os"

	"github.com/krapht-foundation/goje/audio"
)

func main() {
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		log.Fatal(err)
	}
	s, err := audio.DecodeScore(data)
	if err != nil {
		log.Fatal(err)
	}
	clip, err := audio.RenderScore(s)
	if err != nil {
		log.Fatal(err)
	}
	if _, err := os.Stdout.Write(clip.PCM()); err != nil {
		log.Fatal(err)
	}
}
