package client

import (
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/go-theft-craft/headless-minecraft/auth"
	"github.com/go-theft-craft/headless-minecraft/event"
	"github.com/go-theft-craft/headless-minecraft/safety"
	"github.com/go-theft-craft/headless-minecraft/version"
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

	mu       sync.Mutex
	closed   bool
	closeErr error
	stop     func()
	done     chan struct{}
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

// New validates a configuration and returns a client that has not connected.
// It performs no network work and no authentication.
func New(options ...Option) (*Client, error) {
	c := &Client{
		logger:         slog.New(slog.DiscardHandler),
		recovery:       safety.Strict(),
		connectTimeout: defaultConnectTimeout,
		bundleLimit:    defaultBundleLimit,
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

	return c, nil
}

// ConnectTimeout reports the deadline Connect applies.
func (c *Client) ConnectTimeout() time.Duration { return c.connectTimeout }

// Subscribe returns a bounded subscription over the selected domains.
func (c *Client) Subscribe(selector event.Domain, buffer int) (*Subscription, error) {
	return c.events.subscribe(selector, buffer)
}

// Close ends the client and every subscription it handed out. It is
// idempotent, and it is safe on a client that never connected.
//
// Closed is the last event a subscriber receives, and it is published exactly
// once however many times Close is called.
func (c *Client) Close() error {
	c.mu.Lock()
	if c.closed {
		err := c.closeErr
		c.mu.Unlock()

		return err
	}
	c.closed = true
	stop := c.stop
	c.mu.Unlock()

	if stop != nil {
		stop()
	}

	var collector event.Collector
	event.Emit(&collector, event.Closed{})
	c.events.publish(collector.Events(0))
	c.events.closeAll()
	close(c.done)

	c.mu.Lock()
	defer c.mu.Unlock()

	return c.closeErr
}
