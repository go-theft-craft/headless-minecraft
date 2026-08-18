// Package v1_8 translates protocol 47 packets into client events.
//
// It is internal because it is a translation table, not a contract. The
// contract is version.Adapter, and a caller selects this one through
// version/java.
package v1_8

import (
	"context"
	"fmt"
	"slices"
	"sync/atomic"

	protocol "github.com/go-theft-craft/minecraft-protocol"
	gen "github.com/go-theft-craft/minecraft-protocol/generated/java/v1_8"

	"github.com/go-theft-craft/headless-minecraft/event"
	"github.com/go-theft-craft/headless-minecraft/version"
	"github.com/go-theft-craft/headless-minecraft/world"
)

// BundleDelimiter is empty: protocol 47 has no packet bundling, so every
// batch holds one packet.
const BundleDelimiter = ""

// ProtocolID names the profile this adapter translates for.
const ProtocolID = "java/1.8.9"

type adapter struct {
	collector *event.Collector
	outbox    *version.Outbox
	// transactions numbers window clicks. This protocol confirms a click by
	// echoing its number back, so whoever allocates one has to be whoever
	// waits for the echo, and that is this adapter rather than the caller.
	transactions *atomic.Int32
}

// New returns an adapter that appends to the given collector and queues its
// answers in the given outbox.
//
// Both are owned by the read loop and scoped to one batch, so handlers never
// publish and never write: a batch's events reach subscribers together, and
// its answers reach the server together.
func New(collector *event.Collector, outbox *version.Outbox) version.Adapter {
	return adapter{collector: collector, outbox: outbox, transactions: new(atomic.Int32)}
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

// LoginTerminalState is empty: protocol 47 has no configuration state, so
// there is nothing before play for the client to take over.
func (adapter) LoginTerminalState() protocol.State { return "" }

// Handshake asks for login: next state 2. Protocol 47 sends its own protocol
// number, which the server compares against its own.
func (adapter) Handshake(host string, port uint16) protocol.Packet {
	value := &gen.HandshakingServerboundSetProtocol{
		ProtocolVersion: 47,
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
	return map[string]version.Handler{
		"keep_alive":      handlerFunc(a.keepAlive),
		"custom_payload":  handlerFunc(a.customPayload),
		"kick_disconnect": handlerFunc(a.disconnect),
	}
}

// handlerFunc adapts a function to version.Handler.
type handlerFunc func(context.Context, protocol.Packet) error

func (f handlerFunc) Handle(ctx context.Context, p protocol.Packet) error { return f(ctx, p) }

// Each handler ignores a value it does not recognize rather than failing.
// A packet the session could not decode arrives as an UnknownPacket under the
// same name, and dropping a connection over one is worse than observing less.

func (a adapter) keepAlive(_ context.Context, p protocol.Packet) error {
	value, ok := p.Value.(*gen.PlayClientboundKeepAlive)
	if !ok {
		return nil
	}
	// Answering is what keeps the session alive: a server drops a client that
	// stays silent. The event is named for the answer, so the answer is
	// queued here rather than left to a consumer that may not be listening.
	answer := &gen.PlayServerboundKeepAlive{KeepAliveID: value.KeepAliveID}
	a.outbox.Add(protocol.Packet{
		State:     gen.StatePlay,
		Direction: protocol.DirectionServerbound,
		ID:        answer.PacketID(),
		Name:      "keep_alive",
		Value:     answer,
	})

	// Elapsed stays zero: measuring it needs the round trip, which the loop
	// owns, not the adapter.
	event.Emit(a.collector, event.KeepAlivePonged{ID: int64(value.KeepAliveID)})

	return nil
}

func (a adapter) customPayload(_ context.Context, p protocol.Packet) error {
	value, ok := p.Value.(*gen.PlayClientboundCustomPayload)
	if !ok {
		return nil
	}
	event.Emit(a.collector, event.CustomPayloadReceived{
		Channel: value.Channel,
		Payload: slices.Clone(value.Data),
	})

	return nil
}

func (a adapter) disconnect(_ context.Context, p protocol.Packet) error {
	value, ok := p.Value.(*gen.PlayClientboundKickDisconnect)
	if !ok {
		return nil
	}
	event.Emit(a.collector, event.Disconnected{
		Source: event.DisconnectByServer,
		Reason: value.Reason,
		State:  string(gen.StatePlay),
	})

	return nil
}

// readiness implements version.ReadinessRule for protocol 47.
//
// Protocol 47 has no teleport confirmation. The server places the player with
// a clientbound position and the client acknowledges by echoing it as a
// serverbound position-look.
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
			r.state.GameMode = value.GameMode
			r.state.Dimension = dimensionName(value.Dimension)

		case *gen.PlayClientboundPosition:
			if !r.seenLogin {
				continue
			}
			if value.Flags != 0 {
				return version.ReadyState{}, nil, fmt.Errorf(
					"%w: position flags are 0x%02x", version.ErrRelativeSpawn, value.Flags,
				)
			}

			reply = append(reply, protocol.Packet{
				State:     gen.StatePlay,
				Direction: protocol.DirectionServerbound,
				ID:        gen.PlayServerboundPositionLook{}.PacketID(),
				Name:      "position_look",
				Value: &gen.PlayServerboundPositionLook{
					X: value.X, Y: value.Y, Z: value.Z,
					Yaw: value.Yaw, Pitch: value.Pitch, OnGround: true,
				},
			})
			r.ready = true
			r.state.Ready = true
		}
	}

	return r.state, reply, nil
}

// dimensionName maps protocol 47's numeric dimension to a stable name, so
// the Ready event carries the same shape both protocols produce.
func dimensionName(id int8) string {
	switch id {
	case -1:
		return "minecraft:the_nether"
	case 1:
		return "minecraft:the_end"
	default:
		return "minecraft:overworld"
	}
}

// nextTransaction allocates the number that identifies one window click.
//
// It never returns zero and never returns a negative: the field is a signed
// short on the wire, and a server that has seen 32767 clicks in one session is
// better served by wrapping back to one than by a number it reads as older
// than everything before it.
func (a adapter) nextTransaction() int16 {
	if a.transactions == nil {
		return 1
	}

	return int16(a.transactions.Add(1)%maxTransaction47 + 1)
}

// maxTransaction47 is how many distinct click numbers this protocol's signed
// short holds above zero.
const maxTransaction47 = 32767
