package version

import (
	"slices"

	protocol "github.com/go-theft-craft/minecraft-protocol"
)

// Outbox holds the packets a batch's handlers asked to send.
//
// Handlers do not write to the stream, for the same reason they do not
// publish events: the read goroutine owns both ends of one batch, and a
// handler that wrote directly could interleave its packet with the loop's own
// replies. The loop drains the outbox once the batch's handlers have run and
// writes what it holds, in the order it was added.
//
// It exists because some answers are not readiness. A keepalive must be
// answered for the whole session, and a configuration server stops until its
// known-packs question is answered; the readiness rule stops observing the
// moment the player is placed, so it cannot own either.
//
// An Outbox is not safe for concurrent use. It does not need to be: it is
// owned by the read goroutine, which is the only goroutine that runs
// handlers.
type Outbox struct {
	packets []protocol.Packet
}

// Add queues one packet.
func (o *Outbox) Add(p protocol.Packet) { o.packets = append(o.packets, p) }

// Len reports how many packets are queued.
func (o *Outbox) Len() int { return len(o.packets) }

// Drain returns the queued packets in the order they were added and empties
// the outbox, retaining its capacity for the next batch.
func (o *Outbox) Drain() []protocol.Packet {
	packets := slices.Clone(o.packets)
	o.packets = o.packets[:0]

	return packets
}
