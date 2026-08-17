package v26_1

import (
	"slices"
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
		containerReducer(w.Containers()),
		registryReducer(w.Registries()),
		payloadReducer(w.Payloads()),
		chatReducer(w.Chat()),
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

// containerReducer decodes the packets that describe open menus.
//
// This protocol carries three facts protocol 47 has no packet for: a recipe
// book, a trade list, and the state ID that sequences every inventory change.
func containerReducer(containers *world.Containers) world.Func {
	return func(_ *world.Context, batch version.Batch, c *event.Collector) error {
		for _, packet := range batch.Packets {
			reduceContainerPacket(containers, packet, c)
		}

		return nil
	}
}

// protocol 47 half for why it is not split.
//
//nolint:gocyclo // One switch over one protocol's container packets; see the
func reduceContainerPacket(
	containers *world.Containers,
	packet protocol.Packet,
	c *event.Collector,
) {
	switch value := packet.Value.(type) {
	case *gen.PlayClientboundOpenWindow:
		containers.Opened(c, event.ContainerOpened{
			ContainerID: value.WindowID,
			// A number here, a name on protocol 47. It indexes the session's
			// menu registry, which the registry domain receives in
			// configuration; until a caller resolves it there, the server's own
			// identifier is what the world keeps.
			MenuType: menuType(value.InventoryType),
			// No title. 775 sends it as a chat component, and rendering one is
			// a presentation decision this library does not make for a
			// consumer -- the same call the disconnect reasons make. A caller
			// that wants the text renders it from the raw packet.
		})

	case *gen.PlayClientboundOpenHorseWindow:
		containers.Opened(c, event.ContainerOpened{
			ContainerID: value.WindowID,
			MenuType:    "java/26.1:menu/horse",
			SlotCount:   value.NbSlots,
			EntityID:    value.EntityID, EntityKnown: true,
		})

	case *gen.PlayClientboundCloseWindow:
		containers.Closed(c, value.WindowID)

	case *gen.PlayClientboundWindowItems:
		items := make(map[int32]any, len(value.Items))
		for slot, item := range value.Items {
			items[int32(slot)] = item
		}
		containers.SlotsChanged(c, value.WindowID, items, value.StateID, true)
		// The same packet carries the cursor, which belongs to no menu.
		containers.CursorChanged(c, value.CarriedItem, value.CarriedItem != nil)

	case *gen.PlayClientboundSetSlot:
		// 775 has a dedicated cursor packet, and it still honours the legacy
		// addressing protocol 47 uses.
		if value.WindowID == cursorContainer && value.Slot == cursorSlot {
			containers.CursorChanged(c, value.Item, value.Item != nil)

			return
		}
		containers.SlotsChanged(c,
			value.WindowID, map[int32]any{int32(value.Slot): value.Item}, value.StateID, true)

	case *gen.PlayClientboundSetCursorItem:
		containers.CursorChanged(c, value.Contents, value.Contents != nil)

	case *gen.PlayClientboundSetPlayerInventory:
		containers.PlayerSlotChanged(c, value.SlotID, value.Contents, value.Contents != nil)

	case *gen.PlayClientboundCraftProgressBar:
		containers.PropertyChanged(c, value.WindowID, int32(value.Property), int32(value.Value))

	case *gen.PlayClientboundCraftRecipeResponse:
		containers.CraftResponse(c, value.WindowID)

	case *gen.PlayClientboundDeclareRecipes:
		containers.RecipesDeclared(c, len(value.Recipes))

	case *gen.PlayClientboundRecipeBookAdd:
		// The display ID is the only stable identifier the entry carries, and
		// it is what the removal packet names too.
		added := make([]int32, 0, len(value.Entries))
		for _, entry := range value.Entries {
			added = append(added, entry.Recipe.DisplayID)
		}
		containers.RecipesChanged(c, added, nil, value.Replace)

	case *gen.PlayClientboundRecipeBookRemove:
		containers.RecipesChanged(c, nil, value.RecipeIds, false)

	case *gen.PlayClientboundTradeList:
		containers.TradesChanged(c, event.ContainerTradesChanged{
			ContainerID: value.WindowID, Count: len(value.Trades),
			VillagerLevel: value.VillagerLevel, Experience: value.Experience,
			Regular: value.IsRegularVillager, CanRestock: value.CanRestock,
		})
	}
}

// The cursor's legacy address, which both protocols still use: slot -1 of
// container -1.
const (
	cursorContainer int32 = -1
	cursorSlot      int16 = -1
)

// menuType renders protocol 775's numeric menu type, on the same rule the
// entity type follows: the number indexes a session registry, and the server's
// own identifier is kept rather than a vanilla name guessed from it.
func menuType(kind int32) string { return "java/26.1:menu/" + strconv.Itoa(int(kind)) }

// registryReducer decodes the packets that describe the server's own
// vocabulary.
//
// **Registry data arrives in the configuration state, before play.** It
// reaches this reducer because the client owns the configuration phase rather
// than letting the login negotiator consume it, and because the world applies
// batches in every state rather than only in play. The state the batch arrived
// in is recorded with the registry, so "not sent yet" and "will not be sent"
// stay distinguishable.
func registryReducer(registries *world.Registries) world.Func {
	return func(ctx *world.Context, batch version.Batch, c *event.Collector) error {
		for _, packet := range batch.Packets {
			reduceRegistryPacket(ctx, registries, packet, c)
		}

		return nil
	}
}

func reduceRegistryPacket(
	ctx *world.Context,
	registries *world.Registries,
	packet protocol.Packet,
	c *event.Collector,
) {
	switch value := packet.Value.(type) {
	case *gen.ConfigurationClientboundRegistryData:
		// The entry order is the registry's numeric ID space: entry 0 is the
		// ID a packet means by 0. An unknown namespace is kept as sent, which
		// is the whole point on a modded server.
		entries := make([]string, 0, len(value.Entries))
		for _, entry := range value.Entries {
			entries = append(entries, entry.Key)
		}
		registries.DataReceived(c, value.ID, entries, ctx.State)

	case *gen.ConfigurationClientboundTags:
		counts := make(map[string]int, len(value.Tags))
		for _, group := range value.Tags {
			counts[group.TagType] = len(group.Tags)
		}
		registries.TagsReceived(c, counts)

	case *gen.PlayClientboundTags:
		counts := make(map[string]int, len(value.Tags))
		for _, group := range value.Tags {
			counts[group.TagType] = len(group.Tags)
		}
		registries.TagsReceived(c, counts)

	case *gen.PlayClientboundDeclareCommands:
		registries.CommandsReceived(c, len(value.Nodes))

	case *gen.PlayClientboundPlayerInfo:
		reducePlayerList775(registries, value, c)

	case *gen.PlayClientboundPlayerRemove:
		removed := make([]string, 0, len(value.Players))
		for _, uuid := range value.Players {
			removed = append(removed, uuid.String())
		}
		registries.PlayerListChanged(c, nil, removed)
	}
}

// reducePlayerList775 reads 775's action bitfield. One packet can add a player
// and change another's latency, which is why every field carries whether it
// was supplied.
func reducePlayerList775(
	registries *world.Registries,
	value *gen.PlayClientboundPlayerInfo,
	c *event.Collector,
) {
	action := value.Action
	changes := make([]world.PlayerListChange, 0, len(value.Data))
	for _, item := range value.Data {
		change := world.PlayerListChange{UUID: item.UUID.String()}
		if action.AddPlayer {
			change.Name, change.SetName = item.Player.True.Name, true
		}
		if action.UpdateGameMode {
			change.GameMode, change.SetGameMode = item.Gamemode.True, true
		}
		if action.UpdateLatency {
			change.Latency, change.SetLatency = item.Latency.True, true
		}
		if action.UpdateListed {
			// The schema decodes the flag as a number, and anything non-zero
			// is listed.
			change.Listed, change.SetListed = item.Listed.True != 0, true
		}
		changes = append(changes, change)
	}

	registries.PlayerListChanged(c, changes, nil)
}

// payloadReducer keeps the last plugin message per channel, in both
// configuration and play: 775 sends custom payloads in each, and a message
// that arrived before play is exactly the one a caller cannot have subscribed
// in time for.
func payloadReducer(payloads *world.Payloads) world.Func {
	return func(_ *world.Context, batch version.Batch, _ *event.Collector) error {
		for _, packet := range batch.Packets {
			switch value := packet.Value.(type) {
			case *gen.PlayClientboundCustomPayload:
				payloads.Received(value.Channel, value.Data)
			case *gen.ConfigurationClientboundCustomPayload:
				payloads.Received(value.Channel, value.Data)
			}
		}

		return nil
	}
}

// Protocol 775's boss-bar and scoreboard actions. The boss bar numbers its
// actions where the team packet names its modes.
const (
	bossBarAdd        int32 = 0
	bossBarRemove     int32 = 1
	bossBarUpdateHP   int32 = 2
	objectiveRemove   int8  = 1
	teamModeRemove          = "remove"
	teamModeAdd             = "add"
	teamModeJoin            = "join"
	teamModeLeaveName       = "leave"
)

// chatReducer decodes the packets that describe chat and the UI around it.
//
// 775 splits into three packets what protocol 47 sent as one chat packet —
// player, system, and profileless — and one ChatReceived carrying a kind
// reports all three, rather than three events for one fact.
func chatReducer(chat *world.Chat) world.Func {
	return func(_ *world.Context, batch version.Batch, c *event.Collector) error {
		for _, packet := range batch.Packets {
			reduceChatPacket(chat, packet, c)
		}

		return nil
	}
}

// protocol's chat packets, and splitting it would hide which packet feeds what.
//
//nolint:gocyclo // One switch over one
func reduceChatPacket(chat *world.Chat, packet protocol.Packet, c *event.Collector) {
	switch value := packet.Value.(type) {
	case *gen.PlayClientboundPlayerChat:
		// The one message either protocol sends as plain text as well as a
		// component. Signed says a signature arrived; nothing validates it,
		// and claiming otherwise would be worse than not doing it.
		chat.Received(c, world.Message{
			Kind: event.ChatKindPlayer, Text: value.PlainMessage,
			Sender: value.SenderUUID.String(),
			Index:  value.GlobalIndex, IndexKnown: true,
			Signed: value.Signature != nil,
		}, false)

	case *gen.PlayClientboundSystemChat:
		chat.Received(c, world.Message{Kind: event.ChatKindSystem}, value.IsActionBar)

	case *gen.PlayClientboundProfilelessChat:
		// A sender name with no player profile behind it, which protocol 47
		// has no equivalent for.
		chat.Received(c, world.Message{Kind: event.ChatKindProfileless}, false)

	case *gen.PlayClientboundHideMessage:
		chat.Removed(c, value.ID)

	case *gen.PlayClientboundSetTitleText, *gen.PlayClientboundSetTitleSubtitle:
		chat.TitleChanged(c, event.ChatTitleChanged{})

	case *gen.PlayClientboundSetTitleTime:
		chat.TitleChanged(c, event.ChatTitleChanged{
			FadeIn: value.FadeIn, Stay: value.Stay, FadeOut: value.FadeOut, TimesKnown: true,
		})

	case *gen.PlayClientboundClearTitles:
		chat.TitleChanged(c, event.ChatTitleChanged{Cleared: true, Reset: value.Reset})

	case *gen.PlayClientboundActionBar:
		chat.ActionBarChanged(c)

	case *gen.PlayClientboundBossBar:
		bar := event.ChatBossBarChanged{UUID: value.EntityUUID.String()}
		switch value.Action {
		case bossBarRemove:
			bar.Removed = true
		case bossBarAdd:
			bar.Health, bar.HealthKnown = value.Health.Case0, true
		case bossBarUpdateHP:
			bar.Health, bar.HealthKnown = value.Health.Case2, true
		}
		chat.BossBarChanged(c, bar)

	case *gen.PlayClientboundScoreboardObjective:
		chat.ObjectiveChanged(c, value.Name, value.Action == objectiveRemove)

	case *gen.PlayClientboundScoreboardScore:
		chat.ScoreChanged(c, value.ScoreName, value.ItemName, value.Value, false)

	case *gen.PlayClientboundResetScore:
		// 775 removes a score through its own packet. With no objective named
		// it resets the entry everywhere, which the world models as removing
		// it from the objective it names — nothing, when none is named.
		objective := ""
		if value.ObjectiveName != nil {
			objective = *value.ObjectiveName
		}
		chat.ScoreChanged(c, objective, value.EntityName, 0, true)

	case *gen.PlayClientboundScoreboardDisplayObjective:
		chat.ObjectiveDisplayed(c, value.Name, value.Position)

	case *gen.PlayClientboundTeams:
		reduceTeam775(chat, value, c)

	case *gen.PlayClientboundAdvancements:
		chat.AdvancementsChanged(c, event.ChatAdvancementsChanged{
			Added:   len(value.AdvancementMapping),
			Removed: len(value.Identifiers),
			Reset:   value.Reset,
		})

	case *gen.PlayClientboundSoundEffect:
		chat.SoundPlayed(c, event.ChatSoundPlayed{
			Sound: soundName(value.Sound),
			X:     float64(value.X) / 8, Y: float64(value.Y) / 8, Z: float64(value.Z) / 8,
			Positioned: true, Volume: value.Volume, Pitch: value.Pitch,
		})

	case *gen.PlayClientboundEntitySoundEffect:
		chat.SoundPlayed(c, event.ChatSoundPlayed{
			Sound:    soundName(value.Sound),
			EntityID: value.EntityID, EntityKnown: true,
			Volume: value.Volume, Pitch: value.Pitch,
		})

	case *gen.PlayClientboundStopSound:
		chat.SoundPlayed(c, event.ChatSoundPlayed{Stopped: true})

	case *gen.PlayClientboundStatistics:
		chat.StatisticsReceived(c, len(value.Entries))

	case *gen.PlayClientboundShowDialog:
		chat.DialogShown(c, false)

	case *gen.PlayClientboundClearDialog:
		chat.DialogShown(c, true)

	case *gen.PlayClientboundTabComplete:
		matches := make([]string, 0, len(value.Matches))
		for _, match := range value.Matches {
			matches = append(matches, match.Match)
		}
		chat.TabCompleted(c, event.ChatTabCompleted{
			Matches: matches, TransactionID: value.TransactionID, TransactionKnown: true,
		})
	}
}

func reduceTeam775(chat *world.Chat, value *gen.PlayClientboundTeams, c *event.Collector) {
	changed := event.ChatTeamsChanged{Team: value.Team, Mode: value.Mode}
	switch value.Mode {
	case teamModeRemove:
		changed.Removed = true
	case teamModeAdd:
		changed.Players = slices.Clone(value.Players.Add)
	case teamModeJoin:
		changed.Players = slices.Clone(value.Players.Join)
	case teamModeLeaveName:
		changed.Players = nil
	}

	chat.TeamChanged(c, changed)
}

// soundName reads a sound's name out of the registry reference 775 sends. A
// reference by ID carries no name, and inventing one from the generated
// registry would name a sound a modded server may not mean.
func soundName(sound gen.ItemSoundHolder) string {
	if sound.Inline == nil {
		return ""
	}

	return sound.Inline.SoundName
}
