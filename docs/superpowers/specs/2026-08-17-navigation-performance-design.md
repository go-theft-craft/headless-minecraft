# Navigation performance design

## Status

The user approved the decisions in this design on 2026-08-17. Implementation
requires the matching implementation plan and an explicit request to execute it.

It builds on the navigation design of 2026-08-17 and on the `terrain` and
`navigation` packages that plan delivered.

## Purpose

`navigation.Find` searches a fresh A* over concrete cells on every call. That is
correct and deterministic, and it is the wrong shape for two things a real
consumer does constantly: routing a long way, and routing again after the world
moved under it.

This design adds a `Planner` that remembers, and corrects one defect the
delivered search shipped with.

## What measurement says, and does not

No measurement exists yet. Every claim below about where the time goes is
reasoning from the code, and the first task of the plan is to establish a
baseline, because a performance change without a before is a guess.

The reasoning: `Find`'s inner loop calls `terrain.Query.Passable` once per
candidate cell, and each call runs two `collision.Gather` sweeps — one for the
body box, one for the ground probe — plus two more on the step path. Every sweep
walks cells and allocates. The frontier, by comparison, does `log n` work on a
slice. So the hot cost is terrain reads, not the heap, and a memo should beat a
faster queue. **The plan must confirm that with a profile before building the
memo**, and if the profile disagrees, this design is wrong and should be
revised rather than followed.

## Goals

- Make a long route cost far less than a full concrete search over its length,
  accepting a near-optimal route in exchange.
- Make a re-search after a small world change cost far less than the first
  search.
- Restore to `Find` the shortest-path guarantee its heuristic does not currently
  hold.
- Keep every result deterministic, and keep `Find` exactly as it is.

## Non-goals

- **D\* Lite.** It was on the approved list and this design defers it, which is
  a scope reduction the user should overrule if unwanted. Three reasons: it
  needs a consistent heuristic and a fixed goal with a moving start, which is
  the opposite of a bot whose goal moves; combining it with a cluster
  abstraction is a research-grade problem rather than an engineering one; and
  the dirty-cluster mechanism below already makes replanning cheap by
  construction. Revisit it only if measurement shows replanning is still the
  bottleneck after this lands.
- Parallelism. A single search here is too small for it to pay, and it cannot
  produce a deterministic partial path. Across-search parallelism already works:
  `Find` is pure over immutable snapshot views, so one goroutine per body needs
  no new code.
- JPS. It assumes uniform cost over a 2D grid; this search has five edge kinds
  with different prices and a vertical axis.
- Weighted A*. A bounded-suboptimal mode is a separate decision, and this design
  is restoring optimality rather than trading it.
- Changing `Find`, `Path`, `Edge`, `Reason`, or `Budget`.

## Decisions the user settled

| Decision | Choice |
| --- | --- |
| Where state lives | A stateful `Planner`; `Find` stays pure |
| How staleness is learned | Explicitly: the caller reports changed cells |
| What a long route returns | A fully refined `Path` of the same `Edge` type |

## The Planner

```go
// Planner is one body's memory of a world.
type Planner struct { /* unexported */ }

func NewPlanner(view world.View, facts terrain.Facts, capability Capability, options Options) (*Planner, error)

// Plan routes from one cell to another, using the cluster graph and the memo.
func (p *Planner) Plan(ctx context.Context, from, goal geom.BlockPos, budget Budget) (Path, error)

// Observe reports cells whose block state changed.
func (p *Planner) Observe(cells []geom.BlockPos)

// Reset drops every cached answer, for a caller that changed worlds.
func (p *Planner) Reset()
```

A `Planner` is **not safe for concurrent use**. One body owns one planner, which
is what makes across-body parallelism the free win it already is. The
constructor takes the `Capability` because every cached answer depends on the
body: a memo keyed on a cell alone is only sound when the body asking never
changes.

`Options` carries the cluster size, defaulting to 16 — a chunk's width, though
this package must not say so. `world` has no notion of a chunk and the module is
version neutral, so the number is a parameter with a documented default, not a
constant named after a Minecraft fact.

### What `Plan` guarantees, and what it does not

Two different contracts are at work here and conflating them would be the
easiest way to get this wrong.

**The memo must be transparent.** A cache that changes an answer is a bug, not
an optimization. So wherever `Plan` runs the concrete search, it must return
byte-identically what `Find` returns. That is a strict equality property and the
plan tests it directly.

**The cluster graph is not transparent, by construction.** A hierarchical search
routes through transition nodes, and the cheapest route through those nodes is
not always the cheapest route through the world — that is the trade the
abstraction exists to make. So `Plan` over distance returns a **valid but
possibly suboptimal** path, and the properties it must hold are the ones the
delivered suite already asserts: contiguous, every edge legal under the
capability, every arrival standable and never hazardous, deterministic.

This sits oddly beside the heuristic fix below, which restores optimality to
`Find`, and the oddity is worth stating rather than hiding: the two entry points
now have different contracts. `Find` is optimal and exhaustive. `Plan` is fast
and near-optimal over distance. A caller that needs a guaranteed shortest route
calls `Find`. Both are documented as such, and neither silently becomes the
other.

Short routes skip the abstraction entirely. When start and goal fall in the same
cluster, or within one of each other, `Plan` runs the concrete search directly
over the memo — so for short routes the strict equality property applies, and
the cluster graph earns its cost only across distance.

## The memo

`Planner` caches `terrain.Query.Passable` and the search's `arriveAt` answers,
keyed by cell.

### How invalidation stays correct

Deriving by hand which cells an answer depended on is fragile — `Passable` reads
the body's column, the ground below it, and on the step path a second column
above that, and a body of a different height reads a different span. A rule
written from that reasoning would be wrong the first time the body changed.

So the memo records dependencies rather than deducing them, which is what
`sim.TickState` already does one layer up: it accumulates a `Dependency` per
block it reads so a result can name what it was computed against.

The mechanism here is a recording view. `Planner` wraps its `world.View` in a
decorator that logs every cell `CollisionShape` and `BlockState` touched during
one query. The cells logged become that answer's dependency set, and a reverse
index maps each cell to the answers depending on it. `Observe(cells)` walks that
index and drops exactly those answers.

This needs no change to `terrain`, works for `Passable` and `arriveAt` alike,
and cannot drift from what the code actually reads, because it *is* what the
code actually read.

### Bounding it

An unbounded memo is a leak; a bot walks a long way. The memo takes a cell
budget from `Options` and evicts by insertion order when full. Eviction order is
deterministic and never affects an answer, only whether it must be recomputed.

## The cluster graph

Clusters partition the world on X and Z at the configured size, unbounded in Y.
Y is deliberately not partitioned: a vertical split would put a transition node
in the middle of a fall, and falls are the edges whose costs are least local.

**Entrances.** Along each border two clusters share, the builder finds maximal
runs of adjacent cell pairs that are mutually enterable, and places one
transition node at each run's midpoint. A run is a vertical-and-horizontal span,
so a doorway and a cliff edge each produce one node rather than dozens.

**Intra-cluster edges.** For each pair of a cluster's transition nodes, the
builder runs the concrete search between them, confined to that cluster, and
records the cost.

Confinement needs no change to `Find`. A view decorator reports every cell
outside the cluster as `world.LookupUnknown`, and the search already refuses to
route through unknown terrain — the rule written so a bot would not walk into a
wall it could not see turns out to be exactly the rule that keeps a
cluster-local search inside its cluster. Reusing it means the confined search is
the same code, with the same tests behind it.

**The abstract search.** A* over transition nodes, with the same frontier and
the same total node order, so the abstract layer inherits the determinism gate
rather than needing its own.

**Refinement.** Each abstract leg is refined by the confined concrete search
that produced its cost, and the legs are concatenated. Start and goal are
inserted as temporary transition nodes in their own clusters and removed
afterward.

**Dirty clusters.** `Observe` marks the clusters containing the changed cells,
and their neighbours across a shared border, as dirty. A dirty cluster's
entrances and intra-edges are recomputed the next time the abstract search needs
them, never eagerly — a bot that never returns to a cluster never pays for the
block that changed there.

## The heuristic

The delivered `cheapest()` returns the lowest cost of any single edge, and the
heuristic scales Manhattan distance by it. A fall edge covers two Manhattan
blocks — one across, one down — for one fall cost, so the scale is too high and
the heuristic overestimates. From `(0,0,0)` to a landing at `(1,-1,0)` it
predicts 6 against a true cost of 3. `Find` breaks on the first goal pop, so it
can return a route that is not shortest, and the doc comment claiming otherwise
is false.

The admissible floor is the cheapest cost **per Manhattan block**, not the
cheapest edge:

```go
min(WalkTicks, StepTicks/2, FallTicks/2, SwimTicks when CanSwim)
```

Walk covers one block for `WalkTicks`. Step covers two for `StepTicks`. A fall
of depth D covers `1+D` blocks for `FallTicks*D`, which is smallest at D=1 —
`FallTicks/2`. Swim covers one for `SwimTicks`. On the existing test capability
this yields 1.5, which makes the counterexample exactly tight.

The abstract search needs an admissible heuristic too, and the same floor
serves: an abstract leg's cost is a real concrete path's cost, so scaling
Manhattan distance between transition nodes by the same floor cannot
overestimate it.

## Testing

The properties, in the order they matter:

- **`Plan` equals `Find` where the memo alone is in play.** Over the existing
  maze seeds and every route short enough to skip the abstraction, a warm
  planner and a cold `Find` return byte-identical paths. This is the whole
  safety argument for the memo, and it must be tested with a warm cache, a cold
  cache, and a cache warmed by a *different* route through the same world.
- **`Plan` over distance is valid, not equal.** For long routes the assertion is
  the delivered property set — contiguous, every edge legal under the
  capability, every arrival standable and never hazardous — plus a recorded
  ratio of `Plan`'s cost to `Find`'s. The ratio is reported rather than bounded:
  it is the price of the abstraction, and a plan that asserted a bound would be
  inventing a guarantee HPA* does not make.
- **`Observe` restores correctness.** Build a path, change a block that breaks
  it, `Observe` the cell, re-plan, and assert the new path avoids the change —
  and that it equals what a fresh `Find` returns against the changed world.
  A test that only checks the cache was dropped would pass on a planner that
  dropped everything always.
- **Staleness is caught.** The same scenario **without** the `Observe` call must
  produce a stale path. That asserts the test above is testing something.
- **Determinism.** The existing 100-run reproducibility gate, extended to
  `Plan`, plus a run where the same plan is requested twice with an eviction
  forced between them.
- **Admissibility.** A world where a fall route is cheaper than a walk route
  that looks shorter, asserting `Find` returns the fall. This test fails against
  today's code, which is what makes it worth writing.
- **Benchmarks**, committed with their baseline: a short route, a long route
  cold, the same long route warm, and a re-plan after a one-block change.

Every gate remains two-version and race-clean, per the module's existing rules.

## Sequencing

1. Benchmarks and a profile, against today's code. Nothing else starts until
   the baseline exists and confirms terrain reads dominate.
2. The heuristic fix and its admissibility test. Independent of everything else,
   and it corrects a shipped defect.
3. The recording view and the memo, with the `Plan` equals `Find` property.
4. The cluster graph, entrances, and confined intra-cluster search.
5. The abstract search and refinement.
6. Dirty clusters and `Observe`, with the staleness pair.

Steps 1 and 2 are worth landing on their own even if the rest is reconsidered.

## Acceptance criteria

- A profile of the baseline shows where the time actually goes, and this design
  is revised if it is not terrain reads.
- `Plan` and `Find` return byte-identical paths across every maze seed and every
  route short enough to skip the cluster graph, from a cold cache, a warm cache,
  and a cache warmed by a different route.
- `Plan` over distance returns paths holding every delivered validity property,
  with its cost ratio against `Find` recorded rather than asserted.
- A long route through a warm planner costs measurably less than the same route
  through `Find`, with both numbers committed.
- A re-plan after a one-block change costs measurably less than the first plan.
- Omitting `Observe` after a world change produces a stale path — the negative
  control passes.
- A fall route cheaper than a shorter-looking walk route is returned, which
  fails before the heuristic fix and passes after.
- `Find`, `Path`, `Edge`, `Reason`, and `Budget` are unchanged.
