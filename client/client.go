package client

import (
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	protocol "github.com/go-theft-craft/minecraft-protocol"

	"github.com/go-theft-craft/headless-minecraft/auth"
	"github.com/go-theft-craft/headless-minecraft/event"
	"github.com/go-theft-craft/headless-minecraft/safety"
	"github.com/go-theft-craft/headless-minecraft/version"
	"github.com/go-theft-craft/headless-minecraft/world"
)

const (
	defaultConnectTimeout = 30 * time.Second
	defaultBundleLimit    = 4096
	// defaultObservationGrace is how long after the server places the player
	// the client waits for terrain before saying it never arrived. It is long
	// enough that a loaded server streaming a large view distance finishes
	// first, and short enough that a person does not have to wonder.
	defaultObservationGrace = 10 * time.Second
)

// ErrInvalidClient reports a configuration rejected before any network work.
var ErrInvalidClient = errors.New("invalid client configuration")

// Option configures a client at construction.
type Option func(*Client) error

// Client is one connection's owner. It is safe for concurrent use by the
// methods it exposes; its internals are owned by the loop goroutine.
type Client struct {
	address        string
	provider       auth.Provider
	profile        version.WireProfile
	authorization  safety.Authorization
	recovery       safety.Profile
	logger         *slog.Logger
	connectTimeout time.Duration
	bundleLimit    int
	// observationGrace bounds how long a placed session may observe no
	// terrain before the client reports it. See terrainWatch.
	observationGrace time.Duration

	events fanout
	world  *world.World

	// writeMu serializes outbound writes. The read loop's replies and every Do
	// take it, so an action never interleaves with a keepalive answer.
	writeMu sync.Mutex

	mu         sync.Mutex
	closed     bool
	closeErr   error
	connecting bool
	stream     *protocol.Stream
	// writer is the outbound half of the connection, recorded so Do can write
	// without reaching through the stream the loop owns.
	writer sender
	// inPlay records that the server placed the player. Before that, an action
	// packet is a protocol error rather than a move.
	inPlay bool
	// reportedEnd records that a disconnect has already been published for this
	// session, so that a connection ending after the server said why does not
	// report the ending twice.
	reportedEnd bool
	// lastState is the state the last packet the loop processed arrived in. A
	// terminated stream answers nothing about itself, so a disconnect read
	// from one names no state at all — and the state a session ended in is
	// most of what a subscriber wants from a connection that died. The loop
	// records it as it goes, because it is the one place that knows without
	// asking anything that can be gone by then.
	lastState string
	loopError error
	stop      func()
	// loop closes when the read loop stops; done closes when Close finishes.
	// They are separate because a session can end without anyone calling
	// Close, and Close must still be able to run afterwards.
	loop chan struct{}
	done chan struct{}
}

// WithAddress sets the server endpoint. There is no default: a client that
// dialled something by default would be a client that dialled by accident.
func WithAddress(address string) Option {
	return func(c *Client) error {
		if address == "" {
			return fmt.Errorf("%w: empty address", ErrInvalidClient)
		}
		c.address = address

		return nil
	}
}

// WithAuth sets the identity provider.
func WithAuth(provider auth.Provider) Option {
	return func(c *Client) error {
		if provider == nil {
			return fmt.Errorf("%w: nil auth provider", ErrInvalidClient)
		}
		c.provider = provider

		return nil
	}
}

// WithVersion sets the complete wire profile.
func WithVersion(profile version.WireProfile) Option {
	return func(c *Client) error {
		c.profile = profile

		return nil
	}
}

// WithAuthorization records the operator's declaration of endpoint and
// scopes. It cannot prove permission; it prevents a script using high-level
// actions against an arbitrary address by accident.
func WithAuthorization(a safety.Authorization) Option {
	return func(c *Client) error {
		c.authorization = a

		return nil
	}
}

// WithLogger sets a structured logger. The default discards output. Packet
// payloads are never logged: they carry chat, plugin data, and identity.
func WithLogger(logger *slog.Logger) Option {
	return func(c *Client) error {
		if logger == nil {
			return fmt.Errorf("%w: nil logger", ErrInvalidClient)
		}
		c.logger = logger

		return nil
	}
}

// WithConnectTimeout bounds Connect. Connect waits on a packet the server
// sends at its own pace, so it needs a deadline of its own.
func WithConnectTimeout(d time.Duration) Option {
	return func(c *Client) error {
		if d <= 0 {
			return fmt.Errorf("%w: connect timeout must be positive, got %v", ErrInvalidClient, d)
		}
		c.connectTimeout = d

		return nil
	}
}

// WithBundleLimit bounds one protocol 775 bundle's packet count.
func WithBundleLimit(n int) Option {
	return func(c *Client) error {
		if n <= 0 {
			return fmt.Errorf("%w: bundle limit must be positive, got %d", ErrInvalidClient, n)
		}
		c.bundleLimit = n

		return nil
	}
}

// WithObservationGrace bounds how long a placed session may observe no terrain
// before the client publishes event.ObservationMissing and logs a warning.
//
// It applies only when a world is installed, and it reports once per
// connection: a session that loads no chunk is suspect rather than invalid, so
// nothing here ends the connection over it. Raise it for a server that streams
// a large view distance slowly.
func WithObservationGrace(d time.Duration) Option {
	return func(c *Client) error {
		if d <= 0 {
			return fmt.Errorf("%w: observation grace must be positive, got %v", ErrInvalidClient, d)
		}
		c.observationGrace = d

		return nil
	}
}

// reducerSource is an adapter that can build the world's reducers.
//
// The client asserts for it rather than the version package declaring it,
// because version.WireProfile is what world depends on: naming this in
// version would make the two packages import each other.
type reducerSource interface {
	Reducers(*world.World) []world.Reducer
}

// WithWorld installs the observed world state a connection maintains.
//
// Without one the client publishes events and keeps no state, which is what a
// consumer that only watches traffic wants. With one, every batch is applied
// to it before its events are published, so an event and the snapshot it
// names describe the same instant.
func WithWorld(w *world.World) Option {
	return func(c *Client) error {
		if w == nil {
			return fmt.Errorf("%w: nil world", ErrInvalidClient)
		}
		c.world = w

		return nil
	}
}

// World returns the current observed state, or the zero snapshot when no
// world is installed.
func (c *Client) World() world.Snapshot {
	if c.world == nil {
		return world.Snapshot{}
	}

	return c.world.Snapshot()
}

// New validates a configuration and returns a client that has not connected.
// It performs no network work and no authentication.
func New(options ...Option) (*Client, error) {
	c := &Client{
		logger:           slog.New(slog.DiscardHandler),
		recovery:         safety.Strict(),
		connectTimeout:   defaultConnectTimeout,
		bundleLimit:      defaultBundleLimit,
		observationGrace: defaultObservationGrace,
		loop:             make(chan struct{}),
		done:             make(chan struct{}),
	}

	for _, option := range options {
		if option == nil {
			return nil, fmt.Errorf("%w: nil option", ErrInvalidClient)
		}
		if err := option(c); err != nil {
			return nil, err
		}
	}

	if c.address == "" {
		return nil, fmt.Errorf("%w: no address", ErrInvalidClient)
	}
	if c.provider == nil {
		return nil, fmt.Errorf("%w: no auth provider", ErrInvalidClient)
	}
	if err := c.profile.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidClient, err)
	}
	// Observing is the least a connected client does, so it is the scope the
	// authorization must cover before anything dials.
	if !c.authorization.Allows(c.address, safety.ScopeObserve) {
		return nil, fmt.Errorf(
			"%w: %w: %q is not authorized for the %q scope",
			ErrInvalidClient,
			safety.ErrUnauthorized,
			c.address,
			safety.ScopeObserve,
		)
	}

	if err := c.registerReducers(); err != nil {
		return nil, err
	}

	return c, nil
}

// registerReducers gives the installed world the adapter's reducers.
//
// It runs after every option, not inside WithWorld, because it needs the
// profile: an option that read another option's value would make the order
// they were passed in matter.
//
// A world with no reducers is refused rather than accepted. This seam is
// satisfied by interface assertion, so an adapter that misspells the method or
// declares it as a package-level function still compiles, still passes its own
// tests, and installs a world that counts batches and observes nothing — which
// is what shipped in M7 and what nothing reported. There is no consumer who
// asks for observed state and wants none of it, so the case that used to be
// legal is now the error it always was.
func (c *Client) registerReducers() error {
	if c.world == nil {
		return nil
	}

	source, ok := c.profile.Adapter.(reducerSource)
	if !ok {
		return fmt.Errorf(
			"%w: the %s adapter supplies no reducers, so an installed world would observe nothing",
			ErrInvalidClient,
			c.profile.Protocol.ID(),
		)
	}

	reducers := source.Reducers(c.world)
	if len(reducers) == 0 {
		return fmt.Errorf(
			"%w: the %s adapter returned no reducers, so an installed world would observe nothing",
			ErrInvalidClient,
			c.profile.Protocol.ID(),
		)
	}

	for _, reducer := range reducers {
		if err := c.world.Register(reducer); err != nil {
			return fmt.Errorf("%w: %w", ErrInvalidClient, err)
		}
	}

	return nil
}

// ConnectTimeout reports the deadline Connect applies.
func (c *Client) ConnectTimeout() time.Duration { return c.connectTimeout }

// Subscribe returns a bounded subscription over the selected domains.
func (c *Client) Subscribe(selector event.Domain, buffer int) (*Subscription, error) {
	return c.events.subscribe(selector, buffer)
}
