package behaviour

import (
	"context"
	"errors"
	"strings"
	"testing"

	protocol "github.com/go-theft-craft/minecraft-protocol"

	"github.com/go-theft-craft/headless-minecraft/safety"
	"github.com/go-theft-craft/headless-minecraft/version"
	"github.com/go-theft-craft/headless-minecraft/world"
)

const endpoint = "localhost:25565"

// every scope any behaviour here needs, for the tests that are about something
// other than authorization.
func fullAuth(t *testing.T) safety.Authorization {
	t.Helper()

	authorization, err := safety.Authorize(endpoint,
		safety.ScopeObserve, safety.ScopeMove, safety.ScopeInventory,
		safety.ScopeInteract, safety.ScopeAttack, safety.ScopeDig, safety.ScopeBuild,
	)
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}

	return authorization
}

// placed returns a snapshot of a live player standing at the origin.
func placed() world.Snapshot {
	return world.Snapshot{
		Player: world.PlayerView{
			Known: true, Placed: true, Health: 20, Food: 20,
		},
		Entities: world.EntitiesView{Tracked: map[int32]world.EntityView{}},
	}
}

// withEntity returns the snapshot with one entity tracked at a position.
func withEntity(observed world.Snapshot, id int32, x, y, z float64) world.Snapshot {
	tracked := make(map[int32]world.EntityView, len(observed.Entities.Tracked)+1)
	for key, value := range observed.Entities.Tracked {
		tracked[key] = value
	}
	tracked[id] = world.EntityView{EntityID: id, X: x, Y: y, Z: z}
	observed.Entities = world.EntitiesView{Tracked: tracked}

	return observed
}

// offhandAdapter answers the offhand probe the shield behaviour makes.
type offhandAdapter struct{ supported bool }

func (offhandAdapter) ProtocolID() string { return "test" }

func (offhandAdapter) LoginTerminalState() protocol.State { return "" }

func (offhandAdapter) Handshake(string, uint16) protocol.Packet { return protocol.Packet{} }

func (a offhandAdapter) EncodeAction(version.Action) (protocol.Packet, error) {
	if a.supported {
		return protocol.Packet{}, nil
	}

	return protocol.Packet{}, version.ErrUnsupportedAction
}

func (offhandAdapter) Handlers() map[string]version.Handler { return nil }

// TestRequireScopesNamesEveryMissingScope pins that a caller fixing an
// authorization is told the whole list.
//
// One scope at a time, one construction at a time, is the same work spread over
// as many edits as there are scopes.
func TestRequireScopesNamesEveryMissingScope(t *testing.T) {
	t.Parallel()

	authorization, err := safety.Authorize(endpoint, safety.ScopeObserve)
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}

	err = RequireScopes(authorization, endpoint,
		safety.ScopeObserve, safety.ScopeDig, safety.ScopeBuild)
	if err == nil {
		t.Fatal("RequireScopes accepted a behaviour with two missing scopes")
	}
	if !errors.Is(err, safety.ErrUnauthorized) {
		t.Errorf("error %v does not wrap safety.ErrUnauthorized", err)
	}
	for _, want := range []string{"dig", "build"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name the missing scope %q", err, want)
		}
	}
}

func TestRequireScopesAcceptsWhatIsAuthorized(t *testing.T) {
	t.Parallel()

	authorization, err := safety.Authorize(endpoint, safety.ScopeObserve, safety.ScopeMove)
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}

	if err := RequireScopes(authorization, endpoint, safety.ScopeMove); err != nil {
		t.Errorf("RequireScopes: %v", err)
	}
}

// TestRequireScopesRefusesAnotherEndpoint pins that an authorization is scoped
// to where it was granted, not just to what it permits.
func TestRequireScopesRefusesAnotherEndpoint(t *testing.T) {
	t.Parallel()

	authorization, err := safety.Authorize(endpoint, safety.ScopeObserve, safety.ScopeMove)
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}

	if err := RequireScopes(authorization, "example.com:25565", safety.ScopeMove); err == nil {
		t.Fatal("an authorization for one endpoint covered another")
	}
}

// TestEveryBehaviourRefusesWithoutItsScopes is the construction-time gate,
// asserted once per behaviour.
//
// Each case grants everything except the one scope the behaviour needs, so a
// behaviour that stopped checking a scope fails here rather than at tick four
// hundred with the bot somewhere it should not be.
func TestEveryBehaviourRefusesWithoutItsScopes(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name    string
		granted []safety.Scope
		build   func(safety.Authorization) error
	}{
		{
			name:    "follow without move",
			granted: []safety.Scope{safety.ScopeObserve},
			build: func(a safety.Authorization) error {
				_, err := NewFollow(a, endpoint, 1, Motion{Step: 0.2})

				return err
			},
		},
		{
			name:    "flee without move",
			granted: []safety.Scope{safety.ScopeObserve},
			build: func(a safety.Authorization) error {
				_, err := NewFlee(a, endpoint, 1, 10, Motion{Step: 0.2})

				return err
			},
		},
		{
			name:    "eat without inventory",
			granted: []safety.Scope{safety.ScopeObserve, safety.ScopeInteract},
			build: func(a safety.Authorization) error {
				_, err := NewEat(a, endpoint, 0, 20, 32)

				return err
			},
		},
		{
			name:    "block without interact",
			granted: []safety.Scope{safety.ScopeObserve},
			build: func(a safety.Authorization) error {
				_, err := NewBlock(a, endpoint, offhandAdapter{supported: true})

				return err
			},
		},
		{
			name:    "dig without dig",
			granted: []safety.Scope{safety.ScopeObserve, safety.ScopeInventory},
			build: func(a safety.Authorization) error {
				_, err := NewDig(a, endpoint, version.BlockPos{}, version.FaceTop, 0, 10)

				return err
			},
		},
		{
			name:    "fish without inventory",
			granted: []safety.Scope{safety.ScopeObserve, safety.ScopeInteract},
			build: func(a safety.Authorization) error {
				_, err := NewFish(a, endpoint, 0, neverBites{}, 1, 100)

				return err
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			authorization, err := safety.Authorize(endpoint, test.granted...)
			if err != nil {
				t.Fatalf("Authorize: %v", err)
			}
			if err := test.build(authorization); err == nil {
				t.Fatal("the behaviour constructed without its scopes")
			} else if !errors.Is(err, safety.ErrUnauthorized) {
				t.Fatalf("error %v does not wrap safety.ErrUnauthorized", err)
			}
		})
	}
}

// neverBites is a detector that never reports a bite, which is what makes the
// fishing tests here about waiting rather than about a threshold.
type neverBites struct{}

func (neverBites) Bit(world.Snapshot, int32) bool { return false }

// TestAWaitingBehaviourReturnsRunningWithNoActions is the property that makes
// "asked once per tick" work.
//
// A behaviour that emitted an action every tick while waiting would flood the
// connection, and one that slept would take the tick rate away from the caller.
// This drives each waiting behaviour past its first action and asserts that
// every tick after it is silent.
func TestAWaitingBehaviourReturnsRunningWithNoActions(t *testing.T) {
	t.Parallel()

	authorization := fullAuth(t)

	for _, test := range []struct {
		name  string
		build func() Behaviour
	}{
		{
			name: "eating holds and says nothing",
			build: func() Behaviour {
				eat, err := NewEat(authorization, endpoint, 0, 20, 32)
				if err != nil {
					t.Fatalf("NewEat: %v", err)
				}

				return eat
			},
		},
		{
			name: "a raised shield says nothing",
			build: func() Behaviour {
				block, err := NewBlock(authorization, endpoint, offhandAdapter{supported: true})
				if err != nil {
					t.Fatalf("NewBlock: %v", err)
				}

				return block
			},
		},
		{
			name: "a breaking block says nothing",
			build: func() Behaviour {
				dig, err := NewDig(authorization, endpoint, version.BlockPos{}, version.FaceTop, 0, 20)
				if err != nil {
					t.Fatalf("NewDig: %v", err)
				}

				return dig
			},
		},
		{
			name: "a cast line says nothing",
			build: func() Behaviour {
				fish, err := NewFish(authorization, endpoint, 0, neverBites{}, 1, 100)
				if err != nil {
					t.Fatalf("NewFish: %v", err)
				}

				return fish
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			behaviour := test.build()
			observed := placed()
			// Hungry, so eating has something to do.
			observed.Player.Food = 4

			// The first tick is allowed to act: it is what starts the thing
			// being waited for.
			if _, err := behaviour.Tick(t.Context(), observed); err != nil {
				t.Fatalf("Tick: %v", err)
			}

			for tick := range 10 {
				outcome, err := behaviour.Tick(t.Context(), observed)
				if err != nil {
					t.Fatalf("Tick: %v", err)
				}
				if outcome.Status != Running {
					break
				}
				if len(outcome.Actions) != 0 {
					t.Fatalf("waiting tick %d emitted %d actions", tick, len(outcome.Actions))
				}
			}
		})
	}
}

// TestEveryBehaviourTerminates is the gate that catches a behaviour which runs
// forever against a world that never helps it.
//
// Ten thousand ticks of an unhelpful world is far longer than any of these
// should need. A behaviour still Running at the end is one a caller would drive
// until the process stopped.
func TestEveryBehaviourTerminates(t *testing.T) {
	t.Parallel()

	authorization := fullAuth(t)

	for _, test := range []struct {
		name     string
		build    func() Behaviour
		observed world.Snapshot
	}{
		{
			name: "following an entity that never gets closer",
			build: func() Behaviour {
				follow, err := NewFollow(authorization, endpoint, 7,
					Motion{Step: 0, Arrive: 1, Patience: 20})
				if err != nil {
					t.Fatalf("NewFollow: %v", err)
				}

				return follow
			},
			observed: withEntity(placed(), 7, 50, 0, 0),
		},
		{
			name: "fleeing a threat that cannot be escaped",
			build: func() Behaviour {
				flee, err := NewFlee(authorization, endpoint, 7, 20,
					Motion{Step: 0, Patience: 20})
				if err != nil {
					t.Fatalf("NewFlee: %v", err)
				}

				return flee
			},
			observed: withEntity(placed(), 7, 1, 0, 0),
		},
		{
			name: "eating something that never feeds the body",
			build: func() Behaviour {
				eat, err := NewEat(authorization, endpoint, 0, 20, 32)
				if err != nil {
					t.Fatalf("NewEat: %v", err)
				}

				return eat
			},
			observed: hungry(),
		},
		{
			name: "digging a block that never breaks",
			build: func() Behaviour {
				dig, err := NewDig(authorization, endpoint, version.BlockPos{}, version.FaceTop, 0, 20)
				if err != nil {
					t.Fatalf("NewDig: %v", err)
				}

				return dig
			},
			observed: placed(),
		},
		{
			name: "fishing where nothing ever bites",
			build: func() Behaviour {
				fish, err := NewFish(authorization, endpoint, 0, neverBites{}, 1, 100)
				if err != nil {
					t.Fatalf("NewFish: %v", err)
				}

				return fish
			},
			observed: placed(),
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			behaviour := test.build()
			for tick := range 10_000 {
				outcome, err := behaviour.Tick(t.Context(), test.observed)
				if err != nil {
					t.Fatalf("tick %d: %v", tick, err)
				}
				if outcome.Status != Running {
					return
				}
			}

			t.Fatal("still running after ten thousand ticks against a world that never helps")
		})
	}
}

// hungry returns a snapshot of a player whose food level never moves.
func hungry() world.Snapshot {
	observed := placed()
	observed.Player.Food = 4

	return observed
}

// TestABlockRefusesOnAProtocolWithNoOffHand is the version gate.
//
// 47 has no offhand and therefore no shield, and refusing at construction is
// where a caller can still do something about it. A behaviour that sent an
// offhand use there would be describing an arm the player does not have.
func TestABlockRefusesOnAProtocolWithNoOffHand(t *testing.T) {
	t.Parallel()

	authorization := fullAuth(t)

	if _, err := NewBlock(authorization, endpoint, offhandAdapter{supported: false}); err == nil {
		t.Fatal("a shield behaviour built on a protocol with no offhand")
	} else if !errors.Is(err, ErrNoOffHand) {
		t.Fatalf("error %v does not wrap ErrNoOffHand", err)
	}

	if _, err := NewBlock(authorization, endpoint, offhandAdapter{supported: true}); err != nil {
		t.Fatalf("a shield behaviour refused on a protocol that has an offhand: %v", err)
	}
}

// TestFishRefusesWithoutAMeasuredBiteDetector is the discipline this package
// holds hardest.
//
// No packet in either protocol says a fish bit. A default detector would be a
// threshold this package invented, and a bot fishing against an invented
// threshold looks like it is working. Until a trace has been captured per
// version through the M9.1 lane, the caller supplies the detector or gets
// nothing.
func TestFishRefusesWithoutAMeasuredBiteDetector(t *testing.T) {
	t.Parallel()

	if _, err := NewFish(fullAuth(t), endpoint, 0, nil, 1, 100); err == nil {
		t.Fatal("a fishing behaviour built with no way to tell a bite")
	} else if !errors.Is(err, ErrNoBiteDetector) {
		t.Fatalf("error %v does not wrap ErrNoBiteDetector", err)
	}
}

// TestFishIsNotGatedOnAMeasuredTrace records why there is no test asserting
// that Fish catches anything.
//
// The behaviour is written and its waiting, its termination, and its refusals
// are all asserted above. What is not asserted is that a real bite is detected,
// because that needs a captured trace per version and no recorded session has a
// rod in it. This test exists so the absence is recorded rather than looking
// like an oversight.
func TestFishIsNotGatedOnAMeasuredTrace(t *testing.T) {
	t.Skip("no fishing trace has been captured on either version; " +
		"the capture lanes both ran on 2026-08-17, and no session through them " +
		"involved a fishing rod. Until one does, Fish is not claimed to work " +
		"and no threshold here is measured.")
}

// TestASequenceStopsAtAStageThatGaveUp pins that composition does not paper
// over a failure.
//
// A sequence that carried on past a stage which gave up would be a bot digging
// a corridor it never walked to.
func TestASequenceStopsAtAStageThatGaveUp(t *testing.T) {
	t.Parallel()

	sequence, err := NewSequence(stubBehaviour{status: Stopped, reason: ReasonBlocked}, stubBehaviour{status: Complete})
	if err != nil {
		t.Fatalf("NewSequence: %v", err)
	}

	outcome, err := sequence.Tick(context.Background(), placed())
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if outcome.Status != Stopped || outcome.Reason != ReasonBlocked {
		t.Fatalf("outcome = %v/%v, want stopped/blocked", outcome.Status, outcome.Reason)
	}
}

// TestASequenceRunsItsStagesInOrder pins the delegation, including that two
// stages never run in one tick.
//
// Running two in one tick would let a behaviour drive, which is the one thing
// none of them may do.
func TestASequenceRunsItsStagesInOrder(t *testing.T) {
	t.Parallel()

	first := &countingBehaviour{until: 2}
	second := &countingBehaviour{until: 2}

	sequence, err := NewSequence(first, second)
	if err != nil {
		t.Fatalf("NewSequence: %v", err)
	}

	for range 10 {
		outcome, err := sequence.Tick(context.Background(), placed())
		if err != nil {
			t.Fatalf("Tick: %v", err)
		}
		if outcome.Status == Complete {
			break
		}
	}

	if first.ticks == 0 || second.ticks == 0 {
		t.Fatalf("stages ticked %d and %d; both should have run", first.ticks, second.ticks)
	}
}

func TestASequenceNeedsAStage(t *testing.T) {
	t.Parallel()

	if _, err := NewSequence(); !errors.Is(err, ErrNoStages) {
		t.Fatalf("NewSequence() error = %v, want ErrNoStages", err)
	}
	if _, err := NewSequence(nil); err == nil {
		t.Fatal("NewSequence accepted a nil stage")
	}
}

// stubBehaviour answers with one fixed outcome.
type stubBehaviour struct {
	status Status
	reason Reason
}

func (s stubBehaviour) Tick(context.Context, world.Snapshot) (Outcome, error) {
	return Outcome{Status: s.status, Reason: s.reason}, nil
}

// countingBehaviour completes after a fixed number of ticks and counts them.
type countingBehaviour struct {
	until int
	ticks int
}

func (c *countingBehaviour) Tick(context.Context, world.Snapshot) (Outcome, error) {
	c.ticks++
	if c.ticks >= c.until {
		return complete(), nil
	}

	return running(), nil
}
