package v26_1_test

import (
	"testing"

	protocol "github.com/go-theft-craft/minecraft-protocol"
	gen "github.com/go-theft-craft/minecraft-protocol/generated/java/v26_1"

	"github.com/go-theft-craft/headless-minecraft/event"
	adapter "github.com/go-theft-craft/headless-minecraft/internal/adapter/v26_1"
	"github.com/go-theft-craft/headless-minecraft/version"
	"github.com/go-theft-craft/headless-minecraft/world"
)

func TestAFullWindowSetSupersedesThePendingClicks(t *testing.T) {
	t.Parallel()

	// This protocol answers a click — landed or not — by resending the whole
	// window, and the resend is the truth the predictions were waiting for.
	// Restoring a snapshot over it would replace the server's answer with the
	// client's guess, so the pendings are dropped rather than rolled back.
	w := world.New()
	for _, reducer := range adapter.Reducers(w) {
		if err := w.Register(reducer); err != nil {
			t.Fatalf("Register: %v", err)
		}
	}

	if _, err := w.Amend(func(s *world.Containers, collector *event.Collector) error {
		s.Click(collector, s.Snapshot(1, 3, 0), []world.SlotSnapshot{
			{Slot: 0, Held: false, Known: true},
		})

		return nil
	}); err != nil {
		t.Fatalf("Amend: %v", err)
	}

	var c event.Collector
	truth := &gen.Slot{}
	if _, err := w.Apply(version.Batch{Packets: []protocol.Packet{
		play(&gen.PlayClientboundWindowItems{WindowID: 3, StateID: 9, Items: []*gen.Slot{truth}}),
	}, State: gen.StatePlay}, &c); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	containers := w.Snapshot().Containers
	if containers.PendingClicks != 0 {
		t.Fatalf("PendingClicks = %d after the server resent the window", containers.PendingClicks)
	}
	if got := containers.Open[3].Slots[0]; got != any(truth) {
		t.Fatalf("slot 0 holds %v, want the resent truth", got)
	}
}
