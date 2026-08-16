# Headless Connection Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the `headless-minecraft` client that connects to a Java Edition server, completes an offline login, reaches a defined ready state, and publishes normalized events from a packet loop that respects protocol 775's bundle delimiters.

**Architecture:** Four leaf packages with one direction of dependency. `event` declares the taxonomy and carries no protocol knowledge. `version` binds a generated protocol to an adapter and a readiness rule, and owns the `Batch` the read loop delivers. `client` owns the lifecycle, the loop, and subscriptions. `internal/adapter/v1_8` and `internal/adapter/v26_1` hold the per-version translation tables, because 47 has 74 clientbound play packets and 775 has 141, sharing few structures.

**Tech Stack:** Go 1.26.6 via `openserbia/go-flake`, Devbox, Task, `github.com/go-theft-craft/minecraft-protocol` as a released module. No other dependencies.

## Global Constraints

- Work in `/home/ocharnyshevich/pet.projects/go-theft-craft/headless-minecraft`.
- Run every command as `devbox run -- task <name>`. Never call `go` directly.
- `task test` already runs with `-race`. There is no separate `test:race`.
- The module depends on `minecraft-protocol` and nothing else. Do not add a dependency.
- M6.3 changes no code in `minecraft-protocol`. Where the design assumed a change there — `router.Dispatch` — this plan works without it. See the dependency note below.
- Handlers run on the read goroutine. A handler must not block, must not perform I/O, and must not wait on a subscriber.
- The library never reconnects. `Close` is idempotent.
- Secrets and access tokens never appear in errors, logs, or events.
- Pass `context.Context` as the first argument to every blocking public operation.
- Event payloads are immutable from the caller's perspective. Never hand out a slice or map the client retains.
- Each task ends with a commit. Run `devbox run -- task precommit` before every commit.
- Never add the `Co-Authored-By` or `Claude-Session` trailer to a commit message.

## Dependencies

| Needs | For | Status |
| --- | --- | --- |
| M4 Task 9 (modern login sequence) | 775 login through configuration | Not started |
| M4.4 (`v26_1.Protocol()` verified) | The 775 profile | Not started |

The 775 halves of Tasks 5, 10, 13, and 14 need M4. Their protocol 47 halves do
not, and protocol 47 needs no configuration state at all. If M4 slips, build and
verify the whole plan on 47 and leave the 775 lanes failing until it lands — do
not stub them green.

### The router question, and why this plan does not depend on it

The design says the read loop dispatches through M5's router. Task 11's loop
cannot call `router.Run`, because `Run` owns its own loop over a `Receiver` one
packet at a time and the loop has to group packets into batches before anything
is applied. It would need `Router.Dispatch(ctx, Packet) error`, the
single-packet form `Run` already uses internally.

**M5's approved plan does not produce `Dispatch`.** It produces `Handle`,
`HandleID`, `Fallback`, and `Run`. Adding it is one exported method over
existing behavior, but it is a change to another repository's approved scope,
and this plan does not assume it.

Task 11's `runLoop` therefore takes a `dispatcher` parameter typed as an
interface this package declares:

```go
// dispatcher runs the handlers registered for one packet.
type dispatcher interface {
	Dispatch(ctx context.Context, p protocol.Packet) error
}
```

Two implementations satisfy it, and the choice is deferred to whoever executes
Task 11:

- **`tableDispatcher`**, in this repository, built from the adapter's
  `Handlers()` map. Fifteen lines, no dependency on M5, and it is what the tests
  in this plan use. Registration is by packet name, which is what the adapter
  already keys on, so name-to-ID resolution is not needed.
- **M5's router**, if and when it exports `Dispatch`. Swapping to it is a
  one-line change at the construction site in Task 12's `Connect`.

Start with `tableDispatcher`. It removes the cross-repository dependency
entirely and keeps M6.3 buildable the moment Task 1 lands. If M5 later exports
`Dispatch` and the router's ordered-handler and fallback behavior turns out to
be worth having, switch then — the interface is the seam that makes it cheap.
Record the decision in `MASTER_PLAN.md` at Task 14 either way.

## File Structure

**New files:**

| File | Responsibility |
| --- | --- |
| `event/domain.go` | `Domain` bitmask, `Event` interface, `EventName` |
| `event/taxonomy.go` | All 73 canonical event names as constants, grouped by domain |
| `event/session.go` | The 16 session-domain event structs |
| `event/collector.go` | Batch-scoped event collector handed to handlers |
| `event/*_test.go` | Name uniqueness, domain coverage, collector ownership |
| `version/batch.go` | `Batch` and the bundle batcher |
| `version/readiness.go` | `ReadinessRule` |
| `version/batch_test.go` | Delimiter toggling, limits, unterminated bundles |
| `client/client.go` | `Client`, `New`, options, validation |
| `client/subscription.go` | Bounded subscriptions and fan-out |
| `client/connect.go` | Dial, handshake, login, readiness handshake |
| `client/loop.go` | Read loop: batch, dispatch, publish |
| `client/close.go` | Shutdown, disconnect classification |
| `client/*_test.go` | Unit and end-to-end coverage |
| `client/internal/fixture/server.go` | Scripted in-process server for tests |
| `auth/auth.go` | `Provider`, `Offline` |
| `auth/auth_test.go` | Offline identity, nil provider rejection |
| `internal/adapter/v1_8/adapter.go` | Protocol 47 handlers and readiness rule |
| `internal/adapter/v26_1/adapter.go` | Protocol 775 handlers and readiness rule |
| `internal/adapter/*/adapter_test.go` | Per-version translation and readiness |
| `version/java/java.go` | `Java1_8()` and `JavaCurrent()` complete profiles |

**Modified files:**

| File | Change |
| --- | --- |
| `go.mod` | Go 1.26.6; released `minecraft-protocol`; no `replace` |
| `devbox.json` | `go_1_26_6` |
| `version/profile.go` | `Adapter` grows `Handlers`; `WireProfile` grows `Readiness` |
| `Taskfile.yml` | `test:e2e` |
| `README.md`, `CHANGELOG.md`, `ROADMAP.md` | Documentation |
| `MASTER_PLAN.md` | Milestone records |

---

## Stage A — Foundation

### Task 1: Toolchain and released dependency

`go.mod` pins Go 1.26.5 and a pseudo-version of `minecraft-protocol` from
2026-08-13, predating the `v0.1.0` release M3 consumed. M3 recorded that a shell
entered from a sibling repository leaks its `GOROOT` and every build fails on a
toolchain mismatch; `devbox.json` here already sets `GOROOT` explicitly, so only
the version pins move.

**Files:**
- Modify: `go.mod`, `devbox.json`

**Interfaces:**
- Produces: a module on Go 1.26.6 depending on `minecraft-protocol v0.1.0` or later, with no `replace` directive.

- [x] **Step 1: Check what the protocol module actually published**

```bash
cd ../minecraft-protocol && git tag --list 'v*' | sort -V | tail -3
```

Use the newest released tag. `v0.1.0` is enough for this whole plan: nothing
here imports the router, and the packages it does import — the root protocol
package, `login`, and `generated/java/*` — all shipped in it.

- [x] **Step 2: Move the toolchain pin**

In `devbox.json`, change `github:openserbia/go-flake#go_1_26_5` to
`github:openserbia/go-flake#go_1_26_6`. Leave the `GOROOT` env entry and the
`init_hook` exactly as they are.

In `go.mod`, change `go 1.26.5` to `go 1.26.6`.

- [x] **Step 3: Move the dependency to the released module**

```bash
devbox run -- go get github.com/go-theft-craft/minecraft-protocol@v0.1.0
devbox run -- task deps
```

- [x] **Step 4: Verify**

```bash
devbox run -- task verify
grep -c replace go.mod   # expect 0
```

Expected: `verify` passes and `go.mod` contains no `replace` directive.

- [x] **Step 5: Commit**

```bash
git add go.mod go.sum devbox.json devbox.lock
git commit -m "build: move to Go 1.26.6 and the released protocol module"
```

---

## Stage B — The taxonomy

### Task 2: Domains, names, and the Event contract

The taxonomy fixes 73 names now so M7 extends the surface rather than reshaping
it. Only the 16 session events get structs in this milestone. The other 57 are
declared as name constants, which gives M7 the contract without leaving 57
unused structs for the linter to reject.

**Files:**
- Create: `event/domain.go`, `event/taxonomy.go`, `event/taxonomy_test.go`

**Interfaces:**
- Produces: `Domain`, `Event`, `EventName`, `AllNames() []EventName`, `(EventName).Domain() Domain`, and the 73 name constants.

- [x] **Step 1: Write the failing test**

```go
package event_test

import (
	"strings"
	"testing"

	"github.com/go-theft-craft/headless-minecraft/event"
)

func TestEveryNameIsUniqueAndDomainPrefixed(t *testing.T) {
	prefixes := map[event.Domain]string{
		event.DomainSession:    "session.",
		event.DomainPlayer:     "player.",
		event.DomainWorld:      "world.",
		event.DomainEntities:   "entity.",
		event.DomainContainers: "container.",
		event.DomainRegistry:   "registry.",
		event.DomainChat:       "chat.",
	}

	seen := make(map[event.EventName]bool)
	for _, name := range event.AllNames() {
		if seen[name] {
			t.Fatalf("duplicate event name %q", name)
		}
		seen[name] = true

		domain := name.Domain()
		prefix, ok := prefixes[domain]
		if !ok {
			t.Fatalf("event %q reports unknown domain %d", name, domain)
		}
		if !strings.HasPrefix(string(name), prefix) {
			t.Fatalf("event %q is in domain %q but lacks prefix %q", name, prefix, prefix)
		}
	}
}

func TestTaxonomyCoversEveryDeclaredDomain(t *testing.T) {
	counts := make(map[event.Domain]int)
	for _, name := range event.AllNames() {
		counts[name.Domain()]++
	}

	// The design fixes these counts. A change here is a taxonomy change and
	// must be a deliberate edit to the design, not a drive-by addition.
	want := map[event.Domain]int{
		event.DomainSession:    14,
		event.DomainPlayer:     10,
		event.DomainWorld:      12,
		event.DomainEntities:   12,
		event.DomainContainers: 7,
		event.DomainRegistry:   4,
		event.DomainChat:       12,
	}
	for domain, expected := range want {
		if counts[domain] != expected {
			t.Errorf("domain %d has %d events, want %d", domain, counts[domain], expected)
		}
	}
}

func TestRawIsNotPartOfTheNamedTaxonomy(t *testing.T) {
	for _, name := range event.AllNames() {
		if name.Domain() == event.DomainRaw {
			t.Fatalf("raw packets are a selector, not a named event: %q", name)
		}
	}
}
```

- [x] **Step 2: Run and verify failure**

```bash
devbox run -- task test -- ./event
```

Expected: FAIL, `event` package does not exist.

- [x] **Step 3: Implement the domain and event contract**

`event/domain.go`:

```go
// Package event declares the headless client's event taxonomy.
//
// An event announces what changed in observed state, not which packet
// arrived. Four packets move an entity in protocol 775 and one event reports
// it, so a subscriber written against this taxonomy keeps working when a
// version changes which packet carries a fact.
//
// This package knows nothing about the protocol. It imports no generated
// code and no wire types, which is what lets both the client and the
// per-version adapters depend on it without a cycle.
package event

// Domain groups events by the part of observed state they describe. It is a
// bitmask so a subscriber selects several domains in one value.
type Domain uint

const (
	// DomainSession covers connection lifecycle. M6.3 implements it.
	DomainSession Domain = 1 << iota
	// DomainPlayer covers the local player's own state.
	DomainPlayer
	// DomainWorld covers chunks, blocks, and environment.
	DomainWorld
	// DomainEntities covers tracked entities other than the local player.
	DomainEntities
	// DomainContainers covers open menus, slots, and recipes.
	DomainContainers
	// DomainRegistry covers server-supplied registries, tags, and commands.
	DomainRegistry
	// DomainChat covers chat and presentational UI.
	DomainChat
	// DomainRaw selects undecoded protocol packets. It names no events of
	// its own: a raw delivery carries the packet, not a taxonomy entry.
	DomainRaw
)

// EventName is an event's stable identifier. Names are prefixed by domain so
// a log line or a filter expression is readable without a lookup table.
type EventName string

// Event is one immutable observation. Implementations are values, not
// pointers, so a subscriber cannot mutate what another subscriber sees.
type Event interface {
	Name() EventName
	Domain() Domain
}
```

- [x] **Step 4: Implement the taxonomy**

`event/taxonomy.go`. Every name below is derived from a packet that exists in
the pinned 775 or 47 schema.

```go
package event

// Session events. M6.3 implements all sixteen.
const (
	SessionConnecting             EventName = "session.connecting"
	SessionAuthenticated          EventName = "session.authenticated"
	SessionStateChanged           EventName = "session.state_changed"
	SessionReady                  EventName = "session.ready"
	SessionDisconnected           EventName = "session.disconnected"
	SessionClosed                 EventName = "session.closed"
	SessionKeepAlivePonged        EventName = "session.keepalive_ponged"
	SessionTransferRequested      EventName = "session.transfer_requested"
	SessionResourcePackOffered    EventName = "session.resource_pack_offered"
	SessionResourcePackRevoked    EventName = "session.resource_pack_revoked"
	SessionServerMetadataChanged  EventName = "session.server_metadata_changed"
	SessionCookieRequested        EventName = "session.cookie_requested"
	SessionCookieStored           EventName = "session.cookie_stored"
	SessionCustomPayloadReceived  EventName = "session.custom_payload_received"
	SessionPacketReceived         EventName = "session.packet_received"
	SessionPacketSent             EventName = "session.packet_sent"
)

// Player events. M7 implements these.
const (
	PlayerSpawned          EventName = "player.spawned"
	PlayerMoved            EventName = "player.moved"
	PlayerHealthChanged    EventName = "player.health_changed"
	PlayerExperienceChanged EventName = "player.experience_changed"
	PlayerAbilitiesChanged EventName = "player.abilities_changed"
	PlayerGameModeChanged  EventName = "player.game_mode_changed"
	PlayerRespawned        EventName = "player.respawned"
	PlayerHeldSlotChanged  EventName = "player.held_slot_changed"
	PlayerEffectsChanged   EventName = "player.effects_changed"
	PlayerCooldownChanged  EventName = "player.cooldown_changed"
)

// World events. M7 implements these.
const (
	WorldChunkLoaded              EventName = "world.chunk_loaded"
	WorldChunkUnloaded            EventName = "world.chunk_unloaded"
	WorldBlocksChanged            EventName = "world.blocks_changed"
	WorldBlockEntityChanged       EventName = "world.block_entity_changed"
	WorldLightChanged             EventName = "world.light_changed"
	WorldTimeChanged              EventName = "world.time_changed"
	WorldBorderChanged            EventName = "world.border_changed"
	WorldWeatherChanged           EventName = "world.weather_changed"
	WorldDifficultyChanged        EventName = "world.difficulty_changed"
	WorldExplosionOccurred        EventName = "world.explosion_occurred"
	WorldEventOccurred            EventName = "world.event_occurred"
	WorldSimulationSettingsChanged EventName = "world.simulation_settings_changed"
)

// Entity events. M7 implements these.
const (
	EntitySpawned            EventName = "entity.spawned"
	EntityRemoved            EventName = "entity.removed"
	EntityMoved              EventName = "entity.moved"
	EntityMetadataChanged    EventName = "entity.metadata_changed"
	EntityEquipmentChanged   EventName = "entity.equipment_changed"
	EntityAttributesChanged  EventName = "entity.attributes_changed"
	EntityEffectsChanged     EventName = "entity.effects_changed"
	EntityVelocityChanged    EventName = "entity.velocity_changed"
	EntityPassengersChanged  EventName = "entity.passengers_changed"
	EntityDamaged            EventName = "entity.damaged"
	EntityAnimated           EventName = "entity.animated"
	EntityItemCollected      EventName = "entity.item_collected"
)

// Container events. M7 implements these.
const (
	ContainerOpened          EventName = "container.opened"
	ContainerClosed          EventName = "container.closed"
	ContainerSlotsChanged    EventName = "container.slots_changed"
	ContainerCursorChanged   EventName = "container.cursor_changed"
	ContainerRecipesChanged  EventName = "container.recipes_changed"
	ContainerTradesChanged   EventName = "container.trades_changed"
	ContainerCraftResponse   EventName = "container.craft_response"
)

// Registry events. M7 implements these.
const (
	RegistryDataReceived     EventName = "registry.data_received"
	RegistryTagsReceived     EventName = "registry.tags_received"
	RegistryCommandsReceived EventName = "registry.commands_received"
	RegistryPlayerListChanged EventName = "registry.player_list_changed"
)

// Chat and UI events. M7 implements these.
const (
	ChatReceived        EventName = "chat.received"
	ChatRemoved         EventName = "chat.removed"
	ChatTitleChanged    EventName = "chat.title_changed"
	ChatActionBarChanged EventName = "chat.action_bar_changed"
	ChatBossBarChanged  EventName = "chat.boss_bar_changed"
	ChatScoreboardChanged EventName = "chat.scoreboard_changed"
	ChatTeamsChanged    EventName = "chat.teams_changed"
	ChatAdvancementsChanged EventName = "chat.advancements_changed"
	ChatSoundPlayed     EventName = "chat.sound_played"
	ChatStatisticsReceived EventName = "chat.statistics_received"
	ChatDialogShown     EventName = "chat.dialog_shown"
	ChatTabCompleted    EventName = "chat.tab_completed"
)

// domains maps each name to its domain. It is the single source of truth:
// Domain reads it, and AllNames enumerates it.
var domains = map[EventName]Domain{
	SessionConnecting: DomainSession, SessionAuthenticated: DomainSession,
	SessionStateChanged: DomainSession, SessionReady: DomainSession,
	SessionDisconnected: DomainSession, SessionClosed: DomainSession,
	SessionKeepAlivePonged: DomainSession, SessionTransferRequested: DomainSession,
	SessionResourcePackOffered: DomainSession, SessionResourcePackRevoked: DomainSession,
	SessionServerMetadataChanged: DomainSession, SessionCookieRequested: DomainSession,
	SessionCookieStored: DomainSession, SessionCustomPayloadReceived: DomainSession,
	// SessionPacketReceived and SessionPacketSent are deliberately absent.
	// Raw delivery is a selector, not a taxonomy entry: the two names exist
	// so a log line can identify them, and they report DomainRaw so a
	// subscriber opts into them separately.

	PlayerSpawned: DomainPlayer, PlayerMoved: DomainPlayer,
	PlayerHealthChanged: DomainPlayer, PlayerExperienceChanged: DomainPlayer,
	PlayerAbilitiesChanged: DomainPlayer, PlayerGameModeChanged: DomainPlayer,
	PlayerRespawned: DomainPlayer, PlayerHeldSlotChanged: DomainPlayer,
	PlayerEffectsChanged: DomainPlayer, PlayerCooldownChanged: DomainPlayer,

	WorldChunkLoaded: DomainWorld, WorldChunkUnloaded: DomainWorld,
	WorldBlocksChanged: DomainWorld, WorldBlockEntityChanged: DomainWorld,
	WorldLightChanged: DomainWorld, WorldTimeChanged: DomainWorld,
	WorldBorderChanged: DomainWorld, WorldWeatherChanged: DomainWorld,
	WorldDifficultyChanged: DomainWorld, WorldExplosionOccurred: DomainWorld,
	WorldEventOccurred: DomainWorld, WorldSimulationSettingsChanged: DomainWorld,

	EntitySpawned: DomainEntities, EntityRemoved: DomainEntities,
	EntityMoved: DomainEntities, EntityMetadataChanged: DomainEntities,
	EntityEquipmentChanged: DomainEntities, EntityAttributesChanged: DomainEntities,
	EntityEffectsChanged: DomainEntities, EntityVelocityChanged: DomainEntities,
	EntityPassengersChanged: DomainEntities, EntityDamaged: DomainEntities,
	EntityAnimated: DomainEntities, EntityItemCollected: DomainEntities,

	ContainerOpened: DomainContainers, ContainerClosed: DomainContainers,
	ContainerSlotsChanged: DomainContainers, ContainerCursorChanged: DomainContainers,
	ContainerRecipesChanged: DomainContainers, ContainerTradesChanged: DomainContainers,
	ContainerCraftResponse: DomainContainers,

	RegistryDataReceived: DomainRegistry, RegistryTagsReceived: DomainRegistry,
	RegistryCommandsReceived: DomainRegistry, RegistryPlayerListChanged: DomainRegistry,

	ChatReceived: DomainChat, ChatRemoved: DomainChat,
	ChatTitleChanged: DomainChat, ChatActionBarChanged: DomainChat,
	ChatBossBarChanged: DomainChat, ChatScoreboardChanged: DomainChat,
	ChatTeamsChanged: DomainChat, ChatAdvancementsChanged: DomainChat,
	ChatSoundPlayed: DomainChat, ChatStatisticsReceived: DomainChat,
	ChatDialogShown: DomainChat, ChatTabCompleted: DomainChat,
}

// Domain reports which domain an event name belongs to, or zero when the
// name is not part of the taxonomy.
func (n EventName) Domain() Domain { return domains[n] }

// AllNames returns every declared event name in sorted order.
func AllNames() []EventName {
	names := make([]EventName, 0, len(domains))
	for name := range domains {
		names = append(names, name)
	}
	slices.Sort(names)

	return names
}
```

Add `"slices"` to the imports.

- [x] **Step 5: Run and verify it passes**

```bash
devbox run -- task test -- ./event
```

Expected: PASS, all three tests.

- [x] **Step 6: Commit**

```bash
git add event/
git commit -m "feat(event): declare the client event taxonomy"
```

### Task 3: The session events and the collector

Handlers do not publish. They append to a collector scoped to the batch they
are running inside, and the loop publishes the collector's contents once the
batch closes. That is what makes a bundle atomic from a subscriber's view.

**Files:**
- Create: `event/session.go`, `event/collector.go`, `event/session_test.go`, `event/collector_test.go`

**Interfaces:**
- Consumes: `Domain`, `Event`, `EventName` from Task 2.
- Produces: 16 session event structs each implementing `Event`; `Collector` with `Add(Event)`, `Events() []Event`, `Reset()`.

- [x] **Step 1: Write the failing test**

```go
package event_test

import (
	"testing"

	"github.com/go-theft-craft/headless-minecraft/event"
)

func TestSessionEventsReportTheirOwnNames(t *testing.T) {
	cases := []struct {
		value event.Event
		name  event.EventName
	}{
		{event.Connecting{Address: "localhost:25565"}, event.SessionConnecting},
		{event.Authenticated{Username: "tester"}, event.SessionAuthenticated},
		{event.StateChanged{From: "login", To: "play"}, event.SessionStateChanged},
		{event.Ready{EntityID: 7}, event.SessionReady},
		{event.Disconnected{Reason: "kicked"}, event.SessionDisconnected},
		{event.Closed{}, event.SessionClosed},
	}

	for _, tc := range cases {
		if got := tc.value.Name(); got != tc.name {
			t.Errorf("%T reports name %q, want %q", tc.value, got, tc.name)
		}
		if got := tc.value.Domain(); got != event.DomainSession {
			t.Errorf("%T reports domain %d, want DomainSession", tc.value, got)
		}
	}
}

func TestCollectorReturnsEventsInAppendOrder(t *testing.T) {
	var c event.Collector
	c.Add(event.Connecting{Address: "a"})
	c.Add(event.Authenticated{Username: "b"})

	events := c.Events()
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2", len(events))
	}
	if events[0].Name() != event.SessionConnecting {
		t.Errorf("first event is %q, want connecting", events[0].Name())
	}
}

func TestCollectorEventsDoNotAliasTheCollector(t *testing.T) {
	var c event.Collector
	c.Add(event.Connecting{Address: "a"})

	events := c.Events()
	c.Reset()
	c.Add(event.Closed{})

	if len(events) != 1 || events[0].Name() != event.SessionConnecting {
		t.Fatal("Events returned a slice that the collector kept writing into")
	}
}

func TestResetEmptiesTheCollector(t *testing.T) {
	var c event.Collector
	c.Add(event.Closed{})
	c.Reset()

	if got := len(c.Events()); got != 0 {
		t.Errorf("collector holds %d events after Reset, want 0", got)
	}
}
```

- [ ] **Step 2: Run and verify failure**

```bash
devbox run -- task test -- ./event
```

Expected: FAIL, undefined `event.Connecting` and `event.Collector`.

- [x] **Step 3: Implement the session events**

`event/session.go`. Each struct is a value type and implements `Event`.

```go
package event

import "time"

// Connecting reports that the client is about to dial. It carries no
// credential: the address is public and the identity is not.
type Connecting struct {
	Address string
}

func (Connecting) Name() EventName { return SessionConnecting }
func (Connecting) Domain() Domain  { return DomainSession }

// Authenticated reports that the auth provider returned an identity.
type Authenticated struct {
	Username string
	UUID     string
}

func (Authenticated) Name() EventName { return SessionAuthenticated }
func (Authenticated) Domain() Domain  { return DomainSession }

// StateChanged reports a protocol state transition in either direction,
// including the play-to-configuration return that protocol 775 permits.
type StateChanged struct {
	From string
	To   string
}

func (StateChanged) Name() EventName { return SessionStateChanged }
func (StateChanged) Domain() Domain  { return DomainSession }

// Ready reports that the server will accept action packets. It is emitted
// once per connection, at the point Connect returns.
type Ready struct {
	EntityID  int32
	Dimension string
	GameMode  uint8
}

func (Ready) Name() EventName { return SessionReady }
func (Ready) Domain() Domain  { return DomainSession }

// DisconnectSource says who ended the session.
type DisconnectSource string

const (
	// DisconnectByServer is a disconnect packet from the peer.
	DisconnectByServer DisconnectSource = "server"
	// DisconnectByTransport is a connection loss with no disconnect packet.
	DisconnectByTransport DisconnectSource = "transport"
)

// Disconnected reports that the session ended before Close was called.
type Disconnected struct {
	Source DisconnectSource
	Reason string
	State  string
}

func (Disconnected) Name() EventName { return SessionDisconnected }
func (Disconnected) Domain() Domain  { return DomainSession }

// Closed reports that Close finished and every owned goroutine stopped. It
// is the last event a subscription receives.
type Closed struct{}

func (Closed) Name() EventName { return SessionClosed }
func (Closed) Domain() Domain  { return DomainSession }

// KeepAlivePonged reports that the client answered a keepalive.
type KeepAlivePonged struct {
	ID      int64
	Elapsed time.Duration
}

func (KeepAlivePonged) Name() EventName { return SessionKeepAlivePonged }
func (KeepAlivePonged) Domain() Domain  { return DomainSession }

// TransferRequested reports a server asking the client to move to another
// host. The client never follows it: transferring repeats a connection, and
// the library does not reconnect on its own.
type TransferRequested struct {
	Host string
	Port uint16
}

func (TransferRequested) Name() EventName { return SessionTransferRequested }
func (TransferRequested) Domain() Domain  { return DomainSession }

// ResourcePackOffered reports a pack the server offered. The client does not
// download it.
type ResourcePackOffered struct {
	UUID     string
	URL      string
	Hash     string
	Required bool
}

func (ResourcePackOffered) Name() EventName { return SessionResourcePackOffered }
func (ResourcePackOffered) Domain() Domain  { return DomainSession }

// ResourcePackRevoked reports a pack the server withdrew.
type ResourcePackRevoked struct {
	UUID string
}

func (ResourcePackRevoked) Name() EventName { return SessionResourcePackRevoked }
func (ResourcePackRevoked) Domain() Domain  { return DomainSession }

// ServerMetadataChanged reports server-describing data: server data, links,
// feature flags, report details, and low-disk warnings.
type ServerMetadataChanged struct {
	Kind  string
	Value map[string]string
}

func (ServerMetadataChanged) Name() EventName { return SessionServerMetadataChanged }
func (ServerMetadataChanged) Domain() Domain  { return DomainSession }

// CookieRequested reports a server asking for a stored cookie.
type CookieRequested struct {
	Key string
}

func (CookieRequested) Name() EventName { return SessionCookieRequested }
func (CookieRequested) Domain() Domain  { return DomainSession }

// CookieStored reports a server storing a cookie on the client.
type CookieStored struct {
	Key   string
	Bytes int
}

func (CookieStored) Name() EventName { return SessionCookieStored }
func (CookieStored) Domain() Domain  { return DomainSession }

// CustomPayloadReceived reports a plugin message in configuration or play.
// Payload is an owned copy.
type CustomPayloadReceived struct {
	Channel string
	Payload []byte
}

func (CustomPayloadReceived) Name() EventName { return SessionCustomPayloadReceived }
func (CustomPayloadReceived) Domain() Domain  { return DomainSession }

// PacketReceived carries one decoded inbound packet. It is delivered only to
// subscribers that selected DomainRaw.
type PacketReceived struct {
	State     string
	Name      string
	ID        int32
	Bundled   bool
}

func (PacketReceived) Name() EventName { return SessionPacketReceived }
func (PacketReceived) Domain() Domain  { return DomainRaw }

// PacketSent carries one packet the client wrote.
type PacketSent struct {
	State string
	Name  string
	ID    int32
}

func (PacketSent) Name() EventName { return SessionPacketSent }
func (PacketSent) Domain() Domain  { return DomainRaw }
```

`PacketReceived` and `PacketSent` report `DomainRaw`, and Task 2 already left
their names out of the `domains` table for that reason. The struct's `Domain`
method is what a subscriber's selector matches; `EventName.Domain` returns zero
for these two, which is what keeps `TestRawIsNotPartOfTheNamedTaxonomy` honest.

- [x] **Step 4: Implement the collector**

`event/collector.go`:

```go
package event

import "slices"

// Collector accumulates the events produced while one batch is applied.
//
// Handlers append here rather than publishing directly, because a protocol
// 775 bundle must reach subscribers as one unit. The loop resets a collector
// per batch and publishes what it holds once the batch closes.
//
// A Collector is not safe for concurrent use. It does not need to be: it is
// owned by the read goroutine, which is the only goroutine that runs
// handlers.
type Collector struct {
	events []Event
}

// Add appends one event.
func (c *Collector) Add(e Event) { c.events = append(c.events, e) }

// Events returns an owned copy in append order, so the caller keeps a stable
// view after the collector is reset and reused.
func (c *Collector) Events() []Event { return slices.Clone(c.events) }

// Reset empties the collector, retaining its capacity for the next batch.
func (c *Collector) Reset() { c.events = c.events[:0] }
```

- [x] **Step 5: Run and verify it passes**

```bash
devbox run -- task test -- ./event
```

Expected: PASS, including Task 2's three tests.

- [x] **Step 6: Commit**

```bash
git add event/
git commit -m "feat(event): add session events and the batch collector"
```

**What executing this task changed, and why.**

- **The revision from the M7 design landed here**, as that design requires. It
  is not a field on each struct: `Event` gains `Revision() uint64`, and every
  event embeds `Stamp`, which supplies it. The field is unexported and has no
  exported setter, so no handler, reducer, or subscriber can set or forge it.
- **The collector stamps, which forced `Emit` to be a function.** The design
  says the collector stamps every event it holds after the revision is bumped.
  An event is a value inside an `Event` interface, and a value in an interface
  cannot be written to, so a collector that took `Add(Event)` would have erased
  the concrete type it needs to stamp. Go methods cannot be generic, so the
  append is the package function `event.Emit(c, X{...})`, which remembers the
  type and stamps a copy at publication. `Events` therefore takes the revision:
  `Events(revision uint64) []Event`. There is no way to get an unstamped event
  out of a collector, which is the guarantee stated as a mechanism.
- **Stamped events keep their concrete types.** The obvious alternative — wrap
  each event in a stamping wrapper — would have broken every `switch e :=
  e.(type)` a subscriber writes. A test asserts the concrete type survives
  publication.
- **`PacketReceived` and `PacketSent` cannot have a `Name` field.** The plan
  gave both one alongside the `Name()` method every event owes `Event`, which
  does not compile. The field is `Packet`.
- **Steps 1 and 2 ran out of order.** The tests were written after the
  implementation rather than before it, so the failure observed was the
  generic constraint failing to compile, not the intended undefined-symbol
  failure. The tests do fail against an absent implementation; they were not
  watched doing it.
- **`.golangci.yml` gained one scoped exclusion.** revive requires a doc
  comment on every exported method, and the plan's own code declares 32 bare
  one-line `Name` and `Domain` methods. The rule is switched off for those
  three method names under `event/` only, because their meaning is documented
  on the `Event` interface and repeating it 32 times would say nothing about
  the event it sits on. Every other exported symbol still needs its comment.

---

## Stage C — Batching and readiness

### Task 4: The bundle batcher

Protocol 775 wraps groups of packets in `PlayClientboundBundleDelimiter`, which
toggles: the first occurrence opens a bundle and the next closes it. Packets
inside must be applied atomically. Protocol 47 has no delimiter, so every batch
holds one packet and the interface needs no version branch.

**Files:**
- Create: `version/batch.go`, `version/batch_test.go`

**Interfaces:**
- Produces: `Batch{Packets []protocol.Packet; Bundled bool}`, `Batcher`, `NewBatcher(delimiter string, limit int) (*Batcher, error)`, `(*Batcher).Accept(protocol.Packet) (Batch, bool, error)`, `(*Batcher).Open() bool`, `ErrBundleTooLarge`, `ErrBundleUnterminated`.

- [x] **Step 1: Write the failing test**

```go
package version_test

import (
	"errors"
	"testing"

	protocol "github.com/go-theft-craft/minecraft-protocol"

	"github.com/go-theft-craft/headless-minecraft/version"
)

func packet(name string) protocol.Packet {
	return protocol.Packet{Name: name, State: "play", Direction: protocol.DirectionClientbound}
}

func TestUnbundledProtocolAlwaysEmitsSinglePacketBatches(t *testing.T) {
	b, err := version.NewBatcher("", 16)
	if err != nil {
		t.Fatalf("NewBatcher: %v", err)
	}

	for _, name := range []string{"login", "position", "chat"} {
		batch, ready, err := b.Accept(packet(name))
		if err != nil {
			t.Fatalf("Accept(%s): %v", name, err)
		}
		if !ready {
			t.Fatalf("Accept(%s) withheld a batch on a protocol with no delimiter", name)
		}
		if len(batch.Packets) != 1 || batch.Bundled {
			t.Fatalf("Accept(%s) produced %d packets, bundled=%v", name, len(batch.Packets), batch.Bundled)
		}
	}
}

func TestDelimiterTogglesAndGroups(t *testing.T) {
	b, _ := version.NewBatcher("bundle_delimiter", 16)

	if _, ready, err := b.Accept(packet("bundle_delimiter")); err != nil || ready {
		t.Fatalf("opening delimiter produced ready=%v err=%v, want false/nil", ready, err)
	}
	if !b.Open() {
		t.Fatal("batcher reports closed after an opening delimiter")
	}

	for _, name := range []string{"spawn_entity", "entity_metadata"} {
		if _, ready, err := b.Accept(packet(name)); err != nil || ready {
			t.Fatalf("packet inside a bundle produced ready=%v err=%v, want false/nil", ready, err)
		}
	}

	batch, ready, err := b.Accept(packet("bundle_delimiter"))
	if err != nil || !ready {
		t.Fatalf("closing delimiter produced ready=%v err=%v, want true/nil", ready, err)
	}
	if !batch.Bundled {
		t.Error("closed bundle reports Bundled=false")
	}
	if len(batch.Packets) != 2 {
		t.Fatalf("bundle holds %d packets, want 2", len(batch.Packets))
	}
	if batch.Packets[0].Name != "spawn_entity" || batch.Packets[1].Name != "entity_metadata" {
		t.Error("bundle lost wire order")
	}
	if b.Open() {
		t.Error("batcher reports open after a closing delimiter")
	}
}

func TestPacketOutsideABundleEmitsImmediately(t *testing.T) {
	b, _ := version.NewBatcher("bundle_delimiter", 16)

	batch, ready, err := b.Accept(packet("keep_alive"))
	if err != nil || !ready {
		t.Fatalf("got ready=%v err=%v, want true/nil", ready, err)
	}
	if batch.Bundled || len(batch.Packets) != 1 {
		t.Errorf("unbundled packet produced bundled=%v with %d packets", batch.Bundled, len(batch.Packets))
	}
}

func TestEmptyBundleIsAValidEmptyBatch(t *testing.T) {
	b, _ := version.NewBatcher("bundle_delimiter", 16)
	_, _, _ = b.Accept(packet("bundle_delimiter"))

	batch, ready, err := b.Accept(packet("bundle_delimiter"))
	if err != nil || !ready {
		t.Fatalf("got ready=%v err=%v, want true/nil", ready, err)
	}
	if len(batch.Packets) != 0 || !batch.Bundled {
		t.Errorf("empty bundle produced %d packets, bundled=%v", len(batch.Packets), batch.Bundled)
	}
}

func TestOversizeBundleIsAnErrorBeforeUnboundedBuffering(t *testing.T) {
	b, _ := version.NewBatcher("bundle_delimiter", 3)
	_, _, _ = b.Accept(packet("bundle_delimiter"))

	var err error
	for i := 0; i < 10 && err == nil; i++ {
		_, _, err = b.Accept(packet("filler"))
	}
	if !errors.Is(err, version.ErrBundleTooLarge) {
		t.Fatalf("got %v, want ErrBundleTooLarge", err)
	}
}

func TestFinishReportsAnUnterminatedBundle(t *testing.T) {
	b, _ := version.NewBatcher("bundle_delimiter", 16)
	_, _, _ = b.Accept(packet("bundle_delimiter"))
	_, _, _ = b.Accept(packet("spawn_entity"))

	if err := b.Finish(); !errors.Is(err, version.ErrBundleUnterminated) {
		t.Fatalf("got %v, want ErrBundleUnterminated", err)
	}
}

func TestFinishOnAClosedBatcherIsNil(t *testing.T) {
	b, _ := version.NewBatcher("bundle_delimiter", 16)
	_, _, _ = b.Accept(packet("keep_alive"))

	if err := b.Finish(); err != nil {
		t.Fatalf("Finish on a closed batcher returned %v, want nil", err)
	}
}

func TestNewBatcherRejectsANonPositiveLimit(t *testing.T) {
	if _, err := version.NewBatcher("bundle_delimiter", 0); err == nil {
		t.Fatal("NewBatcher accepted a zero limit")
	}
}
```

- [x] **Step 2: Run and verify failure**

```bash
devbox run -- task test -- ./version
```

Expected: FAIL, undefined `version.NewBatcher`.

- [x] **Step 3: Implement**

`version/batch.go`:

```go
package version

import (
	"errors"
	"fmt"
	"slices"

	protocol "github.com/go-theft-craft/minecraft-protocol"
)

// ErrBundleTooLarge reports a bundle that exceeded its packet limit. It is a
// protocol error rather than a truncation, because the alternative is an
// unbounded buffer fed by the peer.
var ErrBundleTooLarge = errors.New("bundle exceeds its packet limit")

// ErrBundleUnterminated reports a bundle still open when the stream ended.
var ErrBundleUnterminated = errors.New("bundle was never closed")

// Batch is the unit the read loop delivers.
//
// A batch is applied atomically: M7 bumps the observed-state revision once
// per batch, never once per packet, so a subscriber never sees an entity
// spawned without the metadata that arrived with it.
type Batch struct {
	Packets []protocol.Packet
	Bundled bool
}

// Batcher groups inbound packets at bundle boundaries.
//
// It is owned by the read goroutine and is not safe for concurrent use.
type Batcher struct {
	delimiter string
	limit     int
	pending   []protocol.Packet
	open      bool
}

// NewBatcher returns a batcher for one protocol.
//
// An empty delimiter names a protocol with no bundling, such as Java 1.8's
// protocol 47: every packet becomes its own batch and Open is always false.
// The limit bounds one bundle's packet count.
func NewBatcher(delimiter string, limit int) (*Batcher, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("%w: limit must be positive, got %d", ErrBundleTooLarge, limit)
	}

	return &Batcher{delimiter: delimiter, limit: limit}, nil
}

// Open reports whether a bundle is currently accumulating.
func (b *Batcher) Open() bool { return b.open }

// Accept consumes one packet. It returns a batch when one is complete; the
// boolean is false while a bundle is still accumulating.
func (b *Batcher) Accept(p protocol.Packet) (Batch, bool, error) {
	if b.delimiter == "" {
		return Batch{Packets: []protocol.Packet{p}}, true, nil
	}

	if p.Name == b.delimiter {
		if !b.open {
			b.open = true

			return Batch{}, false, nil
		}

		batch := Batch{Packets: slices.Clone(b.pending), Bundled: true}
		b.pending = b.pending[:0]
		b.open = false

		return batch, true, nil
	}

	if !b.open {
		return Batch{Packets: []protocol.Packet{p}}, true, nil
	}

	if len(b.pending) >= b.limit {
		return Batch{}, false, fmt.Errorf("%w: limit is %d", ErrBundleTooLarge, b.limit)
	}
	b.pending = append(b.pending, p)

	return Batch{}, false, nil
}

// Finish reports whether the stream ended mid-bundle. The loop calls it after
// its last read, so a peer that opens a bundle and disappears is a named
// error rather than silently discarded packets.
func (b *Batcher) Finish() error {
	if b.open {
		return fmt.Errorf("%w: %d packets pending", ErrBundleUnterminated, len(b.pending))
	}

	return nil
}
```

- [x] **Step 4: Run and verify it passes**

```bash
devbox run -- task test -- ./version
```

Expected: PASS, all eight tests.

- [x] **Step 5: Commit**

```bash
git add version/batch.go version/batch_test.go
git commit -m "feat(version): group inbound packets at bundle boundaries"
```

**What executing this task changed, and why.**

- **A non-positive limit no longer wraps `ErrBundleTooLarge`.** The plan
  returned that sentinel from `NewBatcher`, which would have made a caller
  passing a bad limit indistinguishable from a peer sending an oversize
  bundle. The first is a bug here and the second is hostile traffic, and Task
  11 has to tell them apart. `NewBatcher` returns a plain error.
- **One test was added**, covering that a returned bundle does not alias the
  batcher's reused pending slice. The implementation clones, and nothing in
  the plan's eight tests would have failed if it stopped cloning.

### Task 5: The readiness rule and the extended profile

`Connect` returns when the server accepts action packets, and that point is
version-specific. Protocol 775 places the player with `PlayClientboundPosition`
carrying a `TeleportID` and expects `PlayServerboundTeleportConfirm`. Protocol
47's `PlayClientboundPosition` has no teleport ID and the acknowledgement is a
`PlayServerboundPositionLook` echo.

**Files:**
- Create: `version/readiness.go`
- Modify: `version/profile.go`
- Create: `version/profile_test.go`

**Interfaces:**
- Consumes: `Batch` from Task 4.
- Produces: `ReadinessRule`, `ReadyState`, `Adapter` with `Handlers()`, `WireProfile.Readiness`, `ErrRelativeSpawn`.

- [x] **Step 1: Write the failing test**

```go
package version_test

import (
	"testing"

	protocol "github.com/go-theft-craft/minecraft-protocol"

	"github.com/go-theft-craft/headless-minecraft/version"
)

type stubAdapter struct{ id string }

func (s stubAdapter) ProtocolID() string { return s.id }
func (stubAdapter) Handlers() map[string]version.Handler {
	return map[string]version.Handler{}
}

type stubReadiness struct{}

func (stubReadiness) Observe(version.Batch) (version.ReadyState, []protocol.Packet, error) {
	return version.ReadyState{}, nil, nil
}

func TestValidateRejectsAProfileWithoutAReadinessRule(t *testing.T) {
	limits, err := protocol.NewLimits()
	if err != nil {
		t.Fatalf("NewLimits: %v", err)
	}

	p := version.WireProfile{
		ID:       "java/1.8.9",
		Protocol: fakeProtocol{id: "java/1.8.9"},
		Adapter:  stubAdapter{id: "java/1.8.9"},
		Limits:   limits,
	}

	if err := p.Validate(); err == nil {
		t.Fatal("Validate accepted a profile with no readiness rule")
	}
}

func TestValidateAcceptsACompleteProfile(t *testing.T) {
	limits, _ := protocol.NewLimits()
	p := version.WireProfile{
		ID:        "java/1.8.9",
		Protocol:  fakeProtocol{id: "java/1.8.9"},
		Adapter:   stubAdapter{id: "java/1.8.9"},
		Limits:    limits,
		Readiness: stubReadiness{},
	}

	if err := p.Validate(); err != nil {
		t.Fatalf("Validate rejected a complete profile: %v", err)
	}
}
```

Add a `fakeProtocol` implementing `protocol.Protocol` in the test file: `ID`
returns its field, `Edition` returns `protocol.EditionJava`, `Version` returns
`protocol.Version{Name: "1.8.9", Protocol: 47}`, and `NewSession` returns
`nil, nil` because no test in this file starts a session.

- [x] **Step 2: Run and verify failure**

```bash
devbox run -- task test -- ./version
```

Expected: FAIL, `WireProfile` has no `Readiness` field and `Adapter` has no
`Handlers`.

- [x] **Step 3: Implement the readiness contract**

`version/readiness.go`:

```go
package version

import (
	"errors"

	protocol "github.com/go-theft-craft/minecraft-protocol"
)

// ErrRelativeSpawn reports a spawn position the client cannot acknowledge.
//
// Both protocols mark a position packet's fields as absolute or relative. At
// spawn the server sends absolute coordinates, and answering a relative one
// correctly requires a prior position the connection layer does not track --
// that is M7's observed state. A relative spawn is therefore a named error
// rather than a wrong acknowledgement.
var ErrRelativeSpawn = errors.New("server placed the player relative to an unknown position")

// ReadyState is what a readiness rule learned on the way to ready.
type ReadyState struct {
	// Ready reports that the server will accept action packets.
	Ready bool
	// EntityID, Dimension, and GameMode come from the play login packet and
	// are zero until it arrives.
	EntityID  int32
	Dimension string
	GameMode  uint8
}

// ReadinessRule decides when a connection has reached the point where the
// server accepts action packets, and supplies whatever the client must send
// to get there.
//
// It is version-owned because the sequence differs: protocol 775 answers the
// placing position with a teleport confirmation carrying its ID, and protocol
// 47 has no such packet and echoes a position-look instead.
//
// Observe is called once per batch, on the read goroutine, until it reports
// Ready. The packets it returns are written in the order given.
type ReadinessRule interface {
	Observe(Batch) (ReadyState, []protocol.Packet, error)
}
```

- [x] **Step 4: Extend the profile**

In `version/profile.go`, replace the `Adapter` interface and add the field.
Keep the existing `Validate` checks and add one:

```go
// Handler processes one dispatched packet. It matches the signature the
// shared router's middleware.Handler declares, restated here so this package
// does not import the router to name a one-method function type.
type Handler interface {
	Handle(ctx context.Context, p protocol.Packet) error
}

// Adapter translates one protocol's packets into client events.
type Adapter interface {
	ProtocolID() string
	// Handlers are registered with the router by packet name. Each appends
	// to the batch-scoped collector it was built with; none publishes
	// directly, because a batch's events are published together or not at
	// all.
	Handlers() map[string]Handler
}

// WireProfile is the transport portion of a complete gameplay profile.
// Later components extend this with physics, collision, inventory, and
// ordering rules.
type WireProfile struct {
	ID        string
	Protocol  protocol.Protocol
	Adapter   Adapter
	Limits    protocol.Limits
	Readiness ReadinessRule
}
```

In `Validate`, after the limits check:

```go
	if p.Readiness == nil {
		return fmt.Errorf("%w: missing readiness rule", ErrInvalidProfile)
	}
```

Add `"context"` to the imports.

- [x] **Step 5: Run and verify it passes**

```bash
devbox run -- task test -- ./version
```

Expected: PASS, including Task 4's batcher tests.

- [x] **Step 6: Commit**

```bash
git add version/
git commit -m "feat(version): add the readiness rule and adapter handlers"
```

**What executing this task changed, and why.**

- **`Validate`'s existing checks gained tests.** The plan tested only the new
  readiness check, leaving the ID, protocol, adapter, limits, and
  protocol/adapter-mismatch branches uncovered. They are covered now, which is
  what takes the package to 100%.
- **A compile-time assertion pins `Handler`'s signature.** The interface is
  deliberately identical to the router's `middleware.Handler` and is written
  out by hand rather than imported, so nothing but a test notices if the two
  drift.

---

## Stage D — Authentication and construction

### Task 6: The authentication seam

M6.3 ships offline only. `login.Offline` in `minecraft-protocol` already
derives the offline UUID byte-identically to the server's own, which is what
keeps saved player files reachable.

**Files:**
- Create: `auth/auth.go`, `auth/auth_test.go`

**Interfaces:**
- Produces: `Provider`, `Offline(name string) (Provider, error)`, `Identity`.

- [ ] **Step 1: Write the failing test**

```go
package auth_test

import (
	"context"
	"testing"

	"github.com/go-theft-craft/headless-minecraft/auth"
)

func TestOfflineReturnsAValidatedIdentity(t *testing.T) {
	p, err := auth.Offline("tester")
	if err != nil {
		t.Fatalf("Offline: %v", err)
	}

	id, err := p.Authenticate(context.Background())
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if id.Username != "tester" {
		t.Errorf("username is %q, want tester", id.Username)
	}
	if id.Authenticator == nil {
		t.Error("identity carries no authenticator for the login negotiator")
	}
}

func TestOfflineRejectsAnInvalidName(t *testing.T) {
	if _, err := auth.Offline(""); err == nil {
		t.Fatal("Offline accepted an empty username")
	}
}

func TestOfflineIsDeterministic(t *testing.T) {
	first, _ := auth.Offline("tester")
	second, _ := auth.Offline("tester")

	a, _ := first.Authenticate(context.Background())
	b, _ := second.Authenticate(context.Background())

	if a.UUID != b.UUID {
		t.Errorf("offline UUID is not stable: %q then %q", a.UUID, b.UUID)
	}
}
```

- [ ] **Step 2: Run and verify failure**

```bash
devbox run -- task test -- ./auth
```

Expected: FAIL, package `auth` does not exist.

- [ ] **Step 3: Implement**

`auth/auth.go`:

```go
// Package auth supplies a session identity to the client.
//
// M6.3 implements offline authentication only. The Microsoft device-code
// provider is M6.4 and plugs into the same seam: it is four HTTP exchanges
// plus refresh and storage, shares nothing with the packet loop, and blocks
// nothing in M7, because offline mode covers every test server.
//
// Secrets and access tokens never appear in errors, logs, or events.
package auth

import (
	"context"
	"fmt"

	"github.com/go-theft-craft/minecraft-protocol/login"
)

// Identity is the account a connection presents.
type Identity struct {
	Username string
	UUID     string
	// Authenticator is handed to the shared login negotiator, which calls
	// it to prove account ownership. The offline one does nothing, because
	// there is nobody to tell.
	Authenticator login.Authenticator
}

// Provider supplies an identity before the client dials.
type Provider interface {
	Authenticate(ctx context.Context) (Identity, error)
}

type offline struct {
	inner login.Offline
}

// Offline returns a provider for a server that does not verify accounts. It
// validates the name at construction, so an invalid one fails before any
// network work.
func Offline(name string) (Provider, error) {
	inner, err := login.NewOffline(name)
	if err != nil {
		return nil, fmt.Errorf("offline provider: %w", err)
	}

	return offline{inner: inner}, nil
}

// Authenticate implements Provider. It makes no request.
func (o offline) Authenticate(context.Context) (Identity, error) {
	profile := o.inner.Profile()

	return Identity{
		Username:      profile.Name.String(),
		UUID:          login.OfflineUUID(profile.Name).String(),
		Authenticator: o.inner,
	}, nil
}
```

- [ ] **Step 4: Run and verify it passes**

```bash
devbox run -- task test -- ./auth
```

Expected: PASS, all three tests.

If `login.OfflineUUID` or `java.UUID.String()` do not match these names, read
`../minecraft-protocol/login/acceptor.go:311` and adjust; do not invent a UUID
derivation here, because the server looks player files up by it.

- [ ] **Step 5: Commit**

```bash
git add auth/
git commit -m "feat(auth): add the offline identity provider"
```

### Task 7: Subscriptions

A subscriber that falls behind is dropped, not blocked. That is the opposite of
M5's history ring, which drops data and keeps running: a ring is a debugger's
tool where forgetting is fine, and a subscriber silently missing events would
have a wrong view of the world without being told.

**Files:**
- Create: `client/subscription.go`, `client/subscription_test.go`

**Interfaces:**
- Consumes: `event.Domain`, `event.Event`.
- Produces: `Subscription`, `(*Subscription).C()`, `Err()`, `Close()`, `ErrOverflow`, and the internal `fanout` with `subscribe`, `publish`, `closeAll`.

- [ ] **Step 1: Write the failing test**

```go
package client

import (
	"errors"
	"testing"

	"github.com/go-theft-craft/headless-minecraft/event"
)

func TestSubscriberReceivesOnlySelectedDomains(t *testing.T) {
	var f fanout
	sub, err := f.subscribe(event.DomainSession, 4)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	f.publish([]event.Event{
		event.Connecting{Address: "a"},
		event.PacketReceived{Name: "keep_alive"},
	})

	got := <-sub.C()
	if got.Name() != event.SessionConnecting {
		t.Fatalf("received %q, want connecting", got.Name())
	}

	select {
	case extra := <-sub.C():
		t.Fatalf("received unselected event %q", extra.Name())
	default:
	}
}

func TestSlowSubscriberOverflowsAndCloses(t *testing.T) {
	var f fanout
	sub, _ := f.subscribe(event.DomainSession, 1)

	f.publish([]event.Event{event.Connecting{Address: "a"}})
	f.publish([]event.Event{event.Closed{}})

	// Drain what fitted, then the channel must be closed.
	<-sub.C()
	if _, open := <-sub.C(); open {
		t.Fatal("channel stayed open after overflow")
	}
	if !errors.Is(sub.Err(), ErrOverflow) {
		t.Fatalf("Err is %v, want ErrOverflow", sub.Err())
	}
}

func TestOverflowDoesNotStallOtherSubscribers(t *testing.T) {
	var f fanout
	slow, _ := f.subscribe(event.DomainSession, 1)
	fast, _ := f.subscribe(event.DomainSession, 16)

	for i := 0; i < 8; i++ {
		f.publish([]event.Event{event.Connecting{Address: "a"}})
	}

	if len(fast.C()) != 8 {
		t.Fatalf("fast subscriber holds %d events, want 8", len(fast.C()))
	}
	<-slow.C()
	if _, open := <-slow.C(); open {
		t.Fatal("slow subscriber survived overflow")
	}
}

func TestCloseIsIdempotentAndRaceFree(t *testing.T) {
	var f fanout
	sub, _ := f.subscribe(event.DomainSession, 4)

	if err := sub.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := sub.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}

	// Publishing to a closed subscription must not panic on a closed channel.
	f.publish([]event.Event{event.Connecting{Address: "a"}})
}

func TestCloseAllClosesEverySubscription(t *testing.T) {
	var f fanout
	first, _ := f.subscribe(event.DomainSession, 4)
	second, _ := f.subscribe(event.DomainRaw, 4)

	f.closeAll()

	if _, open := <-first.C(); open {
		t.Error("first subscription stayed open")
	}
	if _, open := <-second.C(); open {
		t.Error("second subscription stayed open")
	}
}

func TestSubscribeRejectsANonPositiveBuffer(t *testing.T) {
	var f fanout
	if _, err := f.subscribe(event.DomainSession, 0); err == nil {
		t.Fatal("subscribe accepted a zero buffer")
	}
}
```

- [ ] **Step 2: Run and verify failure**

```bash
devbox run -- task test -- ./client
```

Expected: FAIL, package `client` does not exist.

- [ ] **Step 3: Implement**

`client/subscription.go`:

```go
package client

import (
	"errors"
	"fmt"
	"sync"

	"github.com/go-theft-craft/headless-minecraft/event"
)

// ErrOverflow reports a subscriber that fell behind its buffer.
//
// The subscription is closed rather than the event dropped. A ring that
// forgets is the right tool for a debugger, which is what M5's history ring
// is; a subscriber that silently missed an event would hold a wrong view of
// the world and never learn it did.
var ErrOverflow = errors.New("subscription buffer overflowed")

// Subscription delivers selected events to one consumer.
type Subscription struct {
	selector event.Domain
	ch       chan event.Event

	mu     sync.Mutex
	closed bool
	err    error
}

// C returns the delivery channel. It closes when the subscription ends, by
// Close, by overflow, or by the client shutting down.
func (s *Subscription) C() <-chan event.Event { return s.ch }

// Err reports why the subscription ended, or nil for a clean close.
func (s *Subscription) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.err
}

// Close ends the subscription. It is idempotent.
func (s *Subscription) Close() error {
	s.finish(nil)

	return nil
}

// finish closes the channel exactly once and records the reason.
func (s *Subscription) finish(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return
	}
	s.closed = true
	s.err = err
	close(s.ch)
}

// deliver attempts one non-blocking send. It reports whether the
// subscription is still alive.
func (s *Subscription) deliver(e event.Event) bool {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()

		return false
	}

	select {
	case s.ch <- e:
		s.mu.Unlock()

		return true
	default:
		s.closed = true
		s.err = fmt.Errorf("%w: capacity %d", ErrOverflow, cap(s.ch))
		close(s.ch)
		s.mu.Unlock()

		return false
	}
}

// fanout owns every subscription and publishes to them.
//
// Publishing never blocks: a full subscriber is closed, not waited on, so a
// slow consumer cannot stall the read goroutine or delay keepalive handling.
type fanout struct {
	mu   sync.Mutex
	subs []*Subscription
}

func (f *fanout) subscribe(selector event.Domain, buffer int) (*Subscription, error) {
	if buffer <= 0 {
		return nil, fmt.Errorf("subscribe: buffer must be positive, got %d", buffer)
	}
	if selector == 0 {
		return nil, errors.New("subscribe: no domain selected")
	}

	sub := &Subscription{selector: selector, ch: make(chan event.Event, buffer)}

	f.mu.Lock()
	f.subs = append(f.subs, sub)
	f.mu.Unlock()

	return sub, nil
}

// publish delivers one batch's events to every matching subscription and
// drops the subscriptions that overflowed.
func (f *fanout) publish(events []event.Event) {
	if len(events) == 0 {
		return
	}

	f.mu.Lock()
	live := f.subs[:0]
	for _, sub := range f.subs {
		alive := true
		for _, e := range events {
			if sub.selector&e.Domain() == 0 {
				continue
			}
			if !sub.deliver(e) {
				alive = false

				break
			}
		}
		if alive {
			live = append(live, sub)
		}
	}
	f.subs = live
	f.mu.Unlock()
}

func (f *fanout) closeAll() {
	f.mu.Lock()
	subs := f.subs
	f.subs = nil
	f.mu.Unlock()

	for _, sub := range subs {
		sub.finish(nil)
	}
}
```

- [ ] **Step 4: Run and verify it passes**

```bash
devbox run -- task test -- ./client
```

Expected: PASS, all six tests, under `-race`.

- [ ] **Step 5: Commit**

```bash
git add client/subscription.go client/subscription_test.go
git commit -m "feat(client): add bounded event subscriptions"
```

### Task 8: The client, its options, and validation

`client.New` does no network and no authentication. It validates the profile,
the address, and the safety authorization, so a misconfigured client fails
before it dials.

**Files:**
- Create: `client/client.go`, `client/client_test.go`

**Interfaces:**
- Consumes: `version.WireProfile`, `auth.Provider`, `safety.Authorization`, `fanout`.
- Produces: `Client`, `New(...Option) (*Client, error)`, `WithAddress`, `WithAuth`, `WithVersion`, `WithAuthorization`, `WithLogger`, `WithConnectTimeout`, `WithBundleLimit`, `(*Client).Subscribe`, `ErrInvalidClient`.

- [ ] **Step 1: Write the failing test**

```go
package client_test

import (
	"errors"
	"testing"
	"time"

	"github.com/go-theft-craft/headless-minecraft/auth"
	"github.com/go-theft-craft/headless-minecraft/client"
	"github.com/go-theft-craft/headless-minecraft/event"
	"github.com/go-theft-craft/headless-minecraft/safety"
	"github.com/go-theft-craft/headless-minecraft/version/java"
)

func testOptions() []client.Option {
	provider, _ := auth.Offline("tester")
	authz, _ := safety.Authorize("localhost:25565", safety.ScopeObserve)

	return []client.Option{
		client.WithAddress("localhost:25565"),
		client.WithAuth(provider),
		client.WithVersion(java.Java1_8()),
		client.WithAuthorization(authz),
	}
}

func TestNewRejectsAMissingAddress(t *testing.T) {
	opts := testOptions()[1:]
	if _, err := client.New(opts...); !errors.Is(err, client.ErrInvalidClient) {
		t.Fatalf("got %v, want ErrInvalidClient", err)
	}
}

func TestNewRejectsAMissingAuthProvider(t *testing.T) {
	provider, _ := auth.Offline("tester")
	_ = provider
	authz, _ := safety.Authorize("localhost:25565", safety.ScopeObserve)

	_, err := client.New(
		client.WithAddress("localhost:25565"),
		client.WithVersion(java.Java1_8()),
		client.WithAuthorization(authz),
	)
	if !errors.Is(err, client.ErrInvalidClient) {
		t.Fatalf("got %v, want ErrInvalidClient", err)
	}
}

func TestNewRejectsAnAuthorizationForADifferentEndpoint(t *testing.T) {
	provider, _ := auth.Offline("tester")
	authz, _ := safety.Authorize("elsewhere.example:25565", safety.ScopeObserve)

	_, err := client.New(
		client.WithAddress("localhost:25565"),
		client.WithAuth(provider),
		client.WithVersion(java.Java1_8()),
		client.WithAuthorization(authz),
	)
	if !errors.Is(err, client.ErrInvalidClient) {
		t.Fatalf("got %v, want ErrInvalidClient", err)
	}
}

func TestNewRejectsAnInvalidProfile(t *testing.T) {
	provider, _ := auth.Offline("tester")
	authz, _ := safety.Authorize("localhost:25565", safety.ScopeObserve)

	_, err := client.New(
		client.WithAddress("localhost:25565"),
		client.WithAuth(provider),
		client.WithVersion(version.WireProfile{ID: "incomplete"}),
		client.WithAuthorization(authz),
	)
	if !errors.Is(err, client.ErrInvalidClient) {
		t.Fatalf("got %v, want ErrInvalidClient", err)
	}
}

func TestNewAcceptsACompleteConfiguration(t *testing.T) {
	bot, err := client.New(testOptions()...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = bot.Close() }()

	sub, err := bot.Subscribe(event.DomainSession, 8)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	_ = sub.Close()
}

func TestNewRejectsANonPositiveConnectTimeout(t *testing.T) {
	opts := append(testOptions(), client.WithConnectTimeout(0))
	if _, err := client.New(opts...); !errors.Is(err, client.ErrInvalidClient) {
		t.Fatalf("got %v, want ErrInvalidClient", err)
	}
}

func TestNewAppliesADefaultConnectTimeout(t *testing.T) {
	bot, err := client.New(testOptions()...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = bot.Close() }()

	if got := bot.ConnectTimeout(); got != 30*time.Second {
		t.Errorf("default connect timeout is %v, want 30s", got)
	}
}
```

Import `version` in the test file for `TestNewRejectsAnInvalidProfile`.

- [ ] **Step 2: Run and verify failure**

```bash
devbox run -- task test -- ./client
```

Expected: FAIL, `client.New` undefined.

- [ ] **Step 3: Implement**

`client/client.go`:

```go
// Package client connects to a Java Edition server and publishes what it
// observes.
//
// It owns the connection lifecycle and one read loop. Observed world state
// is M7 and plugs into the batch boundary this package defines; gameplay
// actions are M9.
//
// The library never reconnects. Reconnecting can repeat actions, so retry
// policy belongs to the application.
package client

import (
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/go-theft-craft/headless-minecraft/auth"
	"github.com/go-theft-craft/headless-minecraft/event"
	"github.com/go-theft-craft/headless-minecraft/safety"
	"github.com/go-theft-craft/headless-minecraft/version"
)

const (
	defaultConnectTimeout = 30 * time.Second
	defaultBundleLimit    = 4096
)

// ErrInvalidClient reports a configuration rejected before any network work.
var ErrInvalidClient = errors.New("invalid client configuration")

// Option configures a client at construction.
type Option func(*Client) error

// Client is one connection's owner. It is safe for concurrent use by the
// methods it exposes; its internals are owned by the loop goroutine.
type Client struct {
	address        string
	provider       auth.Provider
	profile        version.WireProfile
	authorization  safety.Authorization
	recovery       safety.Profile
	logger         *slog.Logger
	connectTimeout time.Duration
	bundleLimit    int

	events fanout

	mu       sync.Mutex
	closed   bool
	closeErr error
	stop     func()
	done     chan struct{}
}

// WithAddress sets the server endpoint. There is no default: a client that
// dialled something by default would be a client that dialled by accident.
func WithAddress(address string) Option {
	return func(c *Client) error {
		if address == "" {
			return fmt.Errorf("%w: empty address", ErrInvalidClient)
		}
		c.address = address

		return nil
	}
}

// WithAuth sets the identity provider.
func WithAuth(provider auth.Provider) Option {
	return func(c *Client) error {
		if provider == nil {
			return fmt.Errorf("%w: nil auth provider", ErrInvalidClient)
		}
		c.provider = provider

		return nil
	}
}

// WithVersion sets the complete wire profile.
func WithVersion(profile version.WireProfile) Option {
	return func(c *Client) error {
		c.profile = profile

		return nil
	}
}

// WithAuthorization records the operator's declaration of endpoint and
// scopes. It cannot prove permission; it prevents a script using high-level
// actions against an arbitrary address by accident.
func WithAuthorization(a safety.Authorization) Option {
	return func(c *Client) error {
		c.authorization = a

		return nil
	}
}

// WithLogger sets a structured logger. The default discards output. Packet
// payloads are never logged: they carry chat, plugin data, and identity.
func WithLogger(logger *slog.Logger) Option {
	return func(c *Client) error {
		if logger == nil {
			return fmt.Errorf("%w: nil logger", ErrInvalidClient)
		}
		c.logger = logger

		return nil
	}
}

// WithConnectTimeout bounds Connect. Connect waits on a packet the server
// sends at its own pace, so it needs a deadline of its own.
func WithConnectTimeout(d time.Duration) Option {
	return func(c *Client) error {
		if d <= 0 {
			return fmt.Errorf("%w: connect timeout must be positive, got %v", ErrInvalidClient, d)
		}
		c.connectTimeout = d

		return nil
	}
}

// WithBundleLimit bounds one protocol 775 bundle's packet count.
func WithBundleLimit(n int) Option {
	return func(c *Client) error {
		if n <= 0 {
			return fmt.Errorf("%w: bundle limit must be positive, got %d", ErrInvalidClient, n)
		}
		c.bundleLimit = n

		return nil
	}
}

// New validates a configuration and returns a client that has not connected.
// It performs no network work and no authentication.
func New(options ...Option) (*Client, error) {
	c := &Client{
		logger:         slog.New(slog.DiscardHandler),
		recovery:       safety.Strict(),
		connectTimeout: defaultConnectTimeout,
		bundleLimit:    defaultBundleLimit,
		done:           make(chan struct{}),
	}

	for _, option := range options {
		if option == nil {
			return nil, fmt.Errorf("%w: nil option", ErrInvalidClient)
		}
		if err := option(c); err != nil {
			return nil, err
		}
	}

	if c.address == "" {
		return nil, fmt.Errorf("%w: no address", ErrInvalidClient)
	}
	if c.provider == nil {
		return nil, fmt.Errorf("%w: no auth provider", ErrInvalidClient)
	}
	if err := c.profile.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidClient, err)
	}
	if err := c.authorization.Permits(c.address, safety.ScopeObserve); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidClient, err)
	}

	return c, nil
}

// ConnectTimeout reports the deadline Connect applies.
func (c *Client) ConnectTimeout() time.Duration { return c.connectTimeout }

// Subscribe returns a bounded subscription over the selected domains.
func (c *Client) Subscribe(selector event.Domain, buffer int) (*Subscription, error) {
	return c.events.subscribe(selector, buffer)
}
```

- [ ] **Step 4: Check the safety package's actual authorization method**

`safety.Authorization` is constructed by `Authorize(endpoint string, scopes
...Scope)`. Read `safety/authorization.go` for the method that checks an
endpoint and scope. If it is not named `Permits`, use the real name; if no
such method exists, add one in this task with its own test, because `New`
must reject an authorization for a different endpoint.

- [ ] **Step 5: Run and verify it passes**

```bash
devbox run -- task test -- ./client
```

Expected: PASS. `java.Java1_8()` does not exist yet, so this task's tests stay
red until Task 10. Write the tests now, mark them skipped with
`t.Skip("needs version/java from Task 10")` on the ones that need a real
profile, and remove the skips in Task 10.

- [ ] **Step 6: Commit**

```bash
git add client/client.go client/client_test.go
git commit -m "feat(client): validate a configuration before any network work"
```

---

## Stage E — Adapters and the loop

### Task 9: The protocol 47 adapter and readiness rule

Build 47 first. It needs no configuration state, no bundle delimiter, and no
part of M4, so the whole loop can be proved before 775 exists.

**Files:**
- Create: `internal/adapter/v1_8/adapter.go`, `internal/adapter/v1_8/adapter_test.go`

**Interfaces:**
- Consumes: `version.Adapter`, `version.ReadinessRule`, `version.Batch`, `event.Collector`.
- Produces: `New(*event.Collector) version.Adapter`, `Readiness() version.ReadinessRule`, `BundleDelimiter = ""`.

- [ ] **Step 1: Write the failing test**

```go
package v1_8_test

import (
	"context"
	"errors"
	"testing"

	protocol "github.com/go-theft-craft/minecraft-protocol"
	gen "github.com/go-theft-craft/minecraft-protocol/generated/java/v1_8"

	adapter "github.com/go-theft-craft/headless-minecraft/internal/adapter/v1_8"
	"github.com/go-theft-craft/headless-minecraft/event"
	"github.com/go-theft-craft/headless-minecraft/version"
)

func clientbound(name string, value any) protocol.Packet {
	return protocol.Packet{
		State:     gen.StatePlay,
		Direction: protocol.DirectionClientbound,
		Name:      name,
		Value:     value,
	}
}

func TestKeepAliveProducesAPongedEvent(t *testing.T) {
	var c event.Collector
	a := adapter.New(&c)

	handler, ok := a.Handlers()["keep_alive"]
	if !ok {
		t.Fatal("no handler registered for keep_alive")
	}
	if err := handler.Handle(context.Background(), clientbound("keep_alive", &gen.PlayClientboundKeepAlive{})); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	events := c.Events()
	if len(events) != 1 || events[0].Name() != event.SessionKeepAlivePonged {
		t.Fatalf("got %v, want one keepalive_ponged", events)
	}
}

func TestReadinessNeedsLoginThenPosition(t *testing.T) {
	rule := adapter.Readiness()

	state, reply, err := rule.Observe(version.Batch{Packets: []protocol.Packet{
		clientbound("position", &gen.PlayClientboundPosition{X: 1, Y: 2, Z: 3}),
	}})
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if state.Ready {
		t.Fatal("rule reported ready before the login packet arrived")
	}
	if len(reply) != 0 {
		t.Fatal("rule replied to a position it had no login for")
	}

	state, _, err = rule.Observe(version.Batch{Packets: []protocol.Packet{
		clientbound("login", &gen.PlayClientboundLogin{EntityID: 42, GameMode: 1}),
	}})
	if err != nil || state.Ready {
		t.Fatalf("login alone gave ready=%v err=%v, want false/nil", state.Ready, err)
	}
	if state.EntityID != 42 {
		t.Errorf("entity ID is %d, want 42", state.EntityID)
	}

	state, reply, err = rule.Observe(version.Batch{Packets: []protocol.Packet{
		clientbound("position", &gen.PlayClientboundPosition{X: 1, Y: 2, Z: 3, Yaw: 4, Pitch: 5}),
	}})
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if !state.Ready {
		t.Fatal("rule did not report ready after login and position")
	}
	if len(reply) != 1 {
		t.Fatalf("rule sent %d packets, want exactly one position-look echo", len(reply))
	}

	echo, ok := reply[0].Value.(*gen.PlayServerboundPositionLook)
	if !ok {
		t.Fatalf("reply is %T, want *PlayServerboundPositionLook", reply[0].Value)
	}
	if echo.X != 1 || echo.Y != 2 || echo.Z != 3 || echo.Yaw != 4 || echo.Pitch != 5 {
		t.Errorf("echo does not match the placing position: %+v", echo)
	}
}

func TestReadinessRejectsARelativeSpawn(t *testing.T) {
	rule := adapter.Readiness()
	_, _, _ = rule.Observe(version.Batch{Packets: []protocol.Packet{
		clientbound("login", &gen.PlayClientboundLogin{EntityID: 1}),
	}})

	// Any non-zero flag bit marks at least one field relative.
	_, _, err := rule.Observe(version.Batch{Packets: []protocol.Packet{
		clientbound("position", &gen.PlayClientboundPosition{Flags: 0x01}),
	}})
	if !errors.Is(err, version.ErrRelativeSpawn) {
		t.Fatalf("got %v, want ErrRelativeSpawn", err)
	}
}

func TestReadinessReportsReadyOnlyOnce(t *testing.T) {
	rule := adapter.Readiness()
	_, _, _ = rule.Observe(version.Batch{Packets: []protocol.Packet{
		clientbound("login", &gen.PlayClientboundLogin{EntityID: 1}),
	}})
	_, _, _ = rule.Observe(version.Batch{Packets: []protocol.Packet{
		clientbound("position", &gen.PlayClientboundPosition{}),
	}})

	_, reply, err := rule.Observe(version.Batch{Packets: []protocol.Packet{
		clientbound("position", &gen.PlayClientboundPosition{}),
	}})
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if len(reply) != 0 {
		t.Fatal("rule echoed a second position after it was already ready")
	}
}

func TestAdapterIdentifiesItsProtocol(t *testing.T) {
	var c event.Collector
	if got := adapter.New(&c).ProtocolID(); got != "java/1.8.9" {
		t.Errorf("ProtocolID is %q, want java/1.8.9", got)
	}
}
```

- [ ] **Step 2: Run and verify failure**

```bash
devbox run -- task test -- ./internal/adapter/v1_8
```

Expected: FAIL, package does not exist.

Confirm the real generated names first:

```bash
grep -n 'type PlayClientboundKeepAlive struct' -A 3 \
  ../minecraft-protocol/generated/java/v1_8/packets.go
grep -n 'StatePlay\|packetName' ../minecraft-protocol/generated/java/v1_8/protocol.go | head
```

Use the descriptor's own packet names for the handler map keys. Do not guess
`"keep_alive"`; read what the generated descriptor registers.

- [ ] **Step 3: Implement the adapter**

`internal/adapter/v1_8/adapter.go`:

```go
// Package v1_8 translates protocol 47 packets into client events.
//
// It is internal because it is a translation table, not a contract. The
// contract is version.Adapter, and a caller selects this one through
// version/java.
package v1_8

import (
	"context"
	"time"

	protocol "github.com/go-theft-craft/minecraft-protocol"
	gen "github.com/go-theft-craft/minecraft-protocol/generated/java/v1_8"

	"github.com/go-theft-craft/headless-minecraft/event"
	"github.com/go-theft-craft/headless-minecraft/version"
)

// BundleDelimiter is empty: protocol 47 has no packet bundling, so every
// batch holds one packet.
const BundleDelimiter = ""

type adapter struct{ collector *event.Collector }

// New returns an adapter that appends to the given collector.
//
// The collector is owned by the read loop and reset per batch, so handlers
// never publish and a batch's events reach subscribers together.
func New(collector *event.Collector) version.Adapter {
	return adapter{collector: collector}
}

func (adapter) ProtocolID() string { return "java/1.8.9" }

func (a adapter) Handlers() map[string]version.Handler {
	return map[string]version.Handler{
		"keep_alive":     handlerFunc(a.keepAlive),
		"custom_payload": handlerFunc(a.customPayload),
		"kick_disconnect": handlerFunc(a.disconnect),
	}
}

// handlerFunc adapts a function to version.Handler.
type handlerFunc func(context.Context, protocol.Packet) error

func (f handlerFunc) Handle(ctx context.Context, p protocol.Packet) error { return f(ctx, p) }

func (a adapter) keepAlive(_ context.Context, p protocol.Packet) error {
	value, ok := p.Value.(*gen.PlayClientboundKeepAlive)
	if !ok {
		return nil
	}
	a.collector.Add(event.KeepAlivePonged{ID: int64(value.KeepAliveID)})

	return nil
}

func (a adapter) customPayload(_ context.Context, p protocol.Packet) error {
	value, ok := p.Value.(*gen.PlayClientboundCustomPayload)
	if !ok {
		return nil
	}
	a.collector.Add(event.CustomPayloadReceived{
		Channel: value.Channel,
		Payload: append([]byte(nil), value.Data...),
	})

	return nil
}

func (a adapter) disconnect(_ context.Context, p protocol.Packet) error {
	value, ok := p.Value.(*gen.PlayClientboundKickDisconnect)
	if !ok {
		return nil
	}
	a.collector.Add(event.Disconnected{
		Source: event.DisconnectByServer,
		Reason: value.Reason,
		State:  string(gen.StatePlay),
	})

	return nil
}

var _ = time.Duration(0)
```

Read the real field names from `packets.go` before writing each handler — the
1.8 keepalive field, the custom-payload channel and data fields, and the kick
reason are the three to confirm. Delete the `time` blank reference once
`KeepAlivePonged.Elapsed` is populated or drop that field's use here.

- [ ] **Step 4: Implement the readiness rule**

Append to `internal/adapter/v1_8/adapter.go`:

```go
// readiness implements version.ReadinessRule for protocol 47.
//
// Protocol 47 has no teleport confirmation. The server places the player with
// a clientbound position and the client acknowledges by echoing it as a
// serverbound position-look.
type readiness struct {
	seenLogin bool
	ready     bool
	state     version.ReadyState
}

// Readiness returns a fresh rule. One rule serves one connection: it carries
// per-connection progress and must not be shared.
func Readiness() version.ReadinessRule { return &readiness{} }

func (r *readiness) Observe(batch version.Batch) (version.ReadyState, []protocol.Packet, error) {
	if r.ready {
		return r.state, nil, nil
	}

	var reply []protocol.Packet
	for _, p := range batch.Packets {
		switch value := p.Value.(type) {
		case *gen.PlayClientboundLogin:
			r.seenLogin = true
			r.state.EntityID = value.EntityID
			r.state.GameMode = value.GameMode
			r.state.Dimension = dimensionName(value.Dimension)

		case *gen.PlayClientboundPosition:
			if !r.seenLogin {
				continue
			}
			if value.Flags != 0 {
				return version.ReadyState{}, nil, fmt.Errorf(
					"%w: position flags are 0x%02x", version.ErrRelativeSpawn, value.Flags)
			}

			reply = append(reply, protocol.Packet{
				State:     gen.StatePlay,
				Direction: protocol.DirectionServerbound,
				ID:        gen.PlayServerboundPositionLook{}.PacketID(),
				Value: &gen.PlayServerboundPositionLook{
					X: value.X, Y: value.Y, Z: value.Z,
					Yaw: value.Yaw, Pitch: value.Pitch, OnGround: true,
				},
			})
			r.ready = true
			r.state.Ready = true
		}
	}

	return r.state, reply, nil
}

// dimensionName maps protocol 47's numeric dimension to a stable name, so
// the Ready event carries the same shape both protocols produce.
func dimensionName(id int8) string {
	switch id {
	case -1:
		return "minecraft:the_nether"
	case 1:
		return "minecraft:the_end"
	default:
		return "minecraft:overworld"
	}
}
```

Add `"fmt"` to the imports.

- [ ] **Step 5: Run and verify it passes**

```bash
devbox run -- task test -- ./internal/adapter/v1_8
```

Expected: PASS, all five tests.

- [ ] **Step 6: Commit**

```bash
git add internal/adapter/v1_8/
git commit -m "feat(adapter): translate protocol 47 packets and readiness"
```

### Task 10: The profile constructors

**Files:**
- Create: `version/java/java.go`, `version/java/java_test.go`
- Modify: `client/client_test.go` to drop the skips added in Task 8

**Interfaces:**
- Produces: `Java1_8() version.WireProfile`, `JavaCurrent() version.WireProfile`, `Java1_8With(*event.Collector) version.WireProfile`.

- [ ] **Step 1: Write the failing test**

```go
package java_test

import (
	"testing"

	"github.com/go-theft-craft/headless-minecraft/version/java"
)

func TestJava1_8IsAValidProfile(t *testing.T) {
	p := java.Java1_8()
	if err := p.Validate(); err != nil {
		t.Fatalf("Java1_8 is not a valid profile: %v", err)
	}
	if p.Protocol.Version().Protocol != 47 {
		t.Errorf("protocol number is %d, want 47", p.Protocol.Version().Protocol)
	}
}

func TestJavaCurrentIsAValidProfile(t *testing.T) {
	p := java.JavaCurrent()
	if err := p.Validate(); err != nil {
		t.Fatalf("JavaCurrent is not a valid profile: %v", err)
	}
	if p.Protocol.Version().Protocol != 775 {
		t.Errorf("protocol number is %d, want 775", p.Protocol.Version().Protocol)
	}
}

func TestEachCallReturnsAFreshReadinessRule(t *testing.T) {
	first := java.Java1_8()
	second := java.Java1_8()

	if first.Readiness == second.Readiness {
		t.Fatal("two profiles share one readiness rule; per-connection progress would leak between clients")
	}
}
```

- [ ] **Step 2: Run and verify failure**

```bash
devbox run -- task test -- ./version/java
```

Expected: FAIL, package does not exist. `JavaCurrent` stays failing until M4.4
publishes a verified `v26_1`; leave that test in place rather than removing it.

- [ ] **Step 3: Implement**

`version/java/java.go`:

```go
// Package java assembles the built-in Java Edition profiles.
//
// It is the one package that imports the per-version adapters, so a consumer
// linking only one version does not pull in the other's generated code.
package java

import (
	protocol "github.com/go-theft-craft/minecraft-protocol"
	gen1_8 "github.com/go-theft-craft/minecraft-protocol/generated/java/v1_8"
	gen26_1 "github.com/go-theft-craft/minecraft-protocol/generated/java/v26_1"

	"github.com/go-theft-craft/headless-minecraft/event"
	adapter1_8 "github.com/go-theft-craft/headless-minecraft/internal/adapter/v1_8"
	adapter26_1 "github.com/go-theft-craft/headless-minecraft/internal/adapter/v26_1"
	"github.com/go-theft-craft/headless-minecraft/version"
)

// Java1_8 returns the protocol 47 profile with its own collector.
func Java1_8() version.WireProfile { return Java1_8With(new(event.Collector)) }

// Java1_8With returns the protocol 47 profile bound to a caller's collector.
// The client uses this form so its loop owns the collector it resets per
// batch.
func Java1_8With(collector *event.Collector) version.WireProfile {
	limits, err := protocol.NewLimits()
	if err != nil {
		// NewLimits with no options returns validated defaults, so this
		// cannot fail. Panicking here rather than returning an error keeps
		// the profile constructors total.
		panic("java: default limits are invalid: " + err.Error())
	}

	return version.WireProfile{
		ID:        "java/1.8.9",
		Protocol:  gen1_8.Protocol(),
		Adapter:   adapter1_8.New(collector),
		Limits:    limits,
		Readiness: adapter1_8.Readiness(),
	}
}

// JavaCurrent returns the current stable Java profile, protocol 775.
func JavaCurrent() version.WireProfile { return JavaCurrentWith(new(event.Collector)) }

// JavaCurrentWith returns the protocol 775 profile bound to a collector.
func JavaCurrentWith(collector *event.Collector) version.WireProfile {
	limits, err := protocol.NewLimits()
	if err != nil {
		panic("java: default limits are invalid: " + err.Error())
	}

	return version.WireProfile{
		ID:        "java/26.1",
		Protocol:  gen26_1.Protocol(),
		Adapter:   adapter26_1.New(collector),
		Limits:    limits,
		Readiness: adapter26_1.Readiness(),
	}
}

// BundleDelimiter reports the packet name that opens and closes a bundle for
// one profile, or the empty string when the protocol does not bundle.
func BundleDelimiter(id string) string {
	switch id {
	case "java/26.1":
		return adapter26_1.BundleDelimiter
	default:
		return adapter1_8.BundleDelimiter
	}
}
```

- [ ] **Step 4: Create the 775 adapter as a compiling stub**

`JavaCurrent` cannot be written without `internal/adapter/v26_1` existing.
Create it now with the same structure as Task 9's, implementing
`ProtocolID`, `Handlers` returning an empty map, `BundleDelimiter` set to the
generated bundle-delimiter packet name, and `Readiness` returning a rule that
watches for `PlayClientboundLogin` then `PlayClientboundPosition` and replies
with `PlayServerboundTeleportConfirm{TeleportID: value.TeleportID}`.

Confirm the delimiter's registered name first:

```bash
grep -n 'BundleDelimiter' ../minecraft-protocol/generated/java/v26_1/protocol.go | head
```

Task 12 fills in its handlers. Its readiness rule is written here because
Task 12's end-to-end test needs it and it is four lines different from 47's.

- [ ] **Step 5: Drop the skips from Task 8**

Remove every `t.Skip("needs version/java from Task 10")` in
`client/client_test.go`.

- [ ] **Step 6: Run and verify**

```bash
devbox run -- task test -- ./version/... ./client ./internal/...
```

Expected: PASS, except `TestJavaCurrentIsAValidProfile` if M4.4 has not landed.

- [ ] **Step 7: Commit**

```bash
git add version/java/ internal/adapter/v26_1/ client/client_test.go
git commit -m "feat(version): assemble the built-in Java profiles"
```

### Task 11: The read loop

The loop is the one owner of `Stream.Read` after login returns. It batches,
dispatches each packet in the batch through the router, feeds the readiness
rule, and publishes the collector once per batch.

**Files:**
- Create: `client/loop.go`, `client/loop_test.go`

**Interfaces:**
- Consumes: `version.Batcher`, `version.ReadinessRule`, `event.Collector`, `fanout`.
- Produces: `receiver`, `sender`, `dispatcher`, `tableDispatcher`, `newTableDispatcher(map[string]version.Handler) *tableDispatcher`, `(*Client).runLoop(...) error`.

- [ ] **Step 1: Write the failing test**

Drive the loop over a fake receiver, never a real stream.

```go
package client

import (
	"context"
	"errors"
	"io"
	"testing"

	protocol "github.com/go-theft-craft/minecraft-protocol"

	"github.com/go-theft-craft/headless-minecraft/event"
	"github.com/go-theft-craft/headless-minecraft/version"
)

// sliceReceiver replays packets and then returns io.EOF.
type sliceReceiver struct {
	packets []protocol.Packet
	at      int
}

func (r *sliceReceiver) Receive(context.Context) (protocol.Packet, error) {
	if r.at >= len(r.packets) {
		return protocol.Packet{}, io.EOF
	}
	p := r.packets[r.at]
	r.at++

	return p, nil
}

// recordingSender captures readiness replies.
type recordingSender struct{ sent []protocol.Packet }

func (s *recordingSender) Write(_ context.Context, p protocol.Packet) error {
	s.sent = append(s.sent, p)

	return nil
}

// countingReadiness reports ready on the batch containing "position" and
// asks for one reply packet.
type countingReadiness struct {
	ready bool
	calls int
}

func (r *countingReadiness) Observe(b version.Batch) (version.ReadyState, []protocol.Packet, error) {
	r.calls++
	for _, p := range b.Packets {
		if p.Name == "position" && !r.ready {
			r.ready = true

			return version.ReadyState{Ready: true, EntityID: 7}, []protocol.Packet{
				{Name: "teleport_confirm", State: "play"},
			}, nil
		}
	}

	return version.ReadyState{}, nil, nil
}

// failingReadiness always errors, standing in for a relative spawn.
type failingReadiness struct{ err error }

func (r failingReadiness) Observe(version.Batch) (version.ReadyState, []protocol.Packet, error) {
	return version.ReadyState{}, nil, r.err
}

// harness builds a loop over a fixed packet script.
type harness struct {
	client    *Client
	receiver  *sliceReceiver
	sender    *recordingSender
	batcher   *version.Batcher
	collector *event.Collector
	ready     chan version.ReadyState
}

// newHarness wires a loop with no router registrations, so every packet
// dispatches to nothing and only the batcher, readiness rule, and fan-out are
// under test. Pass an empty delimiter for an unbundled protocol.
func newHarness(t *testing.T, delimiter string, limit int, names ...string) *harness {
	t.Helper()

	packets := make([]protocol.Packet, 0, len(names))
	for _, name := range names {
		packets = append(packets, protocol.Packet{
			Name: name, State: "play", Direction: protocol.DirectionClientbound,
		})
	}

	batcher, err := version.NewBatcher(delimiter, limit)
	if err != nil {
		t.Fatalf("NewBatcher: %v", err)
	}

	return &harness{
		client:    &Client{done: make(chan struct{})},
		receiver:  &sliceReceiver{packets: packets},
		sender:    &recordingSender{},
		batcher:   batcher,
		collector: new(event.Collector),
		ready:     make(chan version.ReadyState, 1),
	}
}

// run drives the loop with no router. dispatcher is nil, which the loop must
// tolerate, because a client with no registered handlers is a valid client.
func (h *harness) run(ctx context.Context, rule version.ReadinessRule) error {
	return h.client.runLoop(
		ctx, h.receiver, h.sender, nil, h.batcher, h.collector, rule, h.ready,
	)
}

func TestBundledPacketsPublishTogether(t *testing.T) {
	// Two packets inside one bundle must reach a subscriber as one
	// uninterrupted run, with the unbundled packet that follows arriving
	// only after both.
	h := newHarness(t, "bundle", 16,
		"bundle", "spawn_entity", "entity_metadata", "bundle", "keep_alive")

	sub, err := h.client.events.subscribe(event.DomainRaw, 16)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	if err := h.run(context.Background(), &countingReadiness{}); err != nil {
		t.Fatalf("runLoop: %v", err)
	}
	_ = sub.Close()

	var names []string
	var bundled []bool
	for e := range sub.C() {
		received, ok := e.(event.PacketReceived)
		if !ok {
			continue
		}
		names = append(names, received.Name)
		bundled = append(bundled, received.Bundled)
	}

	want := []string{"spawn_entity", "entity_metadata", "keep_alive"}
	if len(names) != len(want) {
		t.Fatalf("got %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("got %v, want %v", names, want)
		}
	}
	if !bundled[0] || !bundled[1] {
		t.Error("packets inside the bundle are not marked bundled")
	}
	if bundled[2] {
		t.Error("the packet after the bundle is marked bundled")
	}
}

func TestUnterminatedBundleFailsTheLoop(t *testing.T) {
	h := newHarness(t, "bundle", 16, "bundle", "spawn_entity")

	err := h.run(context.Background(), &countingReadiness{})
	if !errors.Is(err, version.ErrBundleUnterminated) {
		t.Fatalf("got %v, want ErrBundleUnterminated", err)
	}
}

func TestOversizeBundleFailsTheLoop(t *testing.T) {
	h := newHarness(t, "bundle", 2, "bundle", "a", "b", "c")

	err := h.run(context.Background(), &countingReadiness{})
	if !errors.Is(err, version.ErrBundleTooLarge) {
		t.Fatalf("got %v, want ErrBundleTooLarge", err)
	}
}

func TestLoopReturnsNilOnCleanEOF(t *testing.T) {
	h := newHarness(t, "", 16, "keep_alive", "chat")

	if err := h.run(context.Background(), &countingReadiness{}); err != nil {
		t.Fatalf("clean EOF returned %v, want nil", err)
	}
}

func TestLoopReturnsContextErrorOnCancellation(t *testing.T) {
	h := newHarness(t, "", 16, "keep_alive")
	// Replace the receiver with one that blocks until the context ends, so
	// cancellation is observed mid-read rather than after EOF.
	h.receiver = &sliceReceiver{}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	blocking := blockingReceiver{}
	err := h.client.runLoop(
		ctx, blocking, h.sender, nil, h.batcher, h.collector, &countingReadiness{}, h.ready,
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want context.Canceled", err)
	}
}

func TestReadinessReplyIsWrittenBeforeReadyIsSignalled(t *testing.T) {
	h := newHarness(t, "", 16, "login", "position")

	sub, _ := h.client.events.subscribe(event.DomainSession, 16)
	rule := &countingReadiness{}

	if err := h.run(context.Background(), rule); err != nil {
		t.Fatalf("runLoop: %v", err)
	}
	_ = sub.Close()

	if len(h.sender.sent) != 1 || h.sender.sent[0].Name != "teleport_confirm" {
		t.Fatalf("sent %v, want exactly one teleport_confirm", h.sender.sent)
	}

	var sawReady bool
	for e := range sub.C() {
		if e.Name() == event.SessionReady {
			if !sawReady {
				sawReady = true

				continue
			}
			t.Fatal("Ready was published more than once")
		}
	}
	if !sawReady {
		t.Fatal("no Ready event was published")
	}

	select {
	case state := <-h.ready:
		if state.EntityID != 7 {
			t.Errorf("ready state carries entity %d, want 7", state.EntityID)
		}
	default:
		t.Fatal("nothing was sent on the ready channel")
	}
}

func TestReadinessErrorStopsTheLoop(t *testing.T) {
	h := newHarness(t, "", 16, "position")
	sentinel := errors.New("relative spawn")

	err := h.run(context.Background(), failingReadiness{err: sentinel})
	if !errors.Is(err, sentinel) {
		t.Fatalf("got %v, want the readiness rule's error", err)
	}
}

// blockingReceiver blocks until its context ends.
type blockingReceiver struct{}

func (blockingReceiver) Receive(ctx context.Context) (protocol.Packet, error) {
	<-ctx.Done()

	return protocol.Packet{}, ctx.Err()
}
```

- [ ] **Step 2: Run and verify failure**

```bash
devbox run -- task test -- ./client
```

Expected: FAIL, `runLoop` undefined.

- [ ] **Step 3: Implement**

`client/loop.go`:

```go
package client

import (
	"context"
	"errors"
	"fmt"
	"io"

	protocol "github.com/go-theft-craft/minecraft-protocol"

	"github.com/go-theft-craft/headless-minecraft/event"
	"github.com/go-theft-craft/headless-minecraft/version"
)

// receiver is the loop's inbound source. A stream satisfies it directly; a
// test satisfies it with a slice.
type receiver interface {
	Receive(ctx context.Context) (protocol.Packet, error)
}

// sender is the loop's outbound sink, used for readiness replies.
type sender interface {
	Write(ctx context.Context, p protocol.Packet) error
}

// dispatcher runs the handlers registered for one packet.
//
// It is an interface so the loop does not care whether handlers come from
// this package's table or from the shared router. See the plan's dependency
// note: the router cannot drive this loop unless it exports a single-packet
// Dispatch, which its approved plan does not.
type dispatcher interface {
	Dispatch(ctx context.Context, p protocol.Packet) error
}

// tableDispatcher looks handlers up by packet name.
//
// The adapter already keys its handlers on the descriptor's packet names, so
// no name-to-ID resolution is needed. An unregistered packet is not an error:
// a client that ignores most of the play state is the normal case.
type tableDispatcher struct {
	handlers map[string]version.Handler
}

func newTableDispatcher(handlers map[string]version.Handler) *tableDispatcher {
	return &tableDispatcher{handlers: handlers}
}

func (d *tableDispatcher) Dispatch(ctx context.Context, p protocol.Packet) error {
	handler, ok := d.handlers[p.Name]
	if !ok {
		return nil
	}

	return handler.Handle(ctx, p)
}

// runLoop owns inbound delivery until it returns.
//
// One goroutine reads, batches, dispatches, and publishes. Handlers run here,
// so a handler that blocks stalls the connection, including keepalive; the
// fan-out never blocks, which is what keeps that rule enforceable.
func (c *Client) runLoop(
	ctx context.Context,
	r receiver,
	w sender,
	d dispatcher,
	batcher *version.Batcher,
	collector *event.Collector,
	readiness version.ReadinessRule,
	ready chan<- version.ReadyState,
) error {
	readySent := false

	for {
		packet, err := r.Receive(ctx)
		switch {
		case errors.Is(err, io.EOF):
			if finishErr := batcher.Finish(); finishErr != nil {
				return finishErr
			}

			return nil
		case err != nil:
			return fmt.Errorf("receive: %w", err)
		}

		batch, complete, err := batcher.Accept(packet)
		if err != nil {
			return err
		}
		if !complete {
			continue
		}

		collector.Reset()

		for _, p := range batch.Packets {
			if d != nil {
				if err := d.Dispatch(ctx, p); err != nil {
					return fmt.Errorf("dispatch %s: %w", p.Name, err)
				}
			}
			collector.Add(event.PacketReceived{
				State:   string(p.State),
				Name:    p.Name,
				ID:      p.ID,
				Bundled: batch.Bundled,
			})
		}

		state, reply, err := readiness.Observe(batch)
		if err != nil {
			return err
		}
		for _, p := range reply {
			if err := w.Write(ctx, p); err != nil {
				return fmt.Errorf("write readiness reply: %w", err)
			}
			collector.Add(event.PacketSent{State: string(p.State), Name: p.Name, ID: p.ID})
		}

		// Publish before signalling ready, so a subscriber that was waiting
		// on Connect has already seen everything the placing batch produced.
		c.events.publish(collector.Events())

		if state.Ready && !readySent {
			readySent = true
			c.events.publish([]event.Event{event.Ready{
				EntityID:  state.EntityID,
				Dimension: state.Dimension,
				GameMode:  state.GameMode,
			}})
			select {
			case ready <- state:
			default:
			}
		}
	}
}
```

- [ ] **Step 4: Run and verify it passes**

```bash
devbox run -- task test -- ./client
```

Expected: PASS, all six loop tests, under `-race`.

- [ ] **Step 5: Commit**

```bash
git add client/loop.go client/loop_test.go
git commit -m "feat(client): batch, dispatch, and publish inbound packets"
```

### Task 12: Connect and Close

**Files:**
- Create: `client/connect.go`, `client/close.go`, `client/connect_test.go`, `client/internal/fixture/server.go`

**Interfaces:**
- Produces: `(*Client).Connect(ctx) error`, `(*Client).Close() error`, `(*Client).Wait() error`, `ErrConnectTimeout`, `fixture.Server`.

- [ ] **Step 1: Write the fixture server**

`client/internal/fixture/server.go` accepts one connection on a loopback
listener and replays a scripted server side: read handshake, read login start,
write login success, write play login, write position, then hold. It uses
`protocol.NewStream` with `protocol.RoleServer` and the generated descriptor,
so the fixture exercises real codecs rather than hand-built bytes.

Expose:

```go
// Script says how far the fixture server plays its part.
type Script struct {
	// ThroughReady sends the play login and a placing position. When false
	// the fixture completes login and then sends nothing, so a client
	// reaches play but is never placed.
	ThroughReady bool
	// ThenKick sends a disconnect with this reason after the client is
	// placed. Empty sends none.
	ThenKick string
	// ThenDropConn closes the transport without a disconnect packet.
	ThenDropConn bool
}

// Start listens on loopback and serves one connection. The returned stop
// function closes the listener and waits for the server goroutine.
func Start(t *testing.T, script Script) (addr string, stop func())
```

The fixture defaults to protocol 47. Task 14 adds a `Profile` field to `Script`
for the 775 lane.

- [ ] **Step 2: Write the failing test**

```go
package client_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/go-theft-craft/headless-minecraft/auth"
	"github.com/go-theft-craft/headless-minecraft/client"
	"github.com/go-theft-craft/headless-minecraft/client/internal/fixture"
	"github.com/go-theft-craft/headless-minecraft/event"
	"github.com/go-theft-craft/headless-minecraft/safety"
	"github.com/go-theft-craft/headless-minecraft/version/java"
)

// connectTo builds a client pointed at addr with a short connect timeout.
func connectTo(t *testing.T, addr string) *client.Client {
	t.Helper()

	provider, err := auth.Offline("tester")
	if err != nil {
		t.Fatalf("Offline: %v", err)
	}
	authz, err := safety.Authorize(addr, safety.ScopeObserve)
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}

	bot, err := client.New(
		client.WithAddress(addr),
		client.WithAuth(provider),
		client.WithVersion(java.Java1_8()),
		client.WithAuthorization(authz),
		client.WithConnectTimeout(2*time.Second),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	return bot
}

// drain collects every event a subscription delivers until it closes.
func drain(sub *client.Subscription) []event.Event {
	var events []event.Event
	for e := range sub.C() {
		events = append(events, e)
	}

	return events
}

// count returns how many events carry the given name.
func count(events []event.Event, name event.EventName) int {
	n := 0
	for _, e := range events {
		if e.Name() == name {
			n++
		}
	}

	return n
}

func TestConnectReachesReady(t *testing.T) {
	addr, stop := fixture.Start(t, fixture.Script{ThroughReady: true})
	defer stop()

	bot := connectTo(t, addr)
	sub, err := bot.Subscribe(event.DomainSession, 64)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	if err := bot.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if err := bot.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	events := drain(sub)
	if got := count(events, event.SessionReady); got != 1 {
		t.Errorf("got %d Ready events, want 1", got)
	}
	if got := count(events, event.SessionConnecting); got != 1 {
		t.Errorf("got %d Connecting events, want 1", got)
	}
	if got := count(events, event.SessionAuthenticated); got != 1 {
		t.Errorf("got %d Authenticated events, want 1", got)
	}
}

func TestConnectTimesOutWhenTheServerNeverPlaces(t *testing.T) {
	// The fixture completes login and then sends nothing, so the client
	// reaches play but is never placed.
	addr, stop := fixture.Start(t, fixture.Script{ThroughReady: false})
	defer stop()

	provider, _ := auth.Offline("tester")
	authz, _ := safety.Authorize(addr, safety.ScopeObserve)
	bot, err := client.New(
		client.WithAddress(addr),
		client.WithAuth(provider),
		client.WithVersion(java.Java1_8()),
		client.WithAuthorization(authz),
		client.WithConnectTimeout(200*time.Millisecond),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = bot.Close() }()

	start := time.Now()
	err = bot.Connect(context.Background())
	if !errors.Is(err, client.ErrConnectTimeout) {
		t.Fatalf("got %v, want ErrConnectTimeout", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("Connect took %v, well past its 200ms deadline", elapsed)
	}
}

func TestConnectReportsTheStateItReachedOnTimeout(t *testing.T) {
	addr, stop := fixture.Start(t, fixture.Script{ThroughReady: false})
	defer stop()

	provider, _ := auth.Offline("tester")
	authz, _ := safety.Authorize(addr, safety.ScopeObserve)
	bot, _ := client.New(
		client.WithAddress(addr),
		client.WithAuth(provider),
		client.WithVersion(java.Java1_8()),
		client.WithAuthorization(authz),
		client.WithConnectTimeout(200*time.Millisecond),
	)
	defer func() { _ = bot.Close() }()

	err := bot.Connect(context.Background())
	// "stuck in configuration" and "never placed" must be distinguishable
	// from the message alone, because that is the whole point of naming the
	// state a timeout reached.
	if err == nil || !strings.Contains(err.Error(), "play") {
		t.Fatalf("timeout error %v does not name the state it reached", err)
	}
}

func TestConnectFailsOnARefusedDial(t *testing.T) {
	// Port 1 on loopback refuses immediately on every supported platform.
	bot := connectTo(t, "127.0.0.1:1")
	defer func() { _ = bot.Close() }()

	if err := bot.Connect(context.Background()); err == nil {
		t.Fatal("Connect succeeded against a refused address")
	}
}

func TestConnectTwiceIsAnError(t *testing.T) {
	addr, stop := fixture.Start(t, fixture.Script{ThroughReady: true})
	defer stop()

	bot := connectTo(t, addr)
	defer func() { _ = bot.Close() }()

	if err := bot.Connect(context.Background()); err != nil {
		t.Fatalf("first Connect: %v", err)
	}
	if err := bot.Connect(context.Background()); err == nil {
		t.Fatal("second Connect succeeded; a client owns one connection")
	}
}

func TestCloseIsIdempotent(t *testing.T) {
	addr, stop := fixture.Start(t, fixture.Script{ThroughReady: true})
	defer stop()

	bot := connectTo(t, addr)
	if err := bot.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	if err := bot.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := bot.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestCloseEmitsClosedExactlyOnce(t *testing.T) {
	addr, stop := fixture.Start(t, fixture.Script{ThroughReady: true})
	defer stop()

	bot := connectTo(t, addr)
	sub, _ := bot.Subscribe(event.DomainSession, 64)

	_ = bot.Connect(context.Background())
	_ = bot.Close()
	_ = bot.Close()

	if got := count(drain(sub), event.SessionClosed); got != 1 {
		t.Errorf("got %d Closed events, want exactly 1", got)
	}
}

func TestDisconnectPacketProducesDisconnected(t *testing.T) {
	addr, stop := fixture.Start(t, fixture.Script{
		ThroughReady: true,
		ThenKick:     "server closing",
	})
	defer stop()

	bot := connectTo(t, addr)
	sub, _ := bot.Subscribe(event.DomainSession, 64)

	if err := bot.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	_ = bot.Wait()
	_ = bot.Close()

	var found bool
	for _, e := range drain(sub) {
		d, ok := e.(event.Disconnected)
		if !ok {
			continue
		}
		found = true
		if d.Source != event.DisconnectByServer {
			t.Errorf("source is %q, want server", d.Source)
		}
		if d.Reason != "server closing" {
			t.Errorf("reason is %q, want the kick text", d.Reason)
		}
	}
	if !found {
		t.Fatal("no Disconnected event was published")
	}
}

func TestTransportLossProducesDisconnectedWithTransportSource(t *testing.T) {
	addr, stop := fixture.Start(t, fixture.Script{
		ThroughReady: true,
		ThenDropConn: true,
	})
	defer stop()

	bot := connectTo(t, addr)
	sub, _ := bot.Subscribe(event.DomainSession, 64)

	if err := bot.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	_ = bot.Wait()
	_ = bot.Close()

	var found bool
	for _, e := range drain(sub) {
		if d, ok := e.(event.Disconnected); ok && d.Source == event.DisconnectByTransport {
			found = true
		}
	}
	if !found {
		t.Fatal("a dropped connection did not produce a transport disconnect")
	}
}
```

Add `"strings"` to the imports. `fixture.Script` is the struct Step 1 defines:
`ThroughReady bool`, `ThenKick string`, `ThenDropConn bool`.

- [ ] **Step 3: Run and verify failure**

```bash
devbox run -- task test -- ./client/...
```

- [ ] **Step 4: Implement Connect**

`Connect` performs, in order: publish `Connecting`; call the auth provider and
publish `Authenticated`; dial; build the session with
`profile.Protocol.NewSession(protocol.RoleClient, profile.Limits)`; build and
start the stream; write the handshake with `NextState` 2; run
`login.NewNegotiator(identity.Authenticator).Negotiate(ctx, stream)`; build the
dispatcher with `newTableDispatcher(profile.Adapter.Handlers())`; build the
batcher from `java.BundleDelimiter(profile.ID)` and the client's bundle limit;
start `runLoop` in a goroutine; and wait on the ready channel or the connect
deadline.

Publish `StateChanged` from the stream's transition observations rather than
inferring them, so a play-to-configuration return in 775 is reported.

- [ ] **Step 5: Implement Close**

`Close` cancels the loop context, calls `Stream.Shutdown` with a reason, waits
for the loop goroutine, publishes `Closed`, and closes every subscription. It
is idempotent through the `closed` flag under `c.mu` and records its first
error in `closeErr`. `Wait` blocks on `c.done` and returns that error.

Classify the ending: a disconnect packet produces `Disconnected` with
`DisconnectByServer` and the reason; a transport error with no disconnect
packet produces `DisconnectByTransport`.

- [ ] **Step 6: Run and verify it passes**

```bash
devbox run -- task test -- ./client/...
```

Expected: PASS, all nine tests, under `-race`.

- [ ] **Step 7: Commit**

```bash
git add client/connect.go client/close.go client/connect_test.go client/internal/
git commit -m "feat(client): connect to ready and close cleanly"
```

### Task 13: The protocol 775 adapter handlers

Fill in the adapter Task 10 stubbed. This is the task M4 gates.

**Files:**
- Modify: `internal/adapter/v26_1/adapter.go`
- Create: `internal/adapter/v26_1/adapter_test.go`

- [ ] **Step 1: Write the failing test**

Copy Task 9's five tests and retarget them at the 775 generated types, then add
these four:

```go
func TestTeleportConfirmCarriesTheTeleportID(t *testing.T) {
	rule := adapter.Readiness()
	_, _, _ = rule.Observe(version.Batch{Packets: []protocol.Packet{
		clientbound("login", &gen.PlayClientboundLogin{EntityID: 3}),
	}})

	state, reply, err := rule.Observe(version.Batch{Packets: []protocol.Packet{
		clientbound("position", &gen.PlayClientboundPosition{TeleportID: 91}),
	}})
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if !state.Ready {
		t.Fatal("rule did not report ready after login and position")
	}
	if len(reply) != 1 {
		t.Fatalf("rule sent %d packets, want exactly one teleport confirm", len(reply))
	}

	confirm, ok := reply[0].Value.(*gen.PlayServerboundTeleportConfirm)
	if !ok {
		t.Fatalf("reply is %T, want *PlayServerboundTeleportConfirm", reply[0].Value)
	}
	if confirm.TeleportID != 91 {
		t.Errorf("confirm carries teleport ID %d, want 91", confirm.TeleportID)
	}
}

func TestConfigurationDisconnectIsReported(t *testing.T) {
	var c event.Collector
	a := adapter.New(&c)

	handler, ok := a.Handlers()[configurationDisconnectName]
	if !ok {
		t.Fatalf("no handler registered for %q", configurationDisconnectName)
	}

	p := protocol.Packet{
		State:     gen.StateConfiguration,
		Direction: protocol.DirectionClientbound,
		Name:      configurationDisconnectName,
		Value:     &gen.ConfigurationClientboundDisconnect{},
	}
	if err := handler.Handle(context.Background(), p); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	events := c.Events()
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	d, ok := events[0].(event.Disconnected)
	if !ok {
		t.Fatalf("got %T, want event.Disconnected", events[0])
	}
	if d.State != string(gen.StateConfiguration) {
		t.Errorf("disconnect reports state %q, want configuration", d.State)
	}
}

func TestBundleDelimiterNamesTheGeneratedPacket(t *testing.T) {
	if adapter.BundleDelimiter == "" {
		t.Fatal("protocol 775 bundles packets; the delimiter must not be empty")
	}

	// The name must be the one the descriptor registers, or the batcher
	// never matches it and every bundle silently becomes loose packets.
	var c event.Collector
	if _, registered := adapter.New(&c).Handlers()[adapter.BundleDelimiter]; registered {
		t.Error("the delimiter has a handler; it is a framing marker, not an event source")
	}
}

func TestRegistryDataInConfigurationIsNotASessionEvent(t *testing.T) {
	var c event.Collector
	a := adapter.New(&c)

	// registry.data_received is M7's to implement. This milestone must not
	// emit a name for which it has defined no struct.
	if _, registered := a.Handlers()[registryDataName]; registered {
		t.Fatal("M6.3 registered a handler for registry data, which is M7's")
	}
}
```

Declare `configurationDisconnectName` and `registryDataName` as test constants
holding the names the 775 descriptor registers. Read them rather than guessing:

```bash
grep -n 'ConfigurationClientboundDisconnect\|ConfigurationClientboundRegistryData' \
  ../minecraft-protocol/generated/java/v26_1/protocol.go | head

- [ ] **Step 2: Run and verify failure**

```bash
devbox run -- task test -- ./internal/adapter/v26_1
```

- [ ] **Step 3: Implement the session-domain handlers**

Register handlers for the 775 packets that map to the fourteen session events:
`keep_alive` and its configuration twin, `custom_payload` in both states,
`kick_disconnect` and `ConfigurationClientboundDisconnect`, `transfer`,
`add_resource_pack`, `remove_resource_pack`, `server_data`, `server_links`,
`feature_flags`, `custom_report_details`, `low_disk_space_warning`,
`cookie_request`, and `store_cookie`.

Read each packet's real field names from
`../minecraft-protocol/generated/java/v26_1/packets.go` before writing its
handler.

- [ ] **Step 4: Run and verify it passes**

```bash
devbox run -- task test -- ./internal/adapter/v26_1
```

- [ ] **Step 5: Commit**

```bash
git add internal/adapter/v26_1/
git commit -m "feat(adapter): translate protocol 775 session packets"
```

---

## Stage F — Gate

### Task 14: End-to-end, documentation, and the release gate

**Files:**
- Create: `client/e2e_test.go`
- Modify: `Taskfile.yml`, `README.md`, `CHANGELOG.md`, `ROADMAP.md`, `MASTER_PLAN.md`

- [ ] **Step 1: Add the end-to-end lane**

`client/e2e_test.go`, behind `testing.Short()` skipping, reaches ready against
the fixture server on both profiles and asserts:

- exactly one `Ready` event;
- the 775 run sends exactly one `TeleportConfirm` and the 47 run exactly one
  `PositionLook`;
- a bundle in the 775 script publishes its events with no other batch's events
  between them;
- `Close` produces exactly one `Closed`.

Add to `Taskfile.yml`:

```yaml
  test:e2e:
    desc: Run the end-to-end client tests against the in-process fixture server
    deps: [deps]
    cmds:
      - go test -race -run 'TestEndToEnd' ./client/...
```

- [ ] **Step 2: Document**

README gains a worked example: construct a client with an offline provider and
`java.JavaCurrent()`, connect, subscribe to the session domain, print events,
and close. State plainly that the library never reconnects, that a slow
subscriber is dropped rather than blocking, and that M7 adds observed world
state.

CHANGELOG records the new packages. ROADMAP marks M6.3.

- [ ] **Step 3: Update the milestone record**

In `MASTER_PLAN.md`, mark M6.3's two checkboxes complete and record anything
found while implementing that affects M6.4 or M7 — in particular, whether the
bundle limit default of 4096 was ever approached, and whether any 775 login
produced a frame larger than the default 2 MiB, which M4.4 also measures.

- [ ] **Step 4: Run the release gate**

```bash
devbox run -- task verify
devbox run -- task test:e2e
```

- [ ] **Step 5: Inspect final scope**

```bash
git status --short
git diff --check
grep -c replace go.mod          # expect 0
devbox run -- go list -deps ./... | grep -c '^github.com/' # only the two modules
```

Confirm: `client` imports no generated package directly; `internal/adapter/*`
are the only packages that do, apart from `version/java`; no handler performs
I/O; no fixture contains credentials.

- [ ] **Step 6: Commit**

```bash
git add .
git commit -m "docs: record the headless connection milestone"
```

---

## Self-review notes

Three things a reader should know before starting:

- **The taxonomy is 71 named events plus 2 raw deliveries, not the 73 named
  events the design states.** `PacketReceived` and `PacketSent` report
  `DomainRaw` and are deliberately absent from the domain table, because raw
  delivery is a selector a subscriber opts into rather than a taxonomy entry.
  Correct the design's count when Task 2 lands.
- **Task 8's tests are written against `java.Java1_8()`, which Task 10
  creates.** The plan handles this with explicit skips added in Task 8 and
  removed in Task 10 rather than reordering, because validating a configuration
  is worth its own reviewable task ahead of profile assembly.
- **The design says the loop dispatches through M5's router; this plan does
  not.** M5's approved plan produces no single-packet `Dispatch`, and `Run`
  cannot drive a loop that batches. Task 11 defines a `dispatcher` interface
  and a fifteen-line `tableDispatcher`, so M6.3 carries no cross-repository
  dependency and can switch to the router later with a one-line change. The
  dependency section explains the trade. If the router is wanted instead,
  that is a decision to make before Task 11, and it requires adding `Dispatch`
  to M5 Task 2.

The 775 halves of Tasks 5, 10, 13, and 14 stay red until M4 lands. That is
intended: a stubbed-green 775 lane would hide exactly the codec and login
problems those tasks exist to find.
