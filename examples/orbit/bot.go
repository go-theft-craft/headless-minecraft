package main

import (
	"fmt"
	"time"
)

// State is where the bot is in its own state machine.
type State uint8

const (
	// Joining waits for the session and the world spawn position.
	Joining State = iota
	// Orbiting walks the circle.
	Orbiting
	// Fleeing runs from the entity that hit the bot.
	Fleeing
	// Dead waits for a respawn to be sent and confirmed.
	Dead
	// Returning walks back to the circle after respawning.
	Returning
	// Trapped stands still, sealed in, and watches for the wall to change.
	Trapped
	// Done is terminal.
	Done
)

// String names a state for logs.
func (s State) String() string {
	switch s {
	case Joining:
		return "joining"
	case Orbiting:
		return "orbiting"
	case Fleeing:
		return "fleeing"
	case Dead:
		return "dead"
	case Returning:
		return "returning"
	case Trapped:
		return "trapped"
	case Done:
		return "done"
	default:
		return "unknown"
	}
}

// Tick is everything the core learns about one moment. The shell assembles it
// from the subscription and the snapshot; the core reads nothing else, which is
// what makes it testable without a server.
type Tick struct {
	Now time.Time
	// Ready is false until the session reaches play.
	Ready bool
	// Self is the local player. Valid only once Ready.
	Self Self
	// Attacker is the entity that just damaged the bot, or zero. This is the
	// field M7 currently cannot fill — see the design's Required surface.
	Attacker int32
	// Died reports a death since the last tick.
	Died bool
	// Respawned reports a confirmed respawn.
	Respawned bool
	// Corrected reports a server position correction, which opens the movement
	// breaker.
	Corrected bool
	// Revision is the snapshot revision. Trapped re-tests on a change rather
	// than on a timer.
	Revision uint64
}

// ActionKind is what the shell should do this tick.
type ActionKind uint8

const (
	// Stand does nothing. It is not the absence of an action: a trapped or
	// dead bot deliberately sends no movement.
	Stand ActionKind = iota
	// StepTo emits one movement update.
	StepTo
	// Strike attacks an entity. The core no longer decides on one -- it flees
	// instead -- and the kind stays because the actuator's Attack does, for
	// the same reason.
	Strike
	// SendRespawn answers a death.
	SendRespawn
	// Exit ends the program.
	Exit
)

// Action is one decision.
type Action struct {
	Kind   ActionKind
	Target Vec3
	Jump   bool
	Entity int32
	// Reason explains an Exit, and is logged for every other kind at debug.
	Reason string
	// Code is the process exit status for Exit.
	Code int
}

// Bot is the decision core. It holds no client, no context, and no clock: the
// shell supplies the time on every tick, so a test can run ten minutes of
// trapped standing in a microsecond.
type Bot struct {
	bounds Bounds
	circle Circle
	state  State

	// waypoint is the index the bot is walking toward.
	waypoint int
	// skips counts consecutive waypoints given up on.
	skips int

	// route is the planned way to the current waypoint, leg the step of it
	// being walked, and routedFor the waypoint it was planned for. The three
	// move together: a route is only valid for the waypoint that asked for it,
	// and a waypoint that changed invalidates the route rather than the leg.
	route     Route
	leg       int
	routedFor int

	// target is the entity being fought.
	target int32
	// escapeTo is the goal the current flight is running to. It is kept so
	// that a flight follows one chosen way out instead of choosing a new one
	// every tick as the threat moves.
	escapeTo Vec3
	// fledAt is when the current flight started.
	fledAt time.Time

	// progressAt is the last time the bot advanced a waypoint, and
	// progressWaypoint the index it advanced to. Together they are the
	// no-progress test.
	progressAt time.Time

	// trappedAt is when the bot last entered Trapped, and trappedRevision the
	// revision it last re-tested at.
	trappedAt       time.Time
	trappedRevision uint64

	// corrections counts acknowledged movement breakers.
	corrections int

	// readyAt is the first tick that reported play, which bounds the wait for
	// the world to supply a spawn position.
	readyAt time.Time
}

// NewBot returns a bot in Joining.
func NewBot(b Bounds) *Bot {
	return &Bot{bounds: b, state: Joining}
}

// State reports the current state, for logs and tests.
func (b *Bot) State() State { return b.state }

// Waypoint reports the waypoint being walked toward, for tests.
func (b *Bot) Waypoint() int { return b.waypoint }

// Route reports the planned route being walked, for tests.
func (b *Bot) Route() Route { return b.route }

// Advance folds one tick into the state machine and returns what to do.
//
// It is the whole program. Everything else in this directory is geometry, a
// search, a port, or a shell that calls this in a loop.
func (b *Bot) Advance(t Tick, w World) Action {
	// Death and correction outrank whatever the bot was doing, in that order.
	// A dead bot has no position worth stepping toward, and a corrected one
	// has a projection the server just disagreed with.
	if b.state != Done {
		if action, handled := b.interrupt(t); handled {
			return action
		}
	}

	switch b.state {
	case Joining:
		return b.join(t, w)
	case Orbiting:
		return b.orbit(t, w)
	case Fleeing:
		return b.flee(t, w)
	case Dead:
		return b.dead(t)
	case Returning:
		return b.returning(t, w)
	case Trapped:
		return b.trapped(t, w)
	case Done:
		return Action{Kind: Stand, Reason: "done"}
	default:
		return b.exit(fmt.Sprintf("unreachable state %d", b.state), 70)
	}
}

// interrupt handles the two events that preempt every state.
func (b *Bot) interrupt(t Tick) (Action, bool) {
	if t.Died && b.state != Dead {
		b.state = Dead
		b.target = 0

		return Action{Kind: SendRespawn, Reason: "died"}, true
	}

	if t.Corrected {
		b.corrections++
		if b.corrections > b.bounds.BreakerBudget {
			return b.exit(fmt.Sprintf(
				"movement corrected %d times; the movement is wrong, not unlucky",
				b.corrections,
			), 1), true
		}
		// Acknowledging the breaker is explicit, per the library's strict
		// recovery rules, and the projection is discarded with it: the bot
		// re-derives its waypoint from where the server says it is.
	}

	return Action{}, false
}

// join waits for play and the spawn position, then builds the circle.
func (b *Bot) join(t Tick, w World) Action {
	if !t.Ready {
		return Action{Kind: Stand, Reason: "waiting for play"}
	}

	// Start the clock at the first ready tick, not at construction: the wait
	// being bounded is about the world answering, and time spent connecting is
	// not the world failing to answer.
	if b.readyAt.IsZero() {
		b.readyAt = t.Now
	}

	centre, known := w.Spawn()
	if !known {
		if t.Now.Sub(b.readyAt) >= b.bounds.JoinTimeout {
			// The world is observed now, so reaching this means the server
			// really did not send a spawn position. The timeout exists because
			// standing in silence looks identical to working, and it is not.
			return b.exit("in play for "+b.bounds.JoinTimeout.String()+
				" and the server sent no spawn position", 3)
		}

		return Action{Kind: Stand, Reason: "waiting for world spawn"}
	}

	b.circle = NewCircle(centre, b.bounds.Radius, b.bounds.Waypoints)
	b.waypoint = b.circle.Nearest(t.Self.Position)
	b.progressAt = t.Now
	b.state = Returning

	return Action{Kind: Stand, Reason: "circle established"}
}

// orbit walks the planned route to the current waypoint, advancing when it
// arrives and planning again when it needs to.
//
// The bot does not walk at the waypoint. It walks the route the planner
// returned, cell centre to cell centre, which is the difference between going
// round a wall and standing against one. What this used to do instead -- aim
// straight at the waypoint and search a radial band when the aim was refused --
// is gone, along with the passability rules it needed: minecraft-simulation
// owns the body, the terrain predicates and an A* over them, and a worked
// example reimplementing any of the three was a worked example getting them
// wrong.
func (b *Bot) orbit(t Tick, w World) Action {
	if action, fighting := b.provoked(t, w); fighting {
		return action
	}

	if t.Self.Position.HorizontalDistance(b.circle.At(b.waypoint, 0)) <= b.bounds.WaypointRadius {
		b.waypoint++
		b.skips = 0
		b.progressAt = t.Now
		b.route = Route{}
	}

	// One search per waypoint, not one per tick. A* over streamed terrain is
	// the most expensive thing this loop can do and the waypoints are some five
	// blocks apart, so planning on arrival costs one search every twenty-five
	// ticks; planning every tick would cost twenty-five times that to answer
	// the same question.
	action, routed := b.follow(t, w, b.circle.At(b.waypoint, 0), b.waypoint)
	if !routed {
		return b.unreachable(t)
	}

	return action
}

// follow walks the planned route to a goal, planning one when there is none.
//
// Three callers now -- the orbit, the walk back to the circle, and the flight
// from a threat -- and they used to be three copies, of which the flight was
// the one that never got a route and went on walking into walls after the
// other two stopped. A goal is identified by a key so that a route survives
// across ticks and is thrown away the moment the goal changes.
//
// The second result is false when nothing could be routed to, which each
// caller answers differently: the orbit skips the waypoint, the return waits,
// and the flight runs at the threat's opposite anyway because standing still
// while something hits it is worse.
func (b *Bot) follow(t Tick, w World, goal Vec3, key int) (Action, bool) {
	if len(b.route.Steps) == 0 || b.routedFor != key {
		route, found := w.Route(t.Self.Position, goal)
		if !found {
			return Action{}, false
		}

		b.route, b.leg, b.routedFor = route, 0, key
	}

	// Consume every leg already arrived at. More than one lands at a time when
	// the planner routes through cells closer together than the arrival radius.
	for b.leg < len(b.route.Steps) &&
		t.Self.Position.HorizontalDistance(b.route.Steps[b.leg]) <= b.bounds.LegRadius {
		b.leg++
		b.progressAt = t.Now
	}

	if b.leg >= len(b.route.Steps) {
		// Walked out. An incomplete route ends short of the goal on purpose,
		// and the next tick plans the rest from where that left the bot, which
		// is closer than where it started.
		b.route = Route{}

		return Action{Kind: Stand, Reason: "route walked out"}, true
	}

	return b.step(b.route.Steps[b.leg]), true
}

// fleeTurns are the escape headings to try, in order: straight away first,
// then wider either side, and behind the threat last. Turning by thirty
// degrees is finer than the planner's own four directions, so a bot that can
// leave at all finds a heading it can leave on.
var fleeTurns = []float64{0, 30, -30, 60, -60, 90, -90, 120, -120, 150, -150, 180}

// fleeRoute is the route key for a flight. Waypoint indices only ever grow
// from zero, so a negative one cannot collide with them.
const fleeRoute = -1

// unreachable answers a waypoint nothing can route to: skip it, and after
// enough skips call the bot sealed in.
func (b *Bot) unreachable(t Tick) Action {
	if b.skips < b.bounds.MaxSkips {
		b.skips++
		b.waypoint++
		b.route = Route{}

		return Action{Kind: Stand, Reason: fmt.Sprintf("skipping to waypoint %d", b.waypoint%b.circle.Waypoints)}
	}

	// Exhausted skips alone are not being trapped: the bot also has to have
	// stopped making progress. A slow mob in the way defeats a search and then
	// walks off.
	if t.Now.Sub(b.progressAt) < b.bounds.NoProgress {
		return Action{Kind: Stand, Reason: "blocked, waiting for it to clear"}
	}

	b.state = Trapped
	b.trappedAt = t.Now
	b.trappedRevision = t.Revision

	return Action{Kind: Stand, Reason: "sealed in"}
}

// flee runs from the threat until it is gone, until the bot is clear of it,
// until the bot has run far enough from its circle, or until the clock runs
// out. Whichever lands first, the bot goes back to orbiting.
//
// It runs rather than fights on purpose. Fighting needs attack, attack needs
// the version profile's cooldown, and that is M9.6; running needs a direction
// and the movement this example already has. A bot that runs is also the
// honest demonstration of what the library can do today, where a bot that
// swings at things would be a demonstration of an error message.
func (b *Bot) flee(t Tick, w World) Action {
	threat, known := w.Entity(b.target)
	switch {
	case !known || !threat.Alive:
		return b.disengage(t, "threat gone")
	case t.Now.Sub(b.fledAt) > b.bounds.Escape:
		return b.disengage(t, "ran as long as it is worth running")
	case t.Self.Position.HorizontalDistance(threat.Position) >= b.bounds.SafeDistance:
		return b.disengage(t, "clear of the threat")
	// The bot's distance from the centre, not the threat's. The bot is the one
	// running, and it is the one that has to come back.
	case t.Self.Position.HorizontalDistance(b.circle.Centre) > b.circle.Radius+b.bounds.FleeMargin:
		return b.disengage(t, "ran far enough from the circle")
	}

	// Keep walking the way out already chosen. Re-choosing every tick as the
	// threat moves would have the bot pivot on the spot instead of leaving.
	if b.routedFor == fleeRoute && len(b.route.Steps) > 0 {
		if action, routed := b.follow(t, w, b.escapeTo, fleeRoute); routed {
			return action
		}
	}

	// Choose one. Directly away if the ground allows it, and otherwise at
	// progressively wider angles either side, because "away" is a direction
	// the threat picks and the ground has no obligation to agree with it. A
	// bot cornered against a wall has to run along the wall, and the straight
	// answer is the one that walks into it.
	direct := Away(t.Self.Position, threat.Position, b.bounds.SafeDistance)
	for _, turn := range fleeTurns {
		goal := direct.RotatedAbout(t.Self.Position, turn)

		route, found := w.Route(t.Self.Position, goal)
		if !found {
			continue
		}

		b.route, b.leg, b.routedFor, b.escapeTo = route, 0, fleeRoute, goal

		return b.step(b.route.Steps[0])
	}

	// Nowhere to run. Standing still while something hits the bot is bad; it
	// is still better than walking into a wall, which the server refuses and
	// the breaker ends the run over. The escape clock is running either way.
	return Action{Kind: Stand, Reason: "cornered, nowhere to run"}
}

// disengage returns to the circle at the nearest waypoint by angle.
func (b *Bot) disengage(t Tick, why string) Action {
	b.target = 0
	b.waypoint = b.circle.Nearest(t.Self.Position)
	b.skips = 0
	b.progressAt = t.Now
	b.state = Returning

	return Action{Kind: Stand, Reason: why}
}

// dead waits for the respawn to be confirmed, resending nothing. The library
// does not retry ambiguous work and neither does this: one respawn was sent on
// the transition into Dead.
func (b *Bot) dead(t Tick) Action {
	if !t.Respawned {
		return Action{Kind: Stand, Reason: "awaiting respawn"}
	}

	b.skips = 0
	b.progressAt = t.Now

	// A bot can die before it ever has a circle. Death preempts every state,
	// including Joining, so a client that connects to a server where it is
	// already dead — which is what a client that died and could not respawn
	// comes back as — goes straight to Dead without ever building one.
	// Returning there walks toward a circle of zero waypoints and divides by
	// it. Joining is what builds the circle, so that is where an unfinished
	// join resumes.
	if b.circle.Waypoints == 0 {
		b.state = Joining

		return Action{Kind: Stand, Reason: "respawned before joining"}
	}

	b.state = Returning

	return Action{Kind: Stand, Reason: "respawned"}
}

// returning walks back to the circle, which may be a long way if the respawn
// point is not spawn. It routes like any other leg, so a wall between the bed and
// the circle is already handled.
func (b *Bot) returning(t Tick, w World) Action {
	if action, fighting := b.provoked(t, w); fighting {
		return action
	}

	nearest := b.circle.Nearest(t.Self.Position)
	if nearest != b.waypoint {
		// A different waypoint is a different goal, and the route to the old
		// one is no longer the way back.
		b.waypoint, b.route = nearest, Route{}
	}
	target := b.circle.At(b.waypoint, 0)

	if t.Self.Position.HorizontalDistance(target) <= b.bounds.WaypointRadius {
		b.state = Orbiting
		b.progressAt = t.Now
		b.route = Route{}

		return Action{Kind: Stand, Reason: "back on the circle"}
	}

	// Routed, like every other leg. The way back is the leg most likely to
	// cross something: the bot got here by running from a threat, and it ran
	// in whatever direction was away rather than in one it had planned.
	action, routed := b.follow(t, w, target, b.waypoint)
	if !routed {
		return Action{Kind: Stand, Reason: "no way back to the circle yet"}
	}

	return action
}

// trapped stands still and re-tests when the world changes.
func (b *Bot) trapped(t Tick, w World) Action {
	// A walled-in bot should still defend itself, and killing the thing may be
	// what opens a way out.
	if action, fighting := b.provoked(t, w); fighting {
		return action
	}

	if t.Now.Sub(b.trappedAt) >= b.bounds.TrappedBudget {
		return b.exit(fmt.Sprintf(
			"sealed in at %.1f,%.1f,%.1f for %s",
			t.Self.Position.X, t.Self.Position.Y, t.Self.Position.Z, b.bounds.TrappedBudget,
		), 1)
	}

	// Re-test on a revision that changed, not on a timer. The revision is
	// already the signal that a block moved; a timer would either burn ticks
	// on a world that has not changed or miss the opening for its whole period.
	if t.Revision == b.trappedRevision {
		return Action{Kind: Stand, Reason: "sealed in"}
	}
	b.trappedRevision = t.Revision

	route, found := w.Route(t.Self.Position, b.circle.At(b.waypoint, 0))
	if !found {
		return Action{Kind: Stand, Reason: "still sealed in"}
	}

	b.route, b.leg, b.routedFor = route, 0, b.waypoint
	b.skips = 0
	b.progressAt = t.Now
	b.state = Orbiting

	return b.step(b.route.Steps[0])
}

// provoked switches to Fleeing when this tick carries an attacker.
func (b *Bot) provoked(t Tick, w World) (Action, bool) {
	if t.Attacker == 0 || b.state == Fleeing {
		return Action{}, false
	}

	attacker, known := w.Entity(t.Attacker)
	if !known || !attacker.Alive {
		return Action{}, false
	}

	b.target = t.Attacker
	b.fledAt = t.Now
	b.state = Fleeing
	b.route = Route{}

	return b.flee(t, w), true
}

// step is a movement update toward a target.
//
// It does not jump. It used to claim it did, on the grounds that jumping in a
// circle is the point, and nothing honoured the claim -- the actuator has no
// body to jump with and discarded the flag. That was harmless while the flag
// stayed inside this program. It stopped being harmless once the bot began
// declaring what it is doing on the wire, because then the claim is a
// statement to the server about a jump that never happens.
func (b *Bot) step(target Vec3) Action {
	return Action{Kind: StepTo, Target: target, Jump: false}
}

// exit ends the run.
func (b *Bot) exit(reason string, code int) Action {
	b.state = Done

	return Action{Kind: Exit, Reason: reason, Code: code}
}
