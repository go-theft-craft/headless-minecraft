package main

import (
	"github.com/go-theft-craft/minecraft-protocol/data"
	"github.com/go-theft-craft/minecraft-simulation/terrain"
	simworld "github.com/go-theft-craft/minecraft-simulation/world"

	"github.com/go-theft-craft/headless-minecraft/predict"
)

// Facts tells the planner what a block does to a body standing in it.
//
// Without it the search knows geometry and nothing else, which is a bot that
// walks into lava as readily as onto grass -- it fits, and nothing said not to.
// The planner refuses a cell with any hazard on it outright rather than pricing
// one, so this is the difference between a route that goes round a fire and a
// route through it.
//
// It is a set of handles rather than a lookup by name. A handle is the profile's
// own and comparing two is comparing two integers, where naming a block per cell
// would put a string lookup inside the innermost loop of an A*.
type Facts struct {
	burn    map[simworld.BlockRef]bool
	contact map[simworld.BlockRef]bool
	water   map[simworld.BlockRef]bool
	lava    map[simworld.BlockRef]bool
}

// Hazard implements terrain.Facts.
func (f Facts) Hazard(ref simworld.BlockRef) terrain.Hazard {
	switch {
	case f.burn[ref]:
		return terrain.HazardBurn
	case f.contact[ref]:
		return terrain.HazardContact
	default:
		return terrain.HazardNone
	}
}

// Fluid implements terrain.Facts.
func (f Facts) Fluid(ref simworld.BlockRef) terrain.Fluid {
	switch {
	case f.water[ref]:
		return terrain.FluidWater
	case f.lava[ref]:
		return terrain.FluidLava
	default:
		return terrain.FluidNone
	}
}

// The blocks this example refuses to walk into, by name.
//
// A list, and not a property read off the data set, because the data set has no
// such property: a block record carries hardness, a bounding box and which tool
// mines it, and 26.1's "material" field turned out to name a mining tool rather
// than a substance. Nothing in it says a block burns you. So these are named
// here, which makes the list a claim this example is making rather than a fact
// it measured, and the honest thing is to say so and keep it short.
//
// Short on purpose. Every name here costs the bot ground it will not cross, and
// a bot that refuses too much is trapped by its own caution -- the failure this
// example already has a state for. What is listed is what damages a body simply
// for being in the cell, which is the question terrain.Hazard asks.
//
// Names absent from a version resolve to nothing and are skipped, which is how
// one list serves two versions that disagree: 1.8 has separate flowing fluids
// and none of the blocks added since, and 26.1 has one water block with a level
// property and no flowing variant at all.
var (
	burnBlocks = []string{
		"fire", "soul_fire", "lava", "flowing_lava", "magma_block",
		"campfire", "soul_campfire",
	}
	contactBlocks = []string{
		"cactus", "sweet_berry_bush", "wither_rose", "powder_snow",
	}
	waterBlocks = []string{"water", "flowing_water"}
	lavaBlocks  = []string{"lava", "flowing_lava"}
)

// NewFacts resolves the lists to every state each block has.
//
// Every state, and through the same resolver the terrain reads cells with.
// Both halves of that are the fix for a bot that burned to death on a live
// server with lava already in these lists.
//
// A profile mints one handle per block state, not per block, and the name
// lookup answers with the default state alone. Lava is sixteen states -- one
// per level -- and only the first of them is the source block; the flowing
// edges of a pool are the other fifteen. Facts built from the name therefore
// knew the middle of a lava pool and not its rim, and the bot walked in at the
// edge, which is the only part of a pool anything walks into.
//
// Going through predict.Blocks rather than the profile's name table is what
// keeps this honest: it is the resolver that turns an observed chunk state
// into a handle, so a handle in these sets is a handle a cell can actually
// produce. Two resolvers would be two chances to disagree.
func NewFacts(set *data.Set, blocks predict.Blocks) Facts {
	return Facts{
		burn:    refsOf(set, blocks, burnBlocks),
		contact: refsOf(set, blocks, contactBlocks),
		water:   refsOf(set, blocks, waterBlocks),
		lava:    refsOf(set, blocks, lavaBlocks),
	}
}

// refsOf resolves what it can and quietly drops what it cannot.
func refsOf(set *data.Set, blocks predict.Blocks, names []string) map[simworld.BlockRef]bool {
	refs := make(map[simworld.BlockRef]bool)

	for _, name := range names {
		block, known := set.Blocks().ByName(name)
		if !known {
			continue
		}

		for _, state := range statesOf(block) {
			if ref, ok := blocks.Ref(state); ok {
				refs[ref] = true
			}
		}
	}

	return refs
}

// statesOf lists the chunk states a block appears as.
//
// The two versions describe this differently and the difference is the
// flattening. Java 26.1 gives a block a contiguous range of state identifiers,
// one per combination of its properties. Java 1.8 has no such thing: a chunk
// carries the block identifier shifted left four with four bits of metadata
// under it, so the states are the sixteen values that metadata can take.
func statesOf(block data.Block) []uint32 {
	if block.MaxStateID >= block.MinStateID && block.MaxStateID > 0 {
		states := make([]uint32, 0, block.MaxStateID-block.MinStateID+1)
		for state := block.MinStateID; state <= block.MaxStateID; state++ {
			states = append(states, uint32(state))
		}

		return states
	}

	states := make([]uint32, 0, 16)
	for meta := range uint32(16) {
		states = append(states, uint32(block.ID)<<4|meta)
	}

	return states
}
