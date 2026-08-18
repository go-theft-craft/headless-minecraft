package crafting

import "github.com/go-theft-craft/minecraft-protocol/data"

// Craft consumes one of each ingredient and returns the result stack and the
// grid that remains.
//
// The remaining grid comes back rather than being mutated in place because a
// caller predicting a craft needs to keep the original until the server
// confirms — the same rollback requirement M9.7 built for clicks. One item
// leaves every occupied cell, which is the game's rule too; a container
// item's remainder (the bucket a cake hands back) is out of scope, as the
// package comment says.
func Craft(reg data.RecipeRegistry, ing Ingredients, g Grid) (data.RecipeResult, Grid, bool) {
	recipe, ok := Match(reg, ing, g)
	if !ok {
		return data.RecipeResult{}, Grid{}, false
	}

	remaining := g.clone()
	for at, cell := range remaining.Cells {
		if cell.Empty() {
			continue
		}
		cell.Count--
		if cell.Count <= 0 {
			cell = Cell{}
		}
		remaining.Cells[at] = cell
	}

	return recipe.Result, remaining, true
}

// ShiftCraft repeats Craft until an ingredient runs out, returning the total
// result and the remaining grid.
//
// It is a separate function rather than a loop at the call site because the
// stopping condition is the subtle part: it stops when the grid stops
// matching, not after a fixed count, and a caller that wrote the loop itself
// would very likely write it to run once. That is not hypothetical — it is
// the defect M3's session findings recorded and fixed.
func ShiftCraft(reg data.RecipeRegistry, ing Ingredients, g Grid) (data.RecipeResult, Grid, bool) {
	total, remaining, ok := Craft(reg, ing, g)
	if !ok {
		return data.RecipeResult{}, Grid{}, false
	}

	for {
		result, next, again := Craft(reg, ing, remaining)
		if !again || result.ID != total.ID || result.Metadata != total.Metadata {
			// A drained cell can leave a grid that matches a different
			// recipe. That craft is a different click's business: vanilla's
			// shift-craft loop stops when the result changes, because the
			// stack under the cursor can hold one item.
			return total, remaining, true
		}
		total.Count += result.Count
		remaining = next
	}
}
