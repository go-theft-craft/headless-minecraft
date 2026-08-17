# Changelog

This file records notable user-visible changes. It follows [Keep a Changelog](https://keepachangelog.com/en/2.0.0/) and [Semantic Versioning](https://semver.org/).

## Unreleased

### Added

- Initial repository structure, endpoint-scoped authorization, strict safety defaults, version-profile validation, Devbox tooling, CI, and a tracked pre-commit hook for lint and secret scanning.
- `event`: the client event taxonomy — eight domains, 76 names, and the sixteen session event structs. Every event carries the observed-state revision that produced it.
- `event`, `world`: damage attribution and death. `event.Damage` names the damage type, the entity held responsible, the entity that dealt it, and a source position, each with a flag saying whether the protocol sent it at all. `player.damaged`, `player.died`, and `entity.died` are new; `entity.damaged` carries `event.Damage` in place of its bare source type. Protocol 775 attributes damage and protocol 47 attributes death, and where a protocol is silent the event reports silence rather than a zero that reads like an observation.
- `auth`: the identity seam, with an offline provider whose UUID matches the server's own derivation.
- `version`: bundle batching, the version-owned readiness rule, and the adapter contract for handshakes and packet handlers.
- `version/java`: the built-in `Java1_8` and `Current` profiles for protocol 47 and protocol 775.
- `version`: `Outbox`, the batch-scoped seam handlers queue answers in. The client answers keepalives in both play and configuration, and answers the two questions a protocol 775 server stops configuration on.
- `client`: the client owns the configuration phase on protocol 775 — it takes the connection over from the login negotiator at configuration, so registry data, feature flags, and resource-pack offers reach handlers rather than being consumed inside the login sequence.
- `client`: connect, close, wait, and bounded event subscriptions. A client dials, logs in, and returns once the server will accept action packets; a subscriber that falls behind is closed rather than blocked.
