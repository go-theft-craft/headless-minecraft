# Aiming and reach geometry design

## Status

Drafted 2026-08-18. Implementation requires the matching implementation plan
and an explicit request to execute it.

## Purpose

`examples/orbit/geometry.go` holds the geometry the orbit bot needed: `Toward`,
`Yaw`, `Away`, `HorizontalDistance`, and a waypoint `Circle`. Every one of them
was written for a bot that walks a flat circle and faces where it is going.
None of them can aim at a block or at an entity, and `send.go` states the
consequence as a literal: `Pitch: 0`.

Every interaction primitive the
[interaction primitives design](2026-08-18-interaction-primitives-design.md)
adds is aimed. A client that cannot pitch cannot mine the block under its feet,
cannot place against a floor, and cannot hit anything that is not at eye
height. This design supplies the missing half of the aiming arithmetic, the
reach test that legality is measured by, and a decision about where the
geometry lives before four more callers copy it.

## Goals

- Give the outbound path a pitch to send.
- Measure reach the way the game measures it: to the nearest point of the
  target's box, not to its centre.
- Settle the duplicate `Vec3` once, while the example is still the only caller.

## Non-goals

- Target selection. Which entity to hit and which block to mine is the
  application's decision, exactly as goal selection is in the navigation design.
- Per-version reach distances. Those are numbers M9.6 measures against a
  vanilla server, and this design takes reach as an argument rather than
  asserting a value.
- Aim smoothing, anti-cheat evasion, or humanised turning. A caller that wants
  those composes them from these functions.
- Replacing `movement`'s trigonometry. The game's own sine table belongs to the
  kernel; nothing here is on the physics path.

## The duplicate Vec3

`examples/orbit/geometry.go` defines its own `Vec3` with `Add`, `Floor`,
`HorizontalDistance`, `Toward`, and `Yaw`. `minecraft-simulation/geom` defines
another with `Add`, `Sub`, `Scale`, `HorizontalLengthSquared`, `IsZero`,
`Floor`, and `BlockPosOf`.

The example already depends on `minecraft-simulation`: both `examples/go.mod`
and the library's own `go.mod` require it. The duplicate is therefore not
isolation, it is a copy, and it is the same failure the navigation design
records about `bypass.go` — 334 lines answering "can I stand here" inside an
example because nothing in the stack exposed the fact.

**Decision: the example's `Vec3` is retired in favour of `geom.Vec3`.** The
aiming functions land in `minecraft-simulation` beside the geometry they
extend, and the example imports them.

`geom` imports nothing outside the standard library and the module README
states that as a property worth keeping. Aiming needs no world view, no
profile, and no entity, so it does not cost `geom` that property. The functions
land in `geom` rather than in a new package.

`Circle`, `Nearest`, and `Deviation` stay in the example. A waypoint ring is
what that bot does, not a fact about the game.

### The release step this needs

The navigation plan of 2026-08-17 defers the orbit rewrite because
"`minecraft-simulation` has no tags. `headless-minecraft` cannot import it
until there is a released version, and a `replace` directive in a public
repository is not acceptable."

That is now stale. `minecraft-simulation` v0.1.0 is tagged and pushed, and both
`headless-minecraft/go.mod` and `examples/go.mod` already require it.

The constraint it states still binds this work, though, in the other direction:
the aiming functions land in `minecraft-simulation`, so `headless-minecraft`
cannot use them until a **new** `minecraft-simulation` release carries them.
This design therefore spans two repositories and a release between them, and
the plan sequences it that way rather than discovering it at the import.

## Functions

All angles are degrees in the protocol's own convention, which is what `Yaw`
already returns and what the outbound actions carry. Radians appear nowhere in
a signature.

| Function | Meaning |
| --- | --- |
| `Pitch(from, to Vec3) float32` | The downward angle from `from` to `to`. Positive is down, matching the wire. |
| `Look(from, to Vec3) (yaw, pitch float32)` | Both at once, because every caller that needs one needs the other and computing them separately walks the same vector twice. |
| `Behind(target, facing Vec3, d float64) Vec3` | The position `d` blocks behind a target, given the direction it faces. |
| `Lead(target, velocity Vec3, ticks float64) Vec3` | Where a mover will be in `ticks`. The caller supplies the velocity; nothing here tracks entities. |
| `Tangent(centre, here Vec3, clockwise bool) Vec3` | The unit heading that circles a point, so a caller strafes without materialising a ring. |
| `Away(here, threat Vec3, distance float64) Vec3` | Already in the example. It moves with the others because `behaviour.Flee` is its second caller. |
| `(AABB) Nearest(p Vec3) Vec3` | The point of a box closest to `p`. This is the one the others are built on. |
| `Reaches(eye Vec3, box AABB, reach float64) bool` | Whether `eye` is within `reach` of the box's nearest point. |

`Nearest` and `Reaches` belong on `geom.AABB`, which already carries `ClampX`,
`ClampY`, and `ClampZ` and is the type collision resolves against.

## Eye height is not a constant here

Reach is measured from the eye, and the eye is a per-version, per-posture
number: 1.62 standing in 1.8.9, and a value the profile supplies in 26.1.2
where a sneaking or crawling body is shorter. `Reaches` therefore takes an eye
position rather than a feet position and an offset, for the same reason
`Capability` takes tick costs rather than deriving them — the two versions
disagree and the argument is where that disagreement belongs.

The example supplies the eye height it already knows. A later caller supplies
the profile's.

## Testing

- Table tests for `Pitch` at the cardinal cases: straight down is +90, straight
  up is -90, level is 0, and a 45-degree descent is +45.
- `Look` agrees with `Yaw` and `Pitch` computed separately, for a thousand
  random vectors.
- `Nearest` returns a point inside the box for every input, returns the input
  itself when the input is inside, and agrees with a brute-force clamp on each
  axis.
- `Reaches` is exactly the comparison of `Nearest`'s distance against the
  limit, checked at the boundary in both directions.
- `Lead` with a zero velocity returns the target unchanged, and `Tangent` at
  the centre returns a zero vector rather than dividing by zero.
- `examples/orbit` compiles with its own `Vec3` deleted and its behaviour
  unchanged. That deletion is the proof, the same way the navigation design
  makes `bypass.go`'s deletion the proof of `terrain`.

## Acceptance criteria

- `Sender.Step` sends a computed pitch rather than the literal `0`, and the
  orbit bot's observable behaviour is unchanged because it still looks level
  while walking a flat circle.
- `examples/orbit/geometry.go` defines no `Vec3` and no `Add`, `Floor`,
  `HorizontalDistance`, `Toward`, `Yaw`, or `Away`.
- `geom` imports nothing outside the standard library.
- Every new function has a test that fails if its sign convention is flipped.
