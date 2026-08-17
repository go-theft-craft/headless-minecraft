// Package fixture serves a scripted Minecraft server over loopback.
//
// It exists so the client's connection tests drive real codecs rather than
// hand-built bytes: the fixture runs the same managed stream, the same
// generated descriptor, and the same login acceptor a real server would.
//
// It is internal to the client package's tests and is not a public contract.
package fixture

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	protocol "github.com/go-theft-craft/minecraft-protocol"
	gen "github.com/go-theft-craft/minecraft-protocol/generated/java/v1_8"
	"github.com/go-theft-craft/minecraft-protocol/login"
)

// Script says how far the fixture server plays its part.
type Script struct {
	// ThroughReady sends the play login and a placing position. When false
	// the fixture completes login and then sends nothing, so a client
	// reaches play but is never placed.
	ThroughReady bool
	// ThenKick sends a disconnect with this reason after the client is
	// placed. Empty sends none.
	ThenKick string
	// ThenDropConn closes the transport without a disconnect packet.
	ThenDropConn bool
	// ThenWorld sends a world script after the client is placed: a chunk, two
	// entities, a move, a container, and a weather change in one wave, then a
	// second wave that takes all of it away. It is what the observed-world
	// end-to-end lane asserts against.
	//
	// The fixture speaks protocol 47 only. Serving 775 needs a server-side
	// login and the shared login.Acceptor is written against the v1_8
	// generated types, which is the same limit M6.3 recorded.
	ThenWorld bool
}

// serverKey is generated once per test binary. Generating an RSA key costs
// more than everything else the fixture does, and no test inspects it: the
// acceptor is offline, so the key is never used for an exchange.
var serverKey = sync.OnceValues(func() (*rsa.PrivateKey, error) {
	return rsa.GenerateKey(rand.Reader, 2048)
})

// Start listens on loopback and serves one connection. The returned stop
// function closes the listener and waits for the server goroutine.
func Start(t *testing.T, script Script) (addr string, stop func()) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		defer close(done)
		serve(ctx, t, listener, script)
	}()

	return listener.Addr().String(), func() {
		cancel()
		_ = listener.Close()
		<-done
	}
}

func serve(ctx context.Context, t *testing.T, listener net.Listener, script Script) {
	t.Helper()

	conn, err := listener.Accept()
	if err != nil {
		// The listener closed before a client arrived, which is how a test
		// that never connects ends.
		return
	}
	defer func() { _ = conn.Close() }()

	stream, err := serveStream(ctx, conn)
	if err != nil {
		t.Errorf("fixture: %v", err)

		return
	}
	defer func() { _ = stream.Close() }()

	if err := play(ctx, stream, conn, script); err != nil && ctx.Err() == nil {
		t.Errorf("fixture: %v", err)
	}

	<-ctx.Done()
}

// serveStream completes the handshake and the login exchange, leaving the
// stream in play.
func serveStream(ctx context.Context, conn net.Conn) (*protocol.Stream, error) {
	limits, err := protocol.NewLimits()
	if err != nil {
		return nil, err
	}
	session, err := gen.Protocol().NewSession(protocol.RoleServer, limits)
	if err != nil {
		return nil, err
	}

	stream, err := protocol.NewStream(session, protocol.Transport{
		Reader:    conn,
		Writer:    conn,
		Interrupt: conn.Close,
	})
	if err != nil {
		return nil, err
	}
	if err := stream.Start(ctx); err != nil {
		return nil, err
	}

	// The handshake carries the next state, and the stream applies that
	// transition itself.
	if _, err := stream.Read(ctx); err != nil {
		return nil, err
	}

	key, err := serverKey()
	if err != nil {
		return nil, err
	}
	acceptor, err := login.NewAcceptor(key, login.WithCompressionThreshold(-1))
	if err != nil {
		return nil, err
	}
	if _, err := acceptor.Accept(ctx, stream); err != nil {
		return nil, err
	}

	return stream, nil
}

func play(ctx context.Context, stream *protocol.Stream, conn net.Conn, script Script) error {
	if !script.ThroughReady {
		return nil
	}

	if err := write(ctx, stream, "login", &gen.PlayClientboundLogin{
		EntityID: 42, GameMode: 0, Dimension: 0, LevelType: "default",
	}); err != nil {
		return err
	}
	if err := write(ctx, stream, "position", &gen.PlayClientboundPosition{
		X: 1, Y: 64, Z: 2,
	}); err != nil {
		return err
	}

	if script.ThenWorld {
		if err := world(ctx, stream); err != nil {
			return err
		}
	}

	if !script.ThenDropConn && script.ThenKick == "" {
		return nil
	}

	// Wait for the client's acknowledgement so the disconnect or the drop
	// lands after it is placed rather than racing the placement.
	if _, err := stream.Read(ctx); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}

	if script.ThenDropConn {
		// No disconnect packet: the transport simply ends, which is the
		// case the client must report as a transport loss. A zero linger
		// sends a reset rather than a graceful close, so the client sees the
		// loss immediately.
		if tcp, ok := conn.(*net.TCPConn); ok {
			_ = tcp.SetLinger(0)
		}

		return conn.Close()
	}

	// Shutdown sends the disconnect packet itself and then hangs up, which is
	// what a real server does. Writing a kick first would send the reason
	// twice and tell the client the session ended twice.
	return stream.Shutdown(ctx, script.ThenKick)
}

// world sends the observed-world script in two waves, so the lane can assert
// that a store fills and then empties again.
//
// Every packet is a separate write, and protocol 47 has no bundle delimiter,
// so the client sees one batch per packet and one revision per batch. That is
// the property the lane exists to check.
func world(ctx context.Context, stream *protocol.Stream) error {
	// A column of one section, every block the same state, so a block lookup
	// has an answer that could not have come from a zero value.
	section := make([]byte, 8192)
	for i := range 4096 {
		section[i*2] = 0x20
	}

	arriving := []struct {
		name  string
		value playPacket
	}{
		{"map_chunk", &gen.PlayClientboundMapChunk{
			X: 0, Z: 0, GroundUp: true, BitMap: 0x0001, ChunkData: section,
		}},
		{"spawn_entity_living", &gen.PlayClientboundSpawnEntityLiving{
			EntityID: 7, Type: 54, X: 32, Y: 2048, Z: 64,
		}},
		{"spawn_entity_living", &gen.PlayClientboundSpawnEntityLiving{
			EntityID: 8, Type: 51, X: 64, Y: 2048, Z: 96,
		}},
		{"rel_entity_move", &gen.PlayClientboundRelEntityMove{EntityID: 7, DX: 32}},
		{"open_window", &gen.PlayClientboundOpenWindow{
			WindowID: 3, InventoryType: "minecraft:chest",
			WindowTitle: `{"text":"Chest"}`, SlotCount: 27,
		}},
		{"game_state_change", &gen.PlayClientboundGameStateChange{Reason: 2}},
	}
	for _, packet := range arriving {
		if err := write(ctx, stream, packet.name, packet.value); err != nil {
			return err
		}
	}

	leaving := []struct {
		name  string
		value playPacket
	}{
		{"entity_destroy", &gen.PlayClientboundEntityDestroy{EntityIds: []int32{7, 8}}},
		{"close_window", &gen.PlayClientboundCloseWindow{WindowID: 3}},
		// Protocol 47 unloads a column by sending it with no sections.
		{"map_chunk", &gen.PlayClientboundMapChunk{X: 0, Z: 0, GroundUp: true}},
	}
	for _, packet := range leaving {
		if err := write(ctx, stream, packet.name, packet.value); err != nil {
			return err
		}
	}

	return nil
}

type playPacket interface {
	PacketID() int32
}

func write(ctx context.Context, stream *protocol.Stream, name string, value playPacket) error {
	writeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	return stream.Write(writeCtx, protocol.Packet{
		State:     gen.StatePlay,
		Direction: protocol.DirectionClientbound,
		ID:        value.PacketID(),
		Name:      name,
		Value:     value,
	})
}
