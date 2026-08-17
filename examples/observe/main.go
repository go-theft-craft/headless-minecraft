package main

import (
	"context"
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
	"github.com/go-theft-craft/headless-minecraft/version/java"
	"github.com/go-theft-craft/headless-minecraft/world"
)

func main() {
	address := flag.String("address", "localhost:25565", "server host:port")
	username := flag.String("username", "observer", "offline-mode username")
	legacy := flag.Bool("legacy", false, "use the protocol 47 profile instead of the current one")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	if code := run(logger, *address, *username, *legacy); code != 0 {
		os.Exit(code)
	}
}

// eventBuffer is the subscription's capacity. The client closes a subscriber
// that falls behind rather than dropping an event, so this is sized for the
// burst that arrives with the first chunks rather than for the steady state.
const eventBuffer = 1024

// stateDomains is every domain the observed world fills. Session events are
// deliberately absent: this example is about state, and the connection's own
// lifecycle is what examples/connect shows.
const stateDomains = event.DomainPlayer | event.DomainWorld | event.DomainEntities |
	event.DomainContainers | event.DomainRegistry | event.DomainChat

// run returns the process exit status. main does nothing else, so every exit
// path here is one a test can call.
func run(logger *slog.Logger, address, username string, legacy bool) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// The world is the point of this example. Without one the client publishes
	// events and keeps no state, and every revision on every line would be
	// zero.
	observed := world.New()

	bot, err := build(address, username, legacy, observed)
	if err != nil {
		logger.Error("build", slog.Any("error", err))

		return 1
	}
	defer func() { _ = bot.Close() }()

	// Subscribe before connecting. Connect publishes its way through play and
	// returns after it, so a subscription opened afterwards has already missed
	// the login, the placement, and the first chunks.
	events, err := bot.Subscribe(stateDomains, eventBuffer)
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

	report(ctx, events)

	// The snapshot is read after the stream ends, so it is the last state the
	// connection reached rather than one taken mid-flight.
	// Ignoring the write error is deliberate: a closed stdout is the pipe the
	// caller closed, and there is nowhere left to report it to.
	_, _ = fmt.Fprintln(os.Stdout, summarize(observed.Snapshot()))

	return 0
}

// build constructs the client without touching the network.
//
// Authorization is declared for this endpoint and the observe scope only. This
// example sends nothing but the keepalive answers the client owes, so anything
// wider would be a claim it does not need.
func build(address, username string, legacy bool, observed *world.World) (*client.Client, error) {
	provider, err := auth.Offline(username)
	if err != nil {
		return nil, fmt.Errorf("offline identity: %w", err)
	}

	authorization, err := safety.Authorize(address, safety.ScopeObserve)
	if err != nil {
		return nil, fmt.Errorf("authorize: %w", err)
	}

	profile := java.Current()
	if legacy {
		profile = java.Java1_8()
	}

	bot, err := client.New(
		client.WithAddress(address),
		client.WithAuth(provider),
		client.WithVersion(profile),
		client.WithAuthorization(authorization),
		client.WithConnectTimeout(30*time.Second),
		client.WithWorld(observed),
	)
	if err != nil {
		return nil, fmt.Errorf("client: %w", err)
	}

	return bot, nil
}

// report prints one line per event until the subscription closes or the
// context ends.
//
// Every line from one batch carries the same revision, which is what makes a
// protocol 775 bundle visible: several lines, one number.
func report(ctx context.Context, events *client.Subscription) {
	for {
		select {
		case <-ctx.Done():
			return

		case e, open := <-events.C():
			if !open {
				return
			}
			_, _ = fmt.Fprintln(os.Stdout, line(e))
		}
	}
}

// line renders one event, with its description only when it has one.
func line(e event.Event) string {
	description := describe(e)
	if description == "" {
		return fmt.Sprintf("%6d  %s", e.Revision(), e.Name())
	}

	return fmt.Sprintf("%6d  %-28s %s", e.Revision(), e.Name(), description)
}
