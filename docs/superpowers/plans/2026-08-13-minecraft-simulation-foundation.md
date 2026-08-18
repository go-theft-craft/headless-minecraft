# Minecraft Simulation Foundation Implementation Plan

> **Status: complete, 2026-08-18.** Shipped as the `minecraft-simulation`
> repository across M8.1 through M8.8, under that repository's own stage plans,
> which supersede this one wherever they disagree. The checkboxes below were
> never ticked and are not evidence; do not re-run this plan.

> **For agentic workers:** REQUIRED SUB-SKILL: Use `subagent-driven-development` (recommended) or execute this plan inline one task at a time. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement one deterministic movement slice for Java Edition 1.8.9 and 26.1.2 through the same server and client kernel.

**Architecture:** An immutable tick input passes through a profile-owned ordered phase pipeline and produces one atomic change set, ordered events, updated random state, dependencies, and a canonical digest. Core packages use semantic simulation types and do not import protocol packages. Official Java profiles adapt only `minecraft-protocol/data`, while the server and the client retain their own state stores.

**Tech Stack:** Go 1.26.5 from `openserbia/go-flake`, Devbox, Task, the Go standard library, `minecraft-protocol/data`, the reviewed Minecraft reference catalogs, gofumpt, gci, golangci-lint v2, govulncheck, and gitleaks.

## Global constraints

- Complete `2026-08-13-minecraft-reference-extraction.md` before Task 1.
- Require `devbox run -- task reference:check REFERENCE_DIR=reference/work` to pass before simulation code begins.
- Require both version catalogs to cover every approved simulation domain.
- Require the first-slice comparison to contain no unresolved record.
- Require every first-slice arithmetic method to have bytecode evidence.
- Require every planned shared rule to name a conformance fixture.
- Complete the shared protocol game-data extraction and both generated Java data families before Task 6.
- Work in `/home/ocharnyshevich/pet.projects/go-theft-craft/minecraft-simulation` unless a task names another repository.
- Run Go, formatting, lint, test, vulnerability, and build commands through `devbox run -- task <name>`.
- Leave changes uncommitted unless the user explicitly asks for a commit.
- Keep all protocol numbers, packet IDs, packet structs, connection states, and encoded wire units outside the simulation API.
- Permit imports of `minecraft-protocol/data` only below `profile/java`.
- Use Java Edition 1.8.9 and 26.1.2 as the first exact behavior targets.
- Return the same ordered result and digest for the same complete input on every supported platform.
- Treat unknown world data as unknown. Never treat it as air.
- Return no applicable changes or emitted events when a requested simulation scope is incomplete.
- Read no wall clock or global random state during a tick.
- Run one simulation scope on one goroutine until deterministic conformance and benchmarks justify internal parallel work.
- Bound collision candidates, phase work, entity steps, changes, events, dependencies, and extension work.
- Keep decompiled jars, classes, Java sources, mappings, and Mojang assets out of Git.
- Store only artifact provenance, prose algorithm notes, independently authored fixtures, and expected outputs in the public repository.
- Do not add fluid propagation, explosions, TNT, vehicles, redstone, pistons, random block ticks, mob AI, or full combat in this plan.

## Planned file map

The first slice creates these ownership boundaries:

```text
geom/                     Numeric and geometric value types
world/                    Known and unknown world queries and dependencies
entity/                   Entity IDs, bodies, poses, and immutable state
sim/                      Tick contracts, limits, RNG state, kernel, and phases
collision/                Swept AABB collision and contact resolution
movement/                 Player movement phase and version-neutral rule inputs
item/                     Dropped-item movement phase
projectile/               Arrow movement and collision phase
profile/                  Profile builder and manifest validation
profile/java/internal/    Adapter from minecraft-protocol/data
profile/java/v1_8/        Java 1.8.9 profile
profile/java/v26_1/       Java 26.1.2 profile
runtime/                  Atomic in-memory store and tick advancement
replay/                   Canonical encoding, digest, recording, and replay
mctest/                   Protocol-free scenarios and fixture runner
reference/                Prepared catalogs, provenance, and prose research notes
internal/buildcheck/      Dependency and repository-content checks
```

---

### Task 1: Add deterministic geometry and Java numeric helpers

**Files:**

- Create: `minecraft-simulation/geom/vec3.go`
- Create: `minecraft-simulation/geom/blockpos.go`
- Create: `minecraft-simulation/geom/aabb.go`
- Create: `minecraft-simulation/geom/axis.go`
- Create: `minecraft-simulation/geom/geom_test.go`
- Create: `minecraft-simulation/sim/numeric.go`
- Create: `minecraft-simulation/sim/numeric_test.go`

**Interfaces:**

- Produces: `geom.Vec3`, `geom.BlockPos`, `geom.AABB`, `geom.Axis`, `geom.AxisOrder`, `sim.JavaFloat`, `sim.JavaInt`, and `sim.CanonicalZero`.

- [ ] **Step 1: Write geometry and numeric edge-case tests**

Test negative block flooring, touching versus intersecting boxes, signed zero normalization for hashing, NaN rejection, infinity rejection, Java float narrowing, and Java integer casts.

```go
func TestBlockPosFromVecFloorsNegativeCoordinates(t *testing.T) {
	got := geom.BlockPosFromVec(geom.Vec3{X: -0.01, Y: 2.99, Z: -1})
	want := geom.BlockPos{X: -1, Y: 2, Z: -1}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}
```

- [ ] **Step 2: Verify the tests fail**

Run `devbox run -- task test -- ./geom ./sim`.

Expected: compilation fails because the packages do not exist.

- [ ] **Step 3: Implement focused immutable value types**

Use these public shapes:

```go
type Vec3 struct{ X, Y, Z float64 }
type BlockPos struct{ X, Y, Z int32 }
type AABB struct{ Min, Max Vec3 }
type Axis uint8
type AxisOrder [3]Axis

func NewAABB(min, max Vec3) (AABB, error)
func (b AABB) Move(delta Vec3) AABB
func (b AABB) Intersects(other AABB) bool
func (b AABB) ExpandForMotion(delta Vec3) AABB
func BlockPosFromVec(v Vec3) BlockPos
```

Reject non-finite coordinates and inverted boxes in `NewAABB`. Keep arithmetic methods free of allocation and mutation.

- [ ] **Step 4: Implement explicit Java conversion helpers**

Keep numeric narrowing visible at call sites:

```go
func JavaFloat(v float64) float64 { return float64(float32(v)) }
func JavaInt(v float64) int32     { return int32(v) }
func CanonicalZero(v float64) float64 {
	if v == 0 {
		return 0
	}
	return v
}
```

Add test vectors from both movement research notes when the operation order narrows a value.

- [ ] **Step 5: Run focused checks**

Run `devbox run -- task fmt`, `devbox run -- task test -- ./geom ./sim`, and `devbox run -- task lint`.

Expected: all geometry and numeric tests pass. Do not commit.

### Task 2: Define immutable world and entity views

**Files:**

- Create: `minecraft-simulation/world/knowledge.go`
- Create: `minecraft-simulation/world/dependency.go`
- Create: `minecraft-simulation/world/block.go`
- Create: `minecraft-simulation/world/view.go`
- Create: `minecraft-simulation/world/memory.go`
- Create: `minecraft-simulation/world/view_test.go`
- Create: `minecraft-simulation/entity/id.go`
- Create: `minecraft-simulation/entity/body.go`
- Create: `minecraft-simulation/entity/state.go`
- Create: `minecraft-simulation/entity/view.go`
- Create: `minecraft-simulation/entity/view_test.go`

**Interfaces:**

- Consumes: `geom.Vec3`, `geom.BlockPos`, and `geom.AABB`.
- Produces: three-state world lookups, stable dependencies, immutable entity state, and sorted iteration.

- [ ] **Step 1: Write unknown-state and isolation tests**

Test known air, known block, unknown block, missing collision shapes, sorted entity IDs, copied slices, and snapshot isolation after the source builder changes.

```go
func TestUnknownBlockIsNotAir(t *testing.T) {
	view := world.NewMemoryView(nil, nil)
	lookup := view.Block(geom.BlockPos{X: 4, Y: 5, Z: 6})
	if lookup.Knowledge != world.Unknown {
		t.Fatalf("got %v, want Unknown", lookup.Knowledge)
	}
}
```

- [ ] **Step 2: Define lookup and dependency contracts**

```go
type Knowledge uint8

const (
	Unknown Knowledge = iota
	KnownAir
	KnownValue
)

type Dependency struct {
	Kind string
	ID   string
}

type BlockLookup struct {
	Knowledge   Knowledge
	State       BlockState
	Dependencies []Dependency
}

type View interface {
	Block(pos geom.BlockPos) BlockLookup
	CollisionBoxes(bounds geom.AABB) CollisionLookup
}
```

Require every dependency list and collision box list to use canonical order.

- [ ] **Step 3: Define entity state without application ownership**

```go
type ID uint64
type Kind string
type Pose string
type ComponentID string

type Component struct {
	ID   ComponentID
	Data []byte
}

type Body struct {
	Bounds     geom.AABB
	StepHeight float64
}

type State struct {
	ID       ID
	Kind     Kind
	Position geom.Vec3
	Velocity geom.Vec3
	Body     Body
	Pose     Pose
	OnGround bool
	Components []Component
}

type View interface {
	Entity(id ID) (State, world.Knowledge)
	IDs() []ID
}
```

Return owned slices from `IDs`. Sort components by namespaced ID, reject
duplicates, and clone every component byte slice. Do not expose maps.

- [ ] **Step 4: Implement in-memory immutable views**

Use builders that clone all input maps and slices at `Build`. Sort collision boxes by minimum coordinates and entity IDs numerically. Reject duplicate entity IDs and invalid boxes.

- [ ] **Step 5: Run focused and race tests**

Run `devbox run -- task test -- ./world ./entity` and `devbox run -- task lint`.

Expected: all isolation, ordering, and unknown-state tests pass under the race detector. Do not commit.

### Task 3: Define tick contracts, limits, random state, and atomic results

**Files:**

- Create: `minecraft-simulation/sim/id.go`
- Create: `minecraft-simulation/sim/snapshot.go`
- Create: `minecraft-simulation/sim/command.go`
- Create: `minecraft-simulation/sim/change.go`
- Create: `minecraft-simulation/sim/event.go`
- Create: `minecraft-simulation/sim/result.go`
- Create: `minecraft-simulation/sim/limits.go`
- Create: `minecraft-simulation/sim/budget.go`
- Create: `minecraft-simulation/sim/random.go`
- Create: `minecraft-simulation/sim/contracts_test.go`

**Interfaces:**

- Consumes: immutable `world.View` and `entity.View` values.
- Produces: `sim.TickInput`, `sim.TickResult`, `sim.Command`, `sim.Change`, `sim.Event`, `sim.Limits`, `sim.Budget`, and `sim.RandomState`.

- [ ] **Step 1: Write validation and atomicity tests**

Test duplicate command IDs, non-increasing command sequence numbers, invalid zero limits, hard-ceiling violations, budget exhaustion, duplicate random stream names, sorted random streams, and incomplete results with no changes or events.

- [ ] **Step 2: Define the tick input and semantic IDs**

```go
type Revision uint64
type Tick int64
type TypeID string
type CommandID uint64

type Scope struct {
	Entities []entity.ID
}

type TickInput struct {
	Revision Revision
	Tick     Tick
	World    world.View
	Entities entity.View
	Scope    Scope
	Commands []Command
	Random   RandomState
	Limits   Limits
}
```

Validation clones and sorts scope IDs. It rejects a nil view, duplicate IDs, invalid limits, and unordered commands.

- [ ] **Step 3: Define extensible ordered records**

```go
type Command interface {
	CommandID() CommandID
	Sequence() uint64
	TypeID() TypeID
	ActorID() entity.ID
}

type Change interface {
	TypeID() TypeID
	OrderKey() string
}

type Event interface {
	TypeID() TypeID
	OrderKey() string
	Presentation() bool
}
```

Built-in command, change, and event implementations use explicit structs. Custom types require profile registration before a kernel accepts them.

- [ ] **Step 4: Add deterministic limits and random state**

```go
type Limits struct {
	MaxPhases              uint32
	MaxEntitySteps         uint32
	MaxCollisionCandidates uint32
	MaxChanges             uint32
	MaxEvents              uint32
	MaxDependencies        uint32
	MaxExtensionWork       uint32
}

type RandomStream struct {
	Name  string
	State []byte
}

type RandomState struct {
	Streams []RandomStream
}
```

Expose `NewLimits(options ...LimitOption) (Limits, error)` with conservative
defaults and non-disableable process ceilings. `Budget.Consume(kind, count)`
returns `ErrBudgetExceeded` before a counter crosses its limit.
Expose `NewBudget(limits Limits) *Budget`. A budget is local to one kernel call
and is never shared between ticks.

- [ ] **Step 5: Define complete and incomplete results**

```go
type Completeness uint8

const (
	Complete Completeness = iota + 1
	Incomplete
)

type TickResult struct {
	Revision     Revision
	Tick         Tick
	Completeness Completeness
	Changes      []Change
	Events       []Event
	Outcomes     []CommandOutcome
	Random       RandomState
	Dependencies []world.Dependency
	Digest       [32]byte
}
```

Make the constructor reject changes or events in an incomplete result. Return owned slices from every accessor.

- [ ] **Step 6: Run focused tests**

Run `devbox run -- task fmt`, `devbox run -- task test -- ./sim`, and `devbox run -- task lint`.

Expected: validation, ordering, limits, random state, and atomicity tests pass. Do not commit.

### Task 4: Build immutable profiles and the ordered kernel

**Files:**

- Create: `minecraft-simulation/sim/phase.go`
- Create: `minecraft-simulation/sim/profile.go`
- Create: `minecraft-simulation/sim/registry.go`
- Create: `minecraft-simulation/sim/kernel.go`
- Create: `minecraft-simulation/sim/kernel_test.go`
- Modify: `minecraft-simulation/sim/snapshot.go`
- Create: `minecraft-simulation/profile/builder.go`
- Create: `minecraft-simulation/profile/manifest.go`
- Create: `minecraft-simulation/profile/builder_test.go`

**Interfaces:**

- Consumes: `sim.TickInput`, `sim.TickResult`, and `sim.Budget`.
- Produces: `sim.Phase`, `sim.Profile`, `sim.Kernel.Step`, `profile.Builder`, and an immutable profile manifest.

- [ ] **Step 1: Write pipeline failure tests**

Test duplicate phase IDs, an unknown insertion boundary, a dependency cycle, an unregistered custom command, phase-budget exhaustion, cancellation, panic-free rule errors, and stable event order.

- [ ] **Step 2: Define profile and phase contracts**

```go
type ProfileID struct {
	Edition       string
	GameVersion   string
	RulesRevision string
}

type Phase interface {
	ID() TypeID
	Run(context.Context, *TickState) error
}

type Profile interface {
	ID() ProfileID
	Manifest() Manifest
	Phases() []Phase
	RegisteredTypes() TypeRegistry
}
```

`Phases` returns a copy. `Manifest` contains the game-data digest, ordered phase IDs, registered type IDs, numeric mode, random mode, and limits.

Add `Profile Profile` to `TickInput`. Input validation rejects a nil profile
before any phase runs.

- [ ] **Step 3: Implement the validated builder**

Expose `NewBuilder(base sim.Profile)`, `ID`, `DataDigest`, `ReplacePhase`,
`InsertAfter`, `RegisterCommand`, `RegisterChange`, `RegisterEvent`,
`RegisterComponent`, `Limits`, and `Build`. A component registration provides
its namespaced ID, validator, canonical encoder, and canonical decoder. `Build`
resolves one total phase order and returns an immutable profile.

- [ ] **Step 4: Implement an all-or-nothing kernel**

```go
type Kernel struct{}

func (Kernel) Step(ctx context.Context, input TickInput) (TickResult, error)
```

Validate the complete input before the first phase. Run phases in manifest order. If a phase reports an unknown dependency, return `Incomplete` with no changes or events. If a phase returns an error or the context is cancelled, return no result that a store can apply.

- [ ] **Step 5: Run focused tests**

Run `devbox run -- task test -- ./sim ./profile` and `devbox run -- task lint`.

Expected: profile validation and deterministic phase-order tests pass. Do not commit.

### Task 5: Implement swept AABB collision

**Files:**

- Create: `minecraft-simulation/collision/contact.go`
- Create: `minecraft-simulation/collision/clip.go`
- Create: `minecraft-simulation/collision/move.go`
- Create: `minecraft-simulation/collision/move_test.go`
- Create: `minecraft-simulation/mctest/world.go`

**Interfaces:**

- Consumes: `geom.AABB`, `world.View`, `entity.Body`, and `sim.Budget`.
- Produces: `collision.Move(view world.View, body entity.Body, delta geom.Vec3, order geom.AxisOrder, budget *sim.Budget) (collision.Result, error)` and ordered contacts.

- [ ] **Step 1: Write collision scenarios first**

Cover empty space, a floor, a wall, a ceiling, a corner, a one-block step, a step blocked by a ceiling, negative coordinates, touching boxes, multiple boxes in shuffled input order, unknown collision data, and candidate-budget exhaustion.

```go
func TestMoveStopsAtWall(t *testing.T) {
	body := entity.Body{Bounds: mustAABB(0, 0, 0, 0.6, 1.8, 0.6)}
	view := mctest.CollisionWorld(mustAABB(1, 0, 0, 2, 2, 1))
	limits, err := sim.NewLimits()
	if err != nil { t.Fatal(err) }
	got, err := collision.Move(view, body, geom.Vec3{X: 2}, geom.OrderYXZ, sim.NewBudget(limits))
	if err != nil { t.Fatal(err) }
	if got.Applied.X != 0.4 { t.Fatalf("X = %v, want 0.4", got.Applied.X) }
}
```

Add `mustAABB` as a private test helper that calls `geom.NewAABB` and fails the
test on invalid input.

- [ ] **Step 2: Verify the scenarios fail**

Run `devbox run -- task test -- ./collision`.

- [ ] **Step 3: Implement axis clipping and contacts**

Clip motion on each profile-supplied axis. Sort candidate boxes canonically before clipping. Record the axis, obstacle box, requested displacement, and applied displacement in each contact.

```go
type Result struct {
	Applied  geom.Vec3
	Blocked  geom.Vec3
	Bounds   geom.AABB
	Contacts []Contact
	OnGround bool
}
```

- [ ] **Step 4: Add stepping and unknown propagation**

Compare the direct path with the profile-permitted step path. Choose the path with greater horizontal distance and the profile-defined tie rule. Return all dependencies and no movement result if the collision query is unknown.

- [ ] **Step 5: Run focused tests and a fixed benchmark**

Run `devbox run -- task test -- ./collision`, `go test -bench=BenchmarkMoveDense -benchmem ./collision` inside Devbox, and `devbox run -- task lint`.

Expected: shuffled candidates produce the same contacts and digest inputs. Do not commit.

### Task 6: Adapt shared game data and construct both Java profiles

**Prerequisite:** `minecraft-protocol/data.Set`, generated Java 1.8.9 data, and generated Java 26.1 data exist and pass their own generation checks.

**Files:**

- Modify: `minecraft-simulation/go.mod`
- Create: `minecraft-simulation/internal/buildcheck/imports_test.go`
- Create: `minecraft-simulation/profile/java/internal/mcdata/adapter.go`
- Create: `minecraft-simulation/profile/java/internal/mcdata/adapter_test.go`
- Create: `minecraft-simulation/profile/java/v1_8/profile.go`
- Create: `minecraft-simulation/profile/java/v1_8/profile_test.go`
- Create: `minecraft-simulation/profile/java/v26_1/profile.go`
- Create: `minecraft-simulation/profile/java/v26_1/profile_test.go`

**Interfaces:**

- Consumes: `*data.Set` from `minecraft-protocol/data`.
- Produces: `v1_8.New(*data.Set)` for Java 1.8.9 and `v26_1.New(*data.Set)` for Java 26.1.2.

- [ ] **Step 1: Add the shared data dependency and import-boundary test**

Add `github.com/go-theft-craft/minecraft-protocol` to `go.mod`, using a local
`replace` only for workspace development. Walk Go imports in
`internal/buildcheck/imports_test.go` and reject every
`minecraft-protocol` import outside `profile/java` and every import below the
protocol module other than `data`.

- [ ] **Step 2: Write data rejection tests**

Reject a nil set, a mismatched Minecraft data family, missing collision shapes, an invalid box, missing player dimensions, and an unstable data digest.

- [ ] **Step 3: Build simulation-owned immutable data**

Convert blocks, collision shapes, entity dimensions, materials, attributes, and fluids into private profile values. Normalize names to namespaced IDs. Sort every registry before hashing. Do not retain mutable maps or slices from `data.Set`.

- [ ] **Step 4: Construct exact official identities**

In `v1_8/profile.go`:

```go
var ID = sim.ProfileID{Edition: "java", GameVersion: "1.8.9", RulesRevision: "vanilla.1"}
func New(set *data.Set) (sim.Profile, error)
```

In `v26_1/profile.go`:

```go
var ID = sim.ProfileID{Edition: "java", GameVersion: "26.1.2", RulesRevision: "vanilla.1"}
func New(set *data.Set) (sim.Profile, error)
```

Register only the collision and movement phases needed by this plan. Use constants and operation order from the approved reference notes. Keep version differences in their matching package.

- [ ] **Step 5: Prove the import boundary**

Run:

```bash
devbox run -- task test -- ./profile/java/...
devbox run -- task test -- ./internal/buildcheck
```

Expected: both profiles validate, and no core package imports `minecraft-protocol`.

- [ ] **Step 6: Run both data-generation gates**

Run `devbox run -- task generate:check` in `minecraft-protocol`, then run `devbox run -- task test -- ./profile/java/...` in `minecraft-simulation`.

Expected: profiles use committed generated data without local edits. Do not commit.

### Task 7: Add cross-version player movement

**Files:**

- Create: `minecraft-simulation/movement/command.go`
- Create: `minecraft-simulation/movement/rules.go`
- Create: `minecraft-simulation/movement/player.go`
- Create: `minecraft-simulation/movement/change.go`
- Create: `minecraft-simulation/movement/event.go`
- Create: `minecraft-simulation/movement/player_test.go`
- Create: `minecraft-simulation/mctest/scenario.go`
- Create: `minecraft-simulation/mctest/testdata/java-1.8.9/player/*.json`
- Create: `minecraft-simulation/mctest/testdata/java-26.1.2/player/*.json`

**Interfaces:**

- Consumes: collision results and profile-supplied `movement.Rules`.
- Produces: `movement.Intent`, `movement.EntityChange`, `movement.ContactEvent`, and the player movement phase.

- [ ] **Step 1: Add protocol-free golden scenarios**

Add fixtures for idle, walk, sprint, jump, airborne input, sneak, ledge sneak, wall collision, step-up, ceiling collision, climb, water entry, water exit, external impulse, and unknown neighboring blocks. Each fixture contains the profile ID, data digest, initial state, commands, expected changes, expected events, and expected dependencies.

- [ ] **Step 2: Define semantic movement input**

```go
type Intent struct {
	ID       sim.CommandID
	Seq      uint64
	Actor    entity.ID
	Forward  float32
	Strafe   float32
	Jump     bool
	Sneak    bool
	Sprint   bool
}
```

Reject non-finite input and values outside `[-1, 1]`. Do not include packet cadence or wire units.

- [ ] **Step 3: Implement the profile-driven operation order**

Separate input acceleration, gravity, fluid response, collision, ground friction, drag, and pose changes into named internal operations. Both profiles call shared operations only where the comparison table and fixtures prove identical behavior.

- [ ] **Step 4: Make unknown movement atomic**

If collision, fluid, or environmental data is unknown, return the sorted dependencies with no `EntityChange`, contact event, or presentation event.

- [ ] **Step 5: Run both profile suites**

Run `devbox run -- task test -- ./movement ./mctest ./profile/java/...`.

Expected: every fixture passes under both the direct kernel and shuffled internal map construction. Do not commit.

### Task 8: Add dropped-item and arrow movement

**Files:**

- Create: `minecraft-simulation/item/rules.go`
- Create: `minecraft-simulation/item/phase.go`
- Create: `minecraft-simulation/item/phase_test.go`
- Create: `minecraft-simulation/projectile/arrow.go`
- Create: `minecraft-simulation/projectile/hit.go`
- Create: `minecraft-simulation/projectile/phase_test.go`
- Create: `minecraft-simulation/mctest/testdata/java-1.8.9/item/*.json`
- Create: `minecraft-simulation/mctest/testdata/java-26.1.2/item/*.json`
- Create: `minecraft-simulation/mctest/testdata/java-1.8.9/arrow/*.json`
- Create: `minecraft-simulation/mctest/testdata/java-26.1.2/arrow/*.json`

**Interfaces:**

- Consumes: entity state, collision queries, and version-specific item and arrow rules.
- Produces: dropped-item state changes, arrow state changes, ordered hit events, and presentation events.

- [ ] **Step 1: Add golden item and arrow scenarios**

For items, cover throw arc, ground rest, water, lava, wall collision, and unknown ground. For arrows, cover free flight, gravity and drag, block hit, entity hit ordering, water drag, embedding, and unknown path data.

- [ ] **Step 2: Implement dropped-item motion without pickup or merging**

Apply the reviewed gravity, move, collision, and drag order for each profile. Preserve numeric narrowing points. Return an entity state change and contact events. Keep pickup delay, merging, and despawning outside this task.

- [ ] **Step 3: Implement arrow motion and hit ordering**

Ray cast both blocks and entities over the swept path. Sort equal-distance candidates with the profile rule and stable entity ID. Emit a semantic hit event. Do not apply combat damage in this plan.

- [ ] **Step 4: Register both phases in both official profiles**

Insert item and arrow phases at the exact boundaries recorded in each reference note. Update profile manifest golden tests.

- [ ] **Step 5: Run focused suites**

Run `devbox run -- task test -- ./item ./projectile ./mctest ./profile/java/...` and `devbox run -- task lint`.

Expected: both profile fixture families pass. Do not commit.

### Task 9: Add canonical encoding, result digests, and replay

**Files:**

- Create: `minecraft-simulation/replay/codec.go`
- Create: `minecraft-simulation/replay/digest.go`
- Create: `minecraft-simulation/replay/record.go`
- Create: `minecraft-simulation/replay/replay.go`
- Create: `minecraft-simulation/replay/replay_test.go`
- Create: `minecraft-simulation/mctest/fixture.go`
- Create: `minecraft-simulation/mctest/runner.go`

**Interfaces:**

- Consumes: profile type registrations, `sim.TickInput`, and `sim.TickResult`.
- Produces: `replay.EncodeInput`, `replay.EncodeResult`, `replay.DigestResult`, `replay.Record`, `replay.Run`, and the shared fixture runner.

- [ ] **Step 1: Write canonicalization tests**

Test signed zero, map insertion order, shuffled source slices, duplicate type registrations, unknown extension types, truncated records, a mismatched profile manifest, a mismatched data digest, and a changed event order.

- [ ] **Step 2: Define the recording envelope**

```go
type Record struct {
	FormatVersion uint32
	Manifest      sim.Manifest
	Input         []byte
	Expected      []byte
	Digest        [32]byte
}
```

Use a length-delimited binary envelope with fixed byte order. Encode floats from `math.Float64bits` after `sim.CanonicalZero`. Encode every collection in its contract order. Do not use Go map iteration or `gob`.

- [ ] **Step 3: Bind custom values to registered codecs**

Require every command, change, event, and extension component type ID to have one canonical codec in the profile registry. Reject an unregistered value before running or recording a tick.

- [ ] **Step 4: Compute and verify result digests**

Hash the profile manifest identity, base revision, tick, completeness, ordered outcomes, dependencies, changes, events, and random state with SHA-256. Set `TickResult.Digest` only after successful canonical encoding.

- [ ] **Step 5: Run replay tests twice**

Run:

```bash
devbox run -- task test -- ./replay ./mctest
devbox run -- task test -- ./replay ./mctest
```

Expected: both runs produce the same fixture digests. Do not commit.

### Task 10: Add the atomic in-memory runtime

**Files:**

- Create: `minecraft-simulation/runtime/store.go`
- Create: `minecraft-simulation/runtime/memory.go`
- Create: `minecraft-simulation/runtime/runtime.go`
- Create: `minecraft-simulation/runtime/runtime_test.go`

**Interfaces:**

- Consumes: `sim.Kernel`, an immutable profile, commands, and canonical change sets.
- Produces: `runtime.Store`, `runtime.MemoryStore`, `runtime.Runtime.Advance`, snapshot publication, and stale-revision rejection.

- [ ] **Step 1: Write store and runtime tests**

Test atomic apply, stale revision, failed tick, incomplete tick, cancelled tick, scheduled command ordering, old snapshot isolation, concurrent readers, and one writer per simulation scope.

- [ ] **Step 2: Define the store contract**

```go
type Store interface {
	Snapshot(context.Context, sim.Scope) (sim.TickInput, error)
	Apply(context.Context, sim.Revision, []sim.Change, sim.RandomState) (sim.Revision, error)
}
```

`Apply` compares the base revision and changes state under one write lock. It publishes the next immutable snapshot only after every change validates.

- [ ] **Step 3: Implement tick advancement**

```go
func (r *Runtime) Advance(ctx context.Context, scope sim.Scope, commands []sim.Command) (sim.TickResult, error)
```

Load one snapshot, attach ordered commands, run the kernel, and apply only a complete successful result. Return incomplete results without calling `Store.Apply`.
Set `TickInput.Profile` from the immutable profile that constructed the
runtime before calling the kernel.

- [ ] **Step 4: Add deterministic scheduling**

Order scheduled work by tick, phase ID, block position or entity ID, and insertion sequence. Do not use timer goroutines or wall-clock deadlines to decide simulation order.

- [ ] **Step 5: Run race and replay checks**

Run `devbox run -- task test -- ./runtime ./replay ./mctest` and `devbox run -- task lint`.

Expected: race tests pass, stale results never mutate the store, and runtime results match direct kernel digests. Do not commit.

### Task 11: Prove server and client reuse with real adapters

**Prerequisites:** The server uses shared `minecraft-protocol` game data, and the headless client has immutable observed world and entity snapshots.

**Files:**

- Create: `server/internal/server/simulation/view.go`
- Create: `server/internal/server/simulation/adapter.go`
- Create: `server/internal/server/simulation/adapter_test.go`
- Modify: `server/go.mod`
- Modify: `server/devbox.json`
- Create: `headless-minecraft/prediction/predictor.go`
- Create: `headless-minecraft/prediction/view.go`
- Create: `headless-minecraft/prediction/predictor_test.go`
- Modify: `headless-minecraft/go.mod`
- Create: `minecraft-simulation/mctest/testdata/consumers/*.json`

**Interfaces:**

- Consumes: the official profiles, server state, client snapshots, and shared fixtures.
- Produces: a server snapshot adapter and `prediction.Predictor` that call the same `sim.Kernel` without sharing storage implementations.

- [ ] **Step 1: Align the server toolchain and add local dependencies**

Update the server to Go 1.26.5 and the same `openserbia/go-flake` Go pin. Add local development requirements in both consumers:

```go
require github.com/go-theft-craft/minecraft-simulation v0.0.0

replace github.com/go-theft-craft/minecraft-simulation => ../minecraft-simulation
```

Do not publish a tag with a local replacement.

- [ ] **Step 2: Adapt server state without moving ownership**

Map `world.World.GetBlock` and shared collision data to `world.View`. Map `player.Position` and server entity state to immutable `entity.State`. Sort IDs and boxes before returning them. Keep sessions, locks, persistence, and packet writes outside the adapter.

- [ ] **Step 3: Add the client prediction entry point**

```go
type Predictor struct {
	Kernel  sim.Kernel
	Profile sim.Profile
}

func (p Predictor) Predict(ctx context.Context, snapshot Snapshot, commands []sim.Command) (sim.TickResult, error)
```

Map known client blocks, entities, and collision data to immutable views. Set
`TickInput.Profile` to `Predictor.Profile`. Preserve unknown chunks and missing
registries as unknown dependencies. Do not apply results to the observed
authoritative snapshot.

- [ ] **Step 4: Run the same fixture through both adapters**

Use one flat-world player movement fixture, one unknown-chunk fixture, one dropped-item fixture, and one arrow fixture. Assert that the server adapter and the complete client adapter produce the kernel fixture digest. Assert that the partial client fixture returns incomplete with no changes or events.

- [ ] **Step 5: Run all consumer checks**

Run `devbox run -- task test -- ./internal/server/simulation` in `server`. Run `devbox run -- task test -- ./prediction` in `headless-minecraft`. Then run lint and build tasks in both repositories.

Expected: both consumers compile against one simulation module and pass the same complete-state scenarios. Do not commit.

### Task 12: Run differential conformance and close the first slice

**Files:**

- Create: `minecraft-simulation/mctest/differential.go`
- Create: `minecraft-simulation/mctest/differential_test.go`
- Create: `minecraft-simulation/reference/java/1.8.9/conformance.md`
- Create: `minecraft-simulation/reference/java/26.1.2/conformance.md`
- Modify: `minecraft-simulation/README.md`
- Create: `minecraft-simulation/CHANGELOG.md`
- Create: `minecraft-simulation/ROADMAP.md`
- Create: `minecraft-simulation/RELEASING.md`

**Interfaces:**

- Consumes: controlled Java reference instances, all golden fixtures, both official profiles, and both consumer adapters.
- Produces: evidence for the first cross-version slice and documented deferred work.

- [ ] **Step 1: Capture independent controlled-world traces**

Run every player, item, and arrow scenario against controlled Java 1.8.9 and 26.1.2 instances. Record the command sequence, initial state, observed per-tick state, server corrections, artifact digest, data digest, and capture procedure. Store no packet dependency in the fixture consumed by the kernel.

- [ ] **Step 2: Compare every captured tick**

Make the differential runner report the first mismatched field, phase, and tick. Correct the matching version package or fixture provenance. Do not relax comparisons with a tolerance unless the reference behavior itself has a documented range.

- [ ] **Step 3: Verify cross-platform canonical output**

Run the fixture suite on Linux amd64 and Linux arm64. Save only the expected canonical digests in the conformance notes. A digest mismatch blocks completion.

- [ ] **Step 4: Document public status and deferred scope**

Mark the module pre-alpha. List player movement, dropped-item motion, arrow motion, collision, replay, and the two official profiles as implemented. Keep fluids beyond occupancy, TNT, explosions, vehicles, block simulation, combat, and AI in `ROADMAP.md`.

- [ ] **Step 5: Run every verification gate**

Run `devbox run -- task verify` in this order:

```text
minecraft-protocol
minecraft-simulation
server
headless-minecraft
```

Then run `git status --short` separately in all four repositories. Expected: every verification gate passes, no decompiled artifact is tracked, no core simulation package imports protocol code, and only the planned scope is changed. Do not commit.

## Plan completion criteria

The first subproject is complete only when:

- Both official profiles pass their research-backed golden fixtures.
- A complete server view and a complete client view produce the same canonical digest.
- A partial client view returns explicit dependencies and no applicable changes or events.
- Replaying the same input produces the same result on Linux amd64 and Linux arm64.
- Core packages contain no `minecraft-protocol` import.
- Official Java profiles import only `minecraft-protocol/data`.
- The runtime rejects stale, failed, cancelled, and incomplete results without state mutation.
- The repository contains no decompiled code, game jar, mapping archive, or Mojang asset.
- Every repository verification gate passes.
