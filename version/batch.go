package version

import (
	"errors"
	"fmt"
	"slices"

	protocol "github.com/go-theft-craft/minecraft-protocol"
)

// ErrBundleTooLarge reports a bundle that exceeded its packet limit. It is a
// protocol error rather than a truncation, because the alternative is an
// unbounded buffer fed by the peer.
var ErrBundleTooLarge = errors.New("bundle exceeds its packet limit")

// ErrBundleUnterminated reports a bundle still open when the stream ended.
var ErrBundleUnterminated = errors.New("bundle was never closed")

// Batch is the unit the read loop delivers.
//
// A batch is applied atomically: M7 bumps the observed-state revision once
// per batch, never once per packet, so a subscriber never sees an entity
// spawned without the metadata that arrived with it.
type Batch struct {
	Packets []protocol.Packet
	Bundled bool
}

// Batcher groups inbound packets at bundle boundaries.
//
// It is owned by the read goroutine and is not safe for concurrent use.
type Batcher struct {
	delimiter string
	limit     int
	pending   []protocol.Packet
	open      bool
}

// NewBatcher returns a batcher for one protocol.
//
// An empty delimiter names a protocol with no bundling, such as Java 1.8's
// protocol 47: every packet becomes its own batch and Open is always false.
// The limit bounds one bundle's packet count.
func NewBatcher(delimiter string, limit int) (*Batcher, error) {
	// Deliberately not ErrBundleTooLarge. That sentinel means the peer sent
	// an oversize bundle; this is a caller passing a limit that cannot bound
	// anything, and the two want different handling.
	if limit <= 0 {
		return nil, fmt.Errorf("batcher limit must be positive, got %d", limit)
	}

	return &Batcher{delimiter: delimiter, limit: limit}, nil
}

// Open reports whether a bundle is currently accumulating.
func (b *Batcher) Open() bool { return b.open }

// Accept consumes one packet. It returns a batch when one is complete; the
// boolean is false while a bundle is still accumulating.
func (b *Batcher) Accept(p protocol.Packet) (Batch, bool, error) {
	if b.delimiter == "" {
		return Batch{Packets: []protocol.Packet{p}}, true, nil
	}

	if p.Name == b.delimiter {
		if !b.open {
			b.open = true

			return Batch{}, false, nil
		}

		// Cloned because the pending slice is reused by the next bundle.
		batch := Batch{Packets: slices.Clone(b.pending), Bundled: true}
		b.pending = b.pending[:0]
		b.open = false

		return batch, true, nil
	}

	if !b.open {
		return Batch{Packets: []protocol.Packet{p}}, true, nil
	}

	if len(b.pending) >= b.limit {
		return Batch{}, false, fmt.Errorf("%w: limit is %d", ErrBundleTooLarge, b.limit)
	}
	b.pending = append(b.pending, p)

	return Batch{}, false, nil
}

// Finish reports whether the stream ended mid-bundle. The loop calls it after
// its last read, so a peer that opens a bundle and disappears is a named
// error rather than silently discarded packets.
func (b *Batcher) Finish() error {
	if b.open {
		return fmt.Errorf("%w: %d packets pending", ErrBundleUnterminated, len(b.pending))
	}

	return nil
}
