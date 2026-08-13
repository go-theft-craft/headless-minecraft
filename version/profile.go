// Package version binds a wire protocol to client-side conformance rules.
package version

import (
	"errors"
	"fmt"

	protocol "github.com/go-theft-craft/minecraft-protocol"
)

// ErrInvalidProfile reports an incomplete or incompatible version profile.
var ErrInvalidProfile = errors.New("invalid version profile")

// Adapter identifies the protocol that it translates for the client.
type Adapter interface {
	ProtocolID() string
}

// WireProfile is the transport portion of a complete gameplay profile.
// Later components extend this with physics, collision, inventory, and ordering rules.
type WireProfile struct {
	ID       string
	Protocol protocol.Protocol
	Adapter  Adapter
	Limits   protocol.Limits
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
	return nil
}
