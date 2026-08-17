package world_test

import (
	"strconv"
	"testing"

	"github.com/go-theft-craft/headless-minecraft/world"
)

func TestAPayloadIsOwnedAndNotAliased(t *testing.T) {
	t.Parallel()

	// The bytes arrive in a buffer the stream reuses. Keeping the caller's
	// slice would let the next packet rewrite state a subscriber already read.
	w := world.New()
	buffer := []byte{1, 2, 3}
	w.Payloads().Received("modded:reactor", buffer)

	buffer[0] = 99

	stored, ok := w.Snapshot().Payloads.Channels.Get("modded:reactor")
	if !ok {
		t.Fatal("the payload was not stored")
	}
	if stored[0] != 1 {
		t.Errorf("the store aliased the caller's buffer: %v", stored)
	}

	// A snapshot's bytes are the snapshot's own, in the other direction too.
	stored[1] = 88
	again, _ := w.Snapshot().Payloads.Channels.Get("modded:reactor")
	if again[1] != 2 {
		t.Errorf("writing to a snapshot reached the store: %v", again)
	}
}

func TestPayloadChannelsAreBoundedAndCounted(t *testing.T) {
	t.Parallel()

	// A server that opens a new channel every tick is the case the bound
	// exists for. What it refuses is counted, because a silent drop is a bug
	// report nobody can act on.
	w := world.New()
	for i := range 300 {
		w.Payloads().Received("channel/"+strconv.Itoa(i), []byte{byte(i)})
	}

	channels := w.Snapshot().Payloads.Channels
	if len(channels.Values) != channels.Max {
		t.Errorf("kept %d channels, want the bound of %d", len(channels.Values), channels.Max)
	}
	if channels.Dropped != 300-channels.Max {
		t.Errorf("dropped counter is %d, want %d", channels.Dropped, 300-channels.Max)
	}

	// A channel already stored is still writable once the bound is reached: it
	// is new keys the bound refuses, not new messages.
	w.Payloads().Received("channel/0", []byte{42})
	stored, _ := w.Snapshot().Payloads.Channels.Get("channel/0")
	if stored[0] != 42 {
		t.Errorf("a known channel stopped accepting messages at the bound: %v", stored)
	}
}

func BenchmarkPayloadSnapshot(b *testing.B) {
	// The cost of copying the raw store on every snapshot, which is what
	// decides whether copy-on-read stays right for this domain.
	w := world.New()
	for i := range 64 {
		w.Payloads().Received("channel/"+strconv.Itoa(i), make([]byte, 256))
	}

	b.ReportAllocs()
	for b.Loop() {
		_ = w.Snapshot().Payloads
	}
}
