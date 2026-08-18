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
	// Escaping gets out of ground that is hurting the bot.
	Escaping
	// Dousing walks to water to put a fire out.
	Dousing
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
	case Escaping:
		return "escaping"
	case Dousing:
		return "dousing"
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
	// checkedAt is the world revision the stretch ahead was last examined at,
	// so a route is re-examined when the world changes and not every tick.
	checkedAt uint64

	// target is the entity being fought.
	target int32
	// escapedAt is when the bot last noticed it was standing in something that
	// hurts, which bounds how long it may spend getting out.
	escapedAt time.Time
	// dousingAt is the water being walked to.
	dousingAt Vec3
	// litAt is when the bot was first seen burning, and lit whether it was.
	// The wire carries the burning bit and not the fire's remaining ticks, so
	// how long is left is worked out from when it started.
	litAt time.Time
	lit   bool
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

	// Standing in something that hurts outranks everything except dying. A bot
	// deciding which waypoint is next while it burns is a bot that will finish
	// deciding somewhere it cannot be revived from.
	if b.state != Done && b.state != Dead && b.state != Escaping && t.Ready && w.Hurting(t.Self.Position) {
		b.state = Escaping
		b.escapedAt = t.Now
		b.route = Route{}
	}

	// Note when the fire started. The server sends a bit that says burning and
	// never says for how long, so the clock is the only way to know how much
	// of it is left.
	if t.Self.OnFire && !b.lit {
		b.litAt, b.lit = t.Now, true
	}
	if !t.Self.OnFire {
		b.lit = false
	}

	// Burning, and not already dealing with it or with something worse. Getting
	// off burning ground outranks this: there is no point walking to water
	// while standing in lava, because the lava relights the fire every tick.
	if t.Self.OnFire && t.Ready &&
		b.state != Escaping && b.state != Dousing && b.state != Dead && b.state != Done {
		if water, worth := b.worthDousing(t, w); worth {
			b.state, b.dousingAt, b.route = Dousing, water, Route{}
		}
	}

	switch b.state {
	case Joining:
		return b.join(t, w)
	case Orbiting:
		return b.orbit(t, w)
	case Fleeing:
		return b.flee(t, w)
	case Escaping:
		return b.escape(t, w)
	case Dousing:
		return b.douse(t, w)
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

	// Look at the ground just ahead whenever the world has changed under the
	// route. A route is planned once and walked over many ticks, and somebody
	// pouring lava in front of a bot already committed to a way through does
	// not change the route -- the bot walks into what it planned across, which
	// is how this one kept finding lava that had not been there when it set
	// off.
	//
	// Only the next stretch, not the whole route. The bot covers a fifth of a
	// block a tick and this runs on every tick the world changed, so anything
	// beyond a couple of blocks is being re-examined long before it is walked,
	// and the cost of asking is paid twenty times a second.
	if b.leg < len(b.route.Steps) && t.Revision != b.checkedAt {
		b.checkedAt = t.Revision

		ahead := t.Self.Position.Toward(b.route.Steps[b.leg], lookahead)
		if !w.Walkable(t.Self.Position, ahead) {
			// Plan again from here rather than walk on. The next tick finds no
			// route and asks for one, over a world that now has the lava in it.
			b.route = Route{}

			return Action{Kind: Stand, Reason: "the way ahead changed"}, true
		}
	}

	if b.leg >= len(b.route.Steps) {
		// Walked out. An incomplete route ends short of the goal on purpose,
		// and the next tick plans the rest from where that left the bot, which
		// is closer than where it started.
		b.route = Route{}

		return Action{Kind: Stand, Reason: "route walked out"}, true
	}

	return b.guardedStep(t, w, b.route.Steps[b.leg]), true
}

// guardedStep refuses to walk into something that hurts, whatever the plan
// said.
//
// The last line, and it exists because every line before it is a prediction.
// The route was planned against a world that has since changed, the check on
// the way ahead runs on the tick the world reports a change and lava spreads
// between ticks, and any of it can be a moment behind. This looks at the one
// position the bot is about to occupy, which is the only claim that has to be
// right.
//
// Dropping the route with it: a step refused here is a route that has stopped
// describing the world, and walking the rest of it would be walking the same
// prediction again.
func (b *Bot) guardedStep(t Tick, w World, target Vec3) Action {
	// A bot already standing in something may step anywhere. The guard exists
	// to keep a body out of harm, and a body in harm has nothing left to
	// protect: every way out of a lava pool starts with a step whose landing
	// is still lava, so refusing those is refusing to leave. This is the
	// mistake that killed the bot in the trace -- it stood in a spreading pool
	// for a second and a half at a time, declining to move, and burned.
	if w.Hurting(t.Self.Position) {
		return b.step(target)
	}

	next := t.Self.Position.Toward(target, b.bounds.Step())
	if w.Hurting(next) {
		b.route = Route{}

		return Action{Kind: Stand, Reason: "not stepping into that"}
	}

	return b.step(target)
}

// fleeTurns are the escape headings to try, in order: straight away first, then
// wider either side.
//
// Ninety degrees is where it stops, which is where the game stops. Vanilla's
// own avoid-entity goal draws its escape inside pi/2 radians of directly away
// and then throws out any destination no further from the threat than the mob
// already is; past a quarter turn those are the same rule, because a heading
// more than ninety degrees off is closing the distance. This walked into that:
// a fan that ran to a full half turn would happily pick a way out that ran at
// the thing it was running from.
//
// Deterministic where the game is random. A mob picks a direction inside the
// arc and retries; this takes the straightest heading that routes, because an
// example whose behaviour changes run to run is an example nobody can debug.
var fleeTurns = []float64{0, 30, -30, 60, -60, 90, -90}

// lookahead is how far along the current leg the bot re-examines when the
// world changes, in blocks. Two is ten ticks of walking: far enough that
// something appearing in the way is seen before it is reached, and short
// enough that the check stays a fixed cost rather than growing with the route.
const lookahead = 2.0

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

// worthDousing decides whether a burning bot should go and stand in water.
//
// The arithmetic is the game's. Fire does one point of damage every twenty
// ticks and lava lights a body for fifteen seconds, so the damage still to come
// is one point per second of fire left. Walking to the water costs that same
// damage for as long as the walk takes, because the fire burns either way --
// so the trip is only worth making if it ends before the fire would have. Water
// four seconds away with ten seconds of fire left saves six points; water ten
// seconds away with four left saves nothing and costs the orbit.
//
// The leash is the other half. A bot that would chase water past the margin its
// flights are held to is a bot that has swapped one problem for being lost, and
// fire that is nearly out is not worth crossing the map for.
//
// Nothing here reads a water bucket, and this is where one would go: emptying a
// bucket at your feet is instant where walking is not, so a bot holding one
// would never need this calculation. Reading the inventory means decoding the
// version's own slot format and using it means an action nothing in this
// library sends yet.
func (b *Bot) worthDousing(t Tick, w World) (Vec3, bool) {
	left := b.bounds.FireDuration - t.Now.Sub(b.litAt)
	if left <= 0 {
		return Vec3{}, false
	}

	water, found := w.Water(t.Self.Position, b.bounds.WaterSearch)
	if !found {
		return Vec3{}, false
	}

	// How long the walk takes, at the speed the bot claims to walk.
	travel := time.Duration(
		t.Self.Position.HorizontalDistance(water) / b.bounds.WalkSpeed * float64(time.Second),
	)
	if travel >= left {
		return Vec3{}, false
	}

	if water.HorizontalDistance(b.circle.Centre) > b.circle.Radius+b.bounds.FleeMargin {
		return Vec3{}, false
	}

	return water, true
}

// douse walks to the water it decided was worth reaching.
//
// It stops the moment the fire is out, which is the point of the walk, and it
// re-asks whether the trip is still worth making rather than committing to it:
// the fire burns down while the bot walks, and water that was worth four
// seconds of walking at the start is not worth it with two seconds of fire
// left. A bot that committed would arrive at a puddle it no longer needed,
// having left its circle for nothing.
func (b *Bot) douse(t Tick, w World) Action {
	if !t.Self.OnFire {
		return b.disengage(t, "the fire is out")
	}

	water, worth := b.worthDousing(t, w)
	if !worth {
		return b.disengage(t, "not worth reaching water for what is left of the fire")
	}
	if water != b.dousingAt {
		b.dousingAt, b.route = water, Route{}
	}

	action, routed := b.follow(t, w, b.dousingAt, douseRoute)
	if !routed {
		return b.disengage(t, "no way to the water")
	}

	return action
}

// douseRoute is the route key for the walk to water.
const douseRoute = -3

// escape gets the bot off ground that is hurting it.
//
// It outranks the orbit, the flight and the walk home, because all three are
// about where to be next and this is about not dying where the bot already is.
// Lava does most of its damage to something that stands in it and thinks about
// the route.
//
// The way out is short and in whatever direction works. A route is planned to a
// few blocks off in each of eight headings and the first that lands somewhere
// safe is taken -- not the best one, the first, because the search costs ticks
// and every tick here is damage. Nothing routable in any direction means the
// bot is in it deep enough that the planner will not start from where it
// stands, so it walks straight out on the shortest heading and takes its
// chances, which beats standing in lava reasoning about headings.
func (b *Bot) escape(t Tick, w World) Action {
	if !w.Hurting(t.Self.Position) {
		return b.disengage(t, "out of it")
	}

	if t.Now.Sub(b.escapedAt) > b.bounds.Escape {
		return b.disengage(t, "still in it, but out of time to be careful")
	}

	// One step, eight directions, and go. This runs before any search because
	// searching is what killed the bot the last time: a packet trace of a bot
	// burning to death shows it walking smoothly, lava arriving in its cell,
	// and then a second and a half in which it sent nothing at all -- twice,
	// which is three seconds of standing in lava. Whatever it was doing in
	// those gaps, asking eight cheap questions and stepping is better.
	//
	// The test is on the place the step lands rather than on the direction,
	// because a body 0.6 wide leaving a pool cares where its box ends up.
	for _, heading := range escapeHeadings {
		aim := t.Self.Position.
			Add(Vec3{X: b.bounds.SafeDistance}).
			RotatedAbout(t.Self.Position, heading)

		if !w.Hurting(t.Self.Position.Toward(aim, b.bounds.Step())) {
			b.route = Route{}

			return b.step(aim)
		}
	}

	// Nothing one step away is clear, so the bot is in deep enough that the
	// way out is worth searching for. The nearest ground that does not hurt:
	// which edge of a pool is closest depends on where in it the bot is, and
	// picking a fixed direction walks half the cases further in.
	out, found := w.Safe(t.Self.Position, b.bounds.WaterSearch)
	if !found {
		// Nothing safe within reach. Keep the way out already being walked if
		// there is one, and otherwise stand: with no known edge in any
		// direction, a step is as likely to go further in as out.
		if b.routedFor == escapeRoute && len(b.route.Steps) > 0 {
			if action, routed := b.follow(t, w, b.escapeTo, escapeRoute); routed {
				return action
			}
		}

		return Action{Kind: Stand, Reason: "in it with no edge in sight"}
	}

	if out != b.escapeTo {
		b.escapeTo, b.route = out, Route{}
	}

	// Routed if the planner will, and walked straight at it if it will not.
	// A planner that refuses to start is a planner standing in lava with the
	// bot: it has nothing to say, and the direction is known regardless.
	if action, routed := b.follow(t, w, b.escapeTo, escapeRoute); routed {
		return action
	}

	return b.step(b.escapeTo)
}

// escapeHeadings are the directions tried for the one step out, straight ahead
// first and then round. Eight is finer than the planner's four and cheap enough
// to exhaust inside a tick, which is the whole point of trying them at all.
var escapeHeadings = []float64{0, 45, -45, 90, -90, 135, -135, 180}

// escapeRoute is the route key for getting out of something that hurts.
const escapeRoute = -2

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
		return b.disengage(t, "clear of "+threatName(threat))
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

	// The way out ran out and the threat is still here. Look for another, and
	// go back to the circle if there is none -- walking a circle while being
	// hit beats standing still while being hit, and it is what the game does:
	// a mob whose avoid goal cannot find a path does not freeze, it carries on
	// with whatever it was doing.
	goal, found := b.wayOut(t, w, threat)
	if !found {
		return b.disengage(t, "nowhere to run from "+threatName(threat))
	}

	// wayOut left the route on the bot; this is the rest of the bookkeeping.
	b.leg, b.routedFor, b.escapeTo = 0, fleeRoute, goal

	return b.step(b.route.Steps[0])
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

	// Something that cannot follow the bot is not worth leaving the circle
	// for. The damage names the cause rather than the thing that arrived --
	// an arrow's shooter, not the arrow -- so what reaches here and stays put
	// really is something the bot can simply walk away from.
	//
	// An entity this client cannot name pursues. A modded server spawns types
	// no data set has heard of, and the safe reading of "I do not know what
	// hit me" is not "it is harmless".
	if attacker.Named && !attacker.Kind.Pursues {
		return Action{}, false
	}

	// Look for a way out before committing to one, the way the game decides
	// whether an avoid goal may start at all. A bot that switched to fleeing
	// and then found nowhere to go would have abandoned its circle to stand
	// still, which is the worst of both.
	b.target = t.Attacker
	b.route = Route{}

	goal, found := b.wayOut(t, w, attacker)
	if !found {
		b.target = 0

		return Action{}, false
	}

	b.fledAt = t.Now
	b.state = Fleeing
	b.leg, b.routedFor, b.escapeTo = 0, fleeRoute, goal

	// Through flee rather than straight to the step, so that the tick which
	// starts the flight is still subject to the tests that end one. A threat
	// already out of range, or a bot already past its margin, ends the flight
	// on the tick it began.
	return b.flee(t, w), true
}

// wayOut picks a heading the bot can actually leave on, and plans the route.
//
// Two rules, both the game's. The heading stays inside a quarter turn of
// directly away, and a destination no further from the threat than the bot
// already is gets thrown out -- the second is what stops a wide fan choosing a
// way out that runs at the thing being run from, and it is a rule vanilla
// states outright in its own avoid-entity goal.
//
// It leaves the route on the bot, because planning it is most of the work and
// the caller is about to walk it.
func (b *Bot) wayOut(t Tick, w World, threat Entity) (Vec3, bool) {
	here := t.Self.Position
	direct := Away(here, threat.Position, b.bounds.SafeDistance)
	// Vanilla measures sixteen blocks; this measures the safe distance, which
	// is shorter, because this bot is on a leash that one is not -- it has a
	// circle to get back to, and FleeMargin ends a flight that leaves it.
	gap := here.HorizontalDistance(threat.Position)

	for _, turn := range fleeTurns {
		goal := direct.RotatedAbout(here, turn)
		if goal.HorizontalDistance(threat.Position) <= gap {
			continue
		}

		route, found := w.Route(here, goal)
		if !found {
			continue
		}

		b.route = route

		return goal, true
	}

	return Vec3{}, false
}

// threatName is what to call a threat in a log line.
func threatName(threat Entity) string {
	if threat.Named && threat.Kind.Name != "" {
		return threat.Kind.Name
	}

	return "an entity this client cannot name"
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
