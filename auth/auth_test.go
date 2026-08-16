package auth_test

import (
	"context"
	"testing"

	"github.com/go-theft-craft/headless-minecraft/auth"
)

func TestOfflineReturnsAValidatedIdentity(t *testing.T) {
	t.Parallel()

	p, err := auth.Offline("tester")
	if err != nil {
		t.Fatalf("Offline: %v", err)
	}

	id, err := p.Authenticate(t.Context())
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if id.Username != "tester" {
		t.Errorf("username is %q, want tester", id.Username)
	}
	if id.UUID == "" {
		t.Error("identity carries no UUID")
	}
	if id.Authenticator == nil {
		t.Error("identity carries no authenticator for the login negotiator")
	}
}

func TestOfflineRejectsAnInvalidName(t *testing.T) {
	t.Parallel()

	// What counts as valid is the shared login package's rule, not one
	// invented here: empty, over 16 bytes, invalid UTF-8, or containing a
	// control character. It deliberately allows names vanilla would not,
	// such as ones with spaces, because an offline or modded server may.
	for _, name := range []string{"", "way_too_long_for_a_minecraft_name", "bad\x00name"} {
		if _, err := auth.Offline(name); err == nil {
			t.Errorf("Offline accepted the username %q", name)
		}
	}
}

func TestOfflineIsDeterministic(t *testing.T) {
	t.Parallel()

	first, _ := auth.Offline("tester")
	second, _ := auth.Offline("tester")

	a, _ := first.Authenticate(t.Context())
	b, _ := second.Authenticate(t.Context())

	if a.UUID != b.UUID {
		t.Errorf("offline UUID is not stable: %q then %q", a.UUID, b.UUID)
	}
}

func TestOfflineAuthenticateMakesNoRequest(t *testing.T) {
	t.Parallel()

	// A cancelled context proves it: a provider that reached the network
	// would fail here, and the offline one has nobody to tell.
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	p, _ := auth.Offline("tester")
	if _, err := p.Authenticate(ctx); err != nil {
		t.Fatalf("Authenticate on a cancelled context returned %v, want nil", err)
	}
}
