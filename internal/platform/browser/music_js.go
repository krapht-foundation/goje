//go:build wasm

package browser

import "syscall/js"

// webMusic mirrors the music voice of audio.Mixer with scheduled Web Audio
// nodes: each track is a looping buffer source → gain → destination, and gain
// ramps are linearRampToValueAtTime. A replacement ramps the current track to
// zero and starts the next one at the end of that ramp, so both hosts switch
// on the same frame. scripts/smoke-mixer.mjs mirrors this graph; keep the two
// in step.
type webMusic struct {
	current, next webTrack
	stopping      bool
	switchAt      float64 // context time when current ends and next begins
}

// webTrack is one scheduled track and the latest linear ramp of its gain.
type webTrack struct {
	source, gain js.Value // undefined when absent
	track        int
	from, to     float64
	start, stop  float64 // the ramp's context times
}

func (m *webMusic) play(context, buffer js.Value, track int, gain, now, fade float64) {
	m.settle(now)
	switch {
	case m.current.source.IsUndefined():
		m.current = newTrack(context, buffer, track, gain, now, fade)
	case m.current.track == track && !m.stopping:
		m.next.end()
		m.current.ramp(gain, now, fade)
	default:
		m.next.end()
		m.current.ramp(0, now, fade)
		m.stopping, m.switchAt = false, now+fade
		m.next = newTrack(context, buffer, track, gain, m.switchAt, fade)
	}
}

func (m *webMusic) stop(now, fade float64) {
	m.settle(now)
	if m.current.source.IsUndefined() {
		return
	}
	m.next.end()
	m.current.ramp(0, now, fade)
	m.stopping, m.switchAt = true, now+fade
}

func (m *webMusic) setGain(gain, now, fade float64) {
	m.settle(now)
	switch {
	case m.current.source.IsUndefined() || m.stopping:
	case !m.next.source.IsUndefined():
		m.next.schedule(0, gain, m.next.start, m.next.stop-m.next.start)
	default:
		m.current.ramp(gain, now, fade)
	}
}

// settle releases a track whose fade out has ended and promotes the next one.
// Until then the ended track loops silently at gain zero.
func (m *webMusic) settle(now float64) {
	pending := m.stopping || !m.next.source.IsUndefined()
	if !pending || now < m.switchAt {
		return
	}
	m.current.end()
	m.current, m.next, m.stopping = m.next, webTrack{}, false
}

func newTrack(context, buffer js.Value, track int, gain, at, fade float64) webTrack {
	source := context.Call("createBufferSource")
	source.Set("buffer", buffer)
	source.Set("loop", true)
	t := webTrack{source: source, gain: context.Call("createGain"), track: track}
	t.schedule(0, gain, at, fade)
	source.Call("connect", t.gain)
	t.gain.Call("connect", context.Get("destination"))
	source.Call("start", at)
	return t
}

// ramp moves the gain linearly from its current value to target.
func (t *webTrack) ramp(target, now, fade float64) {
	from := t.to
	switch {
	case now <= t.start:
		from = t.from
	case now < t.stop:
		from = t.from + (t.to-t.from)*(now-t.start)/(t.stop-t.start)
	}
	t.schedule(from, target, now, fade)
}

func (t *webTrack) schedule(from, to, at, fade float64) {
	gain := t.gain.Get("gain")
	gain.Call("cancelScheduledValues", at)
	if fade > 0 {
		gain.Call("setValueAtTime", from, at)
		gain.Call("linearRampToValueAtTime", to, at+fade)
	} else {
		gain.Call("setValueAtTime", to, at)
	}
	t.from, t.to, t.start, t.stop = from, to, at, at+fade
}

func (t *webTrack) end() {
	if t.source.IsUndefined() {
		return
	}
	t.source.Call("stop") // Harmless on a source that has not started.
	t.gain.Call("disconnect")
	*t = webTrack{}
}
