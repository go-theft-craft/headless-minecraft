package main

import (
	"github.com/go-theft-craft/headless-minecraft/world"
)

// This is the M7 half of the seam. It reads one snapshot per tick and answers
// the core's questions from it; it decides nothing and stores nothing, which is
// what keeps every decision in bot.go where the tests can reach it.
//
// One snapshot per tick, not one per question. The whole point of the world
// package's design is that six domains read at one revision describe one
// instant, and a Block call that took its own snapshot would let the terrain
// move between the bot's feet and its head.

// Solidity answers whether a block state stops the bot.
//
// It is its own port because it is the one thing M7 does not supply and cannot:
// the world package stores state IDs as the server sent them and deliberately
// models no block semantics, so nothing in the library maps a state to "solid".
// Splitting it out means the day a block registry lands, one type is written
// and Observed does not change.
type Solidity interface {
	// Solid reports whether a block state is solid. The second result is false
	// when the mapping is unknown, which is not the same as "not solid" — the
	// bypass search refuses to walk through a state it cannot classify.
	Solid(state uint32) (bool, bool)
}

// Observed implements World over a snapshot supplier.
type Observed struct {
	snapshot world.Snapshot
	solidity Solidity
}

// NewObserved binds one snapshot and a solidity source.
func NewObserved(snapshot world.Snapshot, solidity Solidity) Observed {
	return Observed{snapshot: snapshot, solidity: solidity}
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

// Block reports one block, and distinguishes an unloaded chunk from air.
//
// There are two ways to not know, and both answer false: the chunk is not
// loaded, or it is loaded and nothing can say what its state means. Collapsing
// them here is safe because the caller treats both as Unknown and refuses to
// move through either.
func (o Observed) Block(pos BlockPos) (Block, bool) {
	// The example's own BlockPos is int-wide and the library's is int32-wide.
	// The conversion is safe: these coordinates come from a circle of radius 25
	// around a position the server sent, not from arithmetic that could overflow.
	state, loaded := o.snapshot.Chunks.Block(int32(pos.X), int32(pos.Y), int32(pos.Z))
	if !loaded {
		return Block{}, false
	}

	solid, classified := o.solidity.Solid(state)
	if !classified {
		return Block{}, false
	}

	return Block{Solid: solid}, true
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

	return Entity{
		ID:       tracked.EntityID,
		Position: Vec3{X: tracked.X, Y: tracked.Y, Z: tracked.Z},
		Alive:    !tracked.Dead,
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
	}, true
}

// Chunk47Solidity answers solidity from the table extracted from the game.
//
// It is the whole of vanilla's own rule. Block.isPassable in this version is
// !blockMaterial.blocksMovement(), and the ground navigator that decides where
// a mob may walk calls that same predicate; neither looks at a bounding box or
// at whether the block fills its cell. So this looks up one boolean and stops.
//
// The earlier stand-in classified nothing, which made every position unknown
// and left the bot reporting itself sealed in on open ground. That was honest
// and useless. This is the same honesty with the answer filled in: a block the
// table does not know is still unclassified, because a block nobody has
// described is not a block that has been shown to be safe.
type Chunk47Solidity struct{}

// Solid reports whether a protocol 47 chunk state stops the bot.
func (Chunk47Solidity) Solid(state uint32) (bool, bool) {
	// A chunk state carries the block identifier above the metadata, and the
	// metadata never changes whether a block blocks movement in this version:
	// the material hangs off the block. So the shift is the whole lookup.
	solid, known := blocksMovement[int(state>>4)]

	return solid, known
}
