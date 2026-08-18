package main

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/go-theft-craft/minecraft-protocol/data"
	gen1_8 "github.com/go-theft-craft/minecraft-protocol/generated/java/v1_8"
	gen26_1 "github.com/go-theft-craft/minecraft-protocol/generated/java/v26_1"
	simentity "github.com/go-theft-craft/minecraft-simulation/entity"
	simgeom "github.com/go-theft-craft/minecraft-simulation/geom"
	simmovement "github.com/go-theft-craft/minecraft-simulation/movement"
	"github.com/go-theft-craft/minecraft-simulation/navigation"
	simv1_8 "github.com/go-theft-craft/minecraft-simulation/profile/java/v1_8"
	simv26_1 "github.com/go-theft-craft/minecraft-simulation/profile/java/v26_1"
	simprofile "github.com/go-theft-craft/minecraft-simulation/sim"
	"github.com/go-theft-craft/minecraft-simulation/terrain"
	simworld "github.com/go-theft-craft/minecraft-simulation/world"

	"github.com/go-theft-craft/headless-minecraft/predict"
	"github.com/go-theft-craft/headless-minecraft/world"
)

// This file is the routing half of the seam, and it is deliberately thin.
//
// The example used to carry its own passability rules and a radial-band search
// that was explicitly not a pathfinder. Both are gone: minecraft-simulation
// owns the body, the terrain predicates, and an A* over them, and every one of
// those is better than what a worked example should be reimplementing. What is
// left here is the translation -- observed chunks in, positions to walk out --
// and the numbers this bot chooses to search with.

// errNoBody and errNoBlockNames are the two ways a version can fail to
// describe itself well enough to route over.
var (
	errNoBody       = errors.New("the profile could not spawn a body to measure")
	errNoBlockNames = errors.New("the profile cannot resolve block names")
)

// Route is a way from here to somewhere, as the positions to walk through.
//
// It is positions rather than the planner's typed edges because that is all the
// core does with it. Keeping navigation.Path out of the core is what lets the
// decision tests run without a world: a scripted router returns three points
// and the state machine cannot tell the difference.
type Route struct {
	// Steps are the positions to walk through, in order, each at the centre of
	// the cell the planner routed into.
	Steps []Vec3
	// Complete reports that the steps reach the goal. An incomplete route is
	// still worth walking -- the planner returns one on purpose, because a bot
	// that covers most of the ground and searches again beats one that refuses
	// to move -- and the caller replans when it runs out.
	Complete bool
}

// Navigator plans routes over the observed world.
//
// One per connection, because the profile and the block resolver are built
// once and the body is derived from them. The snapshot is not held here: it
// arrives with each call, so a route is always planned against the terrain the
// tick read rather than whatever was loaded when the navigator was made.
type Navigator struct {
	blocks     predict.Blocks
	profile    simprofile.Profile
	facts      Facts
	capability navigation.Capability
	budget     navigation.Budget
}

// Plan searches a route between two positions over one snapshot.
//
// The context bounds the search rather than the run: A* over a streamed world
// is the one thing in this tick that can take unbounded time, and the budget
// below bounds it by nodes as well.
func (n Navigator) Plan(ctx context.Context, chunks world.ChunksView, from, to Vec3) (Route, bool) {
	view := predict.NewTerrain(chunks, n.blocks, n.profile)

	path, err := navigation.Find(
		ctx, view, n.facts, n.capability, cellOf(from), cellOf(to), n.budget,
	)
	if err != nil || len(path.Edges) == 0 {
		return Route{}, false
	}

	// An incomplete path is worth walking when the search ran out of room to
	// look, and worthless when it looked everywhere and found no way. The
	// planner tells the two apart and this used to ignore the difference: a
	// waypoint behind a lava pool produced a stub of a route heading at the
	// pool, the bot walked the stub, planned again, got another stub, and
	// spent the run shuffling at the water's edge looking like it had stopped.
	//
	// Unreachable means skip the waypoint and try the next one. Budget and
	// ceiling mean walk what there is and search again from further on, which
	// is the case the planner returns partial paths for.
	if !path.Complete && path.Reason == navigation.ReasonUnreachable {
		return Route{}, false
	}

	// Start from where the bot is, then the centre of the cell it stands in,
	// then a centre per edge.
	//
	// The centre matters because the planner emits no diagonals on purpose --
	// the comment on its step table says a corner-cutting rule got wrong walks
	// a body through the gap between two blocks -- and that guarantee only
	// holds between cell centres. A bot standing off-centre and heading for
	// the next centre moves diagonally whatever the planner said.
	steps := make([]Vec3, 0, len(path.Edges)+2)
	steps = append(steps, from, centreOf(from))

	for _, edge := range path.Edges {
		feet := terrain.FeetOf(edge.To)
		steps = append(steps, Vec3{X: feet.X, Y: feet.Y, Z: feet.Z})
	}

	// Then pull the string taut. The search walks a grid four directions at a
	// time, so its route is a staircase and a bot following it turns ninety
	// degrees a dozen times crossing an open field -- which is exactly what it
	// looks like: a machine indexing along a lattice, not somebody walking. A
	// shortcut is only taken where the body has been checked along the whole
	// straight line of it, so the corners this cuts are corners it has looked
	// at, which is the difference between smoothing and guessing.
	// Facts here as well as in the search. A shortcut is only allowed where the
	// body has been checked along the line, and a check that knew nothing about
	// fire would pull a route taut straight through the thing the search went
	// round.
	steps = n.taut(terrain.Query{
		View:  view,
		Facts: n.facts,
		Body:  n.capability.Body,
		Limit: n.capability.CandidateLimit,
	}, steps)

	// The bot is standing on the first point, so it is not a step.
	return Route{Steps: steps[1:], Complete: path.Complete}, true
}

// taut removes every point the body can walk straight past.
//
// Greedy from each kept point: take the furthest later point the body can
// reach in a straight line, and drop everything between. Points at a different
// height are never merged through -- a rise or a drop is a place the bot has
// to arrive at before the next move makes sense, and this example has no body
// that could jump the difference anyway.
func (n Navigator) taut(query terrain.Query, steps []Vec3) []Vec3 {
	if len(steps) < 3 {
		return steps
	}

	taut := make([]Vec3, 0, len(steps))
	taut = append(taut, steps[0])

	for i := 0; i < len(steps)-1; {
		furthest := i + 1
		for j := len(steps) - 1; j > i+1; j-- {
			if steps[j].Y == steps[i].Y && n.clearLine(query, steps[i], steps[j]) {
				furthest = j

				break
			}
		}

		taut = append(taut, steps[furthest])
		i = furthest
	}

	return taut
}

// clearLine reports whether the body can walk the straight line between two
// points.
//
// Sampled rather than swept. A sweep of a box along a line is what collision
// does properly and it is not this example's to write; sampling every fifth of
// a block is finer than the body is wide, so nothing one block across fits
// between two samples unnoticed.
func (n Navigator) clearLine(query terrain.Query, from, to Vec3) bool {
	distance := from.HorizontalDistance(to)

	for travelled := 0.0; travelled <= distance; travelled += lineProbe {
		if !n.standable(query, from.Toward(to, travelled)) {
			return false
		}
	}

	return n.standable(query, to)
}

// standable reports whether the body fits at a position, with something holding
// it up and nothing there that harms it.
//
// All three, because Fits and Ground answer about geometry alone and a body
// fits inside a fire perfectly well. The search refuses a cell carrying any
// hazard and refuses water to a body that cannot swim; a shortcut is only
// honest if it asks the same questions, or the smoothing walks the bot through
// exactly what the search routed around.
func (n Navigator) standable(query terrain.Query, at Vec3) bool {
	feet := simgeom.Vec3{X: at.X, Y: at.Y, Z: at.Z}

	fit, err := query.Fits(feet)
	if err != nil || fit != terrain.FitClear {
		return false
	}

	ground, err := query.Ground(feet)
	if err != nil || ground != terrain.GroundSolid {
		return false
	}

	// Every cell the body covers, not the one its centre falls in.
	//
	// A body 0.6 wide standing on a cell centre stays inside that cell, which
	// is why the search gets away with asking about one: it moves centre to
	// centre along an axis and never leans into a neighbour. Smoothing breaks
	// that. A shortcut is a diagonal, a body on a diagonal straddles up to
	// four cells, and asking only about the middle one lets the route skim the
	// corner of a cell it would never have been allowed to stand in. Lava does
	// not care that the bot's centre was somewhere safe -- this is a bot that
	// caught fire walking past the corner of a lava pool.
	for _, cell := range n.bodyCells(at) {
		hazard, lookup, err := query.HazardAt(cell)
		if err != nil || lookup == simworld.LookupUnknown || hazard != terrain.HazardNone {
			return false
		}

		fluid, lookup, err := query.FluidAt(cell)
		if err != nil || lookup == simworld.LookupUnknown {
			return false
		}
		if fluid == terrain.FluidLava {
			return false
		}
		if fluid == terrain.FluidWater && !n.capability.CanSwim {
			return false
		}
	}

	return true
}

// bodyCells returns the cells the body occupies at a position: the one to four
// columns its box covers, at the height of its feet and of its head.
//
// The head layer matters as much as the feet. Fire fills a cell and a body two
// blocks tall stands in two of them, so a check that looked only underfoot
// would walk the bot upright through a fire burning at chest height.
func (n Navigator) bodyCells(at Vec3) []simgeom.BlockPos {
	half := n.capability.Body.HalfWidth
	feet := at.Floor()

	// One or two per axis, depending on whether the box straddles a boundary.
	xs := axisCells(at.X, half)
	zs := axisCells(at.Z, half)
	layers := int(math.Ceil(n.capability.Body.Height))
	if layers < 1 {
		layers = 1
	}

	cells := make([]simgeom.BlockPos, 0, len(xs)*len(zs)*layers)
	for layer := range layers {
		for _, x := range xs {
			for _, z := range zs {
				cells = append(cells, simgeom.BlockPos{
					X: int32(x), Y: int32(feet.Y + layer), Z: int32(z),
				})
			}
		}
	}

	return cells
}

// axisCells returns the one or two cell coordinates one horizontal axis of the
// body covers.
func axisCells(centre, half float64) []int {
	low, high := int(math.Floor(centre-half)), int(math.Floor(centre+half))
	if low == high {
		return []int{low}
	}

	return []int{low, high}
}

// lineProbe is how finely a straight line is sampled, in blocks. A fifth of a
// block is one movement update and well under the body's width.
const lineProbe = 0.2

// centreOf is the middle of the cell a position stands in, at the position's
// own height.
func centreOf(p Vec3) Vec3 {
	block := p.Floor()

	return Vec3{X: float64(block.X) + 0.5, Y: p.Y, Z: float64(block.Z) + 0.5}
}

// Walkable reports whether the body can still walk the straight line between
// two positions over this snapshot.
//
// It is the same test a shortcut is judged by, offered on its own so a bot part
// way along a route can ask whether the ground it is about to cross is still
// the ground it planned across. Lava poured in front of a walking bot changes
// nothing about the route it already holds.
func (n Navigator) Walkable(chunks world.ChunksView, from, to Vec3) bool {
	view := predict.NewTerrain(chunks, n.blocks, n.profile)

	return n.clearLine(terrain.Query{
		View:  view,
		Facts: n.facts,
		Body:  n.capability.Body,
		Limit: n.capability.CandidateLimit,
	}, from, to)
}

// Hurting reports whether the body at a position is standing in something that
// damages it.
//
// Deliberately narrower than Walkable, which answers false for a great many
// reasons: a wall, a hole, an unstreamed chunk. Unknown ground is not ground
// that hurts, and a bot that treated the two alike would decide it was on fire
// every time it walked to the edge of what the server has sent it.
func (n Navigator) Hurting(chunks world.ChunksView, at Vec3) bool {
	view := predict.NewTerrain(chunks, n.blocks, n.profile)
	query := terrain.Query{View: view, Facts: n.facts, Body: n.capability.Body}

	for _, cell := range n.bodyCells(at) {
		hazard, lookup, err := query.HazardAt(cell)
		if err == nil && lookup != simworld.LookupUnknown && hazard != terrain.HazardNone {
			return true
		}

		fluid, lookup, err := query.FluidAt(cell)
		if err == nil && lookup != simworld.LookupUnknown && fluid == terrain.FluidLava {
			return true
		}
	}

	return false
}

// Safe finds the nearest cell within a radius that does not hurt to stand in.
//
// For a bot that is already standing in something. The way out of a pool is
// toward its nearest edge, and which edge that is depends on where in the pool
// the bot is -- a fixed direction walks half of them deeper in.
//
// Unknown ground does not count as safe. It might be, and a bot fleeing lava
// into a chunk nobody has described has swapped a known problem for one it
// cannot see.
func (n Navigator) Safe(chunks world.ChunksView, from Vec3, within int) (Vec3, bool) {
	view := predict.NewTerrain(chunks, n.blocks, n.profile)
	query := terrain.Query{View: view, Facts: n.facts, Body: n.capability.Body}
	centre := cellOf(from)

	for radius := range within + 1 {
		var nearest simgeom.BlockPos
		found := false

		for _, cell := range ring(centre, radius) {
			if cell.Y != centre.Y || !n.restful(query, cell) {
				continue
			}
			if !found || closer(cell, nearest, centre) {
				nearest, found = cell, true
			}
		}

		if found {
			feet := terrain.FeetOf(nearest)

			return Vec3{X: feet.X, Y: feet.Y, Z: feet.Z}, true
		}
	}

	return Vec3{}, false
}

// restful reports whether a cell is somewhere the body can stand without being
// hurt, with the ground known and solid under it.
func (n Navigator) restful(query terrain.Query, cell simgeom.BlockPos) bool {
	feet := terrain.FeetOf(cell)

	fit, err := query.Fits(feet)
	if err != nil || fit != terrain.FitClear {
		return false
	}

	ground, err := query.Ground(feet)
	if err != nil || ground != terrain.GroundSolid {
		return false
	}

	hazard, lookup, err := query.HazardAt(cell)
	if err != nil || lookup == simworld.LookupUnknown || hazard != terrain.HazardNone {
		return false
	}

	fluid, lookup, err := query.FluidAt(cell)

	return err == nil && lookup != simworld.LookupUnknown && fluid == terrain.FluidNone
}

// Water finds the nearest cell of water the body could stand in, within a
// radius, or reports that there is none.
//
// A ring search outward from the bot rather than a scan of the whole cube: a
// burning bot wants the nearest water and wants it this tick, and the nearest
// is almost always a few blocks off or nowhere at all. Stopping at the first
// ring that has one keeps the common answer cheap and the empty answer bounded.
//
// It searches the layer the bot stands on and the one below. Water a body can
// step into is water at its feet; water further down is a hole to fall into,
// which this bot has no business with -- it cannot fall.
func (n Navigator) Water(chunks world.ChunksView, from Vec3, within int) (Vec3, bool) {
	view := predict.NewTerrain(chunks, n.blocks, n.profile)
	query := terrain.Query{View: view, Facts: n.facts, Body: n.capability.Body}
	centre := cellOf(from)

	for radius := range within + 1 {
		var nearest simgeom.BlockPos
		found := false

		for _, cell := range ring(centre, radius) {
			fluid, lookup, err := query.FluidAt(cell)
			if err != nil || lookup == simworld.LookupUnknown || fluid != terrain.FluidWater {
				continue
			}
			if !found || closer(cell, nearest, centre) {
				nearest, found = cell, true
			}
		}

		if found {
			feet := terrain.FeetOf(nearest)

			return Vec3{X: feet.X, Y: feet.Y, Z: feet.Z}, true
		}
	}

	return Vec3{}, false
}

// ring returns the cells whose Chebyshev distance from a centre is exactly the
// radius, on the centre's layer and the one below it.
func ring(centre simgeom.BlockPos, radius int) []simgeom.BlockPos {
	if radius == 0 {
		return []simgeom.BlockPos{centre, {X: centre.X, Y: centre.Y - 1, Z: centre.Z}}
	}

	cells := make([]simgeom.BlockPos, 0, 16*radius)
	for dx := -radius; dx <= radius; dx++ {
		for dz := -radius; dz <= radius; dz++ {
			if abs(dx) != radius && abs(dz) != radius {
				continue
			}
			for _, dy := range []int{0, -1} {
				cells = append(cells, simgeom.BlockPos{
					X: centre.X + int32(dx), Y: centre.Y + int32(dy), Z: centre.Z + int32(dz),
				})
			}
		}
	}

	return cells
}

// closer reports whether one cell is nearer a centre than another, squared and
// horizontal, which is all the ordering inside one ring needs.
func closer(a, b, centre simgeom.BlockPos) bool {
	return squared(a, centre) < squared(b, centre)
}

func squared(a, b simgeom.BlockPos) int32 {
	dx, dz := a.X-b.X, a.Z-b.Z

	return dx*dx + dz*dz
}

func abs(n int) int {
	if n < 0 {
		return -n
	}

	return n
}

// cellOf is the block a position stands in, in the simulation's terms.
func cellOf(p Vec3) simgeom.BlockPos {
	block := p.Floor()

	return simgeom.BlockPos{X: int32(block.X), Y: int32(block.Y), Z: int32(block.Z)}
}

// NewNavigator builds the planner for a version.
//
// The body is read from the profile rather than declared here. A player's box
// is 0.6 by 1.8 in both versions this speaks, but it is 0.6 by 1.8 as the game
// builds it -- halved in float arithmetic and added to a double -- and a
// literal written here would be a different box in its last bits from the one
// the server collides. Spawning a state and measuring its box is how the
// example gets the server's answer instead of its own.
func NewNavigator(legacy bool, bounds Bounds) (Navigator, error) {
	profile, blocks, set, err := versionTerrain(legacy)
	if err != nil {
		return Navigator{}, err
	}

	state, _, ok := spawnState(legacy, profile)
	if !ok {
		return Navigator{}, errNoBody
	}

	body := terrain.Body{
		HalfWidth: (state.Box.MaxX - state.Box.MinX) / 2,
		Height:    state.Box.MaxY - state.Box.MinY,
		// Zero, and not the profile's own step height.
		//
		// The box is measured off the game because the game is right about it.
		// The step height is not a fact about the player, it is a claim about
		// this bot, and the claim would be false: the example reports a
		// position and simulates no body, so it has nothing that rises. Passing
		// the profile's 0.6 told the planner it could route a one-block step,
		// the planner did, and the bot walked horizontally into the raised
		// block and was pushed back until the breaker ended the run -- a cell
		// the planner called steppable and the server called a wall.
		//
		// It goes back to the profile's value the day a movement kernel is
		// attached, and not before. Describing a body with legs to a planner
		// bolted to a bot without them is how a route becomes a lie.
		StepHeight: 0,
	}

	return Navigator{
		blocks:  blocks,
		profile: profile,
		facts:   NewFacts(set, blocks),
		budget:  navigation.Budget{Nodes: routeNodes},
		capability: navigation.Capability{
			Body: body,
			// It cannot fall either, for the same reason it cannot step: a
			// drop is something a body does under gravity, and this one has
			// none, so a route down a ledge would have it walking out over
			// the edge and reporting itself still on the ground. Nor does it
			// swim. Both are the shape of this bot rather than facts about
			// the game.
			SafeFall: 0,
			CanSwim:  false,
			// Costs in ticks, from the speed this bot walks at. One block at
			// WalkSpeed blocks a second takes 20/WalkSpeed ticks, and a step
			// up and a drop are charged the same because this bot has no
			// physics to make either slower.
			WalkTicks: ticksPerBlock(bounds),
			StepTicks: ticksPerBlock(bounds),
			FallTicks: ticksPerBlock(bounds),
			SwimTicks: ticksPerBlock(bounds),
		},
	}, nil
}

// ticksPerBlock is how long this bot takes to cross one block, in ticks.
func ticksPerBlock(bounds Bounds) float64 { return 1 / bounds.Step() }

// routeNodes bounds one search. The circle's waypoints are about five blocks
// apart, so a route between two of them is short; this is loose enough for a
// long detour around a building and tight enough that a search over open
// unstreamed world gives up inside a tick.
const routeNodes = 4096

// spawnState builds one body of this version, only to measure it.
func spawnState(legacy bool, profile simprofile.Profile) (simentity.State, simmovement.Locomotion, bool) {
	if legacy {
		return simv1_8.Spawn(profile, simgeom.Vec3{}, 0, 0)
	}

	return simv26_1.Spawn(profile, simgeom.Vec3{}, 0, 0)
}

// versionTerrain builds the profile and the block resolver for a version.
//
// The two protocols disagree about what a block state is, which is the whole
// reason predict ships two resolvers: 47 packs a block identifier and four bits
// of metadata into one number, and 775 carries the flattened state identifier
// the profile's own table is keyed by.
func versionTerrain(legacy bool) (simprofile.Profile, predict.Blocks, *data.Set, error) {
	if legacy {
		set, err := gen1_8.Data()
		if err != nil {
			return nil, nil, nil, fmt.Errorf("load the 1.8.9 data set: %w", err)
		}
		profile, err := simv1_8.New(set)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("build the 1.8.9 profile: %w", err)
		}
		names, ok := profile.(simprofile.BlockNames)
		if !ok {
			return nil, nil, nil, errNoBlockNames
		}

		return profile, predict.MetadataBlocks(set, names), set, nil
	}

	set, err := gen26_1.Data()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("load the 26.1 data set: %w", err)
	}
	profile, err := simv26_1.New(set)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("build the 26.1 profile: %w", err)
	}

	return profile, predict.FlattenedBlocks(func(state data.BlockStateID) (simworld.BlockRef, bool) {
		return simv26_1.RefState(profile, state)
	}), set, nil
}
