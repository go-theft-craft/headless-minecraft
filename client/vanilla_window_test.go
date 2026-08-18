//go:build vanilla

// M9.7's live scenarios: a click that is confirmed through each version's own
// mechanism, a shift-click that drains, and a closed window dropping the
// cursor — with the server's own state as the referee.
//
// The referee differs per version because the protocols do. On 26.1.2 the
// server answers every click by resending the window, so the client's state
// after a click IS the server's. On 1.8.9 an accepted click is answered with
// nothing but a verdict, so the scenario closes and reopens the chest — the
// reopen makes the server restate the window — and requires the prediction to
// equal the restatement.
//
// What is deliberately not here, and why:
//   - A provoked rejection. The claim a click carries comes from the world
//     store, so a client whose store is current cannot send a wrong one
//     through its own API; the rejection and rollback path is exercised
//     against real packet shapes by the adapter reduce tests instead.
//   - A 1.8.9 shift-click. The client refuses it by design — the quick-move's
//     destination depends on window layout data the audit found no
//     trustworthy source for, and a 1.8.9 server announces nothing after an
//     accepted click — so the mechanic is absent on that lane, with this
//     sentence as its reason, rather than approximated.
//   - Creative-mode slot setting. A different packet with no confirmation on
//     either version; out of scope, and saying so here keeps the absence from
//     reading as coverage.
package client_test

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/go-theft-craft/headless-minecraft/client"
	"github.com/go-theft-craft/headless-minecraft/internal/vanilla"
	"github.com/go-theft-craft/headless-minecraft/version"
	"github.com/go-theft-craft/headless-minecraft/version/java"
)

// windowLane is one version's window setup: the combat lane plus the chest
// commands the versions spell differently.
type windowLane struct {
	combatLane
	// chest renders the setblock command for a chest with 64 stone in slot 0.
	chest func(x, y, z int32) string
}

func windowLane1_8() windowLane {
	return windowLane{
		combatLane: combatLane1_8(),
		chest: func(x, y, z int32) string {
			return fmt.Sprintf(
				`setblock %d %d %d minecraft:chest 0 replace {Items:[{Slot:0b,id:"minecraft:stone",Count:64b}]}`,
				x, y, z,
			)
		},
	}
}

func windowLane26() windowLane {
	return windowLane{
		combatLane: combatLane26(),
		chest: func(x, y, z int32) string {
			return fmt.Sprintf(
				`setblock %d %d %d minecraft:chest{Items:[{Slot:0b,id:"minecraft:stone",count:64}]}`,
				x, y, z,
			)
		},
	}
}

// stageChest places a chest with 64 stone in slot 0 beside the player and
// returns its cell. Staging and opening are separate on purpose: a reopen
// that restaged the chest would compare the client against the stage rather
// than against the state the server kept, and the referee would agree with
// anything.
func stageChest(t *testing.T, lane windowLane, server *vanilla.Server, bot *client.Client) version.BlockPos {
	t.Helper()

	player := bot.World().Player
	cell := version.BlockPos{X: int32(player.X) + 2, Y: int32(player.Y), Z: int32(player.Z)}
	if err := server.Console(lane.chest(cell.X, cell.Y, cell.Z)); err != nil {
		t.Fatalf("setblock: %v", err)
	}
	time.Sleep(300 * time.Millisecond)

	return cell
}

// openChest opens the chest at the cell and returns the window identifier
// once its slots have arrived.
func openChest(t *testing.T, server *vanilla.Server, bot *client.Client, cell version.BlockPos) int32 {
	t.Helper()

	if err := bot.Place(t.Context(), cell, version.FaceTop); err != nil {
		t.Fatalf("open the chest: %v", err)
	}

	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		for id, view := range bot.World().Containers.Open {
			if id != 0 && len(view.Slots) > 0 {
				return id
			}
		}
		time.Sleep(50 * time.Millisecond)
	}

	t.Fatalf("the chest never opened\nserver log:\n%s", tail(server.Log(), 15))

	return 0
}

// awaitSettled waits for every pending click to be answered.
func awaitSettled(t *testing.T, server *vanilla.Server, bot *client.Client) {
	t.Helper()

	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if bot.World().Containers.PendingClicks == 0 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}

	t.Fatalf("%d clicks were never answered\nserver log:\n%s",
		bot.World().Containers.PendingClicks, tail(server.Log(), 15))
}

// stacks resolves the version's own stack interpreter.
func stacks(t *testing.T, lane windowLane) version.Stacks {
	t.Helper()

	profile := java.Current()
	if lane.name == "1.8.9" {
		profile = java.Java1_8()
	}
	interpreter, ok := profile.Adapter.(version.Stacks)
	if !ok {
		t.Fatal("the adapter cannot interpret its own stacks")
	}

	return interpreter
}

func TestVanillaAClickRoundTripsAndAgreesWithTheServer(t *testing.T) {
	lane := windowLane1_8()
	clickRoundTrip(t, lane, lane.start(t))
}

func TestVanilla26AClickRoundTripsAndAgreesWithTheServer(t *testing.T) {
	lane := windowLane26()
	clickRoundTrip(t, lane, lane.start(t))
}

// clickRoundTrip picks the stack up, puts it back, and then makes the server
// restate the window to referee the result.
func clickRoundTrip(t *testing.T, lane windowLane, server *vanilla.Server) {
	bot := connectForCombat(t, lane.combatLane, server)
	interpret := stacks(t, lane)

	cell := stageChest(t, lane, server, bot)
	window := openChest(t, server, bot, cell)

	// Pick the stack up. Confirmation is version-owned: 47 echoes the
	// transaction, 775 resends the window; the caller sees only a click that
	// settled.
	if err := bot.Click(t.Context(), window, 0, 0, version.ClickPickup); err != nil {
		t.Fatalf("Click: %v", err)
	}
	awaitSettled(t, server, bot)

	containers := bot.World().Containers
	if !containers.CursorHeld || interpret.StackEmpty(containers.Cursor) {
		t.Fatal("the cursor holds nothing after a confirmed pickup")
	}
	if item, ok := containers.Open[window].Slots[0]; ok && !interpret.StackEmpty(item) {
		t.Fatalf("slot 0 still holds %v after a confirmed pickup", item)
	}

	// Put it back, and remember what the client believes.
	if err := bot.Click(t.Context(), window, 0, 0, version.ClickPickup); err != nil {
		t.Fatalf("Click back: %v", err)
	}
	awaitSettled(t, server, bot)
	believed := bot.World().Containers.Open[window].Slots[0]
	if interpret.StackEmpty(believed) {
		t.Fatal("slot 0 holds nothing after placing the stack back")
	}

	// The referee: close and reopen, which makes the server restate the
	// window from its own state. A prediction that drifted from the server
	// shows up as a disagreement here — the exact silent desynchronisation
	// the click path exists to prevent.
	if err := bot.CloseWindow(t.Context(), window); err != nil {
		t.Fatalf("CloseWindow: %v", err)
	}
	time.Sleep(500 * time.Millisecond)

	reopened := openChest(t, server, bot, cell)
	restated := bot.World().Containers.Open[reopened].Slots[0]
	if !reflect.DeepEqual(believed, restated) {
		t.Fatalf("the client believed slot 0 held %v and the server restated %v",
			believed, restated)
	}
}

func TestVanilla26ShiftClickDrainsRatherThanActingOnce(t *testing.T) {
	// One version, and the file comment says why: the 1.8.9 client refuses a
	// quick-move rather than desynchronise, so the mechanic is absent there
	// with a recorded reason.
	lane := windowLane26()
	server := lane.start(t)
	bot := connectForCombat(t, lane.combatLane, server)
	interpret := stacks(t, lane)

	window := openChest(t, server, bot, stageChest(t, lane, server, bot))

	if err := bot.Click(t.Context(), window, 0, 0, version.ClickQuickMove); err != nil {
		t.Fatalf("Click: %v", err)
	}
	awaitSettled(t, server, bot)

	// M3's session findings recorded a real defect of exactly this shape: a
	// shift-click that acted once instead of draining. The server's resend is
	// the truth here, so the assertion is against vanilla itself.
	if item, ok := bot.World().Containers.Open[window].Slots[0]; ok && !interpret.StackEmpty(item) {
		t.Fatalf("the source slot holds %v after a shift-click; the whole stack "+
			"moves, not one item", item)
	}
}

func TestVanillaClosingAWindowDropsTheCursorStack(t *testing.T) {
	lane := windowLane1_8()
	server := lane.start(t)
	bot := connectForCombat(t, lane.combatLane, server)
	interpret := stacks(t, lane)

	cell := stageChest(t, lane, server, bot)
	window := openChest(t, server, bot, cell)
	if err := bot.Click(t.Context(), window, 0, 0, version.ClickPickup); err != nil {
		t.Fatalf("Click: %v", err)
	}
	awaitSettled(t, server, bot)

	if err := bot.CloseWindow(t.Context(), window); err != nil {
		t.Fatalf("CloseWindow: %v", err)
	}

	// Vanilla drops the cursor stack on the ground and announces nothing. A
	// client that keeps believing in it believes in an item on the floor.
	containers := bot.World().Containers
	if containers.CursorHeld && !interpret.StackEmpty(containers.Cursor) {
		t.Fatalf("the cursor still holds %v after the window closed", containers.Cursor)
	}

	// And the server agrees the chest is now empty: the stack left with the
	// cursor, not back into the slot.
	reopened := openChest(t, server, bot, cell)
	if item, ok := bot.World().Containers.Open[reopened].Slots[0]; ok && !interpret.StackEmpty(item) {
		t.Fatalf("slot 0 holds %v after the drop; the server disagrees", item)
	}

	// The dropped stack lands as an item entity beside the chest — which is
	// the world's confirmation the drop was real rather than forgotten.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if len(bot.World().Entities.Tracked) > 0 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("no item entity appeared after the cursor stack was dropped\nserver log:\n%s",
		tail(server.Log(), 15))
}
