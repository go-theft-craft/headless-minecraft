# Navigation design

## Status

The user approved this design on 2026-08-17. Implementation requires the
matching implementation plan and an explicit request to execute it.

This design amends the `minecraft-simulation` design of 2026-08-13, which lists
pathfinding and mob AI as non-goals. See "Charter amendment" below.

## Purpose

Navigation gives a body a route through a world and the per-tick commands that
walk it. One implementation serves every consumer that moves: a client bot, a
server-side mob, and any later controller that has a destination and a body.

Today no such code is shared. `examples/orbit` carries 334 lines that answer
"can I stand here" — `bypass.go` and a 223-line generated `blocksMovement`
table copied into an example, because nothing in the stack exposes the fact.
`bypass.go:3` records that it is not a pathfinder and why it could not be one.
This design supplies what that example had to write by hand.

## Goals

- Answer terrain questions once, for every body, from the collision and block
  data the module already owns.
- Search a route whose edges include digging and building, so a bot that owns a
  pickaxe routes differently from one that does not.
- Produce a route a mob and a bot execute through the same follower, differing
  only by capability.
- Keep every result deterministic enough for `sim`'s digest and for the replay
  comparison M9.3 is built on.
- Bound every search so a hostile or unloaded world cannot consume unbounded
  CPU.
- Report a partial route rather than failing, so a bounded search still makes
  progress.

## Non-goals

- Goal selection. What to navigate toward is the application's decision.
- Combat strategy, scheduling, or autonomous behaviour trees.
- Light propagation. Torch placement is planned from a policy and from
  `EmitLight` and `FilterLight`; the module does not recompute the light engine.
- Fluid flow. Digging beside water or lava is refused conservatively rather
  than simulated.
- A generic graph-search library.

## Charter amendment

Three documents currently exclude this work and must change with it:

| Document | Change |
| --- | --- |
| `minecraft-simulation/README.md` package table | Add `terrain`, `navigation`, `navigator` |
| `minecraft-simulation/README.md` dependency chart | Add the branch below |
| `2026-08-13-minecraft-simulation-design.md:49` | Remove pathfinding from the non-goal list; mob AI, fish AI, combat strategy, and scheduling stay excluded |

The exclusion was not wrong when it was written. It is being changed
deliberately, and the reason is that the alternative homes are worse: a
separate module would have to re-derive passability from collision shapes that
live here, and every consumer would import a navigation module to ask whether a
block is solid.

## Package layout

```text
geom  ->  world  ->  entity  ->  movement  ->  sim  ->  runtime  ->  adapter
   \          \         ^                                               ^
    \          \-> collision                                            |
     \                \                                                 |
      \-> terrain -----/  ->  navigation  ->  navigator ----------------/
```

| Package | Responsibility |
| --- | --- |
| `terrain` | Static predicates over a world view: passability, support, fit, clearance, hazard |
| `navigation` | The speculative overlay, the edge vocabulary, capability, and the search |
| `navigator` | The follower that turns one edge into one tick's commands |

`navigation` does not import `sim`. A dig edge costs break time, and break time
arrives as a function value on `Capability` rather than as an import. That
follows the rule the module already states: a rule that needs a version's
number receives it as an argument. It keeps the search version-neutral and
keeps 1.8.9 and 26.1.2 sharing it.

`navigator` sits beside `adapter` rather than inside it. A follower is what an
`adapter.Source` asks for its commands, and both a client predict loop and a
server mob loop own such a source.

## terrain

`terrain` answers static questions about a world view. Every query takes a body
AABB rather than a height constant, so a one-block mob and a two-block player
use the same code. `examples/orbit`'s `Body = 2` becomes a parameter.

```go
type Passability uint8

const (
    Clear Passability = iota // the body fits and something holds it up
    Steppable                // one solid block with clearance above it
    Blocked                  // solid and too tall to step
    Unknown                  // the chunk is not loaded
)
```

`Unknown` is never folded into `Blocked`. Strict mode refuses those positions
and permissive mode penalizes them; treating them as walls stops a bot at the
edge of its own render distance, which `examples/orbit/bypass.go:20-24` already
records.

The package also reports:

- `Support` — whether a position is held up.
- `Fits` — whether a body box occupies a position without intersecting
  collision shapes.
- `Hazard` — lava, fire, cactus, a drop that exceeds the body's safe fall, and
  **a falling column overhead**. The last is a moving hazard; see "Falling
  blocks".

`terrain` never sees a mutation. It reads a `world.View`, and `navigation`'s
overlay is one.

## Falling blocks

Sand, gravel, and anvils fall. Digging under a column empties the whole column,
which makes a dig edge a non-local mutation and is the reason `navigation`
needs an overlay at all.

The rules, read from the 1.8.9 server sources rather than from memory:

- `BlockFalling.java:81-88` — `canFallInto` is true for fire, and for the air,
  water, and lava materials. Nothing else.
- `EntityFallingBlock.java:99-125` — on landing the entity becomes a block only
  when `canBlockBePlaced(pos)` and `!canFallInto(pos.down())`. Otherwise it
  **drops as an item**. A torch is not replaceable, so a torch in the column
  makes the falling stack pop as items. This is the `Collapse` edge.
- `BlockFalling.java:44-58` — an unloaded area resolves the fall instantly by
  scanning down and placing at the bottom. A loaded area spawns a physics
  entity. A bot digging within its own render distance must wait for that
  entity to settle.

The falling set in 1.8.9 is exactly `BlockSand`, `BlockGravel`, and
`BlockAnvil`. In 26.1.2 it is whatever extends the equivalent class; the
extraction reports it per version and no list is typed into Go.

## navigation

### The overlay

`navigation.Overlay` wraps a `world.View` and applies a path's mutations plus
their gravity cascades. A cascade is a bounded walk up one column, which is
cheap and deterministic. `terrain` reads the overlay as an ordinary view.

### Edges

| Edge | Meaning |
| --- | --- |
| `Walk` | Move to an adjacent standable position |
| `Step` | Move up one block |
| `Fall` | Descend within the body's safe fall |
| `JumpGap` | Cross a gap the movement kernel says the arc clears |
| `Swim` | Traverse fluid |
| `Dig` | Remove a block to open the route |
| `Place` | Place a block to bridge |
| `Support` | Place a block to hold a falling column before digging under it |
| `Collapse` | Place a torch, drop the column, collect the items |

Digging under an unsupported falling column without a preceding `Support` or
`Collapse` is illegal, not merely expensive. The overlay enforces it.

### Cost

Every cost is in ticks. Break time is already in ticks and movement is already
in ticks, so "torch the gravel and walk through" and "detour forty blocks" are
compared in one unit rather than through a weighting.

Resources convert at explicit rates on `Capability`: what a placed block is
worth in ticks, what a torch is worth, what tool durability is worth. A bot
holding two torches routes differently from one holding a stack, and every rate
is a number a test can pin.

### Node identity and validation

A node is `(position, posture)`, where posture is how the body occupies that
position — standing, sneaking, swimming, or falling. Two postures at one
position are distinct nodes because they differ in box, in reach, and in which
edges leave them. Keying on the mutation set would explode the state space, so
mutations ride on edges instead.

`Place` and `Support` close space, so a winning path can be internally
inconsistent: one branch's placement can occupy a position an earlier edge
relied on. `Find` therefore validates. It re-runs the overlay over the winning
path from the start, and on a conflict it bans the offending edge and searches
again. Each iteration bans one edge, so the loop terminates, and the search
itself stays a textbook A*.

### Bounds

`Find` takes a node budget and a cost ceiling. On exhaustion it returns the
best partial path with `Complete` false rather than an error. A bot that
travels most of the way and searches again beats one that refuses to move.

### Determinism

Frontier ties break on a total order over `(position, posture)`, and the
expansion loop iterates no map. Go's randomized map order would make two
identical searches disagree, which fails `sim`'s digest and the M9.3 replay
comparison. This is a gate, not a preference.

### Capability and the evaluator

`Capability` is a plain value: body box, step height, safe fall, can-swim,
can-dig, can-place, resource rates, a break-time function, and the light policy
below. A mob is the value with digging and placement off, and it gets a ground
navigator out of the same search.

An `Evaluator` interface is available for callers whose bodies or rules the
declarative value cannot express. It is an escape hatch with a stated cost: a
path computed through a custom evaluator is not deterministic from the module's
own inputs. `Path` therefore records which produced it, and the conformance
gate runs the declarative path only. A test must not be able to pass on the
unverifiable one.

### Light

Torch placement has two unrelated jobs and the design keeps them apart.

Breaking a falling column is a mechanic with the exact rule above, and it is
the `Collapse` edge.

Lighting a route against hostile spawns is a policy. `Capability` carries a
target light level and a spacing, and the cost is one torch per interval.
`EmitLight` and `FilterLight` supply the numbers. No mob sets it.

## navigator

`Follower` holds one path and its progress. It is asked once per tick and never
drives, matching what `adapter.Source` already requires.

It returns `[]sim.Command`, not `movement.Input`. A tick may need a movement
input and a dig command together, and `sim.Command` already carries both, so a
follower drops into `Source.Commands()` with nothing in between. That is what
makes a server mob and a client bot share this code: both produce a target,
call `Find`, and hand the follower to their own tick driver.

It is a state machine rather than a pure function, because three waits are
legitimate: a dig in progress, a placement settling, and a falling column in
flight. The follower re-checks the column above its next few positions every
tick and holds rather than walking into a suffocation.

Replanning returns a typed reason — `Corrected`, `Stuck`, `Blocked`,
`WorldChanged`, `Complete`, `Failed` — so a bot that walks into a wall leaves an
explanation. Correction and teleport arrive from M8.8's `predict.Correction`.
Stuck is expected-versus-actual displacement over a window, with the threshold
from the profile.

The path stays public. A stateful navigator that hid its route would be hard to
test, hard to debug, and impossible to compare against a captured trace.

## Data dependencies

| Dependency | State | Effect if missing |
| --- | --- | --- |
| `data.Block.Falling` | **Missing.** Needs `mcreference` extraction, `instanceof BlockFalling` | The search cannot tell sand from stone. `Material` will not substitute: soul sand shares `Material.sand` and does not fall |
| Break time | M9.4, planned | Dig edge costs are stubbed |
| Placement legality | M9.5, planned | Place, Support, and Collapse edges are stubbed |
| `movement` jump arcs | Exists | — |
| `collision` shapes | Exists | — |
| `EmitLight`, `FilterLight` | Exists | — |

Until M9.4 and M9.5 land, `Capability` defaults digging and placement off and
the follower refuses those edges. A bot that mines at a plausible-looking wrong
speed is worse than one that refuses to mine.

## Conformance and testing

- `terrain`: table tests over `world/fake.go`, one case per `Passability`
  value, per hazard, and for a one-block body as well as a two-block body.
- `navigation`: property tests, matching the habit `collision/property_test.go`
  already sets. A returned path is contiguous; every edge is legal under its
  capability; cost is monotone along the path; the same inputs produce the
  identical path across a thousand runs.
- The falling-block rules get their own cases, drawn from the source lines
  cited above: a column collapses, a torch drops it as items, an unloaded area
  resolves instantly, a loaded area spawns an entity.
- `navigator`: replay against `mctest` trajectories.
- Two-version gate throughout, per the module's existing rule. A scenario that
  runs on 1.8.9 and not on 26.1.2 is a failure.
- `examples/orbit` is rewritten onto `terrain`, deleting `bypass.go` and
  `blocks.go`. That deletion is the proof the layer works, and it removes a
  generated table copied into an example.

## Sequencing

1. `data.Block.Falling` extraction. Everything with a dig edge waits on it.
2. `terrain`, with the orbit example rewritten onto it. This ships alone and is
   useful alone.
3. `navigation` with the movement edges only: Walk, Step, Fall, JumpGap, Swim.
   Verifiable against static terrain, no dependency on unlanded work.
4. `navigator` over those edges. Needs M8.8's `adapter.Drive` and
   `predict.Correction`.
5. Dig, Place, Support, and Collapse, once M9.4 and M9.5 land.

Steps 1 through 3 are unblocked today. Steps 4 and 5 are not.

## Acceptance criteria

- `examples/orbit` compiles with `bypass.go` and `blocks.go` deleted and its
  behaviour unchanged.
- A thousand searches over the same inputs produce byte-identical paths.
- A search bounded below the cost of the true route returns a partial path with
  `Complete` false, and following it makes progress toward the goal.
- A capability with digging and placement off produces only movement edges, on
  both versions.
- A path that digs under a gravel column contains a `Support` or `Collapse`
  edge before the `Dig`, and the overlay refuses one that does not.
- A `Collapse` edge's expected item yield matches a captured vanilla trace.
- A path computed through a custom `Evaluator` is marked as such and is
  excluded from the conformance gate.
