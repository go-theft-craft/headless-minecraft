package behaviour

import (
	"context"
	"errors"

	"github.com/go-theft-craft/headless-minecraft/safety"
	"github.com/go-theft-craft/headless-minecraft/version"
	"github.com/go-theft-craft/headless-minecraft/world"
)

// ErrNoStages reports a composed behaviour built with nothing to run.
var ErrNoStages = errors.New("behaviour: a sequence needs at least one stage")

// Sequence runs behaviours one after another and stops at the first that does
// not complete.
//
// This is what composition looks like here, and it is the whole of it. There is
// no scheduler because there is nothing to schedule: a composed behaviour holds
// its parts and forwards its tick to whichever is active. StripMine is a
// sequence of a walk, a dig, and a support placement; a caller wanting
// something else composes its own.
//
// It stops rather than skipping when a stage stops. A sequence that carried on
// past a stage that gave up would be a bot digging a corridor it never walked
// to.
type Sequence struct {
	stages []Behaviour
	at     int
}

// NewSequence returns a sequence over the stages, in order.
//
// It declares no scopes of its own. Each stage checked its own at construction,
// and a sequence that re-declared the union would be a second place to keep
// that list correct.
func NewSequence(stages ...Behaviour) (*Sequence, error) {
	if len(stages) == 0 {
		return nil, ErrNoStages
	}
	for _, stage := range stages {
		if stage == nil {
			return nil, errors.New("behaviour: a sequence stage is nil")
		}
	}

	return &Sequence{stages: stages}, nil
}

// Tick implements Behaviour.
func (s *Sequence) Tick(ctx context.Context, observed world.Snapshot) (Outcome, error) {
	if s.at >= len(s.stages) {
		return complete(), nil
	}

	outcome, err := s.stages[s.at].Tick(ctx, observed)
	if err != nil {
		return Outcome{}, err
	}

	switch outcome.Status {
	case Running:
		return outcome, nil
	case Complete:
		s.at++
		if s.at >= len(s.stages) {
			return outcome, nil
		}

		// The finished stage's last actions still go out, and the next stage
		// starts on the next tick rather than in this one. Running two stages
		// in one tick would let a behaviour drive, which is the one thing none
		// of them may do.
		return running(outcome.Actions...), nil
	case Stopped:
		return outcome, nil
	}

	return outcome, nil
}

// Dig breaks one block and waits for the world to say it broke.
//
// The break time is not this package's to know: it depends on the block, the
// tool, and the version, and minecraft-simulation measures it. This holds the
// dig open and watches the world, with a tick bound as the backstop — which is
// the same division every other wait here follows.
type Dig struct {
	at    version.BlockPos
	face  version.Face
	slot  uint8
	ticks int

	started  bool
	waited   int
	revision uint64
}

// NewDig returns a dig, refusing one it is not authorized for.
func NewDig(
	authorization safety.Authorization,
	endpoint string,
	at version.BlockPos,
	face version.Face,
	slot uint8,
	ticks int,
) (*Dig, error) {
	if err := RequireScopes(
		authorization, endpoint,
		safety.ScopeObserve, safety.ScopeDig, safety.ScopeInventory,
	); err != nil {
		return nil, err
	}
	if ticks <= 0 {
		return nil, errors.New("behaviour: digging for no ticks is not digging")
	}

	return &Dig{at: at, face: face, slot: slot, ticks: ticks}, nil
}

// Tick implements Behaviour.
func (d *Dig) Tick(_ context.Context, observed world.Snapshot) (Outcome, error) {
	if !observed.Player.Known {
		return running(), nil
	}

	if !d.started {
		d.started, d.revision, d.waited = true, observed.Revision, 0

		// Select the tool, then start. Both protocols model a dig as start,
		// hold, finish, and the client is what decides when the middle is over.
		return running(
			version.ActionHeldSlot{Slot: d.slot},
			version.ActionDig{Block: d.at, Face: d.face, Stage: version.DigStart},
		), nil
	}

	d.waited++
	if d.waited < d.ticks {
		// The wait. Nothing at all goes out while a block is breaking.
		return running(), nil
	}

	return complete(version.ActionDig{Block: d.at, Face: d.face, Stage: version.DigFinish}), nil
}
