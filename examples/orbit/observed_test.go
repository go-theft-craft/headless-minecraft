package main

import (
	"context"
	"testing"

	simgeom "github.com/go-theft-craft/minecraft-simulation/geom"

	"github.com/go-theft-craft/headless-minecraft/event"
	"github.com/go-theft-craft/headless-minecraft/world"
)

// These tests drive a real *world.World rather than a stub, because the thing
// under test is the mapping between the library's shape and the example's. A
// stub would agree with whatever the adapter did and prove nothing.

func TestSpawnIsTheBlockCentreAndNotItsCorner(t *testing.T) {
	t.Parallel()

	// A circle drawn through block corners is half a block off the one an
	// operator standing at spawn sees, in both horizontal axes.
	w := world.New()

	var c event.Collector
	w.Environment().SpawnChanged(&c, world.BlockPos{X: 100, Y: 64, Z: -20}, "", 0, 0, false)

	centre, known := NewObserved(context.Background(), w.Snapshot(), Navigator{}, Kinds{}).Spawn()
	if !known {
		t.Fatal("the adapter did not see a spawn the world had recorded")
	}
	if want := (simgeom.Vec3{X: 100.5, Y: 64, Z: -19.5}); centre != want {
		t.Errorf("centre is %+v, want %+v", centre, want)
	}
}

func TestAnUnsentSpawnDoesNotCentreTheCircleOnTheOrigin(t *testing.T) {
	t.Parallel()

	// The failure this guards is quiet: a bot orbiting 0,0 looks exactly like a
	// bot orbiting spawn, and every automated check would pass.
	if _, known := silent().Spawn(); known {
		t.Error("the adapter invented a spawn the server never sent")
	}
}

func TestAnEntityIsAliveUntilTheServerSaysOtherwise(t *testing.T) {
	t.Parallel()

	w := world.New()

	var c event.Collector
	w.Entities().Spawned(&c, 42, "", "minecraft:zombie", 10, 64, 10, 0, 0)

	adapter := NewObserved(context.Background(), w.Snapshot(), Navigator{}, Kinds{})

	target, known := adapter.Entity(42)
	if !known {
		t.Fatal("a tracked entity did not reach the adapter")
	}
	if !target.Alive {
		t.Error("a freshly spawned entity read as dead")
	}
	if want := (simgeom.Vec3{X: 10, Y: 64, Z: 10}); target.Position != want {
		t.Errorf("position is %+v, want %+v", target.Position, want)
	}

	if _, known := adapter.Entity(43); known {
		t.Error("the adapter answered for an entity it is not tracking")
	}
}

// TestAnUnstreamedWorldRoutesNowhere pins the refusal the example still owns.
//
// Whether a particular block stops a body is terrain's question and is tested
// where terrain lives. What this example must not do is walk over ground
// nobody has described to it: the planner reports a cell in an unloaded chunk
// as unknown rather than as air, and a bot that got a route out of that would
// be walking on the assumption that the server's world matches its gaps.
func TestAnUnstreamedWorldRoutesNowhere(t *testing.T) {
	t.Parallel()

	if _, found := silent().Route(simgeom.Vec3{Y: 64}, simgeom.Vec3{X: 8, Y: 64}); found {
		t.Error("routed across a world that has streamed nothing")
	}
}

func TestSelfIsUnplacedUntilTheServerPlacesIt(t *testing.T) {
	t.Parallel()

	// A player at the origin and a player the server has not placed are
	// different, and the orbit's first waypoint is chosen from this position.
	if _, placed := observeSelf(world.New().Snapshot()); placed {
		t.Error("read a position from a world that had applied nothing")
	}
}

func TestTheMeasuredTableAgreesWithTheGame(t *testing.T) {
	t.Parallel()

	// These are vanilla's own answers, and the ones the shortcuts get wrong.
	// Treating every non-air state as solid detours the bot around a flower and
	// drowns it in water it read as a wall; reading a bounding box instead of
	// the material calls thin snow a wall, which it is not.
	//
	// The table moved out of this example along with the rest of the terrain
	// rules, and this test did not, because what it pins is not where the
	// numbers live: it is that the bot walking a protocol 47 world gets
	// vanilla's answer for the blocks a bot actually meets.
	profile, blocks, _, err := versionTerrain(true)
	if err != nil {
		t.Fatalf("versionTerrain: %v", err)
	}
	for name, c := range map[string]struct {
		block int
		solid bool
	}{
		"air":          {0, false},
		"stone":        {1, true},
		"grass":        {2, true},
		"water":        {9, false},
		"lava":         {11, false},
		"tall grass":   {31, false},
		"flower":       {37, false},
		"slab":         {44, true},
		"torch":        {50, false},
		"snow layer":   {78, false},
		"double plant": {175, false},
	} {
		// A chunk state carries metadata in the low four bits, and metadata
		// never changes this answer in a version where the material hangs off
		// the block. Check a state with metadata set to prove the shift.
		for _, meta := range []uint32{0, 5} {
			state := uint32(c.block)<<4 | meta
			ref, known := blocks.Ref(state)
			if !known {
				t.Errorf("%s (state %d) is not classified", name, state)

				continue
			}
			shape, ok := profile.Shape(ref)
			if !ok {
				t.Errorf("%s (state %d) has no shape", name, state)

				continue
			}
			// A body is stopped by a collision shape, so an empty one is
			// something the bot walks through. That is the same question the
			// measured table answered and a different way of asking it: the
			// table said what a block is made of, and this says what it
			// collides as.
			if got := !shape.IsEmpty(); got != c.solid {
				t.Errorf("%s (state %d) solid=%v, want %v", name, state, got, c.solid)
			}
		}
	}
}
