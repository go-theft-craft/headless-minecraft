package world

import (
	"maps"
	"slices"

	"github.com/go-theft-craft/headless-minecraft/event"
)

// maxMetadataKeys bounds one entity's metadata.
//
// Every store in this package has a bound, because every store is filled by
// the peer. A vanilla entity uses fewer than twenty indices; a modded one uses
// more, and 64 is generous without letting one entity grow without limit.
const maxMetadataKeys = 64

// Metadata is one entry of an entity's metadata, kept as the server sent it.
//
// The two protocols disagree about almost everything here: protocol 47 packs
// the index and a numeric type into one byte and terminates at 0x7F, and 775
// sends the index, a type name, and terminates at 0xFF. What they share is
// that a value is addressed by index, so that is what the world stores, and
// the decoded value is kept as-is rather than mapped onto a model that would
// have to be extended for every modded entity.
type Metadata struct {
	Index uint8
	// Type is the server's own name or number for the value's shape, as a
	// string. It is what a caller needs to interpret Value, and it is not
	// interpreted here.
	Type  string
	Value any
}

// Attribute is one entity attribute. The key is namespaced and kept as sent.
type Attribute struct {
	Key   string
	Value float64
}

// entity is one tracked entity's state.
type entity struct {
	id         int32
	uuid       string
	kind       string
	x, y, z    float64
	yaw, pitch float32
	headYaw    float32
	onGround   bool
	velocity   [3]int16

	metadata   map[uint8]Metadata
	equipment  map[int32]any
	attributes map[string]Attribute
	effects    map[int32]Effect
	passengers []int32
	vehicle    int32

	// droppedMetadata counts metadata entries refused by the bound. A
	// subscriber that sees an entity behaving oddly can tell "the model does
	// not know that index" from "the client threw the value away".
	droppedMetadata int
}

// EntityView is one entity in a snapshot. Its maps and slices are owned
// copies.
type EntityView struct {
	EntityID   int32
	UUID       string
	Type       string
	X, Y, Z    float64
	Yaw, Pitch float32
	HeadYaw    float32
	OnGround   bool
	Velocity   [3]int16

	Metadata   map[uint8]Metadata
	Equipment  map[int32]any
	Attributes map[string]Attribute
	Effects    map[int32]Effect
	Passengers []int32
	Vehicle    int32

	DroppedMetadata int
}

// Entities is every entity the client is tracking, except the local player.
type Entities struct {
	tracked map[int32]*entity
}

// EntitiesView is the entity half of a snapshot.
type EntitiesView struct {
	Tracked map[int32]EntityView
}

// Get returns one entity, or false when it is not tracked.
func (v EntitiesView) Get(id int32) (EntityView, bool) {
	e, ok := v.Tracked[id]

	return e, ok
}

func newEntities() *Entities { return &Entities{tracked: make(map[int32]*entity)} }

func (s *Entities) view() EntitiesView {
	tracked := make(map[int32]EntityView, len(s.tracked))
	for id, e := range s.tracked {
		tracked[id] = EntityView{
			EntityID: e.id, UUID: e.uuid, Type: e.kind,
			X: e.x, Y: e.y, Z: e.z, Yaw: e.yaw, Pitch: e.pitch,
			HeadYaw: e.headYaw, OnGround: e.onGround, Velocity: e.velocity,
			Metadata:   maps.Clone(e.metadata),
			Equipment:  maps.Clone(e.equipment),
			Attributes: maps.Clone(e.attributes),
			Effects:    maps.Clone(e.effects),
			Passengers: slices.Clone(e.passengers),
			Vehicle:    e.vehicle,

			DroppedMetadata: e.droppedMetadata,
		}
	}

	return EntitiesView{Tracked: tracked}
}

// track returns the entity's state, creating it if the client never saw it
// spawn.
//
// Packets arrive for entities this client has no spawn for — after a chunk
// unload, or when the server tracks something it never announced — and that is
// normal traffic, not a broken invariant. The entity is created empty rather
// than the packet being dropped, so what the server said is not lost.
func (s *Entities) track(id int32) *entity {
	e, ok := s.tracked[id]
	if !ok {
		e = &entity{
			id:         id,
			metadata:   make(map[uint8]Metadata),
			equipment:  make(map[int32]any),
			attributes: make(map[string]Attribute),
			effects:    make(map[int32]Effect),
		}
		s.tracked[id] = e
	}

	return e
}

// Spawned records an entity entering view.
func (s *Entities) Spawned(
	c *event.Collector,
	id int32,
	uuid, kind string,
	x, y, z float64,
	yaw, pitch float32,
) {
	e := s.track(id)
	e.uuid, e.kind = uuid, kind
	e.x, e.y, e.z = x, y, z
	e.yaw, e.pitch = yaw, pitch

	event.Emit(c, event.EntitySpawned{
		EntityID: id, UUID: uuid, Type: kind,
		X: x, Y: y, Z: z, Yaw: yaw, Pitch: pitch,
	})
}

// Removed releases everything the client knew about an entity. Removing one it
// never saw is not an error: the server may have destroyed something this
// client never tracked.
func (s *Entities) Removed(c *event.Collector, id int32) {
	if _, ok := s.tracked[id]; !ok {
		return
	}
	delete(s.tracked, id)

	event.Emit(c, event.EntityRemoved{EntityID: id})
}

// Moved records an absolute position.
func (s *Entities) Moved(
	c *event.Collector,
	id int32,
	x, y, z float64,
	yaw, pitch float32,
	onGround bool,
) {
	e := s.track(id)
	e.x, e.y, e.z = x, y, z
	e.yaw, e.pitch = yaw, pitch
	e.onGround = onGround

	s.emitMove(c, e, false)
}

// MovedBy records a relative move, resolved against the position the entity
// already had. Every protocol sends deltas in its own fixed-point units, so
// the caller converts before it gets here.
func (s *Entities) MovedBy(
	c *event.Collector,
	id int32,
	dx, dy, dz float64,
	onGround bool,
) {
	e := s.track(id)
	e.x, e.y, e.z = e.x+dx, e.y+dy, e.z+dz
	e.onGround = onGround

	s.emitMove(c, e, true)
}

// Looked records a rotation with no position change.
func (s *Entities) Looked(c *event.Collector, id int32, yaw, pitch float32, onGround bool) {
	e := s.track(id)
	e.yaw, e.pitch = yaw, pitch
	e.onGround = onGround

	s.emitMove(c, e, false)
}

// HeadLooked records a head rotation, which is separate from body yaw.
func (s *Entities) HeadLooked(c *event.Collector, id int32, headYaw float32) {
	e := s.track(id)
	e.headYaw = headYaw

	s.emitMove(c, e, false)
}

func (s *Entities) emitMove(c *event.Collector, e *entity, relative bool) {
	event.Emit(c, event.EntityMoved{
		EntityID: e.id, X: e.x, Y: e.y, Z: e.z,
		Yaw: e.yaw, Pitch: e.pitch, HeadYaw: e.headYaw,
		OnGround: e.onGround, Relative: relative,
	})
}

// MetadataChanged merges metadata by index.
//
// A packet carrying one index must not clear the others, and an index this
// client has no name for is kept rather than dropped: on a modded server the
// unknown index is the whole point.
func (s *Entities) MetadataChanged(c *event.Collector, id int32, entries []Metadata) {
	if len(entries) == 0 {
		return
	}

	e := s.track(id)
	indices := make([]uint8, 0, len(entries))
	for _, entry := range entries {
		if _, existing := e.metadata[entry.Index]; !existing && len(e.metadata) >= maxMetadataKeys {
			e.droppedMetadata++

			continue
		}
		e.metadata[entry.Index] = entry
		indices = append(indices, entry.Index)
	}
	if len(indices) == 0 {
		return
	}

	event.Emit(c, event.EntityMetadataChanged{EntityID: id, Indices: indices})
}

// EquipmentChanged records equipment slots. The item value is the protocol's
// own decoded slot, kept as sent.
func (s *Entities) EquipmentChanged(c *event.Collector, id int32, items map[int32]any) {
	if len(items) == 0 {
		return
	}

	e := s.track(id)
	slots := make([]int32, 0, len(items))
	for slot, item := range items {
		e.equipment[slot] = item
		slots = append(slots, slot)
	}
	slices.Sort(slots)

	event.Emit(c, event.EntityEquipmentChanged{EntityID: id, Slots: slots})
}

// AttributesChanged merges attributes by key.
func (s *Entities) AttributesChanged(c *event.Collector, id int32, attributes []Attribute) {
	if len(attributes) == 0 {
		return
	}

	e := s.track(id)
	keys := make([]string, 0, len(attributes))
	for _, attribute := range attributes {
		e.attributes[attribute.Key] = attribute
		keys = append(keys, attribute.Key)
	}

	event.Emit(c, event.EntityAttributesChanged{EntityID: id, Keys: keys})
}

// EffectApplied records a status effect on an entity.
func (s *Entities) EffectApplied(c *event.Collector, id, effectID, amplifier, duration int32) {
	e := s.track(id)
	e.effects[effectID] = Effect{ID: effectID, Amplifier: amplifier, Duration: duration}

	event.Emit(c, event.EntityEffectsChanged{
		EntityID: id, EffectID: effectID, Amplifier: amplifier, Duration: duration,
	})
}

// EffectRemoved records a status effect ending.
func (s *Entities) EffectRemoved(c *event.Collector, id, effectID int32) {
	delete(s.track(id).effects, effectID)

	event.Emit(c, event.EntityEffectsChanged{EntityID: id, EffectID: effectID, Removed: true})
}

// VelocityChanged records the velocity the server assigned.
func (s *Entities) VelocityChanged(c *event.Collector, id int32, x, y, z int16) {
	e := s.track(id)
	e.velocity = [3]int16{x, y, z}

	event.Emit(c, event.EntityVelocityChanged{EntityID: id, X: x, Y: y, Z: z})
}

// PassengersChanged records who is riding an entity, and updates each
// passenger's vehicle.
func (s *Entities) PassengersChanged(c *event.Collector, id int32, passengers []int32) {
	e := s.track(id)
	for _, previous := range e.passengers {
		if rider, ok := s.tracked[previous]; ok {
			rider.vehicle = 0
		}
	}
	e.passengers = slices.Clone(passengers)
	for _, passenger := range passengers {
		s.track(passenger).vehicle = id
	}

	event.Emit(c, event.EntityPassengersChanged{
		EntityID: id, Passengers: slices.Clone(passengers),
	})
}

// Attached records protocol 47's leash-or-ride packet, which says an entity is
// attached to a vehicle without listing every passenger.
func (s *Entities) Attached(c *event.Collector, id, vehicle int32) {
	e := s.track(id)
	e.vehicle = vehicle

	passengers := []int32{id}
	if vehicle == -1 {
		e.vehicle = 0
		passengers = nil
	}

	event.Emit(c, event.EntityPassengersChanged{EntityID: vehicle, Passengers: passengers})
}

// Damaged records an entity taking damage.
func (s *Entities) Damaged(c *event.Collector, id, sourceTypeID int32) {
	s.track(id)

	event.Emit(c, event.EntityDamaged{EntityID: id, SourceTypeID: sourceTypeID})
}

// Animated records an animation the server played.
func (s *Entities) Animated(c *event.Collector, id int32, animation uint8) {
	s.track(id)

	event.Emit(c, event.EntityAnimated{EntityID: id, Animation: animation})
}

// ItemCollected records one entity picking another up. The collected entity is
// released: the server destroys it, and waiting for a destroy packet that may
// never come would leak.
func (s *Entities) ItemCollected(c *event.Collector, collected, collector, count int32) {
	delete(s.tracked, collected)

	event.Emit(c, event.EntityItemCollected{
		CollectedID: collected, CollectorID: collector, Count: count,
	})
}
