package skirmish

import (
	"encoding/binary"
	"errors"

	"github.com/krapht-foundation/goje/engine/grid"
)

// CommandKind says which way a Command changes units.
type CommandKind uint8

const (
	// CommandOrder gives Order to the units, as OrderGroup does.
	CommandOrder CommandKind = iota
	// CommandHold sets or clears the units' hold-fire stance, as SetHold does.
	CommandHold
)

// MaxCommandBytes is the largest payload DecodeCommands accepts.
const MaxCommandBytes = 1024

// Command is one change to a group of units, in the form a player issues it.
// Order is used by CommandOrder and Hold by CommandHold.
type Command struct {
	Kind  CommandKind
	IDs   []uint64
	Order Order
	Hold  bool
}

var errCommand = errors.New("skirmish: malformed command payload")

// Apply runs c and returns how many units took it. CommandOrder is OrderGroup,
// and CommandHold calls SetHold for each ID and counts the living units. Apply
// accepts any IDs; which player may command which units is a rule of the game.
func (s *Sim) Apply(c Command) int {
	switch c.Kind {
	case CommandOrder:
		return s.OrderGroup(c.IDs, c.Order)
	case CommandHold:
		n := 0
		for _, id := range c.IDs {
			if s.SetHold(id, c.Hold) {
				n++
			}
		}
		return n
	default:
		return 0
	}
}

// AppendTo appends c's binary form to dst: the kind byte, the ID count and the
// IDs as uvarints, and then, for an order, the order kind byte, the goal cell
// as two zigzag varints, and the target as a uvarint, or, for a hold, one flag
// byte. The form is described in specs/skirmish-m13.md.
func (c Command) AppendTo(dst []byte) []byte {
	dst = append(dst, byte(c.Kind))
	dst = binary.AppendUvarint(dst, uint64(len(c.IDs)))
	for _, id := range c.IDs {
		dst = binary.AppendUvarint(dst, id)
	}
	switch c.Kind {
	case CommandOrder:
		dst = append(dst, byte(c.Order.Kind))
		dst = binary.AppendVarint(dst, int64(c.Order.Goal.X))
		dst = binary.AppendVarint(dst, int64(c.Order.Goal.Y))
		dst = binary.AppendUvarint(dst, c.Order.Target)
	case CommandHold:
		flag := byte(0)
		if c.Hold {
			flag = 1
		}
		dst = append(dst, flag)
	}
	return dst
}

// DecodeCommands appends the commands in payload, written back to back by
// AppendTo, to dst. Payloads are untrusted: an oversized, truncated, or
// non-canonical payload, an unknown kind, more than MaxUnits IDs, an order kind
// out of range, a hold flag other than 0 or 1, or bytes left over is an error,
// and dst is then returned at its original length. Commands in dst's spare
// capacity are reused with their ID storage, so a dst that has held the same
// commands before decodes without allocating.
func DecodeCommands(dst []Command, payload []byte) ([]Command, error) {
	if len(payload) > MaxCommandBytes {
		return dst, errCommand
	}
	start := len(dst)
	for b := payload; len(b) > 0; {
		if len(dst) == cap(dst) {
			dst = append(dst, Command{})
		} else {
			dst = dst[:len(dst)+1]
		}
		c := &dst[len(dst)-1]
		var ok bool
		if b, ok = decodeCommand(c, b); !ok {
			return dst[:start], errCommand
		}
	}
	return dst, nil
}

// decodeCommand decodes the command at the start of b into c, reusing c.IDs,
// and returns the rest of b.
func decodeCommand(c *Command, b []byte) ([]byte, bool) {
	kind := CommandKind(b[0])
	if kind > CommandHold {
		return nil, false
	}
	count, b, ok := uvarint(b[1:])
	// Each ID takes at least one byte, so the count is checked against the
	// bytes left before anything is allocated.
	if !ok || count > MaxUnits || count > uint64(len(b)) {
		return nil, false
	}
	ids := c.IDs[:0]
	if cap(ids) < int(count) { // #nosec G115 -- count is at most MaxUnits.
		ids = make([]uint64, 0, count)
	}
	for range count {
		var id uint64
		if id, b, ok = uvarint(b); !ok {
			return nil, false
		}
		ids = append(ids, id)
	}
	*c = Command{Kind: kind, IDs: ids}
	if len(b) == 0 {
		return nil, false
	}
	switch kind {
	case CommandOrder:
		c.Order.Kind = OrderKind(b[0])
		if c.Order.Kind > AttackMove {
			return nil, false
		}
		var x, y uint64
		if x, b, ok = uvarint(b[1:]); !ok {
			return nil, false
		}
		if y, b, ok = uvarint(b); !ok {
			return nil, false
		}
		if c.Order.Target, b, ok = uvarint(b); !ok {
			return nil, false
		}
		c.Order.Goal = grid.Cell{X: unzigzag(x), Y: unzigzag(y)}
	case CommandHold:
		if b[0] > 1 {
			return nil, false
		}
		c.Hold, b = b[0] == 1, b[1:]
	}
	return b, true
}

// uvarint reads one uvarint from b and returns the rest. It refuses truncated
// and overflowing values and, so that every accepted payload has exactly one
// form, encodings longer than needed.
func uvarint(b []byte) (v uint64, rest []byte, ok bool) {
	v, n := binary.Uvarint(b)
	if n <= 0 || (n > 1 && b[n-1] == 0) {
		return 0, nil, false
	}
	return v, b[n:], true
}

// unzigzag undoes the zigzag mapping binary.AppendVarint uses for signed values.
func unzigzag(u uint64) int {
	return int(int64(u>>1) ^ -int64(u&1)) // #nosec G115 -- int is 64 bits on every target.
}
