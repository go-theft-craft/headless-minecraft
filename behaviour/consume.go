package behaviour

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-theft-craft/headless-minecraft/safety"
	"github.com/go-theft-craft/headless-minecraft/version"
	"github.com/go-theft-craft/headless-minecraft/world"
)

// ErrNoOffHand reports a shield behaviour on a protocol with no offhand.
var ErrNoOffHand = errors.New("behaviour: this protocol has no offhand")

// Eat holds a food item until the body is fed.
//
// It is the smallest behaviour with a real wait in it: eating takes a fixed
// number of ticks in both versions, the server says nothing while it happens,
// and the client's part is to hold the use down and let go. Every one of those
// waiting ticks returns Running with no action, which is what "a wait is a tick
// that returns no action" means in practice.
//
// It watches the food level rather than counting ticks alone, so a bite the
// server refused does not read as a bite that worked. The tick count is the
// bound on waiting, not the definition of success.
type Eat struct {
	slot  uint8
	hand  version.Hand
	ticks int
	// full is the food level at or above which the body stops eating.
	full int32

	held    bool
	waited  int
	started int32
}

// NewEat returns an eat, refusing one it is not authorized for.
//
// It needs inventory to choose the slot and interact to use what is in it. The
// two are separate scopes because they are separate powers: a caller that may
// read and rearrange an inventory is not thereby a caller that may use things
// out of it on the world.
func NewEat(
	authorization safety.Authorization,
	endpoint string,
	slot uint8,
	full int32,
	ticks int,
) (*Eat, error) {
	if err := RequireScopes(
		authorization, endpoint,
		safety.ScopeObserve, safety.ScopeInventory, safety.ScopeInteract,
	); err != nil {
		return nil, err
	}
	if slot > 8 {
		return nil, fmt.Errorf("behaviour: hotbar slot %d, and a hotbar has nine", slot)
	}
	if ticks <= 0 {
		return nil, fmt.Errorf("behaviour: eating for %d ticks is not eating", ticks)
	}

	return &Eat{slot: slot, hand: version.MainHand, ticks: ticks, full: full}, nil
}

// Tick implements Behaviour.
func (e *Eat) Tick(_ context.Context, observed world.Snapshot) (Outcome, error) {
	if !observed.Player.Known {
		return running(), nil
	}
	if observed.Player.Dead {
		// Nothing to be done about hunger from here, and the respawn is the
		// application's decision rather than this behaviour's.
		return stopped(ReasonFailed), nil
	}

	if !e.held {
		if observed.Player.Food >= e.full {
			// Already fed. Completing without sending anything is the right
			// answer: the task was "be fed", not "eat something".
			return complete(), nil
		}
		e.held, e.started, e.waited = true, observed.Player.Food, 0

		// Select, then use. Both, and in that order, because using without
		// selecting eats whatever happened to be in the hand.
		return running(
			version.ActionHeldSlot{Slot: e.slot},
			version.ActionUseItem{Hand: e.hand},
		), nil
	}

	// Fed while holding: let go and finish. The release is not optional —
	// without it the item stays in use and the next behaviour inherits a hand
	// that is busy.
	if observed.Player.Food > e.started || observed.Player.Food >= e.full {
		e.held = false

		return complete(version.ActionReleaseUse{Hand: e.hand}), nil
	}

	e.waited++
	if e.waited < e.ticks {
		// The wait. No action at all, for as many ticks as eating takes.
		return running(), nil
	}

	// Held for the whole duration and the food level never moved. Either there
	// was nothing edible in the slot or the server refused it, and holding
	// longer will not find out which.
	e.held = false

	return Outcome{
		Actions: []version.Action{version.ActionReleaseUse{Hand: e.hand}},
		Status:  Stopped,
		Reason:  ReasonOutOfResources,
	}, nil
}

// Block raises a shield and holds it up.
//
// It is 26.1.2 only, and refuses to be built on protocol 47 rather than
// pretending: 47 has no offhand and therefore no shield, and a behaviour that
// sent an offhand use there would be describing an arm the player does not
// have. That refusal is at construction, which is where a caller can still do
// something about it.
//
// It never completes on its own. Raising a shield is a state a caller holds and
// then drops, so this runs until Lower is called, and the caller decides when
// that is — this package does not choose when a bot should stop defending
// itself any more than it chooses whom to fight.
type Block struct {
	hand version.Hand

	raised  bool
	lowered bool
}

// NewBlock returns a shield behaviour, refusing one the protocol cannot carry.
//
// The protocol is probed rather than named. Asking the adapter to encode a hand
// swap is the honest test for "does this protocol have an offhand": that action
// is about the offhand rather than merely mentioning one, so the adapters refuse
// it exactly where the arm does not exist. Matching on a protocol identifier
// would be a second place to remember which versions have shields.
func NewBlock(
	authorization safety.Authorization,
	endpoint string,
	adapter version.Adapter,
) (*Block, error) {
	if err := RequireScopes(authorization, endpoint, safety.ScopeObserve, safety.ScopeInteract); err != nil {
		return nil, err
	}
	if adapter == nil {
		return nil, errors.New("behaviour: a shield needs an adapter to ask about the offhand")
	}
	if _, err := adapter.EncodeAction(version.ActionSwapHands{}); err != nil {
		if errors.Is(err, version.ErrUnsupportedAction) {
			return nil, fmt.Errorf("%w: %s has no shield", ErrNoOffHand, adapter.ProtocolID())
		}

		return nil, fmt.Errorf("behaviour: ask %s about the offhand: %w", adapter.ProtocolID(), err)
	}

	return &Block{hand: version.OffHand}, nil
}

// Lower asks the behaviour to drop the shield on its next tick.
func (b *Block) Lower() { b.lowered = true }

// Tick implements Behaviour.
func (b *Block) Tick(_ context.Context, observed world.Snapshot) (Outcome, error) {
	if !observed.Player.Known {
		return running(), nil
	}

	if b.lowered {
		if !b.raised {
			return complete(), nil
		}
		b.raised = false

		return complete(version.ActionReleaseUse{Hand: b.hand}), nil
	}

	if observed.Player.Dead {
		// A dead body is not blocking. Reporting it rather than holding the
		// use down through a death is what stops the shield staying raised
		// through a respawn.
		b.raised = false

		return stopped(ReasonFailed), nil
	}

	if !b.raised {
		b.raised = true

		return running(version.ActionUseItem{Hand: b.hand}), nil
	}

	// Raised and holding. Every tick from here is a wait: the use is a held
	// state, and sending it again each tick would be pressing a key that is
	// already down.
	return running(), nil
}
