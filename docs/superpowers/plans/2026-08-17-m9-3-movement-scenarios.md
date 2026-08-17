# M9.3 Movement Scenarios Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `subagent-driven-development` or execute this plan inline one task at a time. Keep every checkbox current.

**Goal:** Prove the kernel's player movement against captured vanilla behaviour on both 1.8.9 and 26.1.2, including the three cases a fixture suite cannot reach — a server correction, a server teleport, and a disconnect in the middle of an action.

**Architecture:** M8.4 and M8.8 prove movement forwards: scripted input runs through the kernel and a real server accepts it without correcting. This stage proves it backwards: a captured vanilla session is replayed through the kernel and the trajectories must match. The two directions fail differently, which is why both exist. The comparison crosses a repository boundary, and it crosses it as a **file** — `mcrelay trace --format json` — not a Go import, because the oracle must not depend on the thing it verifies and `minecraft-simulation` must not depend on an examples module.

**Tech Stack:** Go 1.26.6 from `openserbia/go-flake`, Devbox, Task, `minecraft-simulation`'s `sim`, `runtime`, `movement`, and `profile/java/*`, `relay`'s capture oracle and `conform` harness, `headless-minecraft`'s client, and pinned vanilla 1.8.9 and 26.1.2 servers.

## Before executing this plan: reconcile it

Every `minecraft-simulation` and `headless-minecraft` symbol this plan names is
**specified but not yet built**. Each is cited to the plan that specifies it:

| Symbol | Specified in |
| --- | --- |
| `sim.TickInput`, `sim.TickResult`, `sim.ChangeSet`, `sim.Digest`, `sim.Profile` | M8.3 plan, Tasks 5, 8, 9 |
| `runtime.Store`, `runtime.Runner`, `runtime.Memory` | M8.3 plan, Tasks 10, 11 |
| `movement.Input`, `movement.Locomotion`, `movement.LocomotionView` | M8.4 plan, Task 1 |
| `profile/java/v1_8.New`, `profile/java/v26_1.New` | M8.4 Task 7, M8.7 Task 5 |
| `client.Do`, `client.Action`, `ActionMove`, `ActionLook`, `ActionGround` | M8.8 plan, Task 1 |
| `adapter.Drive`, `adapter.Source`, `adapter.Sink` | M8.8 plan, Task 2 |
| `predict.Loop`, `predict.Correction` | M8.8 plan, Task 4 |
| `vanilla.Start`, `vanilla.Options` | M8.8 plan, Task 5 |
| `trace.Trace`, `trace.Sample`, `trace.ToleranceFor`, `conform.Scenario`, `conform.Run` | M9.1 (built), M9.1b plan |

**Task 0, before anything else:** read what actually landed and correct the
names, signatures, and file paths in this plan to match. Where a symbol landed
with a different shape, change this plan; where it did not land at all, that is
a blocker to report, not a thing to stub. This project has already paid once for
a plan that named a directory nobody could find, and a plan that names a
function nobody wrote fails the same way.

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

M8.8's gate is zero corrections, so M8.8 cannot test what a correction does.
This can.

**Files:**
- Create: `headless-minecraft/client/correction_test.go`
- Modify: `headless-minecraft/examples/scenario/main.go`

**Interfaces:**
- Consumes: `predict.Loop`, `predict.Correction`, `client.Do`, `vanilla.Start`.

- [ ] **Step 1: Write the failing test**

```go
func TestACorrectionReconcilesRatherThanAccumulates(t *testing.T) {
	t.Parallel()

	for _, version := range vanillaVersions(t) {
		t.Run(version.Name, func(t *testing.T) {
			server := vanilla.Start(t, version.Options)
			c := connected(t, server)

			corrections := subscribe(t, c, event.DomainPlayer)

			// Move somewhere the server will refuse: through a wall, fast
			// enough to trip "moved too quickly". The server answers with an
			// absolute position, and the client must adopt it rather than
			// treating it as one more delta on top of where it thought it was.
			if err := c.Do(t.Context(), client.ActionMove{X: 10_000, Y: 64, Z: 0}); err != nil {
				t.Fatalf("Do: %v", err)
			}

			correction := awaitCorrection(t, corrections)
			settled := awaitSettled(t, c)

			if distance(settled.Position, correction.To) > 1e-9 {
				t.Fatalf("after a correction to %v the client settled at %v; a "+
					"correction is absolute and adopting it as a delta doubles "+
					"every future one", correction.To, settled.Position)
			}
			if got := server.LogLines("moved wrongly"); len(got) > 1 {
				t.Fatalf("the server corrected %d times; one correction that is "+
					"not adopted becomes a correction loop", len(got))
			}
		})
	}
}

func TestACorrectionIsPublishedBeforeTheNextTickIsPredicted(t *testing.T) {
	t.Parallel()

	// Ordering matters: a caller that reads Predicted() after a correction
	// event must see the corrected position, not the one the correction
	// replaced. Publishing after the next prediction would make the event
	// describe a world that no longer exists.
	server := vanilla.Start(t, defaultOptions(t))
	c := connected(t, server)
	corrections := subscribe(t, c, event.DomainPlayer)

	if err := c.Do(t.Context(), client.ActionMove{X: 10_000, Y: 64, Z: 0}); err != nil {
		t.Fatalf("Do: %v", err)
	}

	correction := awaitCorrection(t, corrections)
	if distance(c.Predicted().Position, correction.To) > 1e-9 {
		t.Fatalf("Predicted() = %v at the moment the correction named %v",
			c.Predicted().Position, correction.To)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd headless-minecraft && devbox run -- go test ./client/ -run Correction -v`
Expected: FAIL.

- [ ] **Step 3: Implement the reconciliation**

A correction replaces the predicted position and clears accumulated motion; it
does not add to it. Where the protocols differ — 775's position packet carries
per-axis relativity flags and a teleport ID that must be confirmed, 47's carries
a bitmask and no confirmation — the difference lives in the version adapter, not
in the loop. A loop that branched on protocol would make every later mechanic
branch too.

- [ ] **Step 4: Run the tests and gates**

Run: `cd headless-minecraft && devbox run -- task verify`
Expected: PASS on both versions.

- [ ] **Step 5: Commit**

```bash
git add client/correction_test.go client/
git commit -m "feat(client): adopt a server correction rather than accumulating it

M8.8's gate is zero corrections, so it can never test this path. Provoke
one deliberately and require the client to settle where the server said,
on both protocols."
```

---

## Task 6: The teleport scenario

A teleport differs from a correction in intent and, on 775, in mechanism: it
carries a teleport ID the client must confirm, and a server that never receives
the confirmation will keep resending.

**Files:**
- Create: `headless-minecraft/client/teleport_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestATeleportIsConfirmedOnceOn775(t *testing.T) {
	t.Parallel()

	// Protocol 775 pairs a position packet with a teleport ID and expects a
	// serverbound confirmation. A client that does not send it looks stuck to
	// the server, which resends; a client that sends it twice desynchronises
	// the ID sequence. Once, with the ID the server named.
	server := vanilla.Start(t, options775(t))
	c := connected(t, server)

	teleportPlayer(t, server, geom.Vec3{X: 100, Y: 70, Z: 100})

	confirms := awaitConfirmations(t, c)
	if len(confirms) != 1 {
		t.Fatalf("sent %d teleport confirmations, want exactly 1", len(confirms))
	}
	if got := server.LogLines("moved wrongly"); len(got) != 0 {
		t.Fatalf("the server complained after a teleport: %v", got)
	}
}

func TestATeleportOn47NeedsNoConfirmation(t *testing.T) {
	t.Parallel()

	// The 1.8.9 counterpart, stated rather than skipped. Protocol 47's
	// position packet carries no teleport ID; the client answers with its own
	// position instead. A shared implementation that sent a confirmation here
	// would be sending a packet that does not exist.
	server := vanilla.Start(t, options47(t))
	c := connected(t, server)

	teleportPlayer(t, server, geom.Vec3{X: 100, Y: 70, Z: 100})

	settled := awaitSettled(t, c)
	if distance(settled.Position, geom.Vec3{X: 100, Y: 70, Z: 100}) > 1.0/32 {
		t.Fatalf("settled at %v after a teleport to (100,70,100)", settled.Position)
	}
}

func TestATeleportDoesNotReplayQueuedInput(t *testing.T) {
	t.Parallel()

	// Input queued before a teleport describes a world that no longer exists.
	// Applying it afterwards walks the player off the destination, which reads
	// as a physics bug and is not one.
	server := vanilla.Start(t, defaultOptions(t))
	c := connected(t, server)

	queueInput(t, c, forwardFor(20))
	teleportPlayer(t, server, geom.Vec3{X: 100, Y: 70, Z: 100})

	settled := awaitSettled(t, c)
	if distance(settled.Position, geom.Vec3{X: 100, Y: 70, Z: 100}) > 1.0 {
		t.Fatalf("settled at %v; queued pre-teleport input was replayed after it",
			settled.Position)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd headless-minecraft && devbox run -- go test ./client/ -run Teleport -v`
Expected: FAIL.

- [ ] **Step 3: Implement**

The confirmation is version-owned and belongs in the 775 adapter. The
input-queue flush is version-neutral and belongs in the prediction loop.

- [ ] **Step 4: Run the tests and gates**

Run: `cd headless-minecraft && devbox run -- task verify`

- [ ] **Step 5: Commit**

```bash
git add client/teleport_test.go client/ internal/adapter/
git commit -m "feat(client): confirm a 775 teleport once and flush queued input

47 has no teleport ID and needs no confirmation; sending one would be
sending a packet that does not exist. The confirmation is version-owned;
the queue flush is not."
```

---

## Task 7: Disconnect mid-action

**Files:**
- Create: `headless-minecraft/client/disconnect_action_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestADisconnectMidActionAppliesNothingUnconfirmed(t *testing.T) {
	t.Parallel()

	// A change set is computed against a revision. If the connection dies
	// before the server confirms that revision, applying the change set writes
	// a world state the server never agreed to, and a reconnecting caller
	// starts from a fiction.
	for _, version := range vanillaVersions(t) {
		t.Run(version.Name, func(t *testing.T) {
			server := vanilla.Start(t, version.Options)
			c := connected(t, server)

			before := c.World().Player.Position
			queueInput(t, c, forwardFor(20))
			server.Kill()

			if err := c.Wait(); err == nil {
				t.Fatal("Wait returned nil after the server died")
			}

			after := c.World().Player.Position
			if distance(before, after) > 1.0 {
				t.Fatalf("the world moved from %v to %v after the connection died; "+
					"unconfirmed prediction was applied", before, after)
			}
		})
	}
}

func TestADisconnectMidActionPublishesWhatItHad(t *testing.T) {
	t.Parallel()

	// The opposite failure: dropping events the client already observed.
	// A caller reading the subscription must see everything up to the
	// disconnect, then the disconnect, then nothing.
	server := vanilla.Start(t, defaultOptions(t))
	c := connected(t, server)
	events := subscribe(t, c, event.DomainSession|event.DomainPlayer)

	queueInput(t, c, forwardFor(20))
	server.Kill()
	_ = c.Wait()

	names := drainNames(t, events)
	if !slices.Contains(names, event.NameSessionDisconnected) {
		t.Fatalf("events = %v, want a session.disconnected", names)
	}
	if last := names[len(names)-1]; last != event.NameSessionClosed &&
		last != event.NameSessionDisconnected {
		t.Fatalf("last event = %q, want the session's end; something published "+
			"after the connection died", last)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd headless-minecraft && devbox run -- go test ./client/ -run DisconnectMidAction -v`
Expected: FAIL.

- [ ] **Step 3: Implement**

The prediction loop stops on loop failure, discards its uncommitted change set,
and lets the existing close path publish. `Client.Close` and `loopFinished`
already own the publish ordering; do not add a second path.

- [ ] **Step 4: Run the tests and gates**

Run: `cd headless-minecraft && devbox run -- task verify`

- [ ] **Step 5: Commit**

```bash
git add client/disconnect_action_test.go client/
git commit -m "feat(client): discard unconfirmed prediction on disconnect

A change set computed against a revision the server never confirmed
must not be applied. Everything already observed still publishes."
```

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
