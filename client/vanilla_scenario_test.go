//go:build vanilla

// M9.3's three scenarios: a correction, a teleport, and a disconnect in the
// middle of an action.
//
// They are here rather than beside the movement gate because they want the
// opposite thing from the same server. `TestVanillaMovementDrawsNoCorrections`
// passes when the server never disagrees; these pass when it does, and they
// provoke the disagreement on purpose. Sharing a server between the two would
// make one lane's success the other's failure, and sharing a log assertion
// would make "moved too quickly" mean two contradictory things.

package client_test

import (
	"context"
	"fmt"
	"math"
	"strings"
	"sync"
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

	"github.com/go-theft-craft/headless-minecraft/client"
	"github.com/go-theft-craft/headless-minecraft/event"
	"github.com/go-theft-craft/headless-minecraft/internal/vanilla"
	"github.com/go-theft-craft/headless-minecraft/predict"
)

// username is who the client logs in as, and therefore who a console command
// has to name. It is the name `connectForMovement` and `connect26ForMovement`
// both authenticate with.
const username = "conformance"

// settleTicks is how long a scenario runs after the event it provoked. Sixty
// ticks is three seconds, which is long enough for a correction that was
// mishandled to produce the next one: a client that fights a correction draws
// another within a few ticks, and one that adopts it draws none ever.
const settleTicks = 60

// lane is one version's live setup, so that a scenario can be written once and
// run against both games.
//
// It exists because these three scenarios are about the client's behaviour and
// not about a version's physics, and a scenario written twice is a scenario
// that will be fixed once.
type lane struct {
	name string
	// start runs the server this version needs.
	start func(*testing.T) *vanilla.Server
	// connect builds a client speaking this version's protocol.
	connect func(*testing.T, string) *client.Client
	// predictOn builds the prediction loop for this version's rules.
	predictOn func(*testing.T, *client.Client, func(predict.Correction)) *predict.Loop
	// corrections counts this version's own correction packet.
	corrections func([]event.Event) int
	// precision is the smallest position difference the wire can carry.
	precision float64
	// confirms names the packet this version answers a server position with,
	// and is empty for a version that answers with nothing.
	confirms string
}

func lane1_8() lane {
	return lane{
		name:    "1.8.9",
		start:   func(t *testing.T) *vanilla.Server { return vanilla.Start(t, vanilla.Options{}) },
		connect: connectForMovement,
		predictOn: func(
			t *testing.T, bot *client.Client, on func(predict.Correction),
		) *predict.Loop {
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

			loop, err := predict.New(predict.Options{
				Actor:   bot,
				Profile: profile,
				Blocks:  predict.MetadataBlocks(set, names),
				Spawn: func(pos simgeom.Vec3, yaw, pitch float32) (
					simentity.State, simmovement.Locomotion, bool,
				) {
					return v1_8.Spawn(profile, pos, yaw, pitch)
				},
				OnCorrection: on,
			})
			if err != nil {
				t.Fatalf("predict.New: %v", err)
			}

			return loop
		},
		corrections: countCorrections,
		precision:   wirePrecision,
		// Protocol 47 has no teleport identifier and nothing to confirm. The
		// client answers a server position with its own position, like any other
		// tick, which is why this is empty rather than named.
		confirms: "",
	}
}

func lane26() lane {
	return lane{
		name: "26.1.2",
		start: func(t *testing.T) *vanilla.Server {
			return vanilla.Start(t, vanilla.Options{
				Jar:       jar26,
				Libraries: libraries26,
				LevelType: "minecraft:flat",
				Ready:     5 * time.Minute,
			})
		},
		connect: connect26ForMovement,
		predictOn: func(
			t *testing.T, bot *client.Client, on func(predict.Correction),
		) *predict.Loop {
			t.Helper()

			set, err := gen26.Data()
			if err != nil {
				t.Fatalf("load the 26.1.2 data set: %v", err)
			}
			profile, err := v26_1.New(set)
			if err != nil {
				t.Fatalf("build the 26.1.2 profile: %v", err)
			}

			loop, err := predict.New(predict.Options{
				Actor:   bot,
				Profile: profile,
				Blocks: predict.FlattenedBlocks(func(state data.BlockStateID) (simworld.BlockRef, bool) {
					return v26_1.RefState(profile, state)
				}),
				Spawn: func(pos simgeom.Vec3, yaw, pitch float32) (
					simentity.State, simmovement.Locomotion, bool,
				) {
					return v26_1.Spawn(profile, pos, yaw, pitch)
				},
				OnCorrection: on,
			})
			if err != nil {
				t.Fatalf("predict.New: %v", err)
			}

			return loop
		},
		corrections: countCorrections26,
		// 775 sends an absolute position as float64, so the server's number is
		// the server's number and there is nothing to round to.
		precision: 0,
		confirms:  "teleport_confirm",
	}
}

// live is a connected client with a prediction loop running over it.
type live struct {
	bot  *client.Client
	loop *predict.Loop
	raw  *client.Subscription

	mu          sync.Mutex
	corrections []predict.Correction
}

// seen returns the corrections published so far.
func (l *live) seen() []predict.Correction {
	l.mu.Lock()
	defer l.mu.Unlock()

	return append([]predict.Correction(nil), l.corrections...)
}

// connectLive brings a client to the point where it is predicting.
func connectLive(t *testing.T, lane lane, server *vanilla.Server) *live {
	t.Helper()

	session := &live{}
	session.bot = lane.connect(t, server.Addr())

	raw, err := session.bot.Subscribe(event.DomainRaw, 8192)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	session.raw = raw

	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	t.Cleanup(cancel)

	if err := session.bot.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v\n%s", err, tail(server.Log(), 25))
	}
	t.Cleanup(func() { _ = session.bot.Close() })

	waitForTerrain(t, server, session.bot)

	session.loop = lane.predictOn(t, session.bot, func(correction predict.Correction) {
		session.mu.Lock()
		defer session.mu.Unlock()
		session.corrections = append(session.corrections, correction)
	})
	t.Cleanup(func() { _ = session.loop.Close() })

	if err := session.loop.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Walk, rather than stand, before anything is provoked.
	//
	// Two reasons, and the second one cost a run to find. A position from the
	// server before the client has reported one is the login sequence placing
	// the player, so the loop needs to have reported something for a later one
	// to count as a disagreement at all. And a server rejecting a move puts the
	// player back at the last position it accepted — so a client that never
	// moved gets corrected to exactly where the server already had it, the
	// snapshot does not change, and a reconciliation that watches the snapshot
	// has nothing to see. Walking first makes the correction observable, which
	// is also why M8.8 counts corrections from the wire and not from the loop.
	run(session, simmovement.Input{Forward: 1}, 40)

	return session
}

// run feeds the loop one input for a number of ticks, at the game's own rate.
func run(session *live, input simmovement.Input, ticks int) {
	for range ticks {
		session.loop.Input(input)
		time.Sleep(50 * time.Millisecond)
	}
}

// await waits for a condition the server has to answer for, and reports what it
// was still waiting on when it gave up.
func await(t *testing.T, what string, server *vanilla.Server, condition func() bool) {
	t.Helper()

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}

	t.Fatalf("timed out waiting for %s\nserver log:\n%s", what, tail(server.Log(), 30))
}

// TestVanillaACorrectionIsAdoptedRatherThanFought is Task 5 on 1.8.9.
func TestVanillaACorrectionIsAdoptedRatherThanFought(t *testing.T) {
	server := lane1_8().start(t)
	correctionScenario(t, lane1_8(), server)
}

// TestVanilla26ACorrectionIsAdoptedRatherThanFought is the same on 26.1.2.
func TestVanilla26ACorrectionIsAdoptedRatherThanFought(t *testing.T) {
	server := lane26().start(t)
	correctionScenario(t, lane26(), server)
}

// correctionScenario provokes a server correction and requires the client to
// take it without arguing.
//
// M8.8's gate is zero corrections, which means M8.8 can never test this path at
// all. So this one asks for a correction deliberately, by claiming a position
// no player could have walked to.
//
// What a rejection-type correction turns out to carry is worth stating, because
// it decides what this test can assert. A server that refuses a move puts the
// player back at the last position it accepted, and that position is the one
// the client itself reported on its previous tick — so the correction names the
// prediction's own number and From equals To. Nothing is adopted because
// nothing disagreed. The claim that survives is the one that matters here: the
// refused move never entered the prediction, and one correction did not become
// two. A correction that moves the player somewhere the client did not choose
// is a teleport, and that is the next scenario.
func correctionScenario(t *testing.T, lane lane, server *vanilla.Server) {
	session := connectLive(t, lane, server)

	before := session.bot.World().Player
	walked, ok := session.loop.Predicted()
	if !ok {
		t.Fatal("the loop never predicted anything")
	}
	t.Logf("%s: the server has the player at (%v %v %v), the loop at %+v",
		lane.name, before.X, before.Y, before.Z, feetOf(walked))

	// Somewhere no player walks to in one tick. The server rejects it and
	// answers with an absolute position, which is the event this whole test is
	// about — everything above exists to make one of these arrive.
	bogus := before.X + 10_000
	if err := session.bot.Do(t.Context(), client.ActionMove{
		X: bogus, Y: before.Y, Z: before.Z, OnGround: true,
	}); err != nil {
		t.Fatalf("Do: %v", err)
	}

	await(t, "the server to answer the refused move", server, func() bool {
		return len(session.seen()) > 0 || len(server.Matching("moved too quickly")) > 0
	})

	// Keep predicting. A client that took the correction agrees with the server
	// and draws nothing further; a client that treated an absolute position as
	// one more delta is somewhere the server never named, and the server says so
	// again within a few ticks.
	run(session, simmovement.Input{}, settleTicks)

	// 1. The refused move never entered the prediction.
	//
	// This is the failure that would matter most and show up least: an action a
	// caller sent out of band is not a tick the loop simulated, and a loop that
	// adopted it from its own outbound traffic would be predicting from a
	// position the server had already thrown away.
	settled, ok := session.loop.Predicted()
	if !ok {
		t.Fatal("the loop stopped predicting")
	}
	if at := feetOf(settled); at.X > bogus-1_000 {
		t.Errorf("the prediction is at x=%v after a refused move to x=%v; the loop "+
			"adopted an action the server rejected", at.X, bogus)
	}

	// 2. One rejection, one answer, and no cascade.
	//
	// Counted twice on purpose. The wire count is every position the server
	// sent; the loop count is every one that changed where the client believed
	// it was. They differ legitimately — a server that restates a position the
	// client already held is a packet and not a disagreement — and a gate that
	// watched only one of them would miss half of what a correction loop looks
	// like.
	onWire := lane.corrections(drainEvents(session.raw))
	published := session.seen()
	t.Logf("%s: %d corrections on the wire, %d published by the loop%s",
		lane.name, onWire, len(published), listed(published))

	complaints := len(server.Matching("moved too quickly")) + len(server.Matching("moved wrongly"))
	if complaints == 0 {
		t.Fatalf("the server never complained about a ten-thousand-block step, so "+
			"nothing was provoked and this test proved nothing\n%s", tail(server.Log(), 30))
	}
	if complaints > 1 {
		t.Errorf("the server complained %d times about one refused move; a correction "+
			"that is not taken becomes a correction loop:\n%s",
			complaints, strings.Join(server.Matching("moved"), "\n"))
	}
	if onWire > 2 {
		t.Errorf("the server sent %d positions after one refused move", onWire)
	}

	// 3. And the client is where the server says it is.
	//
	// The allowance is the wire's own resolution plus half a block of settling:
	// an idle player still falls to the floor, and the axis an argument would
	// show on is the horizontal one it was corrected on.
	observed := session.bot.World().Player
	at := feetOf(settled)
	for _, axis := range []struct {
		name      string
		got, want float64
	}{
		{"x", at.X, observed.X},
		{"z", at.Z, observed.Z},
	} {
		allowed := lane.precision + 0.5
		if diff := axis.got - axis.want; diff > allowed || diff < -allowed {
			t.Errorf("after the correction the prediction is at %s=%v and the server "+
				"has it at %v, %v apart", axis.name, axis.got, axis.want, diff)
		}
	}
}

// listed renders corrections for a log line, and nothing at all for none.
func listed(list []predict.Correction) string {
	if len(list) == 0 {
		return ""
	}

	return "\n" + corrections(list)
}

// corrections renders a list of them for a failure message.
func corrections(list []predict.Correction) string {
	rendered := make([]string, 0, len(list))
	for _, one := range list {
		rendered = append(rendered, fmt.Sprintf("  tick %d: (%v %v %v) -> (%v %v %v)",
			one.Tick, one.From.X, one.From.Y, one.From.Z, one.To.X, one.To.Y, one.To.Z))
	}

	return strings.Join(rendered, "\n")
}

// TestVanillaATeleportIsTakenWholeAndAnsweredOnce is Task 6 on 1.8.9.
func TestVanillaATeleportIsTakenWholeAndAnsweredOnce(t *testing.T) {
	lane := lane1_8()
	teleportScenario(t, lane, lane.start(t))
}

// TestVanilla26ATeleportIsTakenWholeAndAnsweredOnce is the same on 26.1.2.
func TestVanilla26ATeleportIsTakenWholeAndAnsweredOnce(t *testing.T) {
	lane := lane26()
	teleportScenario(t, lane, lane.start(t))
}

// teleportScenario moves the player from the server console and requires the
// client to arrive, answer correctly, and not walk away again.
//
// A teleport differs from a correction in intent and, on 775, in mechanism: it
// carries an identifier the client must send back, and a server that never
// receives the confirmation keeps resending. It also differs in what it can
// prove — the destination is one the client did not choose and could not have
// predicted, so unlike a refused move it really does have to be adopted.
func teleportScenario(t *testing.T, lane lane, server *vanilla.Server) {
	session := connectLive(t, lane, server)

	from := session.bot.World().Player
	// Thirty blocks along X, at the height the player is already at. The world
	// is flat, so the destination has the same floor under it: teleporting to a
	// height would test falling as well, and a scenario that tests two things
	// fails ambiguously.
	to := simgeom.Vec3{X: from.X + 30, Y: from.Y, Z: from.Z}

	// Stop walking first. Input queued before a teleport describes a world that
	// no longer exists, and this scenario is about the teleport rather than
	// about what the loop does with stale intent.
	session.loop.Input(simmovement.Input{})

	// Everything the login sequence said, discarded before the teleport is
	// sent. On 775 the placing position carries a teleport identifier of its
	// own and is confirmed like any other, so counting confirmations over the
	// whole session would count the login's and call one teleport two.
	drainEvents(session.raw)

	command := fmt.Sprintf("tp %s %v %v %v", username, to.X, to.Y, to.Z)
	if err := server.Console(command); err != nil {
		t.Fatalf("Console %q: %v", command, err)
	}

	await(t, "the client to arrive where it was sent", server, func() bool {
		at := session.bot.World().Player

		return at.X > to.X-1 && at.X < to.X+1
	})

	// Let it settle where it landed. A client that answered a teleport wrongly
	// gets sent again, and on 775 a client that never confirmed gets the same
	// teleport repeated for as long as the server cares to send it.
	run(session, simmovement.Input{}, settleTicks)

	events := drainEvents(session.raw)

	// 1. The confirmation, where the protocol has one.
	//
	// This is the whole of what differs between the two versions here, and it is
	// stated on both sides rather than skipped on one: a shared implementation
	// that sent a confirmation on 47 would be sending a packet that does not
	// exist, and one that omitted it on 775 would look stuck to the server.
	if lane.confirms != "" {
		var confirmations int
		for _, one := range events {
			if sent, ok := one.(event.PacketSent); ok && sent.Packet == lane.confirms {
				confirmations++
			}
		}
		// Once. A client that sends none looks stuck to the server, which
		// resends; a client that sends two desynchronises the identifier
		// sequence. Both are failures and they are opposite ones, so the
		// assertion is an equality rather than a floor.
		if confirmations != 1 {
			t.Errorf("the client sent %d %s for one teleport, want exactly 1",
				confirmations, lane.confirms)
		}
		t.Logf("%s: %d %s sent", lane.name, confirmations, lane.confirms)
	} else {
		for _, one := range events {
			if sent, ok := one.(event.PacketSent); ok && sent.Packet == "teleport_confirm" {
				t.Fatal("the client sent a teleport confirmation on a protocol that " +
					"has no such packet")
			}
		}
	}

	// 2. The client is where it was sent, and the prediction agrees.
	//
	// Both halves are asserted because they fail differently: an observation
	// that arrived without the prediction adopting it is a loop still walking
	// from where it used to be, and a prediction that moved without the
	// observation is a loop inventing a position.
	observed := session.bot.World().Player
	body, ok := session.loop.Predicted()
	if !ok {
		t.Fatal("the loop stopped predicting")
	}
	at := feetOf(body)
	t.Logf("%s: teleported to (%v %v %v); the server has (%v %v %v) and the loop %+v",
		lane.name, to.X, to.Y, to.Z, observed.X, observed.Y, observed.Z, at)

	for _, axis := range []struct {
		name             string
		got, want, allow float64
	}{
		{"x", observed.X, to.X, 1},
		{"z", observed.Z, to.Z, 1},
		{"predicted x", at.X, to.X, 1},
		{"predicted z", at.Z, to.Z, 1},
	} {
		if diff := axis.got - axis.want; diff > axis.allow || diff < -axis.allow {
			t.Errorf("%s = %v after a teleport to %v", axis.name, axis.got, axis.want)
		}
	}

	// 3. The server did not have to say it twice.
	if repeats := len(server.Matching("moved wrongly")) + len(server.Matching("moved too quickly")); repeats > 0 {
		t.Errorf("the server complained %d times after a teleport it sent itself:\n%s",
			repeats, strings.Join(server.Matching("moved"), "\n"))
	}
}

// TestVanillaADisconnectMidActionAppliesNothingUnconfirmed is Task 7 on 1.8.9.
func TestVanillaADisconnectMidActionAppliesNothingUnconfirmed(t *testing.T) {
	lane := lane1_8()
	disconnectScenario(t, lane, lane.start(t))
}

// TestVanilla26ADisconnectMidActionAppliesNothingUnconfirmed is the same on
// 26.1.2.
func TestVanilla26ADisconnectMidActionAppliesNothingUnconfirmed(t *testing.T) {
	lane := lane26()
	disconnectScenario(t, lane, lane.start(t))
}

// disconnectScenario kills the server under a walking client.
//
// A change set is computed against a revision. If the connection dies before
// the server confirms that revision, applying the change set writes a world
// state the server never agreed to, and a caller that reconnects starts from a
// fiction. The other failure is the opposite one: dropping events the client
// had already observed, so that the disconnect is the first a caller hears of
// anything.
//
// The server is killed rather than stopped, because a stopped server
// disconnects its clients with a reason and that is the ordinary path. This is
// the one where the connection simply stops answering.
func disconnectScenario(t *testing.T, lane lane, server *vanilla.Server) {
	session := connectLive(t, lane, server)

	events, err := session.bot.Subscribe(event.DomainSession, 1024)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	observed := session.bot.World().Player
	before := simgeom.Vec3{X: observed.X, Y: observed.Y, Z: observed.Z}

	// Walking, and mid-action when the floor goes away.
	session.loop.Input(simmovement.Input{Forward: 1})
	server.Kill()

	// Wait returns, and what it returns is nil.
	//
	// That is the contract rather than a defect, and the plan assumed otherwise:
	// a connection ending is not this client's failure, so Wait reports nil for
	// a session that ended however it ended. What must not happen is Wait
	// hanging — a caller blocked forever on a server that is gone has no way to
	// find out that it is gone.
	waited := make(chan error, 1)
	go func() { waited <- session.bot.Wait() }()
	select {
	case err := <-waited:
		if err != nil {
			t.Logf("%s: Wait reported %v", lane.name, err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("Wait did not return after the server was killed under the client")
	}

	// Whatever the loop had in flight has nowhere to go. Give it the time it
	// would need to apply something it should not.
	time.Sleep(500 * time.Millisecond)
	_ = session.loop.Close()

	// 1. The observed world did not move.
	//
	// The prediction may be anywhere — it is a prediction, and predicting past
	// the last confirmation is what it is for. What must not have happened is
	// the observed world adopting it: that is the state a reconnecting caller
	// reads, and it must hold what the server confirmed and nothing else.
	after := session.bot.World().Player
	moved := simgeom.Vec3{X: after.X, Y: after.Y, Z: after.Z}
	if distance(before, moved) > 1e-9 {
		t.Errorf("the observed world moved from %+v to %+v after the connection died; "+
			"unconfirmed prediction was applied to it", before, moved)
	}

	// 2. Everything already observed still published, and the session's end is
	// the last thing on the subscription.
	names := drainNames(t, events)
	if len(names) == 0 {
		t.Fatal("the subscription published nothing at all")
	}
	var ended bool
	for _, name := range names {
		if name == event.NameSessionDisconnected || name == event.NameSessionClosed {
			ended = true
		}
	}
	if !ended {
		t.Errorf("events = %v, want the session's end among them", names)
	}
	if last := names[len(names)-1]; last != event.NameSessionClosed &&
		last != event.NameSessionDisconnected {
		t.Errorf("the last event is %q; something published after the connection died", last)
	}
	t.Logf("%s: %d session events, ending %q", lane.name, len(names), names[len(names)-1])
}

// distance is the straight-line distance between two positions.
func distance(a, b simgeom.Vec3) float64 {
	dx, dy, dz := a.X-b.X, a.Y-b.Y, a.Z-b.Z

	return math.Sqrt(dx*dx + dy*dy + dz*dz)
}

// drainNames reads the names of everything a subscription holds.
func drainNames(t *testing.T, subscription *client.Subscription) []event.Name {
	t.Helper()

	var names []event.Name
	for _, one := range drainEvents(subscription) {
		names = append(names, one.Name())
	}

	return names
}
