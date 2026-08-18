# The vanilla lane

The vanilla lane is every test behind the `vanilla` build tag in `client/`,
run by `task test:vanilla`. Each test starts a real pinned server — vanilla
1.8.9, vanilla 26.1.2, or both — connects the headless client to it, and
checks behaviour against the game itself rather than against this project's
reading of it. It is deliberately outside `task verify`: verify stays offline,
and a lane that failed without a jar would make it depend on a download.

## What it covers

Per version, as of 2026-08-18:

- **Movement** — the six flat-world scenarios
  (`TestVanillaMovementDrawsNoCorrections` and its 26 counterpart), passing
  when the server never disagrees with the prediction; and M9.3's three
  provoked scenarios — a correction adopted rather than fought, a teleport
  taken whole and answered once, a disconnect mid-action applying nothing
  unconfirmed — passing when it does disagree and the client handles it.
- **Combat** (M9.6) — an attack swings before it hits and the server reports
  the target hurt; an out-of-reach attack is never sent; a real death is
  answered by a respawn and the player returns to play.
- **Windows** (M9.7) — a click round-trips and agrees with the server's own
  restatement; a shift-click drains (26.1.2; the 1.8.9 client refuses what it
  cannot predict); closing a window drops the cursor stack and the server
  agrees; plus the window-data capture that feeds the offline audit.
- **Crafting** (M9.8) — a craft through a real server lands the result, in
  the 3x3 table and the 2x2 player grid; a 26.1.2 shift-craft drains the
  grid; an invalid grid shows no result and the server agrees; and the
  mirror corpus is confirmed live on both jars.

What it does not cover: online mode (every check in this project has run
offline; M6.4 is what changes that), anything a version's lane declares
absent with a reason in its own test file, and everything `task verify`
already proves offline.

## Where the jars come from

`minecraft-reference` prepares them: it downloads by URL, verifies SHA-1 and
SHA-256 against Mojang's own manifest before a file reaches its path, and
records the artifacts in the workspace's `manifest.lock.json`. The lane
resolves the prepared workspace in this order (see
`client/vanilla_workspace_test.go`):

1. `MCREFERENCE_WORKSPACE` — an explicit path to a `reference/work`
   directory, for a machine that already has one prepared elsewhere.
2. This repository's own `reference/work`, which
   `task server:vanilla VERSION=1.8.9` and `task server:vanilla
   VERSION=26.1.2` prepare.

A version that is not prepared skips, and the skip message is the command
that fixes it. The lane never reads a relative path into a sibling
repository: a test that reads another repository's ignored directory passes
or fails for reasons its own repository cannot see — which is exactly what it
used to do, and why this document exists.

## The build a result names

Every lane run logs the SHA-256 of the server artifact it ran against, read
from `manifest.lock.json` rather than recomputed from the file — the digest
is the one `minecraft-reference` verified against Mojang's manifest at
download time. A conformance result that does not say which jar produced it
is a story about somebody's laptop; these say. The jars themselves stay
local, always: `reference/` is gitignored here, as it is everywhere else in
the project.
