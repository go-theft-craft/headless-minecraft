package v26_1

import (
	protocol "github.com/go-theft-craft/minecraft-protocol"
	gen "github.com/go-theft-craft/minecraft-protocol/generated/java/v26_1"

	"github.com/go-theft-craft/headless-minecraft/event"
	"github.com/go-theft-craft/headless-minecraft/version"
	"github.com/go-theft-craft/headless-minecraft/world"
)

// This file is protocol 775's half of observed world state. It decodes this
// protocol's packets and calls the same world mutators protocol 47's half
// calls: the state, the arithmetic, and the events are shared, and only the
// decoding differs.

// Reducers returns this protocol's reducers in the order the world must apply
// them. The order is a contract; see the protocol 47 half for why.
func Reducers(w *world.World) []world.Reducer {
	return []world.Reducer{
		playerReducer(w.Player()),
	}
}

// Protocol 775 names its game-state reasons rather than numbering them, and
// the generated codec maps the wire number to the name. Only the game-mode
// reason is the player's; the weather ones are the environment's.
const reasonGameMode = "change_game_mode"

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
		p.Login(c, value.EntityID, value.WorldState.Name, gameModeNumber(value.WorldState.Gamemode))
		ctx.Local = world.LocalRef{EntityID: value.EntityID, Known: true}

	case *gen.PlayClientboundPosition:
		// 775 sends the offsets in their own fields and says which of the
		// nine components are relative. The world resolves them the same way
		// it resolves 47's flag byte.
		relative := world.Relative{
			X: value.Flags.X, Y: value.Flags.Y, Z: value.Flags.Z,
			Yaw: value.Flags.Yaw, Pitch: value.Flags.Pitch,
		}
		p.Move(c, value.X, value.Y, value.Z, value.Yaw, value.Pitch, relative)

	case *gen.PlayClientboundPlayerRotation:
		// A rotation with no position, which protocol 47 cannot send.
		p.Look(c, value.Yaw, value.Pitch, world.Relative{
			Yaw: value.RelativeYaw, Pitch: value.RelativePitch,
		})

	case *gen.PlayClientboundUpdateHealth:
		p.Health(c, value.Health, value.Food, value.FoodSaturation)

	case *gen.PlayClientboundExperience:
		p.Experience(c, value.ExperienceBar, value.Level, value.TotalExperience)

	case *gen.PlayClientboundAbilities:
		p.Abilities(c, value.Flags, value.FlyingSpeed, value.WalkingSpeed)

	case *gen.PlayClientboundGameStateChange:
		// The mode arrives as a float, because this one packet's value field
		// carries a rain level for one reason and a mode number for another.
		if value.Reason == reasonGameMode {
			p.GameMode(c, uint8(value.GameMode))
		}

	case *gen.PlayClientboundRespawn:
		p.Respawn(c, value.WorldState.Name, gameModeNumber(value.WorldState.Gamemode))

	case *gen.PlayClientboundHeldItemSlot:
		p.HeldSlot(c, value.Slot)

	case *gen.PlayClientboundSetCooldown:
		p.Cooldown(c, value.CooldownGroup, value.CooldownTicks)

	case *gen.PlayClientboundEntityEffect:
		if isLocal(ctx, value.EntityID) {
			p.EffectApplied(c, value.EffectID, value.Amplifier, value.Duration)
		}

	case *gen.PlayClientboundRemoveEntityEffect:
		if isLocal(ctx, value.EntityID) {
			p.EffectRemoved(c, value.EffectID)
		}
	}
}

// isLocal reports whether an entity packet is about the local player.
func isLocal(ctx *world.Context, entityID int32) bool {
	return ctx.Local.Known && ctx.Local.EntityID == entityID
}
