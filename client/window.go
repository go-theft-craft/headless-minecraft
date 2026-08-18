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

	plan := c.planClick(view, snapshot, window, slot, button, mode)
	if !plan.exact && c.profile.ID == protocol47 {
		return fmt.Errorf("click %s slot %d: %w", mode, slot, ErrUnpredictable)
	}

	sequence := int16(c.clicks.Add(1))
	pending := world.Pending{}
	events, err := c.world.Amend(func(s *world.Containers, collector *event.Collector) error {
		pending = s.Snapshot(int32(sequence), window, plan.snapshotSlots...)
		s.Click(collector, pending, plan.predicted)
		if plan.exact {
			s.CursorChanged(collector, plan.cursor.Item, plan.cursor.Held)
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("record the click: %w", err)
	}
	c.events.publish(events)

	if err := c.Do(ctx, version.ActionClickSlot{
		Window: window, Slot: slot, Button: button, Mode: mode,
		Sequence: sequence, Claim: plan.claim,
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

// clickPlan is everything one click commits to before it is sent: the claim,
// the prediction, and the slots whose prior contents the rollback keeps.
type clickPlan struct {
	claim         any
	predicted     []world.SlotSnapshot
	cursor        world.SlotSnapshot
	snapshotSlots []int32
	exact         bool
}

// planClick computes the exact outcomes this client will claim to know.
//
// Two paths are exact. A plain left click with exactly one occupied side —
// the whole stack crosses, no arithmetic, no identity question. And a plain
// left click on the result slot of a crafting menu, when the adapter can
// predict the craft: protocol 47's server never sends the result slot, so the
// prediction is the only truth a client there will ever have, and the claim
// it carries is what the server compares its own computation against.
//
// Everything else — merges, swaps, quick-moves, drags — returns exact=false,
// and the caller decides per version whether to send anyway: 775 answers
// every click by resending the truth, 47 answers an accepted click with
// nothing.
func (c *Client) planClick(
	view world.ContainerView, snapshot world.ContainersView,
	window int32, slot int16, button int8, mode version.ClickMode,
) clickPlan {
	claim := view.Slots[int32(slot)]
	inexact := clickPlan{claim: claim, snapshotSlots: []int32{int32(slot)}}

	if mode != version.ClickPickup || button != 0 {
		return inexact
	}
	stacks, ok := c.profile.Adapter.(version.Stacks)
	if !ok {
		return inexact
	}
	cursorEmpty := !snapshot.CursorHeld || stacks.StackEmpty(snapshot.Cursor)

	if crafter, crafts := c.profile.Adapter.(version.Crafter); crafts && cursorEmpty {
		if layout, isCraft := crafter.CraftingLayout(view.MenuType, window); isCraft &&
			layout.Result == int32(slot) {
			return planResultClick(crafter, layout, view)
		}
	}

	item := claim
	slotEmpty := stacks.StackEmpty(item)

	switch {
	case cursorEmpty && !slotEmpty:
		// Pick the stack up.
		return clickPlan{
			claim:         claim,
			predicted:     []world.SlotSnapshot{{Slot: int32(slot), Held: false, Known: true}},
			cursor:        world.SlotSnapshot{Item: item, Held: true},
			snapshotSlots: []int32{int32(slot)},
			exact:         true,
		}
	case !cursorEmpty && slotEmpty:
		// Place the whole cursor stack.
		return clickPlan{
			claim: claim,
			predicted: []world.SlotSnapshot{
				{Slot: int32(slot), Item: snapshot.Cursor, Held: true, Known: true},
			},
			cursor:        world.SlotSnapshot{Held: false},
			snapshotSlots: []int32{int32(slot)},
			exact:         true,
		}
	case cursorEmpty && slotEmpty:
		// A click on nothing changes nothing, exactly.
		return clickPlan{claim: claim, snapshotSlots: []int32{int32(slot)}, exact: true}
	default:
		return inexact
	}
}

// planResultClick predicts a pickup from a crafting menu's result slot: the
// craft the grid produces, the grid one item lighter, and the next result the
// lighter grid produces — because the game refills the slot while the grid
// still matches.
func planResultClick(
	crafter version.Crafter, layout version.CraftingLayout, view world.ContainerView,
) clickPlan {
	grid := make([]any, 0, len(layout.Grid))
	for _, at := range layout.Grid {
		grid = append(grid, view.Slots[at])
	}

	snapshotSlots := append([]int32{layout.Result}, layout.Grid...)
	result, remaining, ok := crafter.PredictCraft(grid)
	if !ok {
		// A grid that crafts nothing has an empty result slot, and clicking
		// it changes nothing — exactly. The claim is the emptiness itself.
		return clickPlan{snapshotSlots: snapshotSlots, exact: true}
	}

	predicted := make([]world.SlotSnapshot, 0, len(layout.Grid)+1)
	for at, cell := range remaining {
		predicted = append(predicted, world.SlotSnapshot{
			Slot: layout.Grid[at], Item: cell, Held: cell != nil, Known: true,
		})
	}
	next, _, refills := crafter.PredictCraft(remaining)
	predicted = append(predicted, world.SlotSnapshot{
		Slot: layout.Result, Item: next, Held: refills, Known: true,
	})

	return clickPlan{
		// The server compares the claim against what its own craft computed,
		// and what it computed is the result — not whatever stale value the
		// slot showed.
		claim:         result,
		predicted:     predicted,
		cursor:        world.SlotSnapshot{Item: result, Held: true},
		snapshotSlots: snapshotSlots,
		exact:         true,
	}
}
