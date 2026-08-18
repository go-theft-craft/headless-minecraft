package v1_8

import (
	"github.com/go-theft-craft/minecraft-protocol/data"

	"github.com/go-theft-craft/headless-minecraft/crafting"
)

// Ingredients is this version's ingredient equality: an item and a metadata
// variant, with -1 as the any-variant wildcard.
//
// The metadata comparison is the point. On 1.8.9 the variant is the metadata
// — red wool is wool 14, blue wool is wool 11 — and a matcher that ignores it
// crafts the wrong thing; the generated recipe file uses real variants
// throughout, 1,123 zeroes and hundreds each of 1 through 15.
type Ingredients struct{}

// Satisfies implements crafting.Ingredients.
func (Ingredients) Satisfies(cell crafting.Cell, want data.Ingredient) bool {
	if cell.Empty() || cell.ID != want.ID {
		return false
	}

	return want.Metadata == -1 || cell.Metadata == want.Metadata
}
