package main

import (
	"context"
	"errors"
	"fmt"
)

// This file is the whole seam between the decision core and the library. The
// core reads World and the shell drives Actuator; neither imports the client.
// When M7 and M9 land, two adapters implement these and nothing else moves.

// Block is what the bot needs to know about one block to decide whether it can
// stand there. It is not a block state: the example has no business modelling
// one, and Solid is the only fact the bypass search asks for.
type Block struct {
	Solid bool
}

// Self is the local player's observed state.
type Self struct {
	Position Vec3
	Health   float64
	OnGround bool
}

// Entity is a tracked entity the bot may fight.
type Entity struct {
	ID       int32
	Position Vec3
	Health   float64
	Alive    bool
}

// World is observed state, read-only, as of one snapshot revision.
//
// Owed by M7.
type World interface {
	// Spawn reports the world spawn position, which is the orbit centre. It
	// is not the respawn point: the two differ once a bed is used, and the
	// circle is defined against the first.
	Spawn() (Vec3, bool)
	// Block reports one block. The second result is false when the chunk is
	// not loaded, which the bypass search must distinguish from air — strict
	// mode refuses to move through collision data it does not have, and a
	// missing chunk answering "not solid" would walk the bot into a wall it
	// simply could not see.
	Block(BlockPos) (Block, bool)
	// Entity reports one tracked entity by ID.
	Entity(id int32) (Entity, bool)
}

// Actuator is the actions the bot takes.
//
// Owed by M9. Step is M8.8 and M9.3, Attack is M9.6, and Respawn is the
// primitive M9 does not currently plan — see the design's Required surface.
type Actuator interface {
	// Step emits one movement update toward a target, jumping if asked. It is
	// one update and not a loop: the tick that drives this program is the
	// example's own, because the library ships no scheduler.
	Step(ctx context.Context, target Vec3, jump bool) error
	// Attack swings at an entity. Timing is the version profile's cooldown,
	// not a constant here, because 1.8.9 and 26.1.2 disagree and the example
	// must not encode either.
	Attack(ctx context.Context, id int32) error
	// Respawn answers a death.
	Respawn(ctx context.Context) error
}

// ErrNotYet reports a capability that a milestone still owes.
var ErrNotYet = errors.New("not available yet")

// Pending stands in for both ports until M7 and M9 land. Every method fails
// with the milestone that owes it, so running this program against a real
// server produces the list of what is missing rather than a nil dereference or
// a bot that silently does nothing.
type Pending struct{}

func (Pending) Spawn() (Vec3, bool)          { return Vec3{}, false }
func (Pending) Block(BlockPos) (Block, bool) { return Block{}, false }
func (Pending) Entity(int32) (Entity, bool)  { return Entity{}, false }
func (Pending) Step(context.Context, Vec3, bool) error {
	return fmt.Errorf("%w: movement is M8.8 and M9.3", ErrNotYet)
}

func (Pending) Attack(context.Context, int32) error {
	return fmt.Errorf("%w: attack is M9.6", ErrNotYet)
}

func (Pending) Respawn(context.Context) error {
	return fmt.Errorf("%w: respawn has no primitive planned, see the design", ErrNotYet)
}

// Missing lists what this example still needs, in the order a reader should
// care about it. It is printed on startup so the program says what it cannot do
// before it tries.
func Missing() []string {
	return []string{
		"M7:   observed world state — player position, blocks, entities, world spawn",
		"M7:   damage attributed to a source entity, and a death event",
		"M8.8: movement, so the bot can step and jump",
		"M9.6: attack, with the version profile's cooldown",
		"M9:   a respawn primitive, which Task 6's list does not contain",
	}
}
