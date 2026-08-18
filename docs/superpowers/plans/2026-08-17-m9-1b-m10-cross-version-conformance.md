# M9.1b and M10 Cross-Version Conformance Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `subagent-driven-development` or execute this plan inline one task at a time. Keep every checkbox current.

**Goal:** Make the M9 capture oracle speak protocol 775 against a pinned vanilla 26.1.2 server, give every M9 mechanic gate a two-version shape, and extend M10's conformance matrix so a release is evidence about both versions rather than about 1.8.9 alone.

**Architecture:** The proxy, the codec, and the capture format are already version-parameterised — `mcrelay` takes `-protocol` and defaults to 775 — so M9.1b is not a new program. What is 47-only is the trace extractor, which turns a recording into trajectories, and it says so itself: "a second version is a second implementation rather than a flag" (`relay/examples/minecraft/trace/extract.go:16`). This plan splits that file behind a per-version interface, writes the 775 implementation against real generated types, derives a replay tolerance from 775's own position encoding instead of inheriting 1.8's, proves the whole thing against a real client and server, and then wires both versions into the gate harness M9.2–M9.8 share and into M10's matrix.

**Tech Stack:** Go 1.26.6 from `openserbia/go-flake`, Devbox, Task, `relay` (proxy transport, `Sink`, hooks), `minecraft-protocol` (`capture`, `replay`, `protocols`, `generated/java/v1_8`, `generated/java/v26_1`), a pinned vanilla 1.8.9 server, a pinned vanilla 26.1.2 server, and real vanilla clients of both versions.

## Global Constraints

- Go 1.26.6 from `openserbia/go-flake`; Devbox and Task drive every gate.
- `relay/examples/minecraft` is the capture oracle. It must not import
  `minecraft-simulation`: an oracle that depends on the thing it verifies would
  cancel out a wrong constant on both sides of the comparison.
- Capture records what happened. It never synthesises a packet, never repairs a
  malformed one, and never reorders.
- Do not name the legacy proxy's project, its protocol, or its codename in any
  file or commit message in this or any other public repository. Refer to it by
  role.
- Every check in this plan runs against offline mode, because M6.4 is
  postponed. Anything that differs between online and offline mode is outside
  this oracle's reach.
- Pinned artifacts only. A server or client fetched without a recorded digest is
  not evidence.
- `task lint`, `task test` under `-race`, and `task verify` pass in every
  repository this plan touches, before every commit.

---

## Design decisions this plan settles

**A version is an implementation, not a flag.** The extractor's own doc comment
already argues this, and the generated types confirm it. On protocol 47
`PlayClientboundEntityTeleport` carries `X, Y, Z int32` in units of 1/32 of a
block; on 775 the same packet carries `X, Y, Z float64`. On 47
`PlayClientboundRelEntityMove` carries `DX, DY, DZ int8`; on 775 it carries
`int16`. Decoding one with the other's rules yields numbers that look right and
are not. Task 1 makes the split structural so that the compiler, rather than a
reviewer, enforces it.

**The 775 tolerance is not one thirty-second of a block, and it is not one
number.** 1.8 quantises everything: absolute positions arrive as fixed point, so
a captured trace verifies to 1/32 and no further. 775 does not. Its absolute
position packets carry `float64`, so at every absolute position the comparison
is exact, and quantisation enters only across runs of relative moves, whose
deltas widened from `int8` to `int16`. That means the 775 lane has a tolerance
of zero at absolute samples and a derived, measured tolerance across relative
runs. Task 3 measures it from a real capture rather than asserting it, because
the resolution the deltas actually carry is a property of the server's encoder
and this project has already been bitten once by reasoning about wire formats
from prose.

**A mechanic that does not exist in a version is a recorded finding, not a
skipped test.** The attack cooldown M9.6 names arrived in 1.9; 1.8.9 has none.
The gate harness in Task 5 lets a scenario declare a mechanic absent for a
version, and requires a reason string, so the conformance report distinguishes
"verified absent" from "never checked". Silently passing a scenario that never
ran is the failure this whole milestone exists to prevent.

**M9.1b gates M9.2, it does not run beside it.** M9.2 is the first stage that
compares a captured trajectory to the kernel, and its gate is now a two-version
gate. If the 775 extractor is not finished, M9.2 cannot state its result
honestly. Sequencing it after M9.1's live check and before M9.2 costs nothing
that running them in parallel would save.

## File structure

**`relay/examples/minecraft/trace/`**

- `extract.go` — the version-neutral driver: opens the recording, resolves the
  descriptor, dispatches to a registered per-version extractor, orders the
  results. Keeps `Extract`, `ExtractFile`, `ErrUnsupportedProtocol`,
  `ErrUnknownEntity`, and `ErrNoTrajectories` exactly as they are today.
- `v1_8.go` — today's protocol 47 packet handling, moved verbatim.
- `v1_8_test.go` — today's protocol 47 tests, moved verbatim.
- `v26_1.go` — new. Protocol 775 packet handling.
- `v26_1_test.go` — new.
- `tolerance.go` — new. Per-version replay tolerance, with the derivation in the
  doc comment.
- `tolerance_test.go` — new.

**`relay/examples/minecraft/conform/`** — new package, the two-version gate
harness every M9 mechanic stage uses.

- `conform.go` — `Scenario`, `Lane`, `Absent`, `Run`, `Report`.
- `conform_test.go`

**`relay/docs/verification/`**

- `2026-08-17-capture-oracle.md` — modify. Add the 775 procedure alongside the
  47 one.

**`headless-minecraft/`**

- `docs/superpowers/plans/2026-08-16-m10-conformance-releases.md` — modify.
  Tasks 1 and 2 gain their 26.1.2 artifacts and matrix rows.
- `MASTER_PLAN.md` — modify. Record the milestone.

---

### Task 1: Split the extractor behind a per-version interface

No behaviour changes. This task exists on its own because a move plus a rewrite
in one commit is a commit nobody can review, and because the 47 tests passing
unchanged afterwards is the proof that the move was faithful.

**Files:**
- Modify: `relay/examples/minecraft/trace/extract.go`
- Create: `relay/examples/minecraft/trace/v1_8.go`
- Create: `relay/examples/minecraft/trace/v1_8_test.go`
- Modify: `relay/examples/minecraft/trace/extract_test.go`

**Interfaces:**
- Consumes: `protocol.Protocol`, `protocol.Limits`, `mccapture.Record`,
  `v1_8.Protocol()`.
- Produces:

```go
// versionRules turns one version's play packets into motion on the shared
// accumulator, which is today's *extractor — the type at extract.go:118 that
// holds live, order, player, self, and the play-record counters. That type
// keeps its name and its methods; only the packet switch moves out of it.
//
// This is an interface rather than a switch because the packet sets, the
// coordinate scales, and the spawn packets are all version-specific. A
// registry keyed by protocol ID means an unregistered version fails at
// ErrUnsupportedProtocol rather than being decoded by the wrong rules.
type versionRules interface {
    // ProtocolID is the descriptor ID these rules read.
    ProtocolID() string
    // Apply folds one decoded play packet into the accumulator. It returns
    // false when the packet carries no motion, so the driver can tell "read
    // and irrelevant" from "not read" — which is what the playOffered and
    // playDecoded counters exist to distinguish.
    Apply(e *extractor, record mccapture.Record, packet protocol.Packet) (bool, error)
}

// register adds a rule set. It panics on a duplicate protocol ID, because two
// rule sets for one version is a build mistake, not a runtime condition.
func register(r versionRules)
```

The accumulator methods the rule sets call already exist and keep their exact
signatures — do not rename them:

```go
func (e *extractor) spawn(record mccapture.Record, id int32, family string, at, motion Vec3)
func (e *extractor) absolute(record mccapture.Record, id int32, at Vec3, onGround bool) error
func (e *extractor) relative(record mccapture.Record, id int32, by Vec3, onGround bool) error
func (e *extractor) velocity(id int32, motion Vec3) error
func (e *extractor) playerAt(record mccapture.Record, at Vec3, flags int8, onGround bool) error
func (e *extractor) close(id int32)
func (e *extractor) done() []Trace
```

One of these does not survive contact with 775: `playerAt` takes `flags int8`,
which is protocol 47's relativity bitmask. 775 carries relativity in
`PlayClientboundPositionFlagsFlags`, a struct. Task 2 Step 4 says what to do
about it; Task 1 leaves the signature alone.

- [x] **Step 1: Write the failing test**

Create `relay/examples/minecraft/trace/v1_8_test.go`. Keep the suite black-box —
`package trace_test`, like `extract_test.go` — and test the registry through
`SupportedProtocols` rather than reaching for the unexported `lookup`. The
suite already has `TestAnotherProtocolIsRefused` covering the refusal path; this
adds the positive half.

```go
package trace_test

import (
	"slices"
	"testing"

	"github.com/go-theft-craft/minecraft-protocol/generated/java/v1_8"

	"github.com/go-theft-craft/relay/examples/minecraft/trace"
)

func TestProtocol47IsRegistered(t *testing.T) {
	t.Parallel()

	got := trace.SupportedProtocols()
	if !slices.Contains(got, v1_8.Protocol().ID()) {
		t.Fatalf("SupportedProtocols() = %v, want it to contain %q",
			got, v1_8.Protocol().ID())
	}
}

func TestSupportedProtocolsIsSortedAndDeduplicated(t *testing.T) {
	t.Parallel()

	// The conformance harness enumerates this to decide which lanes a
	// scenario must declare, and it prints it in errors. An unstable order
	// makes those errors and any golden output flap.
	got := trace.SupportedProtocols()
	if !slices.IsSorted(got) {
		t.Fatalf("SupportedProtocols() = %v, want sorted", got)
	}
	if len(slices.Compact(slices.Clone(got))) != len(got) {
		t.Fatalf("SupportedProtocols() = %v, want no duplicates", got)
	}
}
```

- [x] **Step 2: Run it to verify it fails**

Run: `cd relay && go test ./examples/minecraft/trace/ -run 'TestProtocol47IsRegistered|TestSupportedProtocols' -v`
Expected: FAIL — `undefined: trace.SupportedProtocols`.

- [x] **Step 3: Move protocol 47 into `v1_8.go`**

Move the body of `(*extractor).apply` — the `switch value := packet.Value.(type)`
at `extract.go:210` and its `case *v1_8.Play...` arms — into a new `v1_8.go`,
along with the 47-specific constants: the `positionRelative*` flags around
`:266-275`, `fixed` around `:428`, and the object-type constants around
`:456-462`.

```go
package trace

import (
	protocol "github.com/go-theft-craft/minecraft-protocol"
	mccapture "github.com/go-theft-craft/minecraft-protocol/capture"
	"github.com/go-theft-craft/minecraft-protocol/generated/java/v1_8"
)

// v1_8Rules reads protocol 47.
//
// Its positions are fixed point in units of 1/32 of a block and its relative
// moves are int8, so a trajectory built from it is exact only to that
// resolution. See tolerance.go for what that means for a comparison.
type v1_8Rules struct{}

func init() { register(v1_8Rules{}) }

func (v1_8Rules) ProtocolID() string { return v1_8.Protocol().ID() }

func (v1_8Rules) Apply(e *extractor, record mccapture.Record, packet protocol.Packet) (bool, error) {
	switch value := packet.Value.(type) {
	case *v1_8.PlayClientboundLogin:
		e.self, e.named = value.EntityID, true
	// ... every remaining arm moved verbatim
	default:
		return false, nil
	}
	return true, nil
}
```

`apply` itself stays in `extract.go` as the dispatcher, keeping the
`playOffered` and `playDecoded` accounting that surrounds it — that counting is
version-neutral and moving it would spread one invariant across two files. Its
new body resolves the rules once per extraction and calls `Apply`.

Keep every moved line byte-identical apart from the receiver change the move
forces. If a moved line needs a behaviour change to compile, stop: that is a
finding about the current code, and it belongs in its own commit before this one.

- [x] **Step 4: Add the registry and the dispatch in `extract.go`**

```go
// rules is keyed by protocol ID. It is populated by init in each version
// file, so adding a version is adding a file.
var rules = map[string]versionRules{}

func register(r versionRules) {
	if _, dup := rules[r.ProtocolID()]; dup {
		panic("trace: two rule sets registered for " + r.ProtocolID())
	}
	rules[r.ProtocolID()] = r
}

func lookup(id string) (versionRules, bool) {
	r, ok := rules[id]
	return r, ok
}

// SupportedProtocols is the sorted list of protocol IDs this package reads.
//
// Exported because the conformance harness enumerates it to refuse a scenario
// that checks one version and claims nothing about another.
func SupportedProtocols() []string
```

Replace the descriptor check at `extract.go:53-56` with:

```go
	handler, ok := lookup(descriptor.ID())
	if !ok {
		return nil, fmt.Errorf("%w: %q, want one of %s",
			ErrUnsupportedProtocol, descriptor.ID(), registeredIDs())
	}
```

where `registeredIDs` returns the sorted keys, so the error names what the tool
can read rather than only what it cannot.

- [x] **Step 5: Run the whole trace suite**

Run: `cd relay && go test ./examples/minecraft/trace/ -race -v`
Expected: PASS, including every pre-existing protocol 47 test, unchanged.

- [x] **Step 6: Run the repository gates**

Run: `cd relay && task lint && task test`
Expected: PASS.

- [x] **Step 7: Commit**

```bash
git add relay/examples/minecraft/trace/
git commit -m "refactor(trace): dispatch extraction per protocol version

The extractor's own comment says a second version is a second
implementation rather than a flag. Make that structural: a registry keyed
by protocol ID, one file per version, and an unregistered version failing
at ErrUnsupportedProtocol. No behaviour change; the protocol 47 tests pass
unmoved."
```

---

### Task 2: The protocol 775 extractor

**Files:**
- Create: `relay/examples/minecraft/trace/v26_1.go`
- Create: `relay/examples/minecraft/trace/v26_1_test.go`

**Interfaces:**
- Consumes: `versionRules`, `register`, and the `*extractor` accumulator from Task 1;
  `generated/java/v26_1`.
- Produces: a registered extractor for `v26_1.Protocol().ID()`.

The packets this task handles, with the field types the generated code actually
declares — these are the reason a flag would not have worked:

| Packet | 775 field types | 47 field types |
| --- | --- | --- |
| `PlayClientboundEntityTeleport` | `X, Y, Z float64` | `X, Y, Z int32`, 1/32 block |
| `PlayClientboundRelEntityMove` | `DX, DY, DZ int16` | `DX, DY, DZ int8` |
| `PlayClientboundEntityMoveLook` | `DX, DY, DZ int16` | `DX, DY, DZ int8` |
| `PlayClientboundPosition` | `X, Y, Z, Dx, Dy, Dz float64`, `Flags PlayClientboundPositionFlagsFlags`, `TeleportID int32` | `X, Y, Z float64`, bitmask `int8` |
| `PlayClientboundSyncEntityPosition` | 775 only — no 47 counterpart |

- [x] **Step 1: Write the failing test**

Follow the existing suite's shape exactly: `extract_test.go` is `package
trace_test`, and its `recorder` encodes real packets through a real server-role
session rather than hand-assembling payloads, "because a test that
hand-assembled payloads would prove the extractor agrees with the test's idea of
the wire format". That reasoning applies to 775 unchanged.

First, generalise the recorder in `extract_test.go` — `newRecorder(t)` hardcodes
`v1_8.Protocol()` in both its `NewSession` calls. Add
`newRecorderFor(t, descriptor protocol.Protocol) *recorder` and make
`newRecorder(t)` call it with `v1_8.Protocol()`, so no existing test changes.
Then:

```go
package trace_test

import (
	"errors"
	"math"
	"testing"

	mccapture "github.com/go-theft-craft/minecraft-protocol/capture"
	"github.com/go-theft-craft/minecraft-protocol/generated/java/v26_1"

	"github.com/go-theft-craft/relay/examples/minecraft/trace"
)

func extract775(t *testing.T, records []mccapture.Record) []trace.Trace {
	t.Helper()

	traces, err := trace.Extract(v26_1.Protocol(), testLimits(t), records)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	return traces
}

func TestProtocol775TeleportIsExact(t *testing.T) {
	t.Parallel()

	// 775 sends absolute positions as float64, so the trajectory carries the
	// server's own number. Asserting equality rather than a tolerance is the
	// point: a rounding step borrowed from the 47 path would fail here.
	r := newRecorderFor(t, v26_1.Protocol())
	records := []mccapture.Record{
		r.record(&v26_1.PlayClientboundSpawnEntity{EntityID: 42, X: 0, Y: 64, Z: 0}),
		r.record(&v26_1.PlayClientboundEntityTeleport{
			EntityID: 42, X: 1.0 / 3.0, Y: 64.5, Z: -2.25,
		}),
	}

	traces := extract775(t, records)
	if len(traces) != 1 {
		t.Fatalf("extracted %d traces, want 1", len(traces))
	}

	last := traces[0].Samples[len(traces[0].Samples)-1]
	if last.Position.X != 1.0/3.0 {
		t.Fatalf("X = %v, want %v exactly; the 775 teleport is float64 and must "+
			"not be rounded", last.Position.X, 1.0/3.0)
	}
}

func TestProtocol775RelativeMoveUsesItsOwnScale(t *testing.T) {
	t.Parallel()

	// The delta widened from int8 to int16 between 47 and 775. Reading it at
	// 47's 1/32 scale produces a plausible number wrong by the ratio of the
	// two scales, which is exactly what a shared implementation would have
	// done silently.
	r := newRecorderFor(t, v26_1.Protocol())
	records := []mccapture.Record{
		r.record(&v26_1.PlayClientboundSpawnEntity{EntityID: 7, X: 0, Y: 64, Z: 0}),
		r.record(&v26_1.PlayClientboundRelEntityMove{EntityID: 7, DX: 4096}),
	}

	traces := extract775(t, records)
	last := traces[0].Samples[len(traces[0].Samples)-1]
	if math.Abs(last.Position.X-1.0) > 1e-9 {
		t.Fatalf("X = %v after a one-block delta, want 1.0", last.Position.X)
	}
}

func TestProtocol775MovementForAnUnspawnedEntityIsAFinding(t *testing.T) {
	t.Parallel()

	r := newRecorderFor(t, v26_1.Protocol())
	records := []mccapture.Record{
		r.record(&v26_1.PlayClientboundRelEntityMove{EntityID: 99, DX: 1}),
	}

	_, err := trace.Extract(v26_1.Protocol(), testLimits(t), records)
	if !errors.Is(err, trace.ErrUnknownEntity) {
		t.Fatalf("err = %v, want ErrUnknownEntity; a relative move with no anchor "+
			"must not start a trace at the origin", err)
	}
}
```

`PlayClientboundSpawnEntity`'s 775 field names are not assumed here — read them
out of `generated/java/v26_1/packets.go` and use what is there. If 775 spawns
carry an entity-type field rather than a distinct packet per family, the spawn
in these tests needs that field set to whatever the registry names an item.

- [x] **Step 2: Run it to verify it fails**

Run: `cd relay && go test ./examples/minecraft/trace/ -run TestProtocol775 -v`
Expected: FAIL — `undefined: v26_1Rules`.

- [x] **Step 3: Confirm the relative-move scale against the game before implementing it**

Do not take `4096` from this plan. It is the value the test above asserts
because it is the documented modern encoding, and this project has already paid
once for a wire-format detail taken from prose. Confirm it from the pinned
26.1.2 server before writing the constant:

```bash
# From a capture taken in Task 4, or from a scratch capture against the pinned
# server, print consecutive teleport and rel-move packets for one entity.
cd relay && go run ./examples/minecraft/cmd/mcrelay trace \
  --format json path/to/recording.mccap \
  | jq '.traces[] | select(.family=="item") | .samples[0:8]'
```

Divide an observed `int16` delta by the block distance the surrounding absolute
positions imply. If the quotient is not 4096, use what you measured and say so
in the doc comment, with the recording's digest. If you cannot get a capture
yet, do Task 4 first and return here — an unverified scale is exactly the
"plausible and quietly wrong" outcome the extractor's comment warns about.

- [x] **Step 4: Implement `v26_1.go`**

```go
package trace

import (
	protocol "github.com/go-theft-craft/minecraft-protocol"
	mccapture "github.com/go-theft-craft/minecraft-protocol/capture"
	"github.com/go-theft-craft/minecraft-protocol/generated/java/v26_1"
)

// relMoveScale is the number of delta units in one block on protocol 775.
//
// Measured, not assumed: <recording digest from Step 3> shows a delta of N
// units across a block of movement. Protocol 47's equivalent is 32, and its
// delta is int8; reading a 775 delta at 47's scale misreads the move by the
// ratio of the two scales.
const relMoveScale = 4096.0

// v26_1Rules reads protocol 775.
//
// Absolute positions are float64 here, so a sample taken at a teleport or a
// position packet carries the server's own number exactly. Quantisation enters
// only across runs of relative moves. See tolerance.go.
type v26_1Rules struct{}

func init() { register(v26_1Rules{}) }

func (v26_1Rules) ProtocolID() string { return v26_1.Protocol().ID() }

func (r v26_1Rules) Apply(e *extractor, record mccapture.Record, packet protocol.Packet) (bool, error) {
	switch value := packet.Value.(type) {
	// The join packet carries the connecting player's entity ID and no
	// position, so it opens no trace — same as protocol 47.
	case *v26_1.PlayClientboundLogin:
		e.self, e.named = value.EntityID, true

	case *v26_1.PlayClientboundPosition:
		// 775 carries relativity per axis in a flags struct rather than 47's
		// int8 bitmask, and adds Dx/Dy/Dz for delta movement. playerAt's
		// signature takes int8; see Step 5 for which way to resolve that.
		if err := e.playerAt(record, Vec3{X: value.X, Y: value.Y, Z: value.Z},
			relativeMask(value.Flags), true); err != nil {
			return false, err
		}

	case *v26_1.PlayClientboundEntityTeleport:
		if err := e.absolute(record, value.EntityID,
			Vec3{X: value.X, Y: value.Y, Z: value.Z}, value.OnGround); err != nil {
			return false, err
		}

	case *v26_1.PlayClientboundSyncEntityPosition:
		// 775 only; protocol 47 has no counterpart. Read its fields before
		// writing this arm — it may carry velocity alongside position, in
		// which case it is an absolute plus a velocity, not just an absolute.
		if err := e.absolute(record, value.EntityID,
			Vec3{X: value.X, Y: value.Y, Z: value.Z}, value.OnGround); err != nil {
			return false, err
		}

	case *v26_1.PlayClientboundRelEntityMove:
		if err := e.relative(record, value.EntityID,
			r.delta(value.DX, value.DY, value.DZ), value.OnGround); err != nil {
			return false, err
		}

	case *v26_1.PlayClientboundEntityMoveLook:
		if err := e.relative(record, value.EntityID,
			r.delta(value.DX, value.DY, value.DZ), value.OnGround); err != nil {
			return false, err
		}

	// 775 packs velocity into a java.LPVec3 rather than three int16 fields.
	// Read that type's units before writing the conversion: 47's velocity is
	// in units of 1/8000 of a block per tick, and nothing guarantees 775 kept
	// it.
	case *v26_1.PlayClientboundEntityVelocity:
		if err := e.velocity(value.EntityID, velocityBlocks(value.Velocity)); err != nil {
			return false, err
		}

	case *v26_1.PlayClientboundEntityDestroy:
		for _, id := range value.EntityIds {
			e.close(id)
		}

	default:
		return false, nil
	}
	return true, nil
}

// delta converts 775's int16 relative move to blocks.
func (v26_1Rules) delta(dx, dy, dz int16) Vec3 {
	return Vec3{
		X: float64(dx) / relMoveScale,
		Y: float64(dy) / relMoveScale,
		Z: float64(dz) / relMoveScale,
	}
}
```

Three things this skeleton leaves for you to settle from the generated source,
none of which should be guessed:

- **`relativeMask`.** `playerAt` takes `flags int8`, which is 47's bitmask.
  Either write `relativeMask(PlayClientboundPositionFlagsFlags) int8` to project
  775's struct onto the same three bits `positionRelativeX/Y/Z` already name, or
  widen `playerAt` to take a small `relativity` struct and update the 47 caller.
  Prefer the projection if 775's flags carry only the same three axes; widen if
  it carries more, because dropping a flag on the floor is the silent-wrongness
  this milestone exists to prevent. Also decide what to do with `Dx/Dy/Dz`: if
  they are a separate delta-movement mechanism rather than the same relativity
  expressed twice, `playerAt` cannot represent them and needs an addition.
- **The spawn packets.** Read the `PlayClientboundSpawn*` set in
  `generated/java/v26_1/packets.go` and map each to a `Family`, calling
  `e.spawn(record, id, family, at, motion)`. Where 775 has consolidated 47's
  several spawn packets into fewer, switch on the entity-type field rather than
  packet identity, and add a `Family` constant for anything 47 had no name for
  rather than folding it into an existing one.
- **`velocityBlocks`.** 775's `PlayClientboundEntityVelocity` carries a
  `java.LPVec3`, not three `int16` fields. Read that type and its units before
  converting; 47's velocity is 1/8000 of a block per tick and nothing
  guarantees 775 kept the scale. Add a test in the shape of the existing
  `TestVelocityDoesNotInventAPosition` — a velocity must not produce a sample,
  on either version.
- **Entity removal is already in the skeleton**, but note the field is
  `EntityIds`, not 47's spelling. Omitting the arm fails no test in this task
  while making a reused entity ID append to a dead trace — the defect
  `TestAReusedEntityIDStartsANewTrace` guards on 47. Add its 775 twin.

Do not guess a field name anywhere in this task. `EntityID`, `X/Y/Z`, `DX/DY/DZ`,
`OnGround`, `TeleportID`, and `Flags` above were read out of
`generated/java/v26_1/packets.go`; check every name you add the same way and let
the compiler settle the rest.

- [x] **Step 5: Run the tests**

Run: `cd relay && go test ./examples/minecraft/trace/ -race -v`
Expected: PASS, both version suites.

- [x] **Step 6: Run the repository gates**

Run: `cd relay && task lint && task test && task test:examples`
Expected: PASS.

- [x] **Step 7: Commit**

```bash
git add relay/examples/minecraft/trace/v26_1.go relay/examples/minecraft/trace/v26_1_test.go
git commit -m "feat(trace): extract trajectories from protocol 775

775 sends absolute positions as float64 and relative deltas as int16,
where 47 sends fixed point and int8. Separate implementation, measured
scale, and a test that fails if the 47 scale is reused."
```

---

### Task 3: Derive the 775 replay tolerance from its own encoding

**Files:**
- Create: `relay/examples/minecraft/trace/tolerance.go`
- Create: `relay/examples/minecraft/trace/tolerance_test.go`

**Interfaces:**
- Consumes: `lookup`, `Trace`, `Sample`.
- Produces:

```go
// Tolerance is how far a compared trajectory may differ from a captured one
// before the difference is a finding rather than the wire's resolution.
//
// It has two numbers because 775 has two regimes. Absolute is the allowance at
// a sample the server stated outright; Relative is the allowance accumulated
// across a run of relative moves, per move.
type Tolerance struct {
    Absolute float64 // blocks
    Relative float64 // blocks per relative move
    // Why records the derivation, so a report can print why a comparison
    // passed at the number it used.
    Why string
}

// ToleranceFor returns the tolerance for one protocol ID.
// It returns ErrUnsupportedProtocol for a version with no registered
// extractor, so a comparison cannot silently default to 1.8's number.
func ToleranceFor(protocolID string) (Tolerance, error)
```

- [x] **Step 1: Write the failing test**

```go
// package trace_test, like the rest of the suite.
func TestTheTolerancesDifferByVersion(t *testing.T) {
	t.Parallel()

	old, err := trace.ToleranceFor(v1_8.Protocol().ID())
	if err != nil {
		t.Fatalf("ToleranceFor 47: %v", err)
	}
	modern, err := trace.ToleranceFor(v26_1.Protocol().ID())
	if err != nil {
		t.Fatalf("ToleranceFor 775: %v", err)
	}

	if old.Absolute != 1.0/32.0 {
		t.Fatalf("47 Absolute = %v, want 1/32; its positions are fixed point", old.Absolute)
	}
	if modern.Absolute != 0 {
		t.Fatalf("775 Absolute = %v, want 0; its position packets carry float64", modern.Absolute)
	}
	if modern.Relative >= old.Relative {
		t.Fatalf("775 Relative = %v, 47 Relative = %v; 775's wider delta must not "+
			"buy a looser comparison", modern.Relative, old.Relative)
	}
	if modern.Why == "" || old.Why == "" {
		t.Fatal("a tolerance with no recorded derivation is a magic number")
	}
}

func TestAnUnknownProtocolHasNoTolerance(t *testing.T) {
	t.Parallel()

	if _, err := trace.ToleranceFor("java/0.0"); !errors.Is(err, trace.ErrUnsupportedProtocol) {
		t.Fatal("ToleranceFor returned a tolerance for an unknown protocol; a " +
			"comparison must not fall back to another version's number")
	}
}
```

- [x] **Step 2: Run it to verify it fails**

Run: `cd relay && go test ./examples/minecraft/trace/ -run Toleranc -v`
Expected: FAIL — `undefined: ToleranceFor`.

- [x] **Step 3: Implement it**

```go
// tolerances is keyed by protocol ID. Each entry states its derivation,
// because the number is worthless without it: a reader who does not know why
// the allowance is what it is cannot tell a passing comparison from a
// comparison that was never tight enough to fail.
var tolerances = map[string]Tolerance{
	v1_8.Protocol().ID(): {
		Absolute: 1.0 / 32.0,
		Relative: 1.0 / 32.0,
		Why: "Protocol 47 transmits absolute positions as int32 fixed point in " +
			"units of 1/32 of a block and relative moves as int8 at the same " +
			"scale, so every sample is quantised to 1/32. This catches wrong " +
			"constants and wrong axis order, not last-place drift.",
	},
	v26_1.Protocol().ID(): {
		Absolute: 0,
		Relative: 1.0 / relMoveScale,
		Why: "Protocol 775 transmits absolute positions as float64, so a sample " +
			"taken at a teleport or position packet is the server's own number " +
			"and needs no allowance. Relative moves are int16 at " +
			"1/relMoveScale of a block, so only a run of relative moves " +
			"accumulates error, at that resolution per move.",
	},
}
```

- [x] **Step 4: Replace the hardcoded test constant**

`extract_test.go` opens with `const tolerance = 1.0 / 32` and a comment saying
protocol 47 sends positions as fixed point. That constant is now the 47 entry's
`Relative`. Delete it and have the 47 tests read `ToleranceFor` instead, so
there is one place the number lives and one place its derivation is written.
The 47 tests must still pass at the same value — if any of them needs a looser
number to survive, that is a finding about that test, not a reason to widen the
tolerance.

- [x] **Step 5: Run the tests**

Run: `cd relay && go test ./examples/minecraft/trace/ -race -v`
Expected: PASS.

- [x] **Step 6: Commit**

```bash
git add relay/examples/minecraft/trace/tolerance.go relay/examples/minecraft/trace/tolerance_test.go relay/examples/minecraft/trace/extract_test.go
git commit -m "feat(trace): per-version replay tolerance with its derivation

One thirty-second of a block is a 1.8 artifact, not a project-wide
number. 775's position packets are float64 and its deltas int16, so it
gets a zero absolute allowance and a tighter relative one. An unknown
protocol gets an error rather than another version's number."
```

---

### Task 4: The 775 live check

This is the task that makes M9.1b real. Everything before it drives packets this
project generated through code this project wrote; a shared misunderstanding of
775 passes all of it. It is a manual procedure with a written record, in the
same shape as M9.1's.

**Files:**
- Modify: `relay/docs/verification/2026-08-17-capture-oracle.md`

- [x] **Step 1: Pin the server and record its digest**

Fetch the vanilla 26.1.2 server, record its URL and SHA-256 in the verification
document, and start it in offline mode with a fixed seed and a flat world — the
same configuration M8.8's 26.1.2 lane uses, so the two checks are comparable.
A server fetched without a recorded digest is not evidence and this step is not
done.

- [x] **Step 2: Run a real 26.1.2 client through the proxy** — run with
`headless-minecraft`'s client, not a vanilla one: none is installed on this
machine. What that leaves open is the packet mix a real client sends, not the
wire format, and the verification document says so.

```bash
cd relay && go run ./examples/minecraft/cmd/mcrelay \
  --listen 127.0.0.1:25565 \
  --upstream 127.0.0.1:25566 \
  --capture ./recordings/26_1_2-walk.mccap
```

`-protocol` defaults to 775, so it is not passed here — the mistake this
document should warn about is the reverse of M9.1's, where the 47 lane must
pass `-protocol java/1.8.9` explicitly. Connect a real vanilla 26.1.2 client,
walk, sprint, jump, drop an item, and shoot an arrow. Disconnect cleanly so the
recording gets its trailer.

- [x] **Step 3: Verify the recording replays**

```bash
cd relay && go run ./examples/minecraft/cmd/mcrelay verify ./recordings/26_1_2-walk.mccap
```

Expected: a digest matching the file's trailer, `Complete: true`, and no
divergences. A file without a trailer means the capture was killed rather than
closed, which is a finding to record and repeat, not to work around.

- [x] **Step 4: Extract trajectories and read them**

```bash
cd relay && go run ./examples/minecraft/cmd/mcrelay trace --format json \
  ./recordings/26_1_2-walk.mccap | jq '.traces[] | {entity: .entityID, family: .family, samples: (.samples|length)}'
```

Expected: a trace for the player, one per dropped item, one per arrow, and a
non-zero sample count on each. An empty trace list with a zero exit code is the
failure mode `ErrNoTrajectories` exists to prevent; if it happens anyway, that
is a finding about this task's extractor.

- [x] **Step 5: Confirm the relative-move scale**

Use this recording to complete Task 2 Step 3 if it was deferred. Take one
entity's samples across a teleport, a run of relative moves, and the next
teleport. The accumulated relative motion must land within `Relative ×
(number of moves)` of the second teleport's absolute position. If it does not,
the scale constant is wrong, and the size of the miss says by what factor.

- [x] **Step 6: Write the record**

Add a `## Protocol 775` section to
`relay/docs/verification/2026-08-17-capture-oracle.md` with: the server URL and
digest, the client version, the exact commands, the recording's digest, the
trace counts, the measured relative-move scale, and anything that surprised you.
Write what happened, including what did not work the first time. A verification
document that records only the successful run is a document that will not help
the next person reproduce it.

- [x] **Step 7: Commit**

```bash
git add relay/docs/verification/2026-08-17-capture-oracle.md
git commit -m "docs(verification): record the protocol 775 capture check

A real 26.1.2 client through the proxy to a pinned offline vanilla
server, verified, traced, and the relative-move scale measured rather
than assumed."
```

---

### Task 5: The two-version gate harness

M9.2 through M9.8 each need to state a result per version, and need a way to
say "this version does not have this mechanic" that is distinguishable from
"this was not checked". Building it once here is cheaper than seven stages each
inventing it, and it is what makes the revised M9 gate wording enforceable
rather than aspirational.

**Files:**
- Create: `relay/examples/minecraft/conform/conform.go`
- Create: `relay/examples/minecraft/conform/conform_test.go`

**Interfaces:**
- Consumes: `trace.Trace`, `trace.Tolerance`, `trace.ToleranceFor`.
- Produces:

```go
// Lane is one version's run of one scenario.
type Lane struct {
    ProtocolID string
    // Recording is the captured trace this lane compares against.
    Recording string
    // AbsentReason, when non-empty, declares that the mechanic under test does
    // not exist in this version. A lane with a reason runs nothing and reports
    // Absent; a lane with neither a reason nor a recording is an error.
    AbsentReason string
}

// Scenario is one mechanic, checked on every version that has it.
type Scenario struct {
    Name  string
    Lanes []Lane
}

// Outcome is what one lane produced.
type Outcome struct {
    ProtocolID string
    Status     Status // Pass, Fail, Absent
    Tolerance  trace.Tolerance
    // MaxDeviation is the largest difference observed, in blocks. It is
    // reported on a pass as well as a failure, because a lane that passes at
    // 99% of its tolerance is a lane about to start failing.
    MaxDeviation float64
    Detail       string
}

// Run executes every lane. It returns an error, rather than a report, when a
// scenario names no lane for a version that has a registered extractor: a
// mechanic silently checked on one version is the defect this package exists
// to prevent.
func Run(ctx context.Context, s Scenario, compare Comparer) (Report, error)

// Comparer is the thing under test. M9.2 onward supply a kernel-backed one;
// the tests here supply a stub, because this package must not import
// minecraft-simulation.
type Comparer interface {
    Trajectories(ctx context.Context, protocolID string) ([]trace.Trace, error)
}

// Report is every lane's outcome for one scenario.
type Report struct {
    Scenario string
    Outcomes []Outcome
}

// Outcome returns the outcome for one protocol ID, and a zero Outcome with
// Status Missing if the scenario had no lane for it. Callers that want the
// error should use Run's, which fires before any lane executes.
func (r Report) Outcome(protocolID string) Outcome

// Status values. Missing is distinct from Absent on purpose: Absent means a
// version does not have the mechanic and says why, Missing means nobody
// checked.
const (
    Pass Status = iota
    Fail
    Absent
    Missing
)
```

- [x] **Step 1: Write the failing test**

```go
package conform_test

// stubComparer stands in for the kernel. This package must not import
// minecraft-simulation — an oracle that depends on the thing it verifies would
// reproduce a wrong constant on both sides and cancel it out — so the tests
// supply trajectories rather than computing them.
type stubComparer struct {
	byProtocol map[string][]trace.Trace
}

func (s stubComparer) Trajectories(_ context.Context, protocolID string) ([]trace.Trace, error) {
	return s.byProtocol[protocolID], nil
}

// twoLaneScenario is a scenario with a real lane per registered version, used
// by the tests that care about what Run does rather than what it rejects.
func twoLaneScenario(t *testing.T) conform.Scenario {
	t.Helper()

	return conform.Scenario{
		Name: "dropped item falls",
		Lanes: []conform.Lane{
			{ProtocolID: "java/1.8.9", Recording: "testdata/item-47.mccap"},
			{ProtocolID: "java/26.1", Recording: "testdata/item-775.mccap"},
		},
	}
}

func TestAScenarioMissingAVersionIsAnError(t *testing.T) {
	t.Parallel()

	// The whole point. A scenario that names only 47 must not quietly report
	// "pass" while saying nothing about 775.
	s := conform.Scenario{
		Name:  "dropped item falls",
		Lanes: []conform.Lane{{ProtocolID: "java/1.8.9", Recording: "testdata/item.mccap"}},
	}

	_, err := conform.Run(context.Background(), s, stubComparer{})
	if err == nil {
		t.Fatal("Run accepted a scenario that checks one version and claims nothing " +
			"about the other")
	}
	if !strings.Contains(err.Error(), "java/26.1") {
		t.Fatalf("err = %v, want it to name the version with no lane", err)
	}
}

func TestAnAbsentMechanicIsReportedRatherThanSkipped(t *testing.T) {
	t.Parallel()

	s := conform.Scenario{
		Name: "attack cooldown",
		Lanes: []conform.Lane{
			{ProtocolID: "java/1.8.9", AbsentReason: "the attack cooldown arrived in 1.9"},
			{ProtocolID: "java/26.1", Recording: "testdata/cooldown.mccap"},
		},
	}

	report, err := conform.Run(context.Background(), s, stubComparer{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	got := report.Outcome("java/1.8.9")
	if got.Status != conform.Absent {
		t.Fatalf("Status = %v, want Absent", got.Status)
	}
	if got.Detail == "" {
		t.Fatal("an absent mechanic with no recorded reason is indistinguishable " +
			"from one nobody checked")
	}
}

func TestALaneWithNeitherRecordingNorReasonIsRejected(t *testing.T) {
	t.Parallel()

	s := conform.Scenario{
		Name: "empty lane",
		Lanes: []conform.Lane{
			{ProtocolID: "java/1.8.9"},
			{ProtocolID: "java/26.1", Recording: "testdata/x.mccap"},
		},
	}

	if _, err := conform.Run(context.Background(), s, stubComparer{}); err == nil {
		t.Fatal("Run accepted a lane that neither checks anything nor says why not")
	}
}

func TestEachLaneUsesItsOwnVersionTolerance(t *testing.T) {
	t.Parallel()

	s := twoLaneScenario(t)
	report, err := conform.Run(context.Background(), s, stubComparer{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	old := report.Outcome("java/1.8.9").Tolerance
	modern := report.Outcome("java/26.1").Tolerance
	if old.Absolute == modern.Absolute {
		t.Fatalf("both lanes used Absolute = %v; each version must use its own",
			old.Absolute)
	}
}
```

- [x] **Step 2: Run it to verify it fails**

Run: `cd relay && go test ./examples/minecraft/conform/ -v`
Expected: FAIL — the package does not exist.

- [x] **Step 3: Implement `conform.go`**

The completeness check is the load-bearing part. Enumerate the registered
extractors — expose Task 1's registry through a small exported helper,
`trace.SupportedProtocols() []string` — and require a lane for each:

```go
func Run(ctx context.Context, s Scenario, compare Comparer) (Report, error) {
	seen := map[string]bool{}
	for _, lane := range s.Lanes {
		if lane.Recording == "" && lane.AbsentReason == "" {
			return Report{}, fmt.Errorf("conform: %s: lane %s has no recording and "+
				"no absent reason", s.Name, lane.ProtocolID)
		}
		seen[lane.ProtocolID] = true
	}
	for _, id := range trace.SupportedProtocols() {
		if !seen[id] {
			return Report{}, fmt.Errorf("conform: %s: no lane for %s; a mechanic "+
				"checked on one version claims nothing about the other",
				s.Name, id)
		}
	}
	// ... run each lane at its own ToleranceFor, collect Outcomes
}
```

- [x] **Step 4: Run the tests**

Run: `cd relay && go test ./examples/minecraft/conform/ -race -v`
Expected: PASS.

- [x] **Step 5: Confirm the package does not reach the simulation**

Run: `cd relay && go list -deps ./examples/minecraft/conform/ | grep minecraft-simulation`
Expected: no output. An oracle that imports the thing it verifies is not an
oracle. If this prints anything, the dependency is a defect to remove before
committing.

- [x] **Step 6: Run the repository gates**

Run: `cd relay && task lint && task test && task verify`
Expected: PASS.

- [x] **Step 7: Commit**

```bash
git add relay/examples/minecraft/conform/
git commit -m "feat(conform): two-version gate harness for the M9 mechanics

A scenario that names no lane for a registered version is an error, an
absent mechanic must say why it is absent, and each lane compares at its
own version's tolerance. M9.2 through M9.8 use this rather than each
inventing it."
```

---

### Task 6: Add 26.1.2 to M10's pinned artifact manifest

**Files:**
- Modify: `headless-minecraft/docs/superpowers/plans/2026-08-16-m10-conformance-releases.md:36-127` (Task 1)
- Modify: `testdata/conformance/manifest.json` in each runtime repository, once M10 Task 1 has created them

M10 Task 1's `Artifact` shape already carries what is needed — `Name`,
`Version`, `URL`, `SHA256`, `License`. Nothing about it changes. What changes is
that the manifest must hold a 26.1.2 entry everywhere it holds a 1.8.9 one, and
that a manifest missing one is a failure rather than a smaller matrix.

- [x] **Step 1: Add the completeness test to M10 Task 1**

Insert into Task 1's step list, as a new failing test:

```go
func TestTheManifestPinsBothGameVersions(t *testing.T) {
	t.Parallel()

	artifacts, err := conformance.Load("testdata/conformance/manifest.json")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Every artifact kind that exists for one version must exist for the
	// other. A manifest with a 1.8.9 server and no 26.1.2 server produces a
	// conformance run that looks complete and covers half the surface.
	byKind := map[string]map[string]bool{}
	for _, a := range artifacts {
		if byKind[a.Name] == nil {
			byKind[a.Name] = map[string]bool{}
		}
		byKind[a.Name][gameVersion(a.Version)] = true
	}
	for name, versions := range byKind {
		if !versions["1.8.9"] || !versions["26.1.2"] {
			t.Fatalf("%s is pinned for %v; both 1.8.9 and 26.1.2 are required",
				name, keys(versions))
		}
	}
}
```

The test uses two helpers that must be added to M10 Task 1's `Produces` block
rather than left implicit:

```go
// gameVersion returns the game build's version segment: "1.8.9" from
// "1.8.9-build-445", "26.1.2" from "26.1.2-build-74". It exists because the
// manifest pins builds and the completeness check compares versions.
func gameVersion(artifactVersion string) string

// keys returns a set's members, sorted, for error messages.
func keys(set map[string]bool) []string
```

- [x] **Step 2: Amend Task 1's `Artifact` documentation**

Add to the `Name` field comment the kinds this now implies:
`"paper"`, `"vanilla-server"`, `"vanilla-client"`, `"node-minecraft-protocol"`,
each pinned per game version. Note in the task's prose that `Version` carries
the game build (`"1.8.9-build-445"`, `"26.1.2-build-74"`) and that
`gameVersion` parses the leading segment — M4 already settled that `26.1` names
the dataset and a patch version appears only where a specific build is meant,
so the manifest is one of the places the patch version is meant.

- [x] **Step 3: Commit**

```bash
git add docs/superpowers/plans/2026-08-16-m10-conformance-releases.md
git commit -m "docs(plan): require both game versions in M10's artifact manifest

A manifest that pins a 1.8.9 server and no 26.1.2 server produces a
conformance run that looks complete and covers half the surface. Make the
asymmetry a test failure."
```

---

### Task 7: Add the 775 rows to M10's conformance matrix

**Files:**
- Modify: `headless-minecraft/docs/superpowers/plans/2026-08-16-m10-conformance-releases.md:128-201` (Task 2)

Two rows in that matrix are silently single-version. The `headless-minecraft`
row names "pinned Paper" without a version, and the `minecraft-simulation` row
names "traces from the M9.1 capture repository", which is protocol 47 only.

- [x] **Step 1: Replace the matrix table**

| Under test | Driven against | Proves |
| --- | --- | --- |
| `minecraft-protocol` | Pinned Node `minecraft-protocol`, both directions, protocol 47 | The 47 codecs agree with an independent implementation |
| `minecraft-protocol` | Pinned Node `minecraft-protocol`, both directions, protocol 775 | The 775 codecs agree with an independent implementation, or the lane records that upstream has no 775 support yet |
| `server` (`examples/vanilla`) | Pinned vanilla 1.8.9 client | A real 1.8.9 client plays against it |
| `server` (`examples/vanilla`) | Pinned vanilla 26.1.2 client | A real 26.1.2 client plays against it |
| `headless-minecraft` | `server`'s `examples/vanilla` and pinned Paper 1.8.9 | The client reaches play and observes correctly on 47 |
| `headless-minecraft` | `server`'s `examples/vanilla` and pinned Paper 26.1.2 | The client reaches play and observes correctly on 775 |
| `minecraft-simulation` | Protocol 47 traces from the capture oracle | The kernel reproduces vanilla 1.8.9 trajectories |
| `minecraft-simulation` | Protocol 775 traces from the capture oracle (M9.1b) | The kernel reproduces vanilla 26.1.2 trajectories |
| `headless-minecraft` | Pinned Paper with an open-source anti-cheat | Ordinary automation draws no alerts |

- [x] **Step 2: Record what the Node lane may not be able to do**

M4 already found that upstream Node `minecraft-protocol` had no 775 support and
that the differential suite waits on it. Add a sentence to Task 2 saying so: if
upstream still lacks 775 at M10, that lane records "no independent
implementation available" with the date checked, and the 775 codecs rest on the
live client and server lanes alone. An absent lane that says why is evidence; an
absent lane that says nothing reads as coverage.

- [x] **Step 3: Add the matrix-completeness test to Task 2**

This test reads the lanes as data, which M10 Task 2 does not currently produce —
its lanes are prose in a table. Add this to that task's `Produces` block:

```go
// Lane is one row of the conformance matrix, declared as data so a test can
// check the matrix for completeness rather than a reviewer checking a table.
type Lane struct {
    Name           string
    Versions       []string // game versions this lane covers
    VersionNeutral bool     // true when the lane is not version-specific
    Reason         string   // required when VersionNeutral, says why
}

// Lanes returns every declared lane.
func Lanes() []Lane

// Covers reports whether this lane runs against one game version.
func (l Lane) Covers(gameVersion string) bool
```

```go
func TestEveryVersionedLaneRunsOnBothVersions(t *testing.T) {
	t.Parallel()

	// The lanes are declared as data so this test can read them. A lane that
	// is version-specific must have a sibling for the other version, or
	// declare itself version-neutral with a reason.
	for _, lane := range conformance.Lanes() {
		if lane.VersionNeutral {
			if lane.Reason == "" {
				t.Errorf("%s claims to be version-neutral with no reason", lane.Name)
			}
			continue
		}
		if !lane.Covers("1.8.9") || !lane.Covers("26.1.2") {
			t.Errorf("%s covers %v; a versioned lane needs both", lane.Name, lane.Versions)
		}
	}
}
```

- [x] **Step 4: Commit**

```bash
git add docs/superpowers/plans/2026-08-16-m10-conformance-releases.md
git commit -m "docs(plan): split M10's conformance matrix by game version

Two rows were silently 47-only: the Paper lane named no version and the
simulation lane consumed M9.1 traces, which are protocol 47. Both
versions now have a row, and a versioned lane missing its sibling is a
test failure."
```

---

### Task 8: Record the milestone

**Files:**
- Modify: `headless-minecraft/MASTER_PLAN.md`

- [x] **Step 1: Update the M9 stage tables**

Mark M9.1b complete in both the milestone table row and the M9 stage table, and
record the measured relative-move scale and the derived 775 tolerance next to
the one-thirty-second note, so the next reader finds both numbers together with
their derivations.

- [x] **Step 2: Write what the work found**

Add a session-findings entry in the established shape: what was built, what it
cost that was not budgeted, and what surprised you. Candidates, if they hold:
whether the 775 relative-move scale matched 4096; whether the spawn packets
consolidated in a way that lost information the 47 extractor had; whether the
proxy's 775 login walk needed anything the 47 one did not. Write what happened,
not what the plan hoped.

- [x] **Step 3: Commit**

```bash
git add MASTER_PLAN.md
git commit -m "docs(plan): close M9.1b, and what the 775 oracle found"
```

---

## Stage summary

| Stage | Delivers | Gate |
| --- | --- | --- |
| Task 1 | Per-version extractor dispatch | The protocol 47 suite passes unmoved |
| Task 2 | The 775 extractor | 775 teleports are exact and 775 deltas use their own scale |
| Task 3 | Per-version tolerance | The two versions' numbers differ, and an unknown version has none |
| Task 4 | The live check | A real 26.1.2 client's capture replays to its own digest and traces |
| Task 5 | Two-version gate harness | A scenario missing a version's lane fails to run |
| Tasks 6–7 | M10 matrix and manifest | A versioned lane without its sibling fails |
| Task 8 | The record | The master plan says what was found |

## Definition of done

- `trace.Extract` reads protocol 47 and protocol 775, each by its own
  implementation, and refuses an unregistered version rather than misreading it.
- The 775 relative-move scale is a measured number with a recording digest in
  its doc comment, not a constant taken from prose.
- `ToleranceFor` returns a different, derived tolerance per version, and an
  error for a version with no extractor.
- A real vanilla 26.1.2 client's session, captured through the proxy against a
  pinned offline vanilla 26.1.2 server, verifies to its own digest and yields
  non-empty traces for the player, a dropped item, and an arrow. Recorded in
  `relay/docs/verification/2026-08-17-capture-oracle.md` with digests.
- `conform.Run` refuses a scenario that names no lane for a registered version,
  and reports an absent mechanic with its reason rather than skipping it.
- `relay/examples/minecraft/conform` does not depend on `minecraft-simulation`.
- M10's manifest requires both game versions, and its matrix has a row per
  version for every versioned lane.
- `task lint`, `task test` under `-race`, and `task verify` pass in `relay`.

## Risks

**The 775 spawn packets may not map one-to-one onto 47's families.** 47 has
several spawn packets — named entity, object, living, experience orb — and 775
may have consolidated them behind an entity-type field. If it has, the `Family`
mapping is a lookup against the generated entity registry rather than a switch
on packet identity, and Task 2 is larger than it looks. Read
`generated/java/v26_1/packets.go` before estimating it.

**The live check needs a real 26.1.2 client, and this project runs offline
mode.** M6.4 is postponed, so the client connects to an offline-mode server.
Anything that differs between online and offline mode is outside this oracle,
same as for the 47 lane. Nothing in M9's stages is known to depend on it; if one
turns out to, that stage needs a different instrument.

**Retrofitting the completeness check will fail loudly at first, and should.**
Task 5's `Run` refuses any scenario without a lane per registered version. The
moment the 775 extractor registers, every existing single-version scenario
starts failing. That is the point — it converts the gap this plan was written to
close into a build error — but it means Task 5 must land after Task 2, and the
scenarios that fail must be given real 775 lanes rather than
`VersionNeutral: true` to quiet them.

**M10 Task 1 and Task 2 do not exist yet.** Tasks 6 and 7 edit a plan, not code.
They are worth doing now because the matrix is where the omission would
otherwise survive to a release, and worth re-checking when M10 is actually
implemented, because the file paths in that plan are still proposals.

## What this plan deliberately does not do

**It does not write the M9.3–M9.8 stage plans.** Those stages name types in
`minecraft-simulation` that M8.3 and M8.4 have not created. The M9 plan says
this outright, and gives the reason: a plan that names types that do not exist
reads as authoritative while being invented, which is the failure this project
has already paid for once, in the shared-protocol extraction plan that named a
directory nobody could find. Write each stage's plan when its prerequisite
lands. What this plan does instead is make sure that when they are written, the
harness forces them to be two-version — which is the part that could not wait,
because a single-version habit set now would be seven stages deep before anyone
noticed.

## Follow-on

M9.2 is the first consumer of Task 5's harness and Task 3's tolerances. Its plan
should be written after M8.4 lands, and it inherits two lanes rather than one.
M8.8's 26.1.2 lane and this milestone's 775 traces verify overlapping ground
from opposite directions — M8.8 asks whether a real server accepts what the
kernel predicts, and M9.2 asks whether the kernel reproduces what a real server
did. If they ever disagree, the disagreement is more informative than either
result, and neither should be treated as settling the other.
