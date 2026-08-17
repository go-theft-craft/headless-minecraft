package v26_1_test

import (
	"testing"

	protocol "github.com/go-theft-craft/minecraft-protocol"
	gen "github.com/go-theft-craft/minecraft-protocol/generated/java/v26_1"

	"github.com/go-theft-craft/headless-minecraft/event"
	adapter "github.com/go-theft-craft/headless-minecraft/internal/adapter/v26_1"
	"github.com/go-theft-craft/headless-minecraft/version"
)

// The descriptor's own names for two packets this milestone cares about. A
// name this package guessed would register a handler nothing ever reaches.
const (
	configurationDisconnectName = "disconnect"
	registryDataName            = "registry_data"
)

func configuration(name string, value any) protocol.Packet {
	return protocol.Packet{
		State:     gen.StateConfiguration,
		Direction: protocol.DirectionClientbound,
		Name:      name,
		Value:     value,
	}
}

// handle dispatches one packet through the adapter and returns what it
// produced.
func handle(t *testing.T, p protocol.Packet) []event.Event {
	t.Helper()

	var c event.Collector
	var o version.Outbox
	handler, ok := adapter.New(&c, &o).Handlers()[p.Name]
	if !ok {
		t.Fatalf("no handler registered for %q", p.Name)
	}
	if err := handler.Handle(t.Context(), p); err != nil {
		t.Fatalf("Handle(%s): %v", p.Name, err)
	}

	return c.Events(1)
}

// only asserts that exactly one event was produced and returns it.
func only(t *testing.T, events []event.Event) event.Event {
	t.Helper()

	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}

	return events[0]
}

func TestKeepAliveIsHandledInBothStates(t *testing.T) {
	t.Parallel()

	cases := map[string]protocol.Packet{
		"play":          clientbound("keep_alive", &gen.PlayClientboundKeepAlive{KeepAliveID: 7}),
		"configuration": configuration("keep_alive", &gen.ConfigurationClientboundKeepAlive{KeepAliveID: 7}),
	}

	for state, packet := range cases {
		t.Run(state, func(t *testing.T) {
			t.Parallel()

			ponged, ok := only(t, handle(t, packet)).(event.KeepAlivePonged)
			if !ok {
				t.Fatalf("%s keepalive produced the wrong event", state)
			}
			if ponged.ID != 7 {
				t.Errorf("keepalive ID is %d, want 7", ponged.ID)
			}
		})
	}
}

func TestCustomPayloadIsHandledInBothStates(t *testing.T) {
	t.Parallel()

	data := []byte{1, 2, 3}
	cases := map[string]protocol.Packet{
		"play": clientbound("custom_payload",
			&gen.PlayClientboundCustomPayload{Channel: "minecraft:brand", Data: data}),
		"configuration": configuration("custom_payload",
			&gen.ConfigurationClientboundCustomPayload{Channel: "minecraft:brand", Data: data}),
	}

	for state, packet := range cases {
		t.Run(state, func(t *testing.T) {
			t.Parallel()

			received, ok := only(t, handle(t, packet)).(event.CustomPayloadReceived)
			if !ok {
				t.Fatalf("%s custom payload produced the wrong event", state)
			}
			if received.Channel != "minecraft:brand" {
				t.Errorf("channel is %q", received.Channel)
			}
			if len(received.Payload) != 3 {
				t.Errorf("payload is %d bytes, want 3", len(received.Payload))
			}
		})
	}
}

func TestConfigurationDisconnectIsReported(t *testing.T) {
	t.Parallel()

	packet := configuration(configurationDisconnectName, &gen.ConfigurationClientboundDisconnect{})

	disconnected, ok := only(t, handle(t, packet)).(event.Disconnected)
	if !ok {
		t.Fatal("a configuration disconnect produced the wrong event")
	}
	if disconnected.State != string(gen.StateConfiguration) {
		t.Errorf("disconnect reports state %q, want configuration", disconnected.State)
	}
	if disconnected.Source != event.DisconnectByServer {
		t.Errorf("source is %q, want server", disconnected.Source)
	}
	// The reason is a structured chat component in 775. Rendering one to
	// text is a presentation decision this package does not make.
	if disconnected.Reason != "" {
		t.Errorf("reason is %q, want empty for a component reason", disconnected.Reason)
	}
}

func TestPlayKickIsReported(t *testing.T) {
	t.Parallel()

	packet := clientbound("kick_disconnect", &gen.PlayClientboundKickDisconnect{})

	disconnected, ok := only(t, handle(t, packet)).(event.Disconnected)
	if !ok {
		t.Fatal("a play kick produced the wrong event")
	}
	if disconnected.State != string(gen.StatePlay) {
		t.Errorf("disconnect reports state %q, want play", disconnected.State)
	}
}

func TestTransferReportsItsDestination(t *testing.T) {
	t.Parallel()

	packet := clientbound("transfer", &gen.PlayClientboundTransfer{Host: "elsewhere.test", Port: 25565})

	transfer, ok := only(t, handle(t, packet)).(event.TransferRequested)
	if !ok {
		t.Fatal("transfer produced the wrong event")
	}
	if transfer.Host != "elsewhere.test" || transfer.Port != 25565 {
		t.Errorf("transfer names %s:%d", transfer.Host, transfer.Port)
	}
}

func TestATransferPortOutsideTheRangeIsZero(t *testing.T) {
	t.Parallel()

	// The wire type is wider than a port. Wrapping the number would name a
	// real host on a port nobody asked for.
	packet := configuration("transfer", &gen.ConfigurationClientboundTransfer{Host: "h", Port: 70000})

	transfer, _ := only(t, handle(t, packet)).(event.TransferRequested)
	if transfer.Port != 0 {
		t.Errorf("port is %d, want 0 for an out-of-range value", transfer.Port)
	}
}

func TestResourcePackOfferCarriesItsTerms(t *testing.T) {
	t.Parallel()

	packet := configuration("add_resource_pack", &gen.ConfigurationClientboundAddResourcePack{
		URL: "https://example.test/pack.zip", Hash: "abc", Forced: true,
	})

	offered, ok := only(t, handle(t, packet)).(event.ResourcePackOffered)
	if !ok {
		t.Fatal("a pack offer produced the wrong event")
	}
	if offered.URL != "https://example.test/pack.zip" || offered.Hash != "abc" {
		t.Errorf("offer is %+v", offered)
	}
	if !offered.Required {
		t.Error("a forced pack is not reported as required")
	}
}

func TestRevokingEveryPackHasNoUUID(t *testing.T) {
	t.Parallel()

	// An absent UUID means every pack.
	packet := clientbound("remove_resource_pack", &gen.PlayClientboundRemoveResourcePack{})

	revoked, ok := only(t, handle(t, packet)).(event.ResourcePackRevoked)
	if !ok {
		t.Fatal("a pack revocation produced the wrong event")
	}
	if revoked.UUID != "" {
		t.Errorf("UUID is %q, want empty for all packs", revoked.UUID)
	}
}

func TestStoredCookieReportsItsSizeAndNotItsBytes(t *testing.T) {
	t.Parallel()

	packet := clientbound("store_cookie", &gen.PlayClientboundStoreCookie{
		Key: "session", Value: gen.ByteArray{1, 2, 3, 4},
	})

	stored, ok := only(t, handle(t, packet)).(event.CookieStored)
	if !ok {
		t.Fatal("a stored cookie produced the wrong event")
	}
	if stored.Key != "session" {
		t.Errorf("key is %q", stored.Key)
	}
	// A cookie is a server-issued token. The event says how big it was, not
	// what it said.
	if stored.Bytes != 4 {
		t.Errorf("size is %d, want 4", stored.Bytes)
	}
}

func TestCookieRequestNamesItsKey(t *testing.T) {
	t.Parallel()

	packet := configuration("cookie_request", &gen.ConfigurationClientboundCookieRequest{Cookie: "session"})

	requested, ok := only(t, handle(t, packet)).(event.CookieRequested)
	if !ok {
		t.Fatal("a cookie request produced the wrong event")
	}
	if requested.Key != "session" {
		t.Errorf("key is %q, want session", requested.Key)
	}
}

func TestServerDescribingPacketsShareOneEvent(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		packet protocol.Packet
		kind   string
	}{
		"server data": {
			clientbound("server_data", &gen.PlayClientboundServerData{}), "server_data",
		},
		"server links": {
			configuration("server_links", &gen.ConfigurationClientboundServerLinks{
				Links: make([]gen.ConfigurationClientboundServerLinksLinksItem, 2),
			}), "server_links",
		},
		"feature flags": {
			configuration("feature_flags", &gen.ConfigurationClientboundFeatureFlags{
				Features: []string{"minecraft:vanilla"},
			}), "feature_flags",
		},
		"report details": {
			clientbound("custom_report_details", &gen.PlayClientboundCustomReportDetails{
				Details: []gen.PlayClientboundCustomReportDetailsDetailsItem{{Key: "k", Value: "v"}},
			}), "custom_report_details",
		},
		"low disk space": {
			clientbound("low_disk_space_warning", &gen.PlayClientboundLowDiskSpaceWarning{}),
			"low_disk_space_warning",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			changed, ok := only(t, handle(t, tc.packet)).(event.ServerMetadataChanged)
			if !ok {
				t.Fatalf("%s produced the wrong event", name)
			}
			if changed.Kind != tc.kind {
				t.Errorf("kind is %q, want %q", changed.Kind, tc.kind)
			}
		})
	}
}

func TestServerLinksCountsWhatArrived(t *testing.T) {
	t.Parallel()

	packet := clientbound("server_links", &gen.PlayClientboundServerLinks{
		Links: make([]gen.PlayClientboundServerLinksLinksItem, 3),
	})

	changed, _ := only(t, handle(t, packet)).(event.ServerMetadataChanged)
	if changed.Value["count"] != "3" {
		t.Errorf("count is %q, want 3", changed.Value["count"])
	}
}

func TestReportDetailsCarryEveryEntry(t *testing.T) {
	t.Parallel()

	packet := configuration("custom_report_details", &gen.ConfigurationClientboundCustomReportDetails{
		Details: []gen.ConfigurationClientboundCustomReportDetailsDetailsItem{
			{Key: "server", Value: "paper"},
			{Key: "build", Value: "74"},
		},
	})

	changed, _ := only(t, handle(t, packet)).(event.ServerMetadataChanged)
	if changed.Value["server"] != "paper" || changed.Value["build"] != "74" {
		t.Errorf("details are %v", changed.Value)
	}
}

func TestEveryHandlerIgnoresAValueItDoesNotRecognize(t *testing.T) {
	t.Parallel()

	var c event.Collector
	var o version.Outbox
	a := adapter.New(&c, &o)

	// An undecodable packet arrives as an UnknownPacket under the same name.
	// A bare type assertion would panic on it.
	for name, handler := range a.Handlers() {
		packet := clientbound(name, &protocol.UnknownPacket{Payload: []byte{0}})
		if err := handler.Handle(t.Context(), packet); err != nil {
			t.Errorf("%s handler returned %v on a foreign value, want nil", name, err)
		}
	}

	if got := c.Len(); got != 0 {
		t.Errorf("handlers produced %d events from foreign values, want 0", got)
	}
}

func TestTheBundleDelimiterHasNoHandler(t *testing.T) {
	t.Parallel()

	var c event.Collector
	var o version.Outbox
	if _, registered := adapter.New(&c, &o).Handlers()[adapter.BundleDelimiter]; registered {
		t.Error("the delimiter has a handler; it is a framing marker, not an event source")
	}
}

func TestRegistryDataIsNotHandledHere(t *testing.T) {
	t.Parallel()

	// registry.data_received is M7's to implement. This milestone must not
	// emit a name for which it has defined no struct.
	var c event.Collector
	var o version.Outbox
	if _, registered := adapter.New(&c, &o).Handlers()[registryDataName]; registered {
		t.Fatal("M6.3 registered a handler for registry data, which is M7's")
	}
}

// answers dispatches one packet and returns what the adapter queued to send.
func answers(t *testing.T, p protocol.Packet) []protocol.Packet {
	t.Helper()

	var c event.Collector
	var o version.Outbox
	handler, ok := adapter.New(&c, &o).Handlers()[p.Name]
	if !ok {
		t.Fatalf("no handler registered for %q", p.Name)
	}
	if err := handler.Handle(t.Context(), p); err != nil {
		t.Fatalf("Handle(%s): %v", p.Name, err)
	}

	return o.Drain()
}

func TestKeepAliveIsAnsweredInTheStateItArrivedIn(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		packet protocol.Packet
		state  protocol.State
	}{
		"play": {
			clientbound("keep_alive", &gen.PlayClientboundKeepAlive{KeepAliveID: 7}),
			gen.StatePlay,
		},
		"configuration": {
			configuration("keep_alive", &gen.ConfigurationClientboundKeepAlive{KeepAliveID: 7}),
			gen.StateConfiguration,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			queued := answers(t, tc.packet)
			if len(queued) != 1 {
				t.Fatalf("queued %d answers, want 1", len(queued))
			}
			if queued[0].State != tc.state {
				t.Errorf("answer is addressed to %q, want %q", queued[0].State, tc.state)
			}
			if queued[0].Direction != protocol.DirectionServerbound {
				t.Errorf("answer is %v, want serverbound", queued[0].Direction)
			}

			switch value := queued[0].Value.(type) {
			case *gen.PlayServerboundKeepAlive:
				if value.KeepAliveID != 7 {
					t.Errorf("answer carries ID %d, want 7", value.KeepAliveID)
				}
			case *gen.ConfigurationServerboundKeepAlive:
				if value.KeepAliveID != 7 {
					t.Errorf("answer carries ID %d, want 7", value.KeepAliveID)
				}
			default:
				t.Fatalf("answer is %T, want a serverbound keepalive", queued[0].Value)
			}
		})
	}
}

func TestKnownPacksIsAnsweredWithAnEmptyList(t *testing.T) {
	t.Parallel()

	// A 26.1 server sends no registry data and never finishes configuration
	// until this is answered, and a connection that never answers looks
	// perfectly healthy while it waits forever.
	packet := configuration("select_known_packs", &gen.ConfigurationClientboundSelectKnownPacks{
		Packs: make([]gen.ConfigurationClientboundSelectKnownPacksPacksItem, 3),
	})

	queued := answers(t, packet)
	if len(queued) != 1 {
		t.Fatalf("queued %d answers, want 1", len(queued))
	}
	answer, ok := queued[0].Value.(*gen.ConfigurationServerboundSelectKnownPacks)
	if !ok {
		t.Fatalf("answer is %T, want *ConfigurationServerboundSelectKnownPacks", queued[0].Value)
	}
	// A headless client ships no pack data, so it holds none of them, and
	// claiming otherwise makes the server skip registries it needs.
	if len(answer.Packs) != 0 {
		t.Errorf("answer claims %d known packs, want none", len(answer.Packs))
	}
	if queued[0].Name != "select_known_packs" {
		t.Errorf("answer is named %q", queued[0].Name)
	}
}

func TestFinishConfigurationIsAcknowledged(t *testing.T) {
	t.Parallel()

	packet := configuration("finish_configuration", &gen.ConfigurationClientboundFinishConfiguration{})

	queued := answers(t, packet)
	if len(queued) != 1 {
		t.Fatalf("queued %d answers, want 1", len(queued))
	}
	if _, ok := queued[0].Value.(*gen.ConfigurationServerboundFinishConfiguration); !ok {
		t.Fatalf("answer is %T, want the finish acknowledgement", queued[0].Value)
	}
	if queued[0].Name != "finish_configuration" {
		t.Errorf("answer is named %q", queued[0].Name)
	}
}

func TestObservedConfigurationPacketsQueueNoAnswer(t *testing.T) {
	t.Parallel()

	// Everything else in configuration is content, not a question. Answering
	// one would put a packet on the wire the server never asked for.
	quiet := []protocol.Packet{
		configuration("feature_flags", &gen.ConfigurationClientboundFeatureFlags{}),
		configuration("add_resource_pack", &gen.ConfigurationClientboundAddResourcePack{}),
		configuration("custom_payload", &gen.ConfigurationClientboundCustomPayload{}),
		configuration("cookie_request", &gen.ConfigurationClientboundCookieRequest{}),
	}

	for _, packet := range quiet {
		if queued := answers(t, packet); len(queued) != 0 {
			t.Errorf("%s queued %d answers, want none", packet.Name, len(queued))
		}
	}
}

func TestProtocol775StopsAtConfiguration(t *testing.T) {
	t.Parallel()

	var c event.Collector
	var o version.Outbox
	if got := adapter.New(&c, &o).LoginTerminalState(); got != gen.StateConfiguration {
		t.Errorf("terminal state is %q, want configuration", got)
	}
}

// TestEveryPositionIsConfirmed is the regression for the defect that let the
// orbit example walk nowhere against a vanilla 26.1.2 server.
//
// The server places and corrects the player with the same packet, each
// carrying a teleport ID, and discards the client's movement until that ID
// comes back. Confirming only the first one is not a partial fix: it is
// accepted at spawn and ignored from the first correction onward, which looks
// healthy from the client's side because nothing errors — the bot simply never
// arrives anywhere.
//
// The second and third cases are the point. A test that sends one position
// passes against the code that had the bug.
func TestEveryPositionIsConfirmed(t *testing.T) {
	t.Parallel()

	for _, id := range []int32{9, 10, 11} {
		queued := answers(t, clientbound("position",
			&gen.PlayClientboundPosition{TeleportID: id, X: 1, Y: 2, Z: 3}))
		if len(queued) != 1 {
			t.Fatalf("teleport %d queued %d answers, want 1", id, len(queued))
		}

		confirm, ok := queued[0].Value.(*gen.PlayServerboundTeleportConfirm)
		if !ok {
			t.Fatalf("answer is %T, want *PlayServerboundTeleportConfirm", queued[0].Value)
		}
		if confirm.TeleportID != id {
			t.Errorf("confirmation carries teleport ID %d, want %d", confirm.TeleportID, id)
		}
		if queued[0].Name != "teleport_confirm" {
			t.Errorf("confirmation is named %q, want teleport_confirm", queued[0].Name)
		}
		if queued[0].State != gen.StatePlay {
			t.Errorf("confirmation is addressed to %q, want play", queued[0].State)
		}
		if queued[0].Direction != protocol.DirectionServerbound {
			t.Errorf("confirmation is %v, want serverbound", queued[0].Direction)
		}
	}
}

// TestARelativeCorrectionIsStillConfirmed pins that the handler does not read
// the flags.
//
// Whether a correction's coordinates are absolute or relative decides how the
// world resolves the position, and the world decides it. The confirmation
// echoes an ID, and a server waiting on one discards movement either way, so a
// handler that answered only absolute positions would reintroduce the same
// silence for a server that corrects relatively.
func TestARelativeCorrectionIsStillConfirmed(t *testing.T) {
	t.Parallel()

	queued := answers(t, clientbound("position", &gen.PlayClientboundPosition{
		TeleportID: 4,
		Flags:      gen.PlayClientboundPositionFlagsFlags{X: true, Y: true, Z: true},
	}))
	if len(queued) != 1 {
		t.Fatalf("queued %d answers, want 1", len(queued))
	}
	confirm, ok := queued[0].Value.(*gen.PlayServerboundTeleportConfirm)
	if !ok || confirm.TeleportID != 4 {
		t.Fatalf("answer is %T, want a confirmation for teleport 4", queued[0].Value)
	}
}
