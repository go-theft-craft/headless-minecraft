package main

import (
	"testing"

	simgeom "github.com/go-theft-craft/minecraft-simulation/geom"
	"github.com/go-theft-craft/minecraft-simulation/navigation"
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

			profile, blocks, set, err := versionTerrain(legacy)
			if err != nil {
				t.Fatalf("versionTerrain: %v", err)
			}
			names, ok := profile.(simprofile.BlockNames)
			if !ok {
				t.Fatal("the profile cannot resolve block names")
			}
			facts := NewFacts(set, blocks)

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

	staircase := []simgeom.Vec3{
		{X: 0.5, Y: 64, Z: 0.5},
		{X: 1.5, Y: 64, Z: 0.5},
		{X: 2.5, Y: 64, Z: 1.5},
		{X: 3.5, Y: 64, Z: 3.5},
	}

	for _, step := range navigator.taut(query, walked(staircase...)) {
		if cell := floorOf(step.At); view.harmful[cell] {
			t.Errorf("the taut route stands in fire at %+v", cell)
		}
	}

	// And the diagonal straight across both fires is refused outright.
	if navigator.clearLine(query, staircase[0], staircase[len(staircase)-1]) {
		t.Error("a line through two fires reported itself clear")
	}
}

// TestATautRouteWillNotSkimALavaCorner reproduces the bot catching fire.
//
// This is the failure the centre-cell check let through, seen on a live server:
// the bot walked past the corner of a lava pool, its own centre never entered a
// lava cell, and it caught fire anyway because a body is 0.6 wide and lava does
// not care where the middle of it was.
//
// The diagonal here passes corner to corner with the lava on one side. Every
// cell the route stands in is clear; the body's box is not.
func TestATautRouteWillNotSkimALavaCorner(t *testing.T) {
	t.Parallel()

	// Lava in one cell only, off the line of centres but inside the body's
	// reach as it cuts the corner.
	view := hazardSlab{slab: newSlab(), harmful: map[simgeom.BlockPos]bool{
		{X: 1, Y: 64, Z: 1}: true,
	}}
	navigator := Navigator{
		facts:      Facts{lava: map[simworld.BlockRef]bool{harmfulRef: true}},
		capability: navigation.Capability{Body: terrain.Body{HalfWidth: 0.3, Height: 1.8}},
	}
	query := terrain.Query{View: view, Facts: navigator.facts, Body: navigator.capability.Body}

	// Standing dead centre of the cell next door is fine: the box stays inside.
	if !navigator.standable(query, simgeom.Vec3{X: 0.5, Y: 64, Z: 1.5}) {
		t.Error("refused a cell whose body does not reach the lava")
	}

	// Leaning toward the corner is not. The centre cell is still clear.
	corner := simgeom.Vec3{X: 0.95, Y: 64, Z: 1.05}
	if cell := floorOf(corner); view.harmful[cell] {
		t.Fatalf("the test position is inside the lava cell %+v; it must not be", cell)
	}
	if navigator.standable(query, corner) {
		t.Error("accepted a position whose body overlaps the lava cell")
	}

	// And the shortcut across the corner is refused outright.
	if navigator.clearLine(query, simgeom.Vec3{X: 0.5, Y: 64, Z: 0.5}, simgeom.Vec3{X: 1.5, Y: 64, Z: 1.5}) {
		t.Error("a diagonal skimming the lava corner reported itself clear")
	}
}

// TestFireAtHeadHeightIsNotWalkedThrough pins the layer above the feet.
func TestFireAtHeadHeightIsNotWalkedThrough(t *testing.T) {
	t.Parallel()

	view := hazardSlab{slab: newSlab(), harmful: map[simgeom.BlockPos]bool{
		{X: 3, Y: 65, Z: 3}: true,
	}}
	navigator := Navigator{
		facts:      Facts{burn: map[simworld.BlockRef]bool{harmfulRef: true}},
		capability: navigation.Capability{Body: terrain.Body{HalfWidth: 0.3, Height: 1.8}},
	}
	query := terrain.Query{View: view, Facts: navigator.facts, Body: navigator.capability.Body}

	if navigator.standable(query, simgeom.Vec3{X: 3.5, Y: 64, Z: 3.5}) {
		t.Error("stood upright in a fire burning at head height")
	}
}

// TestTheNavigatorIsBuiltWithItsFacts pins the wiring, not the mechanism.
//
// Every other test here constructs a Navigator literal with facts in it, which
// tests that hazards are honoured and says nothing about whether the navigator
// the program actually builds has any. It did not: the field was declared, the
// sets were built, and the constructor never assigned them, so the planner ran
// with empty facts and the bot walked into lava on a live server while the
// tests were green.
func TestTheNavigatorIsBuiltWithItsFacts(t *testing.T) {
	t.Parallel()

	for name, legacy := range map[string]bool{"26.1": false, "1.8.9": true} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			navigator, err := NewNavigator(legacy, DefaultBounds())
			if err != nil {
				t.Fatalf("NewNavigator: %v", err)
			}

			if len(navigator.facts.lava) == 0 || len(navigator.facts.burn) == 0 {
				t.Fatal("the navigator was built without its facts")
			}

			// And through the resolver the terrain reads with, so a handle in
			// the set is one a cell can actually produce.
			_, blocks, set, err := versionTerrain(legacy)
			if err != nil {
				t.Fatalf("versionTerrain: %v", err)
			}
			lava, known := set.Blocks().ByName("lava")
			if !known {
				t.Fatal("no lava in the data set")
			}

			// Every state, not the default alone. The rim of a pool is the
			// flowing states, and the rim is the part anything walks into.
			for _, state := range statesOf(lava) {
				ref, ok := blocks.Ref(state)
				if !ok {
					continue
				}
				if navigator.facts.Fluid(ref) != terrain.FluidLava {
					t.Errorf("lava state %d is not known as lava", state)
				}
				if navigator.facts.Hazard(ref) != terrain.HazardBurn {
					t.Errorf("lava state %d is not known to burn", state)
				}
			}
		})
	}
}
