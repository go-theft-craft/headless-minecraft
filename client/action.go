package client

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-theft-craft/headless-minecraft/event"
	"github.com/go-theft-craft/headless-minecraft/version"
)

// ErrNotInPlay reports an action attempted before the server placed the player.
// A movement packet sent during login is not a movement the server will accept;
// it is a protocol error that ends the connection.
var ErrNotInPlay = errors.New("client has not reached play")

// The outbound intents, re-exported so a caller that already imports this
// package does not also have to import version to name one. They are aliases,
// not wrappers: version.ActionMove and client.ActionMove are the same type, and
// an adapter that encodes one encodes the other.
type (
	// Action is one outbound intent.
	Action = version.Action
	// ActionMove reports a new position.
	ActionMove = version.ActionMove
	// ActionLook reports a new rotation.
	ActionLook = version.ActionLook
	// ActionMoveLook reports a new position and rotation together.
	ActionMoveLook = version.ActionMoveLook
	// ActionGround reports the standing state alone.
	ActionGround = version.ActionGround
	// ActionRespawn asks the server to respawn a dead player.
	ActionRespawn = version.ActionRespawn
)

// Do sends one outbound intent.
//
// It is the only way to act on a connection, and it is safe to call from any
// goroutine: calls are serialized against each other and against the replies the
// read loop writes, so two intents never interleave on the wire and a keepalive
// answer never lands inside one.
//
// It returns an error rather than swallowing one. A caller predicting locally
// has to know that its intent never left, because a prediction the server never
// heard about is a prediction that will be corrected.
//
// Do refuses before the player is placed and after the client is closed. Neither
// is a transport failure, and both are worth telling a caller apart from one.
func (c *Client) Do(ctx context.Context, action Action) error {
	if action == nil {
		return fmt.Errorf("%w: nil action", ErrInvalidClient)
	}

	writer, adapter, err := c.outbound()
	if err != nil {
		return err
	}

	packet, err := adapter.EncodeAction(action)
	if err != nil {
		return fmt.Errorf("encode %s: %w", action.ActionKind(), err)
	}

	// The same lock the loop takes for its own replies. Holding it across the
	// write is what makes the ordering guarantee true rather than probable.
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	if err := writer.Write(ctx, packet); err != nil {
		return fmt.Errorf("write %s: %w", packet.Name, err)
	}

	// Reported like any other sent packet, so a subscriber watching the wire
	// sees actions and answers in one stream.
	c.events.publish(event.One(event.PacketSent{
		State:  string(packet.State),
		Packet: packet.Name,
		ID:     packet.ID,
	}, unrevised))

	return nil
}

// outbound returns what Do needs, or reports why the client cannot act.
func (c *Client) outbound() (sender, version.Adapter, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	switch {
	case c.closed:
		return nil, nil, ErrClosed
	case c.writer == nil:
		return nil, nil, fmt.Errorf("%w: no connection", ErrNotInPlay)
	case !c.inPlay:
		return nil, nil, fmt.Errorf("%w: still negotiating", ErrNotInPlay)
	}

	return c.writer, c.profile.Adapter, nil
}

// enterPlay records that the server placed the player, after which Do works.
//
// The read loop calls this on the batch that reports readiness, before the ready
// signal reaches Connect, so a caller that returns from Connect and immediately
// acts finds the client willing.
func (c *Client) enterPlay() {
	c.mu.Lock()
	c.inPlay = true
	c.mu.Unlock()
}
