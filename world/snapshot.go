package world

// Snapshot is one instant of observed state, taken at one revision.
//
// Every domain's view in a snapshot was read under the same lock at the same
// revision, which is the property the whole design exists for: six domains
// read at one revision describe one instant, not six instants that happen to
// be close together.
//
// A snapshot is immutable from the caller's perspective. Nothing in it aliases
// state the world keeps mutating.
type Snapshot struct {
	// Revision is the number of batches this world has applied. It is zero
	// on a world that has applied none.
	Revision uint64
	// Player is the local player.
	Player PlayerView
	// Entities is every other entity the client is tracking.
	Entities EntitiesView
	// Chunks is the terrain the server has streamed.
	Chunks ChunksView
	// Environment is the world's scalars: clock, border, weather, difficulty,
	// and simulation settings.
	Environment EnvironmentView
}

// snapshot builds the snapshot. It runs under the world's lock, held by the
// caller, and each domain added by a later task reads its view here.
func (w *World) snapshot() Snapshot {
	return Snapshot{
		Revision:    w.revision,
		Player:      w.player.view(),
		Entities:    w.entities.view(),
		Chunks:      w.chunks.view(),
		Environment: w.environment.view(),
	}
}
