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
	// MaxSkips is how many waypoints in a row may be skipped before the region
	// counts as impassable.
	MaxSkips int
	// NoProgress is how long without net angular progress counts as stuck.
	NoProgress time.Duration
	// FleeMargin is how far beyond the radius a flight may travel. The bot
	// runs from a threat, not to the horizon: past this it stops running and
	// walks back, on the grounds that whatever is chasing it has either given
	// up or is a problem the circle cannot solve.
	FleeMargin float64
	// Escape is how long one flight may last.
	Escape time.Duration
	// WaterSearch is how far, in cells, a burning bot looks for water.
	WaterSearch int
	// FireDuration is how long a body burns after lava lights it.
	//
	// The game's number, not a guess: lavaIgnite calls igniteForSeconds(15).
	// It is here because the bot cannot read the fire's remaining ticks off the
	// wire -- the server sends the burning bit and not the countdown -- so the
	// only way to know how long is left is to know how long it lasts and watch
	// the clock.
	FireDuration time.Duration
	// Reach is how far the bot can hit from.
	//
	// The server's own number rather than a guess: a live 26.1.2 session sends
	// the player an entity_interaction_range attribute of 3. It is a constant
	// here because reading it off the wire per tick would be reading a number
	// that never changes for a bot with no equipment.
	Reach float64
	// Cooldown is how long between swings.
	//
	// Protocol 47 has no attack cooldown at all and 775 derives one from the
	// attack-speed attribute, which for an empty hand is four per second. Six
	// hundred and twenty-five milliseconds is that, and it is the slower of the
	// two, so a bot pacing itself by it is never swinging faster than either
	// version allows.
	Cooldown time.Duration
	// Engagement is how long one fight may last before the bot gives up and
	// goes back to its circle.
	Engagement time.Duration
	// SafeDistance is how far from a threat counts as clear of it. It is also
	// how far ahead the bot aims while running, which is why it is one number
	// and not two: a bot that aimed shorter than it needed to be safe would
	// arrive and stop while still being hit.
	SafeDistance float64
	// KillInterval is how often a sealed-in bot asks the server to kill it.
	//
	// Dying is the way out of a hole nothing can be walked, dug or jumped out
	// of: the bot respawns and walks back to its circle, which is a working
	// bot again. Two minutes because it is a last resort and a bot that killed
	// itself the moment it was briefly boxed in would be a bot that never
	// waited for a wall to open.
	KillInterval time.Duration
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
	// LegRadius is how close counts as arrived at one step of a route.
	//
	// Far smaller than WaypointRadius, and it has to be. A waypoint is a point
	// on a circle and anywhere within a block of it will do; a route leg is a
	// cell centre the bot has to actually reach, because the next leg is only
	// safe from there. Arriving at a leg a block early leaves the bot
	// off-centre and cutting the corner the planner routed around.
	//
	// It used to be a twentieth of a block, which worked only while the bot
	// asserted its own position and could stop exactly on a point. The body is
	// simulated now: it accelerates, and it carries momentum a tick's input
	// cannot cancel, so it cannot land within a twentieth of anything. A
	// tolerance below one tick of travel is a leg the bot never arrives at, and
	// a route it never finishes — so this is above one tick of a walk and well
	// below the half-block that would let it cut a corner.
	LegRadius float64
	// WaypointRadius is how close counts as arrived. Smaller than this and the
	// bot chases a point it overshoots every tick.
	WaypointRadius float64
	// WalkSpeed is how fast the bot claims to walk, in blocks per second.
	//
	// Under vanilla's own 4.317, deliberately. This example has no physics: it
	// reports a position rather than simulating a body, so the only thing
	// keeping its claim plausible is the speed it claims at. A server that
	// disagrees corrects the position, which opens the breaker and stops the
	// run — the honest outcome the design asks for, and not one worth
	// provoking by walking at exactly the limit.
	WalkSpeed float64
}

// Step is how far one movement update may travel, in blocks. It is derived
// rather than stored so that changing the tick rate changes how often the bot
// reports rather than how fast it claims to move, and it lives here because
// both the core and the actuator need the same number: the core to ask what is
// underfoot one step ahead, the actuator to go there.
func (b Bounds) Step() float64 { return b.WalkSpeed * b.Tick.Seconds() }

// DefaultBounds returns the shipped values.
func DefaultBounds() Bounds {
	return Bounds{
		Radius:         25,
		Waypoints:      32,
		MaxSkips:       3,
		NoProgress:     15 * time.Second,
		FleeMargin:     8,
		Escape:         10 * time.Second,
		Reach:          3,
		Cooldown:       625 * time.Millisecond,
		Engagement:     30 * time.Second,
		WaterSearch:    12,
		FireDuration:   15 * time.Second,
		SafeDistance:   12,
		KillInterval:   2 * time.Minute,
		TrappedBudget:  10 * time.Minute,
		BreakerBudget:  5,
		Tick:           50 * time.Millisecond,
		JoinTimeout:    30 * time.Second,
		LegRadius:      0.3,
		WaypointRadius: 1,
		WalkSpeed:      4,
	}
}
