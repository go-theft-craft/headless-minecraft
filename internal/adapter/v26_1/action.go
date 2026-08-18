package v26_1

import (
	protocol "github.com/go-theft-craft/minecraft-protocol"
	gen "github.com/go-theft-craft/minecraft-protocol/generated/java/v26_1"

	"github.com/go-theft-craft/headless-minecraft/version"
)

// EncodeAction implements version.Adapter.
//
// Protocol 775 carries the same four movement packets as protocol 47, with the
// standing state replaced by a flags structure that also reports whether the
// tick's horizontal motion was blocked. That extra claim is why the intents
// carry a collision flag at all: dropping it would make this protocol send less
// than the game does.
func (adapter) EncodeAction(action version.Action) (protocol.Packet, error) {
	switch value := action.(type) {
	case version.ActionMove:
		return play775("position", &gen.PlayServerboundPosition{
			X: value.X, Y: value.Y, Z: value.Z,
			Flags: flags775(value.OnGround, value.HorizontalCollision),
		}), nil

	case version.ActionLook:
		return play775("look", &gen.PlayServerboundLook{
			Yaw: value.Yaw, Pitch: value.Pitch,
			Flags: flags775(value.OnGround, value.HorizontalCollision),
		}), nil

	case version.ActionMoveLook:
		return play775("position_look", &gen.PlayServerboundPositionLook{
			X: value.X, Y: value.Y, Z: value.Z,
			Yaw: value.Yaw, Pitch: value.Pitch,
			Flags: flags775(value.OnGround, value.HorizontalCollision),
		}), nil

	case version.ActionGround:
		return play775("flying", &gen.PlayServerboundFlying{
			Flags: flags775(value.OnGround, value.HorizontalCollision),
		}), nil

	case version.ActionInput:
		return play775("player_input", &gen.PlayServerboundPlayerInput{
			Inputs: gen.PlayServerboundPlayerInputInputsFlags{
				Forward: value.Forward, Backward: value.Backward,
				Left: value.Left, Right: value.Right,
				// The protocol calls sneaking Shift, because the packet is
				// named after the key rather than the posture.
				Jump: value.Jump, Shift: value.Sneak, Sprint: value.Sprint,
			},
		}), nil

	case version.ActionSprint:
		// EntityID is the player's own, and the server reads it from the
		// connection rather than from here: a real client fills it in and a
		// server that disagreed would have no way to act on the difference.
		// Zero is what this sends, because the adapter is not told the entity
		// ID and inventing a wrong one would be worse than sending none.
		return play775("entity_action", &gen.PlayServerboundEntityAction{
			ActionID: sprintCommand775(value.Sprinting),
		}), nil

	case version.ActionCommand:
		return play775("chat_command", &gen.PlayServerboundChatCommand{
			Command: value.Command,
		}), nil

	case version.ActionRespawn:
		// 775 names its client commands where 47 numbers them, so the action
		// is the string the protocol declares rather than a zero.
		return play775("client_command", &gen.PlayServerboundClientCommand{
			ActionID: respawnCommand775,
		}), nil

	default:
		return protocol.Packet{}, version.UnsupportedAction(ProtocolID, action)
	}
}

// respawnCommand775 is the client-command action that asks to respawn.
const respawnCommand775 = "perform_respawn"

// sprintCommand775 names the entity action that starts or stops sprinting.
//
// 775 names these where 47 numbers them, and the names are the protocol's own.
// That difference is why this action is encoded here and refused there: 47's
// schema declares actionId as a bare varint with no names attached, so a 47
// implementation would have to hardcode numbers this repository has not
// measured, and a wrong one is a different action performed confidently.
func sprintCommand775(sprinting bool) string {
	if sprinting {
		return "start_sprinting"
	}

	return "stop_sprinting"
}

// flags775 builds the movement flags this protocol carries.
func flags775(onGround, horizontalCollision bool) gen.MovementFlags {
	return gen.MovementFlags{OnGround: onGround, HasHorizontalCollision: horizontalCollision}
}

// packetValue is what every generated packet type provides.
type packetValue interface {
	PacketID() int32
}

// play775 wraps a serverbound play value in its packet envelope.
func play775(name string, value packetValue) protocol.Packet {
	return protocol.Packet{
		State:     gen.StatePlay,
		Direction: protocol.DirectionServerbound,
		ID:        value.PacketID(),
		Name:      name,
		Value:     value,
	}
}
