// Package version binds a wire protocol to client-side conformance rules.
package version

import (
	"context"
	"errors"
	"fmt"

	protocol "github.com/go-theft-craft/minecraft-protocol"

	"github.com/go-theft-craft/headless-minecraft/event"
)

// ErrInvalidProfile reports an incomplete or incompatible version profile.
var ErrInvalidProfile = errors.New("invalid version profile")

// Handler processes one dispatched packet. It matches the signature the
// shared router's middleware.Handler declares, restated here so this package
// does not import the router to name a one-method function type.
type Handler interface {
	Handle(ctx context.Context, p protocol.Packet) error
}

// Adapter translates one protocol's packets into client events.
type Adapter interface {
	ProtocolID() string
	// LoginTerminalState is the state at which the client takes over from the
	// shared login negotiator, or empty when the protocol has no state before
	// play worth owning.
	//
	// Protocol 775 stops at configuration, because that is where a server
	// sends the registries, tags, feature flags, and resource-pack offers a
	// client needs, and a negotiator that ran through it would consume every
	// one of them. Protocol 47 has no configuration state, so it stops
	// nowhere and its login ends at success.
	LoginTerminalState() protocol.State
	// Handshake builds the packet that opens a connection and asks for
	// login. It is version-owned because nothing about it is shared: the
	// packet type, its protocol number, and its field types all differ, and
	// the client that sends it names no version.
	Handshake(host string, port uint16) protocol.Packet
	// EncodeAction turns one outbound intent into the packet this protocol
	// carries it in. It reports ErrUnsupportedAction for an intent the protocol
	// has no packet for, rather than sending an approximation: a server reads a
	// movement packet as a claim, and the wrong claim is a disagreement.
	EncodeAction(action Action) (protocol.Packet, error)
	// Handlers are registered with the router by packet name. Each appends
	// to the batch-scoped collector it was built with; none publishes
	// directly, because a batch's events are published together or not at
	// all.
	Handlers() map[string]Handler
}

// WireProfile is the transport portion of a complete gameplay profile.
// Later components extend this with physics, collision, inventory, and
// ordering rules.
type WireProfile struct {
	ID        string
	Protocol  protocol.Protocol
	Adapter   Adapter
	Limits    protocol.Limits
	Readiness ReadinessRule
	// Collector is the batch-scoped collector the adapter's handlers append
	// to. It belongs to the profile because the adapter is built around one:
	// a loop that reset a different collector would publish nothing a
	// handler produced.
	Collector *event.Collector
	// Outbox is the batch-scoped outbox the adapter's handlers queue answers
	// in. It belongs to the profile for the same reason the collector does.
	Outbox *Outbox
}

// Validate checks the wire profile without performing network work.
func (p WireProfile) Validate() error {
	if p.ID == "" {
		return fmt.Errorf("%w: missing ID", ErrInvalidProfile)
	}
	if p.Protocol == nil {
		return fmt.Errorf("%w: missing protocol", ErrInvalidProfile)
	}
	if p.Adapter == nil {
		return fmt.Errorf("%w: missing adapter", ErrInvalidProfile)
	}
	if p.Protocol.ID() != p.Adapter.ProtocolID() {
		return fmt.Errorf(
			"%w: protocol %q does not match adapter %q",
			ErrInvalidProfile,
			p.Protocol.ID(),
			p.Adapter.ProtocolID(),
		)
	}
	if !p.Limits.Valid() {
		return fmt.Errorf("%w: construct limits with protocol.NewLimits", ErrInvalidProfile)
	}
	if p.Readiness == nil {
		return fmt.Errorf("%w: missing readiness rule", ErrInvalidProfile)
	}
	if p.Collector == nil {
		return fmt.Errorf("%w: missing event collector", ErrInvalidProfile)
	}
	if p.Outbox == nil {
		return fmt.Errorf("%w: missing outbox", ErrInvalidProfile)
	}

	return nil
}
