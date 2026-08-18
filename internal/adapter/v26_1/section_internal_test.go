package v26_1

import (
	"testing"

	gen "github.com/go-theft-craft/minecraft-protocol/generated/java/v26_1"
)

// TestSectionRecordsUnpackTheWayTheGamePacksThem pins the bit layout.
//
// The end-to-end test can only show that the packet is no longer ignored: a
// chunk this client can decode is a page of real bytes, and without one the
// change is counted rather than applied. The layout is the part worth pinning
// anyway, and it is the part that is easy to get wrong -- the axes are ordered
// x, z, y inside the twelve low bits, which is not the order they are written
// in anywhere else.
//
// SectionPos.java is the source: the position is packed & 4095, the state is
// packed >>> 12, and the axes are >>> 8, >>> 4 and >>> 0 masked to four bits.
func TestSectionRecordsUnpackTheWayTheGamePacksThem(t *testing.T) {
	t.Parallel()

	const state = 117

	for name, test := range map[string]struct {
		section gen.PlayClientboundMultiBlockChangeChunkCoordinatesBits
		x, z, y int32
		wantX   int32
		wantY   int32
		wantZ   int32
	}{
		"origin section": {
			section: gen.PlayClientboundMultiBlockChangeChunkCoordinatesBits{},
			x:       3, z: 5, y: 9,
			wantX: 3, wantY: 9, wantZ: 5,
		},
		"below zero, which is where a modern world's floor is": {
			section: gen.PlayClientboundMultiBlockChangeChunkCoordinatesBits{X: 1, Y: -4, Z: 2},
			x:       3, z: 5, y: 9,
			wantX: 16 + 3, wantY: -64 + 9, wantZ: 32 + 5,
		},
		"the far corner of a section": {
			section: gen.PlayClientboundMultiBlockChangeChunkCoordinatesBits{X: -1, Y: 0, Z: -1},
			x:       15, z: 15, y: 15,
			wantX: -16 + 15, wantY: 15, wantZ: -16 + 15,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			packed := int32(state)<<12 | test.x<<8 | test.z<<4 | test.y
			changes := sectionChanges775(&gen.PlayClientboundMultiBlockChange{
				ChunkCoordinates: test.section,
				Records:          []int32{packed},
			})

			if len(changes) != 1 {
				t.Fatalf("unpacked %d changes, want 1", len(changes))
			}
			if got := changes[0].State; got != state {
				t.Errorf("state is %d, want %d", got, state)
			}
			if got := changes[0].Pos; got.X != test.wantX || got.Y != test.wantY || got.Z != test.wantZ {
				t.Errorf("position is %+v, want (%d, %d, %d)", got, test.wantX, test.wantY, test.wantZ)
			}
		})
	}
}
