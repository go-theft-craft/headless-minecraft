//go:build vanilla

// The owned-server lane: the same client, the same six scenarios, and the same
// assertions as the 1.8.9 vanilla lane, against the server this project ships.
//
// It carries the vanilla build tag because that tag means "starts a real
// server, so an ordinary test run must not" — which is true of this one too.
// It skips unless GOTHEFTCRAFT_SERVER names a built binary, so `task
// test:vanilla` on a machine without one is unchanged.
//
// This is M10's owned-server matrix row, at protocol 47. There is no protocol
// 775 half and there cannot be one yet: the 775 login acceptor landed in
// minecraft-protocol on 2026-08-18, but the server has no 775 play path, and
// its own server/caller.go says so — "with no 775 server to send it to".
//
// **What passing here does not mean.** The correction count is the vanilla
// lane's sharpest assertion and it is vacuous against this server, which
// performs no movement validation: the only clientbound position it sends
// after login is a spectator teleport, so it cannot correct a client that is
// wrong and zero corrections is what it would report for any client at all.
// What the row does measure is real and was not measured before: a login
// completes, a world streams, and the client's outbound cadence over 220 ticks
// is what the rule measured off a real client predicts — the standing scenario
// sends 210 bare ground flags and 10 forced positions here, the same numbers
// vanilla draws. When the server grows movement validation, this lane becomes
// the check that it validates the way vanilla does, and the assertion stops
// being free.

package client_test

import (
	"testing"

	"github.com/go-theft-craft/headless-minecraft/internal/owned"
)

func TestOwnedServerMovementDrawsNoCorrections(t *testing.T) {
	// The lane's whole claim is that the two implementations agree about
	// movement, so it runs the scenarios the vanilla lane runs and holds them
	// to the same tolerance. A scenario suite of its own would let the server
	// be measured against whatever it happens to do.
	server := owned.Start(t, owned.Options{})

	for _, scenario := range vanillaScenarios() {
		t.Run(scenario.name, func(t *testing.T) {
			runVanillaScenario(t, server, scenario)
		})
	}
}
