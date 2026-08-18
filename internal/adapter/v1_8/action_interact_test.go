package v1_8_test

import (
	"errors"
	"strings"
	"testing"

	protocol "github.com/go-theft-craft/minecraft-protocol"
	gen "github.com/go-theft-craft/minecraft-protocol/generated/java/v1_8"

	"github.com/go-theft-craft/headless-minecraft/event"
	adapter "github.com/go-theft-craft/headless-minecraft/internal/adapter/v1_8"
	"github.com/go-theft-craft/headless-minecraft/version"
)

// encode47 encodes one action against a fresh adapter.
func encode47(t *testing.T, action version.Action) protocol.Packet {
	t.Helper()

	packet, err := adapter.New(new(event.Collector), new(version.Outbox)).EncodeAction(action)
	if err != nil {
		t.Fatalf("EncodeAction(%s): %v", action.ActionKind(), err)
	}

	return packet
}

// refused47 requires an action to be refused by kind.
func refused47(t *testing.T, action version.Action) {
	t.Helper()

	_, err := adapter.New(new(event.Collector), new(version.Outbox)).EncodeAction(action)
	if !errors.Is(err, version.ErrUnsupportedAction) {
		t.Fatalf("EncodeAction(%s) = %v, want ErrUnsupportedAction", action.ActionKind(), err)
	}
	if !strings.Contains(err.Error(), action.ActionKind()) {
		t.Fatalf("refusal %q does not name the kind %q", err, action.ActionKind())
	}
}

func TestHeldSlotEncodesTheHotbarIndex(t *testing.T) {
	t.Parallel()

	packet := encode47(t, version.ActionHeldSlot{Slot: 4})

	body, ok := packet.Value.(*gen.PlayServerboundHeldItemSlot)
	if !ok {
		t.Fatalf("encoded %T, want PlayServerboundHeldItemSlot", packet.Value)
	}
	if body.SlotID != 4 {
		t.Fatalf("SlotID = %d, want 4", body.SlotID)
	}
}

func TestHeldSlotAboveTheHotbarIsRefused(t *testing.T) {
	t.Parallel()

	// The hotbar is nine slots. A tenth is a protocol error on both versions
	// and a disconnect on some servers, so it is refused here rather than
	// sent and regretted.
	refused47(t, version.ActionHeldSlot{Slot: 9})
}

func TestSwingEncodesAnArmAnimation(t *testing.T) {
	t.Parallel()

	if _, ok := encode47(t, version.ActionSwing{}).Value.(*gen.PlayServerboundArmAnimation); !ok {
		t.Fatal("swing did not encode an arm animation")
	}
}

func TestSwingWithTheOffHandStillEncodesOn47(t *testing.T) {
	t.Parallel()

	// 47 has no offhand, but a swing is still a swing. The field is dropped
	// rather than the intent refused, which is the rule the design states, and
	// the two hands therefore produce the same bytes.
	main := encode47(t, version.ActionSwing{Hand: version.MainHand})
	off := encode47(t, version.ActionSwing{Hand: version.OffHand})

	if *main.Value.(*gen.PlayServerboundArmAnimation) != *off.Value.(*gen.PlayServerboundArmAnimation) {
		t.Fatal("the dropped hand changed the packet")
	}
}

func TestUseOnCarriesTheCursorNotTheCentre(t *testing.T) {
	t.Parallel()

	// Which half of a slab fills and which way a stair faces are decided by
	// the cursor. An encoder that sent the centre would place a different
	// block from the one the caller asked for.
	packet := encode47(t, version.ActionUseOn{
		Block:  version.BlockPos{X: 10, Y: 64, Z: -3},
		Face:   version.FaceTop,
		Cursor: version.Cursor{X: 0.5, Y: 1, Z: 0.25},
	})

	body, ok := packet.Value.(*gen.PlayServerboundBlockPlace)
	if !ok {
		t.Fatalf("encoded %T, want PlayServerboundBlockPlace", packet.Value)
	}
	if body.Direction != int8(version.FaceTop) {
		t.Errorf("Direction = %d, want %d", body.Direction, version.FaceTop)
	}
	if body.Location != (gen.Position{X: 10, Y: 64, Z: -3}) {
		t.Errorf("Location = %+v", body.Location)
	}
	// 47 writes each axis as (int)(component * 16) and the server reads the
	// byte back divided by sixteen, so a full face is 16 and a quarter is 4.
	if body.CursorX != 8 || body.CursorY != 16 || body.CursorZ != 4 {
		t.Errorf("cursor = (%d, %d, %d), want (8, 16, 4)",
			body.CursorX, body.CursorY, body.CursorZ)
	}
}

func TestUseItemInAirUsesTheSentinelPosition(t *testing.T) {
	t.Parallel()

	// 47 has no separate use-in-air packet. It sends a placement at the
	// sentinel position with the direction byte 255 — written as -1 through a
	// signed field — and the server branches on exactly that.
	packet := encode47(t, version.ActionUseItem{})

	body, ok := packet.Value.(*gen.PlayServerboundBlockPlace)
	if !ok {
		t.Fatalf("encoded %T, want PlayServerboundBlockPlace", packet.Value)
	}
	if body.Direction != -1 {
		t.Errorf("Direction = %d, want -1", body.Direction)
	}
	if body.Location != (gen.Position{X: -1, Y: -1, Z: -1}) {
		t.Errorf("Location = %+v, want the sentinel", body.Location)
	}
}

func TestInteractAttackEncodesTheAttackMode(t *testing.T) {
	t.Parallel()

	packet := encode47(t, version.ActionInteract{Entity: 42, Kind: version.InteractAttack})

	body, ok := packet.Value.(*gen.PlayServerboundUseEntity)
	if !ok {
		t.Fatalf("encoded %T, want PlayServerboundUseEntity", packet.Value)
	}
	if body.Target != 42 {
		t.Errorf("Target = %d, want 42", body.Target)
	}
	// INTERACT, ATTACK, INTERACT_AT: the second member.
	if body.Mouse != 1 {
		t.Errorf("Mouse = %d, want the attack mode 1", body.Mouse)
	}
}

func TestInteractUseAtCarriesThePointAndRefusesWithoutIt(t *testing.T) {
	t.Parallel()

	// Use-at is defined by the position. Sending it without one is a packet
	// the server reads as a point at the entity's feet, so it is refused
	// rather than sent with zeros.
	refused47(t, version.ActionInteract{Entity: 42, Kind: version.InteractUseAt})

	at := version.Cursor{X: 0.25, Y: 1.5, Z: -0.25}
	packet := encode47(t, version.ActionInteract{
		Entity: 42, Kind: version.InteractUseAt, At: &at,
	})

	body := packet.Value.(*gen.PlayServerboundUseEntity)
	if body.Mouse != 2 {
		t.Errorf("Mouse = %d, want the interact-at mode 2", body.Mouse)
	}
	if body.X.Case2 != at.X || body.Y.Case2 != at.Y || body.Z.Case2 != at.Z {
		t.Errorf("point = (%v, %v, %v), want %+v", body.X.Case2, body.Y.Case2, body.Z.Case2, at)
	}
}

func TestDigStagesMapToTheirStatuses(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		stage version.DigStage
		want  int32
	}{
		{version.DigStart, 0},
		{version.DigCancel, 1},
		{version.DigFinish, 2},
	} {
		t.Run(c.stage.String(), func(t *testing.T) {
			t.Parallel()

			packet := encode47(t, version.ActionDig{
				Block: version.BlockPos{X: 1, Y: 2, Z: 3},
				Face:  version.FaceTop,
				Stage: c.stage,
			})

			body, ok := packet.Value.(*gen.PlayServerboundBlockDig)
			if !ok {
				t.Fatalf("encoded %T, want PlayServerboundBlockDig", packet.Value)
			}
			if body.Status != c.want {
				t.Errorf("Status = %d, want %d", body.Status, c.want)
			}
			if body.Location != (gen.Position{X: 1, Y: 2, Z: 3}) {
				t.Errorf("Location = %+v", body.Location)
			}
		})
	}
}

func TestReleaseUseAndDropShareTheDiggingPacket(t *testing.T) {
	t.Parallel()

	// Neither version has a use-release packet or a drop packet: the digging
	// packet carries both as statuses, which is why firing a bow looks like a
	// mining packet on the wire.
	for name, c := range map[string]struct {
		action version.Action
		want   int32
	}{
		"drop one":    {version.ActionDrop{}, 4},
		"drop stack":  {version.ActionDrop{Whole: true}, 3},
		"release use": {version.ActionReleaseUse{}, 5},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			body, ok := encode47(t, c.action).Value.(*gen.PlayServerboundBlockDig)
			if !ok {
				t.Fatal("did not encode a digging packet")
			}
			if body.Status != c.want {
				t.Errorf("Status = %d, want %d", body.Status, c.want)
			}
		})
	}
}

func TestClickSlotCarriesTheCallersSequenceAndClaim(t *testing.T) {
	t.Parallel()

	// 47 confirms a click by echoing its number, and the caller is what
	// records the pending click and waits for the echo — so the number is the
	// caller's, and the claim is the caller's belief about the slot, straight
	// from the world store.
	a := adapter.New(new(event.Collector), new(version.Outbox))
	held := gen.Slot{BlockID: 1}
	click := version.ActionClickSlot{
		Window: 3, Slot: 7, Mode: version.ClickQuickMove, Sequence: 42, Claim: held,
	}

	packet, err := a.EncodeAction(click)
	if err != nil {
		t.Fatalf("EncodeAction: %v", err)
	}

	body := packet.Value.(*gen.PlayServerboundWindowClick)
	if body.Mode != 1 {
		t.Fatalf("Mode = %d, want the quick-move mode 1", body.Mode)
	}
	if body.WindowID != 3 || body.Slot != 7 || body.Action != 42 {
		t.Fatalf("click addressed window %d slot %d transaction %d", body.WindowID, body.Slot, body.Action)
	}
	if body.Item != held {
		t.Fatalf("claimed %+v, want the held stack", body.Item)
	}
}

func TestClickSlotWithNoClaimClaimsAnEmptySlot(t *testing.T) {
	t.Parallel()

	a := adapter.New(new(event.Collector), new(version.Outbox))
	packet, err := a.EncodeAction(version.ActionClickSlot{Window: 3, Slot: 7, Mode: version.ClickPickup})
	if err != nil {
		t.Fatalf("EncodeAction: %v", err)
	}
	if body := packet.Value.(*gen.PlayServerboundWindowClick); body.Item.BlockID != -1 {
		t.Fatalf("claimed %+v, want the empty slot", body.Item)
	}
}

func TestClickSlotRefusesAForeignClaim(t *testing.T) {
	t.Parallel()

	// The claim comes from the world store, which holds this adapter's own
	// decoded values. Anything else is a caller's bug, and claiming it empty
	// would turn every click into a rejection.
	a := adapter.New(new(event.Collector), new(version.Outbox))
	if _, err := a.EncodeAction(version.ActionClickSlot{Claim: "a string"}); err == nil {
		t.Fatal("EncodeAction accepted a claim no protocol decoded")
	}
}

func TestSwapHandsIsRefusedOn47(t *testing.T) {
	t.Parallel()

	// 47 has no offhand, so a hand swap is not a field to drop — it is the
	// whole intent. This is the refusal side of the rule the design states.
	refused47(t, version.ActionSwapHands{})
}

func TestSneakEncodesOn47AsAnEntityAction(t *testing.T) {
	t.Parallel()

	// The mirror of the 775 refusal: 1.8.9 declares sneaking on this packet
	// and 26.1.2 moved it onto the input packet.
	for kind, want := range map[version.EntityActionKind]int32{
		version.SneakStart: 0,
		version.SneakStop:  1,
	} {
		body := encode47(t, version.ActionEntityAction{Kind: kind}).
			Value.(*gen.PlayServerboundEntityAction)
		if body.ActionID != want {
			t.Errorf("%v = %d, want %d", kind, body.ActionID, want)
		}
	}
}

func TestTheEntityActionsThisVersionCannotNameAreRefused(t *testing.T) {
	t.Parallel()

	// 1.8.9 has one member for a horse jump rather than a start and a stop,
	// and no elytra at all. Mapping either onto a neighbouring number would be
	// a different action performed confidently.
	refused47(t, version.ActionEntityAction{Kind: version.HorseJumpStop})
	refused47(t, version.ActionEntityAction{Kind: version.ElytraFlyStart})
}

func TestChatAndCommandDifferOnlyBySlashOn47(t *testing.T) {
	t.Parallel()

	// 1.8.9 has no command packet. A leading slash in chat is how a command
	// was sent, and a caller must not have to know that.
	command := encode47(t, version.ActionCommand{Command: "gamemode creative"})
	chat := encode47(t, version.ActionChat{Message: "/gamemode creative"})

	if *command.Value.(*gen.PlayServerboundChat) != *chat.Value.(*gen.PlayServerboundChat) {
		t.Fatalf("command encoded as %+v, want the same bytes as %+v",
			command.Value, chat.Value)
	}
}
