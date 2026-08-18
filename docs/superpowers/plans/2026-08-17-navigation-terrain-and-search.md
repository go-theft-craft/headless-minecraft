# Navigation: terrain and search Implementation Plan

> **Status: complete, 2026-08-18.** Shipped in `minecraft-simulation/navigation`:
> `terrain`, the frontier and search, `Capability`, `Posture`, `Path`, and the
> four read-only edge kinds `EdgeWalk`, `EdgeStep`, `EdgeFall`, and `EdgeSwim`.
> The boxes below are ticked by outcome, checked against those packages on
> 2026-08-18. What this plan deferred is open elsewhere: `JumpGap` and the missing postures in
> [navigation edge completion](2026-08-18-navigation-edge-completion.md), the
> mutating edges in [mutating edges and pillar](2026-08-18-mutating-edges-pillar.md),
> and the `examples/orbit` rewrite in
> [aiming and reach geometry](2026-08-18-aiming-and-reach-geometry.md).

> **For agentic workers:** REQUIRED SUB-SKILL: Use `subagent-driven-development` (recommended) or `executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build `terrain` and `navigation` in `minecraft-simulation`, so any body — a client bot or a server mob — can ask whether it fits somewhere and get a deterministic route there over walk, step, fall, and swim edges.

**Architecture:** `terrain` answers static questions about a `world.View` through a `Query` value that carries the body and a profile-supplied `Facts` oracle; it owns no version constant and no search. `navigation` composes those answers into a 4-connected A* whose node is `(position, posture)`, whose costs are all in ticks, and whose frontier breaks ties on a total order so two identical searches produce identical bytes. Nothing in either package imports `sim`.

**Tech Stack:** Go 1.26.6 from `openserbia/go-flake`, Devbox, Task, `golangci-lint`, and the existing `geom`, `world`, and `collision` packages.

## Scope

This plan covers steps 1 through 3 of the design's sequencing, minus the parts that depend on unlanded work. Concretely:

**In scope:** the `terrain` package; the `navigation` vocabulary; a bounded, deterministic A* over `Walk`, `Step`, `Fall`, and `Swim` edges.

**Out of scope, with the reason:**

| Deferred | Why |
| --- | --- |
| `navigator` (the follower) | Needs M8.8's `adapter.Drive` and `predict.Correction`, neither of which has landed |
| `Dig`, `Place`, `Support`, `Collapse` edges | Need M9.4 break time and M9.5 placement legality |
| `navigation.Overlay` and the ban-and-re-search validation loop | Only mutating edges need them. With no mutating edges an overlay is dead code |
| `data.Block.Falling` extraction | Only the deferred edges consume it. `minecraft-reference` warns that a published document field is far harder to remove than to add |
| `JumpGap` edges | Doing it honestly needs a per-profile reach table computed from the movement kernel, which is its own deliverable. A guessed maximum gap would be a number this repository does not verify |
| The `Evaluator` escape hatch and `Path` provenance | An override exists to change a decision. With one fixed edge set and no cost policy to replace, there is nothing for a custom evaluator to do, and a provenance field nothing can set is a field every caller must still switch on |
| The light policy on `Capability` | Torch spacing is priced against `Collapse` and the tunnels digging makes. Both are deferred, so a light budget would cost a route nothing in it can spend |
| The `examples/orbit` rewrite | `minecraft-simulation` has no tags. `headless-minecraft` cannot import it until there is a released version, and a `replace` directive in a public repository is not acceptable |

Each deferred item gets its own plan once its prerequisite lands.

## Global Constraints

- Go 1.26.6 from `openserbia/go-flake`; Devbox and Task drive every gate.
- Every gate runs in `minecraft-simulation`. This plan touches no other repository.
- `terrain` and `navigation` import `geom`, `world`, and `collision` only. Neither imports `sim`, `movement`, `entity`, or any profile.
- No version constant is typed into either package. A number a version owns arrives as a field or an argument.
- No map iteration in any code path that affects an output ordering. Go randomizes it, and a randomized order fails the determinism gate.
- Unknown is never folded into blocked. A caller that cannot read a cell is told so.
- `devbox run -- task lint`, `devbox run -- task test`, and `devbox run -- task verify` pass before every commit.
- Conventional commit subjects. No `Co-Authored-By` trailer and no `Claude-Session` line.

## File Structure

All paths are relative to the `minecraft-simulation` repository root.

| File | Responsibility |
| --- | --- |
| `terrain/terrain.go` | Package doc, `Facts`, `Hazard`, `Fluid` |
| `terrain/body.go` | `Body`, `BoxAt`, `FeetOf` |
| `terrain/query.go` | `Query`, `Fits`, `Ground` |
| `terrain/passability.go` | `Passability`, `Passable` |
| `navigation/navigation.go` | Package doc, `Posture`, `Capability` |
| `navigation/edge.go` | `EdgeKind`, `Edge`, `Path`, `Reason` |
| `navigation/frontier.go` | The deterministic priority queue and the node total order |
| `navigation/search.go` | `Budget`, `Find`, neighbour expansion |
| `navigation/property_test.go` | Cross-cutting properties and the determinism gate |

Tests live beside their subject as `_test.go` in the same package, matching the repository's existing habit.

---

## Task 1: terrain body and fit

**Files:**
- Create: `terrain/terrain.go`
- Create: `terrain/body.go`
- Create: `terrain/query.go`
- Test: `terrain/query_test.go`

**Interfaces:**
- Consumes: `geom.AABB`, `geom.Vec3`, `geom.BlockPos`, `world.BlockView`, `collision.Gather`
- Produces: `terrain.Body{HalfWidth, Height, StepHeight float64}`, `terrain.Body.BoxAt(feet geom.Vec3) geom.AABB`, `terrain.FeetOf(cell geom.BlockPos) geom.Vec3`, `terrain.Fit` with `FitUnknown`/`FitClear`/`FitBlocked`, `terrain.Query{View world.View, Facts Facts, Body Body, Limit int}`, `terrain.Query.Fits(feet geom.Vec3) (Fit, error)`

- [x] **Step 1: Write the failing test**

Create `terrain/query_test.go`:

```go
package terrain

import (
	"testing"

	"github.com/go-theft-craft/minecraft-simulation/geom"
	"github.com/go-theft-craft/minecraft-simulation/world"
)

// testBody is a two-block body 0.6 wide. The numbers are the test's, not the
// package's: terrain owns no version constant, so a body arrives as a value.
var testBody = Body{HalfWidth: 0.3, Height: 1.8, StepHeight: 0.6}

// room returns a view with a 5x5x5 air pocket floored with full cubes at y=-1.
func room() *world.Blocks {
	blocks := world.NewBlocks()
	blocks.Fill(geom.BlockPos{X: -2, Y: 0, Z: -2}, geom.BlockPos{X: 2, Y: 4, Z: 2}, geom.EmptyShape())
	blocks.Fill(geom.BlockPos{X: -2, Y: -1, Z: -2}, geom.BlockPos{X: 2, Y: -1, Z: 2}, geom.FullCube())

	return blocks
}

func TestFitsReportsClearAirAsClear(t *testing.T) {
	query := Query{View: room(), Body: testBody}

	fit, err := query.Fits(FeetOf(geom.BlockPos{X: 0, Y: 0, Z: 0}))
	if err != nil {
		t.Fatalf("Fits returned an error: %v", err)
	}
	if fit != FitClear {
		t.Fatalf("Fits = %v, want FitClear", fit)
	}
}

func TestFitsReportsASolidCellAsBlocked(t *testing.T) {
	blocks := room()
	blocks.Set(geom.BlockPos{X: 0, Y: 0, Z: 0}, geom.FullCube())
	query := Query{View: blocks, Body: testBody}

	fit, err := query.Fits(FeetOf(geom.BlockPos{X: 0, Y: 0, Z: 0}))
	if err != nil {
		t.Fatalf("Fits returned an error: %v", err)
	}
	if fit != FitBlocked {
		t.Fatalf("Fits = %v, want FitBlocked", fit)
	}
}

// A two-block body whose head is in stone does not fit, even though its feet
// are in air. A test that only described the feet cell would pass on a
// one-cell implementation.
func TestFitsChecksTheHeadCell(t *testing.T) {
	blocks := room()
	blocks.Set(geom.BlockPos{X: 0, Y: 1, Z: 0}, geom.FullCube())
	query := Query{View: blocks, Body: testBody}

	fit, err := query.Fits(FeetOf(geom.BlockPos{X: 0, Y: 0, Z: 0}))
	if err != nil {
		t.Fatalf("Fits returned an error: %v", err)
	}
	if fit != FitBlocked {
		t.Fatalf("Fits = %v, want FitBlocked", fit)
	}
}

// Resting exactly on a floor is not an overlap. geom.AABB.Intersects excludes
// shared faces for this reason, and a body standing on the ground must fit.
func TestFitsIgnoresTheFloorItStandsOn(t *testing.T) {
	query := Query{View: room(), Body: testBody}

	fit, err := query.Fits(geom.Vec3{X: 0.5, Y: 0, Z: 0.5})
	if err != nil {
		t.Fatalf("Fits returned an error: %v", err)
	}
	if fit != FitClear {
		t.Fatalf("Fits = %v, want FitClear", fit)
	}
}

func TestFitsReportsUnknownForAnUndescribedCell(t *testing.T) {
	blocks := room()
	blocks.Forget(geom.BlockPos{X: 0, Y: 1, Z: 0})
	query := Query{View: blocks, Body: testBody}

	fit, err := query.Fits(FeetOf(geom.BlockPos{X: 0, Y: 0, Z: 0}))
	if err != nil {
		t.Fatalf("Fits returned an error: %v", err)
	}
	if fit != FitUnknown {
		t.Fatalf("Fits = %v, want FitUnknown", fit)
	}
}
```

- [x] **Step 2: Run the test to verify it fails**

Run: `cd <minecraft-simulation> && devbox run -- go test ./terrain/ -v`
Expected: FAIL — the package does not exist.

- [x] **Step 3: Write the package doc and the Facts oracle**

Create `terrain/terrain.go`:

```go
// Package terrain answers static questions about a world view: whether a body
// fits somewhere, whether anything holds it up, and what would hurt it.
//
// It owns no version constant. A body's width and step height arrive as a
// value, and every fact about a block that its collision shape does not carry
// arrives through Facts, because a world.BlockRef is opaque and only the
// profile that minted it can say what it names.
//
// Nothing here searches. Composing these answers into a route is navigation's
// job, and keeping the two apart is what lets a mob and a bot share the
// predicates while disagreeing about the route.
package terrain

import "github.com/go-theft-craft/minecraft-simulation/world"

// Hazard names what makes a position dangerous to occupy.
type Hazard uint8

const (
	// HazardNone means the block does no harm.
	HazardNone Hazard = iota
	// HazardBurn is fire and lava.
	HazardBurn
	// HazardContact is damage on touch, such as cactus.
	HazardContact
)

// Fluid names the fluid filling a cell.
type Fluid uint8

const (
	// FluidNone means the cell holds no fluid.
	FluidNone Fluid = iota
	// FluidWater is water, flowing or still.
	FluidWater
	// FluidLava is lava, flowing or still.
	FluidLava
)

// Facts is what a profile tells terrain about a block that the block's
// collision shape does not already say.
//
// It is an interface rather than a table because world.BlockRef is opaque:
// this package cannot look at a handle and see sand. It is separate from
// sim.Profile for the same reason sim.BlockNames is — a tick never asks these
// questions, and terrain must not import sim.
//
// A nil Facts is legal. It answers HazardNone and FluidNone for everything,
// which is what a caller that only cares about geometry wants.
type Facts interface {
	// Hazard reports what a block does to a body occupying it.
	Hazard(ref world.BlockRef) Hazard
	// Fluid reports the fluid a block is.
	Fluid(ref world.BlockRef) Fluid
}
```

- [x] **Step 4: Write the body**

Create `terrain/body.go`:

```go
package terrain

import "github.com/go-theft-craft/minecraft-simulation/geom"

// Body is the box a thing occupies, measured from its feet.
//
// A position names the feet rather than the centre, because that is where a
// block cell puts a standing entity and it is what every caller here has.
type Body struct {
	// HalfWidth is half the footprint, in blocks.
	HalfWidth float64
	// Height is how tall the body is, in blocks.
	Height float64
	// StepHeight is how far the body rises to clear an obstacle without
	// leaving the ground. It arrives from the profile's MotionConstants.
	StepHeight float64
}

// BoxAt returns the box the body occupies with its feet at the position.
func (b Body) BoxAt(feet geom.Vec3) geom.AABB {
	return geom.AABB{
		MinX: feet.X - b.HalfWidth,
		MinY: feet.Y,
		MinZ: feet.Z - b.HalfWidth,
		MaxX: feet.X + b.HalfWidth,
		MaxY: feet.Y + b.Height,
		MaxZ: feet.Z + b.HalfWidth,
	}
}

// FeetOf returns the feet position of a body standing in the middle of a cell.
func FeetOf(cell geom.BlockPos) geom.Vec3 {
	return geom.Vec3{
		X: float64(cell.X) + 0.5,
		Y: float64(cell.Y),
		Z: float64(cell.Z) + 0.5,
	}
}
```

- [x] **Step 5: Write the query and the fit test**

Create `terrain/query.go`:

```go
package terrain

import (
	"github.com/go-theft-craft/minecraft-simulation/collision"
	"github.com/go-theft-craft/minecraft-simulation/geom"
	"github.com/go-theft-craft/minecraft-simulation/world"
)

// Fit reports whether a body occupies a position.
type Fit uint8

const (
	// FitUnknown means the view could not answer for at least one cell the
	// body would occupy. It is the zero value so that a caller who forgets to
	// switch on it gets the cautious answer.
	FitUnknown Fit = iota
	// FitClear means the body occupies the position without overlapping
	// anything.
	FitClear
	// FitBlocked means something is in the way.
	FitBlocked
)

// Ground reports what is under a body's feet.
type Ground uint8

const (
	// GroundUnknown means the view could not answer.
	GroundUnknown Ground = iota
	// GroundSolid means something holds the body up.
	GroundSolid
	// GroundOpen means nothing does.
	GroundOpen
)

// groundProbe is how far below the feet to look for support. It has to be a
// volume rather than a plane because geom.AABB.Intersects excludes shared
// faces, so a probe of zero height would touch the floor and report nothing.
const groundProbe = 1e-4

// Query is one body asking about one view.
//
// Facts may be nil. Limit is the collision candidate budget; a non-positive
// value means no limit, matching collision.Gather.
type Query struct {
	View  world.View
	Facts Facts
	Body  Body
	Limit int
}

// Fits reports whether the body occupies the position.
func (q Query) Fits(feet geom.Vec3) (Fit, error) {
	box := q.Body.BoxAt(feet)

	candidates, err := collision.Gather(q.View, box, q.Limit)
	if err != nil {
		return FitUnknown, err
	}
	if len(candidates.Unknown) != 0 {
		return FitUnknown, nil
	}

	for _, other := range candidates.Boxes {
		if box.Intersects(other) {
			return FitBlocked, nil
		}
	}

	return FitClear, nil
}
```

- [x] **Step 6: Run the tests to verify they pass**

Run: `cd <minecraft-simulation> && devbox run -- go test ./terrain/ -v`
Expected: PASS, five tests.

- [x] **Step 7: Lint and commit**

```bash
cd <minecraft-simulation>
devbox run -- task lint
git add terrain/
git commit -m "feat(terrain): report whether a body fits a position"
```

---

## Task 2: ground support

**Files:**
- Modify: `terrain/query.go`
- Test: `terrain/query_test.go`

**Interfaces:**
- Consumes: `terrain.Query`, `terrain.Ground`, `groundProbe` from Task 1
- Produces: `terrain.Query.Ground(feet geom.Vec3) (Ground, error)`

- [x] **Step 1: Write the failing test**

Append to `terrain/query_test.go`:

```go
func TestGroundReportsSolidOverAFloor(t *testing.T) {
	query := Query{View: room(), Body: testBody}

	ground, err := query.Ground(FeetOf(geom.BlockPos{X: 0, Y: 0, Z: 0}))
	if err != nil {
		t.Fatalf("Ground returned an error: %v", err)
	}
	if ground != GroundSolid {
		t.Fatalf("Ground = %v, want GroundSolid", ground)
	}
}

func TestGroundReportsOpenOverAHole(t *testing.T) {
	blocks := room()
	blocks.SetAir(geom.BlockPos{X: 0, Y: -1, Z: 0})
	query := Query{View: blocks, Body: testBody}

	ground, err := query.Ground(FeetOf(geom.BlockPos{X: 0, Y: 0, Z: 0}))
	if err != nil {
		t.Fatalf("Ground returned an error: %v", err)
	}
	if ground != GroundOpen {
		t.Fatalf("Ground = %v, want GroundOpen", ground)
	}
}

func TestGroundReportsUnknownForAnUndescribedFloor(t *testing.T) {
	blocks := room()
	blocks.Forget(geom.BlockPos{X: 0, Y: -1, Z: 0})
	query := Query{View: blocks, Body: testBody}

	ground, err := query.Ground(FeetOf(geom.BlockPos{X: 0, Y: 0, Z: 0}))
	if err != nil {
		t.Fatalf("Ground returned an error: %v", err)
	}
	if ground != GroundUnknown {
		t.Fatalf("Ground = %v, want GroundUnknown", ground)
	}
}
```

- [x] **Step 2: Run the tests to verify they fail**

Run: `cd <minecraft-simulation> && devbox run -- go test ./terrain/ -run TestGround -v`
Expected: FAIL — `query.Ground undefined`.

- [x] **Step 3: Implement Ground**

Append to `terrain/query.go`:

```go
// Ground reports whether anything holds the body up.
//
// The probe is a thin slab under the footprint rather than the cell below,
// because a body wider than one block stands on more than one cell and a
// half-slab holds it up from less than a full one.
func (q Query) Ground(feet geom.Vec3) (Ground, error) {
	box := q.Body.BoxAt(feet)
	probe := geom.AABB{
		MinX: box.MinX,
		MinY: feet.Y - groundProbe,
		MinZ: box.MinZ,
		MaxX: box.MaxX,
		MaxY: feet.Y,
		MaxZ: box.MaxZ,
	}

	candidates, err := collision.Gather(q.View, probe, q.Limit)
	if err != nil {
		return GroundUnknown, err
	}
	if len(candidates.Unknown) != 0 {
		return GroundUnknown, nil
	}

	for _, other := range candidates.Boxes {
		if probe.Intersects(other) {
			return GroundSolid, nil
		}
	}

	return GroundOpen, nil
}
```

- [x] **Step 4: Run the tests to verify they pass**

Run: `cd <minecraft-simulation> && devbox run -- go test ./terrain/ -v`
Expected: PASS, eight tests.

- [x] **Step 5: Lint and commit**

```bash
cd <minecraft-simulation>
devbox run -- task lint
git add terrain/
git commit -m "feat(terrain): report whether anything holds a body up"
```

---

## Task 3: passability

**Files:**
- Create: `terrain/passability.go`
- Test: `terrain/passability_test.go`

**Interfaces:**
- Consumes: `terrain.Query.Fits`, `terrain.Query.Ground`, `terrain.FeetOf` from Tasks 1 and 2
- Produces: `terrain.Passability` with `Unknown`/`Clear`/`Steppable`/`Blocked`, `terrain.Passability.String() string`, `terrain.Query.Passable(cell geom.BlockPos) (Passability, error)`

- [x] **Step 1: Write the failing test**

Create `terrain/passability_test.go`:

```go
package terrain

import (
	"testing"

	"github.com/go-theft-craft/minecraft-simulation/geom"
	"github.com/go-theft-craft/minecraft-simulation/world"
)

func TestPassableReportsClearFlatGround(t *testing.T) {
	query := Query{View: room(), Body: testBody}

	got, err := query.Passable(geom.BlockPos{X: 0, Y: 0, Z: 0})
	if err != nil {
		t.Fatalf("Passable returned an error: %v", err)
	}
	if got != Clear {
		t.Fatalf("Passable = %v, want Clear", got)
	}
}

// One solid block with a body's worth of room above it is something to climb,
// not something to route around. Folding this into Blocked makes a bot walk
// around every doorstep.
func TestPassableReportsAOneBlockRiseAsSteppable(t *testing.T) {
	blocks := room()
	blocks.Set(geom.BlockPos{X: 1, Y: 0, Z: 0}, geom.FullCube())
	query := Query{View: blocks, Body: testBody}

	got, err := query.Passable(geom.BlockPos{X: 1, Y: 0, Z: 0})
	if err != nil {
		t.Fatalf("Passable returned an error: %v", err)
	}
	if got != Steppable {
		t.Fatalf("Passable = %v, want Steppable", got)
	}
}

func TestPassableReportsATwoBlockWallAsBlocked(t *testing.T) {
	blocks := room()
	blocks.Set(geom.BlockPos{X: 1, Y: 0, Z: 0}, geom.FullCube())
	blocks.Set(geom.BlockPos{X: 1, Y: 1, Z: 0}, geom.FullCube())
	query := Query{View: blocks, Body: testBody}

	got, err := query.Passable(geom.BlockPos{X: 1, Y: 0, Z: 0})
	if err != nil {
		t.Fatalf("Passable returned an error: %v", err)
	}
	if got != Blocked {
		t.Fatalf("Passable = %v, want Blocked", got)
	}
}

// A hole is not somewhere to stand. It is also not a wall, and Task 7's Fall
// edge is what crosses it; Passable's job is only to say it is not Clear.
func TestPassableReportsAHoleAsBlocked(t *testing.T) {
	blocks := room()
	blocks.SetAir(geom.BlockPos{X: 1, Y: -1, Z: 0})
	query := Query{View: blocks, Body: testBody}

	got, err := query.Passable(geom.BlockPos{X: 1, Y: 0, Z: 0})
	if err != nil {
		t.Fatalf("Passable returned an error: %v", err)
	}
	if got != Blocked {
		t.Fatalf("Passable = %v, want Blocked", got)
	}
}

// An unloaded cell is refused, never guessed. A bot that read unknown as a
// wall would stop at the edge of its own render distance, and one that read it
// as air would walk into a wall it could not see.
func TestPassableReportsUnknownForAnUndescribedCell(t *testing.T) {
	blocks := room()
	blocks.Forget(geom.BlockPos{X: 1, Y: 0, Z: 0})
	query := Query{View: blocks, Body: testBody}

	got, err := query.Passable(geom.BlockPos{X: 1, Y: 0, Z: 0})
	if err != nil {
		t.Fatalf("Passable returned an error: %v", err)
	}
	if got != Unknown {
		t.Fatalf("Passable = %v, want Unknown", got)
	}
}

// A one-block body fits where a two-block body does not. The body is a
// parameter for exactly this reason.
func TestPassableAcceptsAOneBlockBody(t *testing.T) {
	blocks := room()
	blocks.Set(geom.BlockPos{X: 1, Y: 1, Z: 0}, geom.FullCube())
	small := Query{View: blocks, Body: Body{HalfWidth: 0.3, Height: 0.9, StepHeight: 0.6}}
	tall := Query{View: blocks, Body: testBody}

	got, err := small.Passable(geom.BlockPos{X: 1, Y: 0, Z: 0})
	if err != nil {
		t.Fatalf("Passable returned an error: %v", err)
	}
	if got != Clear {
		t.Fatalf("small body Passable = %v, want Clear", got)
	}

	got, err = tall.Passable(geom.BlockPos{X: 1, Y: 0, Z: 0})
	if err != nil {
		t.Fatalf("Passable returned an error: %v", err)
	}
	if got != Blocked {
		t.Fatalf("tall body Passable = %v, want Blocked", got)
	}
}

func TestPassabilityStringNamesEveryValue(t *testing.T) {
	cases := map[Passability]string{
		Unknown:   "unknown",
		Clear:     "clear",
		Steppable: "steppable",
		Blocked:   "blocked",
	}
	for value, want := range cases {
		if got := value.String(); got != want {
			t.Fatalf("Passability(%d).String() = %q, want %q", value, got, want)
		}
	}
}

// world.Blocks is the view under test throughout. Asserting the interface here
// makes a change to world.View fail in this package rather than mysteriously
// in navigation.
var _ world.View = (*world.Blocks)(nil)
```

- [x] **Step 2: Run the tests to verify they fail**

Run: `cd <minecraft-simulation> && devbox run -- go test ./terrain/ -run TestPassab -v`
Expected: FAIL — `undefined: Clear`.

- [x] **Step 3: Implement passability**

Create `terrain/passability.go`:

```go
package terrain

import (
	"fmt"

	"github.com/go-theft-craft/minecraft-simulation/geom"
)

// stepRise is the rise a body climbs by leaving the ground: one block. It is
// geometry rather than a version constant — a cell is a unit cube in every
// version — and it is named because an unexplained 1.0 in a movement rule is
// exactly the kind of number this module refuses.
const stepRise = 1.0

// Passability says why a body cannot stand somewhere, because the four answers
// lead to different places.
type Passability uint8

const (
	// Unknown means at least one cell the body needs is undescribed. It is the
	// zero value, and it is deliberately not folded into Blocked: a body that
	// treats every unloaded chunk as a wall gives up at the edge of its render
	// distance.
	Unknown Passability = iota
	// Clear means the body fits and something holds it up.
	Clear
	// Steppable means the body does not fit here but does one block higher,
	// with support. It is something to climb rather than something to avoid.
	Steppable
	// Blocked means the body cannot stand here and cannot climb it. A hole is
	// Blocked too: it is not somewhere to stand, and crossing it is a fall
	// rather than a step.
	Blocked
)

// String returns the value's name.
func (p Passability) String() string {
	switch p {
	case Unknown:
		return "unknown"
	case Clear:
		return "clear"
	case Steppable:
		return "steppable"
	case Blocked:
		return "blocked"
	default:
		return fmt.Sprintf("Passability(%d)", uint8(p))
	}
}

// Passable reports whether the body can stand in a cell.
func (q Query) Passable(cell geom.BlockPos) (Passability, error) {
	feet := FeetOf(cell)

	fit, err := q.Fits(feet)
	if err != nil {
		return Unknown, err
	}
	switch fit {
	case FitUnknown:
		return Unknown, nil
	case FitBlocked:
		return q.stepped(feet)
	}

	ground, err := q.Ground(feet)
	if err != nil {
		return Unknown, err
	}
	switch ground {
	case GroundUnknown:
		return Unknown, nil
	case GroundOpen:
		return Blocked, nil
	}

	return Clear, nil
}

// stepped answers whether an obstruction is one block tall and standable on
// top of.
func (q Query) stepped(feet geom.Vec3) (Passability, error) {
	above := feet.Add(geom.Vec3{Y: stepRise})

	fit, err := q.Fits(above)
	if err != nil {
		return Unknown, err
	}
	switch fit {
	case FitUnknown:
		return Unknown, nil
	case FitBlocked:
		return Blocked, nil
	}

	ground, err := q.Ground(above)
	if err != nil {
		return Unknown, err
	}
	switch ground {
	case GroundUnknown:
		return Unknown, nil
	case GroundOpen:
		return Blocked, nil
	}

	return Steppable, nil
}
```

- [x] **Step 4: Run the tests to verify they pass**

Run: `cd <minecraft-simulation> && devbox run -- go test ./terrain/ -v`
Expected: PASS, fifteen tests.

- [x] **Step 5: Lint and commit**

```bash
cd <minecraft-simulation>
devbox run -- task lint
git add terrain/
git commit -m "feat(terrain): classify a cell as clear, steppable, blocked, or unknown"
```

---

## Task 4: hazards and fluids

**Files:**
- Modify: `terrain/query.go`
- Test: `terrain/hazard_test.go`

**Interfaces:**
- Consumes: `terrain.Facts`, `terrain.Hazard`, `terrain.Fluid` from Task 1
- Produces: `terrain.Query.HazardAt(cell geom.BlockPos) (Hazard, world.Lookup, error)`, `terrain.Query.FluidAt(cell geom.BlockPos) (Fluid, world.Lookup, error)`

Both report the lookup alongside the answer so a caller can tell "no hazard" from "nobody described this cell". Lava has no collision shape, so a body that only consulted geometry walks into it.

- [x] **Step 1: Write the failing test**

Create `terrain/hazard_test.go`:

```go
package terrain

import (
	"testing"

	"github.com/go-theft-craft/minecraft-simulation/geom"
	"github.com/go-theft-craft/minecraft-simulation/world"
)

// testFacts is a Facts whose answers a test states directly, keyed by the
// opaque handle. It is a map, but nothing here iterates it, so no output
// ordering depends on Go's map order.
type testFacts struct {
	hazards map[world.BlockRef]Hazard
	fluids  map[world.BlockRef]Fluid
}

func (f testFacts) Hazard(ref world.BlockRef) Hazard { return f.hazards[ref] }
func (f testFacts) Fluid(ref world.BlockRef) Fluid   { return f.fluids[ref] }

const (
	refLava   world.BlockRef = 10
	refCactus world.BlockRef = 81
)

func lavaWorld() (*world.Blocks, testFacts) {
	blocks := room()
	// Lava has no collision shape. A body that consulted only geometry would
	// find this cell clear and walk into it, which is the whole reason Facts
	// exists.
	blocks.SetBlock(geom.BlockPos{X: 1, Y: 0, Z: 0}, refLava, geom.EmptyShape())
	blocks.SetBlock(geom.BlockPos{X: 2, Y: 0, Z: 0}, refCactus, geom.FullCube())

	facts := testFacts{
		hazards: map[world.BlockRef]Hazard{refLava: HazardBurn, refCactus: HazardContact},
		fluids:  map[world.BlockRef]Fluid{refLava: FluidLava},
	}

	return blocks, facts
}

func TestHazardAtReportsBurnForLava(t *testing.T) {
	blocks, facts := lavaWorld()
	query := Query{View: blocks, Facts: facts, Body: testBody}

	hazard, lookup, err := query.HazardAt(geom.BlockPos{X: 1, Y: 0, Z: 0})
	if err != nil {
		t.Fatalf("HazardAt returned an error: %v", err)
	}
	if lookup == world.LookupUnknown {
		t.Fatal("HazardAt reported unknown for a described cell")
	}
	if hazard != HazardBurn {
		t.Fatalf("HazardAt = %v, want HazardBurn", hazard)
	}
}

func TestHazardAtReportsContactForCactus(t *testing.T) {
	blocks, facts := lavaWorld()
	query := Query{View: blocks, Facts: facts, Body: testBody}

	hazard, _, err := query.HazardAt(geom.BlockPos{X: 2, Y: 0, Z: 0})
	if err != nil {
		t.Fatalf("HazardAt returned an error: %v", err)
	}
	if hazard != HazardContact {
		t.Fatalf("HazardAt = %v, want HazardContact", hazard)
	}
}

func TestHazardAtReportsUnknownForAnUndescribedCell(t *testing.T) {
	blocks, facts := lavaWorld()
	blocks.Forget(geom.BlockPos{X: 1, Y: 0, Z: 0})
	query := Query{View: blocks, Facts: facts, Body: testBody}

	_, lookup, err := query.HazardAt(geom.BlockPos{X: 1, Y: 0, Z: 0})
	if err != nil {
		t.Fatalf("HazardAt returned an error: %v", err)
	}
	if lookup != world.LookupUnknown {
		t.Fatalf("HazardAt lookup = %v, want LookupUnknown", lookup)
	}
}

// A nil Facts is legal and answers "nothing special", which is what a caller
// that only cares about geometry wants.
func TestHazardAtToleratesNilFacts(t *testing.T) {
	query := Query{View: room(), Body: testBody}

	hazard, _, err := query.HazardAt(geom.BlockPos{X: 0, Y: 0, Z: 0})
	if err != nil {
		t.Fatalf("HazardAt returned an error: %v", err)
	}
	if hazard != HazardNone {
		t.Fatalf("HazardAt = %v, want HazardNone", hazard)
	}
}

func TestFluidAtReportsLava(t *testing.T) {
	blocks, facts := lavaWorld()
	query := Query{View: blocks, Facts: facts, Body: testBody}

	fluid, _, err := query.FluidAt(geom.BlockPos{X: 1, Y: 0, Z: 0})
	if err != nil {
		t.Fatalf("FluidAt returned an error: %v", err)
	}
	if fluid != FluidLava {
		t.Fatalf("FluidAt = %v, want FluidLava", fluid)
	}
}
```

- [x] **Step 2: Run the tests to verify they fail**

Run: `cd <minecraft-simulation> && devbox run -- go test ./terrain/ -run "TestHazardAt|TestFluidAt" -v`
Expected: FAIL — `query.HazardAt undefined`.

- [x] **Step 3: Implement the lookups**

Append to `terrain/query.go`:

```go
// HazardAt reports what a cell would do to a body occupying it.
//
// The lookup is returned because "no hazard" and "nobody described this cell"
// are different answers, and a body that confuses them walks into lava it
// could not see. Lava carries no collision shape, so geometry alone never
// finds it.
func (q Query) HazardAt(cell geom.BlockPos) (Hazard, world.Lookup, error) {
	ref, lookup := q.View.BlockState(cell)
	if lookup == world.LookupUnknown || q.Facts == nil {
		return HazardNone, lookup, nil
	}

	return q.Facts.Hazard(ref), lookup, nil
}

// FluidAt reports the fluid filling a cell.
func (q Query) FluidAt(cell geom.BlockPos) (Fluid, world.Lookup, error) {
	ref, lookup := q.View.BlockState(cell)
	if lookup == world.LookupUnknown || q.Facts == nil {
		return FluidNone, lookup, nil
	}

	return q.Facts.Fluid(ref), lookup, nil
}
```

- [x] **Step 4: Run the tests to verify they pass**

Run: `cd <minecraft-simulation> && devbox run -- go test ./terrain/ -v`
Expected: PASS, twenty-one tests.

- [x] **Step 5: Lint and commit**

```bash
cd <minecraft-simulation>
devbox run -- task lint
git add terrain/
git commit -m "feat(terrain): report block hazards and fluids through a profile oracle"
```

---

## Task 5: the navigation vocabulary

**Files:**
- Create: `navigation/navigation.go`
- Create: `navigation/edge.go`
- Test: `navigation/edge_test.go`

**Interfaces:**
- Consumes: `terrain.Body`, `terrain.Facts`, `geom.BlockPos`
- Produces: `navigation.Posture` with `PostureStand`/`PostureSwim`; `navigation.Capability{Body terrain.Body, SafeFall float64, CanSwim bool, WalkTicks, StepTicks, FallTicks, SwimTicks float64, CandidateLimit int}`; `navigation.Capability.cheapest() float64`; `navigation.EdgeKind` with `EdgeWalk`/`EdgeStep`/`EdgeFall`/`EdgeSwim`; `navigation.Edge{Kind EdgeKind, From, To geom.BlockPos, Posture Posture, Cost float64}`; `navigation.Reason` with `ReasonFound`/`ReasonBudget`/`ReasonCeiling`/`ReasonUnreachable`; `navigation.Path{Edges []Edge, Cost float64, Complete bool, Reason Reason}`

- [x] **Step 1: Write the failing test**

Create `navigation/edge_test.go`:

```go
package navigation

import "testing"

func TestEdgeKindStringNamesEveryValue(t *testing.T) {
	cases := map[EdgeKind]string{
		EdgeWalk: "walk",
		EdgeStep: "step",
		EdgeFall: "fall",
		EdgeSwim: "swim",
	}
	for value, want := range cases {
		if got := value.String(); got != want {
			t.Fatalf("EdgeKind(%d).String() = %q, want %q", value, got, want)
		}
	}
}

func TestReasonStringNamesEveryValue(t *testing.T) {
	cases := map[Reason]string{
		ReasonFound:       "found",
		ReasonBudget:      "budget",
		ReasonCeiling:     "ceiling",
		ReasonUnreachable: "unreachable",
	}
	for value, want := range cases {
		if got := value.String(); got != want {
			t.Fatalf("Reason(%d).String() = %q, want %q", value, got, want)
		}
	}
}

// The heuristic multiplies distance by the cheapest edge a capability can
// take. Getting this wrong makes the search inadmissible and it stops
// returning shortest paths, which no test of a single path would catch.
func TestCheapestIsTheLowestEnabledEdgeCost(t *testing.T) {
	walker := Capability{WalkTicks: 5, StepTicks: 9, FallTicks: 3, SwimTicks: 1}
	if got := walker.cheapest(); got != 3 {
		t.Fatalf("cheapest = %v, want 3 (swimming disabled)", got)
	}

	swimmer := walker
	swimmer.CanSwim = true
	if got := swimmer.cheapest(); got != 1 {
		t.Fatalf("cheapest = %v, want 1", got)
	}
}
```

- [x] **Step 2: Run the test to verify it fails**

Run: `cd <minecraft-simulation> && devbox run -- go test ./navigation/ -v`
Expected: FAIL — the package does not exist.

- [x] **Step 3: Write the package doc, posture, and capability**

Create `navigation/navigation.go`:

```go
// Package navigation searches a route through a world and reports it as typed
// edges.
//
// The route is a value rather than a hidden state machine. A caller can print
// it, test it, and compare it against a recording, which a navigator that only
// answered "what do I press this tick" could not.
//
// Every cost is in ticks. Break time is in ticks and movement is in ticks, so
// a version that adds digging can compare "mine through" against "walk around"
// in one unit rather than through a weighting nobody can justify.
//
// Nothing here imports sim. A rule that needs a version's number receives it
// on Capability, which is what lets 1.8.9 and 26.1.2 share this search.
package navigation

import (
	"fmt"

	"github.com/go-theft-craft/minecraft-simulation/terrain"
)

// Posture is how a body occupies a position.
//
// Two postures at one position are distinct nodes, because they differ in the
// box the body needs and in which edges leave them.
type Posture uint8

const (
	// PostureStand is a body standing on ground.
	PostureStand Posture = iota
	// PostureSwim is a body in a fluid it can swim.
	PostureSwim
)

// String returns the posture's name.
func (p Posture) String() string {
	switch p {
	case PostureStand:
		return "stand"
	case PostureSwim:
		return "swim"
	default:
		return fmt.Sprintf("Posture(%d)", uint8(p))
	}
}

// Capability is what one body can do and what each thing costs it.
//
// A mob is this value with CanSwim false; it gets a ground navigator out of
// the same search. Every duration is in ticks and every one is supplied by the
// caller, because 1.8.9 and 26.1.2 disagree about all of them.
type Capability struct {
	// Body is the box the search routes.
	Body terrain.Body
	// SafeFall is how far the body drops without harm, in blocks.
	SafeFall float64
	// CanSwim allows swim edges.
	CanSwim bool
	// WalkTicks is the cost of crossing one block on the level.
	WalkTicks float64
	// StepTicks is the cost of rising one block.
	StepTicks float64
	// FallTicks is the cost of descending one block.
	FallTicks float64
	// SwimTicks is the cost of crossing one block in fluid.
	SwimTicks float64
	// CandidateLimit bounds one terrain query's collision sweep. A
	// non-positive value means no limit.
	CandidateLimit int
}

// cheapest returns the lowest cost of any edge this capability may take. The
// heuristic scales distance by it, and a heuristic that assumed a higher floor
// would overestimate and stop returning shortest paths.
func (c Capability) cheapest() float64 {
	lowest := c.WalkTicks
	for _, cost := range []float64{c.StepTicks, c.FallTicks} {
		if cost < lowest {
			lowest = cost
		}
	}
	if c.CanSwim && c.SwimTicks < lowest {
		lowest = c.SwimTicks
	}

	return lowest
}

// query returns the terrain query this capability asks with.
func (c Capability) query(view terrainView, facts terrain.Facts) terrain.Query {
	return terrain.Query{View: view, Facts: facts, Body: c.Body, Limit: c.CandidateLimit}
}
```

- [x] **Step 4: Write the edge vocabulary**

Create `navigation/edge.go`:

```go
package navigation

import (
	"fmt"

	"github.com/go-theft-craft/minecraft-simulation/geom"
	"github.com/go-theft-craft/minecraft-simulation/world"
)

// terrainView is the view a search reads. It is world.View under another name
// so that this package's signatures say what they need rather than repeating
// the composite everywhere.
type terrainView = world.View

// EdgeKind names one way of getting from one cell to the next.
//
// The design also specifies JumpGap, Dig, Place, Support, and Collapse. They
// are absent rather than stubbed: each needs work that has not landed, and a
// kind that exists but is never produced is a kind a caller will switch on and
// be wrong about.
type EdgeKind uint8

const (
	// EdgeWalk crosses to an adjacent cell on the level.
	EdgeWalk EdgeKind = iota
	// EdgeStep rises one block into an adjacent cell.
	EdgeStep
	// EdgeFall descends into an adjacent column within the body's safe fall.
	EdgeFall
	// EdgeSwim crosses to an adjacent cell through fluid.
	EdgeSwim
)

// String returns the kind's name.
func (e EdgeKind) String() string {
	switch e {
	case EdgeWalk:
		return "walk"
	case EdgeStep:
		return "step"
	case EdgeFall:
		return "fall"
	case EdgeSwim:
		return "swim"
	default:
		return fmt.Sprintf("EdgeKind(%d)", uint8(e))
	}
}

// Edge is one move.
type Edge struct {
	// Kind names the move.
	Kind EdgeKind
	// From is the cell the body leaves.
	From geom.BlockPos
	// To is the cell the body arrives in.
	To geom.BlockPos
	// Posture is how the body occupies To on arrival.
	Posture Posture
	// Cost is the move's price in ticks.
	Cost float64
}

// Reason says why a search stopped.
type Reason uint8

const (
	// ReasonFound means the goal was reached.
	ReasonFound Reason = iota
	// ReasonBudget means the node budget ran out.
	ReasonBudget
	// ReasonCeiling means every remaining route costs more than the ceiling.
	ReasonCeiling
	// ReasonUnreachable means the frontier emptied without reaching the goal.
	ReasonUnreachable
)

// String returns the reason's name.
func (r Reason) String() string {
	switch r {
	case ReasonFound:
		return "found"
	case ReasonBudget:
		return "budget"
	case ReasonCeiling:
		return "ceiling"
	case ReasonUnreachable:
		return "unreachable"
	default:
		return fmt.Sprintf("Reason(%d)", uint8(r))
	}
}

// Path is a route, complete or not.
//
// An incomplete path is returned rather than an error because a body that
// travels most of the way and searches again beats one that refuses to move.
// Complete says which it is holding; Reason says why.
type Path struct {
	// Edges are the moves in order. It is empty when the body is already at
	// the goal and when nothing was reachable.
	Edges []Edge
	// Cost is the sum of the edge costs, in ticks.
	Cost float64
	// Complete reports that Edges reach the goal.
	Complete bool
	// Reason says why the search stopped.
	Reason Reason
}

// End returns the cell the path arrives at, and the cell the body started in
// when the path is empty.
func (p Path) End(start geom.BlockPos) geom.BlockPos {
	if len(p.Edges) == 0 {
		return start
	}

	return p.Edges[len(p.Edges)-1].To
}
```

- [x] **Step 5: Run the tests to verify they pass**

Run: `cd <minecraft-simulation> && devbox run -- go test ./navigation/ -v`
Expected: PASS, three tests.

- [x] **Step 6: Lint and commit**

```bash
cd <minecraft-simulation>
devbox run -- task lint
git add navigation/
git commit -m "feat(navigation): declare the edge, path, and capability vocabulary"
```

---

## Task 6: the deterministic frontier

**Files:**
- Create: `navigation/frontier.go`
- Test: `navigation/frontier_test.go`

**Interfaces:**
- Consumes: `navigation.Posture` from Task 5, `geom.BlockPos`
- Produces: `navigation.node{Pos geom.BlockPos, Posture Posture}`, `navigation.nodeLess(a, b node) bool`, `navigation.frontier` with `push(node, priority float64)`, `pop() (node, bool)`, `len() int`

The frontier is its own task because its contract is a property — equal priorities pop in one fixed order — and that property is what the whole determinism gate rests on. A reviewer can accept or reject it without reading the search.

- [x] **Step 1: Write the failing test**

Create `navigation/frontier_test.go`:

```go
package navigation

import (
	"testing"

	"github.com/go-theft-craft/minecraft-simulation/geom"
)

func at(x, y, z int32) node {
	return node{Pos: geom.BlockPos{X: x, Y: y, Z: z}, Posture: PostureStand}
}

func TestFrontierPopsLowestPriorityFirst(t *testing.T) {
	var f frontier
	f.push(at(1, 0, 0), 9)
	f.push(at(2, 0, 0), 1)
	f.push(at(3, 0, 0), 5)

	want := []node{at(2, 0, 0), at(3, 0, 0), at(1, 0, 0)}
	for i, expected := range want {
		got, ok := f.pop()
		if !ok {
			t.Fatalf("pop %d: frontier empty", i)
		}
		if got != expected {
			t.Fatalf("pop %d = %v, want %v", i, got, expected)
		}
	}
	if _, ok := f.pop(); ok {
		t.Fatal("pop from an empty frontier reported ok")
	}
}

// The property the determinism gate rests on: equal priorities pop in the node
// order, never in insertion order and never in a heap's incidental order.
func TestFrontierBreaksEqualPrioritiesOnNodeOrder(t *testing.T) {
	insertions := [][]node{
		{at(2, 0, 0), at(1, 0, 0), at(1, 0, 1), at(1, 1, 0)},
		{at(1, 1, 0), at(1, 0, 1), at(1, 0, 0), at(2, 0, 0)},
		{at(1, 0, 1), at(2, 0, 0), at(1, 1, 0), at(1, 0, 0)},
	}
	want := []node{at(1, 0, 0), at(1, 0, 1), at(1, 1, 0), at(2, 0, 0)}

	for _, order := range insertions {
		var f frontier
		for _, n := range order {
			f.push(n, 1)
		}
		for i, expected := range want {
			got, ok := f.pop()
			if !ok {
				t.Fatalf("pop %d: frontier empty", i)
			}
			if got != expected {
				t.Fatalf("insertion %v: pop %d = %v, want %v", order, i, got, expected)
			}
		}
	}
}

func TestNodeLessOrdersByPositionThenPosture(t *testing.T) {
	stand := node{Pos: geom.BlockPos{X: 1, Y: 1, Z: 1}, Posture: PostureStand}
	swim := node{Pos: geom.BlockPos{X: 1, Y: 1, Z: 1}, Posture: PostureSwim}

	if !nodeLess(stand, swim) {
		t.Fatal("nodeLess did not order PostureStand before PostureSwim")
	}
	if nodeLess(swim, stand) {
		t.Fatal("nodeLess is not antisymmetric on posture")
	}
	if nodeLess(stand, stand) {
		t.Fatal("nodeLess reported a node less than itself")
	}
}
```

- [x] **Step 2: Run the tests to verify they fail**

Run: `cd <minecraft-simulation> && devbox run -- go test ./navigation/ -run "TestFrontier|TestNodeLess" -v`
Expected: FAIL — `undefined: node`.

- [x] **Step 3: Implement the frontier**

Create `navigation/frontier.go`:

```go
package navigation

import (
	"container/heap"

	"github.com/go-theft-craft/minecraft-simulation/geom"
)

// node is one search state: where the body is and how it is occupying that
// place. Mutations are not part of it. A version that adds digging records
// them on edges, because keying a node by the set of blocks removed to reach
// it makes the state space explode.
type node struct {
	Pos     geom.BlockPos
	Posture Posture
}

// nodeLess is a total order over nodes.
//
// It exists so that two equal priorities resolve the same way on every run and
// every platform. Without it the frontier's order depends on the heap's
// incidental arrangement, two identical searches return different paths, and
// the digest comparison the replay gate performs fails for a reason nothing in
// the recording explains.
func nodeLess(a, b node) bool {
	if a.Pos.X != b.Pos.X {
		return a.Pos.X < b.Pos.X
	}
	if a.Pos.Y != b.Pos.Y {
		return a.Pos.Y < b.Pos.Y
	}
	if a.Pos.Z != b.Pos.Z {
		return a.Pos.Z < b.Pos.Z
	}

	return a.Posture < b.Posture
}

// entry is one queued node and its priority.
type entry struct {
	node     node
	priority float64
}

// queue is the heap.Interface implementation. It is separate from frontier so
// that the exported-looking heap methods are not part of frontier's surface.
type queue []entry

func (q queue) Len() int { return len(q) }

func (q queue) Less(i, j int) bool {
	if q[i].priority != q[j].priority {
		return q[i].priority < q[j].priority
	}

	return nodeLess(q[i].node, q[j].node)
}

func (q queue) Swap(i, j int) { q[i], q[j] = q[j], q[i] }

func (q *queue) Push(value any) { *q = append(*q, value.(entry)) }

func (q *queue) Pop() any {
	old := *q
	last := old[len(old)-1]
	*q = old[:len(old)-1]

	return last
}

// frontier is the search's open set: lowest priority first, ties broken on the
// node order.
type frontier struct {
	queue queue
}

// push queues a node.
func (f *frontier) push(n node, priority float64) {
	heap.Push(&f.queue, entry{node: n, priority: priority})
}

// pop removes and returns the next node, reporting false when empty.
func (f *frontier) pop() (node, bool) {
	if f.queue.Len() == 0 {
		return node{}, false
	}

	return heap.Pop(&f.queue).(entry).node, true
}

// len returns how many nodes are queued.
func (f *frontier) len() int { return f.queue.Len() }
```

- [x] **Step 4: Run the tests to verify they pass**

Run: `cd <minecraft-simulation> && devbox run -- go test ./navigation/ -v`
Expected: PASS, six tests.

- [x] **Step 5: Lint and commit**

```bash
cd <minecraft-simulation>
devbox run -- task lint
git add navigation/
git commit -m "feat(navigation): add a frontier whose ties break on a total node order"
```

---

## Task 7: the search over walk, step, and fall

**Files:**
- Create: `navigation/search.go`
- Test: `navigation/search_test.go`

**Interfaces:**
- Consumes: everything from Tasks 1 through 6
- Produces: `navigation.Budget{Nodes int, Ceiling float64}`, `navigation.Find(ctx context.Context, view world.View, facts terrain.Facts, capability Capability, from, goal geom.BlockPos, budget Budget) (Path, error)`

- [x] **Step 1: Write the failing test**

Create `navigation/search_test.go`:

```go
package navigation

import (
	"context"
	"testing"

	"github.com/go-theft-craft/minecraft-simulation/geom"
	"github.com/go-theft-craft/minecraft-simulation/terrain"
	"github.com/go-theft-craft/minecraft-simulation/world"
)

// walker is a two-block body whose costs are distinct primes, so a wrong edge
// kind shows up as a wrong total rather than coinciding with the right one.
var walker = Capability{
	Body:      terrain.Body{HalfWidth: 0.3, Height: 1.8, StepHeight: 0.6},
	SafeFall:  3,
	WalkTicks: 5,
	StepTicks: 7,
	FallTicks: 3,
	SwimTicks: 11,
}

// flat returns a floor at y=-1 spanning the inclusive range, with air above it.
func flat(minX, minZ, maxX, maxZ int32) *world.Blocks {
	blocks := world.NewBlocks()
	blocks.Fill(
		geom.BlockPos{X: minX, Y: -1, Z: minZ},
		geom.BlockPos{X: maxX, Y: -1, Z: maxZ},
		geom.FullCube(),
	)
	blocks.Fill(
		geom.BlockPos{X: minX, Y: 0, Z: minZ},
		geom.BlockPos{X: maxX, Y: 3, Z: maxZ},
		geom.EmptyShape(),
	)

	return blocks
}

var wideBudget = Budget{Nodes: 10_000, Ceiling: 10_000}

func TestFindWalksAStraightLine(t *testing.T) {
	path, err := Find(
		context.Background(), flat(-1, -1, 5, 1), nil, walker,
		geom.BlockPos{X: 0, Y: 0, Z: 0}, geom.BlockPos{X: 4, Y: 0, Z: 0}, wideBudget,
	)
	if err != nil {
		t.Fatalf("Find returned an error: %v", err)
	}
	if !path.Complete {
		t.Fatalf("path incomplete, reason %v", path.Reason)
	}
	if len(path.Edges) != 4 {
		t.Fatalf("len(Edges) = %d, want 4", len(path.Edges))
	}
	if path.Cost != 20 {
		t.Fatalf("Cost = %v, want 20", path.Cost)
	}
	for i, edge := range path.Edges {
		if edge.Kind != EdgeWalk {
			t.Fatalf("edge %d kind = %v, want EdgeWalk", i, edge.Kind)
		}
	}
}

func TestFindReturnsAnEmptyCompletePathAtTheGoal(t *testing.T) {
	here := geom.BlockPos{X: 0, Y: 0, Z: 0}

	path, err := Find(context.Background(), flat(-1, -1, 1, 1), nil, walker, here, here, wideBudget)
	if err != nil {
		t.Fatalf("Find returned an error: %v", err)
	}
	if !path.Complete || len(path.Edges) != 0 || path.Cost != 0 {
		t.Fatalf("path = %+v, want an empty complete path", path)
	}
}

func TestFindStepsOverAOneBlockRise(t *testing.T) {
	blocks := flat(-1, -1, 4, 1)
	blocks.Set(geom.BlockPos{X: 2, Y: 0, Z: 0}, geom.FullCube())
	blocks.Set(geom.BlockPos{X: 2, Y: -1, Z: 0}, geom.FullCube())

	path, err := Find(
		context.Background(), blocks, nil, walker,
		geom.BlockPos{X: 0, Y: 0, Z: 0}, geom.BlockPos{X: 2, Y: 1, Z: 0}, wideBudget,
	)
	if err != nil {
		t.Fatalf("Find returned an error: %v", err)
	}
	if !path.Complete {
		t.Fatalf("path incomplete, reason %v", path.Reason)
	}
	last := path.Edges[len(path.Edges)-1]
	if last.Kind != EdgeStep {
		t.Fatalf("last edge kind = %v, want EdgeStep", last.Kind)
	}
	if last.To != (geom.BlockPos{X: 2, Y: 1, Z: 0}) {
		t.Fatalf("last edge To = %v, want {2 1 0}", last.To)
	}
}

func TestFindFallsWithinTheSafeFall(t *testing.T) {
	blocks := world.NewBlocks()
	blocks.Fill(geom.BlockPos{X: -1, Y: -1, Z: -1}, geom.BlockPos{X: 0, Y: -1, Z: 1}, geom.FullCube())
	blocks.Fill(geom.BlockPos{X: -1, Y: 0, Z: -1}, geom.BlockPos{X: 3, Y: 3, Z: 1}, geom.EmptyShape())
	blocks.Fill(geom.BlockPos{X: 1, Y: -3, Z: -1}, geom.BlockPos{X: 3, Y: -3, Z: 1}, geom.FullCube())
	blocks.Fill(geom.BlockPos{X: 1, Y: -2, Z: -1}, geom.BlockPos{X: 3, Y: -1, Z: 1}, geom.EmptyShape())

	path, err := Find(
		context.Background(), blocks, nil, walker,
		geom.BlockPos{X: 0, Y: 0, Z: 0}, geom.BlockPos{X: 1, Y: -2, Z: 0}, wideBudget,
	)
	if err != nil {
		t.Fatalf("Find returned an error: %v", err)
	}
	if !path.Complete {
		t.Fatalf("path incomplete, reason %v", path.Reason)
	}
	if path.Edges[0].Kind != EdgeFall {
		t.Fatalf("first edge kind = %v, want EdgeFall", path.Edges[0].Kind)
	}
}

func TestFindRefusesAFallBeyondTheSafeFall(t *testing.T) {
	blocks := world.NewBlocks()
	blocks.Fill(geom.BlockPos{X: -1, Y: -1, Z: -1}, geom.BlockPos{X: 0, Y: -1, Z: 1}, geom.FullCube())
	blocks.Fill(geom.BlockPos{X: -1, Y: 0, Z: -1}, geom.BlockPos{X: 3, Y: 3, Z: 1}, geom.EmptyShape())
	blocks.Fill(geom.BlockPos{X: 1, Y: -9, Z: -1}, geom.BlockPos{X: 3, Y: -9, Z: 1}, geom.FullCube())
	blocks.Fill(geom.BlockPos{X: 1, Y: -8, Z: -1}, geom.BlockPos{X: 3, Y: -1, Z: 1}, geom.EmptyShape())

	path, err := Find(
		context.Background(), blocks, nil, walker,
		geom.BlockPos{X: 0, Y: 0, Z: 0}, geom.BlockPos{X: 1, Y: -8, Z: 0}, wideBudget,
	)
	if err != nil {
		t.Fatalf("Find returned an error: %v", err)
	}
	if path.Complete {
		t.Fatal("Find crossed a fall beyond the capability's safe fall")
	}
}

// An unloaded cell is refused, never guessed. This is the property that keeps
// a bot from walking into a wall it could not see.
func TestFindRefusesToRouteThroughUnknownCells(t *testing.T) {
	blocks := flat(-1, -1, 5, 1)
	blocks.Forget(geom.BlockPos{X: 2, Y: 0, Z: 0})
	blocks.Forget(geom.BlockPos{X: 2, Y: 0, Z: 1})
	blocks.Forget(geom.BlockPos{X: 2, Y: 0, Z: -1})

	path, err := Find(
		context.Background(), blocks, nil, walker,
		geom.BlockPos{X: 0, Y: 0, Z: 0}, geom.BlockPos{X: 4, Y: 0, Z: 0}, wideBudget,
	)
	if err != nil {
		t.Fatalf("Find returned an error: %v", err)
	}
	if path.Complete {
		t.Fatal("Find routed through an undescribed region")
	}
	if path.Reason != ReasonUnreachable {
		t.Fatalf("Reason = %v, want ReasonUnreachable", path.Reason)
	}
}

// A bounded search reports the best it found rather than nothing, so a body
// that cannot see the whole route still makes progress.
func TestFindReturnsAPartialPathWhenTheBudgetRunsOut(t *testing.T) {
	path, err := Find(
		context.Background(), flat(-1, -1, 40, 1), nil, walker,
		geom.BlockPos{X: 0, Y: 0, Z: 0}, geom.BlockPos{X: 39, Y: 0, Z: 0},
		Budget{Nodes: 12, Ceiling: 10_000},
	)
	if err != nil {
		t.Fatalf("Find returned an error: %v", err)
	}
	if path.Complete {
		t.Fatal("Find completed a route its budget could not cover")
	}
	if path.Reason != ReasonBudget {
		t.Fatalf("Reason = %v, want ReasonBudget", path.Reason)
	}
	if len(path.Edges) == 0 {
		t.Fatal("a partial path with no edges makes no progress")
	}
	if path.Edges[0].From != (geom.BlockPos{X: 0, Y: 0, Z: 0}) {
		t.Fatalf("partial path starts at %v, want the origin", path.Edges[0].From)
	}
}

func TestFindHonoursACancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := Find(
		ctx, flat(-1, -1, 40, 1), nil, walker,
		geom.BlockPos{X: 0, Y: 0, Z: 0}, geom.BlockPos{X: 39, Y: 0, Z: 0}, wideBudget,
	)
	if err == nil {
		t.Fatal("Find ignored a cancelled context")
	}
}
```

- [x] **Step 2: Run the tests to verify they fail**

Run: `cd <minecraft-simulation> && devbox run -- go test ./navigation/ -run TestFind -v`
Expected: FAIL — `undefined: Find`.

- [x] **Step 3: Implement the search**

Create `navigation/search.go`:

```go
package navigation

import (
	"context"
	"errors"
	"math"

	"github.com/go-theft-craft/minecraft-simulation/geom"
	"github.com/go-theft-craft/minecraft-simulation/terrain"
)

// ErrNoBody reports a capability whose body has no volume. A zero body fits
// everywhere, which would return routes through solid stone.
var ErrNoBody = errors.New("navigation: a capability needs a body with a width and a height")

// Budget bounds one search.
//
// Both bounds exist because they stop different runaways: Nodes stops a search
// over a large open world, and Ceiling stops one that would return a route far
// more expensive than the caller would ever walk.
type Budget struct {
	// Nodes is how many nodes may be expanded. A non-positive value means the
	// search expands until the frontier empties.
	Nodes int
	// Ceiling is the highest total cost a route may have, in ticks. A
	// non-positive value means no ceiling.
	Ceiling float64
}

// steps are the four horizontal neighbours, in a fixed order. Diagonals are
// absent: they need a corner-cutting rule, and a wrong one walks a body
// through the gap between two blocks.
var steps = [4]geom.BlockPos{
	{X: 1}, {X: -1}, {Z: 1}, {Z: -1},
}

// maxFallSearch is how far below a neighbour the search looks for a landing.
// It bounds the column walk; a fall further than the capability allows is
// refused anyway.
const maxFallSearch = 32

// Find searches a route from one cell to another.
//
// It returns a Path rather than an error when it cannot reach the goal: an
// incomplete path with Reason set is more useful to a moving body than a
// refusal. An error means the search could not run at all.
func Find(
	ctx context.Context,
	view terrainView,
	facts terrain.Facts,
	capability Capability,
	from, goal geom.BlockPos,
	budget Budget,
) (Path, error) {
	if capability.Body.HalfWidth <= 0 || capability.Body.Height <= 0 {
		return Path{}, ErrNoBody
	}

	query := capability.query(view, facts)
	start := node{Pos: from, Posture: PostureStand}

	cameFrom := make(map[node]link)
	cost := map[node]float64{start: 0}

	var open frontier
	open.push(start, capability.heuristic(from, goal))

	best, bestScore := start, capability.heuristic(from, goal)
	reason := ReasonUnreachable
	expanded := 0

	for {
		if err := ctx.Err(); err != nil {
			return Path{}, err
		}

		current, ok := open.pop()
		if !ok {
			break
		}
		if current.Pos == goal {
			best, reason = current, ReasonFound

			break
		}
		if budget.Nodes > 0 && expanded >= budget.Nodes {
			reason = ReasonBudget

			break
		}
		expanded++

		// The closest node seen is the fallback a partial path is built from,
		// so an exhausted search still returns progress toward the goal.
		if score := capability.heuristic(current.Pos, goal); score < bestScore {
			best, bestScore = current, score
		}

		moves, err := capability.expand(query, current)
		if err != nil {
			return Path{}, err
		}

		for _, move := range moves {
			next := node{Pos: move.To, Posture: move.Posture}
			through := cost[current] + move.Cost
			if budget.Ceiling > 0 && through > budget.Ceiling {
				if reason == ReasonUnreachable {
					reason = ReasonCeiling
				}

				continue
			}
			if seen, ok := cost[next]; ok && seen <= through {
				continue
			}
			cost[next] = through
			cameFrom[next] = link{edge: move, parent: current}
			open.push(next, through+capability.heuristic(next.Pos, goal))
		}
	}

	return assemble(cameFrom, cost, start, best, reason), nil
}

// link is how a node was reached: the edge that arrived, and the node it came
// from.
//
// The parent is stored rather than recovered from the edge, because an edge
// names cells and a node is a cell plus a posture. Two nodes share a cell when
// a body can both stand in it and swim it, and a trace-back that guessed which
// one an edge left would return a path that is right about where it goes and
// wrong about how.
type link struct {
	edge   Edge
	parent node
}

// assemble walks the parent links back from the end node and returns the path
// in travel order.
func assemble(cameFrom map[node]link, cost map[node]float64, start, end node, reason Reason) Path {
	var reversed []Edge
	for current := end; current != start; {
		step, ok := cameFrom[current]
		if !ok {
			break
		}
		reversed = append(reversed, step.edge)
		current = step.parent
	}

	edges := make([]Edge, 0, len(reversed))
	for i := len(reversed) - 1; i >= 0; i-- {
		edges = append(edges, reversed[i])
	}

	return Path{
		Edges:    edges,
		Cost:     cost[end],
		Complete: reason == ReasonFound,
		Reason:   reason,
	}
}

// heuristic estimates the remaining cost. It is Manhattan distance scaled by
// the cheapest edge available, which never overestimates and so keeps the
// search returning shortest paths.
func (c Capability) heuristic(from, goal geom.BlockPos) float64 {
	distance := math.Abs(float64(goal.X-from.X)) +
		math.Abs(float64(goal.Y-from.Y)) +
		math.Abs(float64(goal.Z-from.Z))

	return distance * c.cheapest()
}

// expand returns every edge leaving a node, in the fixed neighbour order.
func (c Capability) expand(query terrain.Query, from node) ([]Edge, error) {
	edges := make([]Edge, 0, len(steps))

	for _, step := range steps {
		neighbour := geom.BlockPos{X: from.Pos.X + step.X, Y: from.Pos.Y, Z: from.Pos.Z + step.Z}

		passable, err := query.Passable(neighbour)
		if err != nil {
			return nil, err
		}

		switch passable {
		case terrain.Clear:
			edges = append(edges, Edge{
				Kind: EdgeWalk, From: from.Pos, To: neighbour,
				Posture: PostureStand, Cost: c.WalkTicks,
			})
		case terrain.Steppable:
			above := geom.BlockPos{X: neighbour.X, Y: neighbour.Y + 1, Z: neighbour.Z}
			edges = append(edges, Edge{
				Kind: EdgeStep, From: from.Pos, To: above,
				Posture: PostureStand, Cost: c.StepTicks,
			})
		case terrain.Blocked:
			fall, ok, err := c.fall(query, from.Pos, neighbour)
			if err != nil {
				return nil, err
			}
			if ok {
				edges = append(edges, fall)
			}
		}
	}

	return edges, nil
}

// fall looks down a neighbouring column for a landing within the safe fall.
//
// It runs only where Passable said Blocked, which is the answer a hole gives:
// nothing holds the body up there. A wall gives the same answer and finds no
// landing, which is why the column walk stops at the first cell the body does
// not fit through.
func (c Capability) fall(query terrain.Query, from, neighbour geom.BlockPos) (Edge, bool, error) {
	for drop := int32(1); drop <= maxFallSearch; drop++ {
		if float64(drop) > c.SafeFall {
			return Edge{}, false, nil
		}

		landing := geom.BlockPos{X: neighbour.X, Y: neighbour.Y - drop, Z: neighbour.Z}

		passable, err := query.Passable(landing)
		if err != nil {
			return Edge{}, false, err
		}
		switch passable {
		case terrain.Clear:
			return Edge{
				Kind: EdgeFall, From: from, To: landing,
				Posture: PostureStand, Cost: c.FallTicks * float64(drop),
			}, true, nil
		case terrain.Unknown, terrain.Steppable:
			return Edge{}, false, nil
		}
	}

	return Edge{}, false, nil
}
```

- [x] **Step 4: Run the tests to verify they pass**

Run: `cd <minecraft-simulation> && devbox run -- go test ./navigation/ -v`
Expected: PASS, fourteen tests.

- [x] **Step 5: Run the whole suite under the race detector**

Run: `cd <minecraft-simulation> && devbox run -- task test`
Expected: PASS, every package.

- [x] **Step 6: Lint and commit**

```bash
cd <minecraft-simulation>
devbox run -- task lint
git add navigation/
git commit -m "feat(navigation): search walk, step, and fall routes with a bounded A*"
```

---

## Task 8: swim edges

**Files:**
- Modify: `navigation/search.go`
- Test: `navigation/search_test.go`

**Interfaces:**
- Consumes: `terrain.Query.FluidAt`, `terrain.FluidWater`, `navigation.PostureSwim`, `Capability.CanSwim`, `Capability.SwimTicks`
- Produces: `EdgeSwim` edges from `Capability.expand`

Water carries no collision shape, so `Passable` already reports a flooded cell as `Clear` or `Blocked` on geometry alone. The swim edge is what distinguishes a body that may enter it from one that may not, and it is why `Facts` exists.

- [x] **Step 1: Write the failing test**

Append to `navigation/search_test.go`:

```go
const refWater world.BlockRef = 9

// waterFacts answers water for one handle and nothing for the rest.
type waterFacts struct{}

func (waterFacts) Hazard(world.BlockRef) terrain.Hazard { return terrain.HazardNone }

func (waterFacts) Fluid(ref world.BlockRef) terrain.Fluid {
	if ref == refWater {
		return terrain.FluidWater
	}

	return terrain.FluidNone
}

// pool returns a flat world whose x=2 column is water to head height.
func pool() *world.Blocks {
	blocks := flat(-1, -1, 5, 1)
	for y := int32(0); y <= 1; y++ {
		blocks.SetBlock(geom.BlockPos{X: 2, Y: y, Z: 0}, refWater, geom.EmptyShape())
	}

	return blocks
}

func TestFindCrossesWaterWhenTheBodyCanSwim(t *testing.T) {
	swimmer := walker
	swimmer.CanSwim = true

	path, err := Find(
		context.Background(), pool(), waterFacts{}, swimmer,
		geom.BlockPos{X: 0, Y: 0, Z: 0}, geom.BlockPos{X: 4, Y: 0, Z: 0}, wideBudget,
	)
	if err != nil {
		t.Fatalf("Find returned an error: %v", err)
	}
	if !path.Complete {
		t.Fatalf("path incomplete, reason %v", path.Reason)
	}

	var swam bool
	for _, edge := range path.Edges {
		if edge.Kind == EdgeSwim {
			swam = true
			if edge.Posture != PostureSwim {
				t.Fatalf("swim edge posture = %v, want PostureSwim", edge.Posture)
			}
		}
	}
	if !swam {
		t.Fatal("a route through water contains no swim edge")
	}
}

// The same world, the same goal, a body that cannot swim: it must route around
// rather than through. This is the mob case the design promises.
func TestFindRoutesAroundWaterWhenTheBodyCannotSwim(t *testing.T) {
	path, err := Find(
		context.Background(), pool(), waterFacts{}, walker,
		geom.BlockPos{X: 0, Y: 0, Z: 0}, geom.BlockPos{X: 4, Y: 0, Z: 0}, wideBudget,
	)
	if err != nil {
		t.Fatalf("Find returned an error: %v", err)
	}
	if !path.Complete {
		t.Fatalf("path incomplete, reason %v", path.Reason)
	}
	for _, edge := range path.Edges {
		if edge.Kind == EdgeSwim {
			t.Fatal("a body that cannot swim took a swim edge")
		}
		if edge.To == (geom.BlockPos{X: 2, Y: 0, Z: 0}) {
			t.Fatal("a body that cannot swim entered the water")
		}
	}
}

const refFire world.BlockRef = 51

// burningFacts answers HazardBurn for one handle. Fire carries no collision
// shape and no fluid, so nothing but the hazard lookup can find it.
type burningFacts struct{}

func (burningFacts) Fluid(world.BlockRef) terrain.Fluid { return terrain.FluidNone }

func (burningFacts) Hazard(ref world.BlockRef) terrain.Hazard {
	if ref == refFire {
		return terrain.HazardBurn
	}

	return terrain.HazardNone
}

func TestFindRoutesAroundFire(t *testing.T) {
	blocks := flat(-1, -1, 5, 1)
	blocks.SetBlock(geom.BlockPos{X: 2, Y: 0, Z: 0}, refFire, geom.EmptyShape())

	path, err := Find(
		context.Background(), blocks, burningFacts{}, walker,
		geom.BlockPos{X: 0, Y: 0, Z: 0}, geom.BlockPos{X: 4, Y: 0, Z: 0}, wideBudget,
	)
	if err != nil {
		t.Fatalf("Find returned an error: %v", err)
	}
	if !path.Complete {
		t.Fatalf("path incomplete, reason %v", path.Reason)
	}
	for _, edge := range path.Edges {
		if edge.To == (geom.BlockPos{X: 2, Y: 0, Z: 0}) {
			t.Fatal("the route walks through fire")
		}
	}
}
```

The test file's import block must now read:

```go
import (
	"context"
	"testing"

	"github.com/go-theft-craft/minecraft-simulation/geom"
	"github.com/go-theft-craft/minecraft-simulation/terrain"
	"github.com/go-theft-craft/minecraft-simulation/world"
)
```

- [x] **Step 2: Run the tests to verify they fail**

Run: `cd <minecraft-simulation> && devbox run -- go test ./navigation/ -run "Water|Swim|Fire" -v`
Expected: FAIL — the swimmer's route contains no `EdgeSwim`, the walker enters the water, and the route walks through fire.

- [x] **Step 3: Implement the swim edge**

In `navigation/search.go`, replace the `case terrain.Clear:` arm of `expand` with a call to a new helper, and add the helper:

```go
		case terrain.Clear:
			edge, ok, err := c.enter(query, from.Pos, neighbour)
			if err != nil {
				return nil, err
			}
			if ok {
				edges = append(edges, edge)
			}
```

Add to `navigation/search.go`:

```go
// enter decides how a body crosses into a cell it geometrically fits in.
//
// Neither a fluid nor a fire carries a collision shape, so Passable calls both
// Clear on geometry alone. Asking Facts here is what stops a body that cannot
// swim from strolling through a lake, and what stops any body from strolling
// through fire or lava.
func (c Capability) enter(query terrain.Query, from, to geom.BlockPos) (Edge, bool, error) {
	hazard, lookup, err := query.HazardAt(to)
	if err != nil {
		return Edge{}, false, err
	}
	if lookup == world.LookupUnknown {
		return Edge{}, false, nil
	}
	if hazard != terrain.HazardNone {
		return Edge{}, false, nil
	}

	fluid, lookup, err := query.FluidAt(to)
	if err != nil {
		return Edge{}, false, err
	}
	if lookup == world.LookupUnknown {
		return Edge{}, false, nil
	}

	switch fluid {
	case terrain.FluidNone:
		return Edge{
			Kind: EdgeWalk, From: from, To: to,
			Posture: PostureStand, Cost: c.WalkTicks,
		}, true, nil
	case terrain.FluidWater:
		if !c.CanSwim {
			return Edge{}, false, nil
		}

		return Edge{
			Kind: EdgeSwim, From: from, To: to,
			Posture: PostureSwim, Cost: c.SwimTicks,
		}, true, nil
	}

	// Lava, and any fluid a later version adds. Refused rather than costed:
	// the design leaves fluid traversal beyond water to its own work, and a
	// body that swam through lava because nothing said not to is worse than
	// one that took the long way.
	return Edge{}, false, nil
}
```

Add `"github.com/go-theft-craft/minecraft-simulation/world"` to the imports of `navigation/search.go`.

- [x] **Step 4: Run the tests to verify they pass**

Run: `cd <minecraft-simulation> && devbox run -- go test ./navigation/ -v`
Expected: PASS, seventeen tests.

- [x] **Step 5: Lint and commit**

```bash
cd <minecraft-simulation>
devbox run -- task lint
git add navigation/
git commit -m "feat(navigation): cross water only when the body can swim, and never fire"
```

---

## Task 9: properties and the determinism gate

**Files:**
- Create: `navigation/property_test.go`

**Interfaces:**
- Consumes: `navigation.Find`, `navigation.Path`, `navigation.Capability`, `terrain` and `world` fakes from earlier tasks
- Produces: no new exported surface. This task's deliverable is the gate.

The single-path tests in Task 7 check one route each. These check the invariants every route must hold, which is what catches a search that returns a plausible path with a hole in it.

- [x] **Step 1: Write the property tests**

Create `navigation/property_test.go`:

```go
package navigation

import (
	"context"
	"math/rand/v2"
	"testing"

	"github.com/go-theft-craft/minecraft-simulation/geom"
	"github.com/go-theft-craft/minecraft-simulation/terrain"
	"github.com/go-theft-craft/minecraft-simulation/world"
)

// seeds are fixed so a failure reproduces exactly. Add to this list rather
// than randomizing it, matching collision/property_test.go.
var seeds = []uint64{1, 2, 3, 5, 8, 13, 21, 34, 55, 89}

// terrainClear is spelled out because the property loops compare against it
// repeatedly and the qualified name buries the assertion.
const terrainClear = terrain.Clear

// maze returns a flat world with pillars knocked into it, deterministic for a
// seed. The start and goal cells are always left open.
func maze(seed uint64) *world.Blocks {
	blocks := flat(-1, -1, 12, 12)
	random := rand.New(rand.NewPCG(seed, 0))

	for x := int32(0); x <= 11; x++ {
		for z := int32(0); z <= 11; z++ {
			if x == 0 && z == 0 {
				continue
			}
			if x == 11 && z == 11 {
				continue
			}
			if random.Float64() < 0.25 {
				blocks.Set(geom.BlockPos{X: x, Y: 0, Z: z}, geom.FullCube())
				blocks.Set(geom.BlockPos{X: x, Y: 1, Z: z}, geom.FullCube())
			}
		}
	}

	return blocks
}

// TestPathsAreContiguous is the first exit property: every edge leaves where
// the previous one arrived. A search that lost a parent link returns a path
// that teleports, and a caller following it walks into a wall.
func TestPathsAreContiguous(t *testing.T) {
	for _, seed := range seeds {
		path := search(t, maze(seed))
		for i := 1; i < len(path.Edges); i++ {
			if path.Edges[i].From != path.Edges[i-1].To {
				t.Fatalf(
					"seed %d: edge %d leaves %v but edge %d arrived at %v",
					seed, i, path.Edges[i].From, i-1, path.Edges[i-1].To,
				)
			}
		}
	}
}

// TestPathsStartAtTheOrigin guards the partial-path case: a bounded search
// must still hand back something the caller can start walking.
func TestPathsStartAtTheOrigin(t *testing.T) {
	for _, seed := range seeds {
		path := search(t, maze(seed))
		if len(path.Edges) == 0 {
			continue
		}
		if path.Edges[0].From != (geom.BlockPos{X: 0, Y: 0, Z: 0}) {
			t.Fatalf("seed %d: path starts at %v, want the origin", seed, path.Edges[0].From)
		}
	}
}

// TestPathCostIsTheSumOfItsEdges guards against a cost that drifts from the
// route it describes, which would make one path compare wrongly against
// another.
func TestPathCostIsTheSumOfItsEdges(t *testing.T) {
	for _, seed := range seeds {
		path := search(t, maze(seed))
		if !path.Complete {
			continue
		}

		var total float64
		for _, edge := range path.Edges {
			total += edge.Cost
		}
		if total != path.Cost {
			t.Fatalf("seed %d: Cost = %v, edges sum to %v", seed, path.Cost, total)
		}
	}
}

// TestEveryEdgeLandsSomewhereStandable checks the property a caller actually
// depends on: each arrival is a cell the body can occupy.
func TestEveryEdgeLandsSomewhereStandable(t *testing.T) {
	for _, seed := range seeds {
		blocks := maze(seed)
		path := search(t, blocks)
		query := walker.query(blocks, nil)

		for i, edge := range path.Edges {
			passable, err := query.Passable(edge.To)
			if err != nil {
				t.Fatalf("seed %d: Passable returned an error: %v", seed, err)
			}
			if passable != terrainClear {
				t.Fatalf("seed %d: edge %d lands on a %v cell", seed, i, passable)
			}
		}
	}
}

// TestSearchesAreReproducible is the determinism gate. Go randomizes map
// iteration, so a search that let map order reach an output would return a
// different path on some runs and fail the replay comparison for a reason
// nothing in the recording explains.
func TestSearchesAreReproducible(t *testing.T) {
	for _, seed := range seeds {
		blocks := maze(seed)
		first := search(t, blocks)

		for run := 0; run < 100; run++ {
			again := search(t, blocks)
			if again.Cost != first.Cost || again.Reason != first.Reason {
				t.Fatalf("seed %d run %d: path summary changed", seed, run)
			}
			if len(again.Edges) != len(first.Edges) {
				t.Fatalf("seed %d run %d: edge count changed", seed, run)
			}
			for i := range first.Edges {
				if again.Edges[i] != first.Edges[i] {
					t.Fatalf("seed %d run %d: edge %d changed", seed, run, i)
				}
			}
		}
	}
}

// search runs one fixed query against a world.
func search(t *testing.T, blocks *world.Blocks) Path {
	t.Helper()

	path, err := Find(
		context.Background(), blocks, nil, walker,
		geom.BlockPos{X: 0, Y: 0, Z: 0}, geom.BlockPos{X: 11, Y: 0, Z: 11},
		Budget{Nodes: 5_000, Ceiling: 5_000},
	)
	if err != nil {
		t.Fatalf("Find returned an error: %v", err)
	}

	return path
}
```

- [x] **Step 2: Run the property tests**

Run: `cd <minecraft-simulation> && devbox run -- go test ./navigation/ -run "TestPath|TestEvery|TestSearchesAreReproducible" -v`
Expected: PASS, five tests. If `TestSearchesAreReproducible` fails, a map is reaching an output ordering — find it and sort the iteration or replace the map.

- [x] **Step 3: Run the whole suite under the race detector**

Run: `cd <minecraft-simulation> && devbox run -- task test`
Expected: PASS, every package.

- [x] **Step 4: Lint and commit**

```bash
cd <minecraft-simulation>
devbox run -- task lint
git add navigation/
git commit -m "test(navigation): assert path contiguity, cost, legality, and reproducibility"
```

---

## Task 10: documentation and the charter amendment

**Files:**
- Modify: `README.md` (package table and dependency chart)
- Modify: `CHANGELOG.md` (Unreleased, Added)
- Modify (in `headless-minecraft`): `docs/superpowers/specs/2026-08-13-minecraft-simulation-design.md:49`

The design's non-goal list currently excludes pathfinding. Leaving it while shipping a pathfinder makes the repository contradict itself, and a reader who found the contradiction would not know which side was current.

- [x] **Step 1: Add the packages to the README table**

In `minecraft-simulation/README.md`, add two rows to the package table, after `collision`:

```markdown
| `terrain` | Static predicates over a world view: fit, ground, passability, hazards, fluids |
| `navigation` | The edge vocabulary, a body's capability, and a bounded deterministic route search |
```

- [x] **Step 2: Extend the dependency chart**

In `minecraft-simulation/README.md`, replace the dependency chart with:

```text
geom  ->  world  ->  entity  ->  movement  ->  sim  ->  runtime  ->  adapter
   \          \         ^
    \          \-> collision
     \                \
      \-> terrain -----/  ->  navigation

profile/java/v1_8  ->  sim, movement, and one version's game data
mctest             ->  sim, runtime, movement
```

Below it, add:

```markdown
`terrain` and `navigation` import `geom`, `world`, and `collision` and nothing
else. Neither imports `sim`, so a version's numbers reach them as arguments the
same way they reach `movement` — a body's width and step height on a value, a
block's hazard through an oracle the profile supplies. That is what lets one
search serve a 1.8.9 mob and a 26.1.2 bot.
```

- [x] **Step 3: Amend the design's non-goals**

In `headless-minecraft/docs/superpowers/specs/2026-08-13-minecraft-simulation-design.md`, replace the non-goal line:

```markdown
- Goal selection, mob AI, fish AI, combat strategy, or automation scheduling
```

and add immediately below the non-goal list:

```markdown
Pathfinding was a non-goal until 2026-08-17, when the navigation design moved
it here. The route search is a deterministic function of terrain and a body's
capability, which is the same kind of rule as collision; what stays excluded is
deciding where to go.
```

- [x] **Step 4: Record the change in the changelog**

In `minecraft-simulation/CHANGELOG.md`, under `## Unreleased` / `### Added`:

```markdown
- `terrain`: static predicates over a world view — whether a body fits, whether
  anything holds it up, and whether a cell is clear, steppable, blocked, or
  undescribed — with the body as a value and every block fact the collision
  shape does not carry supplied through a profile oracle.
- `navigation`: a bounded route search over walk, step, fall, and swim edges,
  costed in ticks, whose frontier breaks ties on a total node order so two
  identical searches return identical paths.
```

- [x] **Step 5: Verify every gate**

Run: `cd <minecraft-simulation> && devbox run -- task verify`
Expected: lint, secrets, test, vuln, and build all pass.

- [x] **Step 6: Commit**

```bash
cd <minecraft-simulation>
git add README.md CHANGELOG.md
git commit -m "docs: record terrain and navigation, and their place in the dependency order"
```

```bash
cd <headless-minecraft>
git add docs/superpowers/specs/2026-08-13-minecraft-simulation-design.md
git commit -m "docs(spec): move pathfinding from a non-goal to a package"
```

---

## What this plan does not deliver

Stated here so a reader of the finished work is not left guessing:

- No follower. A caller gets a `Path` and walks it itself until `navigator` lands.
- No digging, building, or gravity-block handling. `Support` and `Collapse` are specified and unbuilt.
- No jump-gap edges. A one-block gap is routed around, not jumped.
- No diagonal movement. Adding it needs a corner-cutting rule.
- `examples/orbit` still carries `bypass.go` and `blocks.go`. Deleting them is the design's first acceptance criterion and it needs a tagged `minecraft-simulation` release first.
