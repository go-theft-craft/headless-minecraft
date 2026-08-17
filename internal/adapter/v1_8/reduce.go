package v1_8

import (
	"strconv"

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
		entityReducer(w.Entities()),
		chunkReducer(w.Chunks()),
		environmentReducer(w.Environment()),
		containerReducer(w.Containers()),
		registryReducer(w.Registries()),
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

	case *gen.PlayClientboundEntityStatus:
		// The entity reducer switches on this packet too, for the statuses
		// that name somebody else. Same packet, two domains, each ignoring
		// what is not its own — the arrangement GameStateChange already uses.
		if !isLocal(ctx, value.EntityID) {
			return
		}
		switch value.EntityStatus {
		case statusHurt:
			// Protocol 47 sends no source of any kind. An empty Damage says
			// so, and a caller that wants an attacker here infers one itself.
			p.Damaged(c, event.Damage{})
		case statusDeath:
			p.Died(c, 0, false)
		}

	case *gen.PlayClientboundCombatEvent:
		// Protocol 47's combat event is the one place either protocol names a
		// killer: 775 dropped the field from its death event.
		if value.Event != combatEventEntityDead || !isLocal(ctx, value.PlayerID.Case2) {
			return
		}
		killer, attributed := combatKiller(value.EntityID.Case2)
		p.Died(c, killer, attributed)
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

// Protocol 47 sends entity positions as fixed-point integers with five
// fractional bits, and relative moves in the same units. Dividing here rather
// than in the world is deliberate: 775 sends doubles and sixteenths, and the
// world takes blocks.
const (
	fixedPoint47 = 32.0
	deltaScale47 = 32.0
	// Yaw and pitch are one byte covering a full turn.
	angleScale47 = 360.0 / 256.0
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

// by arm would hide the one thing worth seeing: which packets feed which fact.
//
//nolint:gocyclo // One switch over one protocol's entity packets. Splitting it
func reduceEntityPacket(
	ctx *world.Context,
	entities *world.Entities,
	packet protocol.Packet,
	c *event.Collector,
) {
	switch value := packet.Value.(type) {
	case *gen.PlayClientboundSpawnEntity:
		entities.Spawned(c, value.EntityID, "", objectType(value.Type),
			block47(value.X), block47(value.Y), block47(value.Z),
			angle47(value.Yaw), angle47(value.Pitch))

	case *gen.PlayClientboundSpawnEntityLiving:
		entities.Spawned(c, value.EntityID, "", mobType(value.Type),
			block47(value.X), block47(value.Y), block47(value.Z),
			angle47(value.Yaw), angle47(value.Pitch))
		entities.MetadataChanged(c, value.EntityID, metadata47(value.Metadata))
		entities.VelocityChanged(c, value.EntityID, value.VelocityX, value.VelocityY, value.VelocityZ)

	case *gen.PlayClientboundNamedEntitySpawn:
		entities.Spawned(c, value.EntityID, value.PlayerUUID.String(), "minecraft:player",
			block47(value.X), block47(value.Y), block47(value.Z),
			angle47(value.Yaw), angle47(value.Pitch))
		entities.MetadataChanged(c, value.EntityID, metadata47(value.Metadata))

	case *gen.PlayClientboundEntityDestroy:
		for _, id := range value.EntityIds {
			entities.Removed(c, id)
		}

	// Four packets, one EntityMoved. This is the taxonomy's motivating case.
	case *gen.PlayClientboundRelEntityMove:
		entities.MovedBy(c, value.EntityID,
			delta47(value.DX), delta47(value.DY), delta47(value.DZ), value.OnGround)

	case *gen.PlayClientboundEntityMoveLook:
		entities.MovedBy(c, value.EntityID,
			delta47(value.DX), delta47(value.DY), delta47(value.DZ), value.OnGround)
		entities.Looked(c, value.EntityID, angle47(value.Yaw), angle47(value.Pitch), value.OnGround)

	case *gen.PlayClientboundEntityLook:
		entities.Looked(c, value.EntityID, angle47(value.Yaw), angle47(value.Pitch), value.OnGround)

	case *gen.PlayClientboundEntityTeleport:
		entities.Moved(c, value.EntityID,
			block47(value.X), block47(value.Y), block47(value.Z),
			angle47(value.Yaw), angle47(value.Pitch), value.OnGround)

	case *gen.PlayClientboundEntityHeadRotation:
		entities.HeadLooked(c, value.EntityID, angle47(value.HeadYaw))

	case *gen.PlayClientboundEntityMetadata:
		entities.MetadataChanged(c, value.EntityID, metadata47(value.Metadata))

	case *gen.PlayClientboundEntityEquipment:
		entities.EquipmentChanged(c, value.EntityID, map[int32]any{int32(value.Slot): value.Item})

	case *gen.PlayClientboundUpdateAttributes:
		attributes := make([]world.Attribute, 0, len(value.Properties))
		for _, property := range value.Properties {
			attributes = append(attributes, world.Attribute{Key: property.Key, Value: property.Value})
		}
		entities.AttributesChanged(c, value.EntityID, attributes)

	case *gen.PlayClientboundEntityVelocity:
		entities.VelocityChanged(c, value.EntityID, value.VelocityX, value.VelocityY, value.VelocityZ)

	case *gen.PlayClientboundAttachEntity:
		entities.Attached(c, value.EntityID, value.VehicleID)

	case *gen.PlayClientboundEntityStatus:
		// Protocol 47 has no damage packet: hurt and death are two of many
		// entity statuses, and neither carries a source.
		if isLocal(ctx, value.EntityID) {
			return
		}
		switch value.EntityStatus {
		case statusHurt:
			entities.Damaged(c, value.EntityID, event.Damage{})
		case statusDeath:
			entities.Died(c, value.EntityID, 0, false)
		}

	case *gen.PlayClientboundCombatEvent:
		// A death this protocol does attribute — for another player, since the
		// local player's goes to the player domain.
		if value.Event != combatEventEntityDead || isLocal(ctx, value.PlayerID.Case2) {
			return
		}
		killer, attributed := combatKiller(value.EntityID.Case2)
		entities.Died(c, value.PlayerID.Case2, killer, attributed)

	case *gen.PlayClientboundAnimation:
		entities.Animated(c, value.EntityID, value.Animation)

	case *gen.PlayClientboundCollect:
		entities.ItemCollected(c, value.CollectedEntityID, value.CollectorEntityID, 0)

	case *gen.PlayClientboundEntityEffect:
		if !isLocal(ctx, value.EntityID) {
			entities.EffectApplied(c, value.EntityID,
				int32(value.EffectID), int32(value.Amplifier), value.Duration)
		}

	case *gen.PlayClientboundRemoveEntityEffect:
		if !isLocal(ctx, value.EntityID) {
			entities.EffectRemoved(c, value.EntityID, int32(value.EffectID))
		}
	}
}

// The entity statuses this reducer reads, and the combat event that reports a
// death.
//
// Protocol 47 packs dozens of unrelated effects into one numbered status byte
// and the schema names none of them, so the two numbers this client cares about
// are written here rather than looked up: 2 is a living entity being hurt and 3
// is one dying. The 775 half of this adapter names 3 for itself, for the same
// reason and with the same number.
const (
	statusHurt  int8 = 2
	statusDeath int8 = 3

	combatEventEntityDead int32 = 2
)

// combatKiller reads the killer out of a protocol 47 combat event. The server
// sends -1 when nothing killed the player, and the absent case reports a zero
// ID rather than the sentinel, so no caller reads -1 as an entity. Protocol
// 775's damage event uses a different sentinel for the same idea and is undone
// in the same place, in its own adapter.
func combatKiller(entityID int32) (int32, bool) {
	if entityID < 0 {
		return 0, false
	}

	return entityID, true
}

func block47(fixed int32) float64 { return float64(fixed) / fixedPoint47 }

func delta47(d int8) float64 { return float64(d) / deltaScale47 }

func angle47(a int8) float32 { return float32(a) * angleScale47 }

// metadata47 converts protocol 47's packed metadata into the world's
// index-addressed form, keeping every index including ones this client has no
// name for.
func metadata47(items gen.EntityMetadata) []world.Metadata {
	entries := make([]world.Metadata, 0, len(items))
	for _, item := range items {
		entries = append(entries, world.Metadata{
			Index: item.AnonymousBitField1.Key,
			Type:  metadataType47(item.AnonymousBitField1.Type),
			Value: item.Value,
		})
	}

	return entries
}

// metadataType47 names protocol 47's numeric metadata types. An unnamed one
// keeps its number, because dropping it would lose what the server said.
func metadataType47(kind uint8) string {
	switch kind {
	case 0:
		return "byte"
	case 1:
		return "short"
	case 2:
		return "int"
	case 3:
		return "float"
	case 4:
		return "string"
	case 5:
		return "slot"
	case 6:
		return "position"
	case 7:
		return "rotation"
	default:
		return "unknown"
	}
}

// objectType and mobType name protocol 47's numeric entity types.
//
// Protocol 47 identifies an entity by a number whose meaning depends on which
// spawn packet carried it, and the number space is not the registry 775 uses.
// The world keeps the server's own identifier as a string, so the number is
// rendered rather than mapped onto a vanilla name the server may not mean.
func objectType(kind int8) string { return "java/1.8.9:object/" + itoa(int(kind)) }

func mobType(kind uint8) string { return "java/1.8.9:mob/" + itoa(int(kind)) }

// itoa keeps the type names above readable.
func itoa(n int) string { return strconv.Itoa(n) }

// Protocol 47 sends a chunk column as a bitmask and one packed blob: for each
// set bit, 4096 blocks of two little-endian bytes holding the block ID in the
// high twelve bits and the metadata in the low four, then the light arrays,
// then the biomes. Only the block half is decoded; the rest is kept as bytes.
const (
	sectionBlockBytes47 = blocksPerSection47 * 2
	blocksPerSection47  = 4096
)

// chunkReducer decodes the packets that describe terrain.
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
		sections, light := splitColumn47(value.BitMap, value.ChunkData)
		if len(sections) == 0 && !value.GroundUp {
			return
		}
		chunks.Loaded(c, world.ChunkPos{X: value.X, Z: value.Z}, sections, light)

	case *gen.PlayClientboundBlockChange:
		chunks.BlocksChanged(c, []world.BlockChange{{
			Pos:   blockPos47(value.Location),
			State: uint32(value.Type),
		}})

	case *gen.PlayClientboundMultiBlockChange:
		// The same fact as a single change, so the same event: the world
		// takes a set and a single change is a set of one.
		changes := make([]world.BlockChange, 0, len(value.Records))
		for _, record := range value.Records {
			changes = append(changes, world.BlockChange{
				Pos: world.BlockPos{
					X: value.ChunkX*16 + int32(record.HorizontalPos>>4),
					Y: int32(record.Y),
					Z: value.ChunkZ*16 + int32(record.HorizontalPos&15),
				},
				State: uint32(record.BlockID),
			})
		}
		chunks.BlocksChanged(c, changes)

	case *gen.PlayClientboundTileEntityData:
		chunks.BlockEntityChanged(c, blockPos47(value.Location), value.NBTData)
	}
}

func blockPos47(p gen.Position) world.BlockPos {
	return world.BlockPos{X: p.X, Y: int32(p.Y), Z: p.Z}
}

// splitColumn47 slices the packed blob into one immutable byte range per
// present section, and keeps whatever follows as light and biome data.
//
// The blocks come first for every section, so the block half can be sliced
// without knowing whether the dimension carries skylight — which the packet
// does not say and this reducer does not track.
func splitColumn47(bitmap uint16, data []byte) ([]world.SectionData, []byte) {
	var sections []world.SectionData
	offset := 0
	for y := range 16 {
		if bitmap&(1<<uint(y)) == 0 {
			continue
		}
		if offset+sectionBlockBytes47 > len(data) {
			// A truncated column is the server's business, not a reason to
			// end the session. What arrived is kept.
			break
		}
		sections = append(sections, world.SectionData{
			Y:      y,
			Raw:    data[offset : offset+sectionBlockBytes47],
			Decode: decodeSection47,
		})
		offset += sectionBlockBytes47
	}

	return sections, data[min(offset, len(data)):]
}

// decodeSection47 turns one section's bytes into block states. It is pure, as
// the world requires: two readers racing to decode the same bytes compute the
// same answer.
func decodeSection47(raw []byte) ([]uint32, error) {
	if len(raw) < sectionBlockBytes47 {
		return nil, world.ErrSectionNotDecodable
	}

	states := make([]uint32, blocksPerSection47)
	for i := range states {
		// Little-endian: the low byte first, and the value is the block ID
		// shifted four bits with the metadata in the low nibble, which is
		// exactly the state identifier this protocol uses.
		states[i] = uint32(raw[i*2]) | uint32(raw[i*2+1])<<8
	}

	return states, nil
}

// Protocol 47's world border packs six actions into one packet, discriminated
// by an action number, where 775 sends six packets. Both reduce to the same
// six mutators on the environment store.
const (
	borderSetSize       int32 = 0
	borderLerpSize      int32 = 1
	borderSetCenter     int32 = 2
	borderInitialize    int32 = 3
	borderWarningTime   int32 = 4
	borderWarningBlocks int32 = 5
)

// reasonRainLevel is protocol 47's fade value, which changes rain intensity
// without starting or stopping it. There is no thunder reason on 47.
const reasonRainLevel uint8 = 7

// environmentReducer decodes the packets that describe the world's scalars.
func environmentReducer(environment *world.Environment) world.Func {
	return func(_ *world.Context, batch version.Batch, c *event.Collector) error {
		for _, packet := range batch.Packets {
			reduceEnvironmentPacket(environment, packet, c)
		}

		return nil
	}
}

func reduceEnvironmentPacket(
	environment *world.Environment,
	packet protocol.Packet,
	c *event.Collector,
) {
	switch value := packet.Value.(type) {
	case *gen.PlayClientboundUpdateTime:
		// 47 sends the time of day as a number. 775 replaced it with a set of
		// clocks, which is why the world takes both and this passes nil.
		environment.TimeChanged(c, value.Age, value.Time, true, nil)

	case *gen.PlayClientboundWorldBorder:
		reduceBorder47(environment, value, c)

	case *gen.PlayClientboundDifficulty:
		// 47 numbers the difficulties and 775 names them. The world stores the
		// name, so the number is resolved here.
		environment.DifficultyChanged(c, difficultyName(int32(value.Difficulty)), false)

	case *gen.PlayClientboundExplosion:
		environment.Explosion(c, event.WorldExplosionOccurred{
			X: float64(value.X), Y: float64(value.Y), Z: float64(value.Z),
			Radius:     value.Radius,
			KnockbackX: float64(value.PlayerMotionX),
			KnockbackY: float64(value.PlayerMotionY),
			KnockbackZ: float64(value.PlayerMotionZ),
			Knocked:    true,
		})

	case *gen.PlayClientboundWorldEvent:
		environment.WorldEvent(c, value.EffectID, blockPos47(value.Location), value.Data, value.Global)

	case *gen.PlayClientboundGameStateChange:
		// The other half of the packet the player reducer reads. Each ignores
		// the reasons that are not its own.
		switch value.Reason {
		case reasonRainStart:
			environment.WeatherChanged(c, true)
		case reasonRainEnd:
			environment.WeatherChanged(c, false)
		case reasonRainLevel:
			environment.RainLevelChanged(c, value.GameMode)
		}
	}
}

// reduceBorder47 turns one action-discriminated packet into one of the world's
// border mutators. The switch fields are named per case, so reading the wrong
// case's field would silently give zero — which is why each case reads only
// the fields its action carries.
func reduceBorder47(
	environment *world.Environment,
	value *gen.PlayClientboundWorldBorder,
	c *event.Collector,
) {
	switch value.Action {
	case borderSetSize:
		environment.BorderSize(c, value.Radius.Case0)

	case borderLerpSize:
		environment.BorderLerp(c, value.OldRadius.Case1, value.NewRadius.Case1, value.Speed.Case1)

	case borderSetCenter:
		environment.BorderCenter(c, value.X.Case2, value.Z.Case2)

	case borderInitialize:
		environment.BorderInitialized(c, event.Border{
			X: value.X.Case3, Z: value.Z.Case3,
			OldDiameter: value.OldRadius.Case3, NewDiameter: value.NewRadius.Case3,
			Speed:          value.Speed.Case3,
			PortalBoundary: value.PortalBoundary.Case3,
			WarningTime:    value.WarningTime.Case3,
			WarningBlocks:  value.WarningBlocks.Case3,
		})

	case borderWarningTime:
		environment.BorderWarningTime(c, value.WarningTime.Case4)

	case borderWarningBlocks:
		environment.BorderWarningBlocks(c, value.WarningBlocks.Case5)
	}
}

// difficultyName resolves protocol 47's numbered difficulty to the name 775
// sends, which is what the world stores. An unnamed number keeps its digits
// rather than being reported as one of the four.
func difficultyName(difficulty int32) string {
	switch difficulty {
	case 0:
		return "peaceful"
	case 1:
		return "easy"
	case 2:
		return "normal"
	case 3:
		return "hard"
	default:
		return "unknown/" + itoa(int(difficulty))
	}
}

// cursorContainer and cursorSlot are how protocol 47 addresses the cursor: a
// set-slot for container -1, slot -1. It is not a slot in any menu, and
// missing the special case silently corrupts an inventory model.
const (
	cursorContainer int8  = -1
	cursorSlot      int16 = -1
)

// containerReducer decodes the packets that describe open menus.
//
// Protocol 47 has no recipe book, no trade packet, and no state ID, so three
// of this domain's seven events never fire here and the snapshot says the
// recipe set was never supplied rather than presenting it as empty.
func containerReducer(containers *world.Containers) world.Func {
	return func(_ *world.Context, batch version.Batch, c *event.Collector) error {
		for _, packet := range batch.Packets {
			reduceContainerPacket(containers, packet, c)
		}

		return nil
	}
}

func reduceContainerPacket(
	containers *world.Containers,
	packet protocol.Packet,
	c *event.Collector,
) {
	switch value := packet.Value.(type) {
	case *gen.PlayClientboundOpenWindow:
		opened := event.ContainerOpened{
			ContainerID: int32(value.WindowID),
			// 47 names the menu type on the wire; 775 numbers it. Both are
			// kept as the server sent them.
			MenuType:  value.InventoryType,
			Title:     value.WindowTitle,
			SlotCount: int32(value.SlotCount),
		}
		// A horse's menu, and only a horse's, carries the entity it belongs to.
		if value.InventoryType == horseMenuType {
			opened.EntityID, opened.EntityKnown = value.EntityID.EntityHorse, true
		}
		containers.Opened(c, opened)

	case *gen.PlayClientboundCloseWindow:
		containers.Closed(c, int32(value.WindowID))

	case *gen.PlayClientboundWindowItems:
		items := make(map[int32]any, len(value.Items))
		for slot, item := range value.Items {
			items[int32(slot)] = item
		}
		containers.SlotsChanged(c, int32(value.WindowID), items, 0, false)

	case *gen.PlayClientboundSetSlot:
		// Container -1, slot -1 is the cursor, not a slot.
		if value.WindowID == cursorContainer && value.Slot == cursorSlot {
			containers.CursorChanged(c, value.Item, value.Item.BlockID >= 0)

			return
		}
		containers.SlotsChanged(c,
			int32(value.WindowID), map[int32]any{int32(value.Slot): value.Item}, 0, false)

	case *gen.PlayClientboundCraftProgressBar:
		containers.PropertyChanged(c,
			int32(value.WindowID), int32(value.Property), int32(value.Value))
	}
}

// horseMenuType is the one protocol 47 menu whose open packet carries an
// entity. The schema makes the entity field conditional on this exact string.
const horseMenuType = "EntityHorse"

// Protocol 47's player-list actions. The action is a single choice here, where
// 775 sends a bitfield and can do several at once.
const (
	playerAdd         = "add_player"
	playerGameMode    = "update_game_mode"
	playerLatency     = "update_latency"
	playerDisplayName = "update_display_name"
	playerRemove      = "remove_player"
)

// registryReducer decodes the packets that describe the server's own
// vocabulary.
//
// **Protocol 47 has no registry data, no tags, and no command tree.** Its
// registries are entirely static, which is why the only packet this reducer
// reads is the player list, and why the snapshot reports the session registry
// as never supplied rather than as empty.
func registryReducer(registries *world.Registries) world.Func {
	return func(_ *world.Context, batch version.Batch, c *event.Collector) error {
		for _, packet := range batch.Packets {
			value, ok := packet.Value.(*gen.PlayClientboundPlayerInfo)
			if !ok {
				continue
			}
			reducePlayerList47(registries, value, c)
		}

		return nil
	}
}

func reducePlayerList47(
	registries *world.Registries,
	value *gen.PlayClientboundPlayerInfo,
	c *event.Collector,
) {
	var (
		changes []world.PlayerListChange
		removed []string
	)
	for _, item := range value.Data {
		uuid := item.UUID.String()
		switch value.Action {
		case playerAdd:
			add := item.AnonymousSwitch1.AddPlayer
			changes = append(changes, world.PlayerListChange{
				UUID: uuid,
				Name: add.Name, SetName: true,
				GameMode: add.Gamemode, SetGameMode: true,
				Latency: add.Ping, SetLatency: true,
				// 47 has no separate "listed" flag: a player on the list is
				// listed, which is what the add packet means.
				Listed: true, SetListed: true,
			})

		case playerGameMode:
			changes = append(changes, world.PlayerListChange{
				UUID:     uuid,
				GameMode: item.AnonymousSwitch1.UpdateGameMode.Gamemode, SetGameMode: true,
			})

		case playerLatency:
			changes = append(changes, world.PlayerListChange{
				UUID:    uuid,
				Latency: item.AnonymousSwitch1.UpdateLatency.Ping, SetLatency: true,
			})

		case playerDisplayName:
			// The display name is a chat component the library does not render,
			// so this changes nothing the snapshot models. The player is still
			// touched, so a subscriber sees that the server said something.
			changes = append(changes, world.PlayerListChange{UUID: uuid})

		case playerRemove:
			removed = append(removed, uuid)
		}
	}

	registries.PlayerListChanged(c, changes, removed)
}
