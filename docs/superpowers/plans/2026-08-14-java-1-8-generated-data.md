# Java 1.8 generated data implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `subagent-driven-development` or execute this plan inline one task at a time. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Port the PrismarineJS Java 1.8 generator and its pinned source snapshot into `minecraft-protocol`, then publish a deterministic, caller-owned protocol 47 data set under `java/1.8.9`.

**Architecture:** `source/java/1.8` stores the reviewed JSON snapshot and a checksum manifest. `internal/codegen` validates and parses the snapshot, and `cmd/mcdata-gen` renders deterministic Go files under `generated/java/v1_8`. Generated registries implement the immutable `data` interfaces and return owned values from every lookup.

**Tech Stack:** Go 1.26.5, the standard library, embedded Go templates, Devbox, Task, and the existing `data`, `protocol`, and `wire/java` packages.

## Status

Complete in `minecraft-protocol` commit `ad0f2ca`. The commit is local and was not pushed by this execution.

## Global constraints

- Work in `/home/ocharnyshevich/pet.projects/go-theft-craft/minecraft-protocol` on `main`, as previously authorized.
- Run repository commands through `devbox run -- task <name>`.
- Copy source bytes from `/home/ocharnyshevich/pet.projects/go-theft-craft/server/scheme/pc-1.8` without rewriting them.
- Copy only the 17 JSON inputs consumed by the generator. Do not copy `proto.yml`.
- Record SHA-256 checksums in `source/java/1.8/manifest.json` and reject missing, extra, or changed JSON files before generation.
- Treat `java/1.8.9` as the stable target key. Preserve `1.8.8` from `version.json` in `data.Version`; both identify protocol 47 data.
- Use every named ID and collection type from `data`. Do not emit primitive substitutes such as `map[int]float64`, `[]data.Packet`, or `[][]data.Ingredient` where a named type exists.
- Generated registry lookups and `All` methods return caller-owned values. Mutable values must use their `Clone` methods.
- `Data` has signature `func Data() (*data.Set, error)` so it can be registered directly as a `data.Factory`.
- Generated package initialization must not discard registration errors. Panic with version context if `data.Register` fails.
- Keep output deterministic. Do not write absolute paths, timestamps, random values, or map iteration order into generated files.
- Preserve packet field names, `mc` tags, packet IDs, recipe metadata normalization, and metadata terminator `0x7F` from the server generator.
- Do not add a protocol descriptor or migrate consumers in this milestone. Those remain Tasks 5 and 6 of the extraction plan.
- Preserve unrelated work. Create one final commit after task reviews and the full release gate pass. Do not push.

---

### Task 1: Pin the Java 1.8 source snapshot

**Files:**

- Create: `source/java/1.8/attributes.json`
- Create: `source/java/1.8/biomes.json`
- Create: `source/java/1.8/blockCollisionShapes.json`
- Create: `source/java/1.8/blocks.json`
- Create: `source/java/1.8/effects.json`
- Create: `source/java/1.8/enchantments.json`
- Create: `source/java/1.8/entities.json`
- Create: `source/java/1.8/foods.json`
- Create: `source/java/1.8/instruments.json`
- Create: `source/java/1.8/items.json`
- Create: `source/java/1.8/language.json`
- Create: `source/java/1.8/materials.json`
- Create: `source/java/1.8/particles.json`
- Create: `source/java/1.8/protocol.json`
- Create: `source/java/1.8/recipes.json`
- Create: `source/java/1.8/version.json`
- Create: `source/java/1.8/windows.json`
- Create: `source/java/1.8/manifest.json`

**Interfaces:**

- Consumes: the existing server snapshot at `server/scheme/pc-1.8`.
- Produces: an immutable source directory and manifest consumed by Task 2.

- [x] **Step 1: Copy the source bytes**

Copy the 17 JSON files listed above. Do not copy `proto.yml` or modify JSON formatting.

- [x] **Step 2: Add the manifest**

Use this schema:

```json
{
  "edition": "java",
  "targetMinecraftVersion": "1.8.9",
  "sourceMinecraftVersion": "1.8.8",
  "protocol": 47,
  "sourceRepository": "https://github.com/PrismarineJS/minecraft-data",
  "files": {
    "attributes.json": "7b44f604ed7c806f2f1ef717cf6c5576ccd7859461fe82e6426dae8c344c41e3"
  }
}
```

Include all 17 files in `files`, sorted by filename. Use the SHA-256 of the copied bytes.

- [x] **Step 3: Verify byte identity and checksums**

Run `cmp` for every copied file and run `sha256sum source/java/1.8/*.json`. Compare the output with `manifest.json`. Assert that `version.json` contains protocol `47`, Minecraft version `1.8.8`, and major version `1.8`.

- [x] **Step 4: Inspect the source-only scope**

Run `git diff --check` and `git status --short`. Expected: only `source/java/1.8` is new. Do not commit.

### Task 2: Port the generator and produce immutable registries

**Files:**

- Create: `cmd/mcdata-gen/main.go`
- Create: `internal/codegen/schema/types.go`
- Create: `internal/codegen/generator/generator.go`
- Create: `internal/codegen/generator/manifest.go`
- Create: `internal/codegen/generator/templates/*.tmpl`
- Create: `internal/codegen/generator/generator_test.go`
- Create: `generated/java/v1_8/*.go`
- Create: `generated/java/v1_8/data_test.go`

**Interfaces:**

- Consumes: Task 1's manifest and JSON files, `data.Factory`, named `data` IDs and collections, and `wire/java.PacketValue`.
- Produces: CLI flags `-source`, `-out`, `-package`, and `-version`; deterministic generation; `func Data() (*data.Set, error)`; `func Version() protocol.Version`; packet structs; and automatic registration under `java/1.8.9`.

- [x] **Step 1: Write failing manifest and generation tests**

Add tests that copy the source directory into `t.TempDir`, then assert that validation rejects a changed checksum, a missing JSON file, and an extra JSON file. Add a golden test that generates into another temporary directory and compares the relative file list and bytes with `generated/java/v1_8`.

Add focused parser tests for a block drop object, string harvest-tool IDs, a recipe ingredient with null metadata, log metadata normalization, protocol packet IDs and fields, and sorted map-backed output.

- [x] **Step 2: Write failing generated-data tests**

Test these exact public facts:

- `Version()` returns name `1.8.9` and protocol `47`.
- `Data()` returns a set whose `Version()` contains source Minecraft version `1.8.8`, major version `1.8`, and protocol `47`.
- Registry counts are 198 blocks, 336 items, 58 entities, 62 biomes, 23 effects, 25 enchantments, 28 foods, 42 particles, 5 instruments, 12 attributes, 14 windows, and 8 materials.
- Block ID `1` is `stone`, item ID `276` is `diamond_sword`, and both lookup and `All` results remain unchanged after caller mutation.
- Collision shapes, recipes, language, and protocol values remain unchanged after caller mutation.
- Two `Data()` calls return distinct sets and independent registry values.
- `data.Load("java/1.8.9")` succeeds after package initialization.
- Generated `ChatCB` and `ChatSB` satisfy `wire/java.PacketValue`; their packet IDs are `0x02` and `0x01` respectively, and their `mc` tags remain `string`, `i8`, and `string`.

Run `devbox run -- task test -- ./internal/codegen/generator ./generated/java/v1_8`. Expected: compilation fails because the generator and generated package do not exist.

- [x] **Step 3: Port schema parsing and manifest validation**

Port the server schema and generator transformations. Rename `SchemeDir` to `SourceDir`. Validate the manifest before reading generator inputs. Reject an unsupported edition, target version, source version, protocol number, malformed checksum, missing file, extra JSON file, or checksum mismatch with contextual errors.

- [x] **Step 4: Port templates to the current contracts**

Import `github.com/go-theft-craft/minecraft-protocol/data`. Emit named IDs and named collections in fields, registry indexes, lookup signatures, and `All` results. Mutable lookup results call `Clone`; immutable scalar-only values may return directly. Every `All` method returns the named collection's `Clone`.

Generate `Data` by calling `data.NewSet`. Generate this registration policy:

```go
func init() {
	if err := data.Register("java/1.8.9", Data); err != nil {
		panic(fmt.Errorf("register Java 1.8.9 data: %w", err))
	}
}
```

Generate `Version()` as `protocol.Version{Name: "1.8.9", Protocol: 47}`. Keep `MetadataEnd` equal to `0x7F`.

- [x] **Step 5: Implement the CLI**

Require `-source`, `-out`, `-package`, and `-version`. `-version` is the stable registration key `java/1.8.9`; validate its `java` edition and `1.8.9` target against the manifest. Reject empty or mismatched values before generation. Pass them to `generator.Run`. Print relative input and output paths only.

- [x] **Step 6: Generate and verify**

Generate `generated/java/v1_8`, run the focused generator and generated-package tests, run `devbox run -- task test -- ./...`, run `devbox run -- task lint`, and run `git diff --check`. Expected: all pass. Do not commit.

### Task 3: Add generation gates and document the built-in

**Files:**

- Modify: `Taskfile.yml`
- Modify: `README.md`
- Modify: `CHANGELOG.md`
- Modify: `docs/superpowers/plans/2026-08-13-shared-protocol-extraction.md`

**Interfaces:**

- Consumes: Task 2's CLI and checked-in output.
- Produces: `task generate`, `task generate:check`, a release gate that detects stale output, and accurate built-in support documentation.

- [x] **Step 1: Add generation tasks**

`generate` runs:

```bash
go run ./cmd/mcdata-gen -source ./source/java/1.8 -out ./generated/java -package v1_8 -version java/1.8.9
```

`generate:check` creates a temporary directory, runs the same command with that directory as `-out`, and compares its `v1_8` directory with `generated/java/v1_8`. Exclude `*_test.go` because `data_test.go` is hand-written and the generator preserves but does not create it. Remove the temporary directory through a shell trap. Add `generate:check` to `verify` before lint and tests.

- [x] **Step 2: Test stale-output detection**

Run `devbox run -- task generate:check` and expect success. Change one byte in a copied temporary generated directory and run the comparison command against it. Expect a nonzero status without modifying checked-in output.

- [x] **Step 3: Update documentation and plan status**

Mark Java 1.8 protocol 47 generated data as implemented in the README while keeping its protocol descriptor planned. Document `task generate` and `task generate:check`. Add an Unreleased changelog entry for the pinned source, generator, generated registries, packet values, and registration key.

Mark the refreshed Task 4 plan complete in the extraction plan. Leave Tasks 5 through 8 pending.

- [x] **Step 4: Run release gates**

Run `devbox run -- task verify` in `minecraft-protocol`. Run `unset GOROOT; devbox run -- task test` in `server` to confirm that the source repository remains unchanged. Run `git diff --check`, `git status --short`, and `git diff --stat` in both repositories.

- [x] **Step 5: Review and commit**

After every task review and the final whole-change review pass, stage only this milestone. Run `devbox run -- task precommit`, then commit with:

```bash
git commit -m "feat: generate Java 1.8 game data"
```

Do not push.

## Completion criteria

- The copied source files match the server snapshot byte for byte.
- The manifest rejects missing, extra, and changed JSON files.
- Generation is deterministic and `generate:check` detects stale output.
- Generated code uses named IDs and collections throughout.
- Every generated registry returns caller-owned data.
- Java 1.8 packet structs still implement `wire/java.PacketValue`.
- `Data()` returns fresh immutable sets and registers under `java/1.8.9`.
- Source metadata remains `1.8.8`, and the public target version is `1.8.9`.
- The protocol release gate and unchanged server test gate pass.
- The reviewed milestone is committed once and not pushed.

## Final review amendments

The whole-change review found source-fidelity and safety requirements that the
initial task breakdown missed. Completion also requires all of these checks:

- Validate `-package` as one Go identifier and prove that it cannot escape or delete outside `-out`.
- Validate the exact 17-file inventory, use the verified bytes for parsing, render in a temporary sibling directory, and replace the last good output only after every file succeeds.
- Model entity lookup keys without overwriting mob and object IDs or duplicate names.
- Preserve the duplicate biome 161 records without making `All` disagree with lookup results.
- Preserve recipe `outShape`, including nullable cells, and test cake's three returned buckets.
- Preserve fractional drop values and distinguish omitted values from explicit zero.
- Do not expose legacy unframed server-list ping through the normal framed `PacketValue` API.
- Reject malformed or incomplete protocol mappings instead of generating packet ID zero or empty definitions.
- Make `generate:check` compare the explicit generated-file inventory and exempt only `data_test.go`.
- Document how an application activates generated-package registration and qualify the remaining planned PrismarineJS datasets.
- Record verifiable upstream revision and license provenance, and include the required third-party notice before committing copied data.
- Document `data.Protocol` as a summary unless this milestone starts preserving its complete structured type definitions.
