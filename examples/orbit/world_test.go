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
	// harmful holds the cells that damage a body standing in them, and water
	// the cells a burning bot can put itself out in.
	harmful map[BlockPos]bool
	water   map[BlockPos]bool
}

func newScripted() *scripted {
	return &scripted{
		spawn:    Vec3{X: 0, Y: 64, Z: 0},
		hasSpawn: true,
		solid:    map[BlockPos]bool{},
		loaded:   func(BlockPos) bool { return true },
		entities: map[int32]Entity{},
		harmful:  map[BlockPos]bool{},
		water:    map[BlockPos]bool{},
	}
}

func (s *scripted) Spawn() (Vec3, bool) { return s.spawn, s.hasSpawn }

// Route is a straight line, sampled once per block, refused if anything on it
// is solid or unstreamed.
//
// It is deliberately not a planner. The planner is minecraft-simulation's, it
// has its own tests and its own benchmarks, and running it here would test that
// package instead of this state machine. This is just enough routing that a
// test can wall something off and say what the bot should decide.
func (s *scripted) Route(from, to Vec3) (Route, bool) {
	steps := make([]Vec3, 0, 8)

	for d := 1.0; d < from.HorizontalDistance(to); d++ {
		point := from.Toward(to, d)
		if s.blocked(point) {
			return Route{}, false
		}
		steps = append(steps, point)
	}
	if s.blocked(to) {
		return Route{}, false
	}

	return Route{Steps: append(steps, to), Complete: true}, true
}

// Safe returns the nearest cell a test has not declared harmful.
func (s *scripted) Safe(from Vec3, within int) (Vec3, bool) {
	foot := from.Floor()
	for radius := range within + 1 {
		for dx := -radius; dx <= radius; dx++ {
			for dz := -radius; dz <= radius; dz++ {
				if abs(dx) != radius && abs(dz) != radius {
					continue
				}
				cell := BlockPos{X: foot.X + dx, Y: foot.Y, Z: foot.Z + dz}
				if s.harmful[cell] || !s.loaded(cell) {
					continue
				}

				return Vec3{X: float64(cell.X) + 0.5, Y: from.Y, Z: float64(cell.Z) + 0.5}, true
			}
		}
	}

	return Vec3{}, false
}

// Water returns the nearest cell a test has declared to be water.
func (s *scripted) Water(from Vec3, within int) (Vec3, bool) {
	var nearest Vec3
	found := false

	for cell := range s.water {
		at := Vec3{X: float64(cell.X) + 0.5, Y: float64(cell.Y), Z: float64(cell.Z) + 0.5}
		if at.HorizontalDistance(from) > float64(within) {
			continue
		}
		if !found || at.HorizontalDistance(from) < nearest.HorizontalDistance(from) {
			nearest, found = at, true
		}
	}

	return nearest, found
}

// Hurting reports the cells a test has declared harmful.
func (s *scripted) Hurting(at Vec3) bool {
	foot := at.Floor()
	for h := range 2 {
		if s.harmful[BlockPos{X: foot.X, Y: foot.Y + h, Z: foot.Z}] {
			return true
		}
	}

	return false
}

// Walkable is the same straight line Route walks, asked on its own.
func (s *scripted) Walkable(from, to Vec3) bool {
	for d := 1.0; d < from.HorizontalDistance(to); d++ {
		if s.blocked(from.Toward(to, d)) {
			return false
		}
	}

	return !s.blocked(to)
}

// blocked reports whether a body standing here would not fit. The cell the bot
// is already in is never asked about: these fictions put the bot inside walls
// to reach Trapped, and a body that cannot leave a block it is already in is a
// body that can never get out of one.
func (s *scripted) blocked(p Vec3) bool {
	foot := p.Floor()
	for h := range 2 {
		cell := BlockPos{X: foot.X, Y: foot.Y + h, Z: foot.Z}
		if !s.loaded(cell) {
			return true
		}
		// The floor is at y=63 so that a bot standing at y=64 has ground
		// under it.
		if cell.Y >= 64 && s.solid[cell] {
			return true
		}
	}

	return false
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
	// walking is every locomotion state it was told about, in order, so a test
	// can assert that the bot announced the change and did not repeat itself.
	walking []bool
	// marks is every position the trail was asked to paint, and kills how many
	// times the bot asked the server to kill it.
	marks []Vec3
	kills int
}

func (r *recording) Step(_ context.Context, from, target Vec3, _ bool) (Vec3, error) {
	r.steps = append(r.steps, target)

	return from, nil
}

func (r *recording) Attack(_ context.Context, id int32) error {
	r.attacks = append(r.attacks, id)

	return nil
}

func (r *recording) Locomotion(_ context.Context, walking bool) error {
	r.walking = append(r.walking, walking)

	return nil
}

func (r *recording) Mark(_ context.Context, at Vec3) error {
	r.marks = append(r.marks, at)

	return nil
}

func (r *recording) Kill(context.Context) error {
	r.kills++

	return nil
}

func (r *recording) Respawn(context.Context) error {
	r.respawn++

	return nil
}
