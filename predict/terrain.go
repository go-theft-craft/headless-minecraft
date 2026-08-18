// Package predict simulates the local player's movement ahead of the server and
// reconciles when the server disagrees.
//
// The rules are not here. They are minecraft-simulation's, and the whole point
// of this package is that the client and a server run the same ones: what this
// package owns is the fork the prediction is applied to, the choice of which
// movement packet a tick warrants, and what happens when a correction arrives.
//
// The world a prediction runs over is the world the server described. Terrain
// below turns the observed chunks into the tri-state block view the simulation
// reads, so a cell in an unloaded chunk is unknown rather than air — which is
// what makes a tick over unstreamed terrain report itself incomplete instead of
// walking the player through it.
package predict

import (
	"github.com/go-theft-craft/minecraft-protocol/data"
	simgeom "github.com/go-theft-craft/minecraft-simulation/geom"
	simprofile "github.com/go-theft-craft/minecraft-simulation/sim"
	simworld "github.com/go-theft-craft/minecraft-simulation/world"

	"github.com/go-theft-craft/headless-minecraft/world"
)

// Blocks resolves an observed block state to the handle a profile minted for
// it.
//
// The two protocols this client speaks disagree about what a block state is: one
// carries an identifier and four bits of metadata in one number, and the other
// carries the flattened state identifier that replaced them. A resolver is
// therefore per version, and the two constructors below are the two answers.
type Blocks interface {
	// Ref returns the handle for an observed state, or false when the profile
	// does not know it.
	Ref(state uint32) (simworld.BlockRef, bool)
}

// FlattenedBlocks resolves the state identifiers a post-flattening protocol
// carries, where one number names a block and its variant.
//
// It is exactly the identity the profile's own table is keyed by, so the
// resolution is a bounds check.
func FlattenedBlocks(resolve func(data.BlockStateID) (simworld.BlockRef, bool)) Blocks {
	return flattened(resolve)
}

type flattened func(data.BlockStateID) (simworld.BlockRef, bool)

func (f flattened) Ref(state uint32) (simworld.BlockRef, bool) {
	return f(data.BlockStateID(state))
}

// MetadataBlocks resolves the packed identifier a pre-flattening protocol
// carries: the block identifier in the high bits and four bits of metadata in
// the low ones.
//
// The metadata is dropped, because the profile's table for that version is keyed
// by block and its shapes are per block. A variant that collides differently
// from its block — a slab's two halves — is a known limit of that version's
// table rather than of this resolver, and it is recorded there.
func MetadataBlocks(set *data.Set, names simprofile.BlockNames) Blocks {
	return &metadata{set: set, names: names, cache: make(map[uint32]simworld.BlockRef)}
}

type metadata struct {
	set   *data.Set
	names simprofile.BlockNames
	// cache holds one answer per observed state. A tick reads hundreds of cells
	// and a chunk holds thousands, and the lookup behind this is two map reads
	// and a string.
	cache map[uint32]simworld.BlockRef
}

func (m *metadata) Ref(state uint32) (simworld.BlockRef, bool) {
	if ref, ok := m.cache[state]; ok {
		return ref, ref != 0
	}

	block, ok := m.set.Blocks().ByID(data.BlockID(state >> 4))
	if !ok {
		m.cache[state] = 0

		return 0, false
	}

	ref, ok := m.names.Ref(block.Name)
	if !ok {
		m.cache[state] = 0

		return 0, false
	}
	m.cache[state] = ref

	return ref, true
}

// Terrain is the observed world, read as the simulation reads a world.
//
// It answers in three states, which is the distinction the whole design rests
// on: a described block, described air, and a cell nobody has streamed. A
// terrain that answered air for an unloaded chunk would let a prediction walk
// the player through terrain the server has and the client does not.
type Terrain struct {
	chunks  world.ChunksView
	blocks  Blocks
	profile simprofile.Profile
}

// NewTerrain returns the view a tick reads.
func NewTerrain(chunks world.ChunksView, blocks Blocks, profile simprofile.Profile) *Terrain {
	return &Terrain{chunks: chunks, blocks: blocks, profile: profile}
}

// CollisionShape implements the simulation's block view.
func (t *Terrain) CollisionShape(pos simgeom.BlockPos) (simgeom.Shape, simworld.Lookup) {
	ref, lookup := t.BlockState(pos)
	if lookup != simworld.LookupShape {
		return simgeom.EmptyShape(), lookup
	}

	shape, ok := t.profile.Shape(ref)
	if !ok {
		return simgeom.EmptyShape(), simworld.LookupUnknown
	}
	if shape.IsEmpty() {
		return simgeom.EmptyShape(), simworld.LookupAir
	}

	return shape, simworld.LookupShape
}

// BlockState implements the simulation's state view.
func (t *Terrain) BlockState(pos simgeom.BlockPos) (simworld.BlockRef, simworld.Lookup) {
	state, ok := t.chunks.Block(pos.X, pos.Y, pos.Z)
	if !ok {
		// The chunk is not loaded, or its section could not be decoded. Either
		// way nobody has described this cell, and saying so is what makes the
		// tick incomplete rather than wrong.
		return 0, simworld.LookupUnknown
	}

	ref, known := t.blocks.Ref(state)
	if !known {
		// A state the profile has no handle for. It is described and it is not
		// air, so treating it as either would be a guess; the tick stops.
		return 0, simworld.LookupUnknown
	}

	shape, ok := t.profile.Shape(ref)
	if ok && shape.IsEmpty() {
		return ref, simworld.LookupAir
	}

	return ref, simworld.LookupShape
}
