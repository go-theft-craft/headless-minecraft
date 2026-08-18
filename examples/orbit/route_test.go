package main

import (
	"testing"

	simgeom "github.com/go-theft-craft/minecraft-simulation/geom"
	"github.com/go-theft-craft/minecraft-simulation/navigation"
	"github.com/go-theft-craft/minecraft-simulation/terrain"
	simworld "github.com/go-theft-craft/minecraft-simulation/world"
)

// slab is a view with a floor at y=63 and whatever walls a test puts above it.
type slab struct{ solid map[simgeom.BlockPos]bool }

func newSlab() *slab { return &slab{solid: map[simgeom.BlockPos]bool{}} }

func (s *slab) filled(pos simgeom.BlockPos) bool { return pos.Y < 64 || s.solid[pos] }

func (s *slab) CollisionShape(pos simgeom.BlockPos) (simgeom.Shape, simworld.Lookup) {
	if s.filled(pos) {
		return simgeom.FullCube(), simworld.LookupShape
	}

	return simgeom.EmptyShape(), simworld.LookupAir
}

func (s *slab) BlockState(pos simgeom.BlockPos) (simworld.BlockRef, simworld.Lookup) {
	if s.filled(pos) {
		return 1, simworld.LookupShape
	}

	return 0, simworld.LookupAir
}

func query(view *slab) terrain.Query {
	return terrain.Query{
		View: view,
		Body: terrain.Body{HalfWidth: 0.3, Height: 1.8, StepHeight: 0.6},
	}
}

// TestATautRouteCrossesOpenGroundInOneStep pins the smoothing.
//
// The planner walks a grid four directions at a time, so its route across open
// ground is a staircase and a bot following it turns ninety degrees a dozen
// times to cross a field. Nothing is in the way of the diagonal, and a bot that
// takes the staircase anyway does not look like it is walking.
func TestATautRouteCrossesOpenGroundInOneStep(t *testing.T) {
	t.Parallel()

	var navigator Navigator
	staircase := []simgeom.Vec3{
		{X: 0.5, Y: 64, Z: 0.5},
		{X: 1.5, Y: 64, Z: 0.5},
		{X: 1.5, Y: 64, Z: 1.5},
		{X: 2.5, Y: 64, Z: 1.5},
		{X: 2.5, Y: 64, Z: 2.5},
		{X: 3.5, Y: 64, Z: 2.5},
		{X: 3.5, Y: 64, Z: 3.5},
	}

	taut := navigator.taut(query(newSlab()), staircase)

	if len(taut) != 2 {
		t.Fatalf("pulled %d points taut, want the two ends: %+v", len(taut), taut)
	}
	if taut[0] != staircase[0] || taut[1] != staircase[len(staircase)-1] {
		t.Errorf("taut route is %+v, want the two ends of %+v", taut, staircase)
	}
}

// TestATautRouteKeepsTheCornerItHasToWalkRound pins that smoothing is checked
// rather than assumed. Cutting a corner the body does not fit through is the
// defect the whole switch to a planner was meant to end, and a smoother that
// shortened every route would reintroduce it in one line.
func TestATautRouteKeepsTheCornerItHasToWalkRound(t *testing.T) {
	t.Parallel()

	view := newSlab()
	// A wall down the diagonal's path, leaving the staircase as the only way.
	for _, y := range []int32{64, 65} {
		view.solid[simgeom.BlockPos{X: 2, Y: y, Z: 1}] = true
		view.solid[simgeom.BlockPos{X: 1, Y: y, Z: 2}] = true
	}

	var navigator Navigator
	corner := []simgeom.Vec3{
		{X: 0.5, Y: 64, Z: 0.5},
		{X: 1.5, Y: 64, Z: 0.5},
		{X: 1.5, Y: 64, Z: 1.5},
		{X: 2.5, Y: 64, Z: 1.5},
		{X: 2.5, Y: 64, Z: 2.5},
	}

	taut := navigator.taut(query(view), corner)

	if len(taut) < 3 {
		t.Fatalf("smoothed a corner the body cannot cut: %+v", taut)
	}
	if taut[len(taut)-1] != corner[len(corner)-1] {
		t.Errorf("taut route ends at %+v, want %+v", taut[len(taut)-1], corner[len(corner)-1])
	}
}

// TestARiseIsNeverSmoothedThrough pins that a change of height stays a point
// the bot arrives at. This example has no body that jumps, so a shortcut over
// a step is a shortcut through the block making it.
func TestARiseIsNeverSmoothedThrough(t *testing.T) {
	t.Parallel()

	var navigator Navigator
	rise := []simgeom.Vec3{
		{X: 0.5, Y: 64, Z: 0.5},
		{X: 1.5, Y: 64, Z: 0.5},
		{X: 2.5, Y: 65, Z: 0.5},
		{X: 3.5, Y: 65, Z: 0.5},
	}

	taut := navigator.taut(query(newSlab()), rise)

	for _, point := range taut {
		if point.Y == 65 {
			return
		}
	}
	t.Errorf("smoothed straight over the rise: %+v", taut)
}

// TestThePlannerIsNotToldTheBotCanStepOrFall pins the shape of the body the
// search routes for.
//
// The box is measured off the game, because the game is right about it. The
// step height and the safe fall are not facts about a player, they are claims
// about this bot, and both would be false: it reports a position and simulates
// no body, so it has nothing that rises and nothing that falls. Handing the
// planner the profile's own step height told it a one-block rise was routable,
// it routed one, and the bot walked horizontally into the raised block and was
// pushed back until the correction breaker ended the run.
//
// This goes back to the profile's values the day a movement kernel is attached,
// and the test is what will say so.
func TestThePlannerIsNotToldTheBotCanStepOrFall(t *testing.T) {
	t.Parallel()

	navigator, err := NewNavigator(false, DefaultBounds())
	if err != nil {
		t.Fatalf("NewNavigator: %v", err)
	}

	if got := navigator.capability.Body.StepHeight; got != 0 {
		t.Errorf("step height is %v, want 0: this bot has nothing that rises", got)
	}
	if got := navigator.capability.SafeFall; got != 0 {
		t.Errorf("safe fall is %v, want 0: this bot has nothing that falls", got)
	}
	// The box is the game's, though, and has to stay that way.
	if got := navigator.capability.Body.HalfWidth; got <= 0.29 || got >= 0.31 {
		t.Errorf("half width is %v, want the player's 0.3", got)
	}
	if got := navigator.capability.Body.Height; got <= 1.79 || got >= 1.81 {
		t.Errorf("height is %v, want the player's 1.8", got)
	}
}

// waterSlab is a floor with a pool of water sitting on it.
//
// Water carries no collision shape, so it is reported as air with a handle the
// facts call water — which is the whole reason terrain needs Facts at all: a
// body fits inside water perfectly well, and geometry cannot tell a pool from
// an empty room.
type waterSlab struct {
	*slab
	wet map[simgeom.BlockPos]bool
}

const waterRef simworld.BlockRef = 77

func (w waterSlab) BlockState(pos simgeom.BlockPos) (simworld.BlockRef, simworld.Lookup) {
	if w.wet[pos] {
		return waterRef, simworld.LookupAir
	}

	return w.slab.BlockState(pos)
}

// pool returns a view with a square of water centred on the origin cell, and
// the cell the bot stands in at the middle of it.
func pool(radius int32) waterSlab {
	wet := map[simgeom.BlockPos]bool{}
	for x := -radius; x <= radius; x++ {
		for z := -radius; z <= radius; z++ {
			wet[simgeom.BlockPos{X: x, Y: 64, Z: z}] = true
		}
	}

	return waterSlab{slab: newSlab(), wet: wet}
}

// TestABotInWaterFindsAWayOut is the regression test for a bot that stood in a
// pool until the run ended.
//
// Dousing walks the bot into water on purpose, to put a fire out. With swimming
// refused, every cell around it in the middle of a pool was refused too, the
// frontier emptied, and the route came back unreachable — so the thing the bot
// had deliberately walked into was a thing it could not walk out of.
//
// Both halves are asserted, because only the pair says the swim is what fixed
// it: the same world, the same start, the same goal, and the one field between
// them.
func TestABotInWaterFindsAWayOut(t *testing.T) {
	t.Parallel()

	navigator, err := NewNavigator(false, DefaultBounds())
	if err != nil {
		t.Fatalf("NewNavigator: %v", err)
	}

	view := pool(2)
	facts := Facts{water: map[simworld.BlockRef]bool{waterRef: true}}

	// The middle of the pool, out to dry ground past its edge.
	from := simgeom.BlockPos{X: 0, Y: 64, Z: 0}
	goal := simgeom.BlockPos{X: 4, Y: 64, Z: 0}
	budget := navigation.Budget{Nodes: routeNodes}

	swimmer := navigator.capability
	if !swimmer.CanSwim {
		t.Fatal("the shipped capability cannot swim, so this test proves nothing")
	}

	out, err := navigation.Find(t.Context(), view, facts, swimmer, from, goal, budget)
	if err != nil {
		t.Fatalf("Find with a swim: %v", err)
	}
	if !out.Complete {
		t.Fatalf("a bot in the middle of a pool found no way out: %v", out.Reason)
	}

	var swum int
	for _, edge := range out.Edges {
		if edge.Kind == navigation.EdgeSwim {
			swum++
		}
	}
	if swum == 0 {
		t.Fatalf("left the pool without a swim edge: %v", out.Edges)
	}

	// The same world with swimming taken away, which is what the bot shipped
	// with and what stranded it.
	stuck := swimmer
	stuck.CanSwim = false

	trapped, err := navigation.Find(t.Context(), view, facts, stuck, from, goal, budget)
	if err != nil {
		t.Fatalf("Find without a swim: %v", err)
	}
	if trapped.Complete {
		t.Fatal("a bot that cannot swim crossed a pool; the fix is not what made the difference")
	}
}

// TestWaterCostsMoreThanDryGround pins that swimming is priced above walking.
//
// Making water passable is not the same as making it attractive. A planner that
// charged the same for both would send the bot straight through a pond it could
// have walked around, which is slower in the game and looks like a bot that
// cannot tell water from grass.
func TestWaterCostsMoreThanDryGround(t *testing.T) {
	t.Parallel()

	navigator, err := NewNavigator(false, DefaultBounds())
	if err != nil {
		t.Fatalf("NewNavigator: %v", err)
	}

	capability := navigator.capability
	if capability.SwimTicks <= capability.WalkTicks {
		t.Errorf("a block of water costs %v and a block of ground costs %v; water should cost more",
			capability.SwimTicks, capability.WalkTicks)
	}
	if capability.SneakTicks <= capability.WalkTicks {
		t.Errorf("crossing a ledge costs %v and open ground costs %v; the ledge should cost more",
			capability.SneakTicks, capability.WalkTicks)
	}
}

// TestABotRoutesRoundAPoolItCouldWalkAround is the other half of the pricing.
//
// The pool has to be long for the detour to win, and that is the pricing working
// rather than a quirk of the fixture. One block of water costs one extra walk to
// cross and two extra walks to go around, so a puddle is correctly swum — a
// planner that waded round every wet cell would be as wrong as one that swam
// every lake. It is the fifth block of water that makes the long way cheaper.
func TestABotRoutesRoundAPoolItCouldWalkAround(t *testing.T) {
	t.Parallel()

	navigator, err := NewNavigator(false, DefaultBounds())
	if err != nil {
		t.Fatalf("NewNavigator: %v", err)
	}

	// A channel five long on the direct line, with dry ground one cell beside
	// it: straight through is five swims, and round is seven walks.
	wet := map[simgeom.BlockPos]bool{}
	for x := int32(1); x <= 5; x++ {
		wet[simgeom.BlockPos{X: x, Y: 64, Z: 0}] = true
	}
	view := waterSlab{slab: newSlab(), wet: wet}
	facts := Facts{water: map[simworld.BlockRef]bool{waterRef: true}}

	path, err := navigation.Find(
		t.Context(), view, facts, navigator.capability,
		simgeom.BlockPos{X: 0, Y: 64, Z: 0}, simgeom.BlockPos{X: 6, Y: 64, Z: 0},
		navigation.Budget{Nodes: routeNodes},
	)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if !path.Complete {
		t.Fatalf("no route past a five-cell channel: %v", path.Reason)
	}
	for _, edge := range path.Edges {
		if edge.Kind == navigation.EdgeSwim {
			t.Fatalf("swam a five-cell channel it could have walked around in two more steps: %v", path.Edges)
		}
	}
}

// TestAPuddleIsCrossedRatherThanCircled is the finding the test above rests on.
//
// It is asserted rather than assumed because it is the half a reader doubts: the
// point of pricing water above ground is not that the bot avoids water, it is
// that it weighs it.
func TestAPuddleIsCrossedRatherThanCircled(t *testing.T) {
	t.Parallel()

	navigator, err := NewNavigator(false, DefaultBounds())
	if err != nil {
		t.Fatalf("NewNavigator: %v", err)
	}

	view := waterSlab{slab: newSlab(), wet: map[simgeom.BlockPos]bool{
		{X: 2, Y: 64, Z: 0}: true,
	}}
	facts := Facts{water: map[simworld.BlockRef]bool{waterRef: true}}

	path, err := navigation.Find(
		t.Context(), view, facts, navigator.capability,
		simgeom.BlockPos{X: 0, Y: 64, Z: 0}, simgeom.BlockPos{X: 4, Y: 64, Z: 0},
		navigation.Budget{Nodes: routeNodes},
	)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if !path.Complete {
		t.Fatalf("no route across a one-cell puddle: %v", path.Reason)
	}

	var swum int
	for _, edge := range path.Edges {
		if edge.Kind == navigation.EdgeSwim {
			swum++
		}
	}
	if swum != 1 {
		t.Fatalf("crossed a one-cell puddle with %d swim edges, want one: %v", swum, path.Edges)
	}
}
