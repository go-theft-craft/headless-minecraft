package v26_1

import (
	"encoding/binary"
	"errors"
	"os"
	"testing"

	protocol "github.com/go-theft-craft/minecraft-protocol"
	gen "github.com/go-theft-craft/minecraft-protocol/generated/java/v26_1"
	"github.com/go-theft-craft/minecraft-protocol/wire/java"
	"github.com/go-theft-craft/minecraft-protocol/wire/java/chunk"

	"github.com/go-theft-craft/headless-minecraft/event"
	"github.com/go-theft-craft/headless-minecraft/version"
	"github.com/go-theft-craft/headless-minecraft/world"
)

// The fixture is one chunk column as a real Paper 26.1 server sent it, taken
// from the capture behind minecraft-protocol's vanilla-client check. It is the
// thing this decoder was blocked on: the section format is not described by
// any data this repository generates from, and a decoder written from memory
// returns plausible wrong blocks rather than an error.
//
// It carries no block entities, so it is terrain and nothing else.
const columnFixture = "testdata/chunk-26.1-0-0.bin"

// overworldBottom is the lowest section index of a 26.1 overworld, whose
// minimum build height is -64. The blob does not carry it; see splitColumn775.
const overworldBottom = -4

func realColumn(t *testing.T) []byte {
	t.Helper()

	data, err := os.ReadFile(columnFixture)
	if err != nil {
		t.Fatalf("read the captured column: %v", err)
	}

	return data
}

func TestARealColumnSplitsIntoItsSections(t *testing.T) {
	t.Parallel()

	sections, err := splitColumn775(realColumn(t), overworldBottom)
	if err != nil {
		t.Fatalf("splitColumn775: %v", err)
	}

	// A 26.1 overworld is 384 blocks tall from y=-64, which is 24 sections.
	if len(sections) != 24 {
		t.Fatalf("the column split into %d sections, want 24", len(sections))
	}
	if sections[0].Y != -4 {
		t.Errorf("the lowest section is at index %d, want -4", sections[0].Y)
	}
	if sections[len(sections)-1].Y != 19 {
		t.Errorf("the highest section is at index %d, want 19", sections[len(sections)-1].Y)
	}
}

// TestEveryRealSectionDecodesToTheBlockCountTheServerDeclared is the check
// that makes this a decoder rather than a guess.
//
// Each section is prefixed with the count of blocks the server considers
// non-empty. Nothing in the decode path reads it — it is a block semantic, and
// this package does not own those — so it is an independent statement of what
// the section holds, written by the server that packed the bytes. A decoder
// that misreads the bit width, the palette, or the long packing produces a
// different count. All 24 agreeing is not a decoder that runs; it is a decoder
// that agrees with the server about every one of 98,304 blocks.
//
// The comparison treats state 0 as the only empty one. Vanilla counts three
// air states, so a section holding cave_air or void_air would disagree without
// being wrong; this fixture holds neither, which is checked below.
func TestEveryRealSectionDecodesToTheBlockCountTheServerDeclared(t *testing.T) {
	t.Parallel()

	data := realColumn(t)
	sections, err := splitColumn775(data, overworldBottom)
	if err != nil {
		t.Fatalf("splitColumn775: %v", err)
	}

	declared := declaredBlockCounts(t, data)
	if len(declared) != len(sections) {
		t.Fatalf("read %d counts for %d sections", len(declared), len(sections))
	}

	const voidAir, caveAir = 15292, 15293
	for i, section := range sections {
		states, err := section.Decode(section.Raw)
		if err != nil {
			t.Fatalf("section %d: %v", section.Y, err)
		}
		if len(states) != 4096 {
			t.Fatalf("section %d decoded %d states, want 4096", section.Y, len(states))
		}

		filled := 0
		for _, state := range states {
			if state == voidAir || state == caveAir {
				t.Fatalf("section %d holds an air state the count treats as empty", section.Y)
			}
			if state != 0 {
				filled++
			}
		}
		if filled != declared[i] {
			t.Errorf("section %d decoded %d filled blocks, the server declared %d",
				section.Y, filled, declared[i])
		}
	}
}

// declaredBlockCounts is what each section says it holds.
//
// The count is the server's own statement, carried through the split and read
// by nothing in the decode path, so comparing a decode against it still checks
// this client's terrain against the server that packed it -- what changed is
// that the walk producing both is now one walk in the shared reader rather
// than two here.
func declaredBlockCounts(t *testing.T, data []byte) []int {
	t.Helper()

	sections, err := chunk.Split775(data, overworldBottom)
	if err != nil {
		t.Fatalf("chunk.Split775: %v", err)
	}

	counts := make([]int, 0, len(sections))
	for _, section := range sections {
		counts = append(counts, int(section.BlockCount))
	}

	return counts
}

func TestARealColumnAnswersBlockLookupsThroughTheWorld(t *testing.T) {
	t.Parallel()

	// What a consumer actually does: load the column, then ask for a block.
	// The section at index -4 is the bottom of the world, and this column is
	// full there — 4096 filled blocks, which is bedrock and deepslate.
	sections, err := splitColumn775(realColumn(t), overworldBottom)
	if err != nil {
		t.Fatalf("splitColumn775: %v", err)
	}

	states, err := sections[0].Decode(sections[0].Raw)
	if err != nil {
		t.Fatalf("decode the lowest section: %v", err)
	}
	if states[0] == 0 {
		t.Error("the block at the bottom of the world is air")
	}

	// Air above the terrain, so the decode is not reporting one value
	// everywhere. Section 19 is y=304..319.
	top, err := sections[len(sections)-1].Decode(sections[len(sections)-1].Raw)
	if err != nil {
		t.Fatalf("decode the highest section: %v", err)
	}
	for _, state := range top {
		if state != 0 {
			t.Fatalf("the section at y=304 holds state %d, want air", state)
		}
	}
}

// TestAnUndecodableSectionReportsItselfInBothVocabularies pins what this
// package adds to the shared reader.
//
// The reader says which part of the container it could not read. The world
// requires something else: a section it cannot decode has to answer with
// ErrSectionNotDecodable rather than with zeros, because zero is air and air
// is an answer a consumer will walk into. The wrapper owes both, so a caller
// matching either sentinel keeps working.
//
// The two inputs are the failures worth naming. One is short of its long
// array. The other declares more bits an entry than a long can hold, which is
// what a malformed column from the network used to divide by -- the bound
// lives in the reader now, and this is the check that it still reaches a
// caller as a decode failure rather than as a panic.
func TestAnUndecodableSectionReportsItselfInBothVocabularies(t *testing.T) {
	t.Parallel()

	for name, raw := range map[string][]byte{
		"short of its long array": {0x04, 0x01, 0x01, 0x00},
		"an impossible bit width": {0xFF, 0x00},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := decodeSection775(raw)
			if !errors.Is(err, world.ErrSectionNotDecodable) {
				t.Errorf("got %v, want it to be ErrSectionNotDecodable", err)
			}
			if !errors.Is(err, chunk.ErrSection) {
				t.Errorf("got %v, want it to be chunk.ErrSection", err)
			}
		})
	}
}

// TestAColumnThatCannotBeReadIsKeptWhole is the fallback this package owns.
//
// The shared reader refuses a column that does not fit its sections, and what
// this adapter does with that refusal is keep the bytes: a lookup in the
// column reports that it cannot be decoded, which is what it did before any
// decoder existed, rather than reporting air.
func TestAColumnThatCannotBeReadIsKeptWhole(t *testing.T) {
	t.Parallel()

	floor := &columnFloor{section: overworldBottom, placed: true}
	truncated := realColumn(t)[:len(realColumn(t))-1]

	sections := columnSections(floor, truncated)
	if len(sections) != 1 {
		t.Fatalf("a column that does not fit kept %d sections, want 1 whole", len(sections))
	}
	if sections[0].Decode != nil {
		t.Error("a column that could not be read was given a decoder")
	}
	if len(sections[0].Raw) != len(truncated) {
		t.Errorf("kept %d bytes of the %d that arrived", len(sections[0].Raw), len(truncated))
	}
}

// The tests below drive the chain the column's origin actually travels: the
// dimension type registry in configuration, the player's dimension with the
// login, and only then a chunk.

func dimensionRegistry(t *testing.T, floors map[string]int32, order []string) protocol.Packet {
	t.Helper()

	limits, err := protocol.NewLimits()
	if err != nil {
		t.Fatalf("NewLimits: %v", err)
	}

	entries := make([]gen.ConfigurationClientboundRegistryDataEntriesItem, 0, len(order))
	for _, key := range order {
		// An anonymous-root compound holding one TAG_Int named min_y, which is
		// the shape a real server sends among several dozen other keys.
		body := []byte{0x0A, 0x03, 0x00, 0x05, 'm', 'i', 'n', '_', 'y'}
		body = binary.BigEndian.AppendUint32(body, uint32(floors[key]))
		body = append(body, 0x00)

		value, err := java.NewNetworkNBT(body, limits)
		if err != nil {
			t.Fatalf("NewNetworkNBT: %v", err)
		}
		entries = append(entries, gen.ConfigurationClientboundRegistryDataEntriesItem{
			Key: key, Value: &value,
		})
	}

	return protocol.Packet{
		State:     gen.StateConfiguration,
		Direction: protocol.DirectionClientbound,
		Name:      "registry_data",
		Value: &gen.ConfigurationClientboundRegistryData{
			ID: "minecraft:dimension_type", Entries: entries,
		},
	}
}

// sessionScript drives one configuration batch and then play batches against
// one world, which is the order a real session has.
func sessionScript(t *testing.T, configuration []protocol.Packet, batches ...[]protocol.Packet) *world.World {
	t.Helper()

	w := world.New()
	for _, reducer := range Reducers(w) {
		if err := w.Register(reducer); err != nil {
			t.Fatalf("Register: %v", err)
		}
	}

	var c event.Collector
	if _, err := w.Apply(version.Batch{Packets: configuration, State: gen.StateConfiguration}, &c); err != nil {
		t.Fatalf("Apply configuration: %v", err)
	}
	for _, packets := range batches {
		c.Reset()
		if _, err := w.Apply(version.Batch{Packets: packets, State: gen.StatePlay}, &c); err != nil {
			t.Fatalf("Apply play: %v", err)
		}
	}

	return w
}

func spawnIn(dimension int32) protocol.Packet {
	return protocol.Packet{
		State: gen.StatePlay, Direction: protocol.DirectionClientbound, Name: "login",
		Value: &gen.PlayClientboundLogin{
			EntityID:   1,
			WorldState: gen.SpawnInfo{Dimension: dimension, Name: "minecraft:overworld"},
		},
	}
}

func chunkAt(t *testing.T, x, z int32) protocol.Packet {
	t.Helper()

	return protocol.Packet{
		State: gen.StatePlay, Direction: protocol.DirectionClientbound, Name: "map_chunk",
		Value: &gen.PlayClientboundMapChunk{X: x, Z: z, ChunkData: realColumn(t)},
	}
}

func TestAColumnIsPlacedByTheDimensionsFloor(t *testing.T) {
	t.Parallel()

	// The overworld's floor is -64, so the bottom section is index -4 and a
	// lookup at y=-64 is a real block. A reducer that assumed the blob starts
	// at zero would answer for y=0 instead, which is 64 blocks of open air
	// away and reads as a working client.
	w := sessionScript(
		t,
		[]protocol.Packet{dimensionRegistry(
			t,
			map[string]int32{"minecraft:overworld": -64, "minecraft:the_nether": 0},
			[]string{"minecraft:overworld", "minecraft:the_nether"},
		)},
		[]protocol.Packet{spawnIn(0)},
		[]protocol.Packet{chunkAt(t, 0, 0)},
	)

	chunks := w.Snapshot().Chunks
	column, ok := chunks.Get(world.ChunkPos{})
	if !ok {
		t.Fatal("the column was not loaded")
	}
	if len(column.Sections) != 24 {
		t.Fatalf("the column holds %d sections, want 24", len(column.Sections))
	}

	state, ok := chunks.Block(0, -64, 0)
	if !ok {
		t.Fatal("the block at the bottom of the world is not readable")
	}
	if state == 0 {
		t.Error("the block at the bottom of the world is air")
	}
}

func TestAColumnWithNoKnownFloorStaysUndecoded(t *testing.T) {
	t.Parallel()

	// No registry and no login: the client does not know where the column
	// starts, and a guess would put every block in the wrong place. The bytes
	// are kept and the lookup says it cannot answer, which is what this
	// adapter did before the decoder existed.
	w := sessionScript(t, nil, []protocol.Packet{chunkAt(t, 0, 0)})

	if _, ok := w.Snapshot().Chunks.Get(world.ChunkPos{}); !ok {
		t.Fatal("the column was dropped rather than kept")
	}
	if _, ok := w.Snapshot().Chunks.Block(0, -64, 0); ok {
		t.Error("a block was answered from a column with no known floor")
	}
}

func TestChangingDimensionMovesTheFloor(t *testing.T) {
	t.Parallel()

	// A client that learned the overworld's floor and then walked into the
	// nether would place every section 64 blocks too low. The nether's floor
	// is zero, which is also the value an absent lookup would return, so this
	// is the case that separates "not sent" from "sent as zero".
	w := sessionScript(
		t,
		[]protocol.Packet{dimensionRegistry(
			t,
			map[string]int32{"minecraft:overworld": -64, "minecraft:the_nether": 0},
			[]string{"minecraft:overworld", "minecraft:the_nether"},
		)},
		[]protocol.Packet{spawnIn(0)},
		[]protocol.Packet{{
			State: gen.StatePlay, Direction: protocol.DirectionClientbound, Name: "respawn",
			Value: &gen.PlayClientboundRespawn{
				WorldState: gen.SpawnInfo{Dimension: 1, Name: "minecraft:the_nether"},
			},
		}},
		[]protocol.Packet{chunkAt(t, 0, 0)},
	)

	column, ok := w.Snapshot().Chunks.Get(world.ChunkPos{})
	if !ok {
		t.Fatal("the column was not loaded")
	}
	// The same blob, placed from zero: sections 0 through 23 rather than -4
	// through 19.
	if _, ok := column.Sections[0]; !ok {
		t.Error("the nether column has no section at index 0")
	}
	if _, ok := column.Sections[-4]; ok {
		t.Error("the nether column has a section below its floor")
	}
}

func TestALoginInTheSameBatchAsAChunkIsSeenFirst(t *testing.T) {
	t.Parallel()

	// Reducers read a batch in wire order, so a server that bundles the
	// placement with the first columns still places them. Ordering this the
	// other way would leave the whole join-time world undecoded.
	w := sessionScript(
		t,
		[]protocol.Packet{dimensionRegistry(
			t,
			map[string]int32{"minecraft:overworld": -64},
			[]string{"minecraft:overworld"},
		)},
		[]protocol.Packet{spawnIn(0), chunkAt(t, 0, 0)},
	)

	if state, ok := w.Snapshot().Chunks.Block(0, -64, 0); !ok || state == 0 {
		t.Errorf("the bundled column gave %d, %v", state, ok)
	}
}
