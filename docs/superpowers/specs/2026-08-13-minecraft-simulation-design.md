# Minecraft simulation design

## Status

The user approved this design on 2026-08-13. Implementation requires the
matching implementation plan and an explicit request to execute it.

## Purpose

`github.com/go-theft-craft/minecraft-simulation` provides deterministic
Minecraft simulation for Go clients and servers. The module reproduces
observable Java Edition behavior for selected game versions. Applications can
also construct custom profiles with Go rule implementations.

The first supported profiles are Java Edition 1.8.9 and Java Edition 26.1.2.
Their package families are `v1_8` and `v26_1`. A later design can add Bedrock
Edition. The public simulation API does not use protocol numbers, packet IDs,
packet structs, connection states, or encoded wire units.

## Goals

The module has these goals:

- Produce identical state changes and ordered events for the same profile,
  snapshot, commands, random state, and game data on every supported platform.
- Match observable vanilla behavior, including version-specific operation
  order, numeric conversions, and quirks.
- Review decompiled Java client and server implementations to identify exact
  algorithms, phase order, constants, casts, random use, and version changes.
- Let a server run authoritative simulation and a client run prediction with
  the same deterministic kernel.
- Let clients simulate partial worlds without treating unknown data as air.
- Keep application storage, networking, persistence, rendering, and AI outside
  the simulation kernel.
- Support custom gameplay through immutable data overrides and trusted Go rule
  implementations.
- Bound work per tick so hostile or broken state cannot consume unbounded CPU,
  memory, or output queues.
- Record enough identity and state to replay and compare a simulation result.

## Non-goals

The module does not provide these features:

- Network protocols, authentication, connections, packet routing, or packet
  conversion
- Database or file persistence
- Rendering, audio playback, or user interfaces
- Pathfinding, goal selection, mob AI, fish AI, combat strategy, or automation
  scheduling
- Inventory user interfaces or crafting planners
- A generic engine for games that resemble Minecraft
- A public entity-component-system storage framework
- A sandbox for untrusted Go extensions

An AI controller can submit an entity intent. The simulation decides the
deterministic consequences of that intent.

## Repository relationships

The repositories have distinct responsibilities:

- `minecraft-protocol` owns wire codecs and immutable versioned game data.
- `minecraft-simulation` owns deterministic game-state transitions.
- `headless-minecraft` owns observed client state, prediction history,
  reconciliation, commands, and packet conversion.
- `server` owns authoritative application state, network input validation,
  persistence, player sessions, and packet broadcasting.

Core simulation packages do not import `minecraft-protocol`. Official Java
profile packages can import only `minecraft-protocol/data`. They cannot import
wire, codec, packet, connection, or protocol-version packages.

The package dependency direction is:

```text
minecraft-protocol/data
            ^
            |
minecraft-simulation/profile/java
            |
            v
minecraft-simulation core
            ^
            |
      server and client
```

The Java profile packages depend on both the core packages and the shared game
data. The core packages remain independent of the protocol module.

## Simulation boundary

The module owns deterministic state transitions for these areas:

- Entity movement, collision, ray casting, stepping, pushing, mounting, and
  passengers
- Ground, air, swimming, climbing, gliding, flying, and no-clip movement
- Players, living mobs, aquatic bodies, dropped items, experience orbs, and
  projectiles
- Boats, minecarts, falling blocks, fishing bobbers, primed TNT, and other
  entities with physical state
- Fluid occupancy, flow, displacement, currents, and buoyancy
- Explosions, damage, knockback, fire, suffocation, drowning, hunger,
  regeneration, and status effects
- Scheduled block ticks, random block ticks, pistons, redstone, crop growth,
  portals, weather effects, and dimension rules
- Entity spawning consequences, removal, merging, pickup, and despawning
- Environmental interactions such as ladders, vines, cobwebs, ice, slime,
  honey, soul sand, bubble columns, powder snow, and the world border

Particles, sounds, and animations are ordered presentation events. They are
not simulated entities unless a custom profile gives them gameplay state.

## Package layout

The initial module uses this package layout:

```text
minecraft-simulation/
  sim/                    Tick inputs, results, commands, changes, and RNG
  geom/                   Vectors, block positions, AABBs, and voxel shapes
  collision/              Broad phase, sweeps, contacts, and stepping
  world/                  World views, block queries, and scheduled work
  entity/                 Bodies, attributes, effects, and lifecycle state
  movement/               Ground, air, swimming, climbing, gliding, and flying
  item/                   Dropped-item motion and environmental response
  projectile/             Ballistic motion and hit behavior
  vehicle/                Boats, minecarts, mounts, and passengers
  fluid/                  Fluid motion, flow, displacement, and buoyancy
  block/                  Block ticks, falling blocks, pistons, and fire
  explosion/              Propagation, exposure, damage, and block changes
  profile/                Profile contracts, manifests, and custom builder
  profile/java/v1_8/      Official Java Edition 1.8.9 rules
  profile/java/v26_1/     Official Java Edition 26.1.2 rules
  runtime/                Optional tick runner and in-memory state store
  replay/                 Canonical recording, hashing, and replay
  mctest/                 Protocol-free conformance fixtures and helpers
```

Packages organize behavior by mechanic. They do not duplicate a complete
physics engine for each entity type. Entity definitions compose bodies,
movement rules, environmental responses, and type-specific interaction rules.

## Deterministic kernel

The kernel consumes one immutable input and produces one result:

```go
type Kernel interface {
	Step(ctx context.Context, input TickInput) (TickResult, error)
}
```

`TickInput` contains:

- The immutable simulation profile
- The snapshot revision and simulation tick
- Immutable world, entity, dimension, and environment views
- A simulation scope
- Ordered commands
- Explicit serialized random state
- Deterministic work limits

`TickResult` contains:

- The input revision and tick
- An ordered atomic change set
- Ordered domain events
- Ordered presentation events
- Ordered command outcomes
- Updated serialized random state
- The data dependencies read during the tick
- A completeness result
- A canonical result digest

The kernel does not read the wall clock, global random state, network state, or
mutable application objects. A cancelled call returns no applicable change set.

## State views and ownership

The server and the client retain their own storage models. The kernel reads
immutable views that remain valid for the complete `Step` call. A view must
provide deterministic lookup and iteration behavior.

Every world lookup distinguishes these states:

- Known air
- Known block or fluid state
- Unknown

If a rule in the requested simulation scope needs unknown data, the result
identifies the missing regions, entities, or registries. The kernel does not
fabricate state. The complete tick result is incomplete and contains no
applicable changes or emitted events.

A change set records its base revision. A store applies the change set only if
the store still has that revision. The revision check prevents an older
simulation result from overwriting newer state.

The optional `runtime` package advances ticks, runs scheduled work, publishes
snapshots, and applies change sets atomically. The package accepts a storage
interface and includes an in-memory reference store. It does not persist data.

The server uses the runtime for authoritative state transitions. The client can
apply predicted change sets to a forked snapshot. After a server correction,
the client discards the affected fork and replays retained commands from the
new authoritative snapshot.

## Commands, changes, and events

Commands express simulation intent in semantic units. Examples include a
movement intent, an interaction intent, an external impulse, a scheduled block
tick, and an authoritative spawn request. Commands do not contain packet IDs or
encoded wire values.

The adapter that creates a command remains responsible for authentication and
network-level authorization. The profile validates whether the actor and the
current state permit the command.

Change sets contain ordered state operations for entities, blocks, fluids,
scheduled work, world state, and random state. A change set is either fully
applicable or not applicable.

Domain events report simulation facts such as collision, damage, ignition,
detonation, pickup, spawn, and removal. Presentation events request particles,
sounds, and animations. The server can convert events into packets. The client
can convert them into observations or ignore presentation events.

## Tick pipeline

Each profile defines the exact tick phases and their total order. Profiles can
differ in phase order, formulas, numeric conversions, collision rules, random
use, and event order.

The profile builder gives every phase a namespaced ID. A custom profile can
replace a rule group or insert a phase at a named boundary. Construction fails
if a profile contains duplicate phases, missing dependencies, dependency
cycles, incompatible game data, or invalid work limits.

The canonical tick runs on one goroutine in the first release. Applications
can run independent dimensions or isolated simulation scopes concurrently.
Internal parallel execution requires benchmarks and conformance evidence that
prove identical output.

## Profile identity and selection

A profile identity separates the game version from the wire protocol:

```go
type ProfileID struct {
	Edition       string
	GameVersion   string
	RulesRevision string
}
```

Official constructors return complete immutable profiles:

```go
profile := java18.New(dataBundle)
profile := java261.New(dataBundle)
```

A client or server adapter maps a negotiated connection and selected game data
to a profile. For example, an adapter can map Java protocol 47 to the official
Java 1.8.9 profile. A custom wire protocol can select that same profile.

The profile manifest records:

- The profile ID and implementation revision
- The required game-data identity and digest
- The tick phases and their order
- The selected rule-group implementations
- Registered extension component schemas
- Numeric and random compatibility modes
- Work limits and supported capabilities

A replay fails before simulation if its manifest does not match the selected
profile and game data.

## Java compatibility

Official Java profiles preserve observable vanilla behavior. They use
Java-compatible `float64`, `float32`, signed integer overflow, casts, rounding,
and operation order at version-defined points. They do not replace Java numeric
behavior with fixed-point arithmetic.

If a standard-library operation does not guarantee the required cross-platform
result, the profile supplies a canonical implementation. A profile serializes
all random state. Named random streams remain separate when the selected game
version uses separate sources.

The same profile, snapshot, commands, random state, and game data must produce
the same ordered result and canonical digest on every supported platform.

## Custom Go extensions

Custom profiles use a validated builder based on an immutable profile:

```go
profile, err := simulation.NewProfileBuilder(base).
	ID(customID).
	ReplaceMovement(customMovement).
	ReplaceFluidRules(customFluids).
	InsertPhase(afterEntityMotion, customPhase).
	Build()
```

An extension can register:

- Namespaced entity, block, dimension, and world-state components
- A validator and canonical codec for each component
- Deterministic copy and hashing behavior
- Rule handlers and named tick phases
- Custom commands, changes, and events
- Work-budget accounting

Built-in state uses explicit Go structs. Extension components attach registered
state to known simulation objects. The design does not expose a general public
entity-component system.

The runtime rejects unregistered state because it cannot snapshot, replay,
hash, or compare that state. A custom rule receives external input only through
the tick context. The extension contract prohibits wall-clock reads, global
randomness, result-affecting goroutines, hidden mutable state, and unordered
iteration.

Custom Go code is trusted. The module cannot sandbox it. An audit mode can run
a tick twice from the same input and compare result digests to detect common
sources of nondeterminism.

## Error and incomplete-result model

The module distinguishes these outcomes:

- Profile construction errors report missing rules, invalid phase ordering,
  incompatible data, invalid extensions, or invalid limits.
- Command rejection records a deterministic command outcome. It does not abort
  unrelated simulation work.
- Missing snapshot data produces an incomplete result with explicit data
  dependencies.
- A stale base revision prevents change-set application.
- A broken invariant or rule failure aborts the tick and returns no applicable
  change set.
- Context cancellation aborts the tick and returns no applicable change set.
- A work-limit failure identifies the exhausted deterministic budget and aborts
  the affected simulation scope.

Work limits cover entity steps, block updates, scheduled events, collision
candidates, explosion traversal, fluid updates, extension work, and emitted
events.

## Conformance and testing

Every official behavior has protocol-free scenario fixtures. A fixture records:

- The profile ID and game-data digest
- The initial snapshot and simulation scope
- Ordered commands
- Serialized random state
- Expected changes, events, dependencies, and command outcomes
- The expected canonical result digest

The same fixture runner tests the kernel, the in-memory runtime, the server
adapter, and the client prediction adapter.

The test suite includes:

- Unit tests for numeric conversions, geometry, collision, and rule functions
- Golden tick traces for every supported Java profile
- Differential tests against controlled vanilla Java instances
- Replay and canonical digest tests
- Tests on every supported CPU architecture and operating system
- Property tests for collision and state invariants
- Fuzz tests for malformed custom data, extreme coordinates, and extension
  codecs
- Benchmarks with fixed work limits
- Race tests for snapshots, runtime publication, and independent simulation
  scopes

Reference tests can use controlled servers or instrumented test worlds to
capture observable behavior. Stored fixtures record their source and generation
method.

## Reference implementation research

Maintainers can decompile legally obtained Java client and server versions in a
local research workspace. Decompiled code is a reference for behavior that
black-box tests cannot expose clearly. Examples include tick phase order,
floating-point casts, collision iteration, random-stream selection, and the
placement of version-specific quirks.

The standalone
[`2026-08-13-minecraft-reference-extraction.md`](../plans/2026-08-13-minecraft-reference-extraction.md)
plan owns artifact download, vanilla deobfuscation, decompilation, symbol
indexing, and complete domain mapping. Simulation implementation consumes its
reviewed catalogs and notes rather than duplicating the extraction workflow.

Research notes record:

- The exact game version and artifact digest
- Whether the source was the client or the server
- The decompiler and mapping versions
- The relevant class and method identities
- A prose description of the observed algorithm and operation order
- The conformance scenarios that verify the independent Go implementation

The public repository does not contain copied decompiled methods, substantial
decompiled excerpts, obfuscated game classes, or Mojang assets. Maintainers
write the Go rules independently from the recorded behavior. Public fixtures
contain original test inputs and expected outputs rather than extracted game
code.

Comparing decompiled implementations across Java 1.8.9 and Java 26.1.2 is part
of profile development. A behavior that differs between those versions remains
in the matching profile. Shared code contains only behavior that conformance
tests prove identical.

## First implementation subproject

The first subproject proves one narrow vertical slice on both Java versions. It
includes:

- Snapshot, command, change-set, event, revision, and random-state contracts
- Geometry, voxel shapes, swept AABB collision, stepping, and contacts
- Static block collision and basic fluid occupancy
- Player ground, air, jump, sprint, sneak, swim, climb, and external impulse
  behavior where the selected version supports each behavior
- Dropped-item motion
- Arrow motion and collision as the first projectile family
- Java 1.8.9 and Java 26.1.2 profiles
- Partial-world dependency reporting
- The in-memory runtime and replay support
- Shared conformance fixtures for server and client entry points

The first subproject does not implement fluid propagation, explosions, TNT,
vehicles, redstone, pistons, random block ticks, mob AI, or full combat.

## Later subprojects

Later work proceeds in this order:

1. Add living mobs, aquatic bodies, experience orbs, item merging, despawning,
   mounts, passengers, and entity pushing.
2. Add throwable families, fishing bobbers, tridents, explosions, primed TNT,
   falling blocks, and fire.
3. Add boats, minecarts, gliding, portals, the world border, and unusual block
   surfaces.
4. Add fluid propagation, scheduled and random block ticks, pistons, redstone,
   crop growth, weather, and dimension effects.
5. Add damage, combat consequences, hunger, breathing, status effects, and
   other deterministic gameplay transitions.

Each subproject requires its own approved implementation plan and conformance
fixtures. Public API stability starts only after both official profiles and
both consumers pass the shared compatibility suite.

## Acceptance criteria for the architecture

The architecture is ready for implementation planning when all of these
statements are true:

- The simulation core has no dependency on protocol or packet packages.
- Official Java profiles import only shared game-data packages from
  `minecraft-protocol`.
- The server and the client can construct the same profile and call the same
  kernel without sharing storage implementations.
- A partial client snapshot cannot turn unknown state into known air.
- A complete input produces one cross-platform canonical result digest.
- A stale or failed result cannot mutate caller state.
- Every rule consumes deterministic random state and deterministic work
  budgets.
- Novel behavior requires registered Go code and canonically encoded extension
  state.
- The first subproject covers Java 1.8.9 and Java 26.1.2 before broader
  simulation work begins.
