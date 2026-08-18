package predict_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	gen "github.com/go-theft-craft/minecraft-protocol/generated/java/v1_8"
	simentity "github.com/go-theft-craft/minecraft-simulation/entity"
	simgeom "github.com/go-theft-craft/minecraft-simulation/geom"
	simmovement "github.com/go-theft-craft/minecraft-simulation/movement"
	v1_8 "github.com/go-theft-craft/minecraft-simulation/profile/java/v1_8"
	simulation "github.com/go-theft-craft/minecraft-simulation/sim"

	"github.com/go-theft-craft/headless-minecraft/event"
	"github.com/go-theft-craft/headless-minecraft/predict"
	"github.com/go-theft-craft/headless-minecraft/version"
	"github.com/go-theft-craft/headless-minecraft/world"
)

// stone is the block identifier a pre-flattening protocol carries for stone: the
// block in the high bits and four bits of metadata in the low ones.
const stone uint32 = 1 << 4

// actor is a client that records what it was told to send and answers with a
// world a test controls.
type actor struct {
	mu       sync.Mutex
	sent     []version.Action
	snapshot world.Snapshot
	err      error
}

func (a *actor) Do(_ context.Context, action version.Action) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.err != nil {
		return a.err
	}
	a.sent = append(a.sent, action)

	return nil
}

func (a *actor) World() world.Snapshot {
	a.mu.Lock()
	defer a.mu.Unlock()

	return a.snapshot
}

func (a *actor) actions() []version.Action {
	a.mu.Lock()
	defer a.mu.Unlock()

	return append([]version.Action(nil), a.sent...)
}

func (a *actor) setWorld(snapshot world.Snapshot) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.snapshot = snapshot
}

// flatWorld returns an observed world with a floor of stone at y=0 and air
// above it, and the player standing on it.
func flatWorld(t *testing.T, at simgeom.Vec3) *world.World {
	t.Helper()

	built := world.New()
	collector := &event.Collector{}

	// One chunk column around the origin, described from bedrock to sky. The
	// decoder is the test's own: a section is bytes plus a function that says
	// what they mean, and this one means a floor.
	for chunkX := int32(-1); chunkX <= 1; chunkX++ {
		for chunkZ := int32(-1); chunkZ <= 1; chunkZ++ {
			var sections []world.SectionData
			for y := range 4 {
				sections = append(sections, world.SectionData{
					Y:      y,
					Raw:    []byte{byte(y)},
					Decode: decodeFloor,
				})
			}
			built.Chunks().Loaded(
				collector, world.ChunkPos{X: chunkX, Z: chunkZ}, sections, nil,
			)
		}
	}

	built.Player().Login(collector, 1, "overworld", 0)
	built.Player().Move(collector, at.X, at.Y, at.Z, 0, 0, world.Relative{})

	return built
}

// decodeFloor turns a section's one byte into its blocks: the bottom section
// holds stone in its lowest layer and air everywhere else.
func decodeFloor(raw []byte) ([]uint32, error) {
	states := make([]uint32, 16*16*16)
	if len(raw) == 0 || raw[0] != 0 {
		return states, nil
	}

	// Section zero: y=0 is the floor.
	for x := range 16 {
		for z := range 16 {
			states[(0*16+z)*16+x] = stone
		}
	}

	return states, nil
}

// loopOn returns a started loop over a flat world, and the actor it sends
// through.
func loopOn(t *testing.T, options predict.Options) (*predict.Loop, *actor, *world.World) {
	t.Helper()

	set, err := gen.Data()
	if err != nil {
		t.Fatalf("load the 1.8.9 data set: %v", err)
	}
	profile, err := v1_8.New(set)
	if err != nil {
		t.Fatalf("build the 1.8.9 profile: %v", err)
	}

	observed := flatWorld(t, simgeom.Vec3{X: 0.5, Y: 1, Z: 0.5})
	client := &actor{snapshot: observed.Snapshot()}

	names, ok := profile.(simulation.BlockNames)
	if !ok {
		t.Fatal("the profile cannot resolve block names")
	}

	options.Actor = client
	options.Profile = profile
	options.Blocks = predict.MetadataBlocks(set, names)
	options.Spawn = func(pos simgeom.Vec3, yaw, pitch float32) (
		simentity.State, simmovement.Locomotion, bool,
	) {
		return v1_8.Spawn(profile, pos, yaw, pitch)
	}
	if options.Interval == 0 {
		options.Interval = time.Millisecond
	}

	loop, err := predict.New(options)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = loop.Close() })

	return loop, client, observed
}

func TestNewRefusesAnIncompleteLoop(t *testing.T) {
	for name, options := range map[string]predict.Options{
		"no actor":   {},
		"no profile": {Actor: &actor{}},
	} {
		if _, err := predict.New(options); !errors.Is(err, predict.ErrLoop) {
			t.Errorf("New accepted a loop with %s: %v", name, err)
		}
	}
}

func TestThePredictionMovesTheBodyAndReportsIt(t *testing.T) {
	loop, client, _ := loopOn(t, predict.Options{})
	loop.Input(simmovement.Input{Forward: 1})

	if err := loop.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitFor(t, func() bool { return len(client.actions()) >= 5 })

	body, ok := loop.Predicted()
	if !ok {
		t.Fatal("the loop predicts nothing after five ticks")
	}
	if feet(body).Z <= 0.5 {
		t.Errorf("a body walking forward is at z=%v", feet(body).Z)
	}
	if !body.OnGround {
		t.Error("a body walking on a floor is not on the ground")
	}

	// Walking moves further than the cadence rule's threshold every tick, so
	// every packet after the first carries a position.
	var positions int
	for _, action := range client.actions() {
		switch action.(type) {
		case version.ActionMove, version.ActionMoveLook:
			positions++
		}
	}
	if positions < 4 {
		t.Errorf("a walking client sent %d positions in %d packets",
			positions, len(client.actions()))
	}
}

func TestAnIdleClientReportsAGroundFlagAndAForcedPosition(t *testing.T) {
	loop, client, _ := loopOn(t, predict.Options{})

	if err := loop.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitFor(t, func() bool { return len(client.actions()) >= 24 })

	actions := client.actions()

	// The first packet carries a position: nothing has been reported yet, so the
	// rule's "moved" is true by default and its "rotated" is too.
	if _, ok := actions[0].(version.ActionMoveLook); !ok {
		t.Fatalf("the first packet is a %T, want a position and rotation", actions[0])
	}

	// Then twenty bare ground flags, because a standing body's drift is under the
	// threshold, and a forced position on the twenty-first.
	for index := 1; index <= 20; index++ {
		if _, ok := actions[index].(version.ActionGround); !ok {
			t.Fatalf("packet %d is a %T, want a bare ground flag", index, actions[index])
		}
	}
	switch actions[21].(type) {
	case version.ActionMove, version.ActionMoveLook:
	default:
		t.Fatalf("packet 21 is a %T, want the forced position", actions[21])
	}
}

func TestATurnOnTheSpotReportsALook(t *testing.T) {
	loop, client, _ := loopOn(t, predict.Options{})

	if err := loop.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitFor(t, func() bool { return len(client.actions()) >= 3 })

	// Turning without moving: the rule sends a look, and only a look.
	loop.Input(simmovement.Input{Yaw: 90})
	waitFor(t, func() bool {
		for _, action := range client.actions() {
			if _, ok := action.(version.ActionLook); ok {
				return true
			}
		}

		return false
	})
}

func TestAServerPositionAfterTheBoundaryIsACorrection(t *testing.T) {
	var seen []predict.Correction
	var mu sync.Mutex

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

	// The server disagrees and puts the player somewhere else.
	collector := &event.Collector{}
	observed.Player().Move(collector, 4.5, 1, 4.5, 0, 0, world.Relative{})
	client.setWorld(observed.Snapshot())

	waitFor(t, func() bool { return loop.Corrections() >= 1 })

	mu.Lock()
	defer mu.Unlock()
	if len(seen) == 0 {
		t.Fatal("a correction was counted but not published")
	}
	if seen[0].To != (simgeom.Vec3{X: 4.5, Y: 1, Z: 4.5}) {
		t.Errorf("the correction reports %+v as the server's position", seen[0].To)
	}
	if seen[0].From == seen[0].To {
		t.Error("the correction reports the prediction and the server agreeing")
	}

	// And the prediction restarts from the server's position rather than from
	// where it had walked to.
	body, ok := loop.Predicted()
	if !ok {
		t.Fatal("the loop stopped predicting")
	}
	// It restarts from the server's position: the loop keeps walking from there,
	// so the assertion is that it is near it rather than exactly on it.
	if got := feet(body); got.X != 4.5 || got.Z < 4.5 || got.Z > 4.7 {
		t.Errorf("the body kept its predicted position %+v after a correction", got)
	}
}

func TestTheLoginPositionIsNotACorrection(t *testing.T) {
	// The server places the player during login as a matter of course. Counting
	// that would make the gate impossible to pass, and ignoring server positions
	// generally would make it impossible to fail — so the boundary is explicit
	// state: a position is a correction once the client has reported one of its
	// own.
	loop, client, observed := loopOn(t, predict.Options{})

	// A second placement before the loop has ever run.
	collector := &event.Collector{}
	observed.Player().Move(collector, 8.5, 1, 8.5, 0, 0, world.Relative{})
	client.setWorld(observed.Snapshot())

	if err := loop.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitFor(t, func() bool { return len(client.actions()) >= 3 })

	if got := loop.Corrections(); got != 0 {
		t.Errorf("the login placement counted %d corrections", got)
	}
}

func TestACorrectionWithinTheWiresPrecisionStillResetsTheFork(t *testing.T) {
	// 1.8.9 carries a position as fixed point in units of one thirty-second of a
	// block, so a server that agrees with the prediction still sends back a
	// slightly different number. A loop that treated the rounding as agreement
	// would keep predicting from its own value and drift; one that treated it as
	// a disagreement to fight would argue with the server forever. It resets, and
	// it counts.
	loop, client, observed := loopOn(t, predict.Options{})
	loop.Input(simmovement.Input{Forward: 1})

	if err := loop.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitFor(t, func() bool { return len(client.actions()) >= 4 })

	// The input stops first, so that the body is not walking away from the
	// position the server is about to send while the assertion reads it.
	loop.Input(simmovement.Input{})
	time.Sleep(10 * time.Millisecond)

	body, _ := loop.Predicted()
	at := feet(body)
	const unit = 1.0 / 32.0
	nearly := simgeom.Vec3{
		X: float64(int(at.X/unit)) * unit,
		Y: at.Y,
		Z: float64(int(at.Z/unit)) * unit,
	}

	collector := &event.Collector{}
	observed.Player().Move(collector, nearly.X, nearly.Y, nearly.Z, 0, 0, world.Relative{})
	client.setWorld(observed.Snapshot())

	waitFor(t, func() bool { return loop.Corrections() >= 1 })

	after, _ := loop.Predicted()
	got := feet(after)
	if got.Z > at.Z {
		t.Errorf("the fork is at %+v, past where it was at %+v: the correction was ignored",
			got, at)
	}
	if diff := got.Z - nearly.Z; diff < -0.01 || diff > 0.01 {
		t.Errorf("the fork is at %+v, want the server's %+v", got, nearly)
	}
}

func TestRetainedCommandsAreBounded(t *testing.T) {
	loop, client, _ := loopOn(t, predict.Options{Retained: 4})
	loop.Input(simmovement.Input{Forward: 1})

	if err := loop.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitFor(t, func() bool { return len(client.actions()) >= 20 })

	if got := loop.Retained(); got > 4 {
		t.Errorf("the loop retained %d commands, want at most 4", got)
	}
}

func TestATickOverUnstreamedTerrainDoesNotMoveTheBody(t *testing.T) {
	// A world with a player and no chunks: every cell the sweep reads is one
	// nobody has described, so the tick is incomplete and its work is dropped.
	// The client keeps reporting where it is, because a client that went silent
	// would be corrected for silence rather than for drift.
	set, err := gen.Data()
	if err != nil {
		t.Fatalf("load the 1.8.9 data set: %v", err)
	}
	profile, err := v1_8.New(set)
	if err != nil {
		t.Fatalf("build the 1.8.9 profile: %v", err)
	}

	observed := world.New()
	collector := &event.Collector{}
	observed.Player().Login(collector, 1, "overworld", 0)
	observed.Player().Move(collector, 0.5, 1, 0.5, 0, 0, world.Relative{})

	client := &actor{snapshot: observed.Snapshot()}
	names, _ := profile.(simulation.BlockNames)

	loop, err := predict.New(predict.Options{
		Actor:   client,
		Profile: profile,
		Blocks:  predict.MetadataBlocks(set, names),
		Spawn: func(pos simgeom.Vec3, yaw, pitch float32) (simentity.State, simmovement.Locomotion, bool) {
			return v1_8.Spawn(profile, pos, yaw, pitch)
		},
		Interval: time.Millisecond,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = loop.Close() })

	loop.Input(simmovement.Input{Forward: 1})
	if err := loop.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitFor(t, func() bool { return len(client.actions()) >= 5 })

	body, ok := loop.Predicted()
	if !ok {
		t.Fatal("the loop predicts nothing")
	}
	if got := feet(body); got != (simgeom.Vec3{X: 0.5, Y: 1, Z: 0.5}) {
		t.Errorf("an incomplete tick moved the body to %+v", got)
	}
}

func TestCloseStopsTheLoop(t *testing.T) {
	loop, client, _ := loopOn(t, predict.Options{})

	if err := loop.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitFor(t, func() bool { return len(client.actions()) >= 2 })

	if err := loop.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	sent := len(client.actions())
	time.Sleep(20 * time.Millisecond)
	if got := len(client.actions()); got != sent {
		t.Errorf("the loop sent %d more packets after Close", got-sent)
	}

	// Closing twice is not an error, and starting after a close is.
	if err := loop.Close(); err != nil {
		t.Errorf("closing twice returned %v", err)
	}
	if err := loop.Start(context.Background()); !errors.Is(err, predict.ErrLoop) {
		t.Errorf("starting a closed loop returned %v", err)
	}
}

// waitFor blocks until a condition holds or the test times out.
func waitFor(t *testing.T, condition func() bool) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(time.Millisecond)
	}

	t.Fatal("timed out waiting for the loop")
}

// feet returns where a body stands.
//
// A profile whose version keeps a position answers with it; 1.8.9 keeps none and
// derives it from the box, which is what the game does there too.
func feet(body simentity.State) simgeom.Vec3 {
	if body.Position != (simgeom.Vec3{}) {
		return body.Position
	}

	return simgeom.Vec3{
		X: (body.Box.MinX + body.Box.MaxX) / 2,
		Y: body.Box.MinY,
		Z: (body.Box.MinZ + body.Box.MaxZ) / 2,
	}
}
