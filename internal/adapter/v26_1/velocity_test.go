package v26_1_test

import (
	"testing"

	protocol "github.com/go-theft-craft/minecraft-protocol"
	gen "github.com/go-theft-craft/minecraft-protocol/generated/java/v26_1"
	"github.com/go-theft-craft/minecraft-protocol/wire/java"
)

// TestSpawnVelocityDecodesWhatAVanillaServerSent pins an observed velocity to
// bytes rather than to a struct literal.
//
// Every other test in this package builds a packet value, so no decoder runs in
// them — and a byte order that is wrong in both directions round-trips
// perfectly. One was: minecraft-protocol before v0.6.0 read the upper
// thirty-two bits of a quantised vector little endian where vanilla writes them
// big endian, so a real server's velocity decoded into a plausible number
// unrelated to the entity's motion.
//
// The bytes are minecraft-protocol's own fixture, shared with
// TestLPVec3ReadsWhatAVanillaServerSent: the velocity field of the spawn
// packets of two arrows, captured through the relay proxy from a pinned vanilla
// 26.1.2 server on 2026-08-18 and summoned with the motion the operator stated:
//
//	summon minecraft:arrow -4.5 -55.0 9.5  {Motion:[0.10d,0.0d,0.0d]}
//	summon minecraft:arrow -4.5 -52.0 11.5 {Motion:[0.0d,0.0d,0.05d]}
func TestSpawnVelocityDecodesWhatAVanillaServerSent(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		encoded []byte
		want    [3]int16
	}{
		// The encoding keeps fifteen bits per component, so 0.1 comes back as
		// 0.0999816883 — 799.85 in the 1/8000 units the world records, and
		// velocity775 truncates rather than rounds. The unit is on a value the
		// encoding already quantised, and nothing reads it as anything but a
		// report, so this asserts what the code does.
		{
			name:    "an arrow summoned with 0.1 on X",
			encoded: []byte{0x29, 0x33, 0x7f, 0xfe, 0xff, 0xfe},
			want:    [3]int16{799, 0, 0},
		},
		{
			name:    "an arrow summoned with 0.05 on Z",
			encoded: []byte{0xf9, 0xff, 0x86, 0x64, 0xff, 0xfd},
			want:    [3]int16{0, 0, 399},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			limits, err := protocol.NewLimits()
			if err != nil {
				t.Fatal(err)
			}
			buffer, err := java.NewReadBuffer(tc.encoded, limits)
			if err != nil {
				t.Fatal(err)
			}
			velocity, err := buffer.ReadLPVec3("velocity")
			if err != nil {
				t.Fatalf("ReadLPVec3: %v", err)
			}

			w, _ := script(t, []protocol.Packet{
				playLogin(1),
				play(&gen.PlayClientboundSpawnEntity{
					EntityID: 7, Type: 3,
					X: -4.5, Y: -55, Z: 9.5,
					Velocity: velocity,
				}),
			})

			entity, ok := w.Snapshot().Entities.Get(7)
			if !ok {
				t.Fatal("the arrow is not tracked")
			}
			if entity.Velocity != tc.want {
				t.Errorf("velocity is %v, want %v — the motion the server was told to give it",
					entity.Velocity, tc.want)
			}
		})
	}
}
