# Go Theft Craft master plan

Last reviewed: 2026-08-18.

This file is the cross-repository source of truth for **what remains**. The
narrative record of what each finished milestone found has been moved to
[the 2026-08-18 archive](docs/archive/2026-08-18-master-plan.md); nothing in it
is deleted, and a finding worth acting on is restated here as an open item.
Detailed designs and implementation plans stay in their owning repositories.

Scope: the six public repositories — `headless-minecraft`,
`minecraft-protocol`, `minecraft-simulation`, `minecraft-reference`, `relay`,
and `server`. `mcgl-proxy` and `mcgl-research` are private and are not tracked
here.

## Status legend

| Status | Meaning |
| --- | --- |
| Complete | Implemented and verified in the repository named by the milestone. |
| Client checks pending | Implemented, with every automated gate green, but a manual check its release gate requires has not been run. |
| In progress | Some tasks of its plan are done and gated; the rest are not. |
| Next | Approved and ready to implement. |
| Planned | Ordered, but its focused design or implementation plan is not yet approved. |
| Blocked | Cannot start until the named dependency is complete. |

## Milestone tracker

| ID | Deliverable | Owner | Status |
| --- | --- | --- | --- |
| M0 | Shared contracts, bounded Java wire primitives, immutable game data, generated Java 1.8 data, protocol 47 codecs | `minecraft-protocol` | Complete |
| M1 | Managed stream, compression, bounded pipelines, legacy `FE 01`, graceful shutdown | `minecraft-protocol` | Complete |
| M2 | AES-CFB8 encryption and the developer-controllable login lifecycle | `minecraft-protocol` | Complete |
| M2.5 | Schema-first code generation | `minecraft-protocol` | Complete |
| M3 | First migrated connection path: handshake, status, ping, login, compression | `server` | Complete |
| M4 | Java 26.1 data and protocol 775 codecs | `minecraft-protocol` | Complete |
| M5 | Routing, capture history, replay, `mcproto` CLI | `minecraft-protocol` | Complete |
| M6 | Finish the server migration and connect the headless client | `server`, `headless-minecraft` | Complete except **M6.4** (Microsoft device-code), postponed |
| M7 | Immutable observed world state, wire-ordered reducers | `headless-minecraft` | Complete |
| M8 | Deterministic 1.8.9 and 26.1.2 movement kernel, replay, consumer integration | `minecraft-simulation` | Complete (M8.1–M8.8) |
| M9 | Gameplay mechanics, verified against both versions | `minecraft-simulation`, `relay`, `headless-minecraft`, `server` | **In progress**: M9.1 complete, live check run 2026-08-17; M9.1b, M9.2, M9.4, M9.5, M9.6 and M9.7 complete; M9.3 blocked on a human capture; M9.8 not started |
| M10 | Conformance, compatibility contracts, migration notes, `v1.0.0` | all runtime repositories | **In progress**: reconciled 2026-08-18, task 1 of six done |
| P4 | Put every consumer on the released `minecraft-protocol` and keep them there | `minecraft-protocol` | Complete |
| M11 | Turn `server` into a framework | `server` | Complete (M11.1–M11.7) |
| — | Navigation and behaviour pillar | `minecraft-simulation`, `headless-minecraft` | **Complete in code, pending releases**: all four plans implemented and gated 2026-08-18; three tags and two `go.mod` bumps are what remain |

---

## Unfinished work

Each item names the plan that owns it and the evidence its state was read from.

### `relay`

Both questions this repository was carrying are **closed**, and the two lines
below are what is left of them.

- [ ] **Record one online-mode session against a real server.** Step 8 of
  `../relay/docs/verification/2026-08-17-capture-oracle.md`, added after the
  2026-08-17 run and optional: every other step has a result. It needs a paid
  account, which is the same thing M6.4 and M10's online-mode lane need, so it
  is cheapest to do all three in one sitting.
- Closed, 2026-08-17: **M9.1's live check ran.** A real vanilla 1.8.9 server and
  a real client, and it earned its keep — a capture from a server with
  compression on did not replay, because the frame enabling compression was
  withheld as though it were key material. Fixed, and the fix confirmed on the
  wire. Recordings that predate the fix cannot be repaired and are the only
  artifact of the defect.
- Closed, 2026-08-18: **the `Sink` contract is enforced rather than stated.**
  `sinkpump.go` gives every session one bounded queue and one goroutine, with
  `SinkOverflowBlock`, `Drop`, and `EndSession` policies, and the choice between
  the core's queue and the recorder's own was settled by running both in front
  of a real server onto a deliberately slow disk
  (`../relay/docs/verification/2026-08-18-sink-policy-live.md`): the recorder
  keeps its queue, and the core's stays out of the capture path.
  `../relay/docs/2026-08-17-after-the-plan.md` is the record of the problem, not
  of the state.

  **The limit that run found is worth carrying:** `mcrelay verify` digests what
  was written rather than what crossed the wire, so a recording missing 16% of
  its records passed the gate. Dropping is therefore never the policy in front
  of a recorder — but nothing in the format can say a frame is missing, and no
  milestone owns changing that.

### `headless-minecraft`

- [ ] **M6.4 — Microsoft device-code authentication.** Plan ready and every
  prerequisite met; postponed, not blocked
  ([plan](docs/superpowers/plans/2026-08-15-microsoft-authentication.md)).
  It gates M10's online-mode lane: every headless check since M3 has run
  offline.
- [ ] **M9.3 — movement scenarios: blocked on a person, not on an agent**
  ([plan](docs/superpowers/plans/2026-08-17-m9-3-movement-scenarios.md)).
  Tasks 5–7 — the correction, the teleport, and the disconnect, which are this
  file's stated gate for M9.3 — are done. What is open is Tasks 1–4, and Task 3
  cannot be run by an agent: a player trace is built from what the *client*
  reported, so a corpus captured with this repository's headless client is this
  repository's own physics played back to itself and would pass by construction.
  The oracle is a real vanilla client behind the proxy. For 1.8.9 those
  recordings exist in `oracle-evidence`; for 26.1.2 none does, and taking one
  needs a person playing 26.1.2 through the proxy with a paid account — the same
  thing M6.4 and M10's online-mode lane need. Tasks 1 and 2 exist only to serve
  3 and 4, and the plan says freezing a document format nobody reads yet would
  pin whatever its one writer happens to do.
- Closed, 2026-08-18: **M9.4 — digging and block breaking**
  ([plan](docs/superpowers/plans/2026-08-17-m9-4-digging-block-breaking.md)).
  The matrix gate runs on both versions from a corpus asked of each version's
  own jar (`minecraft-simulation` `44339e8`), because a capture through the
  proxy measures a server's leniency rather than a client's break time — the
  plan's Task 5 records that change and what the matrix found. Two things it
  found are open elsewhere: both versions' generated data gets shears wrong
  against leaves and wool, pinned per version and reported below; and nothing
  in this repository has yet sent the three dig packets to a real server in
  order, which is M10's anti-cheat lane rather than a stage of M9.
- Closed, 2026-08-18: **M9.5 — building and placement**
  ([plan](docs/superpowers/plans/2026-08-17-m9-5-building-and-placement.md)).
  All six tasks, gated on both jars: 24 clicks per version answered by
  `Block.onBlockPlaced` and `Block.getStateForPlacement`, compared on the handle.
  The stage's own record says what it found; the two things that outlive it are
  below, under `minecraft-simulation` and here:
  **1.8.9's handles now name a block state rather than a block** (sixteen per
  block, a name still resolving metadata zero), which is what gave a placement
  somewhere to put its answer and fixed a top slab colliding as a bottom one.
- Closed, 2026-08-18: **M9.6 — attack, damage, knockback**
  ([plan](docs/superpowers/plans/2026-08-17-m9-6-attack-damage-knockback.md)).
  The combat rules landed in `minecraft-simulation/combat`, gated on a two-jar
  corpus asked of each version's own jar, the same route M9.4 took: 1.8.9
  answers full strikes and matches bit for bit outside the sine table's yaw
  quantisation; 26.1.2 answers its charge curve exactly and its base impulse
  within the width difference, because its hurt path demands a real
  `ServerLevel` — the corpus's `Dropped` list states the smaller claim.
  `client.Attack` refuses out-of-reach swings before the wire; the respawn
  guard refuses a living player's respawn version-neutrally. The live
  scenarios on both jars found that a 1.8.9 server syncs health only from
  inside the handler the client's own idle reports drive — a silent client is
  never told it died. `examples/orbit` fought, died, respawned, and returned
  to its circle on a live 26.1.2 server; on 1.8.9 it orbits and cannot be
  provoked, because protocol 47 names no attacker and orbit infers none —
  which is `event/damage.go`'s recorded limit, not a gap. This entry used to
  say M9.6 owned damage attribution and respawn; both had landed earlier —
  attribution under M7, the respawn action with M8.8's follow-on — and what
  M9.6 owned was the combat numbers and the scenarios that prove them.
- Closed, 2026-08-18: **M9.7 — containers and inventory**
  ([plan](docs/superpowers/plans/2026-08-17-m9-7-containers-and-inventory.md)).
  The audit answered first, as the plan demanded, and its failure was the
  deliverable: the 26.1 window dataset is unusable — protocol 775 numbers
  menus into the game's built-in registry, which no session defines and no
  pinned data resolves, and the aliased 1.16.1-era records are keyed by names
  no packet mentions ([the audit](docs/verification/2026-08-18-window-data-audit.md),
  pinned by tests that fail the day corrected data lands; the fix to schedule
  is a real 26.1 menu registry in `minecraft-protocol`, keyed in registry
  order). The stage then shipped what the two protocols let an honest client
  do: a rejected click rolls back to its pre-click snapshot — slots and
  cursor, and everything predicted on top of it — because 47's rejection
  carries no state; each version confirms through its own mechanism, 47 by
  transaction echo with the required apology, 775 superseded by the full
  resend; and the live referee agreed on both jars. **What stays open, by
  choice rather than omission:** on 47 the client refuses quick-move, drag,
  and same-item merges — it cannot predict them without window-layout and
  stack-identity data, and a 1.8.9 server announces nothing after an accepted
  click — so those land when the data correction does; window types beyond
  the chest are exercised only by the audit.
- [ ] **M9.8 — crafting**
  ([plan](docs/superpowers/plans/2026-08-17-m9-8-crafting.md)). Only the
  reconcile task is done; tasks 1–5 are open.
Closed, 2026-08-18: **the flake read as "the hangup fixture closes while the
client is still writing".** It was not the fixture. The client acknowledged its
placement, the server received the acknowledgement and hung up, and the client's
own `Write` reported the frame the server already had as refused — so `Connect`
failed on a session the server had seen through. Three windows in
`minecraft-protocol`, closed across `v0.7.1` and `v0.7.2`: an observation that
could not be charged to a budget closing with the transport, a write pump that
dropped the outcome of a frame it had written, and — the one that survived
`v0.7.1` — a coordinator that gave up on the stop while the frame was still
inside the transport call. Bytes reach a peer before the call that sent them
returns, so "no result yet" never meant "nothing was sent".

Closed, 2026-08-18: **`Connect` reporting a placed session as one that never
was.** `awaitReady` selected between the readiness signal and the loop ending,
both ready at once when a server places the player and kicks straight after, and
a select picks at random — so half of those sessions were reported as never
placed, having reached play with the server's reason for leaving already on the
subscription. The same class as the stream defect below, one layer up, and the
same fix: take readiness first. Fixed in `ce6d594`, which also stops the message
rendering a nil loop error as `%!w(<nil>)`.

That commit also moved the state a disconnect names off the observation path,
which was the flaw in the first repair: observations may drop what they have not
delivered when the transport goes, so a disconnect could name the state before
the one the session was in. The read loop records the state each packet arrives
in, which needs nothing that can be gone by then.

Measured, 600 sessions of each of the three teardown tests per arm, interleaved
so both arms saw the same machine: thirty-two failures across four kinds on the
commit that took `v0.7.0`, none on `v0.7.2` with these fixes. That lane has no
known flake left.

Closed, 2026-08-18: **a kicked session reporting the state it ended in as
`unknown`.** Both halves of it, and neither was where the first reading put it.

The kick was being lost, in `minecraft-protocol`: a peer that kicks writes its
disconnect and closes, so the frame and the EOF behind it arrive together, and
the stream stopped — closing the shared budget — while its coordinator still
held the decoded packet. The packet's observation record could not be charged to
a closed budget, that counted as a decode failure, and the packet went with it.
Fixed in `2dcda29` with two regression tests that fail on the code they replace;
this client picks it up with the next release, and until then a lost kick reads
as a transport loss rather than as a session that ended in no state.

The `"unknown"` was this repository's, and it was not only the kick's: the state
was read back off the stream as the ending was reported, and a terminated stream
answers nothing, so every transport loss published it. The client records the
transitions it already observes and reports the last one (`212b160`).

Measured: three failures in eight hundred runs under load before, none in eight
hundred after, with the same load in the same session.

- [ ] **Export `movement.Strategy`** so an application can implement one
  (task 7 of [the world-state plan](docs/superpowers/plans/2026-08-13-world-state-actions.md)).
  Controller-owned strategy switching ships bunnyhop; nothing yet proves a
  strategy defined outside the library works, and `examples/orbit` is the first
  caller to need it.
- [x] **Navigation edge completion**
  ([plan](docs/superpowers/plans/2026-08-18-navigation-edge-completion.md),
  7 tasks, all done 2026-08-18). `EdgeJumpGap`, `EdgeWaterDrop`, `EdgeClimb`,
  and `EdgeDoor` join the four read-only edges, with `PostureFall`,
  `PostureSneak`, and `PostureCrawl`. The jump reach is measured by running each
  profile's own kernel rather than tabulated (`navigation/reach`, and the
  package doc carries the four figures with their date). Crawl is the first
  behaviour 26.1.2 has and 1.8.9 does not, which the `navigation` package doc
  states as the version asymmetry it is.

  Task 5 was recorded here as blocked on the climbable-block property, and that
  was aimed at the wrong thing: `EdgeClimb` reads `terrain.Facts.Climbable`,
  which the caller supplies, so the edge shipped without the release. What is
  still true is that no profile can answer it — `minecraft-simulation` requires
  `minecraft-protocol` v0.6.0 and only test doubles implement `Climbable`, so a
  real caller ships its own ladder list until the release below lands.
- [x] **Mutating edges and pillar**
  ([plan](docs/superpowers/plans/2026-08-18-mutating-edges-pillar.md),
  6 tasks; 1 and 3–6 done 2026-08-18, task 2 is the release below). Task 1
  landed as `minecraft-protocol` `c6557d1`: falling and climbable are measured
  out of the pinned jars into `BlockMovementRegistry.FallsByState` and
  `ClimbableByState`, rather than onto `data.Block` as both designs first said —
  upstream publishes neither property, and a measured fact belongs with the
  dataset whose manifest records the jar's digest.

  `Overlay` (`a1304fd`), then `EdgePlace` and `EdgePillar` with the
  re-run-and-ban validation loop, the vertical envelope, the per-column pillar
  limit, and the recomputed heuristic (`774fb6f`). `EdgePillar` is the edge the
  parent design's list had no member for: `Place` bridges horizontally and
  nothing gained height, so a body with a stack of blocks was capped by its step
  height exactly as a body with none.

  The heuristic fix is the one worth remembering. The floor was computed over
  the movement edges alone, so the moment a body could place, jump, or climb
  more cheaply per block than it could walk, A* stopped returning shortest
  routes — and the symptom would have been paths that are merely suboptimal.
  A property test over random capabilities with random subsets enabled is what
  catches it, and it was checked by breaking the floor and watching it fail.

  `Dig`, `Support`, and `Collapse` stay deferred as the design says: the first
  needs M9.4's break times and the other two M9.5's placement legality and a
  falling-column trace.
- [x] **Aiming and reach geometry**
  ([plan](docs/superpowers/plans/2026-08-18-aiming-and-reach-geometry.md),
  7 tasks; 1–4 and 6–7 done 2026-08-18, task 5 is the release below).
  `AABB.Nearest`, `AABB.Reaches`, `Vec3.Pitch`, `Vec3.Look`, `Behind`, `Lead`,
  `Tangent`, and `Away` are in `minecraft-simulation/geom` (`57a0b56`), which
  still imports nothing outside the standard library. Reach is measured to a
  box's nearest point rather than its centre, because that is what the game
  measures and a client using the centre refuses hits the server accepts.

  `examples/orbit` defines no `Vec3`, no `BlockPos`, and none of the arithmetic
  around them (`7cc675d`), and `Sender.Step` sends a computed pitch where it
  sent the literal `0`. Its behaviour is unchanged — it still looks level
  walking a flat circle — which is what makes the change safe to have made. Its
  `Facts` now answers `Climbable` from the measurement rather than from a list,
  which is the extraction paying off end to end.
- [x] **Composed behaviours**
  ([plan](docs/superpowers/plans/2026-08-18-composed-behaviours.md), 6 tasks,
  done 2026-08-18, `893b99e`). The `behaviour` package: `Follow`, `Flee`, `Eat`,
  `Block`, `Dig`, `Build`, `Fish`, and `Sequence`, all asked once per tick and
  none driving. Scopes are checked at construction. Nothing in the client's
  required path imports it.

  The reviewer question the design flagged is settled: attack gets
  `safety.ScopeAttack` of its own rather than reusing `ScopeInteract`. The two
  are one action on the wire, and the split is a safety decision — an
  authorization that permits opening a chest is not obviously one that permits
  attacking a player.

  `Fish` is written and is not claimed to work. It refuses to construct without
  a bite detector the caller supplies, and its gate is skipped with the reason
  recorded; see the open item below.
- [x] **The pillar's releases.** Done 2026-08-18. The pillar spans three
  repositories and every consumer resolves the others through a released tag, so
  the code being complete was never the same thing as the pillar being usable.
  The whole chain:

  1. `minecraft-reference` — `e78ac4f` carries the two extended jar dumpers.
     Pushed; no new tag cut, and nothing yet needs one.
  2. `minecraft-protocol` v0.7.0 — tagged, pushed, and served by the proxy.
     Every consumer in its `RELEASING.md` table requires it:
     `minecraft-simulation` `84d882e`, `server` `74a865b`, `relay` examples
     `6f51cae`, and this repository `4cdd79e`.
  3. `minecraft-simulation` v0.2.0 — navigation, geom, and placement. Minor
     rather than patch because a 1.8.9 handle now names a block state instead of
     a block.
  4. `headless-minecraft` `go.mod` and `examples/go.mod` bumps to it.

  **What this closed was a red `main`, not a formality.** Every target in
  `Taskfile.yml` runs `GOWORK=off`, deliberately, so a stale pin cannot hide
  behind the gitignored `go.work` — and it was not hiding: `task test`, `task
  verify`, and `task build` failed from the moment `behaviour` landed, with
  `undefined: navigation.EdgePlace`, `EdgePillar`, `EdgeJumpGap`,
  `EdgeWaterDrop`, `EdgeClimb`, `EdgeDoor`, and `Vec3.HorizontalDistance` and
  `Toward` undefined. The aiming plan told task 6 not to start before the tag
  existed, in these words: `headless-minecraft` is public and a `replace` in it
  is not acceptable. A `go.work` was used instead, which is the same hazard
  under another name. `task verify` passes whole again.
- [ ] **`Fish` has no measured bite detector.** No packet in either protocol
  says a fish bit; what a client reads is the bobber's motion, and how much
  motion counts as a dip is a measurement. The behaviour ships with the detector
  behind an interface, refuses to construct without one, and its gate is skipped
  with the reason recorded. It needs a captured trace per version. Both capture
  lanes have run, on 2026-08-17, so what is missing is a recorded session with a
  rod in it rather than an instrument to record it with.
- [ ] **Make the end-to-end lane drive `examples/observe`.** The convention is
  that examples are the integration surface; `client/world_e2e_test.go` mimics
  what `examples/observe` subscribes to rather than driving it. Assigned to
  M8.8, which has since closed — confirm whether it was done or dropped.

### `minecraft-protocol` and M10

The 2026-08-18 M10 reconciliation
([plan](docs/superpowers/plans/2026-08-18-m10-conformance-and-releases.md),
**uncommitted at the time of this review**) checked all nine M10 checklist items
against the working trees and found four already satisfied elsewhere and one
measurably impossible. Its six tasks, none executed:

- [x] **Task 1 — teach `login.Acceptor` protocol 775.** Landed 2026-08-18
  (`minecraft-protocol` `b644bb4`): the acceptor drives both logins from the
  exchange each version declares, with `login_exchange.go` generated for `v1_8`
  and `v26_1` and an acceptor test per version. This was the only code gap M10
  owned outright, and three things it blocked are now unblocked: the matrix row
  for the owned Go server at 775, M11.7's brigadier rendering reaching a client,
  and the headless end-to-end lane covering more than protocol 47.
- [ ] **Task 2 — settle the advertised version string.** Already reconciled in
  code (`"1.8.9"` in the data, `"1.8.8"` advertised and pinned by a test, and
  Node 1.66.2 agrees); the decision itself is unwritten.
- [ ] **Task 3 — give `server` a release gate.** It is public and has no
  `.github/`, no `verify`, `release:check`, `fmt:check`, `secrets`, or `vuln`
  task, no `CHANGELOG.md`, and no tags — while both compatibility matrices are
  driven against it.
- [ ] **Task 4 — stop the vanilla lane reaching into a sibling repository.**
  The 26.1.2 jar is read from `minecraft-simulation`'s gitignored workspace by
  relative path.
- [ ] **Task 5 — freeze the public surface** with `apidiff` before M9.4–M9.8
  move it. No `apidiff`, no `api/` directory, and no `MIGRATION.md` in any of
  the six repositories.
- [ ] **Task 6 — restate M10 as what the reconciliation found.**

Closed, 2026-08-18: **P4's uptake half**
([plan](../minecraft-protocol/docs/superpowers/plans/2026-08-18-p4-shared-consumers.md),
4 of 4 tasks). The three migrations P4 named were done or superseded; what was
left was that a consumer can sit a release behind while a Go workspace makes it
look current, which is how one shipped against a defect `minecraft-protocol` had
already fixed. `headless-minecraft` takes `v0.6.0` in both modules and pins the
corrected velocity to bytes; every lane its `verify` runs now resolves modules
with `GOWORK=off`, so reverting the pin fails locally the way it would in CI; and
`minecraft-protocol`'s `RELEASING.md` names the five consumer modules a release
is not finished without.

Still owned by M10 and outside that plan:

- [ ] **Run at least one online-mode lane**, which is what finally picks up
  M6.4.
- [ ] **Compatibility matrices** beyond the Node-at-47 gate: Paper is opt-in and
  manual, Node at 775 is codec-level only (1.66.2's supported versions end at
  1.21.11), and the owned Go server has no lane at either version.
- [ ] **Community-server cases** as black-box scenarios — not started, and the
  reconciliation argues the existing jar-derived lanes already buy the property
  the conversion was for. Decide explicitly rather than leaving it pending.
- [ ] **Publish stable `v1.0.0` releases** only after every release gate passes.
- [ ] **Measure play-state limits on 775.** M4 measured login only and recorded
  that no milestone may claim the 2 MiB frame and 8 MiB body defaults fit play
  until play is measured. M9.1b gave the first numbers from a real 26.1.2
  server; the roadmap still says play is not measured.

### `minecraft-simulation`

- [ ] **Measure which blocks a placement replaces.** M9.5 needs it and no
  version's generated data carries it: air is replaceable and the tri-state view
  answers that much, while water, lava, tall grass, snow layers, and fire are
  replaceable too and nothing says so. `placement.Replaceables` is the seam and
  nothing implements it, so a placement against tall grass lands one cell high.
  The route is the one `minecraft-protocol` took for falling and climbable on
  2026-08-18 — measure it out of the pinned jars into the dataset — rather than
  a list of block names typed into a profile.
- [ ] **Get the shears tool speeds corrected upstream.** M9.4's matrix found
  that *both* versions' generated data gives shears the wrong speed against
  leaves and wool — 1.8.9 says 6 and 4.8, 26.1 says 1 and 1, and both jars say
  15 and 5. Until the data is fixed, the break times this project computes for
  shears on those blocks are wrong, and no test on this side can make them
  right. Pinned by `TestTheDatasetToolSpeedsThisVersionGetsWrong` in each
  profile and carried as a declared divergence in
  `mining/testdata/vanilla/*.json`, so a correction makes the gate fail rather
  than passing unnoticed. The same test records 26.1's copper tools, which are
  wrong for the same kind of reason.
- [ ] **Replace the 26.1 item fixture.** M9.2's 26.1 item lane rests on an item
  this repository's own client dropped, whose start position was accumulated
  from relative moves and whose start velocity the server stated at a different
  instant. A summoned 26.1 item, captured the way the 1.8.9 ones were, would
  replace it.
- [ ] **Capture 775 from a real vanilla client.** M9.1b's capture was taken with
  this repository's own headless client, so the packet mix a real 26.1.2 client
  sends is unexercised. The wire format itself comes from the server's frames
  and is not in doubt.
- [ ] **Give blocks a landing rule.** An item landing on slime disagrees with
  the game by the whole slime bounce, because no block in the table has a
  landing rule — the dataset publishes none.
- Resolved since the last review: consuming a **released** `minecraft-reference`
  rather than `main`. `minecraft-simulation/Taskfile.yml` runs
  `mcreference@{{.MCREFERENCE_VERSION}}`.

### `server`

- [ ] **Obtain a vanilla-written region fixture.** Task 2 step 1 of
  [the M11.3 storage plan](../server/docs/superpowers/plans/2026-08-17-m11-3-storage.md)
  is the one open step in an otherwise complete M11. Two tests skip without it:
  `TestAVanillaRegionReads` and `TestAVanillaChestReads` in
  `pkg/world/anvil/read_test.go`. It needs a Mojang server jar and a running
  world, which is why it cannot be done from inside the repository.
- [ ] **The circling bot example** (`../server/docs/todo.md`): a headless bot
  that circles spawn at r=25 jumping, routes around obstacles, fights back when
  attacked and resumes, respawns after death, and — if it cannot escape a trap —
  stands still and runs `/kill` once every two minutes. It needs navigation
  (edge completion, mutating edges), aiming, and composed behaviours, so it is a
  natural acceptance test for the whole pillar rather than a separate task.
- Also see M10 Task 3 above: `server` is the one public repository with no
  release gate.

### `minecraft-reference`

Nothing open. The family catalog, the three naming strategies, the maintenance
command, the weekly workflow, and the compatibility gate all shipped; the
README's generated table lists a tested representative per family from 1.0
through 26.2, and the repository is released at `v1.0.1`.

---

## Plans whose checkboxes were stale

Fixed 2026-08-18. Each of these shipped with unticked steps, because it was
executed in another repository under another plan, and anyone reading the boxes
concluded the opposite. The boxes are now ticked by outcome, checked against the
working trees, rather than as a record that each step ran as written; where a
later stage plan did the work differently, that plan is the record of how. Every
one still opens with a status header saying what shipped and where.

| Plan | Boxes | State |
| --- | --- | --- |
| `headless-minecraft` shared-protocol extraction, stream toolkit, immutable data contracts, reference extraction, simulation foundation | 39/39, 41/41, 26/26, 46/46, 62/62 | M0, M2, M8.1 and M8 are complete in the owning repositories |
| `headless-minecraft` world-state and actions | 31 of 56 | Tasks 1–6 shipped as M7. **Tasks 7 through 11 are open**; task 7, `movement.Strategy`, is listed above, and 8 through 11 are now M9.4, M9.5, M9.7, and M9.8 |
| `headless-minecraft` headless-client authentication | 24 of 35 | M6.3 shipped. Tasks 3, 4, and 5 are open: `auth` has no token storage at all, and the device-code half is M6.4, listed above |
| `headless-minecraft` M9 gameplay umbrella | 32/32 | M9.1's live check ran 2026-08-17 and found the compression defect; M9.2 onward have their own stage plans |
| `headless-minecraft` M11 server framework umbrella | 21/21 | M11.1–M11.7 complete in `server` |
| `headless-minecraft` navigation terrain and search | 54/54 | `minecraft-simulation/navigation` carries `search.go`, `frontier.go`, `edge.go`, and the four read-only edge kinds |
| `headless-minecraft` M10 conformance and releases (2026-08-16) | 0 of 33, deliberately | Superseded by the 2026-08-18 reconciliation above. Nothing in it ran, so nothing in it is ticked |
| `minecraft-protocol` protocol 47 codecs, managed stream, encryption lifecycle, schema-first codegen | 27/27, 97/97, 85/85, 27/27 | M0, M1, M2, M2.5 complete; the roadmap's "modern-login transitions" bullet was closed by M4 |
| `minecraft-protocol` relay proxy framework | 0 of 106, deliberately | The copy here is the draft. The executed copy lives in `relay` with every step ticked, and three later documents there amend it |
| `minecraft-protocol` routing, capture, replay, CLI | 51 of 52 | Its one open step is marked "not done, deliberately" |
| `server` protocol migration, play migration, M11.1 framework shape | 106/106, 44/44, 45/45 | M3, M6.1, and M11.1 are complete — `pkg/protocol` and `pkg/gamedata` no longer exist. Confirm nothing was left behind when M10 Task 5 freezes the surface |
| `minecraft-simulation` M8.1 ground truth, M8.2 geometry and collision | 51/51, 57/57 | Both complete and gated against a real server jar |
| `minecraft-reference` standalone release, stable family support | 16/16, 51/51 | The repository is standalone, released, and its generated support table is populated |

---

## Repository conventions

`server`, `headless-minecraft`, `minecraft-protocol`, and `minecraft-simulation`
are frameworks. Applications are not what they ship; composable pieces are.

Every one of them carries an `examples/` directory that binds its pieces
together into runnable programs, and `examples/` is its own Go module. The
library keeps the dependency list its plan declares, examples pull whatever they
need to be realistic, and the cost is a second CI step because `go test ./...`
from the root does not descend into a nested module.

**Examples are the integration test surface, not documentation.** End-to-end
lanes drive an example rather than a harness that exists only inside a `_test.go`
file: `server` points its byte-parity fixtures and its pinned Node client lane at
`examples/vanilla`. An example that only demonstrates rots quietly, and an
example CI runs cannot.

`headless-minecraft` does not meet this convention yet: `examples/connect` was
never built, and the end-to-end lane is `client/world_e2e_test.go`, whose own
comment says it mimics what `examples/observe` subscribes to rather than driving
it. That is the open item under `headless-minecraft` above.

| Repository | Examples | Owning milestone |
| --- | --- | --- |
| `headless-minecraft` | `observe` | M7 |
| | `orbit` | M9.6, rewritten by the aiming plan's task 6 |
| | `microsoft` | M6.4 |
| `server` | `minimal`, `flat`, `vanilla` | M11.1 |

## Update rule

For every milestone:

1. Link its approved specification and implementation plan before source work.
2. Record the starting commit and exact acceptance tests in that plan.
3. Mark this file `Next` when dependencies are complete.
4. Mark it `Complete` only after format, lint, tests, race tests where relevant,
   build, security checks, interoperability tests, and clean-worktree review.
5. Add any newly discovered work to a later milestone instead of silently
   expanding the active milestone.
6. When a milestone completes, move its narrative record to the archive and
   leave only what is still open here.
