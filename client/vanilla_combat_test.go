//go:build vanilla

// M9.6's live scenarios: an attack that swings before it hits, a refusal the
// wire never sees, and a real death answered by a real respawn — on both
// versions.
//
// They are here rather than beside the combat rules because the rules are
// minecraft-simulation's and these tests are about the client: that it sends
// what vanilla sends, in vanilla's order, and refuses what vanilla could not
// have sent.
package client_test

import (
	"errors"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/go-theft-craft/headless-minecraft/client"
	"github.com/go-theft-craft/headless-minecraft/event"
	"github.com/go-theft-craft/headless-minecraft/internal/vanilla"
)

// combatTimeout bounds each wait on the server. Attacks and deaths are
// answered within a tick or two; the slack is for a loaded machine.
const combatTimeout = 45 * time.Second

// combatLane is one version's combat setup: the movement lane plus what this
// file needs that movement never did — a summon command and the version's own
// packet names for a swing and an attack.
type combatLane struct {
	lane
	// summon renders the console command that spawns a motionless pig.
	summon func(x, y, z float64) string
	// swing and attack are the packet names this version's adapter encodes
	// the two halves of an attack into.
	swing, attack string
}

func combatLane1_8() combatLane {
	return combatLane{
		lane: lane1_8(),
		summon: func(x, y, z float64) string {
			// 1.8 names entities in PascalCase and takes NoAI as a plain byte.
			return fmt.Sprintf("summon Pig %.1f %.1f %.1f {NoAI:1}", x, y, z)
		},
		swing:  "arm_animation",
		attack: "use_entity",
	}
}

func combatLane26() combatLane {
	return combatLane{
		lane: lane26(),
		summon: func(x, y, z float64) string {
			return fmt.Sprintf("summon minecraft:pig %.1f %.1f %.1f {NoAI:1b}", x, y, z)
		},
		swing: "arm_animation",
		// The pinned 775 schema splits attack onto its own packet.
		attack: "attack",
	}
}

// connectForCombat brings a client into play with the terrain under it, and
// keeps the connection vanilla-shaped: a stationary vanilla client still
// reports its ground state every tick, and a 1.8.9 server syncs health — the
// packet a death arrives in — only from inside the handler those reports
// drive. A client that goes silent between actions is a client the server
// never tells it died, which this file found the hard way.
func connectForCombat(t *testing.T, lane combatLane, server *vanilla.Server) *client.Client {
	t.Helper()

	bot := lane.connect(t, server.Addr())

	ctx := t.Context()
	if err := bot.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v\n%s", err, tail(server.Log(), 25))
	}
	t.Cleanup(func() { _ = bot.Close() })

	waitForTerrain(t, server, bot)

	go func() {
		ticker := time.NewTicker(50 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				// Errors are the test's to notice through what it awaits: a
				// dead player's idle reports are refused by nothing, and a
				// closed client ends the loop through the context.
				_ = bot.Do(ctx, client.ActionGround{OnGround: true})
			}
		}
	}()

	return bot
}

// summonTarget spawns a motionless pig offset from the player and returns the
// entity identifier the server assigned it, read from the client's own
// tracking — which is the identifier an attack has to name.
func summonTarget(
	t *testing.T, lane combatLane, server *vanilla.Server, bot *client.Client, dx, dz float64,
) int32 {
	t.Helper()

	player := bot.World().Player
	x, y, z := player.X+dx, player.Y, player.Z+dz
	if err := server.Console(lane.summon(x, y, z)); err != nil {
		t.Fatalf("summon: %v", err)
	}

	deadline := time.Now().Add(combatTimeout)
	for time.Now().Before(deadline) {
		for id, tracked := range bot.World().Entities.Tracked {
			ex, ez := tracked.X-x, tracked.Z-z
			if math.Sqrt(ex*ex+ez*ez) < 2 {
				return id
			}
		}
		time.Sleep(50 * time.Millisecond)
	}

	t.Fatalf("the summoned target was never tracked\nserver log:\n%s", tail(server.Log(), 25))

	return 0
}

// awaitName blocks until the subscription delivers one event with the name.
func awaitName(
	t *testing.T, subscription *client.Subscription, want event.Name, server *vanilla.Server,
) {
	t.Helper()

	deadline := time.After(combatTimeout)
	for {
		select {
		case one, ok := <-subscription.C():
			if !ok {
				t.Fatalf("the subscription closed while waiting for %s: %v",
					want, subscription.Err())
			}
			if one.Name() == want {
				return
			}
		case <-deadline:
			t.Fatalf("never saw %s\nserver log:\n%s", want, tail(server.Log(), 25))
		}
	}
}

// sentCombatPackets reads every packet name the client has sent so far,
// keeping only this lane's two combat packets.
func sentCombatPackets(subscription *client.Subscription, lane combatLane) []string {
	var names []string
	for _, one := range drainEvents(subscription) {
		sent, ok := one.(event.PacketSent)
		if !ok {
			continue
		}
		if sent.Packet == lane.swing || sent.Packet == lane.attack {
			names = append(names, sent.Packet)
		}
	}

	return names
}

func TestVanillaAnAttackSwingsAndHits(t *testing.T) {
	lane := combatLane1_8()
	attackScenario(t, lane, lane.start(t))
}

func TestVanilla26AnAttackSwingsAndHits(t *testing.T) {
	lane := combatLane26()
	attackScenario(t, lane, lane.start(t))
}

// attackScenario is the landed half: the swing goes out before the attack,
// and the server answers with a hurt entity.
func attackScenario(t *testing.T, lane combatLane, server *vanilla.Server) {
	bot := connectForCombat(t, lane, server)

	raw, err := bot.Subscribe(event.DomainRaw, 8192)
	if err != nil {
		t.Fatalf("Subscribe raw: %v", err)
	}
	entities, err := bot.Subscribe(event.DomainEntities, 8192)
	if err != nil {
		t.Fatalf("Subscribe entities: %v", err)
	}

	target := summonTarget(t, lane, server, bot, 2, 0)
	drainEvents(raw)

	if err := bot.Attack(t.Context(), target); err != nil {
		t.Fatalf("Attack: %v\n%s", err, tail(server.Log(), 25))
	}

	// Vanilla sends an animation with the attack, in that order. A client
	// that hits without swinging is visible to other players and to an
	// anti-cheat.
	sent := sentCombatPackets(raw, lane)
	if len(sent) != 2 || sent[0] != lane.swing || sent[1] != lane.attack {
		t.Fatalf("sent %v, want [%s %s]", sent, lane.swing, lane.attack)
	}

	// The hit is the server's to confirm, and it confirms by hurting the pig.
	deadline := time.After(combatTimeout)
	for {
		select {
		case one, ok := <-entities.C():
			if !ok {
				t.Fatalf("the entity subscription closed: %v", entities.Err())
			}
			if damaged, is := one.(event.EntityDamaged); is && damaged.EntityID == target {
				return
			}
		case <-deadline:
			t.Fatalf("the server never reported the target hurt\nserver log:\n%s",
				tail(server.Log(), 25))
		}
	}
}

func TestVanillaAnOutOfReachAttackIsNotSent(t *testing.T) {
	// One version suffices: the refusal is version-neutral client code, and
	// the unit tests already pin both versions' numbers. What this adds is a
	// real server that stays silent because nothing reached it.
	lane := combatLane1_8()
	server := lane.start(t)
	bot := connectForCombat(t, lane, server)

	raw, err := bot.Subscribe(event.DomainRaw, 8192)
	if err != nil {
		t.Fatalf("Subscribe raw: %v", err)
	}

	target := summonTarget(t, lane, server, bot, 20, 0)
	drainEvents(raw)

	if err := bot.Attack(t.Context(), target); !errors.Is(err, client.ErrOutOfReach) {
		t.Fatalf("Attack error = %v, want ErrOutOfReach", err)
	}
	if sent := sentCombatPackets(raw, lane); len(sent) != 0 {
		t.Fatalf("an out-of-reach attack was sent anyway: %v", sent)
	}
}

func TestVanillaRespawnAfterDeathReturnsThePlayerToPlay(t *testing.T) {
	lane := combatLane1_8()
	respawnScenario(t, lane, lane.start(t))
}

func TestVanilla26RespawnAfterDeathReturnsThePlayerToPlay(t *testing.T) {
	lane := combatLane26()
	respawnScenario(t, lane, lane.start(t))
}

// respawnScenario is a real death on a real server, and the player back in
// play afterwards. The primitive it exercises was built with M8.8's follow-on;
// what was never proved before this is the round trip.
func respawnScenario(t *testing.T, lane combatLane, server *vanilla.Server) {
	bot := connectForCombat(t, lane, server)

	events, err := bot.Subscribe(event.DomainPlayer, 8192)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	// A respawn request from a living player is a protocol error on both
	// versions and a disconnect on some servers, so it must fail before the
	// wire — here, against the same server that would have disconnected.
	if err := bot.Do(t.Context(), client.ActionRespawn{}); !errors.Is(err, client.ErrNotDead) {
		t.Fatalf("Do while alive = %v, want ErrNotDead", err)
	}

	if err := server.Console("kill " + username); err != nil {
		t.Fatalf("kill: %v", err)
	}
	awaitName(t, events, event.NamePlayerDied, server)

	if err := bot.Do(t.Context(), client.ActionRespawn{}); err != nil {
		t.Fatalf("Do respawn: %v", err)
	}
	awaitName(t, events, event.NamePlayerRespawned, server)

	if bot.World().Player.Dead {
		t.Fatal("the player is still dead after a confirmed respawn")
	}
}
