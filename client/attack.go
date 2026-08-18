package client

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/go-theft-craft/headless-minecraft/version"
)

// ErrUnknownEntity reports an attack on an entity the client is not tracking.
// A vanilla client cannot point at what it has never been shown, so an attack
// on an untracked identifier is a caller's bug rather than a packet to send.
var ErrUnknownEntity = errors.New("entity is not tracked")

// ErrOutOfReach reports an attack the client refused to send.
//
// The refusal is deliberate and happens before the wire: the server has its
// own reach check, and a client that sends attacks the server rejects is a
// client an anti-cheat notices. M10 has an anti-cheat lane that must stay
// quiet, so the client's reach is the stricter of the two ends.
var ErrOutOfReach = errors.New("target is out of reach")

// ErrNotDead reports a respawn requested for a living player. Both protocols
// treat one as a protocol error, and some servers answer it with a
// disconnect, so it is refused here rather than sent and regretted.
var ErrNotDead = errors.New("the player is not dead")

// The combat numbers, which are M9.6's rather than this package's: the rules
// are minecraft-simulation's, and these are transcribed from its combat
// profiles so this package does not load a full game dataset to learn one
// float. TestTheReachNumbersAgreeWithTheSimulation is what keeps the two
// spellings from drifting — the same shape the observability names use.
//
// Survival attack reach is 3.0 on both versions; creative is where they part
// (6.0 on 1.8.9, 5.0 on 26.1.2), and the eye sits 1.62 above the feet on
// both.
const (
	attackReach           = 3.0
	creativeAttackReach18 = 6.0
	creativeAttackReach26 = 5.0
	eyeHeight             = 1.62
)

// gameModeCreative is creative's wire value, shared by both protocols.
const gameModeCreative = 1

// protocol47 is the 1.8.9 adapter's identifier, which is where the two
// versions' creative reaches part.
const protocol47 = "java/1.8.9"

// Attack swings at one entity: the arm animation and the attack interaction
// together, in that order.
//
// Vanilla sends an animation with the attack. A client that sends the
// interact packet alone hits without swinging, which is visible to other
// players and to an anti-cheat.
//
// The reach is measured from the player's eye to the target's reported
// position and refused beyond this version's number for the player's game
// mode. The position is the target's feet — the client tracks no collision
// boxes — so the check is slightly stricter than the game's own box measure,
// which is the right side to miss on: a refusal here costs a retry a tick
// later, and a packet the server rejects costs an anti-cheat flag.
//
// It waits for nothing. An attack is not a request with a reply: what the
// client learns — damage, knockback, a death — arrives as entity events like
// every other change, and a caller that needs them subscribes before calling
// this.
func (c *Client) Attack(ctx context.Context, target int32) error {
	snapshot := c.World()

	entity, ok := snapshot.Entities.Get(target)
	if !ok {
		return fmt.Errorf("attack %d: %w", target, ErrUnknownEntity)
	}

	player := snapshot.Player
	if !player.Placed {
		return fmt.Errorf("attack %d: %w", target, ErrNotInPlay)
	}

	reach := c.attackReach(player.GameMode)
	dx := entity.X - player.X
	dy := entity.Y - (player.Y + eyeHeight)
	dz := entity.Z - player.Z
	if distance := math.Sqrt(dx*dx + dy*dy + dz*dz); distance > reach {
		return fmt.Errorf("attack %d at %.2f blocks with %.2f of reach: %w",
			target, distance, reach, ErrOutOfReach)
	}

	if err := c.Do(ctx, version.ActionSwing{Hand: version.MainHand}); err != nil {
		return fmt.Errorf("swing: %w", err)
	}
	if err := c.Do(ctx, version.ActionInteract{
		Entity: target, Kind: version.InteractAttack, Hand: version.MainHand,
	}); err != nil {
		return fmt.Errorf("attack %d: %w", target, err)
	}

	return nil
}

// attackReach is this version's number for one game mode.
func (c *Client) attackReach(gameMode uint8) float64 {
	if gameMode != gameModeCreative {
		return attackReach
	}
	if c.profile.ID == protocol47 {
		return creativeAttackReach18
	}

	return creativeAttackReach26
}
