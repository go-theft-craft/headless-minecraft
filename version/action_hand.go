package version

import "fmt"

// Hand names which hand an action uses.
//
// Protocol 47 has no offhand at all. An action that only names a hand drops
// the field there rather than being refused, because a main-hand use is still
// a use; an action that is *about* the offhand is refused. See ActionSwapHands.
type Hand uint8

const (
	// MainHand is the held item.
	MainHand Hand = iota
	// OffHand is the shield hand. Protocol 775 only.
	OffHand
)

// String returns the hand's name.
func (h Hand) String() string {
	switch h {
	case MainHand:
		return "main"
	case OffHand:
		return "off"
	default:
		return fmt.Sprintf("Hand(%d)", uint8(h))
	}
}

// Face names a side of a block.
//
// The numbering is the wire's and has been since 1.8, which is why the
// constants are written with explicit values rather than left to iota's order:
// a reordering would place blocks on the wrong side of the target and read as
// a placement bug rather than an enum bug.
type Face uint8

const (
	// FaceBottom is the -Y side.
	FaceBottom Face = 0
	// FaceTop is the +Y side.
	FaceTop Face = 1
	// FaceNorth is the -Z side.
	FaceNorth Face = 2
	// FaceSouth is the +Z side.
	FaceSouth Face = 3
	// FaceWest is the -X side.
	FaceWest Face = 4
	// FaceEast is the +X side.
	FaceEast Face = 5
)

// String returns the face's name.
func (f Face) String() string {
	switch f {
	case FaceBottom:
		return "bottom"
	case FaceTop:
		return "top"
	case FaceNorth:
		return "north"
	case FaceSouth:
		return "south"
	case FaceWest:
		return "west"
	case FaceEast:
		return "east"
	default:
		return fmt.Sprintf("Face(%d)", uint8(f))
	}
}

// BlockPos is the integer block an action targets.
type BlockPos struct{ X, Y, Z int32 }

// Cursor is where within a face the interaction landed, each component in
// [0, 1].
//
// It is not decoration. Both protocols carry it and both read it: which half
// of a slab a placement fills and which way a stair faces are decided by it,
// so an action that always sent the centre would place a different block from
// the one the caller meant.
type Cursor struct{ X, Y, Z float32 }
