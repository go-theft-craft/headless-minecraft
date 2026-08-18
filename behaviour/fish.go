package behaviour

import (
	"context"
	"errors"

	"github.com/go-theft-craft/headless-minecraft/safety"
	"github.com/go-theft-craft/headless-minecraft/version"
	"github.com/go-theft-craft/headless-minecraft/world"
)

// ErrNoBiteDetector reports a fishing behaviour with no way to tell a bite.
var ErrNoBiteDetector = errors.New("behaviour: fishing needs a bite detector")

// Bite reports whether a fish is on the line.
//
// It is an interface, and it ships with no implementation, and that is the whole
// point of it.
//
// # Why there is no default
//
// Casting is a use of the rod and reeling is another, and between them the bot
// has to know that a fish bit. **No packet in either protocol says so.** What a
// client actually observes is the bobber entity's motion changing as it dips,
// and a splash sound at its position. Which of those is reliable, how much
// motion counts as a dip, and whether 26.1.2 signals it differently from 1.8.9
// are measurements rather than readings.
//
// This repository's own history is why that distinction is being held to here:
// careful readings of vanilla behaviour were wrong often enough that M8.4
// replaced them with fixtures the game generates. A threshold written into this
// package from a wiki page or from watching a bobber would be exactly the kind
// of number that turns out to be wrong in a way nothing catches.
//
// So the detector is supplied by whoever has measured one, against a trace
// captured per version through the M9.1 capture lane. Until that lane has run
// live, Fish is not claimed to work, and no test here asserts a threshold
// nobody measured.
type Bite interface {
	// Bit reports whether the bobber has dipped, given the world and the
	// bobber's entity identifier.
	Bit(observed world.Snapshot, bobber int32) bool
}

// Fish casts a rod, waits for a bite, reels in, and casts again.
//
// The behaviour itself is the easiest in this package to write and it is last
// in the sequencing anyway, because the one thing it depends on is the one
// thing nobody has measured. See [Bite].
type Fish struct {
	slot     uint8
	hand     version.Hand
	detector Bite
	// casts is how many times to fish before finishing. Zero means until
	// something stops it.
	casts int

	bobber   int32
	out      bool
	reeled   int
	waited   int
	patience int
}

// NewFish returns a fishing behaviour, refusing one with no way to tell a bite.
func NewFish(
	authorization safety.Authorization,
	endpoint string,
	slot uint8,
	detector Bite,
	casts int,
	patience int,
) (*Fish, error) {
	if err := RequireScopes(
		authorization, endpoint,
		safety.ScopeObserve, safety.ScopeInventory, safety.ScopeInteract,
	); err != nil {
		return nil, err
	}
	if detector == nil {
		// Refusing is the honest failure. A default detector would be a
		// threshold this package invented, and a bot fishing against an
		// invented threshold looks like it is working.
		return nil, ErrNoBiteDetector
	}
	if slot > 8 {
		return nil, errors.New("behaviour: a hotbar has nine slots")
	}

	return &Fish{slot: slot, hand: version.MainHand, detector: detector, casts: casts, patience: patience}, nil
}

// Bobber tells the behaviour which entity the server spawned for its float.
//
// It is set by the caller rather than found here, because finding it means
// knowing which entity type a bobber is in each version, and that is the
// application's vocabulary rather than this package's.
func (f *Fish) Bobber(id int32) { f.bobber = id }

// Tick implements Behaviour.
func (f *Fish) Tick(_ context.Context, observed world.Snapshot) (Outcome, error) {
	if !observed.Player.Known {
		return running(), nil
	}
	if observed.Player.Dead {
		return stopped(ReasonFailed), nil
	}

	if !f.out {
		if f.casts > 0 && f.reeled >= f.casts {
			return complete(), nil
		}
		f.out, f.waited = true, 0

		return running(
			version.ActionHeldSlot{Slot: f.slot},
			version.ActionUseItem{Hand: f.hand},
		), nil
	}

	if f.bobber != 0 {
		if _, tracked := observed.Entities.Get(f.bobber); !tracked {
			// The float is gone without a reel: the rod broke, or the server
			// took it. Casting again into that is how a bot spends an hour
			// doing nothing.
			return stopped(ReasonOutOfResources), nil
		}
		if f.detector.Bit(observed, f.bobber) {
			f.out, f.reeled = false, f.reeled+1

			return running(version.ActionUseItem{Hand: f.hand}), nil
		}
	}

	f.waited++
	if f.patience > 0 && f.waited >= f.patience {
		// Nothing bit for as long as the caller was willing to wait. Reeling in
		// and stopping beats holding a line out forever.
		f.out = false

		return Outcome{
			Actions: []version.Action{version.ActionUseItem{Hand: f.hand}},
			Status:  Stopped,
			Reason:  ReasonFailed,
		}, nil
	}

	// The wait, which is most of fishing.
	return running(), nil
}
