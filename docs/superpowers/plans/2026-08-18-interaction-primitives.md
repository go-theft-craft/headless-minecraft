# Interaction primitives Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `subagent-driven-development` (recommended) or `executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give `version` the outbound vocabulary a bot needs beyond walking — hold, swing, use, interact, dig, click, drop, close, sneak, chat — so consumers stop waiting on M9.

**Architecture:** Each intent is a version-neutral struct in `version` implementing `Action`, and each adapter's `EncodeAction` switch gains a case that maps it to one packet or refuses it with `version.UnsupportedAction`. No intent is approximated by a packet that means something else. No number owned by M9.4, M9.5, or M9.6 appears anywhere in this work.

**Tech Stack:** Go 1.26.6 from `openserbia/go-flake`, Devbox, Task, `golangci-lint`, the generated `minecraft-protocol` codecs for protocol 47 and 775.

Design: [interaction primitives design](../specs/2026-08-18-interaction-primitives-design.md).

## Before executing this plan: reconcile it

Two committed plans specify these primitives under names that no longer match the code, because both were written before the outbound path landed. **Task 0 settles this and both plans are corrected as part of it.**

| Plan | Says | Shipped code says |
| --- | --- | --- |
| `2026-08-17-m9-6-attack-damage-knockback.md` Task 5 | `client.ActionAttack{Target int32}`, `client.ActionRespawn{}` | Actions live in `version`. `version.ActionRespawn` exists and is encoded by both adapters. |
| `2026-08-17-m9-4-digging-block-breaking.md` Task 5 | `client.ActionDig{Block, Face}` and `client.ActionDigCancel{Block}`, two types | This plan ships one `version.ActionDig` carrying a `Stage`, because the wire is one packet with a status field. |
| `2026-08-17-m9-6-attack...` reconcile block | "there is no outbound action path at all until M8.8 Task 1, and no respawn action after it" | M8.8 is complete and `ActionRespawn` landed ahead of its gate. |

The design settles the third disagreement too: attack is not its own action. Both protocols encode it as a mode of the interact packet, so it is `ActionInteract` with `InteractAttack`, and inventing a separate type would invent a distinction the wire does not make.

## Global Constraints

- Go 1.26.6 from `openserbia/go-flake`; Devbox and Task drive every gate.
- Every action type lives in `version` and implements `Action` with an `ActionKind` returning a stable, unique, snake-case name. It is not a packet name.
- An intent a protocol cannot express is refused with `version.UnsupportedAction`, never approximated. A *field* a protocol cannot express is dropped only when dropping it leaves the intent intact.
- No break time, no reach distance, no cooldown, and no placement legality rule appears in this work. Those are M9.4 through M9.6 and a test here that pinned one would pin a guess.
- Nothing in this plan claims verification against a live server. The gates stay with their milestones, following the `ActionRespawn` precedent the master plan records.
- `devbox run -- task lint`, `devbox run -- task test`, and `devbox run -- task verify` pass before every commit.
- Conventional commit subjects. No `Co-Authored-By` trailer and no `Claude-Session` line.

## File Structure

All paths relative to the `headless-minecraft` repository root.

| File | Responsibility |
| --- | --- |
| `version/action.go` | Unchanged: movement, ground, respawn, input, sprint |
| `version/action_hand.go` | `Hand`, `Face`, `BlockPos`, and the enums the interaction actions share |
| `version/action_interact.go` | `ActionHeldSlot`, `ActionSwing`, `ActionUseItem`, `ActionUseOn`, `ActionReleaseUse`, `ActionInteract` |
| `version/action_dig.go` | `ActionDig`, `DigStage` |
| `version/action_window.go` | `ActionClickSlot`, `ActionDrop`, `ActionCloseWindow` |
| `version/action_state.go` | `ActionEntityAction`, `ActionSwapHands`, `ActionChat`, `ActionCommand` |
| `internal/adapter/v1_8/action.go` | Protocol 47 cases and refusals |
| `internal/adapter/v26_1/action.go` | Protocol 775 cases and refusals |
| `version/action_kind_test.go` | The set-wide kind uniqueness gate |

Split by responsibility rather than into one growing `action.go`: the interaction actions change together when a hand or a face changes, and the window actions change together when P4 lands its menu model.

---

## Task 0: Reconcile the two stale plans

**Files:**
- Modify: `docs/superpowers/plans/2026-08-17-m9-4-digging-block-breaking.md`
- Modify: `docs/superpowers/plans/2026-08-17-m9-6-attack-damage-knockback.md`
- Modify: `MASTER_PLAN.md`

**Interfaces:**
- Produces: nothing in code. This task exists so that two committed plans do not instruct a later worker to build types this plan already built under other names.

**Done 2026-08-18, as part of reconciling the M9.3-M9.8 stage plans rather than
as part of executing this one.** Steps 1 to 3 are ticked because the edits are
in those files; Step 4's commit is not, because that reconciliation pass left its
changes uncommitted for review. Two things went further than this task asked:
M9.4's dig correction landed on its Task 4 rather than its Task 5 (the plan's
numbering moved), and M9.7 and M9.8 were corrected the same way for
`ActionClickSlot`, `ActionCloseWindow`, and the `ClickMode` enumeration — which
this plan names but does not enumerate, and M9.7 does.

- [x] **Step 1: Correct the M9.6 plan's Task 5**

Replace `client.ActionAttack{Target int32}` with `version.ActionInteract{Entity: target, Kind: version.InteractAttack}` and `client.ActionRespawn{}` with `version.ActionRespawn{}` at every occurrence. Replace the reconcile block's claim that there is "no outbound action path at all" with a line recording that M8.8 landed it and that `ActionRespawn` followed.

What M9.6 still owns after this edit is the **scenario** — reach validation, cooldown timing, damage, knockback, death, and the respawn gate — not the primitive. Say so in the plan.

- [x] **Step 2: Correct the M9.4 plan's Task 5**

Replace `client.ActionDig{Block, Face}` and `client.ActionDigCancel{Block}` with the single `version.ActionDig{Block, Face, Stage}`, and record why: the wire is one packet with a status field, so two types would be two names for one packet.

What M9.4 still owns is the break time. Say so.

- [x] **Step 3: Correct the master plan**

In the M9 section, the Task 6 primitive list — "use, place, attack, interact, dig, slot, click, drop, and close stay with M9, mechanic by mechanic" — becomes: the primitives land under the interaction primitives plan; M9.4 through M9.8 keep their gates.

- [ ] **Step 4: Commit**

```bash
git add docs/superpowers/plans/ MASTER_PLAN.md
git commit -m "docs: reconcile the M9.4 and M9.6 plans with the landed action path"
```

---

## Task 1: The shared enums

**Files:**
- Create: `version/action_hand.go`
- Test: `version/action_hand_test.go`

**Interfaces:**
- Produces: `type Hand uint8` with `MainHand`, `OffHand`; `type Face uint8` with `FaceBottom`, `FaceTop`, `FaceNorth`, `FaceSouth`, `FaceWest`, `FaceEast`; `type BlockPos struct{ X, Y, Z int32 }`; `type Cursor struct{ X, Y, Z float32 }`.

Every interaction action carries at least one of these, so they land first and alone.

- [ ] **Step 1: Write the failing test**

```go
func TestFaceValuesMatchTheWireOrder(t *testing.T) {
	t.Parallel()

	// Both protocols number the faces the same way and have since 1.8. A
	// renumbering here places blocks on the wrong side of the target, which
	// looks like a placement bug and is an enum bug.
	cases := []struct {
		face Face
		want uint8
	}{
		{FaceBottom, 0},
		{FaceTop, 1},
		{FaceNorth, 2},
		{FaceSouth, 3},
		{FaceWest, 4},
		{FaceEast, 5},
	}

	for _, c := range cases {
		if uint8(c.face) != c.want {
			t.Fatalf("%v = %d, want %d", c.face, uint8(c.face), c.want)
		}
	}
}

func TestHandStringsAreStable(t *testing.T) {
	t.Parallel()

	if MainHand.String() != "main" || OffHand.String() != "off" {
		t.Fatalf("hand names changed: %q, %q", MainHand, OffHand)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `devbox run -- go test ./version/ -run 'TestFace|TestHand' -v`
Expected: FAIL, undefined.

- [ ] **Step 3: Write minimal implementation**

```go
package version

import "fmt"

// Hand names which hand an action uses.
//
// Protocol 47 has no offhand at all. An action that only names a hand drops
// the field there rather than being refused, because a main-hand use is still
// a use; an action that is *about* the offhand is refused. See ActionSwapHands.
type Hand uint8

const (
	// MainHand is the held item.
	MainHand Hand = iota
	// OffHand is the shield hand. Protocol 775 only.
	OffHand
)

// String returns the hand's name.
func (h Hand) String() string {
	switch h {
	case MainHand:
		return "main"
	case OffHand:
		return "off"
	default:
		return fmt.Sprintf("Hand(%d)", uint8(h))
	}
}

// Face names a side of a block.
//
// The numbering is the wire's and has been since 1.8, which is why the
// constants are written with explicit values rather than left to iota's order:
// a reordering would place blocks on the wrong side of the target and read as
// a placement bug rather than an enum bug.
type Face uint8

const (
	FaceBottom Face = 0
	FaceTop    Face = 1
	FaceNorth  Face = 2
	FaceSouth  Face = 3
	FaceWest   Face = 4
	FaceEast   Face = 5
)

// String returns the face's name.
func (f Face) String() string {
	switch f {
	case FaceBottom:
		return "bottom"
	case FaceTop:
		return "top"
	case FaceNorth:
		return "north"
	case FaceSouth:
		return "south"
	case FaceWest:
		return "west"
	case FaceEast:
		return "east"
	default:
		return fmt.Sprintf("Face(%d)", uint8(f))
	}
}

// BlockPos is the integer block an action targets.
type BlockPos struct{ X, Y, Z int32 }

// Cursor is where within a face the interaction landed, each component in
// [0, 1].
//
// It is not decoration. Both protocols carry it and both read it: which half
// of a slab a placement fills and which way a stair faces are decided by it,
// so an action that always sent the centre would place a different block from
// the one the caller meant.
type Cursor struct{ X, Y, Z float32 }
```

- [ ] **Step 4: Run test to verify it passes**

Run: `devbox run -- go test ./version/ -run 'TestFace|TestHand' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add version/action_hand.go version/action_hand_test.go
git commit -m "feat(version): add the hand, face, and cursor vocabulary"
```

---

## Task 2: Held slot and swing

**Files:**
- Create: `version/action_interact.go`
- Modify: `internal/adapter/v1_8/action.go`
- Modify: `internal/adapter/v26_1/action.go`
- Test: `version/action_interact_test.go`, `internal/adapter/v1_8/action_test.go`, `internal/adapter/v26_1/action_test.go`

**Interfaces:**
- Consumes: `Hand` from task 1.
- Produces: `ActionHeldSlot{Slot uint8}`, `ActionSwing{Hand Hand}`.

These two first because attack with a sword, place a block, cast a rod, and raise a shield all begin by selecting a slot, and none of them is expressible without it.

- [ ] **Step 1: Write the failing test**

```go
func TestHeldSlotEncodesTheHotbarIndex(t *testing.T) {
	t.Parallel()

	packet, err := adapter{}.EncodeAction(version.ActionHeldSlot{Slot: 4})
	if err != nil {
		t.Fatalf("EncodeAction: %v", err)
	}

	body, ok := packet.Body.(*gen.PlayServerboundHeldItemSlot)
	if !ok {
		t.Fatalf("encoded %T, want PlayServerboundHeldItemSlot", packet.Body)
	}
	if body.SlotID != 4 {
		t.Fatalf("SlotID = %d, want 4", body.SlotID)
	}
}

func TestHeldSlotAboveTheHotbarIsRefused(t *testing.T) {
	t.Parallel()

	// The hotbar is nine slots. A tenth is a protocol error on both versions
	// and a disconnect on some servers, so it is refused here rather than
	// sent and regretted.
	if _, err := adapter{}.EncodeAction(version.ActionHeldSlot{Slot: 9}); err == nil {
		t.Fatal("EncodeAction accepted slot 9")
	}
}

func TestSwingEncodesAnArmAnimation(t *testing.T) {
	t.Parallel()

	packet, err := adapter{}.EncodeAction(version.ActionSwing{Hand: version.MainHand})
	if err != nil {
		t.Fatalf("EncodeAction: %v", err)
	}

	if _, ok := packet.Body.(*gen.PlayServerboundArmAnimation); !ok {
		t.Fatalf("encoded %T, want PlayServerboundArmAnimation", packet.Body)
	}
}

func TestSwingWithTheOffHandStillEncodesOn47(t *testing.T) {
	t.Parallel()

	// 47 has no offhand, but a swing is still a swing. The field is dropped
	// rather than the intent refused, which is the rule the design states.
	if _, err := adapter{}.EncodeAction(version.ActionSwing{Hand: version.OffHand}); err != nil {
		t.Fatalf("EncodeAction refused an offhand swing on 47: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `devbox run -- go test ./version/... ./internal/adapter/... -run 'TestHeldSlot|TestSwing' -v`
Expected: FAIL, undefined.

- [ ] **Step 3: Write the actions**

```go
package version

// ActionHeldSlot selects a hotbar slot.
//
// It is the first thing every other interaction needs. Attacking with a sword,
// placing a block, casting a rod, and raising a shield all begin here, and a
// client that cannot select a slot uses whatever it happens to be holding.
type ActionHeldSlot struct {
	// Slot is the hotbar index, 0 through 8.
	Slot uint8
}

// ActionKind implements Action.
func (ActionHeldSlot) ActionKind() string { return "held_slot" }

// ActionSwing swings the arm.
//
// Both protocols send it separately from the attack, and vanilla sends both.
// A client that attacks without swinging hits without any visible motion,
// which other players see and an anti-cheat notices.
type ActionSwing struct {
	// Hand is which arm swings. Protocol 47 has no offhand and ignores it.
	Hand Hand
}

// ActionKind implements Action.
func (ActionSwing) ActionKind() string { return "swing" }
```

- [ ] **Step 4: Add the protocol 47 cases**

In `internal/adapter/v1_8/action.go`, before the `default`:

```go
	case version.ActionHeldSlot:
		if value.Slot > maxHotbarSlot {
			return protocol.Packet{}, fmt.Errorf(
				"%w: slot %d is outside the hotbar", version.ErrUnsupportedAction, value.Slot)
		}

		return play47("held_item_slot", &gen.PlayServerboundHeldItemSlot{
			SlotID: int16(value.Slot),
		}), nil

	case version.ActionSwing:
		// 47's animation packet carries no hand, so the field is dropped. A
		// main-hand swing and an offhand swing are the same bytes here, which
		// is what a protocol with one hand means.
		return play47("arm_animation", &gen.PlayServerboundArmAnimation{}), nil
```

And the constant:

```go
// maxHotbarSlot is the highest selectable hotbar index. The hotbar is nine
// slots on both versions.
const maxHotbarSlot = 8
```

- [ ] **Step 5: Add the protocol 775 cases**

The same two cases in `internal/adapter/v26_1/action.go`, against that version's generated types, with the swing carrying the hand rather than dropping it.

- [ ] **Step 6: Run the tests**

Run: `devbox run -- go test ./version/... ./internal/adapter/... -run 'TestHeldSlot|TestSwing' -v`
Expected: PASS on both adapters.

- [ ] **Step 7: Commit**

```bash
git add version/ internal/adapter/
git commit -m "feat(version): add held-slot and swing actions"
```

---

## Task 3: Use, use-on, release, and interact

**Files:**
- Modify: `version/action_interact.go`
- Modify: `internal/adapter/v1_8/action.go`, `internal/adapter/v26_1/action.go`
- Test: the adapter action tests

**Interfaces:**
- Consumes: `Hand`, `Face`, `BlockPos`, `Cursor` from task 1.
- Produces: `ActionUseItem{Hand}`, `ActionUseOn{Block BlockPos, Face Face, Cursor Cursor, Hand Hand}`, `ActionReleaseUse{Hand}`, `ActionInteract{Entity int32, Kind InteractKind, At *Cursor, Hand Hand}`, `type InteractKind uint8` with `InteractAttack`, `InteractUse`, `InteractUseAt`.

- [ ] **Step 1: Write the failing test**

```go
func TestUseOnCarriesTheCursorNotTheCentre(t *testing.T) {
	t.Parallel()

	// Which half of a slab fills and which way a stair faces are decided by
	// the cursor. An encoder that sent the centre would place a different
	// block from the one the caller asked for.
	packet, err := adapter{}.EncodeAction(version.ActionUseOn{
		Block:  version.BlockPos{X: 10, Y: 64, Z: -3},
		Face:   version.FaceTop,
		Cursor: version.Cursor{X: 0.5, Y: 1, Z: 0.25},
		Hand:   version.MainHand,
	})
	if err != nil {
		t.Fatalf("EncodeAction: %v", err)
	}

	body, ok := packet.Body.(*gen.PlayServerboundBlockPlace)
	if !ok {
		t.Fatalf("encoded %T, want PlayServerboundBlockPlace", packet.Body)
	}
	if body.Direction != int8(version.FaceTop) {
		t.Fatalf("Direction = %d, want %d", body.Direction, version.FaceTop)
	}
	// 47 sends the cursor as a byte per axis, sixteenths of a block.
	if body.CursorY != 16 {
		t.Fatalf("CursorY = %d, want 16", body.CursorY)
	}
}

func TestUseItemInAirUsesTheSentinelPosition(t *testing.T) {
	t.Parallel()

	// 47 has no separate use-in-air packet. It sends a block place at the
	// sentinel position with direction -1, and a server reads that as "used
	// the held item where I stand".
	packet, err := adapter{}.EncodeAction(version.ActionUseItem{Hand: version.MainHand})
	if err != nil {
		t.Fatalf("EncodeAction: %v", err)
	}

	body, ok := packet.Body.(*gen.PlayServerboundBlockPlace)
	if !ok {
		t.Fatalf("encoded %T, want PlayServerboundBlockPlace", packet.Body)
	}
	if body.Direction != -1 {
		t.Fatalf("Direction = %d, want -1", body.Direction)
	}
}

func TestInteractAttackEncodesTheAttackMode(t *testing.T) {
	t.Parallel()

	packet, err := adapter{}.EncodeAction(version.ActionInteract{
		Entity: 42,
		Kind:   version.InteractAttack,
	})
	if err != nil {
		t.Fatalf("EncodeAction: %v", err)
	}

	body, ok := packet.Body.(*gen.PlayServerboundUseEntity)
	if !ok {
		t.Fatalf("encoded %T, want PlayServerboundUseEntity", packet.Body)
	}
	if body.Target != 42 {
		t.Fatalf("Target = %d, want 42", body.Target)
	}
	if body.Mouse != attackMouse47 {
		t.Fatalf("Mouse = %d, want the attack mode %d", body.Mouse, attackMouse47)
	}
}

func TestInteractUseAtWithoutAPositionIsRefused(t *testing.T) {
	t.Parallel()

	// Use-at is defined by the position. Sending it without one is a packet
	// the server cannot read, so it is refused rather than sent with zeros.
	_, err := adapter{}.EncodeAction(version.ActionInteract{
		Entity: 42,
		Kind:   version.InteractUseAt,
	})
	if err == nil {
		t.Fatal("EncodeAction accepted use-at with no position")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `devbox run -- go test ./internal/adapter/... -run 'TestUseOn|TestUseItem|TestInteract' -v`
Expected: FAIL, undefined.

- [ ] **Step 3: Write the actions**

```go
// InteractKind names what an interaction with an entity does.
//
// Attack is a mode here rather than an action of its own, because both
// protocols encode it as a mode of the interact packet. A separate ActionAttack
// would invent a distinction the wire does not make, and an adapter would spend
// its first line undoing it.
type InteractKind uint8

const (
	// InteractAttack hits the entity.
	InteractAttack InteractKind = iota
	// InteractUse right-clicks the entity.
	InteractUse
	// InteractUseAt right-clicks a point on the entity. The point decides
	// where a saddle, a name tag, or an armour stand's item goes.
	InteractUseAt
)

// ActionUseItem uses the held item where the player stands: eat, drink, cast a
// rod, raise a shield.
type ActionUseItem struct {
	Hand Hand
}

// ActionKind implements Action.
func (ActionUseItem) ActionKind() string { return "use_item" }

// ActionUseOn uses the held item against a block: place, open, till.
type ActionUseOn struct {
	Block  BlockPos
	Face   Face
	Cursor Cursor
	Hand   Hand
}

// ActionKind implements Action.
func (ActionUseOn) ActionKind() string { return "use_on" }

// ActionReleaseUse stops using the held item: fire the bow, lower the shield,
// stop eating.
//
// Without it a drawn bow stays drawn. Every use that has a duration needs a
// way to end, and no protocol infers it.
type ActionReleaseUse struct {
	Hand Hand
}

// ActionKind implements Action.
func (ActionReleaseUse) ActionKind() string { return "release_use" }

// ActionInteract attacks or interacts with an entity.
type ActionInteract struct {
	Entity int32
	Kind   InteractKind
	// At is where on the entity the interaction landed, and is required for
	// InteractUseAt and ignored otherwise.
	At   *Cursor
	Hand Hand
}

// ActionKind implements Action.
func (ActionInteract) ActionKind() string { return "interact" }
```

- [ ] **Step 4: Add the protocol 47 cases**

`ActionUseOn` maps to `PlayServerboundBlockPlace` with the cursor scaled to sixteenths. `ActionUseItem` maps to the same packet at the sentinel position with `Direction: -1`. `ActionReleaseUse` maps to `PlayServerboundBlockDig` with the release status — 47 carries it there and not on a use packet. `ActionInteract` maps to `PlayServerboundUseEntity`.

```go
// attackMouse47 is the interact packet's attack mode on protocol 47. Zero is
// interact, one is attack, two is interact-at.
const attackMouse47 = 1

// releaseUseStatus47 is the block-dig status that finishes an item use. 47 has
// no use-release packet: the dig packet carries it at the sentinel position,
// which is why a bow is fired through what looks like a mining packet.
const releaseUseStatus47 = 5
```

- [ ] **Step 5: Add the protocol 775 cases**

775 has separate use and use-on packets and carries the hand on each. The release is its own status there too; follow the generated names.

- [ ] **Step 6: Run the tests**

Run: `devbox run -- go test ./internal/adapter/... -run 'TestUseOn|TestUseItem|TestInteract' -v`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add version/ internal/adapter/
git commit -m "feat(version): add use, use-on, release, and interact actions"
```

---

## Task 4: Dig

**Files:**
- Create: `version/action_dig.go`
- Modify: both adapters
- Test: the adapter action tests

**Interfaces:**
- Consumes: `BlockPos`, `Face` from task 1.
- Produces: `ActionDig{Block BlockPos, Face Face, Stage DigStage}`, `type DigStage uint8` with `DigStart`, `DigCancel`, `DigFinish`.

One type with a stage, not three types. The wire is one packet with a status field.

- [ ] **Step 1: Write the failing test**

```go
func TestDigStagesMapToTheirStatuses(t *testing.T) {
	t.Parallel()

	cases := []struct {
		stage version.DigStage
		want  int32
	}{
		{version.DigStart, 0},
		{version.DigCancel, 1},
		{version.DigFinish, 2},
	}

	for _, c := range cases {
		t.Run(c.stage.String(), func(t *testing.T) {
			t.Parallel()

			packet, err := adapter{}.EncodeAction(version.ActionDig{
				Block: version.BlockPos{X: 1, Y: 2, Z: 3},
				Face:  version.FaceTop,
				Stage: c.stage,
			})
			if err != nil {
				t.Fatalf("EncodeAction: %v", err)
			}

			body, ok := packet.Body.(*gen.PlayServerboundBlockDig)
			if !ok {
				t.Fatalf("encoded %T, want PlayServerboundBlockDig", packet.Body)
			}
			if body.Status != c.want {
				t.Fatalf("Status = %d, want %d", body.Status, c.want)
			}
		})
	}
}

func TestDigCarriesNoTiming(t *testing.T) {
	t.Parallel()

	// This is a guard, not a behaviour test. Break time is M9.4's, and an
	// ActionDig that grew a duration field would be this package claiming a
	// number it has not measured.
	var action version.ActionDig
	if reflect.TypeOf(action).NumField() != 3 {
		t.Fatalf("ActionDig has %d fields, want exactly Block, Face, and Stage",
			reflect.TypeOf(action).NumField())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `devbox run -- go test ./internal/adapter/... -run TestDig -v`
Expected: FAIL, undefined.

- [ ] **Step 3: Write the action**

```go
package version

import "fmt"

// DigStage names one step of breaking a block.
type DigStage uint8

const (
	// DigStart begins breaking.
	DigStart DigStage = iota
	// DigCancel abandons a break in progress.
	DigCancel
	// DigFinish reports that the block should now be broken.
	DigFinish
)

// String returns the stage's name.
func (s DigStage) String() string {
	switch s {
	case DigStart:
		return "start"
	case DigCancel:
		return "cancel"
	case DigFinish:
		return "finish"
	default:
		return fmt.Sprintf("DigStage(%d)", uint8(s))
	}
}

// ActionDig reports one stage of breaking a block.
//
// It carries no timing. How long a block takes for a tool, a tier, and an
// effect is M9.4's measurement, and *when* to send DigFinish is therefore the
// caller's decision rather than something this package schedules. A client that
// finishes early is one the server rejects, and a package that guessed the
// interval would be guessing on every caller's behalf at once.
//
// One type with a stage rather than three types, because the wire is one packet
// with a status field. Two names for one packet is two things to keep in step.
type ActionDig struct {
	Block BlockPos
	Face  Face
	Stage DigStage
}

// ActionKind implements Action.
func (ActionDig) ActionKind() string { return "dig" }
```

- [ ] **Step 4: Add both adapter cases**

Map `DigStart`, `DigCancel`, and `DigFinish` to statuses 0, 1, and 2 on both protocols, with the block position converted to the generated `Position` type.

- [ ] **Step 5: Run the tests**

Run: `devbox run -- go test ./internal/adapter/... -run TestDig -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add version/action_dig.go internal/adapter/
git commit -m "feat(version): add the dig action"
```

---

## Task 5: Windows, state, and speech

**Files:**
- Create: `version/action_window.go`, `version/action_state.go`
- Modify: both adapters
- Test: the adapter action tests

**Interfaces:**
- Produces: `ActionClickSlot{Window uint8, Slot int16, Button uint8, Mode ClickMode, Transaction int16}`, `ActionDrop{Whole bool}`, `ActionCloseWindow{Window uint8}`, `ActionEntityAction{Kind EntityActionKind}`, `ActionSwapHands{}`, `ActionChat{Message string}`, `ActionCommand{Command string}`.
- Modifies: `ActionSprint` becomes a caller of `ActionEntityAction` rather than a separate wire concept.

- [ ] **Step 1: Write the failing test**

```go
func TestSwapHandsIsRefusedOn47(t *testing.T) {
	t.Parallel()

	// 47 has no offhand, so a hand swap is not a field to drop — it is the
	// whole intent. This is the refusal side of the rule the design states.
	_, err := adapter{}.EncodeAction(version.ActionSwapHands{})
	if !errors.Is(err, version.ErrUnsupportedAction) {
		t.Fatalf("EncodeAction = %v, want ErrUnsupportedAction", err)
	}
	if !strings.Contains(err.Error(), "swap_hands") {
		t.Fatalf("error %q does not name the action kind", err)
	}
}

func TestCommandOn47IsChatWithASlash(t *testing.T) {
	t.Parallel()

	// 1.8.9 has no command packet. A leading slash in chat is how a command
	// was sent, and a caller must not have to know that.
	command, err := adapter{}.EncodeAction(version.ActionCommand{Command: "gamemode creative"})
	if err != nil {
		t.Fatalf("EncodeAction: %v", err)
	}
	chat, err := adapter{}.EncodeAction(version.ActionChat{Message: "/gamemode creative"})
	if err != nil {
		t.Fatalf("EncodeAction: %v", err)
	}

	if !reflect.DeepEqual(command.Body, chat.Body) {
		t.Fatalf("command encoded as %+v, want the same bytes as %+v", command.Body, chat.Body)
	}
}

func TestSprintStillEncodesAsItDidBefore(t *testing.T) {
	t.Parallel()

	// ActionSprint becomes a caller of ActionEntityAction. Its bytes must not
	// move: this is a refactor, and a refactor that changes the wire is a
	// change.
	packet, err := adapter{}.EncodeAction(version.ActionSprint{Sprinting: true})
	if err != nil {
		t.Fatalf("EncodeAction: %v", err)
	}

	body, ok := packet.Body.(*gen.PlayServerboundEntityAction)
	if !ok {
		t.Fatalf("encoded %T, want PlayServerboundEntityAction", packet.Body)
	}
	if body.ActionID != sprintStartAction47 {
		t.Fatalf("ActionID = %d, want %d", body.ActionID, sprintStartAction47)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `devbox run -- go test ./internal/adapter/... -run 'TestSwapHands|TestCommand|TestSprint' -v`
Expected: FAIL.

- [ ] **Step 3: Write the actions and both adapter cases**

`ActionEntityAction` carries `EntityActionKind` with `SneakStart`, `SneakStop`, `SprintStart`, `SprintStop`, `LeaveBed`, and the horse members each protocol defines. On 47 all of these are `PlayServerboundEntityAction` with the player's own entity ID and the numbered action.

`ActionSwapHands` returns `version.UnsupportedAction(ProtocolID, action)` on 47.

`ActionCommand` on 47 encodes as `PlayServerboundChat` with `"/"` prefixed when the command does not already begin with one.

- [ ] **Step 4: Run the tests**

Run: `devbox run -- go test ./internal/adapter/... -run 'TestSwapHands|TestCommand|TestSprint' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add version/ internal/adapter/
git commit -m "feat(version): add window, state, and speech actions"
```

---

## Task 6: The set-wide gates

**Files:**
- Create: `version/action_kind_test.go`
- Create: `internal/adapter/coverage_test.go`

**Interfaces:**
- Consumes: every action from tasks 2 through 5.
- Produces: nothing. These are the gates that stop a later action being added without a decision.

- [ ] **Step 1: Write the kind uniqueness gate**

```go
func TestEveryActionKindIsUniqueAndSnakeCase(t *testing.T) {
	t.Parallel()

	actions := allActions(t)

	seen := make(map[string]string, len(actions))
	for _, action := range actions {
		kind := action.ActionKind()

		if kind == "" {
			t.Fatalf("%T has an empty kind", action)
		}
		if previous, clash := seen[kind]; clash {
			t.Fatalf("%T and %s share the kind %q", action, previous, kind)
		}
		if !snakeCase.MatchString(kind) {
			t.Fatalf("%T has kind %q, want lower snake_case", action, kind)
		}

		seen[kind] = fmt.Sprintf("%T", action)
	}
}
```

`allActions` returns one zero value of every exported action type, and lives beside this test. Adding an action without adding it to that list fails the coverage gate below, which is the point.

- [ ] **Step 2: Write the coverage gate**

```go
func TestEveryActionEitherEncodesOrRefusesOnBothProtocols(t *testing.T) {
	t.Parallel()

	// The gate is not that every action works everywhere. It is that no action
	// falls through undecided: an adapter either encodes it or names it in a
	// refusal. Adding an action and forgetting one protocol fails here rather
	// than at runtime on somebody's server.
	for _, adapter := range bothAdapters(t) {
		for _, action := range allActions(t) {
			t.Run(fmt.Sprintf("%s/%s", adapter.Name, action.ActionKind()), func(t *testing.T) {
				t.Parallel()

				_, err := adapter.EncodeAction(action)
				if err == nil {
					return
				}
				if !errors.Is(err, version.ErrUnsupportedAction) {
					t.Fatalf("EncodeAction = %v, want nil or ErrUnsupportedAction", err)
				}
				if !strings.Contains(err.Error(), action.ActionKind()) {
					t.Fatalf("refusal %q does not name the kind %q", err, action.ActionKind())
				}
			})
		}
	}
}
```

- [ ] **Step 3: Write the no-numbers gate**

```go
func TestNoMilestoneNumbersLeakedIntoVersion(t *testing.T) {
	t.Parallel()

	// Break times, reach distances, and cooldowns belong to M9.4 through M9.6.
	// A constant here named for one of them is this package claiming a
	// measurement it never made.
	forbidden := []string{"BreakTime", "Reach", "Cooldown", "Hardness"}

	source := readPackageSource(t, "version")
	for _, name := range forbidden {
		if strings.Contains(source, name) {
			t.Fatalf("version mentions %q, which belongs to a milestone gate", name)
		}
	}
}
```

- [ ] **Step 4: Run every gate**

Run: `devbox run -- task verify`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add version/ internal/adapter/
git commit -m "test(version): gate action kinds and per-protocol coverage"
```

---

## Definition of done

- A bot can select a hotbar slot, swing, use an item, use against a block, release a use, interact with or attack an entity, dig in three stages, click a slot, drop, close a window, sneak, swap hands, chat, and issue a command, through `Client.Do`.
- Every action either encodes on both protocols or refuses on the one that cannot express it, proved by the coverage gate in task 6 rather than by a reviewer's reading.
- `ActionSprint` produces the same bytes it produces today after becoming a caller of `ActionEntityAction`.
- No break time, reach distance, or cooldown appears in `version`, proved by the gate in task 6.
- The M9.4 and M9.6 plans and the master plan name the types this plan actually built.
- `devbox run -- task verify` passes.

## Risks

| Risk | Mitigation |
| --- | --- |
| A later worker follows the stale M9.4 or M9.6 plan and builds `client.ActionDig` alongside `version.ActionDig` | Task 0 is first and corrects both plans before any code is written |
| 47's release-use through the dig packet looks like a bug and gets "fixed" | The constant is named `releaseUseStatus47` and its comment says why. `TestUseOn` and the release test both pin it |
| An action is added later and silently falls through one adapter's default | The coverage gate in task 6 fails on any action not named in a refusal |
| The cursor scaling to sixteenths is wrong and places blocks subtly wrong | `TestUseOnCarriesTheCursorNotTheCentre` pins one non-centre value; M9.5's gate measures the rest against vanilla |
