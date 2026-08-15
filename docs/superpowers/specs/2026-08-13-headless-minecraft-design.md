# Headless Minecraft and shared protocol design

## Purpose

`headless-minecraft` is a native Go client for Minecraft Java Edition servers. It implements the client without launching Mojang's Java program. Go programs use the package to authenticate, join a server, observe game state, and perform player actions.

Two Go modules separate shared protocol code from client behavior:

- `github.com/go-theft-craft/minecraft-protocol` owns protocol definitions, game data, wire codecs, generation, and built-in versions.
- `github.com/go-theft-craft/headless-minecraft` owns authentication, client lifecycle, observed state, events, and player actions.

The first shared release includes Java Edition 1.8 protocol 47 for the existing server and the PrismarineJS Java 26.1 data family, protocol 775, for the headless client. The 26.1 bundle covers the current 26.1.2 patch because the protocol number and data family are unchanged. The shared repository pins each Minecraft data family, protocol number, PrismarineJS source revision, and generated file. Updating a built-in version requires a generator run and the protocol test suite.

## Goals

The first release provides these capabilities:

- Connect to online-mode and offline-mode Java Edition servers.
- Authenticate Microsoft accounts through the device-code flow.
- Complete the handshake, status, login, configuration, and play states.
- Handle packet framing, compression, encryption, keepalive packets, disconnects, and reconnect-safe cleanup.
- Expose high-level actions for chat, movement, looking, digging, block placement, entity interaction, inventory, and crafting.
- Maintain concurrency-safe state for the player, world, entities, inventory, health, effects, abilities, and time.
- Generate a built-in protocol and the complete matching PrismarineJS `minecraft-data` dataset from pinned source files.
- Accept caller-supplied protocol and data implementations.
- Expose low-level packets and raw upstream data when the typed API does not cover a use case.
- Let the server, proxy, and headless client reuse protocol and game-data packages without depending on each other.
- Keep the shared protocol contract neutral to Java Edition, Bedrock Edition, and custom protocol families.

## Non-goals

The first release does not provide autonomous goals, combat strategy, pathfinding, or a script scheduler. Applications can build those features from the state and action APIs. The package does not render graphics, play audio, load client mods, or emulate Mojang client user interface behavior.

The first release does not implement a Bedrock client or generate a built-in Bedrock version. The shared interfaces and generated package layout reserve Bedrock as a separate protocol family. Bedrock transport, authentication, and client behavior require a later design.

## Repository boundaries

The shared repository has this layout:

```text
minecraft-protocol/
  .envrc                    Direnv entrypoint for Devbox
  devbox.json               Pinned development packages
  devbox.lock               Resolved Devbox package versions
  Taskfile.yml              Project command interface
  data/                     Game-data interfaces and indexes
  protocol/                 Edition-neutral protocol contracts
  wire/                     Shared packet envelopes and primitives
  wire/java/                Java framing, compression, and encryption
  wire/bedrock/             Reserved for Bedrock RakNet and framing
  stream/                   Optional managed duplex sessions
  router/                   Optional packet dispatch
  middleware/               Optional send and receive wrappers
  capture/                  Packet capture and replay formats
  java/status/              Java server-list ping helper
  java/login/               Java login and configuration helper
  mctest/                   Protocol fixtures and test transports
  generated/
    java/
      v1_8/                 Java 1.8 protocol 47
      v26_1/                Java 26.1 protocol 775
      current/              Alias for the current stable Java version
    bedrock/                Reserved for later built-in versions
  internal/codegen/         JSON schema parsing and Go generation
  cmd/mcdata-gen/           Generator command
  cmd/mcproto/              Inspect, validate, diff, decode, and replay tool
  source/                   Pinned JSON, manifests, checksums, and licenses
  testdata/                 Packet, data, and generator fixtures
```

The headless client has this layout:

```text
headless-minecraft/
  .envrc                    Direnv entrypoint for Devbox
  devbox.json               Pinned development packages
  devbox.lock               Resolved Devbox package versions
  Taskfile.yml              Project command interface
  auth/                     Microsoft and offline authentication
  client/                   Public client, options, lifecycle, and events
  version/                  Built-in and custom behavior profiles
  component/                Construction-time component contracts
  safety/                   Authorization, recovery, and circuit breakers
  capability/               Dynamic rules and effective capabilities
  body/                     Player dimensions and poses
  physics/                  Replaceable mechanics models
  collision/                Version-aware collision interpretation
  movement/                 Replaceable movement controller and strategies
  container/                Generic containers, layouts, and drivers
  inventory/                Replaceable inventory operations
  crafting/                 Replaceable recipe planning and execution
  digging/                  Replaceable digging rules and execution
  building/                 Replaceable placement shapes and execution
  event/                    Normalized client events and subscriptions
  world/                    Player, chunk, entity, and inventory state
  internal/adapter/         Protocol packets to client events and actions
  internal/session/         Login, configuration, and play orchestration
  testdata/                 Session, action, and server fixtures
```

Both projects start on the Go 1.26.5 toolchain published by `openserbia/go-flake` because Go 1.27 is not stable at design time. The public contracts use ordinary interfaces and package-level generic helpers. After Go 1.27 becomes stable and is available through `go-flake`, the projects can add generic-method conveniences on concrete types without changing the injectable interfaces.

Both projects use Devbox for the pinned development environment and Task for every project command. Devbox supplies Go, Task, `gofumpt`, `gci`, `golangci-lint`, Delve, and other build tools. Each repository commits `devbox.json`, `devbox.lock`, and `.envrc`.

The `.envrc` file runs `eval "$(devbox generate direnv --print-envrc)"`, matching the existing server and proxy repositories. Developers run `direnv allow` once after cloning. Entering either repository then activates its pinned Devbox environment.

Developers and CI run commands through `devbox run -- task <name>`. Documentation does not instruct users to run `go`, generators, formatters, or linters directly. Generated files contain a `DO NOT EDIT` header. Developers change the generator or source JSON instead of generated Go.

The dependency graph has one direction:

```text
minecraft-protocol
    +-- headless-minecraft
    +-- server
    +-- proxy
```

## Development commands

Each repository commits a root `Taskfile.yml`. Task names and behavior stay consistent across both repositories:

```text
task deps              Download, tidy, and vendor dependencies
task fmt               Format Go code and imports
task lint              Run formatting checks and golangci-lint
task test              Run unit and integration tests
task test:race         Run race-enabled tests
task build             Build commands and examples
task verify            Run generation checks, formatting, lint, tests, race tests, and builds
```

The shared protocol repository also provides these tasks:

```text
task data:fetch        Fetch pinned PrismarineJS source data
task data:validate     Validate source files, manifests, aliases, and checksums
task generate          Generate protocol and game-data packages
task generate:check    Fail when committed generated output differs
task version:diff      Compare two generated protocol versions
```

Tasks accept configuration through Task variables or `CLI_ARGS`. They do not contain developer-specific paths, hosts, or credentials. Fetch and generation tasks write only to explicit repository paths and support a check mode that does not modify files.

`task verify` is the local and CI release gate. CI uses the committed Devbox lock file and does not install separate tool versions.

## Shared protocol boundary

The shared protocol contract identifies the protocol family and binds packet behavior to matching game data:

```go
type Protocol interface {
	ID() string
	Edition() Edition
	Version() Version
	NewCodec(role Role) Codec
	Data() data.Set
}
```

`ID` is a stable identifier for registration and configuration. `Edition` identifies Java, Bedrock, or a custom family. `Version` contains the Minecraft version name and numeric protocol ID when the family uses one. `Role` selects client-side or server-side packet directions. `Codec` creates and decodes packets for each direction and connection state.

The headless package extends this contract with a client adapter. The adapter translates decoded packets into normalized client events and translates high-level actions into server-bound packet values. This client-specific translation does not belong in the shared repository.

The low-level stream accepts a `protocol.Protocol` directly. The headless client normally accepts a complete conformance profile through `client.WithVersion`. If the caller does not provide one, the client uses the current generated Java profile. Advanced callers may pair a protocol and adapter directly with `client.WithProtocol` for raw lifecycle and packet access, but high-level gameplay components remain unavailable until a compatible conformance profile is supplied. A custom implementation can use generated types, hand-written types, a runtime schema interpreter, or another source.

Protocol instances are immutable and safe to share between clients. Each connection receives a fresh stateful `Codec`.

## Protocol families

Java and Bedrock use the same top-level contracts, packet envelope, data lookup concepts, and source manifest format. They do not share transport assumptions.

Java packages own TCP framing, Java connection states, compression, encryption, and Java packet primitives. Bedrock packages will own RakNet over UDP, Bedrock framing, Bedrock compression, Xbox identity chains, and Bedrock packet primitives.

Custom protocols use their own `Edition` value. The proxy's legacy implementation remains in `proxy/internal/legacy` because it is specific to that project. legacy can reuse shared primitive and data packages and can implement `protocol.Protocol` when integration requires it.

## Version conformance profiles

A client version is more than a packet codec. Each built-in version provides one immutable conformance profile containing its protocol and adapter, vanilla physics defaults, collision interpretation, inventory transaction and state-ID rules, action ordering, capability defaults, and requested packet limits. The current Java profile is the default. Java 1.8 and Java 26.1 keep separate profiles even where they share component implementations.

```go
type Profile struct {
	ID           string
	Protocol     protocol.Protocol
	Adapter      Adapter
	Physics      physics.Factory
	Collision    collision.Factory
	Inventory    inventory.Semantics
	Ordering     interaction.Ordering
	Capabilities capability.Factory
	Limits       protocol.Limits
}
```

`client.WithVersion(profile)` replaces the complete profile at construction. Callers can start with a built-in profile and explicitly override selected mechanics for a modded server, or supply a complete custom protocol family. `client.New` rejects an incomplete or internally incompatible profile before authentication or network work.

The profile owns version-dependent behavior. Gameplay components consume narrow interfaces and do not branch on numeric protocol versions. Inventory semantics decide whether a version uses state IDs, transaction IDs, acknowledgements, or another synchronization scheme. Interaction ordering decides required packet sequences and barriers. Collision and physics use the selected version's shapes, coordinate rules, and defaults, then apply observed attributes and dynamic capabilities.

Packet limits have two levels. A version profile requests limits appropriate for its schema, including maximum frame, string, collection, NBT, plugin-payload, and decompressed sizes. Process-wide hard ceilings protect memory, decompression, queue capacity, and recursion depth. A custom profile may choose stricter values or request larger compatible values up to those ceilings. It cannot disable bounds or exceed the ceilings through ordinary client options.

Built-in profile tests replay vanilla client and server transcripts and assert physics, collision, inventory state IDs, action ordering, and packet bounds. Custom-profile contract tests verify the same invariants supplied by the custom implementation. A profile is compatible because it passes these contracts, not because it imitates a particular anti-cheat implementation.

## Construction-time component graph

The headless client composes replaceable components when `client.New` runs. The default graph includes safety, capabilities, body, physics, movement, interaction, container, inventory, crafting, digging, and building components. Callers can replace gameplay components without replacing the connection lifecycle or protocol stream.

```go
type Components struct {
	Safety       safety.Policy
	Capabilities capability.Factory
	Body         body.Factory
	Physics      physics.Factory
	Collision    collision.Factory
	Movement     movement.Factory
	Interaction  interaction.Factory
	Containers   container.Registry
	Inventory    inventory.Factory
	Crafting     crafting.Factory
	Digging      digging.Factory
	Building     building.Factory
}
```

Factories create per-client component instances from narrow dependencies such as snapshots, packet sending, a clock, game data, and protocol capabilities. Components do not receive unrestricted access to client internals. `client.New` validates missing components, incompatible protocol capabilities, and dependency cycles before any network work starts.

Whole components cannot be replaced while `Client.Run` is active. This rule prevents an in-flight action from losing its state, confirmation waiter, or packet ownership. A component can expose runtime strategy selection when the component owns that transition. For example, the movement component can switch between walk and bunnyhop strategies without replacing the movement component itself.

## Strict safety profile

`safety.Strict()` is the default. High-level automation remains disabled until the application supplies a construction-time server profile that declares the endpoint and the automation scopes its operator permits. This declaration cannot prove permission, but it prevents a script from using high-level actions against an arbitrary address by accident. Raw codec and packet APIs remain available and carry no claim that the library can police custom callers.

The strict profile serializes state-changing operations within each component, permits at most one unresolved operation in that component, and never retries an ambiguous or non-idempotent operation. Inventory corrections discard pending projections and block further writes until a complete authoritative container state arrives. An unknown layout exposes raw read-only state until a driver resolves it. A rejected semantic inventory goal may be planned again only from a newer synchronized revision and only when the caller explicitly requests another attempt.

Movement keeps observed and projected positions separate. Collision checks use loaded world state and current body, physics, and capability values. Strict mode refuses a movement segment through unknown collision data. An unexpected server position or velocity correction stops the active strategy, discards its projection, and opens the movement circuit breaker. The application must acknowledge the correction before starting another strategy. The library does not infer that a correction came from anti-cheat software.

Capability loss, inventory uncertainty, repeated no-progress observations, server warnings, kicks, and disconnects stop dependent work. The library never reconnects or resumes work automatically. Every stop produces an immutable safety event and a structured outcome with the attempted operation, cause, server correction when present, final snapshot revision, completed work, and unattempted work.

The profile applies protocol-conformance checks consistently. It does not detect anti-cheat plugins, inspect their thresholds, introduce human-like jitter, or vary behavior to avoid detection. Integration tests may run against an owned server with an open-source anti-cheat installed, but any alert fails the test and prompts a protocol or mechanics fix.

The client does not proactively announce that it is automated. It also does not ship identity spoofing or false vanilla metadata. By default, it sends only identity and custom payloads required by the selected protocol. Applications may add their own protocol extensions through the raw adapter boundary.

## Packet access

Generated packet structs provide typed access for the built-in protocol. Every decoded packet also has this envelope:

```go
type Packet struct {
	State     State
	Direction Direction
	ID        int32
	Name      string
	Value     any
	Payload   []byte
}
```

`Value` contains the generated packet type when decoding succeeds. `Payload` contains an owned copy of the packet body. Callers can inspect packets that the normalized API does not yet understand.

Unknown packet IDs produce an event instead of terminating the session when packet framing permits the client to skip the payload. Malformed framing, encryption failures, and required-state packet failures terminate the session with a typed error.

## Codec and stream layers

The shared repository does not require callers to use a managed connection. The lowest codec layer is synchronous and works with `io.Reader` and `io.Writer`:

```go
decoder := java.NewDecoder(protocol, limits)
encoder := java.NewEncoder(protocol, limits)

packet, err := decoder.Read(reader)
err = encoder.Write(writer, packet)
```

The codec owns only protocol state such as connection phase, compression threshold, and cipher position. The caller owns the reader, writer, goroutines, deadlines, and shutdown. This layer works with network connections, proxies, files, buffers, and packet captures.

The optional `stream` package manages a duplex connection:

```go
session := stream.New(conn, codec, stream.Options{
	Role: protocol.Client,
})

err := session.Run(ctx, func(ctx context.Context) error {
	return session.Send(ctx, packet)
})
```

The same session supports client and server roles. One goroutine owns decoding, and one goroutine owns encoding. Reads and writes can run concurrently, but a single owner preserves frame boundaries, compression state, and cipher order in each direction.

The codec itself does not accept context because a general `io.Reader` or `io.Writer` has no cancellation contract. The managed stream applies context cancellation through deadlines and transport closure. Closing the transport unblocks pending I/O. A partial frame write terminates the session because retrying the remaining bytes could corrupt protocol state.

## Limits and backpressure

Every decoder has explicit limits for frame size, decompressed size, strings, arrays, NBT depth, NBT size, and collection counts. The decoder validates lengths before allocation and uses bounded readers during decompression. It reads fixed-size payloads with exact-read semantics and handles short reads correctly.

Managed queues have fixed capacities. `Send(ctx, packet)` waits for writer capacity or context cancellation. `TrySend(packet)` returns `ErrBackpressure` immediately. The stream never drops outbound packets silently.

Protocol control traffic has reserved capacity so keepalive and disconnect packets do not wait behind a full automation queue. The writer preserves FIFO order within each priority. Configuration controls queue sizes and limits, and safe finite defaults apply when callers omit them.

The managed stream uses one error group for its goroutines. The first fatal error cancels the group, closes the transport, and becomes the session error. `Close` remains idempotent.

## Optional helpers and tools

Optional packages build on the public codec and stream interfaces. `router` dispatches packets by state, direction, ID, name, or generated Go type. `middleware` wraps send and receive operations for logging, tracing, metrics, filtering, and recording. `java/status` implements server-list ping. `java/login` implements Java handshake, login, encryption, compression, and configuration transitions without requiring the headless client.

The `capture` package defines a versioned packet-capture format with timestamps, state, direction, packet ID, and payload. Capture and replay work at raw-frame and decoded-packet levels.

The `mcproto` command provides non-interactive tools:

```text
mcproto data fetch
mcproto data validate
mcproto generate
mcproto generate --check
mcproto version diff
mcproto packet decode
mcproto packet encode
mcproto capture inspect
mcproto capture replay
```

Every subcommand accepts all required input through flags or stdin. Commands support structured JSON output where another program may consume results. Each `--help` page includes complete examples. Commands validate input before writing, and repeated successful runs produce the same files.

## Complete PrismarineJS data support

For each generated version, the generator imports every dataset that PrismarineJS publishes for that edition and version. The initial Java set includes:

- protocol definitions and protocol-version metadata,
- blocks, block states, block collision shapes, block loot, and materials,
- items, item components, item loot, foods, and recipes,
- entities, entity metadata, entity loot, and attributes,
- biomes, effects, enchantments, particles, sounds, instruments, map icons, and tints, and
- windows, commands, language strings, feature flags, and legacy mappings.

The generator discovers datasets from the upstream version manifest instead of relying on a fixed filename list. It copies every resolved source file into a pinned source directory and records its SHA-256 digest and media type. Known JSON datasets receive generated Go types and indexes. Unknown files and non-JSON files such as `proto.yml` remain available through `data.Set.Raw(name)`. The same mechanism can ingest complete Bedrock datasets when that protocol family is implemented.

The data API supports lookup by numeric ID, namespaced name, block state ID, and other natural keys defined by each dataset. Lookup methods return immutable values. Collections preserve an upstream order only when that order has protocol meaning.

The generator resolves upstream aliases before generation. A generated version contains its own complete view even when PrismarineJS stores unchanged data in another version directory.

## Data provenance and updates

PrismarineJS `minecraft-data` is the primary source. The generator pins a Git commit rather than a moving branch or release alias. The generated manifest records:

- the Minecraft version,
- the numeric protocol version,
- the PrismarineJS repository URL and commit,
- every source path and SHA-256 digest,
- the generator version, and
- the generation time in UTC.

The official Minecraft server JAR report output validates current packet and registry IDs when upstream data is new or incomplete. Validation does not silently rewrite PrismarineJS input. Any correction lives as a reviewed overlay with its source and reason in the manifest.

The repository retains applicable upstream license and attribution files beside the pinned data.

## Migration from server and proxy

The shared repository starts from tested code that already exists in `server`:

- `server/pkg/protocol` moves to the shared `wire` and `wire/java` packages.
- `server/pkg/gamedata` moves to the shared `data` package.
- `server/pkg/gamedata/versions/pc_1_8` becomes `generated/java/v1_8`.
- `server/cmd/codegen` and its schema types move to the shared generator.
- The pinned PrismarineJS input for Java 1.8 moves with the generator output.

The migration preserves behavior before the generator grows current-version support. The server then replaces local imports with `github.com/go-theft-craft/minecraft-protocol`. The proxy replaces its imports of `github.com/go-theft-craft/server/pkg/protocol` with shared imports. The proxy keeps its custom legacy codec, generated packets, and reverse-engineered data.

The migration does not move server world simulation, server handlers, player management, storage, proxy behavior code, or proxy world state. Those packages contain application behavior rather than reusable protocol definitions.

## Authentication

`auth.Provider` supplies a session identity to the client:

```go
type Provider interface {
	Authenticate(ctx context.Context) (Identity, error)
}
```

The built-in providers are:

- `auth.Offline`, which creates an offline identity from a username, and
- `auth.Microsoft`, which implements device-code login and the Microsoft, Xbox Live, XSTS, and Minecraft service token exchange.

The Microsoft provider accepts a token store interface. The library does not persist tokens unless the application supplies a store. Token refresh happens before connection when a stored refresh token is available. Device-code callbacks expose the verification URL, user code, and expiry to the application without printing them.

Secrets and access tokens never appear in errors, logs, events, or packet traces. The package uses the standard system trust store for HTTPS and does not disable certificate verification.

## Client lifecycle

Applications construct a client with functional options and call `Connect`:

```go
bot, err := client.New(
	client.WithAddress("example.org:25565"),
	client.WithAuth(authProvider),
	client.WithVersion(version.JavaCurrent()),
)
if err != nil {
	return err
}

if err := bot.Connect(ctx); err != nil {
	return err
}
defer bot.Close()
```

`Connect` performs authentication, opens TCP, runs the handshake, negotiates encryption and compression, completes configuration, and enters play. It returns only after the client is ready to accept actions. The caller's context controls the complete operation.

`Close` is idempotent. It stops network loops, closes subscriptions, and waits for owned goroutines. The library never reconnects automatically. Applications decide retry policy because reconnecting can repeat actions.

## Events and concurrency

One reader goroutine owns network reads. One writer goroutine serializes outgoing packets. State reducers process decoded packets in wire order before subscribers receive the matching normalized event.

Applications subscribe with a bounded buffer and cancel the subscription explicitly. Each subscription selects normalized event types, raw packets, or both. A slow subscriber receives an overflow error and closes. It cannot block keepalive handling or other subscribers.

Event payloads and state snapshots are immutable from the caller's perspective. The client does not invoke user callbacks from the network loops. Applications consume subscriptions in their own goroutines.

## World state

The world model stores only data observed by the connection. It includes:

- the local player's identity, position, rotation, velocity, health, food, experience, abilities, effects, and game mode,
- loaded chunks, sections, block states, block entities, lighting, and dimension information,
- tracked entities, metadata, attributes, equipment, passengers, and movement,
- open containers, slots, carried items, transaction state, and recipes, and
- world time, weather, spawn position, scoreboard state, teams, and boss bars.

Chunk and entity removal packets release their state. The client exposes read-only snapshots and focused lookup methods. It does not expose internal maps.

Servers can send data-driven registries during configuration. Session registry data overrides the matching built-in registry for that connection. Static generated data remains available for lookups that do not depend on server configuration.

Observed state preserves unknown namespaced attributes, abilities, metadata, menu types, and plugin-channel data. The state layer does not force vanilla defaults onto values that a modded server supplies.

Mechanics live outside observed state. A body model describes dimensions, eye height, and poses. A physics model describes gravity, step height, movement speed, jump strength, collision rules, and other mechanics. A movement strategy decides when and how to move. This split supports one-block-tall players, custom scale, double jump, flight, and bunnyhop movement without adding mod-specific conditions to the core reducer.

The built-in vanilla model uses observed attributes first and documented protocol-version defaults second. Custom components can interpret additional attributes, plugin messages, or custom protocol packets.

## Dynamic capabilities and rules

Components are fixed after construction, but their answers are not. A replaceable capability engine evaluates the current immutable snapshot and returns effective mechanics for that revision. Inputs include equipment, held items, status effects, attributes, abilities, pose, environment, dimension, game mode, session registries, and custom payloads.

Known capabilities have typed views for body dimensions, jump height and count, gravity, velocity limits, walking, sprinting, flying, swimming, diving, reach, digging, placement, and inventory topology. Unknown namespaced capabilities remain queryable as immutable raw values. Every effective value retains provenance so diagnostics can explain which observed fact or rule supplied it.

The engine evaluates an ordered, construction-time rule set. Typed capabilities define their combination semantics explicitly, such as replace, add, multiply, minimum, maximum, enable, or disable; rules do not compete through undocumented last-write behavior. Contributions record their rule, source fact, operation, and priority. Applications can inspect the trace for a capability or replace the engine when a mod uses different semantics.

Body, physics, movement, inventory, digging, and building consult the capability engine at operation time. Results may be cached only by snapshot revision. A potion can therefore raise jump height, water can enable diving, an attribute can change velocity, and worn equipment can alter inventory topology without rebuilding the client.

Inventory topology distinguishes slots advertised by the server from slots currently enabled by rules. A worn backpack can enable an additional slot group when a custom adapter exposes those slots. Removing it disables that group after the matching server update. The client never invents writable protocol slots that the server did not advertise.

Long-running strategies and multi-step plans revalidate relevant capabilities before each primitive. If an effect expires, equipment changes, or the server corrects state, the operation adapts where its policy permits or stops with structured partial progress.

## Containers and slot semantics

The container state always records the server's actual open-screen result: container ID, namespaced menu type, title, state ID, raw slots, carried item, and integer properties. Interacting with a block or entity does not predict which GUI opens.

The generic container API exposes raw slots and click operations for unknown or modded menus. A container-driver registry adds semantic layouts for known menu types. A layout maps semantic references such as player hotbar, armor, input, fuel, and output to actual protocol slot indices.

Built-in drivers cover the player inventory, generic chest rows, crafting, the furnace family, brewing, enchanting, anvil, smithing, merchant, beacon, and horse inventory. Dedicated helpers wrap the generic container and driver. They do not maintain separate copies of container state.

`OpenBlock` and `OpenEntity` return the container that the server actually opened. If a modded server opens a different menu, callers receive that type and can use raw slots or a registered custom driver.

Crafting composes a recipe source, planner, container driver, and inventory executor. Each part is replaceable at construction. A custom recipe system does not require replacing container synchronization.

## Tool-driven world operations

Digging and building are components rather than aliases for one packet. Each component separates a tool-behavior resolver, an operation planner, and an executor. The resolver receives the held stack, observed player attributes and abilities, protocol data, and relevant custom payloads. It can return vanilla single-block behavior or a modded behavior such as vein mining, an area, a line, a plane, or an arbitrary relative placement matrix.

Plans contain ordered primitive interactions with explicit target positions, faces, hands, inventory requirements, and confirmation policy. Executors validate each primitive against the latest observed snapshot and use the shared interaction and inventory ports. Multi-block work is not represented as atomic: cancellation, rejection, or disconnect returns structured progress with completed, failed, and unattempted operations.

The default resolver implements vanilla single-block digging and placement. Applications inject resolvers for modded tools or create plans directly. Limits bound plan size, affected volume, pending confirmations, and execution concurrency. The library never infers an area operation solely from an unfamiliar item name or metadata.

## High-level actions

Actions belong to their configured components. They use context and return after the server confirms the operation when the protocol provides a confirmation. Otherwise, they return after the writer accepts the packet.

The first release includes:

- `Chat` and command submission,
- `Move`, `Look`, `MoveAndLook`, sprint, sneak, jump, and stance changes,
- digging start, cancel, and finish,
- explicit digging and placement-plan execution with progress,
- block use and block placement,
- entity attack and interaction,
- held-slot selection, container clicks, item movement, drop, and close, and
- recipe lookup and crafting requests.

The action layer validates reach, loaded state, slot ranges, and protocol capabilities where the client has enough information. The server remains authoritative. A rejected or corrected action updates state and produces an event.

Default actions do not hide timing. Movement automation controls its own tick rate, and digging automation decides when to finish a dig. A configured movement strategy may own timing explicitly, as a bunnyhop strategy does. This rule prevents the package from silently embedding one bot strategy.

## Errors and observability

Public errors identify the operation and preserve the cause for `errors.Is` and `errors.As`. Error categories include authentication, dialing, protocol negotiation, decoding, encoding, server disconnect, unsupported capability, invalid action, subscription overflow, and closed client.

The client accepts a `log/slog.Logger`. The default logger discards output. Structured fields include the connection state, packet name, packet ID, direction, and operation. Packet payload logging is off by default because packets can contain chat, plugin data, and authentication-related values.

## Testing

The shared repository uses these test levels:

1. Primitive codec tests cover VarInt, strings, positions, NBT, components, arrays, compression, and encryption with boundary and malformed inputs.
2. Generator golden tests compare generated files and manifests with committed fixtures.
3. Packet fixture tests decode captured packets, compare typed values, encode them again, and compare bytes.
4. Compatibility tests run existing Java 1.8 server and proxy fixtures against the migrated packages.

The headless repository uses these test levels:

1. Adapter tests translate generated packets into normalized events and actions back into packet values.
2. State and action tests feed ordered packet sequences into reducers and assert snapshots, events, and emitted packets.
3. End-to-end tests start a pinned local Java Edition server in offline mode. A protocol fixture server verifies the encrypted online-mode handshake without external account access. These tests cover login, configuration, spawning, keepalive, movement, chat, inventory, and disconnect behavior.
4. An owned-server conformance suite runs normal client behavior against pinned Paper and open-source anti-cheat artifacts. Server alerts fail the suite. The suite exists to find invalid protocol ordering, state handling, collision, and mechanics. It does not inspect check thresholds or develop evasions.

Microsoft authentication tests mock HTTP boundaries. A manual opt-in test can verify a real device-code login without storing credentials in the repository or CI.

Fuzz tests cover framing, primitive decoding, generated packet decoding, NBT, and registry input parsing. The race detector runs against lifecycle, subscription, state, and action tests.

## Release criteria

The first release is ready when:

- Java 1.8 protocol 47 and Java 26.1 protocol 775 reproduce from their pinned manifests,
- every resolved PrismarineJS dataset for both built-in versions reproduces from its pinned source,
- the server and proxy use `minecraft-protocol` instead of importing protocol code from the server,
- offline and Microsoft-authenticated clients can enter play on the current stable server,
- the high-level actions listed above have fixture or end-to-end coverage,
- unknown optional packets do not crash the client,
- malformed input does not panic or allocate beyond configured limits, and
- formatting, linting, unit tests, race tests, generation checks, and builds pass locally.
