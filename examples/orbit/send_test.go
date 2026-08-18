package main

import (
	"context"
	"errors"
	"math"
	"testing"

	simgeom "github.com/go-theft-craft/minecraft-simulation/geom"

	"github.com/go-theft-craft/headless-minecraft/version"
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

func TestOneStepReportsOneBoundedMove(t *testing.T) {
	t.Parallel()

	// The bot claims to walk, and the only thing keeping that claim plausible
	// is how far it moves per update. At the shipped four blocks a second on a
	// fifty-millisecond tick that is a fifth of a block, under vanilla's own
	// walking speed.
	client := &captured{}
	bounds := DefaultBounds()

	next, err := NewSender(client, bounds).Step(
		t.Context(),
		simgeom.Vec3{X: 0, Y: 4, Z: 0},
		simgeom.Vec3{X: 100, Y: 4, Z: 0},
		true,
	)
	if err != nil {
		t.Fatalf("step returned %v", err)
	}

	if len(client.actions) != 1 {
		t.Fatalf("sent %d actions, want 1", len(client.actions))
	}

	move, ok := client.actions[0].(version.ActionMoveLook)
	if !ok {
		t.Fatalf("sent %T, want version.ActionMoveLook", client.actions[0])
	}
	if want := bounds.WalkSpeed * bounds.Tick.Seconds(); math.Abs(move.X-want) > 1e-9 {
		t.Errorf("moved %v blocks, want %v", move.X, want)
	}
	if move.Y != 4 {
		t.Errorf("moved to y=%v, want to hold 4: this example simulates no body", move.Y)
	}
	if !move.OnGround {
		t.Error("reported not on the ground, which is a claim to be falling")
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

	if _, err := NewSender(client, DefaultBounds()).Step(
		t.Context(),
		simgeom.Vec3{X: 0, Y: 4, Z: 0},
		simgeom.Vec3{X: -100, Y: 4, Z: 0},
		true,
	); err != nil {
		t.Fatalf("step returned %v", err)
	}

	move := client.actions[0].(version.ActionMoveLook)
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
	next, err := NewSender(client, DefaultBounds()).Step(t.Context(), from, simgeom.Vec3{X: 100}, true)
	if !errors.Is(err, sentinel) {
		t.Errorf("step returned %v, want it to carry %v", err, sentinel)
	}

	// A step that never left the process must not advance the caller's idea of
	// where it is, or prediction and the wire disagree from then on.
	if next != from {
		t.Errorf("a failed send moved the bot to %+v, want it to stay at %+v", next, from)
	}
}

func TestTheJumpFlagIsNotActedOn(t *testing.T) {
	t.Parallel()

	// The core sets it on every step rather than choosing per step, and
	// honouring it would mean picking a height with no physics behind it — a
	// claim to be in the air that a server reads as flying. Both values must
	// therefore produce the same update.
	from, target := simgeom.Vec3{X: 0, Y: 4, Z: 0}, simgeom.Vec3{X: 10, Y: 4, Z: 0}

	jumped, walked := &captured{}, &captured{}
	if _, err := NewSender(jumped, DefaultBounds()).Step(t.Context(), from, target, true); err != nil {
		t.Fatal(err)
	}
	if _, err := NewSender(walked, DefaultBounds()).Step(t.Context(), from, target, false); err != nil {
		t.Fatal(err)
	}

	if jumped.actions[0] != walked.actions[0] {
		t.Errorf("jumping changed the update: %+v against %+v", jumped.actions[0], walked.actions[0])
	}
}

func TestRespawnIsSentAsAnIntent(t *testing.T) {
	t.Parallel()

	// A dead bot can send nothing else, so this is the one action that has to
	// work while dead. Found the hard way: this example was killed by a slime
	// on a live server and had no way to answer.
	client := &captured{}

	if err := NewSender(client, DefaultBounds()).Respawn(t.Context()); err != nil {
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
	client := &captured{}
	sender := NewSender(client, DefaultBounds())
	target := simgeom.Vec3{X: 100, Y: 4, Z: 0}

	at := simgeom.Vec3{X: 0, Y: 4, Z: 0}
	for range 10 {
		next, err := sender.Step(t.Context(), at, target, true)
		if err != nil {
			t.Fatal(err)
		}
		at = next
	}

	want := 10 * DefaultBounds().WalkSpeed * DefaultBounds().Tick.Seconds()
	if math.Abs(at.X-want) > 1e-9 {
		t.Errorf("ten steps reached x=%v, want %v", at.X, want)
	}

	// And each one has to be a distinct position on the wire, not the same
	// coordinate repeated.
	seen := map[float64]bool{}
	for _, a := range client.actions {
		seen[a.(version.ActionMoveLook).X] = true
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

// TestLocomotionSpeaksOnlyWhenTheStateChanges pins the edge trigger.
//
// A real client reports the keys it is holding when they change. Repeating the
// same state twenty times a second would describe a held key as though it were
// being pressed again, and it would put nineteen packets a second on a
// connection to say nothing.
func TestLocomotionSpeaksOnlyWhenTheStateChanges(t *testing.T) {
	t.Parallel()

	client := &spy{}
	sender := NewSender(client, DefaultBounds())

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
	sender := NewSender(client, DefaultBounds())

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
	sender := NewSender(client, DefaultBounds())

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
	sender := NewSender(client, DefaultBounds())

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
			if _, err := NewSender(client, bounds).Step(t.Context(), test.from, test.target, true); err != nil {
				t.Fatalf("Step: %v", err)
			}

			move, ok := client.actions[0].(version.ActionMoveLook)
			if !ok {
				t.Fatalf("sent %T, want version.ActionMoveLook", client.actions[0])
			}
			if math.Abs(float64(move.Pitch-test.want)) > 1e-3 {
				t.Errorf("Pitch = %v, want %v", move.Pitch, test.want)
			}
		})
	}
}
