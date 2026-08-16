# M10 Conformance and Releases Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `subagent-driven-development` or execute this plan inline one task at a time. Keep every checkbox current.

**Goal:** Prove every runtime repository against real implementations, fix its public API with compatibility tests, and publish `v1.0.0` releases that a consumer can depend on.

**Architecture:** Conformance is a matrix, not a suite: each repository is driven against pinned external implementations — Node `minecraft-protocol`, Paper, the owned Go server, and real vanilla clients — and the results gate the release. Compatibility tests freeze the public API surface so a later change either stays source-compatible or announces itself. Releases are last, and a release gate that has not run is a release that does not happen.

**Tech Stack:** Go 1.26.6 from `openserbia/go-flake`, Devbox, Task, pinned Node `minecraft-protocol`, pinned Paper builds, pinned vanilla clients, `apidiff`, and the existing interoperability lanes.

## Global Constraints

- Work across `minecraft-protocol`, `headless-minecraft`, `minecraft-simulation`,
  `server`, and the capture repository from M9.1.
- Run project commands as `devbox run -- task <name>`.
- Leave changes uncommitted unless explicitly requested.
- **Pin every external artifact by URL and SHA-256.** A conformance run against
  "latest Paper" is not reproducible and its failures cannot be bisected.
- **Keep Java reference artifacts and decompiled sources local.** Publish only
  provenance, independent behaviour descriptions, fixtures, and expectations.
  This is the clean-room boundary M8.1 already operates under and M10 does not
  relax it.
- Check the licence of any community-server test case before converting it, and
  record the check. A case whose licence does not permit reuse is reimplemented
  from observed behaviour or dropped.
- Do not detect anti-cheat plugins, tune against their thresholds, add timing
  jitter, or weaken a test client based on plugin detection. Capture alerts as
  failures and fix the protocol ordering, the observed state, or the mechanics.
- Every repository here is public and its releases are permanent: Go's module
  mirror serves immutable snapshots from `proxy.golang.org` and `sum.golang.org`
  records their hashes in an append-only log. A tag cannot be retracted.
- Do not name the legacy proxy's project, protocol, or codename anywhere.

---

### Task 1: The pinned artifact manifest

**Files:**
- Create: `testdata/conformance/manifest.json` in each runtime repository
- Create: `internal/conformance/manifest.go`
- Create: `internal/conformance/manifest_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:

```go
// Artifact is one external implementation a conformance lane runs against.
type Artifact struct {
    Name    string `json:"name"`    // "paper", "node-minecraft-protocol", "vanilla-client"
    Version string `json:"version"` // "1.8.9-build-445"
    URL     string `json:"url"`
    SHA256  string `json:"sha256"`
    License string `json:"license"` // SPDX identifier, or "proprietary"
}

// Manifest is every artifact one repository's conformance lanes need.
func Load(path string) ([]Artifact, error)

// Fetch downloads an artifact to a cache directory and verifies its digest.
// It never returns a file whose digest did not match.
func Fetch(ctx context.Context, a Artifact, cacheDir string) (string, error)
```

- [ ] **Step 1: Write the failing test**

```go
func TestFetchRefusesAMismatchedDigest(t *testing.T) {
    t.Parallel()

    // The whole point of pinning. A conformance run against an artifact that
    // silently changed is worse than no conformance run: it reports green for
    // something nobody tested.
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
        _, _ = w.Write([]byte("not the artifact you pinned"))
    }))
    defer server.Close()

    _, err := conformance.Fetch(t.Context(), conformance.Artifact{
        Name:   "paper",
        URL:    server.URL,
        SHA256: strings.Repeat("0", 64),
    }, t.TempDir())

    if !errors.Is(err, conformance.ErrDigestMismatch) {
        t.Fatalf("got %v, want ErrDigestMismatch", err)
    }
}

func TestEveryManifestEntryDeclaresALicense(t *testing.T) {
    t.Parallel()

    artifacts, err := conformance.Load("../../testdata/conformance/manifest.json")
    if err != nil {
        t.Fatalf("Load: %v", err)
    }
    for _, a := range artifacts {
        if a.License == "" {
            t.Errorf("%s declares no license", a.Name)
        }
    }
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `devbox run -- task test -- ./internal/conformance`
Expected: FAIL, `undefined: conformance.Fetch`.

- [ ] **Step 3: Implement the manifest**

Stream the download into a hash and a temporary file at once, and rename into
the cache only after the digest matches. A partially written artifact must never
appear at its final path, because the next run would use it.

- [ ] **Step 4: Run the tests**

Run: `devbox run -- task test -- ./internal/conformance`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/conformance testdata/conformance
git commit -m "feat(conformance): pin external artifacts by digest"
```

### Task 2: The compatibility matrix lanes

**Files:**
- Create: `internal/conformance/matrix_test.go` in each runtime repository
- Modify: `Taskfile.yml`
- Modify: `.github/workflows/ci.yml`

**Interfaces:**
- Consumes: `conformance.Load`, `conformance.Fetch`.
- Produces: `task test:conformance`, running every pairing below.

| Under test | Driven against | Proves |
| --- | --- | --- |
| `minecraft-protocol` | Pinned Node `minecraft-protocol`, both directions | The codecs agree with an independent implementation |
| `server` (`examples/vanilla`) | Pinned vanilla 1.8.9 and 26.1.2 clients | Real clients play against it |
| `headless-minecraft` | `server`'s `examples/vanilla`, and pinned Paper | The client reaches play and observes correctly |
| `minecraft-simulation` | Traces from the M9.1 capture repository | The kernel reproduces vanilla trajectories |
| `headless-minecraft` | Pinned Paper with an open-source anti-cheat | Ordinary automation draws no alerts |

- [ ] **Step 1: Write the failing test**

```go
func TestTheHeadlessClientReachesPlayAgainstPaper(t *testing.T) {
    if testing.Short() {
        t.Skip("conformance lane")
    }

    paper := conformance.MustFetch(t, "paper")
    instance := conformance.StartPaper(t, paper, conformance.Offline)
    defer instance.Stop()

    bot := mustConnect(t, instance.Addr())
    defer func() { _ = bot.Close() }()

    if got := bot.Snapshot().Revision; got == 0 {
        t.Error("the client reached play without observing a single revision")
    }
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `devbox run -- task test:conformance`
Expected: FAIL, the lane does not exist.

- [ ] **Step 3: Implement the lanes**

Each lane fetches its pinned artifact, starts it on an ephemeral port, drives
the repository under test, and stops it. Lanes are `testing.Short`-skippable so
the ordinary `task test` stays fast, and CI runs them as a separate job.

- [ ] **Step 4: Add the anti-cheat lane**

Run ordinary movement, collision stops, corrections, inventory synchronisation,
digging, and placement against Paper with a pinned open-source anti-cheat.
Capture plugin alerts as test failures.

An alert means the protocol ordering, the observed state, or the mechanics are
wrong, and the fix goes there. Do not read the plugin's thresholds, do not tune
against them, and do not add jitter. A test client weakened to avoid detection
tests nothing.

- [ ] **Step 5: Run the lanes**

Run: `devbox run -- task test:conformance`
Expected: PASS in every repository.

- [ ] **Step 6: Commit**

```bash
git add internal/conformance Taskfile.yml .github
git commit -m "test(conformance): add the compatibility matrix lanes"
```

### Task 3: Prebuilt headless client scenarios

**Files:**
- Create: `internal/conformance/scenario/scenario.go`
- Create: `internal/conformance/scenario/scenarios_test.go`
- Create: `testdata/conformance/scenarios/*.json`

**Interfaces:**
- Consumes: the `headless-minecraft` client and its action API.
- Produces:

```go
// Scenario is one scripted session with an expected outcome.
type Scenario struct {
    Name    string
    Steps   []Step
    Expect  Expectation
}

func Run(ctx context.Context, s Scenario, addr string) (Result, error)
```

Eight scenarios, named by the master plan: status, login, movement, attack,
inventory, crafting, malformed disconnects, and graceful shutdown.

- [ ] **Step 1: Write the failing test**

```go
func TestAMalformedDisconnectIsReportedNotSwallowed(t *testing.T) {
    t.Parallel()

    // The scenario that matters most, because it is the one a happy-path suite
    // never reaches: a server that closes mid-packet must produce a named
    // failure, not a client that hangs or reports a clean close.
    result, err := scenario.Run(t.Context(), scenario.MalformedDisconnect(), fixtureAddr(t))
    if err != nil {
        t.Fatalf("Run: %v", err)
    }
    if result.Outcome != scenario.OutcomeTransportLoss {
        t.Errorf("got %v, want a reported transport loss", result.Outcome)
    }
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `devbox run -- task test -- ./internal/conformance/scenario`
Expected: FAIL.

- [ ] **Step 3: Implement the scenarios**

Each is data plus a runner. Keep them as JSON so a scenario can be added without
a code change, and so the same file drives the lane in more than one repository.

- [ ] **Step 4: Run the scenarios**

Run: `devbox run -- task test:conformance`
Expected: PASS, all eight.

- [ ] **Step 5: Commit**

```bash
git add internal/conformance/scenario testdata
git commit -m "test(conformance): add the eight headless client scenarios"
```

### Task 4: Community test cases, licence-checked

**Files:**
- Create: `docs/verification/2026-XX-XX-community-case-review.md`
- Create: `testdata/conformance/cases/*.json`

**Interfaces:**
- Consumes: Task 3's scenario format.
- Produces: black-box cases derived from community servers, each with its
  licence check recorded.

- [ ] **Step 1: Review licences before converting anything**

For each candidate case, record the source, its licence, and whether it permits
reuse. A case whose licence does not permit it is reimplemented from observed
behaviour or dropped, and the record says which.

Write the record first. A conversion done before the check is a conversion that
has to be undone, and in a public repository with a module mirror, undone is not
the same as gone.

- [ ] **Step 2: Convert the permitted cases**

Express each as a Task 3 scenario. Independently maintained means the case
describes observed behaviour, not the source's implementation.

- [ ] **Step 3: Run them**

Run: `devbox run -- task test:conformance`
Expected: PASS, or a failure that names a real defect.

- [ ] **Step 4: Commit**

```bash
git add docs/verification testdata/conformance/cases
git commit -m "test(conformance): add licence-checked community cases"
```

### Task 5: Public API compatibility tests

**Files:**
- Create: `api/api.txt` in each runtime repository
- Create: `internal/api/compat_test.go`
- Modify: `Taskfile.yml`

**Interfaces:**
- Consumes: `golang.org/x/exp/apidiff`.
- Produces: `task api:check`, failing on any incompatible change to an exported
  symbol, and `task api:accept`, rewriting the baseline deliberately.

- [ ] **Step 1: Write the failing test**

```go
func TestThePublicAPIHasNotChangedIncompatibly(t *testing.T) {
    t.Parallel()

    // The baseline is committed. A change that breaks a consumer fails here and
    // is either reverted or accepted on purpose by rewriting the baseline in
    // the same commit, where a reviewer sees it.
    report := apicheck.Compare(t, "../../api/api.txt", apicheck.Current(t))

    if len(report.Incompatible) != 0 {
        t.Errorf("incompatible API changes:\n%s", report.Incompatible)
    }
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `devbox run -- task api:check`
Expected: FAIL, no baseline exists.

- [ ] **Step 3: Generate the baselines and implement the check**

Write `api/api.txt` for every runtime repository from the current exported
surface, then implement the comparison. Report incompatible and compatible
changes separately: an added method is news, not a failure.

- [ ] **Step 4: Run the check**

Run: `devbox run -- task api:check`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add api internal/api Taskfile.yml
git commit -m "test(api): freeze the public surface with a compatibility baseline"
```

### Task 6: Migration notes

**Files:**
- Create: `docs/MIGRATION.md` in each runtime repository
- Modify: `CHANGELOG.md`

**Interfaces:**
- Consumes: Task 5's compatibility report.
- Produces: one migration note per incompatible change since the last tag.

- [ ] **Step 1: Enumerate the breaking changes**

Run `task api:check` against the last released tag rather than the baseline. Its
incompatible list is the exact set of notes to write; there is no judgement call
about what counts.

- [ ] **Step 2: Write a note per change**

Each note says what changed, why, and the smallest edit that fixes a consumer.
"Renamed for clarity" is not a why. If a change has no reason worth writing, it
is a change worth reverting before `v1.0.0` fixes it forever.

- [ ] **Step 3: Commit**

```bash
git add docs/MIGRATION.md CHANGELOG.md
git commit -m "docs: add migration notes for the v1 surface"
```

### Task 7: Release

**Files:**
- Modify: `CHANGELOG.md`
- Modify: `MASTER_PLAN.md`

**Interfaces:**
- Consumes: every gate above.
- Produces: `v1.0.0` tags.

Release order follows the dependency order, because each consumer must be
released against a released dependency with no `replace` directive:
`minecraft-protocol`, then `minecraft-reference` and `minecraft-simulation`,
then the capture repository, then `headless-minecraft` and `server`.

- [ ] **Step 1: Run every gate in every repository**

Run in each: `devbox run -- task verify`, `task test:conformance`,
`task api:check`, and `task release:check VERSION=v1.0.0`.

`release:check` already asserts a clean worktree, a valid version string, and
**no `replace` directive in `go.mod`**. That last one is what a release-order
mistake trips on, and it is worth knowing it is checked rather than trusted.

- [ ] **Step 2: Confirm the manual checks have been run**

Automated gates cannot cover the two that matter most: a real vanilla client
playing against `server`, and a real Mojang session-server login. Every test
stubs the session server, and a hash wrong in the same way on both sides of a
loopback test still passes.

Check `docs/verification/` for a record of each, against this code, not an
earlier revision. If there is none, run them and write one. Do not tag without
it.

- [ ] **Step 3: Tag, in dependency order**

```bash
git tag -a v1.0.0 -m "v1.0.0" && git push origin v1.0.0
```

After each tag, update the consumer's `go.mod` to the released version, remove
the `replace`, and re-run its gates before tagging it.

- [ ] **Step 4: Update the master plan**

Mark M10 complete only after every gate passed in every repository, and record
the tag and the verification records that closed it.

- [ ] **Step 5: Commit**

```bash
git add MASTER_PLAN.md CHANGELOG.md && git commit -m "docs: record the v1.0.0 releases"
```

---

## What M10 cannot do

Recorded so that a green matrix is not read as more than it is.

**A pinned artifact ages.** Every lane proves this code against one build of
Paper, one Node version, and one client. It says nothing about the next build,
and the pin is what makes the result reproducible rather than current. Re-pin
deliberately, on a schedule, and treat a re-pin failure as a finding.

**A fixture proves the plumbing, not the protocol.** Lanes driven against this
project's own server share this project's understanding of the protocol. A
mutual misunderstanding passes every one of them. Only the Node lanes, the real
clients, and the captured traces can find one, which is why the matrix is a
matrix.

**`v1.0.0` is permanent.** The module mirror serves immutable snapshots and the
checksum database records them in an append-only log. Rewriting history removes
content from GitHub only. Everything in Task 7 is downstream of that: the gates
are not ceremony, they are the last point at which a mistake is still cheap.
