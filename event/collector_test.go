package event_test

import (
	"testing"

	"github.com/go-theft-craft/headless-minecraft/event"
)

func TestCollectorReturnsEventsInAppendOrder(t *testing.T) {
	t.Parallel()

	var c event.Collector
	event.Emit(&c, event.Connecting{Address: "a"})
	event.Emit(&c, event.Authenticated{Username: "b"})

	events := c.Events(1)
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2", len(events))
	}
	if events[0].Name() != event.SessionConnecting {
		t.Errorf("first event is %q, want connecting", events[0].Name())
	}
	if events[1].Name() != event.SessionAuthenticated {
		t.Errorf("second event is %q, want authenticated", events[1].Name())
	}
}

func TestCollectorEventsDoNotAliasTheCollector(t *testing.T) {
	t.Parallel()

	var c event.Collector
	event.Emit(&c, event.Connecting{Address: "a"})

	events := c.Events(1)
	c.Reset()
	event.Emit(&c, event.Closed{})

	if len(events) != 1 || events[0].Name() != event.SessionConnecting {
		t.Fatal("Events returned a slice that the collector kept writing into")
	}
}

func TestResetEmptiesTheCollector(t *testing.T) {
	t.Parallel()

	var c event.Collector
	event.Emit(&c, event.Closed{})
	c.Reset()

	if got := c.Len(); got != 0 {
		t.Errorf("collector holds %d events after Reset, want 0", got)
	}
	if got := len(c.Events(1)); got != 0 {
		t.Errorf("collector published %d events after Reset, want 0", got)
	}
}

func TestEveryEventInABatchCarriesTheBatchRevision(t *testing.T) {
	t.Parallel()

	var c event.Collector
	event.Emit(&c, event.Connecting{Address: "a"})
	event.Emit(&c, event.Ready{EntityID: 7})
	event.Emit(&c, event.PacketReceived{Packet: "login"})

	for _, e := range c.Events(42) {
		if got := e.Revision(); got != 42 {
			t.Errorf("%T reports revision %d, want 42", e, got)
		}
	}
}

func TestPublishedEventsKeepTheirConcreteTypes(t *testing.T) {
	t.Parallel()

	// A subscriber switches on the event type. Stamping must not hand it a
	// wrapper, or every type switch downstream falls through to its default.
	var c event.Collector
	event.Emit(&c, event.Ready{EntityID: 7, Dimension: "overworld"})

	ready, ok := c.Events(9)[0].(event.Ready)
	if !ok {
		t.Fatalf("published event has type %T, want event.Ready", c.Events(9)[0])
	}
	if ready.EntityID != 7 || ready.Dimension != "overworld" {
		t.Errorf("published event lost its fields: %+v", ready)
	}
	if ready.Revision() != 9 {
		t.Errorf("published event reports revision %d, want 9", ready.Revision())
	}
}

func TestEmitCopiesTheEvent(t *testing.T) {
	t.Parallel()

	var c event.Collector
	e := event.Connecting{Address: "first"}
	event.Emit(&c, e)
	e.Address = "second"

	if got := c.Events(1)[0].(event.Connecting).Address; got != "first" {
		t.Errorf("collector held address %q, want the value emitted, %q", got, "first")
	}
}

func TestOneStampsASingleEvent(t *testing.T) {
	t.Parallel()

	events := event.One(event.Closed{}, 5)
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	if events[0].Revision() != 5 {
		t.Errorf("revision is %d, want 5", events[0].Revision())
	}
	if _, ok := events[0].(event.Closed); !ok {
		t.Errorf("got %T, want event.Closed", events[0])
	}
}
