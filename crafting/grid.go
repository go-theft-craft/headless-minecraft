// Package crafting matches crafting grids against generated recipe data and
// computes what a craft produces and consumes.
//
// The matcher is shared between versions and ingredient equality is not: on
// 1.8.9 an ingredient is an item and a metadata variant with -1 as a
// wildcard, and on 26.1.2 the flattening moved variants into distinct item
// IDs. That one rule lives behind Ingredients, implemented per version beside
// the adapters, and everything else here is arithmetic over data.Recipe.
//
// Container-item remainders — the bucket a cake recipe hands back — are out
// of scope and stated here rather than implied covered: Craft consumes one
// item from every occupied cell and returns nothing to the grid.
package crafting

import "github.com/go-theft-craft/minecraft-protocol/data"

// Cell is one crafting grid cell. An empty cell has a zero ID or a zero
// count.
//
// Count is carried even though matching ignores it, because crafting does
// not: a craft consumes one item from every occupied cell, and a shift-craft
// repeats until a cell runs out.
type Cell struct {
	ID       data.ItemID
	Metadata data.Metadata
	Count    int
}

// Empty reports the cell holding nothing.
func (c Cell) Empty() bool { return c.ID == 0 || c.Count <= 0 }

// Grid is a crafting grid, row-major.
//
// Width and Height are carried rather than inferred because a 2x2 grid is not
// a smaller 3x3: it is a different window with its own slot indices, and a
// recipe wider or taller than two cells cannot match in it at all.
type Grid struct {
	Width, Height int
	Cells         []Cell
}

// At returns the cell at (x, y).
func (g Grid) At(x, y int) Cell { return g.Cells[y*g.Width+x] }

// Trim returns the smallest sub-grid containing every non-empty cell, and its
// offset within the original.
//
// This is what makes offset matching tractable: rather than sliding a pattern
// across every position, trim the grid once and compare against the pattern
// directly. The offset comes back so a caller can tell where the match
// landed. An empty grid trims to a zero-by-zero grid rather than panicking.
func (g Grid) Trim() (Grid, int, int) {
	minX, minY := g.Width, g.Height
	maxX, maxY := -1, -1
	for y := 0; y < g.Height; y++ {
		for x := 0; x < g.Width; x++ {
			if g.At(x, y).Empty() {
				continue
			}
			minX, minY = min(minX, x), min(minY, y)
			maxX, maxY = max(maxX, x), max(maxY, y)
		}
	}
	if maxX < 0 {
		return Grid{}, 0, 0
	}

	trimmed := Grid{Width: maxX - minX + 1, Height: maxY - minY + 1}
	trimmed.Cells = make([]Cell, 0, trimmed.Width*trimmed.Height)
	for y := minY; y <= maxY; y++ {
		for x := minX; x <= maxX; x++ {
			trimmed.Cells = append(trimmed.Cells, g.At(x, y))
		}
	}

	return trimmed, minX, minY
}

// clone returns a grid whose cells do not alias the source.
func (g Grid) clone() Grid {
	cells := make([]Cell, len(g.Cells))
	copy(cells, g.Cells)

	return Grid{Width: g.Width, Height: g.Height, Cells: cells}
}
