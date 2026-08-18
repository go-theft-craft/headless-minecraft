package main

import (
	"testing"

	simgeom "github.com/go-theft-craft/minecraft-simulation/geom"
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
	staircase := []Vec3{
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
	corner := []Vec3{
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
	rise := []Vec3{
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
