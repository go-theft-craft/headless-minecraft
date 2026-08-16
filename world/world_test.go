package world_test

import (
	"errors"
	"testing"

	protocol "github.com/go-theft-craft/minecraft-protocol"

	"github.com/go-theft-craft/headless-minecraft/event"
	"github.com/go-theft-craft/headless-minecraft/version"
	"github.com/go-theft-craft/headless-minecraft/world"
)

// countingReducer records how many batches and packets it saw.
type countingReducer struct {
	batches int
	packets int
}

func (r *countingReducer) Reduce(_ *world.Context, b version.Batch, _ *event.Collector) error {
	r.batches++
	r.packets += len(b.Packets)

	return nil
}

// failingReducer stands in for a broken invariant in this repository.
type failingReducer struct{}

var errReducerBroke = errors.New("reducer invariant broke")

func (failingReducer) Reduce(*world.Context, version.Batch, *event.Collector) error {
	return errReducerBroke
}

// recordingReducer records the revision it was told each batch would produce
// and appends one event per batch.
type recordingReducer struct {
	name      string
	order     *[]string
	revisions []uint64
}

func (r *recordingReducer) Reduce(ctx *world.Context, _ version.Batch, c *event.Collector) error {
	if r.order != nil {
		*r.order = append(*r.order, r.name)
	}
	r.revisions = append(r.revisions, ctx.Revision)
	event.Emit(c, event.Closed{})

	return nil
}

func batch(names ...string) version.Batch {
	packets := make([]protocol.Packet, 0, len(names))
	for _, name := range names {
		packets = append(packets, protocol.Packet{Name: name, State: "play"})
	}

	return version.Batch{Packets: packets, Bundled: len(names) > 1, State: "play"}
}

func TestRevisionStartsAtZeroAndBumpsOncePerBatch(t *testing.T) {
	t.Parallel()

	w := world.New()

	if got := w.Snapshot().Revision; got != 0 {
		t.Fatalf("initial revision is %d, want 0", got)
	}

	var c event.Collector
	revision, err := w.Apply(batch("a"), &c)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if revision != 1 {
		t.Errorf("Apply reported revision %d, want 1", revision)
	}
	if got := w.Snapshot().Revision; got != 1 {
		t.Errorf("revision after one batch is %d, want 1", got)
	}

	// A bundle of three packets is still one batch, so still one bump.
	// This is the property M6.3 built batching for.
	if _, err := w.Apply(batch("a", "b", "c"), &c); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got := w.Snapshot().Revision; got != 2 {
		t.Errorf("revision after a three-packet bundle is %d, want 2", got)
	}
}

func TestEveryRegisteredReducerSeesEveryBatch(t *testing.T) {
	t.Parallel()

	w := world.New()
	first, second := &countingReducer{}, &countingReducer{}
	if err := w.Register(first); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := w.Register(second); err != nil {
		t.Fatalf("Register: %v", err)
	}

	var c event.Collector
	if _, err := w.Apply(batch("a", "b"), &c); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	for name, r := range map[string]*countingReducer{"first": first, "second": second} {
		if r.batches != 1 || r.packets != 2 {
			t.Errorf("%s saw %d batches and %d packets, want 1 and 2", name, r.batches, r.packets)
		}
	}
}

func TestAnEmptyBatchStillBumpsTheRevision(t *testing.T) {
	t.Parallel()

	// An empty bundle is legal and observable: the server said something
	// happened even if nothing in it was modelled.
	w := world.New()

	var c event.Collector
	if _, err := w.Apply(version.Batch{Bundled: true, State: "play"}, &c); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if got := w.Snapshot().Revision; got != 1 {
		t.Errorf("revision after an empty bundle is %d, want 1", got)
	}
}

func TestAReducerErrorPoisonsTheWorld(t *testing.T) {
	t.Parallel()

	// A reducer error means an invariant here broke, not that the server
	// sent something odd. The batch is already half applied and nothing can
	// undo it, so the world stops answering rather than answering wrongly.
	w := world.New()
	_ = w.Register(failingReducer{})

	var c event.Collector
	if _, err := w.Apply(batch("a"), &c); err == nil {
		t.Fatal("Apply hid a reducer error")
	}
	if got := w.Snapshot().Revision; got != 0 {
		t.Errorf("revision is %d after a failed batch, want 0", got)
	}

	if _, err := w.Apply(batch("b"), &c); !errors.Is(err, world.ErrWorldPoisoned) {
		t.Errorf("second Apply returned %v, want ErrWorldPoisoned", err)
	}
	if _, err := w.SnapshotErr(); !errors.Is(err, world.ErrWorldPoisoned) {
		t.Errorf("SnapshotErr returned %v, want ErrWorldPoisoned", err)
	}
	// The reducer's own error survives wrapping: a poisoned world says what
	// broke, not only that something did.
	if _, err := w.SnapshotErr(); !errors.Is(err, errReducerBroke) {
		t.Errorf("SnapshotErr lost the reducer's error: %v", err)
	}
}

func TestEveryReducerSeesTheRevisionTheBatchWillProduce(t *testing.T) {
	t.Parallel()

	// Reducers append unstamped events and the world stamps them after the
	// bump, so an event never names a revision that does not yet exist.
	w := world.New()
	reducer := &recordingReducer{name: "one"}
	_ = w.Register(reducer)

	var published []event.Event
	for range 2 {
		var c event.Collector
		revision, err := w.Apply(batch("a"), &c)
		if err != nil {
			t.Fatalf("Apply: %v", err)
		}
		published = append(published, c.Events(revision)...)
	}

	want := []uint64{1, 2}
	for i, revision := range want {
		if reducer.revisions[i] != revision {
			t.Errorf("batch %d told the reducer revision %d, want %d", i, reducer.revisions[i], revision)
		}
		if published[i].Revision() != revision {
			t.Errorf("batch %d published revision %d, want %d", i, published[i].Revision(), revision)
		}
	}
}

func TestReducersRunInRegistrationOrder(t *testing.T) {
	t.Parallel()

	// The entity reducer depends on this to read the local entity ID the
	// player reducer sets.
	var order []string
	w := world.New()
	for _, name := range []string{"player", "entities", "chunks"} {
		if err := w.Register(&recordingReducer{name: name, order: &order}); err != nil {
			t.Fatalf("Register: %v", err)
		}
	}

	var c event.Collector
	if _, err := w.Apply(batch("a"), &c); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	want := []string{"player", "entities", "chunks"}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("reducers ran as %v, want %v", order, want)
		}
	}
}

func TestAFactOneReducerLearnsReachesTheNext(t *testing.T) {
	t.Parallel()

	// Local is the only cross-reducer fact this design admits, and it has to
	// survive the batch that set it: the Login packet arrives once.
	w := world.New()
	setter := reducerFunc(func(ctx *world.Context, _ version.Batch, _ *event.Collector) error {
		ctx.Local = world.LocalRef{EntityID: 42, Known: true}

		return nil
	})

	var seen []world.LocalRef
	reader := reducerFunc(func(ctx *world.Context, _ version.Batch, _ *event.Collector) error {
		seen = append(seen, ctx.Local)

		return nil
	})

	_ = w.Register(setter)
	_ = w.Register(reader)

	var c event.Collector
	_, _ = w.Apply(batch("login"), &c)
	_, _ = w.Apply(batch("later"), &c)

	if len(seen) != 2 {
		t.Fatalf("the reader ran %d times, want 2", len(seen))
	}
	if !seen[0].Known || seen[0].EntityID != 42 {
		t.Errorf("the reader saw %+v in the batch that set it", seen[0])
	}
	if !seen[1].Known || seen[1].EntityID != 42 {
		t.Errorf("the reader saw %+v in the next batch: the fact did not survive", seen[1])
	}
}

func TestSnapshotIsSafeWhileApplyRuns(t *testing.T) {
	t.Parallel()

	// Run under -race: a reader must never observe a partially applied
	// batch, which is the guarantee the whole design rests on.
	w := world.New()
	_ = w.Register(&countingReducer{})

	done := make(chan struct{})
	go func() {
		defer close(done)
		var c event.Collector
		for range 1000 {
			c.Reset()
			_, _ = w.Apply(batch("a"), &c)
		}
	}()

	for range 1000 {
		_ = w.Snapshot()
	}
	<-done
}

func TestRegisterAfterFirstApplyIsAnError(t *testing.T) {
	t.Parallel()

	w := world.New()
	var c event.Collector
	_, _ = w.Apply(batch("a"), &c)

	if err := w.Register(&countingReducer{}); !errors.Is(err, world.ErrWorldStarted) {
		t.Fatal("Register accepted a reducer after the world started applying")
	}
}

func TestRegisterRejectsANilReducer(t *testing.T) {
	t.Parallel()

	if err := world.New().Register(nil); err == nil {
		t.Fatal("Register accepted a nil reducer")
	}
}

// reducerFunc adapts a function to world.Reducer.
type reducerFunc func(*world.Context, version.Batch, *event.Collector) error

func (f reducerFunc) Reduce(ctx *world.Context, b version.Batch, c *event.Collector) error {
	return f(ctx, b, c)
}
