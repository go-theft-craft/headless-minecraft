# M9 Gameplay Mechanics Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `subagent-driven-development` or execute this plan inline one task at a time. Keep every checkbox current.

**Goal:** Verify every gameplay mechanic — items, movement, digging, building, attack, containers, crafting — against captured vanilla behaviour, starting with the capture tool that makes the verification possible.

**Architecture:** M9 subdivides by mechanic because the simulation packages and the conformance fixtures are already organised that way and each mechanic is independently verifiable against a vanilla server. M9.1 builds the oracle: a new repository holding a protocol 47 proxy that sits between a real client and a real server, records both directions through `minecraft-protocol`'s capture format, and replays a recording deterministically. Every later stage is judged against traces that tool produces.

**Tech Stack:** Go 1.26.6 from `openserbia/go-flake`, Devbox, Task, `minecraft-protocol` v0.2.0 (`capture`, `replay`, `router`, `login`, `source`), `minecraft-simulation`'s kernel, and a pinned vanilla 1.8.9 server.

## Global Constraints

- Work in the repository each task names. M9.1 creates a new one; M9.2 through
  M9.8 land in `minecraft-simulation` with consumer changes in
  `headless-minecraft` and `server`.
- Run project commands as `devbox run -- task <name>`.
- Leave changes uncommitted unless explicitly requested.
- The capture repository speaks Java protocol 47 through `minecraft-protocol`.
  It is not built on the legacy proxy: that proxy speaks a different protocol
  family — one-byte packet identifiers, no VarInt framing, UCS-2 strings, and
  encryption in one direction only — and cannot capture Java Edition traces
  without being rewritten into a different program.
- Do not name the legacy proxy's project, its protocol, or its codename in any
  public repository, in any file, or in any commit message. Refer to it by role.
- Capture records what happened. It never synthesises a packet, never repairs a
  malformed one, and never reorders. A recording that cannot be replayed is a
  finding, not something to smooth over.
- Recordings hold player UUIDs, usernames, and chat. They are local runtime
  data, never committed. Add the recording directory to `.gitignore` in Task 1.
- Never record a login key exchange in the clear. `minecraft-protocol` M5 found
  this exact defect and fixed it; the capture sink inherits the fix and Task 5
  proves it still holds through a proxy.
- Do not tune against, detect, or evade anti-cheat. Captures come from servers
  the operator owns.

---

## Stage M9.1: the capture repository

**Dependency:** `minecraft-protocol` M5, which is complete. M9.1 does **not**
depend on M8: capture is a protocol-level problem and needs no kernel.

**Gate:** a captured trace replays deterministically from its recording.

### Task 1: Repository skeleton

**Files:**
- Create: `../minecraft-capture/go.mod`
- Create: `../minecraft-capture/devbox.json`
- Create: `../minecraft-capture/Taskfile.yml`
- Create: `../minecraft-capture/.gitignore`
- Create: `../minecraft-capture/doc.go`
- Create: `../minecraft-capture/LICENSE`

**Interfaces:**
- Consumes: nothing.
- Produces: module `github.com/go-theft-craft/minecraft-capture`, and the
  `devbox run -- task verify` entry point every later task runs.

**Settled 2026-08-17:** the name is `minecraft-capture`, and the module path is
`github.com/go-theft-craft/minecraft-capture`. The repository is local only so
far; nothing is pushed, so the module mirror has not yet made the path
permanent.

- [x] **Step 1: Create the module**

```bash
mkdir -p ../minecraft-capture && cd ../minecraft-capture
go mod init github.com/go-theft-craft/minecraft-capture
go get github.com/go-theft-craft/minecraft-protocol@v0.2.0
```

- [x] **Step 2: Copy the toolchain pin**

Copy `devbox.json` and `devbox.lock` from `minecraft-protocol` unchanged. The
Go toolchain is pinned through `openserbia/go-flake` and every repository in
this project uses the same pin; a capture tool that builds on a different Go
than the library it records is a variable nobody wants when a trace diverges.

- [x] **Step 3: Write the Taskfile**

Copy `Taskfile.yml` from `headless-minecraft` and delete the tasks whose tools
this repository does not have yet. Keep `deps`, `fmt`, `fmt:check`, `lint`,
`test`, `build`, `secrets`, `vuln`, and `verify`.

- [x] **Step 4: Ignore recordings**

```gitignore
# Recordings hold player UUIDs, usernames, and chat. Local runtime data only.
/recordings/
*.mccap
```

- [x] **Step 5: Verify**

Run: `devbox run -- task verify`
Expected: PASS with no packages to test yet.

- [ ] **Step 6: Commit**

```bash
git add . && git commit -m "chore: initialize the capture repository"
```

### Task 2: The transparent proxy

**Files:**
- Create: `../minecraft-capture/proxy/proxy.go`
- Create: `../minecraft-capture/proxy/proxy_test.go`
- Create: `../minecraft-capture/proxy/pump.go`

**Interfaces:**
- Consumes: `protocol.NewSession(Role, Limits)`, `protocol.Stream`,
  `login.NewAcceptor`, `login.Offline`.
- Produces:

```go
// Proxy accepts one client and relays it to one server, observing both
// directions.
type Proxy struct { /* unexported */ }

type Config struct {
    Listen    string        // host:port the client connects to
    Upstream  string        // host:port of the real server
    Observers []protocol.Observer
    Limits    protocol.Limits
}

func New(cfg Config) (*Proxy, error)
func (p *Proxy) Serve(ctx context.Context) error
```

**The proxy runs offline mode only, on both sides.** A client encrypts to
whatever it logged into, so the proxy terminates the client's login with its own
keypair and opens a second, separate login to the upstream server. Online mode
cannot survive that: the client's session token is bound to the server hash of
the box it authenticated against, and the upstream would reject the proxy's
join. State this in the package documentation rather than discovering it during
a capture session.

- [ ] **Step 1: Write the failing test**

```go
func TestTheProxyRelaysBothDirections(t *testing.T) {
    t.Parallel()

    upstream := fixture.NewServer(t)      // echoes every packet it receives
    p, err := proxy.New(proxy.Config{
        Listen:   "127.0.0.1:0",
        Upstream: upstream.Addr(),
        Limits:   mustLimits(t),
    })
    if err != nil {
        t.Fatalf("New: %v", err)
    }

    go func() { _ = p.Serve(t.Context()) }()

    client := fixture.NewClient(t, p.Addr())
    client.Login("tester")
    client.Send(handshakePacket(t))

    if got := upstream.Received(); len(got) == 0 {
        t.Fatal("upstream received nothing through the proxy")
    }
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `devbox run -- task test -- ./proxy`
Expected: FAIL, `undefined: proxy.New`.

- [ ] **Step 3: Implement accept and dial**

`Serve` listens, and for each accepted connection opens a `protocol.Stream` in
the server role toward the client and a second stream in the client role toward
upstream. Run `login.NewAcceptor` against the client half and `login.Negotiate`
with `login.NewOffline` against the upstream half. Only after both reach play
does the pump start.

- [ ] **Step 4: Implement the pump**

Two goroutines, one per direction, each reading a frame and writing it
unchanged. Both close when either side closes. Never decode to relay: the proxy
relays frames and decodes only for observation, so a packet this build cannot
parse still reaches the other side and still gets recorded as raw bytes.

- [ ] **Step 5: Run the test**

Run: `devbox run -- task test -- ./proxy`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add proxy && git commit -m "feat(proxy): relay one client to one server"
```

### Task 3: Recording both directions

**Files:**
- Create: `../minecraft-capture/record/sink.go`
- Create: `../minecraft-capture/record/sink_test.go`
- Modify: `../minecraft-capture/proxy/proxy.go`

**Interfaces:**
- Consumes: `capture.NewFileSink(path, header, options...)`, `capture.Header`,
  `capture.Record`, `protocol.Observation`.
- Produces:

```go
// Sink writes one recording holding both directions of one session.
func NewSink(path string, upstream string) (*Sink, error)
func (s *Sink) Observe(ctx context.Context, o protocol.Observation) error
func (s *Sink) Close() error
```

- [ ] **Step 1: Write the failing test**

```go
func TestARecordingKeepsDirectionAndOrder(t *testing.T) {
    t.Parallel()

    path := filepath.Join(t.TempDir(), "session.mccap")
    sink, err := record.NewSink(path, "example:25565")
    if err != nil {
        t.Fatalf("NewSink: %v", err)
    }

    for _, o := range []protocol.Observation{
        serverbound(t, "handshaking/set_protocol"),
        clientbound(t, "login/success"),
        serverbound(t, "play/position"),
    } {
        if err := sink.Observe(t.Context(), o); err != nil {
            t.Fatalf("Observe: %v", err)
        }
    }
    if err := sink.Close(); err != nil {
        t.Fatalf("Close: %v", err)
    }

    records := readAll(t, path)
    if len(records) != 3 {
        t.Fatalf("recorded %d records, want 3", len(records))
    }
    if records[1].Direction == records[0].Direction {
        t.Error("a clientbound record kept the serverbound direction")
    }
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `devbox run -- task test -- ./record`
Expected: FAIL, `undefined: record.NewSink`.

- [ ] **Step 3: Implement the sink**

Wrap `capture.NewFileSink` and stamp the header with the upstream address, the
protocol ID, and the capture tool's version. Direction comes from the
observation, not from which goroutine called: the pump has two, and attributing
by caller would make a refactor silently mislabel a whole recording.

- [ ] **Step 4: Wire it into the proxy**

Add the sink to `Config.Observers`. The proxy already relays frames without
decoding, so the sink sees every frame including the ones this build cannot
parse.

- [ ] **Step 5: Run the tests**

Run: `devbox run -- task test -- ./record ./proxy`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add record proxy && git commit -m "feat(record): write both directions to one capture"
```

### Task 4: Entity-trace extraction

**Files:**
- Create: `../minecraft-capture/trace/trace.go`
- Create: `../minecraft-capture/trace/extract.go`
- Create: `../minecraft-capture/trace/extract_test.go`

**Interfaces:**
- Consumes: `capture.Record`, the protocol 47 generated packet types.
- Produces:

```go
// Vec3 is a position or a velocity in blocks. It is declared here rather than
// imported from minecraft-simulation: capture is an oracle and must not depend
// on the thing it verifies, or a wrong constant in the kernel would be
// reproduced identically on both sides of the comparison.
type Vec3 struct{ X, Y, Z float64 }

// Trace is one entity's observed motion over one recording.
type Trace struct {
    EntityID int32
    Family   string    // "player", "item", "arrow"
    Samples  []Sample
}

// Sample is one observed position at one tick offset.
type Sample struct {
    Tick     uint64
    Position Vec3     // absolute, with relative moves already accumulated
    Velocity Vec3     // zero when the recording carried none
    OnGround bool
}

func Extract(records []capture.Record) ([]Trace, error)
```

Protocol 47 moves an entity with four packets — absolute teleport, relative
move, relative move with look, and look — and sends positions as fixed point in
thirty-seconds of a block. `Extract` accumulates relative moves onto the last
absolute position and records the absolute result, because a consumer comparing
a simulated trajectory needs positions, not deltas.

- [ ] **Step 1: Write the failing test**

```go
func TestRelativeMovesAccumulateOntoTheLastTeleport(t *testing.T) {
    t.Parallel()

    traces, err := trace.Extract([]capture.Record{
        spawnPlayer(t, 7, 100.0, 64.0, 200.0),
        relativeMove(t, 7, 0.5, 0, 0.25),
        relativeMove(t, 7, 0.5, 0, 0.25),
    })
    if err != nil {
        t.Fatalf("Extract: %v", err)
    }

    if len(traces) != 1 {
        t.Fatalf("extracted %d traces, want 1", len(traces))
    }
    last := traces[0].Samples[len(traces[0].Samples)-1]
    if math.Abs(last.Position.X-101.0) > 1.0/32 {
        t.Errorf("X accumulated to %.4f, want 101.0", last.Position.X)
    }
}

func TestAnUnknownEntityIsAFindingNotASilentDrop(t *testing.T) {
    t.Parallel()

    // A relative move for an entity that never spawned means the recording is
    // incomplete or the extractor is wrong. Both are worth an error: silently
    // starting a trace at the origin invents a trajectory nobody observed.
    if _, err := trace.Extract([]capture.Record{relativeMove(t, 99, 1, 0, 0)}); err == nil {
        t.Fatal("extracted a move for an entity that never spawned")
    }
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `devbox run -- task test -- ./trace`
Expected: FAIL, `undefined: trace.Extract`.

- [ ] **Step 3: Implement extraction**

Decode each record's packet, switch on its name, and maintain a map from entity
ID to the trace being built. Fixed-point positions divide by 32. Runtime IDs are
reused after a removal, so a spawn for an ID already present closes the previous
trace and starts a new one rather than appending to it.

- [ ] **Step 4: Run the tests**

Run: `devbox run -- task test -- ./trace`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add trace && git commit -m "feat(trace): extract absolute entity traces from a recording"
```

### Task 5: The deterministic replay gate

**Files:**
- Create: `../minecraft-capture/replaycheck/check.go`
- Create: `../minecraft-capture/replaycheck/check_test.go`

**Interfaces:**
- Consumes: `replay.Player`, `replay.WithMode`, `capture.NewDigester`.
- Produces:

```go
// Check replays a recording and reports whether it reproduced itself.
func Check(ctx context.Context, path string) (Result, error)

type Result struct {
    Digest      string
    Divergences []replay.Divergence
}
```

This is M9.1's gate. A recording that does not replay to the same digest is not
an oracle, and every later stage is judged against these files.

- [ ] **Step 1: Write the failing test**

```go
func TestARecordingReplaysToItsOwnDigest(t *testing.T) {
    t.Parallel()

    path := recordFixtureSession(t)   // drives the proxy against a fixture server

    first, err := replaycheck.Check(t.Context(), path)
    if err != nil {
        t.Fatalf("Check: %v", err)
    }
    if len(first.Divergences) != 0 {
        t.Fatalf("replay diverged: %v", first.Divergences)
    }

    second, err := replaycheck.Check(t.Context(), path)
    if err != nil {
        t.Fatalf("Check: %v", err)
    }
    if first.Digest != second.Digest {
        t.Errorf("two replays of one recording produced %s and %s", first.Digest, second.Digest)
    }
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `devbox run -- task test -- ./replaycheck`
Expected: FAIL, `undefined: replaycheck.Check`.

- [ ] **Step 3: Implement the check**

Open the recording, run `replay.Player` in the mode that compares each replayed
frame against the recorded one, and digest the result with `capture.Digester`.
Report divergences rather than failing on the first: a recording that diverges
in three places tells you more than one that stops at the first.

- [ ] **Step 4: Add the redaction test**

```go
func TestARecordingNeverHoldsTheKeyExchangeInTheClear(t *testing.T) {
    t.Parallel()

    // minecraft-protocol M5 found exactly this defect: the raw frame record is
    // written before the frame is decoded, so a packet-level redaction check
    // cannot answer for it. Through a proxy there are two logins, so there are
    // two chances to leak one.
    path := recordFixtureSession(t)

    raw, err := os.ReadFile(path)
    if err != nil {
        t.Fatalf("ReadFile: %v", err)
    }
    for _, secret := range []string{"encryption_begin", "shared_secret"} {
        if bytes.Contains(raw, []byte(secret)) {
            t.Errorf("the recording holds %q", secret)
        }
    }
}
```

- [ ] **Step 5: Run the tests**

Run: `devbox run -- task test -- ./replaycheck`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add replaycheck && git commit -m "feat(replaycheck): gate a recording on deterministic replay"
```

### Task 6: The command line

**Files:**
- Create: `../minecraft-capture/cmd/mccapture/main.go`
- Create: `../minecraft-capture/cmd/mccapture/main_test.go`
- Modify: `../minecraft-capture/Taskfile.yml`
- Modify: `../minecraft-capture/README.md`

**Interfaces:**
- Consumes: every package above.
- Produces: `mccapture proxy`, `mccapture trace`, and `mccapture verify`.

- [ ] **Step 1: Write the failing test**

```go
func TestVerifyExitsNonZeroOnADivergentRecording(t *testing.T) {
    t.Parallel()

    path := corruptOneFrame(t, recordFixtureSession(t))

    if code := run([]string{"verify", path}, io.Discard); code == 0 {
        t.Error("verify exited zero on a recording that cannot replay")
    }
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `devbox run -- task test -- ./cmd/mccapture`
Expected: FAIL.

- [ ] **Step 3: Implement the commands**

`proxy --listen --upstream --out` records a session. `trace --in --out` writes
extracted traces as JSON. `verify --in` runs the replay gate. Non-interactive,
documented exit codes, no prompts: this runs in CI and under an agent.

- [ ] **Step 4: Run the tests and the full gate**

Run: `devbox run -- task verify`
Expected: PASS.

- [ ] **Step 5: Capture one real session**

Run the proxy between a real 1.8.9 client and a pinned offline vanilla 1.8.9
server. Walk, sprint, jump, fall, drop an item, and shoot an arrow. Then run
`mccapture verify` on the recording.

Expected: zero divergences. This is the first evidence the oracle works, and no
automated test substitutes for it — every test above uses a fixture server whose
packets this repository generated.

Record the result in `docs/verification/`, including the server build, the
client version, and the digest.

- [ ] **Step 6: Commit**

```bash
git add . && git commit -m "feat(cmd): add the mccapture command line"
```

---

## Stages awaiting their prerequisite

The remaining stages are not written out as tasks here, and that is a decision
rather than an omission. The
[sequencing design](../../../minecraft-simulation/docs/superpowers/specs/2026-08-15-m8-m9-sequencing-design.md)
states it directly: each M9 stage earns a detailed plan when it becomes next,
"for the same reason M8.3's contracts are not specified in this document: the
information needed to write them does not exist yet."

What is missing is specific. M8.3 fixes `TickInput` and `TickResult` field names
and types. M8.4 fixes `MotionConstants` and the tick phase order. Until those
exist, a task here would name types that do not, and the plan would read as
authoritative while being invented — which is the failure mode this project has
already paid for once, in the shared-protocol extraction plan that named a
directory nobody could find.

Each stage below states what it delivers, what must exist first, and its gate.
Write its plan when its prerequisite lands.

| Stage | Delivers | Write its plan after | Gate |
| --- | --- | --- | --- |
| M9.2 | Dropped item and arrow rules, both profiles | M9.1 and M8.4 | Captured traces replay within one thirty-second of a block |
| M9.3 | Movement scenarios | M8.8 | Correction, teleport, and disconnect mid-action behave as vanilla |
| M9.4 | Digging and block breaking | M9.3 | Break times match vanilla across tool, block, and effect combinations |
| M9.5 | Building and placement | M9.4 | Placement legality and resulting block state match vanilla |
| M9.6 | Attack, damage, knockback | M9.3 | Reach validation, cooldown timing, damage, and death match vanilla |
| M9.7 | Containers and inventory | M9.5 | Window open and close, slot synchronisation, and rejected moves match vanilla |
| M9.8 | Crafting | M9.7 | Recipe matching and result stacks match vanilla, including the 2x2 grid |

Three findings from other milestones belong to stages in this table, recorded
here so they are not rediscovered:

- **One thirty-second of a block is the resolution, not a tolerance chosen for
  comfort.** Java Edition 1.8 transmits positions as fixed point, so a captured
  trace verifies to that precision and no further. It catches wrong constants
  and wrong axis order, not last-place drift. M9.2 inherits this.
- **`headless-minecraft` needs a respawn primitive and damage attribution.**
  Its interaction primitive list has no respawn, and its taxonomy has no event
  naming who dealt damage. M9.6 owns both, and
  [`examples/orbit`](../specs/2026-08-16-orbit-example-design.md) is blocked on
  them.
- **The 2x2 crafting matcher is already covered.** The M3 session findings asked
  whether M3's registry swap broke it; it did not, and the real defect was a
  shift-click handler that crafted once instead of draining the grid. Both are
  fixed and tested. M9.8 verifies against vanilla rather than re-litigating it.

## Risks

**The capture repository is new work, not a subcommand.** The parent design
budgeted 400 lines on the assumption that an existing proxy could be extended.
It cannot. Re-estimate M9 before scheduling it.

**Offline mode limits what can be captured.** The proxy terminates one login and
opens another, which online mode cannot survive. Any behaviour that differs
between online and offline mode is outside this oracle's reach, and nothing in
M9's stages is known to depend on it. If one turns out to, that stage needs a
different instrument, not a patched proxy.

**A fixture server proves the plumbing, not the protocol.** Every test in M9.1
except Task 6 Step 5 drives a server this project wrote, against packets this
project generated. A shared misunderstanding of protocol 47 passes all of them.
The live capture is the only step that can find one, which is why it is a step
and not a suggestion.
