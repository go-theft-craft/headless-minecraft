# Composed behaviours Implementation Plan

> **Status: complete, 2026-08-18.** The `behaviour` package shipped in
> `headless-minecraft` (`893b99e`): the one-tick `Behaviour` shape and `Outcome`,
> `Follow` and `Flee`, `Eat` and `Block`, `Sequence` and `Dig`, `Build` over the
> place and pillar edges, and `Fish`. Scopes are checked at construction through
> `RequireScopes`, and both cross-cutting gates exist —
> `TestEveryBehaviourTerminates` and `TestEveryBehaviourRefusesWithoutItsScopes`.
>
> `Fish` ships without a measured bite detector, which is what task 6 planned
> for: the detector is behind an interface, construction refuses without one,
> and the trace-gated test skips with its reason recorded. Both capture lanes
> have since run, so what is missing is a recorded session with a rod in it, not
> the instrument.
>
> For three hours this package did not compile under the repository's own gates.
> It imports `navigation.EdgePlace`, `EdgePillar`, `EdgeJumpGap`,
> `EdgeWaterDrop`, `EdgeClimb`, `EdgeDoor`, and `geom.Vec3.Toward`, none of which
> was in `minecraft-simulation` v0.1.0, which is what `go.mod` pinned. Every task
> target runs `GOWORK=off`, so `task test` failed on `main` while the local
> `go.work` made the work look finished. Closed by v0.2.0 and the two bumps in
> `d3b8a0a`; the ordering that caused it is recorded in the
> [aiming plan](2026-08-18-aiming-and-reach-geometry.md).

> **For agentic workers:** REQUIRED SUB-SKILL: Use `subagent-driven-development` (recommended) or `executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give `headless-minecraft` one shape for a multi-tick behaviour — follow, flee, eat, block, fish, bridge, pillar, strip-mine — so a bot that fishes is composed from primitives rather than hand-written per caller.

**Architecture:** A behaviour is asked once per tick and never drives, matching what `adapter.Source` already requires and what the navigation design's `Follower` already is. It returns the actions this tick wants and a status; a wait is a tick that returns no action. Scopes are checked at construction, not per tick, matching the client's rule that components are validated before network work begins.

**Tech Stack:** Go 1.26.6 from `openserbia/go-flake`, Devbox, Task, `golangci-lint`, and the existing `version`, `world`, `safety`, and `minecraft-simulation/navigation` packages.

Design: [composed behaviours design](../specs/2026-08-18-composed-behaviours-design.md).

## Dependencies

| Needs | From | State |
| --- | --- | --- |
| `version.ActionHeldSlot`, `ActionUseItem`, `ActionReleaseUse`, `ActionInteract`, `ActionSwing` | [interaction primitives plan](2026-08-18-interaction-primitives.md) tasks 2 and 3 | Unblocked |
| `geom.Behind`, `geom.Lead`, `geom.Tangent`, `AABB.Reaches` | [aiming plan](2026-08-18-aiming-and-reach-geometry.md) tasks 1 and 4 | Unblocked |
| `navigation.Find` over movement edges | Shipped | Available |
| `navigation.EdgePlace`, `EdgePillar` | [mutating edges plan](2026-08-18-mutating-edges-pillar.md) tasks 4 and 5 | Blocks tasks 5 only |
| A captured fishing trace per version | The M9.1 and M9.1b capture lanes | Both lanes have run; **nobody has captured fishing through them.** Blocks task 6 |

Tasks 1 through 4 are unblocked once the interaction primitives land.

## Global Constraints

- Go 1.26.6 from `openserbia/go-flake`; Devbox and Task drive every gate.
- A behaviour is asked once per tick and never drives its own loop. No behaviour sleeps, blocks, or owns a goroutine.
- Scopes are checked at construction. A behaviour that discovered on tick four hundred that it may not dig has already walked the bot somewhere it should not be.
- Nothing in the client's required path imports `behaviour`. A caller that wants no behaviours links none.
- No behaviour asserts a threshold that was not measured. `Fish` is the case this exists for.
- No chat is parsed as a control channel. The master plan records that no chat component is rendered anywhere and that this is deliberate and permanent.
- `devbox run -- task lint`, `task test`, and `task verify` pass before every commit.
- Conventional commit subjects. No `Co-Authored-By` trailer and no `Claude-Session` line.

## File Structure

All paths relative to the `headless-minecraft` repository root.

| File | Responsibility |
| --- | --- |
| `behaviour/behaviour.go` | `Behaviour`, `Outcome`, `Status`, `Reason`, and the package doc |
| `behaviour/scope.go` | The construction-time scope check |
| `behaviour/follow.go` | `Follow` and `Flee` |
| `behaviour/consume.go` | `Eat` and `Block` |
| `behaviour/build.go` | `Bridge` and `Pillar` executors |
| `behaviour/mine.go` | `StripMine` |
| `behaviour/fish.go` | `Fish` and its bite detector interface |
| `behaviour/behaviour_test.go` | The cross-cutting gates: termination, waiting, scopes |

`Follow` and `Flee` share a file because they share their replanning; `Eat` and `Block` share one because both are a use with a release.

---

## Task 1: The shape

**Files:**
- Create: `behaviour/behaviour.go`
- Create: `behaviour/scope.go`
- Test: `behaviour/behaviour_test.go`

**Interfaces:**
- Produces: `type Behaviour interface { Tick(context.Context, world.Snapshot) (Outcome, error) }`, `type Outcome struct { Actions []version.Action; Status Status; Reason Reason }`, `type Status uint8` with `Running`, `Complete`, `Stopped`, `type Reason uint8` with `ReasonNone`, `ReasonBlocked`, `ReasonStuck`, `ReasonWorldChanged`, `ReasonFailed`, `ReasonUnauthorized`, `ReasonOutOfResources`, and `func RequireScopes(safety.Authorization, string, ...safety.Scope) error`.

- [x] **Step 1: Write the failing test**

```go
func TestAWaitingBehaviourReturnsRunningWithNoActions(t *testing.T) {
	t.Parallel()

	// This is the property that makes "asked once per tick" work. A
	// behaviour that emitted an action every tick while waiting for a rod to
	// dip would flood the connection, and a behaviour that slept would take
	// the tick rate away from the caller.
	behaviour := &waitingBehaviour{ticks: 5}

	for range 5 {
		outcome, err := behaviour.Tick(t.Context(), emptySnapshot(t))
		if err != nil {
			t.Fatalf("Tick: %v", err)
		}
		if outcome.Status != Running {
			t.Fatalf("Status = %v, want Running", outcome.Status)
		}
		if len(outcome.Actions) != 0 {
			t.Fatalf("a waiting tick emitted %d actions", len(outcome.Actions))
		}
	}
}

func TestRequireScopesRefusesAtConstruction(t *testing.T) {
	t.Parallel()

	authorization, err := safety.Authorize("localhost:25565", safety.ScopeObserve)
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}

	err = RequireScopes(authorization, "localhost:25565", safety.ScopeObserve, safety.ScopeDig)
	if err == nil {
		t.Fatal("RequireScopes accepted a dig behaviour with no dig scope")
	}
	if !strings.Contains(err.Error(), "dig") {
		t.Fatalf("error %q does not name the missing scope", err)
	}
}

func TestRequireScopesAcceptsWhatIsAuthorized(t *testing.T) {
	t.Parallel()

	authorization, err := safety.Authorize("localhost:25565", safety.ScopeObserve, safety.ScopeMove)
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}

	if err := RequireScopes(authorization, "localhost:25565", safety.ScopeMove); err != nil {
		t.Fatalf("RequireScopes: %v", err)
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `devbox run -- go test ./behaviour/ -v`
Expected: FAIL, package does not exist.

- [x] **Step 3: Write minimal implementation**

```go
// Package behaviour composes the outbound primitives into multi-tick tasks.
//
// A behaviour is asked once per tick and never drives. That is not a
// preference: adapter.Source already requires it, and a behaviour that drove
// its own loop could not be composed with a follower that does not. Three
// things follow from it.
//
// A wait is a tick that returns no action. A behaviour waiting for a rod to
// dip, a furnace to smelt, or a placement to settle returns Running with an
// empty action set, so it never sleeps and the tick rate stays the caller's.
//
// A behaviour is testable without a connection: feed it snapshots and read its
// actions, which is how examples/orbit already tests its tick loop.
//
// Behaviours compose by delegation. StripMine holds a follower and a digging
// behaviour and forwards its tick to whichever is active. There is no
// scheduler here, because there is nothing to schedule — choosing what the bot
// should be doing is the application's decision, exactly as goal selection is
// the application's in navigation.
package behaviour
```

`RequireScopes` consults `safety.Authorization.Allows` for each scope and returns an error naming every missing one at once, rather than the first: a caller fixing an authorization wants the whole list.

- [x] **Step 4: Run test to verify it passes**

Run: `devbox run -- go test ./behaviour/ -v`
Expected: PASS.

- [x] **Step 5: Commit**

```bash
git add behaviour/
git commit -m "feat(behaviour): add the tick shape and the scope check"
```

---

## Task 2: Follow and Flee

**Files:**
- Create: `behaviour/follow.go`
- Test: `behaviour/follow_test.go`

**Interfaces:**
- Consumes: `Behaviour` and `RequireScopes` from task 1; `navigation.Find`; `geom.Behind`.
- Produces: `func NewFollow(Deps, FollowConfig) (*Follow, error)`, `func NewFlee(Deps, FleeConfig) (*Flee, error)`.

These two first because they need only movement, which is the one thing that already works end to end.

- [x] **Step 1: Write the failing test**

```go
func TestFollowReplansWhenTheTargetMovesPastTheThreshold(t *testing.T) {
	t.Parallel()

	// Replanning every tick is a search per tick and a bot that stutters.
	// Never replanning is a bot that walks to where the target used to be.
	// The threshold is the whole behaviour.
	planner := &countingPlanner{}
	follow := newTestFollow(t, planner, FollowConfig{ReplanDistance: 4})

	snapshot := snapshotWithTarget(t, geom.Vec3{X: 10, Y: 64, Z: 0})
	if _, err := follow.Tick(t.Context(), snapshot); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if planner.calls != 1 {
		t.Fatalf("first tick planned %d times, want 1", planner.calls)
	}

	// A step the target takes inside the threshold must not replan.
	nudged := snapshotWithTarget(t, geom.Vec3{X: 12, Y: 64, Z: 0})
	if _, err := follow.Tick(t.Context(), nudged); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if planner.calls != 1 {
		t.Fatalf("a two-block move replanned; calls = %d", planner.calls)
	}

	// Past it, it must.
	moved := snapshotWithTarget(t, geom.Vec3{X: 20, Y: 64, Z: 0})
	if _, err := follow.Tick(t.Context(), moved); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if planner.calls != 2 {
		t.Fatalf("a ten-block move did not replan; calls = %d", planner.calls)
	}
}

func TestFollowStopsWithAReasonWhenTheTargetIsUnreachable(t *testing.T) {
	t.Parallel()

	// A bot that cannot reach its target must say so. Returning Running
	// forever is the failure mode that makes a stuck bot look like a slow one.
	follow := newTestFollow(t, unreachablePlanner(t), FollowConfig{ReplanDistance: 4})

	outcome, err := follow.Tick(t.Context(), snapshotWithTarget(t, geom.Vec3{X: 500, Y: 64, Z: 0}))
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}

	if outcome.Status != Stopped {
		t.Fatalf("Status = %v, want Stopped", outcome.Status)
	}
	if outcome.Reason != ReasonBlocked {
		t.Fatalf("Reason = %v, want ReasonBlocked", outcome.Reason)
	}
}

func TestFollowWithoutTheMoveScopeFailsToConstruct(t *testing.T) {
	t.Parallel()

	deps := testDeps(t, safety.ScopeObserve)

	if _, err := NewFollow(deps, FollowConfig{ReplanDistance: 4}); err == nil {
		t.Fatal("NewFollow accepted an authorization with no move scope")
	}
}

func TestFleeGoesAwayFromTheThreat(t *testing.T) {
	t.Parallel()

	flee := newTestFlee(t, FleeConfig{Distance: 20})

	outcome, err := flee.Tick(t.Context(), snapshotWithThreat(t,
		geom.Vec3{X: 0, Y: 64, Z: 0}, geom.Vec3{X: 5, Y: 64, Z: 0}))
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}

	goal := flee.Goal()
	if goal.X >= 0 {
		t.Fatalf("fled toward the threat: goal %v, threat at x=5, bot at x=0", goal)
	}
	if outcome.Status != Running {
		t.Fatalf("Status = %v, want Running", outcome.Status)
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `devbox run -- go test ./behaviour/ -run 'TestFollow|TestFlee' -v`
Expected: FAIL, undefined.

- [x] **Step 3: Write minimal implementation**

`Follow` holds a planner, a path, and the target position it last planned against. `Tick` replans when the target has moved further than `ReplanDistance` from that position, then advances the path by one edge and returns the movement actions for it. `Flee` is `Follow` with a goal from `geom.Away` and no target tracking.

- [x] **Step 4: Run the tests**

Run: `devbox run -- go test ./behaviour/ -run 'TestFollow|TestFlee' -v`
Expected: PASS.

- [x] **Step 5: Commit**

```bash
git add behaviour/follow.go behaviour/follow_test.go
git commit -m "feat(behaviour): add follow and flee"
```

---

## Task 3: Eat and Block

**Files:**
- Create: `behaviour/consume.go`
- Test: `behaviour/consume_test.go`

**Interfaces:**
- Consumes: `version.ActionHeldSlot`, `ActionUseItem`, `ActionReleaseUse` from the interaction primitives plan.
- Produces: `func NewEat(Deps, EatConfig) (*Eat, error)`, `func NewBlock(Deps, BlockConfig) (*Block, error)`.

- [x] **Step 1: Write the failing test**

```go
func TestEatSelectsTheSlotBeforeUsingIt(t *testing.T) {
	t.Parallel()

	// Using without selecting eats whatever happens to be held, which on a
	// bot that just mined is a pickaxe.
	eat := newTestEat(t, EatConfig{Slot: 3, Threshold: 16})

	outcome, err := eat.Tick(t.Context(), snapshotWithFood(t, 10))
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}

	if len(outcome.Actions) < 2 {
		t.Fatalf("emitted %d actions, want a slot selection and a use", len(outcome.Actions))
	}
	if _, ok := outcome.Actions[0].(version.ActionHeldSlot); !ok {
		t.Fatalf("first action is %T, want ActionHeldSlot", outcome.Actions[0])
	}
	if _, ok := outcome.Actions[1].(version.ActionUseItem); !ok {
		t.Fatalf("second action is %T, want ActionUseItem", outcome.Actions[1])
	}
}

func TestEatDoesNothingWhenTheFoodLevelIsAboveTheThreshold(t *testing.T) {
	t.Parallel()

	eat := newTestEat(t, EatConfig{Slot: 3, Threshold: 16})

	outcome, err := eat.Tick(t.Context(), snapshotWithFood(t, 20))
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}

	if outcome.Status != Complete {
		t.Fatalf("Status = %v, want Complete", outcome.Status)
	}
	if len(outcome.Actions) != 0 {
		t.Fatalf("a full bot emitted %d actions", len(outcome.Actions))
	}
}

func TestEatReleasesWhenTheFoodLevelRecovers(t *testing.T) {
	t.Parallel()

	// Every use with a duration needs an end. A bot that never releases holds
	// the item forever and eats nothing more.
	eat := newTestEat(t, EatConfig{Slot: 3, Threshold: 16})

	if _, err := eat.Tick(t.Context(), snapshotWithFood(t, 10)); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	outcome, err := eat.Tick(t.Context(), snapshotWithFood(t, 20))
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}

	if !slices.ContainsFunc(outcome.Actions, func(a version.Action) bool {
		_, ok := a.(version.ActionReleaseUse)
		return ok
	}) {
		t.Fatalf("recovered without releasing the use: %v", outcome.Actions)
	}
}

func TestBlockRefusesToConstructOnProtocol47(t *testing.T) {
	t.Parallel()

	// 47 has no shield and no offhand. Refusing at construction is better
	// than a behaviour that ticks forever emitting refusals.
	deps := testDepsForProtocol(t, "47")

	_, err := NewBlock(deps, BlockConfig{Slot: 8})
	if !errors.Is(err, version.ErrUnsupportedAction) {
		t.Fatalf("NewBlock = %v, want ErrUnsupportedAction", err)
	}
}

func TestBlockConstructsOnProtocol775(t *testing.T) {
	t.Parallel()

	if _, err := NewBlock(testDepsForProtocol(t, "775"), BlockConfig{Slot: 8}); err != nil {
		t.Fatalf("NewBlock: %v", err)
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `devbox run -- go test ./behaviour/ -run 'TestEat|TestBlock' -v`
Expected: FAIL, undefined.

- [x] **Step 3: Write minimal implementation**

`Eat` is a three-state machine: idle above the threshold, using below it, releasing once the observed food level recovers. `Block` is the same shape with the shield slot and no threshold, and its constructor probes the adapter with an `ActionUseItem{Hand: version.OffHand}` to learn whether the protocol carries an offhand at all.

- [x] **Step 4: Run the tests**

Run: `devbox run -- go test ./behaviour/ -run 'TestEat|TestBlock' -v`
Expected: PASS.

- [x] **Step 5: Commit**

```bash
git add behaviour/consume.go behaviour/consume_test.go
git commit -m "feat(behaviour): add eat and shield-block"
```

---

## Task 4: The cross-cutting gates

**Files:**
- Modify: `behaviour/behaviour_test.go`

**Interfaces:**
- Consumes: every behaviour built so far.

These run over the whole set, so a behaviour added later cannot skip them.

- [x] **Step 1: Write the termination gate**

```go
func TestEveryBehaviourTerminatesAgainstAnUnhelpfulWorld(t *testing.T) {
	t.Parallel()

	// A behaviour that never stops is the failure this package exists to
	// prevent: it looks like slow progress and is actually a hang. Ten
	// thousand ticks is far more than any real task needs.
	for name, construct := range allBehaviours(t) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			behaviour := construct(t)
			snapshot := unhelpfulSnapshot(t)

			for tick := range 10000 {
				outcome, err := behaviour.Tick(t.Context(), snapshot)
				if err != nil {
					t.Fatalf("Tick %d: %v", tick, err)
				}
				if outcome.Status != Running {
					return
				}
			}

			t.Fatal("still Running after ten thousand ticks against a world that offers nothing")
		})
	}
}

func TestNoBehaviourEmitsActionsWhileWaiting(t *testing.T) {
	t.Parallel()

	// A behaviour that emits every tick while waiting floods the connection
	// and is the thing an anti-cheat notices first.
	for name, construct := range allBehaviours(t) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			behaviour := construct(t)
			snapshot := waitingSnapshot(t)

			var emitted int
			for range 200 {
				outcome, err := behaviour.Tick(t.Context(), snapshot)
				if err != nil {
					t.Fatalf("Tick: %v", err)
				}
				emitted += len(outcome.Actions)
			}

			if emitted > maxActionsWhileWaiting {
				t.Fatalf("emitted %d actions over 200 waiting ticks", emitted)
			}
		})
	}
}

func TestEveryBehaviourDeclaresItsScopes(t *testing.T) {
	t.Parallel()

	for name, construct := range allBehavioursWithScopes(t) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			// Constructed with every scope but one of its own, it must refuse.
			for _, missing := range construct.Scopes {
				deps := testDepsWithout(t, missing)

				if _, err := construct.New(deps); err == nil {
					t.Fatalf("constructed without the %s scope", missing)
				}
			}
		})
	}
}
```

- [x] **Step 2: Write the linkage gate**

```go
func TestTheClientDoesNotImportBehaviour(t *testing.T) {
	t.Parallel()

	// A caller that wants no behaviours links none. If the client's required
	// path grew an import, every consumer would pay for the package.
	out, err := exec.Command("go", "list", "-deps", "./client").Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}

	if strings.Contains(string(out), "headless-minecraft/behaviour") {
		t.Fatal("client imports behaviour; the package must stay optional")
	}
}
```

- [x] **Step 3: Run every gate**

Run: `devbox run -- task verify`
Expected: PASS.

- [x] **Step 4: Commit**

```bash
git add behaviour/behaviour_test.go
git commit -m "test(behaviour): gate termination, waiting, scopes, and linkage"
```

---

## Task 5: Bridge, Pillar, and StripMine

**Files:**
- Create: `behaviour/build.go`, `behaviour/mine.go`
- Test: `behaviour/build_test.go`, `behaviour/mine_test.go`

**Interfaces:**
- Consumes: `navigation.EdgePlace` and `EdgePillar` from the [mutating edges plan](2026-08-18-mutating-edges-pillar.md) tasks 4 and 5.
- Produces: `func NewBridge(Deps, BridgeConfig) (*Bridge, error)`, `func NewPillar(Deps, PillarConfig) (*Pillar, error)`, `func NewStripMine(Deps, StripMineConfig) (*StripMine, error)`.

**Blocked** until those edges exist. `StripMine` is additionally blocked on M9.4's break times and stays out of this plan's definition of done until they land.

- [x] **Step 1: Write the failing test**

```go
func TestBridgeExecutesAPlaceEdgeAndWaitsForItToSettle(t *testing.T) {
	t.Parallel()

	// The executor does not plan. The route is navigation's and the placement
	// decision is the edge's; this turns one edge into the actions that
	// perform it, which is what lets a server mob use the same edges with no
	// behaviour at all.
	bridge := newTestBridge(t, pathWithOnePlaceEdge(t))

	outcome, err := bridge.Tick(t.Context(), snapshotBeforeThePlacement(t))
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}

	if !slices.ContainsFunc(outcome.Actions, func(a version.Action) bool {
		_, ok := a.(version.ActionUseOn)
		return ok
	}) {
		t.Fatalf("a place edge emitted no use-on: %v", outcome.Actions)
	}

	// The next tick must wait rather than place again: the server has not yet
	// confirmed the block, and placing twice consumes two.
	next, err := bridge.Tick(t.Context(), snapshotBeforeThePlacement(t))
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if len(next.Actions) != 0 {
		t.Fatalf("placed again before the first settled: %v", next.Actions)
	}
}

func TestBridgeStopsOutOfResourcesWhenTheBlocksRunOut(t *testing.T) {
	t.Parallel()

	bridge := newTestBridge(t, pathWithFivePlaceEdges(t))

	outcome, err := tickUntilStopped(t, bridge, snapshotWithBlocks(t, 2))
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}

	if outcome.Reason != ReasonOutOfResources {
		t.Fatalf("Reason = %v, want ReasonOutOfResources", outcome.Reason)
	}
}

func TestPillarPlacesBeneathTheBodyAndRises(t *testing.T) {
	t.Parallel()

	pillar := newTestPillar(t, pathWithThreePillarEdges(t))

	outcome, err := pillar.Tick(t.Context(), snapshotOnTheGround(t))
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}

	action, ok := firstUseOn(outcome.Actions)
	if !ok {
		t.Fatalf("a pillar edge emitted no use-on: %v", outcome.Actions)
	}
	if action.Face != version.FaceTop {
		t.Fatalf("placed against the %v face, want the top", action.Face)
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `devbox run -- go test ./behaviour/ -run 'TestBridge|TestPillar' -v`
Expected: FAIL, undefined.

- [x] **Step 3: Write minimal implementation**

Each executor is a two-state machine per edge: emit the actions, then wait until the snapshot shows the block. `StripMine` holds a follower and a digging behaviour and forwards its tick to whichever is active.

- [x] **Step 4: Run the tests and every gate**

Run: `devbox run -- task verify`
Expected: PASS, including the cross-cutting gates from task 4, which now cover the new behaviours.

- [x] **Step 5: Commit**

```bash
git add behaviour/build.go behaviour/mine.go behaviour/build_test.go behaviour/mine_test.go
git commit -m "feat(behaviour): execute bridge and pillar edges"
```

---

## Task 6: Fish

**Files:**
- Create: `behaviour/fish.go`
- Test: `behaviour/fish_test.go`
- Create: `behaviour/testdata/fish-1.8.9.trace`, `behaviour/testdata/fish-26.1.2.trace`

**Interfaces:**
- Produces: `func NewFish(Deps, FishConfig) (*Fish, error)`, `type BiteDetector interface { Bit(before, after world.Entity) bool }`.

**Blocked** on a captured trace per version. The instrument is no longer the
problem: M9.1's live check ran on 2026-08-17 and M9.1b's 775 check ran with it,
both recorded in `relay/docs/verification/2026-08-17-capture-oracle.md`. What is
missing is a session in which somebody fished, on each version, so the bite
signal stays unmeasured until one is taken.

**No packet in either protocol says a fish bit.** What a client observes is the bobber entity's motion changing as it dips, and a splash sound at its position. Which of those is reliable, how much motion counts as a dip, and whether 26.1.2 signals it differently are measurements. M8.4 found that two of eight careful prose readings of vanilla behaviour were wrong, and replaced prose with fixtures the game generates. This follows that.

- [x] **Step 1: Write the structure test, which runs today**

```go
func TestFishCastsSelectsTheRodFirst(t *testing.T) {
	t.Parallel()

	// This much needs no trace: casting is a slot selection and a use, and
	// that is true regardless of how a bite is detected.
	fish := newTestFish(t, alwaysBites{})

	outcome, err := fish.Tick(t.Context(), snapshotWithNoBobber(t))
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}

	if _, ok := outcome.Actions[0].(version.ActionHeldSlot); !ok {
		t.Fatalf("first action is %T, want ActionHeldSlot", outcome.Actions[0])
	}
}

func TestFishReelsOnlyWhenTheDetectorSaysSo(t *testing.T) {
	t.Parallel()

	fish := newTestFish(t, neverBites{})

	// Cast, then two hundred ticks of a bobber sitting still. A bot that
	// reels on a timer catches nothing and looks like it is working.
	if _, err := fish.Tick(t.Context(), snapshotWithNoBobber(t)); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	for range 200 {
		outcome, err := fish.Tick(t.Context(), snapshotWithStillBobber(t))
		if err != nil {
			t.Fatalf("Tick: %v", err)
		}
		if len(outcome.Actions) != 0 {
			t.Fatalf("reeled with no bite: %v", outcome.Actions)
		}
	}
}
```

- [x] **Step 2: Write the trace-gated test, skipped until the traces exist**

```go
func TestTheBiteDetectorAgreesWithACapturedTrace(t *testing.T) {
	t.Parallel()

	// The gate that decides whether Fish works. Until a trace exists this
	// skips with a reason rather than passing, because a skipped gate that
	// reports "ok" is worse than no gate.
	for _, version := range []string{"1.8.9", "26.1.2"} {
		t.Run(version, func(t *testing.T) {
			t.Parallel()

			trace, err := loadTrace(t, "fish-"+version+".trace")
			if errors.Is(err, fs.ErrNotExist) {
				t.Skipf("no captured fishing trace for %s: the capture lane exists, "+
					"but no session with a rod in it has been recorded, so the bite "+
					"signal is unmeasured", version)
			}
			if err != nil {
				t.Fatalf("loadTrace: %v", err)
			}

			detector := NewMotionBiteDetector(defaultDipThreshold)

			bites := countBites(t, detector, trace)
			if bites != trace.ExpectedBites {
				t.Fatalf("detected %d bites, the trace has %d", bites, trace.ExpectedBites)
			}
		})
	}
}
```

- [x] **Step 3: Run the tests**

Run: `devbox run -- go test ./behaviour/ -run TestFish -v`
Expected: the structure tests PASS; the trace test SKIPs with its reason printed.

- [x] **Step 4: Record the state in the package doc**

State in `fish.go`'s doc comment that `Fish` is not claimed to work, name the two milestones the traces wait on, and say that `defaultDipThreshold` is a starting value with no measurement behind it.

- [x] **Step 5: Commit**

```bash
git add behaviour/fish.go behaviour/fish_test.go
git commit -m "feat(behaviour): add fishing behind a trace-gated bite detector"
```

---

## Definition of done

- A bot follows a moving player across terrain it must route around, and stops with `ReasonBlocked` when the player becomes unreachable.
- A behaviour constructed without its scopes fails to construct, proved for every behaviour and every one of its scopes.
- No behaviour busy-loops while waiting, and every behaviour reaches a stopped status within ten thousand ticks against an unhelpful world.
- `Block` refuses at construction on protocol 47 and constructs on 775.
- The client compiles and runs with `behaviour` unimported, proved by the linkage gate.
- `Fish` asserts no threshold that was not measured, and its trace gate skips with a stated reason rather than passing.
- `devbox run -- task verify` passes.

Tasks 5 and 6 are excluded from this definition until their dependencies land; the plan is complete at task 4 and resumes when they do.

## Risks

| Risk | Mitigation |
| --- | --- |
| A behaviour emits an action every tick while waiting and floods the connection | `TestNoBehaviourEmitsActionsWhileWaiting` runs over every behaviour, so one added later cannot skip it |
| `Fish` ships with a guessed dip threshold that looks like it works | The trace gate skips loudly rather than passing, the doc comment says the threshold is unmeasured, and task 6 is outside the definition of done |
| The `behaviour` package creeps into the client's required path | The linkage gate runs `go list -deps ./client` and fails on the import |
| A behaviour hangs and reads as slow progress | The ten-thousand-tick termination gate covers every behaviour in the set |
| Scope creep into goal selection and scheduling | The package doc states the boundary, and no behaviour takes another behaviour as a policy input — only by delegation, as `StripMine` does |
