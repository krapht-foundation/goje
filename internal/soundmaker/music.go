package soundmaker

import "github.com/krapht-foundation/goje/audio"

// Music holds the current score of the Music panel, its canonical JSON, and
// its rendered loop. Its zero value is not usable; call NewMusic.
type Music struct {
	score audio.Score
	json  []byte
	pcm   []byte
}

// DefaultScore is the four-bar loop a new Music document starts with.
func DefaultScore() audio.Score {
	notes := func(pitches ...int) []audio.Note {
		out := make([]audio.Note, len(pitches))
		for i, p := range pitches {
			out[i] = audio.Note{Step: 4 * i, Pitch: p, Length: 3, Level: 80}
		}
		return out
	}
	return audio.Score{
		Version: audio.ScoreVersion, Tempo: 120, Steps: 64,
		Channels: []audio.Channel{
			{
				Waveform: audio.Triangle,
				Notes:    notes(45, 45, 52, 45, 41, 41, 48, 41, 48, 48, 55, 48, 43, 43, 50, 43),
			},
			{
				Waveform: audio.Sine,
				Notes:    notes(69, 72, 76, 72, 65, 69, 72, 69, 67, 72, 76, 72, 67, 71, 74, 71),
			},
		},
	}
}

// NewMusic returns a document holding DefaultScore.
func NewMusic() *Music {
	m := &Music{}
	if err := m.Apply(DefaultScore()); err != nil {
		panic(err) //nolint:forbidigo // DefaultScore is a valid constant score.
	}
	return m
}

// Score returns the current score.
func (m *Music) Score() audio.Score { return m.score }

// JSON returns the canonical score that the export writes. The slice is
// shared and MUST NOT be modified.
func (m *Music) JSON() []byte { return m.json }

// PCM returns one loop of the rendered score. The slice is shared and MUST
// NOT be modified.
func (m *Music) PCM() []byte { return m.pcm }

// Apply replaces the score. An invalid candidate leaves the current score,
// its JSON, and its samples untouched.
func (m *Music) Apply(s audio.Score) error {
	data, err := audio.EncodeScore(s)
	if err != nil {
		return err
	}
	clip, err := audio.RenderScore(s)
	if err != nil {
		return err
	}
	m.score, m.json, m.pcm = s, data, clip.PCM()
	return nil
}

// ApplyJSON decodes a score with audio.DecodeScore and applies it.
func (m *Music) ApplyJSON(data []byte) error {
	s, err := audio.DecodeScore(data)
	if err != nil {
		return err
	}
	return m.Apply(s)
}
