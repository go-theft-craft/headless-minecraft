# Changelog

This file records notable user-visible changes. It follows [Keep a Changelog](https://keepachangelog.com/en/2.0.0/) and [Semantic Versioning](https://semver.org/).

## Unreleased

### Added

- `behaviour`: one shape for a multi-tick task — follow, flee, eat, block with a
  shield, dig, build, fish, and a sequence that composes them. A behaviour is
  asked once per tick and never drives, which is what `adapter.Source` already
  requires; a wait is a tick that returns no action, so nothing here sleeps,
  blocks, or owns a goroutine. Scopes are checked at construction rather than per
  tick, because a behaviour that discovered on tick four hundred that it may not
  dig has already walked the bot somewhere it should not be.

  `Block` refuses at construction on protocol 47, which has no offhand and
  therefore no shield, and it finds that out by asking the adapter rather than by
  matching a protocol name. `Fish` refuses to be built without a bite detector
  the caller supplies: no packet in either protocol says a fish bit, what a
  client actually reads is the bobber's motion, and a threshold this package
  invented would make a bot that does nothing look like it works. Nothing in the
  client's required path imports any of it.

- `safety`: `ScopeAttack`, separate from `ScopeInteract`. The two are one action
  on the wire — both protocols carry attack as a mode of the same interact packet
  — and the split is a safety decision rather than a protocol one: an
  authorization that permits opening a chest is not obviously one that permits
  attacking a player.

### Changed

- `internal/adapter`: both adapters read a chunk column through
  `minecraft-protocol`'s new `wire/java/chunk` rather than carrying their own
  copy of the layout, which `v0.8.0` brings in. The column is the same bytes for
  anything that speaks the protocol, and it was being read from scratch in every
  repository that wanted a block out of one — the layout is the part that fails
  silently, so a second copy is a second chance to decode plausible wrong blocks
  rather than to fail.

  What stays here is what belongs to a client rather than to the wire: where a
  775 column sits, which the blob does not say and the `dimension_type` registry
  does; that a section this client cannot read answers in the world's own
  vocabulary; and that a column that cannot be read at all is kept whole and
  undecoded rather than dropped. The tests that pinned the container layout move
  to the module that now owns it, including the captured Paper 26.1 column; the
  ones that pin placement, the fallback, and the world's answer stay.

- `examples/orbit` no longer defines its own `Vec3`, `BlockPos`, or the vector
  arithmetic around them. It was never isolation, only a copy — the example
  already depended on `minecraft-simulation` — and the arithmetic now lives in
  `geom` with the doc comments it was written with. `Sender.Step` sends a
  computed pitch where it sent a literal zero, so a caller can aim at a block
  underfoot; the bot's own behaviour is unchanged, because it still looks level
  walking a flat circle.

- `predict`: a prediction loop that simulates the local player ahead of the
  server and reconciles when the server disagrees. The rules are
  `minecraft-simulation`'s, so this client and a server run the same ones; what
  this package owns is the fork a prediction is applied to, the choice of which
  movement packet a tick warrants, and what happens when a correction arrives.
  It carries the cadence rule `version/action.go` measured off a real client's
  traffic, and its tests assert it tick by tick without a server: a position when
  the squared distance passes 9.0e-4 or twenty packets have gone without one, a
  look when only the rotation changed, and a bare ground flag when neither did.
  `predict.Terrain` turns the observed chunks into the simulation's tri-state
  block view, so a cell in an unloaded chunk is unknown rather than air and a
  tick over unstreamed terrain reports itself incomplete instead of walking the
  player through it. The resolver is per version, because the two protocols
  disagree about what a block state is.
- `internal/vanilla` and a `test:vanilla` task: the conformance lane. It starts a
  real server, connects, runs scripted input, and requires zero corrections. Six
  scenarios on Java 1.8.9 and the same six on 26.1.2 pass — no position packet
  after the client began reporting its own, nothing in either server's log about
  a player moving wrongly or too quickly, and the cadence exactly as the measured
  rule predicts. It runs behind a build tag and skips when the prepared jar is
  absent, so an ordinary verify stays fast and offline.
  The lane runs in offline mode, which is a limit of it: nothing measured there
  says anything about online-mode behaviour until Microsoft authentication lands.

- Initial repository structure, endpoint-scoped authorization, strict safety defaults, version-profile validation, Devbox tooling, CI, and a tracked pre-commit hook for lint and secret scanning.
- `version`, `client`: the outbound action path. `Client.Do` sends one
  version-neutral intent — `ActionMove`, `ActionLook`, `ActionMoveLook`, or
  `ActionGround` — which the profile's adapter encodes for its protocol. Calls are
  serialized against each other and against the read loop's own replies, refused
  before the server places the player, and report a failed write rather than
  swallowing it.
- `event`: the client event taxonomy — eight domains, 76 names, and the sixteen session event structs. Every event carries the observed-state revision that produced it.
- `event`, `world`: damage attribution and death. `event.Damage` names the damage type, the entity held responsible, the entity that dealt it, and a source position, each with a flag saying whether the protocol sent it at all. `player.damaged`, `player.died`, and `entity.died` are new; `entity.damaged` carries `event.Damage` in place of its bare source type. Protocol 775 attributes damage and protocol 47 attributes death, and where a protocol is silent the event reports silence rather than a zero that reads like an observation.
- `auth`: the identity seam, with an offline provider whose UUID matches the server's own derivation.
- `version`: bundle batching, the version-owned readiness rule, and the adapter contract for handshakes and packet handlers.
- `version/java`: the built-in `Java1_8` and `Current` profiles for protocol 47 and protocol 775.
- `version`: `Outbox`, the batch-scoped seam handlers queue answers in. The client answers keepalives in both play and configuration, and answers the two questions a protocol 775 server stops configuration on.
- `client`: the client owns the configuration phase on protocol 775 — it takes the connection over from the login negotiator at configuration, so registry data, feature flags, and resource-pack offers reach handlers rather than being consumed inside the login sequence.
- `world`: observed world state. Eight immutable domains — player, entities, chunks, environment, containers, registries, raw payloads, and chat — read at one revision, applied one revision per batch so a reader never sees half a protocol 775 bundle. Every event carries the revision that produced it, and `Snapshot()` at that revision shows what the event describes.
- `world`: unknown values are preserved rather than defaulted. A metadata index no version models, a registry key with an unknown namespace, a menu type that is not vanilla, an unknown attribute, and a plugin message on an unregistered channel are each kept as sent and addressable by key. Every peer-filled store is bounded per owner and reports what its bound refused.
- `client`: `WithWorld` installs the observed state a connection maintains, and `World()` returns the current snapshot.
- `examples/observe`: connects, maintains a world, and prints every state event with its revision.
- `client`: connect, close, wait, and bounded event subscriptions. A client dials, logs in, and returns once the server will accept action packets; a subscriber that falls behind is closed rather than blocked.
- `event`, `world`: the spawn position. `world.spawn_changed` and `EnvironmentView.Spawn` report the compass target both protocols send, with the dimension and facing angle protocol 775 adds and protocol 47 does not. It is the level's shared spawn on join and the player's own respawn point after a bed, because the packet carries one value for both and no reason for it; `SpawnKnown` separates a server that said nothing from one that named the origin.
- `version`, `client`: `ActionRespawn`, the intent a dead client sends to come back. Protocol 47 numbers its client commands and 775 names them, and the adapter carries that difference. Without it a client that died could not act at all — movement and interaction are refused while dead — so it stayed dead for the rest of the session.
- `internal/adapter/v26_1`: protocol 775 chunk sections are decoded. A column is split into its sections and each is decoded lazily into block states, so `ChunksView.Block` answers on 26.1 rather than reporting `ErrSectionNotDecodable`. Where a column sits is read from the dimension type registry's `min_y` — the overworld's lowest section is -4, the nether's is 0 — and a column whose floor is not yet known is kept whole and undecoded as before, rather than placed at a guess.
- `client`, `event`: two guards against a session that works and observes nothing. `New` now refuses a world installed against an adapter that supplies no reducers, which is the M7 defect that shipped: the seam is satisfied by interface assertion, so an adapter whose `Reducers` is a package-level function compiles, passes its own tests, and installs a world that counts batches and sees nothing. And a placed session that has loaded no chunk after `WithObservationGrace` (ten seconds by default) publishes `session.observation_missing` and logs a warning, rather than answering "not loaded" for every block in silence. The report does not end the connection: no protocol obliges a server to send terrain.
- `internal/adapter/v1_8`: the join-time world. A vanilla 1.8.9 server sends every column a joining player can see as `map_chunk_bulk` and never as a single-column `map_chunk`, and only the latter was reduced — so a session against vanilla loaded no terrain, answered "not loaded" for every block, and reported nothing wrong. Bulk columns now reach the same sections the single-column packet does. `SkyLightSent` sets the stride, and a blob shorter than its metadata claims stops rather than misaligning the columns after it.
- `internal/adapter/v1_8`: health reaching zero is a death. A vanilla 1.8.9 server was observed killing a player and sending neither the entity status nor the combat event this adapter had been reading: hurt statuses, an enter-combat and an end-combat, health stepping to zero, and nothing naming a death. The vanilla client of that era has no death packet to wait for either — it shows its death screen when health reaches zero — so reading the same signal follows the protocol as implemented. `player.died` still fires once per death when a server sends both.

### Fixed

- `internal/adapter/v26_1`: an entity's velocity is the velocity the server sent.
  This client pinned `minecraft-protocol v0.5.0`, whose quantised-vector reader
  took the upper thirty-two bits little endian where vanilla writes them big
  endian, so every spawn and velocity packet on protocol 775 reached the observed
  world as a plausible number unrelated to the entity's motion — an arrow
  summoned with `Motion:[0.1d,0.0d,0.0d]` was recorded moving on all three axes.
  Taking `v0.6.0` fixes it, and a new test decodes the bytes a real 26.1.2 server
  sent rather than building the packet as a value, because a value-built test
  cannot see a byte order at all.
- `internal/adapter`, `client`: a kick reaches this client. Taking
  `minecraft-protocol v0.7.0` picks up a stream fix: a peer that kicks writes its
  disconnect and closes, so the frame and the EOF behind it arrive together, and
  the stream discarded that packet when its queue closed with the transport.
  This client saw a bare EOF instead of the server's reason, rarely and only
  under load — about three sessions in eight hundred on a busy machine.
- `examples/orbit`: a bot in a pool swims out of it rather than hopping across
  it. The planner offered a gap-crossing jump from a cell the body was swimming
  in, so the route out of water was three jumps no body can make; a swimmer
  cannot push off anything, and `minecraft-simulation` says so as of the pin
  below. That defect only became visible from here once the bot started
  performing the jumps it was routed, which is the entry above.
- `examples/orbit`: the bot jumps the edges it was routed over. The planner
  crosses a one-block rise with `EdgeStep` and a hole with `EdgeJumpGap`, and the
  example flattened a path to bare positions — so the edge kind was lost, every
  step was sent as a walk, and the bot walked into the block it had been routed
  over and off the edge of the gap it had been routed across. A route carries a
  jump flag per waypoint now, smoothing will not shortcut a jump away, and the
  actuator arcs the body where the planner said to. The flag was a hard `false`
  for a reason worth keeping: the actuator had no body to jump with, and a claim
  the body cannot honour becomes a lie to the server once the bot reports what it
  is doing on the wire.
- `client`: acknowledging placement to a server that then hangs up is not a
  failed connection. `minecraft-protocol v0.7.2` reports what the transport did
  with a frame that was in flight when the peer left, rather than refusing a
  write the peer already had — which this client read as a session that ended
  before the player was placed.
- `client`: a session that was placed and then kicked is not reported as one
  that never connected. `Connect` waited on the readiness signal and the read
  loop ending, and a server that places the player and hangs up straight after
  makes both ready in the same instant — a select picks between ready cases at
  random, so about half of those returned "connection ended before the player
  was placed" for a session that reached play and already had the server's
  reason for leaving waiting on its subscription. Readiness is now taken first.
  The same message rendered a loop that ended without an error as
  `%!w(<nil>)`, which described the formatting verb rather than the session.
- `client`: a disconnect names the state the session ended in. The state was
  read back off the stream at the moment the ending was reported, and a
  terminated stream answers nothing about itself — so every transport loss, the
  killed-server case included, published `"unknown"` for a state the client had
  watched the session enter. It now reports the state the last packet it
  processed arrived in — the read loop knows that without asking anything that
  can be gone by then — and keeps `"unknown"` for a session that never reached a
  state at all.
- Taskfile: every lane `verify` runs resolves modules with the workspace off. A
  `go.work` is gitignored, so it is present on a developer machine and absent in
  CI, and the gate was building the neighbouring working tree while CI built the
  version `go.mod` pins — which is how this repository ran a release behind
  without a red check anywhere. `test:fast` keeps the workspace, because editing
  two modules together is what it is for.
- `internal/adapter/v26_1`: a malformed chunk column no longer takes the process
  down. A protocol 775 paletted container states how many bits an entry it
  packs; the decoder divided 64 by that number to get entries per long, and then
  divided by the result, so a width past 64 divided by zero and panicked. The
  bytes come from whatever the session is connected to, which made it a crash a
  server could ask for rather than a decode failure. A width nobody could have
  meant is now a column this client cannot read, which leaves its bytes in the
  store undecoded exactly as every other unreadable column does.
