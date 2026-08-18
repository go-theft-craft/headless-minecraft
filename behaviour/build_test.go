package behaviour

import (
	"errors"
	"testing"

	simgeom "github.com/go-theft-craft/minecraft-simulation/geom"
	"github.com/go-theft-craft/minecraft-simulation/navigation"

	"github.com/go-theft-craft/headless-minecraft/version"
)

// bridgePath is a route that bridges one cell east and then pillars up once.
func bridgePath() navigation.Path {
	return navigation.Path{
		Complete: true,
		Edges: []navigation.Edge{
			{Kind: navigation.EdgeWalk, From: simgeom.BlockPos{}, To: simgeom.BlockPos{X: 1}},
			{
				Kind: navigation.EdgePlace,
				From: simgeom.BlockPos{X: 1}, To: simgeom.BlockPos{X: 2},
			},
			{
				Kind: navigation.EdgePillar,
				From: simgeom.BlockPos{X: 2}, To: simgeom.BlockPos{X: 2, Y: 1},
			},
		},
	}
}

// TestABuildPerformsOnlyThePlacingEdges pins that the executor executes and
// does not plan.
//
// The route is navigation's and the decision to place is the edge's. What this
// turns into actions is the placements alone; the walking between them is the
// follower's job, and a build that also walked would be two behaviours wearing
// one name.
func TestABuildPerformsOnlyThePlacingEdges(t *testing.T) {
	t.Parallel()

	build, err := NewBuild(fullAuth(t), endpoint, bridgePath(), 3, 4)
	if err != nil {
		t.Fatalf("NewBuild: %v", err)
	}

	observed := placed()
	var placements []version.ActionUseOn

	for range 100 {
		outcome, err := build.Tick(t.Context(), observed)
		if err != nil {
			t.Fatalf("Tick: %v", err)
		}
		for _, action := range outcome.Actions {
			if use, ok := action.(version.ActionUseOn); ok {
				placements = append(placements, use)
			}
		}
		if outcome.Status == Complete {
			break
		}
		// The world never confirms anything, so the settle bound is what moves
		// the executor on. That is the backstop working.
		observed.Revision++
	}

	if len(placements) != 2 {
		t.Fatalf("performed %d placements for a path with two placing edges", len(placements))
	}

	// The bridge places against the block under the body, on the side facing
	// where it is going.
	if got, want := placements[0].Block, (version.BlockPos{X: 1, Y: -1}); got != want {
		t.Errorf("the bridge placed against %v, want %v", got, want)
	}
	if placements[0].Face != version.FaceEast {
		t.Errorf("the bridge placed on the %v face, want east", placements[0].Face)
	}

	// The pillar places on top of what is holding the body up.
	if got, want := placements[1].Block, (version.BlockPos{X: 2, Y: -1}); got != want {
		t.Errorf("the pillar placed against %v, want %v", got, want)
	}
	if placements[1].Face != version.FaceTop {
		t.Errorf("the pillar placed on the %v face, want top", placements[1].Face)
	}
}

// TestABuildRefusesARouteWithNothingToPlace pins that the executor says so
// rather than completing instantly.
//
// A build handed a walk is a caller that meant something else, and completing
// without comment would hide it.
func TestABuildRefusesARouteWithNothingToPlace(t *testing.T) {
	t.Parallel()

	walk := navigation.Path{
		Complete: true,
		Edges: []navigation.Edge{
			{Kind: navigation.EdgeWalk, From: simgeom.BlockPos{}, To: simgeom.BlockPos{X: 1}},
		},
	}

	if _, err := NewBuild(fullAuth(t), endpoint, walk, 0, 4); !errors.Is(err, ErrNotAPlacement) {
		t.Fatalf("NewBuild error = %v, want ErrNotAPlacement", err)
	}
}

// TestABuildSelectsItsSlotBeforePlacing pins the ordering.
//
// Placing without selecting puts down whatever happened to be in the hand.
func TestABuildSelectsItsSlotBeforePlacing(t *testing.T) {
	t.Parallel()

	build, err := NewBuild(fullAuth(t), endpoint, bridgePath(), 5, 4)
	if err != nil {
		t.Fatalf("NewBuild: %v", err)
	}

	outcome, err := build.Tick(t.Context(), placed())
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if len(outcome.Actions) < 2 {
		t.Fatalf("the first tick emitted %d actions, want a select and a place", len(outcome.Actions))
	}

	slot, ok := outcome.Actions[0].(version.ActionHeldSlot)
	if !ok {
		t.Fatalf("the first action is %T, want version.ActionHeldSlot", outcome.Actions[0])
	}
	if slot.Slot != 5 {
		t.Errorf("selected slot %d, want 5", slot.Slot)
	}
	if _, ok := outcome.Actions[1].(version.ActionUseOn); !ok {
		t.Fatalf("the second action is %T, want version.ActionUseOn", outcome.Actions[1])
	}
}
