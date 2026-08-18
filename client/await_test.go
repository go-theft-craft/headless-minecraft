package client

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/go-theft-craft/headless-minecraft/version"
)

// TestAwaitReadyPrefersASessionThatWasPlaced pins which of two simultaneous
// answers Connect gives.
//
// A server that places the player and kicks straight after leaves the readiness
// signal and the ended loop ready at the same instant, and a select picks
// between ready cases at random. Connect then reported a session that reached
// play — and that has the server's own reason for leaving waiting on its
// subscription — as one that was never placed, in about half of them.
func TestAwaitReadyPrefersASessionThatWasPlaced(t *testing.T) {
	t.Parallel()

	// Once would pass on the old code one time in two. Twenty makes a random
	// pick indistinguishable from a broken one.
	for range 20 {
		bot := &Client{connectTimeout: time.Second, loop: make(chan struct{})}
		ready := make(chan version.ReadyState, 1)
		ready <- version.ReadyState{Ready: true, EntityID: 7}
		close(bot.loop)

		if err := bot.awaitReady(t.Context(), nil, ready); err != nil {
			t.Fatalf("awaitReady() = %v, want nil: the player was placed and the server then hung up", err)
		}
	}
}

// TestAwaitReadyNamesASessionThatEndedWithNoError covers the other order: the
// loop ends and nothing was ever ready.
func TestAwaitReadyNamesASessionThatEndedWithNoError(t *testing.T) {
	t.Parallel()

	bot := &Client{connectTimeout: time.Second, loop: make(chan struct{})}
	close(bot.loop)

	err := bot.awaitReady(context.Background(), nil, make(chan version.ReadyState, 1))
	if err == nil {
		t.Fatal("awaitReady() = nil, want an error: the connection ended before play")
	}
	// A server that hangs up ends the loop with no error at all, and the
	// message wrapped that nil.
	if strings.Contains(err.Error(), "%!") {
		t.Errorf("awaitReady() = %q, which renders a formatting verb rather than the session", err)
	}
}
