package version

import "fmt"

// DigStage names one step of breaking a block.
type DigStage uint8

const (
	// DigStart begins breaking.
	DigStart DigStage = iota
	// DigCancel abandons a break in progress.
	DigCancel
	// DigFinish reports that the block should now be broken.
	DigFinish
)

// String returns the stage's name.
func (s DigStage) String() string {
	switch s {
	case DigStart:
		return "start"
	case DigCancel:
		return "cancel"
	case DigFinish:
		return "finish"
	default:
		return fmt.Sprintf("DigStage(%d)", uint8(s))
	}
}

// ActionDig reports one stage of breaking a block.
//
// It carries no timing. How long a block takes for a tool, a tier, and an
// effect is M9.4's measurement, and *when* to send DigFinish is therefore the
// caller's decision rather than something this package schedules. A client that
// finishes early is one the server rejects, and a package that guessed the
// interval would be guessing on every caller's behalf at once.
//
// One type with a stage rather than three types, because the wire is one packet
// with a status field. Two names for one packet is two things to keep in step.
type ActionDig struct {
	Block BlockPos
	Face  Face
	Stage DigStage
}

// ActionKind implements Action.
func (ActionDig) ActionKind() string { return "dig" }
