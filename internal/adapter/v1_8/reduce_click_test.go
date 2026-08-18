package v1_8_test

import (
	"errors"
	"testing"

	protocol "github.com/go-theft-craft/minecraft-protocol"
	gen "github.com/go-theft-craft/minecraft-protocol/generated/java/v1_8"

	"github.com/go-theft-craft/headless-minecraft/event"
	adapter "github.com/go-theft-craft/headless-minecraft/internal/adapter/v1_8"
	"github.com/go-theft-craft/headless-minecraft/version"
	"github.com/go-theft-craft/headless-minecraft/world"
)

// clickScript stages a world with an open chest, one pending click, and the
// prediction it applied, then applies the given packets.
func clickScript(t *testing.T, packets ...protocol.Packet) (*world.World, error) {
	t.Helper()

	w := world.New()
	for _, reducer := range adapter.Reducers(w) {
		if err := w.Register(reducer); err != nil {
			t.Fatalf("Register: %v", err)
		}
	}

	var c event.Collector
	if _, err := w.Apply(version.Batch{Packets: []protocol.Packet{
		play(&gen.PlayClientboundOpenWindow{WindowID: 3, InventoryType: "minecraft:chest"}),
		play(&gen.PlayClientboundWindowItems{WindowID: 3, Items: []gen.Slot{{BlockID: 1}, {BlockID: -1}}}),
	}, State: gen.StatePlay}, &c); err != nil {
		t.Fatalf("Apply the stage: %v", err)
	}

	// The pending click, recorded the way client.Click records one: snapshot
	// first, prediction after.
	if _, err := w.Amend(func(s *world.Containers, collector *event.Collector) error {
		pending := s.Snapshot(7, 3, 0)
		s.Click(collector, pending, []world.SlotSnapshot{{Slot: 0, Held: false, Known: true}})
		s.CursorChanged(collector, gen.Slot{BlockID: 1}, true)

		return nil
	}); err != nil {
		t.Fatalf("Amend: %v", err)
	}

	var answers event.Collector
	_, err := w.Apply(version.Batch{Packets: packets, State: gen.StatePlay}, &answers)

	return w, err
}

func TestAnAcceptedTransactionConfirmsThePendingClick(t *testing.T) {
	t.Parallel()

	w, err := clickScript(t,
		play(&gen.PlayClientboundTransaction{WindowID: 3, Action: 7, Accepted: true}))
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	containers := w.Snapshot().Containers
	if containers.PendingClicks != 0 {
		t.Fatalf("PendingClicks = %d after the server accepted", containers.PendingClicks)
	}
	// The prediction stands: this protocol sends nothing after an accepted
	// click, and the prediction is all the client will ever have.
	if got := containers.Open[3].Slots[0]; got != nil {
		t.Fatalf("slot 0 holds %v after an accepted pickup", got)
	}
}

func TestARejectedTransactionRollsThePendingClickBack(t *testing.T) {
	t.Parallel()

	w, err := clickScript(t,
		play(&gen.PlayClientboundTransaction{WindowID: 3, Action: 7, Accepted: false}))
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	containers := w.Snapshot().Containers
	if containers.PendingClicks != 0 {
		t.Fatalf("PendingClicks = %d after the server rejected", containers.PendingClicks)
	}
	if got := containers.Open[3].Slots[0]; got != any(gen.Slot{BlockID: 1}) {
		t.Fatalf("slot 0 holds %v after the rollback, want the stack", got)
	}
	if containers.CursorHeld {
		t.Fatal("the cursor still holds the rejected pickup")
	}
}

func TestATransactionForAClickNobodyMadeIsIgnored(t *testing.T) {
	t.Parallel()

	// A server answering a click made before a reconnect, or in a window that
	// has closed, is not a poisoned world.
	w, err := clickScript(t,
		play(&gen.PlayClientboundTransaction{WindowID: 3, Action: 99, Accepted: true}))
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got := w.Snapshot().Containers.PendingClicks; got != 1 {
		t.Fatalf("PendingClicks = %d; the unknown answer resolved something", got)
	}
}

func TestAnOutOfOrderConfirmationPoisonsTheWorld(t *testing.T) {
	t.Parallel()

	// Confirming the second click while the first is unanswered means the
	// first was skipped and its prediction would stand unexamined forever.
	// The pendings as a whole can no longer be trusted, and a world that
	// cannot be trusted says so through the same mechanism every other
	// unrecoverable divergence uses.
	w := world.New()
	for _, reducer := range adapter.Reducers(w) {
		if err := w.Register(reducer); err != nil {
			t.Fatalf("Register: %v", err)
		}
	}
	if _, err := w.Amend(func(s *world.Containers, collector *event.Collector) error {
		s.Click(collector, s.Snapshot(1, 3, 0), nil)
		s.Click(collector, s.Snapshot(2, 3, 1), nil)

		return nil
	}); err != nil {
		t.Fatalf("Amend: %v", err)
	}

	var c event.Collector
	_, err := w.Apply(version.Batch{Packets: []protocol.Packet{
		play(&gen.PlayClientboundTransaction{WindowID: 3, Action: 2, Accepted: true}),
	}, State: gen.StatePlay}, &c)
	if !errors.Is(err, world.ErrOutOfOrder) {
		t.Fatalf("Apply = %v, want ErrOutOfOrder", err)
	}
}

func TestARejectedTransactionIsApologisedFor(t *testing.T) {
	t.Parallel()

	// A 1.8.9 server that rejects a click disables further clicking until the
	// client echoes the transaction back with accepted=false. A client that
	// never apologises has a window that silently stopped working.
	outbox := new(version.Outbox)
	a := adapter.New(new(event.Collector), outbox)

	handler, ok := a.Handlers()["transaction"]
	if !ok {
		t.Fatal("the adapter registers no transaction handler")
	}
	if err := handler.Handle(t.Context(), play(&gen.PlayClientboundTransaction{
		WindowID: 3, Action: 7, Accepted: false,
	})); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	queued := outbox.Drain()
	if len(queued) != 1 || queued[0].Name != "transaction" {
		t.Fatalf("queued %d packets, want the one apology", len(queued))
	}
	answer := queued[0].Value.(*gen.PlayServerboundTransaction)
	if answer.WindowID != 3 || answer.Action != 7 || answer.Accepted {
		t.Fatalf("apologised with %+v", answer)
	}
}

func TestAnAcceptedTransactionIsNotEchoed(t *testing.T) {
	t.Parallel()

	// Vanilla echoes only rejections. An echoed acceptance is a packet the
	// server never expects.
	outbox := new(version.Outbox)
	a := adapter.New(new(event.Collector), outbox)

	if err := a.Handlers()["transaction"].Handle(t.Context(), play(&gen.PlayClientboundTransaction{
		WindowID: 3, Action: 7, Accepted: true,
	})); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if queued := outbox.Drain(); len(queued) != 0 {
		t.Fatalf("queued %d packets for an accepted transaction", len(queued))
	}
}
