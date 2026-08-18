package v26_1

import (
	"github.com/go-theft-craft/minecraft-protocol/data"

	"github.com/go-theft-craft/headless-minecraft/crafting"
)

// Ingredients is this version's ingredient equality: an item ID and nothing
// else.
//
// The flattening moved variants into distinct item IDs, and the generated
// recipe file states it by carrying Metadata: -1 on all 9,911 of its
// ingredients and results. Comparing metadata here would be comparing a
// constant — harmless, and exactly the kind of accidental correctness the
// version-owned seam exists to make deliberate instead.
type Ingredients struct{}

// Satisfies implements crafting.Ingredients.
func (Ingredients) Satisfies(cell crafting.Cell, want data.Ingredient) bool {
	return !cell.Empty() && cell.ID == want.ID
}
