package client

import (
	"context"
	"errors"
	"testing"

	gen1_8data "github.com/go-theft-craft/minecraft-protocol/generated/java/v1_8"
	gen26data "github.com/go-theft-craft/minecraft-protocol/generated/java/v26_1"
	"github.com/go-theft-craft/minecraft-simulation/combat"
	simv1_8 "github.com/go-theft-craft/minecraft-simulation/profile/java/v1_8"
	simv26_1 "github.com/go-theft-craft/minecraft-simulation/profile/java/v26_1"

	"github.com/go-theft-craft/headless-minecraft/event"
	"github.com/go-theft-craft/headless-minecraft/version"
	"github.com/go-theft-craft/headless-minecraft/version/java"
	"github.com/go-theft-craft/headless-minecraft/world"
)

// targetID is the entity every attack test swings at.
const targetID int32 = 7

// combatClient is an actionClient with a world: a placed player at (0, 64, 0)
// and a target spawned distance blocks away along +X.
func combatClient(t *testing.T, profile version.WireProfile, w sender, distance float64) *Client {
	t.Helper()

	c := actionClient(t, profile, w)
	c.world = world.New()

	collector := &event.Collector{}
	c.world.Player().Login(collector, 1, "overworld", 0)
	c.world.Player().Move(collector, 0, 64, 0, 0, 0, world.Relative{})
	c.world.Entities().Spawned(collector, targetID, "", "pig", distance, 64, 0, 0, 0)

	return c
}

// sentNames lists the packets a sender recorded, in order.
func sentNames(s *recordingSender) []string {
	names := make([]string, 0, len(s.sent))
	for _, packet := range s.sent {
		names = append(names, packet.Name)
	}

	return names
}

func TestAnAttackSwingsBeforeItHits(t *testing.T) {
	t.Parallel()

	// Vanilla sends an animation with the attack. A client that sends the
	// interact packet alone hits without swinging, which is visible to other
	// players and to an anti-cheat. The names differ per protocol; the order
	// does not.
	for name, test := range map[string]struct {
		profile version.WireProfile
		want    []string
	}{
		"1.8.9":  {java.Java1_8(), []string{"arm_animation", "use_entity"}},
		"26.1.2": {java.Current(), []string{"arm_animation", "attack"}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			sender := &recordingSender{}
			c := combatClient(t, test.profile, sender, 2)

			if err := c.Attack(context.Background(), targetID); err != nil {
				t.Fatalf("Attack: %v", err)
			}

			got := sentNames(sender)
			if len(got) != len(test.want) {
				t.Fatalf("sent %v, want %v", got, test.want)
			}
			for at, want := range test.want {
				if got[at] != want {
					t.Fatalf("sent %v, want %v", got, test.want)
				}
			}
		})
	}
}

func TestAnOutOfReachAttackIsNotSent(t *testing.T) {
	t.Parallel()

	// The client's reach must be the stricter of the two ends. Sending
	// attacks the server rejects is what an anti-cheat lane notices, and M10
	// has one that must stay quiet.
	sender := &recordingSender{}
	c := combatClient(t, java.Java1_8(), sender, 20)

	err := c.Attack(context.Background(), targetID)
	if !errors.Is(err, ErrOutOfReach) {
		t.Fatalf("Attack error = %v, want ErrOutOfReach", err)
	}
	if len(sender.sent) != 0 {
		t.Fatalf("an out-of-reach attack still sent %v", sentNames(sender))
	}
}

func TestAnAttackOnAnUntrackedEntityIsNotSent(t *testing.T) {
	t.Parallel()

	// A vanilla client cannot point at what it has never been shown, so an
	// untracked identifier is a caller's bug rather than a packet.
	sender := &recordingSender{}
	c := combatClient(t, java.Java1_8(), sender, 2)

	err := c.Attack(context.Background(), 99)
	if !errors.Is(err, ErrUnknownEntity) {
		t.Fatalf("Attack error = %v, want ErrUnknownEntity", err)
	}
	if len(sender.sent) != 0 {
		t.Fatalf("an unknown-entity attack still sent %v", sentNames(sender))
	}
}

func TestCreativeReachIsLongerAndVersionOwned(t *testing.T) {
	t.Parallel()

	// 5.5 blocks: beyond every survival reach, within 1.8.9's creative 6.0,
	// and beyond 26.1.2's creative 5.0. One distance separates all three
	// rules.
	for name, test := range map[string]struct {
		profile version.WireProfile
		reaches bool
	}{
		"1.8.9":  {java.Java1_8(), true},
		"26.1.2": {java.Current(), false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			sender := &recordingSender{}
			c := combatClient(t, test.profile, sender, 5.5)

			collector := &event.Collector{}
			c.world.Player().GameMode(collector, gameModeCreative)

			err := c.Attack(context.Background(), targetID)
			if test.reaches && err != nil {
				t.Fatalf("Attack: %v", err)
			}
			if !test.reaches && !errors.Is(err, ErrOutOfReach) {
				t.Fatalf("Attack error = %v, want ErrOutOfReach", err)
			}
		})
	}
}

func TestRespawnIsRefusedWhileAlive(t *testing.T) {
	t.Parallel()

	// A respawn request from a living player is a protocol error on both
	// versions and a disconnect on some servers.
	sender := &recordingSender{}
	c := combatClient(t, java.Java1_8(), sender, 2)

	err := c.Do(context.Background(), ActionRespawn{})
	if !errors.Is(err, ErrNotDead) {
		t.Fatalf("Do error = %v, want ErrNotDead", err)
	}
	if len(sender.sent) != 0 {
		t.Fatalf("a refused respawn still sent %v", sentNames(sender))
	}
}

func TestRespawnIsSentOnceThePlayerIsDead(t *testing.T) {
	t.Parallel()

	sender := &recordingSender{}
	c := combatClient(t, java.Java1_8(), sender, 2)

	collector := &event.Collector{}
	c.world.Player().Died(collector, 0, false)

	if err := c.Do(context.Background(), ActionRespawn{}); err != nil {
		t.Fatalf("Do: %v", err)
	}
	if got := sentNames(sender); len(got) != 1 || got[0] != "client_command" {
		t.Fatalf("sent %v, want the client-command respawn", got)
	}
}

func TestTheCombatNumbersAgreeWithTheSimulation(t *testing.T) {
	t.Parallel()

	// The rules are minecraft-simulation's combat profiles; this package
	// carries transcribed copies so an attack does not load a game dataset to
	// learn one float. This is what keeps the two spellings from drifting —
	// the same shape the observability names use. It became possible offline
	// when the released module first carried combat, in v0.3.0; before that
	// the constants were pinned by literals here.
	set, err := gen1_8data.Data()
	if err != nil {
		t.Fatalf("load the 1.8.9 data set: %v", err)
	}
	old, err := simv1_8.New(set)
	if err != nil {
		t.Fatalf("v1_8.New: %v", err)
	}
	set26, err := gen26data.Data()
	if err != nil {
		t.Fatalf("load the 26.1.2 data set: %v", err)
	}
	modern, err := simv26_1.New(set26)
	if err != nil {
		t.Fatalf("v26_1.New: %v", err)
	}

	for name, check := range map[string]struct{ got, want float64 }{
		"survival":        {attackReach, old.(combat.Fighter).Reach().Attack},
		"survival 26":     {attackReach, modern.(combat.Fighter).Reach().Attack},
		"creative 1.8.9":  {creativeAttackReach18, old.(combat.Fighter).CreativeReach().Attack},
		"creative 26.1.2": {creativeAttackReach26, modern.(combat.Fighter).CreativeReach().Attack},
		"eye height":      {eyeHeight, old.(combat.Fighter).EyeHeight()},
		"eye height 26":   {eyeHeight, modern.(combat.Fighter).EyeHeight()},
	} {
		if check.got != check.want {
			t.Errorf("%s: this package says %v and the simulation says %v",
				name, check.got, check.want)
		}
	}
}
