package v1_8

import (
	protocol "github.com/go-theft-craft/minecraft-protocol"
	gen "github.com/go-theft-craft/minecraft-protocol/generated/java/v1_8"

	"github.com/go-theft-craft/headless-minecraft/event"
	"github.com/go-theft-craft/headless-minecraft/version"
	"github.com/go-theft-craft/headless-minecraft/world"
)

// This file is protocol 47's half of observed world state: it decodes packets
// and calls the world's mutators. The state, the arithmetic, and the events
// are shared with protocol 775, because those are the parts the two protocols
// agree on; only the decoding differs, and that is what lives here.

// Reducers returns this protocol's reducers in the order the world must apply
// them.
//
// The order is a contract, not a preference: the player reducer learns the
// local entity ID and every reducer after it reads that ID to tell the local
// player apart from every other entity.
func Reducers(w *world.World) []world.Reducer {
	return []world.Reducer{
		playerReducer(w.Player()),
	}
}

// gameStateChange reasons that protocol 47 packs into one packet. The value
// carries a different meaning for each, which is why the reducer switches on
// the reason rather than on the packet.
const (
	reasonRainEnd   uint8 = 1
	reasonRainStart uint8 = 2
	reasonGameMode  uint8 = 3
)

// playerReducer decodes the packets that describe the local player.
func playerReducer(p *world.Player) world.Func {
	return func(ctx *world.Context, batch version.Batch, c *event.Collector) error {
		for _, packet := range batch.Packets {
			reducePlayerPacket(ctx, p, packet, c)
		}

		return nil
	}
}

func reducePlayerPacket(
	ctx *world.Context,
	p *world.Player,
	packet protocol.Packet,
	c *event.Collector,
) {
	switch value := packet.Value.(type) {
	case *gen.PlayClientboundLogin:
		p.Login(c, value.EntityID, dimensionName(value.Dimension), value.GameMode)
		// Every reducer after this one reads the local entity ID from the
		// context, which is how an entity packet naming the local player
		// reaches the player rather than the entity store.
		ctx.Local = world.LocalRef{EntityID: value.EntityID, Known: true}

	case *gen.PlayClientboundPosition:
		p.Move(c, value.X, value.Y, value.Z, value.Yaw, value.Pitch, relativeFlags(value.Flags))

	case *gen.PlayClientboundUpdateHealth:
		p.Health(c, value.Health, value.Food, value.FoodSaturation)

	case *gen.PlayClientboundExperience:
		p.Experience(c, value.ExperienceBar, value.Level, value.TotalExperience)

	case *gen.PlayClientboundAbilities:
		p.Abilities(c, value.Flags, value.FlyingSpeed, value.WalkingSpeed)

	case *gen.PlayClientboundGameStateChange:
		// One packet, several unrelated meanings, discriminated by a reason
		// byte. Only the game-mode one belongs to this domain; the weather
		// reasons are the environment's.
		if value.Reason == reasonGameMode {
			p.GameMode(c, uint8(value.GameMode))
		}

	case *gen.PlayClientboundRespawn:
		p.Respawn(c, dimensionName(int8(value.Dimension)), value.Gamemode)

	case *gen.PlayClientboundHeldItemSlot:
		p.HeldSlot(c, int32(value.Slot))

	case *gen.PlayClientboundEntityEffect:
		if isLocal(ctx, value.EntityID) {
			p.EffectApplied(c, int32(value.EffectID), int32(value.Amplifier), value.Duration)
		}

	case *gen.PlayClientboundRemoveEntityEffect:
		if isLocal(ctx, value.EntityID) {
			p.EffectRemoved(c, int32(value.EffectID))
		}
	}
}

// isLocal reports whether an entity packet is about the local player. An
// entity packet naming the local player's ID updates the player domain, and
// one naming any other ID does not.
func isLocal(ctx *world.Context, entityID int32) bool {
	return ctx.Local.Known && ctx.Local.EntityID == entityID
}

// relativeFlags unpacks protocol 47's position flag byte. Protocol 775 sends
// the same information as a struct of booleans, which is why the world takes
// the unpacked form.
func relativeFlags(flags int8) world.Relative {
	return world.Relative{
		X:     flags&0x01 != 0,
		Y:     flags&0x02 != 0,
		Z:     flags&0x04 != 0,
		Yaw:   flags&0x08 != 0,
		Pitch: flags&0x10 != 0,
	}
}
