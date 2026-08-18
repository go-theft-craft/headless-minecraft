// Package behaviour composes the outbound primitives into multi-tick tasks.
//
// The other pieces of the client supply an aim, an action, an edge, and a path.
// None of them supplies a bot that fishes, because fishing is not a packet: it
// is cast, wait, read a signal the server never names, reel, repeat, and give
// up when the rod breaks. That shape recurs — eat, block with a shield, follow a
// player, flee a threat, bridge a gap, pillar to the surface, strip-mine a
// corridor — and this package says what it is.
//
// # Asked once per tick, never driving
//
// A behaviour is asked once per tick and never drives. That is not a
// preference: adapter.Source already requires it, and a behaviour that drove its
// own loop could not be composed with a follower that does not. Three things
// follow from it.
//
// A wait is a tick that returns no action. A behaviour waiting for a rod to dip,
// a furnace to smelt, or a placement to settle returns Running with an empty
// action set, so it never sleeps, never blocks, and never owns a goroutine, and
// the tick rate stays the caller's.
//
// A behaviour is testable without a connection: feed it snapshots and read its
// actions, which is how examples/orbit already tests its own tick loop.
//
// Behaviours compose by delegation. StripMine holds a follower and a digging
// behaviour and forwards its tick to whichever is active. There is no scheduler
// here, because there is nothing to schedule — choosing what the bot should be
// doing is the application's decision, exactly as goal selection is the
// application's in navigation. Behaviour trees, priority arbitration, and combat
// strategy are all deliberately absent.
//
// # Authorization is checked at construction
//
// Every behaviour declares the scopes it needs and refuses to be built without
// them. Checking at construction rather than per tick matches the client's own
// rule that components are selected and validated before network work begins: a
// behaviour that discovered on tick four hundred that it may not dig has already
// walked the bot somewhere it should not be.
//
// # Nothing in the client's required path imports this
//
// A caller that wants no behaviours links none of this.
package behaviour

import (
	"context"
	"fmt"

	"github.com/go-theft-craft/headless-minecraft/version"
	"github.com/go-theft-craft/headless-minecraft/world"
)

// Behaviour is one multi-tick task.
//
// Tick is asked once per tick with the world as it stands, and answers with what
// it wants done and whether it is finished. It must not block, sleep, or retain
// the context beyond the call.
type Behaviour interface {
	Tick(ctx context.Context, observed world.Snapshot) (Outcome, error)
}

// Outcome is what one tick of a behaviour decided.
type Outcome struct {
	// Actions are the intents this tick wants sent, in order. It is empty on a
	// tick that is waiting, which is the ordinary case and not a failure.
	Actions []version.Action
	// Status says whether the behaviour wants another tick.
	Status Status
	// Reason says why a stopped behaviour stopped. It is ReasonNone for every
	// other status.
	Reason Reason
}

// Status says whether a behaviour wants another tick.
type Status uint8

const (
	// Running means the behaviour wants another tick. A behaviour that is
	// waiting is running.
	Running Status = iota
	// Complete means the behaviour did what it was asked and is finished.
	Complete
	// Stopped means the behaviour gave up. Reason says why.
	//
	// It is separate from Complete because the two mean opposite things to a
	// caller: one is a task done and the other is a task that cannot be, and a
	// caller that retried the second forever is the failure this distinction
	// exists to prevent.
	Stopped
)

// String returns the status's name.
func (s Status) String() string {
	switch s {
	case Running:
		return "running"
	case Complete:
		return "complete"
	case Stopped:
		return "stopped"
	default:
		return fmt.Sprintf("Status(%d)", uint8(s))
	}
}

// Reason says why a behaviour stopped.
//
// The first four mirror the follower's, because a behaviour built on a route
// fails the way a route does. The last two are the ones only a behaviour has.
type Reason uint8

const (
	// ReasonNone is the reason of a behaviour that has not stopped.
	ReasonNone Reason = iota
	// ReasonBlocked means the way is shut and no route was found.
	ReasonBlocked
	// ReasonStuck means the body stopped making progress.
	ReasonStuck
	// ReasonWorldChanged means what the behaviour was working on is gone.
	ReasonWorldChanged
	// ReasonFailed means the behaviour could not do what it was asked, for a
	// reason it can name and the others do not cover.
	ReasonFailed
	// ReasonUnauthorized means the authorization stopped covering the scopes
	// the behaviour needs.
	//
	// Scopes are checked at construction, so this is the rare case where they
	// stop holding afterwards rather than the ordinary refusal.
	ReasonUnauthorized
	// ReasonOutOfResources means the body ran out of what the task consumes:
	// blocks to place, food to eat, rods to cast.
	ReasonOutOfResources
)

// String returns the reason's name.
func (r Reason) String() string {
	switch r {
	case ReasonNone:
		return "none"
	case ReasonBlocked:
		return "blocked"
	case ReasonStuck:
		return "stuck"
	case ReasonWorldChanged:
		return "world-changed"
	case ReasonFailed:
		return "failed"
	case ReasonUnauthorized:
		return "unauthorized"
	case ReasonOutOfResources:
		return "out-of-resources"
	default:
		return fmt.Sprintf("Reason(%d)", uint8(r))
	}
}

// running returns a tick that wants another one and asks for nothing.
//
// It is the shape of a wait, and it is a helper because writing it out at every
// wait is where an accidental action creeps in.
func running(actions ...version.Action) Outcome {
	return Outcome{Actions: actions, Status: Running}
}

// complete returns a finished behaviour, with any last actions it wants sent.
func complete(actions ...version.Action) Outcome {
	return Outcome{Actions: actions, Status: Complete}
}

// stopped returns a behaviour that gave up, and why.
func stopped(reason Reason) Outcome {
	return Outcome{Status: Stopped, Reason: reason}
}
