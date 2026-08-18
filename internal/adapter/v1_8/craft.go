package v1_8

import (
	"sync"

	"github.com/go-theft-craft/minecraft-protocol/data"
	gen "github.com/go-theft-craft/minecraft-protocol/generated/java/v1_8"

	"github.com/go-theft-craft/headless-minecraft/crafting"
	"github.com/go-theft-craft/headless-minecraft/version"
)

// This protocol's server never sends the result slot of a crafting grid:
// SlotCrafting is excluded from every slot send, and an accepted click is
// answered with nothing. A vanilla client computes the result locally, so
// this adapter does too, from the same generated recipe registry the gate
// checks.

// The two crafting menus this version has, with the slot geometry its own
// generated window data states: result at 0, grid row-major from 1.
const (
	craftingTableMenu       = "minecraft:crafting_table"
	playerMenu        int32 = 0
)

// CraftingLayout implements version.Crafter.
func (adapter) CraftingLayout(menuType string, window int32) (version.CraftingLayout, bool) {
	switch {
	case menuType == craftingTableMenu:
		return version.CraftingLayout{
			Result: 0, Grid: []int32{1, 2, 3, 4, 5, 6, 7, 8, 9}, Width: 3, Height: 3,
		}, true
	case window == playerMenu:
		return version.CraftingLayout{
			Result: 0, Grid: []int32{1, 2, 3, 4}, Width: 2, Height: 2,
		}, true
	default:
		return version.CraftingLayout{}, false
	}
}

// recipes loads the generated registry once: the registry is immutable and a
// prediction per click must not re-parse a dataset.
var recipes = sync.OnceValues(func() (data.RecipeRegistry, error) {
	set, err := gen.Data()
	if err != nil {
		return nil, err
	}

	return set.Recipes(), nil
})

// PredictCraft implements version.Crafter.
func (adapter) PredictCraft(grid []any) (any, []any, bool) {
	registry, err := recipes()
	if err != nil {
		return nil, nil, false
	}

	width, height := 3, 3
	if len(grid) == 4 {
		width, height = 2, 2
	}
	cells := crafting.Grid{Width: width, Height: height, Cells: make([]crafting.Cell, 0, len(grid))}
	for _, stack := range grid {
		cells.Cells = append(cells.Cells, cellOf(stack))
	}

	result, remaining, ok := crafting.Craft(registry, Ingredients{}, cells)
	if !ok {
		return nil, nil, false
	}

	after := make([]any, 0, len(remaining.Cells))
	for at, cell := range remaining.Cells {
		if cell.Empty() {
			after = append(after, nil)

			continue
		}
		// The cell keeps its identity — the NBT included — and loses one
		// item, so the remaining stack is the original with its count moved.
		kept, isSlot := grid[at].(gen.Slot)
		if !isSlot {
			after = append(after, nil)

			continue
		}
		kept.AnonymousSwitch1.Default.ItemCount = int8(cell.Count)
		after = append(after, kept)
	}

	return gen.Slot{
		BlockID: int16(result.ID),
		AnonymousSwitch1: gen.SlotAnonymousSwitch1Switch{
			Default: gen.SlotAnonymousSwitch1SwitchDefault{
				ItemCount:  int8(result.Count),
				ItemDamage: int16(result.Metadata),
			},
		},
	}, after, true
}

// cellOf reads one decoded stack into the matcher's vocabulary.
func cellOf(stack any) crafting.Cell {
	value, ok := stack.(gen.Slot)
	if !ok || value.BlockID < 0 {
		return crafting.Cell{}
	}

	return crafting.Cell{
		ID:       data.ItemID(value.BlockID),
		Metadata: data.Metadata(value.AnonymousSwitch1.Default.ItemDamage),
		Count:    int(value.AnonymousSwitch1.Default.ItemCount),
	}
}
