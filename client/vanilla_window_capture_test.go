//go:build vanilla

// M9.7 Task 1's capture half: open a window of every block-openable type
// against the pinned 26.1.2 server and record what the server actually sent —
// the menu type identifier, the slot count, and the property count. The
// offline audit in internal/conformance reads the committed recording, so it
// runs without a jar; this file is what makes the recording, and re-running it
// without -write-windows checks the committed one still says what the server
// says.
package client_test

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/go-theft-craft/headless-minecraft/client"
	"github.com/go-theft-craft/headless-minecraft/event"
	"github.com/go-theft-craft/headless-minecraft/internal/vanilla"
	"github.com/go-theft-craft/headless-minecraft/version"
)

// writeWindows makes the capture write its recording, the same deliberate-act
// flag shape the fixture generators use.
var writeWindows = flag.Bool("write-windows", false,
	"record the observed 26.1.2 windows instead of checking the committed recording")

// windowRecordingPath is where the offline audit reads the recording from.
const windowRecordingPath = "../internal/conformance/testdata/windows/26_1_2.json"

// observedWindow is one window as the server sent it.
type observedWindow struct {
	// Block is the block that was used to open it.
	Block string `json:"block"`
	// Menu is the server's menu identifier as the adapter renders it: the
	// numeric index into the game's own menu registry, because the wire
	// carries a number and no session registry names it.
	Menu string `json:"menu"`
	// Slots is how many slots the server's full-window set carried, window
	// and player inventory together — that is the packet's own framing.
	Slots int `json:"slots"`
	// Properties is how many distinct properties the server sent unprompted.
	Properties int `json:"properties"`
}

// windowRecording is the committed shape.
type windowRecording struct {
	Version   string           `json:"version"`
	Source    string           `json:"source"`
	Unchecked []string         `json:"unchecked"`
	Windows   []observedWindow `json:"windows"`
}

// windowBlocks is every window this capture can open by placing a block and
// using it, in a fixed order so two captures compare.
var windowBlocks = []string{
	"minecraft:chest",
	"minecraft:barrel",
	"minecraft:crafting_table",
	"minecraft:furnace",
	"minecraft:blast_furnace",
	"minecraft:smoker",
	"minecraft:hopper",
	"minecraft:dispenser",
	"minecraft:dropper",
	"minecraft:enchanting_table",
	"minecraft:brewing_stand",
	"minecraft:anvil",
	"minecraft:grindstone",
	"minecraft:stonecutter",
	"minecraft:cartography_table",
	"minecraft:loom",
	"minecraft:smithing_table",
	"minecraft:shulker_box",
	"minecraft:crafter",
	"minecraft:beacon",
}

// windowsUnchecked is what this capture deliberately does not open, and why.
// Silent truncation would read as "every window checked" when it is not.
var windowsUnchecked = []string{
	"generic_9x1, 9x2, 9x4, 9x5: no vanilla block opens them; they are plugin territory",
	"generic_9x6: a double chest; forming one needs two directed chest halves and adds nothing the 9x3 case does not show",
	"lectern: needs a placed book to open at all",
	"merchant: needs a villager",
	"the horse window: needs a tamed horse, and travels on its own packet",
}

func TestVanilla26WindowCapture(t *testing.T) {
	lane := combatLane26()
	server := lane.start(t)
	bot := connectForCombat(t, combatLane26(), server)

	containers, err := bot.Subscribe(event.DomainContainers, 8192)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	player := bot.World().Player
	cell := version.BlockPos{
		X: int32(player.X) + 2, Y: int32(player.Y), Z: int32(player.Z),
	}

	recording := windowRecording{
		Version: "26.1.2",
		Source: "observed from a pinned vanilla 26.1.2 server on 2026-08-18 " +
			"through the headless client and its protocol 775 adapter",
		Unchecked: windowsUnchecked,
	}

	for _, block := range windowBlocks {
		recording.Windows = append(recording.Windows,
			openAndRecord(t, server, bot, containers, cell, block))
	}

	if *writeWindows {
		content, err := json.MarshalIndent(recording, "", "  ")
		if err != nil {
			t.Fatalf("encode: %v", err)
		}
		if err := os.WriteFile(windowRecordingPath, append(content, '\n'), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
		t.Logf("wrote %s: %d windows", windowRecordingPath, len(recording.Windows))

		return
	}

	committed, err := os.ReadFile(windowRecordingPath)
	if err != nil {
		t.Fatalf("%v (pass -write-windows to record it)", err)
	}
	var stored windowRecording
	if err := json.Unmarshal(committed, &stored); err != nil {
		t.Fatalf("decode the committed recording: %v", err)
	}
	if len(stored.Windows) != len(recording.Windows) {
		t.Fatalf("the committed recording holds %d windows and the server sent %d",
			len(stored.Windows), len(recording.Windows))
	}
	for at, want := range recording.Windows {
		if got := stored.Windows[at]; got != want {
			t.Errorf("%s: committed %+v, the server says %+v", want.Block, got, want)
		}
	}
}

// openAndRecord places one block, opens it, and records what the server sent.
func openAndRecord(
	t *testing.T, server *vanilla.Server, bot *client.Client,
	containers *client.Subscription, cell version.BlockPos, block string,
) observedWindow {
	t.Helper()

	if err := server.Console(fmt.Sprintf("setblock %d %d %d %s", cell.X, cell.Y, cell.Z, block)); err != nil {
		t.Fatalf("setblock %s: %v", block, err)
	}
	time.Sleep(300 * time.Millisecond)

	if err := bot.Place(t.Context(), cell, version.FaceTop); err != nil {
		t.Fatalf("open %s: %v", block, err)
	}

	// The open packet, then the full-window set. Properties arrive unprompted
	// between and after; the settle window is what bounds "unprompted".
	var opened event.ContainerOpened
	deadline := time.After(20 * time.Second)
	for found := false; !found; {
		select {
		case one, ok := <-containers.C():
			if !ok {
				t.Fatalf("the container subscription closed: %v", containers.Err())
			}
			if value, is := one.(event.ContainerOpened); is {
				opened, found = value, true
			}
		case <-deadline:
			t.Fatalf("%s never opened a window\nserver log:\n%s", block, tail(server.Log(), 15))
		}
	}

	time.Sleep(1500 * time.Millisecond)
	view, ok := bot.World().Containers.Open[opened.ContainerID]
	if !ok {
		t.Fatalf("%s: window %d opened and was never tracked", block, opened.ContainerID)
	}

	if err := bot.Do(t.Context(), version.ActionCloseWindow{Window: opened.ContainerID}); err != nil {
		t.Fatalf("close %s: %v", block, err)
	}
	if err := server.Console(fmt.Sprintf("setblock %d %d %d minecraft:air", cell.X, cell.Y, cell.Z)); err != nil {
		t.Fatalf("clear %s: %v", block, err)
	}
	time.Sleep(300 * time.Millisecond)

	return observedWindow{
		Block:      block,
		Menu:       view.MenuType,
		Slots:      len(view.Slots),
		Properties: len(view.Properties),
	}
}
