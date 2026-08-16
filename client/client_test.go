package client_test

import (
	"context"
	"errors"
	"testing"
	"time"

	protocol "github.com/go-theft-craft/minecraft-protocol"

	"github.com/go-theft-craft/headless-minecraft/auth"
	"github.com/go-theft-craft/headless-minecraft/client"
	"github.com/go-theft-craft/headless-minecraft/event"
	"github.com/go-theft-craft/headless-minecraft/safety"
	"github.com/go-theft-craft/headless-minecraft/version"
	"github.com/go-theft-craft/headless-minecraft/world"
)

// The real profiles arrive in Task 10. These stubs let construction be tested
// without them: New validates a profile, it does not use one.

type stubProtocol struct{ id string }

func (s stubProtocol) ID() string              { return s.id }
func (stubProtocol) Edition() protocol.Edition { return protocol.EditionJava }
func (stubProtocol) Version() protocol.Version { return protocol.Version{Name: "1.8.9", Protocol: 47} }

func (stubProtocol) NewSession(protocol.Role, protocol.Limits) (protocol.Session, error) {
	return nil, nil
}

type stubAdapter struct{ id string }

func (s stubAdapter) ProtocolID() string                     { return s.id }
func (stubAdapter) LoginTerminalState() protocol.State       { return "" }
func (stubAdapter) Handshake(string, uint16) protocol.Packet { return protocol.Packet{} }

func (stubAdapter) Handlers() map[string]version.Handler { return nil }

type stubReadiness struct{}

func (stubReadiness) Observe(version.Batch) (version.ReadyState, []protocol.Packet, error) {
	return version.ReadyState{}, nil, nil
}

func stubProfile(t *testing.T) version.WireProfile {
	t.Helper()

	limits, err := protocol.NewLimits()
	if err != nil {
		t.Fatalf("NewLimits: %v", err)
	}

	return version.WireProfile{
		ID:        "java/1.8.9",
		Protocol:  stubProtocol{id: "java/1.8.9"},
		Adapter:   stubAdapter{id: "java/1.8.9"},
		Limits:    limits,
		Readiness: stubReadiness{},
		Collector: new(event.Collector),
		Outbox:    new(version.Outbox),
	}
}

func testOptions(t *testing.T) []client.Option {
	t.Helper()

	provider, err := auth.Offline("tester")
	if err != nil {
		t.Fatalf("Offline: %v", err)
	}
	authz, err := safety.Authorize("localhost:25565", safety.ScopeObserve)
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}

	return []client.Option{
		client.WithAddress("localhost:25565"),
		client.WithAuth(provider),
		client.WithVersion(stubProfile(t)),
		client.WithAuthorization(authz),
	}
}

// without returns the options with the one at index i removed.
func without(options []client.Option, i int) []client.Option {
	remaining := make([]client.Option, 0, len(options)-1)
	remaining = append(remaining, options[:i]...)

	return append(remaining, options[i+1:]...)
}

func TestNewRejectsAMissingAddress(t *testing.T) {
	t.Parallel()

	if _, err := client.New(without(testOptions(t), 0)...); !errors.Is(err, client.ErrInvalidClient) {
		t.Fatalf("got %v, want ErrInvalidClient", err)
	}
}

func TestNewRejectsAMissingAuthProvider(t *testing.T) {
	t.Parallel()

	if _, err := client.New(without(testOptions(t), 1)...); !errors.Is(err, client.ErrInvalidClient) {
		t.Fatalf("got %v, want ErrInvalidClient", err)
	}
}

func TestNewRejectsAnAuthorizationForADifferentEndpoint(t *testing.T) {
	t.Parallel()

	authz, _ := safety.Authorize("elsewhere.example:25565", safety.ScopeObserve)
	options := append(without(testOptions(t), 3), client.WithAuthorization(authz))

	_, err := client.New(options...)
	if !errors.Is(err, client.ErrInvalidClient) {
		t.Fatalf("got %v, want ErrInvalidClient", err)
	}
	if !errors.Is(err, safety.ErrUnauthorized) {
		t.Errorf("got %v, want it to name the authorization failure", err)
	}
}

func TestNewRejectsAnAuthorizationWithoutTheObserveScope(t *testing.T) {
	t.Parallel()

	authz, _ := safety.Authorize("localhost:25565", safety.ScopeMove)
	options := append(without(testOptions(t), 3), client.WithAuthorization(authz))

	if _, err := client.New(options...); !errors.Is(err, client.ErrInvalidClient) {
		t.Fatalf("got %v, want ErrInvalidClient", err)
	}
}

func TestNewRejectsAnInvalidProfile(t *testing.T) {
	t.Parallel()

	options := append(without(testOptions(t), 2), client.WithVersion(version.WireProfile{ID: "incomplete"}))

	_, err := client.New(options...)
	if !errors.Is(err, client.ErrInvalidClient) {
		t.Fatalf("got %v, want ErrInvalidClient", err)
	}
	if !errors.Is(err, version.ErrInvalidProfile) {
		t.Errorf("got %v, want it to name the profile failure", err)
	}
}

func TestNewRejectsANilOption(t *testing.T) {
	t.Parallel()

	if _, err := client.New(append(testOptions(t), nil)...); !errors.Is(err, client.ErrInvalidClient) {
		t.Fatalf("got %v, want ErrInvalidClient", err)
	}
}

func TestNewAcceptsACompleteConfiguration(t *testing.T) {
	t.Parallel()

	bot, err := client.New(testOptions(t)...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = bot.Close() }()

	sub, err := bot.Subscribe(event.DomainSession, 8)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	_ = sub.Close()
}

func TestOptionsRejectTheirOwnBadValues(t *testing.T) {
	t.Parallel()

	cases := map[string]client.Option{
		"empty address":        client.WithAddress(""),
		"nil provider":         client.WithAuth(nil),
		"nil logger":           client.WithLogger(nil),
		"zero connect timeout": client.WithConnectTimeout(0),
		"zero bundle limit":    client.WithBundleLimit(0),
	}

	for name, option := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			options := append(testOptions(t), option)
			if _, err := client.New(options...); !errors.Is(err, client.ErrInvalidClient) {
				t.Fatalf("New accepted %s: %v", name, err)
			}
		})
	}
}

func TestNewAppliesADefaultConnectTimeout(t *testing.T) {
	t.Parallel()

	bot, err := client.New(testOptions(t)...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = bot.Close() }()

	if got := bot.ConnectTimeout(); got != 30*time.Second {
		t.Errorf("default connect timeout is %v, want 30s", got)
	}
}

// New must not reach the network or the auth provider. A provider that
// recorded a call would prove it did.
type countingProvider struct {
	inner auth.Provider
	calls int
}

func (p *countingProvider) Authenticate(ctx context.Context) (auth.Identity, error) {
	p.calls++

	return p.inner.Authenticate(ctx)
}

func TestNewDoesNotAuthenticate(t *testing.T) {
	t.Parallel()

	offline, _ := auth.Offline("tester")
	provider := &countingProvider{inner: offline}

	options := append(without(testOptions(t), 1), client.WithAuth(provider))
	if _, err := client.New(options...); err != nil {
		t.Fatalf("New: %v", err)
	}

	if provider.calls != 0 {
		t.Errorf("New authenticated %d times, want 0", provider.calls)
	}
}

func TestWorldIsZeroWithoutOne(t *testing.T) {
	t.Parallel()

	bot, err := client.New(testOptions(t)...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = bot.Close() }()

	// A consumer that only watches traffic installs no world, and asking for
	// one must not panic or lie.
	if got := bot.World().Revision; got != 0 {
		t.Errorf("a client with no world reports revision %d, want 0", got)
	}
}

func TestWithWorldRejectsNil(t *testing.T) {
	t.Parallel()

	options := append(testOptions(t), client.WithWorld(nil))
	if _, err := client.New(options...); !errors.Is(err, client.ErrInvalidClient) {
		t.Fatalf("got %v, want ErrInvalidClient", err)
	}
}

func TestWorldReportsTheInstalledWorld(t *testing.T) {
	t.Parallel()

	w := world.New()
	var c event.Collector
	if _, err := w.Apply(version.Batch{State: "play"}, &c); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	options := append(testOptions(t), client.WithWorld(w))
	bot, err := client.New(options...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = bot.Close() }()

	if got := bot.World().Revision; got != 1 {
		t.Errorf("client reports revision %d, want the world's 1", got)
	}
}
