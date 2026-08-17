package world

import (
	"slices"
)

// Raw is a bounded, key-addressable store of bytes the server sent that this
// client does not model.
//
// It exists because "preserve what the server said" is a requirement in its
// own right, and because the alternative — decoding what is recognized and
// discarding the rest — makes a modded server's traffic invisible. What a
// modded server sends that no version models is not noise; it is the point.
//
// **A raw store is bounded, and what it refuses is counted.** An unbounded one
// is a memory leak with a server that talks on a new channel every tick, and a
// silent drop is a bug report nobody can act on. The bound is per owner, not
// global, on the design's rule that an unknown value belongs to its owner: an
// unknown metadata index belongs to its entity, an unknown registry key to the
// registries, and a custom payload to the connection.
type Raw struct {
	values  map[string][]byte
	max     int
	dropped int
}

// RawView is a Raw in a snapshot. Its map and every slice in it are owned
// copies, so a caller cannot reach back into the store through the bytes.
type RawView struct {
	Values map[string][]byte
	// Dropped counts values the bound refused. A caller seeing values it
	// expected to be there checks this before concluding the server never
	// sent them.
	Dropped int
	// Max is the bound, so a caller reading Dropped knows what it was
	// measured against.
	Max int
}

// Get returns one value, or false when the store has none under that key. The
// bytes are the snapshot's own copy.
func (v RawView) Get(key string) ([]byte, bool) {
	value, ok := v.Values[key]

	return value, ok
}

func newRaw(maxKeys int) *Raw {
	return &Raw{values: make(map[string][]byte), max: maxKeys}
}

// Set stores an owned copy under a key, replacing whatever was there.
//
// The copy is not optional. The bytes arrive in a buffer the stream reuses, and
// keeping the caller's slice would let the next packet rewrite state a
// subscriber already read.
func (r *Raw) Set(key string, value []byte) {
	if _, existing := r.values[key]; !existing && len(r.values) >= r.max {
		r.dropped++

		return
	}
	r.values[key] = slices.Clone(value)
}

func (r *Raw) view() RawView {
	values := make(map[string][]byte, len(r.values))
	for key, value := range r.values {
		values[key] = slices.Clone(value)
	}

	return RawView{Values: values, Dropped: r.dropped, Max: r.max}
}

// maxPayloadChannels bounds the plugin channels one connection may accumulate.
//
// A modded server opens a handful. One that opens a new channel per tick is
// the case this exists for.
const maxPayloadChannels = 256

// Payloads is every plugin channel the server has spoken on, holding the last
// message per channel.
//
// A custom payload belongs to the connection rather than to any domain, which
// is why it is its own store: a message on `minecraft:brand` describes the
// server, and a message on a modded channel describes whatever the mod means
// by it. Nothing here interprets either.
type Payloads struct {
	raw *Raw
}

// PayloadsView is the payload half of a snapshot.
type PayloadsView struct {
	// Channels holds the last message received on each channel, by channel
	// name, as owned bytes.
	Channels RawView
}

func newPayloads() *Payloads { return &Payloads{raw: newRaw(maxPayloadChannels)} }

func (s *Payloads) view() PayloadsView { return PayloadsView{Channels: s.raw.view()} }

// Received records a plugin message.
//
// It publishes nothing. The taxonomy already names this fact —
// `session.custom_payload_received`, which the adapter's handler publishes
// with its own owned copy — and a second event for one packet would report the
// same thing twice under two names. What this adds is the state: the event
// tells a subscriber a message arrived, and the snapshot lets one that was not
// subscribed at the time read what the last one said.
func (s *Payloads) Received(channel string, payload []byte) {
	s.raw.Set(channel, payload)
}
