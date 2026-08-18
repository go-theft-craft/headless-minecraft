package v26_1

import (
	"fmt"

	protocol "github.com/go-theft-craft/minecraft-protocol"
	gen "github.com/go-theft-craft/minecraft-protocol/generated/java/v26_1"
	"github.com/go-theft-craft/minecraft-protocol/wire/java"

	"github.com/go-theft-craft/headless-minecraft/version"
)

// maxHotbarSlot is the highest selectable hotbar index. The hotbar is nine
// slots on both versions.
const maxHotbarSlot = 8

// The player-action statuses, read off 26.1.2's
// ServerboundPlayerActionPacket.Action, which the packet writes as an ordinal.
// The first six members are the ones 1.8.9 also has, in the same order.
const (
	digStartStatus775   int32 = 0
	digCancelStatus775  int32 = 1
	digFinishStatus775  int32 = 2
	dropStackStatus775  int32 = 3
	dropSingleStatus775 int32 = 4
	releaseUseStatus775 int32 = 5
	// swapHandsStatus775 is the two members 1.8.9 does not have. A hand swap
	// travels on the digging packet here rather than on the state packet,
	// which is where a reader expecting 1.8.9's shape would look for it.
	swapHandsStatus775 int32 = 6
)

// The hands, named rather than numbered on the packets that carry a name and
// numbered on the ones that carry a number. Both orders are main hand first.
const (
	mainHandName775 = "main_hand"
	offHandName775  = "off_hand"
	mainHandID775   = 0
	offHandID775    = 1
)

// The window-click modes. 26.1.2 numbers them as 1.8.9 does.
const (
	pickupMode775      int32 = 0
	quickMoveMode775   int32 = 1
	swapHotbarMode775  int32 = 2
	middleMode775      int32 = 3
	dropMode775        int32 = 4
	dragMode775        int32 = 5
	doubleClickMode775 int32 = 6
)

// The entity-action names, read off this version's own generated registry
// rather than off an ordinal, because 775 names these where 47 numbers them.
const (
	leaveBedAction775    = "leave_bed"
	sprintStartAction775 = "start_sprinting"
	sprintStopAction775  = "stop_sprinting"
	horseJumpStart775    = "start_horse_jump"
	horseJumpStop775     = "stop_horse_jump"
	openVehicleAction775 = "open_vehicle_inventory"
	elytraFlyStartAction = "start_elytra_flying"
)

// acknowledgedChatBytes is the fixed width of the chat acknowledgement bitset.
// The codec refuses any other length, so an unsigned message writes three
// zeroes rather than leaving the field absent.
const acknowledgedChatBytes = 3

// unsequenced is the prediction sequence a client sends when it is predicting
// nothing. Vanilla increments one per block action and the server echoes it in
// its acknowledgement; a client that predicts no block change has nothing for
// the server to confirm, and zero says so.
const unsequenced int32 = 0

// unstamped is the container state a client sends when it has not been told
// one. See the click case for why an unmatched state is self-correcting.
const unstamped int32 = 0

// wirePos775 converts a version-neutral block position to this protocol's.
func wirePos775(pos version.BlockPos) gen.Position {
	return gen.Position{X: pos.X, Y: int16(pos.Y), Z: pos.Z}
}

// handName775 names a hand for the packets that carry a string.
func handName775(hand version.Hand) string {
	if hand == version.OffHand {
		return offHandName775
	}

	return mainHandName775
}

// handID775 numbers a hand for the packets that carry a varint.
func handID775(hand version.Hand) int32 {
	if hand == version.OffHand {
		return offHandID775
	}

	return mainHandID775
}

// encodeInteraction encodes the interaction, dig, window, and state actions.
//
// A second switch rather than more cases in EncodeAction, for the reason the
// protocol 47 adapter states: movement is settled and these grow with each
// mechanic milestone.
func (adapter) encodeInteraction(action version.Action) (protocol.Packet, bool, error) {
	switch value := action.(type) {
	case version.ActionHeldSlot:
		if value.Slot > maxHotbarSlot {
			return protocol.Packet{}, true, fmt.Errorf(
				"%w: protocol %s cannot encode %s: slot %d is outside the hotbar",
				version.ErrUnsupportedAction, ProtocolID, value.ActionKind(), value.Slot)
		}

		return play775("held_item_slot", &gen.PlayServerboundHeldItemSlot{
			SlotID: int16(value.Slot),
		}), true, nil

	case version.ActionSwing:
		// This version's animation packet carries the hand, so the field
		// survives here where 47 drops it.
		return play775("arm_animation", &gen.PlayServerboundArmAnimation{
			Hand: handID775(value.Hand),
		}), true, nil

	case version.ActionUseOn:
		// The cursor is a float per axis here rather than 47's byte of
		// sixteenths, so the caller's [0, 1] value travels unrounded.
		return play775("block_place", &gen.PlayServerboundBlockPlace{
			Hand:      handID775(value.Hand),
			Location:  wirePos775(value.Block),
			Direction: int32(value.Face),
			CursorX:   value.Cursor.X,
			CursorY:   value.Cursor.Y,
			CursorZ:   value.Cursor.Z,
			Sequence:  unsequenced,
		}), true, nil

	case version.ActionUseItem:
		// A use in the air is its own packet here, rather than 47's placement
		// at a sentinel position.
		return play775("use_item", &gen.PlayServerboundUseItem{
			Hand:     handID775(value.Hand),
			Sequence: unsequenced,
		}), true, nil

	case version.ActionReleaseUse:
		return play775("block_dig", &gen.PlayServerboundBlockDig{
			Status:   releaseUseStatus775,
			Face:     int8(version.FaceBottom),
			Sequence: unsequenced,
		}), true, nil

	case version.ActionInteract:
		return interact775(value)

	case version.ActionDig:
		status, ok := digStatus775(value.Stage)
		if !ok {
			return protocol.Packet{}, true, version.UnsupportedAction(ProtocolID, value)
		}

		return play775("block_dig", &gen.PlayServerboundBlockDig{
			Status:   status,
			Location: wirePos775(value.Block),
			Face:     int8(value.Face),
			Sequence: unsequenced,
		}), true, nil

	case version.ActionDrop:
		status := dropSingleStatus775
		if value.Whole {
			status = dropStackStatus775
		}

		return play775("block_dig", &gen.PlayServerboundBlockDig{
			Status:   status,
			Face:     int8(version.FaceBottom),
			Sequence: unsequenced,
		}), true, nil

	case version.ActionSwapHands:
		// On the digging packet, not the state packet. 47 refuses this
		// outright; here it is a status the same packet already carries.
		return play775("block_dig", &gen.PlayServerboundBlockDig{
			Status:   swapHandsStatus775,
			Face:     int8(version.FaceBottom),
			Sequence: unsequenced,
		}), true, nil

	case version.ActionClickSlot:
		mode, ok := clickMode775(value.Mode)
		if !ok {
			return protocol.Packet{}, true, version.UnsupportedAction(ProtocolID, value)
		}

		// No transaction number: this version confirms a click with a state ID
		// the server stamps. Nothing here yet tracks the last one the server
		// sent — that is M9.7's, along with the confirmation it feeds — and an
		// unmatched state is self-correcting rather than fatal: 26.1.2's
		// ServerGamePacketListenerImpl.handleContainerClick performs the click
		// either way and follows a mismatch with a full resend of the window.
		return play775("window_click", &gen.PlayServerboundWindowClick{
			WindowID:    value.Window,
			StateID:     unstamped,
			Slot:        value.Slot,
			MouseButton: value.Button,
			Mode:        mode,
		}), true, nil

	case version.ActionCloseWindow:
		return play775("close_window", &gen.PlayServerboundCloseWindow{
			WindowID: value.Window,
		}), true, nil

	case version.ActionSprint:
		state := version.SprintStop
		if value.Sprinting {
			state = version.SprintStart
		}

		return adapter{}.encodeInteraction(version.ActionEntityAction{Kind: state})

	case version.ActionEntityAction:
		id, ok := entityAction775(value.Kind)
		if !ok {
			return protocol.Packet{}, true, version.UnsupportedAction(ProtocolID, value)
		}

		// EntityID is the player's own, and the server reads it from the
		// connection rather than from here: a real client fills it in and a
		// server that disagreed would have no way to act on the difference.
		// Zero is what this sends, because the adapter is not told the entity
		// ID and inventing a wrong one would be worse than sending none.
		return play775("entity_action", &gen.PlayServerboundEntityAction{ActionID: id}), true, nil

	case version.ActionChat:
		// Unsigned. The acknowledgement bitset is a fixed three bytes on this
		// protocol and a nil slice fails to encode, so an empty one is written
		// rather than left absent.
		return play775("chat_message", &gen.PlayServerboundChatMessage{
			Message:      value.Message,
			Acknowledged: make([]byte, acknowledgedChatBytes),
		}), true, nil

	default:
		return protocol.Packet{}, false, nil
	}
}

// interact775 encodes an entity interaction.
//
// Two packets, not one. The game models attack as a member of one interaction
// enumeration, and this protocol's pinned schema splits that member onto a
// packet of its own — so the version-neutral action stays one thing and the
// split lives here, which is what an adapter is for.
func interact775(value version.ActionInteract) (protocol.Packet, bool, error) {
	switch value.Kind {
	case version.InteractAttack:
		return play775("attack", &gen.PlayServerboundAttack{EntityID: value.Entity}), true, nil

	case version.InteractUse:
		// The location is not optional on this packet, so a plain interaction
		// sends the entity's own origin. That is the point a vanilla client
		// reports when the interaction is not aimed at a part of the body.
		return play775("use_entity", &gen.PlayServerboundUseEntity{
			Target:   value.Entity,
			Hand:     handName775(value.Hand),
			Sneaking: value.Sneaking,
		}), true, nil

	case version.InteractUseAt:
		if value.At == nil {
			return protocol.Packet{}, true, fmt.Errorf(
				"%w: protocol %s cannot encode %s: use-at carries no position",
				version.ErrUnsupportedAction, ProtocolID, value.ActionKind())
		}

		return play775("use_entity", &gen.PlayServerboundUseEntity{
			Target: value.Entity,
			Hand:   handName775(value.Hand),
			Location: java.LPVec3{
				X: float64(value.At.X), Y: float64(value.At.Y), Z: float64(value.At.Z),
			},
			Sneaking: value.Sneaking,
		}), true, nil

	default:
		return protocol.Packet{}, true, version.UnsupportedAction(ProtocolID, value)
	}
}

// digStatus775 maps a dig stage to its status.
func digStatus775(stage version.DigStage) (int32, bool) {
	switch stage {
	case version.DigStart:
		return digStartStatus775, true
	case version.DigCancel:
		return digCancelStatus775, true
	case version.DigFinish:
		return digFinishStatus775, true
	default:
		return 0, false
	}
}

// clickMode775 maps a click mode to its number.
func clickMode775(mode version.ClickMode) (int32, bool) {
	switch mode {
	case version.ClickPickup:
		return pickupMode775, true
	case version.ClickQuickMove:
		return quickMoveMode775, true
	case version.ClickSwapHotbar:
		return swapHotbarMode775, true
	case version.ClickMiddle:
		return middleMode775, true
	case version.ClickDrop:
		return dropMode775, true
	case version.ClickDrag:
		return dragMode775, true
	case version.ClickDoubleClick:
		return doubleClickMode775, true
	default:
		return 0, false
	}
}

// entityAction775 maps a declared body state to this protocol's name.
//
// Sneaking has no member here. 26.1.2 moved it onto the input packet —
// ServerboundPlayerCommandPacket.Action names sleeping, sprinting, horse jumps,
// vehicle inventories, and elytra, and nothing else — so a sneak on this
// version is ActionInput and a sneak encoded here would be some other action.
func entityAction775(kind version.EntityActionKind) (string, bool) {
	switch kind {
	case version.LeaveBed:
		return leaveBedAction775, true
	case version.SprintStart:
		return sprintStartAction775, true
	case version.SprintStop:
		return sprintStopAction775, true
	case version.HorseJumpStart:
		return horseJumpStart775, true
	case version.HorseJumpStop:
		return horseJumpStop775, true
	case version.OpenVehicleInventory:
		return openVehicleAction775, true
	case version.ElytraFlyStart:
		return elytraFlyStartAction, true
	case version.SneakStart, version.SneakStop:
		return "", false
	default:
		return "", false
	}
}
