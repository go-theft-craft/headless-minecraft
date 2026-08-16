package main

import "time"

// Bounds is every number this program ships with, in one struct, so a review
// can argue with them and a test can shrink them. The design's Bounds table is
// the same list with the reasoning.
type Bounds struct {
	// Radius of the orbit, in blocks.
	Radius float64
	// Waypoints sampled around the circle. Thirty-two puts the walked polygon
	// within an eighth of a block of the true circle at radius 25; sixteen
	// would be half a block out.
	Waypoints int
	// RadialBand is how far in and out the bypass search may move, in blocks.
	RadialBand int
	// MaxSkips is how many waypoints in a row may be skipped before the region
	// counts as impassable.
	MaxSkips int
	// NoProgress is how long without net angular progress counts as stuck.
	NoProgress time.Duration
	// ChaseMargin is how far beyond the radius a fight may travel.
	ChaseMargin float64
	// Engagement is how long one fight may last.
	Engagement time.Duration
	// Reach is the attack range.
	Reach float64
	// TrappedBudget is how long the bot stands sealed in before it gives up
	// and exits non-zero.
	TrappedBudget time.Duration
	// BreakerBudget is how many movement corrections may be acknowledged
	// before repeated corrections count as the movement being wrong rather
	// than unlucky.
	BreakerBudget int
	// Tick is the movement update period. Twenty hertz is what the server
	// expects.
	Tick time.Duration
	// JoinTimeout is how long the bot waits after reaching play for the world
	// to tell it where spawn is. Without it a bot whose world port cannot
	// answer stands in silence forever, which is what the first live run of
	// this example did.
	JoinTimeout time.Duration
	// WaypointRadius is how close counts as arrived. Smaller than this and the
	// bot chases a point it overshoots every tick.
	WaypointRadius float64
}

// DefaultBounds returns the shipped values.
func DefaultBounds() Bounds {
	return Bounds{
		Radius:         25,
		Waypoints:      32,
		RadialBand:     4,
		MaxSkips:       3,
		NoProgress:     15 * time.Second,
		ChaseMargin:    8,
		Engagement:     30 * time.Second,
		Reach:          3,
		TrappedBudget:  10 * time.Minute,
		BreakerBudget:  5,
		Tick:           50 * time.Millisecond,
		JoinTimeout:    30 * time.Second,
		WaypointRadius: 1,
	}
}
