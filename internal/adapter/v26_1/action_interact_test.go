package v26_1_test

import (
	"errors"
	"strings"
	"testing"

	protocol "github.com/go-theft-craft/minecraft-protocol"
	gen "github.com/go-theft-craft/minecraft-protocol/generated/java/v26_1"

	"github.com/go-theft-craft/headless-minecraft/event"
	adapter "github.com/go-theft-craft/headless-minecraft/internal/adapter/v26_1"
	"github.com/go-theft-craft/headless-minecraft/version"
)

// encode775 encodes one action against a fresh adapter.
func encode775(t *testing.T, action version.Action) protocol.Packet {
	t.Helper()

	packet, err := adapter.New(new(event.Collector), new(version.Outbox)).EncodeAction(action)
	if err != nil {
		t.Fatalf("EncodeAction(%s): %v", action.ActionKind(), err)
	}

	return packet
}

// refused775 requires an action to be refused by kind.
func refused775(t *testing.T, action version.Action) {
	t.Helper()

	_, err := adapter.New(new(event.Collector), new(version.Outbox)).EncodeAction(action)
	if !errors.Is(err, version.ErrUnsupportedAction) {
		t.Fatalf("EncodeAction(%s) = %v, want ErrUnsupportedAction", action.ActionKind(), err)
	}
	if !strings.Contains(err.Error(), action.ActionKind()) {
		t.Fatalf("refusal %q does not name the kind %q", err, action.ActionKind())
	}
}

func TestSwingCarriesTheHandOn775(t *testing.T) {
	t.Parallel()

	// The mirror of 47's dropped field: this protocol has two hands and its
	// animation packet says which one, so the field survives here.
	main := encode775(t, version.ActionSwing{Hand: version.MainHand}).
		Value.(*gen.PlayServerboundArmAnimation)
	off := encode775(t, version.ActionSwing{Hand: version.OffHand}).
		Value.(*gen.PlayServerboundArmAnimation)

	if main.Hand != 0 || off.Hand != 1 {
		t.Fatalf("hands encoded as %d and %d, want 0 and 1", main.Hand, off.Hand)
	}
}

func TestUseOnCarriesTheCursorUnrounded(t *testing.T) {
	t.Parallel()

	// 47 rounds the cursor to sixteenths of a block; this protocol carries a
	// float per axis, so the caller's value arrives as it was given.
	packet := encode775(t, version.ActionUseOn{
		Block:  version.BlockPos{X: 10, Y: 64, Z: -3},
		Face:   version.FaceTop,
		Cursor: version.Cursor{X: 0.3, Y: 1, Z: 0.7},
		Hand:   version.OffHand,
	})

	body, ok := packet.Value.(*gen.PlayServerboundBlockPlace)
	if !ok {
		t.Fatalf("encoded %T, want PlayServerboundBlockPlace", packet.Value)
	}
	if body.CursorX != 0.3 || body.CursorY != 1 || body.CursorZ != 0.7 {
		t.Errorf("cursor = (%v, %v, %v), want the caller's own values",
			body.CursorX, body.CursorY, body.CursorZ)
	}
	if body.Direction != int32(version.FaceTop) {
		t.Errorf("Direction = %d", body.Direction)
	}
	if body.Location != (gen.Position{X: 10, Y: 64, Z: -3}) {
		t.Errorf("Location = %+v", body.Location)
	}
	if body.Hand != 1 {
		t.Errorf("Hand = %d, want the offhand", body.Hand)
	}
}

func TestUseInAirIsItsOwnPacketOn775(t *testing.T) {
	t.Parallel()

	// The other half of what 47 does with a sentinel position: this protocol
	// has a packet that means exactly "use what I hold".
	if _, ok := encode775(t, version.ActionUseItem{}).Value.(*gen.PlayServerboundUseItem); !ok {
		t.Fatal("a use in the air did not encode a use_item packet")
	}
}

func TestAttackIsItsOwnPacketOn775(t *testing.T) {
	t.Parallel()

	// The game models attack as a member of one interaction enumeration; this
	// protocol's pinned schema puts that member on a packet of its own. The
	// caller says ActionInteract either way, which is what the adapter is for.
	packet := encode775(t, version.ActionInteract{Entity: 42, Kind: version.InteractAttack})

	body, ok := packet.Value.(*gen.PlayServerboundAttack)
	if !ok {
		t.Fatalf("encoded %T, want PlayServerboundAttack", packet.Value)
	}
	if body.EntityID != 42 {
		t.Errorf("EntityID = %d, want 42", body.EntityID)
	}
}

func TestInteractUseAtCarriesThePointAndRefusesWithoutIt775(t *testing.T) {
	t.Parallel()

	refused775(t, version.ActionInteract{Entity: 42, Kind: version.InteractUseAt})

	at := version.Cursor{X: 0.25, Y: 1.5, Z: -0.25}
	body := encode775(t, version.ActionInteract{
		Entity: 42, Kind: version.InteractUseAt, At: &at, Hand: version.MainHand, Sneaking: true,
	}).Value.(*gen.PlayServerboundUseEntity)

	if body.Location.X != 0.25 || body.Location.Y != 1.5 || body.Location.Z != -0.25 {
		t.Errorf("point = %+v, want %+v", body.Location, at)
	}
	if body.Hand != "main_hand" {
		t.Errorf("Hand = %q", body.Hand)
	}
	if !body.Sneaking {
		t.Error("the sneaking claim was dropped, and this protocol reads it")
	}
}

func TestDigStagesMapToTheirStatusesOn775(t *testing.T) {
	t.Parallel()

	// The same three numbers as 47. Both games write
	// START_DESTROY_BLOCK, ABORT_DESTROY_BLOCK, STOP_DESTROY_BLOCK as an
	// ordinal, and that ordering has not moved.
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

			body, ok := encode775(t, version.ActionDig{
				Block: version.BlockPos{X: 1, Y: 2, Z: 3},
				Stage: c.stage,
			}).Value.(*gen.PlayServerboundBlockDig)
			if !ok {
				t.Fatal("did not encode a digging packet")
			}
			if body.Status != c.want {
				t.Errorf("Status = %d, want %d", body.Status, c.want)
			}
		})
	}
}

func TestSwapHandsTravelsOnTheDiggingPacketOn775(t *testing.T) {
	t.Parallel()

	// 47 refuses this outright. Here it is a seventh status on the packet that
	// already carries digging, dropping, and releasing a use — which is not
	// where a reader who knows 1.8.9 would look for it.
	body, ok := encode775(t, version.ActionSwapHands{}).Value.(*gen.PlayServerboundBlockDig)
	if !ok {
		t.Fatal("a hand swap did not encode a digging packet")
	}
	if body.Status != 6 {
		t.Errorf("Status = %d, want the swap status 6", body.Status)
	}
}

func TestSneakIsRefusedOn775BecauseItMovedToTheInputPacket(t *testing.T) {
	t.Parallel()

	// The mirror of 47's refusals. 26.1.2's ServerboundPlayerCommandPacket
	// names sleeping, sprinting, horse jumps, vehicle inventories, and elytra,
	// and nothing else — so a sneak encoded here would be some other action.
	refused775(t, version.ActionEntityAction{Kind: version.SneakStart})
	refused775(t, version.ActionEntityAction{Kind: version.SneakStop})
}

func TestChatFillsTheAcknowledgementBitsetSoItEncodes(t *testing.T) {
	t.Parallel()

	// The bitset is a fixed three bytes and the codec refuses any other
	// length, so a nil slice would be a packet that fails at the write rather
	// than at the encode. The set-wide gate writes every action through the
	// real codec; this pins why this one has a field it does not use.
	packet := encode775(t, version.ActionChat{Message: "hello"})

	body, ok := packet.Value.(*gen.PlayServerboundChatMessage)
	if !ok {
		t.Fatalf("encoded %T, want PlayServerboundChatMessage", packet.Value)
	}
	if body.Message != "hello" {
		t.Errorf("Message = %q", body.Message)
	}
	if body.Signature != nil {
		t.Error("an unsigned message carries a signature")
	}
	if len(body.Acknowledged) != 3 {
		t.Fatalf("acknowledged is %d bytes, want 3", len(body.Acknowledged))
	}
}
