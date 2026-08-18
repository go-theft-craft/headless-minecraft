package main

import (
	"context"
	"errors"

	simgeom "github.com/go-theft-craft/minecraft-simulation/geom"
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
	Position simgeom.Vec3
	Health   float64
	// OnGround reports that the body is standing on something rather than in
	// the air. The tick loop fills it from the actuator, which simulates the
	// body; the snapshot cannot answer it, because for a client's own body the
	// server only ever echoes the client's last claim.
	//
	// It gates the checks that ask "could the body stand here", because a body
	// mid-jump cannot stand anywhere and asking anyway makes every airborne
	// tick look like the world changed.
	OnGround bool
	// OnFire reports that the player is burning, read from the burning bit of
	// its own entity metadata. It stays true for as long as the fire lasts,
	// which is well after the bot has left whatever lit it.
	OnFire bool
}

// Entity is a tracked entity the bot may fight.
type Entity struct {
	ID       int32
	Position simgeom.Vec3
	Health   float64
	Alive    bool
	// Kind is what this client can say about the entity: a name for a log
	// line, and whether it is the sort of thing that follows a bot walking
	// away. Named reports whether the client could say anything at all -- a
	// modded server spawns types no data set has heard of, and the caller must
	// not read the zero value as "harmless".
	Kind  Kind
	Named bool
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
	Spawn() (simgeom.Vec3, bool)
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
	Route(from, to simgeom.Vec3) (Route, bool)
	// Walkable reports whether the body can still walk a straight line between
	// two positions.
	//
	// A route is planned once and walked over many ticks, and the world does
	// not hold still for it: somebody pours lava in front of a bot that is
	// already committed to a way through. This is how the core asks whether
	// the next stretch is still the stretch it planned across, and it is a
	// cheaper question than planning again.
	Walkable(from, to simgeom.Vec3) bool
	// Hurting reports whether the body at a position is standing in something
	// that damages it.
	//
	// Narrower than Walkable on purpose. Walkable is false for a wall, a hole
	// and an unstreamed chunk as well as for lava, and a bot that read "not
	// walkable" as "I am burning" would panic every time it reached the edge
	// of the world the server has sent it.
	Hurting(at simgeom.Vec3) bool
	// Water finds the nearest water the body could stand in within a radius of
	// cells, for a bot that is on fire and would rather not be.
	Water(from simgeom.Vec3, within int) (simgeom.Vec3, bool)
	// Safe finds the nearest cell within a radius that does not hurt to stand
	// in, for a bot that is standing in something that does.
	Safe(from simgeom.Vec3, within int) (simgeom.Vec3, bool)
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
	Step(ctx context.Context, from, target simgeom.Vec3, jump bool) (simgeom.Vec3, error)
	// Attack hits an entity.
	Attack(ctx context.Context, id int32) error
	// Locomotion declares whether the body is walking or standing, so that a
	// watcher sees a player rather than a position that changes. It is
	// separate from Step because it is edge-triggered: the state changes far
	// less often than the position does.
	Locomotion(ctx context.Context, walking bool) error
	// Mark paints the floor under a position, so a route can be seen from
	// inside the game rather than inferred from a log.
	//
	// On the Actuator because it is something the bot does to the world, and
	// separate from Step because it is a debugging aid: a run with it off must
	// send nothing at all.
	Mark(ctx context.Context, at simgeom.Vec3) error
	// Kill asks the server to kill the bot, which is how it leaves a trap
	// nothing else gets it out of.
	Kill(ctx context.Context) error
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
