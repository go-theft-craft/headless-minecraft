package version

import "fmt"

// ActionHeldSlot selects a hotbar slot.
//
// It is the first thing every other interaction needs. Attacking with a sword,
// placing a block, casting a rod, and raising a shield all begin here, and a
// client that cannot select a slot uses whatever it happens to be holding.
type ActionHeldSlot struct {
	// Slot is the hotbar index, 0 through 8.
	Slot uint8
}

// ActionKind implements Action.
func (ActionHeldSlot) ActionKind() string { return "held_slot" }

// ActionSwing swings the arm.
//
// Both protocols send it separately from the attack, and vanilla sends both.
// A client that attacks without swinging hits without any visible motion,
// which other players see and an anti-cheat notices.
type ActionSwing struct {
	// Hand is which arm swings. Protocol 47 has no offhand and ignores it.
	Hand Hand
}

// ActionKind implements Action.
func (ActionSwing) ActionKind() string { return "swing" }

// InteractKind names what an interaction with an entity does.
//
// Attack is a mode here rather than an action of its own, because that is what
// the game itself models: 1.8.9's C02PacketUseEntity and 26.1.2's
// ServerboundInteractPacket both carry an action enumeration whose members are
// interact, attack, and interact-at. A separate ActionAttack would split one
// game concept in two.
//
// How each protocol *encodes* that enumeration differs, and the adapters absorb
// it. Protocol 47 sends one packet with a mode field; the pinned protocol 775
// schema splits attack onto a packet of its own. Neither shape reaches a caller.
type InteractKind uint8

const (
	// InteractAttack hits the entity.
	InteractAttack InteractKind = iota
	// InteractUse right-clicks the entity.
	InteractUse
	// InteractUseAt right-clicks a point on the entity. The point decides
	// where a saddle, a name tag, or an armour stand's item goes.
	InteractUseAt
)

// String returns the interaction's name.
func (k InteractKind) String() string {
	switch k {
	case InteractAttack:
		return "attack"
	case InteractUse:
		return "use"
	case InteractUseAt:
		return "use_at"
	default:
		return fmt.Sprintf("InteractKind(%d)", uint8(k))
	}
}

// ActionUseItem uses the held item where the player stands: eat, drink, cast a
// rod, raise a shield.
type ActionUseItem struct {
	Hand Hand
}

// ActionKind implements Action.
func (ActionUseItem) ActionKind() string { return "use_item" }

// ActionUseOn uses the held item against a block: place, open, till.
type ActionUseOn struct {
	Block  BlockPos
	Face   Face
	Cursor Cursor
	Hand   Hand
}

// ActionKind implements Action.
func (ActionUseOn) ActionKind() string { return "use_on" }

// ActionReleaseUse stops using the held item: fire the bow, lower the shield,
// stop eating.
//
// Without it a drawn bow stays drawn. Every use that has a duration needs a
// way to end, and no protocol infers it.
type ActionReleaseUse struct {
	Hand Hand
}

// ActionKind implements Action.
func (ActionReleaseUse) ActionKind() string { return "release_use" }

// ActionInteract attacks or interacts with an entity.
type ActionInteract struct {
	Entity int32
	Kind   InteractKind
	// At is where on the entity the interaction landed. It is required for
	// InteractUseAt and ignored otherwise.
	At   *Cursor
	Hand Hand
	// Sneaking is what the player claims about crouching as it interacts.
	// Protocol 775 carries it and reads it — sneaking suppresses a block's own
	// interaction — and protocol 47 has no field for it and drops it.
	Sneaking bool
}

// ActionKind implements Action.
func (ActionInteract) ActionKind() string { return "interact" }
