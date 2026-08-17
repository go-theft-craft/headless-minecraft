package main

import (
	"strings"
	"testing"

	"github.com/go-theft-craft/headless-minecraft/event"
	"github.com/go-theft-craft/headless-minecraft/world"
)

func TestEveryDeclaredEventRendersALine(t *testing.T) {
	t.Parallel()

	// A line is printed for every event, described or not. An event this
	// example has no description for must still say its name and revision
	// rather than printing an empty line.
	for _, e := range []event.Event{
		event.PlayerSpawned{EntityID: 42, Dimension: "minecraft:overworld"},
		event.WorldLightChanged{},
		event.ChatTabCompleted{},
	} {
		rendered := line(e)
		if !strings.Contains(rendered, string(e.Name())) {
			t.Errorf("line for %s is %q", e.Name(), rendered)
		}
	}
}

func TestUnattributedDamageIsPrintedAsUnattributed(t *testing.T) {
	t.Parallel()

	// Protocol 47 sends no damage source of any kind. Printing "from entity 0"
	// would name whatever happens to hold that ID.
	if got := describe(event.PlayerDamaged{}); got != "unattributed" {
		t.Errorf("protocol 47 damage prints as %q", got)
	}

	got := describe(event.PlayerDamaged{Damage: event.Damage{
		TypeID: 11, Typed: true,
		CauseID: 42, Attributed: true,
		DirectID: 99, Direct: true,
	}})
	for _, want := range []string{"type 11", "from entity 42", "dealt by 99"} {
		if !strings.Contains(got, want) {
			t.Errorf("775 damage prints as %q, missing %q", got, want)
		}
	}
}

func TestAnUnrenderedMessageSaysSoRatherThanPrintingNothing(t *testing.T) {
	t.Parallel()

	// Both protocols send most chat as a structured component and the library
	// does not render one. A blank line would read as an empty message.
	got := describe(event.ChatReceived{Kind: event.ChatKindSystem})
	if !strings.Contains(got, "not rendered") {
		t.Errorf("an unrendered message prints as %q", got)
	}

	if got := describe(event.ChatReceived{
		Kind: event.ChatKindPlayer, Text: "hello",
	}); !strings.Contains(got, "hello") {
		t.Errorf("a plain message prints as %q", got)
	}
}

func TestTheSummaryCountsEveryDomain(t *testing.T) {
	t.Parallel()

	// The run's last line is what it ended up holding, not what went past.
	summary := summarize(world.New().Snapshot())
	for _, want := range []string{"revision", "entities", "chunks", "menus", "registries", "messages"} {
		if !strings.Contains(summary, want) {
			t.Errorf("summary %q does not mention %q", summary, want)
		}
	}
}
