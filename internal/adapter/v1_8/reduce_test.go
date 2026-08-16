package v1_8_test

import (
	"testing"

	protocol "github.com/go-theft-craft/minecraft-protocol"
	gen "github.com/go-theft-craft/minecraft-protocol/generated/java/v1_8"

	"github.com/go-theft-craft/headless-minecraft/event"
	adapter "github.com/go-theft-craft/headless-minecraft/internal/adapter/v1_8"
	"github.com/go-theft-craft/headless-minecraft/version"
	"github.com/go-theft-craft/headless-minecraft/world"
)

// script drives a world through batches of protocol 47 packets, one batch per
// argument, and returns the events every batch produced.
//
// A packet script is how every reducer is tested: real generated values, in
// wire order, through the same Apply the client calls.
func script(t *testing.T, batches ...[]protocol.Packet) (*world.World, []event.Event) {
	t.Helper()

	w := world.New()
	for _, reducer := range adapter.Reducers(w) {
		if err := w.Register(reducer); err != nil {
			t.Fatalf("Register: %v", err)
		}
	}

	var published []event.Event
	for _, packets := range batches {
		var c event.Collector
		revision, err := w.Apply(version.Batch{
			Packets: packets, Bundled: len(packets) > 1, State: gen.StatePlay,
		}, &c)
		if err != nil {
			t.Fatalf("Apply: %v", err)
		}
		published = append(published, c.Events(revision)...)
	}

	return w, published
}

func play(value any) protocol.Packet {
	return protocol.Packet{State: gen.StatePlay, Direction: protocol.DirectionClientbound, Value: value}
}

func login(entityID int32) protocol.Packet {
	return play(&gen.PlayClientboundLogin{EntityID: entityID, GameMode: 1, Dimension: 0})
}

func names(events []event.Event) []event.Name {
	out := make([]event.Name, 0, len(events))
	for _, e := range events {
		out = append(out, e.Name())
	}

	return out
}

func TestLoginNamesTheLocalPlayer(t *testing.T) {
	t.Parallel()

	w, events := script(t, []protocol.Packet{login(42)})

	player := w.Snapshot().Player
	if !player.Known || player.EntityID != 42 {
		t.Fatalf("player is %+v, want entity 42 known", player)
	}
	if player.GameMode != 1 {
		t.Errorf("game mode is %d, want 1", player.GameMode)
	}
	if player.Dimension != "minecraft:overworld" {
		t.Errorf("dimension is %q, want minecraft:overworld", player.Dimension)
	}
	if got := names(events); len(got) != 1 || got[0] != event.NamePlayerSpawned {
		t.Errorf("published %v, want one player.spawned", got)
	}
}

func TestPacketsInOneBatchApplyInWireOrder(t *testing.T) {
	t.Parallel()

	// Two held-slot packets in one batch leave the second one's slot, and
	// both are published, in order.
	_, events := script(t, []protocol.Packet{
		login(1),
		play(&gen.PlayClientboundHeldItemSlot{Slot: 3}),
		play(&gen.PlayClientboundHeldItemSlot{Slot: 7}),
	})

	want := []event.Name{
		event.NamePlayerSpawned,
		event.NamePlayerHeldSlotChanged,
		event.NamePlayerHeldSlotChanged,
	}
	got := names(events)
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("published %v, want %v", got, want)
		}
	}
	if slot := events[2].(event.PlayerHeldSlotChanged).Slot; slot != 7 {
		t.Errorf("the last event carries slot %d, want 7", slot)
	}
}

func TestTheSnapshotDoesNotAliasTheReducer(t *testing.T) {
	t.Parallel()

	w, _ := script(t, []protocol.Packet{
		login(1),
		play(&gen.PlayClientboundEntityEffect{EntityID: 1, EffectID: 8, Duration: 100}),
	})

	snapshot := w.Snapshot()
	// A caller holding a snapshot must not see later batches, and must not be
	// able to reach into the world by writing to what it was handed.
	snapshot.Player.Effects[99] = world.Effect{ID: 99}

	if _, ok := w.Snapshot().Player.Effects[99]; ok {
		t.Fatal("writing to a snapshot's map reached the world")
	}
}

func TestRelativePositionFlagsResolveAgainstThePrevious(t *testing.T) {
	t.Parallel()

	// The readiness rule refuses a relative spawn because it has no previous
	// position. Here there is one, and this is where the arithmetic belongs.
	cases := map[string]struct {
		flags   int8
		want    [3]float64
		wantYaw float32
	}{
		"absolute":   {0x00, [3]float64{5, 5, 5}, 5},
		"relative x": {0x01, [3]float64{15, 5, 5}, 5},
		"relative y": {0x02, [3]float64{5, 25, 5}, 5},
		"relative z": {0x04, [3]float64{5, 5, 35}, 5},
		"relative yaw": {
			0x08, [3]float64{5, 5, 5}, 45,
		},
		"all relative": {0x1F, [3]float64{15, 25, 35}, 45},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			w, _ := script(
				t,
				[]protocol.Packet{
					login(1),
					play(&gen.PlayClientboundPosition{X: 10, Y: 20, Z: 30, Yaw: 40, Pitch: 50}),
				},
				[]protocol.Packet{
					play(&gen.PlayClientboundPosition{X: 5, Y: 5, Z: 5, Yaw: 5, Pitch: 5, Flags: tc.flags}),
				},
			)

			player := w.Snapshot().Player
			got := [3]float64{player.X, player.Y, player.Z}
			if got != tc.want {
				t.Errorf("position is %v, want %v", got, tc.want)
			}
			if player.Yaw != tc.wantYaw {
				t.Errorf("yaw is %v, want %v", player.Yaw, tc.wantYaw)
			}
		})
	}
}

func TestARelativeMoveIsReportedAsOne(t *testing.T) {
	t.Parallel()

	_, events := script(
		t,
		[]protocol.Packet{login(1), play(&gen.PlayClientboundPosition{X: 1})},
		[]protocol.Packet{play(&gen.PlayClientboundPosition{X: 1, Flags: 0x01})},
	)

	last := events[len(events)-1].(event.PlayerMoved)
	if !last.Relative {
		t.Error("a relative move is not reported as one")
	}
	if last.X != 2 {
		t.Errorf("the event carries x %v, want the resolved 2", last.X)
	}
}

func TestGameStateChangeCarriesSeveralUnrelatedMeanings(t *testing.T) {
	t.Parallel()

	// One packet type, discriminated by a reason byte. Only the game-mode
	// reason belongs to the player domain.
	_, events := script(t, []protocol.Packet{
		login(1),
		play(&gen.PlayClientboundGameStateChange{Reason: 3, GameMode: 2}),
		play(&gen.PlayClientboundGameStateChange{Reason: 2}),
	})

	var modes int
	for _, e := range events {
		if changed, ok := e.(event.PlayerGameModeChanged); ok {
			modes++
			if changed.GameMode != 2 {
				t.Errorf("game mode is %d, want 2", changed.GameMode)
			}
		}
	}
	if modes != 1 {
		t.Errorf("a rain-start reason produced %d game-mode events, want 1 from the mode change only", modes)
	}
}

func TestAnEffectForAnotherEntityIsNotThePlayers(t *testing.T) {
	t.Parallel()

	// The boundary in both directions: the local player's ID updates the
	// player domain, and any other ID does not.
	w, _ := script(t, []protocol.Packet{
		login(42),
		play(&gen.PlayClientboundEntityEffect{EntityID: 42, EffectID: 1, Amplifier: 2, Duration: 200}),
		play(&gen.PlayClientboundEntityEffect{EntityID: 7, EffectID: 9, Duration: 100}),
	})

	effects := w.Snapshot().Player.Effects
	if len(effects) != 1 {
		t.Fatalf("the player holds %d effects, want only its own", len(effects))
	}
	if effect, ok := effects[1]; !ok || effect.Amplifier != 2 || effect.Duration != 200 {
		t.Errorf("the player's effect is %+v", effects)
	}
}

func TestRemovingAnEffectReleasesIt(t *testing.T) {
	t.Parallel()

	w, events := script(
		t,
		[]protocol.Packet{
			login(42),
			play(&gen.PlayClientboundEntityEffect{EntityID: 42, EffectID: 1, Duration: 200}),
		},
		[]protocol.Packet{
			play(&gen.PlayClientboundRemoveEntityEffect{EntityID: 42, EffectID: 1}),
		},
	)

	if got := len(w.Snapshot().Player.Effects); got != 0 {
		t.Errorf("the player holds %d effects after removal, want 0", got)
	}

	last := events[len(events)-1].(event.PlayerEffectsChanged)
	if !last.Removed {
		t.Error("the removal is not reported as one")
	}
}

func TestRespawnResetsPlacementAndEffects(t *testing.T) {
	t.Parallel()

	w, _ := script(
		t,
		[]protocol.Packet{
			login(42),
			play(&gen.PlayClientboundPosition{X: 1, Y: 2, Z: 3}),
			play(&gen.PlayClientboundEntityEffect{EntityID: 42, EffectID: 1, Duration: 200}),
		},
		[]protocol.Packet{
			play(&gen.PlayClientboundRespawn{Dimension: -1, Gamemode: 3}),
		},
	)

	player := w.Snapshot().Player
	if player.Placed {
		t.Error("a respawned player is still reported as placed")
	}
	if player.Dimension != "minecraft:the_nether" || player.GameMode != 3 {
		t.Errorf("respawn left the player in %q mode %d", player.Dimension, player.GameMode)
	}
	if len(player.Effects) != 0 {
		t.Error("effects survived a respawn")
	}
}

func TestAPacketBeforeLoginIsNotAnError(t *testing.T) {
	t.Parallel()

	// An effect for an entity nobody has claimed yet is normal traffic, not
	// a broken invariant.
	w, _ := script(t, []protocol.Packet{
		play(&gen.PlayClientboundEntityEffect{EntityID: 42, EffectID: 1}),
	})

	if _, err := w.SnapshotErr(); err != nil {
		t.Fatalf("an effect before login poisoned the world: %v", err)
	}
	if got := len(w.Snapshot().Player.Effects); got != 0 {
		t.Errorf("the player holds %d effects with no login, want 0", got)
	}
}

func TestHealthExperienceAndAbilitiesAreRecorded(t *testing.T) {
	t.Parallel()

	w, _ := script(t, []protocol.Packet{
		login(1),
		play(&gen.PlayClientboundUpdateHealth{Health: 17.5, Food: 12, FoodSaturation: 3.5}),
		play(&gen.PlayClientboundExperience{ExperienceBar: 0.25, Level: 4, TotalExperience: 90}),
		play(&gen.PlayClientboundAbilities{Flags: 0x04, FlyingSpeed: 0.1, WalkingSpeed: 0.2}),
	})

	player := w.Snapshot().Player
	switch {
	case player.Health != 17.5 || player.Food != 12 || player.Saturation != 3.5:
		t.Errorf("health is %+v", player)
	case player.ExperienceBar != 0.25 || player.Level != 4 || player.TotalExperience != 90:
		t.Errorf("experience is %+v", player)
	case player.AbilityFlags != 0x04 || player.FlyingSpeed != 0.1 || player.WalkingSpeed != 0.2:
		t.Errorf("abilities are %+v", player)
	}
}
