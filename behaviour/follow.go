package behaviour

import (
	"context"
	"errors"
	"fmt"

	simgeom "github.com/go-theft-craft/minecraft-simulation/geom"

	"github.com/go-theft-craft/headless-minecraft/safety"
	"github.com/go-theft-craft/headless-minecraft/version"
	"github.com/go-theft-craft/headless-minecraft/world"
)

// ErrNoTarget reports a following behaviour built without something to follow.
var ErrNoTarget = errors.New("behaviour: a follow needs a target entity")

// Motion is what a following behaviour is allowed to do with its body.
//
// Every field is the caller's, because every one of them is a claim about a
// body this package does not simulate. A step longer than the version's walking
// speed is a claim a server corrects; a give-up distance is a policy about how
// far a bot should chase.
type Motion struct {
	// Step is how far one tick may move the body, in blocks.
	Step float64
	// Arrive is how close counts as arrived, in blocks. A follow holds this
	// distance rather than closing it.
	Arrive float64
	// GiveUp is how far the target may get before the behaviour stops. Zero
	// means it never gives up on distance alone.
	GiveUp float64
	// Eye is how far above the feet the body looks from, in blocks. It is a
	// per-version, per-posture number and therefore an argument.
	Eye float64
	// Patience is how many ticks of no progress count as stuck. Zero means the
	// behaviour never reports being stuck, which is what a caller driving it
	// by hand wants.
	Patience int
}

// Follow walks the body toward a tracked entity and holds a distance from it.
//
// It is deliberately not a planner. Routing around terrain belongs to
// navigation, and the follower that walks a path belongs with it; this steps
// straight toward the target, which is what the client can already do end to
// end. When the navigation follower lands, this is where it attaches, and the
// shape of the behaviour does not change.
//
// Choosing whom to follow is the application's. That is the same division
// navigation keeps: goal selection is not the search's business.
type Follow struct {
	target int32
	motion Motion

	// closest is the nearest the body has been to the target, and idle counts
	// ticks since that improved. Together they are the stuck test: a body that
	// is walking but never getting closer is caught on something.
	closest float64
	idle    int
	started bool
}

// NewFollow returns a follow, refusing one it is not authorized for.
func NewFollow(
	authorization safety.Authorization,
	endpoint string,
	target int32,
	motion Motion,
) (*Follow, error) {
	if err := RequireScopes(authorization, endpoint, safety.ScopeObserve, safety.ScopeMove); err != nil {
		return nil, err
	}
	if target == 0 {
		return nil, fmt.Errorf("%w: entity 0", ErrNoTarget)
	}

	return &Follow{target: target, motion: motion}, nil
}

// Tick implements Behaviour.
func (f *Follow) Tick(_ context.Context, observed world.Snapshot) (Outcome, error) {
	self, ok := bodyOf(observed)
	if !ok {
		// The server has not placed the player yet. Waiting is right: it will,
		// and a behaviour that gave up here would give up on every join.
		return running(), nil
	}

	entity, tracked := observed.Entities.Get(f.target)
	if !tracked {
		// The target is gone — dead, out of range, or never there. That is the
		// world changing under the task rather than the task failing.
		return stopped(ReasonWorldChanged), nil
	}

	target := simgeom.Vec3{X: entity.X, Y: entity.Y, Z: entity.Z}
	distance := self.HorizontalDistance(target)

	if f.motion.GiveUp > 0 && distance > f.motion.GiveUp {
		return stopped(ReasonBlocked), nil
	}
	if outcome, stop := f.progress(distance); stop {
		return outcome, nil
	}
	if distance <= f.motion.Arrive {
		// Close enough. A follow holds its distance rather than closing it, so
		// this is a wait and not an arrival: the target will move again.
		return running(), nil
	}

	return running(f.stepToward(self, target)), nil
}

// progress records how the distance is trending and reports being stuck.
//
// A body walking into a wall sends a move every tick and never arrives, which
// looks identical to following from the outside. The distance closing is what
// tells them apart.
func (f *Follow) progress(distance float64) (Outcome, bool) {
	if !f.started || distance < f.closest-progressEpsilon {
		f.started, f.closest, f.idle = true, distance, 0

		return Outcome{}, false
	}

	f.idle++
	if f.motion.Patience > 0 && f.idle >= f.motion.Patience {
		return stopped(ReasonStuck), true
	}

	return Outcome{}, false
}

// progressEpsilon is how much closer counts as closer, in blocks.
//
// It is not zero because a body that oscillates within floating-point noise of
// one position would look like it were making progress forever, which is the
// state the stuck test exists to catch.
const progressEpsilon = 1e-6

// stepToward returns the one move that walks the body at the target.
func (f *Follow) stepToward(self, target simgeom.Vec3) version.Action {
	next := self.Toward(target, f.motion.Step)
	yaw, pitch := self.Add(simgeom.Vec3{Y: f.motion.Eye}).Look(target)

	return version.ActionMoveLook{
		X: next.X, Y: next.Y, Z: next.Z,
		Yaw: yaw, Pitch: pitch,
		// This package runs no physics, so the only thing it can say honestly
		// about the ground is what the caller's step assumes: a body walking a
		// surface. A version of this on top of the movement kernel says what
		// the kernel decided instead.
		OnGround: true,
	}
}

// Flee walks the body away from a threat until it is far enough off.
//
// It is Follow's mirror and shares its shape on purpose: the same distance
// bookkeeping, the same stuck test, and the same one move per tick. What differs
// is the direction and what counts as done — a flee finishes, where a follow
// holds a distance and goes on.
type Flee struct {
	threat int32
	motion Motion
	// Safe is how far away is far enough, in blocks.
	safe float64

	furthest float64
	idle     int
	started  bool
}

// NewFlee returns a flee, refusing one it is not authorized for.
func NewFlee(
	authorization safety.Authorization,
	endpoint string,
	threat int32,
	safe float64,
	motion Motion,
) (*Flee, error) {
	if err := RequireScopes(authorization, endpoint, safety.ScopeObserve, safety.ScopeMove); err != nil {
		return nil, err
	}
	if threat == 0 {
		return nil, fmt.Errorf("%w: entity 0", ErrNoTarget)
	}

	return &Flee{threat: threat, safe: safe, motion: motion}, nil
}

// Tick implements Behaviour.
func (f *Flee) Tick(_ context.Context, observed world.Snapshot) (Outcome, error) {
	self, ok := bodyOf(observed)
	if !ok {
		return running(), nil
	}

	entity, tracked := observed.Entities.Get(f.threat)
	if !tracked {
		// The threat is gone, which is the outcome a flee wanted. This is the
		// one place where losing track of an entity is success rather than the
		// world changing under the task.
		return complete(), nil
	}

	threat := simgeom.Vec3{X: entity.X, Y: entity.Y, Z: entity.Z}
	distance := self.HorizontalDistance(threat)
	if distance >= f.safe {
		return complete(), nil
	}

	if !f.started || distance > f.furthest+progressEpsilon {
		f.started, f.furthest, f.idle = true, distance, 0
	} else {
		f.idle++
		if f.motion.Patience > 0 && f.idle >= f.motion.Patience {
			// Cornered: running is not working and something is still hitting
			// the body. Saying so beats sending the same move forever.
			return stopped(ReasonStuck), nil
		}
	}

	// The full safe distance rather than one step. The step is clamped by
	// Toward on the way out, and a target recomputed one step at a time would
	// chase a threat that has also moved.
	away := simgeom.Away(self, threat, f.safe)
	next := self.Toward(away, f.motion.Step)
	// It looks where it is going rather than back at the threat. A body that
	// ran while facing behind it is a thing a watcher notices, and nothing here
	// needs to aim at what it is escaping.
	yaw, pitch := self.Add(simgeom.Vec3{Y: f.motion.Eye}).Look(away)

	return running(version.ActionMoveLook{
		X: next.X, Y: next.Y, Z: next.Z,
		Yaw: yaw, Pitch: pitch, OnGround: true,
	}), nil
}

// bodyOf returns where the server last put the player, and whether it has.
//
// A player the server has not placed and a player at the origin are different,
// and a behaviour that confused them would walk away from a coordinate nobody
// ever claimed the body was at.
func bodyOf(observed world.Snapshot) (simgeom.Vec3, bool) {
	if !observed.Player.Known || !observed.Player.Placed {
		return simgeom.Vec3{}, false
	}

	return simgeom.Vec3{X: observed.Player.X, Y: observed.Player.Y, Z: observed.Player.Z}, true
}
