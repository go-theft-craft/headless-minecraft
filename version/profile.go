// Package version binds a wire protocol to client-side conformance rules.
package version

import (
	"context"
	"errors"
	"fmt"

	protocol "github.com/go-theft-craft/minecraft-protocol"
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

	return nil
}
