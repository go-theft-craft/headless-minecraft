# M9.3 Movement Scenarios Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `subagent-driven-development` or execute this plan inline one task at a time. Keep every checkbox current.

**Goal:** Prove the kernel's player movement against captured vanilla behaviour on both 1.8.9 and 26.1.2, including the three cases a fixture suite cannot reach — a server correction, a server teleport, and a disconnect in the middle of an action.

**Architecture:** M8.4 and M8.8 prove movement forwards: scripted input runs through the kernel and a real server accepts it without correcting. This stage proves it backwards: a captured vanilla session is replayed through the kernel and the trajectories must match. The two directions fail differently, which is why both exist. The comparison crosses a repository boundary, and it crosses it as a **file** — what `mcrelay trace -out` writes — not a Go import, because the oracle must not depend on the thing it verifies and `minecraft-simulation` must not depend on an examples module.

**Tech Stack:** Go 1.26.6 from `openserbia/go-flake`, Devbox, Task, `minecraft-simulation`'s `sim`, `runtime`, `movement`, and `profile/java/*`, `relay`'s capture oracle and `conform` harness, `headless-minecraft`'s client, and pinned vanilla 1.8.9 and 26.1.2 servers.

## Before executing this plan: reconcile it

**Reconciled 2026-08-18, by Task 0.** The table below is what is built today,
not what was specified. Where this plan said something that turned out to be
false, the task now says the true thing and the difference is recorded under
"What reconciliation changed" beneath the table.

| Symbol | Where it is | State |
| --- | --- | --- |
| `sim.TickInput`, `sim.TickResult`, `sim.ChangeSet`, `sim.Digest`, `sim.Profile` | `minecraft-simulation/sim` | built, names unchanged |
| `runtime.Store`, `runtime.NewRunner`, `runtime.NewMemory` | `minecraft-simulation/runtime` | built; the constructors are `NewRunner` and `NewMemory`, not bare types |
| `movement.Input`, `movement.Locomotion`, `movement.LocomotionView` | `minecraft-simulation/movement` | built, names unchanged |
| `profile/java/v1_8.New`, `profile/java/v26_1.New` | `minecraft-simulation/profile/java/*` | built, `New(*data.Set) (sim.Profile, error)` |
| `adapter.Drive`, `adapter.Source`, `adapter.Sink` | `minecraft-simulation/adapter` | built — in the simulation, not in this repository |
| `client.Do`, `ActionMove`, `ActionLook`, `ActionGround` | `headless-minecraft/client` | built |
| `predict.Loop`, `predict.Correction` | `headless-minecraft/predict` | built, and **a correction is a callback, not an event** |
| `vanilla.Start`, `vanilla.Options` | `headless-minecraft/internal/vanilla` | built; the server exposes `Lines`, `Log`, `Matching`, `Stop` |
| `trace.Trace`, `trace.Sample`, `trace.Tolerance`, `trace.ToleranceFor` | `relay/examples/minecraft/trace` | built (M9.1, M9.1b) |
| `conform.Scenario`, `conform.Lane`, `conform.Run`, `conform.Comparer`, `conform.Loader` | `relay/examples/minecraft/conform` | built (M9.1b) |
| `mctest.Captured`, `mctest.ReplayCaptured` | `minecraft-simulation/mctest` | built by M9.2, and it is the comparator this plan proposed to write |
| `trace.Document`, `trace.Schema`, `trace.WriteDocument`, `trace.ReadDocument` | — | this plan, Task 1 |
| `conformance.Compare` | — | this plan, Task 2, **and see the finding about `mctest.Captured`** |

### What reconciliation changed

Seven things this plan asserted are not true of the tree it now runs against.

- **`predict.Correction` is delivered by a callback, and `Predicted` is not on
  the client.** `predict.Options.OnCorrection func(Correction)` is how a
  correction reaches a caller, and the predicted state is
  `predict.Loop.Predicted() (entity.State, bool)`. Tasks 5 and 6 were written
  against `c.Predicted()` and a `event.DomainPlayer` correction event, and
  neither exists. The tests are rewritten against the loop. The risk section at
  the foot of this plan called this out in advance — "if `Correction` is not
  published as an event, those tasks need reworking" — and this is that
  rework.
- **The 775 teleport confirmation is already implemented.** It is in
  `internal/adapter/v26_1/handlers.go`, and it fires on every server-initiated
  position rather than only on the placing one. Task 6 is therefore a test
  against a claim, not a feature: if it passes first time, that is the correct
  outcome and not a sign the test is wrong. What is unverified is the *count* —
  exactly one confirmation per teleport — and that a real server stops
  resending.
- **The live lane is `//go:build vanilla` and `-run TestVanilla`.** `task
  test:vanilla` runs `go test ./client/ -run TestVanilla -tags vanilla`. A live
  test named anything else is a test nothing runs, and a live test without the
  tag breaks `task verify`, which must stay offline. Tasks 5 to 7 land in
  `client/vanilla_e2e_test.go`'s lane and are named `TestVanilla…`, not in the
  three new files this plan invented.
- **`vanilla.Server` has no `LogLines` and no `Kill`.** It has `Matching(sub)
  []string`, `Lines()`, `Log()`, and `Stop()`. Task 7 wanted a server killed
  under a live client; `Stop()` is a clean shutdown, so the disconnect has to
  be provoked at the connection rather than at the process.
- **`mcrelay`'s flags are not what Task 3 wrote.** It is `-listen`,
  `-upstream`, `-record <directory>` — `-capture` is a bool that records raw
  bytes, not a path — and extraction is `mcrelay trace -in <file> -out <file>`,
  with no `--format json`. The commands in Task 3 are corrected.
- **`traceDocument` is at `cmd/mcrelay/main.go:496`, not `:380`, and carries a
  `Note`.** Its fields today are `Recording`, `Protocol`, `Note`, `Traces`.
  Task 1 keeps `Note`: dropping a field to match a plan written before it
  existed would lose what the recorder had to say about the session.
- **M9.2 already built a captured-trajectory comparator, and it is not this
  one.** `mctest.Captured` and `mctest.ReplayCaptured` replay a captured
  trajectory against a profile at the version's own tolerance, out of a
  hand-built JSON form with per-sample tick offsets. Task 2 proposed a second
  comparator in a new `conformance` package reading the trace document
  directly. Two comparators for one job is the thing to avoid, so Task 2 is
  re-scoped: extend `mctest` rather than fork it, and make the trace document a
  *source* the corpus is generated from rather than a second runtime format.

### The blocker this plan did not see

**Task 3 cannot be run for 26.1.2 by anyone without a real Minecraft client,
and Task 4 depends on Task 3.**

The bodies M9.2 captured — an item, an arrow — are simulated by the server, so
a capture of them is an oracle no matter which client was connected. A player
is not. The server does not narrate a player's walking back to it; the player
trace is built from what the *client* reported, as `trace.Trace`'s own doc
comment says. So a movement corpus captured through this repository's headless
client is this repository's own physics played back to itself, and comparing
the kernel against it would pass by construction.

The oracle for a player trace is a real vanilla client behind the proxy. For
1.8.9 those recordings exist and are archived — `oracle-evidence/2026-08-17-relay-capture/fixed/20260817-132217-003.mccap`,
5304 records from a real 1.8.9 client, is the largest. For **26.1.2 there is
no such recording**: every 775 capture in `oracle-evidence` was taken with the
headless client, and the verification document says so outright.

This is a two-version stage, so Tasks 3 and 4 are blocked on a human playing
26.1.2 through the proxy. They are not startable by an agent and must not be
faked with a headless capture. Tasks 5 to 7 — the correction, the teleport, and
the disconnect, which are the master plan's stated gate for M9.3 — do not
depend on the corpus and are unblocked.

## Global Constraints

- Go 1.26.6 from `openserbia/go-flake`; Devbox and Task drive every gate.
- Every gate in this stage is a two-version gate. A scenario that runs on 1.8.9
  and not on 26.1.2 is a failure, and `conform.Run` enforces it.
- The capture oracle must not import `minecraft-simulation`. Comparison goes
  through the trace document, a file.
- `minecraft-simulation` must not import `relay`. It is a separate module whose
  oracle code lives under `examples/`, and depending on an examples module for
  a core comparison would invert the dependency the oracle constraint exists to
  protect.
- Checks run against offline mode, because M6.4 is postponed.
- Pinned servers only, with recorded digests.
- `task lint`, `task test` under `-race`, and `task verify` pass in every
  repository this plan touches, before every commit.

---

## Design decisions this plan settles

**The trace document is the contract between the repositories.** `mcrelay trace
--format json` already emits one. This stage freezes it: a schema version, a
stable field order, and a checked-in golden file on both sides. Two repositories
that agree through a file can be released independently; two that agree through
a Go interface cannot, and one of them is an examples module that was never
meant to be a dependency.

**Replay-against-capture and drive-against-server are different tests, and
neither subsumes the other.** M8.8 asks "does a real server accept what the
kernel does". This stage asks "does the kernel reproduce what a real server
did". A kernel with a wrong constant in a rule the server never validates passes
M8.8 and fails here; a kernel with a wrong *cadence* passes here and fails M8.8.
Running both is not redundancy.

**A correction is the scenario, not an error in it.** M8.8's gate is zero
corrections, which means M8.8 can never test what happens when one arrives. This
stage deliberately provokes one — by feeding the client a position the server
will reject — and checks that the client reconciles the way vanilla does rather
than that no correction happened.

**Disconnect mid-action is tested at the session boundary, not the kernel.** A
kernel has no concept of a connection. What this stage checks is that the
client's prediction loop stops cleanly, publishes what it had, and does not
apply a change set computed against a revision the server will never confirm.

## File structure

**`relay/examples/minecraft/trace/`**

- `document.go` — new. The versioned trace document: `Document`, `Schema`,
  `WriteDocument`, and the stable field order. Today's JSON is assembled inline
  in `cmd/mcrelay/main.go`; this moves it behind a type so both sides can pin it.
- `document_test.go` — new, with a golden file.
- `testdata/document-golden.json` — new.

**`minecraft-simulation/conformance/`** — new package.

- `document.go` — the reader for the trace document, and the same golden file.
- `compare.go` — `Compare`, walking a captured trace against kernel output at
  the version's tolerance.
- `scenario.go` — `Scenario`, `Load`, and the scenario corpus loader.
- `testdata/traces/` — the captured trace documents, per version.
- `testdata/document-golden.json` — byte-identical to relay's.

**`headless-minecraft/`**

- `client/action_test.go` — modify. Add the correction, teleport, and
  disconnect cases against a pinned server.
- `examples/scenario/` — new. The runnable surface CI drives.

---

## Task 0: Reconcile this plan against what is built

- [x] **Step 1: Check every symbol the table names**

Done. The table above now says where each one is and what shape it landed in,
and "What reconciliation changed" lists the seven places this plan was wrong.

- [x] **Step 2: Check the commands, not only the symbols**

A plan that names a flag nobody implemented fails the same way as one that
names a function nobody wrote. `mcrelay`'s flags and `task test:vanilla`'s
selector were both wrong here, and both would have been found at the worst
moment — with a server running and a client connected.

- [x] **Step 3: Report what is blocked rather than stubbing it**

Tasks 3 and 4 need a real 26.1.2 client and are blocked on a person. That is
written above as a blocker, not worked around: a movement corpus captured with
our own client is our own physics played back to itself.

- [x] **Step 4: Commit the reconciliation**

```bash
git add docs/superpowers/plans/2026-08-17-m9-3-movement-scenarios.md
git commit -m "docs(plan): reconcile M9.3 against what M8.8 and M9.2 landed"
```

---

## Task 1: Freeze the trace document

**Files:**
- Create: `relay/examples/minecraft/trace/document.go`
- Create: `relay/examples/minecraft/trace/document_test.go`
- Create: `relay/examples/minecraft/trace/testdata/document-golden.json`
- Modify: `relay/examples/minecraft/cmd/mcrelay/main.go` — the `traceDocument`
  type around `:380` moves into the package and gains a schema version

**Interfaces:**
- Consumes: `Trace`, `Sample`, `Tolerance`, `mccapture.Header`.
- Produces:

```go
// Schema is the trace document's format version.
//
// It is a number in the file rather than a convention in two heads because two
// repositories read this format and they release separately. A reader that
// finds a schema it does not know refuses the file; a reader that guesses
// produces trajectories nobody observed.
const Schema = 1

// Document is one recording's extracted trajectories, as written to disk.
//
// Field order in the emitted JSON is fixed, and the golden file pins it. That
// matters because the simulation repository checks in the same golden file: a
// silent reordering on this side is caught on this side, not three commits
// later in a diff nobody connects to it.
type Document struct {
    Schema     int              `json:"schema"`
    Protocol   string           `json:"protocol"`
    Recording  string           `json:"recording"`
    Digest     string           `json:"digest"`
    Tolerance  Tolerance        `json:"tolerance"`
    Traces     []Trace          `json:"traces"`
}

// NewDocument builds a document from an extraction. It sorts nothing: Extract
// already returns traces in first-appearance order, and re-sorting here would
// make a diff between two runs of one scenario stop lining up.
func NewDocument(protocolID, recording, digest string, traces []Trace) (Document, error)

// WriteDocument writes d as JSON with the fixed field order.
func WriteDocument(w io.Writer, d Document) error
```

- [ ] **Step 1: Write the failing test**

```go
package trace_test

func TestTheDocumentMatchesItsGoldenFile(t *testing.T) {
	t.Parallel()

	// The simulation repository checks in a byte-identical copy of this file
	// and reads documents against it. If this test and its twin there ever
	// disagree, one repository is writing a format the other cannot read, and
	// the failure should land here rather than in a trajectory comparison
	// three steps downstream.
	doc, err := trace.NewDocument("java/1.8.9", "walk.mccap", "sha256:abc", fixtureTraces(t))
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}

	var got bytes.Buffer
	if err := trace.WriteDocument(&got, doc); err != nil {
		t.Fatalf("WriteDocument: %v", err)
	}

	want, err := os.ReadFile(filepath.Join("testdata", "document-golden.json"))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if !bytes.Equal(got.Bytes(), want) {
		t.Fatalf("document does not match the golden file.\n got: %s\nwant: %s",
			got.Bytes(), want)
	}
}

func TestAnUnknownSchemaIsRefused(t *testing.T) {
	t.Parallel()

	_, err := trace.ReadDocument(strings.NewReader(`{"schema":99,"protocol":"java/1.8.9"}`))
	if err == nil {
		t.Fatal("ReadDocument accepted a schema it does not know; a reader that " +
			"guesses at an unknown format produces trajectories nobody observed")
	}
	if !strings.Contains(err.Error(), "99") {
		t.Fatalf("err = %v, want it to name the schema it found", err)
	}
}

func TestTheDocumentCarriesTheVersionTolerance(t *testing.T) {
	t.Parallel()

	// A consumer must not have to know which tolerance applies to which
	// protocol. The document says so, derivation included.
	doc, err := trace.NewDocument("java/26.1", "walk.mccap", "sha256:abc", fixtureTraces(t))
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	if doc.Tolerance.Absolute != 0 {
		t.Fatalf("Absolute = %v, want 0 for 775", doc.Tolerance.Absolute)
	}
	if doc.Tolerance.Why == "" {
		t.Fatal("the document carries a tolerance with no derivation")
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd relay && go test ./examples/minecraft/trace/ -run Document -v`
Expected: FAIL — `undefined: trace.NewDocument`.

- [ ] **Step 3: Move the document out of main.go and implement it**

`cmd/mcrelay/main.go:380` declares `traceDocument` with `Protocol` and the
traces. Move it into the package as `Document`, add `Schema`, `Digest`, and
`Tolerance`, and have `main.go` call `trace.WriteDocument` instead of encoding
inline. Emit with `json.Encoder` and `SetIndent("", "  ")`, and declare the
struct fields in the order the golden file has them — Go's encoder follows
declaration order, which is what makes the golden file meaningful.

`ReadDocument` checks `Schema` **before** decoding anything else, so an unknown
format fails on the version rather than on a field that happens to have moved.

- [ ] **Step 4: Generate the golden file, then read it**

Write the golden file from the implementation, then open it and check it by
eye: schema first, protocol next, one trace per entity, samples in order.
A golden file committed without being read is a golden file that pins a bug.

- [ ] **Step 5: Run the tests and the gates**

Run: `cd relay && go test ./examples/minecraft/... -race && task lint && task verify`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add relay/examples/minecraft/trace/ relay/examples/minecraft/cmd/mcrelay/main.go
git commit -m "feat(trace): freeze the trace document behind a schema version

Two repositories read this format and release separately. Put a version
in the file, pin the field order with a golden file, and refuse an
unknown schema rather than guessing at it."
```

---

## Task 2: The reader and comparator in `minecraft-simulation`

**Files:**
- Create: `minecraft-simulation/conformance/document.go`
- Create: `minecraft-simulation/conformance/document_test.go`
- Create: `minecraft-simulation/conformance/compare.go`
- Create: `minecraft-simulation/conformance/compare_test.go`
- Create: `minecraft-simulation/conformance/testdata/document-golden.json` —
  byte-identical to relay's

**Interfaces:**
- Consumes: `sim.Profile`, `runtime.Store`, `runtime.Runner`, `movement.Input`,
  `geom.Vec3`.
- Produces:

```go
// Document mirrors the capture oracle's trace document.
//
// It is declared here rather than imported because the oracle lives in an
// examples module in another repository, and because an oracle that shares a
// type with the thing it verifies can encode the same misunderstanding on both
// sides and cancel it out. The golden file is what keeps the two honest.
type Document struct {
    Schema    int       `json:"schema"`
    Protocol  string    `json:"protocol"`
    Recording string    `json:"recording"`
    Digest    string    `json:"digest"`
    Tolerance Tolerance `json:"tolerance"`
    Traces    []Trace   `json:"traces"`
}

// ReadDocument decodes a document and refuses a schema it does not know.
func ReadDocument(r io.Reader) (Document, error)

// Deviation is one sample where the kernel and the capture disagreed.
type Deviation struct {
    EntityID int32
    Sequence uint64
    Observed geom.Vec3
    Computed geom.Vec3
    Distance float64
    // Allowed is the tolerance in force at this sample, which differs between
    // absolute and relative samples on protocol 775.
    Allowed float64
}

// Comparison is what one trace comparison learned.
type Comparison struct {
    EntityID int32
    Samples  int
    // MaxDistance is reported on success as well as failure: a comparison that
    // passes at 99% of its allowance is a comparison about to start failing,
    // and that is worth seeing before it does.
    MaxDistance float64
    Deviations  []Deviation
}

// Compare replays one captured trace through the kernel and reports where the
// two disagreed beyond the document's tolerance.
func Compare(ctx context.Context, profile sim.Profile, doc Document, t Trace) (Comparison, error)
```

- [ ] **Step 1: Write the failing test**

```go
package conformance_test

func TestTheGoldenFileIsIdenticalToTheOracles(t *testing.T) {
	t.Parallel()

	// The point of this test is the byte comparison, not the parse. If the
	// oracle changes its format and this repository does not, this fails here
	// rather than as a mysterious trajectory mismatch later.
	mine, err := os.ReadFile(filepath.Join("testdata", "document-golden.json"))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	theirs, err := os.ReadFile(oracleGoldenPath(t))
	if err != nil {
		t.Skipf("the oracle repository is not checked out beside this one: %v", err)
	}
	if !bytes.Equal(mine, theirs) {
		t.Fatal("this repository's golden trace document differs from the capture " +
			"oracle's; one of them is writing a format the other cannot read")
	}
}

func TestAPerfectKernelDeviatesNowhere(t *testing.T) {
	t.Parallel()

	// The stub profile reproduces the capture exactly. This proves the
	// comparator reports agreement rather than proving anything about physics.
	doc := loadDocument(t, "testdata/traces/1_8_9-walk.json")
	got, err := conformance.Compare(context.Background(), echoProfile(t, doc), doc, doc.Traces[0])
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	if len(got.Deviations) != 0 {
		t.Fatalf("a kernel that reproduces the capture exactly reported %d deviations: %+v",
			len(got.Deviations), got.Deviations)
	}
}

func TestADeviationBeyondToleranceIsReported(t *testing.T) {
	t.Parallel()

	// One block off is far outside either version's tolerance. A comparator
	// that reports agreement here would pass every later stage in this
	// milestone while proving nothing.
	doc := loadDocument(t, "testdata/traces/1_8_9-walk.json")
	got, err := conformance.Compare(context.Background(), offsetProfile(t, doc, 1.0), doc, doc.Traces[0])
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	if len(got.Deviations) == 0 {
		t.Fatal("a kernel one block off reported no deviations")
	}
	if got.MaxDistance < 1.0 {
		t.Fatalf("MaxDistance = %v, want at least 1.0", got.MaxDistance)
	}
}

func TestAnAbsoluteSampleOn775HasNoAllowance(t *testing.T) {
	t.Parallel()

	// This is the whole reason tolerance is two numbers. On 775 the server
	// sends absolute positions as float64, so a kernel that is off by a
	// thousandth of a block at a teleport is wrong and must be reported.
	doc := loadDocument(t, "testdata/traces/26_1_2-walk.json")
	got, err := conformance.Compare(context.Background(), offsetProfile(t, doc, 0.001), doc, doc.Traces[0])
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	if len(got.Deviations) == 0 {
		t.Fatal("a kernel a thousandth of a block off passed at an absolute 775 " +
			"sample; the 1.8 tolerance has leaked into the modern lane")
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd minecraft-simulation && devbox run -- go test ./conformance/ -v`
Expected: FAIL — the package does not exist.

- [ ] **Step 3: Implement `document.go` and `compare.go`**

`Compare` drives a `runtime.Runner` over the profile, feeding one
`movement.Input` per tick reconstructed from the trace's serverbound samples,
and lines the results up against the trace by `Elapsed` rather than by index —
a capture has no ticks in it, as `trace.Sample`'s own doc comment says, and
dividing elapsed time by fifty milliseconds would be a guess dressed as a
measurement.

The allowance per sample:

```go
// allowanceFor picks the tolerance in force at one sample.
//
// A sample the server stated outright gets Absolute; a sample reached by
// accumulating relative moves gets Relative times the number of moves since
// the last absolute. On protocol 47 the two are equal and this reduces to one
// number; on 775 they differ by everything.
func allowanceFor(tol Tolerance, sinceAbsolute int) float64 {
	if sinceAbsolute == 0 {
		return tol.Absolute
	}
	return tol.Absolute + tol.Relative*float64(sinceAbsolute)
}
```

- [ ] **Step 4: Run the tests**

Run: `cd minecraft-simulation && devbox run -- go test ./conformance/ -race -v`
Expected: PASS.

- [ ] **Step 5: Confirm the module boundary**

Run: `cd minecraft-simulation && go list -deps ./conformance/ | grep -c relay`
Expected: `0`. The comparison goes through a file. If this is not zero, the
dependency inversion this task exists to avoid has happened anyway.

- [ ] **Step 6: Commit**

```bash
cd minecraft-simulation
git add conformance/
git commit -m "feat(conformance): compare kernel trajectories against captured traces

The document is read from a file rather than imported, so the oracle
stays independent of the thing it verifies and the two repositories
release separately. A shared golden file keeps the formats honest."
```

---

## Task 3: Capture the movement corpus on both versions

This is a manual capture task with a written record, in the shape of M9.1's and
M9.1b's live checks. It produces the fixtures every remaining task compares
against, so it comes before them.

**Files:**
- Create: `minecraft-simulation/conformance/testdata/traces/*.json`
- Modify: `relay/docs/verification/2026-08-17-capture-oracle.md`

- [ ] **Step 1: Script the nine scenarios**

Walk, sprint, sneak, jump, fall, collide, correction, teleport, and disconnect
mid-action. Write the exact input sequence for each into the verification
document first, so the capture is reproducible and so a later re-capture
produces something comparable rather than something similar.

- [ ] **Step 2: Capture each on 1.8.9**

```bash
cd relay && go run ./examples/minecraft/cmd/mcrelay \
  -protocol java/1.8.9 \
  --listen 127.0.0.1:25565 --upstream 127.0.0.1:25566 \
  --capture ./recordings/1_8_9-walk.mccap
```

`-protocol java/1.8.9` is easy to miss and the default is 775, so a 47 session
recorded under a 775 header will not replay. Verify every recording before
moving on:

```bash
cd relay && go run ./examples/minecraft/cmd/mcrelay verify ./recordings/1_8_9-walk.mccap
```

- [ ] **Step 3: Capture each on 26.1.2**

Same nine, against the pinned 26.1.2 server, with `-protocol` omitted.

- [ ] **Step 4: Convert to trace documents and check them in**

```bash
cd relay && for r in ./recordings/*.mccap; do
  go run ./examples/minecraft/cmd/mcrelay trace --format json "$r" \
    > "../minecraft-simulation/conformance/testdata/traces/$(basename "${r%.mccap}").json"
done
```

Open two of them and read them. A fixture corpus committed unread is a corpus
that pins whatever the capture happened to do, including its mistakes.

- [ ] **Step 5: Record what the capture found**

Add a `## Movement corpus` section to the verification document: the eighteen
recordings, their digests, the server builds, and — the part that matters —
anything that differed between the two versions that the plan did not predict.

- [ ] **Step 6: Commit**

```bash
git add minecraft-simulation/conformance/testdata/traces/ relay/docs/verification/
git commit -m "test(conformance): capture the movement corpus on both versions

Nine scenarios each on pinned 1.8.9 and 26.1.2, verified to their own
digests before conversion. The scripts are in the verification document
so a re-capture produces something comparable rather than similar."
```

---

## Task 4: The six ordinary scenarios

Walk, sprint, sneak, jump, fall, and collide. These overlap M8.4's fixture suite
on purpose: M8.4 checks the kernel against fixtures generated from the game's
own movement tick, and this checks it against what a server actually sent over a
wire. A disagreement between them is more informative than either result.

**Files:**
- Create: `minecraft-simulation/conformance/movement_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestTheOrdinaryMovementScenariosMatchVanilla(t *testing.T) {
	t.Parallel()

	for _, scenario := range []string{"walk", "sprint", "sneak", "jump", "fall", "collide"} {
		for _, version := range []struct {
			id      string
			profile func(*testing.T) sim.Profile
		}{
			{"1_8_9", v1_8Profile},
			{"26_1_2", v26_1Profile},
		} {
			t.Run(scenario+"/"+version.id, func(t *testing.T) {
				t.Parallel()

				doc := loadDocument(t, filepath.Join("testdata", "traces",
					version.id+"-"+scenario+".json"))
				player := playerTrace(t, doc)

				got, err := conformance.Compare(context.Background(),
					version.profile(t), doc, player)
				if err != nil {
					t.Fatalf("Compare: %v", err)
				}
				if len(got.Deviations) != 0 {
					t.Fatalf("%d deviations, first at sequence %d: observed %v, "+
						"computed %v, %.6f blocks apart, allowed %.6f",
						len(got.Deviations), got.Deviations[0].Sequence,
						got.Deviations[0].Observed, got.Deviations[0].Computed,
						got.Deviations[0].Distance, got.Deviations[0].Allowed)
				}
				t.Logf("%s/%s: %d samples, max distance %.6f of %.6f allowed",
					scenario, version.id, got.Samples, got.MaxDistance,
					doc.Tolerance.Relative)
			})
		}
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd minecraft-simulation && devbox run -- go test ./conformance/ -run Ordinary -v`
Expected: FAIL. Which way it fails is itself information — record it. A failure
on 1.8.9 contradicts M8.4's fixture suite and means one of the two oracles is
wrong; a failure on 26.1.2 alone is likelier a wrong constant in the v26_1
profile, whose gate M8.7 already recorded as the weaker one.

- [ ] **Step 3: Fix what the failures name**

Do not widen a tolerance to make a test pass. The tolerances are derived from
wire encodings, and a kernel that needs a looser one is a kernel that is wrong.
If a deviation is real, it is a defect in `movement` or in a profile's
constants, and the fix belongs there.

- [ ] **Step 4: Run the full suite and the gates**

Run: `cd minecraft-simulation && devbox run -- task verify`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add conformance/movement_test.go
git commit -m "test(conformance): walk, sprint, sneak, jump, fall, and collide

Checked against captured vanilla traces on both versions, which is the
opposite direction from M8.4's fixtures and M8.8's live gate. All three
can disagree, and a disagreement is the informative outcome."
```

---

## Task 5: The correction scenario

**Rewritten by Task 0.** The plan's tests were written against `c.Predicted()`
and a correction event, neither of which exists. Corrections are a callback on
`predict.Options`, and the predicted body is on the loop.

**Files:**
- Create: `headless-minecraft/predict/correction_test.go`
- Create: `headless-minecraft/client/vanilla_scenario_test.go`
- Modify: `headless-minecraft/predict/predict.go`

- [x] **Step 1: Write the failing test**

Two, in two places. Offline, in `predict`: a callback that asks the loop what it
now believes, which is the first thing any caller does with a correction. Live,
in the vanilla lane: a position no player could have walked to, sent out of
band, and the server's answer to it.

- [x] **Step 2: Run it to verify it fails**

It deadlocked, which is a sharper answer than a failed assertion. `OnCorrection`
was called from `reconcile`, which runs under the loop's own mutex, so
`Predicted()` inside it blocked forever on a lock its own goroutine held.

- [x] **Step 3: Implement the reconciliation**

`reconcile` now reports the correction instead of publishing it, and `step`
publishes after releasing the lock and before simulating the tick that follows.
That is the ordering the plan asked for, made testable: at the moment the
callback runs, the adoption has happened and the next tick has not, so
`Predicted()` answers with the position the correction names.

- [x] **Step 4: Run the tests and gates**

- [x] **Step 5: Commit**

### What this scenario can and cannot prove

Recorded because it changes what the test asserts, and because the plan assumed
otherwise.

A server that refuses a move puts the player back at the last position it
accepted — and that position is the one the client itself reported on its
previous tick. So `From` equals `To`, exactly, and nothing is adopted because
nothing disagreed. The first run made this visible twice over: with the client
standing still, the server corrected it to where it already was, the snapshot
did not change, and the loop saw no correction at all. Walking first is what
makes a rejection observable, and it is the same reason M8.8 counts corrections
from the wire rather than from the loop.

What survives, and is asserted: the refused move never enters the prediction,
one refusal produces one answer, and the client ends where the server says it
is. The claim that a correction is *adopted* needs a server-chosen destination,
and that is Task 6.

---

## Task 6: The teleport scenario

**Rewritten by Task 0.** The 775 confirmation already exists — it is in
`internal/adapter/v26_1/handlers.go` and fires on every server-initiated
position. This task is a test against that claim, not a feature.

**Files:**
- Modify: `headless-minecraft/client/vanilla_scenario_test.go`
- Modify: `headless-minecraft/internal/vanilla/server.go`

- [x] **Step 1: Give the test server a console**

A teleport is something the server does of its own accord, so a client-driven
test cannot reach it. `vanilla.Server.Console` runs a command the way an
operator does.

Writing it found a defect in the shutdown path: `Stop` asked for the process's
stdin pipe *after* the process had started, which cannot work, so the "stop"
command was never sent and every server in the suite was killed twenty seconds
later by the timeout the comment describes as the fallback. The pipe is now
opened before the process starts and kept, which is what both `Console` and
`Stop` needed.

- [x] **Step 2: Write the failing test**

- [x] **Step 3: Run it**

- [x] **Step 4: Run the tests and gates**

- [x] **Step 5: Commit**

### What execution changed

**The confirmation count is measured from the teleport, not from the session.**
The first run reported two confirmations for one teleport, and both were
correct: 775's placing position during login carries a teleport identifier of
its own and is confirmed like any other. Counting over the whole session counts
the login's. The test now discards everything before the teleport, which is what
lets it assert exactly one rather than at least one — and "exactly" is the
assertion worth having, since none and two are opposite failures.

---

## Task 7: Disconnect mid-action

**Rewritten by Task 0.** `vanilla.Server` had no `Kill`, and `Wait` does not
return an error for a session that ended.

**Files:**
- Modify: `headless-minecraft/client/vanilla_scenario_test.go`
- Modify: `headless-minecraft/client/close.go`, `client/loop.go`, `client/client.go`
- Modify: `headless-minecraft/client/internal/fixture/server.go`
- Modify: `headless-minecraft/client/connect_test.go`

- [x] **Step 1: Write the failing test**

- [x] **Step 2: Run it to verify it fails**

It failed on both halves, and only one of them was a defect.

- [x] **Step 3: Implement**

**The defect: a server that dies without saying so published nothing at all.**
`publishDisconnect` reported a transport loss only when the read loop stopped
with an error. A killed server sends no disconnect packet and resets nothing —
the operating system closes the socket, the client reads EOF, and the loop stops
with no error. So the one ending a subscriber has no other way to learn about
was the one ending that was never reported. `event.DisconnectByTransport` is
documented as "a connection loss with no disconnect packet", which is exactly
this case.

The fix reports on what has already been said rather than on whether there was
an error: the loop records that the server gave its own reason, and the
transport report at the end publishes only when nothing did. A kick still
reports once.

**Not a defect: `Wait` returning nil.** Its contract is that a connection ending
is not this client's failure, so it reports nil however the session ended. The
plan assumed an error. What the test asserts instead is that `Wait` returns at
all — a caller blocked forever on a server that is gone has no way to find out
that it is gone.

The offline lane needed a fixture for this, because the one that existed sets a
zero linger and sends a reset, which gives the client an error to report from.
`ThenHangUp` closes gracefully instead, which is what a killed server leaves
behind. Without it the defect is invisible to `task verify`, and the 26.1.2
server happens to reset rather than hang up — so on that lane alone the test
would have passed over the bug.

- [x] **Step 4: Run the tests and gates**

- [x] **Step 5: Commit**

---

## Task 8: Wire the stage into the gate harness and record it

**Files:**
- Create: `minecraft-simulation/conformance/scenario.go`
- Modify: `headless-minecraft/MASTER_PLAN.md`
- Modify: `headless-minecraft/docs/superpowers/plans/2026-08-16-m9-gameplay-mechanics.md`

- [ ] **Step 1: Declare the nine scenarios as `conform.Scenario` values**

Each with a lane per version. `conform.Run` refuses a scenario missing a lane,
so this is where the two-version gate becomes enforceable rather than a
convention.

- [ ] **Step 2: Add `task test:conformance` and put it in CI**

The corpus is checked in and the comparison needs no server, so this runs in
ordinary CI. The live-server tests from Tasks 5 to 7 need a pinned jar; gate
them behind the same build tag M8.8's vanilla lane uses.

- [ ] **Step 3: Record the milestone**

Mark M9.3 complete in both stage tables. Write what the work found, in the
established shape — what was built, what cost more than budgeted, and what
surprised you. If the captured traces and M8.4's fixtures disagreed anywhere,
that is the most valuable thing this stage produced and it belongs in the
master plan, not only in a commit message.

- [ ] **Step 4: Commit**

```bash
git add MASTER_PLAN.md docs/superpowers/plans/
git commit -m "docs(plan): close M9.3, and what the movement corpus found"
```

---

## Definition of done

- The trace document has a schema version, a golden file, and byte-identical
  copies in both repositories, with a test on each side that fails if they drift.
- `minecraft-simulation/conformance` compares kernel trajectories against
  captured traces at the version's own tolerance, and does not depend on `relay`.
- Walk, sprint, sneak, jump, fall, and collide match captured vanilla on both
  1.8.9 and 26.1.2, with the max deviation logged even on success.
- A provoked correction is adopted absolutely, published before the next
  prediction, and does not produce a second correction.
- A 775 teleport is confirmed exactly once; a 47 teleport is settled without a
  confirmation packet; queued pre-teleport input is not replayed after it.
- A disconnect mid-action applies no unconfirmed change set and publishes
  everything already observed.
- All nine scenarios are declared as `conform.Scenario` values with a lane per
  version.
- `task lint`, `task test` under `-race`, and `task verify` pass in
  `minecraft-simulation`, `headless-minecraft`, and `relay`.

## Risks

**M8.4's fixtures and this stage's captures may disagree.** M8.2 already
measured that hand-written fixtures and the game disagreed in two of six cases,
and M8.4's plan inverted its verification order because of it. If the same
happens here, resist the urge to pick a winner by which suite is older. The
capture is what a server sent; the fixture is what a generator produced from the
game's own tick. Both are evidence, and the disagreement localises the defect
better than either alone.

**The correction test provokes anti-cheat behaviour on purpose.** "Moved too
quickly" is exactly what M10's anti-cheat lane wants to see *absent*. These two
lanes want opposite things from the same server, so they must not share a
fixture server or a log assertion.

**Nine scenarios times two versions is eighteen manual captures.** Task 3 is the
longest task here and it is entirely manual. Budget it as such, and write the
input scripts before capturing rather than reconstructing them afterwards from
what the recordings appear to show.

**`predict.Loop` is M8.8 Task 4 and does not exist.** Tasks 5 through 7 are
written against its specified shape. If it landed differently — in particular if
`Correction` is not published as an event — those tasks need reworking before
they can be executed, not while.
