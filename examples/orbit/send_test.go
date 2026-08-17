package main

import (
	"context"
	"errors"
	"math"
	"testing"

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
		Vec3{X: 0, Y: 4, Z: 0},
		Vec3{X: 100, Y: 4, Z: 0},
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
		Vec3{X: 0, Y: 4, Z: 0},
		Vec3{X: -100, Y: 4, Z: 0},
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

	from := Vec3{X: 7, Y: 4, Z: 9}
	next, err := NewSender(client, DefaultBounds()).Step(t.Context(), from, Vec3{X: 100}, true)
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
	from, target := Vec3{X: 0, Y: 4, Z: 0}, Vec3{X: 10, Y: 4, Z: 0}

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
	target := Vec3{X: 100, Y: 4, Z: 0}

	at := Vec3{X: 0, Y: 4, Z: 0}
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
