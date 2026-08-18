# Constructed Components, World State, and Operations Implementation Plan

> **Status: tasks 1 through 6 complete; task 7 open, 2026-08-18.** Tasks 1
> through 6 shipped as M7 (eight observed domains, wire-ordered reducers on
> both protocols) and the outbound action path, `Client.Do`, which has since
> gained `ActionRespawn` and the interaction primitives. **Task 7 is the one
> piece still open**: `movement.Strategy` is not exported, so nothing proves a
> strategy defined outside the library works, and `examples/orbit` is the first
> caller to need it. The checkboxes below were never ticked; read the task
> list, not the boxes.

> **For agentic workers:** REQUIRED SUB-SKILL: Use `subagent-driven-development` or execute this plan inline one task at a time. Keep every checkbox current.

**Goal:** Build immutable observed state and a construction-time graph of replaceable gameplay components for movement, containers, inventory, crafting, digging, building, and interaction, including modded bodies, abilities, menus, multi-block tools, and strict recovery safeguards.

**Architecture:** `client.New` constructs and validates one component graph before network work begins. An immutable version profile supplies the protocol adapter, physics, collision, inventory synchronization, action ordering, capability defaults, and bounded wire limits. Built-in profiles reproduce vanilla behavior; callers can provide a complete custom profile or explicit overrides. `safety.Strict()` is the default and high-level automation requires a construction-time authorization declaration for the endpoint and scopes. Components reconcile with authoritative server outcomes, stop on uncertainty, and never retry ambiguous work.

**Tech Stack:** Go 1.26.5 from `openserbia/go-flake`, Devbox, Task, `headless-minecraft`, `minecraft-protocol` Java 26.1 protocol 775 data, immutable copy-on-write indexes, contexts, bounded queues, and fixture-server tests.

## Global Constraints

- Complete the shared protocol and headless authentication plans first.
- Work in `/home/ocharnyshevich/pet.projects/go-theft-craft/headless-minecraft`.
- Run project commands as `devbox run -- task <name>`.
- Leave changes uncommitted unless explicitly requested.
- Construct and validate the complete graph in `client.New`; perform no authentication, DNS, or network I/O until validation succeeds.
- Do not replace whole components while `Client.Run` is active.
- Give factories only declared, narrow dependencies; do not expose client internals through a service locator.
- Maintain state only from packets observed by the connection and never expose mutable internal maps or slices.
- Preserve unknown namespaced attributes, abilities, metadata, menu types, item components, recipes, and plugin payloads.
- Reevaluate effective capabilities by snapshot revision; never freeze runtime equipment, effect, pose, environment, or inventory rules at construction.
- Keep adapters version-specific and public state, component, and operation APIs version-neutral.
- Use vanilla-compatible conformance profiles for built-in versions. Keep custom versions injectable without version-number branches in gameplay components.
- Permit custom packet limits only within non-disableable process hard ceilings.
- Keep primitive operations bounded, context-cancellable, and explicit about timing and confirmation.
- Use `safety.Strict()` by default. Do not automatically retry, reconnect, resume, or acknowledge a circuit breaker.
- Do not detect anti-cheat plugins, tune against their thresholds, add human-like jitter, or ship identity spoofing.
- Do not proactively announce automation. Send only protocol-required identity and custom payloads unless the application explicitly adds an extension.
- Do not add autonomous goal selection, pathfinding, combat strategy, or a scheduler.
- The server remains authoritative. Never report a multi-block operation as atomic.

---

### Task 1: Define immutable observed snapshots

**Files:**
- Create: `world/snapshot.go`
- Create: `world/reducer.go`
- Create: `world/position.go`
- Create: `world/dimension.go`
- Create: `world/snapshot_test.go`
- Modify: `client/adapter.go`
- Modify: `client/client.go`

**Produces:** `world.Snapshot`, `world.Reducer`, atomic snapshot publication, and `Client.Snapshot()`.

- [ ] **Step 1: Write isolation and publication tests**

Test that an old snapshot remains unchanged after later reductions, returned collections cannot mutate stored state, concurrent readers pass the race detector, and one inbound packet publishes exactly one revision before its normalized events.

- [ ] **Step 2: Add core value types and immutable indexes**

```go
type Vec3 struct{ X, Y, Z float64 }
type BlockPos struct{ X, Y, Z int32 }

type Snapshot struct {
	Revision  uint64
	Player    Player
	Dimension Dimension
	Time      Time
}
```

Keep large maps and slices unexported. Expose focused lookup and range methods that return values or owned copies.

- [ ] **Step 3: Implement transactional reduction**

Clone only changed indexes, reject malformed packets without publication, increment the revision once, atomically publish, and only then release normalized events.

- [ ] **Step 4: Verify**

Run `devbox run -- task test:race -- ./world ./client`.

### Task 2: Construct and validate the component graph

**Files:**
- Create: `component/kind.go`
- Create: `component/factory.go`
- Create: `component/dependencies.go`
- Create: `component/graph.go`
- Create: `component/graph_test.go`
- Create: `safety/profile.go`
- Create: `safety/authorization.go`
- Create: `safety/outcome.go`
- Create: `safety/breaker.go`
- Create: `safety/event.go`
- Create: `safety/profile_test.go`
- Create: `version/profile.go`
- Create: `version/validate.go`
- Create: `version/builtin.go`
- Create: `version/profile_test.go`
- Create: `client/components.go`
- Modify: `client/options.go`
- Modify: `client/client.go`

**Produces:** typed component factories, complete version profiles, `client.Components`, strict default safeguards, scoped server authorization, circuit breakers, dependency validation, and construction-time replacement options.

- [ ] **Step 1: Write construction failure tests**

Cover a missing required factory, incomplete version profile, incompatible protocol and adapter, unsupported inventory semantics, limits above hard ceilings, duplicate provider, undeclared dependency access, dependency cycle, wrong produced type, factory error, panic containment, and an automation scope without endpoint authorization. Assert that no auth provider, resolver, dialer, or goroutine starts after validation fails.

- [ ] **Step 2: Define factory contracts**

```go
type Descriptor struct {
	Kind     Kind
	Requires []Kind
}

type Factory[T any] interface {
	Descriptor() Descriptor
	New(Dependencies) (T, error)
}
```

Provide a package-level generic `component.Require[T]` helper because Go 1.26 does not support generic methods. `Dependencies` exposes core ports and only the component kinds declared by the factory.

- [ ] **Step 3: Define the standard graph**

```go
type Components struct {
	Safety       safety.Policy
	Capabilities capability.Factory
	Body       body.Factory
	Physics    physics.Factory
	Collision  collision.Factory
	Movement   movement.Factory
	Containers container.Factory
	Inventory  inventory.Factory
	Crafting   crafting.Factory
	Interaction interaction.Factory
	Digging    digging.Factory
	Building   building.Factory
}
```

Supply `DefaultComponents()` with `safety.Strict()` and focused `WithMovement`, `WithContainers`, and equivalent options. Avoid an untyped `map[string]any` public API.

- [ ] **Step 4: Define complete version profiles**

Make `client.WithVersion` accept the protocol, adapter, physics, collision, inventory synchronization, action ordering, capability defaults, and requested limits as one validated value. Supply separate built-in Java 1.8 and Java 26.1 profiles. Allow explicit copy-and-override construction for modded servers and a complete custom implementation. Gameplay packages consume profile interfaces and never branch on protocol numbers.

- [ ] **Step 5: Define strict authorization and recovery**

Bind an authorization declaration to the normalized endpoint and explicit scopes such as observe, move, inventory, interact, dig, and build. Require it before high-level automation. Define typed outcomes, safety events, component-local uncertainty states, and manually acknowledged circuit breakers. Strict mode allows one unresolved state-changing operation per component and no automatic retry, reconnect, resume, or breaker reset.

Do not add anti-cheat detection, threshold tuning, timing randomization, proactive bot announcements, vanilla-brand spoofing, or false identity metadata. The default adapter sends only fields and payloads required by the selected protocol.

- [ ] **Step 6: Build once in `client.New`**

Topologically validate and construct the graph. Components receive stable state, sender, clock, protocol-capability, and data ports, not `*client.Client`. Freeze the graph before returning the client. Do not expose a runtime replace method.

- [ ] **Step 7: Verify**

Run `devbox run -- task test:race -- ./component ./version ./safety ./client`.

### Task 3: Preserve player facts and derive dynamic capabilities

**Files:**
- Create: `world/player.go`
- Create: `world/attribute.go`
- Create: `world/ability.go`
- Create: `world/item.go`
- Create: `world/environment.go`
- Create: `world/custom.go`
- Create: `world/time.go`
- Create: `world/weather.go`
- Create: `capability/value.go`
- Create: `capability/set.go`
- Create: `capability/engine.go`
- Create: `capability/vanilla.go`
- Create: `capability/engine_test.go`
- Create: `body/model.go`
- Create: `body/vanilla.go`
- Create: `body/model_test.go`
- Create: `physics/model.go`
- Create: `physics/vanilla.go`
- Create: `physics/model_test.go`
- Create: `collision/model.go`
- Create: `collision/vanilla.go`
- Create: `collision/model_test.go`
- Create: `internal/adapter/java/player.go`
- Create: `internal/adapter/java/player_test.go`

**Produces:** observed player facts, a replaceable revision-aware capability engine, replaceable body, physics, and collision models, and versioned vanilla defaults that defer to observed values.

- [ ] **Step 1: Add ordered transcript tests**

Feed join, position correction, health, experience, ability, attribute, effect, equipment, held-item, environment, custom payload, respawn, time, and weather packets. Include unknown namespaced attributes and custom payload-derived abilities. Assert exact revisions.

- [ ] **Step 2: Implement player reducers**

Confirm teleport IDs before publishing corrected position. Replace dimension-scoped state on respawn while preserving account identity. Keep raw unknown facts alongside normalized known fields.

- [ ] **Step 3: Implement dynamic capability evaluation**

Evaluate equipment, held items, effects, attributes, abilities, pose, environment, dimension, game mode, session registries, and custom payloads for one snapshot revision. Provide typed known capabilities plus immutable namespaced raw values. Give each typed capability explicit replace, add, multiply, min, max, enable, or disable semantics. Preserve the ordered contribution trace and provenance for each result. Cache only by revision.

- [ ] **Step 4: Define mechanics independently from observations**

The body model resolves dimensions, eye height, poses, collision shape, and scale. The physics model resolves gravity, step height, acceleration, movement speed, jump strength, air control, flight, and jump count. The collision model resolves version-specific block shapes, fluids, unloaded boundaries, coordinate rules, and swept-body tests. They receive a snapshot and profile data and return immutable values.

- [ ] **Step 5: Test changing mechanics**

Add fixtures for a one-block-tall body, custom scale, equipment-granted flight, water-only diving, velocity modifiers, non-vanilla gravity, double jump, and a potion that raises jump height to two blocks. Remove or expire each source and assert the next revision loses the capability. Verify provenance and custom plugin rules.

- [ ] **Step 6: Verify**

Run `devbox run -- task test:race -- ./world ./capability ./body ./physics ./collision ./internal/adapter/java`.

### Task 4: Track entities, registries, and chunks

**Files:**
- Create: `world/entity.go`
- Create: `world/entity_index.go`
- Create: `world/registry.go`
- Create: `world/chunk.go`
- Create: `world/section.go`
- Create: `world/palette.go`
- Create: `world/block_entity.go`
- Create: `internal/adapter/java/entity.go`
- Create: `internal/adapter/java/registry.go`
- Create: `internal/adapter/java/chunk.go`
- Create: `internal/adapter/java/world_test.go`
- Create: `internal/adapter/java/fuzz_chunk_test.go`

**Produces:** immutable entity lookup, per-session registry overlays, loaded chunks, block lookup, block entities, and lighting.

- [ ] **Step 1: Add entity lifecycle tests**

Cover spawn, relative move, teleport, velocity, rotation, metadata, unknown metadata, attributes, equipment, passengers, events, removal, and runtime-ID reuse.

- [ ] **Step 2: Add bounded chunk fixtures**

Cover single-value, indirect, and direct palettes; negative dimension height; boundary coordinates; updates; block entities; lighting; unload; truncated data; and oversized declared collections.

- [ ] **Step 3: Implement immutable indexes and overlay precedence**

Remove runtime-ID and UUID indexes together. Deep-copy mutable metadata and NBT. Resolve session registry data before generated static data.

- [ ] **Step 4: Decode palettes with explicit limits**

Validate bit widths, palette lengths, backing-long counts, section counts, and state IDs before allocation or indexing. Apply updates with structural sharing.

- [ ] **Step 5: Verify**

Run `devbox run -- task test:race -- ./world ./internal/adapter/java` and `devbox run -- task fuzz:smoke -- ./internal/adapter/java`.

### Task 5: Represent actual containers with generic slot semantics

**Files:**
- Create: `world/container.go`
- Create: `container/container.go`
- Create: `container/slot.go`
- Create: `container/layout.go`
- Create: `container/driver.go`
- Create: `container/registry.go`
- Create: `container/generic.go`
- Create: `container/container_test.go`
- Create: `internal/adapter/java/container.go`
- Create: `internal/adapter/java/container_test.go`

**Produces:** observed raw container state, generic click access, semantic slot references, dynamic inventory topology, and an injectable menu-driver registry.

- [ ] **Step 1: Write actual-open-screen tests**

Cover full content, single-slot updates, carried stack, state ID, integer properties, open, close, rejected clicks, corrections, response timeout, and a block interaction that opens an unexpected modded menu. Assert that the returned type is the server's actual namespaced menu type. A rejection or ambiguous timeout must discard pending projections and block writes until a complete authoritative state arrives.

- [ ] **Step 2: Implement safe stack ownership**

Represent empty slots explicitly. Deep-copy item components and NBT. Compare stacks by item identity, count, and complete component data.

- [ ] **Step 3: Define raw and semantic APIs**

```go
type SlotRef struct {
	Role  Role
	Index int
}

type Driver interface {
	Type() string
	Resolve(LayoutFacts, SlotRef) (int, bool)
}
```

The generic driver accepts protocol slot indices for every menu. Specialized drivers only map semantic references onto that same state.

Layouts distinguish server-advertised slots from rule-enabled slot groups. Recompute enabled groups for the current snapshot revision. Test worn equipment that enables extra inventory capacity, removal that disables it, and a correction that invalidates an in-flight slot choice. Never create a writable slot absent from the server-advertised layout.

- [ ] **Step 4: Add custom-driver registration**

Reject duplicate menu types unless replacement is explicit at construction. Freeze the registry after construction. Do not infer a layout from title text.

- [ ] **Step 5: Verify**

Run `devbox run -- task test:race -- ./world ./container ./internal/adapter/java`.

### Task 6: Add primitive interaction and confirmation ports

**Files:**
- Create: `interaction/controller.go`
- Create: `interaction/primitive.go`
- Create: `interaction/ordering.go`
- Create: `interaction/ordering_test.go`
- Create: `interaction/errors.go`
- Create: `interaction/controller_test.go`
- Create: `internal/session/confirmations.go`
- Create: `internal/session/confirmations_test.go`
- Create: `internal/adapter/java/interactions.go`
- Create: `internal/adapter/java/interactions_test.go`

**Produces:** bounded primitive packet operations, version-defined action ordering and barriers, and keyed server-confirmation waits.

- [ ] **Step 1: Write validation and confirmation tests**

Cover closed and pre-ready clients, missing authorization scope, stale revisions, unloaded targets, reach, unsupported capability, queue backpressure, context cancellation, disconnect, response-before-wait, independent concurrent waits, correction, uncertain outcome, blocked component, manual breaker acknowledgement, version-specific packet ordering and barriers, and sequence wrap.

- [ ] **Step 2: Define primitives**

Include chat, command, movement update, look, stance, use block, use item, place, attack, interact entity, dig phase, select held slot, raw container click, drop, and close. Primitives carry explicit positions, faces, hands, sequence IDs, and expected snapshot revision where applicable. The selected profile maps each operation to its required ordered packet sequence and synchronization barriers.

- [ ] **Step 3: Implement all-or-none packet enqueue**

Encode against one snapshot, register confirmation keys before enqueue, and enqueue every packet in a primitive or none. Bound pending confirmation registrations and preserve server corrections through the inbound reducer. Emit a structured safety outcome for rejection, correction, timeout, cancellation, and disconnect. Never infer success from local projection and never retry automatically.

- [ ] **Step 4: Verify**

Run `devbox run -- task test:race -- ./interaction ./internal/session ./internal/adapter/java`.

### Task 7: Implement replaceable movement with owned strategies

**Files:**
- Create: `movement/controller.go`
- Create: `movement/strategy.go`
- Create: `movement/manual.go`
- Create: `movement/bunnyhop.go`
- Create: `movement/controller_test.go`
- Create: `internal/adapter/java/movement.go`
- Create: `internal/adapter/java/movement_test.go`

**Produces:** one constructed movement controller, explicit one-shot movement, and controller-owned strategy switching.

- [ ] **Step 1: Write movement model tests**

Cover finite coordinates, yaw and pitch normalization, sprint, sneak, one-shot jump, one-block-tall collision checks, unknown collision data, double jump, equipment-granted flight, swimming, diving, velocity modifiers, potion-granted two-block jump, effect expiry, unexpected position and velocity corrections, repeated no progress, breaker acknowledgement, and cancellation.

- [ ] **Step 2: Implement manual movement**

Manual calls emit one update and never start a hidden ticker. Consult current-revision capabilities plus the injected body and physics components for feasibility. Strict mode refuses segments through unloaded or unknown collision data. Keep projected position separate and never overwrite observed state optimistically.

- [ ] **Step 3: Implement strategy ownership**

Allow `UseStrategy` only through the movement controller. Switching stops and joins the prior strategy before starting the next. An unexpected server correction stops the strategy, discards projection, emits a safety event, and opens the movement breaker until explicit acknowledgement. Implement an opt-in bunnyhop strategy with an explicit clock and tick policy. Do not replace the movement component or leak goroutines.

- [ ] **Step 4: Verify**

Run `devbox run -- task test:race -- ./movement ./internal/adapter/java`.

### Task 8: Implement inventory and crafting as composed components

**Files:**
- Create: `inventory/controller.go`
- Create: `inventory/executor.go`
- Create: `inventory/semantics.go`
- Create: `inventory/semantics_test.go`
- Create: `inventory/controller_test.go`
- Create: `crafting/recipe.go`
- Create: `crafting/source.go`
- Create: `crafting/planner.go`
- Create: `crafting/executor.go`
- Create: `crafting/crafting_test.go`
- Create: `internal/adapter/java/inventory.go`
- Create: `internal/adapter/java/inventory_test.go`

**Produces:** inventory mutation over generic containers and replaceable recipe source, planner, and executor.

- [ ] **Step 1: Write transaction tests**

Cover held-slot selection, click modes, item movement, drop, close, versioned state IDs or transaction acknowledgements, correction, declared recipes, recipe-book changes, insufficient ingredients, custom inventory semantics, and a custom recipe source.

- [ ] **Step 2: Implement the inventory executor**

Resolve semantic slots through the actual container driver, or accept raw slots through the generic API. Delegate state IDs, transaction IDs, changed-slot maps, acknowledgement rules, and resynchronization barriers to the selected version profile. Wait only when that profile exposes a matching confirmation.

- [ ] **Step 3: Compose crafting**

Keep recipe lookup pure. The planner returns an inspectable inventory-operation plan. The executor revalidates every step and returns structured partial progress if the server corrects or closes the menu.

- [ ] **Step 4: Verify**

Run `devbox run -- task test:race -- ./inventory ./crafting ./internal/adapter/java`.

### Task 9: Add tool-driven digging and building plans

**Files:**
- Create: `operation/plan.go`
- Create: `operation/progress.go`
- Create: `operation/limits.go`
- Create: `operation/executor.go`
- Create: `operation/executor_test.go`
- Create: `digging/behavior.go`
- Create: `digging/planner.go`
- Create: `digging/controller.go`
- Create: `building/behavior.go`
- Create: `building/matrix.go`
- Create: `building/planner.go`
- Create: `building/controller.go`
- Create: `digging/digging_test.go`
- Create: `building/building_test.go`

**Produces:** held-tool behavior resolution, explicit single- and multi-block plans, bounded execution, and structured progress.

- [ ] **Step 1: Write behavior-resolution tests**

Cover vanilla single-block behavior, custom item component behavior, plugin-payload behavior, vein mining, rectangular area digging, line and plane placement, rotated and mirrored matrices, missing inventory, and an unknown tool. Assert that an unknown tool never silently becomes an area operation.

- [ ] **Step 2: Define plans and progress**

```go
type Plan struct {
	Operations []Primitive
}

type Progress struct {
	Completed   []Result
	Failed      *Failure
	Unattempted []Primitive
}
```

Each primitive records its target, face, hand, expected item, confirmation policy, and optional caller-selected timing. Plans are immutable and inspectable before execution.

- [ ] **Step 3: Implement matrices and coordinate transforms**

Represent a placement matrix as relative cells with expected block/item data. Resolve origin, orientation, mirroring, and order into absolute primitives. Reject duplicate targets, overflow, unloaded positions, and plans beyond configured volume or operation limits.

- [ ] **Step 4: Execute with current-state revalidation**

The executor checks reach, loaded state, current held stack, effective capabilities, rule-enabled inventory topology, and snapshot revision before each primitive. Use bounded concurrency only when the behavior explicitly marks primitives independent. On capability loss, cancellation, rejection, correction, disconnect, or inventory exhaustion, adapt only when plan policy permits; otherwise return completed, failed, and unattempted work.

- [ ] **Step 5: Keep digging phases and timing explicit**

Expose start, cancel, and finish primitives. The vanilla resolver does not hide break duration. A custom tool behavior may supply a timing policy as an explicit part of its plan.

- [ ] **Step 6: Verify**

Run `devbox run -- task test:race -- ./operation ./digging ./building`.

### Task 10: Supply default and specialized container/tool behavior

**Files:**
- Create: `container/vanilla/player.go`
- Create: `container/vanilla/chest.go`
- Create: `container/vanilla/crafting.go`
- Create: `container/vanilla/furnace.go`
- Create: `container/vanilla/special.go`
- Create: `container/vanilla/drivers_test.go`
- Create: `digging/vanilla.go`
- Create: `building/vanilla.go`
- Create: `digging/vanilla_test.go`
- Create: `building/vanilla_test.go`

**Produces:** vanilla defaults without closing extension points for modded servers.

- [ ] **Step 1: Add vanilla container drivers**

Cover player inventory, generic chest rows, crafting, furnace family, brewing, enchanting, anvil, smithing, merchant, beacon, and horse inventory. Test every semantic-to-protocol slot mapping against protocol data fixtures.

- [ ] **Step 2: Add dedicated helper views**

Provide typed wrappers such as chest and furnace views over the generic container. They retain no separate state and fail with a typed layout error if used with an incompatible actual menu.

- [ ] **Step 3: Add vanilla tool resolvers**

Resolve normal digging and placement from generated block, item, material, and attribute data. Default to a single target. Keep area, vein, and matrix behavior opt-in through injected resolvers or caller-created plans.

- [ ] **Step 4: Verify**

Run `devbox run -- task test:race -- ./container/... ./digging ./building`.

### Task 11: Prove custom composition end to end

**Files:**
- Create: `internal/fixture/modded_server.go`
- Create: `internal/fixture/modded_server_test.go`
- Create: `testdata/conformance/manifest.json`
- Create: `internal/conformance/anticheat_test.go`
- Create: `examples/observe/main.go`
- Create: `examples/custom-version/main.go`
- Create: `examples/custom-components/main.go`
- Create: `examples/custom-menu/main.go`
- Create: `examples/tool-matrix/main.go`
- Modify: `README.md`
- Modify: `Taskfile.yml`

**Produces:** fixture coverage and runnable examples for construction-time replacement, built-in vanilla conformance, and complete custom versions.

- [ ] **Step 1: Add a modded fixture scenario**

Construct a client with strict safeguards, scoped authorization, dynamic capabilities, a one-block body, potion-granted two-block jump, equipment-granted flight and inventory slots, diving, bunnyhop movement, a custom menu driver, custom recipe source, vein-dig behavior, and a placement matrix. Run capability gain and loss, custom payload, movement correction, unexpected menu open, inventory rejection and resynchronization, partial multi-block completion, breaker acknowledgement, cancellation, kick, and disconnect.

- [ ] **Step 2: Assert construction immutability**

Verify all factories run before authentication, missing dependencies fail before dialing, the graph cannot be replaced after construction, a movement strategy can switch safely, and container/tool registries are frozen. Run reusable profile-contract tests against Java 1.8, Java 26.1, a built-in profile with modded physics and inventory overrides, and a complete custom protocol profile.

- [ ] **Step 3: Add examples with explicit ownership**

Show default construction, copying and overriding a built-in version profile, supplying a complete custom profile, replacing individual factories, registering a modded menu driver, creating a matrix, inspecting the plan, executing it with limits, and handling partial progress. Every example owns its context and exits on cancellation.

- [ ] **Step 4: Document escape hatches and non-goals**

Document endpoint authorization, strict recovery, safety outcomes, raw packets, raw container slots, custom protocol adapters, custom component factories, custom tool resolvers, and direct operation plans. State that the library executes caller-selected work but does not choose goals, pathfind, guarantee permission, or guarantee that a third-party server will not punish automation. State the neutral identity rule and the absence of anti-cheat evasion features.

- [ ] **Step 5: Add owned-server conformance tests**

Pin an offline Paper test server and an open-source anti-cheat artifact by URL and SHA-256. Run ordinary movement, collision stops, corrections, inventory synchronization, digging, and placement. Capture server alerts as test failures. Use failures to correct protocol ordering, observed state, or mechanics. Do not read or tune against check thresholds, add timing jitter, or weaken the test client based on plugin detection.

- [ ] **Step 6: Run repository gates**

Run `devbox run -- task verify` and `devbox run -- task test:conformance` in `headless-minecraft`. Run `devbox run -- task verify` in `minecraft-protocol`. Run the lint, test, and build tasks in `server` and `proxy` to catch shared-module regressions.

Expected: every command passes.

- [ ] **Step 7: Inspect final scope**

Run `git status --short` in all four repositories. Confirm that changes match the approved design and contain no credentials, token stores, unbounded queues, hidden autonomous loops, anti-cheat detection, identity spoofing, or committed `docs/`. Do not commit.
