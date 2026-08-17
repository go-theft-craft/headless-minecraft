package main

import (
	"context"
	"fmt"

	"github.com/go-theft-craft/headless-minecraft/version"
)

// This is the outbound half of the seam. It turns one decision into one
// version-neutral intent and hands it to the client, which picks the packet.

// sender is the client's outbound path, narrowed to what this example uses so
// the tick loop can be tested without a connection.
type sender interface {
	Do(ctx context.Context, action version.Action) error
}

// Sender implements the movement half of Actuator over a client.
//
// It reports a position; it does not simulate a body. The library's action path
// is deliberately version-neutral and deliberately not physics — a caller says
// "I am here and I am on the ground" and the adapter chooses the packet — so
// everything between "walk toward that waypoint" and a coordinate is the
// example's arithmetic, and there is not much of it: step toward the target at
// a bounded speed, face it, and claim to be standing.
//
// What that leaves out is gravity, collision, step-up, and the jump the design
// asks for. Those are a movement kernel, they are M8's subject rather than this
// one's, and no consumer-facing seam exposes them yet. The consequence is
// stated rather than hidden: this bot walks a flat world and nothing else, and
// a server that disagrees with where it claims to be corrects it, which opens
// the breaker and ends the run.
type Sender struct {
	client sender
	// step is how far one update may move, in blocks. It is derived once from
	// the walk speed and the tick period rather than recomputed, so changing
	// the tick rate changes how often the bot reports rather than how fast it
	// claims to move.
	step float64
}

// NewSender returns the actuator for a client, walking at the bounds' speed.
func NewSender(client sender, bounds Bounds) Sender {
	return Sender{
		client: client,
		step:   bounds.WalkSpeed * bounds.Tick.Seconds(),
	}
}

// Step reports one position toward the target and returns the position it
// reported.
//
// It returns that position because nothing else will tell the caller. Observed
// state is what the server sent, and a server sends a position when it places
// the player or corrects it — never in acknowledgement of a move it accepted.
// A caller that reads its own position back from the snapshot therefore reads
// the same coordinate forever and walks nowhere, which is exactly what the
// first live run of this did: one step, then seventeen hundred identical
// updates. Where the bot thinks it is, is the bot's to remember.
//
// The jump flag is accepted and not acted on. The core sets it on every step
// rather than choosing per step, so it carries no decision to honour today, and
// honouring it would mean picking a Y — which, with no physics behind it, is a
// claim to be in the air that a server reads as flying. When a movement kernel
// exists this is where it attaches; until then, saying so here is better than a
// bot that quietly rises off the ground.
func (s Sender) Step(ctx context.Context, from, target Vec3, _ bool) (Vec3, error) {
	next := from.Toward(target, s.step)

	// MoveLook rather than Move: a bot that walks a circle without turning
	// faces one direction the whole way round, which is visible to anyone
	// watching and wrong about where it is going.
	err := s.client.Do(ctx, version.ActionMoveLook{
		X: next.X, Y: next.Y, Z: next.Z,
		Yaw: from.Yaw(target), Pitch: 0,
		// The example has no ground check. It walks a flat world and claims
		// the only thing that is true there; the moment that stops being true,
		// the server corrects it and the breaker says so.
		OnGround: true,
	})
	if err != nil {
		// The move did not happen, so the caller must not advance its idea of
		// where it is. Reporting the old position keeps prediction and the wire
		// in agreement about a step that never left.
		return from, err
	}

	return next, nil
}

// Attack swings at an entity.
func (Sender) Attack(context.Context, int32) error {
	return fmt.Errorf("%w: attack is M9.6", ErrNotYet)
}

// Respawn answers a death.
//
// It is the one action a dead client can send, and without it a bot that dies
// is finished: it cannot move, cannot fight, and cannot come back, so it lies
// there until someone stops the process. This example is what found that — it
// was killed by a slime on a live server and stood dead through the rest of the
// run — and the primitive was added for it.
func (s Sender) Respawn(ctx context.Context) error {
	return s.client.Do(ctx, version.ActionRespawn{})
}
