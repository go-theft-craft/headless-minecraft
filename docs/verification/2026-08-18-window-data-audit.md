# The 26.1.2 window data audit

**Date:** 2026-08-18. **Question:** is the 26.1 window dataset — an alias of
Java 1.16.1, and its generated file says so — usable to build M9.7's slot
handling on? **Answer: no.** The 26.1.2 lane rests on what the server sends at
runtime, its gate is the weaker of the two, and the fix is a data correction
in `minecraft-protocol`, not a workaround here.

## How it was measured

`client`'s `TestVanilla26WindowCapture` (vanilla lane, `-write-windows`)
opened every window a placed block can open against the pinned vanilla 26.1.2
server, through the headless client and its protocol 775 adapter, and
recorded what the server actually sent: the menu identifier from the open
packet, the slot count from the full-window set, and how many properties
arrived unprompted. The recording is committed at
`internal/conformance/testdata/windows/26_1_2.json`; the offline audit in
`internal/conformance/windows_test.go` reads it, so the finding is asserted on
every offline run, not remembered here.

## What the server sent

The wire carries no window names. Protocol 775's open packet numbers the menu
into the game's built-in menu registry, which the session never defines — it
is not one of the data-pack registries a server sends in configuration — and
the pinned tree carries no data that could resolve the number. The names below
come from reading `MenuType.java`'s registration order in the deobfuscated
26.1.2 server jar; nothing in the running system can perform this resolution.

| opened block | menu index | menu (from the jar source) | slots sent | properties |
| --- | ---: | --- | ---: | ---: |
| chest, barrel | 2 | `generic_9x3` | 63 | 0 |
| shulker_box | 20 | `shulker_box` | 63 | 0 |
| dispenser, dropper | 6 | `generic_3x3` | 45 | 0 |
| crafter | 7 | `crafter_3x3` | 46 | 10 |
| crafting_table | 12 | `crafting` | 46 | 0 |
| furnace | 14 | `furnace` | 39 | 4 |
| blast_furnace | 10 | `blast_furnace` | 39 | 4 |
| smoker | 22 | `smoker` | 39 | 4 |
| hopper | 16 | `hopper` | 41 | 0 |
| enchanting_table | 13 | `enchantment` | 38 | 10 |
| brewing_stand | 11 | `brewing_stand` | 41 | 2 |
| anvil | 8 | `anvil` | 39 | 1 |
| grindstone | 15 | `grindstone` | 39 | 0 |
| stonecutter | 24 | `stonecutter` | 38 | 1 |
| cartography_table | 23 | `cartography_table` | 39 | 0 |
| loom | 18 | `loom` | 40 | 1 |
| smithing_table | 21 | `smithing` | 40 | 1 |
| beacon | 9 | `beacon` | 37 | 3 |

Slot counts include the player inventory the full-window set appends — that
is the packet's own framing, and the window's own slot count is the total
minus 36.

Not captured, and therefore not checked: `generic_9x1/9x2/9x4/9x5` (no
vanilla block opens them), `generic_9x6` (a double chest), `lectern` (needs a
placed book), `merchant` (needs a villager), and the horse window (its own
packet). A window nobody opened is a window nobody has checked.

## How far the alias is from this

Three ways, each worse than the last:

1. **The addressing schemes do not meet.** The aliased records are keyed by
   1.8-era identifiers — `minecraft:chest`, `EntityHorse`,
   `minecraft:villager` — that no protocol 775 packet ever mentions. There is
   no mapping from the wire's menu index to any aliased record, so even the
   records that notionally describe the same window (furnace, anvil) cannot
   be reached from a running session.
2. **The vocabulary is missing most of the menus.** No aliased record exists
   for `blast_furnace`, `smoker`, `grindstone`, `stonecutter`,
   `cartography_table`, `loom`, `smithing`, `shulker_box`, `crafter_3x3`, or
   the `generic_NxM` family — the names the modern server actually opens.
3. **Where a human can pair records by hand, they disagree.** The enchantment
   table's aliased record names 7 properties and the server sends 10 (three
   costs, a seed, three enchantment ids, three levels). The brewing stand's
   names 1 and the server sends 2 — the fuel bar arrived in 1.9. Both
   disagreements are pinned by
   `TestTheAliasedRecordsDisagreeWhereAHumanCanPairThem`, so a corrected
   dataset fails the pin and the marker comes off.

## The decision

This is the plan's third outcome: **the alias is unusable.** Accordingly:

- M9.7's 26.1.2 lane rests on the server's own runtime data. The full-window
  set is the authoritative slot count, the observed recording is what makes
  that verifiable, and no slot index on this lane may come from the generated
  window registry.
- The 26.1.2 gate is therefore weaker than the 1.8.9 lane's, and the master
  plan says so where the stage is listed rather than only here.
- The correct fix is a data correction in `minecraft-protocol`: a real menu
  registry for 26.1, keyed in registry order, so the wire's index resolves to
  a name and a layout. An override table here would be a second source of
  truth about a registry, and the next consumer would not find it. The audit
  test fails the day that data lands, which is the signal to rebuild the lane
  on it.
