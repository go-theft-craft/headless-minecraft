package v26_1_test

import (
	"errors"
	"testing"

	protocol "github.com/go-theft-craft/minecraft-protocol"
	gen "github.com/go-theft-craft/minecraft-protocol/generated/java/v26_1"

	"github.com/go-theft-craft/headless-minecraft/event"
	adapter "github.com/go-theft-craft/headless-minecraft/internal/adapter/v26_1"
	"github.com/go-theft-craft/headless-minecraft/version"
)

func clientbound(name string, value any) protocol.Packet {
	return protocol.Packet{
		State:     gen.StatePlay,
		Direction: protocol.DirectionClientbound,
		Name:      name,
		Value:     value,
	}
}

func login() protocol.Packet {
	return clientbound("login", &gen.PlayClientboundLogin{
		EntityID:   42,
		WorldState: gen.SpawnInfo{Name: "minecraft:overworld", Gamemode: "creative"},
	})
}

func TestAdapterIdentifiesItsProtocol(t *testing.T) {
	t.Parallel()

	var c event.Collector
	if got := adapter.New(&c).ProtocolID(); got != "java/26.1" {
		t.Errorf("ProtocolID is %q, want java/26.1", got)
	}
}

func TestBundleDelimiterIsTheGeneratedPacketName(t *testing.T) {
	t.Parallel()

	if adapter.BundleDelimiter != "bundle_delimiter" {
		t.Errorf("BundleDelimiter is %q, want bundle_delimiter", adapter.BundleDelimiter)
	}
}

func TestReadinessConfirmsTheTeleport(t *testing.T) {
	t.Parallel()

	rule := adapter.Readiness()

	state, reply, err := rule.Observe(version.Batch{Packets: []protocol.Packet{login()}})
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if state.Ready || len(reply) != 0 {
		t.Fatal("login alone reported ready")
	}
	if state.EntityID != 42 {
		t.Errorf("entity ID is %d, want 42", state.EntityID)
	}
	if state.Dimension != "minecraft:overworld" {
		t.Errorf("dimension is %q, want minecraft:overworld", state.Dimension)
	}
	if state.GameMode != 1 {
		t.Errorf("game mode is %d, want 1 for creative", state.GameMode)
	}

	state, reply, err = rule.Observe(version.Batch{Packets: []protocol.Packet{
		clientbound("position", &gen.PlayClientboundPosition{TeleportID: 9, X: 1, Y: 2, Z: 3}),
	}})
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if !state.Ready {
		t.Fatal("rule did not report ready after login and position")
	}
	if len(reply) != 1 {
		t.Fatalf("rule sent %d packets, want one teleport confirmation", len(reply))
	}

	confirm, ok := reply[0].Value.(*gen.PlayServerboundTeleportConfirm)
	if !ok {
		t.Fatalf("reply is %T, want *PlayServerboundTeleportConfirm", reply[0].Value)
	}
	if confirm.TeleportID != 9 {
		t.Errorf("confirmation carries teleport ID %d, want 9", confirm.TeleportID)
	}
	if reply[0].Name != "teleport_confirm" || reply[0].Direction != protocol.DirectionServerbound {
		t.Errorf("confirmation is addressed %q/%v", reply[0].Name, reply[0].Direction)
	}
}

func TestReadinessIgnoresAPositionBeforeLogin(t *testing.T) {
	t.Parallel()

	rule := adapter.Readiness()

	state, reply, err := rule.Observe(version.Batch{Packets: []protocol.Packet{
		clientbound("position", &gen.PlayClientboundPosition{TeleportID: 9}),
	}})
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if state.Ready || len(reply) != 0 {
		t.Fatal("rule confirmed a teleport it had no login for")
	}
}

func TestReadinessRejectsARelativeSpawn(t *testing.T) {
	t.Parallel()

	rule := adapter.Readiness()
	_, _, _ = rule.Observe(version.Batch{Packets: []protocol.Packet{login()}})

	_, _, err := rule.Observe(version.Batch{Packets: []protocol.Packet{
		clientbound("position", &gen.PlayClientboundPosition{
			TeleportID: 9,
			Flags:      gen.PlayClientboundPositionFlagsFlags{X: true},
		}),
	}})
	if !errors.Is(err, version.ErrRelativeSpawn) {
		t.Fatalf("got %v, want ErrRelativeSpawn", err)
	}
}

func TestReadinessConfirmsOnlyOnce(t *testing.T) {
	t.Parallel()

	rule := adapter.Readiness()
	_, _, _ = rule.Observe(version.Batch{Packets: []protocol.Packet{login()}})
	_, _, _ = rule.Observe(version.Batch{Packets: []protocol.Packet{
		clientbound("position", &gen.PlayClientboundPosition{TeleportID: 1}),
	}})

	_, reply, err := rule.Observe(version.Batch{Packets: []protocol.Packet{
		clientbound("position", &gen.PlayClientboundPosition{TeleportID: 2}),
	}})
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if len(reply) != 0 {
		t.Fatal("rule confirmed a second teleport after it was already ready")
	}
}

func TestGameModeNamesMapToTheirNumbers(t *testing.T) {
	t.Parallel()

	cases := map[string]uint8{
		"survival": 0, "creative": 1, "adventure": 2, "spectator": 3,
		"something_a_mod_invented": 0,
	}

	for name, want := range cases {
		rule := adapter.Readiness()
		state, _, err := rule.Observe(version.Batch{Packets: []protocol.Packet{
			clientbound("login", &gen.PlayClientboundLogin{
				WorldState: gen.SpawnInfo{Gamemode: name},
			}),
		}})
		if err != nil {
			t.Fatalf("Observe: %v", err)
		}
		if state.GameMode != want {
			t.Errorf("game mode %q is %d, want %d", name, state.GameMode, want)
		}
	}
}

func TestHandlersAreEmptyUntilTheLoopNeedsThem(t *testing.T) {
	t.Parallel()

	var c event.Collector
	if got := len(adapter.New(&c).Handlers()); got != 0 {
		t.Errorf("adapter registers %d handlers, want 0 for now", got)
	}
}

func TestHandshakeAsksForLogin(t *testing.T) {
	t.Parallel()

	var c event.Collector
	packet := adapter.New(&c).Handshake("example.test", 25565)

	value, ok := packet.Value.(*gen.HandshakingServerboundSetProtocol)
	if !ok {
		t.Fatalf("handshake carries %T, want *HandshakingServerboundSetProtocol", packet.Value)
	}
	if value.NextState != 2 {
		t.Errorf("next state is %d, want 2 for login", value.NextState)
	}
	if value.ProtocolVersion != 775 {
		t.Errorf("protocol version is %d, want 775", value.ProtocolVersion)
	}
	if value.ServerHost != "example.test" || value.ServerPort != 25565 {
		t.Errorf("handshake addresses %s:%d", value.ServerHost, value.ServerPort)
	}
	if packet.State != gen.StateHandshaking || packet.Direction != protocol.DirectionServerbound {
		t.Errorf("handshake is addressed %q/%v", packet.State, packet.Direction)
	}
}
