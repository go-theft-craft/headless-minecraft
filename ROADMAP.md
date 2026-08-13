# Roadmap

The roadmap records dependency order, not release dates. Completed work remains in the chart so later decisions retain context.

```mermaid
flowchart LR
    P0["P0: repository foundation<br/>in progress"]
    P1["P1: lifecycle and auth"]
    P2["P2: observed world state"]
    P3["P3: constructed components"]
    P4["P4: containers, inventory,<br/>and crafting"]
    P5["P5: movement, digging,<br/>and building"]
    P6["P6: conformance and v1"]
    PM["Cross-cutting: modded<br/>capabilities and profiles"]

    P0 --> P1
    P1 --> P2
    P2 --> P3
    P3 --> P4
    P3 --> P5
    P4 --> P6
    P5 --> P6
    PM -. extends .-> P2
    PM -. extends .-> P3
    PM -. validates .-> P6
```

## P0: repository foundation

- Pin the Go toolchain through `openserbia/go-flake`.
- Define endpoint-scoped authorization and strict safety defaults.
- Define the first wire-profile validation contract.
- Establish release, changelog, and roadmap rules.

## P1: lifecycle and authentication

- Add scoped client lifecycle and bounded stream ownership.
- Add offline and Microsoft authentication providers.
- Complete Java login, encryption, compression, configuration, and play transitions.

## P2: observed world state

- Publish immutable player, entity, chunk, registry, container, and environment snapshots.
- Preserve unknown namespaced attributes, metadata, item data, and custom payloads.
- Apply packet reducers in wire order before publishing events.

## P3: constructed components

- Validate and construct the component graph before network work.
- Add dynamic capabilities, body, physics, collision, and interaction contracts.
- Keep component replacement at construction time.

## P4: containers and crafting

- Represent the menu that the server actually opens.
- Add generic raw slots and semantic menu drivers.
- Add version-aware inventory synchronization and composed crafting plans.

## P5: world operations

- Add manual movement and controller-owned movement strategies.
- Add explicit digging phases and tool-driven multi-block plans.
- Add rotated and mirrored building matrices with partial progress.

## P6: stable contracts

- Run fixture, owned-server, and anti-cheat conformance tests.
- Verify strict recovery for corrections, uncertainty, and disconnects.
- Publish `v1.0.0` after public component and state contracts have compatibility tests.

## Deferred work

Pathfinding, autonomous goal selection, combat strategy, and scheduling remain application concerns. A later Bedrock client requires a separate design after the shared protocol repository implements Bedrock transport and authentication.
