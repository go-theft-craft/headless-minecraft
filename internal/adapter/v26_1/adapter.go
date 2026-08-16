// Package v26_1 translates protocol 775 packets into client events.
//
// It is internal because it is a translation table, not a contract. The
// contract is version.Adapter, and a caller selects this one through
// version/java.
//
// Its handler table is empty for now: the read loop is proved against
// protocol 47 first, which needs no configuration state and no bundling.
// What this package already owns is the part 775 cannot borrow from 47 —
// the bundle delimiter and the teleport-confirming readiness rule.
package v26_1

import (
	"fmt"

	protocol "github.com/go-theft-craft/minecraft-protocol"
	gen "github.com/go-theft-craft/minecraft-protocol/generated/java/v26_1"

	"github.com/go-theft-craft/headless-minecraft/event"
	"github.com/go-theft-craft/headless-minecraft/version"
)

// BundleDelimiter is the packet that opens and closes a bundle. It toggles:
// the first occurrence opens, the next closes.
const BundleDelimiter = "bundle_delimiter"

// ProtocolID names the profile this adapter translates for.
const ProtocolID = "java/26.1"

type adapter struct{ collector *event.Collector }

// New returns an adapter that appends to the given collector.
func New(collector *event.Collector) version.Adapter {
	return adapter{collector: collector}
}

func (adapter) ProtocolID() string { return ProtocolID }

// Handshake asks for login: next state 2.
func (adapter) Handshake(host string, port uint16) protocol.Packet {
	value := &gen.HandshakingServerboundSetProtocol{
		ProtocolVersion: 775,
		ServerHost:      host,
		ServerPort:      port,
		NextState:       2,
	}

	return protocol.Packet{
		State:     gen.StateHandshaking,
		Direction: protocol.DirectionServerbound,
		ID:        value.PacketID(),
		Name:      "set_protocol",
		Value:     value,
	}
}

func (adapter) Handlers() map[string]version.Handler {
	return map[string]version.Handler{}
}

// readiness implements version.ReadinessRule for protocol 775.
//
// The server places the player with a position carrying a teleport ID, and
// expects that ID back in a teleport confirmation. Protocol 47 has no such
// packet and echoes a position-look instead, which is why the rule is
// version-owned.
type readiness struct {
	seenLogin bool
	ready     bool
	state     version.ReadyState
}

// Readiness returns a fresh rule. One rule serves one connection: it carries
// per-connection progress and must not be shared.
func Readiness() version.ReadinessRule { return &readiness{} }

func (r *readiness) Observe(batch version.Batch) (version.ReadyState, []protocol.Packet, error) {
	if r.ready {
		return r.state, nil, nil
	}

	var reply []protocol.Packet
	for _, p := range batch.Packets {
		switch value := p.Value.(type) {
		case *gen.PlayClientboundLogin:
			r.seenLogin = true
			r.state.EntityID = value.EntityID
			r.state.Dimension = value.WorldState.Name
			r.state.GameMode = gameModeNumber(value.WorldState.Gamemode)

		case *gen.PlayClientboundPosition:
			if !r.seenLogin {
				continue
			}
			if relative := value.Flags; relative.X || relative.Y || relative.Z {
				return version.ReadyState{}, nil, fmt.Errorf(
					"%w: position flags are %+v", version.ErrRelativeSpawn, relative,
				)
			}

			reply = append(reply, protocol.Packet{
				State:     gen.StatePlay,
				Direction: protocol.DirectionServerbound,
				ID:        gen.PlayServerboundTeleportConfirm{}.PacketID(),
				Name:      "teleport_confirm",
				Value:     &gen.PlayServerboundTeleportConfirm{TeleportID: value.TeleportID},
			})
			r.ready = true
			r.state.Ready = true
		}
	}

	return r.state, reply, nil
}

// gameModeNumber maps protocol 775's game-mode name to the number protocol 47
// sends, so the Ready event carries the same shape from both.
//
// An unrecognized name reports survival rather than failing: a modded server
// may name a mode this client has never heard of, and refusing to spawn over
// it would be worse than reporting the default.
func gameModeNumber(name string) uint8 {
	switch name {
	case "creative":
		return 1
	case "adventure":
		return 2
	case "spectator":
		return 3
	default:
		return 0
	}
}
