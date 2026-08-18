# Immutable game-data contracts implementation plan

> **Status: complete, 2026-08-18.** Shipped in `minecraft-protocol` as part of
> M0, alongside the generated Java 1.8 data. The checkboxes below were never
> ticked and are not evidence; do not re-run this plan.

> **For agentic workers:** REQUIRED SUB-SKILL: Use `subagent-driven-development` or execute this plan inline one task at a time. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add the immutable typed game-data model, raw-dataset access, and version registry that the Java 1.8 generator and later protocol versions will use.

**Architecture:** The `data` package owns plain value types and small read-only registry interfaces. `Set` groups those interfaces and owns raw datasets through a constructor that clones caller data. An instance `Registry` provides isolated tests and custom catalogs; package functions wrap one process-wide registry for generated package registration.

**Tech Stack:** Go 1.26.5 from `openserbia/go-flake`, Devbox, Task, the Go standard library, and the existing `server/pkg/gamedata` contracts.

## Status and scope

- `minecraft-protocol` commit `1b545bc` is the baseline.
- This plan supersedes Task 3 of `2026-08-13-shared-protocol-extraction.md`.
- After this plan passes, resume that plan at Task 4 to port the Java 1.8 generator and generated data.
- This milestone does not add generated Java data, source manifests, generator code, packet codecs, or server imports.

## Global constraints

- Work in `/home/ocharnyshevich/pet.projects/go-theft-craft/minecraft-protocol` unless a step names another repository.
- Run Go, formatting, lint, test, and build commands through `devbox run -- task <name>`.
- Preserve `data.RawDataset` and its owned-byte `Clone` behavior.
- Preserve source values and use the module-root wire widths for protocol-facing IDs: `PacketID` and `ProtocolNumber` have underlying type `int32`; the remaining source ID namespaces have underlying type `int`. Use named collection types where a collection has domain meaning or nested ownership behavior.
- Keep registry interfaces read-only. Lookup methods that return values with maps, slices, or pointers must return deep-owned values.
- Use named collection types for nested ownership. Use `slices.Clone` and `maps.Clone` for flat collections.
- Do not add a public generic registry abstraction.
- `Set` must not expose its raw dataset map or a caller-owned slice.
- Reject duplicate raw dataset names, empty version names, nil factories, duplicate version registration, nil factory results, and unknown versions with errors usable through `errors.Is`. Propagate factory errors with version context.
- Sort raw dataset names and registered version names.
- Make `Registry` safe for concurrent registration, listing, and loading.
- Call a registered factory once per `Load`; do not cache returned `*Set` values.
- Package-level `Register`, `Load`, and `RegisteredVersions` wrap a private default `Registry`.
- Preserve prior commits and unrelated work. Create one final commit only after all task reviews and the full release gate pass.

---

### Task 1: Port value types with deep-copy methods

**Files:**

- Create: `data/block.go`
- Create: `data/item.go`
- Create: `data/entity.go`
- Create: `data/biome.go`
- Create: `data/effect.go`
- Create: `data/enchantment.go`
- Create: `data/food.go`
- Create: `data/particle.go`
- Create: `data/instrument.go`
- Create: `data/attribute.go`
- Create: `data/window.go`
- Create: `data/material.go`
- Create: `data/recipe.go`
- Create: `data/collision_shape.go`
- Create: `data/protocol.go`
- Create: `data/version.go`
- Create: `data/clone_test.go`

**Interfaces:**

- Consumes: the public fields in `server/pkg/gamedata/*.go`.
- Produces: the same value-type names under package `data`, domain ID types, named nested collection types, and `Clone` methods on values that contain pointers, slices, maps, or nested values.

- [ ] **Step 1: Write mutation-isolation tests**

Create table-driven tests that clone each mutable type, mutate every nested reference in the clone, and assert that the source is unchanged. Use the named ID and collection types from Step 3 in test fixtures. Cover this ownership table:

| Type | Nested data to isolate |
| --- | --- |
| `Block` | `Hardness`, `Drops`, `HarvestTools`, `Variations` |
| `Item` | `EnchantCategories`, `RepairWith`, `Variations` |
| `Entity` | `Width`, `Height` |
| `Enchantment` | `Exclude` |
| `Food` | `Variations` |
| `Window` | `Slots`, `Properties`, `OpenedWith` |
| `Material` | `ToolSpeeds` |
| `Recipe` | `Ingredients`, both levels of `InShape` |
| `CollisionShapes` | `Blocks`, each block slice, `Shapes`, and each bounding-box slice |
| `Protocol` | `Types`, `Phases`, both packet slices, and every packet's `Fields` |

Use exact assertions such as:

```go
source := Block{
	Hardness:     float64Pointer(1.5),
	Drops:        Drops{{ID: 1}},
	HarvestTools: HarvestToolSet{257: true},
	Variations:   Variations{{Metadata: 1}},
}
clone := source.Clone()
*clone.Hardness = 2
clone.Drops[0].ID = 2
clone.HarvestTools[257] = false
clone.Variations[0].Metadata = 2
```

Also assert that cloning nil pointers, maps, and slices preserves nil.

- [ ] **Step 2: Run the tests and verify failure**

Run:

```bash
devbox run -- task test -- ./data -run 'Test.*Clone'
```

Expected: compilation fails because the value types and clone methods do not exist.

- [ ] **Step 3: Port the value types**

Copy the public struct definitions from these exact server files:

```text
attribute.go biome.go block.go collision_shape.go effect.go
enchantment.go entity.go food.go instrument.go item.go material.go
particle.go protocol.go recipe.go version.go window.go
```

Do not rename fields. Keep source widths except for protocol-facing IDs, which use the existing module-root `int32` wire width. `data.Protocol` is the data schema. It remains distinct from the module-root `protocol.Protocol` interface.

Define these ID namespaces:

```go
type BlockID int
type ItemID int
type EntityID int
type EntityInternalID int
type BiomeID int
type EffectID int
type EnchantmentID int
type ParticleID int
type InstrumentID int
type ShapeID int
type PacketID int32
type ProtocolNumber int32
type Metadata int
type WindowID string
```

Use them in their matching `ID` fields and registry lookups. `Food.ID`, `Drop.ID`, `Ingredient.ID`, and `RecipeResult.ID` are `ItemID`. All existing `Metadata` fields use `Metadata`. `Version.Protocol` uses `ProtocolNumber`. `Window.ID` and `WindowRegistry.ByID` use `WindowID`. Keep `WindowOpener.ID` as `int` because `WindowOpener.Type` selects either the block or entity ID namespace.

Use these named types for nested collections:

```go
type PacketFields []PacketField
type Packets []Packet
type Drops []Drop
type Variations []Variation
type RecipeIngredients []Ingredient
type RecipeShape []RecipeIngredients
type Recipes []Recipe
type RecipeIndex map[ItemID]Recipes
type ShapeIDs []ShapeID
type BoundingBoxes []BoundingBox
type BlockShapeIndex map[string]ShapeIDs
type BoundingBoxIndex map[ShapeID]BoundingBoxes
type HarvestToolSet map[ItemID]bool
type ToolSpeedIndex map[ItemID]float64
type ProtocolTypes map[string]string
type ProtocolPhases map[string]ProtocolPhase
type Language map[string]string

type Blocks []Block
type Items []Item
type Entities []Entity
type Biomes []Biome
type Effects []Effect
type Enchantments []Enchantment
type Foods []Food
type Particles []Particle
type Instruments []Instrument
type Attributes []Attribute
type Windows []Window
type Materials []Material
```

Change the corresponding struct fields and registry method results to these named types. `Block.Drops` uses `Drops`, both block and item variation fields use `Variations`, `Block.HarvestTools` uses `HarvestToolSet`, and `Material.ToolSpeeds` uses `ToolSpeedIndex`. Untyped numeric values in generated literals remain assignable. Task 4 updates generator templates that emit explicit primitive map or slice types.

- [ ] **Step 4: Implement deep-copy methods**

Add `Clone` methods to every named collection. Collections whose elements are scalar-only can use `slices.Clone` or `maps.Clone` inside their method. Collections whose elements own references must allocate and call each element's `Clone`. Do not add generic or bespoke clone helpers. Containing values call the collection's `Clone` method so the ownership rule stays attached to the domain type. Immutable scalar-only values need no `Clone` method.

For pointers, allocate a new value:

```go
func cloneFloat64(value *float64) *float64 {
	if value == nil {
		return nil
	}
	clone := *value
	return &clone
}
```

- [ ] **Step 5: Run the clone tests**

Run:

```bash
devbox run -- task test -- ./data -run 'Test.*Clone'
```

Expected: every source value remains unchanged after nested mutations to its clone.

- [ ] **Step 6: Run package checks**

Run:

```bash
devbox run -- task test -- ./data
devbox run -- task lint
git diff --check
```

Expected: tests and lint pass. Do not commit.

### Task 2: Define read-only registries and an immutable set

**Files:**

- Create: `data/registry.go`
- Create: `data/set.go`
- Create: `data/set_test.go`
- Modify: `data/raw.go`

**Interfaces:**

- Consumes: Task 1 value types and `RawDataset.Clone()`.
- Produces: typed read-only registry interfaces, `SetOptions`, `NewSet(SetOptions) (*Set, error)`, typed `Set` accessors, `(*Set).Raw(string) (RawDataset, bool)`, and `(*Set).DatasetNames() []string`.

- [ ] **Step 1: Write the interface compile checks**

Define test fakes for each interface and add compile-time assignments. The interfaces retain these server method sets:

```go
type BlockRegistry interface {
	ByID(BlockID) (Block, bool)
	ByName(string) (Block, bool)
	All() Blocks
}

type AttributeRegistry interface {
	ByName(string) (Attribute, bool)
	ByResource(string) (Attribute, bool)
	All() Attributes
}

type RecipeRegistry interface {
	ByID(ItemID) Recipes
	All() RecipeIndex
}

type LanguageRegistry interface {
	Get(string) (string, bool)
	All() Language
}
```

Define the remaining ID-and-name interfaces with their matching named ID and plural result types. Define `WindowRegistry` with `WindowID`, `MaterialRegistry` with name lookup and `Materials`, and `LanguageRegistry.All` with `Language`.

Document on every interface that returned collections and nested reference fields are owned by the caller.

- [ ] **Step 2: Write raw-dataset ownership tests**

Construct a set from two unsorted `RawDataset` values. Assert that:

- `DatasetNames` returns sorted names.
- Mutating the input bytes after `NewSet` does not change stored data.
- Mutating a value returned by `Raw` does not change a later lookup.
- Mutating a returned names slice does not change later results.
- An unknown name returns `false`.
- Duplicate and empty dataset names return `ErrInvalidDataset` through `errors.Is`.

- [ ] **Step 3: Write set-accessor tests**

Create one fake for every typed registry. Pass all fakes plus `CollisionShapes`, `Protocol`, and `Version` through `SetOptions`. Assert that each accessor returns the selected registry and that value accessors return deep copies:

```go
shapes := set.CollisionShapes()
shapes.Blocks["stone"][0] = 99
if got := set.CollisionShapes().Blocks["stone"][0]; got == 99 {
	t.Fatal("CollisionShapes returned mutable internal data")
}
```

Apply the same mutation check to `Protocol()`. `Version()` returns a scalar value.

- [ ] **Step 4: Run the tests and verify failure**

Run:

```bash
devbox run -- task test -- ./data -run 'Test(Set|Raw|RegistryInterfaces)'
```

Expected: compilation fails because the registry interfaces and `Set` do not exist.

- [ ] **Step 5: Implement `SetOptions` and `Set`**

Use this construction boundary:

```go
type SetOptions struct {
	Blocks          BlockRegistry
	Items           ItemRegistry
	Entities        EntityRegistry
	Biomes          BiomeRegistry
	Effects         EffectRegistry
	Enchantments    EnchantmentRegistry
	Foods           FoodRegistry
	Particles       ParticleRegistry
	Instruments     InstrumentRegistry
	Attributes      AttributeRegistry
	Windows         WindowRegistry
	Materials       MaterialRegistry
	Recipes         RecipeRegistry
	Language        LanguageRegistry
	CollisionShapes CollisionShapes
	Protocol        Protocol
	Version         Version
	Raw             []RawDataset
}
```

Store fields privately. `NewSet` clones `CollisionShapes`, `Protocol`, and every raw dataset. Typed registry accessors return their read-only interfaces. Value accessors return owned clones.

Do not require every typed registry to be non-nil yet. Some protocol families or source versions omit datasets. Generated built-ins validate their required datasets in their own constructors.

- [ ] **Step 6: Implement raw access**

Add `ErrInvalidDataset`. In `NewSet`, reject an empty `RawDataset.Name` and duplicate names. Store raw datasets by name. `Raw` returns `RawDataset.Clone()`. `DatasetNames` creates and sorts a new slice for every call.

- [ ] **Step 7: Run package checks**

Run:

```bash
devbox run -- task test -- ./data
devbox run -- task lint
git diff --check
```

Expected: all data tests and lint pass. Do not commit.

### Task 3: Add isolated and package-level version registries

**Files:**

- Create: `data/loader.go`
- Create: `data/loader_test.go`

**Interfaces:**

- Consumes: `data.Set` from Task 2.
- Produces: `Factory`, `Registry`, `NewRegistry`, `(*Registry).Register`, `(*Registry).Load`, `(*Registry).Versions`, and package-level `Register`, `Load`, and `RegisteredVersions`.

- [ ] **Step 1: Write validation tests**

Cover empty names, nil factories, duplicate registration, unknown versions, and a factory that returns nil. Assert these sentinels through `errors.Is`:

```go
var (
	ErrInvalidVersion   = errors.New("invalid data version")
	ErrDuplicateVersion = errors.New("data version already registered")
	ErrUnknownVersion   = errors.New("unknown data version")
	ErrNilSet           = errors.New("data factory returned nil set")
)
```

- [ ] **Step 2: Write order and factory-isolation tests**

Register `java/b` and `java/a`. Assert that `Versions()` returns `[]string{"java/a", "java/b"}` and returns a fresh slice on every call.

Use a factory that increments a counter and returns `NewSet(SetOptions{Version: Version{MinecraftVersion: strconv.Itoa(counter)}})`. The factory returns both values from `NewSet`. Load twice. Assert that the counter is two, the pointers differ, and the version values differ.

- [ ] **Step 3: Write concurrency tests**

Start concurrent goroutines that register unique names, list versions, and load one stable version. The test must pass under the repository's race-enabled `task test` command. Do not assert goroutine completion with sleeps. Use a `sync.WaitGroup`.

- [ ] **Step 4: Run the tests and verify failure**

Run:

```bash
devbox run -- task test -- ./data -run 'TestRegistry'
```

Expected: compilation fails because the version registry does not exist.

- [ ] **Step 5: Implement the instance registry**

Use this contract:

```go
type Factory func() (*Set, error)

type Registry struct {
	mu        sync.RWMutex
	factories map[string]Factory
}

func NewRegistry() *Registry
func (r *Registry) Register(name string, factory Factory) error
func (r *Registry) Load(name string) (*Set, error)
func (r *Registry) Versions() []string
```

Initialize the map in `NewRegistry` and lazily in `Register` so a zero-value `Registry` also works. Hold locks only while accessing the factory map. Call factories after releasing the read lock. If a factory returns an error, wrap it with the version name and preserve it for `errors.Is`. If it returns `(nil, nil)`, return `ErrNilSet`.

- [ ] **Step 6: Add package-level wrappers**

Create a private `defaultRegistry := NewRegistry()` and expose:

```go
func Register(name string, factory Factory) error
func Load(name string) (*Set, error)
func RegisteredVersions() []string
```

Generated packages call `Register` from `init` and must handle its error. Task 4 will choose the generated failure policy when it ports the generator.

- [ ] **Step 7: Run package checks**

Run:

```bash
devbox run -- task test -- ./data
devbox run -- task lint
git diff --check
```

Expected: validation, ordering, isolation, and concurrency tests pass. Do not commit.

### Task 4: Document and verify the milestone

**Files:**

- Modify: `README.md`
- Modify: `CHANGELOG.md`
- Modify: `docs/superpowers/plans/2026-08-13-shared-protocol-extraction.md`

**Interfaces:**

- Consumes: the complete `data` package from Tasks 1 through 3.
- Produces: accurate pre-alpha support documentation and a verified commit candidate.

- [ ] **Step 1: Add contract examples**

Update the README to state that typed game-data contracts, immutable lookup ownership rules, raw dataset lookup, and version registration exist. Keep generated Java 1.8 and Java 26.1 datasets marked as planned.

Add one short example that constructs a `Set` with `NewSet`, registers a factory, loads it, and reads `DatasetNames`. Use error handling for both `NewSet` and `Register`.

- [ ] **Step 2: Update the changelog and old plan**

Add an Unreleased changelog entry for the typed data contracts and registry. Add this note below the old shared-extraction plan header:

```markdown
> [!NOTE]
> Tasks 1 through 3 describe the pre-foundation package layout. The repository
> foundation and Java wire extraction are complete. Use
> `2026-08-13-immutable-game-data-contracts.md` for the game-data contract
> milestone, then resume this plan at Task 4.
```

- [ ] **Step 3: Run the protocol release gate**

Run:

```bash
devbox run -- task verify
```

Expected: formatting, lint, secret scanning, race-enabled tests, vulnerability scanning, and build all pass.

- [ ] **Step 4: Run the unchanged server gate**

From `/home/ocharnyshevich/pet.projects/go-theft-craft/server`, run:

```bash
unset GOROOT
devbox run -- task test
```

Expected: all server tests pass and the server worktree remains clean.

- [ ] **Step 5: Inspect and review the final scope**

Run:

```bash
git diff --check
git status --short
git diff --stat
```

Expected: changes are limited to `data`, `README.md`, and `CHANGELOG.md`. The ignored plan files remain local.

- [ ] **Step 6: Commit after review**

After every task review and the final whole-change review pass, stage only the milestone files and run:

```bash
devbox run -- task precommit
git commit -m "feat: add immutable game data contracts"
```

Do not push.

## Completion criteria

This milestone is complete only when all of these statements are true:

- Value types match the existing server generator inputs.
- Every mutable value type has tested deep-copy behavior.
- Registry interfaces promise caller-owned results.
- `Set` owns raw datasets and mutable schema values.
- Raw dataset and version names are sorted.
- Duplicate and invalid registration paths return typed errors.
- Instance and package-level version registries exist.
- Concurrent version registry use passes the race detector.
- Generated Java versions remain outside this milestone.
- Both repository gates pass.
- The final reviewed change is committed once and not pushed.
