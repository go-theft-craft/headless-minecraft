# Observed world state design

- Status: Draft for review
- Date: 2026-08-16
- Repository: `headless-minecraft`
- Milestone: M7

## Context

M7 builds the observed world: player, entities, chunks, containers, registries,
environment, chat. It publishes the non-session events M6.3's taxonomy
declared — 57 of them then, 60 after Decision 11 — and it holds only what the
server sent. Mechanics stay out. A body
model, a physics kernel, and a movement strategy are M8 and M9.

M7 never got a design pass. M6.3's design fixed the taxonomy and the batch
boundary and stopped there, and the M7 implementation plan dated 2026-08-15 made
five decisions inside itself and asked for them to be rejected cheaply if wrong.
This document is that review. It keeps three of the five, replaces one, narrows
one, and adds four decisions the plan did not know it needed.

Reviewing the plan against M6.3's own plan turned up three problems that are
cheaper to fix now than after Task 1:

1. **The first configuration phase never reaches the client's loop.** `Connect`
   runs `login.Negotiate` to completion and only then starts `runLoop`, so every
   configuration packet on the way in is consumed inside the negotiator.
   `ConfigurationClientboundRegistryData` is the packet M7's registry domain
   exists for, and under this arrangement M7 cannot see it. Decision 6.
2. **`Event` has no revision field.** M6.3's design says an event "carries the
   snapshot revision that produced it". M6.3's plan does not implement that: the
   `Event` interface is `Name()` and `Domain()`, and nothing in the connection
   plan mentions a revision. M7's Task 2 asserts events carry one. Decision 2.
3. **Seven world events have no owner.** The taxonomy declares twelve world
   names. The plan's Task 5 covers the five chunk and block names and no task
   covers `WorldTimeChanged`, `WorldBorderChanged`, `WorldWeatherChanged`,
   `WorldDifficultyChanged`, `WorldExplosionOccurred`, `WorldEventOccurred`, or
   `WorldSimulationSettingsChanged`. Task 10's completeness test fails on those
   seven. Decision 8.

## Goals

- One immutable snapshot per revision, one revision per batch, across every
  domain at once.
- Reducers that are testable from a packet script with no network, no clock, and
  no other reducer.
- The same script through protocol 47 and 775 produces the same snapshot where
  the versions agree, and a documented difference where they do not.
- Unknown values kept as the server sent them, addressable by key, bounded.
- No unbounded store anywhere. A bot that runs for a week is the target.

## Non-goals

- Gameplay actions, container drivers, and semantic slot layouts. M9.
- Physics, collision, and capabilities. M8.
- Validating chat signatures. The snapshot reports whether a signature is
  present. It does not check one, and saying otherwise would be worse than not
  doing it.
- Prediction of any kind. The world records what arrived. It never guesses which
  menu a block opens or what a server would have sent.

## Decision 1: one lock, one revision, one batch

Kept from the plan, decision 1. A single monotonic `Revision` on the snapshot,
bumped once per batch under one write lock. Per-domain revisions would leave a
caller correlating two domains by guessing, and the batch is already the atomic
unit that M6.3 built the batcher to provide.

Reducer application order is fixed and part of the contract:

```text
player → entities → chunks → environment → containers → registry → chat
```

The plan says reducers never see each other. That is true of state and false of
facts: the entity reducer has to know the local player's entity ID so it does
not track the player as a mob, and only the player reducer learns that ID, from
the play `Login` packet. Ordering makes the dependency explicit instead of
leaving it to registration accident. Decision 5 gives it a mechanism.

`Register` after the first `Apply` stays an error, for the reason the plan gives:
a reducer that missed earlier batches holds state nobody can reconstruct.

## Decision 2: the revision is stamped after the bump, once, for the whole batch

M6.3 collects a batch's events into one `event.Collector` and publishes them
after the batch is applied. M7 adds the revision to that boundary rather than to
each reducer.

`Event` gains a third method:

```go
type Event interface {
	Name() EventName
	Domain() Domain
	Revision() uint64
}
```

with a small embedded value carrying the field, so the 16 session structs M6.3
wrote gain it by embedding rather than by 16 edits.

The world stamps. A reducer appends an unstamped event, `Apply` bumps the
revision, and the collector stamps every event it holds with the post-bump value
before `runLoop` publishes. Reducers cannot get the number wrong because they
never touch it, and the guarantee a subscriber needs holds by construction:
every event names a revision that already exists, and `Snapshot()` at that
revision shows the state the event describes.

Session events from the same batch are stamped with the same revision. They are
produced by M6.3's handlers, which run before `Apply`, and publishing them
alongside the state events is the point of sharing one collector.

**This is a change to M6.3's `event` package.** It is three lines plus an
embedded struct, and it is what M6.3's own design promised. It has to land in
M6.3 or in M7 Task 1, not later, because every session event struct is affected.

## Decision 3: reducers do not fail on server data

Narrowed from the plan. The plan has `Reduce` return an error, has `Apply` abort
the batch without bumping, and documents that the batch is then partially
applied and the session must end. That is the correct handling of the case. The
problem is how often the case can happen: the plan's own entity task says a
packet for an unknown entity ID must not fail the session, and the container
task says an unknown menu type is still a usable container.

So the contract narrows. A reducer returns an error only when its own invariant
broke, never because the server sent something the model does not recognize.
Unrecognized, unmapped, out-of-range, and simply weird are all normal traffic on
a modded server, and every one of them is preserved under Decision 7 or dropped
with a counter, never surfaced as an error.

What remains is a bug in this repository. `Apply` aborts, does not bump, marks
the world poisoned, and every later `Apply` and `Snapshot` reports the same
error. The session ends. A world that silently diverged from the server is worse
than a connection that stopped, and a poisoned world that keeps answering
queries is worse than both.

The counter matters more than it looks. `Snapshot` exposes per-domain counts of
packets seen, packets applied, and packets dropped with a reason. Without them,
"the bot did not react" and "the reducer never got the packet" are
indistinguishable from the outside, which is the failure mode a normalizing
library is most likely to produce.

## Decision 4: sections are immutable, and a decoded section is a pure cache

This replaces the plan's decision 3, which as written is a data race.

The plan stores a chunk in the server's palette form and decodes a section on
the first block read. Combine that with the plan's decision 2, where `Snapshot`
hands out a copy-on-read view under an `RWMutex`, and a snapshot reader
triggering a lazy decode mutates shared chunk state while holding a read lock,
concurrently with `Apply` writing a block change into the same chunk. `task test`
runs with `-race` and would find it, probably in Task 5 and possibly not until
the end-to-end lane in Task 10.

Laziness is worth keeping. Eagerly decoding 4096 blocks per section for every
chunk a server streams is work almost no consumer wants. The fix is immutability
rather than eagerness:

- A section's received bytes are immutable once stored.
- A decoded section is a pure function of those bytes, published through an
  `atomic.Pointer`. Two readers racing to decode the same section both compute
  the same result and one wins the store. A duplicate decode is wasted work and
  never wrong.
- A block write does not mutate a section. It builds a replacement section and
  swaps it in, which is a 4096-entry copy bounded per write, under the world's
  write lock.

`Snapshot` then copies pointers, not blocks, and the read path takes no lock at
all. Copy-on-write at section granularity costs one section copy per
`BlockChange`, and a `MultiBlockChange` addresses one section by definition, so
it also costs one.

The chunk benchmark the plan asks for in Task 5 stays. It now measures the read
path, which is the number that decides whether copy-on-read snapshots survive.

## Decision 5: reducers share facts through a batch context, not through each other

```go
// Context is what a reducer knows beyond its own state and the batch. It is
// owned by the world, passed by pointer, and valid only for one Reduce call.
type Context struct {
	Revision uint64 // the revision this batch will produce
	State    string // the protocol state the batch arrived in
	Local    LocalRef // the local player's entity ID, once known
}
```

The player reducer sets `Local` when it reads the play `Login` packet. Every
reducer ordered after it reads it. That is the only cross-reducer fact this
design admits, and adding a second one requires a design change rather than a
new field, because the list of facts everyone can see is the thing that decides
whether reducers stay independently testable.

`State` is here because Decision 6 lets batches arrive in configuration, and a
reducer needs to distinguish "the server has not sent registries yet" from "this
is the configuration pass where it will".

## Decision 6: the client owns the configuration phase

M6.3's `Connect` runs `login.Negotiate` to its terminal state, which under 775
is play, and starts `runLoop` afterwards. Every configuration packet on the way
in is therefore consumed inside the negotiator and never dispatched.

That breaks M7's registry domain outright: `ConfigurationClientboundRegistryData`
arrives exactly once, in configuration, and it is the packet the domain exists
for. It also quietly breaks two of M6.3's own session events.
`ServerMetadataChanged` is fed by `FeatureFlags` and `ResourcePackOffered` by
`AddResourcePack`, and both are configuration packets on the inbound path. Under
the current arrangement, M6.3's handlers for them only ever fire on a
play-to-configuration return, which most sessions never do.

So the fix belongs in M6.3 and M7 depends on it: `Connect` negotiates with a
terminal state of configuration, then owns the loop through configuration into
play. The readiness rule already decides when the client is ready and is
version-specific, so extending it to observe the configuration-to-play
transition costs one rule, not a new mechanism. Protocol 47 has no configuration
state and is unaffected, which is the usual shape of these differences.

The alternative, having the negotiator hand back a bag of observed configuration
packets, was rejected. It loses batch grouping and wire order, and it gives the
registry reducer a second input path that no test drives.

**This is a finding against M6.3's plan, not just an M7 decision.** M6.3 Task 12
changes, and M7 cannot start Task 7 until it does.

## Decision 7: unknown values belong to their owner and are bounded

Kept from the plan, decision 4, with a bound attached. Unknown metadata indices,
unknown namespaced registry keys, unknown menu types, unmapped custom payloads,
and unknown attribute keys are all kept as the bytes that arrived, addressable
by key. The state layer never substitutes a vanilla default for something the
server did not send, because on a modded server the default is a lie.

The plan's Task 8 makes the shared `Raw` type belong to its owner rather than to
a world-wide bag, which is right: an unknown metadata index belongs to its
entity and dies with it.

What the plan defers and this design fixes now is the bound. Every store gets
one, declared in the design rather than chosen during implementation:

| Store | Bound | On overflow |
| --- | --- | --- |
| Raw values per owner | 64 keys, 64 KiB total | Drop the new value, count it |
| Entities | none, released by `EntityDestroy` | Removal is the bound |
| Chunks | none, released by `UnloadChunk` | Removal is the bound |
| Chat log | 1024 messages | Evict oldest |
| Registry raw entries | 4 MiB total | Drop, count, report in the snapshot |

The entity and chunk stores have no numeric bound because the server tells the
client what to release, and a leak there is a bug in the reducer rather than a
missing limit. Both get the plan's release tests, and the chunk one is the
memory bug a long-running bot will certainly hit.

## Decision 8: environment is its own reducer

The seven world events nobody owned are environment facts, not chunk facts, and
they come from an entirely different packet set: `UpdateTime`, the six
`WorldBorder*` packets, `Difficulty`, `GameRuleValues`, `Explosion`,
`WorldEvent`, `WorldParticles`, `SimulationDistance`, `UpdateViewDistance`,
`UpdateViewPosition`, `SetTickingState`, `StepTick`.

Folding them into the chunk reducer would put the largest and most
performance-sensitive domain in the same file as a pile of scalars. They get
`world/environment.go` and a task of their own.

`GameStateChange` is the packet that forces the split to be explicit. One packet
type carries a game-mode change, a rain toggle, and more, discriminated by a
reason byte, so the player reducer and the environment reducer both handle it
and each ignores the reasons that are not theirs. The plan already asserts this
in Task 3 and names `WorldWeatherChanged` while assigning that event to no
reducer at all.

## Decision 9: snapshots are copy-on-read, and the benchmark decides

Kept from the plan, decision 2, with the escape route named. `Snapshot()` clones
the maps it exposes. This is obviously correct and O(entities + chunks) per
call, and after Decision 4 the chunk half is a pointer copy.

The honest risk is a caller snapshotting per tick with thousands of entities. If
Task 5's benchmark says the copy is the hot path, the answer is a persistent map
rather than handing out a mutable one, and that change is contained to
`snapshot.go` because no caller can tell the difference.

Per-domain accessors that skip the copy were considered and rejected for now.
They would be cheap and they would give up the property the whole design is
built on, which is that six domains read at one revision describe one instant.

## Decision 10: chat and UI stay the cut line

Kept from the plan, decision 5. The 12 chat names are the largest domain by
packet count, they carry no state any later milestone consumes, and the packets
stay reachable raw if the domain is cut. If M7 runs long, stop after the raw
task, mark it deferred in `MASTER_PLAN.md`, and ship the rest.

If it is cut, Task 10's completeness test asserts the implemented set equals
`AllNames()` minus the deferred chat names, and the deferral is recorded in the
milestone rather than left for a reader to infer from a failing test.

## Decision 11: damage names its source, death is an event, and neither is inferred

Added 2026-08-17, after the taxonomy met the first consumer that needs to act on
being hit. `examples/orbit`'s design found the gap: the taxonomy has
`PlayerHealthChanged` and `EntityDamaged`, neither names who dealt the damage,
and there is no death event at all — so a bot cannot pick a target to retaliate
against, and it learns it died by inferring it from a health number reaching
zero. Both are normalization decisions M7 has to make rather than inherit,
because the two protocols disagree about which of them they support.

Three names are added: `player.damaged`, `player.died`, and `entity.died`. The
taxonomy goes from 71 named events to 74.

**Damage carries an attribution value, not a bare type ID.** `event.Damage`
holds the damage type, the entity held responsible, the entity that physically
dealt it, and a source position, each paired with a boolean that says whether
the protocol sent it. The booleans are not decoration: entity 0 is a legal
entity and damage type 0 is a legal damage type, so there is no sentinel
available that means "the server said nothing".

**Nothing is inferred where a protocol is silent.** Protocol 775 sends a damage
event with a cause and a direct source. Protocol 47 has no damage packet at all
— being hurt is one of several dozen numbered entity statuses, and the status
byte is the entire message. A protocol 47 `EntityDamaged` therefore carries an
empty `Damage`, and a caller that wants an attacker on 1.8.9 infers one from
who swung or who is nearest. That inference is a guess, guesses belong to the
caller, and a guess presented through the same field as an observation is worse
than an honest zero.

**Death is observed, not derived.** Health reaching zero is not death: a server
can hold a player at zero health, and a death arrives as its own packet. Both
protocols announce one death through more than one packet, so `Died` is
idempotent until the next respawn and the first announcement wins. That costs
attribution when the unattributed packet arrives first, which is why `PlayerDied`
reports whether it was attributed rather than reporting a killer of zero.

**The two protocols attribute opposite halves.** 775 attributes damage and not
death — its death event carries a player and a message, and the killer field
was removed. 47 attributes death and not damage — its combat event carries the
killer. Neither protocol is the superset, which is the case that justifies
having a normalized shape at all.

**A dead entity stays tracked.** A server keeps a corpse for its death animation
and destroys it about a second later. Releasing it on death would hide it from
the caller that was fighting it during exactly the window that caller needs, so
`EntityView.Dead` is set and `EntityRemoved` still does the releasing.

**Damage to the local player is a player event.** `PlayerDamaged`, not
`EntityDamaged` filtered by the caller, on the rule the rest of the taxonomy
already follows: the player domain is the local player and the entity domain is
everybody else. Without this the local player also acquires a phantom entry in
the entity store, which is how the gap was noticed a second time.

What this does not add is respawning. Respawn is an action, actions are M9, and
a library that respawned a bot on its own would be deciding something the caller
may want to handle. `examples/orbit`'s required-surface item 6 stays open
against M9.

## Dependencies

M6.3, complete, including the two changes this design puts on it: the revision
on `Event` from Decision 2 and the configuration-phase loop from Decision 6.

M7 does not depend on M4 for its protocol 47 half. Every reducer is built and
tested against 47 first, and 775 coverage follows in the same task. The registry
domain is the exception, because protocol 47 has no registry data packet at all,
and its 47 test asserts the snapshot says so rather than presenting an empty
session registry as if the server sent one.

## Testing

Every reducer is driven by a packet script, which is a `[]version.Batch` built
from real generated packet values, applied in order, asserting on the snapshot
and the collected events after each batch. Five tests exist in every domain by
name: wire ordering within a batch, snapshot collections not aliasing the
reducer's, removal actually releasing, unknown values preserved rather than
defaulted, and the same script through both protocols.

Beyond those:

- A race test applying and snapshotting concurrently, which is the guarantee the
  whole design rests on.
- A section decode race test specifically, with many readers hitting one
  undecoded section while a block change replaces it. Decision 4 is the reason
  this test exists, and it is the one that would have caught the plan's version.
- A poisoned-world test: a reducer breaks its invariant, `Apply` aborts, the
  revision does not move, and every later call reports the same error.
- A fixture chunk from each protocol, asserting the same block appears at the
  same coordinate through two entirely different wire formats.
- The end-to-end lane from the plan's Task 10, extended to run through
  configuration so registry data is covered on the path it really arrives on.

## Risks

**Decision 6 reopens M6.3.** It is the expensive one. If M6.3 has already shipped
Task 12 by the time this is read, changing where the loop starts means changing
`Connect`, the readiness rules for both versions, and the connect tests. It is
still cheaper than the alternative, which is a registry domain fed by a side
channel.

**The chunk formats are the least understood part.** Protocol 47 sends a bitmask
and a packed blob, 775 sends per-section palettes and separate light data, and
this design asserts both reduce to the same block lookups without having decoded
either yet. If that is wrong, it is wrong in Task 5 and it is the task most
likely to overrun.

**57 event names were fixed before any of them met a real packet.** M6.3's design
already flags this and says M7's first job is to confirm the mapping against real
traffic before writing reducers. This design does not change that. It adds one
data point in the same direction: reviewing the taxonomy against the plan found
seven names with no owner, which is the kind of mistake a paper taxonomy
produces.

**The raw store bounds in Decision 7 are guesses.** They are stated so they can
be tested and argued with rather than chosen silently during Task 8. A modded
server that trips them will say so through the drop counters, which is the point
of having counters.


## Decision 12: a world that observes nothing is an error, and a play session without terrain says so

Added 2026-08-17, after three defects found in one day turned out to share a
shape. A client whose installed world was fed by no reducers, a bot reading its
own position back from server-sent state, and an adapter that never reduced the
packet carrying the join-time world: each one connected, ran, reported nothing,
and was found by a person watching a bot stand still. Tests passed throughout.
The common failure is not a bug class but a reporting one — the library knew
enough to say something and said nothing.

**A world installed with no reducers is refused at construction.** The seam is
satisfied by interface assertion, so an adapter that spells `Reducers` as a
package-level function rather than a method compiles, passes its own tests, and
installs a world that counts batches and observes nothing. That is exactly what
shipped. `New` now returns `ErrInvalidClient` when `WithWorld` is given and the
profile's adapter either does not satisfy the assertion or returns an empty
list. No consumer asks for observed state and wants none of it, so the case that
used to be documented as legal is the error it always was. This one is checked
at construction because it is a static fact about the configuration: it needs no
clock, no network, and no session.

**A play session that observes no terrain publishes `session.observation_missing`.**
This one cannot be static. Whether a server sends chunks is a fact about the
session, and it is only knowable by waiting. So the client watches: once the
server places the player, if no `world.chunk_loaded` has been observed after a
grace period, it publishes once and logs a warning. The grace is
`WithObservationGrace`, defaulting to ten seconds.

It reports rather than fails, for two reasons. Nothing in either protocol
obliges a server to send terrain, so a session without it is suspect rather than
invalid — and a library that ends a connection over its own heuristic is worse
than one that says what it sees. The check also rides on inbound traffic rather
than a timer, because the loop is one goroutine and a timer would need a second:
it runs when a batch closes. Keepalives make that reliable in practice on both
protocols, and a connection so dead that no packet arrives at all is a failure
the loop already reports.

`session.observation_missing` is the one name in the taxonomy that no packet
carries — it reports a packet's absence, which is the thing no packet can say.
The session domain goes from 14 named events to 15, and the taxonomy from 76 to
77.

## Decision 13: the 775 section format comes from the server's source, and a column's floor comes from the registry

Added 2026-08-17, closing the deferral this design recorded. Decision 4 kept 775
sections as opaque bytes because the paletted container's encoding could not be
checked here: nothing generates it, and no captured 26.1 chunk existed. The
milestone recorded that one captured chunk would unblock it. That was wrong in
an instructive way — the chunk was necessary and not sufficient.

**A capture says what the bytes are; only the source says what they mean.**
Four format hypotheses were tried against a real captured column — with and
without a block count, with and without a long-array length prefix — and all
four desynced within a few sections. The 26.1 layout is not the one a reader of
earlier versions writes down: a section carries `nonEmptyBlockCount` *and* a
`fluidCount`, and the long array has no count because vanilla writes it with
`writeFixedSizeLongArray`. That came from `LevelChunkSection.write` in the
26.1.2 server, decompiled locally by `mcreference`. **The reference workspace is
part of this repository's verification path, not only the simulation's.**

**The check is the server's own arithmetic, not the decoder's.** Each section
declares how many of its blocks are not air. Nothing in the decode path reads
it — that is a block semantic and `world` does not own those — so it is an
independent statement, written by the server that packed the bytes, about what
the section holds. All 24 sections of the captured column agree: 98,304 blocks,
block for block. A decoder that misreads the bit width, the palette, or the
packing does not survive that.

At runtime the guard is different, because the counts are not usable there: a
section holding `cave_air` would disagree without being wrong. What holds
instead is that a column's sections must consume its blob **exactly**. Every
misread layout leaves the cursor short or past, and this is the only signal
available before wrong blocks reach a consumer.

**A column does not say where it starts.** The blob is a run of sections with no
origin. Its lowest section is the dimension's minimum build height over sixteen
— -4 in the overworld and 0 in the nether — which arrives in configuration
inside the dimension type registry's NBT, while the player's own dimension
arrives with the login. So the chunk reducer watches the registry, the login,
and the respawn as well as the chunk, and reads them in wire order like
everything else, which is what makes a login bundled with the first columns work.

That needed `NetworkNBT.Int` in `minecraft-protocol`: both NBT types were
retained losslessly and exposed only as bytes, so `min_y` existed on the wire
and nowhere a consumer could reach it. Reading it here would have meant a second
NBT walker in a repository that already depends on one — the same argument that
put block solidity in that repository.

**Until the floor is known, nothing is decoded.** The column is kept whole and a
block lookup reports `ErrSectionNotDecodable`, which is what this adapter always
did. Guessing the floor is the one option not taken: it fails by 64 blocks in
the overworld while still answering every lookup, which is the failure this
milestone spent a day learning to refuse.

## Exit criteria

| | Criterion |
| --- | --- |
| 1 | One batch produces exactly one revision, and a 775 bundle of N packets produces one |
| 2 | Every published event names a revision that already exists, and `Snapshot()` at that revision shows what the event describes |
| 3 | Six domains read from one snapshot describe one instant, proven by a race test under `-race` |
| 4 | A section is decoded on first block read, and concurrent readers plus a concurrent block change are race-clean |
| 5 | No reducer returns an error for unrecognized server data, proven by a script of packets no version models |
| 6 | Every declared event name has an implementation, minus anything the chat deferral removed and recorded |
| 7 | Registry data received in configuration reaches the registry reducer on both the first pass and a play-to-configuration return |
| 8 | Loading and releasing 1000 chunks and 1000 entities returns the stores to empty |
| 9 | A world installed against an adapter that supplies no reducers is refused by `New`, and the adapter's reducer is proven to run rather than merely to be registered |
| 10 | A session that reaches play and loads no chunk publishes `session.observation_missing` once, and one that loads a chunk publishes none |
| 11 | Every section of a captured 26.1 column decodes to the non-air block count the server declared for it, and a column that does not consume its blob exactly is refused |
| 12 | A column is placed at the floor its dimension declares, moves when the player changes dimension, and stays undecoded when no floor is known |

## Plan amendments this design requires

The 2026-08-15 implementation plan needs six changes before Task 1:

1. Replace design decision 3 with Decision 4's immutable sections and atomic
   decode cache. Task 5's first test changes accordingly.
2. Add the revision to `Event` and the stamping rule to Task 1, and note the
   `event` package change it implies for M6.3's session structs.
3. Narrow `Reduce`'s error contract per Decision 3, and add the drop counters to
   `Snapshot`.
4. Add the `Context` type and the fixed reducer order to Task 1.
5. Add an environment reducer task between the chunk and container tasks,
   covering the seven orphaned world events.
6. Move the configuration-phase question out of Task 7's self-review note and
   into a prerequisite on M6.3, per Decision 6.
7. Add `examples/observe` to Task 10 and drive the end-to-end lane through it
   rather than through a harness that exists only in a test file, per the
   repository conventions in `MASTER_PLAN.md`. M6.3 gains `examples/connect` on
   the same rule, and `examples/` is its own Go module so the library keeps its
   single dependency.
