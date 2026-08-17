package main

import "context"

// scripted is a world a test writes by hand. It is the reason the decision core
// has no client in it: a sealed box is three lines here and a server, a plugin,
// and a fixture world otherwise.
type scripted struct {
	spawn    Vec3
	hasSpawn bool
	// solid holds the blocks that are solid. Everything else is air.
	solid map[BlockPos]bool
	// loaded bounds the known world. A position outside it reports unloaded,
	// which is what the strict path refuses.
	loaded   func(BlockPos) bool
	entities map[int32]Entity
}

func newScripted() *scripted {
	return &scripted{
		spawn:    Vec3{X: 0, Y: 64, Z: 0},
		hasSpawn: true,
		solid:    map[BlockPos]bool{},
		loaded:   func(BlockPos) bool { return true },
		entities: map[int32]Entity{},
	}
}

func (s *scripted) Spawn() (Vec3, bool) { return s.spawn, s.hasSpawn }

func (s *scripted) Block(p BlockPos) (Block, bool) {
	if !s.loaded(p) {
		return Block{}, false
	}
	// The floor is at y=63 so that a bot standing at y=64 has ground under it.
	if p.Y < 64 {
		return Block{Solid: true}, true
	}

	return Block{Solid: s.solid[p]}, true
}

func (s *scripted) Entity(id int32) (Entity, bool) {
	e, ok := s.entities[id]

	return e, ok
}

// wall makes a column solid from y=64 up to height.
func (s *scripted) wall(x, z, height int) {
	for y := 64; y < 64+height; y++ {
		s.solid[BlockPos{X: x, Y: y, Z: z}] = true
	}
}

// seal makes every block in the band around a waypoint solid, which is the only
// way into Trapped.
func (s *scripted) seal(c Circle, waypoint, band int) {
	for offset := -band; offset <= band; offset++ {
		p := c.At(waypoint, float64(offset)).Floor()
		s.wall(p.X, p.Z, 3)
	}
}

// recording actuator, for the shell test.
type recording struct {
	steps   []Vec3
	attacks []int32
	respawn int
}

func (r *recording) Step(_ context.Context, from, target Vec3, _ bool) (Vec3, error) {
	r.steps = append(r.steps, target)

	return from, nil
}

func (r *recording) Attack(_ context.Context, id int32) error {
	r.attacks = append(r.attacks, id)

	return nil
}

func (r *recording) Respawn(context.Context) error {
	r.respawn++

	return nil
}
