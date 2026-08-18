package client

import (
	"context"
	"fmt"

	"github.com/go-theft-craft/headless-minecraft/version"
)

// Place uses the held item against one face of one block.
//
// It is placement seen from the wire: the packet says which cell was clicked,
// which face, and where in that face the cursor landed, and the server decides
// what — if anything — appears. A client that sends a cell and a face without a
// cursor gets a slab in the wrong half and a stair facing the wrong way, so the
// cursor is computed here rather than left to a caller to remember.
//
// The cursor is the centre of the clicked face, which is where a player aiming
// at a block lands most of the time and is the one point that is unambiguous
// for every face. A caller that needs a particular half — the top half of a
// slab, from the side — sends version.ActionUseOn itself with the cursor it
// wants. Nothing here hides that action; this is the common case with the
// arithmetic done.
//
// **It does not aim, and the orientation of an orientable block depends on
// aim.** A stair, a furnace, and a piston take their facing from the yaw the
// *server* has for the player, so a caller that wants one facing a particular
// way sends version.ActionLook first and lets the server see it. Computing that
// yaw needs the player's own position and the geometry to turn two points into
// an angle, which is the aiming plan's, not this function's.
//
// It waits for nothing. A placement is not a request with a reply: the server
// answers by changing the world, and what the client learns arrives as a world
// event like every other block change. A caller that needs to know the block
// appeared subscribes to event.DomainWorld before calling this and reads the
// change; a Place that blocked on one would be a Place that never returns when
// the server refuses, which is exactly when a caller needs to know.
func (c *Client) Place(ctx context.Context, block version.BlockPos, face version.Face) error {
	if err := c.Do(ctx, version.ActionUseOn{
		Block:  block,
		Face:   face,
		Cursor: FaceCentre(face),
		Hand:   version.MainHand,
	}); err != nil {
		return fmt.Errorf("place against %v: %w", face, err)
	}

	return nil
}

// FaceCentre is the middle of one face of a block, in block-local coordinates.
//
// The two axes that run across the face are halfway, and the axis the face
// faces along is at that face: the top of a block is y = 1, the bottom is
// y = 0. Both games read the cursor's height to decide a slab's half, so the
// exact value on the facing axis is not decoration.
func FaceCentre(face version.Face) version.Cursor {
	centre := version.Cursor{X: 0.5, Y: 0.5, Z: 0.5}
	switch face {
	case version.FaceBottom:
		centre.Y = 0
	case version.FaceTop:
		centre.Y = 1
	case version.FaceNorth:
		centre.Z = 0
	case version.FaceSouth:
		centre.Z = 1
	case version.FaceWest:
		centre.X = 0
	case version.FaceEast:
		centre.X = 1
	}

	return centre
}
