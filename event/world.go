package event

// The world domain covers the terrain the server streamed and the scalars that
// describe the world around it. Chunks and environment are separate reducers;
// they share this domain because a subscriber selects by domain, not by file.

// BlockPosition is one block's coordinates in an event.
type BlockPosition struct{ X, Y, Z int32 }

// WorldChunkLoaded reports a column the server sent. Its sections are stored
// as received and decoded on first block read.
type WorldChunkLoaded struct {
	Stamp

	X, Z     int32
	Sections int
}

func (WorldChunkLoaded) Name() Name     { return NameWorldChunkLoaded }
func (WorldChunkLoaded) Domain() Domain { return DomainWorld }

// WorldChunkUnloaded reports a column released.
type WorldChunkUnloaded struct {
	Stamp

	X, Z int32
}

func (WorldChunkUnloaded) Name() Name     { return NameWorldChunkUnloaded }
func (WorldChunkUnloaded) Domain() Domain { return DomainWorld }

// WorldBlocksChanged reports blocks the server changed.
//
// One packet changes one block and another changes many, and both are the same
// fact, so this carries a set and a single change is a set of one. Dropped
// counts changes that landed in a chunk this client has not loaded or a
// section it cannot decode.
type WorldBlocksChanged struct {
	Stamp

	Positions []BlockPosition
	States    []uint32
	Dropped   int
}

func (WorldBlocksChanged) Name() Name     { return NameWorldBlocksChanged }
func (WorldBlocksChanged) Domain() Domain { return DomainWorld }

// WorldBlockEntityChanged reports a block entity the server sent.
type WorldBlockEntityChanged struct {
	Stamp

	X, Y, Z int32
}

func (WorldBlockEntityChanged) Name() Name     { return NameWorldBlockEntityChanged }
func (WorldBlockEntityChanged) Domain() Domain { return DomainWorld }

// WorldLightChanged reports light data for a column.
type WorldLightChanged struct {
	Stamp

	X, Z int32
}

func (WorldLightChanged) Name() Name     { return NameWorldLightChanged }
func (WorldLightChanged) Domain() Domain { return DomainWorld }
