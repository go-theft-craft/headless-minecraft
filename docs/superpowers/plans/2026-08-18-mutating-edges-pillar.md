# Mutating edges and pillar Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `subagent-driven-development` (recommended) or `executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extract the two block properties every mutating edge waits on, build the overlay and its validation loop against the smallest edge that needs them, and add the vertical edge the parent design's list has no member for.

**Architecture:** The block properties land as an extracted dataset alongside `blockMovement.json`, not as fields on `data.Block`, because they are measured out of a jar rather than published upstream. `navigation.Overlay` then decorates a `world.View` with pending placements, `Place` exercises it, and `Pillar` follows — with the two bounds and the recomputed heuristic that a search able to reach every Y coordinate now needs.

**Tech Stack:** Go 1.26.6 from `openserbia/go-flake`, Devbox, Task, `golangci-lint`, `mcreference` for the jar extraction, and the existing `geom`, `world`, `collision`, `terrain`, and `navigation` packages.

Design: [mutating edge amendment](../specs/2026-08-18-mutating-edge-amendment-design.md). Parent: [navigation design](../specs/2026-08-17-navigation-design.md).

## Before executing this plan: reconcile it

**The design and the parent design are both wrong about where the block properties go.** Both say `data.Block.Falling`. `minecraft-protocol` already has the right home and the right precedent, and `data/block_movement.go` states the rule in its own doc comment:

> It is separate from `BlockRegistry` because the fact has a different source. Upstream's block data says what a block is called, how hard it is, and what it drops; it does not say whether an entity can occupy its cell. That answer lives in the game's own material, so it is measured out of a Mojang jar and arrives here as an extracted dataset, the same way physics constants do. A version nobody has measured publishes no registry at all rather than an empty one, because "no measurement" and "nothing blocks movement" are not the same statement.

Falling and climbable are exactly that kind of fact — `instanceof BlockFalling` and `instanceof BlockLadder`, neither of which upstream publishes. So they extend the extracted dataset that already carries `blocksMovement`, and `data.Block` gains no field. Task 1 does it that way and corrects both design documents.

This also inherits the rule that matters most: **a version nobody has measured publishes no registry rather than an empty one.** A missing measurement is not "nothing falls".

## Global Constraints

- Go 1.26.6 from `openserbia/go-flake`; Devbox and Task drive every gate.
- **`minecraft-protocol` is public and permanent.** Its module zip is served immutably by `proxy.golang.org` and hashed into `sum.golang.org`. A published field is far harder to remove than to add; task 1 adds two and no more.
- Unknown is never folded into a default. A block the measurement does not describe reports "not described", and a caller that reads that as "does not fall" is wrong.
- No version constant is typed into `navigation`. A number a version owns arrives as a `Capability` field.
- No break time and no placement legality rule appears in this work. Those are M9.4 and M9.5; `Capability` defaults digging and placement off and the search refuses those edges until they land.
- No map iteration in any path that affects output ordering.
- `devbox run -- task lint`, `task test`, `task determinism`, and `task verify` pass before every commit.
- Conventional commit subjects. No `Co-Authored-By` trailer and no `Claude-Session` line.

## Scope

**In scope:** the two extracted properties, the overlay, the validation loop, `Place`, `Pillar`, the two search bounds, and the recomputed heuristic.

**Out of scope, with the reason:**

| Deferred | Why |
| --- | --- |
| `Dig` | Needs M9.4's break times. A bot that mines at a plausible-looking wrong speed is worse than one that refuses to mine |
| `Support` and `Collapse` | Need M9.5's placement legality and a captured falling-column trace |
| Scaffolding, block retrieval, route cleanup | A bot that pillars up and leaves the pillar is behaving correctly for this design |
| The light policy on `Capability` | Torch spacing is priced against `Collapse` and the tunnels digging makes. Both are deferred |

---

## Task 1: Extract falling and climbable

**Files:**
- Modify: `minecraft-protocol/data/block_movement.go`
- Modify: `minecraft-protocol/cmd/mcdata-gen` render for the movement dataset
- Modify: `minecraft-protocol/source/java/1.8/blockMovement.json`, `source/java/26.1/blockMovement.json`
- Modify: `minecraft-protocol/source/java/1.8/manifest.json`, `source/java/26.1/manifest.json`
- Modify: `headless-minecraft/docs/superpowers/specs/2026-08-18-mutating-edge-amendment-design.md`
- Modify: `headless-minecraft/docs/superpowers/specs/2026-08-17-navigation-design.md`
- Test: `minecraft-protocol/data/block_movement_test.go`

**Interfaces:**
- Produces: `BlockMovementRegistry.FallsByState(BlockStateID) (bool, bool)`, `FallsByID(BlockID) (bool, bool)`, `ClimbableByState(BlockStateID) (bool, bool)`, `ClimbableByID(BlockID) (bool, bool)`.

The dataset already carries `blocksMovement` per block with the jar's SHA-256 as provenance. These two ride in the same records, from the same extraction pass, with the same version rule.

- [ ] **Step 1: Correct the two design documents**

In both specs, replace every `data.Block.Falling` with the extracted-registry form, and record the reason in one sentence: upstream publishes neither property, so they belong with the measured dataset rather than on the published block record.

- [ ] **Step 2: Write the failing test**

```go
func TestGravelFallsAndSoulSandDoesNot(t *testing.T) {
	t.Parallel()

	// Material will not substitute for this measurement: soul sand shares
	// Material.sand with gravel and does not fall. That is the whole reason
	// the property is extracted rather than derived.
	registry := movementRegistry(t, "java/1.8.9")

	gravel, described := registry.FallsByID(gravelID)
	if !described {
		t.Fatal("gravel is not described by the measurement")
	}
	if !gravel {
		t.Fatal("gravel does not fall")
	}

	soulSand, described := registry.FallsByID(soulSandID)
	if !described {
		t.Fatal("soul sand is not described by the measurement")
	}
	if soulSand {
		t.Fatal("soul sand falls")
	}
}

func TestALadderIsClimbableAndStoneIsNot(t *testing.T) {
	t.Parallel()

	registry := movementRegistry(t, "java/1.8.9")

	ladder, described := registry.ClimbableByID(ladderID)
	if !described || !ladder {
		t.Fatalf("ladder climbable = %v, described = %v; want true, true", ladder, described)
	}

	stone, described := registry.ClimbableByID(stoneID)
	if !described || stone {
		t.Fatalf("stone climbable = %v, described = %v; want false, true", stone, described)
	}
}

func TestAnUndescribedBlockReportsNotDescribed(t *testing.T) {
	t.Parallel()

	// The registry's existing rule, extended to the new properties: unknown
	// is not "does not fall". A caller that reads a missing answer as a
	// negative digs under a gravel column the measurement never mentioned.
	registry := movementRegistry(t, "java/1.8.9")

	if _, described := registry.FallsByID(BlockID(0xFFFF)); described {
		t.Fatal("an unmeasured block claimed a falling answer")
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `cd minecraft-protocol && devbox run -- go test ./data/ -run 'TestGravel|TestALadder|TestAnUndescribed' -v`
Expected: FAIL, undefined.

- [ ] **Step 4: Extend the dataset records**

Each entry in `blockMovement.json` gains two fields beside `blocksMovement`:

```json
    {
      "id": 13,
      "name": "minecraft:gravel",
      "blocksMovement": true,
      "falls": true,
      "climbable": false
    }
```

Re-run the extraction against the pinned jar whose SHA-256 the file already records, so the provenance stays true. Do not hand-edit the values: the file records a measurement, and a hand-edited measurement is a guess wearing a hash.

- [ ] **Step 5: Extend the registry and the generator**

Add the four accessors to `BlockMovementRegistry`, mirroring `ByState` and `ByID` exactly, including the two-result "described" convention. Extend `mcdata-gen`'s renderer to emit them.

- [ ] **Step 6: Run the generation check**

Run: `cd minecraft-protocol && devbox run -- task generate && devbox run -- task generate:check`
Expected: PASS, and the diff shows only the two new properties.

- [ ] **Step 7: Run every gate**

Run: `cd minecraft-protocol && devbox run -- task verify`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add data/ cmd/ source/ generated/
git commit -m "feat(data): extract whether a block falls and whether it is climbable"
```

---

## Task 2: Release and consume

**Files:**
- Modify: `minecraft-protocol/CHANGELOG.md`
- Modify: `minecraft-simulation/go.mod`

**Interfaces:**
- Produces: a `minecraft-protocol` release `minecraft-simulation` can require.

- [ ] **Step 1: Add the changelog entry**

```markdown
### Added

- `BlockMovementRegistry` reports whether a block falls and whether it is
  climbable, from the same extraction pass and the same pinned jar as
  `blocksMovement`. A version nobody has measured publishes no registry.
```

- [ ] **Step 2: Release**

```bash
cd minecraft-protocol
devbox run -- task release:check
git add CHANGELOG.md
git commit -m "docs: prepare the 0.6.0 release"
git tag v0.6.0
git push origin main --tags
```

- [ ] **Step 3: Consume it**

```bash
cd minecraft-simulation
devbox run -- go get github.com/go-theft-craft/minecraft-protocol@v0.6.0
devbox run -- task verify
```

- [ ] **Step 4: Commit**

```bash
git add go.mod go.sum
git commit -m "build: take minecraft-protocol v0.6.0 for the block behaviour registry"
```

---

## Task 3: The overlay

**Files:**
- Create: `minecraft-simulation/navigation/overlay.go`
- Test: `minecraft-simulation/navigation/overlay_test.go`

**Interfaces:**
- Consumes: `world.View`, which is an interface with `CollisionShape(geom.BlockPos) (geom.Shape, world.Lookup)` and `BlockState(geom.BlockPos) (world.BlockRef, world.Lookup)`.
- Produces: `type Overlay struct{...}` implementing `world.View`, with `func NewOverlay(base world.View) *Overlay`, `func (*Overlay) Place(geom.BlockPos, world.BlockRef, geom.Shape)`, `func (*Overlay) Remove(geom.BlockPos)`, `func (*Overlay) Reset()`.

`world.View` being an interface is what makes this a decorator rather than a change to `world`. Nothing in `world` moves.

- [ ] **Step 1: Write the failing test**

```go
func TestAPlacementIsVisibleThroughTheOverlayAndNotTheBase(t *testing.T) {
	t.Parallel()

	base := world.NewBlocks()
	base.SetAir(geom.BlockPos{X: 0, Y: 64, Z: 0})

	overlay := NewOverlay(base)
	overlay.Place(geom.BlockPos{X: 0, Y: 64, Z: 0}, stoneRef(t), stoneShape(t))

	shape, lookup := overlay.CollisionShape(geom.BlockPos{X: 0, Y: 64, Z: 0})
	if lookup != world.Described || shape.IsEmpty() {
		t.Fatalf("overlay does not see the placement: lookup %v, shape %v", lookup, shape)
	}

	// The search plans against a world it may not touch. An overlay that
	// wrote through would corrupt the caller's snapshot on a route that is
	// then discarded.
	shape, _ = base.CollisionShape(geom.BlockPos{X: 0, Y: 64, Z: 0})
	if !shape.IsEmpty() {
		t.Fatal("the placement reached the base view")
	}
}

func TestTheOverlayDoesNotInventAnAnswerForAnUnknownCell(t *testing.T) {
	t.Parallel()

	// Unknown is never folded into anything. An overlay that answered
	// "air" for a cell the base cannot describe would let a route run
	// through unloaded chunks.
	overlay := NewOverlay(world.NewBlocks())

	if _, lookup := overlay.CollisionShape(geom.BlockPos{X: 99, Y: 64, Z: 99}); lookup == world.Described {
		t.Fatal("the overlay described a cell the base does not")
	}
}

func TestResetForgetsEveryPlacement(t *testing.T) {
	t.Parallel()

	base := world.NewBlocks()
	base.SetAir(geom.BlockPos{X: 0, Y: 64, Z: 0})

	overlay := NewOverlay(base)
	overlay.Place(geom.BlockPos{X: 0, Y: 64, Z: 0}, stoneRef(t), stoneShape(t))
	overlay.Reset()

	if shape, _ := overlay.CollisionShape(geom.BlockPos{X: 0, Y: 64, Z: 0}); !shape.IsEmpty() {
		t.Fatal("Reset left a placement behind")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd minecraft-simulation && devbox run -- go test ./navigation/ -run TestAPlacement -v`
Expected: FAIL, `NewOverlay` undefined.

- [ ] **Step 3: Write minimal implementation**

```go
// Overlay is a world.View that answers from a set of pending placements first
// and from a base view otherwise.
//
// A search that can place blocks has to search a world in which the placements
// already exist, and it must do that without touching the caller's snapshot:
// most of the routes it considers are discarded, and a search that wrote
// through would leave the discarded ones behind.
//
// world.View is an interface, so this is a decorator and nothing in world
// changes. That is what the interface is for.
//
// It is not safe for concurrent use. One search owns one overlay, which is the
// same rule the frontier follows.
type Overlay struct {
	base   world.View
	placed map[geom.BlockPos]placement
}
```

`CollisionShape` and `BlockState` consult `placed` first and fall through to `base`. Iterating `placed` is forbidden anywhere that affects ordering; it is only ever indexed.

- [ ] **Step 4: Run the tests**

Run: `cd minecraft-simulation && devbox run -- go test ./navigation/ -run 'TestAPlacement|TestTheOverlay|TestReset' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add navigation/overlay.go navigation/overlay_test.go
git commit -m "feat(navigation): plan against an overlay of pending placements"
```

---

## Task 4: Place, and the validation loop

**Files:**
- Create: `minecraft-simulation/navigation/place.go`
- Modify: `navigation/edge.go`, `navigation/navigation.go`, `navigation/search.go`
- Test: `navigation/place_test.go`

**Interfaces:**
- Consumes: `Overlay` from task 3.
- Produces: `EdgePlace`, `Capability.CanPlace bool`, `Capability.PlaceTicks float64`, `Capability.BlockBudget int`, `Capability.BlockTicks float64`.

`Place` is the smallest edge that exercises the overlay, which is why it comes before `Pillar`.

- [ ] **Step 1: Write the failing test**

```go
func TestABodyBridgesAGapItCannotJump(t *testing.T) {
	t.Parallel()

	// Six blocks of air: further than any jump reach, so the only crossing
	// is a bridge.
	view := scene.Fill(t, "wide-gap")
	capability := placingCapability(t)

	path, err := Find(t.Context(), view, capability, geom.BlockPos{X: 0, Y: 64, Z: 0},
		geom.BlockPos{X: 7, Y: 64, Z: 0}, defaultBudget())
	if err != nil {
		t.Fatalf("Find: %v", err)
	}

	if !path.Complete {
		t.Fatalf("path incomplete: %v", path.Reason)
	}

	var placed int
	for _, edge := range path.Edges {
		if edge.Kind == EdgePlace {
			placed++
		}
	}
	if placed == 0 {
		t.Fatal("crossed a six-block gap with no placement")
	}
}

func TestABodyWithNoBlocksDoesNotBridge(t *testing.T) {
	t.Parallel()

	capability := placingCapability(t)
	capability.BlockBudget = 0

	path, err := Find(t.Context(), scene.Fill(t, "wide-gap"), capability,
		geom.BlockPos{X: 0, Y: 64, Z: 0}, geom.BlockPos{X: 7, Y: 64, Z: 0}, defaultBudget())
	if err != nil {
		t.Fatalf("Find: %v", err)
	}

	for _, edge := range path.Edges {
		if edge.Kind == EdgePlace {
			t.Fatal("an empty inventory produced a placement")
		}
	}
}

func TestAPathWhosePlacementsConflictIsRejectedAndResearched(t *testing.T) {
	t.Parallel()

	// The parent design's validation rule: a winning path can be internally
	// inconsistent because one branch's placement occupies a cell an earlier
	// edge relied on. Find re-runs the overlay over the winner, bans the
	// offending edge, and searches again.
	view := scene.Fill(t, "gap-where-a-bridge-would-block-its-own-approach")
	capability := placingCapability(t)

	path, err := Find(t.Context(), view, capability, geom.BlockPos{X: 0, Y: 64, Z: 0},
		geom.BlockPos{X: 7, Y: 64, Z: 0}, defaultBudget())
	if err != nil {
		t.Fatalf("Find: %v", err)
	}

	if err := replayOverlay(t, view, capability, path); err != nil {
		t.Fatalf("Find returned a self-inconsistent path: %v", err)
	}
}

func TestTheValidationLoopTerminates(t *testing.T) {
	t.Parallel()

	// Each iteration bans exactly one edge, so the loop is bounded by the
	// edge count. A loop that banned nothing would spin forever on a world
	// designed to conflict.
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	if _, err := Find(ctx, scene.Fill(t, "adversarial-bridge"), placingCapability(t),
		geom.BlockPos{X: 0, Y: 64, Z: 0}, geom.BlockPos{X: 20, Y: 64, Z: 0}, defaultBudget()); err != nil {
		t.Fatalf("Find: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd minecraft-simulation && devbox run -- go test ./navigation/ -run 'TestABodyBridges|TestAPathWhose' -v`
Expected: FAIL, `EdgePlace` undefined.

- [ ] **Step 3: Write the edge, the resource accounting, and the loop**

Append `EdgePlace` after the read-only kinds. `Capability` gains:

```go
	// CanPlace allows placement edges. A mob is this value with it off, which
	// is how one search serves a body with no inventory.
	CanPlace bool
	// PlaceTicks is the cost of one placement, in ticks.
	PlaceTicks float64
	// BlockBudget is how many placeable blocks the body carries. A path may
	// not contain more placements than this.
	BlockBudget int
	// BlockTicks is what one placed block is worth, in ticks.
	//
	// Every cost here is in ticks so that "bridge the gap" and "walk around
	// it" are compared in one unit rather than through a weighting. A bot
	// holding two blocks routes differently from one holding a stack, and this
	// is the number that makes it so.
	BlockTicks float64
```

`Find` gains the re-run-and-ban loop the parent design specifies: run the overlay over the winning path from the start, and on a conflict ban that edge and search again. Each iteration bans one edge, so it terminates.

- [ ] **Step 4: Run the tests and the determinism gate**

Run: `cd minecraft-simulation && devbox run -- go test ./navigation/ -v && devbox run -- task determinism`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add navigation/
git commit -m "feat(navigation): bridge a gap by placing blocks"
```

---

## Task 5: Pillar, and the bounds it forces

**Files:**
- Create: `minecraft-simulation/navigation/pillar.go`
- Modify: `navigation/edge.go`, `navigation/navigation.go`
- Test: `navigation/pillar_test.go`

**Interfaces:**
- Consumes: `Overlay` from task 3, `EdgePlace`'s resource accounting from task 4, `PostureFall` from the [edge completion plan](2026-08-18-navigation-edge-completion.md) task 2.
- Produces: `EdgePillar`, `Capability.VerticalEnvelope int32`, `Capability.MaxPillarHeight int`.

This is the edge the parent design's list has no member for. `Place` bridges horizontally and `Support` holds a column; nothing gains height, so a bot with a stack of blocks is capped by `StepHeight` and a jump arc exactly as a bot with nothing is.

- [ ] **Step 1: Write the failing test**

```go
func TestABodyPillarsOutOfAShaft(t *testing.T) {
	t.Parallel()

	// A one-block shaft, ten blocks deep, with no ladder and no slope. Every
	// read-only edge is exhausted: step rises one, jump needs somewhere to
	// land, and climb needs a ladder.
	view := scene.Fill(t, "ten-block-shaft")
	capability := pillaringCapability(t)

	path, err := Find(t.Context(), view, capability, geom.BlockPos{X: 0, Y: 54, Z: 0},
		geom.BlockPos{X: 0, Y: 64, Z: 0}, defaultBudget())
	if err != nil {
		t.Fatalf("Find: %v", err)
	}

	if !path.Complete {
		t.Fatalf("could not pillar out of a shaft: %v", path.Reason)
	}

	var pillared int
	for _, edge := range path.Edges {
		if edge.Kind == EdgePillar {
			pillared++
		}
	}
	if pillared != 10 {
		t.Fatalf("used %d pillar edges to rise ten blocks", pillared)
	}
}

func TestPillaringIntoACeilingIsRefused(t *testing.T) {
	t.Parallel()

	path, err := Find(t.Context(), scene.Fill(t, "shaft-with-a-ceiling"), pillaringCapability(t),
		geom.BlockPos{X: 0, Y: 54, Z: 0}, geom.BlockPos{X: 0, Y: 64, Z: 0}, defaultBudget())
	if err != nil {
		t.Fatalf("Find: %v", err)
	}

	if path.Complete {
		t.Fatal("pillared through a ceiling")
	}
}

func TestNoPathWalksBackDownAPillar(t *testing.T) {
	t.Parallel()

	// A pillar cannot be walked back down. Coming down is Fall within the
	// safe fall or Dig beneath the body, and treating the edge as symmetric
	// produces routes that strand the body on top of a tower.
	path, err := Find(t.Context(), scene.Fill(t, "pillar-then-descend"), pillaringCapability(t),
		geom.BlockPos{X: 0, Y: 64, Z: 0}, geom.BlockPos{X: 6, Y: 64, Z: 0}, defaultBudget())
	if err != nil {
		t.Fatalf("Find: %v", err)
	}

	for i, edge := range path.Edges {
		if edge.Kind == EdgePillar && edge.To.Y < edge.From.Y {
			t.Fatalf("edge %d pillars downward: %+v", i, edge)
		}
	}
}

func TestThePerColumnPillarLimitIsRespected(t *testing.T) {
	t.Parallel()

	capability := pillaringCapability(t)
	capability.MaxPillarHeight = 3

	path, err := Find(t.Context(), scene.Fill(t, "tower-or-walk"), capability,
		geom.BlockPos{X: 0, Y: 64, Z: 0}, geom.BlockPos{X: 10, Y: 64, Z: 0}, defaultBudget())
	if err != nil {
		t.Fatalf("Find: %v", err)
	}

	if run := longestPillarRun(path); run > 3 {
		t.Fatalf("stacked %d pillar edges in one column, limit is 3", run)
	}
}

func TestAGoalOutsideTheVerticalEnvelopeIsUnreachable(t *testing.T) {
	t.Parallel()

	// Without this, a search that can pillar spends its whole node budget
	// climbing toward a goal it can never reach, and reports ReasonBudget —
	// which reads as "try a bigger budget" and never succeeds.
	capability := pillaringCapability(t)
	capability.VerticalEnvelope = 16

	path, err := Find(t.Context(), scene.Fill(t, "open-sky"), capability,
		geom.BlockPos{X: 0, Y: 64, Z: 0}, geom.BlockPos{X: 0, Y: 200, Z: 0}, defaultBudget())
	if err != nil {
		t.Fatalf("Find: %v", err)
	}

	if path.Reason != ReasonUnreachable {
		t.Fatalf("Reason = %v, want ReasonUnreachable", path.Reason)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd minecraft-simulation && devbox run -- go test ./navigation/ -run TestABodyPillars -v`
Expected: FAIL, `EdgePillar` undefined.

- [ ] **Step 3: Write the expansion**

```go
// pillars returns the single upward edge that places a block under the body.
//
// It is not a special case of Place. Place puts a block into a cell the body
// will walk across; this puts one into the cell the body is standing in, while
// the body is above it, and the body arrives one block higher. The
// preconditions differ, the resulting node differs, and it is not reversible:
// coming down is a Fall within the safe fall, or a Dig beneath the body, and
// treating this as symmetric produces routes that strand the body on a tower.
func (c Capability) pillars(o oracle, overlay *Overlay, from node) (Edge, bool, error) {
	if !c.CanPlace || c.BlockBudget <= 0 || from.Posture != PostureStand {
		return Edge{}, false, nil
	}

	above := geom.BlockPos{X: from.Pos.X, Y: from.Pos.Y + 1, Z: from.Pos.Z}

	// A bot cannot pillar into a ceiling. The body has to fit where it lands
	// before anything is placed under it.
	arr, err := o.arriveAt(above)
	if err != nil {
		return Edge{}, false, err
	}
	if !arr.ok {
		return Edge{}, false, nil
	}

	return Edge{
		Kind: EdgePillar, From: from.Pos, To: above,
		Posture: PostureStand,
		Cost:    c.PlaceTicks + c.BlockTicks,
	}, true, nil
}
```

The per-column limit and the vertical envelope are checked in `expand` before `pillars` is called, so a bounded search never builds the node at all.

- [ ] **Step 4: Run the tests**

Run: `cd minecraft-simulation && devbox run -- go test ./navigation/ -run 'TestABodyPillars|TestPillaring|TestNoPath|TestThePerColumn|TestAGoalOutside' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add navigation/pillar.go navigation/pillar_test.go navigation/edge.go navigation/navigation.go
git commit -m "feat(navigation): pillar up by placing beneath the body"
```

---

## Task 6: Recompute the heuristic

**Files:**
- Modify: `minecraft-simulation/navigation/search.go`
- Test: `navigation/property_test.go`

**Interfaces:**
- Modifies: `Capability.perBlockFloor`, which today is computed over the movement edges alone.

`perBlockFloor`'s own comment records the care already taken: a step closes two blocks for one step's cost, and the floor is deliberately not the cheapest edge because "an overestimating heuristic lets the search settle a goal on a route that is not shortest." `Pillar` closes a block of vertical distance for one placement's cost, so it enters that calculation. If it does not, the floor overestimates the moment a placement is cheaper than walking, and the failure surfaces as the determinism gate disagreeing about routes that are merely suboptimal — the worst kind to diagnose.

- [ ] **Step 1: Write the admissibility property test**

```go
func TestTheHeuristicNeverExceedsTheTrueCost(t *testing.T) {
	t.Parallel()

	// This is the gate on the whole change. An inadmissible heuristic does
	// not crash and does not return a wrong-looking path; it returns a
	// slightly-too-expensive one, and the determinism gate then disagrees
	// across runs for reasons nobody can see.
	rng := rand.New(rand.NewPCG(7, 11))

	for range 2000 {
		capability := randomCapability(t, rng)
		view := scene.Fill(t, "mixed-terrain")

		start, goal := randomCells(t, rng)

		path, err := Find(t.Context(), view, capability, start, goal, generousBudget())
		if err != nil {
			t.Fatalf("Find: %v", err)
		}
		if !path.Complete {
			continue
		}

		if h := capability.heuristic(start, goal); h > path.Cost+1e-9 {
			t.Fatalf("heuristic %v exceeds the true cost %v for capability %+v",
				h, path.Cost, capability)
		}
	}
}
```

`randomCapability` must enable random subsets of the edges, including the pillar and place edges with random costs, because the floor is only wrong for some subsets.

- [ ] **Step 2: Run it to verify it fails**

Run: `cd minecraft-simulation && devbox run -- go test ./navigation/ -run TestTheHeuristicNever -v`
Expected: FAIL, with a capability whose pillar is cheaper than its walk.

- [ ] **Step 3: Recompute the floor over the enabled edge set**

Extend `perBlockFloor` to consider every enabled edge's cost per block of Manhattan distance closed, keeping the existing reasoning about the step edge closing two blocks. Update its doc comment to say that the floor is computed over the enabled set and why.

- [ ] **Step 4: Run the property test and the determinism gate**

Run: `cd minecraft-simulation && devbox run -- go test ./navigation/ -run TestTheHeuristicNever -v && devbox run -- task determinism`
Expected: PASS.

- [ ] **Step 5: Confirm the existing behaviour is unchanged**

```go
func TestACapabilityWithOnlyMovementEdgesRoutesAsItDidBefore(t *testing.T) {
	t.Parallel()

	// Everything in this plan is additive. A capability with placement off
	// must produce exactly the paths it produced before the overlay existed.
	capability := movementOnlyCapability(t)

	for _, name := range []string{"flat", "steps", "ledge", "water"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			path, err := Find(t.Context(), scene.Fill(t, name), capability,
				geom.BlockPos{X: 0, Y: 64, Z: 0}, geom.BlockPos{X: 8, Y: 64, Z: 0}, defaultBudget())
			if err != nil {
				t.Fatalf("Find: %v", err)
			}

			assertMatchesGolden(t, name, path)
		})
	}
}
```

- [ ] **Step 6: Run every gate**

Run: `cd minecraft-simulation && devbox run -- task verify && devbox run -- task determinism`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add navigation/
git commit -m "fix(navigation): compute the heuristic floor over the enabled edges"
```

---

## Definition of done

- A bot underground with a stack of blocks routes to the surface.
- A bot with an empty inventory produces the same path it produced before this plan, proved by the golden comparison in task 6.
- The heuristic is admissible for every combination of enabled edges, proved by a two-thousand-case property test.
- No returned path descends a pillar by walking.
- The per-column pillar limit and the vertical envelope are both respected, and a goal outside the envelope reports `ReasonUnreachable` rather than `ReasonBudget`.
- Falling and climbable are extracted from the pinned jar into the movement dataset, and an unmeasured block reports "not described" rather than a negative.
- Both design documents name the extracted registry rather than `data.Block.Falling`.
- `Dig`, `Support`, and `Collapse` are absent, and `Capability` defaults digging off.
- `devbox run -- task verify` and `task determinism` pass in all three repositories.

## Risks

| Risk | Mitigation |
| --- | --- |
| A published field in `minecraft-protocol` cannot be withdrawn — the module mirror serves it immutably and `sum.golang.org` records the hash | Task 1 adds two properties to an existing extracted dataset rather than to the published `data.Block`, and runs `generate:check` and `release:check` before the tag |
| The extracted values are hand-edited and the jar hash then lies about their provenance | Step 4 re-runs the extraction against the pinned jar and forbids hand-editing in as many words |
| An inadmissible heuristic ships and surfaces as intermittent determinism failures | Task 6's property test runs two thousand random capabilities over random edge subsets, and is written before the fix |
| The validation loop fails to terminate on an adversarial world | Each iteration bans exactly one edge; `TestTheValidationLoopTerminates` runs it under a ten-second context |
| `Pillar` is implemented as reversible because a symmetric edge is the natural thing to write | `TestNoPathWalksBackDownAPillar` exists for that specific mistake, and the doc comment names it |
| Task 5 starts before `PostureFall` exists | It is listed as a consumed interface from the other plan's task 2 |
