# Mutating edge amendment

## Status

Drafted 2026-08-18. Amends the
[navigation design](2026-08-17-navigation-design.md), which stands except where
this document says otherwise. Implementation requires the matching
implementation plan and an explicit request to execute it.

## Purpose

The parent design already owns most of this. `navigation.Overlay`, the `Dig`,
`Place`, `Support`, and `Collapse` edges, resource rates on `Capability`, the
re-run-and-ban validation loop, and the falling-column rules are all specified
there, and step 5 of its sequencing holds them behind M9.4 and M9.5. None of it
is built.

This document does three things the parent does not: it records what is
actually missing before step 5 can start, it adds the one edge the parent's
list has no member for, and it bounds a search that can now change the world it
is searching.

**The missing edge is vertical.** `Place` bridges horizontally and `Support`
holds a column from below before a dig. Nothing in the list gains height. A bot
that can place blocks but cannot pillar up is still bounded by `StepHeight` and
a jump arc, which is the same ceiling a bot with no blocks at all has, and
"pillar up to the surface" is the ordinary answer to being underground.

## Goals

- Add `Pillar`, and bound the state-space explosion it causes.
- Record the parent design's unmet prerequisites concretely, with the
  repository each one lands in.
- Keep every rule this design cannot verify refusing rather than guessing.

## Non-goals

- Re-specifying the overlay, the cascade, the validation loop, the cost model,
  or the falling-column rules. The parent design holds all five and they stand.
- Break times and placement legality. Those are M9.4 and M9.5, and this design
  ships no number either of them owns.
- Scaffolding, retrieval of placed blocks, and route cleanup. A bot that
  pillars up and leaves the pillar is behaving correctly for this design.

## Pillar

A `Pillar` edge rises one block by placing a block into the position the body
just left.

It is not a special case of `Place`. `Place` puts a block into a cell the body
will walk across; `Pillar` puts one into the cell the body is standing in,
while the body is above it, and the body arrives one block higher. The
preconditions differ, the resulting node differs, and the failure modes differ.

Its preconditions:

- The cell above the body admits the body's box, from `Query.Fits`. A bot
  cannot pillar into a ceiling.
- The cell the block goes into has a face to place against, which after the
  first block of a pillar is the pillar itself.
- The capability can place, and the resource rate accounts for one block.

It depends on `PostureFall` from the
[edge completion amendment](2026-08-18-navigation-edge-completion-design.md),
because the body is airborne at the moment of placement. That is the one
ordering constraint between the two amendments; everything else in them is
independent.

### Descending is not the inverse

A pillar cannot be walked back down. Coming down is `Fall` if the drop is
within `SafeFall`, and `Dig` beneath the body otherwise, which is a different
edge with a different cost and a tool requirement.

The search must not treat `Pillar` as reversible. Stating it here because a
symmetric edge is the natural thing to write and it would produce routes that
strand the body.

## Bounding the search

`Pillar` makes every Y coordinate reachable from every position. Combined with
`Dig`, so does downward travel. The parent design bounds the search with a node
budget and a cost ceiling and returns a partial path on exhaustion, which stops
the search running forever but does not stop it wasting its whole budget
climbing.

Two bounds, both on `Capability`, both numbers a test can pin:

- **A vertical envelope.** The search expands no node further than a stated
  distance above or below the start. A bot that wants the surface states the
  surface as its goal, and the envelope is sized around the goal rather than
  being unbounded.
- **A per-column pillar limit.** How many `Pillar` edges may stack in one
  column, which is what stops a search building a tower to reach a horizontal
  detour it should have walked.

### The heuristic changes

`Capability.perBlockFloor` returns the lowest cost the body can pay per block
of Manhattan distance, and its comment already records the care taken: a step
closes two blocks for one step's cost, and the floor is deliberately not the
cheapest edge because "an overestimating heuristic lets the search settle a
goal on a route that is not shortest."

`Pillar` and `Dig` both close vertical distance and both must enter that
calculation. A floor computed over the movement edges alone would overestimate
the moment a bot can dig downward more cheaply than it can walk, and the
parent design's determinism gate would start failing on routes that are merely
suboptimal — which is the worst kind of failure to diagnose.

Recomputing the floor over the enabled edge set is part of this work, not a
follow-up.

## Unmet prerequisites

The parent design's data-dependency table lists these. Their state as of
2026-08-18:

| Prerequisite | Repository | State |
| --- | --- | --- |
| `BlockMovementRegistry.FallsByState` | `minecraft-protocol` | **Landed 2026-08-18.** Not the `data.Block.Falling` this document first named: upstream publishes the property nowhere, so it is measured out of the pinned jars and rides in `blockMovement.json` beside `blocksMovement`, whose provenance the manifest already records. `Material` will not substitute — soul sand shares `Material.sand` with gravel and does not fall. |
| Break time | `minecraft-simulation` | M9.4, planned. Dig costs stay stubbed and dig stays off by default. |
| Placement legality | `minecraft-simulation` | M9.5, planned. Place, Pillar, Support, and Collapse stay stubbed and placement stays off by default. |
| `BlockMovementRegistry.ClimbableByState` | `minecraft-protocol` | **Landed 2026-08-18**, in the same extraction pass, for the same reason and in the same place. Needed by the other amendment; listed here because one pass supplied both. |
| `PostureFall` | `minecraft-simulation` | The other amendment. Blocks `Pillar` only. |

Two of these five land in `minecraft-protocol`, and both are block properties
that one extraction pass supplies. That pass is the first task of this work
even though the search code lives elsewhere.

**Where they landed, and why not where this document first said.** Both were
drafted as `data.Block.Falling`, a field on the published block record. That was
wrong, and `data/block_movement.go` already stated the rule that says so:
upstream's block data says what a block is called, how hard it is, and what it
drops, and says nothing about either of these. A fact measured out of a Mojang
jar belongs with the measured dataset whose manifest records the jar's digest,
not on the record built from what upstream published. So they extend
`blockMovement.json` and `BlockMovementRegistry`, and `data.Block` gained no
field. The rule that comes with that home is the one that matters most: a
version nobody has measured publishes no registry rather than an empty one, and
a block the measurement does not describe reports "not described" rather than
"does not fall".

The parent design's rule holds unchanged: "Until M9.4 and M9.5 land,
`Capability` defaults digging and placement off and the follower refuses those
edges. A bot that mines at a plausible-looking wrong speed is worse than one
that refuses to mine."

## Testing

Extending the parent design's acceptance criteria rather than replacing them.

- A body in a one-block shaft with a placement capability and a ceiling ten
  blocks up reaches the top through `Pillar` edges, and the same body with
  placement off returns an incomplete path.
- A body with a ceiling one block above its head produces no `Pillar` edge.
- No returned path descends a pillar by walking. A path that gains height
  through `Pillar` and later loses it contains `Fall` or `Dig`.
- The per-column pillar limit is respected, and a search that would exceed it
  routes horizontally instead.
- The vertical envelope is respected, and a goal outside it returns
  `ReasonUnreachable` rather than exhausting the node budget.
- `perBlockFloor` never exceeds the true cost of any route, checked by
  property test over random capabilities with random subsets of edges enabled.
  This is the test that catches an inadmissible heuristic, and it is the reason
  the floor is recomputed rather than left alone.
- Every parent-design criterion still passes: the gravel column still needs a
  `Support` or `Collapse` before its `Dig`, a thousand searches still agree,
  and a capability with digging and placement off still produces only movement
  edges on both versions.

## Sequencing

1. Extract falling and climbable in `minecraft-protocol`. Both amendments wait
   on this and nothing else does. **Done 2026-08-18.**
2. `Overlay` and the validation loop, with `Place` alone. The smallest edge
   that exercises the overlay.
3. `Pillar`, the two bounds, and the recomputed heuristic.
4. `Dig`, once M9.4 supplies break times.
5. `Support` and `Collapse`, once M9.5 supplies placement legality and the
   falling-column trace exists.

Step 1 is unblocked today. Step 2 is unblocked once step 1 lands, because
placement legality is only needed to *cost* a placement, not to model the
overlay. Steps 4 and 5 are not unblocked.

## Acceptance criteria

- A bot underground with a stack of blocks routes to the surface.
- A bot with an empty inventory produces the same path it produces today.
- The heuristic remains admissible for every combination of enabled edges.
- No number owned by M9.4 or M9.5 appears in this work, and both edge families
  refuse until their milestone lands.
