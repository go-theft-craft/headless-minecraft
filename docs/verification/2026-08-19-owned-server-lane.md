# The owned-server matrix row

M10's compatibility matrix had one row nothing had written: the headless client
against this project's own Go server. Task 1's protocol 775 acceptor unblocked
it and the master plan called it "pure scheduling". This is the run.

## What ran

`task test:owned`, on 2026-08-19, with the server built from `server`'s
`examples/vanilla` at `332e80b`, driving the six M8.4 movement scenarios — walk,
sprint, jump, sneak, stand, turn — at 220 ticks each through the same scenario
runner the vanilla lane uses.

**All six passed.** Zero corrections, zero reconciliations, 289 chunks streamed
per scenario, and the client's outbound cadence identical to what the same
scenarios draw from a real 1.8.9 server:

| Scenario | Owned server | Vanilla 1.8.9 |
| --- | --- | --- |
| stand | `flying:210 position:10 position_look:2` | `flying:210 position:10 position_look:2` |
| turn | `flying:2 look:208 position_look:12` | `flying:2 look:208 position_look:12` |

The vanilla figures are from the same afternoon's run of `task test:vanilla`
against the pinned 1.8.9 jar, so the two columns are one session apart and not
one release apart.

## What this row does not say

**The correction count is vacuous against this server.** It performs no
movement validation: the only clientbound position it sends after login is a
spectator teleport (`internal/server/conn/handler_play.go`), so it cannot
correct a client that is wrong, and zero corrections is what it would report
for any client at all — including one whose physics were nonsense. The vanilla
lane's sharpest assertion is free here.

What the row does measure, and what nothing measured before: a login completes
against the owned server, a world streams, the session survives 1,320 ticks of
scripted movement across six scenarios, and the outbound packet cadence is what
the rule measured off a real client predicts. When the server grows movement
validation, this lane becomes the check that it validates the way vanilla does.

**Both ends are this project's code.** A mutual misunderstanding of protocol 47
passes this lane. The Node interop lane and the vanilla lanes are what find one.

**There is no protocol 775 half, and it is not scheduling.** The 775 login
acceptor landed in `minecraft-protocol` on 2026-08-18, so a 775 login can now
complete — but the server has no 775 play path at all. It advertises protocol
47, its packets come from the `v1_8` generated types, and `server/caller.go`
says so where somebody reads it: "with no 775 server to send it to". The row
is protocol 47 only until the server speaks 775, which is a milestone rather
than a lane. M11.7's brigadier rendering is still unsent to a client for the
same reason.

## Where it lives

`internal/owned` is the harness, `client/owned_e2e_test.go` is the lane. It
shares the `vanilla` build tag — the tag means "starts a real server, so an
ordinary run must not" — and is selected by name, so `task test:vanilla` is
unchanged and skips nothing it used to run.

The server binary is named by `GOTHEFTCRAFT_SERVER` and the lane skips without
it. It is built rather than resolved from a sibling path, for the reason M10
Task 4 gave when it stopped the vanilla lanes reaching into
`../../minecraft-simulation/reference/work`: a lane that reaches into a
checkout is a lane that breaks when somebody moves one.
