package client

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"

	protocol "github.com/go-theft-craft/minecraft-protocol"
	gen1_8 "github.com/go-theft-craft/minecraft-protocol/generated/java/v1_8"
	gen26_1 "github.com/go-theft-craft/minecraft-protocol/generated/java/v26_1"

	"github.com/go-theft-craft/headless-minecraft/version"
	"github.com/go-theft-craft/headless-minecraft/version/java"
)

// actionClient returns a client in play whose writes land in the given sender.
//
// It builds the client by hand rather than connecting: Do's contract is about
// state and serialization, and a network in the way would test neither.
func actionClient(t *testing.T, profile version.WireProfile, w sender) *Client {
	t.Helper()

	return &Client{
		profile: profile,
		writer:  w,
		inPlay:  true,
		done:    make(chan struct{}),
		loop:    make(chan struct{}),
	}
}

func TestEachActionEncodesToItsProtocol47Packet(t *testing.T) {
	t.Parallel()

	for name, test := range map[string]struct {
		action version.Action
		packet string
		want   any
	}{
		"move": {
			action: ActionMove{X: 1, Y: 2, Z: 3, OnGround: true},
			packet: "position",
			want:   &gen1_8.PlayServerboundPosition{X: 1, Y: 2, Z: 3, OnGround: true},
		},
		"look": {
			action: ActionLook{Yaw: 90, Pitch: -45, OnGround: true},
			packet: "look",
			want:   &gen1_8.PlayServerboundLook{Yaw: 90, Pitch: -45, OnGround: true},
		},
		"move and look": {
			action: ActionMoveLook{X: 1, Y: 2, Z: 3, Yaw: 90, Pitch: -45},
			packet: "position_look",
			want: &gen1_8.PlayServerboundPositionLook{
				X: 1, Y: 2, Z: 3, Yaw: 90, Pitch: -45,
			},
		},
		"ground only": {
			action: ActionGround{OnGround: true},
			packet: "flying",
			want:   &gen1_8.PlayServerboundFlying{OnGround: true},
		},
		// This protocol numbers its client commands; respawn is zero.
		"respawn": {
			action: ActionRespawn{},
			packet: "client_command",
			want:   &gen1_8.PlayServerboundClientCommand{Payload: 0},
		},
		// This protocol has no command packet. A command is chat with a
		// leading slash, which is how its server tells the two apart, so the
		// slash the intent does not carry is added here.
		"command": {
			action: ActionCommand{Command: "setblock 1 2 3 stone"},
			packet: "chat",
			want:   &gen1_8.PlayServerboundChat{Message: "/setblock 1 2 3 stone"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := java.Java1_8().Adapter.EncodeAction(test.action)
			if err != nil {
				t.Fatalf("EncodeAction: %v", err)
			}
			if got.Name != test.packet {
				t.Fatalf("packet name = %q, want %q", got.Name, test.packet)
			}
			if got.State != gen1_8.StatePlay || got.Direction != protocol.DirectionServerbound {
				t.Fatalf("packet envelope = (%s, %d), want serverbound play", got.State, got.Direction)
			}
			assertPacketValue(t, got, test.want)
		})
	}
}

func TestEachActionEncodesToItsProtocol775Packet(t *testing.T) {
	t.Parallel()

	// Protocol 775 replaces the standing flag with a flags structure that also
	// carries the horizontal collision, so every case sets both.
	flags := gen26_1.MovementFlags{OnGround: true, HasHorizontalCollision: true}

	for name, test := range map[string]struct {
		action version.Action
		packet string
		want   any
	}{
		"move": {
			action: ActionMove{X: 1, Y: 2, Z: 3, OnGround: true, HorizontalCollision: true},
			packet: "position",
			want:   &gen26_1.PlayServerboundPosition{X: 1, Y: 2, Z: 3, Flags: flags},
		},
		"look": {
			action: ActionLook{Yaw: 90, Pitch: -45, OnGround: true, HorizontalCollision: true},
			packet: "look",
			want:   &gen26_1.PlayServerboundLook{Yaw: 90, Pitch: -45, Flags: flags},
		},
		"move and look": {
			action: ActionMoveLook{
				X: 1, Y: 2, Z: 3, Yaw: 90, Pitch: -45,
				OnGround: true, HorizontalCollision: true,
			},
			packet: "position_look",
			want: &gen26_1.PlayServerboundPositionLook{
				X: 1, Y: 2, Z: 3, Yaw: 90, Pitch: -45, Flags: flags,
			},
		},
		"ground only": {
			action: ActionGround{OnGround: true, HorizontalCollision: true},
			packet: "flying",
			want:   &gen26_1.PlayServerboundFlying{Flags: flags},
		},
		// This protocol names its client commands where 47 numbers them.
		"respawn": {
			action: ActionRespawn{},
			packet: "client_command",
			want:   &gen26_1.PlayServerboundClientCommand{ActionID: "perform_respawn"},
		},
		// The protocol calls sneaking Shift, after the key rather than the
		// posture, so the field the intent's Sneak lands in is not the one its
		// name suggests.
		"input": {
			action: ActionInput{Forward: true, Sneak: true},
			packet: "player_input",
			want: &gen26_1.PlayServerboundPlayerInput{
				Inputs: gen26_1.PlayServerboundPlayerInputInputsFlags{Forward: true, Shift: true},
			},
		},
		"start sprinting": {
			action: ActionSprint{Sprinting: true},
			packet: "entity_action",
			want:   &gen26_1.PlayServerboundEntityAction{ActionID: "start_sprinting"},
		},
		"stop sprinting": {
			action: ActionSprint{},
			packet: "entity_action",
			want:   &gen26_1.PlayServerboundEntityAction{ActionID: "stop_sprinting"},
		},
		// 775 carries a command on its own packet, whose field holds the
		// command without the slash 47 needs.
		"command": {
			action: ActionCommand{Command: "setblock 1 2 3 stone"},
			packet: "chat_command",
			want:   &gen26_1.PlayServerboundChatCommand{Command: "setblock 1 2 3 stone"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := java.Current().Adapter.EncodeAction(test.action)
			if err != nil {
				t.Fatalf("EncodeAction: %v", err)
			}
			if got.Name != test.packet {
				t.Fatalf("packet name = %q, want %q", got.Name, test.packet)
			}
			if got.State != gen26_1.StatePlay || got.Direction != protocol.DirectionServerbound {
				t.Fatalf("packet envelope = (%s, %d), want serverbound play", got.State, got.Direction)
			}
			assertPacketValue(t, got, test.want)
		})
	}
}

func TestProtocol47DropsTheCollisionFlagItHasNoFieldFor(t *testing.T) {
	t.Parallel()

	// The intents are shared, so a caller writing for both protocols sets the
	// flag once. Protocol 47 has nowhere to put it, and dropping it must not
	// change the rest of the packet.
	with, err := java.Java1_8().Adapter.EncodeAction(
		ActionMove{X: 1, OnGround: true, HorizontalCollision: true},
	)
	if err != nil {
		t.Fatalf("EncodeAction: %v", err)
	}
	without, err := java.Java1_8().Adapter.EncodeAction(ActionMove{X: 1, OnGround: true})
	if err != nil {
		t.Fatalf("EncodeAction: %v", err)
	}

	first, ok := with.Value.(*gen1_8.PlayServerboundPosition)
	if !ok {
		t.Fatalf("value is %T, want a position", with.Value)
	}
	second, ok := without.Value.(*gen1_8.PlayServerboundPosition)
	if !ok {
		t.Fatalf("value is %T, want a position", without.Value)
	}
	if *first != *second {
		t.Fatalf("the collision flag changed the packet: %+v vs %+v", *first, *second)
	}
}

func TestAnUnknownActionIsRefusedByName(t *testing.T) {
	t.Parallel()

	for name, adapter := range map[string]version.Adapter{
		"protocol 47":  java.Java1_8().Adapter,
		"protocol 775": java.Current().Adapter,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := adapter.EncodeAction(unknownAction{})
			if !errors.Is(err, version.ErrUnsupportedAction) {
				t.Fatalf("EncodeAction error = %v, want ErrUnsupportedAction", err)
			}
		})
	}
}

func TestDoBeforePlayReturnsAnErrorNamingTheState(t *testing.T) {
	t.Parallel()

	sender := &recordingSender{}
	c := actionClient(t, java.Java1_8(), sender)
	c.inPlay = false

	err := c.Do(context.Background(), ActionGround{OnGround: true})
	if !errors.Is(err, ErrNotInPlay) {
		t.Fatalf("Do error = %v, want ErrNotInPlay", err)
	}
	if len(sender.sent) != 0 {
		t.Fatalf("a refused action still wrote %d packets", len(sender.sent))
	}
}

func TestDoWithNoConnectionReturnsAnError(t *testing.T) {
	t.Parallel()

	c := actionClient(t, java.Java1_8(), nil)
	if err := c.Do(context.Background(), ActionGround{}); !errors.Is(err, ErrNotInPlay) {
		t.Fatalf("Do error = %v, want ErrNotInPlay", err)
	}
}

func TestDoAfterCloseReturnsAnErrorRatherThanBlocking(t *testing.T) {
	t.Parallel()

	c := actionClient(t, java.Java1_8(), &recordingSender{})
	c.closed = true

	done := make(chan error, 1)
	go func() { done <- c.Do(context.Background(), ActionGround{}) }()

	select {
	case err := <-done:
		if !errors.Is(err, ErrClosed) {
			t.Fatalf("Do error = %v, want ErrClosed", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Do on a closed client blocked")
	}
}

func TestDoRefusesANilAction(t *testing.T) {
	t.Parallel()

	c := actionClient(t, java.Java1_8(), &recordingSender{})
	if err := c.Do(context.Background(), nil); err == nil {
		t.Fatal("Do accepted a nil action")
	}
}

func TestDoReportsAFailedWrite(t *testing.T) {
	t.Parallel()

	failure := errors.New("connection died")
	c := actionClient(t, java.Java1_8(), failingSender{err: failure})

	if err := c.Do(context.Background(), ActionGround{}); !errors.Is(err, failure) {
		t.Fatalf("Do error = %v, want the write's error", err)
	}
}

func TestConcurrentActionsAreSerialized(t *testing.T) {
	t.Parallel()

	const perGoroutine = 50

	writer := &concurrencyCheckingSender{t: t}
	c := actionClient(t, java.Java1_8(), writer)

	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range perGoroutine {
				if err := c.Do(context.Background(), ActionGround{OnGround: true}); err != nil {
					t.Errorf("Do: %v", err)

					return
				}
			}
		}()
	}
	wg.Wait()

	if got := writer.count(); got != 2*perGoroutine {
		t.Fatalf("the writer saw %d packets, want %d", got, 2*perGoroutine)
	}
}

// concurrencyCheckingSender fails the test if two writes overlap.
//
// It does not lock around the whole write: the point is to observe whether the
// client serialized, not to serialize on its behalf.
type concurrencyCheckingSender struct {
	t *testing.T

	mu      sync.Mutex
	inside  bool
	written int
}

func (s *concurrencyCheckingSender) Write(context.Context, protocol.Packet) error {
	s.mu.Lock()
	if s.inside {
		s.mu.Unlock()
		s.t.Error("two writes overlapped: Do is not serialized")

		return errors.New("overlapping write")
	}
	s.inside = true
	s.mu.Unlock()

	// A yield, so an unserialized caller has a real chance to overlap.
	runtime.Gosched()

	s.mu.Lock()
	s.inside = false
	s.written++
	s.mu.Unlock()

	return nil
}

func (s *concurrencyCheckingSender) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.written
}

// unknownAction is an intent no protocol has a packet for.
type unknownAction struct{}

func (unknownAction) ActionKind() string { return "test.unknown" }

// assertPacketValue compares a packet's value against the expected one by
// dereferencing both, so the failure names the fields rather than two addresses.
func assertPacketValue(t *testing.T, got protocol.Packet, want any) {
	t.Helper()

	if fmt.Sprintf("%+v", got.Value) != fmt.Sprintf("%+v", want) {
		t.Fatalf("packet value = %+v, want %+v", got.Value, want)
	}
	if got.ID != packetID(t, want) {
		t.Fatalf("packet ID = %d, want %d", got.ID, packetID(t, want))
	}
}

// packetID reads the generated identifier off an expected value.
func packetID(t *testing.T, value any) int32 {
	t.Helper()

	identified, ok := value.(interface{ PacketID() int32 })
	if !ok {
		t.Fatalf("value %T does not report a packet ID", value)
	}

	return identified.PacketID()
}

// TestProtocol47RefusesTheLocomotionItCannotName pins a deliberate gap.
//
// 47 has no input packet at all: there is no packet that reports which keys are
// held, and approximating one with movement would report a different thing.
//
// Sprint used to be refused here too, on the ground that 47's entity_action
// declares actionId as a bare varint with no names attached and this repository
// had not measured the numbers. It has now — C0BPacketEntityAction.Action in
// the deobfuscated 1.8.9 jar, written as an ordinal — so sprint encodes on both
// versions and only the input packet is missing. See TestSprintEncodesOnBoth.
func TestProtocol47RefusesTheLocomotionItCannotName(t *testing.T) {
	t.Parallel()

	_, err := java.Java1_8().Adapter.EncodeAction(ActionInput{Forward: true})
	if !errors.Is(err, version.ErrUnsupportedAction) {
		t.Fatalf("EncodeAction returned %v, want ErrUnsupportedAction", err)
	}
}

// TestSprintEncodesOnBothVersionsAsAnEntityAction pins the change.
//
// Sprinting is a declared state on both protocols and both declare it on the
// same packet — one numbering the action and one naming it. A caller says
// "start sprinting" once.
func TestSprintEncodesOnBothVersionsAsAnEntityAction(t *testing.T) {
	t.Parallel()

	on47, err := java.Java1_8().Adapter.EncodeAction(ActionSprint{Sprinting: true})
	if err != nil {
		t.Fatalf("EncodeAction on 47: %v", err)
	}
	body47, ok := on47.Value.(*gen1_8.PlayServerboundEntityAction)
	if !ok {
		t.Fatalf("47 encoded %T, want an entity action", on47.Value)
	}
	// START_SNEAKING, STOP_SNEAKING, STOP_SLEEPING, START_SPRINTING: the
	// fourth member, so three.
	if body47.ActionID != 3 {
		t.Errorf("47 sprint action = %d, want 3", body47.ActionID)
	}

	on775, err := java.Current().Adapter.EncodeAction(ActionSprint{Sprinting: true})
	if err != nil {
		t.Fatalf("EncodeAction on 775: %v", err)
	}
	body775, ok := on775.Value.(*gen26_1.PlayServerboundEntityAction)
	if !ok {
		t.Fatalf("775 encoded %T, want an entity action", on775.Value)
	}
	if body775.ActionID != "start_sprinting" {
		t.Errorf("775 sprint action = %q, want start_sprinting", body775.ActionID)
	}
}
