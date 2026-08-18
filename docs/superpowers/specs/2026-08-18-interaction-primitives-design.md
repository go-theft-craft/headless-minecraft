# Interaction primitives design

## Status

Drafted 2026-08-18. Implementation requires the matching implementation plan
and an explicit request to execute it.

## Purpose

`version/action.go` carries seven actions and six of them are movement:
`ActionMove`, `ActionLook`, `ActionMoveLook`, `ActionGround`, `ActionInput`,
`ActionSprint`, and `ActionRespawn`. A client built on this vocabulary can walk,
turn, declare what it is doing with its legs, and come back from a death. It
cannot swing at anything, hold anything, use anything, or open anything.

The master plan's Task 6 names the remaining primitives — chat, command, use,
place, attack, interact, dig, slot, click, drop, and close — and leaves them
with M9 "mechanic by mechanic". This design states what those types are, adds
the four the task list omits, and records why the primitives may land before
the gates that verify them.

## The respawn precedent

The master plan records that `ActionRespawn` landed ahead of M9.6 and states
the rule: what the milestone owns is "the respawn scenario — the gate that
proves the primitive against vanilla — not the primitive itself."

This design takes that rule as general. A primitive is a type and an encoding.
A gate is a measurement against a live server, and M9.4 through M9.8 keep every
one of them. Landing the types first is what lets `examples/orbit` and the
`server` repository compile against a stable vocabulary while the conformance
lanes are still being built.

The rule has a limit, and it is the same one the navigation design draws about
break times: **a primitive whose numbers are unmeasured must refuse rather than
guess.** Dig ships as an intent the adapter encodes; how long a block takes is
M9.4's, and until M9.4 lands nothing in this repository claims to know it.

## Goals

- Name every outbound intent a bot needs, version-neutrally, in one place.
- Keep the two protocols' disagreements at the adapter, refused through the
  existing `ErrUnsupportedAction`, never smoothed over.
- Land the vocabulary before the gates, so consumers stop waiting on M9.

## Non-goals

- Inventory semantics. Which slot holds what is P4's, and the actions here
  carry slot indices rather than meanings.
- Break-time arithmetic, placement legality, reach limits, and cooldown
  timing. Those are M9.4 through M9.6, and this design ships no number they own.
- Chat rendering. The master plan records that no chat component is rendered
  anywhere and that this is "deliberate and permanent". `ActionChat` sends a
  string; nothing here reads one back.

## The actions

### Holding and swinging

| Action | Fields | Notes |
| --- | --- | --- |
| `ActionHeldSlot` | `Slot uint8` | Hotbar selection, 0 through 8. Both protocols. |
| `ActionSwing` | `Hand Hand` | The arm swing, which both protocols send separately from the attack. 47 has no hand and ignores the field. |

`ActionHeldSlot` is the first one to land. Attack with a sword, place a block,
cast a rod, and raise a shield all begin by selecting a slot, and none of them
is expressible without it.

### Using and interacting

| Action | Fields | Notes |
| --- | --- | --- |
| `ActionUseItem` | `Hand Hand` | Use the held item in place: eat, drink, cast a rod, raise a shield. |
| `ActionUseOn` | `Block BlockPos`, `Face Face`, `Cursor Vec3`, `Hand Hand` | Use against a block: place, open, till. The cursor is the hit point within the face, which both protocols carry and which decides slab and stair orientation. |
| `ActionInteract` | `Entity int32`, `Kind InteractKind`, `At *Vec3`, `Hand Hand` | Interact, interact-at, or attack an entity. One packet in both protocols carries all three, so one action does. |
| `ActionReleaseUse` | `Hand Hand` | Stop using: fire the bow, lower the shield, stop eating. Without it a bow is drawn forever. |

`ActionInteract` covers attack rather than a separate `ActionAttack`, because
both protocols encode attack as a mode of the interact packet. Splitting it in
the neutral vocabulary would invent a distinction the wire does not make.

### Digging

| Action | Fields | Notes |
| --- | --- | --- |
| `ActionDig` | `Block BlockPos`, `Face Face`, `Stage DigStage` | `DigStart`, `DigCancel`, `DigFinish`. Both protocols send the same three, and the caller decides when, because how long a block takes is M9.4's. |

### Inventory and containers

| Action | Fields | Notes |
| --- | --- | --- |
| `ActionClickSlot` | `Window uint8`, `Slot int16`, `Button uint8`, `Mode ClickMode`, `Transaction int16` | Raw and generic, matching P4's "generic raw slots" before its semantic drivers. |
| `ActionDrop` | `Whole bool` | Drop one or drop the stack. |
| `ActionCloseWindow` | `Window uint8` | |

### State and speech

| Action | Fields | Notes |
| --- | --- | --- |
| `ActionEntityAction` | `Kind EntityActionKind` | Sneak start and stop, sprint start and stop, leave bed, and the horse family. 47 sends all of these as one packet; `ActionSprint` becomes a thin caller of it rather than a separate wire concept. |
| `ActionSwapHands` | — | Offhand swap. **775 only.** |
| `ActionChat` | `Message string` | |
| `ActionCommand` | `Command string` | Separate from chat because 775 signs and encodes commands differently, and a caller that must know which it is sending is a caller that has already lost. |

## Version asymmetry

The master plan already sets the policy: "a per-version gate must be allowed to
say this behavior does not exist in this version and record why". Four entries
land on it.

| Intent | 47 (1.8.9) | 775 (26.1) |
| --- | --- | --- |
| `Hand` on any action | No offhand exists. The field is ignored, not refused, because a main-hand use is still a use. | Both hands. |
| `ActionSwapHands` | Refused: `ErrUnsupportedAction`. | Encoded. |
| `ActionInput` | Already refused today; 47 has no input packet. | Encoded. |
| `ActionCommand` | Encoded as chat, because 1.8.9 has no command packet and a leading slash is how it was sent. | Its own packet. |

Refusing and ignoring are different and the difference is deliberate. A field a
protocol cannot express is dropped when dropping it still leaves the intent
intact, and the whole action is refused when it does not. `Hand` on a
main-hand use is the first case; a hand swap is the second.

`Sender.Locomotion` already shows the caller-side pattern for a refusal: catch
`ErrUnsupportedAction`, mute that intent, and carry on. That pattern is the
documented contract, not an example's improvisation.

## Testing

- Every action encodes on both adapters or refuses with `ErrUnsupportedAction`
  naming its kind. A round-trip test asserts one or the other for all of them,
  so a new action cannot be added without deciding.
- `ActionKind` strings are stable and unique, checked by a test over the set.
- The 47 adapter's `ActionCommand` produces the same bytes as an
  `ActionChat` whose message is the command with a leading slash.
- Golden packet bytes for each encoding, against the generated codecs.
- No test asserts a break time, a reach distance, or a cooldown. Those belong
  to M9.4 through M9.6 and a test here that pinned one would pin a guess.

## Sequencing

1. `ActionHeldSlot`, `ActionSwing`, `ActionEntityAction`. No unlanded
   dependency, and they unblock everything aimed.
2. `ActionUseItem`, `ActionUseOn`, `ActionReleaseUse`, `ActionInteract`.
   Needs `Hand`, `Face`, and the cursor vector.
3. `ActionDig`. Ships as an intent; M9.4 supplies the timing.
4. `ActionClickSlot`, `ActionDrop`, `ActionCloseWindow`. Wants P4's window
   model to be useful, though not to compile.
5. `ActionChat`, `ActionCommand`, `ActionSwapHands`.

Step 1 is unblocked today. Nothing here waits on M9.1b, because none of it
claims to have been verified against a server.

## Acceptance criteria

- A bot can select a hotbar slot, swing, use an item, use against a block,
  interact with an entity, dig in three stages, click a slot, drop, close a
  window, sneak, chat, and issue a command, through `Client.Do`.
- Every one of those either encodes on both protocols or refuses on the one
  that cannot express it, and a test proves which.
- `ActionSprint` still behaves exactly as it does today after becoming a caller
  of `ActionEntityAction`.
- No number owned by M9.4, M9.5, or M9.6 appears in this package.
