# Navigation: admissible heuristic and the Planner memo Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `subagent-driven-development` (recommended) or `executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Restore `navigation.Find`'s shortest-path guarantee, then add a stateful `Planner` that caches terrain answers and drops exactly the ones a reported world change invalidates.

**Architecture:** The heuristic fix is a two-line formula correction plus its doc comment. The memo is built on two seams that keep `Find` untouched: a recording `world.View` decorator that logs which cells an answer actually read, and an oracle interface the search asks its two terrain questions through. `Find` gets a direct oracle and behaves exactly as it does today; `Planner.Plan` gets a memoizing one. Invalidation walks a reverse index from cell to dependent answers, so it can never drift from what the code really reads.

**Tech Stack:** Go 1.26.6 from `openserbia/go-flake`, Devbox, Task, `golangci-lint`, `go test -bench`, `pprof`, and the existing `geom`, `world`, `collision`, `terrain`, and `navigation` packages.

## Scope

This plan covers steps 1 through 3 of the design's sequencing.

**In scope:** the admissible heuristic; benchmarks and a baseline profile; the recording view; the oracle seam; the memoizing oracle; `Planner` with `Plan`, `Observe`, and `Reset`; memo bounds.

**Out of scope, with the reason:**

| Deferred | Why |
| --- | --- |
| The cluster graph, entrances, abstract search, refinement, dirty clusters | The HPA* subsystem is its own deliverable and its own plan. It depends on the memo landing first, and the design gates it on baseline numbers that do not exist yet |
| D* Lite | Deferred in the design, with reasons, and flagged to the user there |
| Dig and place edges, and the permission and inventory inputs they need | `EdgeKind` states that these are absent rather than stubbed, so nothing here produces them. They are the reason the memo's non-view-input rule is written down now: a server may refuse a dig or a placement for reasons no block read can see, and the body's inventory and held tool change under a route that is already cached. Both are generation-counter inputs, not cells, and neither has a design yet |

## Global Constraints

- Go 1.26.6 from `openserbia/go-flake`; Devbox and Task drive every gate.
- Every change is in `minecraft-simulation`. This plan touches no other repository.
- `navigation` imports `geom`, `world`, `terrain`, and the standard library only. Never `sim`, `movement`, `entity`, `collision`, or a profile package.
- **`Find`, `Path`, `Edge`, `Reason`, and `Budget` keep their current shapes.** The oracle seam is an internal refactor; a caller must not be able to tell it happened.
- No version constant is typed into the package. Every cost and bound comes from `Capability` or `Options`.
- **No map iteration in any code path affecting an output ordering.** The memo is a map; iterating it to decide an expansion, an edge, or a path order would break the determinism gate. `TestSearchesAreReproducible` must pass after every task.
- A cache must never change an answer. Where `Plan` runs the concrete search it returns byte-identically what `Find` returns.
- The repository runs `exhaustive` (a switch over an enum needs every arm or a `default`) and `unused` (an unexported declaration with no consumer is rejected). Both have forced changes to earlier plans; when one objects, report the exact message rather than reshaping code silently.
- `devbox run -- task lint` and `devbox run -- task test` pass before every commit. `golangci-lint` shares a lock with concurrent sessions and can block for minutes — wait it out.
- Stay on `main`. No branch, no worktree. `git add navigation/` only; other sessions commit here concurrently.
- Conventional commit subjects. No `Co-Authored-By` trailer and no `Claude-Session` line.

## File Structure

All paths relative to the `minecraft-simulation` repository root.

| File | Responsibility |
| --- | --- |
| `navigation/navigation.go` | Modified: `cheapest` becomes `perBlockFloor` |
| `navigation/search.go` | Modified: heuristic doc, and the oracle seam |
| `navigation/oracle.go` | New: the `oracle` interface and the direct implementation |
| `navigation/recording.go` | New: the read-recording `world.View` decorator |
| `navigation/memo.go` | New: the memoizing oracle, its dependency index, and eviction |
| `navigation/planner.go` | New: `Options`, `Planner`, `NewPlanner`, `Plan`, `Observe`, `Reset` |
| `navigation/bench_test.go` | New: the benchmarks |
| `docs/navigation-baseline.md` | New: the committed baseline numbers and profile finding |

---

## Task 1: The admissible heuristic

**Files:**
- Modify: `navigation/navigation.go` (`cheapest`)
- Modify: `navigation/search.go` (`heuristic` doc comment)
- Modify: `navigation/edge_test.go` (`TestCheapestIsTheLowestEnabledEdgeCost`)
- Test: `navigation/search_test.go`

**Interfaces:**
- Consumes: `Capability{WalkTicks, StepTicks, FallTicks, SwimTicks, CanSwim}`
- Produces: `Capability.perBlockFloor() float64`, replacing `Capability.cheapest()`. `heuristic` is unchanged in body; only what it multiplies by changes.

**Why the test is a unit assertion rather than an end-to-end one.** The natural test would be a world where the search returns a non-shortest route. That is awkward to build: the overestimate is bounded — the old floor is `FallTicks` and the new one is `FallTicks/2` whenever falls are cheapest, so `h` is inflated by at most 2× — and a demonstrating fixture needs two routes whose costs sit inside that factor with the cheaper one running downhill. The defect is *admissibility*, so assert admissibility directly: it is a stronger, sharper check than any single fixture, and it fails today.

- [x] **Step 1: Write the failing tests**

Append to `navigation/search_test.go`:

```go
// A fall closes two blocks of Manhattan distance — one across, one down — for
// one fall's cost. A heuristic scaled by the cheapest single edge predicts two
// falls' worth of cost for that move and overestimates, which is what lets Find
// settle a goal on a non-shortest route.
func TestHeuristicNeverExceedsTheCostOfAFall(t *testing.T) {
	from := geom.BlockPos{X: 0, Y: 0, Z: 0}
	goal := geom.BlockPos{X: 1, Y: -1, Z: 0}

	trueCost := walker.FallTicks

	if got := walker.heuristic(from, goal); got > trueCost {
		t.Fatalf("heuristic = %v, exceeds the true cost %v of the fall that reaches the goal", got, trueCost)
	}
}

// The same property for a step, which closes two blocks for one step's cost.
func TestHeuristicNeverExceedsTheCostOfAStep(t *testing.T) {
	from := geom.BlockPos{X: 0, Y: 0, Z: 0}
	goal := geom.BlockPos{X: 1, Y: 1, Z: 0}

	trueCost := walker.StepTicks

	if got := walker.heuristic(from, goal); got > trueCost {
		t.Fatalf("heuristic = %v, exceeds the true cost %v of the step that reaches the goal", got, trueCost)
	}
}

// A level walk closes one block for one walk's cost, so the floor must not be
// below the walk cost for a body that can only walk. Guarding the other
// direction matters: a floor of zero would be admissible and useless, turning
// the search into Dijkstra.
func TestHeuristicIsTightForALevelWalk(t *testing.T) {
	lander := walker
	lander.StepTicks = 100
	lander.FallTicks = 100

	from := geom.BlockPos{X: 0, Y: 0, Z: 0}
	goal := geom.BlockPos{X: 3, Y: 0, Z: 0}

	want := 3 * lander.WalkTicks

	if got := lander.heuristic(from, goal); got != want {
		t.Fatalf("heuristic = %v, want %v", got, want)
	}
}
```

Replace `TestCheapestIsTheLowestEnabledEdgeCost` in `navigation/edge_test.go` with:

```go
// The heuristic multiplies Manhattan distance by this floor. It is the lowest
// cost per block of distance closed, not the lowest edge cost: a step and a
// fall each close two blocks for one edge's price. Getting it wrong makes the
// search inadmissible and it stops returning shortest paths, which no test of a
// single path would catch.
func TestPerBlockFloorIsTheLowestCostPerBlockClosed(t *testing.T) {
	walker := Capability{WalkTicks: 5, StepTicks: 9, FallTicks: 3, SwimTicks: 1}
	// Walk 5 per block, step 9/2 = 4.5, fall 3/2 = 1.5, swim disabled.
	if got := walker.perBlockFloor(); got != 1.5 {
		t.Fatalf("perBlockFloor = %v, want 1.5 (fall, swimming disabled)", got)
	}

	swimmer := walker
	swimmer.CanSwim = true
	// Swim closes one block for 1, which is below the fall's 1.5.
	if got := swimmer.perBlockFloor(); got != 1 {
		t.Fatalf("perBlockFloor = %v, want 1", got)
	}

	// A body whose walk is cheapest per block still floors on the walk.
	walkerFirst := Capability{WalkTicks: 1, StepTicks: 9, FallTicks: 9, SwimTicks: 9}
	if got := walkerFirst.perBlockFloor(); got != 1 {
		t.Fatalf("perBlockFloor = %v, want 1", got)
	}
}
```

- [x] **Step 2: Run the tests to verify they fail**

Run: `cd <minecraft-simulation> && devbox run -- go test ./navigation/ -run "Heuristic|PerBlockFloor" -v`
Expected: FAIL. `TestHeuristicNeverExceedsTheCostOfAFall` reports `heuristic = 6, exceeds the true cost 3`. `TestPerBlockFloorIsTheLowestCostPerBlockClosed` fails to compile (`perBlockFloor` undefined) — fix the compile error by writing Step 3, then re-run to see the value assertions.

- [x] **Step 3: Replace `cheapest` with `perBlockFloor`**

In `navigation/navigation.go`, replace the whole `cheapest` method:

```go
// perBlockFloor returns the lowest cost this capability can pay for one block
// of Manhattan distance closed.
//
// It is deliberately not the cheapest edge. A step closes two blocks — one
// across, one up — for one step's cost, and a fall of depth D closes 1+D blocks
// for FallTicks*D, which is cheapest per block at D=1. Scaling distance by the
// cheapest edge instead overestimates on both, and an overestimating heuristic
// lets the search settle a goal on a route that is not shortest.
func (c Capability) perBlockFloor() float64 {
	lowest := c.WalkTicks
	for _, cost := range []float64{c.StepTicks / 2, c.FallTicks / 2} {
		if cost < lowest {
			lowest = cost
		}
	}
	if c.CanSwim && c.SwimTicks < lowest {
		lowest = c.SwimTicks
	}

	return lowest
}
```

- [x] **Step 4: Correct the heuristic's doc comment and its call**

In `navigation/search.go`, replace `heuristic`'s comment and the `cheapest()` call:

```go
// heuristic estimates the remaining cost as Manhattan distance scaled by the
// lowest cost the body can pay per block of that distance.
//
// The scale is per block closed rather than per edge because a step and a fall
// each close two blocks at once. It never overestimates, which is what keeps
// the search returning shortest paths.
func (c Capability) heuristic(from, goal geom.BlockPos) float64 {
	distance := math.Abs(float64(goal.X-from.X)) +
		math.Abs(float64(goal.Y-from.Y)) +
		math.Abs(float64(goal.Z-from.Z))

	return distance * c.perBlockFloor()
}
```

- [x] **Step 5: Run the whole navigation suite**

Run: `cd <minecraft-simulation> && devbox run -- go test ./navigation/ -v`
Expected: PASS, all tests. `TestSearchesAreReproducible` must still pass — the floor is lower, so the search explores more nodes, but its ordering is unchanged.

If a pre-existing path test now reports a different path or cost, **stop and report it**. A lower floor can legitimately change which of two equal-cost routes wins, and that is a fact worth surfacing rather than absorbing.

- [x] **Step 6: Full suite and commit**

```bash
cd <minecraft-simulation>
devbox run -- task test
devbox run -- task lint
git add navigation/
git commit -m "fix(navigation): scale the heuristic per block closed, not per edge"
```

---

## Task 2: Benchmarks and the baseline — THIS IS A GATE

**Files:**
- Create: `navigation/bench_test.go`
- Create: `docs/navigation-baseline.md`

**Interfaces:**
- Consumes: `Find`, `Budget`, and `search_test.go`'s `walker`, `flat`, `maze`, `wideBudget`
- Produces: `BenchmarkFindShort`, `BenchmarkFindLong`, `BenchmarkFindMaze`, and a committed baseline document

**This task decides whether the rest of the plan is worth doing.** The design's premise is that terrain reads dominate — each `Passable` runs two `collision.Gather` sweeps, and the frontier only does `log n` work on a slice. If the profile disagrees, the memo is the wrong optimization and **the plan stops here for a design revision**. Report the finding either way; do not proceed to Task 3 on a profile that does not support it.

- [x] **Step 1: Write the benchmarks**

Create `navigation/bench_test.go`:

```go
package navigation

import (
	"context"
	"testing"

	"github.com/go-theft-craft/minecraft-simulation/geom"
	"github.com/go-theft-craft/minecraft-simulation/world"
)

// benchBudget is generous enough that no benchmark measures budget exhaustion.
var benchBudget = Budget{Nodes: 200_000, Ceiling: 200_000}

// corridor returns a flat world spanning the given half-extent in x and z.
func corridor(extent int32) *world.Blocks {
	return flat(-extent, -extent, extent, extent)
}

func BenchmarkFindShort(b *testing.B) {
	blocks := corridor(8)
	goal := geom.BlockPos{X: 4, Y: 0, Z: 0}

	b.ReportAllocs()
	for b.Loop() {
		if _, err := Find(context.Background(), blocks, nil, walker,
			geom.BlockPos{X: 0, Y: 0, Z: 0}, goal, benchBudget); err != nil {
			b.Fatalf("Find returned an error: %v", err)
		}
	}
}

func BenchmarkFindLong(b *testing.B) {
	blocks := corridor(64)
	goal := geom.BlockPos{X: 60, Y: 0, Z: 0}

	b.ReportAllocs()
	for b.Loop() {
		if _, err := Find(context.Background(), blocks, nil, walker,
			geom.BlockPos{X: 0, Y: 0, Z: 0}, goal, benchBudget); err != nil {
			b.Fatalf("Find returned an error: %v", err)
		}
	}
}

// BenchmarkFindMaze uses the property suite's obstacle fixture, so the
// benchmark measures a search that actually branches rather than one walking a
// straight line.
func BenchmarkFindMaze(b *testing.B) {
	blocks := maze(seeds[0])
	goal := geom.BlockPos{X: 11, Y: 0, Z: 11}

	b.ReportAllocs()
	for b.Loop() {
		if _, err := Find(context.Background(), blocks, nil, walker,
			geom.BlockPos{X: 0, Y: 0, Z: 0}, goal, benchBudget); err != nil {
			b.Fatalf("Find returned an error: %v", err)
		}
	}
}
```

- [x] **Step 2: Run the benchmarks and record the numbers**

Run: `cd <minecraft-simulation> && devbox run -- go test ./navigation/ -bench . -benchmem -run '^$' -count 5 | tee /tmp/nav-baseline.txt`
Expected: three benchmark lines with ns/op, B/op, allocs/op.

- [x] **Step 3: Profile the long search**

Run:

```bash
cd <minecraft-simulation>
devbox run -- go test ./navigation/ -bench BenchmarkFindLong -run '^$' -cpuprofile /tmp/nav-cpu.out
devbox run -- go tool pprof -top -nodecount=25 /tmp/nav-cpu.out
```

Read the top entries. The question to answer is narrow: **what fraction of time is under `terrain.Query.Passable`, `terrain.Query.Fits`, `terrain.Query.Ground`, and `collision.Gather`, versus under the frontier (`container/heap`, `queue.Less`, `nodeLess`) and the maps (`cameFrom`, `cost`)?**

- [x] **Step 4: Write the baseline document**

Create `docs/navigation-baseline.md` with: the exact commands run, the Go version and machine, the five-run benchmark output, the pprof top-25, and one paragraph answering the question in Step 3 — does terrain reading dominate, yes or no, with the percentages that say so.

- [x] **Step 5: Commit, then STOP and report**

```bash
cd <minecraft-simulation>
devbox run -- task lint
git add navigation/bench_test.go docs/navigation-baseline.md
git commit -m "test(navigation): add search benchmarks and record the baseline profile"
```

Then report to the controller with the verdict:

- **Terrain reads dominate** → proceed to Task 3.
- **They do not** → report `BLOCKED` with the profile. The design's premise is wrong and it needs revising before any memo is built. Do not proceed.

---

## Task 3: The recording view

**Files:**
- Create: `navigation/recording.go`
- Test: `navigation/recording_test.go`

**Interfaces:**
- Consumes: `world.View`, `world.Lookup`, `world.BlockRef`, `geom.Shape`, `geom.BlockPos`
- Produces: `recordingView` with `CollisionShape`, `BlockState`, `reset()`, and `read() []geom.BlockPos`

Deriving by hand which cells a `Passable` answer depended on would be wrong the first time a body's height changed. This decorator records what was actually read instead, which is what `sim.TickState` does one layer up with its `Dependency` list.

- [x] **Step 1: Write the failing test**

Create `navigation/recording_test.go`:

```go
package navigation

import (
	"testing"

	"github.com/go-theft-craft/minecraft-simulation/geom"
	"github.com/go-theft-craft/minecraft-simulation/terrain"
	"github.com/go-theft-craft/minecraft-simulation/world"
)

func TestRecordingViewLogsEveryCellRead(t *testing.T) {
	blocks := flat(-1, -1, 1, 1)
	recorder := &recordingView{view: blocks}

	recorder.CollisionShape(geom.BlockPos{X: 0, Y: 0, Z: 0})
	recorder.BlockState(geom.BlockPos{X: 1, Y: 0, Z: 0})

	read := recorder.read()
	if len(read) != 2 {
		t.Fatalf("read %d cells, want 2: %v", len(read), read)
	}
}

// A cell read twice is recorded once. Without this the dependency set of one
// Passable answer grows with the body's height for no benefit.
func TestRecordingViewDeduplicates(t *testing.T) {
	blocks := flat(-1, -1, 1, 1)
	recorder := &recordingView{view: blocks}

	cell := geom.BlockPos{X: 0, Y: 0, Z: 0}
	recorder.CollisionShape(cell)
	recorder.BlockState(cell)
	recorder.CollisionShape(cell)

	if read := recorder.read(); len(read) != 1 {
		t.Fatalf("read %d cells, want 1: %v", len(read), read)
	}
}

func TestRecordingViewResetClearsTheLog(t *testing.T) {
	blocks := flat(-1, -1, 1, 1)
	recorder := &recordingView{view: blocks}

	recorder.CollisionShape(geom.BlockPos{X: 0, Y: 0, Z: 0})
	recorder.reset()

	if read := recorder.read(); len(read) != 0 {
		t.Fatalf("read %d cells after reset, want 0", len(read))
	}
}

// The decorator must answer exactly what it wraps. A recorder that changed an
// answer would corrupt every cached result built on it.
func TestRecordingViewAnswersAsItsWrappedView(t *testing.T) {
	blocks := flat(-1, -1, 1, 1)
	blocks.Set(geom.BlockPos{X: 0, Y: 0, Z: 0}, geom.FullCube())
	recorder := &recordingView{view: blocks}

	for _, cell := range []geom.BlockPos{{X: 0, Y: 0, Z: 0}, {X: 1, Y: 0, Z: 0}, {X: 9, Y: 9, Z: 9}} {
		wantShape, wantLookup := blocks.CollisionShape(cell)
		gotShape, gotLookup := recorder.CollisionShape(cell)
		if gotLookup != wantLookup || gotShape.Len() != wantShape.Len() {
			t.Fatalf("CollisionShape(%v) = %v/%v, want %v/%v", cell, gotShape.Len(), gotLookup, wantShape.Len(), wantLookup)
		}

		wantRef, wantLookup := blocks.BlockState(cell)
		gotRef, gotLookup := recorder.BlockState(cell)
		if gotRef != wantRef || gotLookup != wantLookup {
			t.Fatalf("BlockState(%v) = %v/%v, want %v/%v", cell, gotRef, gotLookup, wantRef, wantLookup)
		}
	}
}

// It must satisfy world.View, since terrain.Query takes one.
var _ world.View = (*recordingView)(nil)

// And a terrain.Query built on it must work unchanged.
func TestRecordingViewDrivesATerrainQuery(t *testing.T) {
	blocks := flat(-1, -1, 1, 1)
	recorder := &recordingView{view: blocks}
	query := terrain.Query{View: recorder, Body: walker.Body}

	got, err := query.Passable(geom.BlockPos{X: 0, Y: 0, Z: 0})
	if err != nil {
		t.Fatalf("Passable returned an error: %v", err)
	}
	if got != terrain.Clear {
		t.Fatalf("Passable = %v, want Clear", got)
	}
	if len(recorder.read()) == 0 {
		t.Fatal("a Passable query recorded no reads")
	}
}
```

- [x] **Step 2: Run the tests to verify they fail**

Run: `cd <minecraft-simulation> && devbox run -- go test ./navigation/ -run TestRecordingView -v`
Expected: FAIL — `undefined: recordingView`.

- [x] **Step 3: Implement the recorder**

Create `navigation/recording.go`:

```go
package navigation

import (
	"github.com/go-theft-craft/minecraft-simulation/geom"
	"github.com/go-theft-craft/minecraft-simulation/world"
)

// recordingView is a world.View that logs the cells read through it.
//
// It exists so a cached answer can say what it was computed from without anyone
// deriving that by hand. Passable reads a body's whole column plus the ground
// below it, and a taller body reads more; a hand-written rule about which cells
// an answer depends on would be wrong the first time the body changed. This
// records what the code actually read, so it cannot drift from it.
//
// It is not safe for concurrent use, and neither is the Planner that owns one.
type recordingView struct {
	view world.View
	// seen deduplicates, and cells preserves first-read order. The order is
	// not load-bearing for correctness — invalidation drops a set — but a
	// deterministic dependency list keeps a failing test reproducible.
	seen  map[geom.BlockPos]struct{}
	cells []geom.BlockPos
}

// CollisionShape implements world.BlockView, recording the read.
func (r *recordingView) CollisionShape(pos geom.BlockPos) (geom.Shape, world.Lookup) {
	r.record(pos)

	return r.view.CollisionShape(pos)
}

// BlockState implements world.StateView, recording the read.
func (r *recordingView) BlockState(pos geom.BlockPos) (world.BlockRef, world.Lookup) {
	r.record(pos)

	return r.view.BlockState(pos)
}

// reset clears the log, ready for the next answer.
func (r *recordingView) reset() {
	clear(r.seen)
	r.cells = r.cells[:0]
}

// read returns the cells logged since the last reset, in first-read order.
func (r *recordingView) read() []geom.BlockPos {
	return r.cells
}

// record logs one cell, once.
func (r *recordingView) record(pos geom.BlockPos) {
	if r.seen == nil {
		r.seen = make(map[geom.BlockPos]struct{})
	}
	if _, ok := r.seen[pos]; ok {
		return
	}
	r.seen[pos] = struct{}{}
	r.cells = append(r.cells, pos)
}
```

- [x] **Step 4: Run the tests to verify they pass**

Run: `cd <minecraft-simulation> && devbox run -- go test ./navigation/ -run TestRecordingView -v`
Expected: PASS, five tests.

- [x] **Step 5: Commit**

```bash
cd <minecraft-simulation>
devbox run -- task test
devbox run -- task lint
git add navigation/
git commit -m "feat(navigation): record the cells a terrain answer was computed from"
```

---

## Task 4: The oracle seam

**Files:**
- Create: `navigation/oracle.go`
- Modify: `navigation/search.go` (`Find`, `expand`, `fall`, `enter`, `arriveAt`)

**Interfaces:**
- Consumes: `terrain.Query`, `terrain.Passability`, `arrival`
- Produces: `oracle` interface with `passable(geom.BlockPos) (terrain.Passability, error)` and `arriveAt(geom.BlockPos) (arrival, error)`; `directOracle` implementing it over a `terrain.Query`

**This is a pure refactor. No behaviour changes and no new tests.** The existing suite is the gate: every test must pass unchanged, including `TestSearchesAreReproducible` and every hazard test. If any test needs editing to pass, the refactor is wrong — stop and report.

The seam moves `arriveAt` off `Capability` and behind the interface, so the memo can substitute an implementation without `Find` knowing.

- [x] **Step 1: Define the oracle**

Create `navigation/oracle.go`:

```go
package navigation

import (
	"github.com/go-theft-craft/minecraft-simulation/geom"
	"github.com/go-theft-craft/minecraft-simulation/terrain"
)

// oracle answers the only two questions the search asks about a cell.
//
// It exists so a caching implementation can stand in for the direct one without
// the search knowing. Find uses the direct oracle and behaves exactly as it did
// before this seam; Planner supplies a memoizing one.
type oracle interface {
	// passable classifies a cell for the body.
	passable(cell geom.BlockPos) (terrain.Passability, error)
	// arriveAt reports whether the body may come to rest in a cell, and how.
	arriveAt(cell geom.BlockPos) (arrival, error)
}

// directOracle asks terrain every time, caching nothing.
type directOracle struct {
	query      terrain.Query
	capability Capability
}

// passable implements oracle.
func (d directOracle) passable(cell geom.BlockPos) (terrain.Passability, error) {
	return d.query.Passable(cell)
}

// arriveAt implements oracle.
func (d directOracle) arriveAt(cell geom.BlockPos) (arrival, error) {
	return d.capability.arrivalAt(d.query, cell)
}
```

- [x] **Step 2: Rename the existing gate and thread the oracle through**

In `navigation/search.go`:

1. Rename the method `func (c Capability) arriveAt(query terrain.Query, cell geom.BlockPos) (arrival, error)` to `arrivalAt`, leaving its body and doc comment otherwise unchanged. It keeps `Capability` as its receiver because it reads `c.CanSwim`.
2. Change `expand`, `fall`, and `enter` to take `oracle` in place of `terrain.Query`, and to call `o.passable(...)` and `o.arriveAt(...)` instead of `query.Passable(...)` and `c.arriveAt(query, ...)`.
3. In `Find`, replace `query := capability.query(view, facts)` with:

```go
	o := directOracle{query: capability.query(view, facts), capability: capability}
```

   and pass `o` everywhere `query` was passed. Name it `o` and not `search`: Task 6 introduces a package-level function called `search`, and a local of that name would shadow it.

Change nothing else. Every cost, every edge, every ordering stays as it is.

- [x] **Step 3: Run the whole suite unchanged**

Run: `cd <minecraft-simulation> && devbox run -- go test ./navigation/ -v`
Expected: PASS, every existing test, with no test file edited.

If any test required a change, **stop and report** — a refactor that moves behaviour is not this task.

- [x] **Step 4: Confirm the benchmarks did not regress**

Run: `cd <minecraft-simulation> && devbox run -- go test ./navigation/ -bench . -benchmem -run '^$' -count 5`
Expected: within noise of Task 2's baseline. An interface call per cell is one indirection; if it costs more than a few percent, record the number in the report.

- [x] **Step 5: Commit**

```bash
cd <minecraft-simulation>
devbox run -- task test
devbox run -- task lint
git add navigation/
git commit -m "refactor(navigation): ask terrain through an oracle the search can swap"
```

---

## Task 5: The memoizing oracle

**Files:**
- Create: `navigation/memo.go`
- Test: `navigation/memo_test.go`

**Interfaces:**
- Consumes: `oracle`, `recordingView`, `terrain.Query`, `terrain.Passability`, `arrival`, `Capability`
- Produces: `memoOracle` implementing `oracle`, with `newMemoOracle(view world.View, facts terrain.Facts, capability Capability) *memoOracle`, `invalidate(cells []geom.BlockPos)`, and `reset()`

Every cached answer is keyed by cell alone, which is sound only because one memo serves one `Capability` — a different body reads a different span and would need a different answer.

- [x] **Step 1: Write the failing test**

Create `navigation/memo_test.go`:

```go
package navigation

import (
	"testing"

	"github.com/go-theft-craft/minecraft-simulation/geom"
	"github.com/go-theft-craft/minecraft-simulation/terrain"
)

// The memo must answer exactly what the direct oracle answers, warm or cold.
// A cache that changes an answer is a bug, not an optimization.
func TestMemoAnswersAsTheDirectOracle(t *testing.T) {
	blocks := maze(seeds[0])
	direct := directOracle{query: walker.query(blocks, nil), capability: walker}
	memo := newMemoOracle(blocks, nil, walker)

	for x := int32(0); x <= 11; x++ {
		for z := int32(0); z <= 11; z++ {
			cell := geom.BlockPos{X: x, Y: 0, Z: z}

			want, err := direct.passable(cell)
			if err != nil {
				t.Fatalf("direct.passable returned an error: %v", err)
			}
			// Twice, so the second call is served from the cache.
			for range 2 {
				got, err := memo.passable(cell)
				if err != nil {
					t.Fatalf("memo.passable returned an error: %v", err)
				}
				if got != want {
					t.Fatalf("memo.passable(%v) = %v, want %v", cell, got, want)
				}
			}
		}
	}
}

func TestMemoServesTheSecondCallFromCache(t *testing.T) {
	blocks := flat(-1, -1, 1, 1)
	memo := newMemoOracle(blocks, nil, walker)
	cell := geom.BlockPos{X: 0, Y: 0, Z: 0}

	if _, err := memo.passable(cell); err != nil {
		t.Fatalf("passable returned an error: %v", err)
	}
	before := memo.misses

	if _, err := memo.passable(cell); err != nil {
		t.Fatalf("passable returned an error: %v", err)
	}
	if memo.misses != before {
		t.Fatalf("misses went from %d to %d; the second call was not cached", before, memo.misses)
	}
}

// Invalidating a cell the answer depended on must drop that answer, even though
// the cell is not the one the answer is keyed by. Passable(c) reads the ground
// under c, so changing the ground must invalidate c.
func TestMemoInvalidatesByDependency(t *testing.T) {
	blocks := flat(-1, -1, 1, 1)
	memo := newMemoOracle(blocks, nil, walker)
	cell := geom.BlockPos{X: 0, Y: 0, Z: 0}
	ground := geom.BlockPos{X: 0, Y: -1, Z: 0}

	first, err := memo.passable(cell)
	if err != nil {
		t.Fatalf("passable returned an error: %v", err)
	}
	if first != terrain.Clear {
		t.Fatalf("passable = %v, want Clear", first)
	}

	blocks.SetAir(ground)
	memo.invalidate([]geom.BlockPos{ground})

	second, err := memo.passable(cell)
	if err != nil {
		t.Fatalf("passable returned an error: %v", err)
	}
	if second != terrain.Blocked {
		t.Fatalf("passable = %v after the ground was removed, want Blocked", second)
	}
}

// The negative control: without the invalidate call the memo must return the
// stale answer. Without this, the test above would pass on a memo that cached
// nothing at all.
func TestMemoWithoutInvalidationIsStale(t *testing.T) {
	blocks := flat(-1, -1, 1, 1)
	memo := newMemoOracle(blocks, nil, walker)
	cell := geom.BlockPos{X: 0, Y: 0, Z: 0}

	if _, err := memo.passable(cell); err != nil {
		t.Fatalf("passable returned an error: %v", err)
	}

	blocks.SetAir(geom.BlockPos{X: 0, Y: -1, Z: 0})

	stale, err := memo.passable(cell)
	if err != nil {
		t.Fatalf("passable returned an error: %v", err)
	}
	if stale != terrain.Clear {
		t.Fatalf("passable = %v, want the stale Clear — the memo is not caching", stale)
	}
}

func TestMemoResetDropsEverything(t *testing.T) {
	blocks := flat(-1, -1, 1, 1)
	memo := newMemoOracle(blocks, nil, walker)
	cell := geom.BlockPos{X: 0, Y: 0, Z: 0}

	if _, err := memo.passable(cell); err != nil {
		t.Fatalf("passable returned an error: %v", err)
	}
	memo.reset()
	before := memo.misses

	if _, err := memo.passable(cell); err != nil {
		t.Fatalf("passable returned an error: %v", err)
	}
	if memo.misses == before {
		t.Fatal("the call after reset was served from cache")
	}
}

var _ oracle = (*memoOracle)(nil)
```

- [x] **Step 2: Run the tests to verify they fail**

Run: `cd <minecraft-simulation> && devbox run -- go test ./navigation/ -run TestMemo -v`
Expected: FAIL — `undefined: newMemoOracle`.

- [x] **Step 3: Implement the memo**

Create `navigation/memo.go`:

```go
package navigation

import (
	"github.com/go-theft-craft/minecraft-simulation/geom"
	"github.com/go-theft-craft/minecraft-simulation/terrain"
	"github.com/go-theft-craft/minecraft-simulation/world"
)

// passEntry is one cached Passable answer and the cells it was computed from.
type passEntry struct {
	value terrain.Passability
	deps  []geom.BlockPos
}

// arriveEntry is one cached arrival and the cells it was computed from.
type arriveEntry struct {
	value arrival
	deps  []geom.BlockPos
}

// memoOracle caches terrain answers for one body.
//
// Keying by cell alone is sound only because one memo serves one Capability: a
// body of a different height reads a different span and would need a different
// answer. NewPlanner takes the capability for this reason.
//
// Invalidation can only see what the recording view saw. An input that does
// not flow through that view leaves no dependency behind, so Observe cannot
// drop the answers computed from it and they stay stale for the planner's
// whole life. Every input a future answer reads must therefore either be read
// through the recorder, or invalidate the memo wholesale through Reset.
//
// Nothing here has such an input yet. Dig and place edges would: a server may
// refuse a break or a placement for reasons no block read can see, and the
// body's inventory and held tool decide what a place edge costs while changing
// under a route that is already cached. A denial that is genuinely per-cell is
// the easy case and needs nothing new — report the cell to Observe and the
// reverse index drops exactly the right answers. A rule that covers a region,
// or a permission the body gains and loses, is not a cell and must not be
// faked as one.
//
// It is not safe for concurrent use. One body owns one memo, which is what
// leaves across-body parallelism free.
type memoOracle struct {
	recorder   *recordingView
	query      terrain.Query
	capability Capability

	pass    map[geom.BlockPos]passEntry
	arrive  map[geom.BlockPos]arriveEntry
	// dependents maps a cell to the answers computed from it, so invalidation
	// drops exactly what a change affects rather than everything.
	dependents map[geom.BlockPos]*dependentSet

	// misses counts recomputations, for tests and for the benchmark report.
	misses int
}

// dependentSet is the answers depending on one cell, split by kind so
// invalidation touches the right map.
type dependentSet struct {
	pass   map[geom.BlockPos]struct{}
	arrive map[geom.BlockPos]struct{}
}

// newMemoOracle returns an empty memo over a view.
func newMemoOracle(view world.View, facts terrain.Facts, capability Capability) *memoOracle {
	recorder := &recordingView{view: view}

	return &memoOracle{
		recorder:   recorder,
		query:      capability.query(recorder, facts),
		capability: capability,
		pass:       make(map[geom.BlockPos]passEntry),
		arrive:     make(map[geom.BlockPos]arriveEntry),
		dependents: make(map[geom.BlockPos]*dependentSet),
	}
}

// passable implements oracle.
func (m *memoOracle) passable(cell geom.BlockPos) (terrain.Passability, error) {
	if entry, ok := m.pass[cell]; ok {
		return entry.value, nil
	}

	m.misses++
	m.recorder.reset()
	value, err := m.query.Passable(cell)
	if err != nil {
		return terrain.Unknown, err
	}

	deps := m.claim(cell, true)
	m.pass[cell] = passEntry{value: value, deps: deps}

	return value, nil
}

// arriveAt implements oracle.
func (m *memoOracle) arriveAt(cell geom.BlockPos) (arrival, error) {
	if entry, ok := m.arrive[cell]; ok {
		return entry.value, nil
	}

	m.misses++
	m.recorder.reset()
	value, err := m.capability.arrivalAt(m.query, cell)
	if err != nil {
		return refused, err
	}

	deps := m.claim(cell, false)
	m.arrive[cell] = arriveEntry{value: value, deps: deps}

	return value, nil
}

// claim copies the recorder's log and files the answer under every cell it read.
func (m *memoOracle) claim(cell geom.BlockPos, isPass bool) []geom.BlockPos {
	read := m.recorder.read()
	deps := make([]geom.BlockPos, len(read))
	copy(deps, read)

	for _, dep := range deps {
		set, ok := m.dependents[dep]
		if !ok {
			set = &dependentSet{
				pass:   make(map[geom.BlockPos]struct{}),
				arrive: make(map[geom.BlockPos]struct{}),
			}
			m.dependents[dep] = set
		}
		if isPass {
			set.pass[cell] = struct{}{}
		} else {
			set.arrive[cell] = struct{}{}
		}
	}

	return deps
}

// invalidate drops every answer computed from any of the given cells.
//
// Iterating the dependent sets is safe for determinism: invalidation decides
// what is recomputed, never what an answer is, so the order it visits them in
// cannot reach an output.
func (m *memoOracle) invalidate(cells []geom.BlockPos) {
	for _, cell := range cells {
		set, ok := m.dependents[cell]
		if !ok {
			continue
		}
		for key := range set.pass {
			m.forgetPass(key)
		}
		for key := range set.arrive {
			m.forgetArrive(key)
		}
		delete(m.dependents, cell)
	}
}

// forgetPass drops one cached Passable answer and its index entries.
func (m *memoOracle) forgetPass(cell geom.BlockPos) {
	entry, ok := m.pass[cell]
	if !ok {
		return
	}
	for _, dep := range entry.deps {
		if set, ok := m.dependents[dep]; ok {
			delete(set.pass, cell)
		}
	}
	delete(m.pass, cell)
}

// forgetArrive drops one cached arrival and its index entries.
func (m *memoOracle) forgetArrive(cell geom.BlockPos) {
	entry, ok := m.arrive[cell]
	if !ok {
		return
	}
	for _, dep := range entry.deps {
		if set, ok := m.dependents[dep]; ok {
			delete(set.arrive, cell)
		}
	}
	delete(m.arrive, cell)
}

// reset drops every cached answer.
func (m *memoOracle) reset() {
	clear(m.pass)
	clear(m.arrive)
	clear(m.dependents)
}
```

- [x] **Step 4: Run the tests to verify they pass**

Run: `cd <minecraft-simulation> && devbox run -- go test ./navigation/ -run TestMemo -v`
Expected: PASS, six tests — including the negative control, which proves the invalidation test is testing something.

- [x] **Step 5: Commit**

```bash
cd <minecraft-simulation>
devbox run -- task test
devbox run -- task lint
git add navigation/
git commit -m "feat(navigation): cache terrain answers and invalidate them by dependency"
```

---

## Task 6: The Planner

**Files:**
- Create: `navigation/planner.go`
- Test: `navigation/planner_test.go`

**Interfaces:**
- Consumes: `memoOracle`, `oracle`, `Find`'s internals, `Capability`, `Budget`, `Path`
- Produces: `Options{MemoCells int}`, `Planner`, `NewPlanner(view world.View, facts terrain.Facts, capability Capability, options Options) (*Planner, error)`, `(*Planner).Plan(ctx, from, goal, budget) (Path, error)`, `(*Planner).Observe(cells []geom.BlockPos)`, `(*Planner).Reset()`

`Find` currently builds its own oracle inline. To let `Plan` reuse the search with a different oracle, extract the loop into an unexported `search(ctx, o oracle, capability Capability, from, goal, budget)` that both call. `Find` keeps its exact signature and behaviour.

- [x] **Step 1: Write the failing test**

Create `navigation/planner_test.go`:

```go
package navigation

import (
	"context"
	"testing"

	"github.com/go-theft-craft/minecraft-simulation/geom"
)

// The property every cache in this design rests on: where Plan runs the
// concrete search it must return byte-identically what Find returns.
func TestPlanEqualsFind(t *testing.T) {
	for _, seed := range seeds {
		blocks := maze(seed)
		from := geom.BlockPos{X: 0, Y: 0, Z: 0}
		goal := geom.BlockPos{X: 11, Y: 0, Z: 11}
		budget := Budget{Nodes: 5_000, Ceiling: 5_000}

		want, err := Find(context.Background(), blocks, nil, walker, from, goal, budget)
		if err != nil {
			t.Fatalf("seed %d: Find returned an error: %v", seed, err)
		}

		planner, err := NewPlanner(blocks, nil, walker, Options{})
		if err != nil {
			t.Fatalf("seed %d: NewPlanner returned an error: %v", seed, err)
		}

		// Cold, then warm, then warm after a different route has filled the
		// cache with answers this route did not ask for.
		for run := range 3 {
			if run == 2 {
				if _, err := planner.Plan(context.Background(), goal, from, budget); err != nil {
					t.Fatalf("seed %d: warming Plan returned an error: %v", seed, err)
				}
			}

			got, err := planner.Plan(context.Background(), from, goal, budget)
			if err != nil {
				t.Fatalf("seed %d run %d: Plan returned an error: %v", seed, run, err)
			}
			assertSamePath(t, seed, run, got, want)
		}
	}
}

// assertSamePath compares two paths field by field.
func assertSamePath(t *testing.T, seed uint64, run int, got, want Path) {
	t.Helper()

	if got.Complete != want.Complete || got.Reason != want.Reason || got.Cost != want.Cost {
		t.Fatalf("seed %d run %d: summary %v/%v/%v, want %v/%v/%v",
			seed, run, got.Complete, got.Reason, got.Cost, want.Complete, want.Reason, want.Cost)
	}
	if len(got.Edges) != len(want.Edges) {
		t.Fatalf("seed %d run %d: %d edges, want %d", seed, run, len(got.Edges), len(want.Edges))
	}
	for i := range want.Edges {
		if got.Edges[i] != want.Edges[i] {
			t.Fatalf("seed %d run %d: edge %d = %+v, want %+v", seed, run, i, got.Edges[i], want.Edges[i])
		}
	}
}

// Observe must make a stale plan correct again, matching what a fresh Find says
// about the changed world.
func TestObserveRestoresCorrectness(t *testing.T) {
	blocks := flat(-1, -1, 5, 1)
	from := geom.BlockPos{X: 0, Y: 0, Z: 0}
	goal := geom.BlockPos{X: 4, Y: 0, Z: 0}

	planner, err := NewPlanner(blocks, nil, walker, Options{})
	if err != nil {
		t.Fatalf("NewPlanner returned an error: %v", err)
	}
	if _, err := planner.Plan(context.Background(), from, goal, wideBudget); err != nil {
		t.Fatalf("Plan returned an error: %v", err)
	}

	// Wall off the straight line at x=2, z=0.
	changed := []geom.BlockPos{{X: 2, Y: 0, Z: 0}, {X: 2, Y: 1, Z: 0}}
	for _, cell := range changed {
		blocks.Set(cell, geom.FullCube())
	}
	planner.Observe(changed)

	got, err := planner.Plan(context.Background(), from, goal, wideBudget)
	if err != nil {
		t.Fatalf("Plan returned an error: %v", err)
	}
	want, err := Find(context.Background(), blocks, nil, walker, from, goal, wideBudget)
	if err != nil {
		t.Fatalf("Find returned an error: %v", err)
	}
	assertSamePath(t, 0, 0, got, want)

	for _, edge := range got.Edges {
		if edge.To == (geom.BlockPos{X: 2, Y: 0, Z: 0}) {
			t.Fatal("the replanned route walks through the new wall")
		}
	}
}

// The negative control. Without the Observe call the plan must stay stale —
// otherwise the test above would pass on a planner that caches nothing.
func TestWithoutObserveThePlanIsStale(t *testing.T) {
	blocks := flat(-1, -1, 5, 1)
	from := geom.BlockPos{X: 0, Y: 0, Z: 0}
	goal := geom.BlockPos{X: 4, Y: 0, Z: 0}

	planner, err := NewPlanner(blocks, nil, walker, Options{})
	if err != nil {
		t.Fatalf("NewPlanner returned an error: %v", err)
	}
	if _, err := planner.Plan(context.Background(), from, goal, wideBudget); err != nil {
		t.Fatalf("Plan returned an error: %v", err)
	}

	for _, cell := range []geom.BlockPos{{X: 2, Y: 0, Z: 0}, {X: 2, Y: 1, Z: 0}} {
		blocks.Set(cell, geom.FullCube())
	}
	// Deliberately no Observe call — that is what this test is about.

	stale, err := planner.Plan(context.Background(), from, goal, wideBudget)
	if err != nil {
		t.Fatalf("Plan returned an error: %v", err)
	}

	var through bool
	for _, edge := range stale.Edges {
		if edge.To == (geom.BlockPos{X: 2, Y: 0, Z: 0}) {
			through = true
		}
	}
	if !through {
		t.Fatal("the un-Observed plan avoided the new wall; the planner is not caching")
	}
}

func TestNewPlannerRefusesABodilessCapability(t *testing.T) {
	if _, err := NewPlanner(flat(-1, -1, 1, 1), nil, Capability{}, Options{}); err == nil {
		t.Fatal("NewPlanner accepted a capability with no body")
	}
}

// Reset must return the planner to a cold cache.
func TestResetDropsTheCache(t *testing.T) {
	blocks := flat(-1, -1, 5, 1)
	planner, err := NewPlanner(blocks, nil, walker, Options{})
	if err != nil {
		t.Fatalf("NewPlanner returned an error: %v", err)
	}
	from := geom.BlockPos{X: 0, Y: 0, Z: 0}
	goal := geom.BlockPos{X: 4, Y: 0, Z: 0}

	if _, err := planner.Plan(context.Background(), from, goal, wideBudget); err != nil {
		t.Fatalf("Plan returned an error: %v", err)
	}
	planner.Reset()
	before := planner.memo.misses

	if _, err := planner.Plan(context.Background(), from, goal, wideBudget); err != nil {
		t.Fatalf("Plan returned an error: %v", err)
	}
	if planner.memo.misses == before {
		t.Fatal("the plan after Reset was served from cache")
	}
}
```

- [x] **Step 2: Run the tests to verify they fail**

Run: `cd <minecraft-simulation> && devbox run -- go test ./navigation/ -run "TestPlan|TestObserve|TestWithoutObserve|TestNewPlanner|TestReset" -v`
Expected: FAIL — `undefined: NewPlanner`.

- [x] **Step 3: Extract the search loop**

In `navigation/search.go`, change `Find` so its body after the `ErrNoBody` guard becomes a call to a new unexported function, and move the loop into it:

```go
// search is the A* both Find and Planner.Plan run. It is separate from Find so
// that a planner can supply a caching oracle without Find changing shape.
func search(
	ctx context.Context,
	o oracle,
	capability Capability,
	from, goal geom.BlockPos,
	budget Budget,
) (Path, error) {
	// Body moved verbatim from Find, below.
}
```

This is a **move, not a rewrite**. Take every line of the current `Find` body from `start := node{Pos: from, Posture: PostureStand}` through the closing `return assemble(...)`, and paste it unchanged into `search`. The only edits are mechanical: the `ErrNoBody` guard and the `o := directOracle{...}` line from Task 4 stay behind in `Find`, and the oracle parameter is already named `o` so no identifier changes. Do not retype the loop from memory and do not "tidy" it on the way — a moved body that differs by one comparison is the hardest kind of regression to find, and `TestPlanEqualsFind` in Step 5 is the only thing that would catch it.

`Find` becomes:

```go
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

	return search(ctx, directOracle{query: capability.query(view, facts), capability: capability}, capability, from, goal, budget)
}
```

- [x] **Step 4: Implement the Planner**

Create `navigation/planner.go`:

```go
package navigation

import (
	"context"

	"github.com/go-theft-craft/minecraft-simulation/geom"
	"github.com/go-theft-craft/minecraft-simulation/terrain"
	"github.com/go-theft-craft/minecraft-simulation/world"
)

// Options configures a Planner. A zero Options takes every default.
type Options struct {
	// MemoCells bounds how many cached answers of each kind the planner keeps.
	// A non-positive value takes the default.
	MemoCells int
}

// Planner is one body's memory of one world.
//
// Find searches fresh every time, which is correct and is the wrong shape for a
// body that routes repeatedly through terrain that mostly did not change. A
// Planner caches what terrain said and drops exactly the answers a reported
// change invalidates.
//
// A Planner is NOT safe for concurrent use, and it is bound to one Capability:
// its cached answers are keyed by cell, which is sound only while the body
// asking stays the same. One body owns one planner — which is also what leaves
// across-body parallelism free, since Find and Plan share no mutable state
// between planners.
//
// The caller is responsible for reporting world changes through Observe. A
// planner that is never told cannot know, and will happily return a route
// through a wall built after its last look.
type Planner struct {
	capability Capability
	memo       *memoOracle
}

// NewPlanner returns a planner for one body over one view.
func NewPlanner(view world.View, facts terrain.Facts, capability Capability, options Options) (*Planner, error) {
	if capability.Body.HalfWidth <= 0 || capability.Body.Height <= 0 {
		return nil, ErrNoBody
	}

	return &Planner{
		capability: capability,
		memo:       newMemoOracle(view, facts, capability),
	}, nil
}

// Plan routes from one cell to another.
//
// It returns what Find returns for the same inputs. The cache changes how long
// that takes, never what it says.
func (p *Planner) Plan(ctx context.Context, from, goal geom.BlockPos, budget Budget) (Path, error) {
	return search(ctx, p.memo, p.capability, from, goal, budget)
}

// Observe reports cells whose block state changed, dropping every cached answer
// computed from any of them.
//
// A caller knows exactly which cells moved: a client receives block-change
// packets and a server owns its own edits. Nothing here scans for changes,
// because scanning would cost more than the cache saves.
func (p *Planner) Observe(cells []geom.BlockPos) {
	p.memo.invalidate(cells)
}

// Reset drops every cached answer, for a caller whose world changed wholesale
// — and for one whose answers depended on something the recording view never
// saw, which Observe cannot reach. See memoOracle's doc comment.
func (p *Planner) Reset() {
	p.memo.reset()
}
```

- [x] **Step 5: Run the tests to verify they pass**

Run: `cd <minecraft-simulation> && devbox run -- go test ./navigation/ -v`
Expected: PASS — every new planner test and every pre-existing test, unchanged.

`TestPlanEqualsFind` is the one to watch. If it fails, the memo is changing an answer and that is a Critical defect, not a tuning problem: **stop and report the seed, the diverging edge, and both paths.**

- [x] **Step 6: Commit**

```bash
cd <minecraft-simulation>
devbox run -- task test
devbox run -- task lint
git add navigation/
git commit -m "feat(navigation): add a Planner that caches terrain across searches"
```

---

## Task 7: Bounding the memo

**Files:**
- Modify: `navigation/memo.go`
- Modify: `navigation/planner.go` (pass `Options.MemoCells` through)
- Test: `navigation/memo_test.go`

**Interfaces:**
- Consumes: `memoOracle`, `Options`
- Produces: `newMemoOracle` gains a `limit int` parameter; eviction by insertion order

An unbounded memo is a leak — a bot walks a long way, and every cell it passes stays cached forever. Eviction order must never affect an answer, only whether it must be recomputed.

- [x] **Step 1: Write the failing test**

Append to `navigation/memo_test.go`:

```go
func TestMemoEvictsWhenFull(t *testing.T) {
	blocks := flat(-1, -1, 20, 1)
	memo := newMemoOracle(blocks, nil, walker, 4)

	for x := int32(0); x < 8; x++ {
		if _, err := memo.passable(geom.BlockPos{X: x, Y: 0, Z: 0}); err != nil {
			t.Fatalf("passable returned an error: %v", err)
		}
	}

	if len(memo.pass) > 4 {
		t.Fatalf("cached %d answers, want at most 4", len(memo.pass))
	}
}

// Eviction must not leave dangling index entries, or invalidate would walk a
// reverse index that outlived the answers it points at and grow without bound.
func TestMemoEvictionCleansTheDependencyIndex(t *testing.T) {
	blocks := flat(-1, -1, 20, 1)
	memo := newMemoOracle(blocks, nil, walker, 2)

	for x := int32(0); x < 8; x++ {
		if _, err := memo.passable(geom.BlockPos{X: x, Y: 0, Z: 0}); err != nil {
			t.Fatalf("passable returned an error: %v", err)
		}
	}

	for cell, set := range memo.dependents {
		for key := range set.pass {
			if _, ok := memo.pass[key]; !ok {
				t.Fatalf("cell %v still indexes evicted answer %v", cell, key)
			}
		}
	}
}

// Eviction changes only whether an answer is recomputed, never what it is.
func TestMemoEvictionDoesNotChangeAnswers(t *testing.T) {
	blocks := maze(seeds[0])
	direct := directOracle{query: walker.query(blocks, nil), capability: walker}
	memo := newMemoOracle(blocks, nil, walker, 2)

	for x := int32(0); x <= 11; x++ {
		for z := int32(0); z <= 11; z++ {
			cell := geom.BlockPos{X: x, Y: 0, Z: z}

			want, err := direct.passable(cell)
			if err != nil {
				t.Fatalf("direct.passable returned an error: %v", err)
			}
			got, err := memo.passable(cell)
			if err != nil {
				t.Fatalf("memo.passable returned an error: %v", err)
			}
			if got != want {
				t.Fatalf("memo.passable(%v) = %v, want %v", cell, got, want)
			}
		}
	}
}
```

Update the existing `newMemoOracle` calls in `memo_test.go` to pass a limit of `0`, meaning the default.

- [x] **Step 2: Run to verify it fails**

Run: `cd <minecraft-simulation> && devbox run -- go test ./navigation/ -run TestMemoEvict -v`
Expected: FAIL to compile — `newMemoOracle` takes three arguments.

- [x] **Step 3: Add the bound**

In `navigation/memo.go`:

1. Add the default and the field:

```go
// defaultMemoCells bounds each cache when Options does not. It is a count of
// cells, not bytes: an entry is a small value plus its dependency list, and a
// body's working set over a few chunks is well inside this.
const defaultMemoCells = 16_384
```

2. Add to `memoOracle`: `limit int`, `passOrder []geom.BlockPos`, `arriveOrder []geom.BlockPos`.

3. Change the constructor signature to `newMemoOracle(view world.View, facts terrain.Facts, capability Capability, limit int) *memoOracle`, setting `limit` to `defaultMemoCells` when the argument is non-positive.

4. In `passable`, after `m.pass[cell] = ...`, append the key and evict:

```go
	m.passOrder = append(m.passOrder, cell)
	for len(m.pass) > m.limit && len(m.passOrder) > 0 {
		oldest := m.passOrder[0]
		m.passOrder = m.passOrder[1:]
		m.forgetPass(oldest)
	}
```

5. Do the same in `arriveAt` with `arriveOrder` and `forgetArrive`.

6. In `reset`, clear both order slices.

In `navigation/planner.go`, pass the option through:

```go
		memo:       newMemoOracle(view, facts, capability, options.MemoCells),
```

- [x] **Step 4: Run the tests to verify they pass**

Run: `cd <minecraft-simulation> && devbox run -- go test ./navigation/ -v`
Expected: PASS, everything — including `TestPlanEqualsFind`, which now exercises a bounded cache.

- [x] **Step 5: Commit**

```bash
cd <minecraft-simulation>
devbox run -- task test
devbox run -- task lint
git add navigation/
git commit -m "feat(navigation): bound the memo and evict in insertion order"
```

---

## Task 8: Measure the result and record it

**Files:**
- Modify: `navigation/bench_test.go`
- Modify: `docs/navigation-baseline.md`
- Modify: `README.md`
- Modify: `CHANGELOG.md`

**Interfaces:**
- Consumes: `NewPlanner`, `Plan`, `Observe`, and the Task 2 benchmarks
- Produces: `BenchmarkPlanCold`, `BenchmarkPlanWarm`, `BenchmarkPlanAfterChange`, and the recorded comparison

A performance change without an after is as much a guess as one without a before.

- [x] **Step 1: Add the planner benchmarks**

Append to `navigation/bench_test.go`:

```go
func BenchmarkPlanCold(b *testing.B) {
	blocks := corridor(64)
	goal := geom.BlockPos{X: 60, Y: 0, Z: 0}

	b.ReportAllocs()
	for b.Loop() {
		planner, err := NewPlanner(blocks, nil, walker, Options{})
		if err != nil {
			b.Fatalf("NewPlanner returned an error: %v", err)
		}
		if _, err := planner.Plan(context.Background(),
			geom.BlockPos{X: 0, Y: 0, Z: 0}, goal, benchBudget); err != nil {
			b.Fatalf("Plan returned an error: %v", err)
		}
	}
}

func BenchmarkPlanWarm(b *testing.B) {
	blocks := corridor(64)
	goal := geom.BlockPos{X: 60, Y: 0, Z: 0}
	from := geom.BlockPos{X: 0, Y: 0, Z: 0}

	planner, err := NewPlanner(blocks, nil, walker, Options{})
	if err != nil {
		b.Fatalf("NewPlanner returned an error: %v", err)
	}
	if _, err := planner.Plan(context.Background(), from, goal, benchBudget); err != nil {
		b.Fatalf("warming Plan returned an error: %v", err)
	}

	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		if _, err := planner.Plan(context.Background(), from, goal, benchBudget); err != nil {
			b.Fatalf("Plan returned an error: %v", err)
		}
	}
}

// The replanning case: one block changes, the planner is told, and it routes
// again. This is what a follower does when the world moves under it.
func BenchmarkPlanAfterChange(b *testing.B) {
	blocks := corridor(64)
	goal := geom.BlockPos{X: 60, Y: 0, Z: 0}
	from := geom.BlockPos{X: 0, Y: 0, Z: 0}
	changed := []geom.BlockPos{{X: 30, Y: 0, Z: 1}}

	planner, err := NewPlanner(blocks, nil, walker, Options{})
	if err != nil {
		b.Fatalf("NewPlanner returned an error: %v", err)
	}
	if _, err := planner.Plan(context.Background(), from, goal, benchBudget); err != nil {
		b.Fatalf("warming Plan returned an error: %v", err)
	}

	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		planner.Observe(changed)
		if _, err := planner.Plan(context.Background(), from, goal, benchBudget); err != nil {
			b.Fatalf("Plan returned an error: %v", err)
		}
	}
}
```

- [x] **Step 2: Run every benchmark and compare**

Run: `cd <minecraft-simulation> && devbox run -- go test ./navigation/ -bench . -benchmem -run '^$' -count 5`

- [x] **Step 3: Record the comparison**

Update `docs/navigation-baseline.md` with an "After" section: the same table, the delta against the baseline, and one paragraph of plain assessment. **If warm planning is not meaningfully faster than `Find`, say so.** A memo that does not pay is a finding, not a failure to hide — report it and let the controller decide whether Task 7's bound, the dependency-set size, or the design's premise is at fault.

- [x] **Step 4: Document the Planner**

In `README.md`, extend the `navigation` package-table row to mention the planner, leaving every other row untouched:

```markdown
| `navigation` | The edge vocabulary, a body's capability, a bounded deterministic route search, and a planner that caches terrain across searches |
```

In `CHANGELOG.md`, under `## Unreleased` → `### Added`:

```markdown
- `navigation.Planner`: a per-body cache of terrain answers, invalidated by the
  cells a caller reports through `Observe`. Where it runs the concrete search it
  returns byte-identically what `Find` returns; the cache changes how long an
  answer takes, never what it says.
```

And under `### Fixed`, creating the section if it does not exist:

```markdown
- `navigation`: the search heuristic scaled Manhattan distance by the cheapest
  single edge, but a step and a fall each close two blocks of distance for one
  edge's cost, so it overestimated and the search could return a route that was
  not shortest. It now scales by the lowest cost per block closed.
```

- [x] **Step 5: Verify everything and commit**

```bash
cd <minecraft-simulation>
devbox run -- task verify
git add navigation/ docs/navigation-baseline.md README.md CHANGELOG.md
git commit -m "test(navigation): benchmark the planner and record the measured result"
```

---

## What this plan does not deliver

- No cluster graph, no abstract search, no HPA*. Long routes still run a full concrete search; they are merely cheaper per cell. That is the next plan, and it is gated on Task 2's baseline.
- No D* Lite, deferred in the design with reasons.
- `Plan` and `Find` return identical paths in every case here, because no abstraction is in play yet. The design's "valid but possibly suboptimal" contract for `Plan` only starts to apply when the cluster graph lands.
- No change to `terrain`. Every seam added here lives in `navigation`.

---

## Execution record, 2026-08-18

All eight tasks landed in `minecraft-simulation`, `32d753c` through `a51820d`.
Every commit passed `task test` and `task lint`; Task 8 passed `task verify`.

**Task 2's gate: terrain reading dominates, so the memo was worth building.**
`terrain.Query.Passable` is 57.48% of `BenchmarkFindLong` cumulative and
`arrivalAt` another 10.24%, against 6–8% for the frontier and under 9% for the
search's own maps. `docs/navigation-baseline.md` has the numbers.

**The measured result.** A warm `Plan` is 2.11× faster than a fresh `Find` on
half the allocations. A cold `Plan` is 1.31× *slower* — filling the memo costs
39,000 allocations `Find` never makes, so a caller that plans once should call
`Find`. `Observe` of one changed cell costs nothing measurable: the replan comes
out level with the warm case.

Three deviations from the plan as written, all forced:

| Deviation | Why |
| --- | --- |
| `Find` declares `var o oracle = directOracle{...}` rather than `o := directOracle{...}` | The plan's form converted to the interface at every `expand` call. `directOracle` is wider than a word, so that boxed it on the heap once per node expanded — `BenchmarkFindLong` went from 21,263 to 24,263 allocations. Declaring the variable as the interface converts once and returns the count to baseline |
| `property_test.go`'s `search` helper is renamed `findPath` | Task 6 introduces a package-level `search`, and test files share the package. Two declarations of one name do not compile. The plan anticipated a local shadowing `search` but not an existing helper holding the name |
| Task 7's memo bound was committed before Task 6's `Planner` | `revive` rejected `NewPlanner`'s `options` parameter as unused while `newMemoOracle` still took no limit: `unused-parameter: parameter 'options' seems to be unused`. Since lint gates every commit, `Options` could not exist un-wired in any committed state. Landing the bound first made the wiring available; both commits are self-consistent and the end state is what the plan specifies |
