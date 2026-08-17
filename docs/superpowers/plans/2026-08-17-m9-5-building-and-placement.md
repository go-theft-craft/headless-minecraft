# M9.5 Building and Placement Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `subagent-driven-development` or execute this plan inline one task at a time. Keep every checkbox current.

**Goal:** Decide whether a block placement is legal and what block state results, matching vanilla on both Java Edition 1.8.9 and 26.1.2.

**Architecture:** Placement splits cleanly into two questions that fail differently: *may this be placed here* — a predicate over the target cell, the replaced block, and the entities standing in the way — and *what does it become* — a state derived from the face clicked, where the player stood, and where the player looked. The first is nearly version-neutral. The second is not version-neutral at all, because 1.8.9 addresses states as an id and a four-bit metadata while 26.1.2 addresses them as flat state IDs with a per-block range, and the generated data reflects that: `v1_8` blocks carry `Variations` keyed by metadata, `v26_1` blocks carry `DefaultState`, `MinStateID`, and `MaxStateID`, and `collision_shapes.go` indexes shapes per state offset. The resulting-state rule is therefore version-owned.

**Tech Stack:** Go 1.26.6 from `openserbia/go-flake`, Devbox, Task, `minecraft-simulation`'s `sim`, `world`, `collision`, and `profile/java/*`, `minecraft-protocol`'s generated block and collision-shape data, `relay`'s capture oracle, and pinned vanilla 1.8.9 and 26.1.2 servers.

## Before executing this plan: reconcile it

Depends on M9.4 and, through it, on the M8 contracts. Symbols specified but not
built:

| Symbol | Specified in |
| --- | --- |
| `sim.Phase`, `sim.TickState`, `sim.Command`, `sim.CommandOutcome`, `sim.Op`, `sim.OpSetBlock` | M8.3 plan, Tasks 5, 6, 9 |
| `world.BlockRef`, `world.View`, `world.Blocks.SetBlock` | M8.3 plan, Task 2 |
| `collision.Result`, the swept AABB path | M8.2 (built) |
| `profile/java/v1_8`, `profile/java/v26_1` block tables | M8.4 Task 6, M8.7 Task 4 |
| `client.Do`, `client.Action` | M8.8 plan, Task 1 |
| `conformance.Compare`, `conform.Scenario` | M9.3 plan, M9.1b plan |
| `mining.Face` | M9.4 plan, Task 3 |

Symbols that exist today and were read before writing this plan:
`data.Block.Variations` (1.8.9, keyed by metadata), `data.Block.DefaultState`,
`MinStateID`, `MaxStateID` (26.1.2), `data.CollisionShapes.Blocks`
(`BlockShapeIndex`, block name to a `ShapeIDs` array indexed by state offset),
and `data.Block.BoundingBox`.

**Task 0:** reconcile before touching anything.

## Global Constraints

- Go 1.26.6 from `openserbia/go-flake`; Devbox and Task drive every gate.
- Two-version gate. `conform.Run` refuses a scenario missing a lane.
- Block states come from generated data. A state constant typed into a rule is
  a constant that will be wrong at the next data regeneration.
- The capture oracle must not import `minecraft-simulation`.
- Offline mode, pinned servers with recorded digests.
- `task lint`, `task test` under `-race`, and `task verify` pass before every
  commit.

---

## Design decisions this plan settles

**Legality and resulting state are separate rules with separate tests.** They
fail in ways that look nothing alike: a legality bug places a block where
vanilla refuses, and the server corrects it a tick later; a state bug places the
right block facing the wrong way, and nothing corrects it at all — the client
and server simply disagree forever about what is in that cell. Testing them
together means a state bug can hide behind a legality pass.

**The entity-obstruction check reuses M8.2's collision, and must.** "Is a mob
standing where this block would go" is an AABB overlap against the block's
collision shape, and M8.2 already built that path and proved it bit-identical to
a real 1.8.9 server across 2,872 whole moves. Writing a second overlap test here
would be a second implementation of the thing most likely to disagree subtly.

**The resulting state is version-owned because the addressing is.** 1.8.9's
stairs are one block ID with metadata carrying facing and half; 26.1.2's are a
state ID range where facing, half, and shape are positional within the range.
There is no shared representation that is not a lie about one of them. Each
profile computes its own, and the shared code names only the *inputs* — face,
player yaw, player position, the block being replaced.

**A placement against an unknown block is an incomplete tick.** The tri-state
block view exists for exactly this. A client that has not received the target
cell cannot decide legality, and guessing "air" places blocks into walls.

**Replaceability is a property of the block being replaced, not of the block
being placed.** Grass, tall grass, snow layers, and water are replaced in place;
everything else places against the clicked face instead. Getting this backwards
produces a placement one cell off, which reads as an aim bug.

## File structure

**`minecraft-simulation/placement/`** — new package. Version-neutral legality.

- `placement.go` — `Place` command, `Legality`, `Target`, `Resolve`.
- `placement_test.go`
- `phase.go` — the kernel phase.
- `phase_test.go`

**`minecraft-simulation/profile/java/v1_8/`** and **`.../v26_1/`**

- `placement.go` in each — `PlacedState`, the version-owned resulting state.

**`headless-minecraft/`**

- `client/place.go`, `client/place_test.go`

**`minecraft-simulation/conformance/`**

- `placement_test.go`, `testdata/placement/`

---

## Task 1: Resolving the target cell

**Files:**
- Create: `minecraft-simulation/placement/placement.go`
- Test: `minecraft-simulation/placement/placement_test.go`

**Interfaces:**
- Consumes: `geom.BlockPos`, `geom.Vec3`, `world.View`, `mining.Face`.
- Produces:

```go
// Target is where a placement will actually land.
//
// The clicked cell and the placed cell are usually different: clicking the top
// of a stone block places into the cell above it. They are the same when the
// clicked block is replaceable — grass, snow, water — because those are
// replaced in place. Getting this backwards puts every placement one cell off,
// which reads as an aim bug and is not one.
type Target struct {
    // Clicked is the cell the player's cursor was on.
    Clicked geom.BlockPos
    // Placed is where the block goes.
    Placed geom.BlockPos
    // Replacing reports that the placement replaces the clicked block rather
    // than sitting against its face.
    Replacing bool
}

// Resolve decides which cell a click lands in.
//
// replaceable is supplied rather than looked up because which blocks are
// replaceable is version data, and this package holds no version data.
func Resolve(clicked geom.BlockPos, face Face, replaceable bool) Target
```

- [ ] **Step 1: Write the failing test**

```go
func TestClickingASolidFacePlacesAgainstIt(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		face Face
		want geom.BlockPos
	}{
		{FaceTop, geom.BlockPos{X: 0, Y: 1, Z: 0}},
		{FaceBottom, geom.BlockPos{X: 0, Y: -1, Z: 0}},
		{FaceNorth, geom.BlockPos{X: 0, Y: 0, Z: -1}},
		{FaceSouth, geom.BlockPos{X: 0, Y: 0, Z: 1}},
		{FaceWest, geom.BlockPos{X: -1, Y: 0, Z: 0}},
		{FaceEast, geom.BlockPos{X: 1, Y: 0, Z: 0}},
	} {
		got := placement.Resolve(geom.BlockPos{}, test.face, false)
		if got.Placed != test.want {
			t.Errorf("face %v placed at %v, want %v", test.face, got.Placed, test.want)
		}
		if got.Replacing {
			t.Errorf("face %v reported replacing a solid block", test.face)
		}
	}
}

func TestClickingAReplaceableBlockPlacesIntoIt(t *testing.T) {
	t.Parallel()

	// Grass, tall grass, snow layers, and water are replaced in place. The
	// face is irrelevant when the clicked block is replaceable, and honouring
	// it anyway is what puts the block one cell off.
	got := placement.Resolve(geom.BlockPos{X: 3, Y: 64, Z: 3}, FaceTop, true)
	if got.Placed != (geom.BlockPos{X: 3, Y: 64, Z: 3}) {
		t.Fatalf("placed at %v, want the clicked cell itself", got.Placed)
	}
	if !got.Replacing {
		t.Fatal("Replacing = false when replacing a replaceable block")
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd minecraft-simulation && devbox run -- go test ./placement/ -v`
Expected: FAIL — the package does not exist.

- [ ] **Step 3: Implement**

- [ ] **Step 4: Run the tests and gates**

Run: `cd minecraft-simulation && devbox run -- task verify`

- [ ] **Step 5: Commit**

```bash
cd minecraft-simulation
git add placement/
git commit -m "feat(placement): resolve which cell a click lands in

A replaceable clicked block is replaced in place and the face is
irrelevant; anything else places against the face. Reversing the two
puts every placement one cell off."
```

---

## Task 2: Legality

**Files:**
- Modify: `minecraft-simulation/placement/placement.go`
- Test: `minecraft-simulation/placement/legality_test.go`

**Interfaces:**
- Produces:

```go
// Legality is why a placement may or may not happen.
//
// It carries a reason on refusal rather than a bare false, because a caller
// that cannot distinguish "a mob is standing there" from "that cell is out of
// reach" cannot do anything useful about either.
type Legality struct {
    Allowed bool
    Reason  string
}

// Reasons, as constants so a test asserts on one rather than on prose.
const (
    ReasonOccupied     = "the target cell holds a non-replaceable block"
    ReasonEntity       = "an entity's collision box overlaps the placed block"
    ReasonOutOfReach   = "the target is beyond the player's reach"
    ReasonOutOfWorld   = "the target is outside the world's height range"
    ReasonUnsupported  = "the block needs a support it does not have"
)

// Check decides whether a placement is legal.
//
// shape is the placed block's collision shape, supplied by the profile.
// Entities are checked against it with the same swept-AABB code the movement
// path uses: writing a second overlap test here would be a second
// implementation of the thing most likely to disagree subtly, and M8.2 already
// proved that one bit-identical to a real server across 2,872 whole moves.
func Check(view world.View, entities entity.View, t Target, shape geom.Shape, eye geom.Vec3, reach float64) (Legality, sim.Completeness)
```

- [ ] **Step 1: Write the failing test**

```go
func TestPlacingIntoASolidBlockIsRefused(t *testing.T) {
	t.Parallel()

	got, complete := placement.Check(stoneAt(t, geom.BlockPos{}), noEntities(t),
		placement.Target{Placed: geom.BlockPos{}}, fullBlockShape(t), eyeAt(t, 0, 2, 0), 4.5)
	if !complete.Complete {
		t.Fatalf("incomplete against a known block: %v", complete.Missing)
	}
	if got.Allowed {
		t.Fatal("placing into stone was allowed")
	}
	if got.Reason != placement.ReasonOccupied {
		t.Fatalf("Reason = %q, want %q", got.Reason, placement.ReasonOccupied)
	}
}

func TestPlacingThroughAnEntityIsRefused(t *testing.T) {
	t.Parallel()

	// The overlap is against the placed block's collision shape, not against a
	// full cube. A slab at the bottom of a cell does not overlap a mob standing
	// on it, and refusing that placement would be wrong in the other direction.
	got, _ := placement.Check(airAt(t, geom.BlockPos{}), entityStandingAt(t, geom.BlockPos{}),
		placement.Target{Placed: geom.BlockPos{}}, fullBlockShape(t), eyeAt(t, 0, 2, 0), 4.5)
	if got.Allowed {
		t.Fatal("placing a full block through a standing entity was allowed")
	}
	if got.Reason != placement.ReasonEntity {
		t.Fatalf("Reason = %q, want %q", got.Reason, placement.ReasonEntity)
	}
}

func TestABottomSlabDoesNotCollideWithAMobStandingOnIt(t *testing.T) {
	t.Parallel()

	// The other half of the rule above, and the reason the check takes a shape
	// rather than assuming a cube.
	got, _ := placement.Check(airAt(t, geom.BlockPos{}), entityStandingAt(t, geom.BlockPos{Y: 1}),
		placement.Target{Placed: geom.BlockPos{}}, bottomSlabShape(t), eyeAt(t, 0, 3, 0), 4.5)
	if !got.Allowed {
		t.Fatalf("placing a bottom slab under a standing mob was refused: %s", got.Reason)
	}
}

func TestPlacingBeyondReachIsRefused(t *testing.T) {
	t.Parallel()

	got, _ := placement.Check(airAt(t, geom.BlockPos{X: 100}), noEntities(t),
		placement.Target{Placed: geom.BlockPos{X: 100}}, fullBlockShape(t), eyeAt(t, 0, 2, 0), 4.5)
	if got.Allowed {
		t.Fatal("placing a hundred blocks away was allowed")
	}
	if got.Reason != placement.ReasonOutOfReach {
		t.Fatalf("Reason = %q, want %q", got.Reason, placement.ReasonOutOfReach)
	}
}

func TestPlacingAgainstAnUnknownBlockIsIncompleteNotRefused(t *testing.T) {
	t.Parallel()

	// A cell the client has not received is not a cell that refuses placement.
	// Guessing air here places blocks into walls, and the tri-state view M8.2
	// built exists to stop exactly that.
	_, complete := placement.Check(emptyView(t), noEntities(t),
		placement.Target{Placed: geom.BlockPos{X: 5}}, fullBlockShape(t), eyeAt(t, 0, 2, 0), 4.5)
	if complete.Complete {
		t.Fatal("a placement against an unknown cell reported a complete decision")
	}
	if len(complete.Missing) == 0 {
		t.Fatal("an incomplete decision named nothing missing")
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd minecraft-simulation && devbox run -- go test ./placement/ -run Legal -v`

- [ ] **Step 3: Implement, reusing `collision`**

- [ ] **Step 4: Run the tests and gates**

- [ ] **Step 5: Commit**

```bash
cd minecraft-simulation
git add placement/
git commit -m "feat(placement): legality with a reason, over the shared collision path

The entity check goes through M8.2's swept AABB against the placed
block's real shape, so a bottom slab under a standing mob is legal and a
full block is not. An unknown cell is incomplete, never a refusal."
```

---

## Task 3: The resulting state, per version

**Files:**
- Create: `minecraft-simulation/profile/java/v1_8/placement.go`
- Create: `minecraft-simulation/profile/java/v26_1/placement.go`
- Test: one `placement_test.go` in each

**Interfaces:**
- Produces, in each profile:

```go
// PlacedState returns the block state that results from placing item into
// target, given how the player was standing and looking.
//
// This is version-owned because the addressing is. 1.8.9 names a state as a
// block ID plus four bits of metadata, and the generated data carries
// Variations keyed by that metadata. 26.1.2 names a state as a flat ID inside
// the block's MinStateID..MaxStateID range, and collision_shapes.go indexes
// shapes by the offset within that range. There is no shared representation
// that is not a lie about one of them.
func (p *Profile) PlacedState(item ItemRef, t placement.Target, face Face, yaw float32, eye geom.Vec3) (world.BlockRef, error)
```

- [ ] **Step 1: Write the failing test**

Write the same behavioural cases in both profiles, asserting on observable
consequences rather than on raw state numbers, so the test reads the same on
both versions even though the numbers do not:

```go
func TestStairsFaceThePlayer(t *testing.T) {
	t.Parallel()

	p := newProfile(t)
	for _, test := range []struct {
		name string
		yaw  float32
		want Facing
	}{
		{"looking north", 180, FacingNorth},
		{"looking south", 0, FacingSouth},
		{"looking west", 90, FacingWest},
		{"looking east", 270, FacingEast},
	} {
		t.Run(test.name, func(t *testing.T) {
			ref, err := p.PlacedState(stairsItem(t, p),
				placement.Target{Placed: geom.BlockPos{}}, FaceTop, test.yaw, eyeAt(t, 0, 2, 0))
			if err != nil {
				t.Fatalf("PlacedState: %v", err)
			}
			if got := facingOf(t, p, ref); got != test.want {
				t.Fatalf("stairs faced %v, want %v", got, test.want)
			}
		})
	}
}

func TestASlabPlacedOnTheUnderSideIsATopSlab(t *testing.T) {
	t.Parallel()

	// The half is chosen from the face and, when the face is a side, from where
	// in the cell the click landed. Getting this wrong produces a block that
	// looks right in the client and collides wrong, which is the worst
	// combination to debug.
	p := newProfile(t)
	ref, err := p.PlacedState(slabItem(t, p),
		placement.Target{Placed: geom.BlockPos{}}, FaceBottom, 0, eyeAt(t, 0, 3, 0))
	if err != nil {
		t.Fatalf("PlacedState: %v", err)
	}
	if !isTopHalf(t, p, ref) {
		t.Fatal("a slab placed against the underside of a block is a bottom slab")
	}
}

func TestALogPlacedOnASideIsAxisAligned(t *testing.T) {
	t.Parallel()

	p := newProfile(t)
	ref, err := p.PlacedState(logItem(t, p),
		placement.Target{Placed: geom.BlockPos{}}, FaceEast, 0, eyeAt(t, -2, 1, 0))
	if err != nil {
		t.Fatalf("PlacedState: %v", err)
	}
	if got := axisOf(t, p, ref); got != AxisX {
		t.Fatalf("log axis = %v after an east-face placement, want X", got)
	}
}

func TestAPlainBlockGetsItsDefaultState(t *testing.T) {
	t.Parallel()

	// Stone has one state and no orientation. The rule must not invent a
	// variant for a block that has none — on 26.1.2 that means MinStateID,
	// which is not always the same as zero.
	p := newProfile(t)
	ref, err := p.PlacedState(stoneItem(t, p),
		placement.Target{Placed: geom.BlockPos{}}, FaceTop, 137, eyeAt(t, 0, 2, 0))
	if err != nil {
		t.Fatalf("PlacedState: %v", err)
	}
	if ref != defaultStateOf(t, p, "stone") {
		t.Fatalf("stone placed as %v, want its default state", ref)
	}
}

func TestEveryPlaceableItemResolvesAState(t *testing.T) {
	t.Parallel()

	// The sweep. A profile that handles stairs, slabs, and logs and misses
	// eight hundred other blocks passes every case above.
	p := newProfile(t)
	var missed []string
	for _, item := range placeableItems(t, p) {
		if _, err := p.PlacedState(item.Ref, placement.Target{Placed: geom.BlockPos{}},
			FaceTop, 0, eyeAt(t, 0, 2, 0)); err != nil {
			missed = append(missed, item.Name)
		}
	}
	if len(missed) != 0 {
		t.Fatalf("%d placeable items resolved no state: %v", len(missed), first(missed, 20))
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd minecraft-simulation && devbox run -- go test ./profile/java/... -run Placed -v`

- [ ] **Step 3: Implement each version**

1.8.9: compute the metadata bits and combine with the block ID. The `Variations`
in the generated data name what each metadata value means; use them rather than
hardcoding bit layouts per block.

26.1.2: compute the offset within `MinStateID..MaxStateID`. The ordering of
properties within that range is data, not convention — derive it from the
generated state information rather than assuming facing comes first. If the
generated data does not carry enough to derive it, that is a finding to report
before writing a guess: a state offset computed from an assumed property order
is wrong for every block whose properties are ordered differently, and it looks
right for the ones that happen to match.

- [ ] **Step 4: Run the tests and gates**

- [ ] **Step 5: Commit**

```bash
cd minecraft-simulation
git add profile/java/
git commit -m "feat(profile): resulting block state per version

1.8.9 addresses states as id plus metadata and 26.1.2 as a flat range
with per-state shapes. The behavioural tests read the same on both; the
numbers do not, which is why the rule is version-owned."
```

---

## Task 4: The placement command and phase

**Files:**
- Create: `minecraft-simulation/placement/phase.go`
- Test: `minecraft-simulation/placement/phase_test.go`

**Interfaces:**
- Produces:

```go
// Place asks the kernel to place the held item against a face.
type Place struct {
    Entity  entity.ID
    Clicked geom.BlockPos
    Face    Face
    // Cursor is where within the clicked face the click landed, in the range
    // [0,1] per axis. Slabs and stairs need it; most blocks do not.
    Cursor geom.Vec3
}

func (Place) CommandKind() string { return "placement.place" }

// Phase is the kernel phase that applies placements.
func Phase() sim.Phase
```

- [ ] **Step 1: Write the failing test**

```go
func TestAnAcceptedPlacementEmitsExactlyOneSetBlock(t *testing.T) {
	t.Parallel()

	k := kernelFor(t, flatWorld(t))
	result := step(t, k, placement.Place{Entity: 1, Clicked: ground, Face: FaceTop})

	ops := blockOps(t, result.Changes)
	if len(ops) != 1 {
		t.Fatalf("emitted %d block operations, want 1: %+v", len(ops), ops)
	}
	if ops[0].Block != above(ground) {
		t.Fatalf("placed at %v, want %v", ops[0].Block, above(ground))
	}
}

func TestARefusedPlacementEmitsNoChangeAndSaysWhy(t *testing.T) {
	t.Parallel()

	// The pair matters. A refusal that still emits a change writes a block the
	// server will not have; a refusal with no reason gives the caller nothing
	// to act on.
	k := kernelFor(t, solidWorld(t))
	result := step(t, k, placement.Place{Entity: 1, Clicked: ground, Face: FaceTop})

	if len(blockOps(t, result.Changes)) != 0 {
		t.Fatal("a refused placement emitted a block change")
	}
	outcome := onlyOutcome(t, result)
	if outcome.Accepted {
		t.Fatal("the placement was accepted")
	}
	if outcome.Reason == "" {
		t.Fatal("a refused placement gave no reason")
	}
}

func TestAPlacementAgainstAnUnknownCellProducesNoChangeSet(t *testing.T) {
	t.Parallel()

	// M8.3's contract: an incomplete tick carries no applicable change set, so
	// applying it is impossible rather than merely discouraged.
	k := kernelFor(t, emptyWorld(t))
	result := step(t, k, placement.Place{Entity: 1, Clicked: unknown, Face: FaceTop})

	if result.Completeness.Complete {
		t.Fatal("a placement against an unknown cell reported a complete tick")
	}
	if !result.Changes.IsEmpty() {
		t.Fatal("an incomplete tick carried a change set")
	}
}

func TestTwoPlacementsInOneTickApplyInOrder(t *testing.T) {
	t.Parallel()

	// Operations keep insertion order and a later one may overwrite an
	// earlier one. Sorting them would change what the change set means.
	k := kernelFor(t, flatWorld(t))
	result := step(t, k,
		placement.Place{Entity: 1, Clicked: ground, Face: FaceTop},
		placement.Place{Entity: 1, Clicked: ground, Face: FaceTop},
	)

	outcomes := result.Outcomes
	if len(outcomes) != 2 {
		t.Fatalf("got %d outcomes, want 2", len(outcomes))
	}
	if outcomes[0].Accepted == outcomes[1].Accepted {
		t.Fatal("both placements into the same cell had the same outcome; the " +
			"second must see the first")
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

- [ ] **Step 3: Implement**

- [ ] **Step 4: Run the tests and gates**

- [ ] **Step 5: Commit**

```bash
cd minecraft-simulation
git add placement/
git commit -m "feat(placement): the place command and its kernel phase

A refusal emits no change and carries a reason. An incomplete tick
carries no change set at all, so applying it is impossible rather than
discouraged. Two placements in one tick see each other."
```

---

## Task 5: The client placement action

**Files:**
- Create: `headless-minecraft/client/place.go`
- Test: `headless-minecraft/client/place_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestAPlacementReachesTheServerAndTheBlockAppears(t *testing.T) {
	t.Parallel()

	for _, version := range vanillaVersions(t) {
		t.Run(version.Name, func(t *testing.T) {
			server := vanilla.Start(t, version.Options)
			c := connected(t, server)
			blocks := subscribe(t, c, event.DomainWorld)

			if err := c.Do(t.Context(), client.ActionPlace{
				Clicked: world.BlockPosition{X: 0, Y: 63, Z: 0},
				Face:    client.FaceTop,
			}); err != nil {
				t.Fatalf("Do: %v", err)
			}

			changed := awaitBlocksChanged(t, blocks)
			if !changed.Includes(world.BlockPosition{X: 0, Y: 64, Z: 0}) {
				t.Fatalf("the server changed %v, want the cell above the clicked face",
					changed.Positions())
			}
		})
	}
}

func TestARefusedPlacementDoesNotDesynchronise(t *testing.T) {
	t.Parallel()

	// The failure this guards is silent: the client predicts a block, the
	// server refuses, and nothing corrects it, so the two disagree about that
	// cell for the rest of the session. Unlike a movement correction, no
	// packet announces this.
	server := vanilla.Start(t, defaultOptions(t))
	c := connected(t, server)

	_ = c.Do(t.Context(), client.ActionPlace{
		Clicked: world.BlockPosition{X: 0, Y: 63, Z: 0},
		Face:    client.FaceTop,
	})
	_ = c.Do(t.Context(), client.ActionPlace{
		Clicked: world.BlockPosition{X: 0, Y: 63, Z: 0},
		Face:    client.FaceTop,
	})

	settle(t, c)
	if got := c.World().BlockAt(world.BlockPosition{X: 0, Y: 65, Z: 0}); got.Known() {
		t.Fatalf("the client believes %v holds %v after a refused placement",
			world.BlockPosition{X: 0, Y: 65, Z: 0}, got)
	}
}

func TestThe775PlacementCarriesASequenceNumber(t *testing.T) {
	t.Parallel()

	// 775 numbers block actions so the server can acknowledge and roll back a
	// mispredicted one. 47 has no such field, and a shared encoder that sent
	// one would be sending bytes the server reads as something else.
	server := vanilla.Start(t, options775(t))
	c := connected(t, server)
	sent := recordOutbound(t, c)

	_ = c.Do(t.Context(), client.ActionPlace{
		Clicked: world.BlockPosition{X: 0, Y: 63, Z: 0}, Face: client.FaceTop,
	})

	if seq := placementSequences(t, sent); len(seq) == 0 {
		t.Fatal("the 775 placement carried no sequence number")
	} else if !slices.IsSorted(seq) {
		t.Fatalf("sequences = %v, want monotonic", seq)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

- [ ] **Step 3: Implement**

- [ ] **Step 4: Run the tests and gates**

- [ ] **Step 5: Commit**

```bash
git add client/place.go client/place_test.go internal/adapter/
git commit -m "feat(client): place a block, with 775's action sequence

A refused placement must not leave the client believing in a block the
server never had: nothing announces that disagreement the way a movement
correction does."
```

---

## Task 6: The gate and the milestone record

**Files:**
- Create: `minecraft-simulation/conformance/placement_test.go`
- Create: `minecraft-simulation/conformance/testdata/placement/`
- Modify: `headless-minecraft/MASTER_PLAN.md`

- [ ] **Step 1: Capture the corpus on both versions**

Place, on each version: stairs from four yaws, a slab against a top face and an
underside, a log against each of three axes, a torch on a wall, a door, a plain
block, a placement into a mob, a placement out of reach, and a placement into a
replaceable block. Record what the server said the resulting state was.

- [ ] **Step 2: Write the failing gate**

The comparison is exact on both legality and resulting state. A placement is a
discrete decision with no wire quantisation, so there is nothing here a
tolerance would legitimately absorb.

- [ ] **Step 3: Declare the scenario, run it, fix what it names**

- [ ] **Step 4: Record the milestone**

Write what the work found. Candidates: whether the 26.1.2 state-offset ordering
was derivable from generated data or needed a new dump; whether any block's
`Variations` in the 1.8.9 data disagreed with what the server placed; and how
many placeable items the sweep test found unhandled on the first run.

- [ ] **Step 5: Commit**

```bash
git commit -m "docs(plan): close M9.5, and what the placement corpus found"
```

---

## Definition of done

- `Resolve` places against the face for a solid clicked block and into the cell
  for a replaceable one.
- `Check` refuses with a named reason for an occupied cell, an overlapping
  entity, an out-of-reach target, and an out-of-world target; allows a bottom
  slab under a standing mob; and reports an unknown cell as incomplete.
- The entity check goes through M8.2's collision code, not a second overlap
  implementation.
- Each profile computes the resulting state from its own addressing, and a sweep
  test proves every placeable item resolves one.
- The phase emits exactly one block operation on acceptance, none on refusal,
  and none at all on an incomplete tick.
- The client places on both protocols, carries 775's action sequence, and does
  not retain a block the server refused.
- Legality and resulting state match vanilla exactly across the corpus on both
  versions.
- `task lint`, `task test` under `-race`, and `task verify` pass in
  `minecraft-simulation` and `headless-minecraft`.

## Risks

**The 26.1.2 state-offset ordering may not be derivable from what is
generated.** `collision_shapes.go` indexes shapes by state offset, which proves
the ordering exists and is stable, but not that the generated data names which
property varies fastest. If it does not, this stage needs a data addition in
`minecraft-protocol` before Task 3 can be written honestly, and that is a
blocker to report rather than a place to guess. A state offset computed from an
assumed property order is right for the blocks that happen to match and wrong
for the rest.

**A refused placement desynchronises silently.** Unlike movement, nothing sends
a correction. The client simply believes in a block that is not there. This is
the most likely defect to survive every automated gate in this stage and show up
as an unexplained failure in M9.7's container work, so Task 5's second test
earns its place.

**Placement legality depends on rules this stage does not model.** Support
requirements — a torch needs a wall, a door needs two cells, a sapling needs
dirt — are per-block data that neither version's generated set clearly carries.
`ReasonUnsupported` is declared for it, and the corpus includes a torch and a
door to find out how far the generated data actually gets. If it does not get
far, say what is missing rather than special-casing the two blocks the corpus
happens to test.
