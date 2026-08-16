package world

import (
	"errors"
	"maps"
	"sync/atomic"

	"github.com/go-theft-craft/headless-minecraft/event"
)

// ErrSectionNotDecodable reports a section whose bytes this client cannot
// decode. It is not a session error: the bytes are kept and stay reachable,
// and only block lookups inside that section are unavailable.
var ErrSectionNotDecodable = errors.New("section format is not decodable by this client")

// ChunkPos identifies a chunk column.
type ChunkPos struct{ X, Z int32 }

// BlockPos identifies one block.
type BlockPos struct{ X, Y, Z int32 }

// SectionDecoder turns one section's received bytes into 4096 block state
// identifiers, indexed y*256 + z*16 + x.
//
// It is supplied by the version adapter, because the section format is the
// part of a chunk the two protocols share nothing of. It must be pure: the
// world calls it without a lock and two callers may race to call it, and both
// must compute the same answer from the same bytes.
type SectionDecoder func(raw []byte) ([]uint32, error)

// Section is one 16x16x16 slice of a chunk.
//
// A section is immutable once stored. Its received bytes are never written,
// and its decoded form is a pure function of them published through an atomic
// pointer, so a reader that triggers a decode mutates nothing another reader
// or a writer can see. A block write does not edit a section: it builds a
// replacement and swaps it in, which is what makes a snapshot a pointer copy.
type Section struct {
	y   int
	raw []byte

	decode  SectionDecoder
	decoded atomic.Pointer[sectionBlocks]
}

// sectionBlocks is the result of decoding a section, cached once.
type sectionBlocks struct {
	states []uint32
	err    error
}

// Y reports which section of the column this is.
func (s *Section) Y() int { return s.y }

// Raw returns the bytes the server sent, which stay reachable whether or not
// this client can decode them.
func (s *Section) Raw() []byte { return s.raw }

// Decoded reports whether this section has already been decoded. It exists so
// a test can prove laziness; a caller wanting blocks calls Block.
func (s *Section) Decoded() bool { return s.decoded.Load() != nil }

// blocks decodes on first use and caches the result.
//
// Two readers racing here both decode and one wins the store. A duplicate
// decode is wasted work and never wrong, which is the property that lets this
// run without a lock.
func (s *Section) blocks() ([]uint32, error) {
	if cached := s.decoded.Load(); cached != nil {
		return cached.states, cached.err
	}

	result := &sectionBlocks{}
	if s.decode == nil {
		result.err = ErrSectionNotDecodable
	} else {
		result.states, result.err = s.decode(s.raw)
	}
	s.decoded.CompareAndSwap(nil, result)

	cached := s.decoded.Load()

	return cached.states, cached.err
}

// Block returns one block state from this section, by its coordinates within
// the section.
func (s *Section) Block(x, y, z int32) (uint32, error) {
	states, err := s.blocks()
	if err != nil {
		return 0, err
	}
	index := y*256 + z*16 + x
	if index < 0 || int(index) >= len(states) {
		return 0, ErrSectionNotDecodable
	}

	return states[index], nil
}

// withBlock returns a replacement section with one block changed.
//
// The copy is one section, bounded at 4096 entries, and it is what keeps a
// held snapshot describing the revision it was taken at.
func (s *Section) withBlock(x, y, z int32, state uint32) (*Section, error) {
	states, err := s.blocks()
	if err != nil {
		return nil, err
	}

	replacement := make([]uint32, len(states))
	copy(replacement, states)
	index := y*256 + z*16 + x
	if index < 0 || int(index) >= len(replacement) {
		return nil, ErrSectionNotDecodable
	}
	replacement[index] = state

	next := &Section{y: s.y, raw: s.raw}
	next.decoded.Store(&sectionBlocks{states: replacement})

	return next, nil
}

// SectionData is one section as the adapter received it.
type SectionData struct {
	Y      int
	Raw    []byte
	Decode SectionDecoder
}

// chunk is one column.
type chunk struct {
	pos      ChunkPos
	sections map[int]*Section
	// entities holds block entities by position, as the protocol decoded
	// them. The world does not interpret them.
	entities map[BlockPos]any
	// light is the light data the server sent, kept as bytes. Nothing in
	// this milestone reads it, and dropping it would lose what arrived.
	light []byte
}

// ChunkView is one column in a snapshot. Its sections are shared pointers to
// immutable sections, so taking a snapshot copies pointers rather than blocks.
type ChunkView struct {
	Pos      ChunkPos
	Sections map[int]*Section
	Entities map[BlockPos]any
}

// Block returns one block state by world coordinates.
func (v ChunkView) Block(x, y, z int32) (uint32, error) {
	section, ok := v.Sections[sectionIndex(y)]
	if !ok {
		return 0, ErrSectionNotDecodable
	}

	return section.Block(mod16(x), mod16(y), mod16(z))
}

// Chunks is every loaded column.
type Chunks struct {
	loaded map[ChunkPos]*chunk
}

// ChunksView is the chunk half of a snapshot.
type ChunksView struct {
	Loaded map[ChunkPos]ChunkView
}

// Get returns one loaded column.
func (v ChunksView) Get(pos ChunkPos) (ChunkView, bool) {
	chunk, ok := v.Loaded[pos]

	return chunk, ok
}

// Block returns one block state by world coordinates, or false when its chunk
// is not loaded.
func (v ChunksView) Block(x, y, z int32) (uint32, bool) {
	chunk, ok := v.Get(ChunkPos{X: chunkCoord(x), Z: chunkCoord(z)})
	if !ok {
		return 0, false
	}
	state, err := chunk.Block(x, y, z)
	if err != nil {
		return 0, false
	}

	return state, true
}

func newChunks() *Chunks { return &Chunks{loaded: make(map[ChunkPos]*chunk)} }

func (s *Chunks) view() ChunksView {
	loaded := make(map[ChunkPos]ChunkView, len(s.loaded))
	for pos, c := range s.loaded {
		loaded[pos] = ChunkView{
			Pos: pos,
			// Pointer copies: a section is immutable, and a block write
			// swaps in a replacement rather than editing this one.
			Sections: maps.Clone(c.sections),
			Entities: maps.Clone(c.entities),
		}
	}

	return ChunksView{Loaded: loaded}
}

// Loaded records a chunk the server sent.
//
// Sections are stored as received and are not decoded here. A server streams
// hundreds of chunks a consumer never reads a block from, and decoding 4096
// blocks per section for all of them is work almost nobody wants.
func (s *Chunks) Loaded(c *event.Collector, pos ChunkPos, sections []SectionData, light []byte) {
	column, ok := s.loaded[pos]
	if !ok {
		column = &chunk{pos: pos, sections: make(map[int]*Section), entities: make(map[BlockPos]any)}
		s.loaded[pos] = column
	}
	for _, data := range sections {
		column.sections[data.Y] = &Section{y: data.Y, raw: data.Raw, decode: data.Decode}
	}
	if light != nil {
		column.light = light
	}

	event.Emit(c, event.WorldChunkLoaded{X: pos.X, Z: pos.Z, Sections: len(column.sections)})
}

// Unloaded releases a chunk and everything in it. A chunk store that keeps
// unloaded columns is the memory bug a long-running session will certainly
// hit.
func (s *Chunks) Unloaded(c *event.Collector, pos ChunkPos) {
	if _, ok := s.loaded[pos]; !ok {
		return
	}
	delete(s.loaded, pos)

	event.Emit(c, event.WorldChunkUnloaded{X: pos.X, Z: pos.Z})
}

// LightChanged records light data for a column.
func (s *Chunks) LightChanged(c *event.Collector, pos ChunkPos, light []byte) {
	if column, ok := s.loaded[pos]; ok {
		column.light = light
	}

	event.Emit(c, event.WorldLightChanged{X: pos.X, Z: pos.Z})
}

// BlockChange is one block the server changed.
type BlockChange struct {
	Pos   BlockPos
	State uint32
}

// BlocksChanged applies one or many block changes.
//
// A single change and a multi-block change are the same fact, so they produce
// one event carrying a set: a subscriber never has to handle both shapes.
// A change inside a section this client cannot decode is counted and dropped
// rather than failing the session.
func (s *Chunks) BlocksChanged(c *event.Collector, changes []BlockChange) {
	if len(changes) == 0 {
		return
	}

	applied := make([]BlockChange, 0, len(changes))
	dropped := 0
	for _, change := range changes {
		if s.apply(change) {
			applied = append(applied, change)

			continue
		}
		dropped++
	}

	event.Emit(c, event.WorldBlocksChanged{
		Positions: positions(applied),
		States:    states(applied),
		Dropped:   dropped,
	})
}

// apply swaps in a replacement section holding the new block.
func (s *Chunks) apply(change BlockChange) bool {
	column, ok := s.loaded[ChunkPos{X: chunkCoord(change.Pos.X), Z: chunkCoord(change.Pos.Z)}]
	if !ok {
		return false
	}
	section, ok := column.sections[sectionIndex(change.Pos.Y)]
	if !ok {
		return false
	}

	next, err := section.withBlock(
		mod16(change.Pos.X), mod16(change.Pos.Y), mod16(change.Pos.Z), change.State,
	)
	if err != nil {
		return false
	}
	column.sections[section.y] = next

	return true
}

// BlockEntityChanged records a block entity the server sent, as the protocol
// decoded it. The world does not interpret it: a sign, a chest, and something
// a mod invented are all data addressed by position.
func (s *Chunks) BlockEntityChanged(c *event.Collector, pos BlockPos, value any) {
	column, ok := s.loaded[ChunkPos{X: chunkCoord(pos.X), Z: chunkCoord(pos.Z)}]
	if ok {
		column.entities[pos] = value
	}

	event.Emit(c, event.WorldBlockEntityChanged{X: pos.X, Y: pos.Y, Z: pos.Z})
}

func positions(changes []BlockChange) []event.BlockPosition {
	out := make([]event.BlockPosition, 0, len(changes))
	for _, change := range changes {
		out = append(out, event.BlockPosition{X: change.Pos.X, Y: change.Pos.Y, Z: change.Pos.Z})
	}

	return out
}

func states(changes []BlockChange) []uint32 {
	out := make([]uint32, 0, len(changes))
	for _, change := range changes {
		out = append(out, change.State)
	}

	return out
}

// chunkCoord and sectionIndex use floored division, because a block at -1 is
// in chunk -1, not chunk 0.
func chunkCoord(v int32) int32 { return v >> 4 }

func sectionIndex(y int32) int { return int(y >> 4) }

func mod16(v int32) int32 { return v & 15 }
