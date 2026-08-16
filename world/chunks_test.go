package world_test

import (
	"errors"
	"sync"
	"testing"

	"github.com/go-theft-craft/headless-minecraft/event"
	"github.com/go-theft-craft/headless-minecraft/world"
)

// countingDecoder records how many times a section was decoded, which is how
// laziness and the decode cache are proved.
type countingDecoder struct {
	mu    sync.Mutex
	calls int
	state uint32
}

func (d *countingDecoder) decode([]byte) ([]uint32, error) {
	d.mu.Lock()
	d.calls++
	d.mu.Unlock()

	states := make([]uint32, 4096)
	for i := range states {
		states[i] = d.state
	}

	return states, nil
}

func (d *countingDecoder) count() int {
	d.mu.Lock()
	defer d.mu.Unlock()

	return d.calls
}

func loadChunk(t *testing.T, chunks *world.Chunks, pos world.ChunkPos, decode world.SectionDecoder) {
	t.Helper()

	var c event.Collector
	chunks.Loaded(&c, pos, []world.SectionData{{Y: 4, Raw: []byte{1, 2, 3}, Decode: decode}}, nil)
}

func TestASectionIsDecodedOnFirstBlockReadNotOnReceipt(t *testing.T) {
	t.Parallel()

	// A server streams hundreds of chunks a consumer never reads a block
	// from. Decoding them all on arrival is work almost nobody wants.
	decoder := &countingDecoder{state: 7}
	w := world.New()
	loadChunk(t, w.Chunks(), world.ChunkPos{}, decoder.decode)

	if decoder.count() != 0 {
		t.Fatalf("loading a chunk decoded it %d times, want 0", decoder.count())
	}

	if state, ok := w.Snapshot().Chunks.Block(1, 64, 1); !ok || state != 7 {
		t.Fatalf("block read gave %d, %v", state, ok)
	}
	if decoder.count() != 1 {
		t.Errorf("a block read decoded %d times, want exactly 1", decoder.count())
	}

	// The cache is on the section, so a second read from a new snapshot does
	// not decode again.
	_, _ = w.Snapshot().Chunks.Block(2, 64, 2)
	if decoder.count() != 1 {
		t.Errorf("a second read decoded again: %d", decoder.count())
	}
}

func TestConcurrentReadersAndAWriterNeverRace(t *testing.T) {
	t.Parallel()

	// This is the test that catches the design this plan originally carried,
	// where a lazy decode under a read lock mutated shared state while a
	// block change wrote into the same chunk. Run under -race.
	decoder := &countingDecoder{state: 3}
	w := world.New()
	loadChunk(t, w.Chunks(), world.ChunkPos{}, decoder.decode)

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 200 {
				_, _ = w.Snapshot().Chunks.Block(1, 64, 1)
			}
		}()
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := range 200 {
			var c event.Collector
			_, _ = w.Apply(batch(), &c)
			w.Chunks().BlocksChanged(&c, []world.BlockChange{{
				Pos: world.BlockPos{X: 1, Y: 64, Z: 1}, State: uint32(i),
			}})
		}
	}()

	wg.Wait()
}

func TestABlockWriteSwapsASectionRatherThanEditingIt(t *testing.T) {
	t.Parallel()

	decoder := &countingDecoder{state: 1}
	w := world.New()
	loadChunk(t, w.Chunks(), world.ChunkPos{}, decoder.decode)

	held := w.Snapshot()
	if state, _ := held.Chunks.Block(1, 64, 1); state != 1 {
		t.Fatalf("block is %d before the change, want 1", state)
	}

	var c event.Collector
	w.Chunks().BlocksChanged(&c, []world.BlockChange{{
		Pos: world.BlockPos{X: 1, Y: 64, Z: 1}, State: 99,
	}})

	// The held snapshot describes the revision it was taken at. That is what
	// copy-on-write at section granularity buys.
	if state, _ := held.Chunks.Block(1, 64, 1); state != 1 {
		t.Errorf("the held snapshot now reports %d: the write edited a shared section", state)
	}
	if state, _ := w.Snapshot().Chunks.Block(1, 64, 1); state != 99 {
		t.Errorf("the world reports %d after the change, want 99", state)
	}
}

func TestOneChangeAndManyProduceTheSameEvent(t *testing.T) {
	t.Parallel()

	decoder := &countingDecoder{}
	w := world.New()
	loadChunk(t, w.Chunks(), world.ChunkPos{}, decoder.decode)

	var single event.Collector
	w.Chunks().BlocksChanged(&single, []world.BlockChange{
		{Pos: world.BlockPos{X: 1, Y: 64, Z: 1}, State: 5},
	})
	var many event.Collector
	w.Chunks().BlocksChanged(&many, []world.BlockChange{
		{Pos: world.BlockPos{X: 1, Y: 64, Z: 1}, State: 5},
		{Pos: world.BlockPos{X: 2, Y: 64, Z: 2}, State: 6},
	})

	one := single.Events(1)
	lots := many.Events(1)
	if len(one) != 1 || len(lots) != 1 {
		t.Fatalf("got %d and %d events, want one each", len(one), len(lots))
	}
	if one[0].Name() != lots[0].Name() {
		t.Errorf("a single change is %q and a multi is %q", one[0].Name(), lots[0].Name())
	}
	if got := len(lots[0].(event.WorldBlocksChanged).Positions); got != 2 {
		t.Errorf("the multi change carries %d positions, want 2", got)
	}
}

func TestAChangeInAnUnloadedChunkIsCountedNotFatal(t *testing.T) {
	t.Parallel()

	w := world.New()

	var c event.Collector
	w.Chunks().BlocksChanged(&c, []world.BlockChange{
		{Pos: world.BlockPos{X: 1000, Y: 64, Z: 1000}, State: 5},
	})

	changed := c.Events(1)[0].(event.WorldBlocksChanged)
	if changed.Dropped != 1 {
		t.Errorf("dropped count is %d, want 1", changed.Dropped)
	}
	if len(changed.Positions) != 0 {
		t.Errorf("an unloaded chunk reported %d applied changes", len(changed.Positions))
	}
}

func TestASectionThisClientCannotDecodeKeepsItsBytes(t *testing.T) {
	t.Parallel()

	// A version whose section format this client cannot read must not lose
	// what arrived, and must not end the session.
	w := world.New()
	var c event.Collector
	w.Chunks().Loaded(&c, world.ChunkPos{}, []world.SectionData{
		{Y: 4, Raw: []byte{9, 9, 9}},
	}, nil)

	chunk, ok := w.Snapshot().Chunks.Get(world.ChunkPos{})
	if !ok {
		t.Fatal("the chunk was not loaded")
	}
	if got := len(chunk.Sections[4].Raw()); got != 3 {
		t.Errorf("the section kept %d bytes, want the 3 that arrived", got)
	}
	if _, err := chunk.Block(1, 64, 1); !errors.Is(err, world.ErrSectionNotDecodable) {
		t.Errorf("block read returned %v, want ErrSectionNotDecodable", err)
	}
	if _, ok := w.Snapshot().Chunks.Block(1, 64, 1); ok {
		t.Error("an undecodable section reported a block")
	}
}

func TestUnloadReleasesEveryChunk(t *testing.T) {
	t.Parallel()

	// A chunk store that leaks is the one memory bug a long-running session
	// will certainly hit.
	decoder := &countingDecoder{}
	w := world.New()

	var c event.Collector
	for x := range int32(1000) {
		w.Chunks().Loaded(&c, world.ChunkPos{X: x}, []world.SectionData{
			{Y: 0, Raw: make([]byte, 8192), Decode: decoder.decode},
		}, nil)
	}
	if got := len(w.Snapshot().Chunks.Loaded); got != 1000 {
		t.Fatalf("loaded %d chunks, want 1000", got)
	}

	for x := range int32(1000) {
		w.Chunks().Unloaded(&c, world.ChunkPos{X: x})
	}
	if got := len(w.Snapshot().Chunks.Loaded); got != 0 {
		t.Errorf("%d chunks survived unloading", got)
	}
}

func TestNegativeCoordinatesLandInTheRightChunk(t *testing.T) {
	t.Parallel()

	// A block at -1 is in chunk -1, not chunk 0. Getting this wrong puts
	// every block west or north of spawn in the wrong column.
	decoder := &countingDecoder{state: 4}
	w := world.New()
	loadChunk(t, w.Chunks(), world.ChunkPos{X: -1, Z: -1}, decoder.decode)

	if state, ok := w.Snapshot().Chunks.Block(-1, 64, -1); !ok || state != 4 {
		t.Errorf("block at -1,-1 gave %d, %v", state, ok)
	}
}

func BenchmarkBlockLookup(b *testing.B) {
	decoder := &countingDecoder{state: 12}
	w := world.New()
	var c event.Collector
	w.Chunks().Loaded(&c, world.ChunkPos{}, []world.SectionData{
		{Y: 4, Raw: make([]byte, 8192), Decode: decoder.decode},
	}, nil)

	snapshot := w.Snapshot()
	b.ResetTimer()
	for i := range b.N {
		_, _ = snapshot.Chunks.Block(int32(i%16), 64, int32(i%16))
	}
}

func BenchmarkSnapshotWithChunks(b *testing.B) {
	// Decision 9 says copy-on-read snapshots are defensible only while this
	// stays cheap. After the immutable-section decision it is a pointer copy
	// per section, not a block copy.
	decoder := &countingDecoder{}
	w := world.New()
	var c event.Collector
	for x := range int32(400) {
		w.Chunks().Loaded(&c, world.ChunkPos{X: x}, []world.SectionData{
			{Y: 0, Raw: make([]byte, 8192), Decode: decoder.decode},
			{Y: 1, Raw: make([]byte, 8192), Decode: decoder.decode},
		}, nil)
	}

	b.ResetTimer()
	for range b.N {
		_ = w.Snapshot()
	}
}
