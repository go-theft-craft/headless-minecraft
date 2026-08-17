package event_test

import (
	"strings"
	"testing"

	"github.com/go-theft-craft/headless-minecraft/event"
)

func TestEveryNameIsUniqueAndDomainPrefixed(t *testing.T) {
	t.Parallel()

	prefixes := map[event.Domain]string{
		event.DomainSession:    "session.",
		event.DomainPlayer:     "player.",
		event.DomainWorld:      "world.",
		event.DomainEntities:   "entity.",
		event.DomainContainers: "container.",
		event.DomainRegistry:   "registry.",
		event.DomainChat:       "chat.",
	}

	seen := make(map[event.Name]bool)
	for _, name := range event.AllNames() {
		if seen[name] {
			t.Fatalf("duplicate event name %q", name)
		}
		seen[name] = true

		domain := name.Domain()
		prefix, ok := prefixes[domain]
		if !ok {
			t.Fatalf("event %q reports unknown domain %d", name, domain)
		}
		if !strings.HasPrefix(string(name), prefix) {
			t.Fatalf("event %q is in domain %q but lacks prefix %q", name, prefix, prefix)
		}
	}
}

func TestTaxonomyCoversEveryDeclaredDomain(t *testing.T) {
	t.Parallel()

	counts := make(map[event.Domain]int)
	for _, name := range event.AllNames() {
		counts[name.Domain()]++
	}

	// The design fixes these counts. A change here is a taxonomy change and
	// must be a deliberate edit to the design, not a drive-by addition.
	want := map[event.Domain]int{
		event.DomainSession:    15,
		event.DomainPlayer:     12,
		event.DomainWorld:      13,
		event.DomainEntities:   13,
		event.DomainContainers: 7,
		event.DomainRegistry:   4,
		event.DomainChat:       12,
	}
	for domain, expected := range want {
		if counts[domain] != expected {
			t.Errorf("domain %d has %d events, want %d", domain, counts[domain], expected)
		}
	}
}

func TestRawIsNotPartOfTheNamedTaxonomy(t *testing.T) {
	t.Parallel()

	for _, name := range event.AllNames() {
		if name.Domain() == event.DomainRaw {
			t.Fatalf("raw packets are a selector, not a named event: %q", name)
		}
	}
}
