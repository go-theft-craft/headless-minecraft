package client

import (
	"errors"
	"fmt"
	"sync"

	"github.com/go-theft-craft/headless-minecraft/event"
)

// ErrOverflow reports a subscriber that fell behind its buffer.
//
// The subscription is closed rather than the event dropped. A ring that
// forgets is the right tool for a debugger, which is what M5's history ring
// is; a subscriber that silently missed an event would hold a wrong view of
// the world and never learn it did.
var ErrOverflow = errors.New("subscription buffer overflowed")

// Subscription delivers selected events to one consumer.
type Subscription struct {
	selector event.Domain
	ch       chan event.Event

	mu     sync.Mutex
	closed bool
	err    error
}

// C returns the delivery channel. It closes when the subscription ends, by
// Close, by overflow, or by the client shutting down.
func (s *Subscription) C() <-chan event.Event { return s.ch }

// Err reports why the subscription ended, or nil for a clean close.
func (s *Subscription) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.err
}

// Close ends the subscription. It is idempotent.
func (s *Subscription) Close() error {
	s.finish(nil)

	return nil
}

// finish closes the channel exactly once and records the reason.
func (s *Subscription) finish(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return
	}
	s.closed = true
	s.err = err
	close(s.ch)
}

// deliver attempts one non-blocking send. It reports whether the
// subscription is still alive.
func (s *Subscription) deliver(e event.Event) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return false
	}

	select {
	case s.ch <- e:
		return true
	default:
		s.closed = true
		s.err = fmt.Errorf("%w: capacity %d", ErrOverflow, cap(s.ch))
		close(s.ch)

		return false
	}
}

// fanout owns every subscription and publishes to them.
//
// Publishing never blocks: a full subscriber is closed, not waited on, so a
// slow consumer cannot stall the read goroutine or delay keepalive handling.
type fanout struct {
	mu   sync.Mutex
	subs []*Subscription
}

func (f *fanout) subscribe(selector event.Domain, buffer int) (*Subscription, error) {
	if buffer <= 0 {
		return nil, fmt.Errorf("subscribe: buffer must be positive, got %d", buffer)
	}
	if selector == 0 {
		return nil, errors.New("subscribe: no domain selected")
	}

	sub := &Subscription{selector: selector, ch: make(chan event.Event, buffer)}

	f.mu.Lock()
	f.subs = append(f.subs, sub)
	f.mu.Unlock()

	return sub, nil
}

// publish delivers one batch's events to every matching subscription and
// drops the subscriptions that overflowed.
func (f *fanout) publish(events []event.Event) {
	if len(events) == 0 {
		return
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	live := f.subs[:0]
	for _, sub := range f.subs {
		alive := true
		for _, e := range events {
			if sub.selector&e.Domain() == 0 {
				continue
			}
			if !sub.deliver(e) {
				alive = false

				break
			}
		}
		if alive {
			live = append(live, sub)
		}
	}
	// Everything past live is either overflowed or already closed; clear the
	// tail so the fanout does not pin dead subscriptions.
	clear(f.subs[len(live):])
	f.subs = live
}

func (f *fanout) closeAll() {
	f.mu.Lock()
	subs := f.subs
	f.subs = nil
	f.mu.Unlock()

	for _, sub := range subs {
		sub.finish(nil)
	}
}
