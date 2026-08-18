package main

import (
	"context"
	"errors"
	"math"
	"testing"

	simgeom "github.com/go-theft-craft/minecraft-simulation/geom"
	simulation "github.com/go-theft-craft/minecraft-simulation/sim"
	simworld "github.com/go-theft-craft/minecraft-simulation/world"

	"github.com/go-theft-craft/headless-minecraft/version"
	"github.com/go-theft-craft/headless-minecraft/world"
)

// captured is a client that records the intents sent to it.
type captured struct {
	actions []version.Action
	err     error
}

func (c *captured) Do(_ context.Context, action version.Action) error {
	c.actions = append(c.actions, action)

	return c.err
}

// World satisfies the sender's view of the world. It is empty because these
// tests hand the sender its terrain directly; see testSender.
func (c *captured) World() world.Snapshot { return world.Snapshot{} }

// ground is a flat world: solid below a height, air above it.
//
// The kernel collides against this, so the tests need one. A body with real
// physics and nothing under it falls, which is correct and is not what most of
// these tests are about.
type ground struct {
	floor int32
	solid simworld.BlockRef
}

func (g ground) CollisionShape(pos simgeom.BlockPos) (simgeom.Shape, simworld.Lookup) {
	if pos.Y < g.floor {
		return simgeom.FullCube(), simworld.LookupShape
	}

	return simgeom.EmptyShape(), simworld.LookupAir
}

func (g ground) BlockState(pos simgeom.BlockPos) (simworld.BlockRef, simworld.Lookup) {
	if pos.Y < g.floor {
		return g.solid, simworld.LookupShape
	}

	return 0, simworld.LookupAir
}

// testSender builds a sender over the 1.8.9 rules, standing on a floor at y=4.
//
// The profile is the real one, because the whole point of the kernel being here
// is that the arithmetic is the game's. The block under the body is resolved by
// name through the profile that minted it rather than written as a number: a
// handle means nothing outside its own profile, and a made-up one is a block the
// tick cannot resolve.
func testSender(t *testing.T, client sender, bounds Bounds) *Sender {
	t.Helper()

	navigator, err := NewNavigator(true, bounds)
	if err != nil {
		t.Fatalf("NewNavigator: %v", err)
	}
	physics := navigator.Physics()

	names, ok := physics.Profile.(simulation.BlockNames)
	if !ok {
		t.Fatal("the profile cannot resolve block names")
	}
	stone, ok := names.Ref("stone")
	if !ok {
		t.Fatal("the profile does not know stone")
	}

	built, err := NewSender(client, bounds, physics)
	if err != nil {
		t.Fatalf("NewSender: %v", err)
	}
	built.terrain = func() simworld.View { return ground{floor: 4, solid: stone} }

	return built
}

// standing is where the fixtures start: on top of the floor testSender lays.
var standing = simgeom.Vec3{X: 0, Y: 4, Z: 0}

// settled returns a sender whose body is walking toward a target with its feet
// on the ground, and the position it has reached, with the recording cleared.
//
// Two ticks, not one, and the reason is worth stating. A box resting exactly on
// a block face does not intersect it — shared faces are excluded, which is what
// stops a standing body colliding with the ground every tick forever — so a
// freshly placed body is genuinely unsupported until gravity has pressed it down
// once. The first tick is what establishes contact and reports the body airborne;
// the second is the first one of an ordinary walk. A test about walking, or about
// jumping, wants a body that is already standing.
func settled(t *testing.T, client *captured, bounds Bounds, target simgeom.Vec3) (*Sender, simgeom.Vec3) {
	t.Helper()

	sender := testSender(t, client, bounds)

	at := standing
	for range 2 {
		next, err := sender.Step(t.Context(), at, target, false)
		if err != nil {
			t.Fatalf("settling step: %v", err)
		}
		at = next
	}
	client.actions = nil

	return sender, at
}

// moveOf returns the one movement update in a recording.
//
// It is picked out by type rather than by index because a step may put a sprint
// declaration on the wire ahead of it, and a test that reached for actions[0]
// would be comparing two sprint flags while calling itself a movement
// assertion. That is exactly how the old jump test kept passing after it stopped
// being true.
func moveOf(t *testing.T, actions []version.Action) version.ActionMoveLook {
	t.Helper()

	var found []version.ActionMoveLook
	for _, action := range actions {
		if move, ok := action.(version.ActionMoveLook); ok {
			found = append(found, move)
		}
	}
	if len(found) != 1 {
		t.Fatalf("recording holds %d movement updates, want one: %+v", len(found), actions)
	}

	return found[0]
}

func TestOneStepReportsOneBoundedMove(t *testing.T) {
	t.Parallel()

	// The bot no longer decides how far it moves — the kernel does, from the
	// input it is handed. So this asserts the shape of one step rather than an
	// exact distance: it goes toward the target, it does not teleport, and the
	// position handed back to the caller is the one that went on the wire.
	//
	// The distance is the game's now, and it is deliberately not written down
	// here. A body accelerates from rest, so the first tick covers less ground
	// than a later one, and a literal would be this test asserting the kernel
	// against a number this file made up.
	client := &captured{}
	far := simgeom.Vec3{X: 100, Y: 4, Z: 0}
	sender, at := settled(t, client, DefaultBounds(), far)

	next, err := sender.Step(t.Context(), at, far, false)
	if err != nil {
		t.Fatalf("step returned %v", err)
	}

	move := moveOf(t, client.actions)

	if move.X <= at.X {
		t.Errorf("moved to x=%v, which is not toward a target at x=100", move.X)
	}
	// One tick is fifty milliseconds. A body that covered a whole block in that
	// time is a body the server corrects.
	if move.X-at.X > 1 {
		t.Errorf("moved %v blocks in one tick, which is not walking", move.X-at.X)
	}
	if !move.OnGround {
		t.Error("a walking body reported itself off the ground")
	}
	if move.Y != standing.Y {
		t.Errorf("a walk on level ground changed height to %v", move.Y)
	}

	// The caller has to be told where it ended up, because nothing else will:
	// a server sends a position to place or to correct, never to acknowledge.
	if next.X != move.X || next.Y != move.Y || next.Z != move.Z {
		t.Errorf("returned %+v but reported %v,%v,%v", next, move.X, move.Y, move.Z)
	}
}

func TestAStepTurnsTowardWhereItIsGoing(t *testing.T) {
	t.Parallel()

	// MoveLook rather than Move: a bot that walks a circle without turning
	// faces one direction the whole way round.
	client := &captured{}

	if _, err := testSender(t, client, DefaultBounds()).Step(
		t.Context(),
		simgeom.Vec3{X: 0, Y: 4, Z: 0},
		simgeom.Vec3{X: -100, Y: 4, Z: 0},
		true,
	); err != nil {
		t.Fatalf("step returned %v", err)
	}

	move := moveOf(t, client.actions)
	if math.Abs(float64(move.Yaw)-90) > 1e-4 {
		t.Errorf("faced yaw %v walking west, want 90", move.Yaw)
	}
}

func TestAFailedSendIsReported(t *testing.T) {
	t.Parallel()

	// A prediction the server never heard about is a prediction that will be
	// corrected, so a write that did not happen must not look like one that
	// did.
	sentinel := errors.New("connection reset")
	client := &captured{err: sentinel}

	from := simgeom.Vec3{X: 7, Y: 4, Z: 9}
	next, err := testSender(t, client, DefaultBounds()).Step(t.Context(), from, simgeom.Vec3{X: 100}, true)
	if !errors.Is(err, sentinel) {
		t.Errorf("step returned %v, want it to carry %v", err, sentinel)
	}

	// A step that never left the process must not advance the caller's idea of
	// where it is, or prediction and the wire disagree from then on.
	if next != from {
		t.Errorf("a failed send moved the bot to %+v, want it to stay at %+v", next, from)
	}
}

func TestTheJumpFlagLeavesTheGround(t *testing.T) {
	t.Parallel()

	// This test used to assert the opposite, and it was right to: with no
	// physics behind it, honouring a jump meant picking a height, and a height
	// picked rather than simulated is a claim to be flying. The kernel is what
	// changed — the arc is the game's arithmetic now, so the flag can be
	// honoured and the difference shows on the wire.
	target := simgeom.Vec3{X: 10, Y: 4, Z: 0}

	jumped, walked := &captured{}, &captured{}
	jumper, jumpFrom := settled(t, jumped, DefaultBounds(), target)
	walker, walkFrom := settled(t, walked, DefaultBounds(), target)

	if _, err := jumper.Step(t.Context(), jumpFrom, target, true); err != nil {
		t.Fatal(err)
	}
	if _, err := walker.Step(t.Context(), walkFrom, target, false); err != nil {
		t.Fatal(err)
	}

	jumping, walking := moveOf(t, jumped.actions), moveOf(t, walked.actions)

	if jumping.Y <= walking.Y {
		t.Errorf("jumping reached y=%v and walking reached y=%v; the jump should rise",
			jumping.Y, walking.Y)
	}
	if jumping.OnGround {
		t.Error("a jumping body reported itself on the ground")
	}
	if !walking.OnGround {
		t.Error("a walking body reported itself off the ground")
	}
	if walking.Y != walkFrom.Y {
		t.Errorf("a walk on level ground changed height to %v", walking.Y)
	}
}

// TestTheTickAfterAPlacementEstablishesContact records the one tick that reports
// a standing body as airborne, so it is a known property rather than a puzzle.
//
// A box resting exactly on a block face does not intersect it: shared faces are
// excluded, which is what stops a standing body colliding with the ground every
// tick forever. So a freshly placed body is genuinely unsupported until gravity
// has pressed it down once, and the tick that does that is the one that reports
// it off the ground. Nothing moves vertically while it happens.
func TestTheTickAfterAPlacementEstablishesContact(t *testing.T) {
	t.Parallel()

	client := &captured{}
	sender := testSender(t, client, DefaultBounds())

	first, err := sender.Step(t.Context(), standing, standing, false)
	if err != nil {
		t.Fatal(err)
	}
	opening := moveOf(t, client.actions)
	if opening.OnGround {
		t.Error("the tick that establishes contact reported the body already supported")
	}
	if first.Y != standing.Y {
		t.Errorf("establishing contact moved the body to y=%v", first.Y)
	}

	client.actions = nil
	if _, err := sender.Step(t.Context(), first, standing, false); err != nil {
		t.Fatal(err)
	}
	if !moveOf(t, client.actions).OnGround {
		t.Error("the body is still not on the ground a tick after contact")
	}
}

func TestTheBotDeclaresSprintingRatherThanJustMovingFaster(t *testing.T) {
	t.Parallel()

	// Sprinting is a state the server keeps, not a speed it infers. A client
	// that simply moves faster is a client the server corrects; one that says it
	// is sprinting is one the server lets run. So the declaration has to go out,
	// and once rather than every tick — a held key is not pressed again twenty
	// times a second.
	// Not settled: the declaration goes out on the first tick that wants to run,
	// and settling would spend it before the recording started.
	client := &captured{}
	far := simgeom.Vec3{X: 100, Y: 4, Z: 0}
	sender := testSender(t, client, DefaultBounds())

	at := standing
	for range 5 {
		next, err := sender.Step(t.Context(), at, far, false)
		if err != nil {
			t.Fatal(err)
		}
		at = next
	}

	var sprints []version.ActionSprint
	for _, action := range client.actions {
		if sprint, ok := action.(version.ActionSprint); ok {
			sprints = append(sprints, sprint)
		}
	}

	if len(sprints) != 1 {
		t.Fatalf("declared sprinting %d times over five ticks, want once: %+v", len(sprints), sprints)
	}
	if !sprints[0].Sprinting {
		t.Error("declared a stop rather than a start while running at a distant target")
	}
}

func TestTheBotWalksTheLastStretchRatherThanSprintingIntoIt(t *testing.T) {
	t.Parallel()

	// A sprint carries momentum the kernel will not cancel on arrival, so a bot
	// that ran all the way to a waypoint would overshoot and come back for it.
	// Walking the last few ticks is what makes arrival settle.
	client := &captured{}
	sender, at := settled(t, client, DefaultBounds(), simgeom.Vec3{X: 100, Y: 4, Z: 0})
	near := simgeom.Vec3{X: at.X + 0.1, Y: 4, Z: 0}

	if _, err := sender.Step(t.Context(), at, near, false); err != nil {
		t.Fatal(err)
	}

	for _, action := range client.actions {
		if sprint, ok := action.(version.ActionSprint); ok && sprint.Sprinting {
			t.Error("sprinted at a target a tenth of a block away")
		}
	}
}

func TestRespawnIsSentAsAnIntent(t *testing.T) {
	t.Parallel()

	// A dead bot can send nothing else, so this is the one action that has to
	// work while dead. Found the hard way: this example was killed by a slime
	// on a live server and had no way to answer.
	client := &captured{}

	if err := testSender(t, client, DefaultBounds()).Respawn(t.Context()); err != nil {
		t.Fatalf("respawn returned %v", err)
	}
	if len(client.actions) != 1 {
		t.Fatalf("sent %d actions, want 1", len(client.actions))
	}
	if _, ok := client.actions[0].(version.ActionRespawn); !ok {
		t.Errorf("sent %T, want version.ActionRespawn", client.actions[0])
	}
}

func TestWalkingAccumulatesAcrossSteps(t *testing.T) {
	t.Parallel()

	// The bug this pins cost ninety seconds of a live run: the loop read its
	// own position back from observed state, which holds what the server sent
	// and is never updated by a move the server merely accepted. Every tick
	// stepped from the same coordinate, so the bot sent one step and then
	// seventeen hundred identical updates while standing still.
	//
	// Feeding each step's result into the next is what makes walking add up.
	// What it adds up to is the kernel's business — a body accelerates, so ten
	// ticks is not ten times the first one — so this asserts that the ground
	// covered grows and that no two updates repeat a position, which is what the
	// bug looked like.
	client := &captured{}
	target := simgeom.Vec3{X: 100, Y: 4, Z: 0}
	sender, at := settled(t, client, DefaultBounds(), target)

	previous := at.X
	for tick := range 10 {
		next, err := sender.Step(t.Context(), at, target, false)
		if err != nil {
			t.Fatal(err)
		}
		if next.X <= previous {
			t.Fatalf("tick %d moved from x=%v to x=%v, which is not progress", tick, previous, next.X)
		}
		previous, at = next.X, next
	}

	// Ten ticks of walking is somewhere around two blocks. The bound is loose on
	// purpose: it is here to catch a body that teleported or stood still, not to
	// restate the kernel's arithmetic.
	if at.X < 1 || at.X > 3 {
		t.Errorf("ten ticks of walking reached x=%v, which is not a walk", at.X)
	}

	// And each one has to be a distinct position on the wire, not the same
	// coordinate repeated.
	seen := map[float64]bool{}
	for _, action := range client.actions {
		if move, ok := action.(version.ActionMoveLook); ok {
			seen[move.X] = true
		}
	}
	if len(seen) != 10 {
		t.Errorf("sent %d distinct positions in 10 steps, want 10", len(seen))
	}
}

// spy records the intents an actuator puts on the wire.
type spy struct {
	actions []version.Action
	err     error
}

func (s *spy) Do(_ context.Context, action version.Action) error {
	if s.err != nil {
		return s.err
	}
	s.actions = append(s.actions, action)

	return nil
}

// World satisfies the sender view. The spy records what goes out; the terrain
// these tests run against is handed over by testSender.
func (s *spy) World() world.Snapshot { return world.Snapshot{} }

// TestLocomotionSpeaksOnlyWhenTheStateChanges pins the edge trigger.
//
// A real client reports the keys it is holding when they change. Repeating the
// same state twenty times a second would describe a held key as though it were
// being pressed again, and it would put nineteen packets a second on a
// connection to say nothing.
func TestLocomotionSpeaksOnlyWhenTheStateChanges(t *testing.T) {
	t.Parallel()

	client := &spy{}
	sender := testSender(t, client, DefaultBounds())

	for _, walking := range []bool{true, true, true, false, false, true} {
		if err := sender.Locomotion(t.Context(), walking); err != nil {
			t.Fatalf("Locomotion(%v): %v", walking, err)
		}
	}

	want := []bool{true, false, true}
	if len(client.actions) != len(want) {
		t.Fatalf("sent %d intents, want %d: %+v", len(client.actions), len(want), client.actions)
	}
	for i, forward := range want {
		input, ok := client.actions[i].(version.ActionInput)
		if !ok {
			t.Fatalf("intent %d is %T, want version.ActionInput", i, client.actions[i])
		}
		if input.Forward != forward {
			t.Errorf("intent %d holds forward=%v, want %v", i, input.Forward, forward)
		}
		// Never. Sprinting is a declared state, and declaring none is what
		// makes this a walk.
		if input.Sprint {
			t.Errorf("intent %d claims to be sprinting", i)
		}
	}
}

// TestLocomotionStopsAskingOnceTheProtocolRefuses pins that a protocol without
// an input packet is asked once, not twenty times a second for the whole run.
func TestLocomotionStopsAskingOnceTheProtocolRefuses(t *testing.T) {
	t.Parallel()

	client := &spy{err: version.UnsupportedAction("java/1.8.9", version.ActionInput{})}
	sender := testSender(t, client, DefaultBounds())

	if err := sender.Locomotion(t.Context(), true); !errors.Is(err, version.ErrUnsupportedAction) {
		t.Fatalf("first call returned %v, want the refusal", err)
	}
	for range 5 {
		if err := sender.Locomotion(t.Context(), false); err != nil {
			t.Fatalf("kept reporting the refusal: %v", err)
		}
	}
}

// TestTheTrailPaintsTheFloorAndNotThePath pins where a marker goes.
//
// One block down. A marker in the cell the route runs through is a wall: the
// planner would route around the bot's own trail, and the run would be spent
// drawing a maze for itself.
func TestTheTrailPaintsTheFloorAndNotThePath(t *testing.T) {
	t.Parallel()

	client := &spy{}
	sender := testSender(t, client, DefaultBounds())

	if err := sender.Mark(t.Context(), simgeom.Vec3{X: 10.5, Y: 64, Z: -3.5}); err != nil {
		t.Fatalf("Mark: %v", err)
	}
	if len(client.actions) != 1 {
		t.Fatalf("sent %d intents, want 1", len(client.actions))
	}

	command, ok := client.actions[0].(version.ActionCommand)
	if !ok {
		t.Fatalf("intent is %T, want version.ActionCommand", client.actions[0])
	}
	// Floored, not truncated: z=-3.5 is in cell -4. Truncation would fold
	// -3.5 and 3.5 onto cells of opposite sign and paint the trail one block
	// out on the negative half of the circle, which is the bug simgeom.Vec3.Floor
	// exists to avoid.
	if command.Command != "setblock 10 63 -4 stone" {
		t.Errorf("ran %q, want the floor at y=63 under the step", command.Command)
	}
}

// TestTheTrailPaintsEachCellOnce pins that the trail is not resent.
//
// A route is replanned at every waypoint and consecutive routes overlap, so a
// trail that did not remember itself would run the same command every few
// ticks for the length of the run.
func TestTheTrailPaintsEachCellOnce(t *testing.T) {
	t.Parallel()

	client := &spy{}
	sender := testSender(t, client, DefaultBounds())

	for _, at := range []simgeom.Vec3{
		{X: 10.1, Y: 64, Z: -3.9},
		{X: 10.5, Y: 64, Z: -3.5},
		{X: 10.9, Y: 64, Z: -3.1},
		{X: 11.5, Y: 64, Z: -3.5},
	} {
		if err := sender.Mark(t.Context(), at); err != nil {
			t.Fatalf("Mark(%+v): %v", at, err)
		}
	}

	if len(client.actions) != 2 {
		t.Errorf("painted %d cells, want 2", len(client.actions))
	}
}

// TestAStepSendsAComputedPitch is the acceptance criterion for the aiming work.
//
// Before it, Step sent a literal zero, so the bot could not look at anything
// above or below its own eyes and every aimed primitive was blocked behind that.
// What this asserts is both halves of the change: the angle is computed now, and
// walking a flat circle still looks level — the observable behaviour of this bot
// is unchanged, which is what makes the change safe to have made.
func TestAStepSendsAComputedPitch(t *testing.T) {
	t.Parallel()

	bounds := DefaultBounds()

	for _, test := range []struct {
		name   string
		from   simgeom.Vec3
		target simgeom.Vec3
		want   float32
	}{
		{
			// The circle is flat and the eye is level with nothing in
			// particular, so the pitch stays where it has always been.
			name:   "a flat walk still looks level",
			from:   simgeom.Vec3{X: 0, Y: 64, Z: 0},
			target: simgeom.Vec3{X: 0, Y: 65.62, Z: 100},
			want:   0,
		},
		{
			// A target at the bot's own feet is below its eyes, and the pitch
			// says so. Positive is down, which is the game's convention and not
			// the intuitive one.
			name:   "a target underfoot looks down",
			from:   simgeom.Vec3{X: 0, Y: 64, Z: 0},
			target: simgeom.Vec3{X: 0, Y: 64, Z: 1.62},
			want:   45,
		},
		{
			name:   "a target overhead looks up",
			from:   simgeom.Vec3{X: 0, Y: 64, Z: 0},
			target: simgeom.Vec3{X: 0, Y: 64 + 1.62 + 1.62, Z: 1.62},
			want:   -45,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			client := &captured{}
			if _, err := testSender(t, client, bounds).Step(t.Context(), test.from, test.target, true); err != nil {
				t.Fatalf("Step: %v", err)
			}

			move := moveOf(t, client.actions)
			if math.Abs(float64(move.Pitch-test.want)) > 1e-3 {
				t.Errorf("Pitch = %v, want %v", move.Pitch, test.want)
			}
		})
	}
}
