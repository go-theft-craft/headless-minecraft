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

func TestAnUnsentSpawnIsUnknownRatherThanTheOrigin(t *testing.T) {
	t.Parallel()

	// The origin is a legal spawn, so a caller cannot tell a server that said
	// nothing from one that named 0,0,0 unless the boolean says so. An orbit
	// centred on an unsent spawn would circle the origin and look like it was
	// working.
	w := world.New()

	if spawn := w.Snapshot().Environment; spawn.SpawnKnown {
		t.Errorf("spawn is known before the server sent one: %+v", spawn.Spawn)
	}

	var c event.Collector
	w.Environment().SpawnChanged(&c, world.BlockPos{}, "", 0, 0, false)

	if spawn := w.Snapshot().Environment; !spawn.SpawnKnown {
		t.Error("a spawn at the origin did not register as known")
	}
}

func TestTheLatestSpawnWins(t *testing.T) {
	t.Parallel()

	// A vanilla server re-sends this packet when the player's respawn point
	// moves. Keeping the first would report a landmark the server has since
	// abandoned.
	w := world.New()

	var c event.Collector
	w.Environment().SpawnChanged(&c, world.BlockPos{X: 8, Y: 64, Z: 8}, "", 0, 0, false)
	w.Environment().SpawnChanged(
		&c,
		world.BlockPos{X: 120, Y: 70, Z: -40},
		"minecraft:overworld",
		90, 0, true,
	)

	spawn := w.Snapshot().Environment
	if want := (world.BlockPos{X: 120, Y: 70, Z: -40}); spawn.Spawn != want {
		t.Errorf("spawn is %+v, want the second one at %+v", spawn.Spawn, want)
	}
	if spawn.SpawnDimension != "minecraft:overworld" {
		t.Errorf("dimension is %q, want minecraft:overworld", spawn.SpawnDimension)
	}
	if !spawn.SpawnAngled || spawn.SpawnYaw != 90 {
		t.Errorf("angle is %v at yaw %v, want an angled 90", spawn.SpawnAngled, spawn.SpawnYaw)
	}
}

func TestASpawnChangeIsPublished(t *testing.T) {
	t.Parallel()

	w := world.New()

	var c event.Collector
	w.Environment().SpawnChanged(&c, world.BlockPos{X: 1, Y: 2, Z: 3}, "", 0, 0, false)

	events := c.Events(1)
	if len(events) != 1 {
		t.Fatalf("collected %d events, want 1", len(events))
	}

	spawn, ok := events[0].(event.WorldSpawnChanged)
	if !ok {
		t.Fatalf("collected %T, want event.WorldSpawnChanged", events[0])
	}
	if want := (event.BlockPosition{X: 1, Y: 2, Z: 3}); spawn.Position != want {
		t.Errorf("published %+v, want %+v", spawn.Position, want)
	}
	// Protocol 47 sends no angle, and the event must not imply one.
	if spawn.Angled {
		t.Error("an unangled spawn published as angled")
	}
}
