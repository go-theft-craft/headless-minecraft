package client

import (
	"context"
	"errors"
	"fmt"
	"io"

	protocol "github.com/go-theft-craft/minecraft-protocol"

	"github.com/go-theft-craft/headless-minecraft/event"
	"github.com/go-theft-craft/headless-minecraft/version"
)

// unrevised is the revision the loop stamps its events with.
//
// A revision names an observed-state snapshot, and this milestone publishes
// no state: the world that bumps one per batch is M7, and it stamps from
// there. Zero is the value no revision ever takes.
const unrevised = 0

// receiver is the loop's inbound source. A stream satisfies it directly; a
// test satisfies it with a slice.
type receiver interface {
	Receive(ctx context.Context) (protocol.Packet, error)
}

// sender is the loop's outbound sink, used for readiness replies.
type sender interface {
	Write(ctx context.Context, p protocol.Packet) error
}

// dispatcher runs the handlers registered for one packet.
//
// It is an interface so the loop does not care whether handlers come from
// this package's table or from the shared router.
type dispatcher interface {
	Dispatch(ctx context.Context, p protocol.Packet) error
}

// tableDispatcher looks handlers up by packet name.
//
// The adapter already keys its handlers on the descriptor's packet names, so
// no name-to-ID resolution is needed. An unregistered packet is not an error:
// a client that ignores most of the play state is the normal case.
type tableDispatcher struct {
	handlers map[string]version.Handler
}

func newTableDispatcher(handlers map[string]version.Handler) *tableDispatcher {
	return &tableDispatcher{handlers: handlers}
}

func (d *tableDispatcher) Dispatch(ctx context.Context, p protocol.Packet) error {
	handler, ok := d.handlers[p.Name]
	if !ok {
		return nil
	}

	return handler.Handle(ctx, p)
}

// runLoop owns inbound delivery until it returns.
//
// One goroutine reads, batches, dispatches, and publishes. Handlers run here,
// so a handler that blocks stalls the connection, including keepalive; the
// fan-out never blocks, which is what keeps that rule enforceable.
func (c *Client) runLoop(
	ctx context.Context,
	r receiver,
	w sender,
	d dispatcher,
	batcher *version.Batcher,
	collector *event.Collector,
	outbox *version.Outbox,
	readiness version.ReadinessRule,
	ready chan<- version.ReadyState,
) error {
	readySent := false

	for {
		packet, err := r.Receive(ctx)
		switch {
		case errors.Is(err, io.EOF):
			if finishErr := batcher.Finish(); finishErr != nil {
				return finishErr
			}

			return nil
		case err != nil:
			return fmt.Errorf("receive: %w", err)
		}

		batch, complete, err := batcher.Accept(packet)
		if err != nil {
			return err
		}
		if !complete {
			continue
		}

		collector.Reset()
		_ = outbox.Drain()

		for _, p := range batch.Packets {
			if d != nil {
				if err := d.Dispatch(ctx, p); err != nil {
					return fmt.Errorf("dispatch %s: %w", p.Name, err)
				}
			}
			event.Emit(collector, event.PacketReceived{
				State:   string(p.State),
				Packet:  p.Name,
				ID:      p.ID,
				Bundled: batch.Bundled,
			})
		}

		// Handlers answer through the outbox rather than writing: keepalives
		// for the whole session, and the two questions a configuration server
		// stops on. They go out before the readiness reply, in the order they
		// were queued.
		if err := c.send(ctx, w, collector, outbox.Drain()); err != nil {
			return err
		}

		state, reply, err := readiness.Observe(batch)
		if err != nil {
			return err
		}
		if err := c.send(ctx, w, collector, reply); err != nil {
			return err
		}

		// Publish before signalling ready, so a subscriber that was waiting
		// on Connect has already seen everything the placing batch produced.
		c.events.publish(collector.Events(unrevised))

		if state.Ready && !readySent {
			readySent = true
			c.publishReady(state)

			select {
			case ready <- state:
			default:
			}
		}
	}
}

// send writes one batch's answers and reports each as a sent packet.
func (c *Client) send(
	ctx context.Context,
	w sender,
	collector *event.Collector,
	packets []protocol.Packet,
) error {
	for _, p := range packets {
		if err := w.Write(ctx, p); err != nil {
			return fmt.Errorf("write %s: %w", p.Name, err)
		}
		event.Emit(collector, event.PacketSent{State: string(p.State), Packet: p.Name, ID: p.ID})
	}

	return nil
}

// publishReady announces the one point in a connection where the server will
// accept action packets.
func (c *Client) publishReady(state version.ReadyState) {
	var announcement event.Collector
	event.Emit(&announcement, event.Ready{
		EntityID:  state.EntityID,
		Dimension: state.Dimension,
		GameMode:  state.GameMode,
	})

	c.events.publish(announcement.Events(unrevised))
}
