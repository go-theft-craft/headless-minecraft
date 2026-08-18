# M10 Conformance and Releases Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `subagent-driven-development` (recommended) or `executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Do the part of M10 that M9 does not block, and stop before the tag. Close the one code gap M10 owns outright, settle the one decision it has been carrying since M3, give the one repository with no release gate the gate every other one has, stop the two-version vanilla lane depending on a sibling repository's ignored directory, and freeze the public surface while it is still cheap to change.

**Architecture:** The 2026-08-16 M10 plan was written before M9 measured anything, and it proposed a conformance framework — a per-repository `internal/conformance` package, five copies of a pinned artifact manifest, and a matrix of lanes — as if none of it existed. Half of it does exist, unevenly and under other names, and one piece of it exists in a released repository whose whole job is fetching and verifying Mojang artifacts. This plan checks each of the nine M10 checklist items against the working trees on 2026-08-18, records what is already satisfied rather than rebuilding it, and executes only the items whose prerequisites are met today.

**Tech Stack:** Go 1.26.6 from `openserbia/go-flake`, Devbox, Task, golangci-lint, `golang.org/x/exp/apidiff`, the pinned Node `minecraft-protocol` 1.66.2 lanes, and `minecraft-reference`'s artifact pipeline. One new dependency, in one repository: `apidiff`, in Task 5.

## Before executing this plan: what M10 turned out to be

`MASTER_PLAN.md`'s M10 section states nine checklist items. Each was checked
against the working trees on 2026-08-18, and the state below is what the check
found. Read this before the tasks: four of the nine are already satisfied or
already answered elsewhere, one is measurably impossible, and the plan records
that rather than redoing it.

| M10 checklist item | State | Evidence |
| --- | --- | --- |
| Convert community-server cases into black-box scenarios, licence-checked | Not started, and no longer the cheapest way to get what it wanted | No `testdata/conformance` in any of the six repositories. Every scenario actually in hand — the six-per-version vanilla lanes, `relay`'s capture oracle, M9.2's item and arrow gates — was written from behaviour observed against a real jar, which is the "independently maintained" property the conversion was for. The licence review is work that buys a property the existing lanes already have |
| Compatibility matrices against Node, Paper, the owned Go server, and vanilla clients | Half exists; the Node half is now measured rather than guessed | Node at 47: `interop/node/runner.mjs` drives `minecraft-protocol@1.66.2` at `1.8.8`, both directions, as a required gate. Node at 775: codec-level only — `TestProtocol775DifferentialFixtures` runs 44 fixtures through the pinned `minecraft-data` `26.1` ProtoDef schema. A 775 *client* lane cannot be written: 1.66.2's `supportedVersions` ends at `1.21.11`. Paper: `livecheck` is opt-in, manual, and gated on `MCPROTO_LIVE_ADDR`. Vanilla clients: one record, `minecraft-protocol/docs/verification/2026-08-16-vanilla-client-check.md`. Owned Go server: no lane at either version, and none is possible at 775 for the reason in the next row |
| Teach `login.Acceptor` protocol 775 | Open. It is the only code gap M10 owns outright, and it blocks four other things | `login/acceptor.go` names `generated/java/v1_8` at ten call sites. `login/doc.go` says "This package is protocol 47 only", and that sentence is now wrong about half the package: `Negotiator` is version-neutral through `protocol.LoginExchange`, and `login/negotiator_v26_1_test.go` drives a full 775 login through configuration into play. `server/server/commands/v775/brigadier.go` renders 775 command trees that M11.7 recorded have never been sent to a client, because the server cannot serve a 775 login |
| Run at least one online-mode lane, picking up M6.4 | Open, and it is a manual act rather than a task | `headless-minecraft/auth/auth.go`: "M6.3 implements offline authentication only. The Microsoft device-code…". Nothing has changed since M6 closed |
| Settle the advertised version string | Already reconciled in code. Only the decision is unwritten | `minecraft-protocol/generated/java/v1_8/version.go` carries both names, four lines apart: `VersionName string = "1.8.9"` at line 11 and `MinecraftVersion: "1.8.8"` at line 22. `server/internal/server/protocolinfo/protocolinfo.go` picks `"1.8.8"`, says why in a comment, and pins it with a test. The independent implementation agrees: Node 1.66.2 lists protocol 47 as `1.8.8`, and `interop/node/runner.mjs` drives it under that name. There is nothing to change and something to record |
| Prebuilt headless vanilla-client scenarios for eight behaviours | Exists as six per version, under a different name, and is not reproducible from a pin | `client/vanilla_scenario_test.go` and `client/vanilla_e2e_test.go`, behind the `vanilla` build tag, run by `task test:vanilla`. The 1.8.9 jar comes from `mcreference` through `task server:vanilla`. The 26.1.2 jar is read from `../../minecraft-simulation/reference/work/versions/26.1.2/server/executable.jar` — a relative path into a sibling repository's gitignored workspace |
| Keep Java reference artifacts and decompiled sources local | Done, and enforced by a rule rather than a habit | `minecraft-reference/.gitignore` ignores `*.jar`, `*.class`, `*.java`, every mapping format, `**/versions/*/mappings/**`, and `/reference/work/` |
| Public API compatibility tests and migration notes | Not started anywhere | No `apidiff` import, no `api/` directory, and no `MIGRATION.md` in any of `minecraft-protocol`, `headless-minecraft`, `minecraft-simulation`, `server`, `relay`, or `minecraft-reference` |
| Publish stable releases only after all release gates pass | Blocked on one repository having no gates at all, while another already shipped its `v1.0.0` | `server` has no `.github/` directory, no `verify` task, no `release:check`, no `fmt:check`, no `secrets`, no `vuln`, no `CHANGELOG.md`, and no tags — and it is public, and M10 names it as the harness both matrices are driven against. `minecraft-reference` is at `v1.0.1` with `ci.yml`, `compatibility.yml`, and `release.yml` |

Five facts the checklist does not cover, each of which changes a task:

- **The pinned artifact manifest already exists, in the repository whose job it
  is.** M10 Task 1 proposed `internal/conformance/manifest.go` plus
  `testdata/conformance/manifest.json` in each of five repositories.
  `minecraft-reference` already downloads by URL and verifies SHA-1 and SHA-256
  against Mojang's own manifest before a file reaches its final path
  (`internal/reference/artifact/download.go`), keeps a version catalog with
  per-version acceptance thresholds
  (`internal/reference/config/defaults/versions.json`), prints the compatibility
  matrix (`task versions:matrix`), accepts passing reports into the catalog
  (`task versions:accept`), and runs it in CI (`compatibility.yml`). It is
  released. Writing five more manifests would duplicate a released module and
  give the two halves of the project two different ideas of which build was
  tested.

- **M9 blocks the release half, and is not close.** M9.3's stated gate is met on
  both versions, but its replay-against-corpus half is blocked on a capture
  nobody in these repositories can take. M9.4 through M9.8 are drafted plans
  ahead of their prerequisites, not executed work. M10 depends on M9. No task
  here tags anything.

- **`server`'s test task cannot run in a fresh clone.** `task test` runs
  `go test -mod vendor`, and `vendor/` is gitignored and untracked — zero files
  under `vendor/` are in the index. It works today because `deps` runs
  `go mod vendor` first on a developer's machine. There is no CI to notice.

- **`server`'s `deps` task turns off checksum verification globally and
  permanently.** It runs `go env -w GOSUMDB=off`, which writes to the user's Go
  environment file, not to a shell. Every module that user fetches afterwards,
  in every repository, skips `sum.golang.org`. In a project about to publish six
  `v1.0.0` tags into that same append-only log, that is worth fixing before the
  tags rather than after.

- **The P4 uptake work is live in `headless-minecraft`'s index.** As of
  2026-08-18 its `go.mod`, `go.sum`, `examples/go.mod`, and `examples/go.sum` are
  staged with the `v0.5.0` → `v0.6.0` bump, and `version/java/action_coverage_test.go`
  is untracked. Task 4 lands in that repository and must not commit somebody
  else's staged work.

## Global Constraints

- Work in the repository each task names. Tasks 1, 2, and 5 land in
  `minecraft-protocol`, Task 3 in `server`, Task 4 in `headless-minecraft`, and
  Task 6 in `headless-minecraft` and `minecraft-protocol`.
- Run project commands as `devbox run -- task <name>`. Where no task exists for
  the command, run it as `devbox run -- go ...` so it uses the pinned toolchain.
- Tests run with `-race` wherever the repository's own task does.
- Each task ends with a commit. Never add a `Co-Authored-By` or
  `Claude-Session` trailer to a commit message.
- Start each task from a clean tree in the repository it names. `minecraft-protocol`
  and `server` were clean at 2026-08-18; `headless-minecraft` was not, and Task 4
  says what to do about that. If `git status --porcelain` shows work you did not
  do, stop and ask rather than committing it.
- Every repository this plan touches is public, and a published module version
  cannot be retracted: `proxy.golang.org` serves immutable snapshots and
  `sum.golang.org` records their hashes in an append-only log. Rewriting git
  history removes content from GitHub only.
- Keep Java reference artifacts and decompiled sources local. Publish
  provenance, independent behaviour descriptions, fixtures, and expectations.
  No task here relaxes that boundary.
- Do not name the legacy proxy's project, its protocol, its codename, or its
  repository directory anywhere. No task here has any reason to.
- Add no dependency except the one Task 5 names, and add it in one repository.

## Design decisions this plan settles

**A version-neutral login has two halves, and only one was built.**
`protocol.LoginExchange` is the seam that made `Negotiator` version-neutral, and
every method on it is a client's: `StartLogin`, `ReadEncryptionRequest`,
`WriteEncryptionResponse`, `ReadLoginSuccess`. The server half was never
declared, so `Acceptor` reached past the seam and imported a version package
directly. Task 1 declares the server half beside the client half, in the same
file, generated from the same two templates, so the next protocol gets both or
neither.

**The acceptor drives the configuration phase; it does not fill it.** Protocol
775 puts a configuration state between login success and play, and a real
vanilla client will not leave it until it has registry data. Registry data is
game content, and `minecraft-protocol` owns the protocol rather than the game.
So the acceptor runs a caller-supplied configuration step between
login-acknowledged and finish-configuration, and its default sends nothing. That
is enough for this project's own client and not enough for a vanilla one, and
Task 1's documentation says so in those words rather than implying a 775 server.

**The artifact pin is `minecraft-reference`, not five copies of it.** M10 Task 1
is replaced by Task 4, which points the lanes at the pipeline that already
downloads, verifies, catalogs, and reports — and which is already released at
`v1.0.1`. A conformance run is reproducible because one released module says
which build it fetched, not because five repositories each keep a JSON file they
hope agree.

**Two names for protocol 47 is the right answer, and the contract says which is
which.** `1.8.9` names the dataset, because that is the version PrismarineJS
published the data under and the version `mcreference` prepares. `1.8.8` is what
a client is told, because that is what protocol 47 clients and the independent
Node implementation call it. M3 changed no byte and called the reconciliation "a
decision of its own"; Task 2 makes it a decision by writing it down and pinning
both names in one test, still changing no byte.

**A baseline taken now costs nothing and a baseline taken at the tag costs a
revert.** M9.4 through M9.8 will move the public surface of `minecraft-protocol`
and `minecraft-simulation`. Task 5 freezes it before they do, so each of those
milestones sees its own API change in its own diff, rather than a reviewer at
`v1.0.0` discovering an accumulated surface nobody chose.

## Not in this plan

- **Any `v1.0.0` tag, in any repository.** M10 depends on M9, M9.4 through M9.8
  are unexecuted, and M9.3's corpus half is blocked. Task 6 records that; it does
  not work around it.
- **The online-mode lane, and therefore M6.4.** It needs a real Microsoft
  account and a real online-mode server, which is a manual act by a person with
  credentials, not a task an agent executes. Task 6 keeps it on the checklist
  with the prerequisite named.
- **The community-server case conversion.** Task 6 removes it from the checklist
  with the reason, rather than leaving an unstarted item that reads like planned
  work.
- **A 775 Node client lane.** It is not deferred, it is unavailable: 1.66.2 tops
  out at `1.21.11`. Task 6 records the version checked and the date, because an
  absent lane that says why is evidence and an absent lane that says nothing
  reads as coverage.
- **The conformance matrix lanes for movement, digging, placement, attack,
  containers, and crafting.** Those are M9.4 through M9.8's gates, in M9.4
  through M9.8's plans. M10 collects them; it does not write them.
- **Registry data for a 775 server.** Task 1 opens the seam. Filling it so a
  real vanilla 26.1.2 client reaches play against `examples/vanilla` is
  `server`'s work and is not scheduled here.
- **`server`'s vendor directory.** Task 3 makes the gate not depend on it. Whether
  to track it, or drop `-mod vendor`, is a decision for whoever owns `server`'s
  build, and Task 3 states the choice it made and why rather than making it
  silently for the whole repository.

---

### Task 1: `login.Acceptor` speaks protocol 775

**Files:**
- Modify: `login_exchange.go`
- Modify: `internal/codegen/generator/templates/login_exchange.go.tmpl`
- Modify: `internal/codegen/generator/templates/v26_1/login_exchange.go.tmpl`
- Modify: `login/acceptor.go`
- Modify: `login/doc.go`
- Create: `login/acceptor_v26_1_test.go`
- Modify: `CHANGELOG.md`
- Regenerated: `generated/java/v1_8/login_exchange.go`, `generated/java/v26_1/login_exchange.go`

**Done, 2026-08-18, in `b644bb4`. One deviation, recorded below.**

`ServerSteps() []LoginStep` was not built. The negotiator already tells the two
protocols apart by asking `Answer(RoleLoginAcknowledged)` whether the client has
an acknowledgement to send — "a protocol whose login ends at success has no
acknowledgement to send" — and the acceptor can ask the same question from the
other end of the wire. A second way of describing the same fact would have been
a second thing to keep true. What the server half needed instead was `Announce`,
symmetric to `Answer`: the packets a server sends to take a step of the sequence
itself, which is `RoleConfigurationFinished` and nothing else. Seven methods
either way; one fewer concept.

**Interfaces:**
- Consumes: `protocol.LoginIdentity`, `protocol.EncryptionRequest`,
  `protocol.LoginRole`, `protocol.Packet`, `protocol.Stream`.
- Produces, in `login_exchange.go`, beside the client half and in the same
  interface, because a protocol that can be logged into can be logged in from
  and splitting them into two interfaces would let a version implement one:

```go
// ReadLoginStart reads the account a client claimed when it opened a login.
// The UUID is empty where the protocol does not carry one.
ReadLoginStart(Packet) (LoginIdentity, error)
// WriteEncryptionRequest builds a server's request to encrypt. A protocol
// with no ShouldAuthenticate field ignores it rather than encoding it.
WriteEncryptionRequest(EncryptionRequest) (Packet, error)
// ReadEncryptionResponse reads a client's answer: the shared secret and the
// verify token, both still encrypted with the server's public key.
ReadEncryptionResponse(Packet) (secret, verifyToken []byte, err error)
// WriteLoginSuccess builds the packet that confirms an account.
WriteLoginSuccess(LoginIdentity) (Packet, error)
// WriteCompress builds the packet that sets a compression threshold, and
// reports false for a protocol that has none.
WriteCompress(threshold int32) (Packet, bool)
// WriteLoginDisconnect builds the packet that refuses a login, with the
// reason rendered the way this protocol states one.
WriteLoginDisconnect(reason string) (Packet, error)
// ServerSteps reports the states a server drives a login through after
// success, in order, and the role it waits for in each. Protocol 47 reports
// nothing, which is how it says its login ends at success; protocol 775
// reports the configuration state and the acknowledgement it waits for.
ServerSteps() []LoginStep
```

```go
// LoginStep is one state a server takes a login through after success.
type LoginStep struct {
    // Enter is the state the stream moves to.
    Enter State
    // AwaitBefore is the role the server waits for before entering, or
    // empty when it enters as soon as the previous step finished.
    AwaitBefore LoginRole
    // FinishWith is the role the server sends to leave the state, or empty
    // when leaving is the client's move.
    FinishWith LoginRole
}
```

And on `Acceptor`, one option, because the acceptor knows the shape of the
configuration phase and nothing about its content:

```go
// WithConfiguration sets the step the acceptor runs while the connection is
// in a configuration state, between the client's acknowledgement and the
// packet that ends configuration.
//
// It exists because protocol 775 puts a state between login and play that a
// real client will not leave until it has registry data, and registry data is
// game content rather than protocol. The default sends nothing, which is
// enough for a client that answers what it is sent and is not enough for a
// vanilla client. A protocol with no configuration state never calls it.
func WithConfiguration(step func(context.Context, *protocol.Stream) error) AcceptorOption
```

- [x] **Step 1: Write the failing test**

`login/acceptor_v26_1_test.go`, built the way `login/negotiator_v26_1_test.go`
is built — a real `v26_1.Protocol().NewSession`, a real stream, the acceptor on
one end. The one that matters is the round trip, because `Negotiator` and
`Acceptor` already test against each other at 47 and the whole point of this task
is that they can do it at 775:

```go
func TestTheAcceptorAndNegotiatorAgreeOn775(t *testing.T) {
    t.Parallel()

    // The 47 pair has been tested against itself since P3b. This is the same
    // test at 775, and it is the first thing in this project that could serve
    // a modern login: until it passes, server/commands/v775/brigadier.go
    // renders command trees that no client has ever been sent.
    client, server := net.Pipe()
    t.Cleanup(func() { _ = client.Close(); _ = server.Close() })

    accepted := make(chan Profile, 1)
    go func() {
        acceptor := mustAcceptor(t, WithConfiguration(func(context.Context, *protocol.Stream) error {
            return nil
        }))
        profile, err := acceptor.Accept(t.Context(), serverStream(t, server, v26_1.Protocol()))
        if err != nil {
            t.Errorf("Accept: %v", err)
            return
        }
        accepted <- profile
    }()

    negotiator := mustNegotiator(t, mustOffline(t, "Dinnerbone"))
    stream := clientStream(t, client, v26_1.Protocol())
    profile, err := negotiator.Negotiate(t.Context(), stream)
    if err != nil {
        t.Fatalf("Negotiate: %v", err)
    }

    if got := stream.Snapshot().State; got != v26_1.StatePlay {
        t.Errorf("the client ended in %v, want play", got)
    }
    if got := (<-accepted).Name.String(); got != profile.Name.String() {
        t.Errorf("the server confirmed %q, the client believes %q", got, profile.Name.String())
    }
}

func TestTheAcceptorRunsTheConfigurationStepExactlyOnce(t *testing.T) {
    t.Parallel()

    // Once, and between the acknowledgement and the packet that ends
    // configuration. A step run before the acknowledgement writes into a state
    // the client has not entered; a step run twice sends a registry twice.
    var calls atomic.Int32
    // ... drive the pair with WithConfiguration incrementing calls ...
    if got := calls.Load(); got != 1 {
        t.Errorf("the configuration step ran %d times, want 1", got)
    }
}

func TestTheAcceptorNeverRunsAConfigurationStepOn47(t *testing.T) {
    t.Parallel()

    // Protocol 47 has no configuration state. A step that runs anyway would
    // write packets into login, and the failure would look like a codec bug.
    var calls atomic.Int32
    // ... drive the 47 pair with WithConfiguration incrementing calls ...
    if got := calls.Load(); got != 0 {
        t.Errorf("the configuration step ran %d times on 47, want 0", got)
    }
}
```

- [x] **Step 2: Run it to verify it fails**

Run: `devbox run -- go test -race -run 'Acceptor' ./login`
Expected: FAIL, `undefined: WithConfiguration`, and the acceptor writing v1_8
packets into a v26_1 session.

- [x] **Step 3: Add the server half to the contract and both templates**

Add the seven methods and `LoginStep` to `login_exchange.go`. Then implement
them in `internal/codegen/generator/templates/login_exchange.go.tmpl` and
`internal/codegen/generator/templates/v26_1/login_exchange.go.tmpl` and
regenerate:

Run: `devbox run -- task generate` then `devbox run -- task generate:check`
Expected: the two `generated/java/*/login_exchange.go` files change and the
check passes.

`ServerSteps` is where the two protocols differ and the only place they should:
protocol 47 returns nothing, protocol 775 returns one step entering
`StateConfiguration` after `RoleLoginAcknowledged` and finishing with
`RoleConfigurationFinished`. If `login_role.go` has no role for one of those,
add it there rather than naming a packet type in the acceptor.

- [x] **Step 4: Rewrite the acceptor against the contract**

Delete the `generated/java/v1_8` import from `login/acceptor.go`. Every one of
its ten call sites becomes a `LoginExchange` call. The terminal-state assertion
becomes: walk `ServerSteps()`, and the login ends in whatever state the last one
leaves — which is play for both, by different routes.

Run: `devbox run -- go test -race ./login`
Expected: PASS, including the existing 47 acceptor tests unchanged. If a 47 test
needed changing, the contract is wrong; fix the contract, not the test.

- [x] **Step 5: Correct `login/doc.go`**

The sentence "This package is protocol 47 only" was already wrong about
`Negotiator` before this task and is wrong about both halves after it. Replace
it with what is true: the package drives either protocol's login through
`protocol.LoginExchange`, the acceptor's configuration step is empty by default,
and an empty configuration step is enough for a client that answers what it is
sent and not enough for a vanilla one.

- [x] **Step 6: Run the full gate**

Run: `devbox run -- task verify`
Expected: PASS, including `test:interop` and `test:protodef`.

- [x] **Step 7: Commit**

```bash
git add login login_exchange.go login_role.go internal/codegen generated CHANGELOG.md
git commit -m "feat(login): accept a protocol 775 login"
```

### Task 2: The advertised version string is a decision, not two comments

**Files:**
- Modify: `generated/java/v1_8/version.go` — comment only, through the template
- Modify: `internal/codegen/generator/templates/` — whichever template emits `version.go`
- Create: `docs/version-names.md`
- Modify: `README.md` or `doc.go`, wherever the data contract is stated
- Create or modify: a test in `generated/java/v1_8/data_test.go` that pins both names together

**Interfaces:**
- Consumes: `protocol.Version`, `data.Version`.
- Produces: no new exported symbol. This task changes no byte, which is the
  point — M3 already changed none, and what was missing was the record.

- [ ] **Step 1: Write the failing test**

```go
func TestProtocol47HasTwoNamesAndTheContractSaysWhichIsWhich(t *testing.T) {
    t.Parallel()

    // Two names, four lines apart in this file, and until now the difference
    // was a coincidence a reader had to notice. "1.8.9" names the dataset,
    // because that is what PrismarineJS published it as and what mcreference
    // prepares. "1.8.8" is what a client is told, because that is what
    // protocol 47 clients call it and what the independent Node
    // implementation lists. A change to either is a change to a contract.
    if got := Version().Name; got != "1.8.9" {
        t.Errorf("the dataset name is %q, want %q", got, "1.8.9")
    }
    set, err := data.Load("java/1.8.9")
    if err != nil {
        t.Fatalf("data.Load: %v", err)
    }
    if got := set.Version().MinecraftVersion; got != "1.8.8" {
        t.Errorf("the advertised name is %q, want %q", got, "1.8.8")
    }
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `devbox run -- go test ./generated/java/v1_8`
Expected: FAIL until the assertion is added. `TestVersionAndRegistration` in
that file already asserts both values; this test exists beside it to say why
they differ, so if the two ever disagree the failure names the contract rather
than a literal.

The dataset is addressed as `java/1.8.9` and reports `MinecraftVersion: "1.8.8"`,
which is the whole shape of the confusion in one call.

- [ ] **Step 3: Write the record**

`docs/version-names.md` says the rule in three paragraphs: which name goes in a
status response and a login, which name goes in a dataset path and a
`mcreference` invocation, and that they differ for protocol 47 and coincide for
protocol 775 — where `26.1` names the family and `26.1.2` names a build, which
M4 already settled.

Name the two consumers of the rule, because the record is for them:
`server/internal/server/protocolinfo/protocolinfo.go` advertises `1.8.8` and its
comment can now point here instead of arguing the case locally, and
`interop/node/runner.mjs` drives Node at `1.8.8` because that is the name the
independent implementation lists.

- [ ] **Step 4: Point the generated comment at the record**

Through the template, not by hand. Regenerate and check:

Run: `devbox run -- task generate && devbox run -- task generate:check`
Expected: PASS.

- [ ] **Step 5: Run the tests**

Run: `devbox run -- task verify`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add docs/version-names.md generated internal/codegen README.md
git commit -m "docs: settle which name protocol 47 advertises and which names its data"
```

### Task 3: `server` gets the release gate every other repository has

**Files:**
- Modify: `Taskfile.yml`
- Create: `.github/workflows/ci.yml`
- Create: `CHANGELOG.md`
- Modify: `.gitignore` if the vendor decision requires it

**Interfaces:**
- Consumes: the task names the other five repositories already use, so that
  "run every gate in every repository" is one command rather than six.
- Produces: `task fmt:check`, `task vuln`, `task secrets`, `task verify`, and
  `task release:check VERSION=…`, matching `minecraft-protocol/Taskfile.yml`'s
  shape, plus a CI workflow that runs `verify` on push and pull request.

**Copy the gate, do not design one.** `minecraft-protocol`, `headless-minecraft`,
`minecraft-simulation`, and `minecraft-reference` all have the same five tasks
with the same names. A sixth variant is a sixth thing to remember.

- [ ] **Step 1: Prove the current gate is broken from a clean clone**

```bash
git -C "$(mktemp -d)" clone --depth 1 https://github.com/go-theft-craft/server.git fresh
cd fresh && devbox run -- go test -mod vendor ./... 2>&1 | head -5
```

Expected: it fails. `vendor/` is gitignored and no file under it is tracked, and
`task test` passes `-mod vendor`. Record the exact error in the commit message;
it is the reason this task exists rather than an opinion about tooling.

- [ ] **Step 2: Decide the vendor question and write the decision down**

Two options, and the task takes the second:

Track `vendor/`, which makes `-mod vendor` honest and adds a large diff to every
dependency bump in a public repository. Or drop `-mod vendor` and let the module
cache resolve, which is what the other five repositories do and what CI does
anyway.

Take the second: remove `-mod vendor` from `test` and `test:race`, remove
`go mod vendor` from `deps`, and leave `vendor/` ignored so an existing
developer's directory is harmless. Put the reason in a comment above `deps`.

- [ ] **Step 3: Stop `deps` disabling checksum verification globally**

`go env -w GOSUMDB=off` writes to the user's Go environment file. It is not
scoped to this repository, this shell, or this task, and it silently opts every
later `go get` — in every repository on that machine — out of
`sum.golang.org`. Remove it. Remove `go env -w GOPROXY=…` with it: the default
is already `proxy.golang.org,direct`, and writing it globally to get it is the
same mistake in a milder form.

Remove `go mod tidy` from `deps` as well, or move it to its own task. A `deps`
that every other task depends on and that rewrites `go.mod` means any task can
change the module graph, which is precisely what `release:check`'s clean-tree
assertion is there to catch.

- [ ] **Step 4: Split `fmt` from `fmt:check`**

`lint` depends on `fmt`, and `fmt` writes files. So linting mutates the tree,
which a clean-tree release check would fail and a reviewer would never see.
Add `fmt:check` — the same formatters with `-l` and no `-w`, failing on any
output — and point `lint` at it.

- [ ] **Step 5: Add `vuln`, `secrets`, `verify`, and `release:check`**

Copy the four from `minecraft-protocol/Taskfile.yml`, adjusting only paths.
`release:check` keeps all four of its assertions, and the `replace`-directive
one earns its place here more than anywhere: `server` requires both
`minecraft-protocol` and `minecraft-simulation`, so it is the repository a
release-order mistake lands in.

- [ ] **Step 6: Add the CI workflow**

Copy `minecraft-protocol/.github/workflows/ci.yml`, which runs `verify` under
Devbox with the pinned toolchain. Include `test:examples`, because
`examples/` is a nested module and M11 made it this repository's test surface.

- [ ] **Step 7: Add `CHANGELOG.md`**

The same header the other repositories use, an `## Unreleased` section, and
under it the entries M11's seven sub-milestones earned — the framework shape,
the seams, the version-neutral world model, the audit trail, the examples. This
is the first release note `server` will have, and M11 is what it is about.

- [ ] **Step 8: Run the gate**

Run: `devbox run -- task verify` and
`devbox run -- task release:check VERSION=v0.1.0`
Expected: PASS. `release:check` is being run to prove it works, not to release;
no tag is created here or anywhere in this plan.

- [ ] **Step 9: Commit**

```bash
git add Taskfile.yml .github CHANGELOG.md .gitignore
git commit -m "build: give server the release gate every other repository has"
```

### Task 4: The vanilla lane stops reaching into a sibling repository

**Files:**
- Modify: `client/vanilla_e2e_test.go`
- Modify: `client/vanilla_scenario_test.go` if it carries the same paths
- Modify: `Taskfile.yml`
- Create: `docs/vanilla-lane.md`

**Interfaces:**
- Consumes: `minecraft-reference`'s `task reference:prepare` and its version
  catalog.
- Produces: the two-version lane driven by one environment variable naming a
  prepared reference workspace, defaulting to this repository's own, and
  skipping with a message that says how to prepare one.

**Why this and not a manifest.** M10 Task 1 wanted a pinned artifact manifest so
a conformance result names the build it tested. `minecraft-reference` already
does that, verifies SHA-1 and SHA-256 against Mojang's manifest before the file
reaches its path, and is released at `v1.0.1`. The gap is not a missing
manifest; it is that `client/vanilla_e2e_test.go` hardcodes
`../../minecraft-simulation/reference/work/versions/26.1.2/server/executable.jar`,
so the lane passes on a machine where a different repository happens to have
been prepared and skips everywhere else, and nothing records which build ran.

- [ ] **Step 0: Do not disturb the staged P4 work**

`git status --porcelain` in this repository shows staged `go.mod`, `go.sum`,
`examples/go.mod`, and `examples/go.sum`, and an untracked
`version/java/action_coverage_test.go`. That is the P4 uptake bump, mid-flight.
Either finish and commit it as P4's Task 1 says, or stash it, before starting
here. Do not `git add -A`, and do not commit it under this task's message.

- [ ] **Step 1: Write the failing test**

```go
func TestTheLaneNamesTheBuildItRan(t *testing.T) {
    // A conformance result that does not say which jar produced it is a story
    // about somebody's laptop. This reads the prepared workspace's own record
    // rather than trusting a path.
    workspace := vanillaWorkspace(t) // MCREFERENCE_WORKSPACE, or the default
    build, err := preparedBuild(workspace, "26.1.2")
    if err != nil {
        t.Skipf("no prepared 26.1.2 reference workspace: %v; run `task server:vanilla VERSION=26.1.2`", err)
    }
    if build.SHA256 == "" {
        t.Fatalf("the workspace records no digest for 26.1.2")
    }
    t.Logf("26.1.2 server jar %s", build.SHA256)
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `devbox run -- go test ./client -run TestTheLaneNamesTheBuild -tags vanilla`
Expected: FAIL, `undefined: vanillaWorkspace`.

- [ ] **Step 3: Replace both hardcoded paths**

One helper, used by both lanes, resolving in this order: an explicit
`MCREFERENCE_WORKSPACE`, then this repository's own prepared workspace, then
skip. Never a relative path into a sibling repository — a test that reads
another repository's ignored directory passes or fails for reasons its own
repository cannot see.

Keep the skip. The lane is behind the `vanilla` build tag and out of `verify`
deliberately, and a lane that fails when no jar is prepared would make `verify`
depend on a download.

- [ ] **Step 4: Teach `task server:vanilla` the second version**

It already takes `VERSION` and defaults to `1.8.9`. Make `26.1.2` a documented
value and have it prepare into this repository's workspace, so
`task server:vanilla VERSION=26.1.2` is the whole answer to the skip message.

- [ ] **Step 5: Write `docs/vanilla-lane.md`**

What the lane covers — six scenarios per version, named — what it does not, how
to prepare each version, and where the digest of the jar that ran is recorded.
Note the property M10 was after and this delivers: the result names its build.

- [ ] **Step 6: Run both lanes**

Run: `devbox run -- task server:vanilla VERSION=1.8.9` then
`devbox run -- task server:vanilla VERSION=26.1.2`, then
`devbox run -- task test:vanilla`
Expected: PASS on both versions, and each logs the digest of the jar it ran
against.

- [ ] **Step 7: Commit**

```bash
git add client Taskfile.yml docs/vanilla-lane.md
git commit -m "test(vanilla): resolve the pinned jars from a reference workspace"
```

### Task 5: Freeze the public surface before M9.4 through M9.8 move it

**Files:**
- Create: `api/api.txt` in `minecraft-protocol`
- Create: `internal/apicheck/apicheck.go`
- Create: `internal/apicheck/apicheck_test.go`
- Modify: `Taskfile.yml`
- Modify: `go.mod` — adds `golang.org/x/exp`

**Interfaces:**
- Consumes: `golang.org/x/exp/apidiff`.
- Produces: `task api:check`, failing on any incompatible change to an exported
  symbol, and `task api:accept`, rewriting the baseline on purpose.

**Two repositories, not six.** A baseline is load-bearing where there are
consumers across a module boundary. `minecraft-protocol` has five; do it here.
`minecraft-simulation` has two and takes the same tooling when its own next
release comes, and this task's `internal/apicheck` is written to be copied.
`server`, `relay`, and `headless-minecraft` are consumed by nothing in this
project today, and freezing an API nobody imports buys a maintenance cost.

- [ ] **Step 1: Write the failing test**

```go
func TestThePublicSurfaceHasNotChangedIncompatibly(t *testing.T) {
    t.Parallel()

    // The baseline is committed, so an incompatible change fails here and is
    // either reverted or accepted deliberately by rewriting the baseline in
    // the same commit, where a reviewer sees both halves at once.
    report, err := apicheck.Compare("../../api/api.txt", apicheck.Current(t))
    if err != nil {
        t.Fatalf("Compare: %v", err)
    }
    if len(report.Incompatible) != 0 {
        t.Errorf("incompatible API changes:\n%s", strings.Join(report.Incompatible, "\n"))
    }
}

func TestAnAddedMethodIsNewsAndNotAFailure() { /* compatible changes report separately */ }
```

- [ ] **Step 2: Run it to verify it fails**

Run: `devbox run -- go test ./internal/apicheck`
Expected: FAIL, no baseline.

- [ ] **Step 3: Generate the baseline and implement the check**

Write `api/api.txt` from the current exported surface of every non-`internal`
package. `generated/java/v1_8` and `generated/java/v26_1` belong in it: they are
exported, consumers import them directly, and a regeneration that changes a
field type is exactly the change this is for.

Report incompatible and compatible changes separately.

- [ ] **Step 4: Run the check**

Run: `devbox run -- task api:check`
Expected: PASS.

- [ ] **Step 5: Wire it into `verify`**

`api:check` runs in `verify`, so an incompatible change fails in the branch that
made it rather than at a release.

Run: `devbox run -- task verify`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add api internal/apicheck Taskfile.yml go.mod go.sum
git commit -m "test(api): freeze the public surface with a compatibility baseline"
```

### Task 6: Restate M10 as what the reconciliation found

**Files:**
- Modify: `MASTER_PLAN.md` (in `headless-minecraft`)
- Modify: `ROADMAP.md` (in `minecraft-protocol`)
- Modify: `docs/superpowers/plans/2026-08-16-m10-conformance-releases.md`

**Interfaces:**
- Consumes: Tasks 1 through 5, and the table at the top of this plan.
- Produces: an M10 checklist a reader can act on, where every remaining item
  either has a prerequisite named or is unblocked.

- [ ] **Step 1: Rewrite the M10 checklist**

Mark done, with its evidence: the version-string decision (Task 2), the
`login.Acceptor` row (Task 1), the local-reference-artifacts row (already
enforced by `minecraft-reference/.gitignore`), and the prebuilt scenarios row —
recorded as six per version rather than eight, with the four the eight-item
list named that the six do not cover, so the reduction is a statement rather
than a quiet substitution.

Remove the community-server case row, with the reason from the table above.

Rewrite the matrix row into four rows with their real states: Node at 47 running
as a required gate; Node at 775 as a codec differential of 44 fixtures with the
client lane unavailable at 1.66.2, checked 2026-08-18; Paper as opt-in and
manual; the owned Go server as unblocked by Task 1 and unwritten.

Leave the online-mode row, the API-and-migration row, and the release row open,
each with what it waits on: a real account and a real server, `minecraft-simulation`
taking Task 5's tooling, and M9.4 through M9.8.

- [ ] **Step 2: Record what M10 cannot claim**

Add to the M10 section, in the master plan's own findings style:

**A pinned artifact ages.** Every lane proves this code against one build. The
pin makes the result reproducible, not current. Re-pin deliberately and treat a
re-pin failure as a finding.

**A fixture proves the plumbing, not the protocol.** Lanes driven against this
project's own server share this project's understanding of the protocol, and a
mutual misunderstanding passes all of them. Only the Node lanes, the real
clients, and the captured traces can find one — and at 775 the Node lane reaches
codecs and not sessions, so 775's session behaviour rests on the live client and
server lanes alone.

**Every automated check in this project has run against offline mode.** M6.4 is
still postponed and M10's online-mode lane is still what picks it up. A `v1.0.0`
that ships Microsoft authentication without it ships a code path this project
has never executed.

**`v1.0.0` is permanent.** The module mirror serves immutable snapshots and the
checksum database records them in an append-only log. Rewriting history removes
content from GitHub only.

- [ ] **Step 3: Reconcile `minecraft-protocol`'s P5**

P5 says "Publish `v1.0.0` after public APIs have compatibility tests", "Document
support windows for built-in protocol versions", and "Require migration notes for
every later breaking change". Task 5 satisfies the first. Task 2's
`docs/version-names.md` is where the second starts. Say so, and say that P5's
tag waits on M9 like M10's does, so a reader of either roadmap reaches the same
answer.

- [ ] **Step 4: Supersede the 2026-08-16 plan**

Add a header to it pointing here, saying which of its tasks were replaced and
why: Task 1's five manifests by `minecraft-reference`, Task 2's matrix by the
four measured rows, Task 4's community cases by their removal. Its Tasks 5, 6,
and 7 stand — Task 5 partly executed here, 6 and 7 waiting on M9.

Do not delete it. It is the record of what was believed before anything was
measured, and the difference between the two documents is the finding.

- [ ] **Step 5: Commit**

```bash
# in headless-minecraft
git add MASTER_PLAN.md docs/superpowers/plans
git commit -m "docs: restate M10 as what the working trees say it is"
# in minecraft-protocol
git add ROADMAP.md
git commit -m "docs: point P5 at M10's measured state"
```

---

## What this plan leaves M10 waiting on

Three things, and none of them is work an agent finishes.

**M9.4 through M9.8.** Drafted plans, unexecuted, each a two-version gate. M10
collects their results; it cannot produce them. Until they run, the compatibility
matrix has rows for status, login, and movement and nothing for digging,
placement, attack, containers, or crafting.

**A capture nobody here can take.** M9.3's replay-against-corpus half is blocked
on a player trace whose oracle is not itself, and M10 inherits the gap: the
movement row of the matrix rests on the six scenarios per version, which is
narrower than "movement matches vanilla".

**One person with an account.** The online-mode lane needs credentials and a real
online-mode server. It is the last unexercised code path before `v1.0.0` and the
only M10 item that no amount of scheduling turns into a task.
