package main

import (
	"fmt"

	"github.com/go-theft-craft/minecraft-protocol/data"
	gen1_8 "github.com/go-theft-craft/minecraft-protocol/generated/java/v1_8"
	gen26_1 "github.com/go-theft-craft/minecraft-protocol/generated/java/v26_1"

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
// It is its own port because the world package stores state IDs as the server
// sent them and deliberately models no block semantics. Splitting it out was a
// bet that the day a block registry landed, one type would be written and
// Observed would not change. That day came: MeasuredSolidity is that type, and
// nothing else here moved.
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

// MeasuredSolidity answers solidity from the library's measurement of the
// game's own rule.
//
// The example held that table itself until 2026-08-17, which put block
// semantics in whichever consumer happened to want them first and left the next
// one to write its own. The library owns it now — one measurement, both
// protocols, and the state encoding decoded where the version is already
// known — so this type is the whole of what the example still owes: a port
// implemented against a registry.
//
// A version nobody has measured yields a registry that is nil, and this
// classifies nothing rather than guessing. That is the same honesty the earlier
// stand-in had: a block nobody has described is not a block that has been shown
// to be safe, and the bypass search refuses to walk through what it cannot
// classify.
type MeasuredSolidity struct {
	movement data.BlockMovementRegistry
}

// NewSolidity reads the measurement for the version profile the bot is
// speaking.
//
// The version matters and getting it wrong is silent. Protocol 47 packs a chunk
// state as the block identifier shifted left four; a flattened protocol does
// not, so a 47 table asked about a 775 state answers about an unrelated block
// every time, confidently. Loading the version's own data is what keeps that
// impossible.
func NewSolidity(legacy bool) (MeasuredSolidity, error) {
	load := gen26_1.Data
	if legacy {
		load = gen1_8.Data
	}

	set, err := load()
	if err != nil {
		return MeasuredSolidity{}, fmt.Errorf("load game data: %w", err)
	}

	return MeasuredSolidity{movement: set.BlockMovement()}, nil
}

// Measured reports whether this version publishes the measurement at all. A
// bot without it can still see the world and cannot walk through it, so the
// example says so before connecting rather than after standing still.
func (s MeasuredSolidity) Measured() bool { return s.movement != nil }

// Solid reports whether a chunk block state stops the bot.
func (s MeasuredSolidity) Solid(state uint32) (bool, bool) {
	if s.movement == nil {
		return false, false
	}

	return s.movement.ByState(data.BlockStateID(state))
}
