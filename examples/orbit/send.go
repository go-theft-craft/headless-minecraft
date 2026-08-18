package main

import (
	"context"
	"errors"
	"fmt"
	"math"

	simentity "github.com/go-theft-craft/minecraft-simulation/entity"
	simgeom "github.com/go-theft-craft/minecraft-simulation/geom"
	simmovement "github.com/go-theft-craft/minecraft-simulation/movement"
	"github.com/go-theft-craft/minecraft-simulation/runtime"
	simulation "github.com/go-theft-craft/minecraft-simulation/sim"
	simworld "github.com/go-theft-craft/minecraft-simulation/world"

	"github.com/go-theft-craft/headless-minecraft/predict"
	"github.com/go-theft-craft/headless-minecraft/version"
	"github.com/go-theft-craft/headless-minecraft/world"
)

// This is the outbound half of the seam. It turns one decision into one
// version-neutral intent and hands it to the client, which picks the packet.

// sender is the client's outbound path and its view of the world, narrowed to
// what this example uses so the tick loop can be tested without a connection.
//
// The world is here because the body is simulated now. A movement kernel decides
// where a step lands by colliding against terrain, so it has to read the terrain
// the server described; a sender that could only write would have to be told
// what it was walking into by whoever called it.
type sender interface {
	Do(ctx context.Context, action version.Action) error
	World() world.Snapshot
}

// simBody is the entity the simulation moves. This bot has one body, so it is a
// constant rather than an argument.
const simBody = simentity.ID(1)

// simStore runs the simulated body against the world the server described.
//
// runtime.Memory already holds a body, its locomotion, and the revision check
// that applies a tick's change set. The one thing it holds that this bot must
// not use is its own block table: the terrain a prediction collides against is
// the server's, streamed and incomplete, and a store that answered from its own
// copy would simulate a world nobody is standing in. So Blocks is overridden and
// everything else is Memory's.
type simStore struct {
	*runtime.Memory
	view simworld.View
}

// Blocks implements runtime.Store, reading the observed world instead of the
// store's own.
func (s *simStore) Blocks() simworld.View { return s.view }

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
	// step is how far one update may move, in blocks. It is what the planner
	// prices a route with, and it is no longer what moves the body: the kernel
	// decides that from the input it is given. It is kept because the route's
	// cost model and the bot's arrival tests are both built on it.
	step float64

	// physics is the version's own movement rules, and the kernel that runs
	// them.
	physics Physics
	kernel  simulation.Kernel
	store   *simStore
	runner  *runtime.Runner
	// seeded says the simulated body has been placed. Until the server has put
	// the player somewhere there is nothing to simulate from.
	seeded bool
	// terrain returns the world the simulated body collides against.
	//
	// It is a function rather than a snapshot because the answer changes every
	// tick as chunks arrive, and it is a field rather than a call to the client
	// because it is a collaborator like the client is: a test that wants to
	// know what the kernel does on a floor should be able to hand it a floor,
	// the same way send_test hands this a client that records instead of
	// connecting.
	terrain func() simworld.View
	// sprinting is the last sprint state put on the wire.
	//
	// It is edge-triggered for the reason walking is: sprinting is a state a
	// client declares and the server keeps, so declaring it twenty times a
	// second is pressing a key that is already down. And it must be declared
	// rather than implied — a client that simply moves at a sprint is a client
	// the server corrects.
	sprinting bool

	// walking is the last locomotion state put on the wire, and nil until one
	// has been. A real client speaks when the state changes rather than every
	// tick, so this is what makes the difference between describing a body and
	// narrating one.
	walking *bool
	// mute records that this protocol does not carry locomotion, so the
	// refusal is asked for once rather than twenty times a second.
	mute bool
	// marked is every floor cell the trail has already painted.
	marked map[simgeom.BlockPos]bool
}

// eyeOffset raises a feet position to where a standing player looks from.
//
// It is a constant here and it should not be one anywhere that matters. Eye
// height is a per-version, per-posture number — 1.62 standing in 1.8.9, and
// something the profile supplies in 26.1.2 where a crouched body is shorter —
// and geom.AABB.Reaches takes an eye position rather than a body and an offset
// for exactly that reason. This example knows one posture and one height, so it
// writes the number it knows and says where the real one comes from.
var eyeOffset = simgeom.Vec3{Y: 1.62}

// NewSender returns the actuator for a client, moving the body by the version's
// own movement rules.
//
// A pointer, because it remembers what it last said about the body and, now,
// what the body is doing. Two senders on one connection would each keep half
// that history and contradict each other.
//
// It can fail, which it could not before. A kernel is built from a profile, and
// a profile that cannot produce one is a bot that cannot move — which is worth
// finding out here rather than on the first tick.
func NewSender(client sender, bounds Bounds, physics Physics) (*Sender, error) {
	kernel, err := simulation.NewKernel(physics.Profile)
	if err != nil {
		return nil, fmt.Errorf("build a movement kernel: %w", err)
	}

	store := &simStore{Memory: runtime.NewMemory(physics.Profile)}
	runner := runtime.NewRunner(store, kernel)
	runner.SetScope(simulation.Scope{Entities: []simentity.ID{simBody}})

	sender := &Sender{
		client:  client,
		step:    bounds.Step(),
		physics: physics,
		kernel:  kernel,
		store:   store,
		runner:  runner,
		marked:  map[simgeom.BlockPos]bool{},
	}
	sender.terrain = func() simworld.View {
		return predict.NewTerrain(client.World().Chunks, physics.Blocks, physics.Profile)
	}

	return sender, nil
}

// Locomotion declares what the body is doing: walking, or standing still.
//
// Reporting a position says where the player is, and says nothing about
// whether anybody is walking there. A server told only the coordinates has a
// player who slides; one told this has a player who walks. The bot never
// claims to sprint, and that omission is the whole of the difference between
// walking and running -- sprinting is a state a client declares, not a speed
// it moves at, so a client that declares nothing is walking by definition.
//
// It speaks only on a change, which is what a real client does. Sending the
// same state twenty times a second would be describing a key that is being
// held down as though it were being pressed again.
func (s *Sender) Locomotion(ctx context.Context, walking bool) error {
	if s.mute || (s.walking != nil && *s.walking == walking) {
		return nil
	}

	// Forward, because the bot faces where it is going: Step turns it toward
	// the target before it moves, so the direction it is holding is forward
	// and not a strafe.
	err := s.client.Do(ctx, version.ActionInput{Forward: walking})
	if err != nil {
		// A protocol that cannot carry this is not a broken run. 47 has no
		// input packet at all, and a bot that stopped walking over it would be
		// trading the thing that works for the thing that decorates it.
		if errors.Is(err, version.ErrUnsupportedAction) {
			s.mute = true
		}

		return err
	}

	s.walking = &walking

	return nil
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
// The jump flag is honoured, and honouring it is what the kernel is here for.
//
// This used to assert a position: it moved the reported point toward the target
// and claimed the body was on the ground, because there was nothing behind it
// that could say otherwise. That worked for a flat circle and nothing else. A
// jump was refused outright, because picking a Y with no physics behind it is a
// claim to be in the air that a server reads as flying.
//
// Now the body is simulated by the version's own rules. The bot hands the kernel
// an intent — hold forward, this yaw, sprinting, jumping — and reports where the
// kernel put the body. Gravity, collision, the step up, and the jump arc are all
// the game's arithmetic rather than this file's, which is why the arc is a shape
// the server accepts instead of a line through the air.
//
// What the caller gets back is still the position that went on the wire, for the
// reason it always was: a server sends a position to place or to correct, never
// to acknowledge, so nothing else can tell the bot where it now is.
func (s *Sender) Step(ctx context.Context, from, target simgeom.Vec3, jump bool) (simgeom.Vec3, error) {
	// The terrain a tick collides against is what the server has described as
	// of this tick. It is asked for each time rather than kept, because a chunk
	// that arrived since the last step is terrain the body should be able to
	// stand on.
	s.store.view = s.terrain()

	if err := s.place(from); err != nil {
		return from, err
	}

	// The look is computed from the eye rather than the feet, because that is
	// where a client looks from and because a pitch measured from the feet aims
	// at a target's ankles. Both angles come from one call: a caller that needs
	// one needs the other, and assembling an aim from two calls is two chances
	// to be given different endpoints.
	yaw, pitch := from.Add(eyeOffset).Look(target)

	// Sprint whenever there is enough ground left to be worth running across.
	// Close in it walks, so it does not overshoot the thing it is arriving at —
	// a sprint carries momentum the kernel will not cancel on the spot.
	sprint := from.HorizontalDistance(target) > s.step*sprintFrom
	if err := s.declareSprint(ctx, sprint); err != nil {
		return from, err
	}

	result, err := s.runner.Step(ctx, []simulation.Command{simmovement.Input{
		Entity: simBody,
		// Full forward, because the heading is the yaw: this bot turns toward
		// where it is going rather than strafing there.
		Forward: 1,
		Yaw:     yaw,
		Pitch:   pitch,
		Jump:    jump,
		Sprint:  sprint,
	}})
	if err != nil {
		return from, fmt.Errorf("simulate a tick: %w", err)
	}
	if !result.Completeness.Complete {
		// The tick needed a cell nobody has described, which happens at the
		// edge of what the server has streamed. Standing still for a tick is
		// the honest answer: the alternative is to guess what the body would
		// have collided with.
		return from, nil
	}

	body, ok := s.store.Entities().Entity(simBody)
	if !ok {
		return from, errNoBody
	}
	next := feet(body.Box)

	// MoveLook rather than Move: a bot that walks a circle without turning
	// faces one direction the whole way round, which is visible to anyone
	// watching and wrong about where it is going.
	if err := s.client.Do(ctx, version.ActionMoveLook{
		X: next.X, Y: next.Y, Z: next.Z,
		Yaw: yaw, Pitch: pitch,
		// The kernel decided this, which is the difference. A jumping bot
		// reports itself off the ground while it is, and back on it when it
		// lands, because that is what the simulation says happened.
		OnGround: body.OnGround,
	}); err != nil {
		// The move did not happen, so the caller must not advance its idea of
		// where it is. Reporting the old position keeps prediction and the wire
		// in agreement about a step that never left.
		return from, err
	}

	return next, nil
}

// sprintFrom is how many ticks of ground must remain before the bot runs,
// rather than walks, at it.
//
// Sprinting into a waypoint overshoots it: the kernel carries momentum, and a
// body that was running when it arrived keeps going. Walking the last stretch is
// what makes arrival settle instead of oscillate.
const sprintFrom = 6

// place puts the simulated body where the caller says the bot is, when the two
// have parted company.
//
// They part for two reasons and both need this. The first step of a run has no
// body to move yet. And a server correction moves the player without asking, so
// the position the caller carries is the server's and the simulation's is stale
// — continuing from the stale one would compound the disagreement the correction
// existed to settle.
func (s *Sender) place(at simgeom.Vec3) error {
	if s.seeded {
		if body, ok := s.store.Entities().Entity(simBody); ok && sameSpot(feet(body.Box), at) {
			return nil
		}
	}

	state, loco, ok := s.physics.Spawn(at, 0, 0)
	if !ok {
		return errNoBody
	}
	s.store.SetEntity(simBody, state)
	s.store.SetLocomotion(simBody, loco)
	s.seeded = true

	return nil
}

// sameSpot reports whether two positions are the same place.
//
// The tolerance is not zero because the simulation's position and the one the
// caller carries are computed differently and will disagree in their last bits.
// It is tiny because anything larger is a correction, and noticing a correction
// is what the caller of this wants.
func sameSpot(a, b simgeom.Vec3) bool {
	return math.Abs(a.X-b.X) < sameSpotWithin &&
		math.Abs(a.Y-b.Y) < sameSpotWithin &&
		math.Abs(a.Z-b.Z) < sameSpotWithin
}

const sameSpotWithin = 1e-6

// feet returns the point a body stands on: the middle of its box horizontally,
// and the bottom of it vertically.
//
// The box is read rather than the state's position because the two versions
// disagree about which of them is the original — 1.8.9 moves the box and leaves
// the position zero — and the box is the one both maintain.
func feet(box simgeom.AABB) simgeom.Vec3 {
	return simgeom.Vec3{
		X: (box.MinX + box.MaxX) / 2,
		Y: box.MinY,
		Z: (box.MinZ + box.MaxZ) / 2,
	}
}

// declareSprint tells the server the body started or stopped running.
//
// Sprinting is a declared state rather than a speed, so this is what the server
// keeps: a client that simply moves faster is a client the server corrects, and
// one that says it is sprinting is one the server lets run. A protocol that
// cannot carry it is not a broken run — the bot moves at the speed the kernel
// gives it either way, and only the server's own idea of the state is missing.
func (s *Sender) declareSprint(ctx context.Context, sprinting bool) error {
	if s.mute || s.sprinting == sprinting {
		return nil
	}

	if err := s.client.Do(ctx, version.ActionSprint{Sprinting: sprinting}); err != nil {
		if errors.Is(err, version.ErrUnsupportedAction) {
			return nil
		}

		return err
	}
	s.sprinting = sprinting

	return nil
}

// Attack hits an entity: the swing a watcher sees, then the blow.
//
// Both, and in that order, because they are two packets and a real client
// sends both. The animation is what makes an attack visible to everyone else;
// the interaction is what does the damage. A client that sent only the second
// would hurt things without appearing to move.
func (s *Sender) Attack(ctx context.Context, id int32) error {
	if err := s.client.Do(ctx, version.ActionSwing{}); err != nil {
		return fmt.Errorf("swing: %w", err)
	}

	return s.client.Do(ctx, version.ActionInteract{
		Entity: id,
		Kind:   version.InteractAttack,
	})
}

// Kill asks the server to kill the bot.
//
// The way out of a hole nothing can be dug or walked out of. It is a command,
// so it needs the bot opped, and an unopped one is refused and stays where it
// is -- which is the same place it was going to stay anyway.
func (s *Sender) Kill(ctx context.Context) error {
	return s.client.Do(ctx, version.ActionCommand{Command: "kill"})
}

// Mark paints the floor under a position with stone.
//
// The floor, one block down, and never the cell the route runs through: a
// marker in the path is a wall, the planner would route around its own trail,
// and the bot would spend the run drawing a maze for itself. Stone under grass
// is what makes a walked route legible from above.
//
// It runs a command rather than placing a block, because placing one needs an
// item in a hand this bot has no inventory for, and because a trail is a thing
// somebody switches on to look at rather than gameplay. The server has to be
// willing: an unopped bot gets its command refused and the run carries on with
// no trail and no error, which is the right failure for a debugging aid.
//
// Each cell is painted once. A route is replanned every waypoint and the legs
// overlap, so without this the same command goes out every few ticks for the
// length of the run.
func (s *Sender) Mark(ctx context.Context, at simgeom.Vec3) error {
	floor := floorOf(at)
	floor.Y--

	if s.marked[floor] {
		return nil
	}
	s.marked[floor] = true

	return s.client.Do(ctx, version.ActionCommand{
		Command: fmt.Sprintf("setblock %d %d %d stone", floor.X, floor.Y, floor.Z),
	})
}

// Respawn answers a death.
//
// It is the one action a dead client can send, and without it a bot that dies
// is finished: it cannot move, cannot fight, and cannot come back, so it lies
// there until someone stops the process. This example is what found that — it
// was killed by a slime on a live server and stood dead through the rest of the
// run — and the primitive was added for it.
func (s *Sender) Respawn(ctx context.Context) error {
	return s.client.Do(ctx, version.ActionRespawn{})
}
