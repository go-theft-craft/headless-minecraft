package predict

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/go-theft-craft/minecraft-simulation/adapter"
	simentity "github.com/go-theft-craft/minecraft-simulation/entity"
	simgeom "github.com/go-theft-craft/minecraft-simulation/geom"
	simmovement "github.com/go-theft-craft/minecraft-simulation/movement"
	simulation "github.com/go-theft-craft/minecraft-simulation/sim"
	simworld "github.com/go-theft-craft/minecraft-simulation/world"

	"github.com/go-theft-craft/headless-minecraft/version"
	"github.com/go-theft-craft/headless-minecraft/world"
)

// ErrLoop reports a loop that cannot be built or started as asked.
var ErrLoop = errors.New("predict: invalid loop")

// The cadence rule, from version.Action's documentation. It was read off a real
// client's own traffic rather than inferred, and these are the two numbers it
// turns on.
const (
	// movedThreshold is the squared distance from the last reported position
	// above which a tick reports a position.
	movedThreshold = 9.0e-4
	// forcedInterval is how many packets may pass without a position before one
	// is sent anyway. The counter resets on any packet that carried a position,
	// so the forced update lands on the twenty-first after it.
	forcedInterval = 20
)

// defaultRetained is how many commands the loop keeps for replay after a
// correction. It is a bound rather than a guess: retention exists to replay the
// handful of ticks that were in flight when a correction arrived, and an
// unbounded queue is a leak that appears exactly when a server stops answering.
const defaultRetained = 64

// Actor is the half of a client this loop sends through.
//
// It is an interface so that the loop can be tested without a connection: a
// prediction that only ran against a real server would have its reconciliation
// tested by nothing.
type Actor interface {
	// Do sends one outbound intent.
	Do(ctx context.Context, action version.Action) error
	// World returns the observed state the server has described.
	World() world.Snapshot
}

// Correction is a position the server sent after the client began reporting its
// own.
//
// From is where the client believed it was, To is where the server put it, and
// Tick is the loop's own tick at which the two were reconciled. A caller that
// sees these accumulate is watching a prediction that does not match the server.
type Correction struct {
	Tick simulation.Tick
	From simgeom.Vec3
	To   simgeom.Vec3
}

// Options configures a loop.
type Options struct {
	// Actor is the client to predict for. Required.
	Actor Actor
	// Profile is the rules to simulate with. Required, and it must be the rules
	// of the version the client connected with: predicting one version's physics
	// against another's server is the one mistake this package cannot detect.
	Profile simulation.Profile
	// Blocks resolves observed block states for that version. Required.
	Blocks Blocks
	// Spawn builds the body a position starts in. It is the profile's own
	// Spawn function, passed in because this package must not know which
	// version's package to import.
	Spawn func(pos simgeom.Vec3, yaw, pitch float32) (simentity.State, simmovement.Locomotion, bool)
	// Interval is how long a tick lasts. The zero value is the game's own fifty
	// milliseconds.
	Interval time.Duration
	// OnCorrection is called when the server's position replaces the predicted
	// one. It runs on the loop's goroutine, so it should not block.
	OnCorrection func(Correction)
	// Retained bounds the commands kept for replay. The zero value is the
	// default; a negative value keeps none.
	Retained int
}

// Loop predicts the local player's movement, tick by tick.
//
// One goroutine owns the prediction: Start runs it, Input hands it the caller's
// intent for the next tick, and Close stops it. Everything the loop reads about
// the server arrives through the actor's snapshot, so a correction is observed
// rather than pushed.
type Loop struct {
	options Options
	kernel  simulation.Kernel

	mu       sync.Mutex
	input    simmovement.Input
	body     simentity.State
	loco     simmovement.Locomotion
	started  bool
	closed   bool
	tick     simulation.Tick
	reported reported
	retained []retainedCommand
	// corrections counts what the server disagreed about after the boundary. It
	// is the milestone's own exit criterion, so it is a number this package
	// keeps rather than one a test derives.
	corrections int
	// acknowledged is set once the client has reported a position of its own.
	// Until then a server position is the login sequence placing the player, not
	// a correction, and counting it would make the gate impossible to pass.
	acknowledged bool
	// seen is the last position the server reported, so that a repeat of the
	// same position is not counted twice.
	seen       simgeom.Vec3
	seenPlaced bool

	// err is why the loop stopped, when it stopped on its own.
	err error

	done chan struct{}
	stop context.CancelFunc
}

// reported is what the loop last told the server, which is what the cadence rule
// compares against.
type reported struct {
	pos        simgeom.Vec3
	yaw, pitch float32
	// since counts packets since one carried a position.
	since int
	// any reports that a position has ever been sent.
	any bool
}

// retainedCommand is one tick's intent, kept for replay after a correction.
type retainedCommand struct {
	tick  simulation.Tick
	input simmovement.Input
}

// entity is the body this loop simulates. A client predicts for itself and for
// nothing else.
const entityID = simentity.ID(1)

// New builds a loop. It does not connect to anything and it does not tick until
// Start is called.
func New(options Options) (*Loop, error) {
	switch {
	case options.Actor == nil:
		return nil, fmt.Errorf("%w: no client to act through", ErrLoop)
	case options.Profile == nil:
		return nil, fmt.Errorf("%w: no profile", ErrLoop)
	case options.Blocks == nil:
		return nil, fmt.Errorf("%w: no block resolver", ErrLoop)
	case options.Spawn == nil:
		return nil, fmt.Errorf("%w: no spawn function", ErrLoop)
	}

	kernel, err := simulation.NewKernel(options.Profile)
	if err != nil {
		return nil, fmt.Errorf("predict: build a kernel: %w", err)
	}
	if options.Interval <= 0 {
		options.Interval = 50 * time.Millisecond
	}
	if options.Retained == 0 {
		options.Retained = defaultRetained
	}

	return &Loop{options: options, kernel: kernel, done: make(chan struct{})}, nil
}

// Start begins ticking until the context is cancelled or Close is called.
//
// It returns as soon as the loop is running. The first tick waits for the server
// to place the player: a prediction before that has no position to start from.
func (l *Loop) Start(ctx context.Context) error {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()

		return fmt.Errorf("%w: already closed", ErrLoop)
	}
	if l.started {
		l.mu.Unlock()

		return fmt.Errorf("%w: already started", ErrLoop)
	}
	l.started = true
	ctx, l.stop = context.WithCancel(ctx)
	l.mu.Unlock()

	go l.run(ctx)

	return nil
}

// Close stops the loop and waits for its goroutine to finish.
func (l *Loop) Close() error {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()

		return nil
	}
	l.closed = true
	stop := l.stop
	started := l.started
	l.mu.Unlock()

	if stop != nil {
		stop()
	}
	if started {
		<-l.done
	}

	return nil
}

// Input records the caller's intent for the next tick.
//
// The last intent before a tick wins, which is the same rule the kernel applies
// to two commands in one tick. A caller that sets nothing keeps sending the
// previous intent, because a held key is held until it is released.
func (l *Loop) Input(input simmovement.Input) {
	l.mu.Lock()
	defer l.mu.Unlock()

	input.Entity = entityID
	l.input = input
}

// Predicted returns the body the loop currently believes in, and false before
// the first tick.
func (l *Loop) Predicted() (simentity.State, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()

	return l.body, l.body.Family != simentity.FamilyUnknown
}

// Corrections returns how many times the server has replaced the predicted
// position since the client began reporting one.
func (l *Loop) Corrections() int {
	l.mu.Lock()
	defer l.mu.Unlock()

	return l.corrections
}

// Err returns why the loop stopped, or nil while it is running or after a clean
// close.
//
// A prediction that stopped silently is the worst failure this package has: the
// client keeps its last position, the server keeps moving the world, and nothing
// says why. So the reason is kept.
func (l *Loop) Err() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	return l.err
}

// Retained returns how many commands are being kept for replay. It is here so
// that the bound can be asserted rather than reasoned about.
func (l *Loop) Retained() int {
	l.mu.Lock()
	defer l.mu.Unlock()

	return len(l.retained)
}

// run is the loop's own goroutine.
func (l *Loop) run(ctx context.Context) {
	defer close(l.done)

	ticker := time.NewTicker(l.options.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := l.step(ctx); err != nil {
				// A failed send or a failed tick ends the loop rather than
				// looping on it: a prediction the server never heard about is a
				// prediction that will be corrected, and repeating the failure
				// twenty times a second reports the same fact twenty times. Err
				// is how a caller finds out.
				l.mu.Lock()
				l.err = err
				l.mu.Unlock()

				return
			}
		}
	}
}

// step runs one tick: reconcile, simulate, report.
func (l *Loop) step(ctx context.Context) error {
	snapshot := l.options.Actor.World()
	if !snapshot.Player.Known || !snapshot.Player.Placed {
		return nil
	}

	l.mu.Lock()
	if l.body.Family == simentity.FamilyUnknown {
		if !l.adopt(snapshot.Player) {
			l.mu.Unlock()

			return nil
		}
	}
	l.reconcile(snapshot.Player)

	l.tick++
	input := l.input
	input.Entity = entityID
	l.retain(retainedCommand{tick: l.tick, input: input})
	tick := l.tick
	l.mu.Unlock()

	result, err := l.simulate(ctx, snapshot, tick, input)
	if err != nil {
		return err
	}
	if !result.Completeness.Complete {
		// The tick read terrain the server has not streamed. The body is left
		// where it was, and the client keeps telling the server the truth about
		// where that is: a client that stopped reporting would be corrected for
		// silence rather than for drift.
		return l.report(ctx, false)
	}

	return l.report(ctx, result.Domain != nil && collided(result.Domain))
}

// adopt takes the server's position as the starting state. It runs under the
// lock.
func (l *Loop) adopt(player world.PlayerView) bool {
	state, loco, ok := l.options.Spawn(
		simgeom.Vec3{X: player.X, Y: player.Y, Z: player.Z}, player.Yaw, player.Pitch,
	)
	if !ok {
		return false
	}

	l.body = state
	l.loco = loco
	l.seen = simgeom.Vec3{X: player.X, Y: player.Y, Z: player.Z}
	l.seenPlaced = true

	return true
}

// reconcile replaces the prediction when the server has moved the player since
// the last tick. It runs under the lock.
//
// A correction is a server position that arrives after the client has begun
// reporting its own. The login sequence's position is not one, which is why the
// acknowledgement boundary is explicit state rather than a timeout: a timeout
// would make this flaky on a slow machine, and a flaky gate gets deleted.
func (l *Loop) reconcile(player world.PlayerView) {
	at := simgeom.Vec3{X: player.X, Y: player.Y, Z: player.Z}
	if l.seenPlaced && at == l.seen {
		return
	}

	from := simgeom.Vec3{X: l.body.Position.X, Y: l.body.Position.Y, Z: l.body.Position.Z}
	if from == (simgeom.Vec3{}) {
		// A profile whose version keeps no position: the box is the body, and
		// its feet are where the server put it.
		from = simgeom.Vec3{
			X: (l.body.Box.MinX + l.body.Box.MaxX) / 2,
			Y: l.body.Box.MinY,
			Z: (l.body.Box.MinZ + l.body.Box.MaxZ) / 2,
		}
	}

	l.seen = at
	l.seenPlaced = true

	state, loco, ok := l.options.Spawn(at, player.Yaw, player.Pitch)
	if !ok {
		return
	}
	// The locomotion the server does not carry stays ours: it knows where the
	// player is, not which keys are held.
	loco.Sprinting = l.loco.Sprinting
	loco.Sneaking = l.loco.Sneaking
	loco.Jumping = l.loco.Jumping
	loco.JumpTicks = l.loco.JumpTicks

	l.body = state
	l.loco = loco
	// Everything retained is superseded. This protocol carries no
	// acknowledgement number, so there is nothing to match a retained command
	// against: what the server has corrected, it has corrected in full, and what
	// has not happened yet has not been sent.
	l.retained = l.retained[:0]

	if !l.acknowledged {
		// The login sequence, not a disagreement.
		return
	}
	l.corrections++
	if l.options.OnCorrection != nil {
		l.options.OnCorrection(Correction{Tick: l.tick, From: from, To: at})
	}
}

// retain keeps a command for replay, dropping the oldest when the bound is
// reached. It runs under the lock.
func (l *Loop) retain(command retainedCommand) {
	if l.options.Retained < 0 {
		return
	}
	if len(l.retained) == l.options.Retained {
		copy(l.retained, l.retained[1:])
		l.retained = l.retained[:len(l.retained)-1]
	}
	l.retained = append(l.retained, command)
}

// simulate runs one tick against a store built from the observed world.
func (l *Loop) simulate(
	ctx context.Context, snapshot world.Snapshot, tick simulation.Tick, input simmovement.Input,
) (simulation.TickResult, error) {
	l.mu.Lock()
	store := &fork{
		revision: simulation.Revision(snapshot.Revision),
		blocks:   NewTerrain(snapshot.Chunks, l.options.Blocks, l.options.Profile),
		bodies:   simentity.NewBodies(),
		moving:   simmovement.NewBodies(),
	}
	store.bodies.Set(entityID, l.body)
	store.moving.Set(entityID, l.loco)
	l.mu.Unlock()

	source := &tickSource{tick: tick, commands: []simulation.Command{input}}

	result, err := adapter.Drive(ctx, l.kernel, store, source, store)
	if err != nil {
		return result, fmt.Errorf("predict: %w", err)
	}

	if result.Completeness.Complete {
		l.mu.Lock()
		if body, ok := store.bodies.Entity(entityID); ok {
			l.body = body
		}
		if loco, ok := store.moving.Locomotion(entityID); ok {
			l.loco = loco
		}
		l.mu.Unlock()
	}

	return result, nil
}

// report tells the server what the tick decided, choosing the packet the cadence
// rule calls for.
func (l *Loop) report(ctx context.Context, horizontal bool) error {
	l.mu.Lock()
	body := l.body
	loco := l.loco
	state := l.reported

	pos := body.Position
	if pos == (simgeom.Vec3{}) {
		pos = simgeom.Vec3{
			X: (body.Box.MinX + body.Box.MaxX) / 2,
			Y: body.Box.MinY,
			Z: (body.Box.MinZ + body.Box.MaxZ) / 2,
		}
	}

	moved := !state.any || squaredDistance(pos, state.pos) > movedThreshold ||
		state.since >= forcedInterval
	rotated := state.any && (loco.Yaw != state.yaw || loco.Pitch != state.pitch)
	if !state.any {
		rotated = true
	}

	action := actionFor(moved, rotated, pos, loco, body.OnGround, horizontal)

	if moved {
		l.reported = reported{pos: pos, yaw: loco.Yaw, pitch: loco.Pitch, since: 0, any: true}
		// The boundary: from here on, a position from the server is a
		// disagreement rather than the login sequence placing the player.
		//
		// What is deliberately not updated here is the last position the server
		// was seen at. That is the server's own number, and overwriting it with
		// a predicted one would make the next tick read its own prediction as a
		// correction and reset to it — a loop that corrected itself every tick
		// and never moved.
		l.acknowledged = true
	} else {
		l.reported.since++
		if rotated {
			l.reported.yaw = loco.Yaw
			l.reported.pitch = loco.Pitch
		}
	}
	l.mu.Unlock()

	if err := l.options.Actor.Do(ctx, action); err != nil {
		return fmt.Errorf("predict: send %s: %w", action.ActionKind(), err)
	}

	return nil
}

// actionFor is the cadence rule itself, and it is the whole of what this loop
// decides about the wire.
//
// A server reads the choice as information: a position for a tick where nothing
// moved, or a bare ground flag for a tick where the player walked, is a
// disagreement it may correct. So this is behaviour the gate measures, not
// formatting.
func actionFor(
	moved, rotated bool,
	pos simgeom.Vec3,
	loco simmovement.Locomotion,
	onGround, horizontal bool,
) version.Action {
	switch {
	case moved && rotated:
		return version.ActionMoveLook{
			X: pos.X, Y: pos.Y, Z: pos.Z,
			Yaw: loco.Yaw, Pitch: loco.Pitch,
			OnGround: onGround, HorizontalCollision: horizontal,
		}
	case moved:
		return version.ActionMove{
			X: pos.X, Y: pos.Y, Z: pos.Z,
			OnGround: onGround, HorizontalCollision: horizontal,
		}
	case rotated:
		return version.ActionLook{
			Yaw: loco.Yaw, Pitch: loco.Pitch,
			OnGround: onGround, HorizontalCollision: horizontal,
		}
	default:
		return version.ActionGround{OnGround: onGround, HorizontalCollision: horizontal}
	}
}

// squaredDistance is the comparison the cadence rule makes. It is squared
// because the rule is: the game never takes the root.
func squaredDistance(a, b simgeom.Vec3) float64 {
	dx, dy, dz := a.X-b.X, a.Y-b.Y, a.Z-b.Z

	return dx*dx + dy*dy + dz*dz
}

// collided reports whether the tick's events say a horizontal axis was blocked.
func collided(events []simulation.DomainEvent) bool {
	for _, event := range events {
		if event.Kind == "movement.collided" {
			return true
		}
	}

	return false
}

// fork is the store a prediction is applied to.
//
// Its blocks are the server's and are read-only: a client does not predict block
// changes in this milestone, and a change set that tried to write one is a bug
// worth an error rather than a silent local edit that the next chunk update
// would undo.
type fork struct {
	revision simulation.Revision
	blocks   *Terrain
	bodies   *simentity.Bodies
	moving   *simmovement.Bodies
}

func (f *fork) Revision() simulation.Revision { return f.revision }

func (f *fork) Blocks() simworld.View { return f.blocks }

func (f *fork) Entities() simentity.View { return f.bodies }

func (f *fork) Locomotion() simmovement.LocomotionView { return f.moving }

// Apply writes the tick's own result into the fork.
func (f *fork) Apply(changes simulation.ChangeSet) error {
	if changes.BaseRevision != f.revision {
		return fmt.Errorf("%w: set is based at %d, fork is at %d",
			ErrLoop, changes.BaseRevision, f.revision)
	}

	for _, op := range changes.Ops {
		switch op.Kind {
		case simulation.OpSetEntity:
			f.bodies.Set(op.Entity, op.State)
		case simulation.OpRemoveEntity:
			f.bodies.Remove(op.Entity)
			f.moving.Remove(op.Entity)
		case simulation.OpSetLocomotion:
			f.moving.Set(op.Entity, op.Locomotion)
		case simulation.OpSetBlock:
			return fmt.Errorf("%w: a prediction wrote a block, which the server owns", ErrLoop)
		default:
			return fmt.Errorf("%w: unknown operation %s", ErrLoop, op.Kind)
		}
	}
	f.revision++

	return nil
}

// Observe is the sink's other half. A prediction has nothing to log.
func (f *fork) Observe(simulation.TickResult) {}

// tickSource is one tick's contribution.
type tickSource struct {
	tick     simulation.Tick
	commands []simulation.Command
}

func (s *tickSource) Tick() simulation.Tick          { return s.tick }
func (s *tickSource) Commands() []simulation.Command { return s.commands }
func (s *tickSource) Limits() simulation.Limits      { return simulation.Limits{} }
func (s *tickSource) Scope() simulation.Scope {
	return simulation.Scope{Entities: []simentity.ID{entityID}}
}
