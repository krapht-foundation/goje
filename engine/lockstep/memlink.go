package lockstep

// MemoryNet connects the players of a session in memory, for tests and
// headless runs. Every message a player sends reaches every other player,
// reliably and in order, like a relay. Faults can be injected: latency,
// bursts, and a cut player. A MemoryNet and its links are not safe for
// concurrent use; drive every session from one goroutine.
type MemoryNet struct {
	latency, burst int
	cut            []bool
	links          []*memoryLink
	free           [][]byte // message buffers to reuse, so steady traffic does not allocate
}

type memoryMsg struct {
	data []byte
	due  int // the receiver's Recv count from which the message is visible
}

type memoryLink struct {
	net    *MemoryNet
	player int
	recvs  int
	open   bool // a burst is being delivered
	inbox  []memoryMsg
	last   []byte // the message returned by the previous Recv
}

// NewMemoryNet returns a network of players links without faults.
func NewMemoryNet(players int) *MemoryNet {
	n := &MemoryNet{cut: make([]bool, players), links: make([]*memoryLink, players)}
	for p := range n.links {
		n.links[p] = &memoryLink{net: n, player: p}
	}
	return n
}

// Link returns the link of a player.
func (n *MemoryNet) Link(player int) Link { return n.links[player] }

// SetLatency makes each later message visible to a receiver only after that
// receiver has called Recv the given number of times since it was sent.
func (n *MemoryNet) SetLatency(polls int) { n.latency = polls }

// SetBurst makes receivers see messages only on every n-th Recv call that
// starts a delivery; such a call and the calls that follow it deliver every
// visible message until one finds none. Zero or one turns bursts off.
func (n *MemoryNet) SetBurst(calls int) { n.burst = calls }

// Cut drops every message the player sends from now on, as if its connection
// had silently stopped.
func (n *MemoryNet) Cut(player int) { n.cut[player] = true }

func (l *memoryLink) Send(msg []byte) error {
	n := l.net
	if n.cut[l.player] {
		return nil
	}
	for _, to := range n.links {
		if to == l {
			continue
		}
		var buf []byte
		if k := len(n.free); k > 0 {
			buf, n.free = n.free[k-1], n.free[:k-1]
		}
		to.inbox = append(to.inbox, memoryMsg{append(buf[:0], msg...), to.recvs + n.latency})
	}
	return nil
}

func (l *memoryLink) Recv() (msg []byte, ok bool, err error) {
	if l.last != nil {
		l.net.free = append(l.net.free, l.last)
		l.last = nil
	}
	l.recvs++
	if !l.open && l.net.burst > 1 && l.recvs%l.net.burst != 0 {
		return nil, false, nil
	}
	if len(l.inbox) == 0 || l.inbox[0].due > l.recvs {
		l.open = false
		return nil, false, nil
	}
	l.open = true
	l.last = l.inbox[0].data
	copy(l.inbox, l.inbox[1:])
	l.inbox = l.inbox[:len(l.inbox)-1]
	return l.last, true, nil
}
