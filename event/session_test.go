package event_test

import (
	"testing"

	"github.com/go-theft-craft/headless-minecraft/event"
)

func TestSessionEventsReportTheirOwnNames(t *testing.T) {
	t.Parallel()

	cases := []struct {
		value event.Event
		name  event.Name
	}{
		{event.Connecting{Address: "localhost:25565"}, event.NameSessionConnecting},
		{event.Authenticated{Username: "tester"}, event.NameSessionAuthenticated},
		{event.StateChanged{From: "login", To: "play"}, event.NameSessionStateChanged},
		{event.Ready{EntityID: 7}, event.NameSessionReady},
		{event.Disconnected{Reason: "kicked"}, event.NameSessionDisconnected},
		{event.Closed{}, event.NameSessionClosed},
		{event.KeepAlivePonged{ID: 3}, event.NameSessionKeepAlivePonged},
		{event.TransferRequested{Host: "elsewhere"}, event.NameSessionTransferRequested},
		{event.ResourcePackOffered{UUID: "u"}, event.NameSessionResourcePackOffered},
		{event.ResourcePackRevoked{UUID: "u"}, event.NameSessionResourcePackRevoked},
		{event.ServerMetadataChanged{Kind: "links"}, event.NameSessionServerMetadataChanged},
		{event.CookieRequested{Key: "k"}, event.NameSessionCookieRequested},
		{event.CookieStored{Key: "k"}, event.NameSessionCookieStored},
		{event.CustomPayloadReceived{Channel: "c"}, event.NameSessionCustomPayloadReceived},
	}

	for _, tc := range cases {
		if got := tc.value.Name(); got != tc.name {
			t.Errorf("%T reports name %q, want %q", tc.value, got, tc.name)
		}
		if got := tc.value.Domain(); got != event.DomainSession {
			t.Errorf("%T reports domain %d, want DomainSession", tc.value, got)
		}
		if got := tc.value.Revision(); got != 0 {
			t.Errorf("%T reports revision %d before publication, want 0", tc.value, got)
		}
	}
}

func TestRawPacketEventsReportTheRawDomain(t *testing.T) {
	t.Parallel()

	cases := []struct {
		value event.Event
		name  event.Name
	}{
		{event.PacketReceived{Packet: "chunk_data"}, event.NameSessionPacketReceived},
		{event.PacketSent{Packet: "keep_alive"}, event.NameSessionPacketSent},
	}

	for _, tc := range cases {
		if got := tc.value.Name(); got != tc.name {
			t.Errorf("%T reports name %q, want %q", tc.value, got, tc.name)
		}
		// The struct answers DomainRaw, which is what a subscriber's selector
		// matches, while the name stays outside the named taxonomy.
		if got := tc.value.Domain(); got != event.DomainRaw {
			t.Errorf("%T reports domain %d, want DomainRaw", tc.value, got)
		}
		if got := tc.name.Domain(); got != 0 {
			t.Errorf("name %q is in the named taxonomy under domain %d", tc.name, got)
		}
	}
}

func TestEverySessionNameHasAnEventThatReportsIt(t *testing.T) {
	t.Parallel()

	implemented := []event.Event{
		event.Connecting{},
		event.Authenticated{},
		event.StateChanged{},
		event.Ready{},
		event.Disconnected{},
		event.Closed{},
		event.KeepAlivePonged{},
		event.TransferRequested{},
		event.ResourcePackOffered{},
		event.ResourcePackRevoked{},
		event.ServerMetadataChanged{},
		event.CookieRequested{},
		event.CookieStored{},
		event.CustomPayloadReceived{},
	}

	seen := make(map[event.Name]bool, len(implemented))
	for _, e := range implemented {
		seen[e.Name()] = true
	}

	for _, name := range event.AllNames() {
		if name.Domain() != event.DomainSession {
			continue
		}
		if !seen[name] {
			t.Errorf("session name %q has no event struct", name)
		}
	}
}
