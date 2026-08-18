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
| M9 | Gameplay mechanics, verified against both versions | `minecraft-simulation`, `relay`, `headless-minecraft`, `server` | **In progress**: M9.1 complete, live check run 2026-08-17; M9.1b and M9.2 complete; M9.3 and M9.4 part-done; M9.5–M9.8 not started |
| M10 | Conformance, compatibility contracts, migration notes, `v1.0.0` | all runtime repositories | **In progress**: reconciled 2026-08-18, task 1 of six done |
| P4 | Put every consumer on the released `minecraft-protocol` and keep them there | `minecraft-protocol` | Complete |
| M11 | Turn `server` into a framework | `server` | Complete (M11.1–M11.7) |
| — | Navigation and behaviour pillar | `minecraft-simulation`, `headless-minecraft` | **In progress**: terrain, search, heuristic, memo, interaction primitives, and the block-movement extraction landed; four plans open |

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
- [ ] **M9.3 — movement scenarios**
  ([plan](docs/superpowers/plans/2026-08-17-m9-3-movement-scenarios.md)).
  Tasks 5–7 (correction, teleport, disconnect mid-action) are done. Open:
  freeze the trace document, the reader and comparator in
  `minecraft-simulation`, capture the corpus on both versions, and the six
  ordinary scenarios (walk, sprint, sneak, jump, fall, collide).
- [ ] **M9.4 — digging and block breaking**
  ([plan](docs/superpowers/plans/2026-08-17-m9-4-digging-block-breaking.md)).
  Tasks 1–6 are done and the kernel side has landed in `minecraft-simulation`
  (`06b97a9`, `0019b26`, `eb21f71`). Open: task 7 — capture the corpus, the
  failing cross-version test, and the refusal of a one-version gate — and
  task 8, the milestone record.
- [ ] **M9.5 — building and placement**
  ([plan](docs/superpowers/plans/2026-08-17-m9-5-building-and-placement.md)).
  Only the reconcile task is done; tasks 1–6 are open.
- [ ] **M9.6 — attack, damage, knockback**
  ([plan](docs/superpowers/plans/2026-08-17-m9-6-attack-damage-knockback.md)).
  Only the reconcile task is done. Damage attribution and the respawn
  primitive have landed already, so what remains is reach validation,
  cooldown, damage and knockback, the attack command and phase, and the
  scenarios.
- [ ] **M9.7 — containers and inventory**
  ([plan](docs/superpowers/plans/2026-08-17-m9-7-containers-and-inventory.md)).
  Only the reconcile task is done. Task 1 is an audit whose failure is the
  deliverable: 26.1's window dataset is an alias of Java 1.16.1, so this stage
  may be building on decade-old slot layouts. **Re-estimate the stage after it.**
- [ ] **M9.8 — crafting**
  ([plan](docs/superpowers/plans/2026-08-17-m9-8-crafting.md)). Only the
  reconcile task is done; tasks 1–5 are open.
- [ ] **A kicked session sometimes reports the state it ended in as `unknown`.**
  Seen once on 2026-08-18, in `TestEndToEndSurvivesTheServerHangingUp` under a
  whole-suite `-race` run, and not reproduced in fifty isolated runs of the same
  test. The adapter's kick handler publishes a disconnect naming `play`; the
  loop's transport path publishes one whose state it reads back off the stream,
  and a stream already closed answers `unknown`. So the failure says the
  transport report won a race the kick packet should have settled, and a
  subscriber is told a session ended in no state at all. Whether the kick is
  being missed or only the state is, is the thing to find out.

- [ ] **Export `movement.Strategy`** so an application can implement one
  (task 7 of [the world-state plan](docs/superpowers/plans/2026-08-13-world-state-actions.md)).
  Controller-owned strategy switching ships bunnyhop; nothing yet proves a
  strategy defined outside the library works, and `examples/orbit` is the first
  caller to need it.
- [ ] **Navigation edge completion**
  ([plan](docs/superpowers/plans/2026-08-18-navigation-edge-completion.md),
  7 tasks). `JumpGap` and the missing postures, plus the read-only edges the
  navigation design never named. Task 1 builds the jump reach table by running
  the movement kernel; no later task takes a gap distance from anywhere else.
  Task 5 is blocked on the climbable-block property. The extraction landed on
  2026-08-18, but `minecraft-protocol` has not released it, so what task 5 now
  waits on is the mutating-edges plan's task 2.
- [ ] **Mutating edges and pillar**
  ([plan](docs/superpowers/plans/2026-08-18-mutating-edges-pillar.md),
  6 tasks, task 1 done). `EdgePlace` and `EdgePillar` do not exist:
  `minecraft-simulation/navigation` has `EdgeWalk`, `EdgeStep`, `EdgeFall`, and
  `EdgeSwim` only. Task 1 landed 2026-08-18 (`minecraft-protocol` `c6557d1`):
  falling and climbable are measured out of the pinned jars into
  `BlockMovementRegistry.FallsByState` and `ClimbableByState`, rather than onto
  `data.Block` as both designs first said — upstream publishes neither property,
  and a measured fact belongs with the dataset whose manifest records the jar's
  digest.
- [ ] **Aiming and reach geometry**
  ([plan](docs/superpowers/plans/2026-08-18-aiming-and-reach-geometry.md),
  7 tasks). `geom.Behind`, `geom.Lead`, `geom.Tangent`, and `AABB.Reaches` are
  absent from `minecraft-simulation/geom`. Task 6 is the `examples/orbit`
  rewrite.
- [ ] **Composed behaviours**
  ([plan](docs/superpowers/plans/2026-08-18-composed-behaviours.md),
  6 tasks). No `behaviour` package exists. Tasks 1–4 unblock once the
  interaction primitives land (they have); task 5 needs the mutating edges;
  task 6 (`Fish`) needs a captured fishing trace per version, and neither oracle
  session captured one — the plan's own dependency table says "neither has
  run", which is now wrong about the sessions and still right about the trace.
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
