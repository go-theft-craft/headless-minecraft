package v26_1_test

import (
	"testing"

	protocol "github.com/go-theft-craft/minecraft-protocol"
	gen "github.com/go-theft-craft/minecraft-protocol/generated/java/v26_1"

	"github.com/go-theft-craft/headless-minecraft/event"
	adapter "github.com/go-theft-craft/headless-minecraft/internal/adapter/v26_1"
	"github.com/go-theft-craft/headless-minecraft/version"
	"github.com/go-theft-craft/headless-minecraft/world"
)

// script drives a world through batches of protocol 775 packets. The protocol
// 47 half of this domain has the same test with the same assertions, which is
// how the two versions are held to one snapshot shape.
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

func playLogin(entityID int32) protocol.Packet {
	return play(&gen.PlayClientboundLogin{
		EntityID:   entityID,
		WorldState: gen.SpawnInfo{Name: "minecraft:overworld", Gamemode: "creative"},
	})
}

func TestLoginNamesTheLocalPlayer(t *testing.T) {
	t.Parallel()

	w, events := script(t, []protocol.Packet{playLogin(42)})

	player := w.Snapshot().Player
	if !player.Known || player.EntityID != 42 {
		t.Fatalf("player is %+v, want entity 42 known", player)
	}
	// The same snapshot shape protocol 47 produces: a named dimension and a
	// numbered game mode, from a protocol that sends both as strings.
	if player.Dimension != "minecraft:overworld" || player.GameMode != 1 {
		t.Errorf("player is in %q mode %d, want overworld mode 1", player.Dimension, player.GameMode)
	}
	if len(events) != 1 || events[0].Name() != event.NamePlayerSpawned {
		t.Errorf("published %v, want one player.spawned", events)
	}
}

func TestRelativePositionResolvesAgainstThePrevious(t *testing.T) {
	t.Parallel()

	// 775 says which components are relative in a struct rather than a flag
	// byte, and the world resolves both the same way.
	w, _ := script(
		t,
		[]protocol.Packet{
			playLogin(1),
			play(&gen.PlayClientboundPosition{X: 10, Y: 20, Z: 30, Yaw: 40}),
		},
		[]protocol.Packet{
			play(&gen.PlayClientboundPosition{
				X: 5, Y: 5, Z: 5, Yaw: 5,
				Flags: gen.PlayClientboundPositionFlagsFlags{X: true, Yaw: true},
			}),
		},
	)

	player := w.Snapshot().Player
	if player.X != 15 || player.Y != 5 || player.Z != 5 {
		t.Errorf("position is %v,%v,%v, want 15,5,5", player.X, player.Y, player.Z)
	}
	if player.Yaw != 45 {
		t.Errorf("yaw is %v, want 45", player.Yaw)
	}
}

func TestARotationWithNoPositionKeepsThePosition(t *testing.T) {
	t.Parallel()

	// Protocol 47 has no packet for this; 775 does, and it must not move the
	// player to the origin on its way past.
	w, _ := script(
		t,
		[]protocol.Packet{
			playLogin(1),
			play(&gen.PlayClientboundPosition{X: 10, Y: 20, Z: 30}),
		},
		[]protocol.Packet{
			play(&gen.PlayClientboundPlayerRotation{Yaw: 90, Pitch: 45}),
		},
	)

	player := w.Snapshot().Player
	if player.X != 10 || player.Y != 20 || player.Z != 30 {
		t.Errorf("a rotation moved the player to %v,%v,%v", player.X, player.Y, player.Z)
	}
	if player.Yaw != 90 || player.Pitch != 45 {
		t.Errorf("rotation is %v,%v, want 90,45", player.Yaw, player.Pitch)
	}
}

func TestGameModeArrivesThroughANamedReason(t *testing.T) {
	t.Parallel()

	_, events := script(t, []protocol.Packet{
		playLogin(1),
		play(&gen.PlayClientboundGameStateChange{Reason: "rain_level_change", GameMode: 1}),
		play(&gen.PlayClientboundGameStateChange{Reason: "change_game_mode", GameMode: 3}),
	})

	var modes []uint8
	for _, e := range events {
		if changed, ok := e.(event.PlayerGameModeChanged); ok {
			modes = append(modes, changed.GameMode)
		}
	}
	if len(modes) != 1 || modes[0] != 3 {
		t.Errorf("game-mode events are %v, want one spectator change", modes)
	}
}

func TestCooldownsAreRecordedAndReleased(t *testing.T) {
	t.Parallel()

	// Protocol 47 has no cooldown packet at all, so this domain half exists
	// only on 775.
	w, _ := script(
		t,
		[]protocol.Packet{
			playLogin(1),
			play(&gen.PlayClientboundSetCooldown{CooldownGroup: "minecraft:ender_pearl", CooldownTicks: 20}),
		},
	)
	if got := w.Snapshot().Player.Cooldowns["minecraft:ender_pearl"]; got != 20 {
		t.Fatalf("cooldown is %d ticks, want 20", got)
	}

	w, _ = script(
		t,
		[]protocol.Packet{
			playLogin(1),
			play(&gen.PlayClientboundSetCooldown{CooldownGroup: "g", CooldownTicks: 20}),
		},
		[]protocol.Packet{
			play(&gen.PlayClientboundSetCooldown{CooldownGroup: "g", CooldownTicks: 0}),
		},
	)
	if got := len(w.Snapshot().Player.Cooldowns); got != 0 {
		t.Errorf("a zero-tick cooldown left %d entries, want it released", got)
	}
}

func TestAnEffectForAnotherEntityIsNotThePlayers(t *testing.T) {
	t.Parallel()

	w, _ := script(t, []protocol.Packet{
		playLogin(42),
		play(&gen.PlayClientboundEntityEffect{EntityID: 42, EffectID: 1, Amplifier: 2, Duration: 200}),
		play(&gen.PlayClientboundEntityEffect{EntityID: 7, EffectID: 9, Duration: 100}),
	})

	if got := len(w.Snapshot().Player.Effects); got != 1 {
		t.Errorf("the player holds %d effects, want only its own", got)
	}
}

func TestPacketsInOneBundleApplyInWireOrder(t *testing.T) {
	t.Parallel()

	// A bundle is where wire order matters most: every packet in it takes
	// effect at one revision, so the last one wins and all of them publish.
	w, events := script(t, []protocol.Packet{
		playLogin(1),
		play(&gen.PlayClientboundHeldItemSlot{Slot: 2}),
		play(&gen.PlayClientboundHeldItemSlot{Slot: 5}),
	})

	if got := w.Snapshot().Player.HeldSlot; got != 5 {
		t.Errorf("held slot is %d, want the last packet's 5", got)
	}
	if got := w.Snapshot().Revision; got != 1 {
		t.Errorf("a three-packet bundle produced revision %d, want 1", got)
	}
	for _, e := range events {
		if e.Revision() != 1 {
			t.Errorf("%s carries revision %d, want 1", e.Name(), e.Revision())
		}
	}
}
