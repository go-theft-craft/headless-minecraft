package event

// Collector accumulates the events produced while one batch is applied.
//
// Handlers append here rather than publishing directly, because a protocol
// 775 bundle must reach subscribers as one unit. The loop resets a collector
// per batch and publishes what it holds once the batch closes.
//
// A Collector is not safe for concurrent use. It does not need to be: it is
// owned by the read goroutine, which is the only goroutine that runs
// handlers.
type Collector struct {
	pending []func(uint64) Event
}

// stampable constrains Emit to a pointer to an event that embeds Stamp.
//
// The pointer is what makes the revision reachable. Events are values, so a
// value in an Event interface cannot be written to; *E can, and setRevision
// is promoted to it by the embedded Stamp.
type stampable[E Event] interface {
	*E

	setRevision(uint64)
}

// Emit appends one event to the collector.
//
// It is a function rather than a method because it is generic and Go methods
// are not: the collector has to remember each event's concrete type to stamp
// it later, and Add(Event) would erase exactly that.
//
// The event is copied on the way in, so a handler may reuse the value it
// passed.
func Emit[E Event, PE stampable[E]](c *Collector, e E) {
	c.pending = append(c.pending, func(revision uint64) Event {
		stamped := e
		PE(&stamped).setRevision(revision)

		return stamped
	})
}

// Len reports how many events the collector holds.
func (c *Collector) Len() int { return len(c.pending) }

// Events stamps every held event with revision and returns them in append
// order, in a slice the caller owns.
//
// Stamping happens here, at publication, rather than where the event was
// emitted. A handler never names a revision, so it cannot name one that does
// not exist yet, and every event from one batch reports the same number. The
// stamped values keep their own concrete types, so a subscriber still
// switches on Connecting or Ready rather than on a wrapper.
func (c *Collector) Events(revision uint64) []Event {
	events := make([]Event, len(c.pending))
	for i, stamp := range c.pending {
		events[i] = stamp(revision)
	}

	return events
}

// Reset empties the collector, retaining its capacity for the next batch.
func (c *Collector) Reset() { c.pending = c.pending[:0] }
