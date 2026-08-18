package main

import (
	"math"

	simgeom "github.com/go-theft-craft/minecraft-simulation/geom"
)

// The vector arithmetic this example used to define now lives in
// minecraft-simulation/geom, and this file imports it.
//
// It was never isolation, only a copy: the example already depended on that
// module, so a second Vec3 beside it was the same failure the navigation design
// records about the 334 lines of bypass.go that answered "can I stand here"
// inside an example because nothing in the stack exposed the fact. Add, Floor,
// HorizontalDistance, Toward, Yaw, and Away are all in geom now, with the doc
// comments they were written with — the reasoning in them was paid for by a
// live run and is not the kind of thing to retype.
//
// What stays here is what is true about this bot rather than about the game: a
// waypoint ring, which waypoint to resume at, and how far the walked polygon
// falls inside the circle it approximates.

// Circle is the orbit: a centre, a radius, and the waypoints the bot walks
// between.
type Circle struct {
	Centre    simgeom.Vec3
	Radius    float64
	Waypoints int
}

// NewCircle returns the orbit for a spawn position.
func NewCircle(centre simgeom.Vec3, radius float64, waypoints int) Circle {
	return Circle{Centre: centre, Radius: radius, Waypoints: waypoints}
}

// At returns waypoint i, wrapping, at a radius offset by delta blocks.
//
// The offset shifts the circle in or out. Nothing in the bot uses it now that
// the planner routes around obstacles, and it stays because the circle is the
// invariant and the radius is the free variable, so going around an obstacle is
// a choice of delta rather than a search through a graph.
func (c Circle) At(i int, delta float64) simgeom.Vec3 {
	angle := c.angle(i)
	radius := c.Radius + delta

	return simgeom.Vec3{
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
func (c Circle) Nearest(p simgeom.Vec3) int {
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

// rotatedAbout turns a point around a centre by an angle in degrees.
//
// It is horizontal only, like everything else this bot does: it walks a flat
// world, and turning a position through the vertical would aim it at the sky.
//
// It is a function rather than a method now that the vector belongs to another
// package, and it stays here rather than moving with the others because it is
// how this bot picks an escape heading — a fact about this program and not
// about the game.
func rotatedAbout(v, centre simgeom.Vec3, degrees float64) simgeom.Vec3 {
	radians := degrees * math.Pi / 180
	sin, cos := math.Sin(radians), math.Cos(radians)
	dx, dz := v.X-centre.X, v.Z-centre.Z

	return simgeom.Vec3{
		X: centre.X + dx*cos - dz*sin,
		Y: v.Y,
		Z: centre.Z + dx*sin + dz*cos,
	}
}

// floorOf is geom.BlockPosOf under the name this example used for it.
//
// It exists so the call sites read the way they did — a position floored to the
// cell it is in — while the arithmetic is the shared one. geom's own Floor
// rounds toward negative infinity for the reason this bot needed it to:
// truncation folds -0.5 and 0.5 onto the same block, which puts the bot one
// block off on the negative side of spawn and nowhere else, so the bug only
// shows on half the circle.
//
// It replaced two functions. The example used to floor to its own BlockPos and
// then convert that into the simulation's, which is the shape a duplicated type
// forces on everything that touches both — and the conversion is exactly where a
// width or a rounding rule goes quietly wrong.
func floorOf(v simgeom.Vec3) simgeom.BlockPos { return simgeom.BlockPosOf(v) }
