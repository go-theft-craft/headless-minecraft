package client

import (
	"testing"

	protocol "github.com/go-theft-craft/minecraft-protocol"
	gen "github.com/go-theft-craft/minecraft-protocol/generated/java/v26_1"

	"github.com/go-theft-craft/headless-minecraft/event"
	"github.com/go-theft-craft/headless-minecraft/version"
	"github.com/go-theft-craft/headless-minecraft/version/java"
)

// This file drives the real protocol 775 profile through a scripted
// configuration phase and into play.
//
// It lives here rather than in an end-to-end lane because no 775 server can be
// stood up in this repository: the shared login.Acceptor is written against
// the v1_8 generated types. What can be driven is everything after the login
// negotiator hands the connection over, which is exactly the part the client
// now owns.

// script builds a receiver over decoded packets in the order a 26.1 server
// sends them.
func script(packets ...protocol.Packet) *sliceReceiver {
	return &sliceReceiver{packets: packets}
}

func configurationPacket(name string, value any) protocol.Packet {
	return protocol.Packet{
		State:     gen.StateConfiguration,
		Direction: protocol.DirectionClientbound,
		Name:      name,
		Value:     value,
	}
}

func playPacket(name string, value any) protocol.Packet {
	return protocol.Packet{
		State:     gen.StatePlay,
		Direction: protocol.DirectionClientbound,
		Name:      name,
		Value:     value,
	}
}

// runProfile drives one profile's own adapter, readiness rule, collector, and
// outbox over a scripted server.
func runProfile(t *testing.T, profile version.WireProfile, receiver *sliceReceiver) (
	*Client, *recordingSender, []event.Event,
) {
	t.Helper()

	bot := &Client{loop: make(chan struct{}), done: make(chan struct{})}
	sub, err := bot.events.subscribe(event.DomainSession|event.DomainRaw, 256)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	batcher, err := version.NewBatcher(java.BundleDelimiter(profile.ID), 64)
	if err != nil {
		t.Fatalf("NewBatcher: %v", err)
	}

	sender := &recordingSender{}
	err = bot.runLoop(
		t.Context(), receiver, sender,
		newTableDispatcher(profile.Adapter.Handlers()),
		batcher, profile.Collector, profile.Outbox, profile.Readiness,
		make(chan version.ReadyState, 1),
	)
	if err != nil {
		t.Fatalf("runLoop: %v", err)
	}
	_ = sub.Close()

	var events []event.Event
	for e := range sub.C() {
		events = append(events, e)
	}

	return bot, sender, events
}

func TestConfigurationIsDrivenByTheClientIntoPlay(t *testing.T) {
	t.Parallel()

	profile := java.Current()
	_, sender, events := runProfile(t, profile, script(
		configurationPacket("select_known_packs", &gen.ConfigurationClientboundSelectKnownPacks{
			Packs: make([]gen.ConfigurationClientboundSelectKnownPacksPacksItem, 2),
		}),
		configurationPacket("registry_data", &gen.ConfigurationClientboundRegistryData{
			ID: "minecraft:dimension_type",
		}),
		configurationPacket("feature_flags", &gen.ConfigurationClientboundFeatureFlags{
			Features: []string{"minecraft:vanilla"},
		}),
		configurationPacket("add_resource_pack", &gen.ConfigurationClientboundAddResourcePack{
			URL: "https://example.test/pack.zip", Forced: true,
		}),
		configurationPacket("keep_alive", &gen.ConfigurationClientboundKeepAlive{KeepAliveID: 11}),
		configurationPacket("finish_configuration", &gen.ConfigurationClientboundFinishConfiguration{}),
		playPacket("login", &gen.PlayClientboundLogin{
			EntityID:   42,
			WorldState: gen.SpawnInfo{Name: "minecraft:overworld", Gamemode: "survival"},
		}),
		playPacket("position", &gen.PlayClientboundPosition{TeleportID: 9}),
	))

	// The three things the client owes a 26.1 server before it can play, in
	// the order the server asks for them, plus the teleport confirmation that
	// completes the placement.
	var sent []string
	for _, p := range sender.sent {
		sent = append(sent, p.Name)
	}
	want := []string{"select_known_packs", "keep_alive", "finish_configuration", "teleport_confirm"}
	if len(sent) != len(want) {
		t.Fatalf("client sent %v, want %v", sent, want)
	}
	for i := range want {
		if sent[i] != want[i] {
			t.Fatalf("client sent %v, want %v", sent, want)
		}
	}

	// Configuration content reached handlers, which is the whole point of the
	// client owning the phase: under a negotiator that ran to play, every one
	// of these was consumed inside the login sequence.
	names := map[event.Name]int{}
	for _, e := range events {
		names[e.Name()]++
	}
	for _, name := range []event.Name{
		event.SessionServerMetadataChanged,
		event.SessionResourcePackOffered,
		event.SessionKeepAlivePonged,
		event.SessionReady,
	} {
		if names[name] == 0 {
			t.Errorf("no %s event was published from the configuration phase", name)
		}
	}
}

func TestRegistryDataReachesTheLoopUnhandled(t *testing.T) {
	t.Parallel()

	// M7 owns registry.data_received. What this milestone owes it is the
	// packet arriving in the loop at all, which a raw subscriber can see.
	profile := java.Current()
	_, _, events := runProfile(t, profile, script(
		configurationPacket("registry_data", &gen.ConfigurationClientboundRegistryData{
			ID: "minecraft:worldgen/biome",
		}),
	))

	var seen bool
	for _, e := range events {
		if packet, ok := e.(event.PacketReceived); ok && packet.Packet == "registry_data" {
			seen = true
			if packet.State != string(gen.StateConfiguration) {
				t.Errorf("registry data arrived in state %q, want configuration", packet.State)
			}
		}
	}
	if !seen {
		t.Fatal("registry data never reached the loop")
	}
}

func TestProtocol47NeedsNoConfigurationPhase(t *testing.T) {
	t.Parallel()

	// The same loop, the same seam, and nothing to take over: protocol 47's
	// login ends at success and its first play packet is the login.
	profile := java.Java1_8()
	if got := profile.Adapter.LoginTerminalState(); got != "" {
		t.Fatalf("protocol 47 stops at %q, want nowhere", got)
	}
}
