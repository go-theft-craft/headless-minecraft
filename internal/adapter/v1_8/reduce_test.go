package v1_8_test

import (
	"slices"
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

func TestFourMovementPacketsProduceOneEventShape(t *testing.T) {
	t.Parallel()

	// The taxonomy's motivating case: a subscriber written against
	// entity.moved keeps working whichever packet carried the fact.
	w, events := script(
		t,
		[]protocol.Packet{
			login(1),
			play(&gen.PlayClientboundSpawnEntityLiving{EntityID: 7, Type: 54, X: 320, Y: 640, Z: 960}),
		},
		[]protocol.Packet{play(&gen.PlayClientboundRelEntityMove{EntityID: 7, DX: 32})},
		[]protocol.Packet{play(&gen.PlayClientboundEntityLook{EntityID: 7, Yaw: 64})},
		[]protocol.Packet{play(&gen.PlayClientboundEntityMoveLook{EntityID: 7, DY: 32})},
		[]protocol.Packet{play(&gen.PlayClientboundEntityTeleport{EntityID: 7, X: 640, Y: 640, Z: 960})},
	)

	var moves int
	for _, e := range events {
		if _, ok := e.(event.EntityMoved); ok {
			moves++
		}
	}
	// One per packet, and the move-look packet moves and looks.
	if moves != 5 {
		t.Errorf("published %d entity.moved events, want one per movement packet", moves)
	}

	entity, ok := w.Snapshot().Entities.Get(7)
	if !ok {
		t.Fatal("the entity is not tracked")
	}
	// The teleport is absolute, so it wins over everything relative before it.
	if entity.X != 20 || entity.Y != 20 || entity.Z != 30 {
		t.Errorf("entity is at %v,%v,%v, want the teleported 20,20,30", entity.X, entity.Y, entity.Z)
	}
}

func TestRelativeMovesAccumulateAgainstTheStoredPosition(t *testing.T) {
	t.Parallel()

	w, _ := script(
		t,
		[]protocol.Packet{
			login(1),
			play(&gen.PlayClientboundSpawnEntityLiving{EntityID: 7, X: 320, Y: 0, Z: 0}),
		},
		[]protocol.Packet{play(&gen.PlayClientboundRelEntityMove{EntityID: 7, DX: 32})},
		[]protocol.Packet{play(&gen.PlayClientboundRelEntityMove{EntityID: 7, DX: 32})},
	)

	entity, _ := w.Snapshot().Entities.Get(7)
	if entity.X != 12 {
		t.Errorf("entity x is %v, want 10 plus two one-block steps", entity.X)
	}
}

func TestMetadataIsMergedByIndexAndKeepsUnknownOnes(t *testing.T) {
	t.Parallel()

	// An index this client has no name for is the whole point on a modded
	// server, and a packet carrying one index must not clear the others.
	w, _ := script(
		t,
		[]protocol.Packet{
			login(1),
			play(&gen.PlayClientboundEntityMetadata{EntityID: 7, Metadata: gen.EntityMetadata{
				{AnonymousBitField1: gen.EntityMetadataItemAnonymousBitField1Bits{Key: 0, Type: 0}},
				{AnonymousBitField1: gen.EntityMetadataItemAnonymousBitField1Bits{Key: 99, Type: 3}},
			}}),
		},
		[]protocol.Packet{
			play(&gen.PlayClientboundEntityMetadata{EntityID: 7, Metadata: gen.EntityMetadata{
				{AnonymousBitField1: gen.EntityMetadataItemAnonymousBitField1Bits{Key: 0, Type: 0}},
			}}),
		},
	)

	entity, _ := w.Snapshot().Entities.Get(7)
	if len(entity.Metadata) != 2 {
		t.Fatalf("entity holds %d metadata entries, want both indices", len(entity.Metadata))
	}
	unknown, ok := entity.Metadata[99]
	if !ok {
		t.Fatal("an unknown metadata index was dropped")
	}
	if unknown.Type != "float" {
		t.Errorf("index 99 has type %q, want the type the server sent", unknown.Type)
	}
}

func TestDestroyReleasesEverythingAboutAnEntity(t *testing.T) {
	t.Parallel()

	w, _ := script(
		t,
		[]protocol.Packet{
			login(1),
			play(&gen.PlayClientboundSpawnEntityLiving{EntityID: 7}),
			play(&gen.PlayClientboundEntityMetadata{EntityID: 7, Metadata: gen.EntityMetadata{
				{AnonymousBitField1: gen.EntityMetadataItemAnonymousBitField1Bits{Key: 1}},
			}}),
		},
		[]protocol.Packet{play(&gen.PlayClientboundEntityDestroy{EntityIds: []int32{7}})},
	)

	if _, ok := w.Snapshot().Entities.Get(7); ok {
		t.Error("a destroyed entity is still tracked; a long session would grow without bound")
	}
}

func TestAPacketForAnUnknownEntityIsNotAnError(t *testing.T) {
	t.Parallel()

	// Packets arrive for entities this client never saw spawn — after a
	// chunk unload, for instance. What the server said is kept rather than
	// dropped, and the session survives.
	w, _ := script(t, []protocol.Packet{
		login(1),
		play(&gen.PlayClientboundEntityTeleport{EntityID: 404, X: 320, Y: 320, Z: 320}),
	})

	if _, err := w.SnapshotErr(); err != nil {
		t.Fatalf("an unknown entity poisoned the world: %v", err)
	}
	entity, ok := w.Snapshot().Entities.Get(404)
	if !ok || entity.X != 10 {
		t.Errorf("the unknown entity was dropped rather than tracked: %+v", entity)
	}
}

func TestTheLocalPlayerIsNotAnEntity(t *testing.T) {
	t.Parallel()

	// The boundary in the other direction from the player test: an effect
	// naming the local player must not create an entity for it.
	w, _ := script(t, []protocol.Packet{
		login(42),
		play(&gen.PlayClientboundEntityEffect{EntityID: 42, EffectID: 1, Duration: 100}),
		play(&gen.PlayClientboundEntityEffect{EntityID: 7, EffectID: 1, Duration: 100}),
	})

	if _, ok := w.Snapshot().Entities.Get(42); ok {
		t.Error("the local player is tracked as an entity too")
	}
	if _, ok := w.Snapshot().Entities.Get(7); !ok {
		t.Error("another entity's effect did not track it")
	}
}

func TestCollectingAnItemReleasesIt(t *testing.T) {
	t.Parallel()

	w, events := script(
		t,
		[]protocol.Packet{
			login(1),
			play(&gen.PlayClientboundSpawnEntity{EntityID: 7, Type: 2}),
		},
		[]protocol.Packet{
			play(&gen.PlayClientboundCollect{CollectedEntityID: 7, CollectorEntityID: 42}),
		},
	)

	if _, ok := w.Snapshot().Entities.Get(7); ok {
		t.Error("a collected item is still tracked")
	}
	last := events[len(events)-1].(event.EntityItemCollected)
	if last.CollectedID != 7 || last.CollectorID != 42 {
		t.Errorf("collection is %+v", last)
	}
}

func TestAProtocol47ChunkDecodesToItsBlocks(t *testing.T) {
	t.Parallel()

	// Protocol 47 packs a section as 4096 little-endian shorts holding the
	// block ID in the high twelve bits and the metadata in the low four.
	blob := make([]byte, 8192)
	// Block at section-local 1,0,0 is index 1: stone with metadata 0 is
	// state 1<<4.
	blob[2] = byte(1 << 4)
	blob[3] = 0

	w, _ := script(t, []protocol.Packet{
		login(1),
		play(&gen.PlayClientboundMapChunk{X: 0, Z: 0, GroundUp: true, BitMap: 0x0001, ChunkData: blob}),
	})

	state, ok := w.Snapshot().Chunks.Block(1, 0, 0)
	if !ok {
		t.Fatal("the block was not readable")
	}
	if state != 1<<4 {
		t.Errorf("block state is %d, want stone", state)
	}
}

// bulkColumn47 builds one column of a bulk blob: one section's blocks with a
// single state written at section-local index 0, then the light arrays and the
// biomes that follow it.
func bulkColumn47(state uint16, skyLight bool) []byte {
	column := make([]byte, 8192+2048+256)
	if skyLight {
		column = make([]byte, 8192+2048+2048+256)
	}
	column[0] = byte(state)
	column[1] = byte(state >> 8)

	return column
}

func TestABulkChunkLoadsEveryColumnItCarries(t *testing.T) {
	t.Parallel()

	// A vanilla 1.8.9 server sends the whole join-time world as map_chunk_bulk
	// and never a single-column map_chunk, so an adapter that reduces only the
	// latter sees no terrain and says nothing about it.
	blob := append(bulkColumn47(1<<4, true), bulkColumn47(2<<4, true)...)

	w, events := script(t, []protocol.Packet{
		login(1),
		play(&gen.PlayClientboundMapChunkBulk{
			SkyLightSent: true,
			Meta: []gen.PlayClientboundMapChunkBulkMetaItem{
				{X: 0, Z: 0, BitMap: 0x0001},
				{X: 1, Z: 0, BitMap: 0x0001},
			},
			Data: blob,
		}),
	})

	chunks := w.Snapshot().Chunks
	if state, ok := chunks.Block(0, 0, 0); !ok || state != 1<<4 {
		t.Errorf("first column gave %d, %v, want stone", state, ok)
	}
	// The second column only reads back when the stride is right: getting
	// SkyLightSent wrong misaligns every column after the first.
	if state, ok := chunks.Block(16, 0, 0); !ok || state != 2<<4 {
		t.Errorf("second column gave %d, %v, want granite", state, ok)
	}
	loads := 0
	for _, name := range names(events) {
		if name == event.NameWorldChunkLoaded {
			loads++
		}
	}
	if loads != 2 {
		t.Errorf("published %d loads, want 2", loads)
	}
}

func TestABulkChunkWithoutSkylightUsesTheShorterStride(t *testing.T) {
	t.Parallel()

	// The nether and the end send no skylight, so the same metadata describes
	// a column 2048 bytes per section shorter.
	blob := append(bulkColumn47(1<<4, false), bulkColumn47(2<<4, false)...)

	w, _ := script(t, []protocol.Packet{
		login(1),
		play(&gen.PlayClientboundMapChunkBulk{
			SkyLightSent: false,
			Meta: []gen.PlayClientboundMapChunkBulkMetaItem{
				{X: 0, Z: 0, BitMap: 0x0001},
				{X: 1, Z: 0, BitMap: 0x0001},
			},
			Data: blob,
		}),
	})

	if state, ok := w.Snapshot().Chunks.Block(16, 0, 0); !ok || state != 2<<4 {
		t.Errorf("second column gave %d, %v, want granite", state, ok)
	}
}

func TestABulkChunkStopsAtATruncatedBlob(t *testing.T) {
	t.Parallel()

	// Once the blob runs short, every column after it starts at an unknown
	// offset, so the rest is unreadable rather than merely truncated. What
	// arrived is kept and the guesses are not.
	blob := append(bulkColumn47(1<<4, true), make([]byte, 64)...)

	w, _ := script(t, []protocol.Packet{
		login(1),
		play(&gen.PlayClientboundMapChunkBulk{
			SkyLightSent: true,
			Meta: []gen.PlayClientboundMapChunkBulkMetaItem{
				{X: 0, Z: 0, BitMap: 0x0001},
				{X: 1, Z: 0, BitMap: 0x0001},
			},
			Data: blob,
		}),
	})

	chunks := w.Snapshot().Chunks
	if state, ok := chunks.Block(0, 0, 0); !ok || state != 1<<4 {
		t.Errorf("the complete column gave %d, %v", state, ok)
	}
	if _, ok := chunks.Get(world.ChunkPos{X: 1}); ok {
		t.Error("the truncated column was stored")
	}
}

func TestABlockChangeAppliesToALoadedChunk(t *testing.T) {
	t.Parallel()

	w, _ := script(
		t,
		[]protocol.Packet{
			login(1),
			play(&gen.PlayClientboundMapChunk{
				X: 0, Z: 0, GroundUp: true, BitMap: 0x0001, ChunkData: make([]byte, 8192),
			}),
		},
		[]protocol.Packet{
			play(&gen.PlayClientboundBlockChange{
				Location: gen.Position{X: 3, Y: 4, Z: 5}, Type: 42,
			}),
		},
	)

	state, ok := w.Snapshot().Chunks.Block(3, 4, 5)
	if !ok || state != 42 {
		t.Errorf("block is %d, %v, want 42", state, ok)
	}
}

func TestAMultiBlockChangeAppliesEveryRecord(t *testing.T) {
	t.Parallel()

	w, _ := script(
		t,
		[]protocol.Packet{
			login(1),
			play(&gen.PlayClientboundMapChunk{
				X: 0, Z: 0, GroundUp: true, BitMap: 0x0001, ChunkData: make([]byte, 8192),
			}),
		},
		[]protocol.Packet{
			play(&gen.PlayClientboundMultiBlockChange{
				ChunkX: 0, ChunkZ: 0,
				Records: []gen.PlayClientboundMultiBlockChangeRecordsItem{
					{HorizontalPos: 0x12, Y: 4, BlockID: 7},
					{HorizontalPos: 0x34, Y: 5, BlockID: 8},
				},
			}),
		},
	)

	// HorizontalPos packs x in the high nibble and z in the low one.
	if state, ok := w.Snapshot().Chunks.Block(1, 4, 2); !ok || state != 7 {
		t.Errorf("first record gave %d, %v", state, ok)
	}
	if state, ok := w.Snapshot().Chunks.Block(3, 5, 4); !ok || state != 8 {
		t.Errorf("second record gave %d, %v", state, ok)
	}
}

func TestUnloadingIsDrivenByTheServer(t *testing.T) {
	t.Parallel()

	// Protocol 47 has no unload packet: a ground-up column with an empty
	// section bitmask is the unload. Reading that as a load of nothing leaves
	// the column in the store for the rest of the session, which is the memory
	// bug a long-running bot will certainly hit.
	w, events := script(
		t,
		[]protocol.Packet{
			login(1),
			play(&gen.PlayClientboundMapChunk{
				X: 0, Z: 0, GroundUp: true, BitMap: 0x0001, ChunkData: make([]byte, 8192),
			}),
		},
		[]protocol.Packet{
			play(&gen.PlayClientboundMapChunk{X: 0, Z: 0, GroundUp: true}),
		},
	)

	if _, ok := w.Snapshot().Chunks.Get(world.ChunkPos{}); ok {
		t.Error("the chunk survived its unload")
	}
	if !slices.Contains(names(events), event.NameWorldChunkUnloaded) {
		t.Errorf("published %v, want an unload", names(events))
	}
}

func TestProtocol47DamageIsHonestlyUnattributed(t *testing.T) {
	t.Parallel()

	// Protocol 47 has no damage packet. Hurt is one of many entity statuses
	// and carries no source at all, so the event says so rather than naming
	// entity 0 or damage type 0 as though the server had sent them.
	_, events := script(t, []protocol.Packet{
		login(1),
		play(&gen.PlayClientboundEntityStatus{EntityID: 7, EntityStatus: 2}),
	})

	last := events[len(events)-1]
	damaged, ok := last.(event.EntityDamaged)
	if !ok {
		t.Fatalf("published %T, want event.EntityDamaged", last)
	}
	if damaged.EntityID != 7 {
		t.Errorf("damage names entity %d, want 7", damaged.EntityID)
	}
	if damaged.Damage.Typed || damaged.Damage.Attributed || damaged.Damage.Direct {
		t.Errorf("protocol 47 attributed damage it cannot see: %+v", damaged.Damage)
	}
}

func TestHurtNamingTheLocalPlayerIsAPlayerEvent(t *testing.T) {
	t.Parallel()

	w, events := script(t, []protocol.Packet{
		login(42),
		play(&gen.PlayClientboundEntityStatus{EntityID: 42, EntityStatus: 2}),
	})

	last := events[len(events)-1]
	if _, ok := last.(event.PlayerDamaged); !ok {
		t.Fatalf("published %T, want event.PlayerDamaged", last)
	}
	if _, tracked := w.Snapshot().Entities.Get(42); tracked {
		t.Error("the local player is tracked as an entity")
	}
}

func TestProtocol47NamesTheKillerItDoesSend(t *testing.T) {
	t.Parallel()

	// The reverse of damage: 47's combat event carries a killer and 775's
	// death event does not. The status and the combat event describe one
	// death, and one death publishes once.
	w, events := script(t, []protocol.Packet{
		login(42),
		play(&gen.PlayClientboundEntityStatus{EntityID: 42, EntityStatus: 3}),
		play(&gen.PlayClientboundCombatEvent{
			Event:    2,
			PlayerID: gen.PlayClientboundCombatEventPlayerIDSwitch{Case2: 42},
			EntityID: gen.PlayClientboundCombatEventEntityIDSwitch{Case2: 9},
		}),
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
	// The entity status arrived first and it names no killer. Publishing once
	// costs the attribution the later packet carried, and publishing twice
	// would report two deaths — the first is the lesser wrong, and it is why
	// PlayerDied says whether it was attributed at all.
	if deaths[0].Attributed {
		t.Errorf("the first announcement of a death claimed a killer: %+v", deaths[0])
	}
	if !w.Snapshot().Player.Dead {
		t.Error("the player is not dead after dying")
	}
}

func TestACombatDeathAloneCarriesItsKiller(t *testing.T) {
	t.Parallel()

	_, events := script(t, []protocol.Packet{
		login(42),
		play(&gen.PlayClientboundCombatEvent{
			Event:    2,
			PlayerID: gen.PlayClientboundCombatEventPlayerIDSwitch{Case2: 7},
			EntityID: gen.PlayClientboundCombatEventEntityIDSwitch{Case2: 9},
		}),
	})

	last := events[len(events)-1]
	died, ok := last.(event.EntityDied)
	if !ok {
		t.Fatalf("published %T, want event.EntityDied", last)
	}
	if died.EntityID != 7 || !died.Attributed || died.KillerID != 9 {
		t.Errorf("death is %+v, want entity 7 killed by 9", died)
	}
}

func TestACombatDeathWithNoKillerSaysSo(t *testing.T) {
	t.Parallel()

	// Protocol 47 sends -1 when nothing killed the player.
	_, events := script(t, []protocol.Packet{
		login(42),
		play(&gen.PlayClientboundCombatEvent{
			Event:    2,
			PlayerID: gen.PlayClientboundCombatEventPlayerIDSwitch{Case2: 42},
			EntityID: gen.PlayClientboundCombatEventEntityIDSwitch{Case2: -1},
		}),
	})

	died := events[len(events)-1].(event.PlayerDied)
	if died.Attributed || died.KillerID != 0 {
		t.Errorf("a death with no killer is %+v, want unattributed and zero", died)
	}
}

func TestGameStateChangeReachesTwoDomains(t *testing.T) {
	t.Parallel()

	// One packet type, several unrelated meanings, discriminated by a reason
	// byte. The player reducer and the environment reducer both handle it and
	// each ignores the reasons that are not its own.
	_, modeEvents := script(t, []protocol.Packet{
		login(1),
		play(&gen.PlayClientboundGameStateChange{Reason: 3, GameMode: 1}),
	})
	if got := names(modeEvents); !slices.Contains(got, event.NamePlayerGameModeChanged) ||
		slices.Contains(got, event.NameWorldWeatherChanged) {
		t.Errorf("a game-mode change published %v", got)
	}

	// Protocol 47 numbers the weather reasons the opposite way round from 775:
	// here 2 begins rain and 1 ends it.
	w, rainEvents := script(t, []protocol.Packet{
		login(1),
		play(&gen.PlayClientboundGameStateChange{Reason: 2}),
	})
	if got := names(rainEvents); !slices.Contains(got, event.NameWorldWeatherChanged) ||
		slices.Contains(got, event.NamePlayerGameModeChanged) {
		t.Errorf("a weather change published %v", got)
	}
	if weather := w.Snapshot().Environment; !weather.Raining || !weather.WeatherKnown {
		t.Errorf("reason 2 did not start rain: %+v", weather)
	}

	w, _ = script(t, []protocol.Packet{
		login(1),
		play(&gen.PlayClientboundGameStateChange{Reason: 1}),
	})
	if w.Snapshot().Environment.Raining {
		t.Error("reason 1 started rain instead of ending it")
	}
}

func TestWeatherIsUnknownUntilTheServerMentionsIt(t *testing.T) {
	t.Parallel()

	// A world nobody mentioned rain in is not a world known to be dry.
	w, _ := script(t, []protocol.Packet{login(1)})
	if w.Snapshot().Environment.WeatherKnown {
		t.Error("weather is reported as known before the server said anything")
	}
}

func TestSixBorderActionsProduceOneEventShape(t *testing.T) {
	t.Parallel()

	// Protocol 47 sends one packet with an action discriminator where 775
	// sends six packets. Both produce WorldBorderChanged carrying the whole
	// resulting border, and each action leaves the fields it does not carry.
	w, events := script(
		t,
		[]protocol.Packet{
			login(1),
			play(&gen.PlayClientboundWorldBorder{
				Action:         3,
				X:              gen.PlayClientboundWorldBorderXSwitch{Case3: 8},
				Z:              gen.PlayClientboundWorldBorderZSwitch{Case3: 9},
				OldRadius:      gen.PlayClientboundWorldBorderOldRadiusSwitch{Case3: 100},
				NewRadius:      gen.PlayClientboundWorldBorderNewRadiusSwitch{Case3: 100},
				PortalBoundary: gen.PlayClientboundWorldBorderPortalBoundarySwitch{Case3: 29999984},
				WarningTime:    gen.PlayClientboundWorldBorderWarningTimeSwitch{Case3: 15},
				WarningBlocks:  gen.PlayClientboundWorldBorderWarningBlocksSwitch{Case3: 5},
			}),
		},
		[]protocol.Packet{
			play(&gen.PlayClientboundWorldBorder{
				Action: 2,
				X:      gen.PlayClientboundWorldBorderXSwitch{Case2: 1},
				Z:      gen.PlayClientboundWorldBorderZSwitch{Case2: 2},
			}),
		},
	)

	for _, published := range events {
		if published.Name() == event.NameWorldBorderChanged {
			if _, ok := published.(event.WorldBorderChanged); !ok {
				t.Fatalf("border event is %T", published)
			}
		}
	}

	// Moving the centre says nothing about the diameter, and the warning
	// values the initializing packet set must survive it.
	border := w.Snapshot().Environment.Border
	if !border.Known || border.X != 1 || border.Z != 2 {
		t.Errorf("border centre is %+v, want 1,2", border)
	}
	if border.NewDiameter != 100 || border.WarningTime != 15 || border.WarningBlocks != 5 {
		t.Errorf("moving the centre cleared the rest of the border: %+v", border)
	}
}

func TestWorldEventsAndParticlesAreNotTheSameEvent(t *testing.T) {
	t.Parallel()

	// A world event is a discrete effect with an ID and a position; particles
	// are presentational and carry no state.
	_, events := script(t, []protocol.Packet{
		login(1),
		play(&gen.PlayClientboundWorldParticles{ParticleID: 1, Particles: 30}),
		play(&gen.PlayClientboundWorldEvent{
			EffectID: 1003, Location: gen.Position{X: 4, Y: 64, Z: 5}, Data: 0,
		}),
	})

	occurred := 0
	for _, published := range events {
		if world, ok := published.(event.WorldEventOccurred); ok {
			occurred++
			if world.EffectID != 1003 || world.Position.Y != 64 {
				t.Errorf("world event is %+v", world)
			}
		}
	}
	if occurred != 1 {
		t.Errorf("published %d world events, want exactly 1 — particles are not one", occurred)
	}
}

func TestProtocol47SendsNoSimulationSettings(t *testing.T) {
	t.Parallel()

	// View distance, simulation distance, the view centre, the tick rate, and
	// game rules are all 775-only. The snapshot must say unknown rather than
	// reporting a view distance of zero for a whole session.
	w, events := script(t, []protocol.Packet{
		login(1),
		play(&gen.PlayClientboundUpdateTime{Age: 120, Time: 6000}),
	})

	environment := w.Snapshot().Environment
	if environment.SimulationKnown {
		t.Errorf("protocol 47 reported simulation settings: %+v", environment)
	}
	if slices.Contains(names(events), event.NameWorldSimulationSettingsChanged) {
		t.Error("protocol 47 published a simulation settings event")
	}
	// The clock, though, it does send, and directly.
	if !environment.TimeOfDayKnown || environment.TimeOfDay != 6000 || environment.Age != 120 {
		t.Errorf("time is %+v, want age 120 time 6000", environment)
	}
	if len(environment.Clocks) != 0 {
		t.Errorf("protocol 47 produced clocks: %+v", environment.Clocks)
	}
}

func TestDifficultyIsNamedOnBothProtocols(t *testing.T) {
	t.Parallel()

	// 47 numbers them and 775 names them; the snapshot holds the name, so a
	// caller never sees a 2 it has to look up.
	w, _ := script(t, []protocol.Packet{
		login(1),
		play(&gen.PlayClientboundDifficulty{Difficulty: 2}),
	})

	environment := w.Snapshot().Environment
	if !environment.DifficultyKnown || environment.Difficulty != "normal" {
		t.Errorf("difficulty is %q known %v, want normal", environment.Difficulty, environment.DifficultyKnown)
	}
	if environment.Locked {
		t.Error("protocol 47 reported a difficulty lock it does not send")
	}
}

func TestTheContainerRecordsWhatTheServerOpened(t *testing.T) {
	t.Parallel()

	// The container is what the server said it opened, never a menu predicted
	// from the block that was clicked.
	w, events := script(t, []protocol.Packet{
		login(1),
		play(&gen.PlayClientboundOpenWindow{
			WindowID: 3, InventoryType: "minecraft:chest",
			WindowTitle: `{"text":"Chest"}`, SlotCount: 27,
		}),
	})

	opened := events[len(events)-1].(event.ContainerOpened)
	if opened.ContainerID != 3 || opened.MenuType != "minecraft:chest" || opened.SlotCount != 27 {
		t.Errorf("opened %+v", opened)
	}

	menu, ok := w.Snapshot().Containers.Get(3)
	if !ok || menu.MenuType != "minecraft:chest" || menu.Title != `{"text":"Chest"}` {
		t.Errorf("menu is %+v", menu)
	}
	// Protocol 47 has no state ID, and zero is a valid one, so the snapshot
	// must report it as absent rather than as zero.
	if menu.StateKnown {
		t.Error("protocol 47 reported a state ID it does not send")
	}
}

func TestAnUnknownMenuTypeIsStillAUsableContainer(t *testing.T) {
	t.Parallel()

	// A modded server opens menus this client has never heard of. Raw slots
	// are readable; no semantic layout is invented.
	w, _ := script(t, []protocol.Packet{
		login(1),
		play(&gen.PlayClientboundOpenWindow{WindowID: 4, InventoryType: "modded:reactor"}),
		play(&gen.PlayClientboundSetSlot{
			WindowID: 4, Slot: 2, Item: gen.Slot{BlockID: 264},
		}),
	})

	menu, ok := w.Snapshot().Containers.Get(4)
	if !ok || menu.MenuType != "modded:reactor" {
		t.Fatalf("menu is %+v", menu)
	}
	if _, filled := menu.Slots[2]; !filled {
		t.Errorf("slot 2 of an unknown menu is unreadable: %+v", menu.Slots)
	}
}

func TestSetSlotOnContainerMinusOneTargetsTheCursor(t *testing.T) {
	t.Parallel()

	// The special case that silently corrupts an inventory model when missed:
	// container -1, slot -1 is the cursor, not a slot in any menu.
	w, events := script(t, []protocol.Packet{
		login(1),
		play(&gen.PlayClientboundOpenWindow{WindowID: 1, InventoryType: "minecraft:chest"}),
		play(&gen.PlayClientboundSetSlot{WindowID: -1, Slot: -1, Item: gen.Slot{BlockID: 1}}),
	})

	if _, ok := events[len(events)-1].(event.ContainerCursorChanged); !ok {
		t.Fatalf("published %T, want event.ContainerCursorChanged", events[len(events)-1])
	}
	containers := w.Snapshot().Containers
	if !containers.CursorKnown || !containers.CursorHeld {
		t.Errorf("cursor is %+v, want held", containers)
	}
	if menu, _ := containers.Get(1); len(menu.Slots) != 0 {
		t.Errorf("a cursor update landed in a menu: %+v", menu.Slots)
	}
}

func TestClosingAMenuReleasesIt(t *testing.T) {
	t.Parallel()

	w, _ := script(
		t,
		[]protocol.Packet{
			login(1),
			play(&gen.PlayClientboundOpenWindow{WindowID: 5, InventoryType: "minecraft:chest"}),
			play(&gen.PlayClientboundWindowItems{
				WindowID: 5, Items: []gen.Slot{{BlockID: 1}, {BlockID: 2}},
			}),
		},
		[]protocol.Packet{play(&gen.PlayClientboundCloseWindow{WindowID: 5})},
	)

	if _, ok := w.Snapshot().Containers.Get(5); ok {
		t.Error("a closed menu is still open")
	}
}

func TestReopeningAContainerIDDoesNotCarryTheOldMenu(t *testing.T) {
	t.Parallel()

	// A server reuses container IDs. Carrying the old menu's slots into the
	// new one would report a chest's contents as a furnace's.
	w, _ := script(
		t,
		[]protocol.Packet{
			login(1),
			play(&gen.PlayClientboundOpenWindow{WindowID: 6, InventoryType: "minecraft:chest"}),
			play(&gen.PlayClientboundSetSlot{WindowID: 6, Slot: 0, Item: gen.Slot{BlockID: 1}}),
		},
		[]protocol.Packet{
			play(&gen.PlayClientboundOpenWindow{WindowID: 6, InventoryType: "minecraft:furnace"}),
		},
	)

	menu, _ := w.Snapshot().Containers.Get(6)
	if menu.MenuType != "minecraft:furnace" {
		t.Errorf("menu type is %q", menu.MenuType)
	}
	if len(menu.Slots) != 0 {
		t.Errorf("the old menu's slots survived: %+v", menu.Slots)
	}
}

func TestProtocol47HasNoRecipeBook(t *testing.T) {
	t.Parallel()

	// 1.8 has no recipe book and no trade packet. An empty recipe set must not
	// read as "the server sent no recipes".
	w, _ := script(t, []protocol.Packet{login(1)})

	if w.Snapshot().Containers.RecipesKnown {
		t.Error("protocol 47 reported a recipe book it has no packet for")
	}
}

func TestProtocol47HasNoSessionRegistry(t *testing.T) {
	t.Parallel()

	// 1.8's registries are entirely static: there is no registry-data packet,
	// no tag packet, and no command tree. An empty session registry must
	// report that none was sent rather than presenting itself as a server that
	// defined nothing.
	w, _ := script(t, []protocol.Packet{login(1)})

	registries := w.Snapshot().Registries
	if registries.SessionRegistries || registries.TagsSent || registries.CommandsKnown {
		t.Errorf("protocol 47 reported a session vocabulary: %+v", registries)
	}
}

func TestThePlayerListIsNotTheEntityStore(t *testing.T) {
	t.Parallel()

	// A listed player may be nowhere near this client and have no entity. The
	// two describe different things, and adding one must not create the other.
	w, _ := script(t, []protocol.Packet{
		login(1),
		play(&gen.PlayClientboundPlayerInfo{
			Action: "add_player",
			Data: []gen.PlayClientboundPlayerInfoDataItem{{
				AnonymousSwitch1: gen.PlayClientboundPlayerInfoDataItemAnonymousSwitch1Switch{
					AddPlayer: gen.PlayClientboundPlayerInfoDataItemAnonymousSwitch1SwitchAddPlayer{
						Name: "someone", Gamemode: 1, Ping: 25,
					},
				},
			}},
		}),
	})

	snapshot := w.Snapshot()
	if len(snapshot.Registries.Players) != 1 {
		t.Fatalf("player list is %+v", snapshot.Registries.Players)
	}
	for _, player := range snapshot.Registries.Players {
		if player.Name != "someone" || player.Latency != 25 || !player.Listed {
			t.Errorf("listed player is %+v", player)
		}
	}
	if len(snapshot.Entities.Tracked) != 0 {
		t.Errorf("a list entry created an entity: %+v", snapshot.Entities.Tracked)
	}
}

func TestAProtocol47LatencyUpdateKeepsTheName(t *testing.T) {
	t.Parallel()

	// 47's action is a single choice, so a latency update carries nothing
	// else, and it must not blank what the add supplied.
	w, _ := script(
		t,
		[]protocol.Packet{
			login(1),
			play(&gen.PlayClientboundPlayerInfo{
				Action: "add_player",
				Data: []gen.PlayClientboundPlayerInfoDataItem{{
					AnonymousSwitch1: gen.PlayClientboundPlayerInfoDataItemAnonymousSwitch1Switch{
						AddPlayer: gen.PlayClientboundPlayerInfoDataItemAnonymousSwitch1SwitchAddPlayer{
							Name: "someone", Gamemode: 1, Ping: 25,
						},
					},
				}},
			}),
		},
		[]protocol.Packet{
			play(&gen.PlayClientboundPlayerInfo{
				Action: "update_latency",
				Data: []gen.PlayClientboundPlayerInfoDataItem{{
					AnonymousSwitch1: gen.PlayClientboundPlayerInfoDataItemAnonymousSwitch1Switch{
						UpdateLatency: gen.PlayClientboundPlayerInfoDataItemAnonymousSwitch1SwitchUpdateLatency{
							Ping: 300,
						},
					},
				}},
			}),
		},
	)

	for _, player := range w.Snapshot().Registries.Players {
		if player.Latency != 300 || player.Name != "someone" || player.GameMode != 1 {
			t.Errorf("player is %+v, want latency 300 with the rest kept", player)
		}
	}
}

func TestProtocol47SendsEveryMessageThroughOnePacket(t *testing.T) {
	t.Parallel()

	// 47 has one chat packet with a position byte, where 775 has three. Both
	// produce ChatReceived with a kind, and 47 has no way to withdraw a
	// message, no signature, and no message index.
	w, events := script(t, []protocol.Packet{
		login(1),
		play(&gen.PlayClientboundChat{Message: `{"text":"hello"}`}),
		play(&gen.PlayClientboundChat{Message: `{"text":"bar"}`, Position: 2}),
	})

	var received []event.ChatReceived
	for _, published := range events {
		if message, ok := published.(event.ChatReceived); ok {
			received = append(received, message)
		}
	}
	if len(received) != 2 {
		t.Fatalf("published %d messages, want 2", len(received))
	}
	if received[0].Kind != event.ChatKindSystem || received[0].ActionBar {
		t.Errorf("first message is %+v", received[0])
	}
	if !received[1].ActionBar {
		t.Errorf("position 2 is the action bar: %+v", received[1])
	}
	for _, message := range received {
		if message.Signed || message.IndexKnown {
			t.Errorf("protocol 47 claimed a signature or an index: %+v", message)
		}
	}

	if got := len(w.Snapshot().Chat.Log); got != 2 {
		t.Errorf("log holds %d messages, want 2", got)
	}
}

func TestProtocol47HasNoBossBarOrDialog(t *testing.T) {
	t.Parallel()

	// A 1.8 boss bar is a wither entity and 1.8 has no dialog at all. Four of
	// this domain's twelve events never fire here, and the snapshot must not
	// invent them.
	w, _ := script(t, []protocol.Packet{login(1)})

	chat := w.Snapshot().Chat
	if len(chat.BossBars) != 0 || chat.DialogOpen {
		t.Errorf("protocol 47 reported UI it has no packet for: %+v", chat)
	}
}

func TestTheTitlePacketsActionsReachOneEvent(t *testing.T) {
	t.Parallel()

	// 47 packs five title actions into one packet where 775 sends five
	// packets, and both produce ChatTitleChanged.
	w, events := script(
		t,
		[]protocol.Packet{
			login(1),
			play(&gen.PlayClientboundTitle{
				Action:  2,
				FadeIn:  gen.PlayClientboundTitleFadeInSwitch{Case2: 10},
				Stay:    gen.PlayClientboundTitleStaySwitch{Case2: 70},
				FadeOut: gen.PlayClientboundTitleFadeOutSwitch{Case2: 20},
			}),
		},
		[]protocol.Packet{play(&gen.PlayClientboundTitle{Action: 4})},
	)

	changes := 0
	for _, published := range events {
		if _, ok := published.(event.ChatTitleChanged); ok {
			changes++
		}
	}
	if changes != 2 {
		t.Errorf("published %d title changes, want 2", changes)
	}
	// The reset action clears the timings it had learned.
	if w.Snapshot().Chat.TitleTimesKnown {
		t.Error("a title reset left the timings behind")
	}
}

func TestTheSpawnPositionIsReducedWithoutADimensionOrAnAngle(t *testing.T) {
	t.Parallel()

	// Protocol 47 sends coordinates alone. The world records that rather than
	// borrowing the player's dimension, which would be a guess reported as an
	// observation.
	w, _ := script(t, []protocol.Packet{
		login(1),
		play(&gen.PlayClientboundSpawnPosition{
			Location: gen.Position{X: 120, Y: 70, Z: -40},
		}),
	})

	environment := w.Snapshot().Environment
	if !environment.SpawnKnown {
		t.Fatal("the spawn position packet did not reach the world")
	}
	if want := (world.BlockPos{X: 120, Y: 70, Z: -40}); environment.Spawn != want {
		t.Errorf("spawn is %+v, want %+v", environment.Spawn, want)
	}
	if environment.SpawnDimension != "" {
		t.Errorf("dimension is %q, want empty on a protocol that sends none", environment.SpawnDimension)
	}
	if environment.SpawnAngled {
		t.Error("protocol 47 reported a spawn angle it does not send")
	}
}

func TestHealthReachingZeroIsADeath(t *testing.T) {
	t.Parallel()

	// A vanilla 1.8.9 server was observed killing a player and announcing it
	// with nothing but this: seven hurt statuses, an enter-combat and an
	// end-combat, health stepping to zero, and no entity status 3 and no
	// entity-dead combat event. A client that waited for one of those stayed
	// dead for the rest of the session, which is what the orbit example did on
	// a live server.
	w, events := script(t, []protocol.Packet{
		login(1),
		play(&gen.PlayClientboundUpdateHealth{Health: 0, Food: 20}),
	})

	if !w.Snapshot().Player.Dead {
		t.Error("health reached zero and the player is not dead")
	}

	var died int
	for _, e := range events {
		if _, ok := e.(event.PlayerDied); ok {
			died++
		}
	}
	if died != 1 {
		t.Errorf("published %d deaths, want 1", died)
	}
}

func TestADeathIsPublishedOnceAcrossBothItsSignals(t *testing.T) {
	t.Parallel()

	// Health and the combat event are the same death when a server sends both,
	// and a caller that respawns on each would send two.
	_, events := script(t, []protocol.Packet{
		login(1),
		play(&gen.PlayClientboundUpdateHealth{Health: 0, Food: 20}),
		play(&gen.PlayClientboundEntityStatus{EntityID: 1, EntityStatus: 3}),
	})

	var died int
	for _, e := range events {
		if _, ok := e.(event.PlayerDied); ok {
			died++
		}
	}
	if died != 1 {
		t.Errorf("published %d deaths for one death, want 1", died)
	}
}

func TestSurvivingDamageIsNotADeath(t *testing.T) {
	t.Parallel()

	w, _ := script(t, []protocol.Packet{
		login(1),
		play(&gen.PlayClientboundUpdateHealth{Health: 2, Food: 20}),
	})

	if w.Snapshot().Player.Dead {
		t.Error("a player on two hearts was reported dead")
	}
}
