// Package v26_1 translates protocol 775 packets into client events.
//
// It is internal because it is a translation table, not a contract. The
// contract is version.Adapter, and a caller selects this one through
// version/java.
//
// It owns the parts 775 cannot borrow from 47: the bundle delimiter, the
// readiness rule, and the teleport confirmation every server-initiated position
// is owed. Its handlers are in handlers.go.
package v26_1

import (
	"fmt"

	protocol "github.com/go-theft-craft/minecraft-protocol"
	gen "github.com/go-theft-craft/minecraft-protocol/generated/java/v26_1"

	"github.com/go-theft-craft/headless-minecraft/event"
	"github.com/go-theft-craft/headless-minecraft/version"
	"github.com/go-theft-craft/headless-minecraft/world"
)

// BundleDelimiter is the packet that opens and closes a bundle. It toggles:
// the first occurrence opens, the next closes.
const BundleDelimiter = "bundle_delimiter"

// ProtocolID names the profile this adapter translates for.
const ProtocolID = "java/26.1"

type adapter struct {
	collector *event.Collector
	outbox    *version.Outbox
}

// New returns an adapter that appends to the given collector and queues its
// answers in the given outbox.
func New(collector *event.Collector, outbox *version.Outbox) version.Adapter {
	return adapter{collector: collector, outbox: outbox}
}

func (adapter) ProtocolID() string { return ProtocolID }

// Reducers gives an installed world this protocol's reducers.
//
// The client asserts the adapter for this method rather than version
// declaring it, because world imports version and naming it there would make
// the two packages import each other. Without the method the assertion fails
// silently: the world still counts batches and observes nothing, which is the
// fault the observed-world end-to-end lane was written to catch.
func (adapter) Reducers(w *world.World) []world.Reducer { return Reducers(w) }

// LoginTerminalState is configuration: the client takes the connection over
// from the login negotiator there, so the registries, tags, feature flags,
// and resource-pack offers a server sends in configuration reach handlers
// instead of being consumed inside the login sequence.
func (adapter) LoginTerminalState() protocol.State { return gen.StateConfiguration }

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

func (a adapter) Handlers() map[string]version.Handler {
	handlers := make(map[string]version.Handler, len(a.handlers()))
	for name, handler := range a.handlers() {
		handlers[name] = handler
	}

	return handlers
}

// readiness implements version.ReadinessRule for protocol 775.
//
// The server places the player with a position packet, and this rule reports
// that the connection has reached the point where action packets are accepted.
// Protocol 47 reaches the same point differently — it has no teleport
// confirmation and echoes a position-look instead — which is why the rule is
// version-owned.
//
// It does not send the teleport confirmation the placement calls for. That is
// the position handler's, because the server sends the same packet again every
// time it corrects the player and owes a confirmation each time, while this
// rule answers a question that is settled once. Splitting them the other way is
// what left corrections unconfirmed.
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
			// Only at spawn. A later position is a correction, which the
			// world resolves against the position it already has; this is the
			// one that has nothing to resolve against.
			if relative := value.Flags; relative.X || relative.Y || relative.Z {
				return version.ReadyState{}, nil, fmt.Errorf(
					"%w: position flags are %+v", version.ErrRelativeSpawn, relative,
				)
			}

			r.ready = true
			r.state.Ready = true

			// Tell the server the client has finished loading in.
			//
			// Protocol 775 holds a joining player in a loading state until
			// this arrives, and while it is held the server keeps the player
			// where it put them and does not take their word for where they
			// are. It does not wait forever: vanilla gives the client sixty
			// ticks and then declares it loaded anyway, which is what makes a
			// client that never sends this look like it works. Three seconds
			// of walking is discarded, the server adopts the position in the
			// next packet it reads, and that position is three seconds of
			// travel away from where it left the player — so the first thing
			// a silent client does is appear to teleport, and the server says
			// so. The orbit example met this as one "moved too quickly" about
			// three seconds into every single run, at whatever distance it
			// happened to have covered by then.
			//
			// It belongs in the readiness rule rather than in a handler
			// because it is the one thing here that is genuinely settled once:
			// the player loads at spawn, and no later position packet is a
			// second arrival. The teleport confirmation sits on the other side
			// of that line, which is why the two live apart.
			loaded := &gen.PlayServerboundPlayerLoaded{}

			return r.state, []protocol.Packet{serverbound(
				gen.StatePlay, "player_loaded", loaded.PacketID(), loaded,
			)}, nil
		}
	}

	return r.state, nil, nil
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
