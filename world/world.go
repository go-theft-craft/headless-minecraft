// Package world holds what the connection has observed.
//
// It stores only what the server sent. Mechanics live outside it: a body
// model, a physics model, and a movement strategy are M8 and M9, and keeping
// them out is what lets a modded server's world be represented without
// mod-specific conditions in the reducers.
//
// The unit of application is a batch, not a packet. Protocol 775 bundles
// packets that must take effect together, so the revision counter bumps once
// per batch and a reader never observes half a bundle.
package world

import (
	"errors"
	"fmt"
	"sync"

	"github.com/go-theft-craft/headless-minecraft/event"
	"github.com/go-theft-craft/headless-minecraft/version"
)

// ErrWorldStarted reports registration attempted after the first batch.
var ErrWorldStarted = errors.New("world has already applied a batch")

// ErrWorldPoisoned reports a world that aborted a batch part-way and can no
// longer describe the server's state.
var ErrWorldPoisoned = errors.New("world is poisoned by a failed batch")

// LocalRef identifies the local player's entity, once the server has said
// which one it is.
//
// Known is separate from the ID because entity 0 is a legal entity: a zero
// value would otherwise claim the local player is whatever entity happens to
// hold that ID.
type LocalRef struct {
	EntityID int32
	Known    bool
}

// Context is what a reducer knows beyond its own state and the batch. The
// world owns it, it is valid only for one Reduce call, and it is the only
// channel through which one reducer sees a fact another reducer learned.
type Context struct {
	// Revision is the revision this batch will produce, not the current one.
	Revision uint64
	// State is the protocol state the batch arrived in. Registry data
	// arrives in configuration, so "not sent yet" and "will not be sent"
	// are distinguishable.
	State string
	// Local is the local player's entity ID, set by the player reducer when
	// it reads the play Login packet and read by every reducer after it.
	Local LocalRef
}

// Reducer applies one batch to its own private state.
//
// A reducer never publishes, never performs I/O, never blocks, and never
// stamps a revision: it appends unstamped events to the collector and returns.
//
// It returns an error only when an invariant in this package broke. Data the
// server sent that this model does not recognize is never an error: unknown
// entity IDs, unknown metadata indices, unmapped menu types, and out-of-range
// values are all normal traffic from a modded server, and each is preserved or
// dropped with a counter.
type Reducer interface {
	Reduce(ctx *Context, batch version.Batch, collector *event.Collector) error
}

// World owns every reducer and the single revision counter.
//
// Reducers run in registration order, and that order is a contract: the player
// reducer learns the local entity ID and every reducer after it reads that ID
// from the Context.
type World struct {
	mu       sync.RWMutex
	reducers []Reducer
	revision uint64
	started  bool
	poisoned error
	// local carries the local player's entity ID across batches, since the
	// Login packet that supplies it arrives once.
	local LocalRef

	player   *Player
	entities *Entities
	chunks   *Chunks
}

// New returns an empty world with no reducers.
//
// The domain stores exist from the start and are empty. A version adapter
// builds the reducers that fill them, because decoding is the only part of
// observed state that differs between protocols.
func New() *World {
	return &World{player: newPlayer(), entities: newEntities(), chunks: newChunks()}
}

// Player returns the local player's store, for a version adapter to write
// through. A caller reading state wants Snapshot instead: this is not
// synchronised on its own.
func (w *World) Player() *Player { return w.player }

// Entities returns the tracked-entity store, for a version adapter to write
// through.
func (w *World) Entities() *Entities { return w.entities }

// Chunks returns the chunk store, for a version adapter to write through.
func (w *World) Chunks() *Chunks { return w.chunks }

// Register adds a reducer. It must be called before the first Apply, because
// a reducer that missed earlier batches holds state nobody can reconstruct.
func (w *World) Register(r Reducer) error {
	if r == nil {
		return errors.New("world: nil reducer")
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	if w.started {
		return ErrWorldStarted
	}
	w.reducers = append(w.reducers, r)

	return nil
}

// Apply runs every reducer over one batch in registration order under a single
// write lock, bumps the revision, and reports the revision the batch produced.
//
// The caller stamps the collector with what this returns. A reducer never sees
// a stamp and never sets one, so it cannot name a revision that does not exist
// yet, and every event from one batch names the same one.
//
// A reducer error aborts the batch and leaves the revision alone. The batch is
// then partially applied and nothing can undo the reducers that already ran, so
// the world is poisoned: every later call reports the same error and the
// session ends. A world that silently diverged from the server is worse than a
// connection that stopped, and a poisoned world still answering queries is
// worse than both.
func (w *World) Apply(batch version.Batch, collector *event.Collector) (uint64, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.poisoned != nil {
		return w.revision, w.poisoned
	}
	w.started = true

	ctx := &Context{Revision: w.revision + 1, State: string(batch.State), Local: w.local}

	for _, reducer := range w.reducers {
		if err := reducer.Reduce(ctx, batch, collector); err != nil {
			w.poisoned = fmt.Errorf("%w: %w", ErrWorldPoisoned, err)

			return w.revision, w.poisoned
		}
	}
	w.revision++
	w.local = ctx.Local

	return w.revision, nil
}

// Snapshot returns an immutable view of every domain at one revision. A
// poisoned world returns the zero snapshot; callers that need to tell that
// apart from a fresh one use SnapshotErr.
func (w *World) Snapshot() Snapshot {
	snap, _ := w.SnapshotErr()

	return snap
}

// SnapshotErr returns the snapshot and any error that poisoned the world.
func (w *World) SnapshotErr() (Snapshot, error) {
	w.mu.RLock()
	defer w.mu.RUnlock()

	if w.poisoned != nil {
		return Snapshot{}, w.poisoned
	}

	return w.snapshot(), nil
}
