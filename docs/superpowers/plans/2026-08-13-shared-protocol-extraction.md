# Shared Protocol Extraction Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

> [!NOTE]
> Tasks 1 through 3 describe the pre-foundation package layout. The repository
> foundation and Java wire extraction are complete. Use
> `2026-08-13-immutable-game-data-contracts.md` for the game-data contract
> milestone, then resume this plan at Task 4.

## Execution status

| Work | Status | Evidence |
| --- | --- | --- |
| Task 1, repository foundation | Complete | `minecraft-protocol` commit `eee7c1c` |
| Task 2, Java wire primitives | Complete | `minecraft-protocol` commit `1b545bc` |
| Task 3, immutable game-data contracts | Complete through the replacement milestone | `minecraft-protocol` commit `e2e840d` and `2026-08-13-immutable-game-data-contracts.md` |
| Task 4, Java 1.8 generated data | Complete | `minecraft-protocol` commit `ad0f2ca` and `2026-08-14-java-1-8-generated-data.md` |
| Tasks 5 through 8 | Pending | Start only after the refreshed Task 4 plan passes |

The pinned PrismarineJS `pc/1.8` snapshot reports Minecraft `1.8.8` in
`version.json`. The server target is Minecraft `1.8.9`. Both use protocol 47.
The generated `data.Version` preserves the source value, while the built-in
factory uses the stable target key `java/1.8.9`.

**Goal:** Create `github.com/go-theft-craft/minecraft-protocol`, move the tested Java 1.8 wire and game-data code into it, and migrate `server` and `proxy` without changing runtime behavior.

**Architecture:** The shared module owns edition-neutral protocol contracts, Java wire primitives, immutable game-data registries, the PrismarineJS generator, and generated Java 1.8 data. Server and proxy depend on the shared module through temporary local `replace` directives while the repositories are developed together.

**Tech Stack:** Go 1.26.5, Devbox with `openserbia/go-flake`, direnv, Task, gofumpt, gci, golangci-lint v2, PrismarineJS minecraft-data, standard library testing.

## Global Constraints

- Work in `/home/ocharnyshevich/pet.projects/go-theft-craft`.
- Create the shared repository at `/home/ocharnyshevich/pet.projects/go-theft-craft/minecraft-protocol` with branch `main`.
- Keep design and plan files under `docs/` ignored by Git.
- Run project commands only as `devbox run -- task <name>`.
- Commit `.envrc`, `devbox.json`, `devbox.lock`, and `Taskfile.yml` in each new repository.
- Use `eval "$(devbox generate direnv --print-envrc)"` in `.envrc`.
- Leave all changes uncommitted unless the user explicitly asks for commits.
- Preserve server protocol 47 behavior before adding current-version support.
- Keep `proxy/internal/legacy` and its generated files in the proxy repository.
- Do not delete server code until both migrated consumers pass tests.

---

### Task 1: Bootstrap the shared repository and toolchain

**Files:**
- Create: `minecraft-protocol/.gitignore`
- Create: `minecraft-protocol/.envrc`
- Create: `minecraft-protocol/go.mod`
- Create: `minecraft-protocol/devbox.json`
- Create: `minecraft-protocol/Taskfile.yml`
- Create: `minecraft-protocol/.golangci.yml`
- Create: `minecraft-protocol/internal/buildcheck/buildcheck_test.go`

**Interfaces:**
- Produces: module `github.com/go-theft-craft/minecraft-protocol` and tasks `deps`, `fmt`, `lint`, `test`, `test:race`, `build`, and `verify`.

- [ ] **Step 1: Initialize the repository and add the failing build check**

Run `git init -b main minecraft-protocol`. Add a test that imports the packages planned for later tasks:

```go
package buildcheck_test

import (
	"testing"

	"github.com/go-theft-craft/minecraft-protocol/data"
	"github.com/go-theft-craft/minecraft-protocol/protocol"
	javawire "github.com/go-theft-craft/minecraft-protocol/wire/java"
)

func TestPublicPackagesExist(t *testing.T) {
	var _ protocol.Packet
	var _ data.Set
	_ = javawire.DefaultLimits()
}
```

- [ ] **Step 2: Add the pinned development environment**

Use `openserbia/go-flake` pins for Go `1.26.5`, golangci-lint `2.12.2`, gofumpt `0.10.0`, govulncheck `1.6.0`, gopls `0.22.0`, and Delve `1.27.0`. Use nixpkgs packages only for go-task and gci. Add this `.envrc`:

```bash
#!/bin/bash

eval "$(devbox generate direnv --print-envrc)"
```

Ignore `build/`, `coverage.out`, `vendor/`, `.env`, and `docs/`.

- [ ] **Step 3: Add Task targets**

Make `task verify` depend on `lint`, `test`, `test:race`, and `build`. Task 4 adds `generate:check` after generation exists. All Go commands use `-mod=vendor` after `task deps` creates `vendor/modules.txt`.

- [ ] **Step 4: Verify the expected failure**

Run `devbox run -- task test`.

Expected: compilation fails because `data`, `protocol`, and `wire/java` do not exist.

- [ ] **Step 5: Record the checkpoint**

Run `git -C minecraft-protocol status --short` and inspect the complete diff. Do not commit.

### Task 2: Port Java wire primitives with explicit limits

**Files:**
- Create: `minecraft-protocol/protocol/packet.go`
- Create: `minecraft-protocol/protocol/version.go`
- Create: `minecraft-protocol/wire/java/limits.go`
- Create: `minecraft-protocol/wire/java/varint.go`
- Create: `minecraft-protocol/wire/java/fields.go`
- Create: `minecraft-protocol/wire/java/marshal.go`
- Create: `minecraft-protocol/wire/java/frame.go`
- Create: `minecraft-protocol/wire/java/varint_test.go`
- Create: `minecraft-protocol/wire/java/marshal_test.go`
- Create: `minecraft-protocol/wire/java/frame_test.go`

**Interfaces:**
- Produces: `protocol.Packet`, `protocol.Version`, `java.DefaultLimits`, `java.ReadVarInt`, `java.WriteVarInt`, `java.Marshal`, `java.Unmarshal`, `java.ReadRawPacket`, `java.WriteRawPacket`, `java.ReadPacket`, and `java.WritePacket`.

- [ ] **Step 1: Define packet and version contracts**

```go
package protocol

type Packet interface {
	PacketID() int32
}

type Version struct {
	Name     string
	Protocol int32
}
```

- [ ] **Step 2: Write boundary tests before porting code**

Test VarInt round trips for `0`, `1`, `127`, `128`, `math.MaxInt32`, and `-1`. Test rejection of a six-byte VarInt. Test a declared frame length of `MaxFrameSize+1`. Test short reads with a reader that returns one byte per call.

```go
func TestReadRawPacketRejectsOversizedFrame(t *testing.T) {
	var input bytes.Buffer
	_, _ = WriteVarInt(&input, DefaultLimits().MaxFrameSize+1)
	_, _, err := ReadRawPacket(&input, DefaultLimits())
	if !errors.Is(err, ErrFrameTooLarge) {
		t.Fatalf("expected ErrFrameTooLarge, got %v", err)
	}
}
```

- [ ] **Step 3: Run tests and verify failure**

Run `devbox run -- task test -- -run 'Test(ReadRawPacket|VarInt)' ./wire/java`.

Expected: failure because the Java wire functions do not exist.

- [ ] **Step 4: Port the existing implementation and add limits**

Port behavior from `server/pkg/protocol`. Replace the hard-coded `1<<21` check with:

```go
type Limits struct {
	MaxFrameSize int32
	MaxStringLen int32
	MaxArrayLen  int32
}

func DefaultLimits() Limits {
	return Limits{MaxFrameSize: 2 << 20, MaxStringLen: 32767, MaxArrayLen: 1 << 20}
}
```

Use `io.ReadFull` for declared payload lengths. Ensure `WriteRawPacket` uses a full-write helper so a short `Write` cannot truncate a frame silently.

- [ ] **Step 5: Run focused and race tests**

Run `devbox run -- task test -- ./wire/java` and `devbox run -- task test:race -- ./wire/java`.

Expected: all Java wire tests pass.

- [ ] **Step 6: Record the checkpoint**

Inspect `git -C minecraft-protocol diff --check` and `git -C minecraft-protocol status --short`. Do not commit.

### Task 3: Port immutable game-data contracts

**Files:**
- Create: `minecraft-protocol/data/set.go`
- Create: `minecraft-protocol/data/registry.go`
- Create: `minecraft-protocol/data/block.go`
- Create: `minecraft-protocol/data/item.go`
- Create: `minecraft-protocol/data/entity.go`
- Create: `minecraft-protocol/data/biome.go`
- Create: `minecraft-protocol/data/effect.go`
- Create: `minecraft-protocol/data/enchantment.go`
- Create: `minecraft-protocol/data/food.go`
- Create: `minecraft-protocol/data/particle.go`
- Create: `minecraft-protocol/data/instrument.go`
- Create: `minecraft-protocol/data/attribute.go`
- Create: `minecraft-protocol/data/window.go`
- Create: `minecraft-protocol/data/material.go`
- Create: `minecraft-protocol/data/recipe.go`
- Create: `minecraft-protocol/data/collision_shape.go`
- Create: `minecraft-protocol/data/version.go`
- Create: `minecraft-protocol/data/loader.go`
- Create: `minecraft-protocol/data/loader_test.go`

**Interfaces:**
- Produces: `data.Set`, typed registry interfaces, `data.Register`, `data.Load`, and `data.RegisteredVersions`.

- [ ] **Step 1: Write loader isolation tests**

Test duplicate registration rejection, sorted version names, unknown-version errors, and factory isolation. `Load` must return distinct `*Set` values for two calls.

```go
func TestRegisteredVersionsSorted(t *testing.T) {
	registry := NewRegistry()
	require.NoError(t, registry.Register("java/b", func() *Set { return &Set{} }))
	require.NoError(t, registry.Register("java/a", func() *Set { return &Set{} }))
	assert.Equal(t, []string{"java/a", "java/b"}, registry.Versions())
}
```

Use standard-library comparisons if adding `testify` would be the only external dependency.

- [ ] **Step 2: Run the tests and verify failure**

Run `devbox run -- task test -- ./data`.

Expected: failure because the registry types do not exist.

- [ ] **Step 3: Port and tighten the data model**

Port the public structs and registry interfaces from `server/pkg/gamedata`. Rename `GameData` to `Set`. Make registration return an error on duplicate keys. Sort `RegisteredVersions`. Keep returned slices and maps isolated from internal generated storage.

- [ ] **Step 4: Run focused tests**

Run `devbox run -- task test -- ./data`.

Expected: all data tests pass.

- [ ] **Step 5: Record the checkpoint**

Run `git -C minecraft-protocol diff --check`. Do not commit.

### Task 4: Move the Java 1.8 generator and reproduce generated data

> [!NOTE]
> This task's original contracts predate the named ID and collection types in
> `data`. Execute `2026-08-14-java-1-8-generated-data.md` instead. That plan
> defines manifest validation, caller-owned generated registries, the current
> `data.Factory` signature, deterministic generation, and the release gate.

**Files:**
- Create: `minecraft-protocol/cmd/mcdata-gen/main.go`
- Create: `minecraft-protocol/internal/codegen/schema/types.go`
- Create: `minecraft-protocol/internal/codegen/generator/generator.go`
- Create: `minecraft-protocol/internal/codegen/generator/templates/*.tmpl`
- Create: `minecraft-protocol/internal/codegen/generator/generator_test.go`
- Create: `minecraft-protocol/source/java/1.8/*.json`
- Create: `minecraft-protocol/source/java/1.8/manifest.json`
- Create: `minecraft-protocol/generated/java/v1_8/*.go`
- Modify: `minecraft-protocol/Taskfile.yml`

**Interfaces:**
- Consumes: `data` contracts and Java wire package.
- Produces: `mcdata-gen` flags `-source`, `-out`, `-package`, and `-version`, plus `generated/java/v1_8.Data()`.

- [ ] **Step 1: Copy the pinned Java 1.8 source and generator tests**

Copy the files from `server/scheme/pc-1.8` into `source/java/1.8`. Add a manifest with edition `java`, Minecraft version `1.8.9`, protocol `47`, source repository `https://github.com/PrismarineJS/minecraft-data`, and SHA-256 for every copied file.

Write a golden test that generates into `t.TempDir()` and compares every file with `generated/java/v1_8`.

- [ ] **Step 2: Run the golden test and verify failure**

Run `devbox run -- task test -- ./internal/codegen/generator`.

Expected: failure because the generator has not been ported.

- [ ] **Step 3: Port the generator and templates**

Move schema and template behavior from `server/cmd/codegen`. Change imports to the shared module. Generate package `v1_8`, register it as `java/1.8.9`, and expose:

```go
func Data() *data.Set
func Version() protocol.Version
```

Keep generated files deterministic. Do not include local paths or wall-clock timestamps in Go output.

- [ ] **Step 4: Add generation tasks**

`task generate` writes `generated/java/v1_8`. `task generate:check` generates into a temporary directory and fails on any diff. Both tasks validate the source manifest before generation.

- [ ] **Step 5: Generate and test**

Run `devbox run -- task generate`, `devbox run -- task generate:check`, and `devbox run -- task test -- ./generated/java/v1_8 ./internal/codegen/generator`.

Expected: generated output matches and all tests pass.

- [ ] **Step 6: Record the checkpoint**

Inspect all generated changes and manifest checksums. Do not commit.

### Task 5: Add the edition-neutral protocol descriptor

**Files:**
- Modify: `minecraft-protocol/protocol/version.go`
- Create: `minecraft-protocol/protocol/protocol.go`
- Create: `minecraft-protocol/protocol/registry.go`
- Create: `minecraft-protocol/protocol/registry_test.go`
- Create: `minecraft-protocol/generated/java/v1_8/protocol_descriptor.go`

**Interfaces:**
- Produces: `protocol.Edition`, `protocol.Role`, `protocol.Codec`, `protocol.Protocol`, and registry lookup by stable ID.

- [ ] **Step 1: Write registry tests**

Test stable ID `java/1.8.9`, edition `java`, protocol number `47`, client and server codec creation, duplicate registration rejection, and unknown lookup.

- [ ] **Step 2: Define the contracts**

```go
type Edition string

const EditionJava Edition = "java"

type Role uint8

const (
	Client Role = iota + 1
	Server
)

type Protocol interface {
	ID() string
	Edition() Edition
	Version() Version
	NewCodec(role Role) Codec
	Data() *data.Set
}
```

`Codec` exposes packet creation and payload encode/decode by state and direction. It does not own I/O or goroutines.

- [ ] **Step 3: Implement and test the Java 1.8 descriptor**

Run `devbox run -- task test -- ./protocol ./generated/java/v1_8`.

Expected: all registry and descriptor tests pass.

- [ ] **Step 4: Run the shared verification gate**

Run `devbox run -- task verify`.

Expected: generation, formatting, lint, tests, race tests, and build pass.

### Task 6: Migrate the server to the shared module

**Files:**
- Modify: `server/go.mod`
- Modify: `server/Taskfile.yml`
- Modify: `server/Taskfile.codegen.yml`
- Modify: all server Go files importing `github.com/go-theft-craft/server/pkg/protocol`
- Modify: all server Go files importing `github.com/go-theft-craft/server/pkg/gamedata`
- Test: existing tests under `server/internal/server`, `server/internal/server/conn`, `server/internal/server/player`, and `server/pkg/world`

**Interfaces:**
- Consumes: shared `wire/java`, `data`, and `generated/java/v1_8` packages.
- Produces: server behavior with no imports of its local protocol or game-data packages.

- [ ] **Step 1: Add the local shared dependency**

Add:

```go
require github.com/go-theft-craft/minecraft-protocol v0.0.0

replace github.com/go-theft-craft/minecraft-protocol => ../minecraft-protocol
```

- [ ] **Step 2: Migrate imports without deleting old packages**

Map local imports as follows:

```text
server/pkg/protocol                         -> minecraft-protocol/wire/java
server/pkg/gamedata                         -> minecraft-protocol/data
server/pkg/gamedata/versions/pc_1_8         -> minecraft-protocol/generated/java/v1_8
```

Rename identifiers only where `GameData` became `Set`.

- [ ] **Step 3: Update server generation tasks**

Replace the local codegen command with `go run github.com/go-theft-craft/minecraft-protocol/cmd/mcdata-gen` or call `task generate` in the sibling repository through an explicit `PROTOCOL_DIR` variable. Do not hard-code an absolute path.

- [ ] **Step 4: Refresh dependencies and run tests**

Run `devbox run -- task deps`, `devbox run -- task fmt`, `devbox run -- task lint`, `devbox run -- task test`, and `devbox run -- task build` in `server`.

Expected: all commands pass while the old packages still exist but have no importers.

- [ ] **Step 5: Prove local packages have no consumers**

Run:

```bash
rg -n 'github.com/go-theft-craft/server/pkg/(protocol|gamedata)' server --glob '*.go'
```

Expected: no matches.

### Task 7: Migrate the proxy wire imports

**Files:**
- Modify: `proxy/go.mod`
- Modify: `proxy/internal/legacy/io.go`
- Modify: `proxy/internal/legacy/packet.go`
- Modify: `proxy/internal/legacy/packets_itemstack.go`
- Test: `proxy/internal/legacy/*_test.go`

**Interfaces:**
- Consumes: `github.com/go-theft-craft/minecraft-protocol/wire/java` primitives.
- Preserves: custom legacy codec and generated packet packages.

- [ ] **Step 1: Add the local shared dependency**

Add the same `v0.0.0` requirement and `../minecraft-protocol` replacement used by the server.

- [ ] **Step 2: Replace only shared primitive imports**

Change imports of `github.com/go-theft-craft/server/pkg/protocol` to `github.com/go-theft-craft/minecraft-protocol/wire/java`. Do not move or rewrite `proxy/internal/legacy`.

- [ ] **Step 3: Run the legacy codec tests first**

Run `devbox run -- task test -- ./internal/legacy/...` in `proxy`.

Expected: legacy codec and generated packet tests pass unchanged.

- [ ] **Step 4: Run the full proxy gate**

Run `devbox run -- task deps`, `devbox run -- task lint`, `devbox run -- task test`, and `devbox run -- task build`.

Expected: all commands pass.

### Task 8: Remove duplicate server code and verify all repositories

**Files:**
- Delete: `server/pkg/protocol/`
- Delete: `server/pkg/gamedata/`
- Delete: `server/cmd/codegen/`
- Delete: `server/cmd/dmd/`
- Delete: `server/scheme/pc-1.8/`
- Modify: `server/README.md`
- Modify: `server/CLAUDE.md`
- Modify: `proxy/CLAUDE.md`

**Interfaces:**
- Produces: a single owner for shared protocol and data code.

- [ ] **Step 1: Delete only proven-unreferenced duplicates**

Remove the listed directories after Tasks 6 and 7 pass. Preserve unrelated server world and proxy legacy packages.

- [ ] **Step 2: Update repository guidance**

Document that shared Java protocols and PrismarineJS data live in `minecraft-protocol`. Document the exact `devbox run -- task generate` and `generate:check` commands there. Keep legacy generation instructions in proxy.

- [ ] **Step 3: Run all verification gates**

Run `devbox run -- task verify` in `minecraft-protocol`, then run lint, test, and build tasks in `server` and `proxy`.

Expected: every command passes and no deleted package import remains.

- [ ] **Step 4: Inspect final scope**

Run `git status --short` separately in all three repositories. Confirm that only shared extraction, import migration, tooling, and documentation changes are present. Do not commit.
