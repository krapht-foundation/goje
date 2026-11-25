package skirmish

import (
	"bytes"
	"math"
	"math/rand/v2"
	"reflect"
	"testing"

	"github.com/krapht-foundation/goje/engine/grid"
)

// sampleCommands has every kind, every order kind, the boundary ID counts, and
// extreme IDs, cells, and targets.
func sampleCommands() []Command {
	full := make([]uint64, MaxUnits)
	for i := range full {
		full[i] = uint64(i + 1)
	}
	return []Command{
		{Kind: CommandOrder, Order: Order{Kind: Move}}, // Decoding gives nil IDs for none.
		{Kind: CommandOrder, IDs: []uint64{1}, Order: Order{Kind: Move, Goal: grid.Cell{X: 3, Y: 4}}},
		{Kind: CommandOrder, IDs: []uint64{math.MaxUint64, 0, 7, 7}, Order: Order{
			Kind: Attack, Goal: grid.Cell{X: math.MinInt64, Y: math.MaxInt64}, Target: math.MaxUint64,
		}},
		{
			Kind:  CommandOrder,
			IDs:   full,
			Order: Order{Kind: AttackMove, Goal: grid.Cell{X: -1, Y: -200}},
		},
		{Kind: CommandHold, Hold: true},
		{Kind: CommandHold, IDs: []uint64{2, 300}},
		{Kind: CommandHold, IDs: full, Hold: true},
	}
}

func TestCommandsRoundTrip(t *testing.T) {
	var payload []byte
	for _, c := range sampleCommands() {
		one := c.AppendTo(nil)
		got, err := DecodeCommands(nil, one)
		if err != nil || len(got) != 1 || !reflect.DeepEqual(got[0], c) {
			t.Fatalf("round trip of %+v = %+v, %v", c, got, err)
		}
		if c.Kind == CommandHold {
			payload = c.AppendTo(payload) // The full orders together exceed a payload.
		}
	}
	got, err := DecodeCommands(nil, payload)
	if err != nil || len(got) != 3 {
		t.Fatalf("decoding three holds back to back = %d commands, %v", len(got), err)
	}
	if got, err := DecodeCommands(nil, nil); err != nil || len(got) != 0 {
		t.Fatalf("empty payload = %+v, %v", got, err)
	}
}

func TestDecodeCommandsRejectsMalformedPayloads(t *testing.T) {
	order := Command{Kind: CommandOrder, IDs: []uint64{1, 2}, Order: Order{Kind: Attack, Target: 9}}.
		AppendTo(nil)
	hold := Command{Kind: CommandHold, IDs: []uint64{1}, Hold: true}.AppendTo(nil)
	// MaxUnits+1 IDs, all zero, and a hold flag.
	tooMany := append([]byte{byte(CommandHold), 0x81, 0x02}, make([]byte, MaxUnits+2)...)
	oversized := []byte{}
	for len(oversized) <= MaxCommandBytes {
		oversized = append(oversized, hold...)
	}
	cases := map[string][]byte{
		"unknown kind":     {2, 0, 0},
		"too many IDs":     tooMany,
		"count over bytes": {byte(CommandHold), 5, 1, 0},
		"order kind":       {byte(CommandOrder), 0, byte(AttackMove) + 1, 0, 0, 0},
		"hold flag":        {byte(CommandHold), 0, 2},
		"trailing byte":    append(append([]byte{}, hold...), 1),
		"oversized":        oversized,
		"non-canonical":    {byte(CommandHold), 0x80, 0x00, 0},
		"overflowing ID": {
			byte(CommandHold),
			1,
			0xff,
			0xff,
			0xff,
			0xff,
			0xff,
			0xff,
			0xff,
			0xff,
			0xff,
			0x7f,
			0,
		},
		"truncated varint":  {byte(CommandHold), 1, 0x80},
		"no hold flag":      {byte(CommandHold), 0},
		"truncated order":   order[:len(order)-1],
		"no order kind":     order[:4],
		"truncated goal y":  order[:6],
		"kind only":         {byte(CommandOrder)},
		"valid then broken": append(append([]byte{}, hold...), 9),
	}
	for name, payload := range cases {
		dst := make([]Command, 1, 4)
		got, err := DecodeCommands(dst, payload)
		if err == nil || len(got) != 1 {
			t.Errorf(
				"%s: DecodeCommands = %d commands, %v; want an error and dst unchanged",
				name,
				len(got),
				err,
			)
		}
	}
}

func TestDecodeCommandsReusesDstWithoutAllocating(t *testing.T) {
	var payload []byte
	for _, c := range sampleCommands()[:6] {
		payload = c.AppendTo(payload)
	}
	dst, err := DecodeCommands(nil, payload)
	if err != nil {
		t.Fatal(err)
	}
	if allocs := testing.AllocsPerRun(100, func() {
		if dst, err = DecodeCommands(dst[:0], payload); err != nil || len(dst) != 6 {
			t.Fatal(len(dst), err)
		}
	}); allocs != 0 {
		t.Fatalf("DecodeCommands into a reused dst allocated %v times", allocs)
	}
}

// TestApplyMatchesDirectCalls drives two identical simulations with random
// commands, one through the encoding and Apply and one through OrderGroup and
// SetHold, and requires equal counts and equal traces.
func TestApplyMatchesDirectCalls(t *testing.T) {
	for seed := range uint64(4) {
		h := newOrderHistory(seed + 11)
		h.radius = int(seed%2) * 5
		applied, direct := h.build(t), h.build(t)
		rng := rand.New(rand.NewPCG(seed, 1))
		var payload []byte
		var decoded []Command
		for tick := range 900 {
			if tick%4 == 0 {
				c := randomCommand(rng)
				payload = c.AppendTo(payload[:0])
				var err error
				if decoded, err = DecodeCommands(decoded[:0], payload); err != nil {
					t.Fatal(err)
				}
				var want int
				switch c.Kind {
				case CommandOrder:
					want = direct.OrderGroup(c.IDs, c.Order)
				case CommandHold:
					for _, id := range c.IDs {
						if direct.SetHold(id, c.Hold) {
							want++
						}
					}
				}
				if got := applied.Apply(decoded[0]); got != want {
					t.Fatalf("seed %d tick %d: Apply(%+v) = %d, want %d", seed, tick, c, got, want)
				}
			}
			step(t, applied, 1)
			step(t, direct, 1)
		}
		if applied.Trace() != direct.Trace() {
			t.Fatalf("seed %d: traces %d and %d differ", seed, applied.Trace(), direct.Trace())
		}
	}
}

func randomCommand(rng *rand.Rand) Command {
	ids := make([]uint64, rng.IntN(6))
	for i := range ids {
		ids[i] = uint64(rng.IntN(16)) // 0, 15, and dead units test unknown IDs.
	}
	if rng.IntN(4) == 0 {
		return Command{Kind: CommandHold, IDs: ids, Hold: rng.IntN(2) == 0}
	}
	return Command{Kind: CommandOrder, IDs: ids, Order: Order{
		Kind:   OrderKind(rng.IntN(3)),
		Goal:   grid.Cell{X: rng.IntN(14) - 1, Y: rng.IntN(10) - 1},
		Target: uint64(rng.IntN(16)),
	}}
}

func FuzzDecodeCommands(f *testing.F) {
	for _, c := range sampleCommands()[:6] {
		f.Add(c.AppendTo(nil))
	}
	f.Add([]byte{byte(CommandHold), 0x80, 0x00, 0})
	f.Fuzz(func(t *testing.T, payload []byte) {
		got, err := DecodeCommands(nil, payload)
		if err != nil {
			return
		}
		var again []byte
		for _, c := range got {
			again = c.AppendTo(again)
		}
		if !bytes.Equal(again, payload) {
			t.Fatalf("accepted %x but re-encoded it as %x", payload, again)
		}
	})
}
