package version_test

import (
	"errors"
	"testing"

	protocol "github.com/go-theft-craft/minecraft-protocol"

	"github.com/go-theft-craft/headless-minecraft/version"
)

func packet(name string) protocol.Packet {
	return protocol.Packet{Name: name, State: "play", Direction: protocol.DirectionClientbound}
}

func TestUnbundledProtocolAlwaysEmitsSinglePacketBatches(t *testing.T) {
	t.Parallel()

	b, err := version.NewBatcher("", 16)
	if err != nil {
		t.Fatalf("NewBatcher: %v", err)
	}

	for _, name := range []string{"login", "position", "chat"} {
		batch, ready, err := b.Accept(packet(name))
		if err != nil {
			t.Fatalf("Accept(%s): %v", name, err)
		}
		if !ready {
			t.Fatalf("Accept(%s) withheld a batch on a protocol with no delimiter", name)
		}
		if len(batch.Packets) != 1 || batch.Bundled {
			t.Fatalf("Accept(%s) produced %d packets, bundled=%v", name, len(batch.Packets), batch.Bundled)
		}
	}
}

func TestDelimiterTogglesAndGroups(t *testing.T) {
	t.Parallel()

	b, _ := version.NewBatcher("bundle_delimiter", 16)

	if _, ready, err := b.Accept(packet("bundle_delimiter")); err != nil || ready {
		t.Fatalf("opening delimiter produced ready=%v err=%v, want false/nil", ready, err)
	}
	if !b.Open() {
		t.Fatal("batcher reports closed after an opening delimiter")
	}

	for _, name := range []string{"spawn_entity", "entity_metadata"} {
		if _, ready, err := b.Accept(packet(name)); err != nil || ready {
			t.Fatalf("packet inside a bundle produced ready=%v err=%v, want false/nil", ready, err)
		}
	}

	batch, ready, err := b.Accept(packet("bundle_delimiter"))
	if err != nil || !ready {
		t.Fatalf("closing delimiter produced ready=%v err=%v, want true/nil", ready, err)
	}
	if !batch.Bundled {
		t.Error("closed bundle reports Bundled=false")
	}
	if len(batch.Packets) != 2 {
		t.Fatalf("bundle holds %d packets, want 2", len(batch.Packets))
	}
	if batch.Packets[0].Name != "spawn_entity" || batch.Packets[1].Name != "entity_metadata" {
		t.Error("bundle lost wire order")
	}
	if b.Open() {
		t.Error("batcher reports open after a closing delimiter")
	}
}

func TestPacketOutsideABundleEmitsImmediately(t *testing.T) {
	t.Parallel()

	b, _ := version.NewBatcher("bundle_delimiter", 16)

	batch, ready, err := b.Accept(packet("keep_alive"))
	if err != nil || !ready {
		t.Fatalf("got ready=%v err=%v, want true/nil", ready, err)
	}
	if batch.Bundled || len(batch.Packets) != 1 {
		t.Errorf("unbundled packet produced bundled=%v with %d packets", batch.Bundled, len(batch.Packets))
	}
}

func TestEmptyBundleIsAValidEmptyBatch(t *testing.T) {
	t.Parallel()

	b, _ := version.NewBatcher("bundle_delimiter", 16)
	_, _, _ = b.Accept(packet("bundle_delimiter"))

	batch, ready, err := b.Accept(packet("bundle_delimiter"))
	if err != nil || !ready {
		t.Fatalf("got ready=%v err=%v, want true/nil", ready, err)
	}
	if len(batch.Packets) != 0 || !batch.Bundled {
		t.Errorf("empty bundle produced %d packets, bundled=%v", len(batch.Packets), batch.Bundled)
	}
}

func TestBundledBatchDoesNotAliasTheBatcher(t *testing.T) {
	t.Parallel()

	b, _ := version.NewBatcher("bundle_delimiter", 16)
	_, _, _ = b.Accept(packet("bundle_delimiter"))
	_, _, _ = b.Accept(packet("spawn_entity"))

	batch, _, _ := b.Accept(packet("bundle_delimiter"))

	// The batcher reuses its pending slice across bundles. A batch that
	// aliased it would have its packets overwritten by the next bundle.
	_, _, _ = b.Accept(packet("bundle_delimiter"))
	_, _, _ = b.Accept(packet("entity_metadata"))
	_, _, _ = b.Accept(packet("bundle_delimiter"))

	if batch.Packets[0].Name != "spawn_entity" {
		t.Errorf("first bundle now holds %q: the batch aliased the batcher", batch.Packets[0].Name)
	}
}

func TestOversizeBundleIsAnErrorBeforeUnboundedBuffering(t *testing.T) {
	t.Parallel()

	b, _ := version.NewBatcher("bundle_delimiter", 3)
	_, _, _ = b.Accept(packet("bundle_delimiter"))

	var err error
	for range 10 {
		if _, _, err = b.Accept(packet("filler")); err != nil {
			break
		}
	}
	if !errors.Is(err, version.ErrBundleTooLarge) {
		t.Fatalf("got %v, want ErrBundleTooLarge", err)
	}
}

func TestFinishReportsAnUnterminatedBundle(t *testing.T) {
	t.Parallel()

	b, _ := version.NewBatcher("bundle_delimiter", 16)
	_, _, _ = b.Accept(packet("bundle_delimiter"))
	_, _, _ = b.Accept(packet("spawn_entity"))

	if err := b.Finish(); !errors.Is(err, version.ErrBundleUnterminated) {
		t.Fatalf("got %v, want ErrBundleUnterminated", err)
	}
}

func TestFinishOnAClosedBatcherIsNil(t *testing.T) {
	t.Parallel()

	b, _ := version.NewBatcher("bundle_delimiter", 16)
	_, _, _ = b.Accept(packet("keep_alive"))

	if err := b.Finish(); err != nil {
		t.Fatalf("Finish on a closed batcher returned %v, want nil", err)
	}
}

func TestNewBatcherRejectsANonPositiveLimit(t *testing.T) {
	t.Parallel()

	if _, err := version.NewBatcher("bundle_delimiter", 0); err == nil {
		t.Fatal("NewBatcher accepted a zero limit")
	}
}
