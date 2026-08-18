package client

import (
	"context"
	"fmt"
	"time"

	"github.com/go-theft-craft/headless-minecraft/version"
)

// Dig breaks one block, as a sequence rather than as a packet.
//
// Vanilla sends start-digging, waits out the break time, and then sends
// finish-digging, and the server validates the elapsed time between the two. A
// client that sends only the finish packet breaks blocks instantly and is the
// first thing an anti-cheat notices — which matters, because M10 has an
// anti-cheat lane that has to stay quiet.
//
// breaking is how long the block takes, and it is the caller's rather than
// this package's. The break time is a rule, and the rules are
// minecraft-simulation's: mining.BreakTicks computes it from a hardness, a tool
// speed, a harvest legality, an enchantment, two effects, and whether the
// player is submerged or airborne, and this package models none of those. A
// client that guessed would finish early on every block it guessed wrong about,
// which is the one failure the sequence exists to avoid. A zero or negative
// duration is a block that breaks on the tick it is hit, which is a real case:
// a torch, tall grass, and anything else with no hardness at all.
//
// Cancelling the context sends the cancel stage rather than the finish stage,
// and returns the context's error. A dig that was abandoned must not claim to
// have completed: the server would refuse it, and a client that believed
// itself would predict a block that is still there.
//
// It sends no arm swing. Vanilla swings on every tick of a dig, and a caller
// that wants the animation sends version.ActionSwing alongside — this is the
// sequence the server validates, not the one it renders.
func (c *Client) Dig(
	ctx context.Context, block version.BlockPos, face version.Face, breaking time.Duration,
) error {
	if err := c.Do(ctx, version.ActionDig{
		Block: block, Face: face, Stage: version.DigStart,
	}); err != nil {
		return fmt.Errorf("start digging: %w", err)
	}

	if breaking > 0 {
		timer := time.NewTimer(breaking)
		defer timer.Stop()

		select {
		case <-timer.C:
		case <-ctx.Done():
			return c.cancelDig(ctx, block, face)
		case <-c.done:
			return ErrClosed
		}
	}

	if err := c.Do(ctx, version.ActionDig{
		Block: block, Face: face, Stage: version.DigFinish,
	}); err != nil {
		return fmt.Errorf("finish digging: %w", err)
	}

	return nil
}

// cancelDig tells the server the dig was abandoned.
//
// The cancel goes out on a context stripped of the cancellation that caused it,
// because the write would otherwise be refused by the very thing it is
// reporting — and a server left believing a dig is still in progress refuses
// the next one at that position.
func (c *Client) cancelDig(ctx context.Context, block version.BlockPos, face version.Face) error {
	cause := ctx.Err()

	if err := c.Do(context.WithoutCancel(ctx), version.ActionDig{
		Block: block, Face: face, Stage: version.DigCancel,
	}); err != nil {
		return fmt.Errorf("cancel digging after %w: %w", cause, err)
	}

	return cause
}
