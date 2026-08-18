# Composed behaviours design

## Status

Drafted 2026-08-18. Implementation requires the matching implementation plan
and an explicit request to execute it.

Depends on the
[interaction primitives design](2026-08-18-interaction-primitives-design.md)
for its actions and on the
[mutating edge amendment](2026-08-18-mutating-edge-amendment-design.md) for its
routes. It is the last of the five and nothing waits on it.

## Purpose

The other four designs supply parts: an aim, an action, an edge, a path. None
of them supplies a bot that fishes, because fishing is not a packet. It is cast,
wait, read a signal the server never names, reel, repeat, and give up when the
rod breaks.

That shape recurs. Eat, block with a shield, follow a player, flee a threat,
bridge a gap, pillar to the surface, strip-mine a corridor — every one is a
multi-tick state machine over primitives, with a wait it must not busy-loop
through and a failure it must report rather than hang on.

The repository's stated design goal is to "compose tools and plans, including
multi-block digging and rotated or mirrored building matrices, instead of
hard-coding one-block actions." This design says what a composed behaviour is.

## Goals

- One shape for every behaviour, so a caller learns it once.
- Behaviours that report why they stopped rather than stalling silently.
- Behaviours that refuse when they are not authorized, before they act.
- Keep behaviours out of the library's required path. A client that wants none
  of them pays nothing.

## Non-goals

- Behaviour trees, goal selection, scheduling, and priority arbitration.
  Choosing what the bot should be doing is the application's, exactly as the
  navigation design keeps goal selection out of `navigation`.
- Combat strategy. `Attack` is a primitive and `Follow` is a behaviour;
  deciding whom to fight is not here.
- Chat parsing as a control channel. The master plan records that no chat
  component is rendered anywhere and that this is deliberate and permanent.

## The shape

A behaviour is the same shape as the navigation design's `Follower`: **asked
once per tick, never driving.** That is not a preference, it is what
`adapter.Source` already requires, and a behaviour that drove its own loop
could not be composed with a follower that does not.

```
Tick(ctx, observed) (Outcome, error)
```

`Outcome` carries the actions this tick wants and a status: running, complete,
or stopped with a typed reason. The reasons mirror the follower's — `Blocked`,
`Stuck`, `WorldChanged`, `Failed` — plus the two only a behaviour has:
`Unauthorized` and `OutOfResources`.

Three properties follow from asking rather than driving:

- **A wait is a tick that returns no action.** A behaviour waiting for a rod to
  dip, a furnace to smelt, or a placement to settle returns running with an
  empty action set. It never sleeps and never blocks, so the tick rate stays
  the caller's.
- **A behaviour is testable without a connection.** Feed it observed states and
  read its actions, which is exactly how `examples/orbit` tests its own tick
  loop through a narrowed `sender` interface today.
- **Behaviours compose by delegation.** `StripMine` holds a `Follower` and a
  `Dig` behaviour and forwards its tick to whichever is active. There is no
  scheduler, because there is nothing to schedule.

## Authorization is checked at construction

`safety` already has the vocabulary: `ScopeObserve`, `ScopeMove`,
`ScopeInventory`, `ScopeInteract`, `ScopeDig`, and `ScopeBuild`. Every
behaviour declares the scopes it needs, and construction fails when the
authorization does not carry them.

Checking at construction rather than per tick matches the client's own rule
that "components are selected and validated before network work begins". A
behaviour that discovered on tick four hundred that it may not dig has already
walked the bot somewhere it should not be.

**One open question for the reviewer.** Attack currently falls under
`ScopeInteract`, because both are entity interaction on the wire. Whether
killing things deserves a scope of its own is a safety decision rather than a
protocol one: an authorization that permits opening a chest is not obviously an
authorization that permits attacking a player. This design does not settle it,
and flags it rather than quietly reusing `ScopeInteract`.

## The behaviours

| Behaviour | Scopes | Built from |
| --- | --- | --- |
| `Follow` | move | `Find` and a `Follower`, replanned when the target moves past a threshold |
| `Flee` | move | `Away` for the goal, then the same |
| `Eat` | inventory, interact | `HeldSlot`, `UseItem`, `ReleaseUse`, and the observed food level |
| `Block` | interact | `UseItem` on the shield hand until released. **775 only**; 47 has no shield and the behaviour refuses at construction. |
| `Fish` | inventory, interact | `HeldSlot`, `UseItem`, the bobber entity, `UseItem` again |
| `Bridge` | build, move | The `Place` edges from a path |
| `Pillar` | build | The `Pillar` edges from a path |
| `StripMine` | dig, build, move | A `Follower`, `Dig`, and `Support` |

`Bridge` and `Pillar` are executors, not planners. The route is
`navigation`'s and the placement decision is the edge's; the behaviour turns an
edge into the actions that perform it and waits for each one to settle. Keeping
the planning out of them is what lets a server-side mob use the same edges with
no behaviour at all.

## Fishing has an unmeasured signal

Casting is `UseItem` with a rod. Reeling is `UseItem` again. Between them the
bot must know that a fish bit, and **no packet in either protocol says so.**

What a client actually observes is the bobber entity's motion changing as it
dips, and a splash sound effect at its position. Which of those is reliable,
how much motion counts as a dip, and whether 26.1.2 signals it differently from
1.8.9 are measurements, not readings — and this repository's own history is
that careful readings of vanilla behaviour were wrong often enough that M8.4
replaced them with fixtures the game generates.

So `Fish` ships with its bite detector behind an interface and **one captured
trace per version as its gate**, taken through the M9.1 capture lane. Until
that lane has run live — the master plan records M9.1's live check as still
open — `Fish` is not claimed to work, and no test asserts a threshold nobody
measured.

This is the same discipline the navigation design applies to break times, and
it is the reason `Fish` is last in the sequencing below despite being the
easiest to write.

## Where this lives

A `behaviour` package in `headless-minecraft`, importing `version`, `world`,
`safety`, and `minecraft-simulation/navigation`.

Not in `minecraft-simulation`: a behaviour sends actions, and that module's
charter stops at protocol-independent state transitions. Not in `examples`: the
orbit example is where `bypass.go` ended up last time something had no home,
and the navigation design's remedy for that is the precedent being followed
here.

Nothing in the client's required path imports it. A caller that wants no
behaviours links none.

## Testing

- Every behaviour is tested against scripted observed states through a narrowed
  sender, with no connection, matching `examples/orbit/send_test.go`.
- A behaviour that is asked to tick without its scopes fails at construction,
  and a test asserts the failure for each one.
- A waiting behaviour returns running with no actions, for as many ticks as the
  wait lasts. A behaviour that emits an action every tick while waiting is a
  bug this test catches.
- Every behaviour terminates: a property test drives each one for ten thousand
  ticks against an unhelpful world and asserts it reaches a stopped status
  rather than running forever.
- `Block` refuses at construction on 47 and constructs on 775.
- `Fish` is gated on a captured trace per version and is skipped, with a
  recorded reason, until those traces exist.

## Sequencing

1. The `Behaviour` interface, `Outcome`, and the construction-time scope check.
   Nothing else can be written first.
2. `Follow` and `Flee`. They need only movement, which is the one thing that
   already works end to end.
3. `Eat` and `Block`. Need the interaction primitives.
4. `Bridge`, `Pillar`, and `StripMine`. Need the mutating edges, so they wait
   on M9.4 and M9.5.
5. `Fish`. Needs a captured trace on each version, so it waits on M9.1's live
   check and on M9.1b.

## Acceptance criteria

- A bot follows a moving player across terrain it must route around, and stops
  with a typed reason when the player becomes unreachable.
- A behaviour constructed without its scopes fails to construct.
- No behaviour busy-loops while waiting, and every behaviour terminates.
- `Fish` asserts no threshold that was not measured from a captured trace.
- The client compiles and runs with the `behaviour` package unimported.
