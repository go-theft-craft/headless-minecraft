package client_test

import (
	"testing"
	"time"

	"github.com/go-theft-craft/headless-minecraft/auth"
	"github.com/go-theft-craft/headless-minecraft/client"
	"github.com/go-theft-craft/headless-minecraft/client/internal/fixture"
	"github.com/go-theft-craft/headless-minecraft/event"
	"github.com/go-theft-craft/headless-minecraft/safety"
	"github.com/go-theft-craft/headless-minecraft/version/java"
	"github.com/go-theft-craft/headless-minecraft/world"
)

// The observed-world end-to-end lane. It drives a whole connection over a
// loopback socket — real framing, real generated codecs, a real login, the
// client's own loop, and every reducer the protocol 47 adapter builds — and
// asserts the properties the whole design rests on.
//
// It covers protocol 47 only, for the reason the session lane records: serving
// 775 needs a server-side login and the shared login.Acceptor is written
// against the v1_8 generated types. The 775 reducers are covered by their
// adapter's own packet scripts.

func observeTo(t *testing.T, addr string, w *world.World) *client.Client {
	t.Helper()

	provider, err := auth.Offline("observer")
	if err != nil {
		t.Fatalf("Offline: %v", err)
	}
	authz, err := safety.Authorize(addr, safety.ScopeObserve)
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}

	bot, err := client.New(
		client.WithAddress(addr),
		client.WithAuth(provider),
		client.WithVersion(java.Java1_8()),
		client.WithAuthorization(authz),
		client.WithConnectTimeout(5*time.Second),
		client.WithWorld(w),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	return bot
}

func TestEndToEndObservesAWholeWorld(t *testing.T) {
	if testing.Short() {
		t.Skip("end-to-end lane needs a loopback socket")
	}
	t.Parallel()

	addr, stop := fixture.Start(t, fixture.Script{ThroughReady: true, ThenWorld: true})
	defer stop()

	w := world.New()
	bot := observeTo(t, addr, w)

	// Every state domain at once, which is what examples/observe subscribes
	// to and what a consumer maintaining a world would.
	states, err := bot.Subscribe(
		event.DomainPlayer|event.DomainWorld|event.DomainEntities|
			event.DomainContainers|event.DomainRegistry|event.DomainChat,
		512,
	)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	if err := bot.Connect(t.Context()); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	// Connect returns at readiness, which is before the script's two waves
	// have arrived. Waiting for the last thing the script sends is what makes
	// this a lane rather than a race: closing here would assert against
	// whatever happened to have been applied.
	var events []event.Event
	for published := range states.C() {
		events = append(events, published)
		if published.Name() == event.NameWorldChunkUnloaded {
			break
		}
	}
	if err := bot.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	events = append(events, drain(states)...)

	snapshot := w.Snapshot()

	// Every event names a revision that exists, and no event names one the
	// world has not reached. This is the property the design exists for: the
	// snapshot at an event's revision shows what the event describes.
	if snapshot.Revision == 0 {
		t.Fatal("the world applied no batches")
	}
	for _, published := range events {
		if published.Revision() == 0 {
			t.Errorf("%s carries no revision", published.Name())
		}
		if published.Revision() > snapshot.Revision {
			t.Errorf("%s names revision %d, past the world's %d",
				published.Name(), published.Revision(), snapshot.Revision)
		}
	}

	// Protocol 47 has no bundle delimiter, so the fixture's every packet is
	// its own batch and revisions never repeat across two different names
	// from two different packets. Revisions must not go backwards either way.
	previous := uint64(0)
	for _, published := range events {
		if published.Revision() < previous {
			t.Fatalf("%s went back to revision %d from %d",
				published.Name(), published.Revision(), previous)
		}
		previous = published.Revision()
	}

	// The player half: the fixture logs in as entity 42 and places at 1,64,2.
	player := snapshot.Player
	if !player.Known || player.EntityID != 42 || !player.Placed {
		t.Errorf("player is %+v", player)
	}
	if player.X != 1 || player.Y != 64 || player.Z != 2 {
		t.Errorf("player is at %v,%v,%v, want 1,64,2", player.X, player.Y, player.Z)
	}

	// The environment half: the fixture's game-state change starts rain,
	// which on protocol 47 is reason 2 and not reason 1.
	if environment := snapshot.Environment; !environment.WeatherKnown || !environment.Raining {
		t.Errorf("weather is %+v, want raining", environment)
	}

	// The second wave took the entities, the container, and the chunk away.
	// A store that fills and never empties is the memory bug a week-long
	// session hits.
	if len(snapshot.Entities.Tracked) != 0 {
		t.Errorf("entities are %+v, want released", snapshot.Entities.Tracked)
	}
	if _, open := snapshot.Containers.Get(3); open {
		t.Error("the container is still open after a close")
	}
	if _, loaded := snapshot.Chunks.Get(world.ChunkPos{}); loaded {
		t.Error("the chunk is still loaded after an unload")
	}

	// Every domain the script touched published, and the entity that moved
	// published a move rather than only a spawn.
	for _, name := range []event.Name{
		event.NamePlayerSpawned,
		event.NamePlayerMoved,
		event.NameWorldChunkLoaded,
		event.NameWorldWeatherChanged,
		event.NameEntitySpawned,
		event.NameEntityMoved,
		event.NameEntityRemoved,
		event.NameContainerOpened,
		event.NameContainerClosed,
	} {
		if count(events, name) == 0 {
			t.Errorf("no %s was published", name)
		}
	}
}

func TestEndToEndFillsTheStoresBeforeEmptyingThem(t *testing.T) {
	if testing.Short() {
		t.Skip("end-to-end lane needs a loopback socket")
	}
	t.Parallel()

	// The same script with its second wave withheld, so what the stores held
	// at the spawn can be read without racing the wave that empties them.
	//
	// It used to be read from a subscriber the moment the second spawn was
	// published, which asserted that this reader won a race against the loop
	// that was already applying the destroy wave. It usually did. Under load it
	// did not, and it reported an empty world rather than a reader that fell
	// behind — the failure looked like the thing the test exists to catch.
	addr, stop := fixture.Start(t, fixture.Script{ThroughReady: true, ThenWorldArrival: true})
	defer stop()

	w := world.New()
	bot := observeTo(t, addr, w)

	entities, err := bot.Subscribe(event.DomainEntities, 512)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	// The reader starts before Connect. A subscription is bounded and a
	// subscriber that falls behind is closed rather than blocked, so a reader
	// that only started once Connect returned could have the whole script
	// published into its buffer while it waited.
	watched := make(chan uint64, 1)
	go func() {
		for published := range entities.C() {
			if spawned, ok := published.(event.EntitySpawned); ok && spawned.EntityID == 8 {
				watched <- spawned.Revision()

				return
			}
		}
		watched <- 0
	}()

	if err := bot.Connect(t.Context()); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	at := <-watched
	// Separated from the count, so a dropped subscription reports itself as one
	// rather than as a world that held nothing.
	if at == 0 {
		t.Fatal("the spawn of entity 8 never arrived; the subscription ended first")
	}

	// Nothing takes the entities away in this script, so the world still holds
	// them however far behind this reader is.
	snapshot := w.Snapshot()
	if err := bot.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	drain(entities)

	if snapshot.Revision < at {
		t.Fatalf("the snapshot is at revision %d, before the spawn's %d", snapshot.Revision, at)
	}
	if got := len(snapshot.Entities.Tracked); got != 2 {
		t.Errorf("the world held %d entities after both spawns, want 2", got)
	}
}

func observeToWithGrace(t *testing.T, addr string, w *world.World, grace time.Duration) *client.Client {
	t.Helper()

	provider, err := auth.Offline("observer")
	if err != nil {
		t.Fatalf("Offline: %v", err)
	}
	authz, err := safety.Authorize(addr, safety.ScopeObserve)
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}

	bot, err := client.New(
		client.WithAddress(addr),
		client.WithAuth(provider),
		client.WithVersion(java.Java1_8()),
		client.WithAuthorization(authz),
		client.WithConnectTimeout(5*time.Second),
		client.WithWorld(w),
		client.WithObservationGrace(grace),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	return bot
}

func TestEndToEndReportsAPlacedSessionThatLoadsNoChunk(t *testing.T) {
	if testing.Short() {
		t.Skip("end-to-end lane needs a loopback socket")
	}
	t.Parallel()

	// The session works: it logs in, it is placed, and packets keep arriving.
	// It just never carries terrain, which is what a vanilla 1.8.9 server did
	// to an adapter that reduced only map_chunk — and what nothing reported.
	addr, stop := fixture.Start(t, fixture.Script{ThroughReady: true, ThenWithoutTerrain: true})
	defer stop()

	w := world.New()
	bot := observeToWithGrace(t, addr, w, time.Nanosecond)

	session, err := bot.Subscribe(event.DomainSession, 512)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	if err := bot.Connect(t.Context()); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	// Bounded, so a guard that never fires fails as itself rather than as a
	// test binary timing out.
	var reported event.ObservationMissing
	deadline := time.After(5 * time.Second)
	for reported.Observation == "" {
		select {
		case published, open := <-session.C():
			if !open {
				t.Fatal("the subscription ended before the session reported anything")
			}
			if missing, ok := published.(event.ObservationMissing); ok {
				reported = missing
			}
		case <-deadline:
			t.Fatal("the session was placed, loaded no chunk, and reported nothing")
		}
	}
	if err := bot.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	rest := drain(session)

	if reported.Observation != event.NameWorldChunkLoaded {
		t.Fatalf("the session never reported missing terrain, got %q", reported.Observation)
	}
	if reported.Revision() == 0 {
		t.Error("the report names no revision")
	}
	// Once per connection: a warning repeated every batch is a warning nobody
	// reads.
	if again := count(rest, event.NameSessionObservationMissing); again != 0 {
		t.Errorf("the report was published %d more times", again)
	}
}

func TestEndToEndSaysNothingWhenTerrainArrives(t *testing.T) {
	if testing.Short() {
		t.Skip("end-to-end lane needs a loopback socket")
	}
	t.Parallel()

	// The same impatient grace against a session that does load a chunk. A
	// guard that fires here would be worse than no guard: a consumer learns to
	// ignore it and then ignores the real one.
	addr, stop := fixture.Start(t, fixture.Script{ThroughReady: true, ThenWorld: true})
	defer stop()

	w := world.New()
	bot := observeToWithGrace(t, addr, w, time.Nanosecond)

	everything, err := bot.Subscribe(event.DomainSession|event.DomainWorld, 512)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	if err := bot.Connect(t.Context()); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	var events []event.Event
	for published := range everything.C() {
		events = append(events, published)
		if published.Name() == event.NameWorldChunkUnloaded {
			break
		}
	}
	if err := bot.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	events = append(events, drain(everything)...)

	if count(events, event.NameWorldChunkLoaded) == 0 {
		t.Fatal("the script loaded no chunk, so the guard was never tested")
	}
	if said := count(events, event.NameSessionObservationMissing); said != 0 {
		t.Errorf("the guard fired %d times on a session that loaded terrain", said)
	}
}
