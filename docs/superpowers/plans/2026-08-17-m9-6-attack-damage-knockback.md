# M9.6 Attack, Damage, and Knockback Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `subagent-driven-development` or execute this plan inline one task at a time. Keep every checkbox current.

**Goal:** Match vanilla on reach validation, damage, knockback, death, and respawn on both Java Edition 1.8.9 and 26.1.2 — including the attack cooldown, which exists on 26.1.2 and does not exist on 1.8.9 — and add the respawn primitive that `examples/orbit` is blocked on.

**Architecture:** This stage is where the two versions diverge most, and the divergence is not a detail: 1.8.9 has no attack cooldown, and 26.1.2's damage depends on one. A gate written as a shared expectation would either fail on 1.8.9 forever or be loosened until it proves nothing. The two-version harness from M9.1b handles this properly — a lane may declare a mechanic **absent for a version with a recorded reason**, which is distinguishable from a lane nobody ran. Everything else here is shared: reach is a distance, knockback is an impulse, and death is health reaching zero.

**Tech Stack:** Go 1.26.6 from `openserbia/go-flake`, Devbox, Task, `minecraft-simulation`'s `sim`, `entity`, `movement`, and `profile/java/*`, `minecraft-protocol`'s generated item, enchantment, effect, and attribute data, `headless-minecraft`'s client and event taxonomy, `relay`'s capture oracle, and pinned vanilla 1.8.9 and 26.1.2 servers.

## Before executing this plan: reconcile it

Depends on M9.3. Symbols specified but not built:

| Symbol | Specified in |
| --- | --- |
| `sim.Command`, `sim.CommandOutcome`, `sim.DomainEvent`, `sim.Phase`, `sim.TickState` | M8.3 plan, Tasks 6, 9 |
| `entity.State`, `entity.Family`, `entity.View` | M8.3 plan, Task 3 |
| `movement.Locomotion`, `movement.LocomotionView` | M8.4 plan, Task 1 |
| `client.Do`, `client.Action` | M8.8 plan, Task 1 |
| `conform.Scenario`, `conform.Lane`, `conform.Absent` | M9.1b plan, Task 5 |
| `conformance.Compare` | M9.3 plan, Task 2 |

Symbols that exist today and were read before writing this plan:
`event.Damage` with `TypeID`/`Typed`, `CauseID`/`Attributed`, `DirectID`/
`Direct`, and position fields; `event.NamePlayerDamaged`, `NamePlayerDied`,
`NamePlayerRespawned`, `NameEntityDamaged`, `NameEntityDied`,
`NamePlayerCooldownChanged`; `world.Player.Damaged`, `Died`, `Respawn`;
`generated/java/*/attributes.go`, `enchantments.go`, `effects.go`.

**The master plan is out of date on one point.** It says M9.6 owns "damage
attribution", and that landed: `event/damage.go` already carries cause, direct
cause, damage type, and position, and already documents that protocol 47 sends
none of it. What M9.6 still owns is the **respawn primitive** — there is no
outbound action path at all until M8.8 Task 1, and no respawn action after it.
Correct the master plan while closing this stage.

**Task 0:** reconcile before touching anything.

## Global Constraints

- Go 1.26.6 from `openserbia/go-flake`; Devbox and Task drive every gate.
- Two-version gate. A mechanic absent from a version is declared absent with a
  reason, never skipped.
- Damage numbers come from generated attribute, enchantment, and effect data.
- The capture oracle must not import `minecraft-simulation`.
- Offline mode, pinned servers with recorded digests.
- `task lint`, `task test` under `-race`, and `task verify` pass before every
  commit.

---

## Design decisions this plan settles

**The attack cooldown is the model case for "absent, not unchecked".** It
arrived in 1.9. On 1.8.9 every swing does full damage regardless of timing; on
26.1.2 a swing before the cooldown fills does reduced damage and no sweep. The
1.8.9 lane declares it absent with that sentence as its reason, and
`conform.Run` records it as `Absent` rather than passing silently. This is the
difference between a report that says "verified absent" and one that says
nothing — and the second is what makes a later regression invisible.

**On protocol 47 the attacker is not knowable, and this stage will not
invent one.** `event/damage.go` already argues this: 47 reports being hurt as an
entity status and says nothing about the source. A caller wanting an attacker on
47 infers one, and the inference is the caller's. That means the 47 lane's death
and retaliation checks assert on *what the player did*, not on who the server
said hit them, and the gate says so rather than quietly checking less.

**Respawn is an action, not a recovery.** The library will not respawn on the
caller's behalf. `examples/orbit`'s design already settled this — "respawning is
an action and actions are the caller's" — and this stage adds the primitive that
makes it possible, not a policy that uses it.

**Knockback is an impulse on the movement kernel, not a position write.** A
knockback that sets a position skips collision and puts entities through walls.
It sets motion, and the next movement tick resolves it through the swept AABB
path M8.2 already proved.

**Reach is validated at both ends and they disagree on purpose.** The client
refuses to send an out-of-reach attack; the server refuses to honour one. The
numbers differ between the two versions and between survival and creative, and
the client's number must be the stricter one — a client that sends attacks the
server rejects is a client an anti-cheat notices.

## File structure

**`minecraft-simulation/combat/`** — new package.

- `combat.go` — `Attack` command, `Reach`, `Outcome`.
- `combat_test.go`
- `damage.go` — damage arithmetic over supplied modifiers.
- `damage_test.go`
- `knockback.go` — the impulse.
- `knockback_test.go`
- `cooldown.go` — the 26.1.2 cooldown, and the type that lets 1.8.9 say it has
  none.
- `cooldown_test.go`
- `phase.go`, `phase_test.go`

**`minecraft-simulation/profile/java/v1_8/`** and **`.../v26_1/`**

- `combat.go` in each — reach distances, cooldown behaviour or its absence,
  enchantment and effect resolution.

**`headless-minecraft/`**

- `client/attack.go`, `client/attack_test.go`
- `client/respawn.go`, `client/respawn_test.go`

**`minecraft-simulation/conformance/`**

- `combat_test.go`, `testdata/combat/`

---

## Task 1: Reach validation

**Files:**
- Create: `minecraft-simulation/combat/combat.go`
- Test: `minecraft-simulation/combat/combat_test.go`

**Interfaces:**
- Produces:

```go
// Reach is how far an entity can act, in blocks.
//
// It is a value supplied by the profile rather than a constant here because
// the two versions differ and because survival and creative differ within each.
// A single number in this package would be wrong three ways out of four.
type Reach struct {
    // Attack is the maximum distance to a target's collision box.
    Attack float64
    // Interact is the maximum distance to a block face, which is not the same
    // number in either version.
    Interact float64
}

// InReach reports whether target's box is within r of eye.
//
// The distance is to the nearest point of the box, not to its centre. Using the
// centre makes a tall entity unhittable at its feet and a wide one hittable
// from outside its own edge, and both look like reach bugs to a caller.
func InReach(eye geom.Vec3, target geom.AABB, r float64) bool
```

- [ ] **Step 1: Write the failing test**

```go
func TestReachIsMeasuredToTheNearestPointOfTheBox(t *testing.T) {
	t.Parallel()

	// A player's box is 1.8 blocks tall. Measuring to its centre makes its
	// feet unreachable from a distance its head is reachable at, which looks
	// like a reach bug and is a geometry bug.
	eye := geom.Vec3{X: 0, Y: 1.62, Z: 0}
	target := geom.AABB{ /* a standing player 3.5 blocks away */ }

	if !combat.InReach(eye, target, 3.0) {
		t.Fatal("a box whose nearest point is 2.6 blocks away was out of reach at 3.0")
	}
}

func TestAnEntityJustBeyondReachIsRefused(t *testing.T) {
	t.Parallel()

	// The boundary is where anti-cheats look, so the test sits on it rather
	// than well inside it.
	eye := geom.Vec3{Y: 1.62}
	if combat.InReach(eye, boxAt(t, 3.01, 0, 0), 3.0) {
		t.Fatal("a box 3.01 blocks away was in reach at 3.0")
	}
	if !combat.InReach(eye, boxAt(t, 2.99, 0, 0), 3.0) {
		t.Fatal("a box 2.99 blocks away was out of reach at 3.0")
	}
}

func TestEachProfileDeclaresItsOwnReach(t *testing.T) {
	t.Parallel()

	// The numbers differ between versions. A shared constant would be wrong on
	// one of them and nobody would notice until an anti-cheat did.
	old := profileFor(t, "1_8_9").Reach()
	modern := profileFor(t, "26_1_2").Reach()

	for _, r := range []combat.Reach{old, modern} {
		if r.Attack <= 0 || r.Interact <= 0 {
			t.Fatalf("a profile declared reach %+v", r)
		}
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd minecraft-simulation && devbox run -- go test ./combat/ -v`

- [ ] **Step 3: Confirm the reach numbers against the game**

Do not take them from memory. Measure them from the captured corpus in Task 6,
or dump them the way M8.1 dumped the physics constants, and record the source in
the doc comment. If one version's number can only be measured rather than
dumped, say so — the same weaker-gate note M8.7's plan requires.

- [ ] **Step 4: Implement**

- [ ] **Step 5: Run the tests and gates**

- [ ] **Step 6: Commit**

```bash
cd minecraft-simulation
git add combat/
git commit -m "feat(combat): reach measured to the nearest point of the box

Measuring to the centre makes a tall entity unhittable at its feet. Each
profile declares its own attack and interact distances; a shared
constant would be wrong on one version."
```

---

## Task 2: The cooldown, and its absence

**Files:**
- Create: `minecraft-simulation/combat/cooldown.go`
- Create: `minecraft-simulation/profile/java/v1_8/combat.go`
- Create: `minecraft-simulation/profile/java/v26_1/combat.go`
- Test: `combat/cooldown_test.go` and one per profile

**Interfaces:**
- Produces:

```go
// Cooldown is the attack-cooldown rule, or the absence of one.
//
// Absence is a value, not a nil: a profile that returns nil forces every caller
// to nil-check and one of them will forget. A profile with no cooldown returns
// one whose Charge is always full, and says so in Reason.
type Cooldown interface {
    // Charge is how filled the cooldown is, in [0,1]. A version without a
    // cooldown returns 1 always.
    Charge(ticksSinceAttack int, attackSpeed float64) float64
    // Present reports whether this version has the mechanic at all.
    Present() bool
    // Reason explains an absence, and is empty when Present is true. It is
    // required rather than optional because the conformance report prints it,
    // and "absent" with no reason is indistinguishable from "not checked".
    Reason() string
}

// NoCooldown is the rule for a version that has none.
func NoCooldown(reason string) Cooldown
```

- [ ] **Step 1: Write the failing test**

```go
func TestVersion1_8_9HasNoCooldownAndSaysWhy(t *testing.T) {
	t.Parallel()

	c := profileFor(t, "1_8_9").Cooldown()
	if c.Present() {
		t.Fatal("1.8.9 reported an attack cooldown; the mechanic arrived in 1.9")
	}
	if c.Reason() == "" {
		t.Fatal("an absent mechanic with no reason is indistinguishable from one " +
			"nobody checked")
	}
	// Full charge always, so shared damage code needs no version branch.
	for _, ticks := range []int{0, 1, 5, 20} {
		if got := c.Charge(ticks, 4.0); got != 1 {
			t.Fatalf("Charge(%d) = %v on 1.8.9, want 1", ticks, got)
		}
	}
}

func TestVersion26_1_2ChargesOverTime(t *testing.T) {
	t.Parallel()

	c := profileFor(t, "26_1_2").Cooldown()
	if !c.Present() {
		t.Fatal("26.1.2 reported no attack cooldown")
	}
	if c.Reason() != "" {
		t.Fatalf("a present mechanic carried an absence reason: %q", c.Reason())
	}

	immediate := c.Charge(0, 4.0)
	partial := c.Charge(3, 4.0)
	full := c.Charge(100, 4.0)

	if !(immediate < partial && partial < full) {
		t.Fatalf("charge did not increase over time: %v, %v, %v",
			immediate, partial, full)
	}
	if full != 1 {
		t.Fatalf("charge saturated at %v, want 1", full)
	}
}

func TestChargeIsClampedAtBothEnds(t *testing.T) {
	t.Parallel()

	// A charge above 1 multiplies damage above vanilla's maximum, which is the
	// kind of defect that passes a happy-path test and fails an anti-cheat.
	c := profileFor(t, "26_1_2").Cooldown()
	for _, ticks := range []int{-5, 0, 1000} {
		got := c.Charge(ticks, 4.0)
		if got < 0 || got > 1 {
			t.Fatalf("Charge(%d) = %v, want it clamped to [0,1]", ticks, got)
		}
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

- [ ] **Step 3: Implement**

`attackSpeed` comes from the generated attribute data
(`generated/java/v26_1/attributes.go`), not from a literal.

- [ ] **Step 4: Run the tests and gates**

- [ ] **Step 5: Commit**

```bash
cd minecraft-simulation
git add combat/cooldown.go profile/java/
git commit -m "feat(combat): the attack cooldown, and 1.8.9's absence of one

Absence is a value with a required reason rather than a nil, so shared
damage code needs no version branch and the conformance report can tell
\"verified absent\" from \"never checked\"."
```

---

## Task 3: Damage and knockback

**Files:**
- Create: `minecraft-simulation/combat/damage.go`
- Create: `minecraft-simulation/combat/knockback.go`
- Test: one `_test.go` each

**Interfaces:**
- Produces:

```go
// Strike is everything that decides how hard one hit lands.
//
// Every number here is supplied. Which enchantment gives what, and which effect
// scales which term, is version data, and this package holds none.
type Strike struct {
    Base       float64 // the weapon's attack damage attribute
    Charge     float64 // the cooldown charge, 1 on a version with no cooldown
    Sharpness  float64 // added damage from enchantment
    Strength   float64 // added damage from effect
    Weakness   float64 // subtracted damage from effect
    Critical   bool
    Sprinting  bool
    KnockbackLevel int
}

// Damage returns the damage one strike deals, before armour.
//
// Armour is deliberately out of scope: it is a per-target reduction over
// attribute and enchantment data that neither version's generated set has been
// checked for, and folding an unverified reduction into a verified strike would
// make the whole number unverified.
func Damage(s Strike) float64

// Knockback returns the impulse to add to the target's motion.
//
// It is an impulse and not a position, because a knockback that writes a
// position skips collision and puts entities through walls. The next movement
// tick resolves it through the swept AABB path M8.2 already proved bit-identical
// to a real server.
func Knockback(from, to geom.Vec3, s Strike, base geom.Vec3) geom.Vec3
```

- [ ] **Step 1: Write the failing test**

```go
func TestAFullyChargedStrikeBeatsAnUnchargedOne(t *testing.T) {
	t.Parallel()

	full := combat.Damage(combat.Strike{Base: 7, Charge: 1})
	weak := combat.Damage(combat.Strike{Base: 7, Charge: 0.2})
	if full <= weak {
		t.Fatalf("charge 1 dealt %v and charge 0.2 dealt %v", full, weak)
	}
}

func TestChargeDoesNotAffectAVersionWithoutACooldown(t *testing.T) {
	t.Parallel()

	// The 1.8.9 path: charge is always 1, so the shared formula reduces to the
	// pre-1.9 one with no branch anywhere.
	if got, want := combat.Damage(combat.Strike{Base: 7, Charge: 1}), 7.0; got != want {
		t.Fatalf("Damage = %v with no modifiers, want the base %v", got, want)
	}
}

func TestWeaknessCanReduceDamageToZeroButNotBelow(t *testing.T) {
	t.Parallel()

	// Negative damage heals the target, which is a real bug shape and not a
	// theoretical one.
	got := combat.Damage(combat.Strike{Base: 1, Charge: 1, Weakness: 100})
	if got < 0 {
		t.Fatalf("Damage = %v; negative damage heals the target", got)
	}
}

func TestKnockbackIsHorizontalAwayFromTheAttacker(t *testing.T) {
	t.Parallel()

	from := geom.Vec3{X: 0, Y: 64, Z: 0}
	to := geom.Vec3{X: 2, Y: 64, Z: 0}

	got := combat.Knockback(from, to, combat.Strike{Charge: 1}, geom.Vec3{})
	if got.X <= 0 {
		t.Fatalf("knockback X = %v, want positive: away from the attacker", got.X)
	}
	if got.Y <= 0 {
		t.Fatalf("knockback Y = %v, want positive: vanilla lifts the target", got.Y)
	}
}

func TestKnockbackAtZeroDistanceIsNotNaN(t *testing.T) {
	t.Parallel()

	// Two entities at exactly the same position is rare and legal. Normalising
	// a zero vector produces NaN, and M8.3's kernel rejects a result containing
	// one — so this would surface as ErrNaNInResult rather than as a wrong
	// knockback, which is worse to diagnose.
	at := geom.Vec3{X: 1, Y: 64, Z: 1}
	got := combat.Knockback(at, at, combat.Strike{Charge: 1}, geom.Vec3{})
	if math.IsNaN(got.X) || math.IsNaN(got.Y) || math.IsNaN(got.Z) {
		t.Fatalf("knockback = %+v at zero distance", got)
	}
}

func TestSprintAndKnockbackEnchantmentBothIncreaseIt(t *testing.T) {
	t.Parallel()

	from, to := geom.Vec3{}, geom.Vec3{X: 2}
	base := combat.Knockback(from, to, combat.Strike{Charge: 1}, geom.Vec3{})
	sprinting := combat.Knockback(from, to, combat.Strike{Charge: 1, Sprinting: true}, geom.Vec3{})
	enchanted := combat.Knockback(from, to, combat.Strike{Charge: 1, KnockbackLevel: 2}, geom.Vec3{})

	if sprinting.X <= base.X {
		t.Fatalf("sprinting knockback %v did not exceed base %v", sprinting.X, base.X)
	}
	if enchanted.X <= base.X {
		t.Fatalf("enchanted knockback %v did not exceed base %v", enchanted.X, base.X)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

- [ ] **Step 3: Derive the constants from the game**

Same route as M8.1 and M9.4: dump or measure, record the source, and mark
anything unverified as unverified in the doc comment rather than in a note
somebody will not read. Assume any constant in this path is a widened Java
`float` until checked — that was M8.1's finding for the motion constants and
nothing suggests combat is different.

- [ ] **Step 4: Implement**

- [ ] **Step 5: Run the tests and gates**

- [ ] **Step 6: Commit**

```bash
cd minecraft-simulation
git add combat/damage.go combat/knockback.go
git commit -m "feat(combat): damage and knockback over supplied modifiers

Knockback is an impulse resolved by the next movement tick, not a
position write that would skip collision. Zero distance does not produce
NaN, which the kernel would reject as an unattributable failure."
```

---

## Task 4: The attack command and phase

**Files:**
- Create: `minecraft-simulation/combat/phase.go`
- Test: `minecraft-simulation/combat/phase_test.go`

**Interfaces:**
- Produces:

```go
// Attack asks the kernel to swing at a target.
type Attack struct {
    Attacker entity.ID
    Target   entity.ID
}

func (Attack) CommandKind() string { return "combat.attack" }

// Phase applies attacks: reach, cooldown, damage, knockback, death.
func Phase() sim.Phase
```

- [ ] **Step 1: Write the failing test**

```go
func TestAnOutOfReachAttackIsRefusedWithAReason(t *testing.T) {
	t.Parallel()

	k := kernelFor(t, worldWithTargetAt(t, 20, 0, 0))
	result := step(t, k, combat.Attack{Attacker: 1, Target: 2})

	outcome := onlyOutcome(t, result)
	if outcome.Accepted {
		t.Fatal("an attack twenty blocks away was accepted")
	}
	if outcome.Reason == "" {
		t.Fatal("a refused attack gave no reason")
	}
	if !result.Changes.IsEmpty() {
		t.Fatal("a refused attack emitted a change set")
	}
}

func TestAnAttackOnAnUnknownEntityIsIncomplete(t *testing.T) {
	t.Parallel()

	k := kernelFor(t, emptyWorld(t))
	result := step(t, k, combat.Attack{Attacker: 1, Target: 99})

	if result.Completeness.Complete {
		t.Fatal("an attack on an entity the kernel has never seen reported a " +
			"complete tick")
	}
	missing := result.Completeness.Missing
	if len(missing) == 0 || missing[0].Kind != sim.DependencyEntity {
		t.Fatalf("Missing = %+v, want an entity dependency", missing)
	}
}

func TestKnockbackAppearsAsMotionNotPosition(t *testing.T) {
	t.Parallel()

	k := kernelFor(t, worldWithTargetAt(t, 1, 0, 0))
	before := entityState(t, k, 2)
	result := step(t, k, combat.Attack{Attacker: 1, Target: 2})

	after := entityStateFrom(t, result, 2)
	if after.Box != before.Box {
		t.Fatal("the attack moved the target's box directly; knockback must be " +
			"motion that the next movement tick resolves through collision")
	}
	if after.Motion == before.Motion {
		t.Fatal("the attack changed no motion")
	}
}

func TestDeathIsEmittedOnceWhenHealthReachesZero(t *testing.T) {
	t.Parallel()

	// Once. A death emitted per tick while the entity is still being removed
	// makes every caller that counts deaths wrong.
	k := kernelFor(t, worldWithFragileTargetAt(t, 1, 0, 0))
	var deaths int
	for range 5 {
		result := step(t, k, combat.Attack{Attacker: 1, Target: 2})
		deaths += countDeaths(result, 2)
	}
	if deaths != 1 {
		t.Fatalf("emitted %d death events for one death", deaths)
	}
}

func TestTheCooldownGatesRepeatedAttacksOn26_1_2Only(t *testing.T) {
	t.Parallel()

	// Two swings in consecutive ticks: full damage twice on 1.8.9, reduced on
	// the second on 26.1.2. This single test states the divergence rather than
	// two tests that could drift apart.
	for _, version := range []string{"1_8_9", "26_1_2"} {
		t.Run(version, func(t *testing.T) {
			k := kernelForProfile(t, version, worldWithTargetAt(t, 1, 0, 0))
			first := damageDealt(t, step(t, k, combat.Attack{Attacker: 1, Target: 2}))
			second := damageDealt(t, step(t, k, combat.Attack{Attacker: 1, Target: 2}))

			if version == "1_8_9" && second != first {
				t.Fatalf("1.8.9 dealt %v then %v; it has no attack cooldown",
					first, second)
			}
			if version == "26_1_2" && second >= first {
				t.Fatalf("26.1.2 dealt %v then %v; the second swing was not "+
					"reduced by the cooldown", first, second)
			}
		})
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

- [ ] **Step 3: Implement**

- [ ] **Step 4: Run the tests and gates**

- [ ] **Step 5: Commit**

```bash
cd minecraft-simulation
git add combat/phase.go combat/phase_test.go
git commit -m "feat(combat): the attack command and its kernel phase

One death event per death. Knockback lands as motion. The cooldown gates
repeated swings on 26.1.2 and provably does not on 1.8.9, stated in one
test rather than two that could drift."
```

---

## Task 5: The attack and respawn primitives

**Files:**
- Create: `headless-minecraft/client/attack.go`
- Create: `headless-minecraft/client/respawn.go`
- Test: one `_test.go` each

**Interfaces:**
- Produces: `client.ActionAttack{Target int32}`, `client.ActionRespawn{}`.

- [ ] **Step 1: Write the failing test**

```go
func TestAnAttackSwingsAndHits(t *testing.T) {
	t.Parallel()

	// Vanilla sends an animation with the attack. A client that sends the
	// interact packet alone hits without swinging, which is visible to other
	// players and to an anti-cheat.
	for _, version := range vanillaVersions(t) {
		t.Run(version.Name, func(t *testing.T) {
			server := vanilla.Start(t, version.Options)
			c := connected(t, server)
			target := spawnTarget(t, server)

			sent := recordOutbound(t, c)
			if err := c.Do(t.Context(), client.ActionAttack{Target: target}); err != nil {
				t.Fatalf("Do: %v", err)
			}

			kinds := combatPacketKinds(t, sent)
			if !slices.Contains(kinds, "swing") {
				t.Fatalf("sent %v, want a swing animation alongside the attack", kinds)
			}
		})
	}
}

func TestAnOutOfReachAttackIsNotSent(t *testing.T) {
	t.Parallel()

	// The client's reach must be the stricter of the two. Sending attacks the
	// server rejects is what an anti-cheat lane notices, and M10 has one that
	// must stay quiet.
	server := vanilla.Start(t, defaultOptions(t))
	c := connected(t, server)
	target := spawnTargetAt(t, server, 20, 64, 20)

	sent := recordOutbound(t, c)
	err := c.Do(t.Context(), client.ActionAttack{Target: target})
	if err == nil {
		t.Fatal("Do accepted an attack twenty blocks away")
	}
	if len(combatPacketKinds(t, sent)) != 0 {
		t.Fatal("an out-of-reach attack was sent anyway")
	}
}

func TestRespawnIsSentOnlyAfterDeath(t *testing.T) {
	t.Parallel()

	// A respawn request from a living player is a protocol error on both
	// versions and a disconnect on some servers.
	server := vanilla.Start(t, defaultOptions(t))
	c := connected(t, server)

	if err := c.Do(t.Context(), client.ActionRespawn{}); err == nil {
		t.Fatal("Do accepted a respawn from a living player")
	}
}

func TestRespawnAfterDeathReturnsThePlayerToPlay(t *testing.T) {
	t.Parallel()

	// This is what examples/orbit is blocked on: its design says the example
	// sends the respawn itself, because respawning is an action and actions
	// are the caller's. The library supplies the primitive and no policy.
	for _, version := range vanillaVersions(t) {
		t.Run(version.Name, func(t *testing.T) {
			server := vanilla.Start(t, version.Options)
			c := connected(t, server)
			events := subscribe(t, c, event.DomainPlayer)

			killPlayer(t, server)
			awaitName(t, events, event.NamePlayerDied)

			if err := c.Do(t.Context(), client.ActionRespawn{}); err != nil {
				t.Fatalf("Do respawn: %v", err)
			}
			awaitName(t, events, event.NamePlayerRespawned)

			if c.World().Player.Dead {
				t.Fatal("the player is still dead after a confirmed respawn")
			}
		})
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

- [ ] **Step 3: Implement**

Respawn is version-owned at the wire — 47 sends a client-status packet, 775
sends its own — and version-neutral in the guard that refuses it while alive.

- [ ] **Step 4: Run the tests and gates**

- [ ] **Step 5: Commit**

```bash
git add client/attack.go client/respawn.go client/*_test.go internal/adapter/
git commit -m "feat(client): attack and respawn primitives

Respawn is an action the caller sends, not a recovery the library
performs. An out-of-reach attack is refused before it is sent, because
the client's reach must be the stricter of the two."
```

---

## Task 6: The gate, the corpus, and the milestone record

**Files:**
- Create: `minecraft-simulation/conformance/combat_test.go`
- Create: `minecraft-simulation/conformance/testdata/combat/`
- Modify: `headless-minecraft/MASTER_PLAN.md`
- Modify: `headless-minecraft/docs/superpowers/specs/2026-08-16-orbit-example-design.md`

- [ ] **Step 1: Capture the corpus on both versions**

Attack a passive mob at the reach boundary and just beyond it; attack with and
without sprint; attack with a knockback enchantment; attack twice in consecutive
ticks; kill and respawn. Record the observed damage, the observed knockback
trajectory, and the death and respawn packets.

The knockback comparison goes through the trace document and
`conformance.Compare`, at the version's own tolerance — a knockback is a
trajectory, and M9.3 already built the comparator for those.

- [ ] **Step 2: Declare the scenarios, with the cooldown one absent on 1.8.9**

```go
conform.Scenario{
	Name: "attack cooldown",
	Lanes: []conform.Lane{
		{
			ProtocolID:   "java/1.8.9",
			AbsentReason: "the attack cooldown arrived in 1.9; every 1.8.9 swing " +
				"deals full damage regardless of timing",
		},
		{ProtocolID: "java/26.1", Recording: "testdata/combat/26_1_2-cooldown.json"},
	},
}
```

Also declare, and say so in the test file: the 47 lane's damage-attribution
checks assert on what the player did rather than on who the server named,
because protocol 47 sends no attacker. That is a smaller claim than the 775
lane's, and it should read as smaller.

- [ ] **Step 3: Run the gate, fix what it names**

- [ ] **Step 4: Unblock `examples/orbit`**

Its design lists retaliation and respawn as M9.6 and marks itself blocked. With
the primitives landed, update its status and run it against both servers. If it
still cannot run, say what is still missing rather than marking the milestone
complete around it.

- [ ] **Step 5: Correct the master plan's stale claim**

It says M9.6 owns respawn *and* damage attribution. Attribution landed in
`event/damage.go` before this stage started. Fix the line rather than leaving a
completed item listed as outstanding.

- [ ] **Step 6: Record the milestone**

Write what the work found. Candidates: whether the reach numbers were dumpable
or only measurable; whether the cooldown charge curve matched on the first try;
how much of the damage formula rested on unverified constants; and whether
`examples/orbit` ran end to end on both versions or only one.

- [ ] **Step 7: Commit**

```bash
git commit -m "docs(plan): close M9.6, and what the combat corpus found"
```

---

## Definition of done

- Reach is measured to the nearest point of the target's box, each profile
  declares its own numbers, and the boundary cases are tested at the boundary.
- The 1.8.9 profile returns a cooldown that is always full and carries a
  recorded reason for its absence; the 26.1.2 profile's charge rises over time
  and is clamped to `[0,1]`.
- Damage never goes negative; knockback is an impulse, never a position, and
  never NaN at zero distance.
- The phase refuses an out-of-reach attack with a reason and no change set,
  reports an unknown target as incomplete, and emits one death per death.
- One test states the cooldown divergence across both versions.
- The client swings when it attacks, refuses an out-of-reach attack before
  sending it, refuses a respawn while alive, and returns to play after a
  confirmed respawn on both versions.
- The cooldown scenario is declared `Absent` on 1.8.9 with a reason, and the 47
  lane's attribution claim is stated as the smaller claim it is.
- `examples/orbit` runs, or what still blocks it is written down.
- `task lint`, `task test` under `-race`, and `task verify` pass in
  `minecraft-simulation` and `headless-minecraft`.

## Risks

**Armour is out of scope and that limits what "damage matches vanilla"
means.** The corpus should attack unarmoured targets, and the gate should say
so. Folding an unverified armour reduction into a verified strike would make the
whole number unverified, which is worse than a smaller claim honestly stated.

**Protocol 47's silence about attackers is a permanent limit, not a gap to
close.** `event/damage.go` already documents it. No amount of work in this stage
makes the 47 lane prove what the 775 lane proves, and the conformance report
must not present them as equivalent.

**The cooldown charge curve is the likeliest thing to be subtly wrong.** It
depends on the attack-speed attribute, on partial ticks, and on constants that
may be widened Java `float`s. A curve that is right at 0 and 1 and wrong in
between passes every boundary test and fails the corpus in the middle, which is
the right place to catch it — provided the corpus samples the middle.

**`examples/orbit` is a consumer, not a gate.** If it does not run, that is
information about this stage. If it runs, that is not proof the stage is
correct — it exercises one path through combat and none of the matrix.
