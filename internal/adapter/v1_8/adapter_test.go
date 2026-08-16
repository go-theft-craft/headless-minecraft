package v1_8_test

import (
	"errors"
	"testing"

	protocol "github.com/go-theft-craft/minecraft-protocol"
	gen "github.com/go-theft-craft/minecraft-protocol/generated/java/v1_8"

	"github.com/go-theft-craft/headless-minecraft/event"
	adapter "github.com/go-theft-craft/headless-minecraft/internal/adapter/v1_8"
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

func TestKeepAliveProducesAPongedEvent(t *testing.T) {
	t.Parallel()

	var c event.Collector
	a := adapter.New(&c)

	handler, ok := a.Handlers()["keep_alive"]
	if !ok {
		t.Fatal("no handler registered for keep_alive")
	}
	packet := clientbound("keep_alive", &gen.PlayClientboundKeepAlive{KeepAliveID: 7})
	if err := handler.Handle(t.Context(), packet); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	events := c.Events(1)
	if len(events) != 1 {
		t.Fatalf("got %d events, want one keepalive_ponged", len(events))
	}
	ponged, ok := events[0].(event.KeepAlivePonged)
	if !ok {
		t.Fatalf("got %T, want event.KeepAlivePonged", events[0])
	}
	if ponged.ID != 7 {
		t.Errorf("keepalive ID is %d, want 7", ponged.ID)
	}
}

func TestCustomPayloadCopiesItsPayload(t *testing.T) {
	t.Parallel()

	var c event.Collector
	a := adapter.New(&c)

	data := []byte{1, 2, 3}
	packet := clientbound("custom_payload", &gen.PlayClientboundCustomPayload{Channel: "MC|Brand", Data: data})
	if err := a.Handlers()["custom_payload"].Handle(t.Context(), packet); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	// The decoded packet is the stream's, not the subscriber's. An event that
	// aliased it would change under a reader when the buffer is reused.
	data[0] = 9

	received, ok := c.Events(1)[0].(event.CustomPayloadReceived)
	if !ok {
		t.Fatalf("got %T, want event.CustomPayloadReceived", c.Events(1)[0])
	}
	if received.Channel != "MC|Brand" {
		t.Errorf("channel is %q, want MC|Brand", received.Channel)
	}
	if received.Payload[0] != 1 {
		t.Error("event payload aliases the packet's buffer")
	}
}

func TestKickDisconnectReportsTheServerAsTheSource(t *testing.T) {
	t.Parallel()

	var c event.Collector
	a := adapter.New(&c)

	packet := clientbound("kick_disconnect", &gen.PlayClientboundKickDisconnect{Reason: "kicked"})
	if err := a.Handlers()["kick_disconnect"].Handle(t.Context(), packet); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	disconnected, ok := c.Events(1)[0].(event.Disconnected)
	if !ok {
		t.Fatalf("got %T, want event.Disconnected", c.Events(1)[0])
	}
	if disconnected.Source != event.DisconnectByServer {
		t.Errorf("source is %q, want server", disconnected.Source)
	}
	if disconnected.Reason != "kicked" {
		t.Errorf("reason is %q, want kicked", disconnected.Reason)
	}
}

func TestHandlersIgnoreAPacketOfTheWrongType(t *testing.T) {
	t.Parallel()

	var c event.Collector
	a := adapter.New(&c)

	// An unknown packet retains its payload rather than a decoded value. A
	// handler that type-asserted without checking would panic on it.
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

func TestReadinessNeedsLoginThenPosition(t *testing.T) {
	t.Parallel()

	rule := adapter.Readiness()

	state, reply, err := rule.Observe(version.Batch{Packets: []protocol.Packet{
		clientbound("position", &gen.PlayClientboundPosition{X: 1, Y: 2, Z: 3}),
	}})
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if state.Ready {
		t.Fatal("rule reported ready before the login packet arrived")
	}
	if len(reply) != 0 {
		t.Fatal("rule replied to a position it had no login for")
	}

	state, _, err = rule.Observe(version.Batch{Packets: []protocol.Packet{
		clientbound("login", &gen.PlayClientboundLogin{EntityID: 42, GameMode: 1}),
	}})
	if err != nil || state.Ready {
		t.Fatalf("login alone gave ready=%v err=%v, want false/nil", state.Ready, err)
	}
	if state.EntityID != 42 {
		t.Errorf("entity ID is %d, want 42", state.EntityID)
	}
	if state.GameMode != 1 {
		t.Errorf("game mode is %d, want 1", state.GameMode)
	}
	if state.Dimension != "minecraft:overworld" {
		t.Errorf("dimension is %q, want minecraft:overworld", state.Dimension)
	}

	state, reply, err = rule.Observe(version.Batch{Packets: []protocol.Packet{
		clientbound("position", &gen.PlayClientboundPosition{X: 1, Y: 2, Z: 3, Yaw: 4, Pitch: 5}),
	}})
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if !state.Ready {
		t.Fatal("rule did not report ready after login and position")
	}
	if len(reply) != 1 {
		t.Fatalf("rule sent %d packets, want exactly one position-look echo", len(reply))
	}

	echo, ok := reply[0].Value.(*gen.PlayServerboundPositionLook)
	if !ok {
		t.Fatalf("reply is %T, want *PlayServerboundPositionLook", reply[0].Value)
	}
	if echo.X != 1 || echo.Y != 2 || echo.Z != 3 || echo.Yaw != 4 || echo.Pitch != 5 {
		t.Errorf("echo does not match the placing position: %+v", echo)
	}
	if reply[0].Direction != protocol.DirectionServerbound || reply[0].State != gen.StatePlay {
		t.Errorf("echo is addressed %v/%v, want play/serverbound", reply[0].State, reply[0].Direction)
	}
	if reply[0].Name != "position_look" {
		t.Errorf("echo is named %q, want position_look", reply[0].Name)
	}
}

func TestReadinessNamesEachDimension(t *testing.T) {
	t.Parallel()

	cases := map[int8]string{
		-1: "minecraft:the_nether",
		0:  "minecraft:overworld",
		1:  "minecraft:the_end",
	}

	for id, want := range cases {
		rule := adapter.Readiness()
		state, _, err := rule.Observe(version.Batch{Packets: []protocol.Packet{
			clientbound("login", &gen.PlayClientboundLogin{Dimension: id}),
		}})
		if err != nil {
			t.Fatalf("Observe: %v", err)
		}
		if state.Dimension != want {
			t.Errorf("dimension %d is named %q, want %q", id, state.Dimension, want)
		}
	}
}

func TestReadinessRejectsARelativeSpawn(t *testing.T) {
	t.Parallel()

	rule := adapter.Readiness()
	_, _, _ = rule.Observe(version.Batch{Packets: []protocol.Packet{
		clientbound("login", &gen.PlayClientboundLogin{EntityID: 1}),
	}})

	// Any non-zero flag bit marks at least one field relative.
	_, _, err := rule.Observe(version.Batch{Packets: []protocol.Packet{
		clientbound("position", &gen.PlayClientboundPosition{Flags: 0x01}),
	}})
	if !errors.Is(err, version.ErrRelativeSpawn) {
		t.Fatalf("got %v, want ErrRelativeSpawn", err)
	}
}

func TestReadinessReportsReadyOnlyOnce(t *testing.T) {
	t.Parallel()

	rule := adapter.Readiness()
	_, _, _ = rule.Observe(version.Batch{Packets: []protocol.Packet{
		clientbound("login", &gen.PlayClientboundLogin{EntityID: 1}),
	}})
	_, _, _ = rule.Observe(version.Batch{Packets: []protocol.Packet{
		clientbound("position", &gen.PlayClientboundPosition{}),
	}})

	state, reply, err := rule.Observe(version.Batch{Packets: []protocol.Packet{
		clientbound("position", &gen.PlayClientboundPosition{}),
	}})
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if len(reply) != 0 {
		t.Fatal("rule echoed a second position after it was already ready")
	}
	if !state.Ready {
		t.Error("rule stopped reporting ready")
	}
}

func TestEachRuleIsIndependent(t *testing.T) {
	t.Parallel()

	first := adapter.Readiness()
	_, _, _ = first.Observe(version.Batch{Packets: []protocol.Packet{
		clientbound("login", &gen.PlayClientboundLogin{EntityID: 1}),
	}})

	// A rule carries one connection's progress. A second connection that
	// inherited it would answer a position it never saw a login for.
	second := adapter.Readiness()
	state, reply, err := second.Observe(version.Batch{Packets: []protocol.Packet{
		clientbound("position", &gen.PlayClientboundPosition{}),
	}})
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if state.Ready || len(reply) != 0 {
		t.Fatal("a fresh rule inherited another connection's progress")
	}
}

func TestAdapterIdentifiesItsProtocol(t *testing.T) {
	t.Parallel()

	var c event.Collector
	if got := adapter.New(&c).ProtocolID(); got != "java/1.8.9" {
		t.Errorf("ProtocolID is %q, want java/1.8.9", got)
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
	if value.ProtocolVersion != 47 {
		t.Errorf("protocol version is %d, want 47", value.ProtocolVersion)
	}
	if value.ServerHost != "example.test" || value.ServerPort != 25565 {
		t.Errorf("handshake addresses %s:%d", value.ServerHost, value.ServerPort)
	}
	if packet.State != gen.StateHandshaking || packet.Direction != protocol.DirectionServerbound {
		t.Errorf("handshake is addressed %q/%v", packet.State, packet.Direction)
	}
}
