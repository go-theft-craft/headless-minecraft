package client

import (
	"context"
	"errors"
	"io"
	"time"

	protocol "github.com/go-theft-craft/minecraft-protocol"

	"github.com/go-theft-craft/headless-minecraft/event"
)

// shutdownTimeout bounds the polite half of Close: the disconnect the client
// sends before it stops reading. A server that never drains it must not keep
// Close blocked.
const shutdownTimeout = 2 * time.Second

// loopFinished records why the read loop stopped and publishes what a
// subscriber needs to know about it.
//
// The loop ending is not the same as Close being called. A server that kicks,
// or a transport that dies, ends the loop while the client is still open, and
// the subscriber learns which of the two happened from Disconnected.Source.
func (c *Client) loopFinished(err error) {
	c.mu.Lock()
	c.loopError = err
	stream := c.stream
	closing := c.closed
	c.mu.Unlock()

	if !closing {
		c.publishDisconnect(err, stream)
	}

	close(c.loop)
}

// publishDisconnect reports a session that ended on its own.
//
// A kick already produced a Disconnected from the adapter's handler, and the
// loop then ends on EOF. Reporting a transport loss as well would tell a
// subscriber the connection died twice, so a clean end publishes nothing.
func (c *Client) publishDisconnect(err error, stream *protocol.Stream) {
	if ignoreEnded(err) == nil {
		return
	}

	c.events.publish(event.One(event.Disconnected{
		Source: event.DisconnectByTransport,
		Reason: err.Error(),
		State:  string(currentState(context.Background(), stream)),
	}, unrevised))
}

// Close ends the client and every subscription it handed out. It is
// idempotent, and it is safe on a client that never connected.
//
// Closed is the last event a subscriber receives, and it is published exactly
// once however many times Close is called.
func (c *Client) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		<-c.done

		return c.closeErr
	}
	c.closed = true
	stream := c.stream
	stop := c.stop
	c.mu.Unlock()

	if stream != nil {
		// Say goodbye before stopping the loop, so the server sees a
		// disconnect rather than a connection that vanished.
		ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		c.closeErr = ignoreEnded(stream.Shutdown(ctx, "client closed"))
		cancel()
	}
	if stop != nil {
		stop()
		<-c.loop
	}

	c.events.publish(event.One(event.Closed{}, unrevised))
	c.events.closeAll()
	close(c.done)

	return c.closeErr
}

// Wait blocks until the connection ends and reports why.
//
// It returns nil for a session that ended cleanly, including one the server
// closed with a disconnect packet: that is the server's decision, not this
// client's failure.
func (c *Client) Wait() error {
	<-c.loop

	c.mu.Lock()
	defer c.mu.Unlock()

	return ignoreEnded(c.loopError)
}

// loopErr reports the loop's failure without waiting for it.
func (c *Client) loopErr() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.loopError
}

// ignoreEnded drops the errors that mean "this connection is over", which is
// not a failure when it is what was asked for.
func ignoreEnded(err error) error {
	switch {
	case err == nil,
		errors.Is(err, io.EOF),
		errors.Is(err, context.Canceled),
		errors.Is(err, protocol.ErrStreamClosed):
		return nil
	default:
		return err
	}
}
