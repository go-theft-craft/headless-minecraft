package world_test

import (
	"errors"
	"testing"

	"github.com/go-theft-craft/headless-minecraft/event"
	"github.com/go-theft-craft/headless-minecraft/world"
)

// chestWindow is the menu every click test stages.
const chestWindow int32 = 3

// stack is the item shape these tests store. The store keeps items as `any`,
// so what matters here is only that two values compare.
type stack struct {
	Item  string
	Count int
}

// clickFixture is one staged window and the world it lives in.
type clickFixture struct {
	world *world.World
	store *world.Containers
}

// containersWith stages one open window whose slots hold the given stacks.
func containersWith(t *testing.T, items map[int32]any) (clickFixture, *event.Collector) {
	t.Helper()

	w := world.New()
	collector := &event.Collector{}
	s := w.Containers()
	s.Opened(collector, event.ContainerOpened{ContainerID: chestWindow, MenuType: "chest"})
	s.SlotsChanged(collector, chestWindow, items, 0, false)

	return clickFixture{world: w, store: s}, collector
}

// view reads the container snapshot the way a caller would.
func (f clickFixture) view() world.ContainersView { return f.world.Snapshot().Containers }

// slotItem reads one slot through the view.
func slotItem(t *testing.T, f clickFixture, slot int32) any {
	t.Helper()

	return f.view().Open[chestWindow].Slots[slot]
}

// swapPrediction predicts slot 0's stack moving to slot 1.
func swapPrediction() []world.SlotSnapshot {
	return []world.SlotSnapshot{
		{Slot: 0, Held: false, Known: true},
		{Slot: 1, Item: stack{"stone", 64}, Held: true, Known: true},
	}
}

func TestARejectedClickRestoresWhatWasThere(t *testing.T) {
	t.Parallel()

	// The silent failure this exists to prevent: the client predicts a swap,
	// the server refuses, and nothing corrects it. A wrong inventory is
	// invisible until a craft fails, three actions later, for no visible
	// reason.
	f, collector := containersWith(t, map[int32]any{0: stack{"stone", 64}, 1: nil})
	f.store.Click(collector, f.store.Snapshot(1, chestWindow, 0, 1), swapPrediction())

	if got := slotItem(t, f, 1); got != any(stack{"stone", 64}) {
		t.Fatalf("slot 1 holds %v after the predicted swap, want the stack", got)
	}

	if err := f.store.Reject(collector, 1); err != nil {
		t.Fatalf("Reject: %v", err)
	}
	if got := slotItem(t, f, 0); got != any(stack{"stone", 64}) {
		t.Fatalf("slot 0 holds %v after rollback, want the stack", got)
	}
	if got := slotItem(t, f, 1); got != nil {
		t.Fatalf("slot 1 holds %v after rollback, want empty", got)
	}
}

func TestARejectionPublishesTheRestoredSlots(t *testing.T) {
	t.Parallel()

	// Rolling back without publishing leaves every subscriber holding the
	// prediction. The rollback would be correct and invisible, which is the
	// worst of both.
	f, _ := containersWith(t, map[int32]any{0: stack{"stone", 64}, 1: nil})
	collector := &event.Collector{}
	f.store.Click(collector, f.store.Snapshot(1, chestWindow, 0, 1), swapPrediction())

	rejection := &event.Collector{}
	if err := f.store.Reject(rejection, 1); err != nil {
		t.Fatalf("Reject: %v", err)
	}

	var published bool
	for _, one := range rejection.Events(0) {
		if one.Name() == event.NameContainerSlotsChanged {
			published = true
		}
	}
	if !published {
		t.Fatal("a rollback published no slot change")
	}
}

func TestConfirmingAnUnknownSequenceIsAnError(t *testing.T) {
	t.Parallel()

	// A confirmation for a click nobody made means the sequence has drifted,
	// which on 47 means every subsequent confirmation is answering the wrong
	// click. Failing loudly beats accumulating a silent offset.
	f, collector := containersWith(t, map[int32]any{0: stack{"stone", 64}})
	if err := f.store.Confirm(collector, 99); !errors.Is(err, world.ErrUnknownSequence) {
		t.Fatalf("Confirm = %v, want ErrUnknownSequence", err)
	}
	if err := f.store.Reject(collector, 99); !errors.Is(err, world.ErrUnknownSequence) {
		t.Fatalf("Reject = %v, want ErrUnknownSequence", err)
	}
}

func TestConfirmingOutOfOrderIsAnError(t *testing.T) {
	t.Parallel()

	// Protocol 47 answers transactions in order. A confirmation for the
	// second click while the first is unanswered means the first was skipped,
	// and its prediction would stand unexamined forever.
	f, collector := containersWith(t, map[int32]any{0: stack{"stone", 64}, 1: nil, 2: nil})
	f.store.Click(collector, f.store.Snapshot(1, chestWindow, 0, 1), swapPrediction())
	f.store.Click(collector, f.store.Snapshot(2, chestWindow, 1, 2), nil)

	if err := f.store.Confirm(collector, 2); !errors.Is(err, world.ErrOutOfOrder) {
		t.Fatalf("Confirm out of order = %v, want ErrOutOfOrder", err)
	}
	if err := f.store.Confirm(collector, 1); err != nil {
		t.Fatalf("Confirm in order: %v", err)
	}
	if err := f.store.Confirm(collector, 2); err != nil {
		t.Fatalf("Confirm the next: %v", err)
	}
	if got := f.store.PendingClicks(); got != 0 {
		t.Fatalf("%d clicks still pending after both confirmations", got)
	}
}

func TestPendingClicksResolveInOrder(t *testing.T) {
	t.Parallel()

	// Rejecting the first of two pending clicks must roll back the second as
	// well: the second was predicted on top of the first, and keeping it
	// leaves the client holding a state that never existed on either side.
	f, collector := containersWith(t, map[int32]any{0: stack{"stone", 64}, 1: nil, 2: nil})
	f.store.Click(collector, f.store.Snapshot(1, chestWindow, 0, 1), swapPrediction())
	f.store.Click(collector, f.store.Snapshot(2, chestWindow, 1, 2), []world.SlotSnapshot{
		{Slot: 1, Held: false, Known: true},
		{Slot: 2, Item: stack{"stone", 64}, Held: true, Known: true},
	})

	if err := f.store.Reject(collector, 1); err != nil {
		t.Fatalf("Reject: %v", err)
	}
	if got := slotItem(t, f, 0); got != any(stack{"stone", 64}) {
		t.Fatalf("slot 0 holds %v; rejecting the first click did not roll back "+
			"the second, which was predicted on top of it", got)
	}
	if got := slotItem(t, f, 2); got != nil {
		t.Fatalf("slot 2 holds %v after the full rollback, want empty", got)
	}
	if got := f.store.PendingClicks(); got != 0 {
		t.Fatalf("%d clicks still pending after the rollback", got)
	}
}

func TestARejectionRestoresTheCursor(t *testing.T) {
	t.Parallel()

	// Every click can move the cursor, and 47's rejection restores nothing.
	f, collector := containersWith(t, map[int32]any{0: stack{"stone", 64}})
	pending := f.store.Snapshot(1, chestWindow, 0)
	f.store.Click(collector, pending, []world.SlotSnapshot{{Slot: 0, Held: false, Known: true}})
	f.store.CursorChanged(collector, stack{"stone", 64}, true)

	if err := f.store.Reject(collector, 1); err != nil {
		t.Fatalf("Reject: %v", err)
	}
	if view := f.view(); view.CursorHeld {
		t.Fatalf("the cursor holds %v after rollback, want the empty hand it had", view.Cursor)
	}
}

func TestClosingAWindowDropsItsPendingClicks(t *testing.T) {
	t.Parallel()

	// A closed window cannot be rolled back and its confirmations answer
	// nothing; keeping its pendings would wedge the in-order rule forever.
	f, collector := containersWith(t, map[int32]any{0: stack{"stone", 64}, 1: nil})
	f.store.Click(collector, f.store.Snapshot(1, chestWindow, 0, 1), swapPrediction())
	f.store.Closed(collector, chestWindow)

	if got := f.store.PendingClicks(); got != 0 {
		t.Fatalf("%d clicks still pending after the window closed", got)
	}
}
