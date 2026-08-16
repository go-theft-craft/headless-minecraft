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

	events fanout
	world  *world.World

	mu         sync.Mutex
	closed     bool
	closeErr   error
	connecting bool
	stream     *protocol.Stream
	loopError  error
	stop       func()
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
		logger:         slog.New(slog.DiscardHandler),
		recovery:       safety.Strict(),
		connectTimeout: defaultConnectTimeout,
		bundleLimit:    defaultBundleLimit,
		loop:           make(chan struct{}),
		done:           make(chan struct{}),
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
func (c *Client) registerReducers() error {
	if c.world == nil {
		return nil
	}

	// An adapter with no reducers is legal: it observes nothing, and the
	// world still counts batches.
	source, ok := c.profile.Adapter.(reducerSource)
	if !ok {
		return nil
	}

	for _, reducer := range source.Reducers(c.world) {
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
