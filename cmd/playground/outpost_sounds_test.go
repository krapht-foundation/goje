package main

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/krapht-foundation/goje/internal/soundmaker"
)

var updateSounds = flag.Bool(
	"update-sounds",
	false,
	"rewrite cmd/playground/sounds from the settings",
)

// outpostSoundSettings are the Sound Maker settings of each cue.
var outpostSoundSettings = [outpostCueCount]soundmaker.Settings{
	cueBolt: {
		Waveform:   soundmaker.Square,
		StartHz:    1200,
		EndHz:      600,
		DurationMS: 90,
		AttackMS:   2,
		ReleaseMS:  40,
	},
	cueHit: {
		Waveform:   soundmaker.Triangle,
		StartHz:    300,
		EndHz:      120,
		DurationMS: 120,
		AttackMS:   2,
		ReleaseMS:  80,
	},
	cueFall: {
		Waveform:   soundmaker.Sine,
		StartHz:    400,
		EndHz:      60,
		DurationMS: 350,
		AttackMS:   5,
		ReleaseMS:  200,
	},
	cueAlarm: {
		Waveform:   soundmaker.Square,
		StartHz:    880,
		EndHz:      440,
		DurationMS: 400,
		AttackMS:   10,
		ReleaseMS:  120,
	},
	cueClick: {
		Waveform:   soundmaker.Sine,
		StartHz:    1800,
		EndHz:      1200,
		DurationMS: 40,
		AttackMS:   1,
		ReleaseMS:  20,
	},
	cueVictory: {
		Waveform:   soundmaker.Triangle,
		StartHz:    523,
		EndHz:      1046,
		DurationMS: 500,
		AttackMS:   10,
		ReleaseMS:  250,
	},
	cueDefeat: {
		Waveform:   soundmaker.Sine,
		StartHz:    330,
		EndHz:      110,
		DurationMS: 600,
		AttackMS:   10,
		ReleaseMS:  350,
	},
	cueOrder: {
		Waveform:   soundmaker.Sine,
		StartHz:    660,
		EndHz:      880,
		DurationMS: 60,
		AttackMS:   2,
		ReleaseMS:  30,
	},
}

// TestOutpostSoundsMatchTheirSettings keeps the checked-in files equal to what the
// Sound Maker's synthesis makes from outpostSoundSettings. Run the test with
// -update-sounds after changing a setting.
func TestOutpostSoundsMatchTheirSettings(t *testing.T) {
	for cue, name := range outpostSoundNames {
		doc := soundmaker.New()
		if err := doc.Apply(outpostSoundSettings[cue]); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		want, err := doc.ExportWAV()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		path := filepath.Join("sounds", name+".wav")
		if *updateSounds {
			if err = os.WriteFile(path, want, 0o600); err != nil {
				t.Fatal(err)
			}
			continue
		}
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s differs from its settings; run the test with -update-sounds", path)
		}
	}
}

func TestOutpostSoundsLoad(t *testing.T) {
	clips, err := loadOutpostSounds(authoredFiles)
	if err != nil {
		t.Fatal(err)
	}
	if len(clips) != outpostCueCount {
		t.Fatalf("loaded %d clips, want %d", len(clips), outpostCueCount)
	}
}
