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
		chunkReducer(w.Chunks()),
		environmentReducer(w.Environment()),
	}
}

// Protocol 775 names its game-state reasons rather than numbering them, and
// the generated codec maps the wire number to the name. Only the game-mode
// reason is the player's; the weather ones are the environment's.
//
// **The two protocols number the weather reasons oppositely.** Here 1 is
// `start_raining` and 2 is `stop_raining`; on protocol 47, 1 ends rain and 2
// begins it. Names rather than numbers is what makes that safe to read on this
// side, and it is why protocol 47's half names its constants too.
const (
	reasonGameMode     = "change_game_mode"
	reasonRainStart    = "start_raining"
	reasonRainStop     = "stop_raining"
	reasonRainLevel    = "rain_level_change"
	reasonThunderLevel = "thunder_level_change"
)

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

	case *gen.PlayClientboundDamageEvent:
		if isLocal(ctx, value.EntityID) {
			p.Damaged(c, damage775(value))
		}

	case *gen.PlayClientboundDeathCombatEvent:
		// 775 names no killer here. Protocol 47's combat event does, which is
		// the reverse of how the two protocols treat damage.
		if isLocal(ctx, value.PlayerID) {
			p.Died(c, 0, false)
		}

	case *gen.PlayClientboundEntityStatus:
		if isLocal(ctx, value.EntityID) && value.EntityStatus == statusDeath {
			p.Died(c, 0, false)
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
		// Unlike 47, 775 says what did the damage and who is behind it.
		if !isLocal(ctx, value.EntityID) {
			entities.Damaged(c, value.EntityID, damage775(value))
		}

	case *gen.PlayClientboundDeathCombatEvent:
		if !isLocal(ctx, value.PlayerID) {
			entities.Died(c, value.PlayerID, 0, false)
		}

	case *gen.PlayClientboundEntityStatus:
		// Only death is read from this packet on 775. Hurt moved to the damage
		// event in a version this profile is well past, and reading status 2 as
		// damage here would republish something the server no longer means by
		// it.
		if !isLocal(ctx, value.EntityID) && value.EntityStatus == statusDeath {
			entities.Died(c, value.EntityID, 0, false)
		}

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

// statusDeath is the entity status both protocols use for a living entity
// dying. Protocol 47's half of this reducer names it for the same reason: the
// schema numbers the statuses and names none of them.
const statusDeath int8 = 3

// damage775 reads protocol 775's damage event into the shared attribution.
//
// The two source entity IDs are sent offset by one, with zero meaning the
// server named nobody, because entity 0 is a legal entity and could not
// otherwise be told from an absent one. Undoing the offset here rather than in
// the world is the same rule the fixed-point positions follow: wire encoding
// stays in the adapter.
func damage775(value *gen.PlayClientboundDamageEvent) event.Damage {
	damage := event.Damage{TypeID: value.SourceTypeID, Typed: true}
	damage.CauseID, damage.Attributed = sourceEntity775(value.SourceCauseID)
	damage.DirectID, damage.Direct = sourceEntity775(value.SourceDirectID)
	if position := value.SourcePosition; position != nil {
		damage.X, damage.Y, damage.Z = position.X, position.Y, position.Z
		damage.Positioned = true
	}

	return damage
}

func sourceEntity775(raw int32) (int32, bool) {
	if raw <= 0 {
		return 0, false
	}

	return raw - 1, true
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

// chunkReducer decodes the packets that describe terrain.
//
// **Sections are stored as received and not decoded.** Protocol 775 sends each
// section as a paletted container, and the paletted container's encoding has
// changed across recent versions in ways this repository cannot check: the
// shared protocol module treats `chunkData` as an opaque byte array, nothing
// here generates or validates the section format, and no captured 26.1 chunk
// exists in this repository to test a decoder against. A decoder written from
// memory would return wrong blocks silently, and M8's collision and M9's
// digging would then be built on them.
//
// So the world keeps the bytes, reports block lookups in a 775 section as
// undecodable, and everything that does not need block access — chunk load and
// unload, block changes, block entities, light — works. Implementing the
// decoder needs one captured 26.1 chunk as a fixture, which `mcproto capture`
// can record.
func chunkReducer(chunks *world.Chunks) world.Func {
	return func(_ *world.Context, batch version.Batch, c *event.Collector) error {
		for _, packet := range batch.Packets {
			reduceChunkPacket(chunks, packet, c)
		}

		return nil
	}
}

func reduceChunkPacket(chunks *world.Chunks, packet protocol.Packet, c *event.Collector) {
	switch value := packet.Value.(type) {
	case *gen.PlayClientboundMapChunk:
		// One section entry carrying the whole column's bytes: the column is
		// not split, because splitting it means parsing the format this
		// client does not yet read.
		chunks.Loaded(c, world.ChunkPos{X: value.X, Z: value.Z}, []world.SectionData{
			{Y: 0, Raw: value.ChunkData},
		}, nil)

	case *gen.PlayClientboundUnloadChunk:
		chunks.Unloaded(c, world.ChunkPos{X: value.ChunkX, Z: value.ChunkZ})

	case *gen.PlayClientboundUpdateLight:
		chunks.LightChanged(c, world.ChunkPos{X: value.ChunkX, Z: value.ChunkZ}, nil)

	case *gen.PlayClientboundBlockChange:
		chunks.BlocksChanged(c, []world.BlockChange{{
			Pos:   blockPos775(value.Location),
			State: uint32(value.Type),
		}})

	case *gen.PlayClientboundTileEntityData:
		chunks.BlockEntityChanged(c, blockPos775(value.Location), value.NBTData)
	}
}

func blockPos775(p gen.Position) world.BlockPos {
	return world.BlockPos{X: p.X, Y: int32(p.Y), Z: p.Z}
}

// environmentReducer decodes the packets that describe the world's scalars.
//
// This protocol carries five facts protocol 47 has no packet for at all — view
// distance, simulation distance, the view centre, the tick rate, and game
// rules — which is why the snapshot reports them as unknown rather than zero
// for a whole 47 session.
func environmentReducer(environment *world.Environment) world.Func {
	return func(_ *world.Context, batch version.Batch, c *event.Collector) error {
		for _, packet := range batch.Packets {
			reduceEnvironmentPacket(environment, packet, c)
		}

		return nil
	}
}

// protocol 47 half for why it is not split.
//
//nolint:gocyclo // One switch over one protocol's environment packets; see the
func reduceEnvironmentPacket(
	environment *world.Environment,
	packet protocol.Packet,
	c *event.Collector,
) {
	switch value := packet.Value.(type) {
	case *gen.PlayClientboundUpdateTime:
		// 26.1 replaced 47's single time-of-day number with a set of clocks.
		// The world reads a time from it when there is exactly one, and keeps
		// them all either way.
		environment.TimeChanged(c, value.Age, 0, false, clocks775(value.ClockUpdates))

	// Six packets, one WorldBorderChanged — the same six actions protocol 47
	// packs into one packet.
	case *gen.PlayClientboundInitializeWorldBorder:
		environment.BorderInitialized(c, event.Border{
			X: value.X, Z: value.Z,
			OldDiameter: value.OldDiameter, NewDiameter: value.NewDiameter,
			Speed:          int64(value.Speed),
			PortalBoundary: value.PortalTeleportBoundary,
			WarningTime:    value.WarningTime,
			WarningBlocks:  value.WarningBlocks,
		})

	case *gen.PlayClientboundWorldBorderCenter:
		environment.BorderCenter(c, value.X, value.Z)

	case *gen.PlayClientboundWorldBorderSize:
		environment.BorderSize(c, value.Diameter)

	case *gen.PlayClientboundWorldBorderLerpSize:
		environment.BorderLerp(c, value.OldDiameter, value.NewDiameter, int64(value.Speed))

	case *gen.PlayClientboundWorldBorderWarningDelay:
		environment.BorderWarningTime(c, value.WarningTime)

	case *gen.PlayClientboundWorldBorderWarningReach:
		environment.BorderWarningBlocks(c, value.WarningBlocks)

	case *gen.PlayClientboundDifficulty:
		// Already a name here; protocol 47 sends the number this maps from.
		environment.DifficultyChanged(c, value.Difficulty, value.DifficultyLocked)

	case *gen.PlayClientboundExplosion:
		explosion := event.WorldExplosionOccurred{
			X: value.Center.X, Y: value.Center.Y, Z: value.Center.Z,
			Radius: value.Radius,
		}
		// 775 makes the knockback optional, where 47 always sends one.
		if knockback := value.PlayerKnockback; knockback != nil {
			explosion.KnockbackX = knockback.X
			explosion.KnockbackY = knockback.Y
			explosion.KnockbackZ = knockback.Z
			explosion.Knocked = true
		}
		environment.Explosion(c, explosion)

	case *gen.PlayClientboundWorldEvent:
		environment.WorldEvent(c, value.EffectID, blockPos775(value.Location), value.Data, value.Global)

	case *gen.PlayClientboundGameStateChange:
		switch value.Reason {
		case reasonRainStart:
			environment.WeatherChanged(c, true)
		case reasonRainStop:
			environment.WeatherChanged(c, false)
		case reasonRainLevel:
			environment.RainLevelChanged(c, value.GameMode)
		case reasonThunderLevel:
			environment.ThunderLevelChanged(c, value.GameMode)
		}

	case *gen.PlayClientboundUpdateViewDistance:
		environment.ViewDistanceChanged(c, value.ViewDistance)

	case *gen.PlayClientboundSimulationDistance:
		environment.SimulationDistanceChanged(c, value.Distance)

	case *gen.PlayClientboundUpdateViewPosition:
		environment.ViewCenterChanged(c, value.ChunkX, value.ChunkZ)

	case *gen.PlayClientboundSetTickingState:
		environment.TickingStateChanged(c, value.TickRate, value.IsFrozen)

	case *gen.PlayClientboundGameRuleValues:
		rules := make(map[string]string, len(value.Values))
		for _, rule := range value.Values {
			rules[rule.Name] = rule.Value
		}
		environment.GameRulesChanged(c, rules)
	}
}

// clocks775 keeps the server's clocks as sent. The ID space is the server's
// own and nothing here interprets it.
func clocks775(updates []gen.PlayClientboundUpdateTimeClockUpdatesItem) []event.Clock {
	clocks := make([]event.Clock, 0, len(updates))
	for _, update := range updates {
		clocks = append(clocks, event.Clock{
			ID: update.ID, TotalTicks: update.TotalTicks,
			PartialTick: update.PartialTick, Rate: update.Rate,
		})
	}

	return clocks
}
