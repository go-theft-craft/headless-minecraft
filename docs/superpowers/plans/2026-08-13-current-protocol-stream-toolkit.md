# Current Protocol and Stream Toolkit Implementation Plan

> **Status: complete, 2026-08-18.** This umbrella shipped in `minecraft-protocol`
> as M1 (managed stream and compression) and M2 (encryption and login
> lifecycle), under those repositories' own plans. The checkboxes below were
> never ticked and are not evidence of anything; do not re-run this plan. What
> the work found is in
> [the archived master plan](../../archive/2026-08-18-master-plan.md).

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Generate the PrismarineJS Java 26.1 data family, protocol 775, validate it against a server running the current 26.1.2 game build, and provide safe optional stream, routing, capture, login, and CLI helpers.

**Architecture:** Pure generated codecs operate on packet payloads without owning I/O. Optional stream and Java helpers add bounded concurrency, framing, compression, encryption, status, and login. The generator discovers all upstream datasets from a pinned PrismarineJS revision and retains unknown datasets as raw JSON.

**Tech Stack:** Go 1.26.5 from `openserbia/go-flake`, Devbox, Task, PrismarineJS minecraft-data commit `8a80816cbfb3fe2b609f2cde4e57796c8033af61`, standard library networking, zlib, AES, RSA, JSON, and testing.

## Global Constraints

- Complete the shared extraction plan first.
- Work in `/home/ocharnyshevich/pet.projects/go-theft-craft/minecraft-protocol`.
- Run commands only as `devbox run -- task <name>`.
- Leave changes uncommitted unless explicitly requested.
- Generate PrismarineJS Java 26.1, protocol 775, and validate compatibility against a Minecraft Java Edition 26.1.2 server.
- Pin PrismarineJS by full commit and verify every source file with SHA-256.
- Import every resolved upstream dataset, including filenames unknown to typed code.
- Keep pure codecs free of goroutines, contexts, and transport ownership.
- Bound every frame, decompression output, NBT value, collection, and queue.
- Never drop outbound packets silently.
- Keep Bedrock namespaces reserved but do not implement Bedrock in this plan.

---

### Task 1: Build the pinned source manifest and alias resolver

**Files:**
- Create: `source/manifest/manifest.go`
- Create: `source/manifest/manifest_test.go`
- Create: `internal/sourcefetch/fetch.go`
- Create: `internal/sourcefetch/fetch_test.go`
- Create: `cmd/mcproto/main.go`
- Create: `cmd/mcproto/data.go`
- Modify: `Taskfile.yml`

**Interfaces:**
- Produces: `manifest.Manifest`, `sourcefetch.Fetch(ctx, Options)`, `mcproto data fetch`, and `mcproto data validate`.

- [ ] **Step 1: Write manifest validation tests**

Cover duplicate dataset names, path traversal, invalid SHA-256, an alias cycle, a missing alias target, and a successful multi-version alias chain.

```go
type Dataset struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type Manifest struct {
	Edition         string    `json:"edition"`
	Minecraft       string    `json:"minecraft_version"`
	Protocol        int32     `json:"protocol_version"`
	Repository      string    `json:"repository"`
	Commit          string    `json:"commit"`
	Datasets        []Dataset `json:"datasets"`
}
```

- [ ] **Step 2: Run the focused test and verify failure**

Run `devbox run -- task test -- ./source/manifest ./internal/sourcefetch`.

Expected: failure because manifest and fetch packages do not exist.

- [ ] **Step 3: Implement deterministic fetching**

Fetch only commit `8a80816cbfb3fe2b609f2cde4e57796c8033af61`. Resolve Prismarine `dataPaths.json` aliases for `pc/26.1`. Write each resolved source file under `source/java/26.1/data/`, including `proto.yml`, then write `manifest.json` after all hashes pass. Record a media type and SHA-256 for every file. Stage output in a temporary directory and rename it only after validation.

- [ ] **Step 4: Add CLI and Task commands**

Support:

```text
mcproto data fetch --edition java --version 26.1 --protocol 775 --ref 8a80816cbfb3fe2b609f2cde4e57796c8033af61 --output source/java/26.1
mcproto data validate --manifest source/java/26.1/manifest.json
```

Add `task data:fetch` and `task data:validate` with the same defaults.

- [ ] **Step 5: Fetch and validate**

Run `devbox run -- task data:fetch` and `devbox run -- task data:validate`.

Expected: the manifest names every resolved file, hashes match, and a second fetch produces no diff.

### Task 2: Extend `data.Set` for complete and future datasets

**Files:**
- Modify: `data/set.go`
- Modify: `data/registry.go`
- Create: `data/raw.go`
- Create: `data/raw_test.go`
- Create: `data/loot.go`
- Create: `data/command.go`
- Create: `data/login_packet.go`
- Create: `data/map_icon.go`
- Create: `data/sound.go`
- Create: `data/tint.go`

**Interfaces:**
- Produces: `data.RawDataset`, `(*data.Set).Raw(name string) (RawDataset, bool)`, `DatasetNames() []string`, and typed immutable registries for all known Prismarine datasets.

- [ ] **Step 1: Write raw-data ownership tests**

Verify that `Raw` returns a copy, `DatasetNames` is sorted, unknown names return false, and typed and raw access can coexist for the same source dataset.

- [ ] **Step 2: Implement raw fallback storage**

```go
type Set struct {
	// existing typed registries
	raw map[string]RawDataset
}

type RawDataset struct {
	Name      string
	Path      string
	MediaType string
	Data      []byte
}

func (s *Set) Raw(name string) (RawDataset, bool)
func (s *Set) DatasetNames() []string
```

Copy bytes both when constructing the set and when returning them.

- [ ] **Step 3: Add typed models from the manifest**

Add typed models for `blockLoot`, `commands`, `entityLoot`, `loginPacket`, `mapIcons`, `sounds`, and `tints`. Keep `proto.yml` and every typed source available through `Raw` as well. The manifest has 25 dataset keys: attributes, biomes, blockCollisionShapes, blockLoot, blocks, commands, effects, enchantments, entities, entityLoot, foods, instruments, items, language, loginPacket, mapIcons, materials, particles, proto, protocol, recipes, sounds, tints, version, and windows.

- [ ] **Step 4: Run data tests**

Run `devbox run -- task test -- ./data`.

Expected: typed registry and raw fallback tests pass.

### Task 3: Parse ProtoDef schemas into a stable intermediate model

**Files:**
- Create: `internal/codegen/protodef/ast.go`
- Create: `internal/codegen/protodef/parser.go`
- Create: `internal/codegen/protodef/parser_test.go`
- Create: `internal/codegen/protodef/testdata/*.json`

**Interfaces:**
- Produces: `protodef.Parse(json.RawMessage) (*Schema, error)` with named types, containers, arrays, switches, options, buffers, mappers, and packet registries.

- [ ] **Step 1: Add one fixture per ProtoDef construct**

Each fixture contains the smallest schema that uses one construct. Tests assert exact AST values and errors with JSON paths such as `types.packet_play.fields[2]`.

- [ ] **Step 2: Run parser tests and verify failure**

Run `devbox run -- task test -- ./internal/codegen/protodef`.

Expected: failure because the parser does not exist.

- [ ] **Step 3: Implement parsing without code generation**

Use explicit JSON decoding into tagged intermediate nodes. Reject unknown type operators unless the caller enables `AllowUnknown`, in which case preserve the raw node for raw-packet generation.

- [ ] **Step 4: Parse both built-in protocols**

Run parser tests against `source/java/1.8/protocol.json` and `source/java/26.1/data/protocol.json`.

Expected: both schemas parse with no unclassified construct.

### Task 4: Generate reflection-free packet codecs

**Files:**
- Create: `internal/codegen/packetgen/generator.go`
- Create: `internal/codegen/packetgen/templates/*.tmpl`
- Create: `internal/codegen/packetgen/generator_test.go`
- Create: `protocol/limits.go`
- Create: `protocol/limits_test.go`
- Modify: `protocol/protocol.go`
- Modify: `cmd/mcdata-gen/main.go`

**Interfaces:**
- Produces: generated packet structs with `Encode(*wire.Buffer) error`, `Decode(*wire.Buffer) error`, packet metadata, and factories by role, state, and ID.

- [ ] **Step 1: Write golden output for representative packets**

Cover fixed primitives, nested containers, optional values, arrays, switches, NBT, remaining bytes, named aliases, limits at the version boundary, and attempts to disable or exceed hard ceilings. Assert that generated files do not import `reflect`.

- [ ] **Step 2: Add shared buffer primitives**

Create bounded read and write methods used by generated code. Every length-prefixed method accepts effective `Limits` and returns a path-aware decode error. Compute effective values from schema-requested limits clamped by non-disableable process ceilings for frames, decompression, strings, collections, NBT, plugin payloads, recursion, and queues. Custom protocols may request stricter values or larger values within the ceilings, but cannot select zero-as-unlimited.

- [ ] **Step 3: Generate concrete codecs**

Generated packets implement:

```go
type Encodable interface {
	Encode(*wire.Buffer) error
}

type Decodable interface {
	Decode(*wire.Buffer) error
}
```

Factories allocate the exact generated packet type for a state, direction, and ID. Unknown IDs produce `protocol.UnknownPacket` with an owned payload.

- [ ] **Step 4: Replace Java 1.8 reflection use**

Regenerate Java 1.8 with concrete codecs. Keep compatibility wrappers in `wire/java` until server tests pass, then remove reflection from shared runtime paths.

- [ ] **Step 5: Run generator and consumer tests**

Run `devbox run -- task generate`, `generate:check`, and `test`. Then run server and proxy tests.

Expected: no generated diff after a second run, no `reflect` import in generated packages, and consumers remain green.

### Task 5: Generate Java 26.1 and the current alias

**Files:**
- Create: `generated/java/v26_1/*.go`
- Create: `generated/java/current/current.go`
- Create: `generated/java/v26_1/protocol_test.go`
- Modify: `Taskfile.yml`

**Interfaces:**
- Produces: `v26_1.Protocol() protocol.Protocol`, `v26_1.Data() *data.Set`, and `current.Protocol()`.

- [ ] **Step 1: Add version assertions before generation**

Test stable ID `java/26.1`, edition `java`, protocol `775`, every manifest dataset in `DatasetNames`, and packet factories for handshake, status, login, configuration, and play.

- [ ] **Step 2: Run tests and verify failure**

Run `devbox run -- task test -- ./generated/java/v26_1 ./generated/java/current`.

Expected: packages do not exist.

- [ ] **Step 3: Generate the version package**

Generate all typed data, raw datasets, packet structs, registries, codec, and descriptor from the pinned manifest. Make `current` delegate to `v26_1` without copying generated data. Add a fixture-server compatibility test whose reported game version is that of the server under test and whose protocol is 775.

- [ ] **Step 4: Verify reproduction**

Run `devbox run -- task generate:check`, `data:validate`, and `test`.

Expected: all version assertions pass.

### Task 6: Implement Java framing, compression, encryption, and limits

**Files:**
- Create: `wire/java/decoder.go`
- Create: `wire/java/encoder.go`
- Create: `wire/java/compression.go`
- Create: `wire/java/encryption.go`
- Create: `wire/java/codec_test.go`
- Create: `wire/java/fuzz_test.go`

**Interfaces:**
- Produces: `java.Decoder.Read(io.Reader)`, `java.Encoder.Write(io.Writer, protocol.Packet)`, `SetCompression`, and `EnableEncryption`.

- [ ] **Step 1: Add fragmented and hostile input tests**

Test one-byte reads, short writes, multiple frames in one input, EOF at every byte, oversized compressed and decompressed lengths, invalid zlib streams, state changes, and encryption transitions between complete packets.

- [ ] **Step 2: Implement synchronous codecs**

Use `io.ReadFull`, full writes, bounded zlib readers, separate inbound and outbound AES stream state, and owned payloads. The codec owns no goroutine and never closes caller I/O.

- [ ] **Step 3: Add fuzz targets**

Seed framing, VarInt, compression, and generated packet decoders with valid fixtures. Fuzzers must return errors rather than panic and must stay within configured allocation limits.

- [ ] **Step 4: Run tests and fuzz smoke tests**

Run `devbox run -- task test -- ./wire/java` and the Task target `fuzz:smoke` for each fuzz function with a fixed short duration.

### Task 7: Implement the optional managed stream

**Files:**
- Create: `stream/session.go`
- Create: `stream/options.go`
- Create: `stream/queue.go`
- Create: `stream/errors.go`
- Create: `stream/session_test.go`

**Interfaces:**
- Produces: `stream.New`, `(*Session).Run`, `Send`, `SendControl`, `TrySend`, `Packets`, `Close`, and `Wait`.

- [ ] **Step 1: Write lifecycle and backpressure tests**

Use `net.Pipe` and fragmented wrappers. Test concurrent senders, FIFO within normal and control queues, control capacity under a full normal queue, cancellation while enqueueing, partial write failure, idempotent close, and slow packet consumers.

- [ ] **Step 2: Implement one reader and one writer owner**

Use `errgroup.WithContext`. The reader decodes and publishes owned packets. The writer is the only goroutine that encodes. Closing the transport unblocks both loops. A fatal loop error cancels the group and becomes `Wait`'s result.

- [ ] **Step 3: Implement explicit overload behavior**

`Send` waits, `TrySend` returns `ErrBackpressure`, and packets are never dropped. The inbound stream closes with `ErrBackpressure` if its sole consumer stops draining and the configured policy is `FailSession`.

- [ ] **Step 4: Run race and leak tests**

Run `devbox run -- task test:race -- ./stream`.

Expected: no races and every test observes all owned goroutines exit.

### Task 8: Add router, middleware, and capture helpers

**Files:**
- Create: `router/router.go`
- Create: `router/router_test.go`
- Create: `middleware/middleware.go`
- Create: `middleware/chain_test.go`
- Create: `capture/format.go`
- Create: `capture/writer.go`
- Create: `capture/reader.go`
- Create: `capture/capture_test.go`

**Interfaces:**
- Produces: ordered router handlers, send and receive middleware chains, and versioned capture read/write APIs.

- [ ] **Step 1: Test ordering and isolation**

Verify exact handler order, cancellation, panic containment, middleware nesting, payload ownership, capture truncation detection, and deterministic capture round trips.

- [ ] **Step 2: Implement small composable interfaces**

```go
type Sender interface {
	Send(context.Context, protocol.Packet) error
}

type SendMiddleware func(Sender) Sender
type Handler func(context.Context, protocol.Packet) error
```

Do not require `stream.Session`. Adapters can wrap any compatible sender.

- [ ] **Step 3: Run focused tests**

Run `devbox run -- task test -- ./router ./middleware ./capture`.

### Task 9: Add Java status and login helpers

**Files:**
- Create: `java/status/status.go`
- Create: `java/status/status_test.go`
- Create: `java/login/client.go`
- Create: `java/login/server.go`
- Create: `java/login/options.go`
- Create: `java/login/login_test.go`
- Create: `mctest/peer.go`

**Interfaces:**
- Produces: `status.Ping(ctx, Dialer, Address, Protocol)`, client and server login flows, and an in-process fixture peer.

- [ ] **Step 1: Write transcript tests**

Cover status ping, offline login, compression negotiation, RSA shared-secret negotiation, configuration registry exchange, known disconnects, and context cancellation at every phase.

- [ ] **Step 2: Implement helpers over public interfaces**

Accept caller-supplied dialers, random sources, authentication callbacks, and protocol instances. Do not import headless client code. Return only after the requested terminal state is reached.

- [ ] **Step 3: Run transcript and race tests**

Run `devbox run -- task test:race -- ./java/status ./java/login ./mctest`.

### Task 10: Complete the automation-friendly `mcproto` CLI

**Files:**
- Modify: `cmd/mcproto/main.go`
- Create: `cmd/mcproto/generate.go`
- Create: `cmd/mcproto/version.go`
- Create: `cmd/mcproto/packet.go`
- Create: `cmd/mcproto/capture.go`
- Create: `cmd/mcproto/cli_test.go`
- Modify: `Taskfile.yml`

**Interfaces:**
- Produces: data, generation, diff, packet encode/decode, and capture inspect/replay commands with JSON output and stdin support.

- [ ] **Step 1: Add black-box CLI tests**

Test every command's `--help`, one successful JSON invocation, missing-flag error with a valid example, stdin input, deterministic output, and `generate --check` exit status on a modified golden file.

- [ ] **Step 2: Implement subcommands**

Use standard `flag.FlagSet` instances with no interactive prompts. Support `--input -`, `--output -`, and `--format json`. Generation writes through a temporary directory. Replay defaults to a dry run and requires `--connect` for network output.

- [ ] **Step 3: Add Task wrappers**

Make every protocol maintenance task call `mcproto` rather than duplicate logic in YAML.

- [ ] **Step 4: Run the full release gate**

Run `devbox run -- task verify`.

Expected: source validation, generation check, formatting, lint, tests, race tests, fuzz smoke tests, and builds pass.

- [ ] **Step 5: Inspect final scope**

Run `git status --short` and `git diff --check`. Confirm that Bedrock has no implementation, all 26.1 datasets appear in the manifest, the 26.1.2 compatibility fixture passes, and no generated runtime package imports `reflect`. Do not commit.
