package client

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

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

// terrainWatch reports a session that reached play and never observed terrain.
//
// A client that connects, is placed, answers keepalives, and loads no chunk
// looks healthy from every angle the library otherwise reports on, and answers
// "not loaded" for every block a consumer asks about. That is the shape of the
// M7 defect: nothing failed, terrain events simply never fired, and a person
// watching a bot stand still was the only detector.
//
// It reports rather than fails. Nothing in either protocol obliges a server to
// send terrain, so a session without it is suspect rather than invalid.
//
// The check rides on inbound batches rather than a timer: the loop is one
// goroutine, and a timer would need a second one to say something the loop can
// say for itself. Keepalives keep batches arriving on both protocols, and a
// connection quiet enough to defeat this is one the loop already reports on.
type terrainWatch struct {
	grace   time.Duration
	readyAt time.Time
	seen    bool
	said    bool
}

// observe folds one batch's events in and reports whether the client should
// now say that terrain never arrived.
func (w *terrainWatch) observe(now time.Time, events []event.Event) (time.Duration, bool) {
	if w.seen || w.said || w.readyAt.IsZero() {
		return 0, false
	}
	for _, published := range events {
		if published.Name() == event.NameWorldChunkLoaded {
			w.seen = true

			return 0, false
		}
	}

	since := now.Sub(w.readyAt)
	if since < w.grace {
		return 0, false
	}
	w.said = true

	return since, true
}

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
	// Only with a world: without one the client observes no terrain by
	// construction, and reporting that would be reporting the consumer's own
	// choice back at them.
	watch := &terrainWatch{grace: c.observationGrace}
	if c.world == nil {
		watch = nil
	}

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

		// The world is applied after the handlers, so a session event and a
		// state event from one batch publish together, and before the
		// publish, so every event names a revision that already exists.
		revision := uint64(unrevised)
		if c.world != nil {
			revision, err = c.world.Apply(batch, collector)
			if err != nil {
				return fmt.Errorf("apply batch: %w", err)
			}
		}

		// Publish before signalling ready, so a subscriber that was waiting
		// on Connect has already seen everything the placing batch produced.
		published := collector.Events(revision)
		c.events.publish(published)
		c.watchTerrain(watch, published, revision)

		if state.Ready && !readySent {
			readySent = true
			if watch != nil {
				// The clock starts where the promise does: a server has no
				// reason to send terrain before it has placed the player.
				watch.readyAt = time.Now()
			}
			// Before the announcement, so a subscriber that acts on Ready and a
			// caller that returns from Connect both find the client willing.
			c.enterPlay()
			c.publishReady(state)

			select {
			case ready <- state:
			default:
			}
		}
	}
}

// send writes one batch's answers and reports each as a sent packet.
//
// It takes the client's write lock, which Do takes as well: a batch's answers go
// out together, and an action from another goroutine lands either before them or
// after them but never between them.
func (c *Client) send(
	ctx context.Context,
	w sender,
	collector *event.Collector,
	packets []protocol.Packet,
) error {
	if len(packets) == 0 {
		return nil
	}

	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	for _, p := range packets {
		if err := w.Write(ctx, p); err != nil {
			return fmt.Errorf("write %s: %w", p.Name, err)
		}
		event.Emit(collector, event.PacketSent{State: string(p.State), Packet: p.Name, ID: p.ID})
	}

	return nil
}

// watchTerrain publishes the report that a placed session has loaded no chunk.
//
// It is published on its own rather than folded into the batch that triggered
// it: it describes the whole session up to this revision, not what this batch
// carried, and the batch has already gone out.
func (c *Client) watchTerrain(watch *terrainWatch, published []event.Event, revision uint64) {
	if watch == nil {
		return
	}
	since, missing := watch.observe(time.Now(), published)
	if !missing {
		return
	}

	c.logger.Warn(
		"the session was placed and has loaded no chunk; every block lookup will report the chunk as not loaded",
		"since", since,
		"observation", event.NameWorldChunkLoaded,
	)

	var announcement event.Collector
	event.Emit(&announcement, event.ObservationMissing{
		Observation: event.NameWorldChunkLoaded,
		Since:       since,
	})

	c.events.publish(announcement.Events(revision))
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
