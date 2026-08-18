package crafting

import (
	"slices"

	"github.com/go-theft-craft/minecraft-protocol/data"
)

// emptyItem is the item ID a shaped recipe's empty cells carry in the
// generated data: air, on both versions.
const emptyItem data.ItemID = 0

// Match finds the recipe a grid produces, if any.
//
// It walks the whole registry rather than indexing by a candidate result,
// because the caller has a grid and not a guess — and it walks it in item-ID
// order, because a map walk would make two identical grids match different
// recipes on different runs when more than one fits.
//
// A shaped recipe matches at every offset that fits: the grid is trimmed to
// its bounding box once, which is the same thing as sliding the pattern —
// with the requirement the game also imposes that every cell outside the
// pattern be empty. Both versions also try the horizontally mirrored
// pattern for every shaped recipe — 1.8.9's ShapedRecipes.matches checks each
// offset both ways and 26.1.2's ShapedRecipePattern does the same — which the
// mirror corpus in client/testdata confirms against both jars rather than
// leaving as a reading.
func Match(reg data.RecipeRegistry, ing Ingredients, g Grid) (data.Recipe, bool) {
	trimmed, _, _ := g.Trim()
	if trimmed.Width == 0 {
		return data.Recipe{}, false
	}

	index := reg.All()
	ids := make([]data.ItemID, 0, len(index))
	for id := range index {
		ids = append(ids, id)
	}
	slices.Sort(ids)

	for _, id := range ids {
		for _, recipe := range index[id] {
			if matches(recipe, ing, g, trimmed) {
				return recipe, true
			}
		}
	}

	return data.Recipe{}, false
}

// matches answers one recipe against one pre-trimmed grid.
func matches(recipe data.Recipe, ing Ingredients, g, trimmed Grid) bool {
	if len(recipe.InShape) > 0 {
		pattern := patternOf(recipe.InShape)
		if pattern.Width > g.Width || pattern.Height > g.Height {
			// A 3x3 recipe cannot fit a 2x2 player grid however it slides.
			return false
		}

		return shapedMatch(pattern, ing, trimmed) ||
			shapedMatch(mirror(pattern), ing, trimmed)
	}
	if len(recipe.Ingredients) > 0 {
		return shapelessMatch(recipe.Ingredients, ing, trimmed)
	}

	return false
}

// pattern is a shaped recipe's shape with its dimensions made explicit.
type pattern struct {
	Width, Height int
	Cells         []data.Ingredient
}

// at returns the pattern cell at (x, y). A ragged row's missing cells are
// empty.
func (p pattern) at(x, y int) data.Ingredient { return p.Cells[y*p.Width+x] }

// patternOf normalises a recipe shape: rows padded to the widest, then
// trimmed to the bounding box of its non-empty cells, so a shape stated with
// an empty edge compares the same as one stated without.
func patternOf(shape data.RecipeShape) pattern {
	width := 0
	for _, row := range shape {
		width = max(width, len(row))
	}

	p := pattern{Width: width, Height: len(shape)}
	p.Cells = make([]data.Ingredient, 0, width*len(shape))
	for _, row := range shape {
		for x := range width {
			if x < len(row) {
				p.Cells = append(p.Cells, row[x])
			} else {
				p.Cells = append(p.Cells, data.Ingredient{ID: emptyItem})
			}
		}
	}

	return trimPattern(p)
}

// trimPattern is Grid.Trim for a pattern.
func trimPattern(p pattern) pattern {
	minX, minY := p.Width, p.Height
	maxX, maxY := -1, -1
	for y := range p.Height {
		for x := range p.Width {
			if p.at(x, y).ID == emptyItem {
				continue
			}
			minX, minY = min(minX, x), min(minY, y)
			maxX, maxY = max(maxX, x), max(maxY, y)
		}
	}
	if maxX < 0 {
		return pattern{}
	}

	trimmed := pattern{Width: maxX - minX + 1, Height: maxY - minY + 1}
	trimmed.Cells = make([]data.Ingredient, 0, trimmed.Width*trimmed.Height)
	for y := minY; y <= maxY; y++ {
		for x := minX; x <= maxX; x++ {
			trimmed.Cells = append(trimmed.Cells, p.at(x, y))
		}
	}

	return trimmed
}

// mirror flips a pattern horizontally.
func mirror(p pattern) pattern {
	flipped := pattern{Width: p.Width, Height: p.Height}
	flipped.Cells = make([]data.Ingredient, 0, len(p.Cells))
	for y := range p.Height {
		for x := p.Width - 1; x >= 0; x-- {
			flipped.Cells = append(flipped.Cells, p.at(x, y))
		}
	}

	return flipped
}

// shapedMatch compares a trimmed grid against a trimmed pattern cell by cell.
//
// The dimensions must agree exactly: the grid was trimmed to its occupied
// bounding box, so a grid larger than the pattern has an occupied cell
// outside it — the stray-ingredient case the game refuses — and a smaller one
// is missing part of the pattern.
func shapedMatch(p pattern, ing Ingredients, trimmed Grid) bool {
	if p.Width != trimmed.Width || p.Height != trimmed.Height {
		return false
	}
	for y := range p.Height {
		for x := range p.Width {
			want, cell := p.at(x, y), trimmed.At(x, y)
			if want.ID == emptyItem {
				if !cell.Empty() {
					return false
				}

				continue
			}
			if cell.Empty() || !ing.Satisfies(cell, want) {
				return false
			}
		}
	}

	return true
}

// shapelessMatch pairs every occupied cell with exactly one ingredient.
//
// Shapeless means arrangement-free, not quantity-free: the counts must agree
// exactly, and the pairing backtracks because a cell can satisfy more than
// one ingredient when wildcards are involved and a greedy pass can strand a
// later cell.
func shapelessMatch(wants data.RecipeIngredients, ing Ingredients, trimmed Grid) bool {
	occupied := make([]Cell, 0, len(trimmed.Cells))
	for _, cell := range trimmed.Cells {
		if !cell.Empty() {
			occupied = append(occupied, cell)
		}
	}
	if len(occupied) != len(wants) {
		return false
	}

	used := make([]bool, len(wants))

	var pair func(at int) bool
	pair = func(at int) bool {
		if at == len(occupied) {
			return true
		}
		for w := range wants {
			if used[w] || !ing.Satisfies(occupied[at], wants[w]) {
				continue
			}
			used[w] = true
			if pair(at + 1) {
				return true
			}
			used[w] = false
		}

		return false
	}

	return pair(0)
}
