package v1_8

import (
	"fmt"

	protocol "github.com/go-theft-craft/minecraft-protocol"
	gen "github.com/go-theft-craft/minecraft-protocol/generated/java/v1_8"

	"github.com/go-theft-craft/headless-minecraft/version"
)

// maxHotbarSlot is the highest selectable hotbar index. The hotbar is nine
// slots on both versions.
const maxHotbarSlot = 8

// The interact packet's modes. Read off 1.8.9's C02PacketUseEntity.Action,
// whose members are INTERACT, ATTACK, INTERACT_AT and which the packet writes
// as an ordinal.
const (
	interactMouse47 int32 = 0
	attackMouse47   int32 = 1
	interactAtMouse int32 = 2
)

// The digging packet's statuses, read off 1.8.9's C07PacketPlayerDigging.Action
// and identical to 26.1.2's ServerboundPlayerActionPacket.Action for the first
// six members.
const (
	digStartStatus47   int32 = 0
	digCancelStatus47  int32 = 1
	digFinishStatus47  int32 = 2
	dropStackStatus47  int32 = 3
	dropSingleStatus47 int32 = 4
	// releaseUseStatus47 finishes an item use. Neither version has a
	// use-release packet: the digging packet carries it at a sentinel
	// position, which is why a bow is fired through what looks like a mining
	// packet.
	releaseUseStatus47 int32 = 5
)

// useInAirDirection47 is the direction byte that means "no block". 1.8.9's
// server reads the field with readUnsignedByte and branches on 255; the
// generated field is signed, so the same byte is written as -1 here.
const useInAirDirection47 int8 = -1

// The entity-action numbers, read off 1.8.9's C0BPacketEntityAction.Action,
// which the packet writes as an ordinal.
//
// The server never reads the packet's entity ID — NetHandlerPlayServer.
// processEntityAction acts on its own playerEntity — so the adapter sends zero
// rather than inventing one it was not told.
const (
	sneakStartAction47  int32 = 0
	sneakStopAction47   int32 = 1
	leaveBedAction47    int32 = 2
	sprintStartAction47 int32 = 3
	sprintStopAction47  int32 = 4
	horseJumpAction47   int32 = 5
	openVehicleAction47 int32 = 6
)

// The window-click modes, read off 1.8.9's C0EPacketClickWindow, whose mode
// field the server switches on directly.
const (
	pickupMode47      int8 = 0
	quickMoveMode47   int8 = 1
	swapHotbarMode47  int8 = 2
	middleMode47      int8 = 3
	dropMode47        int8 = 4
	dragMode47        int8 = 5
	doubleClickMode47 int8 = 6
)

// emptySlot47 is an absent item stack. 1.8.9 writes a block ID of -1 for one.
//
// Every placement this adapter sends carries one, and that costs nothing: the
// server reads the held stack from the player's own inventory —
// NetHandlerPlayServer.processPlayerBlockPlacement calls
// inventory.getCurrentItem() and never looks at the packet's field.
func emptySlot47() gen.Slot { return gen.Slot{BlockID: -1} }

// wirePos47 converts a version-neutral block position to this protocol's.
func wirePos47(pos version.BlockPos) gen.Position {
	return gen.Position{X: pos.X, Y: int16(pos.Y), Z: pos.Z}
}

// cursorSixteenth converts a cursor component in [0, 1] to the byte 1.8.9
// carries. The client writes (int)(component * 16) and the server reads the
// byte back divided by 16, so a top face is 16 and a centre is 8.
func cursorSixteenth(component float32) int8 {
	return int8(component * 16)
}

// encodeInteraction encodes the interaction, dig, window, and state actions.
//
// It is a second switch rather than more cases in EncodeAction because these
// actions and the movement ones change for different reasons: movement is
// settled and these grow with each mechanic milestone.
func (a adapter) encodeInteraction(action version.Action) (protocol.Packet, bool, error) {
	switch value := action.(type) {
	case version.ActionHeldSlot:
		if value.Slot > maxHotbarSlot {
			return protocol.Packet{}, true, fmt.Errorf(
				"%w: protocol %s cannot encode %s: slot %d is outside the hotbar",
				version.ErrUnsupportedAction, ProtocolID, value.ActionKind(), value.Slot)
		}

		return play47("held_item_slot", &gen.PlayServerboundHeldItemSlot{
			SlotID: int16(value.Slot),
		}), true, nil

	case version.ActionSwing:
		// 47's animation packet carries no hand, so the field is dropped. A
		// main-hand swing and an offhand swing are the same bytes here, which
		// is what a protocol with one hand means.
		return play47("arm_animation", &gen.PlayServerboundArmAnimation{}), true, nil

	case version.ActionUseOn:
		return play47("block_place", &gen.PlayServerboundBlockPlace{
			Location:  wirePos47(value.Block),
			Direction: int8(value.Face),
			HeldItem:  emptySlot47(),
			CursorX:   cursorSixteenth(value.Cursor.X),
			CursorY:   cursorSixteenth(value.Cursor.Y),
			CursorZ:   cursorSixteenth(value.Cursor.Z),
		}), true, nil

	case version.ActionUseItem:
		// 47 has no use-in-air packet. It sends a placement at the sentinel
		// position with the direction byte 255, and the server reads that as
		// "used the held item where I stand". Both halves of the sentinel are
		// what a real 1.8.9 client sends.
		return play47("block_place", &gen.PlayServerboundBlockPlace{
			Location:  gen.Position{X: -1, Y: -1, Z: -1},
			Direction: useInAirDirection47,
			HeldItem:  emptySlot47(),
		}), true, nil

	case version.ActionReleaseUse:
		return play47("block_dig", &gen.PlayServerboundBlockDig{
			Status: releaseUseStatus47,
			Face:   int8(version.FaceBottom),
		}), true, nil

	case version.ActionInteract:
		return interact47(value)

	case version.ActionDig:
		status, ok := digStatus47(value.Stage)
		if !ok {
			return protocol.Packet{}, true, version.UnsupportedAction(ProtocolID, value)
		}

		return play47("block_dig", &gen.PlayServerboundBlockDig{
			Status:   status,
			Location: wirePos47(value.Block),
			Face:     int8(value.Face),
		}), true, nil

	case version.ActionDrop:
		status := dropSingleStatus47
		if value.Whole {
			status = dropStackStatus47
		}

		// No position and the bottom face, which is what vanilla sends: a drop
		// is not aimed at a block, and the fields exist only because the drop
		// shares the digging packet.
		return play47("block_dig", &gen.PlayServerboundBlockDig{
			Status: status,
			Face:   int8(version.FaceBottom),
		}), true, nil

	case version.ActionClickSlot:
		mode, ok := clickMode47(value.Mode)
		if !ok {
			return protocol.Packet{}, true, version.UnsupportedAction(ProtocolID, value)
		}

		// The transaction number is the adapter's to allocate, not the
		// caller's: 47 confirms a click by echoing it, so whoever assigns it
		// has to be whoever waits for it.
		return play47("window_click", &gen.PlayServerboundWindowClick{
			WindowID:    uint8(value.Window),
			Slot:        value.Slot,
			MouseButton: value.Button,
			Action:      a.nextTransaction(),
			Mode:        mode,
			Item:        emptySlot47(),
		}), true, nil

	case version.ActionCloseWindow:
		return play47("close_window", &gen.PlayServerboundCloseWindow{
			WindowID: uint8(value.Window),
		}), true, nil

	case version.ActionEntityAction:
		id, ok := entityAction47(value.Kind)
		if !ok {
			return protocol.Packet{}, true, version.UnsupportedAction(ProtocolID, value)
		}

		return play47("entity_action", &gen.PlayServerboundEntityAction{ActionID: id}), true, nil

	case version.ActionSprint:
		state := version.SprintStop
		if value.Sprinting {
			state = version.SprintStart
		}

		return a.encodeInteraction(version.ActionEntityAction{Kind: state})

	case version.ActionSwapHands:
		return protocol.Packet{}, true, version.UnsupportedAction(ProtocolID, value)

	case version.ActionChat:
		return play47("chat", &gen.PlayServerboundChat{Message: value.Message}), true, nil

	default:
		return protocol.Packet{}, false, nil
	}
}

// interact47 encodes an entity interaction.
//
// One packet with a mode, which is how 1.8.9 models it: C02PacketUseEntity
// carries an INTERACT, ATTACK, INTERACT_AT enumeration and writes the position
// only for the last of the three.
func interact47(value version.ActionInteract) (protocol.Packet, bool, error) {
	packet := &gen.PlayServerboundUseEntity{Target: value.Entity}

	switch value.Kind {
	case version.InteractAttack:
		packet.Mouse = attackMouse47
	case version.InteractUse:
		packet.Mouse = interactMouse47
	case version.InteractUseAt:
		if value.At == nil {
			return protocol.Packet{}, true, fmt.Errorf(
				"%w: protocol %s cannot encode %s: use-at carries no position",
				version.ErrUnsupportedAction, ProtocolID, value.ActionKind())
		}

		packet.Mouse = interactAtMouse
		packet.X = gen.PlayServerboundUseEntityXSwitch{Case2: value.At.X}
		packet.Y = gen.PlayServerboundUseEntityYSwitch{Case2: value.At.Y}
		packet.Z = gen.PlayServerboundUseEntityZSwitch{Case2: value.At.Z}
	default:
		return protocol.Packet{}, true, version.UnsupportedAction(ProtocolID, value)
	}

	return play47("use_entity", packet), true, nil
}

// digStatus47 maps a dig stage to its status.
func digStatus47(stage version.DigStage) (int32, bool) {
	switch stage {
	case version.DigStart:
		return digStartStatus47, true
	case version.DigCancel:
		return digCancelStatus47, true
	case version.DigFinish:
		return digFinishStatus47, true
	default:
		return 0, false
	}
}

// clickMode47 maps a click mode to its number.
func clickMode47(mode version.ClickMode) (int8, bool) {
	switch mode {
	case version.ClickPickup:
		return pickupMode47, true
	case version.ClickQuickMove:
		return quickMoveMode47, true
	case version.ClickSwapHotbar:
		return swapHotbarMode47, true
	case version.ClickMiddle:
		return middleMode47, true
	case version.ClickDrop:
		return dropMode47, true
	case version.ClickDrag:
		return dragMode47, true
	case version.ClickDoubleClick:
		return doubleClickMode47, true
	default:
		return 0, false
	}
}

// entityAction47 maps a declared body state to this protocol's number.
//
// Two members have no number here. A horse jump is one gesture in 1.8.9 rather
// than a start and a stop, and there is no elytra to start flying with.
func entityAction47(kind version.EntityActionKind) (int32, bool) {
	switch kind {
	case version.SneakStart:
		return sneakStartAction47, true
	case version.SneakStop:
		return sneakStopAction47, true
	case version.LeaveBed:
		return leaveBedAction47, true
	case version.SprintStart:
		return sprintStartAction47, true
	case version.SprintStop:
		return sprintStopAction47, true
	case version.HorseJumpStart:
		return horseJumpAction47, true
	case version.OpenVehicleInventory:
		return openVehicleAction47, true
	case version.HorseJumpStop, version.ElytraFlyStart:
		return 0, false
	default:
		return 0, false
	}
}
