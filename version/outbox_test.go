package version_test

import (
	"testing"

	protocol "github.com/go-theft-craft/minecraft-protocol"

	"github.com/go-theft-craft/headless-minecraft/version"
)

func TestOutboxDrainsInAddOrder(t *testing.T) {
	t.Parallel()

	var o version.Outbox
	o.Add(protocol.Packet{Name: "keep_alive"})
	o.Add(protocol.Packet{Name: "finish_configuration"})

	if got := o.Len(); got != 2 {
		t.Fatalf("outbox holds %d packets, want 2", got)
	}

	packets := o.Drain()
	if len(packets) != 2 {
		t.Fatalf("drained %d packets, want 2", len(packets))
	}
	if packets[0].Name != "keep_alive" || packets[1].Name != "finish_configuration" {
		t.Errorf("drained %v, want them in the order they were added", packets)
	}
}

func TestDrainEmptiesTheOutbox(t *testing.T) {
	t.Parallel()

	var o version.Outbox
	o.Add(protocol.Packet{Name: "keep_alive"})
	_ = o.Drain()

	if got := o.Len(); got != 0 {
		t.Errorf("outbox holds %d packets after Drain, want 0", got)
	}
	if got := len(o.Drain()); got != 0 {
		t.Errorf("a second Drain returned %d packets, want 0", got)
	}
}

func TestDrainedPacketsDoNotAliasTheOutbox(t *testing.T) {
	t.Parallel()

	var o version.Outbox
	o.Add(protocol.Packet{Name: "keep_alive"})

	drained := o.Drain()
	// The outbox reuses its slice across batches. A drain that aliased it
	// would have the next batch's answers overwrite the ones in flight.
	o.Add(protocol.Packet{Name: "teleport_confirm"})

	if drained[0].Name != "keep_alive" {
		t.Errorf("drained packet became %q: the drain aliased the outbox", drained[0].Name)
	}
}
