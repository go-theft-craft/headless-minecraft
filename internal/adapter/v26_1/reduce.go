package v26_1

import (
	"strconv"

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
		entityReducer(w.Entities()),
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

// Protocol 775 sends entity positions as doubles and relative moves in
// sixteenths of a block, where 47 used fixed-point thirty-seconds. Both become
// blocks before the world sees them.
const (
	deltaScale775 = 4096.0
	angleScale775 = 360.0 / 256.0
)

// entityReducer decodes the packets that describe every entity except the
// local player.
func entityReducer(entities *world.Entities) world.Func {
	return func(ctx *world.Context, batch version.Batch, c *event.Collector) error {
		for _, packet := range batch.Packets {
			reduceEntityPacket(ctx, entities, packet, c)
		}

		return nil
	}
}

// protocol 47 half for why it is not split.
//
//nolint:gocyclo // One switch over one protocol's entity packets; see the
func reduceEntityPacket(
	ctx *world.Context,
	entities *world.Entities,
	packet protocol.Packet,
	c *event.Collector,
) {
	switch value := packet.Value.(type) {
	case *gen.PlayClientboundSpawnEntity:
		entities.Spawned(c, value.EntityID, value.ObjectUUID.String(), entityType(value.Type),
			value.X, value.Y, value.Z, angle775(value.Yaw), angle775(value.Pitch))
		entities.VelocityChanged(c, value.EntityID,
			velocity775(value.Velocity.X), velocity775(value.Velocity.Y), velocity775(value.Velocity.Z))

	case *gen.PlayClientboundEntityDestroy:
		for _, id := range value.EntityIds {
			entities.Removed(c, id)
		}

	// Five packets move an entity in 775, one more than in 47, and all of
	// them produce one EntityMoved.
	case *gen.PlayClientboundRelEntityMove:
		entities.MovedBy(c, value.EntityID,
			delta775(value.DX), delta775(value.DY), delta775(value.DZ), value.OnGround)

	case *gen.PlayClientboundEntityMoveLook:
		entities.MovedBy(c, value.EntityID,
			delta775(value.DX), delta775(value.DY), delta775(value.DZ), value.OnGround)
		entities.Looked(c, value.EntityID, angle775(value.Yaw), angle775(value.Pitch), value.OnGround)

	case *gen.PlayClientboundEntityLook:
		entities.Looked(c, value.EntityID, angle775(value.Yaw), angle775(value.Pitch), value.OnGround)

	case *gen.PlayClientboundEntityTeleport:
		entities.Moved(c, value.EntityID, value.X, value.Y, value.Z,
			angle775(value.Yaw), angle775(value.Pitch), value.OnGround)

	case *gen.PlayClientboundSyncEntityPosition:
		// The server's authoritative correction, which 47 has no equivalent
		// for: absolute position, and the deltas describe the motion it had.
		entities.Moved(c, value.EntityID, value.X, value.Y, value.Z,
			value.Yaw, value.Pitch, value.OnGround)

	case *gen.PlayClientboundEntityHeadRotation:
		entities.HeadLooked(c, value.EntityID, angle775(value.HeadYaw))

	case *gen.PlayClientboundEntityMetadata:
		entities.MetadataChanged(c, value.EntityID, metadata775(value.Metadata))

	case *gen.PlayClientboundEntityEquipment:
		items := make(map[int32]any, len(value.Equipments))
		for _, item := range value.Equipments {
			items[int32(item.Slot)] = item.Item
		}
		entities.EquipmentChanged(c, value.EntityID, items)

	case *gen.PlayClientboundEntityUpdateAttributes:
		attributes := make([]world.Attribute, 0, len(value.Properties))
		for _, property := range value.Properties {
			attributes = append(attributes, world.Attribute{Key: property.Key, Value: property.Value})
		}
		entities.AttributesChanged(c, value.EntityID, attributes)

	case *gen.PlayClientboundEntityVelocity:
		entities.VelocityChanged(c, value.EntityID,
			velocity775(value.Velocity.X), velocity775(value.Velocity.Y), velocity775(value.Velocity.Z))

	case *gen.PlayClientboundSetPassengers:
		entities.PassengersChanged(c, value.EntityID, value.Passengers)

	case *gen.PlayClientboundAttachEntity:
		entities.Attached(c, value.EntityID, value.VehicleID)

	case *gen.PlayClientboundDamageEvent:
		// Unlike 47, 775 says what did the damage.
		entities.Damaged(c, value.EntityID, value.SourceTypeID)

	case *gen.PlayClientboundAnimation:
		entities.Animated(c, value.EntityID, value.Animation)

	case *gen.PlayClientboundCollect:
		entities.ItemCollected(c, value.CollectedEntityID, value.CollectorEntityID, value.PickupItemCount)

	case *gen.PlayClientboundEntityEffect:
		if !isLocal(ctx, value.EntityID) {
			entities.EffectApplied(c, value.EntityID, value.EffectID, value.Amplifier, value.Duration)
		}

	case *gen.PlayClientboundRemoveEntityEffect:
		if !isLocal(ctx, value.EntityID) {
			entities.EffectRemoved(c, value.EntityID, value.EffectID)
		}
	}
}

func delta775(d int16) float64 { return float64(d) / deltaScale775 }

func angle775(a int8) float32 { return float32(a) * angleScale775 }

// velocity775 converts the protocol's own velocity vector back to the
// short-per-axis units both protocols' events report.
func velocity775(v float64) int16 { return int16(v * 8000) }

// metadata775 converts protocol 775's metadata into the world's
// index-addressed form. The type arrives as a name here, where 47 sends a
// number, and both are kept as strings.
func metadata775(items []gen.PlayClientboundEntityMetadataMetadataItem) []world.Metadata {
	entries := make([]world.Metadata, 0, len(items))
	for _, item := range items {
		entries = append(entries, world.Metadata{
			Index: item.Key,
			Type:  item.Type,
			Value: item.Value,
		})
	}

	return entries
}

// entityType renders protocol 775's numeric entity type.
//
// The number indexes the session's entity registry, which the registry domain
// receives in configuration. Until a caller resolves it there, the world keeps
// the server's own identifier rather than guessing a vanilla name.
func entityType(kind int32) string { return "java/26.1:entity/" + strconv.Itoa(int(kind)) }
