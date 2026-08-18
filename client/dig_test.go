package client

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	gen "github.com/go-theft-craft/minecraft-protocol/generated/java/v1_8"
	gen26_1 "github.com/go-theft-craft/minecraft-protocol/generated/java/v26_1"

	"github.com/go-theft-craft/headless-minecraft/version"
	"github.com/go-theft-craft/headless-minecraft/version/java"
)

// digStages reads the dig stages a recording sender saw, in order.
func digStages(t *testing.T, sender *recordingSender) []version.DigStage {
	t.Helper()

	var stages []version.DigStage
	for _, packet := range sender.sent {
		body, ok := packet.Value.(*gen.PlayServerboundBlockDig)
		if !ok {
			continue
		}
		stages = append(stages, version.DigStage(body.Status))
	}

	return stages
}

func TestADigSendsStartThenFinish(t *testing.T) {
	t.Parallel()

	// Vanilla sends start-digging, waits the break time, then sends
	// finish-digging. A client that sends only the finish packet breaks blocks
	// instantly and is the first thing an anti-cheat notices.
	sender := &recordingSender{}
	c := actionClient(t, java.Java1_8(), sender)

	if err := c.Dig(t.Context(), version.BlockPos{X: 0, Y: 63, Z: 0}, version.FaceTop, 0); err != nil {
		t.Fatalf("Dig: %v", err)
	}

	if got := digStages(t, sender); !slices.Equal(got, []version.DigStage{version.DigStart, version.DigFinish}) {
		t.Fatalf("sent %v, want a start then a finish", got)
	}
}

func TestADigCarriesTheBlockAndFaceOnBothPackets(t *testing.T) {
	t.Parallel()

	// The server validates the position on both. A finish that named a
	// different block is a break the server refuses and the client predicts.
	sender := &recordingSender{}
	c := actionClient(t, java.Java1_8(), sender)
	block := version.BlockPos{X: -7, Y: 12, Z: 300}

	if err := c.Dig(t.Context(), block, version.FaceWest, 0); err != nil {
		t.Fatalf("Dig: %v", err)
	}

	for _, packet := range sender.sent {
		body := packet.Value.(*gen.PlayServerboundBlockDig)
		if body.Location != (gen.Position{X: -7, Y: 12, Z: 300}) {
			t.Errorf("stage %d addressed %+v", body.Status, body.Location)
		}
		if body.Face != int8(version.FaceWest) {
			t.Errorf("stage %d named face %d", body.Status, body.Face)
		}
	}
}

func TestADigDoesNotFinishBeforeItsBreakTime(t *testing.T) {
	t.Parallel()

	// The elapsed time between start and finish is what a server validates,
	// so the assertion is on this client's own timing rather than on any
	// server's tolerance.
	sender := &recordingSender{}
	c := actionClient(t, java.Java1_8(), sender)

	const breaking = 80 * time.Millisecond

	began := time.Now()
	if err := c.Dig(t.Context(), version.BlockPos{}, version.FaceTop, breaking); err != nil {
		t.Fatalf("Dig: %v", err)
	}

	if elapsed := time.Since(began); elapsed < breaking {
		t.Fatalf("finished after %v, want at least %v; a dig this fast is what "+
			"an anti-cheat flags", elapsed, breaking)
	}
}

func TestACancelledDigSendsCancelAndNotFinish(t *testing.T) {
	t.Parallel()

	sender := &recordingSender{}
	c := actionClient(t, java.Java1_8(), sender)

	ctx, cancel := context.WithCancel(t.Context())
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	err := c.Dig(ctx, version.BlockPos{}, version.FaceTop, time.Minute)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Dig error = %v, want the cancellation", err)
	}

	got := digStages(t, sender)
	if slices.Contains(got, version.DigFinish) {
		t.Fatalf("sent %v; a cancelled dig must not claim to have finished", got)
	}
	if !slices.Contains(got, version.DigCancel) {
		t.Fatalf("sent %v, want a cancel: a server left believing a dig is in "+
			"progress refuses the next one at that position", got)
	}
}

func TestADigRefusedBeforePlayNeverStarts(t *testing.T) {
	t.Parallel()

	sender := &recordingSender{}
	c := actionClient(t, java.Java1_8(), sender)
	c.inPlay = false

	if err := c.Dig(t.Context(), version.BlockPos{}, version.FaceTop, 0); !errors.Is(err, ErrNotInPlay) {
		t.Fatalf("Dig error = %v, want ErrNotInPlay", err)
	}
	if len(sender.sent) != 0 {
		t.Fatalf("a refused dig still wrote %d packets", len(sender.sent))
	}
}

func TestADigOnProtocol775CarriesTheSameThreeStages(t *testing.T) {
	t.Parallel()

	// The stages are the version-neutral half and the packet is the
	// version-owned one. Both protocols number these three the same way, which
	// is worth pinning rather than assuming: they diverge at the fourth.
	sender := &recordingSender{}
	c := actionClient(t, java.Current(), sender)

	if err := c.Dig(t.Context(), version.BlockPos{X: 1, Y: 2, Z: 3}, version.FaceTop, 0); err != nil {
		t.Fatalf("Dig: %v", err)
	}

	if len(sender.sent) != 2 {
		t.Fatalf("sent %d packets, want two", len(sender.sent))
	}
	for index, want := range []int32{0, 2} {
		body, ok := sender.sent[index].Value.(*gen26_1.PlayServerboundBlockDig)
		if !ok {
			t.Fatalf("packet %d is %T", index, sender.sent[index].Value)
		}
		if body.Status != want {
			t.Errorf("packet %d has status %d, want %d", index, body.Status, want)
		}
	}
}
