//go:build vanilla

// The vanilla conformance lane. It is behind a build tag because it starts a
// real Minecraft server, which an ordinary test run must not do: `task verify`
// stays fast and offline, and this runs when someone asks for it.
//
// Run it with: devbox run -- task test:vanilla

package client_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/go-theft-craft/minecraft-protocol/data"
	gen "github.com/go-theft-craft/minecraft-protocol/generated/java/v1_8"
	gen26 "github.com/go-theft-craft/minecraft-protocol/generated/java/v26_1"
	simentity "github.com/go-theft-craft/minecraft-simulation/entity"
	simgeom "github.com/go-theft-craft/minecraft-simulation/geom"
	simmovement "github.com/go-theft-craft/minecraft-simulation/movement"
	v1_8 "github.com/go-theft-craft/minecraft-simulation/profile/java/v1_8"
	v26_1 "github.com/go-theft-craft/minecraft-simulation/profile/java/v26_1"
	simulation "github.com/go-theft-craft/minecraft-simulation/sim"
	simworld "github.com/go-theft-craft/minecraft-simulation/world"

	"github.com/go-theft-craft/headless-minecraft/auth"
	"github.com/go-theft-craft/headless-minecraft/client"
	"github.com/go-theft-craft/headless-minecraft/event"
	"github.com/go-theft-craft/headless-minecraft/internal/vanilla"
	"github.com/go-theft-craft/headless-minecraft/predict"
	"github.com/go-theft-craft/headless-minecraft/safety"
	"github.com/go-theft-craft/headless-minecraft/version/java"
	"github.com/go-theft-craft/headless-minecraft/world"
)

// wirePrecision is the smallest position difference protocol 47 can carry: it
// transmits coordinates as fixed point in units of one thirty-second of a block.
// A predicted position and the server's acknowledged one agreeing to better than
// this is agreement; asking for more would be asking the wire for what it does
// not have.
const wirePrecision = 1.0 / 32.0

// scenarioTicks is how long each scenario runs after the login boundary. Two
// hundred ticks is ten seconds of game time, which is long enough for a drift of
// a thousandth of a block per tick to become a teleport.
const scenarioTicks = 220

// vanillaScenario is one scripted run against the real server.
type vanillaScenario struct {
	name string
	// input is what the controller asks for on each tick.
	input func(tick int) simmovement.Input
}

// vanillaScenarios are M8.4's six, driven against a server rather than against a
// jar's own bytecode. The suite is the same because the claim is the same: these
// are the movements the milestone says this module reproduces.
func vanillaScenarios() []vanillaScenario {
	return []vanillaScenario{
		{
			name: "walk",
			input: func(tick int) simmovement.Input {
				return simmovement.Input{Forward: 1, Yaw: float32(tick) * 2}
			},
		},
		{
			name: "sprint",
			input: func(tick int) simmovement.Input {
				return simmovement.Input{Forward: 1, Sprint: true, Yaw: float32(tick)}
			},
		},
		{
			name: "jump",
			input: func(tick int) simmovement.Input {
				return simmovement.Input{Forward: 1, Jump: tick%13 < 9}
			},
		},
		{
			name: "sneak",
			input: func(tick int) simmovement.Input {
				return simmovement.Input{Forward: 1, Strafe: 1, Sneak: true, Yaw: -float32(tick)}
			},
		},
		{
			name: "stand",
			input: func(int) simmovement.Input {
				// The cadence's own scenario. A standing player sends bare ground
				// flags and one forced position every twenty-first packet, and a
				// server that expected a position every tick would say so.
				return simmovement.Input{}
			},
		},
		{
			name: "turn",
			input: func(tick int) simmovement.Input {
				return simmovement.Input{Yaw: float32(tick) * 7, Pitch: float32(tick%40) - 20}
			},
		},
	}
}

// TestVanillaMovementDrawsNoCorrections is M8.8's exit criterion.
//
// It connects to a real 1.8.9 server in offline mode, predicts locally, and
// requires that the server never disagrees: no position packet after the
// acknowledgement boundary, no complaint in its log, and a predicted position
// that matches the server's to within what the wire can carry.
//
// Offline mode is a limitation of the whole lane and it is stated rather than
// hidden: nothing measured here says anything about online-mode behaviour.
func TestVanillaMovementDrawsNoCorrections(t *testing.T) {
	server := vanilla.Start(t, laneBuild(t, "1.8.9").startOptions())

	for _, scenario := range vanillaScenarios() {
		t.Run(scenario.name, func(t *testing.T) {
			runVanillaScenario(t, server, scenario)
		})
	}
}

func runVanillaScenario(t *testing.T, server *vanilla.Server, scenario vanillaScenario) {
	t.Helper()

	set, err := gen.Data()
	if err != nil {
		t.Fatalf("load the 1.8.9 data set: %v", err)
	}
	profile, err := v1_8.New(set)
	if err != nil {
		t.Fatalf("build the 1.8.9 profile: %v", err)
	}
	names, ok := profile.(simulation.BlockNames)
	if !ok {
		t.Fatal("the profile cannot resolve block names")
	}

	bot := connectForMovement(t, server.Addr())

	raw, err := bot.Subscribe(event.DomainRaw, 4096)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()

	if err := bot.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v\n%s", err, server.Log())
	}
	defer func() { _ = bot.Close() }()

	// The world has to arrive before a prediction can run over it: a tick that
	// reads unstreamed terrain is incomplete and the loop reports without moving.
	waitForTerrain(t, server, bot)

	loop, err := predict.New(predict.Options{
		Actor:   bot,
		Profile: profile,
		Blocks:  predict.MetadataBlocks(set, names),
		Spawn: func(pos simgeom.Vec3, yaw, pitch float32) (
			simentity.State, simmovement.Locomotion, bool,
		) {
			return v1_8.Spawn(profile, pos, yaw, pitch)
		},
	})
	if err != nil {
		t.Fatalf("predict.New: %v", err)
	}
	defer func() { _ = loop.Close() }()

	if err := loop.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	for tick := range scenarioTicks {
		loop.Input(scenario.input(tick))
		time.Sleep(50 * time.Millisecond)
	}
	_ = loop.Close()

	// Let anything the server had to say about the last few ticks arrive.
	time.Sleep(500 * time.Millisecond)

	// 1. Corrections, counted from the wire rather than from the loop.
	//
	// The boundary is the client's first outbound movement: a server places its
	// player during login as a matter of course, and counting that would make
	// this impossible to pass. Counting from packets rather than from the loop's
	// own reconciliation is deliberate — the loop reconciles from snapshots, and
	// a server that corrected a player back to a position it had already sent
	// would be a correction the snapshot could not distinguish from silence.
	events := drainEvents(raw)
	corrections := countCorrections(events)
	sent := map[string]int{}
	received := map[string]int{}
	for _, one := range events {
		switch value := one.(type) {
		case event.PacketSent:
			sent[value.Packet]++
		case event.PacketReceived:
			received[value.Packet]++
		}
	}
	// Logged whether or not the scenario passes. The cadence is the thing most
	// likely to be wrong here and the least visible when it is: a run that sent
	// the right number of the wrong packet passes every other assertion.
	t.Logf("%s: %d events, loop err %v, reconciled %d\n  sent %v\n  received %v",
		scenario.name, len(events), loop.Err(), loop.Corrections(), sent, received)
	if corrections > 0 {
		t.Errorf("the server corrected the client %d times\n%s",
			corrections, tail(server.Log(), 40))
	}

	// 2. The server's own complaints. A server can disagree without teleporting:
	// it tolerates a drift for a while and logs it, and a check that watched only
	// for teleports would call that a pass.
	for _, complaint := range []string{"moved wrongly", "moved too quickly"} {
		if lines := server.Matching(complaint); len(lines) > 0 {
			t.Errorf("the server logged %q %d times:\n%s",
				complaint, len(lines), strings.Join(lines, "\n"))
		}
	}

	// 3. Where the server put the player, when it put it anywhere.
	//
	// A 1.8.9 server tells a client its position only to correct it: silence is
	// how it accepts one. So with zero corrections there is no second position to
	// compare against, and the silence above is the agreement. When the server
	// has spoken, the two must agree to within what the wire can carry — it
	// transmits coordinates as fixed point in thirty-seconds of a block, and
	// asking for more precision than that would be asking the wire for what it
	// does not have.
	body, ok := loop.Predicted()
	if !ok {
		t.Fatal("the loop never predicted anything")
	}
	if corrections > 0 {
		observed := bot.World().Player
		predicted := feetOf(body)

		for _, axis := range []struct {
			name      string
			got, want float64
		}{
			{"x", predicted.X, observed.X},
			{"y", predicted.Y, observed.Y},
			{"z", predicted.Z, observed.Z},
		} {
			if diff := axis.got - axis.want; diff > wirePrecision || diff < -wirePrecision {
				t.Errorf("the prediction is at %s=%v and the server put it at %v",
					axis.name, axis.got, axis.want)
			}
		}
	}

	// And the client did keep reporting throughout: a client that stopped sending
	// would draw no corrections either, and would have proved nothing.
	if sent := countSent(events); sent < scenarioTicks/2 {
		t.Errorf("the client sent %d movement packets over %d ticks", sent, scenarioTicks)
	}
}

// countSent counts the outbound movement packets.
func countSent(events []event.Event) int {
	var sent int
	for _, one := range events {
		if value, ok := one.(event.PacketSent); ok {
			switch value.Packet {
			case "position", "position_look", "look", "flying":
				sent++
			}
		}
	}

	return sent
}

// connectForMovement builds a client authorized to move.
func connectForMovement(t *testing.T, addr string) *client.Client {
	t.Helper()

	provider, err := auth.Offline("conformance")
	if err != nil {
		t.Fatalf("Offline: %v", err)
	}
	authz, err := safety.Authorize(addr, safety.ScopeObserve, safety.ScopeMove)
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}

	// A world has to be installed for the client to reduce anything into: the
	// observation is opt-in, and a prediction with nothing to observe would run
	// over an empty world and disagree with the server about everything.
	bot, err := client.New(
		client.WithWorld(world.New()),
		client.WithAddress(addr),
		client.WithAuth(provider),
		client.WithVersion(java.Java1_8()),
		client.WithAuthorization(authz),
		client.WithConnectTimeout(60*time.Second),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	return bot
}

// waitForTerrain blocks until the chunk under the player has arrived.
func waitForTerrain(t *testing.T, server *vanilla.Server, bot *client.Client) {
	t.Helper()

	deadline := time.Now().Add(60 * time.Second)
	var last string
	for time.Now().Before(deadline) {
		snapshot := bot.World()
		if snapshot.Player.Placed && len(snapshot.Chunks.Loaded) > 0 {
			return
		}
		last = fmt.Sprintf("placed=%v known=%v chunks=%d at (%v %v %v)",
			snapshot.Player.Placed, snapshot.Player.Known, len(snapshot.Chunks.Loaded),
			snapshot.Player.X, snapshot.Player.Y, snapshot.Player.Z)
		time.Sleep(50 * time.Millisecond)
	}

	t.Fatalf("the server never streamed the terrain under the player: %s\nserver log:\n%s",
		last, tail(server.Log(), 25))
}

// countCorrections counts clientbound positions after the client's first
// outbound movement.
func countCorrections(events []event.Event) int {
	var acknowledged bool
	var corrections int

	for _, one := range events {
		switch value := one.(type) {
		case event.PacketSent:
			switch value.Packet {
			case "position", "position_look", "look", "flying":
				acknowledged = true
			}
		case event.PacketReceived:
			if acknowledged && value.Packet == "position" {
				corrections++
			}
		}
	}

	return corrections
}

// drainEvents reads everything a subscription holds without blocking.
func drainEvents(subscription *client.Subscription) []event.Event {
	var events []event.Event
	for {
		select {
		case one, ok := <-subscription.C():
			if !ok {
				return events
			}
			events = append(events, one)
		default:
			return events
		}
	}
}

// feetOf returns where a body stands, deriving it from the box for a version
// that keeps no position.
func feetOf(body simentity.State) simgeom.Vec3 {
	if body.Position != (simgeom.Vec3{}) {
		return body.Position
	}

	return simgeom.Vec3{
		X: (body.Box.MinX + body.Box.MaxX) / 2,
		Y: body.Box.MinY,
		Z: (body.Box.MinZ + body.Box.MaxZ) / 2,
	}
}

// tail returns the last lines of a log, because a failure message with a
// three-minute server log in it is not read.
func tail(log string, lines int) string {
	all := strings.Split(log, "\n")
	if len(all) <= lines {
		return log
	}

	return strings.Join(all[len(all)-lines:], "\n")
}

// The 26.1.2 lane. Same six scenarios, same criterion, a different game. Its
// jar and libraries come from the prepared reference workspace like the
// 1.8.9 lane's — see vanilla_workspace_test.go — never from a path into a
// sibling repository.

// TestVanilla26MovementDrawsNoCorrections runs the same gate against a real
// 26.1.2 server.
//
// This is the second half of the milestone's claim: one kernel, two versions,
// and each one checked against its own game. The profile it predicts with was
// gated against that version's own bytecode by M8.7, so a failure here is more
// likely to be about the wire than about the physics — which is the reverse of
// what a version with no jar-backed oracle would have faced.
func TestVanilla26MovementDrawsNoCorrections(t *testing.T) {
	options := laneBuild(t, "26.1.2").startOptions()
	// A modern server generates more before it answers, and it does it on a
	// cold cache the first time.
	options.Ready = 5 * time.Minute
	server := vanilla.Start(t, options)

	for _, scenario := range vanillaScenarios() {
		t.Run(scenario.name, func(t *testing.T) {
			runVanilla26Scenario(t, server, scenario)
		})
	}
}

func runVanilla26Scenario(t *testing.T, server *vanilla.Server, scenario vanillaScenario) {
	t.Helper()

	set, err := gen26.Data()
	if err != nil {
		t.Fatalf("load the 26.1.2 data set: %v", err)
	}
	profile, err := v26_1.New(set)
	if err != nil {
		t.Fatalf("build the 26.1.2 profile: %v", err)
	}

	bot := connect26ForMovement(t, server.Addr())

	raw, err := bot.Subscribe(event.DomainRaw, 8192)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()

	if err := bot.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v\n%s", err, tail(server.Log(), 25))
	}
	defer func() { _ = bot.Close() }()

	waitForTerrain(t, server, bot)

	loop, err := predict.New(predict.Options{
		Actor:   bot,
		Profile: profile,
		// This version's protocol carries the flattened state identifier, which
		// is the identity the profile's own table is keyed by.
		Blocks: predict.FlattenedBlocks(func(state data.BlockStateID) (simworld.BlockRef, bool) {
			return v26_1.RefState(profile, state)
		}),
		Spawn: func(pos simgeom.Vec3, yaw, pitch float32) (
			simentity.State, simmovement.Locomotion, bool,
		) {
			return v26_1.Spawn(profile, pos, yaw, pitch)
		},
	})
	if err != nil {
		t.Fatalf("predict.New: %v", err)
	}
	defer func() { _ = loop.Close() }()

	if err := loop.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	for tick := range scenarioTicks {
		loop.Input(scenario.input(tick))
		time.Sleep(50 * time.Millisecond)
	}
	_ = loop.Close()
	time.Sleep(500 * time.Millisecond)

	events := drainEvents(raw)
	sent := map[string]int{}
	received := map[string]int{}
	for _, one := range events {
		switch value := one.(type) {
		case event.PacketSent:
			sent[value.Packet]++
		case event.PacketReceived:
			received[value.Packet]++
		}
	}
	t.Logf("%s: %d events, loop err %v, reconciled %d\n  sent %v\n  received %v",
		scenario.name, len(events), loop.Err(), loop.Corrections(), sent, received)

	if corrections := countCorrections26(events); corrections > 0 {
		t.Errorf("the server corrected the client %d times\n%s",
			corrections, tail(server.Log(), 40))
	}
	for _, complaint := range []string{"moved wrongly", "moved too quickly"} {
		if lines := server.Matching(complaint); len(lines) > 0 {
			t.Errorf("the server logged %q %d times:\n%s",
				complaint, len(lines), strings.Join(lines, "\n"))
		}
	}
	if sent := countSent(events); sent < scenarioTicks/2 {
		t.Errorf("the client sent %d movement packets over %d ticks", sent, scenarioTicks)
	}
}

// countCorrections26 counts this protocol's own correction packet.
//
// Protocol 775 names it player_position rather than position, and it carries a
// teleport identifier the client answers. The boundary is the same: a position
// before the client has reported one is the login sequence.
func countCorrections26(events []event.Event) int {
	var acknowledged bool
	var corrections int

	for _, one := range events {
		switch value := one.(type) {
		case event.PacketSent:
			switch value.Packet {
			case "position", "position_look", "look", "flying",
				"move_player_pos", "move_player_pos_rot", "move_player_rot", "move_player_status_only":
				acknowledged = true
			}
		case event.PacketReceived:
			if acknowledged && (value.Packet == "position" || value.Packet == "player_position") {
				corrections++
			}
		}
	}

	return corrections
}

// connect26ForMovement builds a client speaking this version's protocol.
func connect26ForMovement(t *testing.T, addr string) *client.Client {
	t.Helper()

	provider, err := auth.Offline("conformance")
	if err != nil {
		t.Fatalf("Offline: %v", err)
	}
	authz, err := safety.Authorize(addr, safety.ScopeObserve, safety.ScopeMove)
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}

	bot, err := client.New(
		client.WithWorld(world.New()),
		client.WithAddress(addr),
		client.WithAuth(provider),
		client.WithVersion(java.Current()),
		client.WithAuthorization(authz),
		client.WithConnectTimeout(90*time.Second),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	return bot
}
