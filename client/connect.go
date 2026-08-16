package client

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"time"

	protocol "github.com/go-theft-craft/minecraft-protocol"
	"github.com/go-theft-craft/minecraft-protocol/login"

	"github.com/go-theft-craft/headless-minecraft/auth"
	"github.com/go-theft-craft/headless-minecraft/event"
	"github.com/go-theft-craft/headless-minecraft/version"
	"github.com/go-theft-craft/headless-minecraft/version/java"
)

// ErrConnectTimeout reports a connection that reached the server but was
// never placed in the world before the connect deadline.
var ErrConnectTimeout = errors.New("connect timed out")

// ErrAlreadyConnected reports a second Connect on one client. A client owns
// one connection: reconnecting can repeat actions, so it is the application's
// decision, and its own new client.
var ErrAlreadyConnected = errors.New("client is already connected")

// ErrClosed reports use of a client that has been closed.
var ErrClosed = errors.New("client is closed")

// Connect dials, logs in, and returns once the server will accept action
// packets.
//
// It publishes what it does as it goes, so a subscriber watching the session
// domain sees Connecting, Authenticated, StateChanged, and finally Ready. It
// never reconnects.
func (c *Client) Connect(ctx context.Context) error {
	if err := c.begin(); err != nil {
		return err
	}

	c.events.publish(event.One(event.Connecting{Address: c.address}, unrevised))

	identity, err := c.provider.Authenticate(ctx)
	if err != nil {
		return fmt.Errorf("authenticate: %w", err)
	}
	c.events.publish(event.One(event.Authenticated{
		Username: identity.Username,
		UUID:     identity.UUID,
	}, unrevised))

	// The loop outlives Connect, so it runs under a context the client owns
	// rather than the caller's, which may be cancelled the moment Connect
	// returns.
	loopCtx, stop := context.WithCancel(context.WithoutCancel(ctx))

	stream, err := c.dial(ctx, loopCtx)
	if err != nil {
		stop()

		return err
	}

	if err := c.negotiate(ctx, stream, identity); err != nil {
		stop()
		_ = stream.Close()

		return err
	}

	ready := make(chan version.ReadyState, 1)
	c.startLoop(loopCtx, stop, stream, ready)

	return c.awaitReady(ctx, stream, ready)
}

// begin claims the client's one connection.
func (c *Client) begin() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return ErrClosed
	}
	if c.connecting || c.stream != nil {
		return ErrAlreadyConnected
	}
	c.connecting = true

	return nil
}

// dial opens the transport and starts the managed stream.
//
// The stream runs until the loop context ends, not until Connect returns.
func (c *Client) dial(ctx, loopCtx context.Context) (*protocol.Stream, error) {
	session, err := c.profile.Protocol.NewSession(protocol.RoleClient, c.profile.Limits)
	if err != nil {
		return nil, fmt.Errorf("new session: %w", err)
	}

	dialer := net.Dialer{Timeout: c.connectTimeout}
	conn, err := dialer.DialContext(ctx, "tcp", c.address)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", c.address, err)
	}

	stream, err := protocol.NewStream(session, protocol.Transport{
		Reader:    conn,
		Writer:    conn,
		Interrupt: conn.Close,
	}, protocol.WithObservationSink(&stateWatcher{client: c}))
	if err != nil {
		_ = conn.Close()

		return nil, fmt.Errorf("new stream: %w", err)
	}
	if err := stream.Start(loopCtx); err != nil {
		_ = conn.Close()

		return nil, fmt.Errorf("start stream: %w", err)
	}

	return stream, nil
}

// negotiate writes the handshake and runs the shared login negotiator.
func (c *Client) negotiate(ctx context.Context, stream *protocol.Stream, identity auth.Identity) error {
	host, port, err := splitAddress(c.address)
	if err != nil {
		return err
	}

	if err := stream.Write(ctx, c.profile.Adapter.Handshake(host, port)); err != nil {
		return fmt.Errorf("write handshake: %w", err)
	}

	// The client takes the connection over at the state the profile names,
	// so configuration packets reach handlers instead of being consumed
	// inside the login sequence. Protocol 47 names none and runs to play.
	negotiator, err := login.NewNegotiator(
		identity.Authenticator,
		login.WithTerminalState(c.profile.Adapter.LoginTerminalState()),
	)
	if err != nil {
		return fmt.Errorf("new negotiator: %w", err)
	}
	if _, err := negotiator.Negotiate(ctx, stream); err != nil {
		return fmt.Errorf("login: %w", err)
	}

	return nil
}

// startLoop records the connection and runs the read loop.
func (c *Client) startLoop(
	loopCtx context.Context,
	stop func(),
	stream *protocol.Stream,
	ready chan version.ReadyState,
) {
	// The bundle limit is validated at construction, so this cannot fail.
	batcher, err := version.NewBatcher(java.BundleDelimiter(c.profile.ID), c.bundleLimit)
	if err != nil {
		panic("client: " + err.Error())
	}

	c.mu.Lock()
	c.stream = stream
	c.stop = stop
	c.connecting = false
	c.mu.Unlock()

	go func() {
		err := c.runLoop(
			loopCtx,
			streamReceiver{stream: stream},
			streamSender{stream: stream},
			newTableDispatcher(c.profile.Adapter.Handlers()),
			batcher,
			c.profile.Collector,
			c.profile.Outbox,
			c.profile.Readiness,
			ready,
		)
		c.loopFinished(err)
	}()
}

// awaitReady blocks until the server places the player, the deadline passes,
// or the loop stops first.
func (c *Client) awaitReady(
	ctx context.Context,
	stream *protocol.Stream,
	ready <-chan version.ReadyState,
) error {
	deadline := time.NewTimer(c.connectTimeout)
	defer deadline.Stop()

	select {
	case <-ready:
		return nil
	case <-c.loop:
		return fmt.Errorf("connection ended before the player was placed: %w", c.loopErr())
	case <-ctx.Done():
		return ctx.Err()
	case <-deadline.C:
		return fmt.Errorf(
			"%w after %v in state %q",
			ErrConnectTimeout, c.connectTimeout, currentState(ctx, stream),
		)
	}
}

// currentState reports the protocol state a stream reached, so a timeout says
// where it stopped rather than only that it did. "Stuck in configuration" and
// "never placed in play" are different faults with different fixes.
func currentState(ctx context.Context, stream *protocol.Stream) protocol.State {
	snapshot, err := stream.Snapshot(ctx)
	if err != nil {
		return "unknown"
	}

	return snapshot.State
}

func splitAddress(address string) (string, uint16, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return "", 0, fmt.Errorf("address %q: %w", address, err)
	}
	number, err := strconv.ParseUint(port, 10, 16)
	if err != nil {
		return "", 0, fmt.Errorf("address %q: %w", address, err)
	}

	return host, uint16(number), nil
}

// streamReceiver and streamSender adapt the managed stream to the loop's own
// interfaces, which exist so the loop can be driven without a network.
type streamReceiver struct{ stream *protocol.Stream }

func (r streamReceiver) Receive(ctx context.Context) (protocol.Packet, error) {
	return r.stream.Read(ctx)
}

type streamSender struct{ stream *protocol.Stream }

func (s streamSender) Write(ctx context.Context, p protocol.Packet) error {
	return s.stream.Write(ctx, p)
}

// stateWatcher publishes a StateChanged for every transition the stream
// applies.
//
// It reads the transitions rather than inferring them, so protocol 775's
// return from play to configuration is reported like any other.
type stateWatcher struct{ client *Client }

func (w *stateWatcher) Observe(_ context.Context, o protocol.Observation) error {
	if o.Before.State == o.After.State {
		return nil
	}
	w.client.events.publish(event.One(event.StateChanged{
		From: string(o.Before.State),
		To:   string(o.After.State),
	}, unrevised))

	return nil
}
