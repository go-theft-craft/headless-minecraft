package main

import (
	simgeom "github.com/go-theft-craft/minecraft-simulation/geom"
	"github.com/go-theft-craft/minecraft-simulation/terrain"
	simworld "github.com/go-theft-craft/minecraft-simulation/world"

	"github.com/go-theft-craft/headless-minecraft/world"
)

// buffered is a world view that reports a cell next to something harmful as
// though it held that harmful thing.
//
// It exists because the planner and the bot measure the same route from
// different places, and the two used to disagree about a lava pool until the bot
// stopped moving.
//
// The planner works in cells and asks about one: a body 0.6 wide standing on a
// cell centre spans the middle six tenths of it and leans into nothing, so
// asking about the centre cell is sound for a route walked centre to centre. The
// bot does not walk centre to centre. It shortcuts, and it arrives within a
// tolerance rather than exactly, so the box it really occupies can poke a few
// centimetres into the next cell — and its own last-line guard measures that box.
// A route the planner called safe was refused by the guard, dropped, planned
// again identically, and refused again, twenty times a second, until the
// no-progress timer skipped the waypoint. Neither check was wrong. They were
// measuring the same line from different places.
//
// Widening the box the checks use closes the gap from one side and opens it from
// the other: the guard stops firing and the lookahead starts, because the
// planner still has no margin. So the margin goes where the disagreement starts.
// A cell beside lava is reported as lava, the planner routes around it, and every
// check downstream agrees because there is nothing marginal left to disagree
// about.
//
// It dilates harm only. Water is left alone deliberately: swimming is a thing
// this bot does on purpose, and a pool reported one cell wider than it is would
// make a body that can swim refuse the shore.
type buffered struct {
	base  simworld.View
	facts terrain.Facts
}

// newBuffered wraps a view so that harm occupies one more cell in every
// horizontal direction.
func newBuffered(base simworld.View, facts terrain.Facts) buffered {
	return buffered{base: base, facts: facts}
}

// CollisionShape implements world.View unchanged.
//
// Only what a cell means is dilated, never what shape it has. A cell beside lava
// is still air to walk through and still air to fall through; it is only a cell
// the planner should not choose.
func (b buffered) CollisionShape(pos simgeom.BlockPos) (simgeom.Shape, simworld.Lookup) {
	return b.base.CollisionShape(pos)
}

// BlockState implements world.View, answering with a harmful neighbour's block
// where there is one.
//
// Substituting the neighbour's own handle rather than inventing one is what
// keeps this honest: everything downstream resolves it through the same facts,
// so a cell beside fire reports fire and a cell beside cactus reports cactus,
// and nothing has to be taught a new kind of answer.
func (b buffered) BlockState(pos simgeom.BlockPos) (simworld.BlockRef, simworld.Lookup) {
	ref, lookup := b.base.BlockState(pos)
	if lookup == simworld.LookupUnknown {
		return ref, lookup
	}
	if b.facts != nil && b.facts.Hazard(ref) != terrain.HazardNone {
		return ref, lookup
	}

	// The four horizontal neighbours, and only those. Harm above or below is
	// not something a step into this cell walks into, and dilating vertically
	// would refuse the ground over a buried pool.
	for _, side := range [4]simgeom.BlockPos{
		{X: 1}, {X: -1}, {Z: 1}, {Z: -1},
	} {
		beside := simgeom.BlockPos{X: pos.X + side.X, Y: pos.Y, Z: pos.Z + side.Z}

		neighbour, found := b.base.BlockState(beside)
		if found == simworld.LookupUnknown {
			continue
		}
		if b.facts != nil && b.facts.Hazard(neighbour) != terrain.HazardNone {
			return neighbour, found
		}
	}

	return ref, lookup
}

// planningView is the world the planner and every check that validates a route
// read: the observed terrain with harm dilated by a cell.
func (n Navigator) planningView(chunks world.ChunksView) simworld.View {
	return newBuffered(n.terrainView(chunks), n.facts)
}
