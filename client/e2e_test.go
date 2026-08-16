package client_test

import (
	"testing"
	"time"

	gen "github.com/go-theft-craft/minecraft-protocol/generated/java/v1_8"

	"github.com/go-theft-craft/headless-minecraft/client/internal/fixture"
	"github.com/go-theft-craft/headless-minecraft/event"
)

// The end-to-end lane drives a whole connection over a loopback socket: real
// framing, real generated codecs, a real login exchange, and the client's own
// loop. Everything else in this package tests one seam at a time.
//
// It covers protocol 47 only. Serving protocol 775 needs a server-side login,
// and the shared login.Acceptor is written against the v1_8 generated types,
// so there is no way to stand up a 775 server here. The 775 client half is
// covered by its adapter's tests and by mcproto's recorded-server lane in
// minecraft-protocol.

func TestEndToEndReachesReadyAndClosesCleanly(t *testing.T) {
	if testing.Short() {
		t.Skip("end-to-end lane needs a loopback socket")
	}
	t.Parallel()

	addr, stop := fixture.Start(t, fixture.Script{ThroughReady: true})
	defer stop()

	bot := connectTo(t, addr, 5*time.Second)
	session, err := bot.Subscribe(event.DomainSession, 128)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	raw, err := bot.Subscribe(event.DomainRaw, 128)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	if err := bot.Connect(t.Context()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if err := bot.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	events := drain(session)
	if got := count(events, event.SessionReady); got != 1 {
		t.Errorf("got %d Ready events, want 1", got)
	}
	if got := count(events, event.SessionClosed); got != 1 {
		t.Errorf("got %d Closed events, want 1", got)
	}

	// The last thing a subscriber sees is Closed. Anything after it would be
	// an event published on a session that had already ended.
	if len(events) == 0 || events[len(events)-1].Name() != event.SessionClosed {
		t.Errorf("the last event was %v, want closed", events[len(events)-1].Name())
	}

	var sent []string
	var placed int
	for _, e := range drain(raw) {
		switch value := e.(type) {
		case event.PacketSent:
			sent = append(sent, value.Packet)
		case event.PacketReceived:
			if value.Packet == "position" {
				placed++
			}
		}
	}

	if placed != 1 {
		t.Errorf("saw %d placing positions, want 1", placed)
	}
	// Protocol 47 acknowledges its placement by echoing a position-look, and
	// exactly once: a second echo would be a second player movement the
	// application never asked for.
	if len(sent) != 1 || sent[0] != "position_look" {
		t.Errorf("client sent %v, want one position_look", sent)
	}
}

func TestEndToEndPublishesAWholeBatchTogether(t *testing.T) {
	if testing.Short() {
		t.Skip("end-to-end lane needs a loopback socket")
	}
	t.Parallel()

	addr, stop := fixture.Start(t, fixture.Script{ThroughReady: true})
	defer stop()

	bot := connectTo(t, addr, 5*time.Second)
	raw, _ := bot.Subscribe(event.DomainRaw, 128)

	if err := bot.Connect(t.Context()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	_ = bot.Close()

	// Protocol 47 does not bundle, so every batch holds one packet and none
	// of them is marked bundled. The bundled case is covered by the loop's
	// own tests, which can script a delimiter this fixture cannot serve.
	for _, e := range drain(raw) {
		if packet, ok := e.(event.PacketReceived); ok && packet.Bundled {
			t.Errorf("packet %q is marked bundled on a protocol with no bundles", packet.Packet)
		}
	}
}

func TestEndToEndSurvivesTheServerHangingUp(t *testing.T) {
	if testing.Short() {
		t.Skip("end-to-end lane needs a loopback socket")
	}
	t.Parallel()

	addr, stop := fixture.Start(t, fixture.Script{
		ThroughReady: true,
		ThenKick:     "closing for maintenance",
	})
	defer stop()

	bot := connectTo(t, addr, 5*time.Second)
	session, _ := bot.Subscribe(event.DomainSession, 128)

	if err := bot.Connect(t.Context()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if err := bot.Wait(); err != nil {
		t.Errorf("Wait after a clean kick returned %v, want nil", err)
	}
	if err := bot.Close(); err != nil {
		t.Errorf("Close after a kick returned %v, want nil", err)
	}

	events := drain(session)
	if got := count(events, event.SessionDisconnected); got != 1 {
		t.Errorf("got %d Disconnected events, want 1", got)
	}
	if got := count(events, event.SessionClosed); got != 1 {
		t.Errorf("got %d Closed events, want 1", got)
	}

	// The state a disconnect names is the state it arrived in.
	for _, e := range events {
		if d, ok := e.(event.Disconnected); ok && d.State != string(gen.StatePlay) {
			t.Errorf("disconnect reports state %q, want play", d.State)
		}
	}
}
