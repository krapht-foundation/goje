package main

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/krapht-foundation/goje/engine/lockstep"
	"github.com/krapht-foundation/goje/engine/matchlog"
	"github.com/krapht-foundation/goje/netlink"
)

const (
	proofMessage byte = 0x54
	finalMessage byte = 0x55
	resumeOffer  byte = 0x56
	resumeReady  byte = 0x57
	proofSize         = 1 + 4 + 8
)

type onlineProof struct {
	tick int
	hash uint64
}

func (p onlineProof) send(link *outpostLink, kind byte) error {
	var msg [proofSize]byte
	msg[0] = kind
	binary.LittleEndian.PutUint32(
		msg[1:],
		uint32(p.tick),
	) // #nosec G115 -- limited by matchlog.MaxTicks.
	binary.LittleEndian.PutUint64(msg[5:], p.hash)
	return link.Send(msg[:])
}

func decodeOnlineProof(msg []byte) (onlineProof, error) {
	if len(msg) != proofSize {
		return onlineProof{}, errors.New("malformed checkpoint proof")
	}
	tick := binary.LittleEndian.Uint32(msg[1:])
	if tick > matchlog.MaxTicks {
		return onlineProof{}, errors.New("checkpoint proof exceeds the match limit")
	}
	return onlineProof{int(tick), binary.LittleEndian.Uint64(msg[5:])}, nil
}

// outpostLink confines a lockstep session to one relay epoch. It handles
// Outpost's proof messages without making them part of lockstep's protocol.
type outpostLink struct {
	conn  *netlink.Conn
	epoch uint64
	peer  onlineProof
	final onlineProof
}

func (l *outpostLink) Send(msg []byte) error { return l.conn.SendEpoch(l.epoch, msg) }

func (l *outpostLink) Recv() (msg []byte, ok bool, err error) {
	for {
		msg, ok, err = l.conn.RecvEpoch(l.epoch)
		if err != nil || !ok || len(msg) == 0 || msg[0] != proofMessage && msg[0] != finalMessage {
			return msg, ok, err
		}
		if readErr := l.readProof(msg); readErr != nil {
			return nil, false, readErr
		}
	}
}

func (l *outpostLink) readProof(msg []byte) error {
	p, err := decodeOnlineProof(msg)
	if err != nil {
		return err
	}
	if msg[0] == finalMessage {
		if l.final.tick != 0 || p.tick == 0 || p.tick < l.peer.tick {
			return errors.New("invalid peer final checkpoint")
		}
		l.final = p
		return nil
	}
	if l.final.tick != 0 || p.tick != l.peer.tick+matchlog.Interval {
		return errors.New("out-of-order peer checkpoint proof")
	}
	l.peer = p
	return nil
}

// prove advances the published floor only once both players have compared the
// state. Confirmed implies remote frames after their Hash but does not imply
// that a remote player saw our Hash, hence the separate proof message.
func (o *onlineSync) prove(ended bool) error {
	if !o.canRecover() {
		return nil
	}
	l := o.link
	confirmed := o.base + int(
		o.lock.Confirmed(),
	) // #nosec G115 -- bounded by the recorder while live proof is active.
	if l.peer.tick > confirmed+2*onlineDelay+1 || l.final.tick > confirmed+2*onlineDelay+1 {
		return errors.New("peer checkpoint proof is too far ahead")
	}
	if next := o.proved + matchlog.Interval; next <= confirmed {
		hash, ok := o.recorder.Checkpoint(next)
		if !ok {
			return errors.New("missing local checkpoint")
		}
		if err := (onlineProof{next, hash}).send(l, proofMessage); err != nil {
			return err
		}
		o.proved = next
	}
	if l.peer.tick > o.shared && l.peer.tick <= o.proved {
		if err := o.checkProof(l.peer); err != nil {
			return err
		}
		o.shared = l.peer.tick
	}
	if l.final.tick != 0 && l.final.tick <= confirmed {
		if !ended || l.final.tick != o.recorder.Ticks() {
			return errors.New("peer ended at a different tick")
		}
		if err := o.checkProof(l.final); err != nil {
			return err
		}
	}
	if ended && confirmed == o.recorder.Ticks() && !o.finalSent {
		hash, ok := o.recorder.Checkpoint(confirmed)
		if !ok {
			return errors.New("missing local final checkpoint")
		}
		if err := (onlineProof{confirmed, hash}).send(l, finalMessage); err != nil {
			return err
		}
		o.finalSent = true
	}
	return nil
}

func (o *onlineSync) checkProof(p onlineProof) error {
	hash, ok := o.recorder.Checkpoint(p.tick)
	if !ok {
		return fmt.Errorf("missing local checkpoint at tick %d", p.tick)
	}
	if hash != p.hash {
		return &lockstep.DesyncError{
			Tick:   uint64(p.tick),
			Local:  hash,
			Remote: p.hash,
			Player: 1 - o.local,
		} // #nosec G115 -- proof tick is nonnegative.
	}
	return nil
}
