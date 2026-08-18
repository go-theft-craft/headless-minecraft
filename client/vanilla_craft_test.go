//go:build vanilla

// M9.8's live scenarios: a craft through a real server on both versions, in
// the 3x3 table and the 2x2 player grid, and the shift-craft that drains.
//
// The matcher can be right and the click sequence wrong: crafting is clicks
// into a grid and a click on the result slot, and the result click is what
// the server validates. On 1.8.9 that validation is the whole gate — the
// server never sends the result slot and answers an accepted click with
// nothing, so the client's claim IS its matcher's answer, and a wrong match
// comes back as a rejection that rolls the prediction back. The cursor is the
// discriminator: it holds the result only if the prediction survived.
//
// Absent here, with reasons rather than silence: the 1.8.9 shift-craft — the
// client refuses a quick-move it cannot predict, because the results'
// inventory destination is window-layout arithmetic the audit left without a
// trustworthy source — and recipe-book gating, which 26.1.2 tracks and
// nothing here acts on; a craft refused for a locked recipe would look like a
// matcher defect and is not one.
package client_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/go-theft-craft/headless-minecraft/client"
	"github.com/go-theft-craft/headless-minecraft/internal/vanilla"
	"github.com/go-theft-craft/headless-minecraft/version"
)

// craftLane is one version's crafting setup: the window lane plus the item
// vocabulary and the give command the versions spell differently.
type craftLane struct {
	windowLane
	// replace renders the console command that puts count of an item into a
	// hotbar slot.
	replace func(hotbar int, item string, count int) string
	// planks names the plank item, which the flattening renamed.
	planks string
}

func craftLane1_8() craftLane {
	return craftLane{
		windowLane: windowLane1_8(),
		replace: func(hotbar int, item string, count int) string {
			return fmt.Sprintf("replaceitem entity %s slot.hotbar.%d %s %d 0",
				username, hotbar, item, count)
		},
		planks: "minecraft:planks",
	}
}

func craftLane26() craftLane {
	return craftLane{
		windowLane: windowLane26(),
		replace: func(hotbar int, item string, count int) string {
			return fmt.Sprintf("item replace entity %s hotbar.%d with %s %d",
				username, hotbar, item, count)
		},
		planks: "minecraft:oak_planks",
	}
}

// stageCraftingTable places a crafting table beside the player and opens it.
func stageCraftingTable(
	t *testing.T, lane craftLane, server *vanilla.Server, bot *client.Client,
) int32 {
	t.Helper()

	player := bot.World().Player
	cell := version.BlockPos{X: int32(player.X) + 2, Y: int32(player.Y), Z: int32(player.Z)}
	block := "minecraft:crafting_table"
	if err := server.Console(fmt.Sprintf("setblock %d %d %d %s", cell.X, cell.Y, cell.Z, block)); err != nil {
		t.Fatalf("setblock: %v", err)
	}
	time.Sleep(300 * time.Millisecond)

	return openChest(t, server, bot, cell)
}

// stock puts count of an item into a hotbar slot and waits until the open
// window shows it at the given window slot.
func stock(
	t *testing.T, lane craftLane, server *vanilla.Server, bot *client.Client,
	window int32, windowSlot int32, hotbar int, item string, count int,
) {
	t.Helper()

	if err := server.Console(lane.replace(hotbar, item, count)); err != nil {
		t.Fatalf("replaceitem: %v", err)
	}

	interpret := stacks(t, lane.windowLane)
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if stack, ok := bot.World().Containers.Open[window].Slots[windowSlot]; ok &&
			!interpret.StackEmpty(stack) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}

	t.Fatalf("the stocked %s never reached window slot %d\nserver log:\n%s",
		item, windowSlot, tail(server.Log(), 15))
}

// move clicks a stack from one window slot into another: two predictable
// pickups.
func move(t *testing.T, bot *client.Client, server *vanilla.Server, window int32, from, to int16) {
	t.Helper()

	if err := bot.Click(t.Context(), window, from, 0, version.ClickPickup); err != nil {
		t.Fatalf("pick up from slot %d: %v", from, err)
	}
	if err := bot.Click(t.Context(), window, to, 0, version.ClickPickup); err != nil {
		t.Fatalf("place into slot %d: %v", to, err)
	}
	awaitSettled(t, server, bot)
}

func TestVanillaCraftingThroughARealServerLandsTheResult(t *testing.T) {
	lane := craftLane1_8()
	craftScenario(t, lane, lane.start(t))
}

func TestVanilla26CraftingThroughARealServerLandsTheResult(t *testing.T) {
	lane := craftLane26()
	craftScenario(t, lane, lane.start(t))
}

// craftScenario lays a torch pattern into a real crafting table and clicks
// the result.
func craftScenario(t *testing.T, lane craftLane, server *vanilla.Server) {
	bot := connectForCombat(t, lane.combatLane, server)
	interpret := stacks(t, lane.windowLane)

	window := stageCraftingTable(t, lane, server, bot)

	// The crafting table's layout on both versions: result 0, grid 1-9,
	// hotbar 37-45. Coal above stick, middle column.
	stock(t, lane, server, bot, window, 37, 0, "minecraft:coal", 1)
	stock(t, lane, server, bot, window, 38, 1, "minecraft:stick", 1)
	move(t, bot, server, window, 37, 2)
	move(t, bot, server, window, 38, 5)

	if err := bot.Click(t.Context(), window, 0, 0, version.ClickPickup); err != nil {
		t.Fatalf("click the result: %v", err)
	}
	awaitSettled(t, server, bot)

	// The cursor is the discriminator. On 1.8.9 a wrong claim — a wrong
	// match — is rejected and rolled back, which leaves the cursor empty; on
	// 26.1.2 the resend is the server's own answer. Either way, torches on
	// the cursor mean the server agreed.
	containers := bot.World().Containers
	if !containers.CursorHeld || interpret.StackEmpty(containers.Cursor) {
		t.Fatalf("the cursor holds nothing after the craft; the server did not "+
			"agree with the matcher\nserver log:\n%s", tail(server.Log(), 15))
	}
	for _, at := range []int32{2, 5} {
		if item, ok := containers.Open[window].Slots[at]; ok && !interpret.StackEmpty(item) {
			t.Fatalf("grid slot %d still holds %v after the craft", at, item)
		}
	}
}

func TestVanillaThe2x2PlayerGridCraftsToo(t *testing.T) {
	lane := craftLane1_8()
	playerGridScenario(t, lane, lane.start(t))
}

func TestVanilla26The2x2PlayerGridCraftsToo(t *testing.T) {
	lane := craftLane26()
	playerGridScenario(t, lane, lane.start(t))
}

// playerGridScenario crafts sticks from two planks in the always-open player
// window, whose layout differs from the table's: result 0, grid 1-4, hotbar
// 36-44.
func playerGridScenario(t *testing.T, lane craftLane, server *vanilla.Server) {
	bot := connectForCombat(t, lane.combatLane, server)
	interpret := stacks(t, lane.windowLane)

	stock(t, lane, server, bot, 0, 36, 0, lane.planks, 1)
	stock(t, lane, server, bot, 0, 37, 1, lane.planks, 1)
	move(t, bot, server, 0, 36, 1)
	move(t, bot, server, 0, 37, 3)

	if err := bot.Click(t.Context(), 0, 0, 0, version.ClickPickup); err != nil {
		t.Fatalf("click the result: %v", err)
	}
	awaitSettled(t, server, bot)

	containers := bot.World().Containers
	if !containers.CursorHeld || interpret.StackEmpty(containers.Cursor) {
		t.Fatalf("the 2x2 player grid produced nothing\nserver log:\n%s",
			tail(server.Log(), 15))
	}
}

func TestVanilla26ShiftCraftingDrainsTheGrid(t *testing.T) {
	// One version, and the file comment says why: the 1.8.9 client refuses a
	// quick-move on the result, because the results' destination is
	// window-layout arithmetic and that server announces nothing.
	lane := craftLane26()
	server := lane.start(t)
	bot := connectForCombat(t, lane.combatLane, server)
	interpret := stacks(t, lane.windowLane)

	window := stageCraftingTable(t, lane, server, bot)
	stock(t, lane, server, bot, window, 37, 0, "minecraft:coal", 64)
	stock(t, lane, server, bot, window, 38, 1, "minecraft:stick", 64)
	move(t, bot, server, window, 37, 2)
	move(t, bot, server, window, 38, 5)

	if err := bot.Click(t.Context(), window, 0, 0, version.ClickQuickMove); err != nil {
		t.Fatalf("shift-click the result: %v", err)
	}
	awaitSettled(t, server, bot)

	for _, at := range []int32{2, 5} {
		if item, ok := bot.World().Containers.Open[window].Slots[at]; ok &&
			!interpret.StackEmpty(item) {
			t.Fatalf("grid slot %d still holds %v after a shift-craft; the drain "+
				"crafted once", at, item)
		}
	}
}

func TestVanillaAnInvalidGridShowsNoResultAndTheServerAgrees(t *testing.T) {
	// The opposite failure: a matcher that shows a result for a grid vanilla
	// refuses lets a caller click a slot that will be rejected. On 1.8.9 the
	// probe is the claim itself — clicking the result claims what the matcher
	// computed, here nothing, and the server accepts exactly when its own
	// computation was also nothing. A rejection would arrive as a full window
	// resend, and none may.
	lane := craftLane1_8()
	server := lane.start(t)
	bot := connectForCombat(t, lane.combatLane, server)

	window := stageCraftingTable(t, lane, server, bot)
	// Two coal side by side craft nothing.
	stock(t, lane, server, bot, window, 37, 0, "minecraft:coal", 1)
	stock(t, lane, server, bot, window, 38, 1, "minecraft:coal", 1)
	move(t, bot, server, window, 37, 2)
	move(t, bot, server, window, 38, 3)

	if err := bot.Click(t.Context(), window, 0, 0, version.ClickPickup); err != nil {
		t.Fatalf("click the empty result: %v", err)
	}
	awaitSettled(t, server, bot)

	containers := bot.World().Containers
	if containers.CursorHeld {
		t.Fatalf("the cursor holds %v after clicking an empty result", containers.Cursor)
	}
	if got := containers.PendingClicks; got != 0 {
		t.Fatalf("%d clicks pending; the server disagreed about the empty result", got)
	}
}

func TestVanillaMirroringMatchesTheCorpus(t *testing.T) {
	lane := craftLane1_8()
	mirrorScenario(t, lane, lane.start(t))
}

func TestVanilla26MirroringMatchesTheCorpus(t *testing.T) {
	lane := craftLane26()
	mirrorScenario(t, lane, lane.start(t))
}

// mirrorScenario is what makes crafting/testdata/mirror.json a corpus rather
// than an assumption: the horizontally mirrored axe crafts on a real server,
// and the vertically flipped one does not.
//
// On 1.8.9 the discriminator is the accepted claim — the client claims what
// its matcher computed, and the server accepts exactly when it computed the
// same — and on 26.1.2 it is the resend. Both surface as the cursor: held
// after the mirrored grid, empty after the flipped one.
func mirrorScenario(t *testing.T, lane craftLane, server *vanilla.Server) {
	bot := connectForCombat(t, lane.combatLane, server)
	interpret := stacks(t, lane.windowLane)

	// The mirrored axe: planks|planks / stick|planks / stick|empty.
	window := stageCraftingTable(t, lane, server, bot)
	layAxe(t, lane, server, bot, window, [][2]int32{{1, 2}}, [][2]int32{{4, 5}}, 7)

	if err := bot.Click(t.Context(), window, 0, 0, version.ClickPickup); err != nil {
		t.Fatalf("click the result: %v", err)
	}
	awaitSettled(t, server, bot)
	containers := bot.World().Containers
	if !containers.CursorHeld || interpret.StackEmpty(containers.Cursor) {
		t.Fatalf("the mirrored axe did not craft; vanilla mirrors horizontally "+
			"and the matcher must too\nserver log:\n%s", tail(server.Log(), 15))
	}

	// A fresh table for the flipped grid: closing drops the crafted axe from
	// the cursor and returns nothing to a grid the craft emptied.
	if err := bot.CloseWindow(t.Context(), window); err != nil {
		t.Fatalf("CloseWindow: %v", err)
	}
	time.Sleep(500 * time.Millisecond)

	// The flipped axe: empty|stick / planks|stick / planks|planks.
	window = openChest(t, server, bot, craftingTableCell(t, bot))
	layFlippedAxe(t, lane, server, bot, window)

	if err := bot.Click(t.Context(), window, 0, 0, version.ClickPickup); err != nil {
		t.Fatalf("click the empty result: %v", err)
	}
	awaitSettled(t, server, bot)
	containers = bot.World().Containers
	if containers.CursorHeld && !interpret.StackEmpty(containers.Cursor) {
		t.Fatalf("the vertically flipped axe crafted %v; neither jar flips "+
			"patterns vertically", containers.Cursor)
	}
}

// craftingTableCell is where stageCraftingTable put the table.
func craftingTableCell(t *testing.T, bot *client.Client) version.BlockPos {
	t.Helper()

	player := bot.World().Player

	return version.BlockPos{X: int32(player.X) + 2, Y: int32(player.Y), Z: int32(player.Z)}
}

// layAxe stocks and places the mirrored axe: planks into the plank slots,
// sticks into the stick slots.
func layAxe(
	t *testing.T, lane craftLane, server *vanilla.Server, bot *client.Client,
	window int32, plankRows [][2]int32, mixedRows [][2]int32, lastStick int32,
) {
	t.Helper()

	hotbar := 0
	place := func(item string, grid int32) {
		windowSlot := int32(37 + hotbar)
		stock(t, lane, server, bot, window, windowSlot, hotbar, item, 1)
		move(t, bot, server, window, int16(windowSlot), int16(grid))
		hotbar++
	}

	for _, row := range plankRows {
		place(lane.planks, row[0])
		place(lane.planks, row[1])
	}
	for _, row := range mixedRows {
		place("minecraft:stick", row[0])
		place(lane.planks, row[1])
	}
	place("minecraft:stick", lastStick)
}

// layFlippedAxe places the vertically flipped pattern.
func layFlippedAxe(
	t *testing.T, lane craftLane, server *vanilla.Server, bot *client.Client, window int32,
) {
	t.Helper()

	hotbar := 0
	place := func(item string, grid int32) {
		windowSlot := int32(37 + hotbar)
		stock(t, lane, server, bot, window, windowSlot, hotbar, item, 1)
		move(t, bot, server, window, int16(windowSlot), int16(grid))
		hotbar++
	}

	// .s. / ps. / pp. in grid slots: 2; 4,5; 7,8.
	place("minecraft:stick", 2)
	place(lane.planks, 4)
	place("minecraft:stick", 5)
	place(lane.planks, 7)
	place(lane.planks, 8)
}
