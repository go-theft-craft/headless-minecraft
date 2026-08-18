package main

import (
	"testing"

	simgeom "github.com/go-theft-craft/minecraft-simulation/geom"
	simprofile "github.com/go-theft-craft/minecraft-simulation/sim"
	"github.com/go-theft-craft/minecraft-simulation/terrain"
	simworld "github.com/go-theft-craft/minecraft-simulation/world"
)

// TestTheFactsResolveAgainstBothProfiles pins that the named blocks are names
// the profiles actually know.
//
// The list is written by hand, which makes a typo silent: a misspelled block
// resolves to nothing, the set is one entry smaller, and the bot walks into the
// thing the entry was there to avoid. Water and lava exist in both versions and
// are the two that matter most, so both versions are checked for both.
func TestTheFactsResolveAgainstBothProfiles(t *testing.T) {
	t.Parallel()

	for name, legacy := range map[string]bool{"26.1": false, "1.8.9": true} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			profile, _, err := versionTerrain(legacy)
			if err != nil {
				t.Fatalf("versionTerrain: %v", err)
			}
			names, ok := profile.(simprofile.BlockNames)
			if !ok {
				t.Fatal("the profile cannot resolve block names")
			}
			facts := NewFacts(profile)

			for block, want := range map[string]terrain.Fluid{
				"water": terrain.FluidWater,
				"lava":  terrain.FluidLava,
			} {
				ref, known := names.Ref(block)
				if !known {
					t.Fatalf("%s does not know %q", name, block)
				}
				if got := facts.Fluid(ref); got != want {
					t.Errorf("%s: %s is fluid %v, want %v", name, block, got, want)
				}
			}

			// Fire and cactus are in both versions and are the two hazards a
			// bot on open ground actually meets.
			for block, want := range map[string]terrain.Hazard{
				"fire":   terrain.HazardBurn,
				"lava":   terrain.HazardBurn,
				"cactus": terrain.HazardContact,
			} {
				ref, known := names.Ref(block)
				if !known {
					t.Fatalf("%s does not know %q", name, block)
				}
				if got := facts.Hazard(ref); got != want {
					t.Errorf("%s: %s is hazard %v, want %v", name, block, got, want)
				}
			}

			// And ordinary ground is neither, which is the case that would
			// strand the bot if the sets were built wrong.
			ref, known := names.Ref("stone")
			if !known {
				t.Fatalf("%s does not know stone", name)
			}
			if facts.Hazard(ref) != terrain.HazardNone || facts.Fluid(ref) != terrain.FluidNone {
				t.Errorf("%s: stone reads as hazardous or wet", name)
			}
		})
	}
}

// hazardSlab is a floor with one column of something harmful in it.
type hazardSlab struct {
	*slab
	harmful map[simgeom.BlockPos]bool
}

const harmfulRef simworld.BlockRef = 99

func (h hazardSlab) BlockState(pos simgeom.BlockPos) (simworld.BlockRef, simworld.Lookup) {
	if h.harmful[pos] {
		return harmfulRef, simworld.LookupAir
	}

	return h.slab.BlockState(pos)
}

// TestATautRouteWillNotCutThroughFire pins that smoothing asks what the search
// asked.
//
// Fits and Ground answer about geometry, and a body fits inside a fire
// perfectly well. A smoother that only asked those would pull a route taut
// straight through the thing the search had just routed around, which is a
// worse bot than one that never smoothed at all.
func TestATautRouteWillNotCutThroughFire(t *testing.T) {
	t.Parallel()

	view := hazardSlab{slab: newSlab(), harmful: map[simgeom.BlockPos]bool{
		{X: 1, Y: 64, Z: 1}: true,
		{X: 2, Y: 64, Z: 2}: true,
	}}
	navigator := Navigator{facts: Facts{burn: map[simworld.BlockRef]bool{harmfulRef: true}}}
	query := terrain.Query{
		View:  view,
		Facts: navigator.facts,
		Body:  terrain.Body{HalfWidth: 0.3, Height: 1.8},
	}

	staircase := []Vec3{
		{X: 0.5, Y: 64, Z: 0.5},
		{X: 1.5, Y: 64, Z: 0.5},
		{X: 2.5, Y: 64, Z: 1.5},
		{X: 3.5, Y: 64, Z: 3.5},
	}

	for _, point := range navigator.taut(query, staircase) {
		if cell := cellOf(point); view.harmful[cell] {
			t.Errorf("the taut route stands in fire at %+v", cell)
		}
	}

	// And the diagonal straight across both fires is refused outright.
	if navigator.clearLine(query, staircase[0], staircase[len(staircase)-1]) {
		t.Error("a line through two fires reported itself clear")
	}
}
