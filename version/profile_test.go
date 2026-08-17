package version_test

import (
	"context"
	"testing"

	protocol "github.com/go-theft-craft/minecraft-protocol"

	"github.com/go-theft-craft/headless-minecraft/event"
	"github.com/go-theft-craft/headless-minecraft/version"
)

// fakeProtocol is a protocol.Protocol that starts no session. No test in this
// file reaches the wire, so NewSession is never called.
type fakeProtocol struct{ id string }

func (f fakeProtocol) ID() string              { return f.id }
func (fakeProtocol) Edition() protocol.Edition { return protocol.EditionJava }
func (fakeProtocol) Version() protocol.Version { return protocol.Version{Name: "1.8.9", Protocol: 47} }

func (fakeProtocol) NewSession(protocol.Role, protocol.Limits) (protocol.Session, error) {
	return nil, nil
}

type stubAdapter struct{ id string }

func (s stubAdapter) ProtocolID() string                     { return s.id }
func (stubAdapter) LoginTerminalState() protocol.State       { return "" }
func (stubAdapter) Handshake(string, uint16) protocol.Packet { return protocol.Packet{} }

func (stubAdapter) EncodeAction(version.Action) (protocol.Packet, error) {
	return protocol.Packet{}, nil
}

func (stubAdapter) Handlers() map[string]version.Handler {
	return map[string]version.Handler{}
}

type stubReadiness struct{}

func (stubReadiness) Observe(version.Batch) (version.ReadyState, []protocol.Packet, error) {
	return version.ReadyState{}, nil, nil
}

func completeProfile(t *testing.T) version.WireProfile {
	t.Helper()

	limits, err := protocol.NewLimits()
	if err != nil {
		t.Fatalf("NewLimits: %v", err)
	}

	return version.WireProfile{
		ID:        "java/1.8.9",
		Protocol:  fakeProtocol{id: "java/1.8.9"},
		Adapter:   stubAdapter{id: "java/1.8.9"},
		Limits:    limits,
		Readiness: stubReadiness{},
		Collector: new(event.Collector),
		Outbox:    new(version.Outbox),
	}
}

func TestValidateRejectsAProfileWithoutAReadinessRule(t *testing.T) {
	t.Parallel()

	p := completeProfile(t)
	p.Readiness = nil

	if err := p.Validate(); err == nil {
		t.Fatal("Validate accepted a profile with no readiness rule")
	}
}

func TestValidateAcceptsACompleteProfile(t *testing.T) {
	t.Parallel()

	if err := completeProfile(t).Validate(); err != nil {
		t.Fatalf("Validate rejected a complete profile: %v", err)
	}
}

func TestValidateRejectsAnIncompleteProfile(t *testing.T) {
	t.Parallel()

	cases := map[string]func(*version.WireProfile){
		"no ID":        func(p *version.WireProfile) { p.ID = "" },
		"no protocol":  func(p *version.WireProfile) { p.Protocol = nil },
		"no adapter":   func(p *version.WireProfile) { p.Adapter = nil },
		"no limits":    func(p *version.WireProfile) { p.Limits = protocol.Limits{} },
		"no collector": func(p *version.WireProfile) { p.Collector = nil },
		"no outbox":    func(p *version.WireProfile) { p.Outbox = nil },
		"mismatched adapter": func(p *version.WireProfile) {
			p.Adapter = stubAdapter{id: "java/26.1.2"}
		},
	}

	for name, breakIt := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			p := completeProfile(t)
			breakIt(&p)

			if err := p.Validate(); err == nil {
				t.Fatalf("Validate accepted a profile with %s", name)
			}
		})
	}
}

// Handler is deliberately shaped like the shared router's middleware.Handler.
// A change to either signature has to be a deliberate one, so assert it here.
var _ version.Handler = handlerFunc(nil)

type handlerFunc func(context.Context, protocol.Packet) error

func (f handlerFunc) Handle(ctx context.Context, p protocol.Packet) error { return f(ctx, p) }
