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

	default:
		return protocol.Packet{}, version.UnsupportedAction(ProtocolID, action)
	}
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
