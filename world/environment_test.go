package world_test

import (
	"strconv"
	"testing"

	"github.com/go-theft-craft/headless-minecraft/event"
	"github.com/go-theft-craft/headless-minecraft/world"
)

func TestGameRulesAreBoundedAndCounted(t *testing.T) {
	t.Parallel()

	// Every store filled by the peer has a bound, because a modded server
	// defines its own rules and a session that runs for a week must not grow
	// without limit. What the bound refuses is counted rather than dropped
	// silently.
	w := world.New()

	var c event.Collector
	rules := make(map[string]string, 300)
	for i := range 300 {
		rules["rule/"+strconv.Itoa(i)] = "true"
	}
	w.Environment().GameRulesChanged(&c, rules)

	environment := w.Snapshot().Environment
	if len(environment.GameRules) != 256 {
		t.Errorf("kept %d rules, want the bound of 256", len(environment.GameRules))
	}
	if environment.DroppedGameRules != 44 {
		t.Errorf("dropped counter is %d, want 44", environment.DroppedGameRules)
	}
}

func TestAnEnvironmentSnapshotDoesNotAliasTheStore(t *testing.T) {
	t.Parallel()

	w := world.New()
	var c event.Collector
	w.Environment().GameRulesChanged(&c, map[string]string{"doFireTick": "false"})
	w.Environment().TimeChanged(&c, 5, 0, false, []event.Clock{{ID: 0, TotalTicks: 100}})

	snapshot := w.Snapshot()
	delete(snapshot.Environment.GameRules, "doFireTick")
	snapshot.Environment.Clocks[0].TotalTicks = 999

	again := w.Snapshot()
	if again.Environment.GameRules["doFireTick"] != "false" {
		t.Error("deleting from a snapshot's game rules reached the store")
	}
	if again.Environment.Clocks[0].TotalTicks != 100 {
		t.Error("writing to a snapshot's clocks reached the store")
	}
}
