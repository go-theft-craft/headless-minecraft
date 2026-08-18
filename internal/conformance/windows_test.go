package conformance_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/go-theft-craft/minecraft-protocol/data"
	v26_1 "github.com/go-theft-craft/minecraft-protocol/generated/java/v26_1"
)

// observedWindow mirrors what client's TestVanilla26WindowCapture records.
type observedWindow struct {
	Block      string `json:"block"`
	Menu       string `json:"menu"`
	Slots      int    `json:"slots"`
	Properties int    `json:"properties"`
}

type windowRecording struct {
	Version   string           `json:"version"`
	Source    string           `json:"source"`
	Unchecked []string         `json:"unchecked"`
	Windows   []observedWindow `json:"windows"`
}

// loadObservedWindows reads the committed 26.1.2 recording.
func loadObservedWindows(t *testing.T) windowRecording {
	t.Helper()

	content, err := os.ReadFile("testdata/windows/26_1_2.json")
	if err != nil {
		t.Fatalf("read the recording: %v (client's TestVanilla26WindowCapture "+
			"-write-windows records it)", err)
	}
	var recording windowRecording
	if err := json.Unmarshal(content, &recording); err != nil {
		t.Fatalf("decode the recording: %v", err)
	}
	if len(recording.Windows) == 0 {
		t.Fatal("the recording holds no windows; an audit over nothing proves nothing")
	}

	return recording
}

// TestTheWindowRegistryCannotNameWhatTheServerSent is M9.7 Task 1's answer,
// pinned: the 26.1 window dataset is unusable for this version.
//
// The generated file says it is an alias — upstream publishes no windows for
// Java 26.1 and the pinned tree resolves to Java 1.16.1 — and the audit found
// the drift is not a matter of degree. The wire does not carry window names at
// all: protocol 775's open packet numbers the menu into the game's built-in
// menu registry, the session defines no such registry, and the pinned tree
// carries no data that could resolve the number. The aliased records are keyed
// by 1.8-era identifiers ("minecraft:chest", "EntityHorse") that no packet
// ever mentions, name none of the menus the modern server actually opens
// (blast_furnace, smoker, grindstone, stonecutter, loom, smithing,
// shulker_box, crafter, cartography_table, the generic_NxM family), and are
// wrong where a human can pair them by hand — see the property pins below.
//
// So M9.7's 26.1.2 lane rests on what the server sends at runtime — the
// full-window set is the slot count, and the observed recording is what makes
// that verifiable — and its gate is weaker than the 1.8.9 lane's. The fix is
// a data correction in minecraft-protocol: a menu registry for 26.1, keyed in
// registry order. The day it lands, the lookups below start answering and
// this test fails, which is the signal to rebuild the lane on data.
func TestTheWindowRegistryCannotNameWhatTheServerSent(t *testing.T) {
	t.Parallel()

	recording := loadObservedWindows(t)
	for _, unchecked := range recording.Unchecked {
		t.Log("not captured: " + unchecked)
	}

	set, err := v26_1.Data()
	if err != nil {
		t.Fatalf("load the 26.1 data set: %v", err)
	}
	registry := set.Windows()

	for _, window := range recording.Windows {
		if !strings.HasPrefix(window.Menu, "java/26.1:menu/") {
			t.Errorf("%s: the adapter rendered %q; the wire is expected to carry "+
				"a bare menu registry index", window.Block, window.Menu)
		}
		if declared, ok := registry.ByName(window.Menu); ok {
			t.Errorf("%s: the registry resolved %q to %+v; the dataset has grown "+
				"real 26.1 menu data and this lane should be rebuilt on it",
				window.Block, window.Menu, declared)
		}
	}
}

// TestTheAliasedRecordsDisagreeWhereAHumanCanPairThem pins the drift on the
// two windows whose aliased record a person can match to an observed one by
// reading both. The assertion is the disagreement: the day the dataset is
// corrected these start agreeing, the test fails, and the pin comes off.
func TestTheAliasedRecordsDisagreeWhereAHumanCanPairThem(t *testing.T) {
	t.Parallel()

	recording := loadObservedWindows(t)
	observed := make(map[string]observedWindow, len(recording.Windows))
	for _, window := range recording.Windows {
		observed[window.Block] = window
	}

	set, err := v26_1.Data()
	if err != nil {
		t.Fatalf("load the 26.1 data set: %v", err)
	}
	registry := set.Windows()

	for _, pin := range []struct {
		block   string
		aliased string
		reason  string
	}{
		{
			"minecraft:enchanting_table", "minecraft:enchanting_table",
			"the server sends 10 enchantment properties — three costs, a seed, " +
				"three enchantment ids, three levels — and the aliased record " +
				"names 7",
		},
		{
			"minecraft:brewing_stand", "minecraft:brewing_stand",
			"the server sends 2 brewing properties — brew time and fuel — and " +
				"the aliased record names 1; the fuel bar arrived in 1.9",
		},
	} {
		window, ok := observed[pin.block]
		if !ok {
			t.Fatalf("the recording does not cover %s", pin.block)
		}
		declared, ok := registry.ByID(data.WindowID(pin.aliased))
		if !ok {
			t.Fatalf("the aliased registry lost %s; rewrite this pin", pin.aliased)
		}
		if len(declared.Properties) == window.Properties {
			t.Errorf("%s: the aliased record now agrees with the server (%d "+
				"properties); the dataset was corrected and this pin should "+
				"come off (%s)", pin.block, window.Properties, pin.reason)
		}
	}
}
