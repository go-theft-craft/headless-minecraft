package client_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/go-theft-craft/headless-minecraft/auth"
	"github.com/go-theft-craft/headless-minecraft/client"
	"github.com/go-theft-craft/headless-minecraft/client/internal/fixture"
	"github.com/go-theft-craft/headless-minecraft/event"
	"github.com/go-theft-craft/headless-minecraft/safety"
	"github.com/go-theft-craft/headless-minecraft/version/java"
)

// connectTo builds a client pointed at addr with a short connect timeout.
func connectTo(t *testing.T, addr string, timeout time.Duration) *client.Client {
	t.Helper()

	provider, err := auth.Offline("tester")
	if err != nil {
		t.Fatalf("Offline: %v", err)
	}
	authz, err := safety.Authorize(addr, safety.ScopeObserve)
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}

	bot, err := client.New(
		client.WithAddress(addr),
		client.WithAuth(provider),
		client.WithVersion(java.Java1_8()),
		client.WithAuthorization(authz),
		client.WithConnectTimeout(timeout),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	return bot
}

// drain collects every event a subscription delivers until it closes.
func drain(sub *client.Subscription) []event.Event {
	var events []event.Event
	for e := range sub.C() {
		events = append(events, e)
	}

	return events
}

// count returns how many events carry the given name.
func count(events []event.Event, name event.Name) int {
	n := 0
	for _, e := range events {
		if e.Name() == name {
			n++
		}
	}

	return n
}

func TestConnectReachesReady(t *testing.T) {
	t.Parallel()

	addr, stop := fixture.Start(t, fixture.Script{ThroughReady: true})
	defer stop()

	bot := connectTo(t, addr, 5*time.Second)
	sub, err := bot.Subscribe(event.DomainSession, 64)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	if err := bot.Connect(t.Context()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if err := bot.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	events := drain(sub)
	if got := count(events, event.NameSessionReady); got != 1 {
		t.Errorf("got %d Ready events, want 1", got)
	}
	if got := count(events, event.NameSessionConnecting); got != 1 {
		t.Errorf("got %d Connecting events, want 1", got)
	}
	if got := count(events, event.NameSessionAuthenticated); got != 1 {
		t.Errorf("got %d Authenticated events, want 1", got)
	}
	// handshaking to login, and login to play.
	if got := count(events, event.NameSessionStateChanged); got < 2 {
		t.Errorf("got %d StateChanged events, want the login and play transitions", got)
	}
}

func TestConnectTimesOutWhenTheServerNeverPlaces(t *testing.T) {
	t.Parallel()

	// The fixture completes login and then sends nothing, so the client
	// reaches play but is never placed.
	addr, stop := fixture.Start(t, fixture.Script{ThroughReady: false})
	defer stop()

	bot := connectTo(t, addr, 200*time.Millisecond)
	defer func() { _ = bot.Close() }()

	start := time.Now()
	err := bot.Connect(t.Context())
	if !errors.Is(err, client.ErrConnectTimeout) {
		t.Fatalf("got %v, want ErrConnectTimeout", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("Connect took %v, well past its 200ms deadline", elapsed)
	}

	// "stuck in configuration" and "never placed" must be distinguishable
	// from the message alone, because that is the whole point of naming the
	// state a timeout reached.
	if !strings.Contains(err.Error(), "play") {
		t.Errorf("timeout error %v does not name the state it reached", err)
	}
}

func TestConnectFailsOnARefusedDial(t *testing.T) {
	t.Parallel()

	// Port 1 on loopback refuses immediately on every supported platform.
	bot := connectTo(t, "127.0.0.1:1", 2*time.Second)
	defer func() { _ = bot.Close() }()

	if err := bot.Connect(t.Context()); err == nil {
		t.Fatal("Connect succeeded against a refused address")
	}
}

func TestConnectTwiceIsAnError(t *testing.T) {
	t.Parallel()

	addr, stop := fixture.Start(t, fixture.Script{ThroughReady: true})
	defer stop()

	bot := connectTo(t, addr, 5*time.Second)
	defer func() { _ = bot.Close() }()

	if err := bot.Connect(t.Context()); err != nil {
		t.Fatalf("first Connect: %v", err)
	}
	if err := bot.Connect(t.Context()); !errors.Is(err, client.ErrAlreadyConnected) {
		t.Fatalf("second Connect returned %v, want ErrAlreadyConnected", err)
	}
}

func TestConnectOnAClosedClientIsAnError(t *testing.T) {
	t.Parallel()

	bot := connectTo(t, "127.0.0.1:1", time.Second)
	_ = bot.Close()

	if err := bot.Connect(t.Context()); !errors.Is(err, client.ErrClosed) {
		t.Fatalf("got %v, want ErrClosed", err)
	}
}

func TestCloseIsIdempotent(t *testing.T) {
	t.Parallel()

	addr, stop := fixture.Start(t, fixture.Script{ThroughReady: true})
	defer stop()

	bot := connectTo(t, addr, 5*time.Second)
	if err := bot.Connect(t.Context()); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	if err := bot.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := bot.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestCloseEmitsClosedExactlyOnce(t *testing.T) {
	t.Parallel()

	addr, stop := fixture.Start(t, fixture.Script{ThroughReady: true})
	defer stop()

	bot := connectTo(t, addr, 5*time.Second)
	sub, _ := bot.Subscribe(event.DomainSession, 64)

	if err := bot.Connect(t.Context()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	_ = bot.Close()
	_ = bot.Close()

	if got := count(drain(sub), event.NameSessionClosed); got != 1 {
		t.Errorf("got %d Closed events, want exactly 1", got)
	}
}

func TestDisconnectPacketProducesDisconnected(t *testing.T) {
	t.Parallel()

	addr, stop := fixture.Start(t, fixture.Script{
		ThroughReady: true,
		ThenKick:     "server closing",
	})
	defer stop()

	bot := connectTo(t, addr, 5*time.Second)
	sub, _ := bot.Subscribe(event.DomainSession, 64)

	if err := bot.Connect(t.Context()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	_ = bot.Wait()
	_ = bot.Close()

	var found bool
	for _, e := range drain(sub) {
		d, ok := e.(event.Disconnected)
		if !ok {
			continue
		}
		if d.Source != event.DisconnectByServer {
			continue
		}
		found = true
		if d.Reason != "server closing" {
			t.Errorf("reason is %q, want the kick text", d.Reason)
		}
	}
	if !found {
		t.Fatal("no server Disconnected event was published")
	}
}

func TestTransportLossProducesDisconnectedWithTransportSource(t *testing.T) {
	t.Parallel()

	addr, stop := fixture.Start(t, fixture.Script{
		ThroughReady: true,
		ThenDropConn: true,
	})
	defer stop()

	bot := connectTo(t, addr, 5*time.Second)
	sub, _ := bot.Subscribe(event.DomainSession, 64)

	if err := bot.Connect(t.Context()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	_ = bot.Wait()
	_ = bot.Close()

	var found bool
	for _, e := range drain(sub) {
		if d, ok := e.(event.Disconnected); ok && d.Source == event.DisconnectByTransport {
			found = true
		}
	}
	if !found {
		t.Fatal("a dropped connection did not produce a transport disconnect")
	}
}

func TestWaitReturnsWhenTheServerEndsTheSession(t *testing.T) {
	t.Parallel()

	addr, stop := fixture.Start(t, fixture.Script{
		ThroughReady: true,
		ThenKick:     "server closing",
	})
	defer stop()

	bot := connectTo(t, addr, 5*time.Second)
	defer func() { _ = bot.Close() }()

	if err := bot.Connect(t.Context()); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	// A server that says goodbye is not this client's failure.
	if err := bot.Wait(); err != nil {
		t.Errorf("Wait after a clean kick returned %v, want nil", err)
	}
}

func TestAServerThatHangsUpSilentlyStillReportsTheDisconnect(t *testing.T) {
	t.Parallel()

	// The case that published nothing. A server that is killed rather than one
	// that kicks sends no disconnect packet and resets nothing: the socket is
	// closed by the operating system, the client reads EOF, and the read loop
	// stops with no error to report. Reporting the loss only when there was an
	// error to name left a subscriber told about every ending except the one it
	// could not find out about any other way.
	addr, stop := fixture.Start(t, fixture.Script{
		ThroughReady: true,
		ThenHangUp:   true,
	})
	defer stop()

	bot := connectTo(t, addr, 5*time.Second)
	sub, _ := bot.Subscribe(event.DomainSession, 64)

	if err := bot.Connect(t.Context()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	_ = bot.Wait()
	_ = bot.Close()

	var found bool
	for _, e := range drain(sub) {
		d, ok := e.(event.Disconnected)
		if !ok {
			continue
		}
		found = true
		if d.Source != event.DisconnectByTransport {
			t.Errorf("source is %q, want a transport loss", d.Source)
		}
		if d.Reason == "" {
			t.Error("the disconnect carries no reason at all")
		}
	}
	if !found {
		t.Fatal("a server that hung up without a disconnect packet published nothing; " +
			"a subscriber has no other way to learn the connection is gone")
	}
}

func TestAKickIsNotReportedTwice(t *testing.T) {
	t.Parallel()

	// The other side of the same change. A kick publishes the server's own
	// reason and the loop then reads EOF, so a transport report at the end of
	// the loop would tell a subscriber the connection died twice.
	addr, stop := fixture.Start(t, fixture.Script{
		ThroughReady: true,
		ThenKick:     "server closing",
	})
	defer stop()

	bot := connectTo(t, addr, 5*time.Second)
	sub, _ := bot.Subscribe(event.DomainSession, 64)

	if err := bot.Connect(t.Context()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	_ = bot.Wait()
	_ = bot.Close()

	var disconnects int
	for _, e := range drain(sub) {
		if _, ok := e.(event.Disconnected); ok {
			disconnects++
		}
	}
	if disconnects != 1 {
		t.Errorf("a kick published %d disconnects, want exactly 1", disconnects)
	}
}
