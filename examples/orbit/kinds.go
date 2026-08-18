package main

import (
	"fmt"
	"strconv"

	"github.com/go-theft-craft/minecraft-protocol/data"
	gen1_8 "github.com/go-theft-craft/minecraft-protocol/generated/java/v1_8"
	gen26_1 "github.com/go-theft-craft/minecraft-protocol/generated/java/v26_1"
)

// Kinds names the entities a server spawns.
//
// The world keeps an entity's type as the server sent it and interprets none of
// it, which is the right call for a library and leaves the bot holding a string
// like "java/26.1:entity/150". This turns that into what the block registry
// already gives for blocks: a name to log and a classification to decide on.
//
// The index is built once and keyed by the wire string itself, so a lookup is
// one map read and no parsing. Building it the other way -- parse the number,
// then ask the registry -- runs into the registry being keyed on a type as well
// as an identifier, and the type is the thing being looked up.
type Kinds struct {
	byWire map[string]data.Entity
}

// Kind is what this client can say about an entity it has seen.
type Kind struct {
	// Name is the entity's display name, for a log line a person reads.
	Name string
	// Pursues reports whether this is the sort of thing that follows a player
	// who walks away from it.
	Pursues bool
}

// Lookup names an entity from the type string the world carries.
//
// An unrecognised type is not an error and not a blank: it is an entity this
// client has no data for, which a modded server produces routinely, and the
// caller is told so rather than handed a zero value it might read as "harmless".
func (k Kinds) Lookup(wire string) (Kind, bool) {
	entity, known := k.byWire[wire]
	if !known {
		return Kind{}, false
	}

	return Kind{Name: entity.DisplayName, Pursues: pursues(entity)}, true
}

// pursues decides whether something is worth running from.
//
// On the category rather than on the type, because the category is the field
// both versions fill in with the same words. Protocol 47 publishes a type that
// only says which of its two identifier namespaces an entity came from -- mob
// or object -- and every hostile mob and every farm animal in that version is
// equally a "mob".
//
// Everything that is not known to stay put pursues. A projectile is already
// past by the time its damage is read and its shooter is what the damage names,
// an immobile entity cannot follow anything, and a vehicle is furniture; the
// rest, including a category nobody has seen before, gets the cautious answer.
func pursues(entity data.Entity) bool {
	switch entity.Category {
	case "Passive mobs", "Immobile", "Projectiles", "Vehicles", "Blocks", "Drops":
		return false
	default:
		return true
	}
}

// NewKinds builds the index for a version.
func NewKinds(legacy bool) (Kinds, error) {
	if legacy {
		set, err := gen1_8.Data()
		if err != nil {
			return Kinds{}, fmt.Errorf("load the 1.8.9 data set: %w", err)
		}

		// Two namespaces, and the wire says which one it means. The adapter
		// spells them into the type string, so this spells them the same way.
		byWire := make(map[string]data.Entity)
		for _, entity := range set.Entities().All() {
			namespace, ok := legacyNamespace(entity.Type)
			if !ok {
				continue
			}
			byWire[legacyPrefix+namespace+"/"+strconv.Itoa(int(entity.ID))] = entity
		}

		return Kinds{byWire: byWire}, nil
	}

	set, err := gen26_1.Data()
	if err != nil {
		return Kinds{}, fmt.Errorf("load the 26.1 data set: %w", err)
	}

	byWire := make(map[string]data.Entity)
	for _, entity := range set.Entities().All() {
		byWire[currentPrefix+strconv.Itoa(int(entity.ID))] = entity
	}

	return Kinds{byWire: byWire}, nil
}

// legacyNamespace maps protocol 47's two identifier namespaces to the words the
// adapter puts in a type string.
//
// The classifications 26.1 publishes are not namespaces and have no place in a
// type string, so they answer false here and the 26.1 index is not built this
// way at all.
func legacyNamespace(kind data.EntityType) (string, bool) {
	switch kind {
	case data.EntityTypeMob:
		return "mob", true
	case data.EntityTypeObject:
		return "object", true
	case data.EntityTypeAmbient, data.EntityTypeAnimal, data.EntityTypeHostile,
		data.EntityTypeLiving, data.EntityTypeOther, data.EntityTypePassive,
		data.EntityTypePlayer, data.EntityTypeProjectile, data.EntityTypeWaterCreature:
		return "", false
	default:
		return "", false
	}
}

// The type strings the two adapters mint. They are duplicated from the
// adapters rather than exported by them, because they are how one internal
// package names things to itself and an example is not owed them; a mismatch
// shows up as an entity this client cannot name, which is a case that has to
// work anyway.
const (
	legacyPrefix  = "java/1.8.9:"
	currentPrefix = "java/26.1:entity/"
)
