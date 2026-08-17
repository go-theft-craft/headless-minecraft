package v26_1_test

import (
	"slices"
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

func names(events []event.Event) []event.Name {
	out := make([]event.Name, 0, len(events))
	for _, e := range events {
		out = append(out, e.Name())
	}

	return out
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

func TestFiveMovementPacketsProduceOneEventShape(t *testing.T) {
	t.Parallel()

	// 775 has one more movement packet than 47 and the same one event.
	w, events := script(
		t,
		[]protocol.Packet{
			playLogin(1),
			play(&gen.PlayClientboundSpawnEntity{EntityID: 7, Type: 3, X: 10, Y: 20, Z: 30}),
		},
		[]protocol.Packet{play(&gen.PlayClientboundRelEntityMove{EntityID: 7, DX: 4096})},
		[]protocol.Packet{play(&gen.PlayClientboundEntityLook{EntityID: 7, Yaw: 64})},
		[]protocol.Packet{play(&gen.PlayClientboundEntityMoveLook{EntityID: 7, DY: 4096})},
		[]protocol.Packet{play(&gen.PlayClientboundEntityTeleport{EntityID: 7, X: 1, Y: 2, Z: 3})},
		[]protocol.Packet{play(&gen.PlayClientboundSyncEntityPosition{EntityID: 7, X: 5, Y: 6, Z: 7})},
	)

	var moves int
	for _, e := range events {
		if _, ok := e.(event.EntityMoved); ok {
			moves++
		}
	}
	if moves != 6 {
		t.Errorf("published %d entity.moved events, want one per movement packet", moves)
	}

	entity, ok := w.Snapshot().Entities.Get(7)
	if !ok {
		t.Fatal("the entity is not tracked")
	}
	if entity.X != 5 || entity.Y != 6 || entity.Z != 7 {
		t.Errorf("entity is at %v,%v,%v, want the synced 5,6,7", entity.X, entity.Y, entity.Z)
	}
}

func TestRelativeMovesUseThisProtocolsUnits(t *testing.T) {
	t.Parallel()

	// 4096 sixteenths is one block in 775, where one block was 32 in 47. The
	// snapshot is in blocks either way.
	w, _ := script(
		t,
		[]protocol.Packet{
			playLogin(1),
			play(&gen.PlayClientboundSpawnEntity{EntityID: 7, X: 10}),
		},
		[]protocol.Packet{play(&gen.PlayClientboundRelEntityMove{EntityID: 7, DX: 4096})},
	)

	entity, _ := w.Snapshot().Entities.Get(7)
	if entity.X != 11 {
		t.Errorf("entity x is %v, want one block past 10", entity.X)
	}
}

func TestMetadataKeepsTheTypeNameTheServerSent(t *testing.T) {
	t.Parallel()

	// 775 names its metadata types where 47 numbers them, and an index this
	// client has no name for is kept either way.
	w, _ := script(t, []protocol.Packet{
		playLogin(1),
		play(&gen.PlayClientboundEntityMetadata{EntityID: 7, Metadata: []gen.PlayClientboundEntityMetadataMetadataItem{
			{Key: 0, Type: "byte"},
			{Key: 240, Type: "something_a_mod_added"},
		}}),
	})

	entity, _ := w.Snapshot().Entities.Get(7)
	if len(entity.Metadata) != 2 {
		t.Fatalf("entity holds %d metadata entries, want both", len(entity.Metadata))
	}
	// 47 terminates metadata at 0x7F and 775 at 0xFF, so this index cannot
	// exist on 47 and must survive on 775.
	if entity.Metadata[240].Type != "something_a_mod_added" {
		t.Errorf("index 240 has type %q", entity.Metadata[240].Type)
	}
}

func TestPassengersAndVehiclesAreBothRecorded(t *testing.T) {
	t.Parallel()

	w, _ := script(
		t,
		[]protocol.Packet{
			playLogin(1),
			play(&gen.PlayClientboundSetPassengers{EntityID: 7, Passengers: []int32{8, 9}}),
		},
	)

	vehicle, _ := w.Snapshot().Entities.Get(7)
	if len(vehicle.Passengers) != 2 {
		t.Fatalf("vehicle carries %v", vehicle.Passengers)
	}
	rider, ok := w.Snapshot().Entities.Get(8)
	if !ok || rider.Vehicle != 7 {
		t.Errorf("rider 8 reports vehicle %d, want 7", rider.Vehicle)
	}

	// Dismounting clears both directions.
	w, _ = script(
		t,
		[]protocol.Packet{
			playLogin(1),
			play(&gen.PlayClientboundSetPassengers{EntityID: 7, Passengers: []int32{8}}),
		},
		[]protocol.Packet{play(&gen.PlayClientboundSetPassengers{EntityID: 7})},
	)
	rider, _ = w.Snapshot().Entities.Get(8)
	if rider.Vehicle != 0 {
		t.Errorf("a dismounted rider still reports vehicle %d", rider.Vehicle)
	}
}

func TestDamageCarriesItsSourceOn775(t *testing.T) {
	t.Parallel()

	// Protocol 47 reports damage as an entity status with no source; 775 says
	// what did it and who is behind it. The source entity IDs arrive offset by
	// one, so the skeleton below is sent as 43 and the arrow as 100.
	_, events := script(t, []protocol.Packet{
		playLogin(1),
		play(&gen.PlayClientboundDamageEvent{
			EntityID: 7, SourceTypeID: 11, SourceCauseID: 43, SourceDirectID: 100,
		}),
	})

	last := events[len(events)-1].(event.EntityDamaged)
	if last.EntityID != 7 || last.Damage.TypeID != 11 || !last.Damage.Typed {
		t.Errorf("damage is %+v", last)
	}
	if !last.Damage.Attributed || last.Damage.CauseID != 42 {
		t.Errorf("cause is %d attributed %v, want 42 true", last.Damage.CauseID, last.Damage.Attributed)
	}
	if !last.Damage.Direct || last.Damage.DirectID != 99 {
		t.Errorf("direct is %d present %v, want 99 true", last.Damage.DirectID, last.Damage.Direct)
	}
	if last.Damage.Positioned {
		t.Errorf("damage with no position reports one at %v,%v,%v",
			last.Damage.X, last.Damage.Y, last.Damage.Z)
	}
}

func TestUnattributedDamageDoesNotNameEntityZero(t *testing.T) {
	t.Parallel()

	// Zero on the wire means the server named nobody, and entity 0 is a legal
	// entity. Reporting CauseID 0 as attributed would send a retaliating
	// caller after whatever holds that ID.
	_, events := script(t, []protocol.Packet{
		playLogin(1),
		play(&gen.PlayClientboundDamageEvent{
			EntityID: 7, SourceTypeID: 3,
			SourcePosition: &gen.Vec3f64{X: 1, Y: 2, Z: 3},
		}),
	})

	last := events[len(events)-1].(event.EntityDamaged)
	if last.Damage.Attributed || last.Damage.Direct {
		t.Errorf("damage from nobody is attributed: %+v", last.Damage)
	}
	if !last.Damage.Positioned || last.Damage.X != 1 || last.Damage.Z != 3 {
		t.Errorf("source position is %+v, want 1,2,3 present", last.Damage)
	}
}

func TestDamageToTheLocalPlayerIsAPlayerEvent(t *testing.T) {
	t.Parallel()

	// The player domain is the local player and the entity domain is everybody
	// else. Damage naming the local entity must not create a tracked entity
	// for it.
	w, events := script(t, []protocol.Packet{
		playLogin(42),
		play(&gen.PlayClientboundDamageEvent{EntityID: 42, SourceTypeID: 11, SourceCauseID: 8}),
	})

	last := events[len(events)-1]
	damaged, ok := last.(event.PlayerDamaged)
	if !ok {
		t.Fatalf("published %T, want event.PlayerDamaged", last)
	}
	if !damaged.Damage.Attributed || damaged.Damage.CauseID != 7 {
		t.Errorf("cause is %+v, want entity 7", damaged.Damage)
	}
	if _, tracked := w.Snapshot().Entities.Get(42); tracked {
		t.Error("the local player is tracked as an entity")
	}
}

func TestDeathIsPublishedOncePerDeath(t *testing.T) {
	t.Parallel()

	// 775 announces the local player's death twice, as an entity status and as
	// a death combat event, and it names no killer in either.
	w, events := script(t, []protocol.Packet{
		playLogin(42),
		play(&gen.PlayClientboundEntityStatus{EntityID: 42, EntityStatus: 3}),
		play(&gen.PlayClientboundDeathCombatEvent{PlayerID: 42}),
	})

	var deaths []event.PlayerDied
	for _, published := range events {
		if died, ok := published.(event.PlayerDied); ok {
			deaths = append(deaths, died)
		}
	}
	if len(deaths) != 1 {
		t.Fatalf("published %d deaths, want 1", len(deaths))
	}
	if deaths[0].Attributed {
		t.Errorf("775 named a killer it does not send: %+v", deaths[0])
	}
	if !w.Snapshot().Player.Dead {
		t.Error("the player is not dead after dying")
	}
}

func TestRespawnClearsDeath(t *testing.T) {
	t.Parallel()

	w, _ := script(
		t,
		[]protocol.Packet{
			playLogin(42),
			play(&gen.PlayClientboundDeathCombatEvent{PlayerID: 42}),
		},
		[]protocol.Packet{
			play(&gen.PlayClientboundRespawn{
				WorldState: gen.SpawnInfo{Name: "minecraft:overworld", Gamemode: "survival"},
			}),
		},
	)

	if w.Snapshot().Player.Dead {
		t.Error("the player is still dead after respawning")
	}
}

func TestAnEntityDeathKeepsTheCorpseTracked(t *testing.T) {
	t.Parallel()

	// The server destroys a corpse a moment after killing it. Until it does,
	// the entity is tracked and marked dead, which is how a caller that was
	// fighting it learns the fight is over.
	w, events := script(t, []protocol.Packet{
		playLogin(1),
		play(&gen.PlayClientboundSpawnEntity{EntityID: 7, Type: 3}),
		play(&gen.PlayClientboundEntityStatus{EntityID: 7, EntityStatus: 3}),
	})

	last := events[len(events)-1]
	died, ok := last.(event.EntityDied)
	if !ok {
		t.Fatalf("published %T, want event.EntityDied", last)
	}
	if died.EntityID != 7 || died.Attributed {
		t.Errorf("death is %+v, want entity 7 unattributed", died)
	}
	corpse, tracked := w.Snapshot().Entities.Get(7)
	if !tracked || !corpse.Dead {
		t.Errorf("corpse is tracked %v dead %v, want both", tracked, corpse.Dead)
	}
}

func TestSnapshotEntitiesDoNotAliasTheStore(t *testing.T) {
	t.Parallel()

	w, _ := script(t, []protocol.Packet{
		playLogin(1),
		play(&gen.PlayClientboundEntityMetadata{EntityID: 7, Metadata: []gen.PlayClientboundEntityMetadataMetadataItem{
			{Key: 1, Type: "byte"},
		}}),
	})

	snapshot := w.Snapshot()
	delete(snapshot.Entities.Tracked, 7)
	entity := snapshot.Entities.Tracked[7]
	_ = entity

	if _, ok := w.Snapshot().Entities.Get(7); !ok {
		t.Fatal("deleting from a snapshot reached the world")
	}
}

func TestAChunkIsTrackedEvenThoughItsSectionsAreNotDecoded(t *testing.T) {
	t.Parallel()

	// This client cannot read a 775 section yet, and that must cost only
	// block lookups: loading, unloading, and everything addressed by
	// position still works, and the bytes stay reachable.
	w, _ := script(t, []protocol.Packet{
		playLogin(1),
		play(&gen.PlayClientboundMapChunk{X: 2, Z: 3, ChunkData: []byte{1, 2, 3, 4}}),
	})

	chunk, ok := w.Snapshot().Chunks.Get(world.ChunkPos{X: 2, Z: 3})
	if !ok {
		t.Fatal("the chunk was not tracked")
	}
	if got := len(chunk.Sections[0].Raw()); got != 4 {
		t.Errorf("the section kept %d bytes, want the 4 that arrived", got)
	}
	if _, ok := w.Snapshot().Chunks.Block(32, 0, 48); ok {
		t.Error("an undecoded 775 section reported a block")
	}
	if _, err := w.SnapshotErr(); err != nil {
		t.Errorf("an undecodable section poisoned the world: %v", err)
	}
}

func TestUnloadChunkReleasesIt(t *testing.T) {
	t.Parallel()

	w, _ := script(
		t,
		[]protocol.Packet{
			playLogin(1),
			play(&gen.PlayClientboundMapChunk{X: 2, Z: 3, ChunkData: []byte{1}}),
		},
		[]protocol.Packet{play(&gen.PlayClientboundUnloadChunk{ChunkX: 2, ChunkZ: 3})},
	)

	if _, ok := w.Snapshot().Chunks.Get(world.ChunkPos{X: 2, Z: 3}); ok {
		t.Error("an unloaded chunk is still tracked")
	}
}

func TestABlockChangeIsRecordedEvenWhereBlocksCannotBeRead(t *testing.T) {
	t.Parallel()

	// The change lands in a section this client cannot decode, so it is
	// counted rather than applied, and the session survives.
	_, events := script(
		t,
		[]protocol.Packet{
			playLogin(1),
			play(&gen.PlayClientboundMapChunk{X: 0, Z: 0, ChunkData: []byte{1}}),
		},
		[]protocol.Packet{
			play(&gen.PlayClientboundBlockChange{Location: gen.Position{X: 1, Y: 4, Z: 1}, Type: 9}),
		},
	)

	last := events[len(events)-1].(event.WorldBlocksChanged)
	if last.Dropped != 1 {
		t.Errorf("dropped count is %d, want the change counted", last.Dropped)
	}
}

func TestGameStateChangeReachesTwoDomainsOn775(t *testing.T) {
	t.Parallel()

	// The same boundary protocol 47 asserts from the other side, and the same
	// two numbers meaning the opposite thing: here start_raining is reason 1,
	// where on 47 reason 1 ends rain. The generated mapper gives this side
	// names, which is what makes the reversal safe to read.
	_, modeEvents := script(t, []protocol.Packet{
		playLogin(1),
		play(&gen.PlayClientboundGameStateChange{Reason: "change_game_mode", GameMode: 1}),
	})
	if got := names(modeEvents); !slices.Contains(got, event.NamePlayerGameModeChanged) ||
		slices.Contains(got, event.NameWorldWeatherChanged) {
		t.Errorf("a game-mode change published %v", got)
	}

	w, rainEvents := script(t, []protocol.Packet{
		playLogin(1),
		play(&gen.PlayClientboundGameStateChange{Reason: "start_raining"}),
		play(&gen.PlayClientboundGameStateChange{Reason: "thunder_level_change", GameMode: 0.75}),
	})
	if got := names(rainEvents); !slices.Contains(got, event.NameWorldWeatherChanged) ||
		slices.Contains(got, event.NamePlayerGameModeChanged) {
		t.Errorf("a weather change published %v", got)
	}
	environment := w.Snapshot().Environment
	if !environment.Raining || environment.ThunderLevel != 0.75 {
		t.Errorf("weather is %+v, want raining with thunder 0.75", environment)
	}
}

func TestSixBorderPacketsProduceOneEventShapeOn775(t *testing.T) {
	t.Parallel()

	// 775 splits into six packets what 47 discriminates by action, and both
	// reduce to the same border and the same event.
	w, events := script(
		t,
		[]protocol.Packet{
			playLogin(1),
			play(&gen.PlayClientboundInitializeWorldBorder{
				X: 8, Z: 9, OldDiameter: 100, NewDiameter: 100,
				PortalTeleportBoundary: 29999984, WarningTime: 15, WarningBlocks: 5,
			}),
		},
		[]protocol.Packet{play(&gen.PlayClientboundWorldBorderCenter{X: 1, Z: 2})},
	)

	changes := 0
	for _, published := range events {
		if _, ok := published.(event.WorldBorderChanged); ok {
			changes++
		}
	}
	if changes != 2 {
		t.Errorf("published %d border changes, want 2", changes)
	}

	border := w.Snapshot().Environment.Border
	if !border.Known || border.X != 1 || border.Z != 2 {
		t.Errorf("border centre is %+v, want 1,2", border)
	}
	if border.NewDiameter != 100 || border.WarningTime != 15 || border.WarningBlocks != 5 {
		t.Errorf("moving the centre cleared the rest of the border: %+v", border)
	}
}

func TestOneClockSuppliesTheTimeAndSeveralDoNot(t *testing.T) {
	t.Parallel()

	// 26.1 replaced protocol 47's single time-of-day number with a set of
	// clocks. One clock is unambiguous; picking one out of several would be a
	// guess, so the snapshot keeps them all and reports the time as unknown.
	w, _ := script(t, []protocol.Packet{
		playLogin(1),
		play(&gen.PlayClientboundUpdateTime{
			Age: 120,
			ClockUpdates: []gen.PlayClientboundUpdateTimeClockUpdatesItem{
				{ID: 0, TotalTicks: 6000, Rate: 1},
			},
		}),
	})

	environment := w.Snapshot().Environment
	if environment.Age != 120 || !environment.TimeOfDayKnown || environment.TimeOfDay != 6000 {
		t.Errorf("one clock gave %+v, want age 120 time 6000", environment)
	}

	w, _ = script(t, []protocol.Packet{
		playLogin(1),
		play(&gen.PlayClientboundUpdateTime{
			Age: 120,
			ClockUpdates: []gen.PlayClientboundUpdateTimeClockUpdatesItem{
				{ID: 0, TotalTicks: 6000}, {ID: 1, TotalTicks: 9000},
			},
		}),
	})

	environment = w.Snapshot().Environment
	if environment.TimeOfDayKnown {
		t.Errorf("two clocks produced a time of day: %+v", environment)
	}
	if len(environment.Clocks) != 2 {
		t.Errorf("clocks are %+v, want both kept", environment.Clocks)
	}
}

func TestSimulationSettingsAndGameRulesShareTheirEvent(t *testing.T) {
	t.Parallel()

	// The taxonomy declares no game-rule name and a game rule is a simulation
	// setting, so they ride one event, which names the rules that changed.
	w, events := script(t, []protocol.Packet{
		playLogin(1),
		play(&gen.PlayClientboundUpdateViewDistance{ViewDistance: 12}),
		play(&gen.PlayClientboundSimulationDistance{Distance: 10}),
		play(&gen.PlayClientboundUpdateViewPosition{ChunkX: 3, ChunkZ: -4}),
		play(&gen.PlayClientboundSetTickingState{TickRate: 20, IsFrozen: true}),
		play(&gen.PlayClientboundGameRuleValues{
			Values: []gen.PlayClientboundGameRuleValuesValuesItem{
				{Name: "doDaylightCycle", Value: "false"},
				{Name: "modded:someRuleThisClientHasNeverHeardOf", Value: "7"},
			},
		}),
	})

	environment := w.Snapshot().Environment
	if !environment.SimulationKnown {
		t.Fatal("775 sent simulation settings and the snapshot reports them unknown")
	}
	if environment.ViewDistance != 12 || environment.SimulationDistance != 10 {
		t.Errorf("distances are %+v", environment)
	}
	if environment.ViewChunkX != 3 || environment.ViewChunkZ != -4 {
		t.Errorf("view centre is %d,%d, want 3,-4", environment.ViewChunkX, environment.ViewChunkZ)
	}
	if environment.TickRate != 20 || !environment.Frozen {
		t.Errorf("ticking state is %+v", environment)
	}
	// An unknown namespaced rule is kept as sent: that is the whole point on a
	// modded server.
	if environment.GameRules["modded:someRuleThisClientHasNeverHeardOf"] != "7" {
		t.Errorf("game rules are %+v", environment.GameRules)
	}

	last := events[len(events)-1].(event.WorldSimulationSettingsChanged)
	if len(last.RuleKeys) != 2 || last.RuleKeys[0] != "doDaylightCycle" {
		t.Errorf("rule keys are %v, want both, sorted", last.RuleKeys)
	}
}

func TestAnExplosionWithNoKnockbackSaysSo(t *testing.T) {
	t.Parallel()

	// 775 makes the knockback optional where 47 always sends one, and neither
	// protocol's explosion carries a usable block list any more.
	_, events := script(t, []protocol.Packet{
		playLogin(1),
		play(&gen.PlayClientboundExplosion{
			Center: gen.Vec3f64{X: 1, Y: 2, Z: 3}, Radius: 4, BlockCount: 9,
		}),
	})

	last := events[len(events)-1].(event.WorldExplosionOccurred)
	if last.X != 1 || last.Z != 3 || last.Radius != 4 {
		t.Errorf("explosion is %+v", last)
	}
	if last.Knocked {
		t.Errorf("an explosion with no knockback reported one: %+v", last)
	}
}

func TestProtocol775CarriesAStateIDAnd47DoesNot(t *testing.T) {
	t.Parallel()

	// Zero is a valid state ID, so the snapshot exposes it as optional rather
	// than defaulting. The protocol 47 half asserts the other side of this.
	w, _ := script(t, []protocol.Packet{
		playLogin(1),
		play(&gen.PlayClientboundOpenWindow{WindowID: 3, InventoryType: 2}),
		play(&gen.PlayClientboundSetSlot{WindowID: 3, StateID: 0, Slot: 1}),
	})

	menu, ok := w.Snapshot().Containers.Get(3)
	if !ok {
		t.Fatal("the menu was not opened")
	}
	if !menu.StateKnown || menu.StateID != 0 {
		t.Errorf("state is %d known %v, want 0 known", menu.StateID, menu.StateKnown)
	}
	// The menu type is a number here and a name on 47; both are kept as the
	// server sent them rather than mapped to a vanilla name.
	if menu.MenuType != "java/26.1:menu/2" {
		t.Errorf("menu type is %q", menu.MenuType)
	}
	if menu.Title != "" {
		t.Errorf("775 rendered a chat component into a title: %q", menu.Title)
	}
}

func TestWindowItemsCarriesTheCursorOn775(t *testing.T) {
	t.Parallel()

	// One packet, two facts: the menu's slots and the stack on the cursor,
	// which belongs to no menu.
	w, _ := script(t, []protocol.Packet{
		playLogin(1),
		play(&gen.PlayClientboundWindowItems{
			WindowID: 1, StateID: 7,
			Items:       []*gen.Slot{nil, {}},
			CarriedItem: &gen.Slot{},
		}),
	})

	containers := w.Snapshot().Containers
	if !containers.CursorKnown || !containers.CursorHeld {
		t.Errorf("cursor is %+v, want held", containers)
	}
	menu, _ := containers.Get(1)
	if len(menu.Slots) != 2 || menu.StateID != 7 {
		t.Errorf("menu is %+v", menu)
	}
}

func TestTheDedicatedCursorPacketEmptiesTheCursor(t *testing.T) {
	t.Parallel()

	w, events := script(
		t,
		[]protocol.Packet{
			playLogin(1),
			play(&gen.PlayClientboundSetCursorItem{Contents: &gen.Slot{}}),
		},
		[]protocol.Packet{play(&gen.PlayClientboundSetCursorItem{})},
	)

	last := events[len(events)-1].(event.ContainerCursorChanged)
	if !last.Empty {
		t.Errorf("an absent stack did not empty the cursor: %+v", last)
	}
	containers := w.Snapshot().Containers
	if !containers.CursorKnown || containers.CursorHeld || containers.Cursor != nil {
		t.Errorf("cursor is %+v, want known and empty", containers)
	}
}

func TestThePlayerInventoryIsMenuZero(t *testing.T) {
	t.Parallel()

	// 775 addresses the player's own inventory directly, with no packet ever
	// opening it, so menu 0 exists on first use rather than waiting for an
	// OpenWindow that never comes.
	w, _ := script(t, []protocol.Packet{
		playLogin(1),
		play(&gen.PlayClientboundSetPlayerInventory{SlotID: 36, Contents: &gen.Slot{}}),
	})

	menu, ok := w.Snapshot().Containers.Get(0)
	if !ok {
		t.Fatal("the player inventory is not a menu")
	}
	if _, filled := menu.Slots[36]; !filled {
		t.Errorf("slot 36 is unset: %+v", menu.Slots)
	}
}

func TestTheRecipeBookIsKnownOnlyOn775(t *testing.T) {
	t.Parallel()

	w, _ := script(
		t,
		[]protocol.Packet{
			playLogin(1),
			play(&gen.PlayClientboundRecipeBookAdd{
				Entries: []gen.PlayClientboundRecipeBookAddEntriesItem{
					{Recipe: gen.PlayClientboundRecipeBookAddEntriesItemRecipe{DisplayID: 4}},
					{Recipe: gen.PlayClientboundRecipeBookAddEntriesItemRecipe{DisplayID: 9}},
				},
			}),
		},
		[]protocol.Packet{play(&gen.PlayClientboundRecipeBookRemove{RecipeIds: []int32{4}})},
	)

	containers := w.Snapshot().Containers
	if !containers.RecipesKnown {
		t.Fatal("775 sent a recipe book and the snapshot reports none")
	}
	if containers.Recipes[4] || !containers.Recipes[9] {
		t.Errorf("recipes are %+v, want 9 only", containers.Recipes)
	}
}

func TestTradesRecordTheirScalars(t *testing.T) {
	t.Parallel()

	// The trades themselves are wire structures M9 models. What the world
	// records is that they arrived, how many, and the villager's own numbers.
	_, events := script(t, []protocol.Packet{
		playLogin(1),
		play(&gen.PlayClientboundTradeList{
			WindowID: 2, VillagerLevel: 3, Experience: 40, CanRestock: true,
			Trades: make([]gen.PlayClientboundTradeListTradesItem, 5),
		}),
	})

	last := events[len(events)-1].(event.ContainerTradesChanged)
	if last.ContainerID != 2 || last.Count != 5 || last.VillagerLevel != 3 || !last.CanRestock {
		t.Errorf("trades are %+v", last)
	}
}
