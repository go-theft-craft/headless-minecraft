# Headless connection design

- Status: Draft for review
- Date: 2026-08-15
- Repository: `headless-minecraft`
- Milestone: M6.3

## Context

`headless-minecraft` is 182 lines of Go across five files: `safety/` and
`version/`. There is no client, no session, no authentication, and no world
state. Everything the 2026-08-13 headless design describes is still ahead.

That design is the wrong document to build from. It was written before M1
through M5 existed and it has drifted:

- It specifies `Protocol` as `NewCodec(role) Codec` plus `Data() data.Set`. The
  shipped interface is `NewSession(Role, Limits) (Session, error)` and has no
  `Data()`.
- Its `Profile` struct carries physics, collision, inventory semantics,
  interaction ordering, and a capability engine — every one of which is M8 or M9
  work. The repository already holds the honest subset:
  `version.WireProfile{ID, Protocol, Adapter, Limits}`, whose own comment says
  later components extend it.
- It describes a legacy-protocol package inside the proxy that does not exist.

This design supersedes that one for the connection layer. The 2026-08-13
document remains the long-range statement of intent for gameplay components.

## Where M6.3 sits

M6 as written in `MASTER_PLAN.md` is three unrelated workstreams in three
repositories. It is subdivided:

| Sub-project | Repository | What it is |
| --- | --- | --- |
| M6.1 | `server` | Replace the remaining play packet structs with generated types and delete the server's last codegen |
| M6.2 | `proxy` | Remove a dead dependency. Not a design problem; one task |
| M6.3 | `headless-minecraft` | **This design.** Connect, authenticate offline, own the packet loop, define the event taxonomy |
| M6.4 | `headless-minecraft` | Microsoft device-code authentication |
| M7 | `headless-minecraft` | Observed world state: reducers, snapshots, and the rest of the taxonomy |

M6.1 and M6.2 are independent of each other and of M6.3. Only M6.3 leads into
M7.

M6.2 is small because the proxy turned out to share nothing with this work. It
imports nothing from `minecraft-protocol` and no proxy Go file imports `server`.
What it has is `vendor/github.com/go-theft-craft/server/pkg/protocol` with zero
importers, held by `replace => ../server`, for a package M3 already deleted from
`server`. The proxy contains no VarInt or Java-wire code at all; it speaks a
different protocol. The task is to drop the require, the replace, and the
vendored tree, and confirm the build stays green.

**Done.** The build graph reached the dead package through a stale vendored copy
of the private research module rather than through any proxy source file, and
re-vendoring dropped it; that needed the toolchain pin moved to 1.26.6 first.
`MASTER_PLAN.md`'s M6.2 section records the rest.

## Goals

- A `client` package that connects to a Java server, completes login, enters
  play, and reaches a defined ready state.
- Offline authentication, behind a provider seam that M6.4 fills.
- One owner of `Stream.Read` after login, dispatching through M5's router.
- Packet batches that respect 775's bundle delimiters, so M7's reducers apply a
  bundle atomically.
- A complete event taxonomy for seven domains, of which M6.3 implements the
  session domain and raw-packet delivery.
- Bounded subscriptions that cannot block the network loops.

## Non-goals

- World state, reducers, and snapshots. That is M7, and this design defines the
  seam it plugs into.
- Any gameplay action. Movement, digging, building, inventory, and crafting are
  M9.
- Physics, collision, capabilities, and container drivers. M8 and M9.
- Microsoft authentication. M6.4.
- Reconnection. The library never reconnects; applications own retry policy,
  because reconnecting can repeat actions.
- Any change to `minecraft-protocol`. M6.3 found one it needs —
  `router.Dispatch`, in Decision 1 — and records it as a finding for M5 rather
  than making the change here.

## Decision 1: the client owns one read loop, driven by M5's router

`login.Negotiator` owns inbound delivery for the duration of the login
sequence and documents that a concurrent reader would steal packets it needs.
When it returns, the stream is in play and the client takes over.

The client runs M5's router over the stream rather than writing a second
dispatch table:

```go
r, err := router.New(profile.Protocol)
for name, handler := range adapter.Handlers() {
	r.Handle(protocolState, protocol.DirectionClientbound, name, handler)
}
```

M5 built dispatch keyed on `(State, Direction, ID)` with registration by packet
name resolved through the descriptor, ordered handlers, and errors that
terminate the run loop. That is what this adapter needs and it is already
tested. Duplicating it here would mean a second implementation of the same
thing, diverging.

**The client does not call `router.Run`.** `Run` owns its own loop over a
`Receiver`, one packet at a time, and Decision 2 needs the loop to group packets
into batches before anything is applied. The client therefore owns the loop and
dispatches each packet in a batch itself.

That needs a method M5 does not currently export. M5's router produces `Handle`,
`HandleID`, `Fallback`, and `Run`; dispatch is internal to `Run`. **Finding for
M5:** the router should export the single-packet form its own run loop already
uses:

```go
// Dispatch runs the handlers registered for one packet. Run is a loop over
// Receive and Dispatch, and a caller that owns its own loop uses Dispatch.
func (r *Router) Dispatch(ctx context.Context, p protocol.Packet) error
```

This is one exported method over existing behavior, not new machinery, and it
belongs in M5 Task 2 rather than here. Without it M6.3 either reimplements
dispatch or gives up batching, and both were rejected above.

This makes M6.3 depend on M5.1, which is already unblocked and independent of
M4.

Exactly one goroutine reads and exactly one writes, as M1 requires. Handlers run
on the read goroutine, so a handler that blocks stalls the connection. Handlers
therefore do no work beyond producing events and handing them to the
subscription fan-out, which never blocks.

## Decision 2: the loop emits batches, not packets

Protocol 775 has `PlayClientboundBundleDelimiter`. Packets between two
delimiters must be applied atomically: an entity spawn and its metadata arrive
as one unit, and applying them separately makes the entity briefly observable
with default metadata. Protocol 47 has no equivalent.

`MASTER_PLAN` says M7's reducers "apply packets in wire order". That is
necessary and not sufficient for 775.

The read loop accumulates between delimiters and hands the adapter a batch:

```go
// Batch is the unit the read loop delivers. A batch is applied atomically:
// M7 bumps the snapshot revision once per batch, never once per packet.
type Batch struct {
	Packets []protocol.Packet
	Bundled bool
}
```

Under protocol 47 every batch holds one packet and `Bundled` is false, so the
interface is version-neutral and 47 needs no special case. Under 775 a bundle
becomes one batch of N.

M6.3 owns this because M6.3 owns the loop. Leaving it to M7 would mean M6.3
defines an event surface that can emit a half-applied bundle, and M7 has to
suppress events it already emitted.

A bundle that never closes is bounded: an unterminated bundle exceeding the
batch limit is a protocol error that terminates the session, rather than an
unbounded buffer.

## Decision 3: readiness is an adapter question, not a client constant

`Connect` returns when the server will accept action packets. Under 775 that is
after the play `Login` packet arrives and the client has answered the server's
first `Position` with `PlayServerboundTeleportConfirm`:

```text
login.Negotiate returns   → state = play
  ↓ PlayClientboundLogin          entity ID, dimension, game mode
  ↓ ...registry and chunk traffic
  ↓ PlayClientboundPosition       the server places the player
  ↑ PlayServerboundTeleportConfirm
  ─── Connect returns ───
```

Anything sent before that point is legitimately ignored or kicks.

**Protocol 47 has no `TeleportConfirm`.** Its serverbound play set is
`KeepAlive`, `Position`, `PositionLook`, `Settings` and the rest; the
acknowledgement is a `PositionLook` echo. Its clientbound play set is 74
packets against 775's 141. So the ready condition is version-specific and
belongs to the adapter:

```go
// Ready reports whether the observed sequence has reached the point where
// the server accepts action packets, and returns any packet the client must
// send to get there.
type ReadinessRule interface {
	Observe(Batch) (done bool, reply []protocol.Packet)
}
```

Hardcoding the 775 sequence in `client` would make the client wrong for the
version the server currently runs on.

`Connect` takes its own timeout, because it waits on a packet the server sends
at its own pace. A timeout produces a typed error naming the last state reached,
so "stuck in configuration" and "never placed" are distinguishable.

## Decision 4: events describe state changes, not packets

An event announces what changed, not which packet arrived, and carries the
snapshot revision that produced it. Four different packets move an entity —
`EntityTeleport`, `RelEntityMove`, `EntityMoveLook`, `SyncEntityPosition` — and
they produce one `EntityMoved`.

The alternative, one typed event per packet, was rejected: it makes the taxonomy
version-specific. A 47 subscriber and a 775 subscriber would share almost no
event names, and every consumer would write the normalization the library exists
to provide.

Raw packets remain available. A subscriber can select normalized events, raw
`protocol.Packet` values, or both, so a packet no event covers is never
unreachable.

## Decision 5: the taxonomy

Seven domains are normalized. Debug and test packets — `Debug*`, `DebugSample`,
`GameTestHighlightPos`, `TestInstanceBlockStatus` — are reachable as raw packets
only; a vanilla server never sends them.

**M6.3 implements the session domain and raw delivery. M7 implements the rest.**
The names are fixed now so M7 extends the surface rather than reshaping it.

### Session — M6.3

| Event | Fed by |
| --- | --- |
| `Connecting` | before dial |
| `Authenticated` | the auth provider returning |
| `StateChanged` | session transitions, either direction |
| `Ready` | Decision 3's readiness rule |
| `Disconnected` | `KickDisconnect`, `ConfigurationClientboundDisconnect`, transport loss |
| `Closed` | `Close` completing |
| `KeepAlivePonged` | `KeepAlive` in play or configuration |
| `ServerTransferRequested` | `Transfer` |
| `ResourcePackOffered`, `ResourcePackRevoked` | `AddResourcePack`, `RemoveResourcePack` |
| `ServerMetadataChanged` | `ServerData`, `ServerLinks`, `FeatureFlags`, `CustomReportDetails`, `LowDiskSpaceWarning` |
| `CookieRequested`, `CookieStored` | `CookieRequest`, `StoreCookie` |
| `CustomPayloadReceived` | `CustomPayload`, both states |
| `PacketReceived`, `PacketSent` | every packet, opt-in |

### Player — M7

`PlayerSpawned`, `PlayerMoved`, `PlayerHealthChanged`, `PlayerExperienceChanged`,
`PlayerAbilitiesChanged`, `PlayerGameModeChanged`, `PlayerRespawned`,
`HeldSlotChanged`, `PlayerEffectsChanged`, `PlayerCooldownChanged`.

Fed by `Login`, `Position`, `PlayerRotation`, `UpdateHealth`, `Experience`,
`Abilities`, `GameStateChange`, `Respawn`, `HeldItemSlot`, `SetCooldown`,
`Camera`.

### World — M7

`ChunkLoaded`, `ChunkUnloaded`, `BlocksChanged`, `BlockEntityChanged`,
`LightChanged`, `WorldTimeChanged`, `WorldBorderChanged`, `WeatherChanged`,
`DifficultyChanged`, `ExplosionOccurred`, `WorldEventOccurred`,
`SimulationSettingsChanged`.

Fed by `MapChunk`, `UnloadChunk`, `ChunkBatchStart`, `ChunkBatchFinished`,
`ChunkBiomes`, `UpdateLight`, `BlockChange`, `MultiBlockChange`, `BlockAction`,
`TileEntityData`, `UpdateTime`, the six `WorldBorder*` packets, `Difficulty`,
`GameRuleValues`, `Explosion`, `WorldEvent`, `WorldParticles`,
`SimulationDistance`, `UpdateViewDistance`, `UpdateViewPosition`,
`SetTickingState`, `StepTick`.

`BlocksChanged` carries a position set rather than a single position, so
`BlockChange` and `MultiBlockChange` produce the same event shape.

### Entities — M7

`EntitySpawned`, `EntityRemoved`, `EntityMoved`, `EntityMetadataChanged`,
`EntityEquipmentChanged`, `EntityAttributesChanged`, `EntityEffectsChanged`,
`EntityVelocityChanged`, `EntityPassengersChanged`, `EntityDamaged`,
`EntityAnimated`, `ItemCollected`.

Fed by `SpawnEntity`, `EntityDestroy`, the four movement packets,
`EntityHeadRotation`, `EntityMetadata`, `EntityEquipment`,
`EntityUpdateAttributes`, `EntityEffect`, `RemoveEntityEffect`,
`EntityVelocity`, `AttachEntity`, `SetPassengers`, `DamageEvent`,
`HurtAnimation`, `EntityStatus`, `Animation`, `Collect`, `MoveMinecart`,
`VehicleMove`.

`EntityMoved` carries position and rotation together, both optional, because the
four movement packets each supply a different subset.

### Containers — M7

`ContainerOpened`, `ContainerClosed`, `ContainerSlotsChanged`,
`CursorItemChanged`, `RecipesChanged`, `TradeListChanged`,
`CraftResponseReceived`.

Fed by `OpenWindow`, `CloseWindow`, `WindowItems`, `SetSlot`, `SetCursorItem`,
`SetPlayerInventory`, `CraftProgressBar`, `CraftRecipeResponse`, `TradeList`,
`OpenHorseWindow`, `OpenBook`, `OpenSignEntity`, `DeclareRecipes`,
`RecipeBookAdd`, `RecipeBookRemove`, `RecipeBookSettings`.

The container state always records what the server actually opened — container
ID, namespaced menu type, title, state ID, raw slots. The client never predicts
which menu a block opens.

### Registry — M7

`RegistryDataReceived`, `TagsReceived`, `CommandsReceived`,
`PlayerListChanged`.

Fed by `ConfigurationClientboundRegistryData`, `Tags`, `DeclareCommands`,
`PlayerInfo`, `PlayerRemove`.

Session registry data overrides the matching generated registry for that
connection. Static generated data stays available for lookups that do not depend
on server configuration.

### Chat and UI — M7

`ChatReceived`, `ChatRemoved`, `TitleChanged`, `ActionBarChanged`,
`BossBarChanged`, `ScoreboardChanged`, `TeamsChanged`, `AdvancementsChanged`,
`SoundPlayed`, `StatisticsReceived`, `DialogShown`, `TabCompleted`.

Fed by `PlayerChat`, `SystemChat`, `ProfilelessChat`, `HideMessage`,
`ChatSuggestions`, `TabComplete`, `ActionBar`, the four `SetTitle*` packets,
`ClearTitles`, `BossBar`, `PlayerlistHeader`, the three `Scoreboard*` packets,
`ResetScore`, `Teams`, `Advancements`, `SelectAdvancementTab`, `Statistics`,
`SoundEffect`, `EntitySoundEffect`, `StopSound`, `ShowDialog`, `ClearDialog`.

`ChatReceived` carries a kind discriminating player, system, and profileless
chat, rather than three events, because a consumer that wants chat wants all
three.

## Decision 6: subscriptions are bounded and cannot block the loop

```go
type Selector uint

const (
	Lifecycle Selector = 1 << iota
	Player
	World
	Entities
	Containers
	Registry
	Chat
	RawPackets
)

func (c *Client) Subscribe(sel Selector, buffer int) (*Subscription, error)
func (s *Subscription) C() <-chan Event
func (s *Subscription) Err() error
func (s *Subscription) Close() error
```

A subscription has a fixed buffer chosen by the caller. A subscriber that falls
behind receives a typed overflow error through `Err`, and its channel closes. It
does not block the read loop, does not block other subscribers, and cannot delay
keepalive handling.

Dropping the slow subscriber rather than the event is deliberate and is the
opposite of M5's history ring, which drops data and keeps running. A ring is a
debugger's tool where forgetting is fine. A subscriber missing events silently
would make its view of the world wrong without saying so.

Event payloads are immutable from the caller's perspective. The client never
invokes application callbacks from the network loops; applications consume
subscriptions in their own goroutines.

## Decision 7: the profile stays small, and the adapter is per version

`version.WireProfile` already exists and is the right size. M6.3 extends it by
exactly what this design needs:

```go
type WireProfile struct {
	ID        string
	Protocol  protocol.Protocol
	Adapter   Adapter
	Limits    protocol.Limits
	Readiness ReadinessRule
}
```

`Adapter` today declares only `ProtocolID()`. It grows the handler set and the
batch translation:

```go
type Adapter interface {
	ProtocolID() string
	// Handlers are registered with the router by packet name. Each appends
	// to the batch-scoped collector it is given; none publishes directly,
	// because a batch's events are published together or not at all.
	Handlers() map[string]middleware.Handler
}
```

An earlier draft of this design also gave the adapter a `Translate(Batch)
[]Event`. That is redundant and contradictory: dispatch happens either through
the router's handler table or through a batch translation function, not both.
Handlers are the mechanism; the batch is the transaction they run inside.

There is one adapter per generated version, because the packet types differ:
`v1_8` has 74 clientbound play packets and `v26_1` has 141, sharing few
structures. The adapters live in `internal/adapter/v1_8` and
`internal/adapter/v26_1`, unexported, because they are translation tables rather
than public contract.

Nothing else from the 2026-08-13 `Profile` is added. Physics, collision,
inventory semantics, ordering, and capabilities arrive when the milestones that
own them arrive, and the struct grows then.

`client.New` validates the profile before any network or authentication work,
as `WireProfile.Validate` already does.

## Decision 8: authentication is a seam M6.3 fills only for offline

```go
type Provider interface {
	Authenticate(ctx context.Context) (login.Profile, error)
}
```

M6.3 ships one implementation, wrapping `login.Offline` from
`minecraft-protocol`, which already derives the offline UUID byte-identically to
the server's own.

M6.4 adds the Microsoft device-code provider: the MSA, Xbox Live, XSTS, and
Minecraft-services token chain, a token store interface, and a device-code
callback exposing the verification URL and user code without printing them. It
is four HTTP exchanges plus refresh and storage, it shares nothing with the
packet loop, and it blocks nothing in M7 — offline mode covers every test server
and the whole world-state effort.

Secrets and access tokens never appear in errors, logs, events, or captures.

## Dependencies

| Needs | For |
| --- | --- |
| M5.1 (router, middleware) | Decision 1's dispatch |
| M4 Task 9 (modern login sequence) | Login through configuration into play on 775 |
| M4.4 (`v26_1.Protocol()` verified) | A profile to connect with |

M4 Task 9 is the one to watch. `login.Negotiator` is protocol 47 only today and
says so in its package documentation; M4 generalizes it with login roles and
`WithTerminalState`, keeping the `Negotiate` signature. M6.3 consumes that and
adds nothing to it.

The repository also has to move to the toolchain M3 established: Go 1.26.6 with
the `openserbia/go-flake` pins, `devbox.json` setting `GOROOT` explicitly, and
`minecraft-protocol` consumed as a released module rather than the
pseudo-version currently in `go.mod`. M3 recorded that a shell entered from a
sibling repository leaks its `GOROOT` and every build fails on a toolchain
mismatch.

## Testing

- Adapter tests translate generated packets into events, table-driven per
  domain, asserting that the four movement packets produce one `EntityMoved`
  shape.
- Batch tests assert that 47 always produces single-packet batches, that a 775
  bundle produces one batch of N, that a nested or unterminated delimiter is a
  protocol error, and that an oversized bundle terminates rather than buffering.
- Readiness tests drive both versions' sequences against a scripted in-process
  server and assert `Connect` returns at the right point and that the 775 path
  sends exactly one `TeleportConfirm`.
- Subscription tests assert ordering, that a slow subscriber overflows and
  closes without stalling the loop, and that closing a subscription mid-stream
  is race-free.
- Lifecycle tests cover cancellation at every state, disconnect in configuration
  and in play, transport loss, and `Close` idempotency.
- An end-to-end test reaches ready against a pinned local server in offline
  mode.

The race detector runs against lifecycle and subscription tests.

## Risks

**M4 Task 9 is not done.** Every 775 path in this design assumes it. If it slips,
M6.3 can be built and tested entirely against protocol 47, which needs no
configuration state — but then the bundle and `TeleportConfirm` paths have no
live coverage, only scripted coverage.

**Handlers run on the read goroutine.** A handler that blocks stalls the
connection, including keepalive. The fan-out never blocks, so the risk is a
future handler that does work it should not. The rule is documented at the
adapter boundary and tested by a handler that would deadlock if it ran anywhere
else.

**The taxonomy is designed ahead of its implementation.** Seventy-three event
names are fixed here — 16 in the session domain and 57 across the other six —
and M7 implements all but the session ones. Every name is derived from packets
that exist in the pinned 775 and 47 schemas, which is the mitigation — but a
name that turns out to describe the wrong grouping is cheaper to change before
M7 than after, and M7's first task should be to confirm the mapping against real
traffic before writing reducers.

**Chat and UI is 28 packets of mostly presentational data** and the largest
domain by packet count. It carries no state any later milestone consumes. If M7
runs long, it is the domain to defer.

## Exit criteria

| | Criterion |
| --- | --- |
| 1 | `client.New` rejects an invalid profile before any network or authentication work |
| 2 | An offline client reaches `Ready` against a local server on protocol 47 and on 775 |
| 3 | A 775 bundle is delivered as one batch; protocol 47 always delivers batches of one |
| 4 | A slow subscriber overflows and closes without stalling the read loop or delaying keepalive |
| 5 | Every session-domain event is produced by a test, and every non-session event name is declared |
| 6 | `Close` is idempotent, stops every owned goroutine, and produces `Closed` exactly once |
| 7 | `headless-minecraft` builds on Go 1.26.6 against a released `minecraft-protocol` with no `replace` directive |
