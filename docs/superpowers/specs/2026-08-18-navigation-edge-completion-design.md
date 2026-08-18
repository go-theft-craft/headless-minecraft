# Navigation edge completion design

## Status

Drafted 2026-08-18. Amends the
[navigation design](2026-08-17-navigation-design.md), which stands except where
this document says otherwise. Implementation requires the matching
implementation plan and an explicit request to execute it.

## Purpose

The navigation design's sequencing lists step 3 as "`navigation` with the
movement edges only: Walk, Step, Fall, JumpGap, Swim" and calls steps 1 through
3 unblocked. What shipped is four of those five. `navigation/edge.go` has
`EdgeWalk`, `EdgeStep`, `EdgeFall`, and `EdgeSwim`; `JumpGap` is absent.
`navigation.Posture` has `PostureStand` and `PostureSwim`; the parent design
names four postures — "standing, sneaking, swimming, or falling".

So step 3 is incomplete, and the gap is not cosmetic. **Nothing in the shipped
vocabulary crosses a gap.** `EdgeStep` rises one block into an adjacent cell
and `EdgeFall` descends into an adjacent column; a body standing at the edge of
a two-block hole has no edge that reaches the far side. Every route around
every gap is a detour, and on broken terrain many goals are simply unreachable.

This document finishes step 3 and adds four edges the parent design does not
name at all.

## Goals

- Land the parent design's own step 3 in full: `JumpGap`, `PostureSneak`,
  `PostureFall`.
- Add the read-only edges the parent design omits: climbing, doors, and a drop
  into water.
- Add `PostureCrawl` where the version has one, and record that one version
  does not.
- Change no interface the mutating edges will need. This and the
  [mutating edge amendment](2026-08-18-mutating-edge-amendment-design.md) are
  independent in every respect but one: that amendment's `Pillar` edge needs
  `PostureFall` from this one, because the body is airborne when it places. No
  other ordering constraint exists between them.

## Non-goals

- Anything that mutates the world or consumes a resource. That is the other
  amendment. Opening a door is the deliberate exception, argued below.
- The `Follower`. Step 4 of the parent sequencing is unchanged and still waits
  on M8.8's `adapter.Drive` and `predict.Correction`.
- Boats, minecarts, and elytra. A vehicle is a different body with different
  physics, not an edge, and nothing in `entity` models one yet.

## JumpGap

The parent design defines it as crossing "a gap the movement kernel says the
arc clears", and that phrasing is the whole design: the reachable set is not a
constant, it is a question for `movement`.

A 1.8.9 sprint-jump and a 26.1.2 sprint-jump do not clear the same distance,
and the module's rule is that a version's numbers arrive as arguments. So
`Capability` gains two fields the caller supplies, in the same shape as
`WalkTicks` and `SafeFall` rather than as constants in `navigation`:
`JumpTicks`, the cost of the edge, and `JumpReach`, the horizontal distance
this version's sprint-jump arc clears. A capability that leaves `JumpReach` at
zero produces no jump edges, which is how a mob keeps getting a ground
navigator out of the same search.

Where `JumpReach` comes from is not the caller's business to invent. The
navigation plan of 2026-08-17 deferred this edge for exactly that reason:
"Doing it honestly needs a per-profile reach table computed from the movement
kernel, which is its own deliverable. A guessed maximum gap would be a number
this repository does not verify."

That still holds and this design does not dodge it. `JumpReach` is a field on
`Capability` so that `navigation` stays free of version constants, and the
number that goes into it is **computed by running the profile's own movement
kernel** over a sprint-jump and measuring where the arc lands. The reach table
is a deliverable of this work, not an assumption of it, and a capability
constructed with a hand-written reach is a capability the conformance gate
excludes.

The edge is legal when the take-off cell is standable, the landing cell is
standable, the horizontal distance is within the supplied reach, and the arc's
clearance over every intervening column admits the body's box. Clearance is a
`terrain` question and reuses `Query.Fits`; nothing here re-derives collision.

`PostureFall` follows from it. A body mid-arc is neither standing nor swimming,
and the parent design already says two postures at one position are distinct
nodes "because they differ in box, in reach, and in which edges leave them". A
falling body cannot start a new jump, which is exactly the constraint the
posture encodes.

## PostureSneak

Sneaking exists in both versions and changes two things the search cares about:
the body does not walk off a ledge, and in 26.1.2 the box is shorter.

It earns a posture rather than a flag on `Capability` because it is a
per-position decision, not a per-body one. A bot sneaks across a one-block
ledge and stands everywhere else, and a single flag would make it sneak for the
whole route or none of it.

## PostureCrawl

**26.1.2 only.** A one-block-tall gap is passable to a crawling body in 26.1.2
and to nothing in 1.8.9, which has no crawl at all.

The parent design's two-version gate says "a scenario that runs on 1.8.9 and
not on 26.1.2 is a failure", and this is the first case that runs the other way
round. The master plan already supplies the resolution: "a per-version gate
must be allowed to say this behavior does not exist in this version and record
why". `Capability` therefore carries the postures the body has, and a 1.8.9
capability omits crawl. The gate asserts the asymmetry rather than tolerating
it, so a future version that gains a posture cannot land silently.

## Climb

Ladders and vines. Genuinely new; the parent design does not name them.

`EdgeClimb` moves one cell vertically within a climbable column, in either
direction, and it needs one fact `terrain` does not expose: whether a block is
climbable. That is a block property, not a shape — a ladder's collision box is
empty, so nothing in `collision` distinguishes it from air. It joins the
parent design's data-dependency table alongside `Falling`.

`Capability` gains `ClimbTicks` and `CanClimb`. A mob that cannot climb gets
the value with it off, which is the parent design's established shape.

## Door

Opening a door mutates the world, and this amendment otherwise excludes
mutation. The exception is argued rather than assumed: a door consumes nothing,
is reversible, cannot fail for want of a resource, and cannot make an earlier
edge illegal — the three properties that force the mutating amendment's overlay
and its validation loop. It is a state toggle on one block.

So `EdgeDoor` records the toggle on the edge and needs no overlay. If a later
reading finds a case where an opened door invalidates an earlier edge, this
edge moves to the other amendment; the acceptance criteria below include the
test that would find it.

`Capability` gains `CanOpenDoors`. Iron doors and any door the version gates
behind redstone are refused, not modelled, because a bot that walks into an
iron door forever is worse than one that routes around it.

## WaterDrop

Descending further than `SafeFall` when the landing column is fluid. The
parent design's `Fall` edge is bounded by `SafeFall` and has no way to express
the drop that is safe because of what is at the bottom.

It reuses `Query.FluidAt`, which exists, and needs a minimum column depth the
caller supplies, because how deep the water must be differs by version and by
fall distance. It arrives in `PostureSwim`.

## What does not change

`Edge`, `Path`, `Reason`, and the search itself keep their current shape. Every
edge here is read-only over a `world.View`, so the parent design's overlay and
its validation loop are not needed and are not built by this work. `Capability`
grows fields and loses none.

## Testing

Following the parent design's stated habit — property tests, matching
`collision/property_test.go`.

- Every property the parent design already asserts holds with the new edges
  present: a returned path is contiguous, every edge is legal under its
  capability, cost is monotone, and a thousand identical searches produce the
  identical path.
- A capability with `JumpReach` at zero produces no `JumpGap` edge, and the same
  world routes around the gap instead.
- A `JumpGap` edge's arc is clear: a case with a block one above the midpoint
  is refused, and the same case with the block removed is admitted.
- A one-block ledge routes through `PostureSneak` when sneaking is available
  and detours when it is not.
- The crawl asymmetry is asserted in both directions: the 26.1.2 capability
  crosses the one-block gap, the 1.8.9 capability does not, and the gate
  records the version reason rather than skipping.
- A climbable column is traversed in both directions, and a capability with
  `CanClimb` off routes around it.
- **The door conflict test.** A route whose later edge opens a door that closes
  the space an earlier edge relied on. If such a case exists, `EdgeDoor` needs
  the overlay and moves to the other amendment; this test is how that is
  discovered rather than assumed.
- A drop of twice `SafeFall` into three blocks of water is admitted and the
  same drop onto stone is refused.

## Sequencing

1. `JumpGap` and `PostureFall`. The largest reachability gain and the parent
   design's own unfinished step 3.
2. `PostureSneak`.
3. `WaterDrop`. Reuses `FluidAt`, which exists.
4. Climb, once the climbable block property is extracted.
5. Door, gated on the conflict test above.

Steps 1 through 3 are unblocked today. Step 4 waits on data extraction in
`minecraft-protocol`, in the same way `Falling` does.

## Acceptance criteria

- A body standing at the edge of a two-block gap with a jump capability routes
  across it, and the same body without one routes around.
- `navigation.Posture` has stand, sneak, swim, and fall, plus crawl on the
  version that has one.
- Every property test the parent design lists still passes with the expanded
  edge set.
- A capability with every optional edge off produces exactly the four edges
  that ship today, so the existing behaviour is provably unchanged.
- The 1.8.9 crawl gate reports "this behaviour does not exist in this version"
  with a reason, and does not pass by silence.
