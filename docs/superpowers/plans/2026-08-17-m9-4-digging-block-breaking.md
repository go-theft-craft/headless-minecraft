# M9.4 Digging and Block Breaking Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `subagent-driven-development` or execute this plan inline one task at a time. Keep every checkbox current.

**Goal:** Compute block break times that match vanilla across every tool, block, and effect combination on both Java Edition 1.8.9 and 26.1.2, and drive the dig sequence through the client so a real server accepts it.

**Architecture:** Break time is a pure function of hardness, tool speed, harvest legality, enchantment, effects, and whether the player is underwater or airborne. All six inputs are already generated data in `minecraft-protocol` — `blocks.go` carries `Hardness` and `HarvestTools`, `materials.go` carries `ToolSpeeds`, `enchantments.go` and `effects.go` carry the rest. So the rule is a kernel phase over data that exists, and the hard part is not the formula but the fact that **the two versions classify blocks by incompatible material vocabularies**. The gate is a matrix run against captured vanilla traces, plus a live dig against a pinned server.

**Tech Stack:** Go 1.26.6 from `openserbia/go-flake`, Devbox, Task, `minecraft-simulation`'s `sim`, `world`, and `profile/java/*`, `minecraft-protocol`'s generated block, material, item, enchantment, and effect data, `relay`'s capture oracle, and pinned vanilla 1.8.9 and 26.1.2 servers.

## Before executing this plan: reconcile it

**Reconciled 2026-08-18, by Task 0.** The table below is what is built today,
not what was specified. Where this plan said something that turned out to be
false, the task now says the true thing and the difference is recorded under
"What reconciliation changed" beneath the table.

| Symbol | Where it is | State |
| --- | --- | --- |
| `sim.Phase`, `sim.TickState`, `sim.Command`, `sim.DomainEvent`, `sim.Profile`, `sim.Kernel` | `minecraft-simulation/sim` | built, names unchanged |
| `sim.CommandOutcome{Index, Kind, Accepted, Reason}` | `minecraft-simulation/sim/command.go` | built, and its fields are the ones Task 3's tests read |
| `sim.TickResult.Completeness{Complete bool, Missing []Dependency}` | `minecraft-simulation/sim/completeness.go` | built, names unchanged |
| `world.BlockRef`, `world.View`, `world.StateView`, `world.BlockView` | `minecraft-simulation/world` | built, names unchanged |
| `profile/java/v1_8.New`, `profile/java/v26_1.New` | `minecraft-simulation/profile/java/*` | built as `New(*data.Set) (sim.Profile, error)` — **the interface, not an exported struct** |
| `client.Do`, `client.Action` | `headless-minecraft/client` | built. `Do(ctx, version.Action) error`; the action types live in `version`, not in `client` |
| `data.Block.Hardness` (`*float64`), `.HarvestTools` (`data.HarvestToolSet`), `.Material` (string), `.Diggable` | `minecraft-protocol/data/block.go` | built, and read again on 2026-08-18 |
| `data.Material{Name, ToolSpeeds}`, `data.ToolSpeedIndex` | `minecraft-protocol/data/material.go` | built. A material carries a name and a tool-speed table, and nothing else |
| `conform.Scenario`, `conform.Lane`, `conform.Run` | `relay/examples/minecraft/conform` | built (M9.1b) |
| `mctest.Captured`, `mctest.LoadCaptured`, `mctest.LoadCapturedDir`, `mctest.ReplayCaptured` | `minecraft-simulation/mctest` | built (M9.2) — **this is what M9.3's `conformance` package became** |
| `mcreference dump` for `26.1.2` | `minecraft-reference/internal/reference/physics/dumper.go` | built, typed rather than reflective |
| `conformance.Compare`, `conformance.Document` | — | **never built and not planned.** See below |
| `mining.BreakTicks`, `mining.Damage`, `mining.Conditions`, `mining.Dig`, `mining.Phase` | — | this plan, Tasks 1 and 3 |
| `mining.Classifier` (the optional profile interface) | — | this plan, Task 2 |
| `version.ActionDig{Block, Face, Stage}` | — | the [interaction primitives plan](2026-08-18-interaction-primitives.md), Task 4. **Not this plan's to build** |

### What reconciliation changed

Six things this plan asserted are not true of the tree it now runs against.

- **There is no `conformance` package, and there will not be one.** M9.3's Task 2
  proposed `conformance.Compare`; its own reconciliation re-scoped that to
  extending `mctest`, which is where M9.2 then built `Captured`, `LoadCaptured`,
  `LoadCapturedDir`, and `ReplayCaptured`. Task 5 below moved out of
  `minecraft-simulation/conformance/` accordingly.
- **A profile is an interface, so `Conditions` and `Hardness` cannot be methods
  on an exported `*Profile`.** `New` returns `sim.Profile`, and that interface
  has exactly five methods — `ID`, `Slipperiness`, `Motion`, `Shape`, `Phases`.
  This repository already has the pattern for extending a profile without
  widening the one interface every profile must satisfy: `sim.BlockNames` and
  `sim.DataDigest` are optional interfaces a caller type-asserts for. Task 2 now
  adds `mining.Classifier` the same way — declared in `mining` rather than in
  `sim`, because `mining` imports `sim` and an interface in `sim` returning a
  `mining.Conditions` would be an import cycle.
- **The 26.1 material vocabulary is larger than this plan said, and its compound
  names are registry keys.** 26.1's registry holds 25 materials, not the five
  this plan listed: `mineable/pickaxe`, `mineable/axe`, `mineable/shovel`,
  `mineable/hoe`, `sword_efficient`, `sword_instantly_mines`,
  `vine_or_glow_lichen`, `gourd`, `coweb`, `default`, `leaves`, `plant`, `wool`,
  seven `incorrect_for_<tier>_tool` entries, and the compounds. 1.8.9's holds 8.
  Crucially, a compound like `gourd;mineable/axe` is **itself a key in the
  registry, with its own merged tool-speed table**, so Task 2's instruction to
  split on `;` and merge by hand was wrong: it would recompute, possibly
  differently, what upstream already states. Checked mechanically on 2026-08-18:
  every distinct `Material` value on every block in both versions is a key in
  that version's material registry, so a direct lookup resolves all of them and
  none needs splitting.
- **Only one `incorrect_for_<tier>_tool` material is used by a block.** 108
  blocks in 26.1 carry `incorrect_for_wooden_tool`; the other six tiers exist in
  the registry and no block names them. What the tier materials mean for speed is
  still the open question this plan flagged — but it is one material to answer it
  for, not seven.
- **The dig primitive is not this plan's.** `client.ActionDig{Block, Face}` and
  `client.ActionDigCancel{Block}` do not exist and will not: actions live in
  `version`, and the [interaction primitives
  plan](2026-08-18-interaction-primitives.md) ships one `version.ActionDig`
  carrying a `Stage`, because the wire is one packet with a status field and two
  types would be two names for one packet. **What M9.4 owns is the break time** —
  the arithmetic, the classification, the phase, and the timing between the start
  and the finish. Task 4 is rewritten against that boundary.

  A nuance worth having exactly right: `client` does re-export the eight actions
  that existed at M8.8 as **type aliases** — `client.ActionMove` and
  `version.ActionMove` are the same type. What it does not do is invent one. The
  interaction primitives plan declares its new actions in `version` and adds no
  alias, so name them `version.X` here.
- **26.1.2 can be read from its jar.** The risk section said `mcreference dump`
  rejects every version but 1.8.9. It does not: `minecraft-reference` carries a
  typed 26.1.2 dumper, and M9.2 used that route to measure that version's item
  and arrow motion constants and confirmed them in bytecode. Both lanes of this
  stage can therefore rest on the same evidence, and Task 1 Step 3's fallback is
  a fallback rather than the expected case.

### How the live tests in this plan must be written

Every test here that starts a real server lands in the lane that already exists
for that, and M9.3's reconciliation found the same thing before this one did. The
rules, read off `client/vanilla_e2e_test.go` and `client/vanilla_scenario_test.go`
on 2026-08-18:

- The file carries `//go:build vanilla` and lives in `client/` (package
  `client_test`). A live test without the tag breaks `task verify`, which must
  stay offline; a live test outside `client/` is a test nothing runs, because the
  task is `go test ./client/ -run TestVanilla -tags vanilla`.
- The two versions are **two top-level tests**, not a loop over a version table:
  `TestVanilla<Thing>` and `TestVanilla26<Thing>`, each one line, each calling a
  shared scenario function with `lane1_8()` or `lane26()`. `-run TestVanilla`
  selects both, and a failure names the version in the test name rather than in a
  subtest the log has to be read for.
- The server comes from `lane.start(t)`, which wraps `vanilla.Start(t,
  vanilla.Options{…})`. 26.1.2's lane sets `Jar`, `Libraries`, `LevelType`, and a
  longer `Ready`; a test that calls `vanilla.Start` with bare options gets a
  1.8.9 server whatever it meant.
- `vanilla.Server` exposes `Lines`, `Log`, `Matching`, and `Stop`. There is no
  `LogLines` and no `Kill`.

The snippets below show scenario bodies. Wrap each in that pair before running
it, and do not invent a `vanillaVersions(t)` table — there is none, and a loop
variable named `version` would shadow the `version` package the bodies now name.

Two smaller name corrections, applied in place below: `world.BlockPosition` is
`world.BlockPos{X, Y, Z int32}` in `headless-minecraft/world`, and
`vanilla.Start(t, vanilla.Options)` returns a `*vanilla.Server` exposing `Lines`,
`Log`, `Matching`, and `Stop` — there is no `LogLines` and no `Kill`.

## Global Constraints

- Go 1.26.6 from `openserbia/go-flake`; Devbox and Task drive every gate.
- Two-version gate. `conform.Run` refuses a scenario missing a lane.
- Break times are computed from generated data, never from constants typed into
  the rule. A number in the formula that is not a game rule is a bug waiting.
- The capture oracle must not import `minecraft-simulation`.
- Offline mode, pinned servers with recorded digests.
- `task lint`, `task test` under `-race`, and `task verify` pass before every
  commit.

---

## Design decisions this plan settles

**The two versions have incompatible material vocabularies, and this is the
whole difficulty.** In `generated/java/v1_8/materials.go` the materials are
`dirt`, `leaves`, `melon`, `plant`, `rock`, `web`, `wood`, `wool`, and stone's
`Material` is `"rock"`. In `generated/java/v26_1/materials.go` there are 25,
built around `mineable/<tool>` names, seven `incorrect_for_<tier>_tool` entries,
a few block families — `gourd`, `coweb`, `vine_or_glow_lichen`,
`sword_efficient`, `sword_instantly_mines` — a catch-all `default`, and compound
names like `gourd;mineable/axe`. Stone's `Material` is `"mineable/pickaxe"`.

These are not renames of one another: 26.1 encodes tool *correctness* as
materials, which 1.8.9 encodes as `HarvestTools` alone. A shared lookup keyed by
material name would silently miss on every 26.1 block. The formula is shared; the
classification is version-owned, and it lives in each profile's block table.

Three names — `leaves`, `plant`, `wool` — appear in both vocabularies, which is
the trap rather than the reassurance: a shared lookup would resolve those three
and miss everything else, so it would work on the blocks a hand-written test
reaches first.

**Harvest legality and tool speed are different questions.** `HarvestTools` says
whether the block drops anything; `ToolSpeeds` says how fast it breaks. A wooden
pickaxe on obsidian is fast enough to eventually break it and drops nothing.
Conflating them — using `HarvestTools` as a gate on speed — produces a plausible
number for the common case and a wrong one for exactly the cases a matrix test
is for.

**`Hardness` is a `*float64` and nil means unbreakable.** Bedrock has no
hardness, not a hardness of zero. A rule that dereferences without checking
either panics or, worse, computes an instant break on bedrock. The nil case is a
distinct outcome, not a default.

**The dig sequence is three packets, not one.** Vanilla sends start-digging,
then either cancel or finish, and the server validates the elapsed time between
them. A client that sends only the finish packet breaks blocks instantly and is
the first thing an anti-cheat notices — which matters because M10 has an
anti-cheat lane that must stay quiet.

## File structure

**`minecraft-simulation/mining/`** — new package. Version-neutral break-time
arithmetic.

- `mining.go` — `BreakTicks`, `Speed`, `Harvestable`, `Unbreakable`.
- `mining_test.go`
- `modifiers.go` — efficiency, haste, mining fatigue, underwater, airborne.
- `modifiers_test.go`

**`minecraft-simulation/profile/java/v1_8/`** and **`.../v26_1/`**

- `mining.go` in each — the version-owned classification: material lookup,
  tool-speed resolution, harvest legality.

**`minecraft-simulation/sim/`**

- `command.go` — modify. `sim.CommandDig` and its outcome.

Nothing else changes in `sim`. `mining.Classifier` — the optional interface a
profile implements to answer a block's hardness and a held tool's conditions —
lives in `mining/profile.go`, because `mining` imports `sim` and declaring it the
other way round would be an import cycle.

**`headless-minecraft/`**

- `client/dig.go` — the dig sequence: start, wait the break time, finish. It
  drives `version.ActionDig`, which the interaction primitives plan owns.
- `client/dig_test.go`

**`minecraft-simulation/mining/`** — the matrix gate lives with the code it
gates, because there is no `conformance` package and the simulation cannot
import `relay`'s examples module to reach `conform`.

- `vanilla_test.go` — the matrix gate.
- `testdata/vanilla/` — the captured break-time corpus, one file per version.

---

## Task 0: Reconcile this plan against what is built

- [x] **Step 1: Check every symbol the table names**

Done. The table above says where each one is and what shape it landed in, and
"What reconciliation changed" lists the six places this plan was wrong. Two of
them would have been found only after writing code against them: a profile is an
interface with five methods, and the dig action belongs to another plan.

- [x] **Step 2: Check the data, not only the symbols**

This stage is a rule over generated data, so the data is as much a dependency as
a function name. Both versions' material vocabularies were read out of the
generated registries and every block's `Material` was checked against them:

```bash
cd minecraft-protocol
for v in v1_8 v26_1; do
  comm -23 \
    <(grep -o 'Material: "[^"]*"' generated/java/$v/blocks.go | sed 's/Material: //' | sort -u) \
    <(grep -o '{Name: "[^"]*"' generated/java/$v/materials.go | sed 's/{Name: //' | sort -u)
done
```

Both print nothing, which is what says a direct lookup resolves every block and
Task 2's splitting rule was solving a problem the data does not have.

- [x] **Step 3: Check what is claimed to be impossible**

The risk section said 26.1.2 cannot be dumped from its jar. It can — the typed
dumper is `minecraft-reference/internal/reference/physics/dumper_source_26_1.go`
and M9.2 used that route. A plan that carries a stale impossibility spends the
stage working around it.

- [x] **Step 4: Commit the reconciliation**

```bash
git add docs/superpowers/plans/2026-08-17-m9-4-digging-block-breaking.md
git commit -m "docs(plan): reconcile M9.4 against the profiles, the data, and the action path"
```

---

## Task 1: Break-time arithmetic

**Files:**
- Create: `minecraft-simulation/mining/mining.go`
- Test: `minecraft-simulation/mining/mining_test.go`

**Interfaces:**
- Consumes: nothing outside the standard library. This package is arithmetic
  over numbers a profile supplies; keeping it free of `world` and `data` is what
  makes it testable as a table.
- Produces:

```go
// Conditions is everything outside the block that changes how fast it breaks.
type Conditions struct {
    // Speed is the tool's multiplier against this block's material, or 1 when
    // the tool is not effective. It is not derived here: which tool is
    // effective against which material is version-owned, and the two versions
    // disagree about the vocabulary entirely.
    Speed float64
    // Harvestable reports whether the held tool is good enough to drop the
    // block. It changes the divisor, not the speed, which is why it is
    // separate from Speed and not folded into it.
    Harvestable bool
    // Efficiency is the enchantment level on the held tool, zero for none.
    Efficiency int
    // Haste and MiningFatigue are effect amplifiers, zero for none. They are
    // separate fields rather than a signed one because a player can have both
    // and vanilla applies both.
    Haste, MiningFatigue int
    // Underwater reports the player's head being in water without the
    // aqua-affinity enchantment; Airborne reports the player not on ground.
    Underwater, Airborne bool
}

// ErrUnbreakable reports a block with no hardness — bedrock, barrier, the void
// air. It is an error rather than an infinite tick count because "never" and "a
// very large number of ticks" behave differently in every caller that has a
// timeout.
var ErrUnbreakable = errors.New("mining: block is unbreakable")

// BreakTicks returns how many ticks breaking the block takes.
//
// hardness is a pointer because the generated block data declares it as one and
// nil means unbreakable rather than zero. Bedrock has no hardness; it does not
// have a hardness of zero, and a rule that treats the two alike breaks bedrock
// instantly.
func BreakTicks(hardness *float64, c Conditions) (int, error)

// Damage is the per-tick progress fraction, which is what the value vanilla
// actually computes and compares against 1. BreakTicks is derived from it, and
// it is exported because the client shows a crack texture driven by this
// number rather than by a tick count.
func Damage(hardness *float64, c Conditions) (float64, error)
```

- [x] **Step 1: Write the failing test**

```go
package mining_test

func TestABlockWithNoHardnessIsUnbreakable(t *testing.T) {
	t.Parallel()

	// Bedrock has no hardness. It does not have a hardness of zero, and the
	// generated data says so by declaring Hardness as *float64 and leaving it
	// nil. A rule that dereferences without checking breaks bedrock instantly.
	if _, err := mining.BreakTicks(nil, mining.Conditions{Speed: 1}); !errors.Is(err, mining.ErrUnbreakable) {
		t.Fatalf("err = %v, want ErrUnbreakable", err)
	}
}

func TestAZeroHardnessBlockBreaksInOneTick(t *testing.T) {
	t.Parallel()

	// Air, torches, and tall grass have a hardness of zero, which is a real
	// value and must not be confused with the nil above. Vanilla still takes
	// one tick, not zero: a break is a tick's worth of progress reaching 1.
	got, err := mining.BreakTicks(ptr(0.0), mining.Conditions{Speed: 1, Harvestable: true})
	if err != nil {
		t.Fatalf("BreakTicks: %v", err)
	}
	if got != 1 {
		t.Fatalf("BreakTicks = %d, want 1", got)
	}
}

func TestHarvestabilityChangesTheDivisorNotTheSpeed(t *testing.T) {
	t.Parallel()

	// A wooden pickaxe on obsidian is the case that separates the two. It is
	// effective — it has a tool speed — and it cannot harvest, so the block
	// breaks far slower than the tool speed alone predicts and drops nothing.
	// Folding harvestability into speed gets the common case right and this
	// one wrong, which is exactly what a matrix test exists to catch.
	fast, err := mining.BreakTicks(ptr(50.0), mining.Conditions{Speed: 2, Harvestable: true})
	if err != nil {
		t.Fatalf("BreakTicks harvestable: %v", err)
	}
	slow, err := mining.BreakTicks(ptr(50.0), mining.Conditions{Speed: 2, Harvestable: false})
	if err != nil {
		t.Fatalf("BreakTicks not harvestable: %v", err)
	}
	if slow <= fast {
		t.Fatalf("unharvestable took %d ticks and harvestable took %d; the "+
			"divisor must differ", slow, fast)
	}
}

func TestEachModifierMovesTheResultInTheRightDirection(t *testing.T) {
	t.Parallel()

	base := mining.Conditions{Speed: 4, Harvestable: true}
	baseline := ticks(t, ptr(1.5), base)

	for _, test := range []struct {
		name string
		with mining.Conditions
		want string // "faster" or "slower"
	}{
		{"efficiency", withEfficiency(base, 3), "faster"},
		{"haste", withHaste(base, 1), "faster"},
		{"mining fatigue", withFatigue(base, 1), "slower"},
		{"underwater", withUnderwater(base), "slower"},
		{"airborne", withAirborne(base), "slower"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got := ticks(t, ptr(1.5), test.with)
			if test.want == "faster" && got >= baseline {
				t.Fatalf("%s gave %d ticks against a baseline of %d, want fewer",
					test.name, got, baseline)
			}
			if test.want == "slower" && got <= baseline {
				t.Fatalf("%s gave %d ticks against a baseline of %d, want more",
					test.name, got, baseline)
			}
		})
	}
}

func TestUnderwaterAndAirborneCompound(t *testing.T) {
	t.Parallel()

	// Both penalties apply at once in vanilla. A rule written as an
	// if-else-if applies whichever it checks first and looks correct in every
	// single-condition test.
	base := mining.Conditions{Speed: 4, Harvestable: true}
	both := ticks(t, ptr(1.5), withAirborne(withUnderwater(base)))
	one := ticks(t, ptr(1.5), withUnderwater(base))
	if both <= one {
		t.Fatalf("underwater and airborne together gave %d ticks, underwater "+
			"alone gave %d; the penalties must compound", both, one)
	}
}

func TestMiningFatigueCanOutweighHaste(t *testing.T) {
	t.Parallel()

	// A player with both effects is not a contrived case: it is what a guardian
	// does to a player wearing a beacon's haste. Vanilla applies both.
	base := mining.Conditions{Speed: 4, Harvestable: true}
	both := ticks(t, ptr(1.5), withFatigue(withHaste(base, 1), 3))
	hasteOnly := ticks(t, ptr(1.5), withHaste(base, 1))
	if both <= hasteOnly {
		t.Fatalf("haste with fatigue gave %d ticks and haste alone gave %d; "+
			"one of the two effects is being ignored", both, hasteOnly)
	}
}
```

- [x] **Step 2: Run it to verify it fails**

Run: `cd minecraft-simulation && devbox run -- go test ./mining/ -v`
Expected: FAIL — the package does not exist.

- [x] **Step 3: Derive the formula from the game, not from a wiki**

Before writing `Damage`, confirm the arithmetic against the jar the way M8.1
and M8.2 did. `minecraft-reference` already extracts from a verified Mojang jar
and `minecraft-simulation/internal/oracle` already runs a harness against a real
server. Use the same route: dump the method that computes block-breaking
progress and transcribe it, recording the source in the doc comment.

Two things M8.1 found that apply here: constants may be Java `float` literals
that widen to `double` where applied, and a product computed in `float64` where
the game computes it in `float32` will not match bit for bit. Assume any
constant in this path is a widened `float` until checked.

If a jar-backed dump is not possible for one version, say so in the doc comment
and in the milestone record, and mark that version's constants as unverified —
the same way M8.7's plan requires for its fixtures. A number that records a
belief and does not say so is what makes a later failure hard to diagnose.

- [x] **Step 4: Implement**

- [x] **Step 5: Run the tests and gates**

Run: `cd minecraft-simulation && devbox run -- task verify`

- [x] **Step 6: Commit**

```bash
cd minecraft-simulation
git add mining/
git commit -m "feat(mining): break-time arithmetic over supplied conditions

Hardness is a pointer and nil is unbreakable, not zero. Harvestability
changes the divisor and not the speed. Underwater, airborne, haste, and
fatigue all compound rather than shadowing one another."
```

---

## Task 2: Version-owned classification

**Files:**
- Create: `minecraft-simulation/profile/java/v1_8/mining.go`
- Create: `minecraft-simulation/profile/java/v26_1/mining.go`
- Test: one `mining_test.go` in each

**Interfaces:**
- Consumes: `mining.Conditions`, `world.BlockRef`, the version's generated
  block, material, item, enchantment, and effect registries.
- Produces, in each profile:

`New` returns `sim.Profile`, and that interface has five methods that every
profile must satisfy. Mining is not one of them and must not become one: a
profile hand-built in a test has no block data to answer from. So this lands the
way `sim.BlockNames` and `sim.DataDigest` already did — an optional interface a
caller type-asserts for.

It is declared in `minecraft-simulation/mining/profile.go` rather than in `sim`,
and that is not a preference: `mining` imports `sim` for `Phase` and `Command`,
so a `sim` interface returning a `mining.Conditions` would be an import cycle.
The optional interface belongs to the package that owns the vocabulary either
way.

```go
// Classifier is a profile that can say how one block breaks under one held item.
//
// It is optional for the same reason BlockNames is: nothing inside a tick reads
// it — the dig phase is handed conditions rather than resolving them — and a
// profile assembled in a test has no registries to answer from. A caller that
// needs it asserts for it and reports a profile that cannot answer, rather than
// every profile being obliged to carry block data.
//
// It is per-version because the two editions classify blocks by different
// vocabularies. 1.8.9's material for stone is "rock"; 26.1.2's is
// "mineable/pickaxe", and 26.1.2 additionally encodes tool correctness as
// materials named "incorrect_for_<tier>_tool" that 1.8.9 has no counterpart
// for. A shared lookup keyed by material name would miss on every 26.1 block.
type Classifier interface {
	// Conditions resolves everything version-specific about breaking one block
	// with one held item.
	Conditions(ref world.BlockRef, held Held, effects Effects, underwater, airborne bool) (Conditions, error)
	// Hardness returns the block's hardness, or nil when it has none.
	Hardness(ref world.BlockRef) *float64
}

// Held is the item in the player's hand, as a version's own item id and the
// enchantment levels that change a break time. It is an id rather than a
// modelled stack because that is all this question needs, and modelling an
// inventory item here would put M9.7's data model in M9.4's path.
type Held struct {
	Item       data.ItemID
	Efficiency int
}

// Effects are the status effects that change a break time. Amplifiers, not
// levels: haste I is amplifier 0, as the protocol sends it.
type Effects struct {
	Haste, MiningFatigue int
	HasHaste, HasFatigue bool
}
```

Each version's `mining.go` then implements it on the unexported profile struct
`New` already returns, and each profile's test asserts the interface is
satisfied — the M7 defect the master plan records is exactly a seam satisfied by
assertion that nobody asserted:

```go
var _ mining.Classifier = (*profile)(nil)
```

`newProfile(t)` in the tests below returns the asserted `mining.Classifier`, not
a bare `sim.Profile`.

- [x] **Step 1: Write the failing test**

```go
func TestStoneIsClassifiedByThisVersionsVocabulary(t *testing.T) {
	t.Parallel()

	// The literal string is the point. If a later data regeneration changes
	// the material name, this fails here rather than as a wrong break time in
	// a matrix of two hundred cases.
	p := newProfile(t)
	got, err := p.Conditions(stoneRef(t, p), woodenPickaxe(t), noEffects, false, false)
	if err != nil {
		t.Fatalf("Conditions: %v", err)
	}
	if got.Speed <= 1 {
		t.Fatalf("Speed = %v for a pickaxe on stone; the material lookup missed",
			got.Speed)
	}
	if !got.Harvestable {
		t.Fatal("a wooden pickaxe cannot harvest stone, according to this profile")
	}
}

func TestAnIneffectiveToolGetsSpeedOneRatherThanZero(t *testing.T) {
	t.Parallel()

	// A shovel on stone is slow, not stuck. Speed zero would make BreakTicks
	// divide by zero or return infinity, and either one reads as "unbreakable"
	// to a caller that only checks the error.
	p := newProfile(t)
	got, err := p.Conditions(stoneRef(t, p), woodenShovel(t), noEffects, false, false)
	if err != nil {
		t.Fatalf("Conditions: %v", err)
	}
	if got.Speed != 1 {
		t.Fatalf("Speed = %v for a shovel on stone, want 1", got.Speed)
	}
	if got.Harvestable {
		t.Fatal("a shovel harvests stone, according to this profile")
	}
}

func TestBedrockHasNoHardness(t *testing.T) {
	t.Parallel()

	p := newProfile(t)
	if got := p.Hardness(bedrockRef(t, p)); got != nil {
		t.Fatalf("Hardness = %v for bedrock, want nil", *got)
	}
}

func TestEveryDiggableBlockResolvesAMaterial(t *testing.T) {
	t.Parallel()

	// The sweep that catches a vocabulary mismatch wholesale rather than one
	// block at a time. A profile that resolves stone and misses eight hundred
	// other blocks passes every hand-written case above.
	p := newProfile(t)
	var missed []string
	for _, block := range diggableBlocks(t, p) {
		if _, err := p.Conditions(block.Ref, correctToolFor(t, block), noEffects, false, false); err != nil {
			missed = append(missed, block.Name)
		}
	}
	if len(missed) != 0 {
		t.Fatalf("%d diggable blocks resolved no material: %v",
			len(missed), first(missed, 20))
	}
}
```

- [x] **Step 2: Run it to verify it fails**

Run: `cd minecraft-simulation && devbox run -- go test ./profile/java/... -run Classif -v`
Expected: FAIL.

- [x] **Step 3: Implement each version**

For 1.8.9: `Material` is a plain name; look it up in the material registry and
take `ToolSpeeds[heldItemID]`, defaulting to 1. `Harvestable` is
`HarvestTools[heldItemID]`, and a block with an empty `HarvestTools` is
harvestable by anything, including a bare hand — an empty set means "no tool
required", not "no tool works".

For 26.1.2: the same shape, and **do not split the compound material names**.
`"gourd;mineable/axe"` looks like two materials joined and is one key in the
material registry, carrying its own merged tool-speed table — splitting it would
recompute by hand what upstream already states, and the two answers would differ
the first time upstream merged them by a rule other than "best speed wins". Task
0 checked every block in both versions: every `Material` value is a registry key,
so the lookup that works for 1.8.9 works here unchanged.

What does need deciding is the `incorrect_for_<tier>_tool` family. 108 blocks in
26.1 carry `incorrect_for_wooden_tool` and no block carries the other six tiers.
The material's table gives the wooden tools a speed of 2 — the same number the
tier gets on a material it *is* correct for — so reading it as an ordinary speed
table gives a fast break where vanilla gives a slow one. Settle it against the
game before implementing: dig one such block with a wooden and with a stone tool
on a pinned 26.1.2 server, and let the two observed times say which reading is
right. Record the answer in the doc comment, because nothing in the dataset says
it.

- [x] **Step 4: Run the tests and gates**

Run: `cd minecraft-simulation && devbox run -- task verify`

- [x] **Step 5: Commit**

```bash
cd minecraft-simulation
git add profile/java/
git commit -m "feat(profile): resolve mining conditions per version

The two editions classify blocks by incompatible vocabularies: 1.8.9's
stone is material \"rock\", 26.1.2's is \"mineable/pickaxe\", and 26.1.2
encodes tool correctness as materials 1.8.9 has no counterpart for. The
sweep test checks every diggable block, not the handful named by hand."
```

---

## Task 3: The dig command and phase

**Files:**
- Modify: `minecraft-simulation/sim/command.go`
- Create: `minecraft-simulation/mining/phase.go`
- Test: `minecraft-simulation/mining/phase_test.go`

**Interfaces:**
- Produces:

```go
// Dig asks the kernel to make progress on breaking one block.
//
// It is a command rather than a state so that a dig which is interrupted —
// by a correction, by the block changing under the player, by the player
// looking away — stops making progress without anything having to cancel it.
type Dig struct {
    Entity entity.ID
    Block  geom.BlockPos
    // Face is which side is being hit. It changes nothing about the time and
    // everything about what the server accepts, so it travels with the command
    // rather than being reconstructed at the wire.
    Face Face
}

func (Dig) CommandKind() string { return "mining.dig" }

// Phase is the kernel phase that accumulates dig progress.
func Phase() sim.Phase
```

- [x] **Step 1: Write the failing test**

```go
func TestProgressAccumulatesAcrossTicks(t *testing.T) {
	t.Parallel()

	// A break is progress reaching 1, not a countdown. The distinction shows
	// up the moment a dig is interrupted and resumed.
	k := kernelFor(t, stoneWorld(t))
	var breaks int
	for range breakTicksFor(t, "stone", ironPickaxe) {
		result := step(t, k, mining.Dig{Entity: 1, Block: stonePos, Face: mining.FaceTop})
		breaks += countBlockBreaks(result)
	}
	if breaks != 1 {
		t.Fatalf("broke the block %d times over its exact break time, want 1", breaks)
	}
}

func TestInterruptingADigResetsProgress(t *testing.T) {
	t.Parallel()

	// Vanilla resets rather than pausing. A kernel that pauses lets a player
	// break a block in several sittings, which is faster than vanilla allows
	// and is precisely the thing an anti-cheat flags.
	k := kernelFor(t, stoneWorld(t))
	total := breakTicksFor(t, "stone", ironPickaxe)

	for range total - 1 {
		step(t, k, mining.Dig{Entity: 1, Block: stonePos, Face: mining.FaceTop})
	}
	step(t, k) // one tick with no dig command
	result := step(t, k, mining.Dig{Entity: 1, Block: stonePos, Face: mining.FaceTop})

	if countBlockBreaks(result) != 0 {
		t.Fatal("the block broke one tick after an interruption; progress was " +
			"paused rather than reset")
	}
}

func TestDiggingAnUnbreakableBlockIsRejectedNotStuck(t *testing.T) {
	t.Parallel()

	// The outcome must say why. A dig that silently makes no progress is
	// indistinguishable from a dig that is merely slow, and a caller waiting
	// on it waits forever.
	k := kernelFor(t, bedrockWorld(t))
	result := step(t, k, mining.Dig{Entity: 1, Block: bedrockPos, Face: mining.FaceTop})

	outcome := onlyOutcome(t, result)
	if outcome.Accepted {
		t.Fatal("a dig on bedrock was accepted")
	}
	if outcome.Reason == "" {
		t.Fatal("a rejected dig gave no reason; a caller cannot tell it from a slow one")
	}
}

func TestDiggingAnUnknownBlockIsIncompleteNotRejected(t *testing.T) {
	t.Parallel()

	// The tri-state block view M8.2 built exists for this. A block the client
	// has not received is not a block that cannot be dug — it is a tick that
	// could not be computed, and the caller is expected to load it and retry.
	k := kernelFor(t, emptyWorld(t))
	result := step(t, k, mining.Dig{Entity: 1, Block: unknownPos, Face: mining.FaceTop})

	if result.Completeness.Complete {
		t.Fatal("a dig against an unknown block reported a complete tick")
	}
	if len(result.Completeness.Missing) == 0 {
		t.Fatal("an incomplete tick named nothing missing")
	}
}
```

- [x] **Step 2: Run it to verify it fails**

Run: `cd minecraft-simulation && devbox run -- go test ./mining/ -run Phase -v`
Expected: FAIL.

- [x] **Step 3: Implement**

- [x] **Step 4: Run the tests and gates**

Run: `cd minecraft-simulation && devbox run -- task verify`

- [x] **Step 5: Commit**

```bash
cd minecraft-simulation
git add sim/command.go mining/
git commit -m "feat(mining): the dig command and its kernel phase

Progress accumulates and an interruption resets it, because vanilla
resets and a kernel that pauses breaks blocks faster than the game
allows. An unknown block is an incomplete tick, not a rejection."
```

---

## Task 4: The client dig sequence

**Files:**
- Create: `headless-minecraft/client/dig.go`
- Test: `headless-minecraft/client/dig_test.go`

**Interfaces:**
- Consumes: `version.Action`, `client.Do`, and `version.ActionDig{Block
  version.BlockPos, Face version.Face, Stage version.DigStage}` with its
  `DigStart`, `DigCancel`, and `DigFinish` stages — all built by the
  [interaction primitives plan](2026-08-18-interaction-primitives.md), Tasks 1
  and 4. **This task builds none of them.**
- Produces: `client.Dig(ctx context.Context, block version.BlockPos, face
  version.Face) error` — the sequence, not the packet. It sends the start stage,
  waits the break time this stage computes, and sends the finish stage; a
  cancelled context sends the cancel stage.

This is the boundary reconciliation moved. One packet with a status field is one
action type with a stage, so the client owns *when* the three stages are sent and
M9.4 owns *how long* the wait between them is. A dig that sends the right three
packets at the wrong times is this stage's failure, not the primitive's.

If the interaction primitives plan has not run when this task is reached, stop
and run it first rather than adding a second dig action here.

- [x] **Step 1: Write the failing test**

```go
func TestADigSendsStartThenFinish(t *testing.T) {
	t.Parallel()

	// Vanilla sends start-digging, waits the break time, then sends
	// finish-digging. A client that sends only the finish packet breaks blocks
	// instantly and is the first thing an anti-cheat notices — which matters,
	// because M10 has an anti-cheat lane that must stay quiet.
	// The loop variable is `lane`, not `version`: the body names the version
	// package, and a variable of that name would shadow it.
	for _, lane := range vanillaVersions(t) {
		t.Run(lane.Name, func(t *testing.T) {
			server := vanilla.Start(t, lane.Options)
			c := connected(t, server)

			sent := recordOutbound(t, c)
			if err := c.Dig(t.Context(),
				version.BlockPos{X: 0, Y: 63, Z: 0}, version.FaceTop); err != nil {
				t.Fatalf("Dig: %v", err)
			}
			awaitBlockBroken(t, c)

			kinds := digPacketKinds(t, sent)
			if len(kinds) < 2 {
				t.Fatalf("sent %v, want a start and a finish", kinds)
			}
			if kinds[0] != "start" || kinds[len(kinds)-1] != "finish" {
				t.Fatalf("sent %v, want start first and finish last", kinds)
			}
		})
	}
}

func TestADigDoesNotFinishEarly(t *testing.T) {
	t.Parallel()

	// The elapsed time between start and finish is what the server validates.
	// Finishing early is accepted by a permissive server and rejected by a
	// strict one, so the test asserts on our own timing rather than on the
	// server's tolerance.
	server := vanilla.Start(t, defaultOptions(t))
	c := connected(t, server)

	sent := recordOutbound(t, c)
	start := time.Now()
	if err := c.Dig(t.Context(),
		version.BlockPos{X: 0, Y: 63, Z: 0}, version.FaceTop); err != nil {
		t.Fatalf("Dig: %v", err)
	}
	awaitBlockBroken(t, c)

	want := expectedBreakDuration(t, c, version.BlockPos{X: 0, Y: 63, Z: 0})
	if elapsed := digElapsed(t, sent); elapsed < want {
		t.Fatalf("finished after %v, want at least %v; a dig this fast is what "+
			"an anti-cheat flags", elapsed, want)
	}
	_ = start
}

func TestACancelledDigSendsCancelAndNotFinish(t *testing.T) {
	t.Parallel()

	server := vanilla.Start(t, defaultOptions(t))
	c := connected(t, server)

	sent := recordOutbound(t, c)
	ctx, cancel := context.WithCancel(t.Context())
	go func() { time.Sleep(50 * time.Millisecond); cancel() }()
	_ = c.Dig(ctx, version.BlockPos{X: 0, Y: 63, Z: 0}, version.FaceTop)

	kinds := digPacketKinds(t, sent)
	if slices.Contains(kinds, "finish") {
		t.Fatalf("sent %v; a cancelled dig must not claim to have finished", kinds)
	}
	if !slices.Contains(kinds, "cancel") {
		t.Fatalf("sent %v, want a cancel", kinds)
	}
}
```

- [x] **Step 2: Run it to verify it fails**

Run: `cd headless-minecraft && devbox run -- go test ./client/ -run Dig -v`
Expected: FAIL.

- [x] **Step 3: Implement**

The three packets are version-owned and belong in each adapter — 47 and 775
number the dig statuses differently and 775 adds a sequence field the server
echoes. The sequencing and the timing are version-neutral and belong in
`client/dig.go`.

- [x] **Step 4: Run the tests and gates**

Run: `cd headless-minecraft && devbox run -- task verify`

- [x] **Step 5: Commit**

```bash
git add client/dig.go client/dig_test.go internal/adapter/
git commit -m "feat(client): dig as start, wait, finish

A client that sends only the finish packet breaks blocks instantly,
which is the first thing an anti-cheat notices. Cancelling sends a
cancel rather than claiming to have finished."
```

---

## Progress, 2026-08-18

Tasks 1 through 4 are built and committed. Tasks 5 and 6 are not: the matrix
gate needs a corpus captured through the proxy against pinned servers on both
versions, which has not been run.

What the four tasks found, beyond what the reconciliation above records:

- **The tick count is an accumulation, not a reciprocal.** Both games hold the
  progress in a float32 and add the per-tick fraction to it, comparing against
  one — `PlayerControllerMP.onPlayerDamageBlock` on 1.8.9 and the same shape on
  26.1.2. Stone with a bare hand is 150 ticks by `ceil(1/damage)` and 151 by the
  addition, and 151 is what the game does. `mining.BreakTicks` performs the
  addition.
- **There is a third outcome besides "breaks" and "unbreakable".** Near one, that
  float32's spacing is about 6e-8, so a per-tick fraction below half of that
  rounds away and the total stops moving: the player mines forever. That is
  `mining.ErrNeverBreaks`, and it is a different answer from a very long time.
- **Bedrock is unbreakable two different ways.** 1.8.9 leaves its hardness
  absent, which this plan anticipated. 26.1.2 records it as -1, which it did
  not. A rule that only checked for nil computes a negative break time here and
  calls it fast.
- **26.1.2's tier materials are not speed tables, and this is worse than the
  reconciliation said.** Gold ore's material is `incorrect_for_wooden_tool`,
  whose table lists the four *wooden* tools at speed two and nothing else. Read
  as a speed table it gives a diamond pickaxe no speed at all — 90 ticks against
  vanilla's 15 — and gives a wooden *shovel* the pickaxe's speed against ore.
  The tool class the dataset's flattening dropped is recoverable from the
  block's own `HarvestTools`, and 107 of the 108 tier-tagged blocks resolve to
  exactly one class that way. The crafter is the one that does not: it publishes
  no harvest tools at all.
- **The 26.1 dataset gets some tool speeds wrong, and the 26.1 lane of this
  stage's gate cannot pass for them.** Checked against the version's own jar:
  `ToolMaterial.COPPER` declares a speed of 5.0F and the dataset gives every
  copper tool a 1, on every material — so a copper pickaxe mines stone at a bare
  hand's rate here and at better than a stone pickaxe's in game. And
  `ShearsItem.createToolProperties` overrides the speed for leaves at 15.0F and
  wool at 5.0F, neither of which the dataset carries, though it does carry the
  same file's cobweb rule at 15. These are not worked around: overriding a
  dataset value with a constant typed into a profile is the one thing that
  module does not do. They are pinned by
  `TestTheDatasetToolSpeedsThisVersionGetsWrong`, which fails the day upstream
  fixes one.
- **The dig progress does not live in the kernel.** This plan's Task 3 tests
  implied a kernel that accumulates across ticks, and `sim.Kernel` holds no
  mutable state by design. The game does not accumulate in the world either: a
  vanilla client keeps `curBlockDamageMP` on the controller doing the digging
  and zeroes it when the button comes up. So `mining.Dig` carries an `Elapsed`
  count that is the caller's, the phase computes rather than accumulates,
  interrupting needs no cancel command, and `sim` did not change at all.
- **`client.Dig` takes the break time rather than computing it.** This plan's
  Task 4 specified `Dig(ctx, block, face)`. `headless-minecraft` models no
  inventory, no effects, and no submersion, so it cannot compute a break time —
  and a client that guessed would finish early on every block it guessed wrong
  about, which is the one failure the three-packet sequence exists to avoid. The
  signature is `Dig(ctx, block, face, breaking time.Duration)`.

---

## Task 5: The matrix gate

**Files:**
- Create: `minecraft-simulation/mining/vanilla_test.go`
- Create: `minecraft-simulation/mining/testdata/vanilla/1_8_9.json`, `26_1_2.json`

The gate lives beside the arithmetic it gates. There is no `conformance`
package — M9.3's proposal for one became `mctest` — and the simulation cannot
import `relay`'s `conform` to register a scenario, because that harness lives in
an examples module and the simulation must not depend on it. What carries the
two-version rule here is the same thing that carries it in `mctest`: a test that
fails when a version has no lane, in the shape of `mctest`'s own
`TestBothVersionsHaveACapturedLane`.

- [ ] **Step 1: Capture the corpus**

Through the proxy, on each version, dig one block per combination and record
the elapsed ticks the server reported. The combinations, chosen to cover the
branches rather than to be exhaustive:

| Axis | Cases |
| --- | --- |
| Block | stone, dirt, wood, wool, web, obsidian, bedrock, glass, a hardness-zero block |
| Tool | bare hand, wooden and diamond pickaxe, shovel, axe, shears, sword |
| Enchantment | none, efficiency 1, efficiency 5 |
| Effect | none, haste 1, mining fatigue 1, both |
| Player | on ground dry, underwater, airborne, underwater and airborne |

Do not take the cross product — it is over four thousand cases and most of them
test nothing new. Take the combinations that exercise a distinct branch, and
**log which combinations were dropped and why**, in the test file. A matrix that
silently samples reads as "covered everything" when it did not.

- [ ] **Step 2: Write the failing test**

```go
func TestBreakTimesMatchVanilla(t *testing.T) {
	t.Parallel()

	for _, version := range []string{"1_8_9", "26_1_2"} {
		corpus := loadMiningCorpus(t, version)
		if len(corpus) == 0 {
			t.Fatalf("%s: the mining corpus is empty; a matrix gate with no cases "+
				"passes and proves nothing", version)
		}

		for _, c := range corpus {
			t.Run(version+"/"+c.Name, func(t *testing.T) {
				t.Parallel()

				profile := profileFor(t, version)
				conditions, err := profile.Conditions(c.Ref, c.Held, c.Effects, c.Underwater, c.Airborne)
				if err != nil {
					t.Fatalf("Conditions: %v", err)
				}

				got, err := mining.BreakTicks(profile.Hardness(c.Ref), conditions)
				if c.Unbreakable {
					if !errors.Is(err, mining.ErrUnbreakable) {
						t.Fatalf("err = %v, want ErrUnbreakable", err)
					}
					return
				}
				if err != nil {
					t.Fatalf("BreakTicks: %v", err)
				}
				if got != c.ObservedTicks {
					t.Fatalf("computed %d ticks, vanilla took %d", got, c.ObservedTicks)
				}
			})
		}
	}
}
```

The comparison is exact. A break time is an integer number of ticks and vanilla
computes it deterministically; a tolerance here would hide the off-by-one that
an anti-cheat will not.

- [ ] **Step 3: Run it, fix what it names**

Run: `cd minecraft-simulation && devbox run -- go test ./mining/ -run BreakTimes -v`

- [ ] **Step 4: Refuse a one-version gate**

There is no `conform.Scenario` to register from here, so the two-version rule is
a test:

```go
func TestBothVersionsHaveABreakTimeCorpus(t *testing.T) {
	t.Parallel()

	// A stage that gates one version and skips the other is the failure this
	// milestone was subdivided to prevent. mctest carries the same test for
	// captured trajectories; this is its counterpart for break times.
	for _, version := range []string{"1_8_9", "26_1_2"} {
		if len(loadMiningCorpus(t, version)) == 0 {
			t.Fatalf("%s has no break-time corpus", version)
		}
	}
}
```

Then record the stage in `relay`'s `conform` matrix the way M9.1b's harness
expects, from the side that may import it.

- [ ] **Step 5: Commit**

```bash
cd minecraft-simulation
git add mining/vanilla_test.go mining/testdata/vanilla/
git commit -m "test(mining): break times match vanilla across the matrix

Exact tick comparison on both versions. The combinations dropped from
the cross product are logged in the test rather than left implied."
```

---

## Task 6: The milestone record

- [ ] **Step 1: Mark M9.4 complete in both stage tables**

- [ ] **Step 2: Write what the work found**

Candidates, if they hold: whether the 26.1.2 `incorrect_for_<tier>_tool`
materials behaved as this plan assumed; whether any break-time constant turned
out to be a widened `float`, as M8.1 found for the motion constants; how many
combinations the matrix actually covered against how many the cross product
holds; and whether a jar-backed dump was possible for both versions or only one.

- [ ] **Step 3: Commit**

```bash
git commit -m "docs(plan): close M9.4, and what the break-time matrix found"
```

---

## Definition of done

- `mining.BreakTicks` treats nil hardness as unbreakable and zero hardness as
  one tick, keeps harvestability separate from speed, and compounds every
  modifier rather than shadowing any.
- Each profile resolves mining conditions from its own version's material
  vocabulary, and a sweep test proves every diggable block resolves one.
- The dig phase accumulates progress, resets on interruption, rejects an
  unbreakable block with a reason, and reports an unknown block as an incomplete
  tick rather than a rejection.
- The client sends start, waits the break time, and sends finish; a cancelled
  dig sends a cancel.
- Computed break times match vanilla exactly across the matrix on both versions,
  and the combinations dropped from the cross product are logged.
- `task lint`, `task test` under `-race`, and `task verify` pass in
  `minecraft-simulation` and `headless-minecraft`.

## Risks

**~~The break-time formula may not be dumpable for 26.1.2.~~ Retired by Task 0.**
This said `mcreference dump` rejects every version but 1.8.9. It no longer does:
`minecraft-reference` carries a typed 26.1.2 dumper, and M9.2 measured that
version's item and arrow motion constants through it and confirmed them in
bytecode. What survives of the risk is the ordinary one — that the method
computing break progress may be harder to reach than the motion constants were —
and the answer is the same as M8.1's: say in the doc comment which of the two
paths, decompiled source and `javap -p -c`, each number came from, and mark a
number that came from neither as unverified rather than letting the two lanes
look equally evidenced.

**The matrix is a sample and must say so.** Four axes multiply to thousands of
cases. Sampling is correct; sampling silently is not. The test logs what it
dropped.

**The anti-cheat lane and the dig timing are coupled.** M10 runs
`headless-minecraft` against Paper with an open-source anti-cheat and requires
no alerts. Dig timing is one of the two or three things such a plugin checks
hardest. If the matrix passes and the anti-cheat lane complains, the defect is
likelier in the packet cadence than in the arithmetic — the same split M8.8's
plan drew between physics problems and cadence problems.

**Effects are observed, not simulated.** Haste and mining fatigue arrive as
`player.effects_changed` events; nothing in this stage makes them tick down.
A dig whose effect expires mid-break will be wrong until an effects rule exists,
and no stage currently owns one. Record it as follow-on rather than quietly
half-implementing it here.
