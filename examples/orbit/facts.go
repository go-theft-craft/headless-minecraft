package main

import (
	simprofile "github.com/go-theft-craft/minecraft-simulation/sim"
	"github.com/go-theft-craft/minecraft-simulation/terrain"
	simworld "github.com/go-theft-craft/minecraft-simulation/world"
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

// NewFacts resolves the lists against a profile.
//
// A profile that cannot name blocks gets empty facts rather than an error. The
// consequence is a bot that routes on geometry alone, which is what it did
// before any of this existed, and refusing to start over it would be trading a
// working bot for a decorated one.
func NewFacts(profile simprofile.Profile) Facts {
	names, ok := profile.(simprofile.BlockNames)
	if !ok {
		return Facts{}
	}

	return Facts{
		burn:    refsOf(names, burnBlocks),
		contact: refsOf(names, contactBlocks),
		water:   refsOf(names, waterBlocks),
		lava:    refsOf(names, lavaBlocks),
	}
}

// refsOf resolves what it can and quietly drops what it cannot.
func refsOf(names simprofile.BlockNames, blocks []string) map[simworld.BlockRef]bool {
	refs := make(map[simworld.BlockRef]bool, len(blocks))
	for _, name := range blocks {
		if ref, ok := names.Ref(name); ok {
			refs[ref] = true
		}
	}

	return refs
}
