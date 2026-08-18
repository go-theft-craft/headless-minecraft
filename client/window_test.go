package client

import (
	"context"
	"errors"
	"testing"

	gen1_8 "github.com/go-theft-craft/minecraft-protocol/generated/java/v1_8"

	"github.com/go-theft-craft/headless-minecraft/event"
	"github.com/go-theft-craft/headless-minecraft/version"
	"github.com/go-theft-craft/headless-minecraft/version/java"
	"github.com/go-theft-craft/headless-minecraft/world"
)

// clickWindow is the menu every offline click test stages.
const clickWindow int32 = 3

// stone47 is a stack as protocol 47 decodes one.
func stone47() gen1_8.Slot { return gen1_8.Slot{BlockID: 1} }

// empty47 is an empty slot as protocol 47 decodes one.
func empty47() gen1_8.Slot { return gen1_8.Slot{BlockID: -1} }

// windowClient is an actionClient with an open chest: slot 0 holds a stack,
// slot 1 is empty, the cursor is empty.
func windowClient(t *testing.T, profile version.WireProfile, w sender) *Client {
	t.Helper()

	c := actionClient(t, profile, w)
	c.world = world.New()

	collector := &event.Collector{}
	containers := c.world.Containers()
	containers.Opened(collector, event.ContainerOpened{ContainerID: clickWindow, MenuType: "chest"})
	if profile.ID == protocol47 {
		containers.SlotsChanged(collector, clickWindow,
			map[int32]any{0: stone47(), 1: empty47()}, 0, false)
	} else {
		containers.SlotsChanged(collector, clickWindow,
			map[int32]any{0: "a stack", 1: nil}, 7, true)
	}

	return c
}

func TestAClickRecordsAPendingAndClaimsTheSlot(t *testing.T) {
	t.Parallel()

	sender := &recordingSender{}
	c := windowClient(t, java.Java1_8(), sender)

	if err := c.Click(context.Background(), clickWindow, 0, 0, version.ClickPickup); err != nil {
		t.Fatalf("Click: %v", err)
	}

	if len(sender.sent) != 1 || sender.sent[0].Name != "window_click" {
		t.Fatalf("sent %v, want one window_click", sentNames(sender))
	}
	body := sender.sent[0].Value.(*gen1_8.PlayServerboundWindowClick)
	if body.Item != stone47() {
		t.Fatalf("claimed %+v, want the stack the slot held", body.Item)
	}

	snapshot := c.World().Containers
	if snapshot.PendingClicks != 1 {
		t.Fatalf("PendingClicks = %d after one unanswered click", snapshot.PendingClicks)
	}
	// The predicted pickup: the slot empties and the cursor holds the stack.
	if got := snapshot.Open[clickWindow].Slots[0]; got != nil {
		t.Fatalf("slot 0 holds %v after a predicted pickup", got)
	}
	if !snapshot.CursorHeld {
		t.Fatal("the cursor holds nothing after a predicted pickup")
	}
}

func TestAnUnpredictableClickIsRefusedOn47(t *testing.T) {
	t.Parallel()

	// A 1.8.9 server that accepts a click announces nothing, so a click the
	// client cannot predict is a silent desynchronisation it refuses to
	// start. The quick-move's destination depends on the window's layout,
	// which the audit found no trustworthy data for.
	sender := &recordingSender{}
	c := windowClient(t, java.Java1_8(), sender)

	err := c.Click(context.Background(), clickWindow, 0, 0, version.ClickQuickMove)
	if !errors.Is(err, ErrUnpredictable) {
		t.Fatalf("Click = %v, want ErrUnpredictable", err)
	}
	if len(sender.sent) != 0 {
		t.Fatalf("a refused click still sent %v", sentNames(sender))
	}
	if got := c.World().Containers.PendingClicks; got != 0 {
		t.Fatalf("PendingClicks = %d after a refused click", got)
	}
}

func TestTheSameClickIsSentOn775(t *testing.T) {
	t.Parallel()

	// Protocol 775 answers every click by resending the truth, so a click
	// the client cannot predict costs nothing but the round trip.
	sender := &recordingSender{}
	c := windowClient(t, java.Current(), sender)

	if err := c.Click(context.Background(), clickWindow, 0, 0, version.ClickQuickMove); err != nil {
		t.Fatalf("Click: %v", err)
	}
	if len(sender.sent) != 1 || sender.sent[0].Name != "window_click" {
		t.Fatalf("sent %v, want one window_click", sentNames(sender))
	}
	if got := c.World().Containers.PendingClicks; got != 1 {
		t.Fatalf("PendingClicks = %d after one unanswered click", got)
	}
}

func TestAFailedSendRollsTheClickBack(t *testing.T) {
	t.Parallel()

	// A click that never reached the wire will never be answered, so its
	// prediction must not stand.
	failure := errors.New("connection died")
	c := windowClient(t, java.Java1_8(), failingSender{err: failure})

	if err := c.Click(context.Background(), clickWindow, 0, 0, version.ClickPickup); !errors.Is(err, failure) {
		t.Fatalf("Click = %v, want the write's error", err)
	}

	snapshot := c.World().Containers
	if snapshot.PendingClicks != 0 {
		t.Fatalf("PendingClicks = %d after a failed send", snapshot.PendingClicks)
	}
	if got := snapshot.Open[clickWindow].Slots[0]; got != any(stone47()) {
		t.Fatalf("slot 0 holds %v after the rollback, want the stack", got)
	}
	if snapshot.CursorHeld {
		t.Fatal("the cursor still holds the failed pickup")
	}
}

func TestAClickIntoAnUnknownWindowIsRefused(t *testing.T) {
	t.Parallel()

	sender := &recordingSender{}
	c := windowClient(t, java.Java1_8(), sender)

	if err := c.Click(context.Background(), 9, 0, 0, version.ClickPickup); !errors.Is(err, ErrUnknownContainer) {
		t.Fatalf("Click = %v, want ErrUnknownContainer", err)
	}
	if len(sender.sent) != 0 {
		t.Fatalf("a refused click still sent %v", sentNames(sender))
	}
}

func TestCloseWindowForgetsTheCursor(t *testing.T) {
	t.Parallel()

	// Vanilla drops the cursor stack on the floor when a window closes, and
	// neither version announces it. A client that keeps it believes in an
	// item that is on the ground.
	sender := &recordingSender{}
	c := windowClient(t, java.Java1_8(), sender)

	collector := &event.Collector{}
	c.world.Containers().CursorChanged(collector, stone47(), true)

	if err := c.CloseWindow(context.Background(), clickWindow); err != nil {
		t.Fatalf("CloseWindow: %v", err)
	}
	if len(sender.sent) != 1 || sender.sent[0].Name != "close_window" {
		t.Fatalf("sent %v, want one close_window", sentNames(sender))
	}
	if c.World().Containers.CursorHeld {
		t.Fatal("the cursor still holds a stack the server has dropped")
	}
}
