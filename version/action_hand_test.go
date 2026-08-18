package version_test

import (
	"testing"

	"github.com/go-theft-craft/headless-minecraft/version"
)

func TestFaceValuesMatchTheWireOrder(t *testing.T) {
	t.Parallel()

	// Both protocols number the faces the same way and have since 1.8. A
	// renumbering here places blocks on the wrong side of the target, which
	// looks like a placement bug and is an enum bug.
	for _, c := range []struct {
		face version.Face
		want uint8
	}{
		{version.FaceBottom, 0},
		{version.FaceTop, 1},
		{version.FaceNorth, 2},
		{version.FaceSouth, 3},
		{version.FaceWest, 4},
		{version.FaceEast, 5},
	} {
		if uint8(c.face) != c.want {
			t.Fatalf("%v = %d, want %d", c.face, uint8(c.face), c.want)
		}
	}
}

func TestHandStringsAreStable(t *testing.T) {
	t.Parallel()

	if version.MainHand.String() != "main" || version.OffHand.String() != "off" {
		t.Fatalf("hand names changed: %q, %q", version.MainHand, version.OffHand)
	}
}

func TestUnnamedEnumValuesStillPrintSomethingReadable(t *testing.T) {
	t.Parallel()

	// A %v on a value outside the enumeration lands in an error message, and
	// an error that says "Hand(9)" is one a reader can act on where an empty
	// string is not.
	if got := version.Hand(9).String(); got != "Hand(9)" {
		t.Errorf("Hand(9) = %q", got)
	}
	if got := version.Face(9).String(); got != "Face(9)" {
		t.Errorf("Face(9) = %q", got)
	}
}
