# Minecraft Reference Extraction Implementation Plan

> **Status: complete, 2026-08-18.** Shipped as the `minecraft-reference`
> repository, released at `v1.0.1`, with `mcreference dump`, a family catalog
> covering 1.0 through 26.2, and a weekly maintenance workflow. The boxes
> below are ticked by outcome, checked against that repository on 2026-08-18.
> Do not re-run this plan.

> **For agentic workers:** REQUIRED SUB-SKILL: Use `subagent-driven-development` (recommended) or execute this plan inline one task at a time. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a repeatable local workflow that downloads, verifies, maps, decompiles, and indexes the official Java Edition 1.8.9 and 26.1.2 client and server artifacts, then map the complete simulation behavior planned for `minecraft-simulation`.

**Architecture:** A small Go command resolves Mojang version metadata, verifies every artifact, applies the version-specific naming strategy, invokes pinned Java tools, and writes all restricted material below one ignored directory. Tracked YAML catalogs map simulation domains to exact classes, methods, descriptors, call relationships, numeric behavior, and conformance scenarios. The later simulation plan consumes the tracked catalogs and prose findings, never the decompiled sources.

**Tech Stack:** Go 1.26.6 selected by `go.mod` from the Go 1.26.5 Devbox bootstrap, Devbox, Task, OpenJDK 25, Mojang version metadata, MCP 1.8.9 stable 22 mappings, SpecialSource 1.11.4, Vineflower 1.12.0, `javap`, gofumpt, gci, golangci-lint v2, govulncheck, and gitleaks.

## Global constraints

- Create and use `/home/ocharnyshevich/pet.projects/go-theft-craft/minecraft-simulation`.
- Run the complete preparation flow with `devbox run -- task reference:prepare VERSIONS=1.8.9,26.1.2 SIDES=client,server REFERENCE_DIR=reference/work`.
- Accept `VERSIONS`, `SIDES`, and `REFERENCE_DIR` as Task variables. Do not require shell environment variables.
- Resolve game downloads from `https://piston-meta.mojang.com/mc/game/version_manifest_v2.json`.
- Verify the version JSON and every Mojang artifact with the SHA-1 and size from Mojang metadata.
- Pin all non-Mojang tools and legacy mappings by URL and SHA-256 in tracked configuration.
- Reject an unsupported version. Do not guess a mapping strategy.
- Use MCP stable 22 plus the MCP 1.8.9 SRG archive for Java 1.8.9.
- Use identity names for Java 26.1.2 because its distributed classes use readable names and its metadata contains no separate mappings.
- Extract the executable server jar when the downloaded server is a Mojang bundler.
- Download Java libraries required for analysis. Do not download assets, native classifiers, logging configuration, or game resources.
- Write downloaded jars, mappings, extracted classes, remapped jars, decompiled Java, bytecode dumps, and generated symbol indexes only below `REFERENCE_DIR`.
- Resolve `REFERENCE_DIR` to an absolute path and reject `/`, the repository root, any parent of the repository, and any path outside the repository.
- Keep `reference/work/` ignored. Never commit or publish game jars, decompiled sources, mappings, Mojang assets, or derived source archives.
- Write downloads through a temporary file, verify them, then rename them into the cache.
- Reuse a cached file only after verifying its size and digest.
- Do not execute downloaded Minecraft classes.
- Invoke only the pinned SpecialSource and Vineflower entry points.
- Store only provenance, independently written behavior descriptions, symbol identities, original fixtures, and expected outputs in tracked files.
- Map every simulation domain approved in the design, not every renderer, UI, network, data-generation, or unrelated game method.
- A mapped method requires an owner class, method name, JVM descriptor, side, behavior role, callers or callees, version, evidence path, and review status.
- Check arithmetic-sensitive methods against `javap -c -p -s` output. Decompiled Java alone is not enough evidence for casts and operation order.
- Leave changes uncommitted unless the user explicitly asks for a commit.

## Output layout

The workflow owns this layout:

```text
minecraft-simulation/
  cmd/mcreference/                    CLI entry point
  internal/reference/artifact/        Mojang metadata and verified downloads
  internal/reference/mapping/         Naming strategies and MCP conversion
  internal/reference/decompile/       Server extraction and Vineflower runner
  internal/reference/index/           javap and source symbol indexes
  internal/reference/catalog/         Catalog validation and coverage reports
  reference/config/tools.json         Pinned tool URLs and SHA-256 values
  reference/config/versions.json      Supported versions and naming strategies
  reference/schema/catalog.schema.json
  reference/catalog/domains.yaml      Required simulation-domain inventory
  reference/catalog/java-1.8.9.yaml   Reviewed 1.8.9 symbols
  reference/catalog/java-26.1.2.yaml  Reviewed 26.1.2 symbols
  reference/catalog/comparison.yaml   Cross-version equivalence decisions
  reference/notes/                     Tracked prose behavior findings
  reference/work/                      Ignored restricted workspace
    cache/                              Verified original downloads and tools
    versions/<version>/<side>/         Original, mapped, and executable jars
    sources/<version>/<side>/          Decompiled Java
    bytecode/<version>/<side>/         javap output for reviewed methods
    index/<version>/<side>/             Generated complete symbol indexes
    reports/                            Generated coverage and comparison reports
```

## Established vanilla workflow

The sibling-repository check found no reusable vanilla workflow: `proxy`
targets the custom legacy client, while `server/cmd/dmd` downloads PrismarineJS
data rather than game jars. Keep both out of this workflow and implement the
documented vanilla pipeline directly:

1. Resolve and verify the original client and server artifacts from Mojang's
   version metadata.
2. For obfuscated releases, remap the jar before decompilation. Java 1.8.9 uses
   the MCP 1.8.9 SRG and stable-name exports, following the MCP/RetroMCP-era
   workflow.
3. For unobfuscated releases, decompile the distributed executable jar without
   a remapping stage. Mojang states that releases after the obfuscation cutoff
   retain readable class, method, field, and variable names.
4. Decompile the readable jar with the pinned Vineflower CLI and supply the
   downloaded Java libraries as external classpath entries.
5. Treat JVM bytecode and descriptors as authoritative when decompiled source
   is ambiguous.

References:

- [Mojang Java version manifest](https://piston-meta.mojang.com/mc/game/version_manifest_v2.json)
- [RetroMCP-Java usage for legacy releases](https://github.com/MCPHackers/RetroMCP-Java#using)
- [Vanilla remap-then-decompile workflow](https://blog.bithole.dev/blogposts/decompiling-minecraft/)
- [Mojang announcement removing Java Edition obfuscation](https://www.minecraft.net/en-us/article/removing-obfuscation-in-java-edition)
- [Vineflower command-line usage](https://vineflower.org/usage/)

---

### Task 1: Create the repository and restricted workspace boundary

**Files:**

- Create: `minecraft-simulation/go.mod`
- Create: `minecraft-simulation/.envrc`
- Create: `minecraft-simulation/devbox.json`
- Create: `minecraft-simulation/Taskfile.yml`
- Create: `minecraft-simulation/.golangci.yml`
- Create: `minecraft-simulation/.gitignore`
- Create: `minecraft-simulation/.githooks/pre-commit`
- Create: `minecraft-simulation/.github/workflows/ci.yml`
- Create: `minecraft-simulation/LICENSE`
- Create: `minecraft-simulation/README.md`
- Create: `minecraft-simulation/doc.go`
- Create: `minecraft-simulation/internal/buildcheck/reference_test.go`

**Interfaces:**

- Produces: module `github.com/go-theft-craft/minecraft-simulation`, the standard verification tasks, OpenJDK 25, and an enforced ignored workspace.

- [x] **Step 1: Initialize the repository**

Run from `/home/ocharnyshevich/pet.projects/go-theft-craft`:

```bash
mkdir minecraft-simulation
git -C minecraft-simulation init -b main
```

Create `go.mod`:

```go
module github.com/go-theft-craft/minecraft-simulation

go 1.26.6
```

- [x] **Step 2: Add the shared Go workflow and Java runtime**

Copy the Go and tool pins from `minecraft-protocol`. Add `javaPackages.compiler.openjdk25@25.0.2+10`. Change the gci module prefix to `github.com/go-theft-craft/minecraft-simulation`. Add focused test arguments:

```yaml
test:
  desc: Run unit tests with the race detector
  deps: [deps]
  cmds:
    - go test -race -covermode=atomic -coverprofile=coverage.out {{.CLI_ARGS | default "./..."}}
```

- [x] **Step 3: Ignore every restricted artifact form**

Add:

```gitignore
/reference/work/
*.jar
*.class
*.java
*.tiny
*.tsrg
*.srg
*.csrg
```

Do not ignore tracked Markdown, YAML, JSON configuration, or original conformance fixtures below `reference/`.

- [x] **Step 4: Enforce the boundary in tests**

Walk `git ls-files -z` and fail for any path below `reference/work` or any forbidden extension. Also fail if a tracked file begins with ZIP or Java class magic bytes.

```go
var forbiddenExtensions = map[string]struct{}{
	".jar": {}, ".class": {}, ".java": {}, ".tiny": {},
	".tsrg": {}, ".srg": {}, ".csrg": {},
}
```

- [x] **Step 5: Verify the empty foundation**

Run `devbox run -- task fmt`, `devbox run -- task verify`, and `git status --short`.

Expected: all checks pass. No restricted file is tracked. Do not commit.

### Task 2: Resolve and download verified Mojang artifacts

**Files:**

- Create: `minecraft-simulation/cmd/mcreference/main.go`
- Create: `minecraft-simulation/cmd/mcreference/prepare.go`
- Create: `minecraft-simulation/internal/reference/artifact/types.go`
- Create: `minecraft-simulation/internal/reference/artifact/manifest.go`
- Create: `minecraft-simulation/internal/reference/artifact/download.go`
- Create: `minecraft-simulation/internal/reference/artifact/path.go`
- Create: `minecraft-simulation/internal/reference/artifact/manifest_test.go`
- Create: `minecraft-simulation/internal/reference/artifact/download_test.go`
- Create: `minecraft-simulation/internal/reference/artifact/path_test.go`
- Create: `minecraft-simulation/reference/config/versions.json`

**Interfaces:**

- Produces: `artifact.Resolve(ctx, version)`, `artifact.Download(ctx, spec, cache)`, safe `REFERENCE_DIR` resolution, and `mcreference prepare` argument parsing.

- [x] **Step 1: Write metadata and path safety tests**

Use `httptest.Server` fixtures. Cover an unknown version, duplicate manifest IDs, a version JSON SHA-1 mismatch, an artifact size mismatch, an artifact SHA-1 mismatch, interrupted download cleanup, cache revalidation, missing client or server metadata, and unsafe output paths.

```go
func TestResolveReferenceDirRejectsRepositoryRoot(t *testing.T) {
	_, err := artifact.ResolveReferenceDir(repoRoot, repoRoot)
	if !errors.Is(err, artifact.ErrUnsafeReferenceDir) {
		t.Fatalf("got %v, want ErrUnsafeReferenceDir", err)
	}
}
```

- [x] **Step 2: Define supported versions explicitly**

Create:

```json
{
  "versions": [
    {"id":"1.8.9","java":8,"naming":"mcp-stable-22"},
    {"id":"26.1.2","java":25,"naming":"identity"}
  ]
}
```

Reject any version absent from this file even when Mojang lists it.

- [x] **Step 3: Resolve Mojang metadata**

Read `version_manifest_v2.json`, find the exact ID, verify the version JSON SHA-1, then decode `downloads.client`, `downloads.server`, and `libraries[].downloads.artifact`. Use the URL and digest from metadata. Resolve old library entries without an explicit URL against `https://libraries.minecraft.net/`.

- [x] **Step 4: Download only analysis inputs**

Download the requested client, server, and Java library artifacts. Skip assets, native classifiers, log configuration, and platform rules that do not produce Java classpath jars. Write `manifest.lock.json` under the ignored version directory with URL, size, SHA-1, and computed SHA-256 for every file.

- [x] **Step 5: Add CLI validation**

Support:

```text
mcreference prepare --versions 1.8.9,26.1.2 --sides client,server --reference-dir reference/work
```

Require at least one version and one side. Accept only `client` and `server`. Resolve and validate the output directory before network access.

- [x] **Step 6: Run focused tests**

Run `devbox run -- task test -- ./internal/reference/artifact ./cmd/mcreference`.

Expected: metadata, digest, cache, and path-safety tests pass. Do not contact Mojang in unit tests. Do not commit.

### Task 3: Pin tools and implement both naming strategies

**Files:**

- Create: `minecraft-simulation/reference/config/tools.json`
- Create: `minecraft-simulation/internal/reference/mapping/config.go`
- Create: `minecraft-simulation/internal/reference/mapping/mcp.go`
- Create: `minecraft-simulation/internal/reference/mapping/specialsource.go`
- Create: `minecraft-simulation/internal/reference/mapping/identity.go`
- Create: `minecraft-simulation/internal/reference/mapping/mapping_test.go`
- Create: `minecraft-simulation/internal/reference/decompile/server.go`
- Create: `minecraft-simulation/internal/reference/decompile/server_test.go`

**Interfaces:**

- Consumes: verified downloaded jars and the configured naming strategy.
- Produces: one analysis jar per version and side with stable readable names.

- [x] **Step 1: Pin every external tool and legacy mapping**

Create `tools.json` with these exact entries:

```json
{
  "tools": [
    {
      "id": "vineflower-1.12.0",
      "url": "https://repo.maven.apache.org/maven2/org/vineflower/vineflower/1.12.0/vineflower-1.12.0.jar",
      "sha256": "1dfcfe974395734fa467ce620661c7623d05ba83670de0529b1fbd63ff548b9d"
    },
    {
      "id": "specialsource-1.11.4",
      "url": "https://repo.maven.apache.org/maven2/net/md-5/SpecialSource/1.11.4/SpecialSource-1.11.4-shaded.jar",
      "sha256": "e2cab24b1c12400ad73b15972bb21e4273a0dc7081c8b3c136ddfdd824c78518"
    },
    {
      "id": "mcp-1.8.9-srg",
      "url": "https://mcp.zeith.org/mcp/1.8.9/mcp-1.8.9-srg.zip",
      "sha256": "a9d6afe0e3bdb4da77a62d7cc79750c7cf53b3f0bc6cc5157f191008d0134558"
    },
    {
      "id": "mcp-stable-22-1.8.9",
      "url": "https://mcp.zeith.org/mcp_stable/22-1.8.9/mcp_stable-22-1.8.9.zip",
      "sha256": "aeed0aaba9d159b7ce60a21e2dcc36adb249fade65ce2f76c730dd0ec7270763"
    }
  ]
}
```

- [x] **Step 2: Write legacy mapping composition tests**

Use small SRG, `methods.csv`, and `fields.csv` fixtures. Verify class, field, and method replacement. Verify overloaded methods by descriptor. Reject a duplicate stable name, a missing SRG member, invalid CSV, invalid descriptors, and output that depends on zip entry order.

- [x] **Step 3: Compose the final 1.8.9 mapping**

Extract `joined.srg`, `methods.csv`, and `fields.csv`. Replace SRG member names with stable MCP names while retaining owner paths and JVM descriptors. Write the composed SRG only below `REFERENCE_DIR`.

- [x] **Step 4: Remap both 1.8.9 sides**

Invoke only:

```text
java -jar SpecialSource-1.11.4-shaded.jar --in-jar <original.jar> --out-jar <mapped.jar> --srg-in <composed.srg>
```

Capture the command, Java version, tool digest, mapping digests, input digest, and output digest in the ignored lock manifest.

- [x] **Step 5: Handle 26.1.2 server bundling and identity names**

If `META-INF/versions.list` exists, parse its tab-separated digest, path, and version fields. Extract the selected inner server jar and verify its declared digest. If no bundler manifest exists, use the downloaded server jar directly. Copy neither jar into a tracked path. Use the executable server jar and client jar as identity-mapped analysis jars.

- [x] **Step 6: Verify mappings without executing game code**

List mapped jar entries and use `javap -p -s` on a fixed sample. For 1.8.9, require readable mapped owners for the entity, world, AABB, item entity, and arrow classes. For 26.1.2, require readable `net.minecraft` owners. Fail before decompilation if the checks do not pass.

- [x] **Step 7: Run focused tests**

Run `devbox run -- task test -- ./internal/reference/mapping ./internal/reference/decompile`.

Expected: both naming strategies and server extraction pass fixture tests. Do not commit.

### Task 4: Decompile, index, and expose one preparation command

**Files:**

- Create: `minecraft-simulation/internal/reference/decompile/vineflower.go`
- Create: `minecraft-simulation/internal/reference/decompile/vineflower_test.go`
- Create: `minecraft-simulation/internal/reference/index/javap.go`
- Create: `minecraft-simulation/internal/reference/index/source.go`
- Create: `minecraft-simulation/internal/reference/index/types.go`
- Create: `minecraft-simulation/internal/reference/index/index_test.go`
- Modify: `minecraft-simulation/cmd/mcreference/prepare.go`
- Modify: `minecraft-simulation/Taskfile.yml`

**Interfaces:**

- Consumes: named analysis jars and verified library classpaths.
- Produces: decompiled sources, complete class and method indexes, bytecode lookup, and the single `reference:prepare` Task command.

- [x] **Step 1: Write runner and index tests**

Use a tiny compiled fixture jar. Verify argument boundaries for paths with spaces, deterministic output paths, captured tool versions, method descriptors, constructors, static initializers, bridge methods, synthetic methods, and overloaded methods.

- [x] **Step 2: Invoke Vineflower with a fixed option set**

Invoke the pinned jar as an argument array, never through a shell. Use a
deterministic thread count of one, folder output, and preserve bridge and
synthetic members for research. Pass each downloaded library with
`--add-external`. Record the exact argument list in the ignored lock manifest.

```text
java -jar <vineflower.jar> --folder --thread-count=1 --remove-bridge=false --remove-synthetic=false --bytecode-source-mapping=true --add-external=<library.jar> <analysis.jar> <sources-dir>
```

If Vineflower changes or rejects an option, fail and require an explicit tracked tool update. Do not silently retry with defaults.

- [x] **Step 3: Generate complete JVM symbol indexes**

Enumerate every class in the analysis jar. Invoke `javap -p -s` in bounded batches. Write sorted JSON Lines records with version, side, owner, member kind, name, descriptor, access flags, and source path. Generate a second source index with declarations and line ranges from decompiled Java.

Implement source-tree walking locally from jar entries and Java package
declarations. Do not infer overload identity from source text; join source
locations to the authoritative `javap` descriptor index.

- [x] **Step 4: Add bytecode extraction for reviewed symbols**

Support:

```text
mcreference bytecode --version 1.8.9 --side client --owner net.minecraft.entity.Entity --method moveEntity --descriptor '(DDD)V' --reference-dir reference/work
```

Invoke `javap -c -p -s` and write the result below `reference/work/bytecode`. Reject ambiguous overloads unless the descriptor is present.

- [x] **Step 5: Add the one-command Task target**

```yaml
reference:prepare:
  desc: Download, map, decompile, and index Minecraft reference artifacts
  requires:
    vars: [VERSIONS]
  vars:
    SIDES: '{{.SIDES | default "client,server"}}'
    REFERENCE_DIR: '{{.REFERENCE_DIR | default "reference/work"}}'
  cmds:
    - >-
      go run ./cmd/mcreference prepare
      --versions '{{.VERSIONS}}'
      --sides '{{.SIDES}}'
      --reference-dir '{{.REFERENCE_DIR}}'
```

- [x] **Step 6: Run the complete local preparation**

Run:

```bash
devbox run -- task reference:prepare VERSIONS=1.8.9,26.1.2 SIDES=client,server REFERENCE_DIR=reference/work
```

Expected: four decompiled source trees, four complete indexes, four verified lock manifests, and no tracked restricted artifact.

- [x] **Step 7: Prove cache idempotence**

Run the same command again. Expected: every verified download and completed stage reports a cache hit. The second run does not change a file digest.

### Task 5: Define the complete simulation-domain catalog

**Files:**

- Create: `minecraft-simulation/reference/schema/catalog.schema.json`
- Create: `minecraft-simulation/reference/catalog/domains.yaml`
- Create: `minecraft-simulation/reference/catalog/java-1.8.9.yaml`
- Create: `minecraft-simulation/reference/catalog/java-26.1.2.yaml`
- Create: `minecraft-simulation/reference/catalog/comparison.yaml`
- Create: `minecraft-simulation/internal/reference/catalog/types.go`
- Create: `minecraft-simulation/internal/reference/catalog/load.go`
- Create: `minecraft-simulation/internal/reference/catalog/validate.go`
- Create: `minecraft-simulation/internal/reference/catalog/coverage.go`
- Create: `minecraft-simulation/internal/reference/catalog/catalog_test.go`
- Modify: `minecraft-simulation/cmd/mcreference/main.go`
- Modify: `minecraft-simulation/Taskfile.yml`

**Interfaces:**

- Produces: one reviewed map of the complete planned simulation behavior, validated against generated JVM indexes.

- [x] **Step 1: Define a method record that cannot hide ambiguity**

Each mapped symbol has this shape:

```yaml
- id: java.entity.base.move
  domain: entity-motion
  version: 1.8.9
  side: client
  owner: net.minecraft.entity.Entity
  method: moveEntity
  descriptor: "(DDD)V"
  source_lines: "Entity.java:640-812"
  bytecode_file: "bytecode/1.8.9/client/net.minecraft.entity.Entity.moveEntity.(DDD)V.txt"
  role: "clips requested movement against block collision boxes"
  calls: [java.collision.aabb.offset-x]
  called_by: [java.entity.living.travel]
  numeric_notes: "double arithmetic; Y is clipped before X and Z"
  random_streams: []
  fixtures: [player-wall, player-step, player-ceiling]
  status: reviewed
```

Allow `status` values `candidate`, `reviewed`, `verified`, and `excluded`. Require an exclusion reason for `excluded`.

- [x] **Step 2: Enumerate every approved domain**

`domains.yaml` contains these required domains:

```text
tick-order, scheduling, random-ticks, numeric-compatibility, random-sources,
geometry, aabb-collision, voxel-shapes, ray-casting, world-queries,
entity-lifecycle, entity-motion, entity-pushing, mounting, passengers,
living-motion, player-ground, player-air, sprinting, sneaking, jumping,
climbing, swimming, gliding, flying, no-clip, aquatic-motion, fluid-occupancy,
fluid-flow, fluid-displacement, currents, buoyancy, dropped-items,
item-merging, item-pickup, item-despawn, experience-orbs, projectiles,
throwables, arrows, tridents, fishing-bobbers, projectile-hits, boats,
minecarts, falling-blocks, primed-tnt, explosions, damage, knockback, fire,
suffocation, drowning, hunger, regeneration, status-effects, attributes,
scheduled-block-ticks, block-neighbor-updates, pistons, redstone, crop-growth,
portals, weather, dimensions, world-border, spawning, despawning,
particles, sounds, animations
```

Mark each domain as `first-slice` or `later`. Do not omit later roadmap work from the catalog.

- [x] **Step 3: Validate catalogs against symbol indexes**

For every non-excluded record, require an exact owner, method, and descriptor match in the chosen version and side index. Require unique IDs, existing call references, valid line ranges, an existing bytecode file for arithmetic-sensitive methods, and at least one fixture for `verified` status.

- [x] **Step 4: Add coverage commands**

Support:

```text
mcreference catalog candidates --domain player-ground --version 1.8.9 --side client --reference-dir reference/work
mcreference catalog check --reference-dir reference/work
mcreference catalog report --reference-dir reference/work
```

Candidate search uses names, descriptors, bytecode constants, source tokens,
and one-hop calls. It writes reports below the ignored `reports` directory and
never edits tracked catalogs. Candidate discovery is local to the vanilla jar.
Do not accept any candidate as a reviewed mapping without an exact JVM symbol
and human review.

- [x] **Step 5: Add Task targets and tests**

Add `reference:candidates`, `reference:check`, and `reference:report`. Run `devbox run -- task test -- ./internal/reference/catalog`.

Expected: schema, exact-symbol validation, call references, and coverage calculations pass fixture tests. Do not commit.

### Task 6: Map and review Java 1.8.9 simulation behavior

**Files:**

- Modify: `minecraft-simulation/reference/catalog/java-1.8.9.yaml`
- Create: `minecraft-simulation/reference/notes/java-1.8.9/tick-and-world.md`
- Create: `minecraft-simulation/reference/notes/java-1.8.9/entity-and-living.md`
- Create: `minecraft-simulation/reference/notes/java-1.8.9/blocks-and-fluids.md`
- Create: `minecraft-simulation/reference/notes/java-1.8.9/projectiles-vehicles-explosions.md`
- Create: `minecraft-simulation/reference/notes/java-1.8.9/effects-and-presentation.md`

**Interfaces:**

- Consumes: mapped 1.8.9 client and server sources, bytecode, symbol indexes, and the required-domain catalog.
- Produces: a reviewed symbol and behavior map for all planned simulation domains in Java 1.8.9.

- [x] **Step 1: Map tick, world, collision, and random behavior**

Trace the client and server tick entry points through scheduling, world queries, AABB clipping, stepping, ray casting, fluid queries, block updates, and random calls. Add exact descriptors and one-hop call relationships. Record client-only and server-only ownership explicitly.

- [x] **Step 2: Map entity families and environmental consequences**

Trace base entities, living entities, players, aquatic mobs, items, experience orbs, projectiles, fishing bobbers, boats, minecarts, falling blocks, and primed TNT. Map movement, lifecycle, collisions, pushing, mounting, passengers, spawning, removal, merging, pickup, and despawning.

- [x] **Step 3: Map blocks, fluids, and world transitions**

Trace water and lava behavior, scheduled ticks, random ticks, neighbor updates, pistons, redstone, crop growth, fire, portals, weather, dimensions, and the world border. Mark absent mechanics as `excluded` with `not present in Java 1.8.9`.

- [x] **Step 4: Map damage, effects, and presentation outputs**

Trace explosions, damage, knockback, suffocation, drowning, hunger, regeneration, attributes, status effects, particles, sounds, and animations. Distinguish a state transition from an emitted presentation request.

- [x] **Step 5: Check arithmetic-sensitive bytecode**

Generate and review bytecode for every mapped method that changes position, velocity, collision bounds, damage, explosion exposure, fluid height, or random state. Record casts, branch comparisons, constant types, and operation order in prose notes.

- [x] **Step 6: Pass the 1.8.9 catalog gate**

Run:

```bash
devbox run -- task reference:check REFERENCE_DIR=reference/work
devbox run -- task reference:report REFERENCE_DIR=reference/work
```

Expected: every domain has at least one reviewed symbol or one reviewed exclusion for each applicable side. Every first-slice arithmetic method has bytecode evidence.

### Task 7: Map Java 26.1.2 and decide cross-version ownership

**Files:**

- Modify: `minecraft-simulation/reference/catalog/java-26.1.2.yaml`
- Modify: `minecraft-simulation/reference/catalog/comparison.yaml`
- Create: `minecraft-simulation/reference/notes/java-26.1.2/tick-and-world.md`
- Create: `minecraft-simulation/reference/notes/java-26.1.2/entity-and-living.md`
- Create: `minecraft-simulation/reference/notes/java-26.1.2/blocks-and-fluids.md`
- Create: `minecraft-simulation/reference/notes/java-26.1.2/projectiles-vehicles-explosions.md`
- Create: `minecraft-simulation/reference/notes/java-26.1.2/effects-and-presentation.md`
- Create: `minecraft-simulation/reference/notes/java/comparison.md`

**Interfaces:**

- Consumes: the 26.1.2 sources and the completed 1.8.9 catalog.
- Produces: full 26.1.2 coverage and explicit decisions about shared or version-owned Go rules.

- [x] **Step 1: Repeat the complete domain review for 26.1.2**

Map every required domain with the same evidence requirements as Task 6. Record new mechanics and changed ownership instead of forcing them into 1.8.9 concepts.

- [x] **Step 2: Compare methods by behavior, not names**

For each domain, compare phase order, inputs, outputs, calls, constants, numeric types, RNG streams, and side ownership. A similar class or method name does not prove equivalent behavior.

- [x] **Step 3: Classify every comparison**

Use exactly these classifications:

```text
shared-identical
shared-parameterized
java-1.8.9-specific
java-26.1.2-specific
client-specific
server-specific
absent
unresolved
```

Each `shared-identical` record names a conformance scenario that must prove identical output. Each `shared-parameterized` record names the parameters and their source. An `unresolved` record blocks the related implementation task.

- [x] **Step 4: Produce the first-slice handoff list**

List the exact symbols and findings required for geometry, AABB collision, stepping, player ground and air movement, sprinting, sneaking, jumping, climbing, swimming, fluid occupancy, external impulses, dropped-item motion, and arrow motion.

- [x] **Step 5: Run the complete catalog gate**

Run `devbox run -- task reference:check REFERENCE_DIR=reference/work` and `devbox run -- task reference:report REFERENCE_DIR=reference/work`.

Expected: all domains have reviewed coverage for both versions, all first-slice comparisons have no `unresolved` record, and every shared-code decision names a future fixture.

### Task 8: Verify publication safety and hand off to simulation implementation

**Files:**

- Modify: `minecraft-simulation/README.md`
- Create: `minecraft-simulation/reference/README.md`
- Create: `minecraft-simulation/reference/provenance.md`
- Modify: `minecraft-simulation/Taskfile.yml`
- Modify: `headless-minecraft/docs/superpowers/plans/2026-08-13-minecraft-simulation-foundation.md`

**Interfaces:**

- Produces: a safe tracked research package and an explicit prerequisite gate for the simulation foundation plan.

- [x] **Step 1: Document the local workflow**

Document the one-command preparation, cache layout, supported versions, mapping strategies, review procedure, catalog statuses, and cleanup command. State that `reference/work` cannot be shared or committed.

- [x] **Step 2: Add a safe cleanup target**

Add `reference:clean` that resolves and validates `REFERENCE_DIR`, then removes only that exact directory. Reject the repository root, parents, and paths outside the repository before deletion.

- [x] **Step 3: Record provenance without restricted content**

Record the Mojang manifest endpoint, version IDs, expected official artifact SHA-1 values from the resolved lock manifests, MCP URLs and SHA-256 values, SpecialSource identity, Vineflower identity, and Java runtime identity. Do not copy decompiled text into provenance.

- [x] **Step 4: Run publication checks**

Run:

```bash
devbox run -- task reference:check REFERENCE_DIR=reference/work
devbox run -- task verify
git status --short --ignored reference
git ls-files reference
```

Expected: only configuration, schemas, catalogs, notes, and provenance are tracked. All jars, mappings, sources, bytecode dumps, indexes, and reports remain ignored.

- [x] **Step 5: Activate the implementation prerequisite**

Require the simulation foundation plan to check:

```text
reference:check passes
both version catalogs cover every required domain
the first-slice comparison contains no unresolved record
every first-slice arithmetic method has bytecode evidence
every planned shared rule names a conformance fixture
```

Do not begin simulation code until all five checks pass.

## Completion criteria

The reference preparation is complete only when:

- One Task command prepares both versions and both sides in a caller-selected safe directory.
- Every download and tool has a verified digest and a recorded provenance entry.
- Java 1.8.9 uses reviewed MCP names, and Java 26.1.2 uses verified identity names.
- Newer bundled servers yield a verified executable server jar before analysis.
- Four decompiled source trees and four complete JVM symbol indexes exist below the ignored directory.
- Every approved simulation domain has a reviewed mapping or a reviewed exclusion for both versions and applicable sides.
- Every arithmetic-sensitive first-slice method has matching bytecode evidence.
- Every first-slice cross-version decision is resolved and names its future conformance scenario.
- No restricted artifact is tracked by Git.
- `devbox run -- task verify` passes.
