package main

// The bypass search, and the reason it is not a pathfinder: it searches one
// dimension, it visits at most 2*band+1 candidates, and it cannot recurse. The
// library's plan forbids shipping a pathfinder and this is what the example
// uses instead.

// Passability says why a position cannot be stood in, which the caller needs
// because the three answers lead to different places: air is walkable, a
// one-block step is jumped rather than avoided, and unloaded is refused.
type Passability uint8

const (
	// Clear means the bot can stand there.
	Clear Passability = iota
	// Steppable means one solid block with room above it, which a jumping bot
	// crosses without leaving the circle.
	Steppable
	// Blocked means solid and too tall to step.
	Blocked
	// Unknown means the chunk is not loaded. Strict mode refuses it, and it is
	// deliberately not folded into Blocked: a bot that treats every unloaded
	// chunk as a wall gives up at the edge of its render distance.
	Unknown
)

// Body is how tall the bot is, in blocks, for the purpose of asking whether it
// fits. Two is the vanilla player; the constant is named because a one-block
// body is a case the library's own plan tests for, and an example that hardcodes
// 2 in three places is an example that cannot be corrected in one.
const Body = 2

// Passable reports whether the bot can occupy a position.
func Passable(w World, p Vec3) Passability {
	foot := p.Floor()

	// Feet and head first: if the bot does not fit, nothing below matters.
	for h := range Body {
		block, known := w.Block(BlockPos{X: foot.X, Y: foot.Y + h, Z: foot.Z})
		if !known {
			return Unknown
		}
		if !block.Solid {
			continue
		}
		if h == 0 && steppable(w, foot) {
			return Steppable
		}

		return Blocked
	}

	// Something has to hold the bot up. A hole in the floor is not a wall, but
	// it is not somewhere to walk either, and the orbit has no business
	// falling into one.
	ground, known := w.Block(BlockPos{X: foot.X, Y: foot.Y - 1, Z: foot.Z})
	if !known {
		return Unknown
	}
	if !ground.Solid {
		return Blocked
	}

	return Clear
}

// steppable reports whether a solid block at foot level has the clearance above
// it for a jumping bot to cross.
func steppable(w World, foot BlockPos) bool {
	for h := 1; h <= Body; h++ {
		block, known := w.Block(BlockPos{X: foot.X, Y: foot.Y + h, Z: foot.Z})
		if !known || block.Solid {
			return false
		}
	}

	return true
}

// Bypass searches the radial band for an offset that clears waypoint i.
//
// Candidates are ordered by absolute offset so the search takes the smallest
// deviation that works: zero, then one in and one out, and so on to the band
// edge. It reports the offset and whether one was found.
func Bypass(w World, c Circle, i, band int) (float64, bool) {
	for _, offset := range candidates(band) {
		switch Passable(w, c.At(i, float64(offset))) {
		case Clear, Steppable:
			return float64(offset), true
		case Blocked, Unknown:
			continue
		}
	}

	return 0, false
}

// candidates returns the band's offsets ordered by distance from the circle:
// 0, -1, +1, -2, +2, and so on. Inward before outward at equal distance, which
// is arbitrary but fixed, because a search that tried them in map order would
// pick a different route on every run and make a failure impossible to
// reproduce.
func candidates(band int) []int {
	offsets := make([]int, 0, 2*band+1)
	offsets = append(offsets, 0)
	for d := 1; d <= band; d++ {
		offsets = append(offsets, -d, d)
	}

	return offsets
}
