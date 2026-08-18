package client

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-theft-craft/headless-minecraft/event"
	"github.com/go-theft-craft/headless-minecraft/version"
	"github.com/go-theft-craft/headless-minecraft/world"
)

// ErrUnknownContainer reports a click into a window the client is not
// tracking. A vanilla client cannot click in a menu it was never shown.
var ErrUnknownContainer = errors.New("window is not open")

// ErrUnpredictable reports a click this client refused to send on protocol 47
// because it cannot predict the outcome.
//
// The refusal is deliberate, and it is version-owned behaviour stated in the
// version-neutral path: a 1.8.9 server that accepts a click sends nothing
// back — it trusts the client to have predicted — so a click the client
// cannot predict leaves it silently holding wrong slots until a craft fails
// three actions later. Protocol 775 answers every click with the truth, so
// the same click is sent freely there.
var ErrUnpredictable = errors.New("this click's outcome cannot be predicted, " +
	"and protocol 47 announces nothing after an accepted click")

// Click clicks one slot in one window: the pre-click snapshot, the predicted
// effect, the claim, and the packet, in that order.
//
// The snapshot is recorded before the packet is sent, so the server's answer
// cannot race past it; the claim is what the client believes the slot holds,
// which is what a 1.8.9 server compares its own result against; and the
// prediction is applied only where it is exact — a pickup or a placement with
// exactly one occupied side. Everything else the client either leaves to the
// server's answer (protocol 775 resends the truth) or refuses to send
// (protocol 47 does not).
//
// Confirmation is version-owned and invisible from here: 47 echoes the
// transaction and the adapter confirms or rolls back from the verdict; 775
// resends the window and the resend supersedes the pending click. A caller
// that needs to know whether predictions are still outstanding reads the
// snapshot's PendingClicks.
func (c *Client) Click(
	ctx context.Context, window int32, slot int16, button int8, mode version.ClickMode,
) error {
	snapshot := c.World().Containers
	view, open := snapshot.Open[window]
	if !open {
		return fmt.Errorf("click window %d: %w", window, ErrUnknownContainer)
	}

	claim, described := view.Slots[int32(slot)]
	predicted, cursorAfter, exact := c.predictClick(view, snapshot, slot, button, mode, described)
	if !exact && c.profile.ID == protocol47 {
		return fmt.Errorf("click %s slot %d: %w", mode, slot, ErrUnpredictable)
	}

	sequence := int16(c.clicks.Add(1))
	pending := world.Pending{}
	events, err := c.world.Amend(func(s *world.Containers, collector *event.Collector) error {
		pending = s.Snapshot(int32(sequence), window, int32(slot))
		s.Click(collector, pending, predicted)
		if exact {
			s.CursorChanged(collector, cursorAfter.Item, cursorAfter.Held)
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("record the click: %w", err)
	}
	c.events.publish(events)

	if err := c.Do(ctx, version.ActionClickSlot{
		Window: window, Slot: slot, Button: button, Mode: mode,
		Sequence: sequence, Claim: claim,
	}); err != nil {
		// The click never reached the wire, so nothing will ever answer it.
		// Its prediction rolls back now rather than standing forever.
		rollback, amendErr := c.world.Amend(func(s *world.Containers, collector *event.Collector) error {
			return s.Reject(collector, int32(sequence))
		})
		if amendErr == nil {
			c.events.publish(rollback)
		}

		return err
	}

	return nil
}

// CloseWindow closes an open window and drops what the cursor holds.
//
// Vanilla drops the cursor stack on the floor when a window closes, on both
// versions, and neither announces it — so the client forgets the stack here
// rather than believing in an item that is on the ground.
func (c *Client) CloseWindow(ctx context.Context, window int32) error {
	if err := c.Do(ctx, version.ActionCloseWindow{Window: window}); err != nil {
		return err
	}

	events, err := c.world.Amend(func(s *world.Containers, collector *event.Collector) error {
		// The server does not echo a close the client initiated, so the
		// window — and the pending clicks that die with it — is released
		// here, the same way the closing half of a vanilla client does it.
		s.Closed(collector, window)
		s.CursorChanged(collector, nil, false)

		return nil
	})
	if err != nil {
		return fmt.Errorf("forget the cursor: %w", err)
	}
	c.events.publish(events)

	return nil
}

// predictClick computes the exact outcomes this client will claim to know.
//
// Only a plain left click with exactly one occupied side is exact: the whole
// stack crosses, no arithmetic, no identity question. A click on two occupied
// sides merges or swaps depending on whether the stacks are the same item,
// and "the same item" is a question about a decoded wire value this
// version-neutral code cannot answer; a quick-move's destination depends on
// the window's layout, which M9.7's audit found no trustworthy data for.
// Those return exact=false, and the caller decides per version whether to
// send anyway.
func (c *Client) predictClick(
	view world.ContainerView, snapshot world.ContainersView,
	slot int16, button int8, mode version.ClickMode, described bool,
) (predicted []world.SlotSnapshot, cursorAfter world.SlotSnapshot, exact bool) {
	if mode != version.ClickPickup || button != 0 || !described {
		return nil, world.SlotSnapshot{}, false
	}

	stacks, ok := c.profile.Adapter.(version.Stacks)
	if !ok {
		return nil, world.SlotSnapshot{}, false
	}

	item := view.Slots[int32(slot)]
	slotEmpty := stacks.StackEmpty(item)
	cursorEmpty := !snapshot.CursorHeld || stacks.StackEmpty(snapshot.Cursor)

	switch {
	case cursorEmpty && !slotEmpty:
		// Pick the stack up.
		return []world.SlotSnapshot{{Slot: int32(slot), Held: false, Known: true}},
			world.SlotSnapshot{Item: item, Held: true}, true
	case !cursorEmpty && slotEmpty:
		// Place the whole cursor stack.
		return []world.SlotSnapshot{{Slot: int32(slot), Item: snapshot.Cursor, Held: true, Known: true}},
			world.SlotSnapshot{Held: false}, true
	case cursorEmpty && slotEmpty:
		// A click on nothing changes nothing, exactly.
		return nil, world.SlotSnapshot{Held: false}, true
	default:
		return nil, world.SlotSnapshot{}, false
	}
}
