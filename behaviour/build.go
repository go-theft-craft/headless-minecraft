package behaviour

import (
	"context"
	"errors"
	"fmt"

	simgeom "github.com/go-theft-craft/minecraft-simulation/geom"
	"github.com/go-theft-craft/minecraft-simulation/navigation"

	"github.com/go-theft-craft/headless-minecraft/safety"
	"github.com/go-theft-craft/headless-minecraft/version"
	"github.com/go-theft-craft/headless-minecraft/world"
)

// ErrNotAPlacement reports a build behaviour handed a path with nothing to
// place in it.
var ErrNotAPlacement = errors.New("behaviour: this path places no blocks")

// Build performs the placing edges of a route.
//
// It is an executor, not a planner. The route is navigation's and the decision
// to place is the edge's; this turns an edge into the actions that perform it
// and waits for each one to settle. Keeping the planning out of it is what lets
// a server-side mob use the same edges with no behaviour at all.
//
// Bridge and Pillar are the same executor over different edges, which is why
// they are one type rather than two: the difference between them is which cell
// the block goes into, and navigation already decided that. Splitting them here
// would be splitting one job by the name of its input.
type Build struct {
	edges []navigation.Edge
	slot  uint8
	hand  version.Hand
	// settle is how many ticks to wait after a placement before moving on. A
	// placement is not instant and the server is what confirms it; this is the
	// caller's number because how long that takes is the server's.
	settle int

	at      int
	waited  int
	placed  bool
	started uint64
}

// NewBuild returns an executor for the placing edges of a path.
//
// It needs build to place and move because performing a placement moves the
// body through the cells it just filled.
func NewBuild(
	authorization safety.Authorization,
	endpoint string,
	path navigation.Path,
	slot uint8,
	settle int,
) (*Build, error) {
	if err := RequireScopes(
		authorization, endpoint,
		safety.ScopeObserve, safety.ScopeBuild, safety.ScopeMove,
	); err != nil {
		return nil, err
	}
	if slot > 8 {
		return nil, fmt.Errorf("behaviour: hotbar slot %d, and a hotbar has nine", slot)
	}

	edges := placements(path)
	if len(edges) == 0 {
		return nil, ErrNotAPlacement
	}

	return &Build{edges: edges, slot: slot, hand: version.MainHand, settle: settle}, nil
}

// placements returns the edges of a path that put a block down, in order.
//
// It keeps only the mutating edges. A build behaviour performs placements; the
// walking between them is the follower's, and a caller running both hands the
// same path to each.
func placements(path navigation.Path) []navigation.Edge {
	edges := make([]navigation.Edge, 0, len(path.Edges))
	for _, edge := range path.Edges {
		if edge.Kind == navigation.EdgePlace || edge.Kind == navigation.EdgePillar {
			edges = append(edges, edge)
		}
	}

	return edges
}

// Tick implements Behaviour.
func (b *Build) Tick(_ context.Context, observed world.Snapshot) (Outcome, error) {
	if !observed.Player.Known {
		return running(), nil
	}
	if b.at >= len(b.edges) {
		return complete(), nil
	}

	if b.placed {
		// Waiting for the placement to settle. The revision moving is the
		// world telling us something arrived; the tick count is the bound on
		// how long to believe it will.
		b.waited++
		if observed.Revision > b.started || b.waited >= b.settle {
			b.placed, b.waited = false, 0
			b.at++
		}

		return running(), nil
	}

	edge := b.edges[b.at]

	target, face, ok := placementFace(edge)
	if !ok {
		return stopped(ReasonFailed), nil
	}

	b.placed, b.started = true, observed.Revision

	// Select, look, place. The look is what decides which face the server
	// resolves the placement against, and a client that placed without turning
	// puts the block wherever it happened to be facing.
	return running(
		version.ActionHeldSlot{Slot: b.slot},
		version.ActionUseOn{
			Block:  version.BlockPos{X: target.X, Y: target.Y, Z: target.Z},
			Face:   face,
			Cursor: version.Cursor{X: 0.5, Y: 0.5, Z: 0.5},
			Hand:   b.hand,
		},
	), nil
}

// placementFace returns the block to place against and the face to place on.
//
// A placement is always against an existing block, never into empty space: that
// is the game's rule and the reason a bridge starts from ground the body is
// already standing on. For a bridge the support is the cell under the body,
// and the new block goes on its side; for a pillar it is the cell under the
// body's feet, and the new block goes on its top.
func placementFace(edge navigation.Edge) (version.BlockPos, version.Face, bool) {
	switch edge.Kind {
	case navigation.EdgePlace:
		// The block fills the cell under where the body lands, so it is placed
		// against the cell under where the body stands — which is the one
		// beside it.
		support := version.BlockPos{X: edge.From.X, Y: edge.From.Y - 1, Z: edge.From.Z}

		return support, sideToward(edge.From, edge.To), true
	case navigation.EdgePillar:
		// The block fills the cell the body is standing in, placed on top of
		// whatever is holding the body up.
		support := version.BlockPos{X: edge.From.X, Y: edge.From.Y - 1, Z: edge.From.Z}

		return support, version.FaceTop, true
	case navigation.EdgeWalk, navigation.EdgeStep, navigation.EdgeFall,
		navigation.EdgeSwim, navigation.EdgeJumpGap, navigation.EdgeWaterDrop,
		navigation.EdgeClimb, navigation.EdgeDoor:
		// Every read-only edge. NewBuild keeps only the placing ones, so
		// reaching here means an edge kind was added to navigation and this
		// executor was not told which cell it fills — which is a thing to stop
		// on rather than to place a block somewhere for.
		return version.BlockPos{}, 0, false
	}

	return version.BlockPos{}, 0, false
}

// sideToward returns the face of a block that points at a neighbour.
func sideToward(from, to simgeom.BlockPos) version.Face {
	switch {
	case to.X > from.X:
		return version.FaceEast
	case to.X < from.X:
		return version.FaceWest
	case to.Z > from.Z:
		return version.FaceSouth
	case to.Z < from.Z:
		return version.FaceNorth
	default:
		return version.FaceTop
	}
}
