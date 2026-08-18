package version

import "fmt"

// ClickMode is what a click does, named rather than numbered.
//
// The wire numbers differ between versions and a caller should not have to
// know either. The names are the vocabulary; each adapter maps them.
type ClickMode uint8

const (
	// ClickPickup takes or places a stack under the cursor.
	ClickPickup ClickMode = iota + 1
	// ClickQuickMove is the shift-click: move the stack to the other half of
	// the window.
	ClickQuickMove
	// ClickSwapHotbar swaps the slot with a numbered hotbar slot.
	ClickSwapHotbar
	// ClickMiddle clones the stack, and is creative-mode only.
	ClickMiddle
	// ClickDrop throws the stack out of the window.
	ClickDrop
	// ClickDrag paints a stack across several slots.
	ClickDrag
	// ClickDoubleClick gathers every matching stack onto the cursor.
	ClickDoubleClick
)

// String returns the mode's name.
func (m ClickMode) String() string {
	switch m {
	case ClickPickup:
		return "pickup"
	case ClickQuickMove:
		return "quick_move"
	case ClickSwapHotbar:
		return "swap_hotbar"
	case ClickMiddle:
		return "middle"
	case ClickDrop:
		return "drop"
	case ClickDrag:
		return "drag"
	case ClickDoubleClick:
		return "double_click"
	default:
		return fmt.Sprintf("ClickMode(%d)", uint8(m))
	}
}

// ActionClickSlot clicks one slot in one window.
//
// The window is the server's own identifier, taken from the packet that opened
// it. Zero is the player's own inventory, which is always open and never
// announced.
type ActionClickSlot struct {
	Window int32
	Slot   int16
	Button int8
	Mode   ClickMode
	// Sequence identifies this click to the confirmation path. The caller
	// allocates it, because the caller is what records the pending click and
	// waits for the answer: protocol 47 carries it on the wire and echoes it,
	// and 775 never sends it — that protocol confirms by state, and the
	// number only keys the pending record.
	Sequence int16
	// Claim is what the client believes the clicked slot holds, as this
	// protocol's own decoded stack straight from the world store; nil claims
	// an empty slot. Protocol 47's server executes the click and then
	// compares this against what it computed — a wrong claim is what turns a
	// click into a rejection and a full resend — and 775 has no field for it.
	Claim any
}

// ActionKind implements Action.
func (ActionClickSlot) ActionKind() string { return "click_slot" }

// ActionDrop throws the held stack on the ground.
//
// It is not a window click. Both protocols carry it on the digging packet, at a
// sentinel position, because dropping what you hold is something you do in the
// world rather than in a window — a player with no window open can still do it.
type ActionDrop struct {
	// Whole drops the entire stack rather than one item from it.
	Whole bool
}

// ActionKind implements Action.
func (ActionDrop) ActionKind() string { return "drop" }

// ActionCloseWindow closes an open window.
//
// Vanilla drops whatever is on the cursor when a window closes, on both
// versions, so a client that keeps believing in that stack believes in an item
// that is now on the floor.
type ActionCloseWindow struct {
	Window int32
}

// ActionKind implements Action.
func (ActionCloseWindow) ActionKind() string { return "close_window" }
