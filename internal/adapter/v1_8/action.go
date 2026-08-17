package v1_8

import (
	protocol "github.com/go-theft-craft/minecraft-protocol"
	gen "github.com/go-theft-craft/minecraft-protocol/generated/java/v1_8"

	"github.com/go-theft-craft/headless-minecraft/version"
)

// EncodeAction implements version.Adapter.
//
// Protocol 47 carries movement in four packets and the choice between them is
// information the server reads, so each intent maps to exactly one packet and
// none is approximated by another. The protocol has no field for a horizontal
// collision, so that part of an intent is dropped here rather than being folded
// into a flag it does not have.
func (adapter) EncodeAction(action version.Action) (protocol.Packet, error) {
	switch value := action.(type) {
	case version.ActionMove:
		return play47("position", &gen.PlayServerboundPosition{
			X: value.X, Y: value.Y, Z: value.Z, OnGround: value.OnGround,
		}), nil

	case version.ActionLook:
		return play47("look", &gen.PlayServerboundLook{
			Yaw: value.Yaw, Pitch: value.Pitch, OnGround: value.OnGround,
		}), nil

	case version.ActionMoveLook:
		return play47("position_look", &gen.PlayServerboundPositionLook{
			X: value.X, Y: value.Y, Z: value.Z,
			Yaw: value.Yaw, Pitch: value.Pitch, OnGround: value.OnGround,
		}), nil

	case version.ActionGround:
		return play47("flying", &gen.PlayServerboundFlying{OnGround: value.OnGround}), nil

	case version.ActionRespawn:
		// Protocol 47 numbers the client commands and respawn is zero. The
		// same packet asks for statistics at one, which nothing here sends.
		return play47("client_command", &gen.PlayServerboundClientCommand{
			Payload: respawnCommand47,
		}), nil

	default:
		return protocol.Packet{}, version.UnsupportedAction(ProtocolID, action)
	}
}

// respawnCommand47 is the client-command action that asks to respawn.
const respawnCommand47 int32 = 0

// packetValue is what every generated packet type provides. It is declared here
// so play47 can name one bound rather than one type per packet.
type packetValue interface {
	PacketID() int32
}

// play47 wraps a serverbound play value in its packet envelope.
func play47(name string, value packetValue) protocol.Packet {
	return protocol.Packet{
		State:     gen.StatePlay,
		Direction: protocol.DirectionServerbound,
		ID:        value.PacketID(),
		Name:      name,
		Value:     value,
	}
}
