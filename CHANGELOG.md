# Changelog

This file records notable user-visible changes. It follows [Keep a Changelog](https://keepachangelog.com/en/2.0.0/) and [Semantic Versioning](https://semver.org/).

## Unreleased

### Added

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
- `internal/adapter/v1_8`: the join-time world. A vanilla 1.8.9 server sends every column a joining player can see as `map_chunk_bulk` and never as a single-column `map_chunk`, and only the latter was reduced — so a session against vanilla loaded no terrain, answered "not loaded" for every block, and reported nothing wrong. Bulk columns now reach the same sections the single-column packet does. `SkyLightSent` sets the stride, and a blob shorter than its metadata claims stops rather than misaligning the columns after it.
- `internal/adapter/v1_8`: health reaching zero is a death. A vanilla 1.8.9 server was observed killing a player and sending neither the entity status nor the combat event this adapter had been reading: hurt statuses, an enter-combat and an end-combat, health stepping to zero, and nothing naming a death. The vanilla client of that era has no death packet to wait for either — it shows its death screen when health reaches zero — so reading the same signal follows the protocol as implemented. `player.died` still fires once per death when a server sends both.
