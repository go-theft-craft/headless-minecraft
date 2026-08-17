# M9.7 Containers and Inventory Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `subagent-driven-development` or execute this plan inline one task at a time. Keep every checkbox current.

**Goal:** Match vanilla on window open and close, slot synchronisation, and rejected moves, on both Java Edition 1.8.9 and 26.1.2 — and find out first whether the 26.1.2 window data this depends on is usable at all.

**Architecture:** M7 already built the observation half: `world/containers.go` tracks open windows, slots, cursor, recipes, and trades, and the seven `container.*` events publish every change. What is missing is the action half — clicking a slot — and the reconciliation half, which is where the two versions diverge hardest. Protocol 47 confirms a click with a transaction packet the client must echo, and a click the server rejects is followed by an apology; protocol 775 stamps every inventory change with a state ID and rolls the client back by resending the whole window. These are not two spellings of one mechanism, and a shared implementation of "did my click land" is not possible.

**Tech Stack:** Go 1.26.6 from `openserbia/go-flake`, Devbox, Task, `headless-minecraft`'s `world`, `event`, `client`, and version adapters, `minecraft-protocol`'s generated window and item data, `relay`'s capture oracle, and pinned vanilla 1.8.9 and 26.1.2 servers.

## Before executing this plan: reconcile it

Depends on M9.5. Symbols specified but not built:

| Symbol | Specified in |
| --- | --- |
| `client.Do`, `client.Action` | M8.8 plan, Task 1 |
| `conform.Scenario`, `conform.Lane` | M9.1b plan, Task 5 |
| `conformance` corpus loading | M9.3 plan, Task 2 |

Symbols that exist today and were read before writing this plan:
`world.Containers` with `Opened`, `Closed`, `SlotsChanged`, `PropertyChanged`,
`PlayerSlotChanged`, `CursorChanged`, `RecipesChanged`, `TradesChanged`,
`CraftResponse`; `world.ContainerView`, `world.ContainersView.Get`;
`event.NameContainerOpened`, `Closed`, `SlotsChanged`, `CursorChanged`,
`RecipesChanged`, `TradesChanged`, `CraftResponse`;
`data.Window`, `data.WindowSlot`, `data.WindowRegistry`.

**Task 0:** reconcile before touching anything. Then do Task 1, which may
change the shape of this whole stage.

## Global Constraints

- Go 1.26.6 from `openserbia/go-flake`; Devbox and Task drive every gate.
- Two-version gate, with absence declared and reasoned where a mechanic differs.
- Slot layouts come from generated window data, never from constants in a rule
  — subject to what Task 1 finds.
- The capture oracle must not import `minecraft-simulation`.
- Offline mode, pinned servers with recorded digests.
- `task lint`, `task test` under `-race`, and `task verify` pass before every
  commit.

---

## Design decisions this plan settles

**The 26.1.2 window data is an alias of Java 1.16.1, and the generated code
says so out loud.** `generated/java/v26_1/windows.go` carries this comment:

> Upstream publishes no windows dataset for Java 26.1. The pinned tree resolves
> the alias to Java 1.16.1, which is what these records describe, so a window
> here may name slots and properties the running server no longer has.

That is roughly a decade of releases between the data and the server. It is one
of the six aliased datasets M4 recorded. **Task 1 exists to find out how wrong
it is before anything is built on it**, because every other task in this stage
would otherwise be written against slot indices that may not exist. This is the
same shape as M8.7's Task 1: start with the feasibility question, and let the
answer decide whether the milestone's gate is the strong one or the weak one.

**Confirmation is not a shared mechanism.** 47's transaction packet and 775's
state ID answer the same question and answer it in incompatible ways. Each
version's adapter owns its answer, and the shared code asks only "did this click
land, and if not what is the truth" — one boolean and one authoritative window.

**A rejected click must roll the client back, and nothing announces that on
47.** On 775 the server resends the window. On 47 the server sends an apology
transaction and the client is expected to know what it had. A client that does
not track its own pre-click state cannot roll back on 47 at all, so the click
path keeps the prior slot contents until confirmation arrives. This is the same
silent-desynchronisation shape M9.5 found for refused placements, and it is
worse here because a wrong inventory is invisible until a craft fails.

**Shift-click drains, it does not act once.** M3's session findings recorded a
real defect of exactly this shape: a shift-click handler that crafted once
instead of draining the grid. It is fixed and tested, and this stage verifies
the behaviour against vanilla rather than re-litigating it — but the test lives
here because this is the stage that owns click modes.

## File structure

**`headless-minecraft/`**

- `world/containers.go` — modify. Pre-click snapshot and rollback.
- `client/window.go` — new. `ActionClickSlot`, `ActionCloseWindow`, and the
  click-mode vocabulary.
- `client/window_test.go`
- `internal/adapter/v1_8/window.go` — the transaction confirmation path.
- `internal/adapter/v26_1/window.go` — the state-ID path.
- `internal/conformance/windows_test.go` — Task 1's audit.

**`minecraft-simulation/conformance/`**

- `containers_test.go`, `testdata/containers/`

---

## Task 1: Find out whether the 26.1.2 window data is usable

This task produces a written answer, not code. Everything after it depends on
the answer, and building first would mean building against slot indices from
Java 1.16.1.

**Files:**
- Create: `headless-minecraft/internal/conformance/windows_test.go`
- Create: `headless-minecraft/docs/verification/2026-08-17-window-data-audit.md`

- [ ] **Step 1: Open a window of every type against the pinned 26.1.2 server**

For each window the registry names, open it against the real server through the
proxy and record what the server actually sent: the window type identifier, the
slot count, and the property count.

- [ ] **Step 2: Write the audit test**

```go
func TestTheWindowRegistryMatchesTheServer(t *testing.T) {
	t.Parallel()

	// The 26.1 window dataset is an alias of Java 1.16.1 — the generated file
	// says so in its own doc comment. This test measures how far apart they
	// have drifted, and it is expected to fail on its first run. The failure
	// is the deliverable.
	observed := loadObservedWindows(t, "testdata/windows/26_1_2.json")
	registry := v26_1.Data().Windows()

	var drift []string
	for _, window := range observed {
		declared, ok := registry.ByName(window.Name)
		if !ok {
			drift = append(drift, fmt.Sprintf("%s: the server sent it, the registry has no such window", window.Name))
			continue
		}
		if got, want := slotCount(declared), window.SlotCount; got != want {
			drift = append(drift, fmt.Sprintf("%s: registry says %d slots, the server sent %d", window.Name, got, want))
		}
		if got, want := len(declared.Properties), window.PropertyCount; got != want {
			drift = append(drift, fmt.Sprintf("%s: registry says %d properties, the server sent %d", window.Name, got, want))
		}
	}

	if len(drift) != 0 {
		t.Fatalf("the 26.1 window registry has drifted from the server in %d ways:\n%s",
			len(drift), strings.Join(drift, "\n"))
	}
}
```

- [ ] **Step 3: Write down what you found, and decide**

Three outcomes, and the plan branches on which one holds:

- **The alias is close enough.** Slot counts and layouts agree for the windows
  this stage tests. Record which windows were checked, note that the alias
  remains a standing risk for windows nobody opened, and continue with Tasks 2
  onward unchanged.
- **The alias is wrong in specific, enumerable ways.** Record each one. Then
  the correct fix is a data correction in `minecraft-protocol`, not a
  workaround here — an override table in `headless-minecraft` would be a second
  source of truth about a registry, and the next consumer would not find it.
  That is a `minecraft-protocol` change to schedule before this stage continues.
- **The alias is unusable.** Then say plainly, here and in the master plan, that
  M9.7's 26.1.2 lane rests on what the server sends at runtime rather than on
  generated data, that its gate is weaker than the 1.8.9 lane's, and that a
  window nobody opened during the corpus capture is a window nobody has checked.
  This is the same admission M8.7's plan requires when no jar-backed oracle
  exists, and for the same reason: a gate that records a belief and does not say
  so is what makes a later failure hard to diagnose.

- [ ] **Step 4: Commit the audit**

```bash
git add internal/conformance/windows_test.go docs/verification/2026-08-17-window-data-audit.md
git commit -m "test(conformance): audit the 26.1 window registry against the server

The dataset is an alias of Java 1.16.1 and the generated file says so.
Measure the drift before building slot handling on top of it."
```

---

## Task 2: The pre-click snapshot and rollback

**Files:**
- Modify: `headless-minecraft/world/containers.go`
- Test: `headless-minecraft/world/containers_test.go`

**Interfaces:**
- Produces:

```go
// Pending is a click the client has made and the server has not answered.
//
// It holds what the affected slots contained before the click, because that is
// the only way to roll back on protocol 47: the server's rejection there is an
// apology transaction that carries no state, and a client that did not keep its
// own prior contents has nothing to restore. Protocol 775 resends the window
// and needs none of this, but keeping one mechanism means the rollback path is
// exercised by both versions' tests rather than only by one.
type Pending struct {
    Sequence int32
    Window   int32
    Before   []SlotSnapshot
}

// Click records a click as pending and applies its predicted effect.
func (s *Containers) Click(c *event.Collector, p Pending, predicted []SlotSnapshot)

// Confirm accepts a pending click. The prediction stands and the snapshot is
// dropped.
func (s *Containers) Confirm(c *event.Collector, sequence int32) error

// Reject rolls a pending click back to its snapshot and publishes the restored
// slots, so a caller watching container.slots_changed sees the truth rather
// than silently holding a wrong inventory.
func (s *Containers) Reject(c *event.Collector, sequence int32) error
```

- [ ] **Step 1: Write the failing test**

```go
func TestARejectedClickRestoresWhatWasThere(t *testing.T) {
	t.Parallel()

	// The silent failure this exists to prevent: the client predicts a swap,
	// the server refuses, and nothing corrects it. A wrong inventory is
	// invisible until a craft fails, three actions later, for no visible
	// reason.
	s, collector := containersWith(t, slot(0, "stone", 64), slot(1, "", 0))
	s.Click(collector, world.Pending{Sequence: 1, Window: 0,
		Before: snapshotOf(t, s, 0, 1)}, swapPrediction(t))

	if got := slotItem(t, s, 1); got != "stone" {
		t.Fatalf("slot 1 holds %q after the predicted swap, want stone", got)
	}

	if err := s.Reject(collector, 1); err != nil {
		t.Fatalf("Reject: %v", err)
	}
	if got := slotItem(t, s, 0); got != "stone" {
		t.Fatalf("slot 0 holds %q after rollback, want stone", got)
	}
	if got := slotItem(t, s, 1); got != "" {
		t.Fatalf("slot 1 holds %q after rollback, want empty", got)
	}
}

func TestARejectionPublishesTheRestoredSlots(t *testing.T) {
	t.Parallel()

	// Rolling back without publishing leaves every subscriber holding the
	// prediction. The rollback would be correct and invisible, which is the
	// worst of both.
	s, collector := containersWith(t, slot(0, "stone", 64), slot(1, "", 0))
	s.Click(collector, world.Pending{Sequence: 1, Before: snapshotOf(t, s, 0, 1)}, swapPrediction(t))
	drain(t, collector)

	if err := s.Reject(collector, 1); err != nil {
		t.Fatalf("Reject: %v", err)
	}
	if !published(t, collector, event.NameContainerSlotsChanged) {
		t.Fatal("a rollback published no slot change")
	}
}

func TestConfirmingAnUnknownSequenceIsAnError(t *testing.T) {
	t.Parallel()

	// A confirmation for a click nobody made means the sequence has drifted,
	// which on 47 means every subsequent confirmation is answering the wrong
	// click. Failing loudly beats accumulating a silent offset.
	s, collector := containersWith(t, slot(0, "stone", 64))
	if err := s.Confirm(collector, 99); err == nil {
		t.Fatal("Confirm accepted a sequence nobody sent")
	}
}

func TestPendingClicksResolveInOrder(t *testing.T) {
	t.Parallel()

	// Rejecting the first of two pending clicks must roll back the second as
	// well: the second was predicted on top of the first, and keeping it
	// leaves the client holding a state that never existed on either side.
	s, collector := containersWith(t, slot(0, "stone", 64), slot(1, "", 0), slot(2, "", 0))
	s.Click(collector, world.Pending{Sequence: 1, Before: snapshotOf(t, s, 0, 1)}, swapPrediction(t))
	s.Click(collector, world.Pending{Sequence: 2, Before: snapshotOf(t, s, 1, 2)}, swapPrediction(t))

	if err := s.Reject(collector, 1); err != nil {
		t.Fatalf("Reject: %v", err)
	}
	if got := slotItem(t, s, 0); got != "stone" {
		t.Fatalf("slot 0 holds %q; rejecting the first click did not roll back "+
			"the second, which was predicted on top of it", got)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd headless-minecraft && devbox run -- go test ./world/ -run Click -v`

- [ ] **Step 3: Implement**

- [ ] **Step 4: Run the tests and gates**

- [ ] **Step 5: Commit**

```bash
git add world/containers.go world/containers_test.go
git commit -m "feat(world): roll a rejected click back to its pre-click snapshot

Protocol 47's rejection carries no state, so the client must keep its own
prior contents or it cannot roll back at all. Rejecting one pending click
rolls back everything predicted on top of it."
```

---

## Task 3: The click action and its version paths

**Files:**
- Create: `headless-minecraft/client/window.go`
- Create: `headless-minecraft/internal/adapter/v1_8/window.go`
- Create: `headless-minecraft/internal/adapter/v26_1/window.go`
- Test: `headless-minecraft/client/window_test.go`

**Interfaces:**
- Produces:

```go
// ClickMode is what a click does, named rather than numbered.
//
// The wire numbers differ between versions and a caller should not have to
// know either. The names are the vocabulary; each adapter maps them.
type ClickMode uint8

const (
    ClickPickup ClickMode = iota + 1
    ClickQuickMove // shift-click
    ClickSwapHotbar
    ClickMiddle
    ClickDrop
    ClickDrag
    ClickDoubleClick
)

// ActionClickSlot clicks one slot in one window.
type ActionClickSlot struct {
    Window int32
    Slot   int16
    Button int8
    Mode   ClickMode
}

// ActionCloseWindow closes an open window.
type ActionCloseWindow struct{ Window int32 }
```

- [ ] **Step 1: Write the failing test**

```go
func TestAClickIsConfirmedOnBothVersions(t *testing.T) {
	t.Parallel()

	// One test, two mechanisms. 47 echoes a transaction; 775 stamps a state
	// ID. The caller sees neither — it sees a click that landed.
	for _, version := range vanillaVersions(t) {
		t.Run(version.Name, func(t *testing.T) {
			server := vanilla.Start(t, version.Options)
			c := connected(t, server)
			openChest(t, c, server)

			slots := subscribe(t, c, event.DomainContainers)
			if err := c.Do(t.Context(), client.ActionClickSlot{
				Window: openWindowID(t, c), Slot: 0, Mode: client.ClickPickup,
			}); err != nil {
				t.Fatalf("Do: %v", err)
			}

			awaitName(t, slots, event.NameContainerSlotsChanged)
			if pending := pendingClicks(t, c); pending != 0 {
				t.Fatalf("%d clicks still pending after confirmation", pending)
			}
		})
	}
}

func TestARejectedClickLeavesTheClientAgreeingWithTheServer(t *testing.T) {
	t.Parallel()

	// The gate's real subject. Provoke a refusal — click a result slot in a
	// way the server will not honour — and require the client's view to match
	// the server's afterwards.
	for _, version := range vanillaVersions(t) {
		t.Run(version.Name, func(t *testing.T) {
			server := vanilla.Start(t, version.Options)
			c := connected(t, server)
			openChest(t, c, server)

			_ = c.Do(t.Context(), illegalClick(t, c))
			settle(t, c)

			mine := slotContents(t, c)
			theirs := serverSlotContents(t, server)
			if !slices.Equal(mine, theirs) {
				t.Fatalf("after a rejected click the client holds %v and the "+
					"server holds %v", mine, theirs)
			}
		})
	}
}

func TestShiftClickDrainsRatherThanActingOnce(t *testing.T) {
	t.Parallel()

	// M3's session findings recorded a real defect of exactly this shape: a
	// shift-click handler that crafted once instead of draining the grid. It
	// is fixed; this is the vanilla check that keeps it fixed.
	server := vanilla.Start(t, defaultOptions(t))
	c := connected(t, server)
	openChestWith(t, c, server, stack("stone", 64))

	if err := c.Do(t.Context(), client.ActionClickSlot{
		Window: openWindowID(t, c), Slot: 0, Mode: client.ClickQuickMove,
	}); err != nil {
		t.Fatalf("Do: %v", err)
	}
	settle(t, c)

	if got := stackSize(t, c, 0); got != 0 {
		t.Fatalf("the source slot holds %d after a shift-click, want 0: the "+
			"whole stack moves, not one item", got)
	}
}

func TestClosingAWindowDropsTheCursorStack(t *testing.T) {
	t.Parallel()

	// Vanilla drops what is on the cursor when the window closes. A client
	// that keeps it believes in an item that is on the ground.
	server := vanilla.Start(t, defaultOptions(t))
	c := connected(t, server)
	openChest(t, c, server)
	pickUpInto(t, c, "cursor")

	if err := c.Do(t.Context(), client.ActionCloseWindow{Window: openWindowID(t, c)}); err != nil {
		t.Fatalf("Do close: %v", err)
	}
	settle(t, c)

	if held := cursorStack(t, c); held != "" {
		t.Fatalf("the cursor holds %q after closing the window", held)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

- [ ] **Step 3: Implement, one confirmation path per adapter**

The 47 adapter echoes the transaction and calls `Confirm` or `Reject` from the
server's accepted flag. The 775 adapter tracks the state ID and calls `Confirm`
when the server's next state matches its prediction and `Reject` when the server
resends the window. Neither mechanism appears in `client/window.go`.

- [ ] **Step 4: Run the tests and gates**

- [ ] **Step 5: Commit**

```bash
git add client/window.go internal/adapter/ client/window_test.go
git commit -m "feat(client): click a slot, with one confirmation path per version

47 echoes a transaction and 775 stamps a state ID; these answer the same
question incompatibly, so each adapter owns its answer and the caller
sees only whether the click landed."
```

---

## Task 4: The gate and the milestone record

**Files:**
- Create: `minecraft-simulation/conformance/containers_test.go`
- Create: `minecraft-simulation/conformance/testdata/containers/`
- Modify: `headless-minecraft/MASTER_PLAN.md`

- [ ] **Step 1: Capture the corpus on both versions**

Open and close a chest, a furnace, a crafting table, and the player inventory;
pick up, place, swap, shift-click, drop, and double-click; provoke one rejection
per window type. Record the server's slot state after each.

If Task 1 found the 26.1.2 window registry unusable, capture the same corpus
anyway — the runtime data is then the only source, and the corpus is what makes
it verifiable at all.

- [ ] **Step 2: Write the failing gate**

Exact comparison of slot contents after each operation. Slots hold discrete
stacks; there is nothing here a tolerance would legitimately absorb.

- [ ] **Step 3: Declare the scenarios**

One per window type per version, with an absence declared and reasoned wherever
a window type exists on one version and not the other — the 26.1.2 window set
has grown considerably since 1.8.9, and a smithing table has no 1.8.9
counterpart.

- [ ] **Step 4: Record the milestone**

Write what the work found. The first thing to record is Task 1's answer: how far
the aliased 26.1 window data had drifted, which windows were checked, and
whether the 26.1.2 lane's gate ended up as strong as the 1.8.9 lane's or weaker.
If it is weaker, the master plan says so in the stage table, not only here.

- [ ] **Step 5: Commit**

```bash
git commit -m "docs(plan): close M9.7, and what the window-data audit found"
```

---

## Definition of done

- Task 1's audit is written, and its answer is recorded in the master plan as
  well as in the verification document.
- A rejected click restores the pre-click contents, publishes the restoration,
  and rolls back anything predicted on top of it.
- A confirmation for an unknown sequence is an error rather than a silent
  offset.
- Clicks are confirmed on both versions through their own mechanisms, with
  neither mechanism visible in the shared click path.
- After a rejected click the client's slot contents equal the server's, on both
  versions.
- Shift-click drains the stack; closing a window drops the cursor stack.
- The corpus gate passes exactly on both versions, with window types absent from
  one version declared absent with a reason.
- `task lint`, `task test` under `-race`, and `task verify` pass in
  `headless-minecraft` and `minecraft-simulation`.

## Risks

**The 26.1 window dataset is an alias of Java 1.16.1 and may be unusable.**
This is not a speculative risk; the generated file states it. Task 1 exists to
size it, and the honest outcomes include "this stage's 26.1.2 lane rests on
runtime data and its gate is the weaker one". Re-estimate M9.7 after Task 1, and
do not let the estimate written before it stand.

**A wrong inventory is invisible.** Unlike movement, nothing corrects it and no
event announces the disagreement. It surfaces two or three actions later as a
craft that fails for no reason — which lands in M9.8, not here, and will look
like a crafting bug. Task 2's rollback and Task 4's server-comparison test are
the two things standing between this stage and a very confusing M9.8.

**Drag clicks are a multi-packet sequence and this plan treats them as one
mode.** `ClickDrag` covers a start, a series of slot additions, and an end, and
a server rejects a malformed sequence wholesale. If the corpus shows the
single-mode treatment cannot express it, split it rather than approximating —
an approximated drag distributes items differently from vanilla, and the
difference is exactly the kind that surfaces as a wrong stack count much later.

**Creative-mode slot setting bypasses all of this.** It is a different packet
with no confirmation on either version. Out of scope here; say so in the test
file rather than letting the absence read as coverage.
