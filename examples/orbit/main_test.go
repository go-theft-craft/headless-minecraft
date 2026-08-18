package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	simgeom "github.com/go-theft-craft/minecraft-simulation/geom"

	"github.com/go-theft-craft/headless-minecraft/event"
	"github.com/go-theft-craft/headless-minecraft/version"
)

func quiet() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestApplyDispatchesEveryActionKind(t *testing.T) {
	t.Parallel()

	actuator := &recording{}
	core := NewBot(DefaultBounds())
	target := simgeom.Vec3{X: 1, Y: 64, Z: 2}

	for _, action := range []Action{
		{Kind: Stand},
		{Kind: StepTo, Target: target, Jump: true},
		{Kind: Strike, Entity: 42},
		{Kind: SendRespawn},
	} {
		_, code, done, err := apply(t.Context(), quiet(), actuator, core, simgeom.Vec3{}, action, false)
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

	_, code, done, err := apply(t.Context(), quiet(), &recording{}, NewBot(DefaultBounds()), simgeom.Vec3{}, Action{
		Kind:   Exit,
		Reason: "sealed in",
		Code:   1,
	}, false)

	if !done || code != 1 || err != nil {
		t.Errorf("got code=%d done=%v err=%v, want 1, true, nil", code, done, err)
	}
}

// failing is an actuator whose every action fails, which is how the loop's
// error paths are reached without a connection.
type failing struct{ err error }

func (f failing) Step(_ context.Context, from, _ simgeom.Vec3, _ bool) (simgeom.Vec3, error) {
	return from, f.err
}
func (f failing) Attack(context.Context, int32) error { return f.err }
func (f failing) Respawn(context.Context) error       { return f.err }

// Locomotion succeeds even here. It is narration rather than an action, and
// these cases are about what happens when an action a milestone owes is asked
// for; failing the narration too would move the failure a line earlier and
// test something else.
func (failing) Locomotion(context.Context, bool) error { return nil }

// Mark succeeds for the same reason Locomotion does: it is not the action
// under test, and failing it would move the failure a line earlier.
func (failing) Mark(context.Context, simgeom.Vec3) error { return nil }

// Kill succeeds too, for the same reason.
func (failing) Kill(context.Context) error { return nil }

func TestAPendingPortStopsTheRunWithoutCrashing(t *testing.T) {
	t.Parallel()

	// The expected outcome today. It has to be a clean, distinguishable exit
	// rather than a panic or a silent loop, because it is what every run of
	// this program does until M9 lands.
	_, code, done, err := apply(
		t.Context(),
		quiet(),
		failing{err: ErrNotYet},
		NewBot(DefaultBounds()),
		simgeom.Vec3{},
		Action{Kind: StepTo},
		false,
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

	_, code, done, err := apply(
		t.Context(),
		quiet(),
		failing{err: sentinel},
		NewBot(DefaultBounds()),
		simgeom.Vec3{},
		Action{Kind: Strike},
		false,
	)

	if !done || code != 1 {
		t.Errorf("got code=%d done=%v, want 1, true", code, done)
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("returned %v, want it to carry %v", err, sentinel)
	}
}

// TestTheExampleStillNamesWhatItCannotDo pins the startup warning.
//
// Attack used to be in this list and is not any more: the library gained an
// interaction vocabulary and the bot hits back. What is left is the body --
// gravity, collision and a jump -- which is why the bot walks a flat world and
// nothing else, and a run that stopped saying so would be a run that looked
// more capable than it is.
func TestTheExampleStillNamesWhatItCannotDo(t *testing.T) {
	t.Parallel()

	owed := Missing()
	if len(owed) == 0 {
		t.Fatal("the example claims to owe nothing while it still has no body")
	}
	for _, line := range owed {
		if line == "" {
			t.Error("an empty line in the list of what is owed")
		}
	}
}

func TestFoldMarksReadyOnTheSessionEvent(t *testing.T) {
	t.Parallel()

	var tick Tick
	if tick.Ready {
		t.Fatal("a zero tick is ready")
	}

	fold(&tick, event.Ready{})
	if !tick.Ready {
		t.Error("session.ready did not mark the tick ready")
	}
}

func TestFoldTakesAnAttackerOnlyFromAnAttributedDamage(t *testing.T) {
	t.Parallel()

	// Protocol 47 sends no damage packet and names nobody, so the alternative
	// to an honest zero is a guess — nearest entity, last swing — presented as
	// an observation. The bot keeps orbiting instead.
	var unattributed Tick
	fold(&unattributed, event.PlayerDamaged{Damage: event.Damage{CauseID: 7}})

	if unattributed.Attacker != 0 {
		t.Errorf("took attacker %d from damage that named nobody", unattributed.Attacker)
	}

	var attributed Tick
	fold(&attributed, event.PlayerDamaged{
		Damage: event.Damage{CauseID: 7, Attributed: true},
	})

	if attributed.Attacker != 7 {
		t.Errorf("attacker is %d, want the attributed 7", attributed.Attacker)
	}
}

func TestFoldCarriesTheEdgeTriggeredFacts(t *testing.T) {
	t.Parallel()

	// These four are the whole reason the loop reads a subscription at all:
	// everything else it needs is in the snapshot, and what a snapshot cannot
	// say is what happened between two of them.
	for name, check := range map[string]struct {
		e    event.Event
		read func(Tick) bool
	}{
		"death":     {event.PlayerDied{}, func(t Tick) bool { return t.Died }},
		"respawn":   {event.PlayerRespawned{}, func(t Tick) bool { return t.Respawned }},
		"placement": {event.PlayerMoved{}, func(t Tick) bool { return t.Corrected }},
	} {
		var tick Tick
		fold(&tick, check.e)

		if !check.read(tick) {
			t.Errorf("%s did not reach the tick", name)
		}
	}
}

// mute is an actuator whose locomotion fails and whose movement does not.
type mute struct{ err error }

func (mute) Step(_ context.Context, from, _ simgeom.Vec3, _ bool) (simgeom.Vec3, error) {
	return from, nil
}
func (mute) Attack(context.Context, int32) error      { return nil }
func (mute) Respawn(context.Context) error            { return nil }
func (m mute) Locomotion(context.Context, bool) error { return m.err }
func (mute) Mark(context.Context, simgeom.Vec3) error { return nil }
func (mute) Kill(context.Context) error               { return nil }

// TestAProtocolThatCannotNarrateStillWalks pins that the decoration is
// optional and the movement is not.
//
// Protocol 47 has no input packet. A bot that treated its absence as a failure
// would trade the thing that works for the thing that describes it, and the
// legacy lane of this example would stop walking for the sake of a packet it
// was never going to send.
func TestAProtocolThatCannotNarrateStillWalks(t *testing.T) {
	t.Parallel()

	_, code, done, err := apply(
		t.Context(), quiet(),
		mute{err: version.UnsupportedAction("java/1.8.9", version.ActionInput{})},
		NewBot(DefaultBounds()), simgeom.Vec3{}, Action{Kind: StepTo}, false,
	)
	if err != nil || done || code != 0 {
		t.Fatalf("apply returned (%d, %v, %v), want the run to carry on", code, done, err)
	}
}

// TestLocomotionFailingForAnyOtherReasonStopsTheRun pins that only the
// protocol's refusal is tolerated. A broken connection is not a decoration.
func TestLocomotionFailingForAnyOtherReasonStopsTheRun(t *testing.T) {
	t.Parallel()

	_, code, done, err := apply(
		t.Context(), quiet(),
		mute{err: errors.New("connection reset")},
		NewBot(DefaultBounds()), simgeom.Vec3{}, Action{Kind: StepTo}, false,
	)
	if err == nil || !done || code != 70 {
		t.Fatalf("apply returned (%d, %v, %v), want a stopped run", code, done, err)
	}
}
