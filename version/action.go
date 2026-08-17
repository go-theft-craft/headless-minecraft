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
// A vanilla client does not send its whole position every tick. It sends
// whichever packet describes what changed: position when only the coordinates
// moved, look when only the rotation did, position-and-look when both did, and
// a bare ground flag when neither did. A server reads the choice as information:
// receiving a position for a tick where nothing moved, or a bare ground flag for
// a tick where the player walked, is a disagreement it may correct.
//
// This package therefore makes the choice explicit rather than inferring it.
// Deciding which intent a tick warrants — including vanilla's habit of sending a
// full position-and-look periodically regardless of what changed — belongs to
// the caller that tracks the previous tick, and the exact cadence is what M8.8's
// vanilla gate measures. Nothing here guesses on the caller's behalf.

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

// UnsupportedAction returns the error an adapter reports for an intent it cannot
// encode. Adapters share it so that two protocols refuse the same way.
func UnsupportedAction(protocolID string, action Action) error {
	kind := "<nil>"
	if action != nil {
		kind = action.ActionKind()
	}

	return fmt.Errorf("%w: protocol %s cannot encode %s", ErrUnsupportedAction, protocolID, kind)
}
