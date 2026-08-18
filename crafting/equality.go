package crafting

import "github.com/go-theft-craft/minecraft-protocol/data"

// Ingredients decides whether a grid cell satisfies a recipe ingredient.
//
// It is version-owned because the two editions disagree about what an
// ingredient is. On 1.8.9 an ingredient is an item and a metadata variant,
// and -1 means any variant. On 26.1.2 the flattening moved variants into
// distinct item IDs and metadata is always -1, so comparing it is comparing a
// constant. A matcher that ignores metadata matches red wool against a
// blue-wool recipe on 1.8.9; one that compares it on 26.1.2 is merely doing
// nothing. The implementations live beside the version adapters, which is
// where version facts live.
type Ingredients interface {
	Satisfies(cell Cell, want data.Ingredient) bool
}
