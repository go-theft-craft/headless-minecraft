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

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	bot, err := connect(ctx, logger, address, username, legacy)
	if err != nil {
		logger.Error("connect", slog.Any("error", err))

		return 1
	}
	defer func() { _ = bot.Close() }()

	logger.Info("connected", slog.String("address", address))

	code, err := drive(ctx, logger, bot)
	if err != nil {
		logger.Error("run", slog.Any("error", err))
	}

	return code
}

// connect builds and connects the client. Authorization is declared for this
// endpoint and these scopes only: the library requires it before any automation
// and it is the application's statement of intent, not proof the server agrees.
func connect(
	ctx context.Context,
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

	bot, err := client.New(
		client.WithAddress(address),
		client.WithAuth(provider),
		client.WithVersion(profile(legacy)),
		client.WithAuthorization(authorization),
		client.WithLogger(logger),
	)
	if err != nil {
		return nil, fmt.Errorf("new client: %w", err)
	}

	if err := bot.Connect(ctx); err != nil {
		_ = bot.Close()

		return nil, fmt.Errorf("connect: %w", err)
	}

	return bot, nil
}

func profile(legacy bool) version.WireProfile {
	if legacy {
		return java.Java1_8()
	}

	return java.Current()
}

// drive runs the tick loop: fold events into a Tick, ask the core what to do,
// and do it. This is the only goroutine that touches the bot.
func drive(ctx context.Context, logger *slog.Logger, c *client.Client) (int, error) {
	bounds := DefaultBounds()
	core := NewBot(bounds)

	// M7 and M9 owe these. Until then both are Pending, which is what turns the
	// first action the core asks for into a clear error instead of silence.
	var (
		world    World    = Pending{}
		actuator Actuator = Pending{}
	)

	events, err := c.Subscribe(event.DomainSession|event.DomainPlayer|event.DomainEntities, 256)
	if err != nil {
		return 1, fmt.Errorf("subscribe: %w", err)
	}
	defer func() { _ = events.Close() }()

	ticker := time.NewTicker(bounds.Tick)
	defer ticker.Stop()

	var pending Tick
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
			action := core.Advance(pending, world)
			// Edge-triggered facts are consumed by the tick that saw them.
			// Leaving Died set would make the core respawn on every tick after
			// a death.
			pending.Attacker, pending.Died, pending.Respawned, pending.Corrected = 0, false, false, false

			code, done, err := apply(ctx, logger, actuator, core, action)
			if done {
				return code, err
			}
		}
	}
}

// fold turns one event into tick state. It is where M7's events will attach;
// today only the session domain has structs, so only Ready is real.
func fold(t *Tick, e event.Event) {
	switch e.(type) {
	case event.Ready:
		t.Ready = true
	default:
		// Every other fact the core needs — position, damage attribution,
		// death, respawn, corrections — is owed by M7. Its events do not exist
		// yet, so there is nothing to fold and nothing to pretend.
	}
	t.Revision = e.Revision()
}

// apply executes one action. It reports the exit status and whether the loop
// should end.
func apply(
	ctx context.Context,
	logger *slog.Logger,
	actuator Actuator,
	core *Bot,
	action Action,
) (int, bool, error) {
	var err error

	switch action.Kind {
	case Stand:
		return 0, false, nil
	case StepTo:
		err = actuator.Step(ctx, action.Target, action.Jump)
	case Strike:
		err = actuator.Attack(ctx, action.Entity)
	case SendRespawn:
		err = actuator.Respawn(ctx)
	case Exit:
		logger.Info(
			"stopping",
			slog.String("reason", action.Reason),
			slog.String("state", core.State().String()),
		)

		return action.Code, true, nil
	default:
		return 70, true, fmt.Errorf("unknown action %d", action.Kind)
	}

	if err == nil {
		return 0, false, nil
	}

	// A pending port is the expected outcome today, and it is not a crash. Say
	// which milestone stopped the run and exit with a status that CI can read.
	if errors.Is(err, ErrNotYet) {
		logger.Error(
			"the core asked for something the library cannot do yet",
			slog.String("state", core.State().String()),
			slog.Any("error", err),
		)

		return 3, true, nil
	}

	return 1, true, err
}
