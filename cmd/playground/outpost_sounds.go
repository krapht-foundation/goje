package main

import (
	"errors"
	"fmt"
	"io/fs"

	"github.com/krapht-foundation/goje/audio"
	"github.com/krapht-foundation/goje/engine"
	"github.com/krapht-foundation/goje/examples/skirmish"
)

// The outpost's sound cues. The values are indexes into the bank that
// loadOutpostSounds returns, in the order of outpostSoundNames.
const (
	cueBolt = iota
	cueHit
	cueFall
	cueAlarm
	cueClick
	cueVictory
	cueDefeat
	cueOrder
	outpostCueCount
)

// outpostSoundNames are the files in sounds/, one per cue. They are made with the
// Sound Maker's synthesis from the settings in outpost_sounds_test.go, and a test
// regenerates them so that the files cannot drift from their source.
var outpostSoundNames = [outpostCueCount]string{
	cueBolt:    "bolt",
	cueHit:     "hit",
	cueFall:    "fall",
	cueAlarm:   "alarm",
	cueClick:   "click",
	cueVictory: "victory",
	cueDefeat:  "defeat",
	cueOrder:   "order",
}

// loadOutpostSounds decodes the cue bank from the embedded files.
func loadOutpostSounds(root fs.FS) ([]audio.Clip, error) {
	clips := make([]audio.Clip, 0, outpostCueCount)
	for _, name := range outpostSoundNames {
		file, err := root.Open("sounds/" + name + ".wav")
		if err != nil {
			return nil, fmt.Errorf("open outpost sound %s: %w", name, err)
		}
		clip, decodeErr := audio.DecodeWAV(file)
		if err := errors.Join(decodeErr, file.Close()); err != nil {
			return nil, fmt.Errorf("outpost sound %s: %w", name, err)
		}
		clips = append(clips, clip)
	}
	return clips, nil
}

// play turns what the last step reported into sound. Sound is output only: it
// reads the simulation and never changes it, so a replay plays back the same
// match and sounds the same. Positions are heard relative to the center of the
// screen, so the pan follows what the camera shows. A volley of the same kind
// is one voice per outpostCueGap ticks.
func (s *outpostSession) play(tick engine.Tick) {
	now := s.sim.Ticks()
	for _, e := range s.sim.Events() {
		if s.versus && !s.seen(e.At.X.Float64(), e.At.Y.Float64()) {
			u, ok := s.sim.Unit(e.Unit)
			if !ok || u.Team != s.fogTeam {
				continue
			}
		}
		var cue int
		switch e.Kind {
		case skirmish.EventBoltFired:
			cue = cueBolt
		case skirmish.EventBoltHit:
			cue = cueHit
		case skirmish.EventUnitFell:
			cue = cueFall
		case skirmish.EventAlarm:
			cue = cueAlarm
		default:
			continue
		}
		x, y := s.camera.ToLogical(
			courtyardProjection.Project(e.At.X.Float64(), e.At.Y.Float64(), 0),
		)
		gain, pan := audio.Spatialize(width/2, height/2, x, y, outpostHearing)
		s.cue(tick, cue, now, gain, pan)
	}
	if s.ordered {
		s.cue(tick, cueOrder, now, 1, 0)
	}
	s.ordered = false
	if s.outcome != outcomePlaying && s.outcome != outcomeDraw && s.outcome != s.announced {
		cue := cueVictory
		if s.outcome == outcomeDefeat {
			cue = cueDefeat
		}
		s.cue(tick, cue, now, 1, 0)
	}
	s.announced = s.outcome
}

// cue plays one voice unless the same cue played within outpostCueGap ticks.
func (s *outpostSession) cue(tick engine.Tick, cue int, now uint64, gain, pan float32) {
	gain *= s.world.volume
	if gain <= 0 {
		return
	}
	if last := s.lastCue[cue]; last != 0 && now >= last && now-last < outpostCueGap {
		return
	}
	s.lastCue[cue] = now
	tick.PlaySoundAt(cue, gain, pan)
}

// updateMusic follows the local UI and leaves the loop running across a scene
// transition, so starting a match does not restart the title's track.
func (a *outpostApp) updateMusic(tick engine.Tick) {
	if _, ok := tick.Sound().(engine.MusicOutput); !ok {
		return
	}
	var on bool
	switch a.state {
	case stateTitle, stateLobby:
		on = true
	case statePlaying, statePaused, stateOnline:
		on = a.session.outcome == outcomePlaying
	case stateWatching:
		on = !a.player.Done()
	case stateMatchWatch:
		on = !a.matchPlayer.done()
	case stateLiveWatch:
		on = a.session != nil && a.session.outcome == outcomePlaying &&
			(a.live.recvErr == nil || a.live.applied < a.live.inbox.end)
	case stateOnlineEnded:
	}
	gain := a.world.volume * 0.35
	if a.state == statePaused || a.state == stateOnline && (a.onlineMenu || a.recovery != nil) {
		gain *= 0.3
	}
	switch {
	case !on && a.musicOn:
		tick.StopMusic(500)
	case on && !a.musicOn:
		tick.PlayMusic(0, gain, 500)
	case on && a.musicGain != gain:
		tick.SetMusicGain(gain, 200)
	}
	a.musicOn, a.musicGain = on, gain
}
