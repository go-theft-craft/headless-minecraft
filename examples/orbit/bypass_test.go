package main

import "testing"

func TestPassableSeparatesAirStepsWallsAndUnloaded(t *testing.T) {
	t.Parallel()

	c := NewCircle(Vec3{Y: 64}, 25, 32)
	at := c.At(0, 0)
	block := at.Floor()

	t.Run("clear ground", func(t *testing.T) {
		t.Parallel()

		if got := Passable(newScripted(), at); got != Clear {
			t.Errorf("open ground is %v, want Clear", got)
		}
	})

	t.Run("one block is steppable", func(t *testing.T) {
		t.Parallel()

		w := newScripted()
		w.wall(block.X, block.Z, 1)
		if got := Passable(w, at); got != Steppable {
			t.Errorf("a one-block rise is %v, want Steppable", got)
		}
	})

	t.Run("two blocks is a wall", func(t *testing.T) {
		t.Parallel()

		w := newScripted()
		w.wall(block.X, block.Z, 2)
		if got := Passable(w, at); got != Blocked {
			t.Errorf("a two-block wall is %v, want Blocked", got)
		}
	})

	t.Run("unloaded is not air", func(t *testing.T) {
		t.Parallel()

		// The distinction that matters: an unloaded chunk answering "not
		// solid" would walk the bot into a wall it cannot see, and answering
		// "wall" would strand it at the edge of its render distance.
		w := newScripted()
		w.loaded = func(BlockPos) bool { return false }
		if got := Passable(w, at); got != Unknown {
			t.Errorf("an unloaded chunk is %v, want Unknown", got)
		}
	})

	t.Run("a hole is not walkable", func(t *testing.T) {
		t.Parallel()

		w := newScripted()
		w.solid = map[BlockPos]bool{}
		// Remove the ground under the waypoint by moving the floor down.
		holed := &holedWorld{scripted: w, hole: BlockPos{X: block.X, Y: 63, Z: block.Z}}
		if got := Passable(holed, at); got != Blocked {
			t.Errorf("a hole in the floor is %v, want Blocked", got)
		}
	})
}

// holedWorld punches one air block into the floor.
type holedWorld struct {
	*scripted
	hole BlockPos
}

func (h *holedWorld) Block(p BlockPos) (Block, bool) {
	if p == h.hole {
		return Block{Solid: false}, true
	}

	return h.scripted.Block(p)
}

func TestBypassPrefersTheSmallestOffset(t *testing.T) {
	t.Parallel()

	c := NewCircle(Vec3{Y: 64}, 25, 32)
	w := newScripted()

	// Wall the circle itself and one block inward, leaving -2 and +1 open. The
	// search must take +1: it is nearer than -2.
	for _, offset := range []float64{0, -1} {
		p := c.At(0, offset).Floor()
		w.wall(p.X, p.Z, 3)
	}

	offset, found := Bypass(w, c, 0, 4)
	if !found {
		t.Fatal("no offset found, want one")
	}
	if offset != 1 {
		t.Errorf("took offset %.0f, want 1 as the nearest clear candidate", offset)
	}
}

func TestBypassSearchesInwardBeforeOutwardAtEqualDistance(t *testing.T) {
	t.Parallel()

	// Not because inward is better, but because the order has to be fixed. A
	// search that tried candidates in map order would pick a different route
	// each run and make a failure impossible to reproduce.
	c := NewCircle(Vec3{Y: 64}, 25, 32)
	w := newScripted()
	p := c.At(0, 0).Floor()
	w.wall(p.X, p.Z, 3)

	offset, found := Bypass(w, c, 0, 4)
	if !found || offset != -1 {
		t.Errorf("took offset %.0f (found=%v), want -1", offset, found)
	}
}

func TestBypassStaysInsideTheBand(t *testing.T) {
	t.Parallel()

	c := NewCircle(Vec3{Y: 64}, 25, 32)
	w := newScripted()
	w.seal(c, 0, 4)

	if offset, found := Bypass(w, c, 0, 4); found {
		t.Errorf("found offset %.0f through a sealed band, want none", offset)
	}

	// One block wider and it would have found a way, which proves the band is
	// what stopped it rather than the wall being infinite.
	if _, found := Bypass(w, c, 0, 6); !found {
		t.Error("a wider band found nothing; the seal is wider than intended")
	}
}

func TestBypassRefusesUnloadedChunks(t *testing.T) {
	t.Parallel()

	c := NewCircle(Vec3{Y: 64}, 25, 32)
	w := newScripted()
	w.loaded = func(BlockPos) bool { return false }

	if _, found := Bypass(w, c, 0, 4); found {
		t.Error("routed through unloaded chunks; strict mode refuses unknown collision")
	}
}

func TestCandidateOrderIsNearestFirst(t *testing.T) {
	t.Parallel()

	got := candidates(3)
	want := []int{0, -1, 1, -2, 2, -3, 3}

	if len(got) != len(want) {
		t.Fatalf("got %d candidates, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("candidates are %v, want %v", got, want)
		}
	}
}
