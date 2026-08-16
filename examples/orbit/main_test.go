package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/go-theft-craft/headless-minecraft/event"
)

func quiet() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestApplyDispatchesEveryActionKind(t *testing.T) {
	t.Parallel()

	actuator := &recording{}
	core := NewBot(DefaultBounds())
	target := Vec3{X: 1, Y: 64, Z: 2}

	for _, action := range []Action{
		{Kind: Stand},
		{Kind: StepTo, Target: target, Jump: true},
		{Kind: Strike, Entity: 42},
		{Kind: SendRespawn},
	} {
		code, done, err := apply(t.Context(), quiet(), actuator, core, action)
		if err != nil || done || code != 0 {
			t.Fatalf("%v produced code=%d done=%v err=%v, want a clean continue", action.Kind, code, done, err)
		}
	}

	if len(actuator.steps) != 1 || actuator.steps[0] != target {
		t.Errorf("steps are %v, want one to %+v", actuator.steps, target)
	}
	if len(actuator.attacks) != 1 || actuator.attacks[0] != 42 {
		t.Errorf("attacks are %v, want one at 42", actuator.attacks)
	}
	if actuator.respawn != 1 {
		t.Errorf("sent %d respawns, want 1", actuator.respawn)
	}
}

func TestApplyEndsTheLoopOnExit(t *testing.T) {
	t.Parallel()

	code, done, err := apply(t.Context(), quiet(), &recording{}, NewBot(DefaultBounds()), Action{
		Kind:   Exit,
		Reason: "sealed in",
		Code:   1,
	})

	if !done || code != 1 || err != nil {
		t.Errorf("got code=%d done=%v err=%v, want 1, true, nil", code, done, err)
	}
}

// failing is an actuator whose every action is still owed by a milestone, which
// is what Pending is today.
type failing struct{ err error }

func (f failing) Step(context.Context, Vec3, bool) error { return f.err }
func (f failing) Attack(context.Context, int32) error    { return f.err }
func (f failing) Respawn(context.Context) error          { return f.err }

func TestAPendingPortStopsTheRunWithoutCrashing(t *testing.T) {
	t.Parallel()

	// The expected outcome today. It has to be a clean, distinguishable exit
	// rather than a panic or a silent loop, because it is what every run of
	// this program does until M9 lands.
	code, done, err := apply(
		t.Context(),
		quiet(),
		failing{err: ErrNotYet},
		NewBot(DefaultBounds()),
		Action{Kind: StepTo},
	)

	if !done {
		t.Error("kept going after a port reported it does not exist yet")
	}
	if code != 3 {
		t.Errorf("exited %d, want the reserved 3 for an unimplemented port", code)
	}
	if err != nil {
		t.Errorf("returned %v, want nil: a missing milestone is expected, not a failure", err)
	}
}

func TestARealActuatorErrorIsNotSwallowed(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("connection reset")

	code, done, err := apply(
		t.Context(),
		quiet(),
		failing{err: sentinel},
		NewBot(DefaultBounds()),
		Action{Kind: Strike},
	)

	if !done || code != 1 {
		t.Errorf("got code=%d done=%v, want 1, true", code, done)
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("returned %v, want it to carry %v", err, sentinel)
	}
}

func TestPendingReportsEveryMilestoneItOwes(t *testing.T) {
	t.Parallel()

	var (
		world    World    = Pending{}
		actuator Actuator = Pending{}
	)

	if _, known := world.Spawn(); known {
		t.Error("Pending claimed to know the spawn position")
	}
	if _, known := world.Block(BlockPos{}); known {
		t.Error("Pending claimed to know a block")
	}
	if _, known := world.Entity(1); known {
		t.Error("Pending claimed to know an entity")
	}

	for name, err := range map[string]error{
		"step":    actuator.Step(t.Context(), Vec3{}, true),
		"attack":  actuator.Attack(t.Context(), 1),
		"respawn": actuator.Respawn(t.Context()),
	} {
		if !errors.Is(err, ErrNotYet) {
			t.Errorf("%s returned %v, want ErrNotYet", name, err)
		}
	}

	if len(Missing()) == 0 {
		t.Error("Missing lists nothing while every port is Pending")
	}
}

func TestFoldMarksReadyOnTheSessionEvent(t *testing.T) {
	t.Parallel()

	// Only Ready is real today. When M7 lands, this is where its events attach,
	// and this test is where the next fact gets one.
	var tick Tick
	if tick.Ready {
		t.Fatal("a zero tick is ready")
	}

	fold(&tick, event.Ready{})
	if !tick.Ready {
		t.Error("session.ready did not mark the tick ready")
	}
}
