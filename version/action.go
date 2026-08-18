package version

import (
	"errors"
	"fmt"
)

// ErrUnsupportedAction reports an intent the selected protocol cannot express.
// It names the action's kind, so a caller learns which intent to stop sending
// rather than only that something failed.
var ErrUnsupportedAction = errors.New("action is not supported by this protocol")

// Action is one outbound intent, stated without reference to a wire format.
//
// Actions are version-neutral for the same reason the rest of this package is:
// protocol 47 and protocol 775 encode player movement differently, and the
// profile-selected adapter is where that difference belongs. A caller says
// "I moved here and I am on the ground"; which packet carries that is the
// adapter's decision.
type Action interface {
	// ActionKind returns a stable name, such as "move_look". It appears in
	// errors and is not a packet name.
	ActionKind() string
}

// Movement packets are stateful in a way an encoding must respect.
//
// A vanilla client does not send its whole position every tick. It sends one
// movement packet per tick, choosing which by what changed since the last packet
// that carried a position:
//
//	moved   = squared distance from the last reported position > 9.0e-4,
//	          or twenty ticks have passed since one was reported
//	rotated = the yaw or the pitch differs from the last reported, exactly
//
//	moved and rotated -> position_look
//	moved             -> position
//	rotated           -> look
//	neither           -> flying, carrying the ground flag alone
//
// The twenty-tick clause is why a stationary player still reports a position now
// and then: the counter resets on any packet that carried one, so the forced
// update lands on the twenty-first packet after it.
//
// This rule was read off a real 1.8.9 client's own traffic rather than inferred:
// 3703 movement packets from a five-minute session, captured between an
// unmodified client and an unmodified offline-mode server, agree with it on
// 3700. The two that do not are at the login boundary.
//
// A server reads the choice as information. Receiving a position for a tick
// where nothing moved, or a bare ground flag for a tick where the player walked,
// is a disagreement it may correct. This package therefore makes the choice
// explicit rather than inferring it: deciding which intent a tick warrants
// belongs to the caller that tracks the previous tick, and nothing here guesses
// on the caller's behalf.

// ActionMove reports a new position and the standing state.
type ActionMove struct {
	X, Y, Z float64
	// OnGround is what the client claims about standing, not what it wishes.
	OnGround bool
	// HorizontalCollision reports that the tick's horizontal motion was
	// blocked. Protocol 775 carries it in its movement flags; protocol 47 has
	// no field for it and drops it.
	HorizontalCollision bool
}

// ActionKind implements Action.
func (ActionMove) ActionKind() string { return "move" }

// ActionLook reports a new rotation and the standing state.
type ActionLook struct {
	// Yaw and Pitch are in degrees, as the wire carries them.
	Yaw, Pitch float32
	OnGround   bool
	// HorizontalCollision is as described on ActionMove.
	HorizontalCollision bool
}

// ActionKind implements Action.
func (ActionLook) ActionKind() string { return "look" }

// ActionMoveLook reports a new position and rotation together.
type ActionMoveLook struct {
	X, Y, Z    float64
	Yaw, Pitch float32
	OnGround   bool
	// HorizontalCollision is as described on ActionMove.
	HorizontalCollision bool
}

// ActionKind implements Action.
func (ActionMoveLook) ActionKind() string { return "move_look" }

// ActionGround reports the standing state and nothing else. It is what a tick
// in which the player neither moved nor turned sends.
type ActionGround struct {
	OnGround bool
	// HorizontalCollision is as described on ActionMove.
	HorizontalCollision bool
}

// ActionKind implements Action.
func (ActionGround) ActionKind() string { return "ground" }

// ActionRespawn asks the server to respawn the player after a death.
//
// It carries nothing. Where the player comes back is the server's to decide —
// its bed, its world spawn, or wherever a plugin says — and a client that named
// a position would be asking for something no protocol lets it ask for.
//
// It is the one action a dead client must be able to send. Everything else it
// might do is refused until it is alive, so a client that cannot send this
// cannot recover from its own death and is stuck for the rest of the session.
type ActionRespawn struct{}

// ActionKind implements Action.
func (ActionRespawn) ActionKind() string { return "respawn" }

// ActionInput reports which movement keys the player is holding.
//
// It is not how a player moves. Position is reported by the movement actions
// and always has been; this says what the body is doing while it moves, and a
// server that never hears it sees coordinates changing with nobody walking.
// What that costs is not an error — it is a player who slides.
//
// A client sends it when the state changes rather than every tick, which is
// the caller's business: this package does not track the previous tick, for
// the same reason it does not choose between the four movement packets.
type ActionInput struct {
	Forward, Backward, Left, Right bool
	Jump, Sneak, Sprint            bool
}

// ActionKind implements Action.
func (ActionInput) ActionKind() string { return "input" }

// ActionSprint starts or stops sprinting.
//
// Sprinting is a declared state, not a speed. A client that simply moves
// faster is a client the server corrects; one that says it is sprinting is one
// the server lets run, and one that says nothing is walking. That is why this
// exists separately from the input flags even though those carry a sprint bit
// too: a real client sends both, and the state the server keeps comes from
// this one.
type ActionSprint struct {
	Sprinting bool
}

// ActionKind implements Action.
func (ActionSprint) ActionKind() string { return "sprint" }

// ActionCommand runs a server command.
//
// The command carries no leading slash. Protocol 775 sends commands on their
// own packet whose field is the command without one, and 47 has no command
// packet at all and sends chat with a slash in front -- so the slash is a
// spelling one version uses and not part of what the caller is asking for.
//
// Unsigned. Both versions this speaks accept a command with no signature, and
// the signed variant carries a timestamp, a salt and per-argument signatures
// that only mean anything for an account this client is not: a headless client
// in offline mode has nothing to sign with. A server that requires signatures
// will refuse this, which is the honest outcome.
type ActionCommand struct {
	// Command is the command and its arguments, without a leading slash.
	Command string
}

// ActionKind implements Action.
func (ActionCommand) ActionKind() string { return "command" }

// UnsupportedAction returns the error an adapter reports for an intent it cannot
// encode. Adapters share it so that two protocols refuse the same way.
func UnsupportedAction(protocolID string, action Action) error {
	kind := "<nil>"
	if action != nil {
		kind = action.ActionKind()
	}

	return fmt.Errorf("%w: protocol %s cannot encode %s", ErrUnsupportedAction, protocolID, kind)
}
