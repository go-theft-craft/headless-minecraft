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

	protocol "github.com/go-theft-craft/minecraft-protocol"
	gen "github.com/go-theft-craft/minecraft-protocol/generated/java/v1_8"

	"github.com/go-theft-craft/headless-minecraft/event"
	"github.com/go-theft-craft/headless-minecraft/version"
)

// BundleDelimiter is empty: protocol 47 has no packet bundling, so every
// batch holds one packet.
const BundleDelimiter = ""

// ProtocolID names the profile this adapter translates for.
const ProtocolID = "java/1.8.9"

type adapter struct{ collector *event.Collector }

// New returns an adapter that appends to the given collector.
//
// The collector is owned by the read loop and reset per batch, so handlers
// never publish and a batch's events reach subscribers together.
func New(collector *event.Collector) version.Adapter {
	return adapter{collector: collector}
}

func (adapter) ProtocolID() string { return ProtocolID }

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
	// Elapsed stays zero: measuring it needs the send time of the answer,
	// which the loop owns, not the adapter.
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
					"%w: position flags are 0x%02x", version.ErrRelativeSpawn, value.Flags)
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
