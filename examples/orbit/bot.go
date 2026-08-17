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
	// Bypassing searches the band for a way around an obstacle.
	Bypassing
	// Engaging fights the entity that hit the bot.
	Engaging
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
	case Bypassing:
		return "bypassing"
	case Engaging:
		return "engaging"
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
	// Strike attacks an entity.
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
	// offset is the radial deviation the bypass search settled on.
	offset float64
	// skips counts consecutive waypoints given up on.
	skips int

	// target is the entity being fought.
	target int32
	// engagedAt is when the current fight started.
	engagedAt time.Time

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

// Offset reports the current radial deviation, for tests.
func (b *Bot) Offset() float64 { return b.offset }

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
	case Bypassing:
		return b.bypass(t, w)
	case Engaging:
		return b.engage(t, w)
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
		b.offset = 0
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

// orbit walks toward the current waypoint, advancing when it arrives.
func (b *Bot) orbit(t Tick, w World) Action {
	if action, fighting := b.provoked(t, w); fighting {
		return action
	}

	target := b.circle.At(b.waypoint, b.offset)
	if t.Self.Position.HorizontalDistance(target) <= b.bounds.WaypointRadius {
		b.waypoint++
		b.skips = 0
		b.progressAt = t.Now
		// The offset is not carried past the obstacle that caused it. Keeping
		// it would leave the bot orbiting at radius 21 forever after one tree.
		b.offset = 0
		target = b.circle.At(b.waypoint, b.offset)
	}

	switch Passable(w, target) {
	case Clear, Steppable:
		return b.step(target)
	case Blocked, Unknown:
		b.state = Bypassing

		return b.bypass(t, w)
	default:
		return b.step(target)
	}
}

// bypass searches the band, then the next waypoints, then gives up.
func (b *Bot) bypass(t Tick, w World) Action {
	if action, fighting := b.provoked(t, w); fighting {
		return action
	}

	if offset, found := Bypass(w, b.circle, b.waypoint, b.bounds.RadialBand); found {
		b.offset = offset
		b.state = Orbiting

		return b.step(b.circle.At(b.waypoint, offset))
	}

	if b.skips < b.bounds.MaxSkips {
		b.skips++
		b.waypoint++
		b.offset = 0

		return Action{Kind: Stand, Reason: fmt.Sprintf("skipping to waypoint %d", b.waypoint%b.circle.Waypoints)}
	}

	// The band and the skips are both exhausted, but that alone is not being
	// trapped: the bot also has to have stopped making progress. A slow mob in
	// the way exhausts the search and then walks off.
	if t.Now.Sub(b.progressAt) < b.bounds.NoProgress {
		return Action{Kind: Stand, Reason: "blocked, waiting for it to clear"}
	}

	b.state = Trapped
	b.trappedAt = t.Now
	b.trappedRevision = t.Revision

	return Action{Kind: Stand, Reason: "sealed in"}
}

// engage fights the target until it dies, leaves, or the clock runs out.
func (b *Bot) engage(t Tick, w World) Action {
	target, known := w.Entity(b.target)
	switch {
	case !known || !target.Alive:
		return b.disengage(t, "target gone")
	case t.Now.Sub(b.engagedAt) > b.bounds.Engagement:
		return b.disengage(t, "engagement timed out")
	case target.Position.HorizontalDistance(b.circle.Centre) > b.circle.Radius+b.bounds.ChaseMargin:
		return b.disengage(t, "target left the neighbourhood")
	}

	if t.Self.Position.HorizontalDistance(target.Position) > b.bounds.Reach {
		return b.step(target.Position)
	}

	return Action{Kind: Strike, Entity: b.target, Reason: "in reach"}
}

// disengage returns to the circle at the nearest waypoint by angle.
func (b *Bot) disengage(t Tick, why string) Action {
	b.target = 0
	b.waypoint = b.circle.Nearest(t.Self.Position)
	b.offset = 0
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

	b.state = Returning
	b.offset = 0
	b.skips = 0
	b.progressAt = t.Now

	return Action{Kind: Stand, Reason: "respawned"}
}

// returning walks back to the circle, which may be a long way if the respawn
// point is not spawn. It reuses the bypass search, so a wall between the bed and
// the circle is already handled.
func (b *Bot) returning(t Tick, w World) Action {
	if action, fighting := b.provoked(t, w); fighting {
		return action
	}

	b.waypoint = b.circle.Nearest(t.Self.Position)
	target := b.circle.At(b.waypoint, 0)

	if t.Self.Position.HorizontalDistance(target) <= b.bounds.WaypointRadius {
		b.state = Orbiting
		b.progressAt = t.Now

		return Action{Kind: Stand, Reason: "back on the circle"}
	}

	return b.step(target)
}

// trapped stands still and re-tests when the world changes.
func (b *Bot) trapped(t Tick, w World) Action {
	// A walled-in bot should still defend itself, and killing the thing may be
	// what clears the band.
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

	if offset, found := Bypass(w, b.circle, b.waypoint, b.bounds.RadialBand); found {
		b.offset = offset
		b.skips = 0
		b.progressAt = t.Now
		b.state = Orbiting

		return b.step(b.circle.At(b.waypoint, offset))
	}

	return Action{Kind: Stand, Reason: "still sealed in"}
}

// provoked switches to Engaging when this tick carries an attacker.
func (b *Bot) provoked(t Tick, w World) (Action, bool) {
	if t.Attacker == 0 || b.state == Engaging {
		return Action{}, false
	}

	attacker, known := w.Entity(t.Attacker)
	if !known || !attacker.Alive {
		return Action{}, false
	}

	b.target = t.Attacker
	b.engagedAt = t.Now
	b.state = Engaging

	return b.engage(t, w), true
}

// step is a movement update toward a target. The bot always jumps: jumping in a
// circle is the point.
func (b *Bot) step(target Vec3) Action {
	return Action{Kind: StepTo, Target: target, Jump: true}
}

// exit ends the run.
func (b *Bot) exit(reason string, code int) Action {
	b.state = Done

	return Action{Kind: Exit, Reason: reason, Code: code}
}
