package soundmaker_test

import (
	"bytes"
	"testing"

	"github.com/krapht-foundation/goje/audio"
	"github.com/krapht-foundation/goje/internal/soundmaker"
)

func TestMusicAppliesExportsAndKeepsRejectedScores(t *testing.T) {
	t.Parallel()
	m := soundmaker.NewMusic()
	clip, err := audio.RenderScore(soundmaker.DefaultScore())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(m.PCM(), clip.PCM()) {
		t.Fatal("the default document does not play the default score")
	}
	exported := bytes.Clone(m.JSON())
	if s, decodeErr := audio.DecodeScore(exported); decodeErr != nil || s.Tempo != 120 {
		t.Fatalf("export decodes to %+v, %v", s, decodeErr)
	}
	if err = m.ApplyJSON([]byte(`{"version":1}`)); err == nil {
		t.Fatal("an invalid score was applied")
	}
	if !bytes.Equal(m.JSON(), exported) || !bytes.Equal(m.PCM(), clip.PCM()) {
		t.Fatal("a rejected score changed the document")
	}
	next := soundmaker.DefaultScore()
	next.Tempo = 90
	data, err := audio.EncodeScore(next)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.ApplyJSON(data); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(m.JSON(), data) || m.Score().Tempo != 90 || len(m.PCM()) == len(clip.PCM()) {
		t.Fatal("an applied score did not replace the export and the samples")
	}
}
