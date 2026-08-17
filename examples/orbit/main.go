package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-theft-craft/headless-minecraft/auth"
	"github.com/go-theft-craft/headless-minecraft/client"
	"github.com/go-theft-craft/headless-minecraft/event"
	"github.com/go-theft-craft/headless-minecraft/safety"
	"github.com/go-theft-craft/headless-minecraft/version"
	"github.com/go-theft-craft/headless-minecraft/version/java"
	"github.com/go-theft-craft/headless-minecraft/world"
)

func main() {
	address := flag.String("address", "localhost:25565", "server host:port")
	username := flag.String("username", "orbit", "offline-mode username")
	legacy := flag.Bool("legacy", false, "use the protocol 47 profile instead of the current one")
	dryRun := flag.Bool("dry-run", false, "report what the example needs and exit without connecting")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	if code := run(logger, *address, *username, *legacy, *dryRun); code != 0 {
		os.Exit(code)
	}
}

// run returns the process exit status. main does nothing else, so every exit
// path here is one a test can call.
func run(logger *slog.Logger, address, username string, legacy, dryRun bool) int {
	// Say what is missing before doing anything. A user who runs this against a
	// server deserves to know it will not orbit before it connects, not after.
	for _, missing := range Missing() {
		logger.Warn("not implemented yet", slog.String("owes", missing))
	}
	if dryRun {
		return 0
	}

	// Before the network, because a bot that cannot classify a block cannot
	// walk, and that is worth knowing at startup rather than after ninety
	// seconds of standing still. Only the version someone has measured has the
	// answer; the other connects, observes, and refuses every step.
	solidity, err := NewSolidity(legacy)
	if err != nil {
		logger.Error("solidity", slog.Any("error", err))

		return 1
	}
	if !solidity.Measured() {
		logger.Warn(
			"no block measurement for this version: the bot will see the world, "+
				"classify nothing in it, and refuse to move",
			slog.Bool("legacy", legacy),
		)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	bot, err := build(logger, address, username, legacy)
	if err != nil {
		logger.Error("build", slog.Any("error", err))

		return 1
	}
	defer func() { _ = bot.Close() }()

	// Subscribe before connecting. Connect publishes session.ready on its way
	// through play and returns after it, so a subscription opened afterwards
	// has already missed it — which is exactly what the first live run of this
	// example did, and it looks like a bot that never joined.
	// Two domains, not five. The subscription carries only what happened
	// between snapshots — readiness, damage, death, respawn, and the server
	// moving the player — and everything else the bot needs is read from the
	// snapshot. Entity and world domains would deliver the first chunk burst to
	// a subscriber that ignores it, and the client closes a subscriber that
	// falls behind rather than dropping an event.
	events, err := bot.Subscribe(event.DomainSession|event.DomainPlayer, eventBuffer)
	if err != nil {
		logger.Error("subscribe", slog.Any("error", err))

		return 1
	}
	defer func() { _ = events.Close() }()

	if err := bot.Connect(ctx); err != nil {
		logger.Error("connect", slog.Any("error", err))

		return 1
	}

	logger.Info("connected", slog.String("address", address))

	code, err := drive(ctx, logger, bot, events, solidity)
	if err != nil {
		logger.Error("run", slog.Any("error", err))
	}

	return code
}

// eventBuffer is the subscription's capacity. The client closes a subscriber
// that falls behind rather than dropping an event, so this is sized for the
// burst that arrives with the first chunks, not for the steady state.
const eventBuffer = 256

// build constructs the client without touching the network. Authorization is
// declared for this endpoint and these scopes only: the library requires it
// before any automation and it is the application's statement of intent, not
// proof the server agrees.
func build(
	logger *slog.Logger,
	address, username string,
	legacy bool,
) (*client.Client, error) {
	provider, err := auth.Offline(username)
	if err != nil {
		return nil, fmt.Errorf("offline identity: %w", err)
	}

	authorization, err := safety.Authorize(
		address,
		safety.ScopeObserve,
		safety.ScopeMove,
		safety.ScopeInteract,
	)
	if err != nil {
		return nil, fmt.Errorf("authorize: %w", err)
	}

	// A client with no world publishes events and keeps no state, which is what
	// a consumer that only watches traffic wants and is useless to one that
	// reads a snapshot every tick. Leaving this out is silent: World() keeps
	// answering, with an empty snapshot, so the bot waits for a spawn the server
	// already sent and then reports that the server never sent one.
	bot, err := client.New(
		client.WithAddress(address),
		client.WithAuth(provider),
		client.WithVersion(profile(legacy)),
		client.WithAuthorization(authorization),
		client.WithWorld(world.New()),
		client.WithLogger(logger),
	)
	if err != nil {
		return nil, fmt.Errorf("new client: %w", err)
	}

	return bot, nil
}

func profile(legacy bool) version.WireProfile {
	if legacy {
		return java.Java1_8()
	}

	return java.Current()
}

// connection is what the tick loop needs of a client: one snapshot to read and
// one way to act. It is an interface so the loop can be tested without a
// connection; *client.Client satisfies it.
type connection interface {
	World() world.Snapshot
	// version.Action, not this package's Action: the example's Action is one
	// decision by the core, and the library's is one intent on the wire. They
	// are different things that meet in Sender.
	Do(ctx context.Context, action version.Action) error
}

// drive runs the tick loop: fold events into a Tick, ask the core what to do,
// and do it. This is the only goroutine that touches the bot.
func drive(
	ctx context.Context,
	logger *slog.Logger,
	source connection,
	events *client.Subscription,
	solidity Solidity,
) (int, error) {
	bounds := DefaultBounds()
	core := NewBot(bounds)

	// Movement is real; attack and respawn are still Pending on Sender, which
	// is what turns the first thing the core asks for that M9 owes into a clear
	// error instead of silence.
	var actuator Actuator = NewSender(source, bounds)

	ticker := time.NewTicker(bounds.Tick)
	defer ticker.Stop()

	var (
		// Connect returned, so play was reached. That is the fact, and the
		// event only confirms it: seeding this here means the bot does not
		// depend on having caught a message that was published before anyone
		// could be listening.
		pending = Tick{Ready: true}
		last    narration
		// Where the bot believes it is. Observed state cannot answer this
		// while the bot is walking: it holds what the server sent, and a
		// server sends a position to place or to correct, never to
		// acknowledge a move it accepted. Reading position back from the
		// snapshot every tick means stepping from the same coordinate
		// forever — one step, then silence, which is what the first live run
		// with movement did for ninety seconds.
		//
		// So the bot carries its own, seeded from the server's placement and
		// reset whenever the server disagrees. That is dead reckoning, and it
		// is the crude half of client-side prediction; the other half is a
		// body that knows about gravity and collision, which is M8's.
		predicted Vec3
		placed    bool
	)
	for {
		select {
		case <-ctx.Done():
			logger.Info("interrupted")

			return 0, nil

		case e, open := <-events.C():
			if !open {
				if err := events.Err(); err != nil {
					return 1, fmt.Errorf("subscription ended: %w", err)
				}

				return 0, nil
			}
			fold(&pending, e)

		case now := <-ticker.C:
			pending.Now = now

			// One snapshot for this whole tick. Taking a second one for the
			// player would let the terrain move between the bot's feet and the
			// position it decided to step to.
			snapshot := source.World()
			if self, known := observeSelf(snapshot); known {
				// The server's word wins on placement and on correction, and
				// only then. pending.Corrected is read here rather than after
				// Advance, which is where it is cleared.
				if !placed || pending.Corrected {
					predicted, placed = self.Position, true
				}
				self.Position = predicted
				pending.Self = self
			}
			pending.Revision = snapshot.Revision

			action := core.Advance(pending, NewObserved(snapshot, solidity))
			narrate(logger, core, action, &last)
			// Edge-triggered facts are consumed by the tick that saw them.
			// Leaving Died set would make the core respawn on every tick after
			// a death.
			pending.Attacker, pending.Died, pending.Respawned, pending.Corrected = 0, false, false, false

			moved, code, done, err := apply(ctx, logger, actuator, core, predicted, action)
			if done {
				return code, err
			}
			predicted = moved
		}
	}
}

// fold turns one event into tick state.
//
// Only the edge-triggered facts come from events. Position and health are read
// from the snapshot instead, because an event says a thing changed and the
// snapshot says what it is now, and a loop that rebuilt state from a stream of
// changes would be keeping a second copy of the world the library already
// keeps. What cannot be read from a snapshot is what happened between two of
// them, and that is exactly this list.
func fold(t *Tick, e event.Event) {
	switch value := e.(type) {
	case event.Ready:
		t.Ready = true

	case event.PlayerDamaged:
		// Only an attributed source is a target. Protocol 47 sends no damage
		// packet at all and reports being hurt as an entity status with nothing
		// behind it, so on 47 this is always unattributed and the bot keeps
		// orbiting rather than swinging at a guess. Inferring an attacker from
		// who is nearest is the kind of guess the library refuses to make, and
		// it would be no more honest here.
		if value.Damage.Attributed {
			t.Attacker = value.Damage.CauseID
		}

	case event.PlayerDied:
		t.Died = true

	case event.PlayerRespawned:
		t.Respawned = true

	case event.PlayerMoved:
		// The server placing the player is a correction, because nothing else
		// in this program moves it: until M8.8 the bot sends no movement, so
		// every position in the player domain arrived from the server. Once it
		// does send movement, a position that agrees with what was sent is not
		// a correction and this has to compare rather than assume.
		t.Corrected = true
	}
}

// narration is the last thing reported, so the log carries changes rather than
// twenty identical lines a second.
type narration struct {
	state  State
	reason string
}

// narrate logs what the bot is doing when it changes.
//
// A bot standing still has to say why. The first live run of this example
// connected, reached play, and then printed nothing for twenty-five seconds
// while it waited for a world that cannot answer yet — which reads exactly like
// a working bot, and was not one.
func narrate(logger *slog.Logger, core *Bot, action Action, last *narration) {
	current := narration{state: core.State(), reason: action.Reason}
	if current == *last {
		return
	}
	*last = current

	if action.Reason == "" {
		logger.Info("state", slog.String("state", current.state.String()))

		return
	}

	logger.Info(action.Reason, slog.String("state", current.state.String()))
}

// apply executes one action. It reports the exit status and whether the loop
// should end.
func apply(
	ctx context.Context,
	logger *slog.Logger,
	actuator Actuator,
	core *Bot,
	from Vec3,
	action Action,
) (Vec3, int, bool, error) {
	var err error

	// Where the bot is after this action. Only a step moves it, and a step
	// that failed to send does not.
	moved := from

	switch action.Kind {
	case Stand:
		return moved, 0, false, nil
	case StepTo:
		moved, err = actuator.Step(ctx, from, action.Target, action.Jump)
	case Strike:
		err = actuator.Attack(ctx, action.Entity)
	case SendRespawn:
		err = actuator.Respawn(ctx)
	case Exit:
		// narrate already printed the reason; repeating it here would put the
		// same sentence on two consecutive lines.
		return moved, action.Code, true, nil
	default:
		return moved, 70, true, fmt.Errorf("unknown action %d", action.Kind)
	}

	if err == nil {
		return moved, 0, false, nil
	}

	// A pending port is the expected outcome today, and it is not a crash. Say
	// which milestone stopped the run and exit with a status that CI can read.
	if errors.Is(err, ErrNotYet) {
		logger.Error(
			"the core asked for something the library cannot do yet",
			slog.String("state", core.State().String()),
			slog.Any("error", err),
		)

		return moved, 3, true, nil
	}

	return moved, 1, true, err
}
