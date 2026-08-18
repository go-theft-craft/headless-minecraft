package version

// CraftingLayout is the slot geometry of one crafting-capable menu: which
// window slot is the result, and which slots are the grid, row-major.
type CraftingLayout struct {
	Result int32
	Grid   []int32
	Width  int
	Height int
}

// Crafter is an adapter that can answer crafting questions for its version.
//
// It exists for protocol 47, whose server never sends the result slot — a
// vanilla client computes it locally, and a client that cannot claims wrong
// and desynchronises on every craft. Protocol 775 is server-authoritative
// about the result and answers every click with a resend, so its adapter
// reports no layouts and the click path stays generic there.
type Crafter interface {
	// CraftingLayout reports the crafting geometry of one menu, or false for
	// a menu this version does not craft in — or a version that never needs
	// local crafting at all.
	CraftingLayout(menuType string, window int32) (CraftingLayout, bool)
	// PredictCraft answers what the grid crafts: the result stack, and what
	// each grid cell holds afterwards, all in this protocol's own decoded
	// stack shape with nil for empty. It reports false for a grid that
	// crafts nothing.
	PredictCraft(grid []any) (result any, remaining []any, ok bool)
}
