# Navigation edge completion Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `subagent-driven-development` (recommended) or `executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Finish the navigation design's own step 3 — `JumpGap` and the missing postures — and add the read-only edges it never named, so a body stops being unable to cross a two-block hole.

**Architecture:** Every edge here is read-only over a `world.View`, so none of them needs the overlay or the ban-and-re-search validation loop the mutating amendment owns. `Capability` grows fields and loses none, and `navigation` still types no version constant: the jump reach arrives as a field, computed by running the profile's own movement kernel rather than guessed.

**Tech Stack:** Go 1.26.6 from `openserbia/go-flake`, Devbox, Task, `golangci-lint`, and the existing `geom`, `world`, `collision`, `terrain`, and `movement` packages.

Design: [navigation edge completion design](../specs/2026-08-18-navigation-edge-completion-design.md). Parent: [navigation design](../specs/2026-08-17-navigation-design.md).

## Before executing this plan: reconcile it

The navigation plan of 2026-08-17 deferred `JumpGap` and gave a reason this plan must honour rather than dodge:

> Doing it honestly needs a per-profile reach table computed from the movement kernel, which is its own deliverable. A guessed maximum gap would be a number this repository does not verify.

That still stands. Task 1 builds the reach table by running the kernel; no later task takes a gap distance from anywhere else.

The same plan lists `data.Block.Falling` and the `examples/orbit` rewrite as deferred. Neither blocks this plan: no edge here digs, and the orbit rewrite is the [aiming plan](2026-08-18-aiming-and-reach-geometry.md)'s task 6. **The climbable block property does block task 5**, and it is extracted in `minecraft-protocol` by the [mutating edges plan](2026-08-18-mutating-edges-pillar.md)'s task 1, because it is the same extraction pass as `Falling`.

## Global Constraints

- Go 1.26.6 from `openserbia/go-flake`; Devbox and Task drive every gate.
- Every gate runs in `minecraft-simulation`. This plan touches no other repository.
- `terrain` and `navigation` import `geom`, `world`, and `collision` only. Task 1's reach table is a separate deliverable that may import `movement` and a profile; `navigation` itself may not.
- No version constant is typed into `navigation`. A number a version owns arrives as a `Capability` field.
- No map iteration in any path that affects output ordering. Go randomizes it and the determinism gate fails.
- Unknown is never folded into blocked. A caller that cannot read a cell is told so.
- `devbox run -- task lint`, `task test`, `task determinism`, and `task verify` pass before every commit.
- Conventional commit subjects. No `Co-Authored-By` trailer and no `Claude-Session` line.

## File Structure

All paths relative to the `minecraft-simulation` repository root.

| File | Responsibility |
| --- | --- |
| `navigation/reach/reach.go` | The jump-reach table, computed by running a profile's movement kernel |
| `navigation/reach/reach_test.go` | Its gate |
| `navigation/navigation.go` | `Posture` gains sneak, fall, and crawl; `Capability` gains the new fields |
| `navigation/edge.go` | `EdgeKind` gains jump, climb, door, and water-drop |
| `navigation/jump.go` | `JumpGap` expansion and its arc clearance |
| `navigation/vertical.go` | `Climb` and `WaterDrop` expansion |
| `navigation/door.go` | `Door` expansion and the conflict check |
| `terrain/passability.go` | `Climbable` |
| `navigation/property_test.go` | The existing cross-cutting properties, extended |

`jump.go`, `vertical.go`, and `door.go` are separate files rather than a growing `search.go`: each holds one expansion rule with its own preconditions, and `search.go` is already the file that holds the A* itself.

---

## Task 1: The jump reach table

**Files:**
- Create: `navigation/reach/reach.go`
- Test: `navigation/reach/reach_test.go`

**Interfaces:**
- Produces: `func Measure(profile sim.Profile, body entity.State, ticks int) (Table, error)`, `type Table struct { HorizontalBlocks float64; PeakRise float64 }`

This is first because the 2026-08-17 plan refused to build `JumpGap` without it. It lives in its own package because it imports `sim`, `movement`, and a profile, and `navigation` may import none of those.

- [ ] **Step 1: Write the failing test**

```go
func TestASprintJumpClearsMoreThanAWalkJump(t *testing.T) {
	t.Parallel()

	// The number itself is the profile's. What this asserts is the ordering
	// that any correct measurement must produce, so a table built from a
	// broken kernel fails here rather than becoming a routing constant.
	walk, err := Measure(v1_8.Profile(), standingPlayer(t), 40)
	if err != nil {
		t.Fatalf("Measure: %v", err)
	}
	sprint, err := Measure(v1_8.Profile(), sprintingPlayer(t), 40)
	if err != nil {
		t.Fatalf("Measure: %v", err)
	}

	if sprint.HorizontalBlocks <= walk.HorizontalBlocks {
		t.Fatalf("sprint jump cleared %v, walk jump cleared %v; want the sprint further",
			sprint.HorizontalBlocks, walk.HorizontalBlocks)
	}
}

func TestTheTwoVersionsDisagreeAboutTheReach(t *testing.T) {
	t.Parallel()

	// If these came out identical the table would be reading a shared
	// constant rather than each version's own kernel, which is the exact
	// failure the 2026-08-17 plan refused to ship.
	old, err := Measure(v1_8.Profile(), sprintingPlayer(t), 40)
	if err != nil {
		t.Fatalf("Measure 1.8.9: %v", err)
	}
	modern, err := Measure(v26_1.Profile(), sprintingPlayer(t), 40)
	if err != nil {
		t.Fatalf("Measure 26.1.2: %v", err)
	}

	if old.HorizontalBlocks == modern.HorizontalBlocks {
		t.Fatalf("both versions measured %v; the table is not reading the kernel",
			old.HorizontalBlocks)
	}
}

func TestMeasureIsDeterministic(t *testing.T) {
	t.Parallel()

	first, err := Measure(v1_8.Profile(), sprintingPlayer(t), 40)
	if err != nil {
		t.Fatalf("Measure: %v", err)
	}

	for range 100 {
		again, err := Measure(v1_8.Profile(), sprintingPlayer(t), 40)
		if err != nil {
			t.Fatalf("Measure: %v", err)
		}
		if again != first {
			t.Fatalf("Measure returned %+v then %+v", first, again)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `devbox run -- go test ./navigation/reach/ -v`
Expected: FAIL, package does not exist.

- [ ] **Step 3: Write minimal implementation**

```go
// Package reach measures how far a body's jump actually carries it, by running
// the profile's own movement kernel over an empty world.
//
// It is a separate package because it imports sim, movement, and a profile,
// and navigation may import none of those. The number crosses that boundary as
// a Capability field rather than as an import.
//
// It is measured rather than tabulated because the two supported versions
// disagree about every constant on the jump path, and because a hand-written
// maximum gap is a number this repository has no way to verify. The navigation
// plan of 2026-08-17 deferred the jump edge for exactly that reason.
package reach

// Table is what one body's jump clears.
type Table struct {
	// HorizontalBlocks is how far the body travels between leaving the ground
	// and landing back at the same height.
	HorizontalBlocks float64
	// PeakRise is the highest the body's feet get above the take-off height.
	// A jump edge needs it to know what clearance the arc requires.
	PeakRise float64
}

// Measure runs the profile's tick over a flat world for at most ticks and
// reports what one jump cleared.
//
// The world is flat and otherwise empty on purpose. This measures the arc, and
// what the arc collides with is the search's question — asked per candidate
// against the real world rather than baked in here.
func Measure(profile sim.Profile, body entity.State, ticks int) (Table, error) {
	kernel, err := sim.NewKernel(profile)
	if err != nil {
		return Table{}, fmt.Errorf("reach: %w", err)
	}

	store := runtime.NewMemory(profile)
	if err := layFloor(store, body); err != nil {
		return Table{}, fmt.Errorf("reach: %w", err)
	}

	runner := runtime.NewRunner(store, kernel)

	start := body.Position
	table := Table{}
	airborne := false

	for range ticks {
		// Jump is held only on the first tick. A held jump re-jumps on
		// landing, which measures two arcs and reports the second.
		commands := holdForward(body)
		if !airborne {
			commands = append(commands, jumpCommand(body))
		}

		result, err := runner.Step(context.Background(), commands)
		if err != nil {
			return Table{}, fmt.Errorf("reach: tick: %w", err)
		}

		state, err := stateOf(result, body)
		if err != nil {
			return Table{}, fmt.Errorf("reach: %w", err)
		}

		rise := state.Position.Y - start.Y
		if rise > table.PeakRise {
			table.PeakRise = rise
		}

		if !airborne && rise > 0 {
			airborne = true

			continue
		}

		// Landed: the body is back on the ground at or below where it left.
		if airborne && state.OnGround {
			table.HorizontalBlocks = math.Hypot(
				state.Position.X-start.X,
				state.Position.Z-start.Z,
			)

			return table, nil
		}
	}

	return Table{}, fmt.Errorf("reach: the body did not land within %d ticks", ticks)
}
```

`layFloor`, `holdForward`, `jumpCommand`, and `stateOf` are small helpers in the same file: the floor is one layer of a full-cube block under the body's start, the commands are the profile's own movement commands, and `stateOf` reads the body out of the tick result's change set. Drive the runner exactly as `mctest`'s replay does, so the measurement runs the same path the conformance fixtures do.

- [ ] **Step 4: Run test to verify it passes**

Run: `devbox run -- go test ./navigation/reach/ -v`
Expected: PASS.

- [ ] **Step 5: Record the measured numbers**

Add a table to the package doc listing what each profile measured, with the date. It is documentation of a measurement, not a constant the code reads.

- [ ] **Step 6: Commit**

```bash
git add navigation/reach/
git commit -m "feat(navigation): measure jump reach from each profile's kernel"
```

---

## Task 2: JumpGap and PostureFall

**Files:**
- Modify: `navigation/navigation.go`
- Modify: `navigation/edge.go`
- Create: `navigation/jump.go`
- Modify: `navigation/search.go`
- Test: `navigation/jump_test.go`

**Interfaces:**
- Consumes: `reach.Table` from task 1, `terrain.Query.Fits`.
- Produces: `EdgeJumpGap`, `PostureFall`, `Capability.JumpTicks float64`, `Capability.JumpReach float64`, `Capability.JumpRise float64`.

- [ ] **Step 1: Write the failing test**

```go
func TestABodyCrossesATwoBlockGap(t *testing.T) {
	t.Parallel()

	// Ground at y=63 from x=0 to x=1, air at x=2 and x=3, ground again at
	// x=4. Nothing in the shipped vocabulary reaches the far side: Step rises
	// into an adjacent cell and Fall descends into one, and neither crosses.
	view := scene.Fill(t, "gap")
	capability := jumpingCapability(t)

	path, err := Find(t.Context(), view, capability, geom.BlockPos{X: 1, Y: 64, Z: 0},
		geom.BlockPos{X: 4, Y: 64, Z: 0}, defaultBudget())
	if err != nil {
		t.Fatalf("Find: %v", err)
	}

	if !path.Complete {
		t.Fatalf("path incomplete: %v", path.Reason)
	}
	if !slices.ContainsFunc(path.Edges, func(e Edge) bool { return e.Kind == EdgeJumpGap }) {
		t.Fatalf("crossed the gap without a jump edge: %v", path.Edges)
	}
}

func TestACapabilityWithNoJumpReachRoutesAround(t *testing.T) {
	t.Parallel()

	view := scene.Fill(t, "gap-with-detour")
	capability := jumpingCapability(t)
	capability.JumpReach = 0

	path, err := Find(t.Context(), view, capability, geom.BlockPos{X: 1, Y: 64, Z: 0},
		geom.BlockPos{X: 4, Y: 64, Z: 0}, defaultBudget())
	if err != nil {
		t.Fatalf("Find: %v", err)
	}

	for _, edge := range path.Edges {
		if edge.Kind == EdgeJumpGap {
			t.Fatal("a capability with no jump reach produced a jump edge")
		}
	}
	if !path.Complete {
		t.Fatalf("the detour was not found: %v", path.Reason)
	}
}

func TestAJumpUnderALowCeilingIsRefused(t *testing.T) {
	t.Parallel()

	// The arc rises. A block one above the midpoint is what the body hits,
	// and a search that only checked the endpoints would route straight
	// through it.
	blocked := scene.Fill(t, "gap-with-low-ceiling")
	clear := scene.Fill(t, "gap")

	capability := jumpingCapability(t)
	start, goal := geom.BlockPos{X: 1, Y: 64, Z: 0}, geom.BlockPos{X: 4, Y: 64, Z: 0}

	blockedPath, err := Find(t.Context(), blocked, capability, start, goal, defaultBudget())
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	clearPath, err := Find(t.Context(), clear, capability, start, goal, defaultBudget())
	if err != nil {
		t.Fatalf("Find: %v", err)
	}

	if hasJump(blockedPath) {
		t.Fatal("jumped through a ceiling one block above the arc")
	}
	if !hasJump(clearPath) {
		t.Fatal("the same gap without the ceiling produced no jump")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `devbox run -- go test ./navigation/ -run TestA.*Gap -v`
Expected: FAIL, `EdgeJumpGap` undefined.

- [ ] **Step 3: Add the constants and fields**

In `edge.go`, append `EdgeJumpGap` to the `EdgeKind` block **after** the existing four, so no shipped value renumbers, and extend `String`. In `navigation.go`, append `PostureFall` after `PostureSwim` for the same reason, extend `String`, and add:

```go
	// JumpTicks is the cost of one jump edge, in ticks.
	JumpTicks float64
	// JumpReach is how far the body's jump carries it horizontally, in
	// blocks. Zero produces no jump edges, which is how a mob keeps getting a
	// ground navigator out of the same search.
	//
	// It comes from navigation/reach, which measures it by running the
	// profile's own movement kernel. A hand-written value here is a number
	// this repository cannot verify, and the conformance gate excludes a
	// capability built with one.
	JumpReach float64
	// JumpRise is how high the arc's peak is above the take-off, in blocks.
	// The clearance check needs it.
	JumpRise float64
```

- [ ] **Step 4: Write the expansion**

In `navigation/jump.go`:

```go
// jumps returns every jump edge leaving a cell.
//
// It is separate from expand's four-neighbour walk because a jump is not a
// neighbour: it reaches cells two, three, and sometimes four blocks away, and
// folding that into the step loop would make the neighbour order depend on the
// capability. The order here is the step order, then increasing distance, which
// keeps the frontier's tie-breaking total.
func (c Capability) jumps(o oracle, from node) ([]Edge, error) {
	if c.JumpReach <= 0 || from.Posture == PostureFall {
		return nil, nil
	}

	maxBlocks := int32(math.Floor(c.JumpReach))
	edges := make([]Edge, 0, len(steps))

	for _, step := range steps {
		for distance := int32(2); distance <= maxBlocks; distance++ {
			landing := geom.BlockPos{
				X: from.Pos.X + step.X*distance,
				Y: from.Pos.Y,
				Z: from.Pos.Z + step.Z*distance,
			}

			arr, err := o.arriveAt(landing)
			if err != nil {
				return nil, err
			}
			if !arr.ok {
				continue
			}

			clear, err := c.arcIsClear(o, from.Pos, step, distance)
			if err != nil {
				return nil, err
			}
			if !clear {
				// A blocked arc at this distance says nothing about a longer
				// one: the body rises and then falls, so a ceiling that stops
				// a short hop can sit above a long one's landing.
				continue
			}

			edges = append(edges, Edge{
				Kind: EdgeJumpGap, From: from.Pos, To: landing,
				Posture: arr.posture, Cost: c.JumpTicks * float64(distance),
			})
		}
	}

	return edges, nil
}
```

`arcIsClear` walks every intervening column and asks `terrain.Query.Fits` for the body's box at the arc's height there, using `JumpRise` to shape the parabola. It reuses the query; it re-derives no collision.

Call `jumps` from `expand` after the four-step loop, so the neighbour order is unchanged for a capability with no jump.

- [ ] **Step 5: Run the tests**

Run: `devbox run -- go test ./navigation/ -run TestA.*Gap -v`
Expected: PASS.

- [ ] **Step 6: Run the determinism gate**

Run: `devbox run -- task determinism`
Expected: PASS. If it fails, the new edges are being appended in a non-total order.

- [ ] **Step 7: Commit**

```bash
git add navigation/
git commit -m "feat(navigation): cross gaps with a measured jump reach"
```

---

## Task 3: PostureSneak

**Files:**
- Modify: `navigation/navigation.go`, `navigation/search.go`
- Test: `navigation/posture_test.go`

**Interfaces:**
- Produces: `PostureSneak`, `Capability.SneakTicks float64`, `Capability.CanSneak bool`.

Sneaking earns a posture rather than a flag because it is a per-position decision. A flag would make a bot sneak for a whole route or none of it, and the whole value of sneaking is doing it at the one ledge that needs it.

- [ ] **Step 1: Write the failing test**

```go
func TestABodySneaksAcrossALedgeAndStandsElsewhere(t *testing.T) {
	t.Parallel()

	// A one-block-wide walkway with a drop on both sides. A standing body
	// walks off it; a sneaking one does not.
	view := scene.Fill(t, "ledge")
	capability := sneakingCapability(t)

	path, err := Find(t.Context(), view, capability, geom.BlockPos{X: 0, Y: 64, Z: 0},
		geom.BlockPos{X: 5, Y: 64, Z: 0}, defaultBudget())
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if !path.Complete {
		t.Fatalf("path incomplete: %v", path.Reason)
	}

	var sneaked, stood int
	for _, edge := range path.Edges {
		switch edge.Posture {
		case PostureSneak:
			sneaked++
		case PostureStand:
			stood++
		}
	}

	if sneaked == 0 {
		t.Fatal("crossed the ledge without sneaking")
	}
	if stood == 0 {
		t.Fatal("sneaked the whole route; sneaking is per position, not per body")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `devbox run -- go test ./navigation/ -run TestABodySneaks -v`
Expected: FAIL, `PostureSneak` undefined.

- [ ] **Step 3: Write the implementation**

Append `PostureSneak` to the `Posture` block after `PostureFall`, extend `String`, add the two `Capability` fields, and have `arrivalAt` return `PostureSneak` for a cell the body may occupy only while sneaking. `enter` prices an edge arriving in `PostureSneak` at `SneakTicks`.

- [ ] **Step 4: Run the tests and the determinism gate**

Run: `devbox run -- go test ./navigation/ -v && devbox run -- task determinism`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add navigation/
git commit -m "feat(navigation): sneak across ledges as a posture"
```

---

## Task 4: WaterDrop

**Files:**
- Create: `navigation/vertical.go`
- Modify: `navigation/edge.go`, `navigation/navigation.go`
- Test: `navigation/vertical_test.go`

**Interfaces:**
- Consumes: `terrain.Query.FluidAt`, which exists.
- Produces: `EdgeWaterDrop`, `Capability.WaterLandingDepth float64`.

The shipped `Fall` edge is bounded by `SafeFall` and has no way to express a drop that is safe because of what is at the bottom.

- [ ] **Step 1: Write the failing test**

```go
func TestADeepDropIntoWaterIsTakenAndOntoStoneIsNot(t *testing.T) {
	t.Parallel()

	capability := swimmingCapability(t)
	capability.SafeFall = 3

	start := geom.BlockPos{X: 0, Y: 70, Z: 0}
	goal := geom.BlockPos{X: 0, Y: 60, Z: 0}

	water, err := Find(t.Context(), scene.Fill(t, "drop-into-water"), capability, start, goal, defaultBudget())
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	stone, err := Find(t.Context(), scene.Fill(t, "drop-onto-stone"), capability, start, goal, defaultBudget())
	if err != nil {
		t.Fatalf("Find: %v", err)
	}

	if !water.Complete {
		t.Fatalf("a ten-block drop into water was refused: %v", water.Reason)
	}
	if stone.Complete {
		t.Fatal("a ten-block drop onto stone was taken with a three-block safe fall")
	}
}

func TestShallowWaterDoesNotBreakTheFall(t *testing.T) {
	t.Parallel()

	capability := swimmingCapability(t)
	capability.SafeFall = 3
	capability.WaterLandingDepth = 2

	path, err := Find(t.Context(), scene.Fill(t, "drop-into-one-block-of-water"), capability,
		geom.BlockPos{X: 0, Y: 70, Z: 0}, geom.BlockPos{X: 0, Y: 60, Z: 0}, defaultBudget())
	if err != nil {
		t.Fatalf("Find: %v", err)
	}

	if path.Complete {
		t.Fatal("one block of water broke a ten-block fall")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `devbox run -- go test ./navigation/ -run TestADeepDrop -v`
Expected: FAIL, `EdgeWaterDrop` undefined.

- [ ] **Step 3: Write the implementation**

`waterDrop` walks the column below a blocked neighbour exactly as `fall` does, bounded by the same `maxFallSearch`, and admits a landing beyond `SafeFall` only when `FluidAt` reports fluid for at least `WaterLandingDepth` blocks. It arrives in `PostureSwim`.

- [ ] **Step 4: Run the tests and every gate**

Run: `devbox run -- task verify`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add navigation/
git commit -m "feat(navigation): drop past the safe fall into deep water"
```

---

## Task 5: Climb

**Files:**
- Modify: `terrain/passability.go`, `terrain/terrain.go`
- Modify: `navigation/vertical.go`, `navigation/edge.go`, `navigation/navigation.go`
- Test: `terrain/passability_test.go`, `navigation/vertical_test.go`

**Interfaces:**
- Consumes: the climbable block property, extracted by the [mutating edges plan](2026-08-18-mutating-edges-pillar.md) task 1.
- Produces: `terrain.Facts.Climbable`, `EdgeClimb`, `Capability.ClimbTicks float64`, `Capability.CanClimb bool`.

**Blocked** until that extraction lands and `minecraft-protocol` releases it. Do not start otherwise.

- [ ] **Step 1: Write the failing test**

```go
func TestALadderIsClimbedInBothDirections(t *testing.T) {
	t.Parallel()

	// A ladder's collision box is empty, so nothing in collision tells it
	// from air. This is why the property is extracted rather than derived.
	view := scene.Fill(t, "ladder-shaft")
	capability := climbingCapability(t)

	up, err := Find(t.Context(), view, capability, geom.BlockPos{X: 0, Y: 64, Z: 0},
		geom.BlockPos{X: 0, Y: 70, Z: 0}, defaultBudget())
	if err != nil {
		t.Fatalf("Find up: %v", err)
	}
	down, err := Find(t.Context(), view, capability, geom.BlockPos{X: 0, Y: 70, Z: 0},
		geom.BlockPos{X: 0, Y: 64, Z: 0}, defaultBudget())
	if err != nil {
		t.Fatalf("Find down: %v", err)
	}

	if !up.Complete || !down.Complete {
		t.Fatalf("ladder not climbable both ways: up %v, down %v", up.Reason, down.Reason)
	}
}

func TestACapabilityThatCannotClimbRoutesAround(t *testing.T) {
	t.Parallel()

	capability := climbingCapability(t)
	capability.CanClimb = false

	path, err := Find(t.Context(), scene.Fill(t, "ladder-shaft-with-stairs"), capability,
		geom.BlockPos{X: 0, Y: 64, Z: 0}, geom.BlockPos{X: 0, Y: 70, Z: 0}, defaultBudget())
	if err != nil {
		t.Fatalf("Find: %v", err)
	}

	for _, edge := range path.Edges {
		if edge.Kind == EdgeClimb {
			t.Fatal("a capability that cannot climb produced a climb edge")
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `devbox run -- go test ./navigation/ -run TestALadder -v`
Expected: FAIL, `EdgeClimb` undefined.

- [ ] **Step 3: Write the implementation**

Add `Climbable(BlockRef) bool` to `terrain.Facts`, which is where a profile already supplies block facts. Add `climbs` to `navigation/vertical.go`, expanding one cell up and one down within a climbable column.

- [ ] **Step 4: Run the tests and every gate**

Run: `devbox run -- task verify`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add terrain/ navigation/
git commit -m "feat(navigation): climb ladders and vines"
```

---

## Task 6: PostureCrawl and the version asymmetry gate

**Files:**
- Modify: `navigation/navigation.go`
- Test: `navigation/posture_test.go`

**Interfaces:**
- Produces: `PostureCrawl`, `Capability.Postures` — the set of postures this body has.

This is the first case that runs backwards through the two-version gate, which currently reads "a scenario that runs on 1.8.9 and not on 26.1.2 is a failure". 26.1.2 has a crawl and 1.8.9 has none.

- [ ] **Step 1: Write the failing test**

```go
func TestTheCrawlAsymmetryIsAssertedInBothDirections(t *testing.T) {
	t.Parallel()

	// The gate's job is to record that a behaviour is absent from a version
	// and why — not to skip quietly. A future version that gains a posture
	// must not land silently, and neither must one that loses it.
	view := scene.Fill(t, "one-block-gap")
	start, goal := geom.BlockPos{X: 0, Y: 64, Z: 0}, geom.BlockPos{X: 3, Y: 64, Z: 0}

	modern, err := Find(t.Context(), view, crawlingCapability(t), start, goal, defaultBudget())
	if err != nil {
		t.Fatalf("Find 26.1.2: %v", err)
	}
	if !modern.Complete {
		t.Fatalf("26.1.2 could not crawl the one-block gap: %v", modern.Reason)
	}

	old, err := Find(t.Context(), view, standingCapability(t), start, goal, defaultBudget())
	if err != nil {
		t.Fatalf("Find 1.8.9: %v", err)
	}
	if old.Complete {
		t.Fatal("1.8.9 crossed a one-block gap; that version has no crawl")
	}
	if old.Reason != ReasonUnreachable {
		t.Fatalf("Reason = %v, want ReasonUnreachable", old.Reason)
	}
}

func TestACapabilityDeclaresItsPostures(t *testing.T) {
	t.Parallel()

	if slices.Contains(standingCapability(t).Postures, PostureCrawl) {
		t.Fatal("the 1.8.9 capability declares a crawl posture")
	}
	if !slices.Contains(crawlingCapability(t).Postures, PostureCrawl) {
		t.Fatal("the 26.1.2 capability does not declare a crawl posture")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `devbox run -- go test ./navigation/ -run 'TestTheCrawl|TestACapabilityDeclares' -v`
Expected: FAIL, `PostureCrawl` undefined.

- [ ] **Step 3: Write the implementation**

Add `PostureCrawl` and `Capability.Postures []Posture`. `arrivalAt` may only return a posture the capability declares, which makes the asymmetry a property of the value rather than a branch in the search.

- [ ] **Step 4: Record the asymmetry**

Add a paragraph to the `navigation` package doc naming crawl as the first behaviour present in 26.1.2 and absent from 1.8.9, and citing the master plan's rule that a per-version gate may say so.

- [ ] **Step 5: Run every gate**

Run: `devbox run -- task verify && devbox run -- task determinism`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add navigation/
git commit -m "feat(navigation): add the crawl posture and gate its absence on 1.8.9"
```

---

## Task 7: Door, and the conflict test that decides where it belongs

**Files:**
- Create: `navigation/door.go`
- Modify: `navigation/edge.go`, `navigation/navigation.go`
- Test: `navigation/door_test.go`

**Interfaces:**
- Produces: `EdgeDoor`, `Capability.CanOpenDoors bool`, `Capability.DoorTicks float64`.

Opening a door mutates the world, and this plan otherwise excludes mutation. The exception holds only if an opened door can never make an earlier edge illegal. **Step 1 is the test that decides that**, and if it finds a case, this task stops and the edge moves to the mutating edges plan.

- [ ] **Step 1: Write the conflict test first**

```go
func TestAnOpenedDoorNeverInvalidatesAnEarlierEdge(t *testing.T) {
	t.Parallel()

	// This is the test the design asks for, and its failure is a finding
	// rather than a bug: it would mean EdgeDoor needs the overlay and belongs
	// in the mutating amendment after all.
	//
	// A door swings into a cell. If a route walks through that cell and later
	// opens the door, the swing closes space the earlier edge relied on.
	view := scene.Fill(t, "door-swinging-into-the-route")
	capability := doorCapability(t)

	path, err := Find(t.Context(), view, capability, geom.BlockPos{X: 0, Y: 64, Z: 0},
		geom.BlockPos{X: 6, Y: 64, Z: 0}, defaultBudget())
	if err != nil {
		t.Fatalf("Find: %v", err)
	}

	if err := replayEdgesAgainstTheWorld(t, view, capability, path); err != nil {
		t.Fatalf("a door edge invalidated an earlier edge: %v.\n"+
			"This is the finding the design predicted. Stop this task, move "+
			"EdgeDoor to the mutating edges plan, and record why.", err)
	}
}
```

`replayEdgesAgainstTheWorld` walks the path applying each door toggle to a copy of the world and re-checks every earlier edge's legality. It is the same shape as the validation loop the parent design specifies, written here once to answer one question.

- [ ] **Step 2: Run it**

Run: `devbox run -- go test ./navigation/ -run TestAnOpenedDoor -v`
Expected: it compiles once `EdgeDoor` exists. **If it fails, stop.** Record the finding, move the edge to the other plan, and skip steps 3 through 6.

- [ ] **Step 3: Write the implementation**

`doors` expands into an adjacent cell whose block is a door the capability may open, recording the toggle on the edge. Iron doors and any door the version gates behind redstone are refused, not modelled: a bot that walks into an iron door forever is worse than one that routes around it.

- [ ] **Step 4: Write the refusal test**

```go
func TestAnIronDoorIsRoutedAroundNotOpened(t *testing.T) {
	t.Parallel()

	path, err := Find(t.Context(), scene.Fill(t, "iron-door-with-detour"), doorCapability(t),
		geom.BlockPos{X: 0, Y: 64, Z: 0}, geom.BlockPos{X: 6, Y: 64, Z: 0}, defaultBudget())
	if err != nil {
		t.Fatalf("Find: %v", err)
	}

	for _, edge := range path.Edges {
		if edge.Kind == EdgeDoor {
			t.Fatal("produced a door edge for an iron door")
		}
	}
	if !path.Complete {
		t.Fatalf("the detour around the iron door was not found: %v", path.Reason)
	}
}
```

- [ ] **Step 5: Run every gate**

Run: `devbox run -- task verify && devbox run -- task determinism`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add navigation/
git commit -m "feat(navigation): open doors on the route"
```

---

## Definition of done

- A body standing at the edge of a two-block gap with a jump capability routes across it, and the same body without one routes around.
- The jump reach comes from `navigation/reach`, which measures it by running each profile's own movement kernel, and the two versions measure differently.
- `navigation.Posture` has stand, sneak, swim, fall, and crawl.
- Every property the parent design lists still passes with the expanded edge set: paths contiguous, edges legal, cost monotone, and a thousand identical searches byte-identical.
- A capability with every optional edge off produces exactly the four edges that ship today.
- The 1.8.9 crawl gate reports the absence with a reason and does not pass by silence.
- Either `EdgeDoor` ships with its conflict test green, or the finding is recorded and the edge is moved.
- `devbox run -- task verify` and `task determinism` pass.

## Risks

| Risk | Mitigation |
| --- | --- |
| The jump reach is guessed rather than measured, which is what the 2026-08-17 plan refused | Task 1 is first, is its own package, and its gate fails if both versions measure the same number |
| The new edges break the frontier's total order and the determinism gate fails intermittently | `task determinism` runs at the end of tasks 2, 3, 6, and 7; the jump expansion appends in step order then increasing distance |
| Appending posture or edge constants renumbers a shipped value | Both tasks append after the existing members and say so explicitly |
| `EdgeDoor` turns out to need the overlay after it has shipped | Task 7's first step is the conflict test, before the implementation, with an explicit stop instruction |
| Task 5 starts before the climbable property exists | Marked blocked, with the extraction named as the other plan's task 1 |
