package event

// The entity domain covers every entity except the local player, including
// other players. Which entity is local is decided by the play login, and the
// player domain owns that one.

// EntitySpawned reports an entity entering the client's view.
type EntitySpawned struct {
	Stamp

	EntityID int32
	// UUID is empty on protocol 47 for entities whose spawn packet carries
	// none. Type is the server's own type identifier, kept as sent: a modded
	// server names types this client has never heard of.
	UUID string
	Type string

	X, Y, Z    float64
	Yaw, Pitch float32
}

func (EntitySpawned) Name() Name     { return NameEntitySpawned }
func (EntitySpawned) Domain() Domain { return DomainEntities }

// EntityRemoved reports an entity leaving the client's view. Everything the
// client knew about it is released with it.
type EntityRemoved struct {
	Stamp

	EntityID int32
}

func (EntityRemoved) Name() Name     { return NameEntityRemoved }
func (EntityRemoved) Domain() Domain { return DomainEntities }

// EntityMoved reports an entity's position or rotation changing.
//
// It is the taxonomy's motivating case: four packets on protocol 47 and five
// on 775 move an entity, and a subscriber written against this event keeps
// working when a version changes which one carries the fact. The coordinates
// are absolute, with relative moves already resolved.
type EntityMoved struct {
	Stamp

	EntityID   int32
	X, Y, Z    float64
	Yaw, Pitch float32
	HeadYaw    float32
	OnGround   bool
	// Relative reports that the server sent an offset rather than an
	// absolute position.
	Relative bool
}

func (EntityMoved) Name() Name     { return NameEntityMoved }
func (EntityMoved) Domain() Domain { return DomainEntities }

// EntityMetadataChanged reports metadata indices that changed. Metadata is
// merged by index, so this names what moved rather than carrying the whole
// set; the snapshot holds the current values, including indices this client
// has no name for.
type EntityMetadataChanged struct {
	Stamp

	EntityID int32
	Indices  []uint8
}

func (EntityMetadataChanged) Name() Name     { return NameEntityMetadataChanged }
func (EntityMetadataChanged) Domain() Domain { return DomainEntities }

// EntityEquipmentChanged reports an equipment slot changing.
type EntityEquipmentChanged struct {
	Stamp

	EntityID int32
	Slots    []int32
}

func (EntityEquipmentChanged) Name() Name     { return NameEntityEquipmentChanged }
func (EntityEquipmentChanged) Domain() Domain { return DomainEntities }

// EntityAttributesChanged reports attribute keys the server updated. Keys are
// namespaced strings kept as sent, because a modded server defines its own.
type EntityAttributesChanged struct {
	Stamp

	EntityID int32
	Keys     []string
}

func (EntityAttributesChanged) Name() Name     { return NameEntityAttributesChanged }
func (EntityAttributesChanged) Domain() Domain { return DomainEntities }

// EntityEffectsChanged reports one status effect on an entity starting,
// changing, or ending.
type EntityEffectsChanged struct {
	Stamp

	EntityID  int32
	EffectID  int32
	Amplifier int32
	Duration  int32
	Removed   bool
}

func (EntityEffectsChanged) Name() Name     { return NameEntityEffectsChanged }
func (EntityEffectsChanged) Domain() Domain { return DomainEntities }

// EntityVelocityChanged reports the velocity the server assigned an entity,
// in the protocol's own units.
type EntityVelocityChanged struct {
	Stamp

	EntityID int32
	X, Y, Z  int16
}

func (EntityVelocityChanged) Name() Name     { return NameEntityVelocityChanged }
func (EntityVelocityChanged) Domain() Domain { return DomainEntities }

// EntityPassengersChanged reports who is riding what. An empty Passengers
// list means everybody dismounted.
type EntityPassengersChanged struct {
	Stamp

	EntityID   int32
	Passengers []int32
}

func (EntityPassengersChanged) Name() Name     { return NameEntityPassengersChanged }
func (EntityPassengersChanged) Domain() Domain { return DomainEntities }

// EntityDamaged reports an entity taking damage. Damage names who is
// responsible, on the protocols that say.
type EntityDamaged struct {
	Stamp

	EntityID int32
	Damage   Damage
}

func (EntityDamaged) Name() Name     { return NameEntityDamaged }
func (EntityDamaged) Domain() Domain { return DomainEntities }

// EntityDied reports an entity dying, which is an observation and not the
// inference a caller would otherwise draw from health reaching zero. It fires
// once: a server announces a death through more than one packet and the world
// publishes the first.
//
// The entity is not removed here. A server keeps a corpse tracked for the death
// animation and destroys it a second later, and the snapshot reports it as dead
// until it does — which is the window a caller has to notice that the thing it
// was fighting is gone.
//
// KillerID names the entity credited with the kill and Attributed reports
// whether the protocol named one. This is the mirror of EntityDamaged:
// protocol 47's combat event carries a killer and protocol 775's death event
// dropped the field, so the version that attributes damage is not the version
// that attributes death.
type EntityDied struct {
	Stamp

	EntityID   int32
	KillerID   int32
	Attributed bool
}

func (EntityDied) Name() Name     { return NameEntityDied }
func (EntityDied) Domain() Domain { return DomainEntities }

// EntityAnimated reports an animation the server played on an entity, in the
// protocol's own numbering.
type EntityAnimated struct {
	Stamp

	EntityID  int32
	Animation uint8
}

func (EntityAnimated) Name() Name     { return NameEntityAnimated }
func (EntityAnimated) Domain() Domain { return DomainEntities }

// EntityItemCollected reports one entity picking another up. Count is zero on
// protocol 47, which does not send it.
type EntityItemCollected struct {
	Stamp

	CollectedID int32
	CollectorID int32
	Count       int32
}

func (EntityItemCollected) Name() Name     { return NameEntityItemCollected }
func (EntityItemCollected) Domain() Domain { return DomainEntities }
