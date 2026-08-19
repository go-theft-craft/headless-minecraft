# Baritone adoption design

## Status

Drafted 2026-08-18. This is an adoption survey and sequencing decision, not a
component design: each numbered stage below still requires its own focused
design and implementation plan before any code is written. It follows the
[navigation design](2026-08-17-navigation-design.md) and the
[composed behaviours design](2026-08-18-composed-behaviours-design.md) and
contradicts neither. It starts after the navigation pillar's pending releases
land, because stage 1 changes `navigation`'s public surface.

## Purpose

Baritone is the reference pathfinding bot for Java Minecraft: a decade of
accumulated decisions about what a bot needs to travel, dig, and remember.
This document walks its public feature catalogue (`FEATURES.md` and `USAGE.md`
in the Baritone repository) against what this project already has, and records
three things for each feature: implement, borrow the idea in another shape, or
skip. It then fixes the order.

The survey covers the public stack — `minecraft-simulation` and this
repository. The legacy proxy is covered by a companion document in its own
repository; this document records only the decisions that bind both (see "The
legacy proxy" below).

## Decisions that shape every verdict

**Full execution fidelity.** Borrowed movement is not limited to what a human
at a keyboard could produce. Tick-perfect sprint-jumps, parkour placement, and
instant rotation are all in scope for the public stack, which targets servers
the operator runs. This decision does not extend to the legacy proxy.

**The layering already decided elsewhere holds.** Search and edges live in
`minecraft-simulation/navigation`; multi-tick tasks live in `behaviour`; goal
*selection* — what the bot should want — stays an application concern, exactly
as the behaviour package's own documentation says. Several Baritone features
(waypoints, chat control, `#goto death`) are goal selection in disguise and are
recorded as application concerns, not library gaps.

**Borrow the architecture, not the module.** The legacy proxy mirrors the
designs proven here; no code is shared, because its physics and world types are
its own and generalizing the public module to fit them would leak
foreign-shaped abstractions into public code.

## Where the project already stands

Verified against the code on 2026-08-18:

- `navigation.Find` is a budgeted A* that returns **incomplete paths with a
  reason** (`Path.Complete`, `Reason`), so Baritone's "segmented calculation"
  is half-covered already: a search that runs out of budget yields the best
  partial route rather than failing.
- The edge vocabulary covers walking, stepping, jump-gaps, falling, swimming,
  pillaring, placing, doors, and vertical movement. **Dig, Support, and
  Collapse are designed but deliberately absent**, blocked on break-time
  numbers (`edge.go`'s own comment records this).
- `behaviour` has Follow, Flee, Fish, Eat, Block, Dig, Build, Sequence, and
  StripMine, all under the asked-once-per-tick contract.
- `predict` already detects the divergence between intended and observed
  movement that path-execution recovery needs.
- `world` stores chunk sections undecoded and decodes on demand; there is no
  compressed long-range pathing view and no memory beyond loaded chunks.

## Adoption map

### Navigation layer (`minecraft-simulation/navigation`)

| Baritone feature | Verdict |
| --- | --- |
| Budgeted A*, partial paths | Already done |
| Pillar, scaffold placement, doors, ladders, water | Already done |
| Goal abstraction (GoalXZ, GoalYLevel, GoalNear, GoalGetToBlock, GoalRunAway, GoalComposite, GoalInverted, GoalAxis) | **Implement** — stage 1 |
| Dig edges: breaking blocks as a path cost, tool-aware | **Implement** — stage 2 |
| Hazard soft costs (lava, fire, magma, liquid-adjacent) | **Implement** — rides with stage 1 |
| Sprint and sprint-jump over 1–3 block gaps; parkour place | **Implement** — stage 6 |
| Fall repertoire: 3-block falls, falls into water | **Implement** — with stage 6 |
| 23-block fall with water-bucket placement | Borrow idea, defer: needs mid-air item use in the executor; high flair, low utility |
| Incremental cost backoff, backtrack splicing, minimum-improvement repropagation | Borrow selectively when long-distance travel lands; these are executor heuristics more than search features |

Goal abstraction is Baritone's single best structural idea: a goal is a
heuristic plus a completion test, and every process falls out of it. `Find`
today takes one exact `geom.BlockPos`; after stage 1 it takes a goal, and an
exact position is merely the simplest goal.

Dig edges are the single most Baritone-defining capability — a mountain is
just a costed region. The design already exists in `edge.go`'s comment; the
work is the break-time numbers the `mining` package owes and the state-space
rule the frontier comment already fixes (mutations recorded on edges, never in
node keys).

### Path execution (this repository)

| Baritone feature | Verdict |
| --- | --- |
| Execute the current segment while the next computes; splice on arrival | **Implement** — stage 3, as a `Navigate` behaviour. The search runs under its node budget across ticks; no goroutine, per the behaviour contract |
| Deviation recovery: knockback or correction → re-path from here | **Implement** — stage 3, on top of `predict`. Baritone's insight is that re-path-from-here is the only recovery; there is no "get back on the old path" |
| Tool selection for break edges | **Implement** — with stage 2/3; `behaviour` digging already selects tools, navigation needs the matching cost |

### World memory (this repository, `world`)

| Baritone feature | Verdict |
| --- | --- |
| Compressed pathing view of chunks (Baritone uses 2 bits: air, solid, water, avoid) and a RAM cache beyond view distance | **Implement** — stage 4. The undecoded-section design finally gets its second consumer |
| Disk persistence of the cache | Borrow idea, defer: an application policy (where, how large). Design the cache so persistence is attachable |
| Cached block-location index (remembered ore positions, Baritone's `find`) | **Implement** — stage 4, alongside the cache |

### Behaviours and processes

| Baritone process | Verdict |
| --- | --- |
| `mine <block> [count]`: scan memory, path to nearest, dig, repeat | **Implement** — stage 5; the flagship composition of stages 1–4 |
| `explore`: visit unvisited chunks systematically | **Implement** — stage 5; falls out of the cache knowing which chunks exist |
| `follow` / flee by pathing rather than steering | **Upgrade** existing behaviours once GoalNear/GoalRunAway exist |
| `tunnel` | Already done (StripMine) |
| `build` from schematics | Already done and continuing under its own plans; borrow only Baritone's material-sourcing pause, later |
| `farm` | Borrow idea, low priority; self-contained behaviour with no prerequisite beyond goals |
| Surface, axis, `thisway` travel | Free once goals exist; not separate work |
| Waypoints, death-position goto | Application concern: state and naming, no library mechanics |
| Elytra flight | Skip for now: a separate flight engine, one protocol only; its own pillar if ever |

### Skip entirely

Chat command control, on-screen selections, chunk-render repair, Schematica
integration, camera-relative `come`, and pig riding: GUI-client features or
novelties, meaningless headless or deliberately application-level here.

## Implementation order

Foundation-first along the dependency spine, matching how every previous
pillar was sequenced: numbers first, edges second, consumers last. Each stage
is one plan-sized unit with its own design.

1. **Goals** in `navigation`, hazard soft costs riding along. Small, pure, no
   physics numbers owed; unblocks everything below.
2. **Dig edges** in `navigation`, paying the break-time debt `edge.go`
   records, with the `mining` package supplying the numbers.
3. **Navigate behaviour** here: segmented execution across ticks, deviation
   recovery through `predict`. The stack becomes usable end to end.
4. **Chunk cache and block index** in `world`: the compressed pathing view,
   persistence attachable but not built.
5. **Mine and Explore behaviours**: thin compositions of 1–4; the visible
   payoff.
6. **Parkour and sprint-jump edges**: last, because nothing depends on them;
   certified per version by the existing replay and oracle harness.

The legacy proxy mirrors each stage one step behind, in its own repository,
under its own plans.

## The legacy proxy

The proxy targets a server its operator does not run and which validates
movement, so two public-stack decisions flip there, and both are recorded here
because they bind the shared sequencing:

- **Execution stays conservative** in the proxy: no tick-perfect parkour, no
  instant rotation. The full-fidelity decision above is public-stack only.
- **Disk persistence of world memory is first-class** there rather than
  deferred, because the proxy is a long-lived service against one persistent
  world.

Everything else about the proxy's adoption — its feature list, its grounding
in code, its order — lives in the companion document in the legacy proxy
repository, which names things this repository deliberately does not.

## Testing

Nothing in this document introduces a new kind of test. Goals and edges are
exercised by the navigation package's existing property, replay, and oracle
suites, extended per stage; behaviours are exercised snapshot-in,
actions-out, as the behaviour package already requires; the cache is exercised
against captured chunk fixtures. Each stage's own design says which suites it
extends.
