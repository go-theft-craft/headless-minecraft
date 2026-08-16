package version

import (
	"errors"

	protocol "github.com/go-theft-craft/minecraft-protocol"
)

// ErrRelativeSpawn reports a spawn position the client cannot acknowledge.
//
// Both protocols mark a position packet's fields as absolute or relative. At
// spawn the server sends absolute coordinates, and answering a relative one
// correctly requires a prior position the connection layer does not track --
// that is M7's observed state. A relative spawn is therefore a named error
// rather than a wrong acknowledgement.
var ErrRelativeSpawn = errors.New("server placed the player relative to an unknown position")

// ReadyState is what a readiness rule learned on the way to ready.
type ReadyState struct {
	// Ready reports that the server will accept action packets.
	Ready bool
	// EntityID, Dimension, and GameMode come from the play login packet and
	// are zero until it arrives.
	EntityID  int32
	Dimension string
	GameMode  uint8
}

// ReadinessRule decides when a connection has reached the point where the
// server accepts action packets, and supplies whatever the client must send
// to get there.
//
// It is version-owned because the sequence differs: protocol 775 answers the
// placing position with a teleport confirmation carrying its ID, and protocol
// 47 has no such packet and echoes a position-look instead.
//
// Observe is called once per batch, on the read goroutine, until it reports
// Ready. The packets it returns are written in the order given.
type ReadinessRule interface {
	Observe(Batch) (ReadyState, []protocol.Packet, error)
}
