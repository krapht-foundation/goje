package matchlog

import "encoding/binary"

// recordMark fixes an endpoint even if later empty ticks extended its last run.
type recordMark struct {
	end   int
	empty uint32
}

func (r *Recorder) mark() recordMark {
	m := recordMark{end: len(r.body)}
	if r.empty >= 0 {
		m.empty = binary.LittleEndian.Uint32(r.body[r.empty:])
	}
	return m
}

func (r *Recorder) markAt(tick int) recordMark {
	switch tick {
	case 0:
		return recordMark{}
	case r.ticks:
		return r.mark()
	default:
		return r.marks[tick/Interval-1]
	}
}

// Ticks returns how many completed ticks the recorder has kept.
func (r *Recorder) Ticks() int { return r.ticks }

// Checkpoint returns the hash after tick, where tick is the number of completed
// ticks. A hash exists after each interval and after the last recorded tick.
func (r *Recorder) Checkpoint(tick int) (uint64, bool) {
	switch {
	case tick < 1 || tick > r.ticks:
		return 0, false
	case tick == r.ticks:
		return r.lastHash, true
	case tick%Interval != 0:
		return 0, false
	default:
		return binary.LittleEndian.Uint64(r.hashes[(tick/Interval-1)*8:]), true
	}
}

// Segment returns a separately owned log of the completed ticks in [first, last).
// Both endpoints count completed ticks. First must be zero or an interval
// boundary; last can also be the current end. Its checkpoints are relative to
// first. Playback of a nonzero segment must start from the state after first
// ticks, not the initial state named by the header.
func (r *Recorder) Segment(first, last int) ([]byte, bool) {
	if first < 0 || first%Interval != 0 || last < first || last > r.ticks ||
		last%Interval != 0 && last != r.ticks {
		return nil, false
	}
	start, end := r.markAt(first), r.markAt(last)
	var lead uint32
	if start.empty != 0 {
		if start.end == end.end {
			lead = end.empty - start.empty
		} else {
			lead = binary.LittleEndian.Uint32(r.body[start.end-4:]) - start.empty
		}
	}
	ticks := last - first
	h := r.header
	size := fixedHeader + len(
		h.Game,
	) + len(
		h.ContentName,
	) + end.end - start.end + 8*checkpointCount(
		ticks,
	)
	if lead != 0 {
		size += 4
	}
	data := appendHeader(make([]byte, 0, size), h, ticks)
	if lead != 0 {
		data = binary.LittleEndian.AppendUint32(data, lead)
	}
	data = append(data, r.body[start.end:end.end]...)
	if end.empty != 0 && end.end != start.end {
		binary.LittleEndian.PutUint32(data[len(data)-4:], end.empty)
	}
	data = append(data, r.hashes[first/Interval*8:last/Interval*8]...)
	if ticks%Interval != 0 {
		data = binary.LittleEndian.AppendUint64(data, r.lastHash)
	}
	return data, true
}

// Rewind drops later ticks and allows Append again at an interval boundary or
// the initial state. An invalid endpoint leaves the recorder unchanged.
func (r *Recorder) Rewind(tick int) bool {
	if tick < 0 || tick > r.ticks || tick%Interval != 0 {
		return false
	}
	m := r.markAt(tick)
	r.lastHash, _ = r.Checkpoint(tick)
	r.ticks, r.empty, r.full = tick, -1, false
	r.body, r.hashes, r.marks = r.body[:m.end], r.hashes[:tick/Interval*8], r.marks[:tick/Interval]
	if m.empty != 0 {
		r.empty = m.end - 4
		binary.LittleEndian.PutUint32(r.body[r.empty:], m.empty)
	}
	return true
}
