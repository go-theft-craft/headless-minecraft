package main

import (
	"context"

	"github.com/go-theft-craft/headless-minecraft/world"
)

// This is the M7 half of the seam. It reads one snapshot per tick and answers
// the core's questions from it; it decides nothing and stores nothing, which is
// what keeps every decision in bot.go where the tests can reach it.
//
// One snapshot per tick, not one per question. The whole point of the world
// package's design is that six domains read at one revision describe one
// instant, and a reader that took its own snapshot per question would let the
// terrain move between the bot's feet and its head.

// Observed implements World over a snapshot supplier.
type Observed struct {
	ctx       context.Context
	snapshot  world.Snapshot
	navigator Navigator
	kinds     Kinds
}

// NewObserved binds one snapshot and the planner that routes over it.
//
// The context belongs to the tick rather than to the navigator: a route is
// searched inside one Advance, and a bot whose run is being shut down should
// abandon the search rather than finish it.
func NewObserved(
	ctx context.Context, snapshot world.Snapshot, navigator Navigator, kinds Kinds,
) Observed {
	return Observed{ctx: ctx, snapshot: snapshot, navigator: navigator, kinds: kinds}
}

// Route plans a way between two positions over this snapshot.
func (o Observed) Route(from, to Vec3) (Route, bool) {
	return o.navigator.Plan(o.ctx, o.snapshot.Chunks, from, to)
}

// Hurting reports whether the bot is standing in something that damages it.
func (o Observed) Hurting(at Vec3) bool {
	return o.navigator.Hurting(o.snapshot.Chunks, at)
}

// Safe finds the nearest cell that does not hurt, over this snapshot.
func (o Observed) Safe(from Vec3, within int) (Vec3, bool) {
	return o.navigator.Safe(o.snapshot.Chunks, from, within)
}

// Water finds the nearest water within a radius over this snapshot.
func (o Observed) Water(from Vec3, within int) (Vec3, bool) {
	return o.navigator.Water(o.snapshot.Chunks, from, within)
}

// Walkable reports whether a straight line is still clear over this snapshot.
func (o Observed) Walkable(from, to Vec3) bool {
	return o.navigator.Walkable(o.snapshot.Chunks, from, to)
}

// Spawn reports the compass target the server sent.
//
// The design called this "the world spawn" and said it is not the respawn
// point. That is backwards for this packet: a vanilla server sends the level's
// shared spawn on join and re-sends the same packet whenever the player's own
// respawn point moves, so the two are the same value and the second overwrites
// the first. The bot never sleeps, so its circle never moves; a bot that did
// would find its orbit recentred on its bed, which is the honest behaviour of
// the only spawn the protocol reports.
func (o Observed) Spawn() (Vec3, bool) {
	environment := o.snapshot.Environment
	if !environment.SpawnKnown {
		return Vec3{}, false
	}

	// The centre of the block, not its corner. A circle drawn through block
	// corners is half a block off the one an operator standing at spawn sees.
	return Vec3{
		X: float64(environment.Spawn.X) + 0.5,
		Y: float64(environment.Spawn.Y),
		Z: float64(environment.Spawn.Z) + 0.5,
	}, true
}

// Entity reports one tracked entity.
//
// Health is not on the snapshot: the server sends other entities' health as an
// attribute or a metadata field and the world stores both as sent, without
// interpreting either. The bot only ever compares health to zero to decide
// whether its target is still worth hitting, and Dead answers that question
// directly and without an inference, so Health stays zero here rather than
// carrying a number nobody computed.
func (o Observed) Entity(id int32) (Entity, bool) {
	tracked, ok := o.snapshot.Entities.Get(id)
	if !ok {
		return Entity{}, false
	}

	kind, named := o.kinds.Lookup(tracked.Type)

	return Entity{
		ID:       tracked.EntityID,
		Position: Vec3{X: tracked.X, Y: tracked.Y, Z: tracked.Z},
		Alive:    !tracked.Dead,
		Kind:     kind,
		Named:    named,
	}, true
}

// Self reads the local player out of the same snapshot.
//
// OnGround is not observed. The server never tells a client whether it is
// standing on something — the client tells the server — so this is false until
// M8.8 owns the body that knows. Nothing in the core reads it yet.
func observeSelf(snapshot world.Snapshot) (Self, bool) {
	player := snapshot.Player
	if !player.Known || !player.Placed {
		return Self{}, false
	}

	return Self{
		Position: Vec3{X: player.X, Y: player.Y, Z: player.Z},
		Health:   float64(player.Health),
		OnFire:   onFire(snapshot, player.EntityID),
	}, true
}

// onFire reads the burning bit out of the player's own entity metadata.
//
// The player is an entity like any other and the server describes it to itself:
// a live 26.1 session sent this client a hundred and seventy-four metadata
// updates about its own entity. The world tracks any entity it hears about,
// spawn packet or not, so the record is there to read.
//
// Index zero is shared_flags, which every entity in both versions carries first
// -- it is the head of the metadata key list the data set publishes -- and its
// low bit is fire. The value arrives as whatever number the codec decoded a
// byte into, so the type switch takes the shapes a byte can turn up as rather
// than betting on one.
func onFire(snapshot world.Snapshot, self int32) bool {
	entity, known := snapshot.Entities.Get(self)
	if !known {
		return false
	}

	flags, present := entity.Metadata[sharedFlagsIndex]
	if !present {
		return false
	}

	var bits int64
	switch value := flags.Value.(type) {
	case int8:
		bits = int64(value)
	case uint8:
		bits = int64(value)
	case int32:
		bits = int64(value)
	case int64:
		bits = value
	default:
		return false
	}

	return bits&onFireFlag != 0
}

// sharedFlagsIndex is the metadata index every entity carries first, and
// onFireFlag is the bit in it that says the entity is burning.
const (
	sharedFlagsIndex uint8 = 0
	onFireFlag       int64 = 0x01
)
