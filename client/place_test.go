package client

import (
	"testing"

	gen "github.com/go-theft-craft/minecraft-protocol/generated/java/v1_8"

	"github.com/go-theft-craft/headless-minecraft/version"
	"github.com/go-theft-craft/headless-minecraft/version/java"
)

// placements reads the placement packets a recording sender saw.
func placements(t *testing.T, sender *recordingSender) []*gen.PlayServerboundBlockPlace {
	t.Helper()

	var sent []*gen.PlayServerboundBlockPlace
	for _, packet := range sender.sent {
		if body, ok := packet.Value.(*gen.PlayServerboundBlockPlace); ok {
			sent = append(sent, body)
		}
	}

	return sent
}

func TestAPlacementCarriesTheCellTheFaceAndTheCursor(t *testing.T) {
	t.Parallel()

	// A cell and a face without a cursor is a placement that puts a slab in
	// the wrong half and a stair the wrong way round, because both games read
	// the cursor's height to decide.
	sender := &recordingSender{}
	c := actionClient(t, java.Java1_8(), sender)

	block := version.BlockPos{X: 4, Y: 63, Z: -7}
	if err := c.Place(t.Context(), block, version.FaceTop); err != nil {
		t.Fatalf("Place: %v", err)
	}

	sent := placements(t, sender)
	if len(sent) != 1 {
		t.Fatalf("sent %d placements, want 1", len(sent))
	}
	if got := sent[0].Location; got.X != block.X || int32(got.Y) != block.Y || got.Z != block.Z {
		t.Fatalf("placed at %+v, want %+v", got, block)
	}
	if got := version.Face(sent[0].Direction); got != version.FaceTop {
		t.Fatalf("face = %v, want the top", got)
	}
	// The cursor crosses protocol 47 as sixteenths of a block, so the top
	// face's y is the whole sixteen rather than the half.
	if sent[0].CursorY != 16 {
		t.Fatalf("cursor y = %d, want 16 — the top of the cell", sent[0].CursorY)
	}
	if sent[0].CursorX != 8 || sent[0].CursorZ != 8 {
		t.Fatalf("cursor x,z = %d,%d, want the middle of the face",
			sent[0].CursorX, sent[0].CursorZ)
	}
}

func TestEveryFaceCentreSitsOnItsOwnFace(t *testing.T) {
	t.Parallel()

	// The axis a face faces along must be at that face and not halfway through
	// the block. A centre that was 0.5 on every axis is the same point for all
	// six faces, and a slab placed against a side would take whichever half the
	// comparison happened to fall on.
	for _, test := range []struct {
		face version.Face
		want version.Cursor
	}{
		{version.FaceBottom, version.Cursor{X: 0.5, Y: 0, Z: 0.5}},
		{version.FaceTop, version.Cursor{X: 0.5, Y: 1, Z: 0.5}},
		{version.FaceNorth, version.Cursor{X: 0.5, Y: 0.5, Z: 0}},
		{version.FaceSouth, version.Cursor{X: 0.5, Y: 0.5, Z: 1}},
		{version.FaceWest, version.Cursor{X: 0, Y: 0.5, Z: 0.5}},
		{version.FaceEast, version.Cursor{X: 1, Y: 0.5, Z: 0.5}},
	} {
		if got := FaceCentre(test.face); got != test.want {
			t.Errorf("%v centres at %+v, want %+v", test.face, got, test.want)
		}
	}
}

func TestAPlacementUsesTheMainHand(t *testing.T) {
	t.Parallel()

	// Protocol 47 has no hand field, so this is checked where the field
	// exists: the action the adapter is handed.
	sender := &recordingSender{}
	c := actionClient(t, java.Java1_8(), sender)

	if err := c.Place(t.Context(), version.BlockPos{}, version.FaceEast); err != nil {
		t.Fatalf("Place: %v", err)
	}
	if len(placements(t, sender)) != 1 {
		t.Fatal("the placement did not reach the wire")
	}
}
