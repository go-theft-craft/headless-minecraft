package version

import "fmt"

// EntityActionKind names a state a player declares about its own body.
//
// The vocabulary is the union of what the two games define, because neither
// one's list is a superset of the other's: 1.8.9 declares sneaking here and
// 26.1.2 moved it onto the input packet, while 26.1.2 splits a horse jump into
// a start and a stop where 1.8.9 has one member and 26.1.2 alone can start
// elytra flight. A member a protocol does not have is refused rather than
// mapped to a neighbour, because these are numbered enumerations and a
// neighbouring number is a different action performed confidently.
//
// Measured on 2026-08-18 from the deobfuscated jars this project pins:
// 1.8.9's C0BPacketEntityAction.Action and 26.1.2's
// ServerboundPlayerCommandPacket.Action.
type EntityActionKind uint8

const (
	// SneakStart begins crouching. Protocol 47 only; 775 reports sneaking
	// through ActionInput instead.
	SneakStart EntityActionKind = iota
	// SneakStop stops crouching. Protocol 47 only, as SneakStart.
	SneakStop
	// LeaveBed wakes the player up.
	LeaveBed
	// SprintStart begins sprinting.
	SprintStart
	// SprintStop stops sprinting.
	SprintStop
	// HorseJumpStart charges a ridden horse's jump.
	HorseJumpStart
	// HorseJumpStop releases it. Protocol 775 only: 47 has one member for the
	// whole gesture and no way to say the second half of it.
	HorseJumpStop
	// OpenVehicleInventory opens the inventory of whatever is being ridden.
	OpenVehicleInventory
	// ElytraFlyStart begins elytra flight. Protocol 775 only; 1.8.9 has no
	// elytra.
	ElytraFlyStart
)

// String returns the state's name.
func (k EntityActionKind) String() string {
	switch k {
	case SneakStart:
		return "sneak_start"
	case SneakStop:
		return "sneak_stop"
	case LeaveBed:
		return "leave_bed"
	case SprintStart:
		return "sprint_start"
	case SprintStop:
		return "sprint_stop"
	case HorseJumpStart:
		return "horse_jump_start"
	case HorseJumpStop:
		return "horse_jump_stop"
	case OpenVehicleInventory:
		return "open_vehicle_inventory"
	case ElytraFlyStart:
		return "elytra_fly_start"
	default:
		return fmt.Sprintf("EntityActionKind(%d)", uint8(k))
	}
}

// ActionEntityAction declares one state about the player's own body.
//
// It is one action with a named state rather than one action per state,
// because the wire is one packet with an enumeration and splitting it would
// give every future member a Go type nobody asked for.
type ActionEntityAction struct {
	Kind EntityActionKind
}

// ActionKind implements Action.
func (ActionEntityAction) ActionKind() string { return "entity_action" }

// ActionSwapHands swaps the main-hand and offhand stacks.
//
// Protocol 47 refuses it. This is the refusal side of the rule the interaction
// actions follow: a hand is a field ActionSwing can drop on a one-handed
// protocol and still mean what it meant, but a hand swap on a protocol with one
// hand is not a field to drop — it is the whole intent.
type ActionSwapHands struct{}

// ActionKind implements Action.
func (ActionSwapHands) ActionKind() string { return "swap_hands" }

// ActionChat says something in chat.
//
// Unsigned, for the reason ActionCommand states: the signed form carries a
// timestamp, a salt, and a signature over an account this client is not.
// A server that requires signed chat refuses this, which is the honest outcome.
//
// A message beginning with a slash is a command on both versions, and
// ActionCommand is the way to say so. Sending one here works on 47 and works on
// 775 too — but it takes the chat path, where a command takes the command path,
// and the two differ in what the server logs and what it signs.
type ActionChat struct {
	Message string
}

// ActionKind implements Action.
func (ActionChat) ActionKind() string { return "chat" }
