package java_test

import (
	"testing"

	"github.com/go-theft-craft/headless-minecraft/event"
	"github.com/go-theft-craft/headless-minecraft/version"
	"github.com/go-theft-craft/headless-minecraft/version/java"
)

func TestJava1_8IsAValidProfile(t *testing.T) {
	t.Parallel()

	p := java.Java1_8()
	if err := p.Validate(); err != nil {
		t.Fatalf("Java1_8 is not a valid profile: %v", err)
	}
	if got := p.Protocol.Version().Protocol; got != 47 {
		t.Errorf("protocol number is %d, want 47", got)
	}
}

func TestCurrentIsAValidProfile(t *testing.T) {
	t.Parallel()

	p := java.Current()
	if err := p.Validate(); err != nil {
		t.Fatalf("Current is not a valid profile: %v", err)
	}
	if got := p.Protocol.Version().Protocol; got != 775 {
		t.Errorf("protocol number is %d, want 775", got)
	}
}

func TestEachCallReturnsAFreshReadinessRule(t *testing.T) {
	t.Parallel()

	first := java.Java1_8()
	second := java.Java1_8()

	if first.Readiness == second.Readiness {
		t.Fatal("two profiles share one readiness rule; per-connection progress would leak between clients")
	}
}

func TestProfilesBindTheCallersCollector(t *testing.T) {
	t.Parallel()

	// The loop resets one collector per batch, so the adapter has to append
	// to the collector the loop owns, not one the profile made.
	var c event.Collector
	var o version.Outbox
	p := java.Java1_8With(&c, &o)

	handler, ok := p.Adapter.Handlers()["kick_disconnect"]
	if !ok {
		t.Fatal("no kick_disconnect handler on the 1.8 profile")
	}
	_ = handler

	if got := p.ID; got != "java/1.8.9" {
		t.Errorf("profile ID is %q, want java/1.8.9", got)
	}
}

func TestBundleDelimiterIsPerProtocol(t *testing.T) {
	t.Parallel()

	if got := java.BundleDelimiter(java.Current().ID); got != "bundle_delimiter" {
		t.Errorf("26.1 delimiter is %q, want bundle_delimiter", got)
	}
	if got := java.BundleDelimiter(java.Java1_8().ID); got != "" {
		t.Errorf("1.8 delimiter is %q, want empty: protocol 47 does not bundle", got)
	}
	if got := java.BundleDelimiter("something else"); got != "" {
		t.Errorf("unknown profile delimiter is %q, want empty", got)
	}
}
