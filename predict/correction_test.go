package predict_test

import (
	"context"
	"sync"
	"testing"

	simgeom "github.com/go-theft-craft/minecraft-simulation/geom"
	simmovement "github.com/go-theft-craft/minecraft-simulation/movement"

	"github.com/go-theft-craft/headless-minecraft/event"
	"github.com/go-theft-craft/headless-minecraft/predict"
	"github.com/go-theft-craft/headless-minecraft/world"
)

// M8.8's gate is zero corrections, so it cannot test what happens when one
// arrives. These are the cases that only exist once one has.

func TestACorrectionCanBeAnsweredByAskingTheLoop(t *testing.T) {
	// The first thing a caller does with a correction is ask the loop what it
	// now believes — that is what a correction is for. So the callback must run
	// somewhere the loop can be asked, and a callback that runs while the loop
	// holds its own lock is a callback that deadlocks the first caller who does
	// the obvious thing.
	var (
		mu        sync.Mutex
		loop      *predict.Loop
		answered  bool
		predicted simgeom.Vec3
		counted   int
	)

	loop, client, observed := loopOn(t, predict.Options{
		OnCorrection: func(predict.Correction) {
			body, ok := loop.Predicted()
			count := loop.Corrections()

			mu.Lock()
			defer mu.Unlock()
			answered, predicted, counted = ok, feet(body), count
		},
	})
	loop.Input(simmovement.Input{Forward: 1})

	if err := loop.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitFor(t, func() bool { return len(client.actions()) >= 5 })

	collector := &event.Collector{}
	observed.Player().Move(collector, 4.5, 1, 4.5, 0, 0, world.Relative{})
	client.setWorld(observed.Snapshot())

	waitFor(t, func() bool {
		mu.Lock()
		defer mu.Unlock()

		return answered
	})

	mu.Lock()
	defer mu.Unlock()

	// The point of the ordering: what the callback is told and what the loop
	// says must be the same position. A correction published after the next tick
	// had been predicted would describe a world the loop had already left.
	if predicted != (simgeom.Vec3{X: 4.5, Y: 1, Z: 4.5}) {
		t.Errorf("the loop said it was at %+v while reporting a correction to (4.5, 1, 4.5)",
			predicted)
	}
	if counted != 1 {
		t.Errorf("the loop counted %d corrections while reporting the first", counted)
	}
}

func TestACorrectionIsAdoptedRatherThanAccumulated(t *testing.T) {
	// A correction is absolute. A loop that added it to where it thought it was
	// would double every future one, and the second correction would be twice as
	// far away as the first — which is what a correction loop looks like from
	// the outside.
	var (
		mu   sync.Mutex
		seen []predict.Correction
	)

	loop, client, observed := loopOn(t, predict.Options{
		OnCorrection: func(correction predict.Correction) {
			mu.Lock()
			defer mu.Unlock()
			seen = append(seen, correction)
		},
	})
	loop.Input(simmovement.Input{Forward: 1})

	if err := loop.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitFor(t, func() bool { return len(client.actions()) >= 5 })

	// Two corrections to the same place. The second is the one that matters: if
	// the first was accumulated rather than adopted, the loop is now somewhere
	// the server never named, and the distance it reports back for the second
	// says so.
	for range 2 {
		collector := &event.Collector{}
		observed.Player().Move(collector, 4.5, 1, 4.5, 0, 0, world.Relative{})
		client.setWorld(observed.Snapshot())

		waitFor(t, func() bool {
			mu.Lock()
			defer mu.Unlock()

			return len(seen) >= 1
		})

		// Walk away from it, so the next server position is a disagreement
		// again rather than a repeat the loop ignores.
		observed.Player().Move(collector, 0.5, 1, 0.5, 0, 0, world.Relative{})
		client.setWorld(observed.Snapshot())
		waitFor(t, func() bool {
			mu.Lock()
			defer mu.Unlock()

			return len(seen) >= 2
		})
	}

	mu.Lock()
	defer mu.Unlock()

	for i, correction := range seen {
		if correction.To != (simgeom.Vec3{X: 4.5, Y: 1, Z: 4.5}) &&
			correction.To != (simgeom.Vec3{X: 0.5, Y: 1, Z: 0.5}) {
			t.Fatalf("correction %d put the player at %+v, which is neither position "+
				"the server named; an adopted correction is the server's number and "+
				"an accumulated one is not", i, correction.To)
		}
	}
}
