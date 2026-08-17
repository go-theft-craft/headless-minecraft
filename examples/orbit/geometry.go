package main

import "math"

// Vec3 is a world position in blocks.
type Vec3 struct{ X, Y, Z float64 }

// BlockPos is the integer block a position falls inside.
type BlockPos struct{ X, Y, Z int }

// Floor converts a position to the block containing it. It floors rather than
// truncates: truncation folds -0.5 and 0.5 onto the same block, which puts the
// bot one block off on the negative side of spawn and nowhere else, so the bug
// only shows on half the circle.
func (v Vec3) Floor() BlockPos {
	return BlockPos{
		X: int(math.Floor(v.X)),
		Y: int(math.Floor(v.Y)),
		Z: int(math.Floor(v.Z)),
	}
}

// Add returns v offset by d.
func (v Vec3) Add(d Vec3) Vec3 { return Vec3{v.X + d.X, v.Y + d.Y, v.Z + d.Z} }

// HorizontalDistance reports the distance to o ignoring height. Every bound in
// this program is horizontal: a mob one block below the bot is in reach, and a
// mob thirty blocks below it on a cliff is not somewhere the bot should chase.
func (v Vec3) HorizontalDistance(o Vec3) float64 {
	return math.Hypot(v.X-o.X, v.Z-o.Z)
}

// Toward returns the position reached by moving at most limit blocks from v
// toward o, horizontally. The height is v's: this program has no physics, so it
// never chooses a Y, and a step that changed one would be claiming to fall or
// fly rather than to walk.
//
// It stops exactly on o rather than overshooting, which is what makes arrival a
// stable condition instead of a point the bot oscillates around.
func (v Vec3) Toward(o Vec3, limit float64) Vec3 {
	distance := v.HorizontalDistance(o)
	if distance <= limit || distance == 0 {
		return Vec3{X: o.X, Y: v.Y, Z: o.Z}
	}

	scale := limit / distance

	return Vec3{
		X: v.X + (o.X-v.X)*scale,
		Y: v.Y,
		Z: v.Z + (o.Z-v.Z)*scale,
	}
}

// Yaw returns the heading from v to o in degrees, as the wire carries it.
//
// Minecraft measures yaw from south, which is +Z, and increases it toward west,
// which is -X. That is neither the mathematical convention nor a compass
// bearing, so the arguments to Atan2 are the way they are on purpose.
func (v Vec3) Yaw(o Vec3) float32 {
	return float32(math.Atan2(-(o.X-v.X), o.Z-v.Z) * 180 / math.Pi)
}

// Circle is the orbit: a centre, a radius, and the waypoints the bot walks
// between.
type Circle struct {
	Centre    Vec3
	Radius    float64
	Waypoints int
}

// NewCircle returns the orbit for a spawn position.
func NewCircle(centre Vec3, radius float64, waypoints int) Circle {
	return Circle{Centre: centre, Radius: radius, Waypoints: waypoints}
}

// At returns waypoint i, wrapping, at a radius offset by delta blocks.
//
// The offset is what makes the bypass search one-dimensional: the circle is the
// invariant and the radius is the free variable, so going around an obstacle is
// a choice of delta rather than a search through a graph.
func (c Circle) At(i int, delta float64) Vec3 {
	angle := c.angle(i)
	radius := c.Radius + delta

	return Vec3{
		X: c.Centre.X + radius*math.Cos(angle),
		Y: c.Centre.Y,
		Z: c.Centre.Z + radius*math.Sin(angle),
	}
}

// angle returns the angle of waypoint i in radians, wrapping negatives so that
// stepping backwards from waypoint zero lands on the last waypoint rather than
// on a negative index.
func (c Circle) angle(i int) float64 {
	wrapped := ((i % c.Waypoints) + c.Waypoints) % c.Waypoints

	return 2 * math.Pi * float64(wrapped) / float64(c.Waypoints)
}

// Nearest returns the waypoint index closest by angle to a position.
//
// Closest by angle, not by distance: after a death the bot may respawn well
// inside or outside the circle, where the nearest waypoint by straight-line
// distance and the one it should resume at are the same, but where a position
// near the centre makes every waypoint almost equidistant and the distance
// comparison decided by floating-point noise. The angle is stable there.
func (c Circle) Nearest(p Vec3) int {
	angle := math.Atan2(p.Z-c.Centre.Z, p.X-c.Centre.X)
	if angle < 0 {
		angle += 2 * math.Pi
	}

	step := 2 * math.Pi / float64(c.Waypoints)
	index := int(math.Round(angle / step))

	return index % c.Waypoints
}

// Deviation reports how far the walked polygon falls inside the true circle,
// which is the sagitta of one chord. It exists so the waypoint count is a
// tested consequence rather than a number in a comment.
func (c Circle) Deviation() float64 {
	return c.Radius * (1 - math.Cos(math.Pi/float64(c.Waypoints)))
}
