# Observed World State Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build immutable observed-world snapshots for the headless client — player, entities, chunks, environment, containers, registries, and chat — applied by reducers in wire order, one revision per batch, and publish the 57 non-session events M6.3's taxonomy declared.

**Architecture:** One reducer per domain, each owning a private mutable store and producing an immutable snapshot view. The client's read loop already delivers `version.Batch`; M7 adds a `world.World` that applies a batch under a single lock, bumps one revision counter, and returns the events the batch produced. Reducers never touch the network and never publish. They see each other only through a read-mostly `Context` carrying the batch's revision, its protocol state, and the local player's entity ID, which keeps each one testable from a packet script alone.

**Tech Stack:** Go 1.26.6 via `openserbia/go-flake`, Devbox, Task, `minecraft-protocol` as a released module.

## Design status

M7 has an approved design:
[Observed world state design](../specs/2026-08-16-observed-world-state-design.md),
2026-08-16. This plan was amended against it on the same day.

The design review kept three of the five decisions this plan originally made on
its own, replaced one, narrowed one, and added four the plan did not know it
needed. What changed here:

1. **Chunk sections are immutable, and a decoded section is a pure cache.**
   Replaces the original decision 3. Lazy decoding stays; the mutation does not.
   The original combination of a lazily-decoded section and a copy-on-read
   snapshot under an `RWMutex` was a data race: a snapshot reader triggering a
   decode mutated shared state under a read lock while `Apply` wrote a block
   change into the same chunk. Sections are now immutable once received, a
   decoded section is published through an `atomic.Pointer`, and a block write
   swaps in a replacement section. See Task 5.
2. **Events carry the revision, stamped once per batch after the bump.** M6.3's
   design promised this and its plan did not implement it. See Task 1.
3. **A reducer never returns an error for server data.** Narrowed from the
   original contract. Unrecognized, unmapped, and out-of-range are normal
   traffic; an error means this repository's invariant broke. See Task 1.
4. **Reducer order is fixed, and reducers share facts through a `Context`.**
   The entity reducer needs the local player's entity ID, which only the player
   reducer learns. See Task 1.
5. **Environment is its own reducer.** Seven of the twelve declared world event
   names had no owner in this plan. See Task 6.
6. **Two prerequisites land on M6.3**, not on this plan. See Dependencies.

Kept unchanged: one revision counter for the whole world, copy-on-read
snapshots with the chunk benchmark as the escape route, unknown values
preserved rather than defaulted, and chat as the cut line.

## Global Constraints

- Work in `/home/ocharnyshevich/pet.projects/go-theft-craft/headless-minecraft`.
- Run every command as `devbox run -- task <name>`. `task test` already runs with `-race`.
- The module depends on `minecraft-protocol` and nothing else. Do not add a dependency.
- **A reducer never publishes, never performs I/O, and never blocks.** It takes a batch and mutates its own store. Everything else is the client's job.
- **A reducer never returns an error for something the server sent.** Unknown entity IDs, unknown metadata indices, unmapped menu types, and out-of-range values are normal traffic. Preserve them or drop them with a counter. An error from `Reduce` means an invariant in this repository broke, and it ends the session.
- **A reducer never stamps a revision.** It appends unstamped events; the world stamps the whole batch after the bump.
- **A snapshot handed to a caller is immutable from the caller's perspective.** Never return a map, slice, or pointer the world keeps mutating.
- **A chunk section is immutable once stored.** A block write builds a replacement section and swaps it in. Nothing mutates a section in place, including a lazy decode.
- **One revision bump per batch.** A 775 bundle produces exactly one, which is the whole reason M6.3 built batching.
- Unknown values are preserved as raw, addressable data. Never substitute a vanilla default for something the server did not send.
- Every event name this plan emits must already exist in `event/taxonomy.go`. Adding a name is a taxonomy change: update the design's count and Task 2's test in `event` first.
- Never add the `Co-Authored-By` or `Claude-Session` trailer to a commit message.
- Run `devbox run -- task precommit` before every commit.

## Dependencies

M6.3, complete, **including two changes the M7 design puts back on it**. Neither
belongs in this plan, and Task 1 cannot start until both have landed:

1. **`Event` carries the revision.** M6.3's design says an event carries the
   snapshot revision that produced it; M6.3's plan implements `Name()` and
   `Domain()` and nothing else. Adding it later means editing all 16 session
   event structs, so it lands as an embedded value in M6.3.
2. **The client's loop owns the configuration phase.** M6.3's `Connect` runs
   `login.Negotiate` to its terminal state, which under 775 is play, and starts
   `runLoop` afterwards. Every configuration packet on the way in is therefore
   consumed inside the negotiator and never dispatched.
   `ConfigurationClientboundRegistryData` arrives exactly once, in
   configuration, and Task 8 exists to reduce it. This also fixes two of M6.3's
   own events: `ServerMetadataChanged` is fed by `FeatureFlags` and
   `ResourcePackOffered` by `AddResourcePack`, both configuration packets, so
   today their handlers only fire on a play-to-configuration return that most
   sessions never make. The fix is to negotiate with a terminal state of
   configuration and let the readiness rule observe the transition into play.

M7 consumes `version.Batch`, `event.Collector`, the taxonomy, and the client's
read loop, and it needs the protocol 47 and 775 adapters to exist so it can be
driven from real packet types.

M7 does **not** depend on M4 for its protocol 47 half. Build and test every
reducer against 47 first; 775 coverage follows each reducer in the same task.

## File Structure

**New files:**

| File | Responsibility |
| --- | --- |
| `world/world.go` | `World`, batch application, the revision counter |
| `world/snapshot.go` | `Snapshot` and its per-domain views |
| `world/reducer.go` | The `Reducer` contract |
| `world/player.go` | Local player state |
| `world/entities.go` | Tracked entities |
| `world/chunks.go` | Chunk store, immutable sections, the atomic decode cache |
| `world/environment.go` | Time, border, weather, difficulty, explosions, world events |
| `world/containers.go` | Open menus, slots, recipes |
| `world/registry.go` | Session registries, tags, commands, player list |
| `world/chat.go` | Chat and presentational UI |
| `world/raw.go` | Unknown metadata, namespaced values, custom payloads |
| `world/*_test.go` | One test file per reducer, driven by packet scripts |
| `examples/observe/main.go` | The example the end-to-end lane drives, in the `examples/` module |
| `event/player.go`, `world.go`, `entities.go`, `containers.go`, `registry.go`, `chat.go` | The 57 event structs |
| `internal/adapter/*/reduce.go` | Per-version packet-to-reducer wiring |

**Modified:**

| File | Change |
| --- | --- |
| `client/client.go`, `client/loop.go` | Own a `World`; apply each batch; expose `World()` |
| `README.md`, `CHANGELOG.md` | Documentation |
| `MASTER_PLAN.md` | Milestone records |

---

## Stage A — The spine

### Task 1: The world, the revision, and the reducer contract

Build the frame before any domain fills it. This task ships with one trivial
reducer so the contract is exercised end to end.

**Files:**
- Create: `world/world.go`, `world/reducer.go`, `world/snapshot.go`, `world/world_test.go`

**Interfaces:**
- Produces: `World`, `New() *World`, `(*World).Apply(version.Batch, *event.Collector) error`, `(*World).Snapshot() Snapshot`, `Snapshot{Revision uint64}`, `Reducer`, `Context`, `(*World).Register(Reducer) error`, `ErrWorldPoisoned`.

Four things the design fixed land here rather than being discovered later:

- `Reduce` takes a `*Context` carrying the revision this batch will produce, the
  protocol state it arrived in, and the local player's entity ID once known.
  That is the only channel through which one reducer sees another's facts.
- Registration order is application order and it is fixed: player, entities,
  chunks, environment, containers, registry, chat. The entity reducer needs the
  local entity ID and only the player reducer learns it, so the order is a
  contract rather than an accident.
- The world stamps the collector after the bump. Reducers append unstamped
  events and cannot get the number wrong.
- A reducer error poisons the world. `Apply` aborts without bumping, and every
  later `Apply` and `Snapshot` reports the same error rather than answering
  from half-applied state.

- [ ] **Step 1: Write the failing test**

```go
package world_test

import (
	"testing"

	protocol "github.com/go-theft-craft/minecraft-protocol"

	"github.com/go-theft-craft/headless-minecraft/event"
	"github.com/go-theft-craft/headless-minecraft/version"
	"github.com/go-theft-craft/headless-minecraft/world"
)

// countingReducer records how many batches and packets it saw.
type countingReducer struct {
	batches int
	packets int
}

func (r *countingReducer) Reduce(_ *world.Context, b version.Batch, _ *event.Collector) error {
	r.batches++
	r.packets += len(b.Packets)

	return nil
}

func batch(names ...string) version.Batch {
	packets := make([]protocol.Packet, 0, len(names))
	for _, name := range names {
		packets = append(packets, protocol.Packet{Name: name, State: "play"})
	}

	return version.Batch{Packets: packets, Bundled: len(names) > 1}
}

func TestRevisionStartsAtZeroAndBumpsOncePerBatch(t *testing.T) {
	w := world.New()

	if got := w.Snapshot().Revision; got != 0 {
		t.Fatalf("initial revision is %d, want 0", got)
	}

	var c event.Collector
	if err := w.Apply(batch("a"), &c); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got := w.Snapshot().Revision; got != 1 {
		t.Errorf("revision after one batch is %d, want 1", got)
	}

	// A bundle of three packets is still one batch, so still one bump.
	// This is the property M6.3 built batching for.
	if err := w.Apply(batch("a", "b", "c"), &c); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got := w.Snapshot().Revision; got != 2 {
		t.Errorf("revision after a three-packet bundle is %d, want 2", got)
	}
}

func TestEveryRegisteredReducerSeesEveryBatch(t *testing.T) {
	w := world.New()
	first, second := &countingReducer{}, &countingReducer{}
	if err := w.Register(first); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := w.Register(second); err != nil {
		t.Fatalf("Register: %v", err)
	}

	var c event.Collector
	_ = w.Apply(batch("a", "b"), &c)

	for name, r := range map[string]*countingReducer{"first": first, "second": second} {
		if r.batches != 1 || r.packets != 2 {
			t.Errorf("%s saw %d batches and %d packets, want 1 and 2", name, r.batches, r.packets)
		}
	}
}

func TestAnEmptyBatchStillBumpsTheRevision(t *testing.T) {
	// An empty bundle is legal and observable: the server said something
	// happened even if nothing in it was modelled.
	w := world.New()

	var c event.Collector
	_ = w.Apply(version.Batch{Bundled: true}, &c)

	if got := w.Snapshot().Revision; got != 1 {
		t.Errorf("revision after an empty bundle is %d, want 1", got)
	}
}

func TestAReducerErrorPoisonsTheWorld(t *testing.T) {
	// A reducer error means an invariant here broke, not that the server
	// sent something odd. The batch is already half applied and nothing can
	// undo it, so the world stops answering rather than answering wrongly.
	w := world.New()
	_ = w.Register(failingReducer{})

	var c event.Collector
	if err := w.Apply(batch("a"), &c); err == nil {
		t.Fatal("Apply hid a reducer error")
	}
	if got := w.Snapshot().Revision; got != 0 {
		t.Errorf("revision is %d after a failed batch, want 0", got)
	}

	if err := w.Apply(batch("b"), &c); !errors.Is(err, world.ErrWorldPoisoned) {
		t.Errorf("second Apply returned %v, want ErrWorldPoisoned", err)
	}
	if _, err := w.SnapshotErr(); !errors.Is(err, world.ErrWorldPoisoned) {
		t.Errorf("SnapshotErr returned %v, want ErrWorldPoisoned", err)
	}
}

func TestEveryReducerSeesTheRevisionTheBatchWillProduce(t *testing.T) {
	// Reducers append unstamped events and the world stamps them after the
	// bump, so an event never names a revision that does not yet exist.
	// Write this body in full: register a reducer that records ctx.Revision
	// and appends one event, apply two batches, and assert the recorded
	// revisions are 1 and 2 and that both published events carry the same.
}

func TestReducersRunInRegistrationOrder(t *testing.T) {
	// Register three recording reducers and assert Reduce ran in the order
	// they were registered. The entity reducer depends on this to read the
	// local entity ID the player reducer sets.
}

func TestSnapshotIsSafeWhileApplyRuns(t *testing.T) {
	// Run under -race: a reader must never observe a partially applied
	// batch, which is the guarantee the whole design rests on.
	w := world.New()
	_ = w.Register(&countingReducer{})

	done := make(chan struct{})
	go func() {
		defer close(done)
		var c event.Collector
		for i := 0; i < 1000; i++ {
			_ = w.Apply(batch("a"), &c)
		}
	}()

	for i := 0; i < 1000; i++ {
		_ = w.Snapshot()
	}
	<-done
}

func TestRegisterAfterFirstApplyIsAnError(t *testing.T) {
	w := world.New()
	var c event.Collector
	_ = w.Apply(batch("a"), &c)

	if err := w.Register(&countingReducer{}); err == nil {
		t.Fatal("Register accepted a reducer after the world started applying")
	}
}
```

Declare `failingReducer` returning a sentinel error, and add `"errors"` to the
imports. Write the two sketched bodies in full.

- [ ] **Step 2: Run and verify failure**

```bash
devbox run -- task test -- ./world
```

Expected: FAIL, package does not exist.

- [ ] **Step 3: Implement**

```go
// Package world holds what the connection has observed.
//
// It stores only what the server sent. Mechanics live outside it: a body
// model, a physics model, and a movement strategy are M8 and M9, and keeping
// them out is what lets a modded server's world be represented without
// mod-specific conditions in the reducers.
//
// The unit of application is a batch, not a packet. Protocol 775 bundles
// packets that must take effect together, so the revision counter bumps once
// per batch and a reader never observes half a bundle.
package world

import (
	"errors"
	"fmt"
	"sync"

	"github.com/go-theft-craft/headless-minecraft/event"
	"github.com/go-theft-craft/headless-minecraft/version"
)

// ErrWorldStarted reports registration attempted after the first batch.
var ErrWorldStarted = errors.New("world has already applied a batch")

// ErrWorldPoisoned reports a world that aborted a batch part-way and can no
// longer describe the server's state.
var ErrWorldPoisoned = errors.New("world is poisoned by a failed batch")

// Context is what a reducer knows beyond its own state and the batch. The
// world owns it, it is valid only for one Reduce call, and it is the only
// channel through which one reducer sees a fact another reducer learned.
type Context struct {
	// Revision is the revision this batch will produce, not the current one.
	Revision uint64
	// State is the protocol state the batch arrived in. Registry data
	// arrives in configuration, so "not sent yet" and "will not be sent"
	// are distinguishable.
	State string
	// Local is the local player's entity ID, set by the player reducer when
	// it reads the play Login packet and read by every reducer after it.
	Local LocalRef
}

// Reducer applies one batch to its own private state.
//
// A reducer never publishes, never performs I/O, never blocks, and never
// stamps a revision: it appends unstamped events to the collector and returns.
//
// It returns an error only when an invariant in this package broke. Data the
// server sent that this model does not recognize is never an error: unknown
// entity IDs, unknown metadata indices, unmapped menu types, and out-of-range
// values are all normal traffic from a modded server, and each is preserved or
// dropped with a counter.
type Reducer interface {
	Reduce(ctx *Context, batch version.Batch, collector *event.Collector) error
}

// World owns every reducer and the single revision counter.
//
// Reducers run in registration order, and that order is a contract: the player
// reducer learns the local entity ID and every reducer after it reads that ID
// from the Context.
type World struct {
	mu       sync.RWMutex
	reducers []Reducer
	revision uint64
	started  bool
	poisoned error
	// local carries the local player's entity ID across batches, since the
	// Login packet that supplies it arrives once.
	local LocalRef
}

// New returns an empty world with no reducers.
func New() *World { return &World{} }

// Register adds a reducer. It must be called before the first Apply, because
// a reducer that missed earlier batches holds state nobody can reconstruct.
func (w *World) Register(r Reducer) error {
	if r == nil {
		return errors.New("world: nil reducer")
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	if w.started {
		return ErrWorldStarted
	}
	w.reducers = append(w.reducers, r)

	return nil
}

// Apply runs every reducer over one batch in registration order under a single
// write lock, bumps the revision, and stamps the batch's events with it.
//
// A reducer error aborts the batch and leaves the revision alone. The batch is
// then partially applied and nothing can undo the reducers that already ran, so
// the world is poisoned: every later call reports the same error and the
// session ends. A world that silently diverged from the server is worse than a
// connection that stopped, and a poisoned world still answering queries is
// worse than both.
//
// Stamping happens here, after the bump, so a subscriber can always resolve an
// event's revision against a snapshot that exists.
func (w *World) Apply(batch version.Batch, collector *event.Collector) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.poisoned != nil {
		return w.poisoned
	}
	w.started = true

	ctx := &Context{Revision: w.revision + 1, State: batch.State, Local: w.local}

	for _, reducer := range w.reducers {
		if err := reducer.Reduce(ctx, batch, collector); err != nil {
			w.poisoned = fmt.Errorf("%w: %w", ErrWorldPoisoned, err)

			return w.poisoned
		}
	}
	w.revision++
	w.local = ctx.Local
	collector.Stamp(w.revision)

	return nil
}

// Snapshot returns an immutable view of every domain at one revision. A
// poisoned world returns the zero snapshot; callers that need to tell that
// apart from a fresh one use SnapshotErr.
func (w *World) Snapshot() Snapshot {
	snap, _ := w.SnapshotErr()

	return snap
}

// SnapshotErr returns the snapshot and any error that poisoned the world.
func (w *World) SnapshotErr() (Snapshot, error) {
	w.mu.RLock()
	defer w.mu.RUnlock()

	if w.poisoned != nil {
		return Snapshot{}, w.poisoned
	}

	return Snapshot{Revision: w.revision}, nil
}
```

`Context.State` needs `version.Batch` to carry the protocol state the batch
arrived in. M6.3's `Batch` has `Packets` and `Bundled` only, and every packet
already carries its own `State`, so either add the field in Task 1 or read it
from the first packet. Adding the field is clearer and an empty batch then still
reports a state.

`collector.Stamp(uint64)` is the `event` package change amendment 2 records.
It sets the revision on every event the collector holds, and it belongs beside
`Reset` and `Events`.

`snapshot.go` starts with only the revision. Each later task adds its domain's
view to `Snapshot` and to the `Snapshot()` method, under the same read lock.

- [ ] **Step 4: Run and verify it passes**

```bash
devbox run -- task test -- ./world
```

Expected: PASS, all eight tests, including the race test.

- [ ] **Step 5: Commit**

```bash
git add world/
git commit -m "feat(world): add the reducer spine and revision counter"
```

### Task 2: Wire the world into the client

Do this before any domain, so every later task is provable end to end rather
than only in a unit test.

**Files:**
- Modify: `client/client.go`, `client/loop.go`
- Modify: `client/loop_test.go`

**Interfaces:**
- Produces: `(*Client).World() world.Snapshot`, `WithWorld(*world.World) Option`.

- [ ] **Step 1: Write the failing test**

```go
func TestEachBatchAdvancesTheWorldRevisionOnce(t *testing.T) {
	h := newHarness(t, "bundle", 16, "bundle", "a", "b", "bundle", "c")
	w := world.New()
	h.client.world = w

	if err := h.run(context.Background(), &countingReadiness{}); err != nil {
		t.Fatalf("runLoop: %v", err)
	}

	// One bundle plus one loose packet is two batches, not three packets.
	if got := w.Snapshot().Revision; got != 2 {
		t.Errorf("world revision is %d, want 2", got)
	}
}

func TestEventsCarryTheRevisionThatProducedThem(t *testing.T) {
	// A subscriber must be able to correlate an event with a snapshot. An
	// event published before the bump would name a revision that does not
	// yet exist.
	...
}

func TestAWorldErrorStopsTheLoop(t *testing.T) { ... }
```

Write the two sketched bodies in full.

- [ ] **Step 2: Run and verify failure**

- [ ] **Step 3: Implement**

In `runLoop`, apply the batch after dispatch and before publishing:

```go
		if c.world != nil {
			if err := c.world.Apply(batch, collector); err != nil {
				return err
			}
		}

		c.events.publish(collector.Events())
```

Apply runs after the adapter's handlers so a session-domain event and a
state-domain event from the same batch publish together, and before the
publish so every event names a revision that already exists. `Apply` stamps
the collector, which covers the session events M6.3's handlers appended as
well as the ones the reducers added.

**Batches from the configuration state reach the world too.** Task 8 reduces
`ConfigurationClientboundRegistryData`, which arrives once and only in
configuration, so the loop must apply configuration batches rather than
starting at play. That depends on the M6.3 prerequisite in Dependencies; assert
it here with a test that drives a configuration batch through `runLoop` and
asserts the world saw it, so the wiring fails loudly in Task 2 rather than
silently in Task 8.

Add `World()` to `Client`, returning `c.world.Snapshot()`, and a zero snapshot
when no world is installed.

- [ ] **Step 4: Run and verify it passes**

- [ ] **Step 5: Commit**

```bash
git add client/ 
git commit -m "feat(client): apply each batch to observed world state"
```

---

## Stage B — The domains

Each task below has the same shape, so it is stated once here and not repeated:

1. Add the domain's event structs to `event/`, using names **already declared**
   in `event/taxonomy.go`. Adding a new name means changing the taxonomy first.
2. Write the reducer's test as a packet script: a `[]version.Batch` built from
   real generated packet values, applied in order, asserting on the snapshot and
   on the collected events after each batch.
3. Implement the reducer and add its view to `Snapshot`.
4. Wire it in `internal/adapter/v1_8/reduce.go` and `internal/adapter/v26_1/reduce.go`.
5. Run `devbox run -- task test`, then commit.

The tests that matter in every domain, and which each task must include by
name:

- **Ordering**: two packets in one batch apply in wire order, not map order.
- **Ownership**: the snapshot's collections do not alias the reducer's.
- **Removal**: whatever removes state actually releases it.
- **Unknown preservation**: a value the model does not know is kept, not dropped
  and not defaulted.
- **Both protocols**: the same script through 47 and 775 produces the same
  snapshot shape where the versions agree, and a documented difference where
  they do not.

### Task 3: The player reducer

**Files:** `world/player.go`, `world/player_test.go`, `event/player.go`

**Events:** `PlayerSpawned`, `PlayerMoved`, `PlayerHealthChanged`,
`PlayerExperienceChanged`, `PlayerAbilitiesChanged`, `PlayerGameModeChanged`,
`PlayerRespawned`, `PlayerHeldSlotChanged`, `PlayerEffectsChanged`,
`PlayerCooldownChanged` — 10 names, all already declared.

**Packets:** `Login`, `Position`, `PlayerRotation` (775 only), `UpdateHealth`,
`Experience`, `Abilities`, `GameStateChange`, `Respawn`, `HeldItemSlot`,
`SetCooldown`, `Camera`, `EntityEffect` and `RemoveEntityEffect` where the
entity is the local player.

Domain-specific tests beyond the standard five:

- **Relative position flags are applied against the previous position.** M6.3's
  readiness rule errors on a relative spawn because it has no prior position;
  the player reducer has one, and this is where the arithmetic belongs. Test
  each flag bit independently.
- **`GameStateChange` carries several unrelated meanings** — game mode, rain
  toggle, and more, discriminated by a reason byte. Assert that a game-mode
  change produces `PlayerGameModeChanged` and a weather change produces the
  world domain's `WorldWeatherChanged`, from the same packet type.
- **The local player is identified by the entity ID from `Login`.** An entity
  packet naming that ID updates the player, not the entity store. Test the
  boundary explicitly, in both directions.

### Task 4: The entity reducer

**Files:** `world/entities.go`, `world/entities_test.go`, `event/entities.go`

**Events:** the 12 declared entity names.

**Packets:** `SpawnEntity`, `NamedEntitySpawn` (47), `EntityDestroy`,
`RelEntityMove`, `EntityMoveLook`, `EntityLook`, `EntityTeleport`,
`SyncEntityPosition` (775), `EntityHeadRotation`, `EntityMetadata`,
`EntityEquipment`, `EntityUpdateAttributes`, `EntityEffect`,
`RemoveEntityEffect`, `EntityVelocity`, `AttachEntity`, `SetPassengers`,
`DamageEvent`, `HurtAnimation`, `EntityStatus`, `Animation`, `Collect`,
`MoveMinecart`, `VehicleMove`.

Domain-specific tests:

- **Four movement packets, one `EntityMoved`.** This is the taxonomy's
  motivating example. Assert that all four produce the same event shape and
  that relative moves accumulate against the stored position.
- **Metadata is merged by index, not replaced.** A metadata packet carrying one
  index must not clear the others.
- **Unknown metadata indices are preserved** and readable from the snapshot as
  raw values. Protocol 47 terminates metadata at `0x7F` and 775 at `0xFF`, and
  the index space differs; a modded server sends indices neither version's
  model names.
- **`EntityDestroy` releases the entity's state**, including its metadata and
  equipment, so a long session does not grow without bound.
- **An entity packet for an unknown entity ID is not an error.** Packets can
  arrive for an entity the client never saw spawn — after a chunk unload, for
  instance. Record it or drop it, but do not fail the session.

### Task 5: The chunk reducer

The largest and the one with the most room to be slow.

**Files:** `world/chunks.go`, `world/chunks_test.go`, `event/world.go`

**Events:** `WorldChunkLoaded`, `WorldChunkUnloaded`, `WorldBlocksChanged`,
`WorldBlockEntityChanged`, `WorldLightChanged`.

**Packets:** `MapChunk`, `UnloadChunk`, `ChunkBatchStart`, `ChunkBatchFinished`
(775), `ChunkBiomes`, `UpdateLight`, `BlockChange`, `MultiBlockChange`,
`BlockAction`, `TileEntityData`.

Domain-specific tests:

- **A section is decoded on first block read, not on receipt.** Assert that
  loading a chunk does not decode it, and that reading one block does.
- **A decode never mutates a section.** The decoded form is published through
  an `atomic.Pointer` and the received bytes are immutable, so two readers
  racing to decode the same section both compute the same result and one wins
  the store. Assert with many concurrent readers on one undecoded section while
  a block change replaces it, under `-race`. This is the test that catches the
  design this plan originally carried, where a lazy decode under a read lock
  mutated shared state while `Apply` wrote to the same chunk.
- **A block write swaps a section rather than editing one.** Hold a snapshot,
  apply a block change, and assert the held snapshot still reports the old
  block. Copy-on-write at section granularity is what makes `Snapshot` a
  pointer copy.
- **`BlockChange` and `MultiBlockChange` produce the same event.**
  `WorldBlocksChanged` carries a position set, so a single change is a set of
  one.
- **A block change inside an undecoded section decodes only that section.**
- **`UnloadChunk` releases the chunk's storage.** Load a thousand chunks, unload
  them, and assert the store is empty — a chunk store that leaks is the one
  memory bug a long-running bot will certainly hit.
- **Protocol 47 and 775 chunk formats differ substantially.** 47 sends a bitmask
  and a packed blob; 775 sends per-section palettes and separate light data.
  Both must produce the same block lookups. Use a fixture chunk from each
  version and assert the same block appears at the same coordinate.

Add a benchmark for the block lookup path and record its result in the commit
message. Design decision 2's copy-on-read snapshot is defensible only while
this stays cheap.

### Task 6: The environment reducer

The seven world event names no task in this plan owned. Task 10's completeness
test fails on every one of them until this exists.

**Files:** `world/environment.go`, `world/environment_test.go`, `event/world.go`

**Events:** `WorldTimeChanged`, `WorldBorderChanged`, `WorldWeatherChanged`,
`WorldDifficultyChanged`, `WorldExplosionOccurred`, `WorldEventOccurred`,
`WorldSimulationSettingsChanged`.

**Packets:** `UpdateTime`, the six `WorldBorder*` packets, `Difficulty`,
`GameRuleValues`, `Explosion`, `WorldEvent`, `WorldParticles`,
`SimulationDistance`, `UpdateViewDistance`, `UpdateViewPosition`,
`SetTickingState`, `StepTick`.

These are environment scalars, not chunk data, and they come from an entirely
different packet set, which is why they are not folded into Task 5. Task 5 is
the largest and most performance-sensitive reducer in the milestone and does not
also need a pile of unrelated scalars in the same file.

Domain-specific tests:

- **`GameStateChange` reaches two reducers.** One packet type carries a
  game-mode change and a rain toggle, discriminated by a reason byte, so the
  player reducer and this one both handle it and each ignores the reasons that
  are not its own. Assert a game-mode change produces `PlayerGameModeChanged`
  and nothing here, and a weather change produces `WorldWeatherChanged` and
  nothing there. Task 3 asserts the same boundary from the other side.
- **The six `WorldBorder*` packets produce one `WorldBorderChanged`.** Protocol
  47 sends one packet with an action discriminator and 775 sends six distinct
  packets; both produce the same event shape and the same snapshot.
- **`WorldEventOccurred` and `WorldParticles` are not the same event.** A
  world event is a discrete effect with an ID and a position; particles are
  presentational. Only the first carries state.

### Task 7: The container reducer

**Files:** `world/containers.go`, `world/containers_test.go`, `event/containers.go`

**Events:** the 7 declared container names.

**Packets:** `OpenWindow`, `CloseWindow`, `WindowItems`, `SetSlot`,
`SetCursorItem` (775), `SetPlayerInventory` (775), `CraftProgressBar`,
`CraftRecipeResponse`, `TradeList`, `OpenHorseWindow`, `OpenBook`,
`OpenSignEntity`, `DeclareRecipes`, `RecipeBookAdd`, `RecipeBookRemove`,
`RecipeBookSettings`.

Domain-specific tests:

- **The container records what the server actually opened** — container ID,
  namespaced menu type, title, state ID, raw slots — and never predicts a menu
  from the block that was clicked.
- **An unknown menu type is still a usable container.** Raw slots are readable;
  no semantic layout is invented. Container drivers and semantic layouts are M9.
- **Protocol 47 has no state ID and 775 does.** The snapshot exposes it as
  optional rather than defaulting to zero, because zero is a valid state ID.
- **`SetSlot` with container ID -1 targets the cursor**, not a slot, in both
  protocols. This is the kind of special case that silently corrupts an
  inventory model.

### Task 8: The registry reducer

**Files:** `world/registry.go`, `world/registry_test.go`, `event/registry.go`

**Events:** `RegistryDataReceived`, `TagsReceived`, `RegistryCommandsReceived`,
`RegistryPlayerListChanged`.

**Packets:** `ConfigurationClientboundRegistryData` (775), `Tags`,
`DeclareCommands`, `PlayerInfo`, `PlayerRemove` (775).

Domain-specific tests:

- **Session registry data overrides the generated registry for that
  connection**, and the generated data stays reachable for lookups that do not
  depend on server configuration. This is the whole point of the domain.
- **An unknown namespaced registry key is preserved.** A modded server sends
  registries the generated data has never heard of.
- **Registry data arrives in the configuration state, before play.** The reducer
  must accept batches from configuration, which means the client applies batches
  in configuration too — check that Task 2's wiring does, and fix it here if not.
- **Protocol 47 has no registry data packet at all.** Its registries are
  entirely static. Assert the snapshot reports that honestly rather than
  presenting an empty session registry as if the server sent one.

### Task 9: Raw and unknown value preservation

This is a `MASTER_PLAN` requirement in its own right — "preserve unknown
metadata, namespaced values, and custom payloads" — and it cuts across every
domain, so it gets a task rather than being assumed.

**Files:** `world/raw.go`, `world/raw_test.go`

- [ ] **Step 1: Write the failing test**

Drive a packet script containing, in one session: an entity metadata index no
version models, a registry key with an unknown namespace, a menu type that is
not in `windows`, a custom payload on an unregistered channel, and an entity
attribute with an unknown key. Assert that every one is readable from the
snapshot, addressable by its key, and byte-identical to what arrived.

Then assert the negative: that none of them produced a defaulted value, and
that no event was published for a name the taxonomy does not declare.

- [ ] **Step 2: Run and verify failure**

- [ ] **Step 3: Implement**

A shared `Raw` type holding key-addressable owned bytes, embedded by the
domains that need it rather than a separate global store — an unknown metadata
index belongs to its entity, not to a world-wide bag.

- [ ] **Step 4: Run, benchmark the memory cost, and commit**

An unbounded raw store is a memory leak with a modded server. Bound it per
owner and record what the bound is.

### Task 10: Chat and UI

**Files:** `world/chat.go`, `world/chat_test.go`, `event/chat.go`

**Events:** the 12 declared chat names.

**This is the cut line.** These 12 events carry no state any later milestone
consumes, and they are the largest domain by packet count. If M7 runs long,
stop after Task 9, mark this task deferred in `MASTER_PLAN.md`, and ship the
rest — the chat packets stay reachable as raw packets, which is where they were
before M7 started.

Domain-specific tests:

- **`ChatReceived` carries a kind** discriminating player, system, and
  profileless chat, rather than three events.
- **Protocol 775 chat is signed and 47 is not.** The snapshot exposes signature
  presence without validating it; validating chat signatures is not something
  this milestone claims to do, and claiming it falsely is worse than not doing
  it.
- **A removed message is removed from the log**, not marked.

---

## Stage C — Gate

### Task 11: End-to-end, documentation, and the release gate

**Files:** `examples/observe/main.go`, `client/world_e2e_test.go`, `README.md`,
`CHANGELOG.md`, `MASTER_PLAN.md`

- [ ] **Step 1: Write `examples/observe`**

A program that connects, subscribes to every state domain, and prints each
event with its revision. It lives in the `examples/` module, which
`MASTER_PLAN.md`'s repository conventions establish: `examples/` has its own
`go.mod` so the library keeps its single dependency, and it needs a second CI
step because `go test ./...` from the root does not descend into it. M6.3
creates the module with `examples/connect`; this task adds the second program.

The `replace` directive pointing `examples/` at its parent is the one place a
`replace` is legitimate in this repository, and it is deliberate rather than an
oversight of M6.3's rule that the module graph carry no `replace`.

- [ ] **Step 2: Add the end-to-end lane**

Drive `examples/observe` against M6.3's fixture server rather than a harness
that exists only inside a test file. An example CI runs cannot rot. Script the
fixture so that, after readiness, it sends a chunk, spawns two entities, moves
one, opens a container, changes the weather, and sends a registry update in
configuration — on both protocols. Assert:

- the revision advanced exactly once per batch;
- a bundle's events published together, with no other batch's events between;
- every event's revision names a snapshot that exists;
- the snapshot's entity, chunk, and container views hold what the script sent;
- unloading the chunk and destroying the entities empties the stores.

- [ ] **Step 3: Confirm the taxonomy is fully implemented**

```bash
# Every declared name must have a struct that reports it.
devbox run -- go test ./event -run TestEveryDeclaredNameHasAnImplementation -v
```

Write that test in this task: reflect over the event package's types, collect
the names they report, and assert the set equals `AllNames()` minus anything
Task 10 deferred. This is exit criterion 5 from M6.3's design, finally
checkable, and it is the test that would have caught the seven world event
names this plan originally left unowned.

- [ ] **Step 4: Document**

README gains a world-state section: how to snapshot, what a revision means,
that a bundle is one revision, that unknown values are preserved rather than
defaulted, and that mechanics are not here.

- [ ] **Step 5: Run the gate**

```bash
devbox run -- task verify
devbox run -- task test:e2e
```

- [ ] **Step 6: Record the milestone and what was found**

Mark M7 complete in `MASTER_PLAN.md`. Record, specifically:

- whether the copy-on-read snapshot held up under the chunk benchmark, or
  whether a persistent map is now needed;
- whether Task 10 shipped or was deferred;
- any generated codec that rejected a real server's packet, because each is a
  bug in `minecraft-protocol`, not here;
- the raw-store bound Task 9 chose;
- whether any reducer wanted to return an error for server data, because each
  such case is either a preservation gap or an argument against the narrowed
  contract in Task 1.

- [ ] **Step 7: Commit**

```bash
git add -A
git commit -m "docs: record observed world state"
```

---

## Self-review notes

- **This plan is less finished than M6.3's, on purpose and not entirely.**
  Tasks 3 through 10 give each domain its events, its packets, and the tests
  that must exist by name, but not the full test code. Writing 57 event structs and
  their reducer tests as literal code before any of them has met a real packet
  would be inventing detail the first real chunk fixture will overturn. Tasks 1
  and 2 — the spine, where the concurrency and revision guarantees live — are
  written out in full, because those are the parts that are expensive to get
  wrong and cheap to specify.
- **The design review changed the two most expensive tasks.** Task 1 gained the
  `Context`, the fixed reducer order, the stamping rule, and the narrowed error
  contract; Task 5 lost the mutable lazy decode that was a data race under
  `-race`. Both were cheap to fix on paper and would have been expensive to find
  in code, which is the argument for the review having happened at all.
- **Two prerequisites now sit on M6.3 rather than here.** Task 1 cannot start
  until `Event` carries a revision and the client's loop owns the configuration
  phase. The second was originally a self-review note on the registry task,
  discovered too late in the plan to be actionable; it is a dependency now.
- **Seven world event names had no owner and Task 6 is why.** They were found by
  reading the taxonomy against the task list rather than by any test, which is
  what Task 11's completeness check exists to make impossible next time.
