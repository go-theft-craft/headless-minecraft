# M9.2 Dropped Items and Arrows Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `subagent-driven-development` or execute this plan inline one task at a time. Keep every checkbox current.

**Goal:** Make a dropped item and an arrow move the way each version of the game moves them, and prove it against both games rather than against a reading of them.

**Architecture:** The kernel already moves one family — the player — and it moves it well enough that M8.8 draws zero corrections from two real servers. Everything M9.2 needs is a second and a third family through the same machinery: constants per family from the generated dataset, a tick shape per family expressed as phases, and a gate per version. Two things stand in the way and both are defects rather than gaps. The dataset carries item and arrow constants for 1.8.9 and not for 26.1.2, and every phase in both profiles reads `entity.FamilyPlayer` outright, so an item in the world today falls at the player's gravity and nobody would find out.

**Tech Stack:** Go 1.26.6 from `openserbia/go-flake`, Devbox, Task, `minecraft-reference` for the jar-backed reading, `minecraft-protocol`'s generated datasets, `minecraft-simulation`'s kernel and `internal/oracle` harness, `relay`'s capture oracle and `mcrelay trace`, a pinned vanilla 1.8.9 server, and a pinned vanilla 26.1.2 server.

## Before executing this plan: reconcile it

Task 0 does this and nothing else. These are the symbols this plan names that
exist today, and the ones it introduces:

| Symbol | Where it is | State |
| --- | --- | --- |
| `entity.Family`, `entity.FamilyPlayer` | `minecraft-simulation/entity/entity.go` | built |
| `sim.MotionConstants`, `sim.Profile.Motion` | `minecraft-simulation/sim/profile.go` | built |
| `data.EntityMotionIndex` | `minecraft-protocol/data/physics.go` | built, and 26.1 carries only `player` |
| `mctest.Fixture`, `mctest.Load` | `minecraft-simulation/mctest` | built, and shaped for input-driven ticks |
| `internal/oracle` harnesses | `minecraft-simulation/internal/oracle` | built for the player on both versions |
| `trace.ToleranceFor`, `mcrelay trace` | `relay/examples/minecraft` | built (M9.1b) |
| `entity.FamilyItem`, `entity.FamilyArrow` | — | this plan, Task 3 |
| `sim.ErrUnknownFamily` | — | this plan, Task 3 |
| `mctest.Captured` | — | this plan, Task 7 |

## Global Constraints

- Work in the repository each task names. Tasks 1 lands in `minecraft-reference`,
  Task 2 in `minecraft-protocol`, Tasks 3–7 in `minecraft-simulation`, Task 8 in
  `headless-minecraft`.
- Run project commands as `devbox run -- task <name>`.
- No game constant is transcribed without the two paths M8.1 requires: the
  decompiled source and `javap -p -c` over the shipped jar. A float and a double
  are different values, and only the bytecode settles which one a line holds.
- The simulation states no game value of its own. Every number arrives through
  the generated dataset, at the width the dataset records.
- Recordings are never committed. Extracted trajectories are numbers and may be;
  a `.mccap` holds player UUIDs, usernames, and chat and stays in
  `oracle-evidence`.
- A mechanic is verified on both versions or on neither. A lane that cannot run
  records why, in the shape M9.1b's `conform` established.

---

## Design decisions this plan settles

**A family is a guard on a phase, not a second phase list.** `Profile.Phases`
returns one ordered list and the kernel runs it once per tick. That is enough
for three families, because the orders interleave rather than conflict: a 1.8.9
item applies gravity *before* its move and a 1.8.9 player applies it *after*, and
one list can hold both as long as each phase skips the families it does not
apply to. The alternative — a phase list per family — would change
`sim.Profile`, and it would buy nothing until a version disagrees about the
order of two phases that both families run.

**An unknown family is an error, not zero constants.** `Motion` returns a map
lookup today, so a body whose family nobody set falls at gravity zero and drifts
forever with no drag. That is the failure this milestone is least able to see:
it looks like a physics bug in whatever consumes the body. Task 3 makes it
`ErrUnknownFamily` at the tick.

**The wire trace cannot gate per-tick physics for these two families, and the
jar can.** A server does not send an entity's position every tick: the tracker
has an update interval per entity type, twenty ticks for both an item and an
arrow, against two for a player. So a captured trace of a falling arrow holds
one absolute position per twenty ticks, and a per-tick comparison against it is
not available at any tolerance. That is a fact about the game, not a limitation
to work around. This plan therefore gates in two places:

- **Per tick, bit for bit, against the game's own classes** — the
  `internal/oracle` harness, extended to tick a real `EntityItem` and a real
  `EntityArrow`. This is the primary gate, and it is the same one M8.4 and M8.7
  used for the player.
- **At the tracker's own cadence, against a real server** — a captured trace
  from each version, compared at the checkpoints the server actually sent, at
  that version's replay tolerance. Twenty ticks of accumulated error is a
  demanding check on gravity and drag even though it says nothing about a single
  tick.

**The captured-trace comparator lives in `minecraft-simulation`, not in
`conform`.** M9.1b built `conform` in `relay/examples/minecraft`, which is an
unpublished module carrying a `replace` for `relay`'s core: nothing can import
it without a `replace` of its own, and a published module must not carry one.
The rule `conform` enforces — a lane per readable version, an absence with a
reason — is reimplemented in this plan's fixture table, in about thirty lines.
Two implementations of one rule is the cost of the module graph, and it is
cheaper than making the oracle importable by the thing it judges.

**Water, lava, merging, despawn, and pickup are out of scope, and the plan says
so where it stops.** The gate is motion on land: gravity, drag, the block under
the body, the item's bounce, and the arrow's stick. Everything else is recorded
as deferred with the version's own line numbers, so the next stage starts from a
list rather than a search.

## File structure

**`minecraft-reference`**

- `reference/notes/physics-motion-26.1.2.md` — modify. Item and arrow sections.

**`minecraft-protocol`**

- `source/java/26.1/physics.json` — modify. Two entries.
- `generated/java/v26_1/physics.go`, `raw/physics.json` — regenerated.
- `CHANGELOG.md` — modify.

**`minecraft-simulation`**

- `entity/entity.go` — modify. `FamilyItem`, `FamilyArrow`.
- `sim/profile.go` — modify. `ErrUnknownFamily`, and `Motion` reporting it.
- `profile/java/v1_8/profile.go`, `phases.go` — modify. Constants per family, phases guarded by family, the item and arrow phases.
- `profile/java/v26_1/profile.go`, `phases.go` — modify. The same, at 26.1's widths and order.
- `internal/oracle/java/ItemArrowOracle.java`, `ItemArrowOracle26.java` — create.
- `internal/oracle/items_test.go`, `items26_test.go` — create.
- `mctest/captured.go`, `captured_test.go` — create.
- `mctest/testdata/captured/` — create. One trajectory per family per version.

**`headless-minecraft`**

- `MASTER_PLAN.md`, `docs/superpowers/plans/2026-08-16-m9-gameplay-mechanics.md` — modify.

---

### Task 0: Reconcile this plan against what is built

- [x] **Step 1: Check every symbol in the table above**

Confirm each one exists where it is named, and correct this plan where it does
not. A plan that names a symbol nobody built is a plan that will be worked
around rather than followed.

- [x] **Step 2: Confirm the two defects this plan rests on**

Both are claims about today's code and both are load-bearing:

```bash
cd minecraft-simulation
grep -n "FamilyPlayer" profile/java/v1_8/phases.go profile/java/v26_1/phases.go
grep -n '"item"\|"arrow"' ../minecraft-protocol/source/java/26.1/physics.json
```

Expected: the phases name `FamilyPlayer` outright, and 26.1's dataset names
neither family. If either has changed, revise the task that depends on it before
running it.

---

### Task 1: Read 26.1.2's item and arrow motion constants

**Files:**
- Modify: `minecraft-reference/reference/notes/physics-motion-26.1.2.md`

The note already says what is missing: "Item and arrow constants are not
recorded. This milestone is the player on land." This task records them, to the
same standard as the player's — decompiled source and bytecode, with the widths
settled by the instruction rather than by the decimal.

- [x] **Step 1: Read the item's constants**

From `net/minecraft/world/entity/item/ItemEntity`: the gravity its
`getDefaultGravity` returns, the friction it multiplies horizontal motion by,
the vertical multiplier, and the bounce it applies on the ground.

- [x] **Step 2: Read the arrow's constants**

From `net/minecraft/world/entity/projectile/arrow/AbstractArrow`: the gravity its
`getDefaultGravity` returns, the inertia it applies when not in water, the water
inertia, and what a tick does while the arrow is in the ground.

- [x] **Step 3: Confirm both in bytecode**

```bash
javap -p -c -cp <server jar> net.minecraft.world.entity.item.ItemEntity
javap -p -c -cp net.minecraft.world.entity.projectile.arrow.AbstractArrow
```

Quote the instruction per constant. `ldc2_w double 0.04d` and
`ldc float 0.04f` are different numbers, and the difference is exactly what
1.8.9 and 26.1.2 disagree about here.

- [x] **Step 4: Write the sections, including the tick order**

Constants alone do not reproduce a trajectory: the order gravity, the move, and
the drags run in is part of the value. Record the order each class ticks in, and
where it differs from 1.8.9's.

- [x] **Step 5: Commit**

```bash
git add reference/notes/physics-motion-26.1.2.md
git commit -m "docs(reference): record 26.1.2's item and arrow motion constants"
```

---

### Task 2: Pin the constants in the dataset

**Files:**
- Modify: `minecraft-protocol/source/java/26.1/physics.json`
- Regenerate: `generated/java/v26_1/physics.go`, `generated/java/v26_1/raw/physics.json`
- Modify: `minecraft-protocol/CHANGELOG.md`

- [x] **Step 1: Add the two entries**

At the widths Task 1 settled, and to full `float64` precision. A `0.04F` is
written `0.03999999910593033`; a `0.04` double is written `0.04`.

- [x] **Step 2: Regenerate and check the diff**

Run: `devbox run -- task generate` (or the repository's generation task).
Expected: `physics.go` gains the two families and nothing else changes.

- [x] **Step 3: Run the gates**

Run: `devbox run -- task verify`.

- [x] **Step 4: Commit**

```bash
git add source/java/26.1/physics.json generated/java/v26_1 CHANGELOG.md
git commit -m "feat(data): pin 26.1.2's item and arrow motion constants"
```

---

### Task 3: Families, and constants per family

**Files:**
- Modify: `minecraft-simulation/entity/entity.go`
- Modify: `minecraft-simulation/sim/profile.go`
- Modify: `minecraft-simulation/profile/java/v1_8/profile.go`, `profile/java/v26_1/profile.go`
- Modify: both profiles' `phases.go`

**Interfaces:**

```go
// In entity: two more families. The values are appended rather than inserted,
// because a family's number is part of a recording's digest.
const (
    FamilyUnknown Family = iota
    FamilyPlayer
    FamilyItem
    FamilyArrow
)

// In sim: an unknown family is reportable.
var ErrUnknownFamily = errors.New("sim: no motion constants for this family")
```

- [x] **Step 1: Write the failing test**

Two tests. One in `entity`: the new families stringify and keep their numbers.
One per profile: a body of each family gets that family's constants, and a body
of an unset family produces `ErrUnknownFamily` from a tick rather than a
trajectory.

- [x] **Step 2: Run them to verify they fail**

- [x] **Step 3: Add the families and the constants**

Both profiles read `item` and `arrow` out of `physics.EntityMotion` the way they
already read `player`, and refuse to build when the dataset lacks one — a
profile that silently omits a family would move it at zero gravity.

- [x] **Step 4: Make every phase read the body's family**

Replace `p.Motion(entity.FamilyPlayer)` with a lookup per body. This is the
defect the plan's architecture note names: today an item in the world falls at
the player's gravity.

- [x] **Step 5: Run the tests, both profiles**

- [x] **Step 6: Commit**

```bash
git commit -m "feat(entity): give items and arrows their own motion constants"
```

---

### Task 4: The item tick

**Files:**
- Modify: both profiles' `phases.go`

The 1.8.9 order, from `EntityItem.onUpdate`: `motionY -= 0.04F`, move, then
`motionX,Z *= slipperiness × 0.98F` (or `0.98F` off the ground),
`motionY *= 0.98F`, then on the ground `motionY *= -0.5`.

The 26.1.2 order, from `ItemEntity.tick`: `applyGravity`, move, then
`multiply(friction, 0.98, friction)` where friction is the block's friction
times `0.98F` on the ground, then on the ground **and only when `y < 0`**,
`multiply(1.0, -0.5, 1.0)`.

- [x] **Step 1: Write the failing test**

A dropped item on a flat floor: it falls, it lands, it bounces once at half its
downward motion, and it comes to rest. Assert the first three ticks against
numbers computed from the constants by hand, so a wrong order fails rather than
a wrong constant only.

- [x] **Step 2: Run it to verify it fails**

- [x] **Step 3: Add the phases, guarded by family**

`item-gravity` before the move, `item-friction` and `item-bounce` after it. The
bounce condition differs between the versions and the difference is recorded in
the phase's own comment.

- [x] **Step 4: Run the tests**

- [x] **Step 5: Commit**

---

### Task 5: The arrow tick

**Files:**
- Modify: both profiles' `phases.go`

An arrow has no friction from the block below it and no bounce. It applies its
inertia to all three axes, applies gravity, and stops entirely once it is in the
ground: a stuck arrow's tick produces no motion at all, which is why a capture
of one is a long run of zero deltas.

- [x] **Step 1: Write the failing test**

An arrow launched horizontally over a floor: it accelerates downward, its
horizontal motion decays by the inertia each tick, it lands, and every tick
after landing leaves it exactly where it was.

- [x] **Step 2: Run it to verify it fails**

- [x] **Step 3: Add the phases**

- [x] **Step 4: Run the tests**

- [x] **Step 5: Commit**

---

### Task 6: The jar-backed differential, both versions

**Files:**
- Create: `minecraft-simulation/internal/oracle/java/ItemArrowOracle.java`, `ItemArrowOracle26.java`
- Create: `minecraft-simulation/internal/oracle/items_test.go`, `items26_test.go`
- Create: `minecraft-simulation/mctest/testdata/` fixtures generated from the runs

This is the primary gate, and it follows `MovementOracle`'s shape exactly: a
harness that constructs the game's own entity in a stub world, ticks it, and
prints its state; a Go test that drives the same trajectory through the kernel
and compares bit for bit. Nothing in either harness reimplements a rule.

- [x] **Step 1: Write the harnesses**

One command to place blocks, one to spawn an item or an arrow with a position and
a motion, one to tick. Print the position and motion after each tick at full
precision.

- [x] **Step 2: Write the differential tests**

Random floors of varying slipperiness, random initial motions, a hundred ticks.
Compare bit for bit, the way the movement oracles do — a tolerance here would
hide exactly the width errors this milestone exists to catch.

- [x] **Step 3: Run them, and record what disagrees**

A disagreement is a finding about the rules, not a reason to loosen the
comparison. Fix the rule, and write down what the game did that the reading of
it missed.

- [x] **Step 4: Generate the fixtures**

So the gate runs without a jar, everywhere, forever — the same split
`mctest` already carries for the player.

- [x] **Step 5: Commit**

---

### Task 7: The captured-trace gate, both versions

**Files:**
- Create: `minecraft-simulation/mctest/captured.go`, `captured_test.go`
- Create: `minecraft-simulation/mctest/testdata/captured/*.json`

**Interfaces:**

```go
// Captured is a trajectory a real server sent, extracted from a recording.
type Captured struct {
    Name     string        // the scenario
    Profile  sim.ProfileID // which game sent it
    Family   entity.Family
    Source   string        // recording digest, server digest, date
    World    scene.World   // the floor it fell onto
    Spawn    Body          // where and how fast it started
    Interval int           // ticks between samples, measured from the recording
    Samples  []Sample      // absolute positions, in order
    // Absent, when non-empty, says this version does not have this scenario and
    // why. A lane with neither samples nor a reason is refused.
    Absent string
}
```

- [x] **Step 1: Capture, both versions**

Through `mcrelay` in front of each pinned server, drop an item and shoot an
arrow. Summon from a height that spans several tracker intervals: a fall of a
dozen blocks is over before the first update.

- [x] **Step 2: Measure the sample cadence rather than assuming it**

Take the modal gap between samples and divide by fifty milliseconds. Refuse a
capture whose gaps are not a consistent multiple: a batched or dropped update
makes a checkpoint comparison meaningless, and it must be visible rather than
absorbed.

- [x] **Step 3: Write the comparator**

Simulate `Interval` ticks between samples and compare at each one, at the
version's own tolerance — `1/32` for 1.8.9, and 26.1.2's zero-at-absolute with
`1/4096` per relative move. A lane with an `Absent` reason reports absent; a
version with neither a lane nor a reason fails the suite.

- [x] **Step 4: Run the gate**

- [x] **Step 5: Commit**

---

### Task 8: Record the milestone

**Files:**
- Modify: `headless-minecraft/MASTER_PLAN.md`
- Modify: `headless-minecraft/docs/superpowers/plans/2026-08-16-m9-gameplay-mechanics.md`

- [x] **Step 1: Mark M9.2 complete in both stage tables**

- [x] **Step 2: Write what the work found**

What was budgeted and what was not: the hardcoded `FamilyPlayer`, the widths
26.1.2 changed, the tracker interval that decides what a wire trace can prove,
and anything the jar did that the source reading missed.

- [x] **Step 3: Commit**

---

## What execution changed about this plan

Written down because a plan that quietly disagrees with what happened is worse
than no plan.

- **Task 3 does not refuse to build a profile whose dataset lacks a family.** It
  takes the families the dataset carries and leaves out the ones it does not,
  and the tick refuses the missing one at the point of use. Refusing at
  construction would take a whole version down because one family is missing,
  and the failure would name the profile rather than the body.
- **Task 6 covers the item on 1.8.9 and no more.** The 1.8.9 harness ticks a
  real `EntityItem` and 440 ticks agree bit for bit. The arrow's tick is
  dominated by a ray cast and an entity sweep the stub world cannot answer, and
  26.1's `ItemEntity.tick` reaches for merging and block effects that its stub
  level cannot either. Both are recorded here rather than attempted and left
  half-working; the wire gate covers those three lanes.
- **Task 7's fixtures carry per-sample tick offsets rather than one interval.**
  A tracker's first update does not fall on its own period: the 1.8.9 arrow's
  first sample is one tick after the spawn and every one after it is twenty.
- **Two defects in the 26.1 profile were found by the gate rather than by the
  tasks that were meant to find them**, because only a non-player body reaches
  them: the move rebuilt every body's box at the player's dimensions, and the
  gravity and drag phases were unguarded by family.
- **The 26.1 lanes skip.** This module pins `minecraft-protocol` v0.5.0, which
  predates the constants Task 2 landed. Every 26.1 check is written, skips with
  that reason, and was run against the new dataset locally. The bump must
  regenerate `replay/testdata/26_1` in the same commit: a profile that gains two
  families gains a data digest.

## Stage summary

| Task | Delivers | Gate |
| --- | --- | --- |
| 0 | Reconciliation | Every symbol this plan names exists or is listed as new |
| 1 | 26.1.2's item and arrow constants, jar-backed | Each constant has a source line and an instruction |
| 2 | The constants in the dataset | The generated profile carries three families |
| 3 | Families and per-family constants | An item falls at the item's gravity; an unset family is an error |
| 4 | The item tick | Fall, land, bounce, rest, on both versions |
| 5 | The arrow tick | Fall, decay, land, and stay, on both versions |
| 6 | The jar-backed differential | A hundred ticks agree bit for bit with the game, both versions |
| 7 | The captured-trace gate | A real server's trajectory replays within the version's tolerance, both versions |
| 8 | The record | The master plan says what was found |

## Definition of done

- 26.1.2's item and arrow constants are recorded with both readings, and pinned
  in the dataset at their real widths.
- `entity.FamilyItem` and `entity.FamilyArrow` exist, both profiles carry their
  constants, and no phase names a family outright.
- A body whose family has no constants fails the tick with `ErrUnknownFamily`.
- The item and arrow ticks agree with the game's own classes, bit for bit, over
  a hundred ticks, on both versions.
- A captured item trajectory and a captured arrow trajectory from each pinned
  server replay within that version's tolerance, or a lane records why it could
  not run.
- `task verify` passes in every repository this plan touches.

## Risks

- **The jar harness may not be able to tick an item without a populated level.**
  `ItemEntity.tick` reaches for block state below it and for merging. If a stub
  world cannot answer, the harness shrinks to what it can drive and the gap is
  recorded rather than papered over.
- **Twenty ticks of accumulated drift may exceed 1/32 of a block even when the
  rules are right**, because the capture's start state is quantised. If so, the
  checkpoint comparison states its own achievable bound, derived rather than
  chosen, and says what it can and cannot catch.
- **26.1.2's item friction reads the block below through a helper with its own
  offset** (`getBlockPosBelowThatAffectsMyMovement`). If it disagrees with the
  player's lookup, the item phase needs its own and the difference is a finding.

## What this plan deliberately does not do

- Water, lava, and bubble columns. Both versions branch to a different tick
  there, and none of the M9 stages before M9.5 need it.
- Item merging, despawn timers, and pickup. They change what exists, not how it
  moves.
- Arrow hits on entities, damage, and knockback. That is M9.6, which owns the
  reach and damage rules the hit feeds.
- Other projectiles. An egg, a snowball, and a thrown potion share the arrow's
  shape with different constants, and adding them is a dataset entry and a
  family once this stage's machinery exists.
