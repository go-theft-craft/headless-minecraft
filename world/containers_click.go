package world

import (
	"fmt"
	"slices"

	"github.com/go-theft-craft/headless-minecraft/event"
)

// SlotSnapshot is one slot's contents at one moment.
//
// Known separates "held nothing" from "was never described": restoring an
// undescribed slot removes the entry rather than writing a nil the view would
// report as a described empty slot.
type SlotSnapshot struct {
	Slot int32
	Item any
	// Held reports the slot holding anything.
	Held bool
	// Known reports the slot having been described at all.
	Known bool
}

// Pending is a click the client has made and the server has not answered.
//
// It holds what the affected slots — and the cursor, which every click can
// move — contained before the click, because that is the only way to roll
// back on protocol 47: the server's rejection there is an apology transaction
// that carries no state, and a client that did not keep its own prior
// contents has nothing to restore. Protocol 775 resends the window and needs
// none of this, but keeping one mechanism means the rollback path is
// exercised by both versions' tests rather than only by one.
type Pending struct {
	// Sequence is the confirmation key. On protocol 47 it is the transaction
	// number the adapter allocated; on 775 the adapter numbers clicks itself,
	// because that protocol confirms by state rather than by echo.
	Sequence int32
	// Window is the menu the click landed in.
	Window int32
	// Before is what the affected slots held, in the order they were read.
	Before []SlotSnapshot
	// CursorBefore and CursorHeld are the cursor's prior contents.
	CursorBefore any
	CursorHeld   bool
}

// Snapshot reads the current contents of one window's slots and the cursor
// into a Pending, ready for Click. It is how a caller builds the rollback
// point without reaching into the store's maps.
func (s *Containers) Snapshot(sequence, window int32, slots ...int32) Pending {
	p := Pending{
		Sequence:     sequence,
		Window:       window,
		CursorBefore: s.cursor,
		CursorHeld:   s.cursorHeld,
	}
	menu, open := s.open[window]
	for _, slot := range slots {
		snapshot := SlotSnapshot{Slot: slot}
		if open {
			if item, known := menu.slots[slot]; known {
				snapshot.Item, snapshot.Known = item, true
				snapshot.Held = item != nil
			}
		}
		p.Before = append(p.Before, snapshot)
	}

	return p
}

// Click records a click as pending and applies its predicted effect.
//
// The prediction is applied through the same mutator a server update takes,
// so a subscriber cannot tell a prediction from a fact — which is the point:
// the caller that needs to know reads PendingClicks.
func (s *Containers) Click(c *event.Collector, p Pending, predicted []SlotSnapshot) {
	if len(s.pending) >= maxPending {
		s.droppedPending++

		return
	}
	s.pending = append(s.pending, p)

	items := make(map[int32]any, len(predicted))
	for _, slot := range predicted {
		value := slot.Item
		if !slot.Held {
			value = nil
		}
		items[slot.Slot] = value
	}
	if len(items) > 0 {
		s.SlotsChanged(c, p.Window, items, 0, false)
	}
}

// Confirm accepts a pending click. The prediction stands and the snapshot is
// dropped.
//
// The click must be the oldest pending one: protocol 47 answers transactions
// in order, so a confirmation for a later click means an earlier one was
// never answered, and treating that as success would leave its prediction
// standing unexamined forever.
func (s *Containers) Confirm(_ *event.Collector, sequence int32) error {
	at := slices.IndexFunc(s.pending, func(p Pending) bool { return p.Sequence == sequence })
	switch {
	case at < 0:
		return fmt.Errorf("%w: confirm %d", ErrUnknownSequence, sequence)
	case at > 0:
		return fmt.Errorf("%w: confirmed %d while %d clicks before it are unanswered",
			ErrOutOfOrder, sequence, at)
	}
	s.pending = s.pending[1:]

	return nil
}

// Reject rolls a pending click back to its snapshot and publishes the
// restored slots, so a caller watching container.slots_changed sees the truth
// rather than silently holding a wrong inventory.
//
// Everything predicted on top of the rejected click rolls back with it,
// newest first: a later click was predicted against the rejected one's
// outcome, and keeping it would leave the client holding a state that never
// existed on either side.
func (s *Containers) Reject(c *event.Collector, sequence int32) error {
	at := slices.IndexFunc(s.pending, func(p Pending) bool { return p.Sequence == sequence })
	if at < 0 {
		return fmt.Errorf("%w: reject %d", ErrUnknownSequence, sequence)
	}

	for index := len(s.pending) - 1; index >= at; index-- {
		s.restore(c, s.pending[index])
	}
	s.pending = s.pending[:at]

	return nil
}

// PendingClicks reports how many clicks await the server's answer.
func (s *Containers) PendingClicks() int { return len(s.pending) }

// Superseded drops every pending click for one window without rolling back.
//
// It is protocol 775's resolution: that protocol answers a click — landed or
// not — by resending the whole window, and the resend is the truth the
// predictions were waiting for. Restoring a snapshot over it would replace
// the server's answer with the client's guess.
func (s *Containers) Superseded(window int32) {
	kept := s.pending[:0]
	for _, p := range s.pending {
		if p.Window != window {
			kept = append(kept, p)
		}
	}
	s.pending = kept
}

// restore puts one pending click's prior contents back and publishes them.
func (s *Containers) restore(c *event.Collector, p Pending) {
	menu, open := s.open[p.Window]
	if open && len(p.Before) > 0 {
		slots := make([]int32, 0, len(p.Before))
		for _, snapshot := range p.Before {
			switch {
			case !snapshot.Known:
				delete(menu.slots, snapshot.Slot)
			case snapshot.Held:
				menu.slots[snapshot.Slot] = snapshot.Item
			default:
				menu.slots[snapshot.Slot] = nil
			}
			slots = append(slots, snapshot.Slot)
		}
		slices.Sort(slots)

		event.Emit(c, event.ContainerSlotsChanged{
			ContainerID: p.Window, Slots: slots,
			StateID: menu.stateID, StateKnown: menu.stateKnown,
			Dropped: menu.droppedSlots,
		})
	}

	s.CursorChanged(c, p.CursorBefore, p.CursorHeld)
}
