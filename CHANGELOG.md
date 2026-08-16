# Changelog

This file records notable user-visible changes. It follows [Keep a Changelog](https://keepachangelog.com/en/2.0.0/) and [Semantic Versioning](https://semver.org/).

## Unreleased

### Added

- Initial repository structure, endpoint-scoped authorization, strict safety defaults, version-profile validation, Devbox tooling, CI, and a tracked pre-commit hook for lint and secret scanning.
- `event`: the client event taxonomy — eight domains, 73 names, and the sixteen session event structs. Every event carries the observed-state revision that produced it.
- `auth`: the identity seam, with an offline provider whose UUID matches the server's own derivation.
- `version`: bundle batching, the version-owned readiness rule, and the adapter contract for handshakes and packet handlers.
- `version/java`: the built-in `Java1_8` and `Current` profiles for protocol 47 and protocol 775.
- `client`: connect, close, wait, and bounded event subscriptions. A client dials, logs in, and returns once the server will accept action packets; a subscriber that falls behind is closed rather than blocked.
