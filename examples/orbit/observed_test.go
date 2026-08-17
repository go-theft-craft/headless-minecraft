package main

import (
	"testing"

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

	centre, known := NewObserved(w.Snapshot(), unclassified{}).Spawn()
	if !known {
		t.Fatal("the adapter did not see a spawn the world had recorded")
	}
	if want := (Vec3{X: 100.5, Y: 64, Z: -19.5}); centre != want {
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

	adapter := NewObserved(w.Snapshot(), unclassified{})

	target, known := adapter.Entity(42)
	if !known {
		t.Fatal("a tracked entity did not reach the adapter")
	}
	if !target.Alive {
		t.Error("a freshly spawned entity read as dead")
	}
	if want := (Vec3{X: 10, Y: 64, Z: 10}); target.Position != want {
		t.Errorf("position is %+v, want %+v", target.Position, want)
	}

	if _, known := adapter.Entity(43); known {
		t.Error("the adapter answered for an entity it is not tracking")
	}
}

// unclassified is a solidity source that knows nothing, which is what a block
// the extracted table has never heard of looks like.
type unclassified struct{}

func (unclassified) Solid(uint32) (bool, bool) { return false, false }

func TestAnUnclassifiableBlockIsUnknownRatherThanAir(t *testing.T) {
	t.Parallel()

	// This is the whole reason Solidity is its own port. A block whose state
	// nothing can classify must read unknown, so the bypass search refuses it;
	// answering "not solid" would walk the bot into a wall it could not see.
	if _, known := silent().Block(BlockPos{X: 0, Y: 64, Z: 0}); known {
		t.Error("an unclassifiable block claimed to be known")
	}

	if passability := Passable(silent(), Vec3{X: 0, Y: 64, Z: 0}); passability != Unknown {
		t.Errorf("passability is %v, want Unknown", passability)
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
	// The table moved into the library and this test did not, because what it
	// pins is not where the numbers live: it is that the bot walking a protocol
	// 47 world gets vanilla's answer for the blocks a bot actually meets.
	solidity, err := NewSolidity(true)
	if err != nil {
		t.Fatalf("NewSolidity: %v", err)
	}
	if !solidity.Measured() {
		t.Fatal("protocol 47 reports no measurement")
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
			got, known := solidity.Solid(state)
			if !known {
				t.Errorf("%s (state %d) is not classified", name, state)

				continue
			}
			if got != c.solid {
				t.Errorf("%s (state %d) solid=%v, want %v", name, state, got, c.solid)
			}
		}
	}
}

func TestAnUnknownBlockIsNotAssumedWalkable(t *testing.T) {
	t.Parallel()

	// A modded block, or one a later version added, is not a block that has
	// been shown to be safe to walk into.
	solidity, err := NewSolidity(true)
	if err != nil {
		t.Fatalf("NewSolidity: %v", err)
	}
	if _, known := solidity.Solid(4000 << 4); known {
		t.Error("a block the table has never heard of claimed to be classified")
	}
}

// TestTheCurrentVersionClassifiesBlocks pins what closed the last gap between
// this example and a complete revolution.
//
// Until the 26.1.2 jar was measured this version had no table at all, so the
// bot could see the whole world, classify nothing in it, and refuse every step.
// The states here are the ones the library measured out of the game, read
// through this example's own port.
func TestTheCurrentVersionClassifiesBlocks(t *testing.T) {
	t.Parallel()

	solidity, err := NewSolidity(false)
	if err != nil {
		t.Fatalf("NewSolidity: %v", err)
	}
	if !solidity.Measured() {
		t.Fatal("the current version reports no measurement")
	}

	for _, test := range []struct {
		name  string
		state uint32
		want  bool
	}{
		{"air", 0, false},
		{"stone", 1, true},
		{"water", 86, false},
	} {
		solid, known := solidity.Solid(test.state)
		if !known {
			t.Errorf("%s was not classified", test.name)
			continue
		}
		if solid != test.want {
			t.Errorf("%s solid = %v, want %v", test.name, solid, test.want)
		}
	}
}

// TestAnUnknownCurrentStateIsNotAssumedWalkable keeps the refusal that mattered
// when there was no measurement at all. A state past the end of the
// measurement is a block this version does not have, and it is not open ground.
func TestAnUnknownCurrentStateIsNotAssumedWalkable(t *testing.T) {
	t.Parallel()

	solidity, err := NewSolidity(false)
	if err != nil {
		t.Fatalf("NewSolidity: %v", err)
	}
	if _, known := solidity.Solid(1 << 24); known {
		t.Error("a state beyond the measurement claimed to be classified")
	}
}
