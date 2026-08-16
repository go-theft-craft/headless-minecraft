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
		// Protocol 47 has no damage packet: hurt is one of many entity
		// statuses, and the source is not sent.
		if value.EntityStatus == statusHurt {
			entities.Damaged(c, value.EntityID, 0)
		}

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

// statusHurt is the entity status protocol 47 uses for damage.
const statusHurt int8 = 2

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
