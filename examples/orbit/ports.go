package main

import (
	"context"
	"errors"
)

// This file is the whole seam between the decision core and the library. The
// core reads World and the shell drives Actuator; neither imports the client.
//
// M7 landed and World is now real: Observed in observed.go implements it over
// one world.Snapshot, and the core did not change to accept it, which is the
// property the split was chosen for. Actuator is still Pending, because the
// actions it names belong to M8.8 and M9.6.

// Self is the local player's observed state.
type Self struct {
	Position Vec3
	Health   float64
	OnGround bool
}

// Entity is a tracked entity the bot may fight.
type Entity struct {
	ID       int32
	Position Vec3
	Health   float64
	Alive    bool
}

// World is observed state, read-only, as of one snapshot revision.
//
// Supplied by M7. See Observed.
type World interface {
	// Spawn reports the spawn position the server sent, which is the orbit
	// centre. It is the compass target and not a separate world landmark: a
	// vanilla server sends the level's shared spawn on join and re-sends the
	// same packet when the player's respawn point moves, so a bot that slept
	// would find its circle recentred. This one never sleeps.
	Spawn() (Vec3, bool)
	// Route plans a way between two positions and reports the positions to
	// walk through. The second result is false when nothing was reachable at
	// all, which the caller answers by skipping the waypoint rather than by
	// walking at it anyway.
	//
	// Routing is a question and not a fact about the world, which is why it is
	// on this port rather than beside the block reader it replaced. The core
	// asks for a way and never learns what terrain is; that is what lets the
	// decision tests script three points and run the whole state machine
	// without a chunk in sight.
	Route(from, to Vec3) (Route, bool)
	// Entity reports one tracked entity by ID.
	Entity(id int32) (Entity, bool)
}

// Actuator is the actions the bot takes.
//
// Owed by M9. Step is M8.8 and M9.3, Attack is M9.6, and Respawn is the
// primitive M9 does not currently plan — see the design's Required surface.
type Actuator interface {
	// Step emits one movement update from where the bot is toward a target,
	// jumping if asked. It is one update and not a loop: the tick that drives
	// this program is the example's own, because the library ships no
	// scheduler.
	//
	// It takes the current position because the outbound path reports where the
	// player is rather than accepting a destination — the caller says "I am
	// here now", and computing the next "here" needs the last one. Nothing in
	// the library will do that arithmetic, because doing it is movement, and
	// movement is a mechanic the library leaves to its consumer.
	//
	// It returns the position it reported, because the library will not: a
	// server sends a position to place or to correct, never to acknowledge,
	// so the snapshot cannot answer "where am I now" for a bot that is
	// walking. The caller carries that between ticks.
	Step(ctx context.Context, from, target Vec3, jump bool) (Vec3, error)
	// Attack swings at an entity. Timing is the version profile's cooldown,
	// not a constant here, because 1.8.9 and 26.1.2 disagree and the example
	// must not encode either.
	//
	// Nothing in this example calls it: the bot runs from what hits it rather
	// than hitting back, so the one action it cannot perform is also the one
	// it never asks for. It stays on the port because the library still owes
	// it and a consumer that wants to fight will need it, and because deleting
	// the surface would hide that M9.6 is unfinished rather than record it.
	Attack(ctx context.Context, id int32) error
	// Locomotion declares whether the body is walking or standing, so that a
	// watcher sees a player rather than a position that changes. It is
	// separate from Step because it is edge-triggered: the state changes far
	// less often than the position does.
	Locomotion(ctx context.Context, walking bool) error
	// Respawn answers a death.
	Respawn(ctx context.Context) error
}

// ErrNotYet reports a capability that a milestone still owes.
var ErrNotYet = errors.New("not available yet")

// Missing lists what this example still needs, in the order a reader should
// care about it. It is printed on startup so the program says what it cannot do
// before it tries.
func Missing() []string {
	return []string{
		"none: gravity, collision, and the jump the design asks for; the " +
			"action path reports a position and simulates no body, so this " +
			"bot walks a flat world and nothing else",
	}
}
