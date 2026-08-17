# M9.4 Digging and Block Breaking Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `subagent-driven-development` or execute this plan inline one task at a time. Keep every checkbox current.

**Goal:** Compute block break times that match vanilla across every tool, block, and effect combination on both Java Edition 1.8.9 and 26.1.2, and drive the dig sequence through the client so a real server accepts it.

**Architecture:** Break time is a pure function of hardness, tool speed, harvest legality, enchantment, effects, and whether the player is underwater or airborne. All six inputs are already generated data in `minecraft-protocol` — `blocks.go` carries `Hardness` and `HarvestTools`, `materials.go` carries `ToolSpeeds`, `enchantments.go` and `effects.go` carry the rest. So the rule is a kernel phase over data that exists, and the hard part is not the formula but the fact that **the two versions classify blocks by incompatible material vocabularies**. The gate is a matrix run against captured vanilla traces, plus a live dig against a pinned server.

**Tech Stack:** Go 1.26.6 from `openserbia/go-flake`, Devbox, Task, `minecraft-simulation`'s `sim`, `world`, and `profile/java/*`, `minecraft-protocol`'s generated block, material, item, enchantment, and effect data, `relay`'s capture oracle, and pinned vanilla 1.8.9 and 26.1.2 servers.

## Before executing this plan: reconcile it

This plan depends on M9.3 and on the M8 contracts it named. Symbols specified
but not yet built, with their source:

| Symbol | Specified in |
| --- | --- |
| `sim.Phase`, `sim.TickState`, `sim.Command`, `sim.DomainEvent`, `sim.Profile` | M8.3 plan, Tasks 6, 9 |
| `world.BlockRef`, `world.View`, `world.StateView` | M8.3 plan, Task 2 |
| `profile/java/v1_8.New`, `profile/java/v26_1.New` and their block tables | M8.4 Task 6–7, M8.7 Task 4–5 |
| `client.Do`, `client.Action` | M8.8 plan, Task 1 |
| `conformance.Compare`, `conformance.Document` | M9.3 plan, Task 2 |
| `conform.Scenario`, `conform.Lane`, `conform.Run` | M9.1b plan, Task 5 |

Symbols that **do** exist today and were read before writing this plan:
`data.Block.Hardness` (a `*float64`), `data.Block.HarvestTools`
(`data.HarvestToolSet`, a set of item IDs), `data.Block.Material` (a string),
`data.Material.ToolSpeeds` (`data.ToolSpeedIndex`, item ID to multiplier), and
the per-version registries in `generated/java/v1_8` and `generated/java/v26_1`.

**Task 0:** reconcile the unlanded names against what shipped before touching
anything else.

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
`Material` is `"rock"`. In `generated/java/v26_1/materials.go` they are
`mineable/pickaxe`, `mineable/axe`, `gourd`, `coweb`, `default`, and a family of
`incorrect_for_<tier>_tool` entries, and stone's `Material` is
`"mineable/pickaxe"`. These are not renames of one another: 26.1 encodes tool
*correctness* as materials, which 1.8.9 encodes as `HarvestTools` alone. A
shared lookup keyed by material name would silently miss on every 26.1 block.
The formula is shared; the classification is version-owned, and it lives in each
profile's block table.

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

**`headless-minecraft/`**

- `client/dig.go` — the three-packet dig sequence behind one action.
- `client/dig_test.go`

**`minecraft-simulation/conformance/`**

- `mining_test.go` — the matrix gate.
- `testdata/mining/` — the captured break-time corpus, per version.

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

- [ ] **Step 1: Write the failing test**

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

- [ ] **Step 2: Run it to verify it fails**

Run: `cd minecraft-simulation && devbox run -- go test ./mining/ -v`
Expected: FAIL — the package does not exist.

- [ ] **Step 3: Derive the formula from the game, not from a wiki**

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

- [ ] **Step 4: Implement**

- [ ] **Step 5: Run the tests and gates**

Run: `cd minecraft-simulation && devbox run -- task verify`

- [ ] **Step 6: Commit**

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

```go
// Conditions resolves everything version-specific about breaking one block
// with one held item.
//
// This is per-version because the two editions classify blocks by different
// vocabularies. 1.8.9's material for stone is "rock"; 26.1.2's is
// "mineable/pickaxe", and 26.1.2 additionally encodes tool correctness as
// materials named "incorrect_for_<tier>_tool" that 1.8.9 has no counterpart
// for. A shared lookup keyed by material name would miss on every 26.1 block.
func (p *Profile) Conditions(ref world.BlockRef, held ItemStack, effects Effects, underwater, airborne bool) (mining.Conditions, error)

// Hardness returns the block's hardness, or nil when it has none.
func (p *Profile) Hardness(ref world.BlockRef) *float64
```

- [ ] **Step 1: Write the failing test**

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

- [ ] **Step 2: Run it to verify it fails**

Run: `cd minecraft-simulation && devbox run -- go test ./profile/java/... -run Classif -v`
Expected: FAIL.

- [ ] **Step 3: Implement each version**

For 1.8.9: `Material` is a plain name; look it up in the material registry and
take `ToolSpeeds[heldItemID]`, defaulting to 1. `Harvestable` is
`HarvestTools[heldItemID]`, and a block with an empty `HarvestTools` is
harvestable by anything, including a bare hand — an empty set means "no tool
required", not "no tool works".

For 26.1.2: the same shape, but `Material` may be a compound like
`"gourd;mineable/axe"` — split on `;` and take the best speed across the parts.
Handle the `incorrect_for_<tier>_tool` materials explicitly: they encode
correctness, and treating them as ordinary speed tables gives a fast break where
vanilla gives a slow one.

- [ ] **Step 4: Run the tests and gates**

Run: `cd minecraft-simulation && devbox run -- task verify`

- [ ] **Step 5: Commit**

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

- [ ] **Step 1: Write the failing test**

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

- [ ] **Step 2: Run it to verify it fails**

Run: `cd minecraft-simulation && devbox run -- go test ./mining/ -run Phase -v`
Expected: FAIL.

- [ ] **Step 3: Implement**

- [ ] **Step 4: Run the tests and gates**

Run: `cd minecraft-simulation && devbox run -- task verify`

- [ ] **Step 5: Commit**

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
- Consumes: `client.Action`, `version.Adapter`.
- Produces: `client.ActionDig{Block, Face}` and `client.ActionDigCancel{Block}`.

- [ ] **Step 1: Write the failing test**

```go
func TestADigSendsStartThenFinish(t *testing.T) {
	t.Parallel()

	// Vanilla sends start-digging, waits the break time, then sends
	// finish-digging. A client that sends only the finish packet breaks blocks
	// instantly and is the first thing an anti-cheat notices — which matters,
	// because M10 has an anti-cheat lane that must stay quiet.
	for _, version := range vanillaVersions(t) {
		t.Run(version.Name, func(t *testing.T) {
			server := vanilla.Start(t, version.Options)
			c := connected(t, server)

			sent := recordOutbound(t, c)
			if err := c.Do(t.Context(), client.ActionDig{
				Block: world.BlockPosition{X: 0, Y: 63, Z: 0},
				Face:  client.FaceTop,
			}); err != nil {
				t.Fatalf("Do: %v", err)
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
	if err := c.Do(t.Context(), client.ActionDig{
		Block: world.BlockPosition{X: 0, Y: 63, Z: 0}, Face: client.FaceTop,
	}); err != nil {
		t.Fatalf("Do: %v", err)
	}
	awaitBlockBroken(t, c)

	want := expectedBreakDuration(t, c, world.BlockPosition{X: 0, Y: 63, Z: 0})
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
	_ = c.Do(ctx, client.ActionDig{
		Block: world.BlockPosition{X: 0, Y: 63, Z: 0}, Face: client.FaceTop,
	})

	kinds := digPacketKinds(t, sent)
	if slices.Contains(kinds, "finish") {
		t.Fatalf("sent %v; a cancelled dig must not claim to have finished", kinds)
	}
	if !slices.Contains(kinds, "cancel") {
		t.Fatalf("sent %v, want a cancel", kinds)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd headless-minecraft && devbox run -- go test ./client/ -run Dig -v`
Expected: FAIL.

- [ ] **Step 3: Implement**

The three packets are version-owned and belong in each adapter — 47 and 775
number the dig statuses differently and 775 adds a sequence field the server
echoes. The sequencing and the timing are version-neutral and belong in
`client/dig.go`.

- [ ] **Step 4: Run the tests and gates**

Run: `cd headless-minecraft && devbox run -- task verify`

- [ ] **Step 5: Commit**

```bash
git add client/dig.go client/dig_test.go internal/adapter/
git commit -m "feat(client): dig as start, wait, finish

A client that sends only the finish packet breaks blocks instantly,
which is the first thing an anti-cheat notices. Cancelling sends a
cancel rather than claiming to have finished."
```

---

## Task 5: The matrix gate

**Files:**
- Create: `minecraft-simulation/conformance/mining_test.go`
- Create: `minecraft-simulation/conformance/testdata/mining/*.json`

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

Run: `cd minecraft-simulation && devbox run -- go test ./conformance/ -run BreakTimes -v`

- [ ] **Step 4: Declare the scenario and run the gate**

Register `mining` as a `conform.Scenario` with a lane per version.

- [ ] **Step 5: Commit**

```bash
cd minecraft-simulation
git add conformance/mining_test.go conformance/testdata/mining/
git commit -m "test(conformance): break times match vanilla across the matrix

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

**The break-time formula may not be dumpable for 26.1.2.** M8.7's plan already
found that `mcreference dump` rejects every version but 1.8.9 and that no
deobfuscated 26.1.2 server jar is held. If that is still true when this stage
runs, the 26.1.2 constants rest on the captured corpus alone, which catches a
wrong formula and not a constant wrong in the fifteenth decimal place. Say so in
the doc comment and in the master plan rather than letting the two lanes look
equally verified.

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
