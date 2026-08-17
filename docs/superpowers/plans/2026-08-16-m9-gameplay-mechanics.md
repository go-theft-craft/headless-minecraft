# M9 Gameplay Mechanics Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `subagent-driven-development` or execute this plan inline one task at a time. Keep every checkbox current.

**Goal:** Verify every gameplay mechanic — items, movement, digging, building, attack, containers, crafting — against captured vanilla behaviour, starting with the capture tool that makes the verification possible.

**Architecture:** M9 subdivides by mechanic because the simulation packages and the conformance fixtures are already organised that way and each mechanic is independently verifiable against a vanilla server. M9.1 builds the oracle: a protocol 47 proxy that sits between a real client and a real server, records both directions through `minecraft-protocol`'s capture format, and replays a recording deterministically. Every later stage is judged against traces that tool produces.

**Tech Stack:** Go 1.26.6 from `openserbia/go-flake`, Devbox, Task, `relay` v0.2.0 (proxy transport, `Sink`, hooks), `minecraft-protocol` v0.2.0 (`capture`, `replay`, `router`, `login`, `source`), `minecraft-simulation`'s kernel, and a pinned vanilla 1.8.9 server.

**Revised 2026-08-17.** M9.1 was written to create a new capture repository,
because when the sequencing design was approved no protocol-agnostic proxy
existed. `relay` v0.2.0 now does, with listeners, an accept loop, per-direction
pumps, a session registry, hooks, and a `Sink` carrying both decoded messages
and raw chunks — every piece Task 2 was going to hand-roll. Its
`examples/minecraft` already supplies the framer, the per-direction codec, and
a status-ping prober. M9.1 therefore grows that consumer into the oracle
instead of standing up a second repository and a second module path. What
remains is the work `relay` deliberately does not do: terminate the login on
both sides, write the capture format, extract traces, and gate on replay.

## Global Constraints

- Work in the repository each task names. M9.1 lands in `relay`; M9.2 through
  M9.8 land in `minecraft-simulation` with consumer changes in
  `headless-minecraft` and `server`.
- Run project commands as `devbox run -- task <name>`.
- Leave changes uncommitted unless explicitly requested.
- Recording is a `Sink`, not a logger, and it belongs to the proxy rather than
  to either endpoint. `server` keeps its operational `slog` output and gains no
  packet log: put the proxy in front of it when a packet log is wanted, and the
  same tool that captures vanilla behaviour also captures ours, in the same
  format, replayable the same way. A recorder built into one endpoint would
  only ever see that endpoint's view.
- The capture consumer speaks Java protocol 47 through `minecraft-protocol` and
  runs on `relay`'s transport. It is not built on the legacy proxy: that proxy
  speaks a different protocol family — one-byte packet identifiers, no VarInt
  framing, UCS-2 strings, and encryption in one direction only — and cannot
  capture Java Edition traces without being rewritten into a different program.
- Do not name the legacy proxy's project, its protocol, or its codename in any
  public repository, in any file, or in any commit message. Refer to it by role.
- Capture records what happened. It never synthesises a packet, never repairs a
  malformed one, and never reorders. A recording that cannot be replayed is a
  finding, not something to smooth over.
- Recordings hold player UUIDs, usernames, and chat. They are local runtime
  data, never committed. Add the recording directory to `relay`'s `.gitignore`
  in Task 1.
- Never record a login key exchange in the clear. `minecraft-protocol` M5 found
  this exact defect and fixed it; the capture sink inherits the fix and Task 5
  proves it still holds through a proxy.
- Do not tune against, detect, or evade anti-cheat. Captures come from servers
  the operator owns.

---

## Stage M9.1: the capture consumer

**Dependency:** `minecraft-protocol` M5 and `relay` v0.2.0, both complete. M9.1
does **not** depend on M8: capture is a protocol-level problem and needs no
kernel.

**Gate:** a captured trace replays deterministically from its recording.

**Where it lands.** `relay/examples/minecraft`, the module that already holds
the framer, the codec, and the prober, extended with a capture package and a
`--capture` flag on `mcrelay`. Nothing goes into `relay`'s core: the core is
dependency-free by construction and none of this is protocol-agnostic.

**Two uses, one tool.** In front of a pinned vanilla 1.8.9 server the proxy
produces the oracle traces M9.2 onwards are judged against. In front of our own
`server` it is the packet log that `server` therefore never has to grow — same
binary, same format, same replay. The second use is not the milestone's gate,
but it is why the recorder belongs in the proxy rather than in either endpoint.

### Task 1: The capture seam

**Files:**
- Create: `relay/examples/minecraft/capture/doc.go`
- Modify: `relay/.gitignore`

**Interfaces:**
- Consumes: nothing yet.
- Produces: the `capture` package inside the examples module, and the
  `devbox run -- task test:examples` entry point every later task runs.

No repository bootstrap: `relay` already pins the toolchain through
`openserbia/go-flake`, and `examples/` is already the module where every
third-party dependency lives. `minecraft-protocol` v0.2.0 is already required
there, so the capture format needs no new dependency either.

- [x] **Step 1: Ignore recordings**

Append to `relay/.gitignore`:

```gitignore
# Recordings hold player UUIDs, usernames, and chat. Local runtime data only.
/recordings/
*.mccap
```

- [x] **Step 2: Write the package documentation**

State in `capture/doc.go` what the oracle is for and what it must never do:
never synthesise a packet, never repair a malformed one, never reorder. A
recording that cannot replay is a finding.

- [x] **Step 3: Verify**

Run: `devbox run -- task verify`
Expected: PASS, unchanged from before the edit.

### Task 2: Follow the protocol's own transitions

**Files:**
- Modify: `relay/examples/minecraft/codec.go`
- Modify: `relay/examples/minecraft/codec_test.go`

**Interfaces:**
- Consumes: `protocol.Session.ProposeTransition`, `ValidateTransition`,
  `ApplyTransition`.
- Produces: a codec that stays in step with the connection through login,
  compression, and into play, so a recording carries packet identities rather
  than a session's worth of opaque bytes.

**Revised 2026-08-17, on finding that login termination is not the blocker.**
This task was written to terminate the client's login with the proxy's own
keypair and open a second offline login upstream. Reading the code first showed
that solves a problem M9.1 does not have. Vanilla only performs the key
exchange in online mode — `login.Acceptor.Accept` runs it "if the login is
online" — and M9.1 captures against an offline pinned server, which the plan
already required and which the milestone cannot avoid: the plan's own note says
an online upstream would reject the proxy's join anyway. Nothing encrypts, the
`ErrEncrypted` latch never fires, and the key exchange the task budgeted for
would have been dead code.

What actually stopped a capture from decoding was smaller and worse. The codec
hand-wrote its transitions and covered exactly one: the handshake. It never
followed login success into play and never applied the compression control, so
against a real server every frame after the login decoded against the wrong
state — and with vanilla's default threshold of 256 the compressed body was
read as a packet ID, yielding a plausible-looking unknown packet rather than an
error. A relay forwards all of it correctly and looks healthy. A capture taken
through it is worthless.

The fix is to stop hand-writing the rules. `protocol.Session` already reports
what a packet implies, and the triggers are version-specific in a way that
makes hand-writing them a standing bug: protocol 47 moves to play on the
clientbound login success, while 775 waits for a serverbound acknowledgement
and passes through a configuration state that 47 does not have. Asking is
shorter and correct on both.

- [x] **Step 1: Write the failing tests**

`TestCodecFollowsTheLoginIntoPlay`, pinned to protocol 47 because the packet
that ends a login is version-specific, and `TestCodecFollowsSetCompression`,
which is version-neutral because both protocols set compression with the same
clientbound login packet. Both drive a `serverPeer` that commits its own
transitions after each packet it encodes, so the bytes under test are the ones
a real endpoint would send rather than ones a test built to suit the codec.

Use a threshold of zero. Vanilla's 256 leaves small packets uncompressed and
lets an unapplied control pass by luck.

- [x] **Step 2: Run them to verify they fail**

Run: `devbox run -- task test:examples`
Expected: FAIL. `packet state = "login", want play`, and a descriptor of
`{ID:19 Name:}` — 0x13 is the first byte of the zlib envelope being read as a
packet ID, which is the silent-corruption mode this task exists to close.

- [x] **Step 3: Delegate to the protocol**

Replace the hand-written state machine with `ProposeTransition` on the session
that decoded, then `ValidateTransition` and `ApplyTransition` on both. Both
decoders take every transition: the pair tracks two directions of coding, not
two conversations, so a login that ends puts both peers in play and a
compression threshold covers every frame on the link. A proposal that errors is
ignored rather than fatal — the relay's job is to forward.

- [x] **Step 4: Keep the encryption latch**

The latch stays for the online case, where decoding genuinely cannot continue,
and the opaque path stays tested. A build that can only capture would be a
worse relay than the one that shipped.

- [x] **Step 5: Run the tests**

Run: `devbox run -- task verify`
Expected: PASS, core and examples both.

- [ ] **Step 6: Commit**

```bash
git add examples/minecraft && git commit -m "fix(minecraft): follow the protocol's transitions so captures decode"
```

**Still open, and deliberately not done here.** Terminating both logins remains
the only way to capture through an *online* server, and it is genuinely
unsolved rather than merely undone. It is not on M9.1's path: the oracle needs
a server whose behaviour is vanilla, not one whose accounts are verified.

### Task 3: Recording both directions

**Files:**
- Create: `relay/examples/minecraft/capture/sink.go`
- Create: `relay/examples/minecraft/capture/sink_test.go`
- Create: `relay/examples/minecraft/multisink.go`
- Create: `relay/examples/minecraft/multisink_test.go`
- Modify: `relay/examples/minecraft/cmd/mcrelay/main.go`
- Modify: `relay/examples/minecraft/proxy_test.go`

**Interfaces:**
- Consumes: `relay.Sink`, `relay.MessageRecord`, `capture.NewFileSink`,
  `capture.Header`, `protocol.Observation`, `protocol.SensitiveFrames`.
- Produces `capture.Recorder`, a `relay.Sink` writing one `.mccap` per session,
  and `minecraft.MultiSink`, which composes it with the existing store.

**Three things the written plan had wrong, all found in the code.**

`relay.Sink` is process-wide, not per-session: one sink serves every connection
and hands back a session identifier. So the shape is a recorder owning many
files, one per session, each with its own frame numbering and clock origin —
not the `NewSink(path, upstream)` this task specified.

`RawChunk` is the wrong source, despite being what the task named. A chunk is a
socket read and its boundaries fall wherever the kernel put them; recording
chunks as frames yields a file that reads back but cannot replay. The right
source is `Message`, which `Session.relay` calls for **every** frame, with a
zero descriptor when decoding failed — so an unparseable frame is recorded with
its bytes and no invented identity. `RawChunk` is implemented as a documented
no-op.

Redaction does not come for free. `minecraft-protocol` fixed the
key-exchange-in-the-clear defect inside its own stream, but a proxy assembles
observations by hand, so the recorder has to ask
`protocol.SensitiveFrames` itself. It asks in the last state a decoded packet
reported, so an opaque frame is judged in the state it arrived in rather than
assumed harmless.

- [x] **Step 1: Write the failing tests**

Direction and order, an undecodable frame surviving as a raw record, the header
naming protocol and upstream, one recording per session, and the key exchange
never landing in the clear. The redaction test asserts a record is *marked*
redacted as well as absent from the bytes — otherwise it passes just as well
when the frame was dropped.

- [x] **Step 2: Run them to verify they fail**

Run: `devbox run -- task test:examples -- ./minecraft/capture/`
Expected: FAIL, `undefined: capture.Recorder`.

- [x] **Step 3: Implement the recorder**

Every frame writes a raw record carrying the complete frame, prefix rebuilt
through the framer; a frame that decoded writes a packet record as well. That is
the pair a real endpoint's stream emits. A write failure stops that recording
and reports once: a recording missing a record in the middle is not evidence,
and appending past the gap hides where it is.

- [x] **Step 4: Wire it into mcrelay**

`-record <dir>` installs the recorder alongside the SQLite store through
`MultiSink`. It is a directory rather than a file because there is one recording
per session, and it is not `-capture` because that flag already means
`CaptureRaw`. A sink that fails to open drops out for that session rather than
failing the connection; every sink failing is reported.

- [x] **Step 5: Run the tests**

Run: `devbox run -- task verify`
Expected: PASS. `TestEndToEndRecording` drives a real status exchange through
the proxy and asserts the recording holds named packets — a file of the right
size full of unidentified frames is the failure Task 2 existed to close.

- [ ] **Step 6: Commit**

```bash
git add examples/minecraft && git commit -m "feat(capture): record both directions to a replayable file"
```

### Task 4: Entity-trace extraction

**Files:**
- Create: `relay/examples/minecraft/trace/trace.go`
- Create: `relay/examples/minecraft/trace/extract.go`
- Create: `relay/examples/minecraft/trace/extract_test.go`

**Interfaces:**
- Consumes: `capture.Record`, the protocol 47 generated packet types.
- Produces `Vec3`, `Sample`, `Trace`, `Extract(descriptor, limits, records)` and
  `ExtractFile(path)`.

**Two deviations from the written interface.** `Sample` carries the recording's
sequence number and elapsed time rather than a tick: a capture has no ticks in
it, and dividing elapsed time by fifty milliseconds would be a guess dressed as
a measurement. And `Extract` takes the descriptor and limits, because the
recording names its own protocol in its header — `ExtractFile` is the
convenience that reads both and calls through.

- [x] **Step 1: Write the failing tests**

Relative moves accumulating onto the last spawn; a teleport resetting rather
than accumulating; an unknown entity being a finding; a reused entity ID
starting a new trace; velocity not inventing a position; another protocol being
refused. Records are built by encoding through a real server-role session, so a
test cannot pass by agreeing with its own idea of the wire format.

- [x] **Step 2: Run them to verify they fail**

Run: `devbox run -- task test:examples -- ./minecraft/trace/`
Expected: FAIL, `undefined: trace.Extract`.

- [x] **Step 3: Implement extraction**

Decode each clientbound packet record, switch on the generated type, and keep a
map from entity ID to the trace being built. Fixed-point positions divide by 32
and velocities by 8000. A spawn for an ID already present closes the previous
trace: the server reuses runtime identifiers, and appending would splice two
trajectories into one nobody followed.

- [x] **Step 4: Run the tests**

Run: `devbox run -- task test:examples -- ./minecraft/trace/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add examples/minecraft/trace && git commit -m "feat(trace): extract absolute entity traces from a recording"
```

### Task 5: The deterministic replay gate

**Files:**
- Create: `relay/examples/minecraft/replaycheck/check.go`
- Create: `relay/examples/minecraft/replaycheck/check_test.go`

**Interfaces:**
- Consumes: `replay.Player`, `replay.WithMode`, `replay.WithResolver`,
  `capture.Reader.Trailer`.
- Produces `Check(ctx, path) (Result, error)`, with `Result.OK` and
  `Result.Explain`.

This is M9.1's gate. A recording that does not replay to the same digest is not
an oracle, and every later stage is judged against these files.

- [x] **Step 1: Write the failing test**

A recording replays to its own digest, and twice to the same one.

- [x] **Step 2: Run it to verify it fails**

Run: `devbox run -- task test:examples -- ./minecraft/replaycheck/`
Expected: FAIL, `undefined: replaycheck.Check`.

- [x] **Step 3: Implement the check**

Open the recording, run `replay.Player` in fast mode over it, and compare the
digest it produces with the one the trailer carries. A file that cannot be read
returns an error; a file that reads and disagrees returns a Result — a caller
that cannot tell those apart reports the wrong thing.

- [x] **Step 4: Add the rejection test**

Not the corruption test this step originally specified. Every record carries a
CRC, so a flipped byte fails the read long before the digest is consulted, and
a test that flipped one would be testing the checksum. The case that actually
happens is a proxy killed mid-session: records, no trailer, reads back
perfectly, and is not evidence.

The redaction test stays at the sink, where `TestARecordingNeverHoldsTheKey
ExchangeInTheClear` covers it. Through a proxy it would be vacuous: an offline
login exchanges no keys, and the proxy cannot stand between an online one.

- [x] **Step 5: Run the tests**

Run: `devbox run -- task test:examples -- ./minecraft/replaycheck/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add examples/minecraft/replaycheck && git commit -m "feat(replaycheck): gate a recording on deterministic replay"
```

### Task 6: The command line

**Files:**
- Modify: `relay/examples/minecraft/cmd/mcrelay/main.go`
- Create: `relay/examples/minecraft/cmd/mcrelay/main_test.go`
- Modify: `relay/examples/README.md`
- Create: `relay/docs/verification/2026-08-17-capture-oracle.md`

**Interfaces:**
- Produces `mcrelay trace` and `mcrelay verify` alongside the relaying mode,
  which `-record` now records.

- [x] **Step 1: Write the failing test**

`verify` exits non-zero on a recording that cannot replay, and zero on one that
can; `trace` writes trajectories whose last position is where the moves put it.
Tests drive `dispatch(args, stdout, stderr) int` so the exit code is the thing
under test.

- [x] **Step 2: Run them to verify they fail**

Run: `devbox run -- task test:examples -- ./minecraft/cmd/...`
Expected: FAIL.

- [x] **Step 3: Implement the commands**

Subcommand dispatch ahead of the relay flags, each mode owning its own
`FlagSet` and usage text. Non-interactive, documented exit codes, no prompts.

- [x] **Step 4: Run the tests and the full gate**

Run: `devbox run -- task verify`
Expected: PASS, core and examples.

- [ ] **Step 5: Capture one real session**

**Not run — this needs a real client and a pinned vanilla server, which no
automated step can stand in for.** The procedure is written out in
`relay/docs/verification/2026-08-17-capture-oracle.md`, including the flag that
is easy to miss: `-protocol java/1.8.9`, because the default is 775 and a 47
session recorded under a 775 header will not replay.

Until this runs, M9.1's gate is met against a stub upstream whose packets this
repository generated, which is exactly the agreement an oracle cannot rely on.

- [ ] **Step 6: Commit**

```bash
git add . && git commit -m "feat(mcrelay): add trace extraction and the replay gate"
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
