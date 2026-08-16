package main

import (
	"math"
	"testing"
)

func TestThirtyTwoWaypointsStayWithinAnEighthOfABlock(t *testing.T) {
	t.Parallel()

	// The waypoint count is a consequence, not a preference. This is the
	// calculation that produced it.
	c := NewCircle(Vec3{}, 25, 32)

	if got := c.Deviation(); got > 0.125 {
		t.Errorf("32 waypoints deviate by %.4f blocks, want at most 0.125", got)
	}

	// And the number below it does not clear the same bar, which is why 16 was
	// not chosen.
	if got := NewCircle(Vec3{}, 25, 16).Deviation(); got <= 0.125 {
		t.Errorf("16 waypoints deviate by %.4f blocks; the choice of 32 is unjustified", got)
	}
}

func TestWaypointsSitOnTheCircle(t *testing.T) {
	t.Parallel()

	c := NewCircle(Vec3{X: 100, Y: 64, Z: -50}, 25, 32)

	for i := range c.Waypoints {
		p := c.At(i, 0)
		if got := p.HorizontalDistance(c.Centre); math.Abs(got-25) > 1e-9 {
			t.Errorf("waypoint %d is %.6f from the centre, want 25", i, got)
		}
		if p.Y != c.Centre.Y {
			t.Errorf("waypoint %d is at height %.1f, want %.1f", i, p.Y, c.Centre.Y)
		}
	}
}

func TestOffsetMovesTheWaypointRadially(t *testing.T) {
	t.Parallel()

	c := NewCircle(Vec3{}, 25, 32)

	for _, offset := range []float64{-4, -1, 1, 4} {
		got := c.At(7, offset).HorizontalDistance(c.Centre)
		if math.Abs(got-(25+offset)) > 1e-9 {
			t.Errorf("offset %.0f put the waypoint at %.6f, want %.0f", offset, got, 25+offset)
		}
	}
}

func TestWaypointIndexWraps(t *testing.T) {
	t.Parallel()

	c := NewCircle(Vec3{}, 25, 32)

	// The orbit increments the index forever and never takes a modulus, so
	// wrapping has to happen here or the bot walks off the end of the circle
	// after one revolution.
	if a, b := c.At(0, 0), c.At(32, 0); math.Abs(a.X-b.X) > 1e-9 || math.Abs(a.Z-b.Z) > 1e-9 {
		t.Errorf("waypoint 32 is %+v, want waypoint 0 at %+v", b, a)
	}
	if a, b := c.At(31, 0), c.At(-1, 0); math.Abs(a.X-b.X) > 1e-9 || math.Abs(a.Z-b.Z) > 1e-9 {
		t.Errorf("waypoint -1 is %+v, want waypoint 31 at %+v", b, a)
	}
}

func TestNearestReturnsTheWaypointAtThatAngle(t *testing.T) {
	t.Parallel()

	c := NewCircle(Vec3{X: 10, Y: 64, Z: 10}, 25, 32)

	for i := range c.Waypoints {
		// A position just outside the circle at a waypoint's angle must resolve
		// to that waypoint. This is the respawn path: the bot comes back from
		// an arbitrary place and has to pick where to rejoin.
		p := c.At(i, 7)
		if got := c.Nearest(p); got != i {
			t.Errorf("nearest to waypoint %d offset outward is %d", i, got)
		}
		// And from inside it, which is where a bed inside the circle puts it.
		if got := c.Nearest(c.At(i, -20)); got != i {
			t.Errorf("nearest to waypoint %d offset inward is %d", i, got)
		}
	}
}

func TestFloorHandlesNegativeCoordinates(t *testing.T) {
	t.Parallel()

	// Truncation would fold -0.5 onto block 0 alongside +0.5, which puts the
	// bot one block off on exactly half the circle.
	if got := (Vec3{X: -0.5, Y: 64.9, Z: -0.5}).Floor(); got != (BlockPos{X: -1, Y: 64, Z: -1}) {
		t.Errorf("floored to %+v, want {-1 64 -1}", got)
	}
	if got := (Vec3{X: 0.5, Y: 64, Z: 0.5}).Floor(); got != (BlockPos{X: 0, Y: 64, Z: 0}) {
		t.Errorf("floored to %+v, want {0 64 0}", got)
	}
}
