// Package auth supplies a session identity to the client.
//
// M6.3 implements offline authentication only. The Microsoft device-code
// provider is M6.4 and plugs into the same seam: it is four HTTP exchanges
// plus refresh and storage, shares nothing with the packet loop, and blocks
// nothing in M7, because offline mode covers every test server.
//
// Secrets and access tokens never appear in errors, logs, or events.
package auth

import (
	"context"
	"fmt"

	"github.com/go-theft-craft/minecraft-protocol/login"
)

// Identity is the account a connection presents.
type Identity struct {
	Username string
	UUID     string
	// Authenticator is handed to the shared login negotiator, which calls
	// it to prove account ownership. The offline one does nothing, because
	// there is nobody to tell.
	Authenticator login.Authenticator
}

// Provider supplies an identity before the client dials.
type Provider interface {
	Authenticate(ctx context.Context) (Identity, error)
}

type offline struct {
	inner login.Offline
}

// Offline returns a provider for a server that does not verify accounts. It
// validates the name at construction, so an invalid one fails before any
// network work.
func Offline(name string) (Provider, error) {
	inner, err := login.NewOffline(name)
	if err != nil {
		return nil, fmt.Errorf("offline provider: %w", err)
	}

	return offline{inner: inner}, nil
}

// Authenticate implements Provider. It makes no request.
func (o offline) Authenticate(context.Context) (Identity, error) {
	profile := o.inner.Profile()

	return Identity{
		Username: profile.Name.String(),
		// Derived by the shared login package, byte-identically to the
		// server's own derivation. A UUID invented here would point at a
		// different player file.
		UUID:          login.OfflineUUID(profile.Name).String(),
		Authenticator: o.inner,
	}, nil
}
