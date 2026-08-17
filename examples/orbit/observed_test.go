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

	centre, known := NewObserved(w.Snapshot(), PendingSolidity{}).Spawn()
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

	adapter := NewObserved(w.Snapshot(), PendingSolidity{})

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
