// Package crafting_test checks the matcher against the real generated recipe
// registries, because a matcher tested against hand-built recipes is tested
// against the author's idea of a recipe.
package crafting_test

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/go-theft-craft/minecraft-protocol/data"
	gen "github.com/go-theft-craft/minecraft-protocol/generated/java/v1_8"
	gen26 "github.com/go-theft-craft/minecraft-protocol/generated/java/v26_1"

	"github.com/go-theft-craft/headless-minecraft/crafting"
	v1_8 "github.com/go-theft-craft/headless-minecraft/internal/adapter/v1_8"
	v26_1 "github.com/go-theft-craft/headless-minecraft/internal/adapter/v26_1"
)

// The 1.8.9 item vocabulary these tests speak, by wire ID.
const (
	stoneID  data.ItemID = 1
	planksID data.ItemID = 5
	woolID   data.ItemID = 35
	torchID  data.ItemID = 50
	chestID  data.ItemID = 54
	coalID   data.ItemID = 263
	stickID  data.ItemID = 280
	quartzID data.ItemID = 406
)

// registry1_8 loads the real 1.8.9 recipe registry.
func registry1_8(t *testing.T) data.RecipeRegistry {
	t.Helper()

	set, err := gen.Data()
	if err != nil {
		t.Fatalf("load the 1.8.9 data set: %v", err)
	}

	return set.Recipes()
}

// registry26 loads the real 26.1.2 recipe registry.
func registry26(t *testing.T) data.RecipeRegistry {
	t.Helper()

	set, err := gen26.Data()
	if err != nil {
		t.Fatalf("load the 26.1.2 data set: %v", err)
	}

	return set.Recipes()
}

// item26 resolves a modern item name to its ID, because the flattening made
// the numbers version facts nobody should type from memory.
func item26(t *testing.T, name string) data.ItemID {
	t.Helper()

	set, err := gen26.Data()
	if err != nil {
		t.Fatalf("load the 26.1.2 data set: %v", err)
	}
	item, ok := set.Items().ByName(name)
	if !ok {
		t.Fatalf("no such 26.1 item %q", name)
	}

	return item.ID
}

// grid3x3 builds a grid from a picture: '.' is empty, any other rune indexes
// the legend.
func grid3x3(t *testing.T, picture string, legend map[rune]crafting.Cell) crafting.Grid {
	t.Helper()

	g := crafting.Grid{Width: 3, Height: 3}
	for _, line := range strings.Fields(picture) {
		for _, r := range line {
			if r == '.' {
				g.Cells = append(g.Cells, crafting.Cell{})

				continue
			}
			cell, ok := legend[r]
			if !ok {
				t.Fatalf("the picture uses %q and the legend does not", r)
			}
			g.Cells = append(g.Cells, cell)
		}
	}
	if len(g.Cells) != 9 {
		t.Fatalf("the picture drew %d cells, want 9", len(g.Cells))
	}

	return g
}

func TestTrimFindsTheSmallestBoundingGrid(t *testing.T) {
	t.Parallel()

	// A 2x2 pattern in the bottom-right of a 3x3 grid is the same recipe as
	// one in the top-left. Trimming is what makes that true without sliding a
	// pattern across nine offsets.
	g := grid3x3(t, `
		...
		.xx
		.xx
	`, map[rune]crafting.Cell{'x': {ID: planksID, Count: 1}})

	trimmed, offX, offY := g.Trim()
	if trimmed.Width != 2 || trimmed.Height != 2 {
		t.Fatalf("trimmed to %dx%d, want 2x2", trimmed.Width, trimmed.Height)
	}
	if offX != 1 || offY != 1 {
		t.Fatalf("offset (%d,%d), want (1,1)", offX, offY)
	}
}

func TestTrimmingAnEmptyGridIsEmptyAndNotAPanic(t *testing.T) {
	t.Parallel()

	trimmed, _, _ := grid3x3(t, `... ... ...`, nil).Trim()
	if trimmed.Width != 0 || trimmed.Height != 0 {
		t.Fatalf("an empty grid trimmed to %dx%d", trimmed.Width, trimmed.Height)
	}
}

func TestMetadataMattersOn1_8_9AndNotOn26_1_2(t *testing.T) {
	t.Parallel()

	// The one version-owned rule in this stage, stated as one test so the two
	// halves cannot drift apart.
	redWool := crafting.Cell{ID: woolID, Metadata: 14, Count: 1}
	blueWoolWanted := data.Ingredient{ID: woolID, Metadata: 11}

	if (v1_8.Ingredients{}).Satisfies(redWool, blueWoolWanted) {
		t.Fatal("1.8.9 matched red wool against a blue-wool ingredient; metadata " +
			"is the variant there and ignoring it crafts the wrong thing")
	}

	// On 26.1.2 the flattening gave each colour its own item ID, so a recipe
	// naming blue wool names a different ID and metadata is always -1.
	blueWool26 := item26(t, "blue_wool")
	if !(v26_1.Ingredients{}).Satisfies(
		crafting.Cell{ID: blueWool26, Count: 1}, data.Ingredient{ID: blueWool26, Metadata: -1},
	) {
		t.Fatal("26.1.2 refused an exact item match")
	}
}

func TestMetadataMinusOneMatchesAnyVariantOn1_8_9(t *testing.T) {
	t.Parallel()

	// -1 is the wildcard. A matcher that compares it literally refuses every
	// recipe that uses one, which is a large fraction of them.
	for _, meta := range []data.Metadata{0, 5, 14} {
		if !(v1_8.Ingredients{}).Satisfies(
			crafting.Cell{ID: woolID, Metadata: meta, Count: 1},
			data.Ingredient{ID: woolID, Metadata: -1},
		) {
			t.Fatalf("metadata %d did not satisfy a -1 wildcard", meta)
		}
	}
}

func TestAnEmptyCellSatisfiesNoIngredient(t *testing.T) {
	t.Parallel()

	for _, impl := range []crafting.Ingredients{v1_8.Ingredients{}, v26_1.Ingredients{}} {
		if impl.Satisfies(crafting.Cell{}, data.Ingredient{ID: woolID, Metadata: -1}) {
			t.Fatalf("%T matched an empty cell against a wildcard ingredient", impl)
		}
	}
}

// torchLegend is coal over stick, the 1x2 torch pattern.
func torchLegend() map[rune]crafting.Cell {
	return map[rune]crafting.Cell{
		'c': {ID: coalID, Count: 1},
		's': {ID: stickID, Count: 1},
	}
}

func TestAShapedRecipeMatchesAtEveryOffsetThatFits(t *testing.T) {
	t.Parallel()

	// This is the case a matcher that only tests the top-left offset gets
	// wrong, and it gets it wrong silently: the tutorial cases all place
	// ingredients neatly in the corner.
	for name, picture := range map[string]string{
		"top-left":     `c.. s.. ...`,
		"top-right":    `..c ..s ...`,
		"bottom-left":  `... c.. s..`,
		"bottom-right": `... ..c ..s`,
		"centre":       `.c. .s. ...`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			g := grid3x3(t, picture, torchLegend())
			got, ok := crafting.Match(registry1_8(t), v1_8.Ingredients{}, g)
			if !ok {
				t.Fatalf("no match for a torch pattern in the %s of a 3x3 grid", name)
			}
			if got.Result.ID != torchID {
				t.Fatalf("matched a recipe producing %d, want a torch", got.Result.ID)
			}
		})
	}
}

func TestAShapedRecipeDoesNotMatchWithAStrayIngredient(t *testing.T) {
	t.Parallel()

	// Every cell outside the pattern must be empty. A matcher that only
	// checks the pattern's own cells crafts from grids vanilla refuses.
	legend := torchLegend()
	legend['x'] = crafting.Cell{ID: stoneID, Count: 1}
	g := grid3x3(t, `c.. s.. ..x`, legend)

	if got, ok := crafting.Match(registry1_8(t), v1_8.Ingredients{}, g); ok {
		t.Fatalf("matched a recipe producing %d from a torch pattern with a "+
			"stray stone in the corner", got.Result.ID)
	}
}

// graniteArrangements is 1.8.9's shapeless granite: one diorite, one quartz,
// in any two cells.
func graniteArrangements(t *testing.T) []crafting.Grid {
	t.Helper()

	legend := map[rune]crafting.Cell{
		'd': {ID: stoneID, Metadata: 3, Count: 1},
		'q': {ID: quartzID, Count: 1},
	}

	return []crafting.Grid{
		grid3x3(t, `dq. ... ...`, legend),
		grid3x3(t, `q.. ..d ...`, legend),
		grid3x3(t, `... .d. ..q`, legend),
	}
}

func TestAShapelessRecipeIgnoresArrangement(t *testing.T) {
	t.Parallel()

	// data.Recipe carries Ingredients for shapeless and InShape for shaped;
	// the two must not be matched by the same path. Granite is also the
	// metadata-sensitive case: the diorite is stone with variant 3, and a
	// matcher that ignored the variant would craft granite from stone.
	for at, g := range graniteArrangements(t) {
		got, ok := crafting.Match(registry1_8(t), v1_8.Ingredients{}, g)
		if !ok {
			t.Fatalf("the granite ingredients did not match in arrangement %d", at)
		}
		if got.Result.ID != stoneID || got.Result.Metadata != 1 {
			t.Fatalf("arrangement %d matched a recipe producing %d:%d, want granite",
				at, got.Result.ID, got.Result.Metadata)
		}
	}
}

func TestPlainStoneDoesNotCraftGranite(t *testing.T) {
	t.Parallel()

	// The other half of the metadata rule, through the whole matcher: stone
	// with variant 0 must not satisfy the diorite the granite recipe wants.
	g := grid3x3(t, `sq. ... ...`, map[rune]crafting.Cell{
		's': {ID: stoneID, Metadata: 0, Count: 1},
		'q': {ID: quartzID, Count: 1},
	})

	if got, ok := crafting.Match(registry1_8(t), v1_8.Ingredients{}, g); ok {
		t.Fatalf("plain stone and quartz matched a recipe producing %d:%d",
			got.Result.ID, got.Result.Metadata)
	}
}

func TestAShapelessRecipeStillRequiresTheRightCount(t *testing.T) {
	t.Parallel()

	// Shapeless means arrangement-free, not quantity-free. A matcher that
	// treats the grid as a set crafts from one ingredient where vanilla wants
	// two.
	g := grid3x3(t, `d.. ... ...`, map[rune]crafting.Cell{
		'd': {ID: stoneID, Metadata: 3, Count: 1},
	})

	if got, ok := crafting.Match(registry1_8(t), v1_8.Ingredients{}, g); ok &&
		got.Result.ID == stoneID && got.Result.Metadata == 1 {
		t.Fatal("granite matched with the quartz missing")
	}
}

func TestARecipeTooLargeForTheGridDoesNotMatch(t *testing.T) {
	t.Parallel()

	// A chest corner in the 2x2 player grid: four planks there are a crafting
	// table, never a partial chest. This is why Grid carries its own
	// dimensions rather than inferring them from the cell count.
	plank := crafting.Cell{ID: planksID, Metadata: 0, Count: 1}
	g := crafting.Grid{Width: 2, Height: 2, Cells: []crafting.Cell{plank, plank, plank, plank}}

	got, ok := crafting.Match(registry1_8(t), v1_8.Ingredients{}, g)
	if !ok {
		t.Fatal("four planks in the 2x2 grid crafted nothing")
	}
	if got.Result.ID == chestID {
		t.Fatal("a 3x3 chest recipe matched in the 2x2 player grid")
	}
}

func TestTheModernRegistryMatchesTooAndByItemIDAlone(t *testing.T) {
	t.Parallel()

	// The same torch, spoken in the flattened vocabulary: distinct item IDs,
	// no metadata anywhere.
	coal, stick, torch := item26(t, "coal"), item26(t, "stick"), item26(t, "torch")
	g := grid3x3(t, `... .c. .s.`, map[rune]crafting.Cell{
		'c': {ID: coal, Count: 1},
		's': {ID: stick, Count: 1},
	})

	got, ok := crafting.Match(registry26(t), v26_1.Ingredients{}, g)
	if !ok {
		t.Fatal("no match for the torch pattern in the 26.1 registry")
	}
	if got.Result.ID != torch {
		t.Fatalf("matched a recipe producing %d, want torch %d", got.Result.ID, torch)
	}
}

func TestCraftConsumesOneOfEachIngredient(t *testing.T) {
	t.Parallel()

	// One, not the whole stack. A craft that consumes the stack empties the
	// grid in a single click and looks like a working shift-craft.
	g := grid3x3(t, `c.. s.. ...`, map[rune]crafting.Cell{
		'c': {ID: coalID, Count: 64},
		's': {ID: stickID, Count: 64},
	})

	_, remaining, ok := crafting.Craft(registry1_8(t), v1_8.Ingredients{}, g)
	if !ok {
		t.Fatal("the torch grid did not craft")
	}
	for at, cell := range remaining.Cells {
		if !cell.Empty() && cell.Count != 63 {
			t.Fatalf("cell %d holds %d after one craft, want 63", at, cell.Count)
		}
	}
}

func TestCraftDoesNotMutateTheInputGrid(t *testing.T) {
	t.Parallel()

	// The caller keeps the original until the server confirms, the same way
	// M9.7's click path keeps its pre-click snapshot.
	g := grid3x3(t, `c.. s.. ...`, map[rune]crafting.Cell{
		'c': {ID: coalID, Count: 64},
		's': {ID: stickID, Count: 64},
	})
	before := slices.Clone(g.Cells)

	crafting.Craft(registry1_8(t), v1_8.Ingredients{}, g)
	if !slices.Equal(g.Cells, before) {
		t.Fatal("Craft mutated its input grid")
	}
}

func TestShiftCraftDrainsTheGrid(t *testing.T) {
	t.Parallel()

	// The M3 defect, in test form: a handler that crafted once instead of
	// draining. Sixty-four of each ingredient produces sixty-four crafts, and
	// a torch recipe produces four per craft.
	g := grid3x3(t, `c.. s.. ...`, map[rune]crafting.Cell{
		'c': {ID: coalID, Count: 64},
		's': {ID: stickID, Count: 64},
	})

	result, remaining, ok := crafting.ShiftCraft(registry1_8(t), v1_8.Ingredients{}, g)
	if !ok {
		t.Fatal("the torch grid did not shift-craft")
	}
	if result.Count != 64*4 {
		t.Fatalf("shift-craft produced %d torches, want %d: it crafted once "+
			"instead of draining the grid", result.Count, 64*4)
	}
	for at, cell := range remaining.Cells {
		if !cell.Empty() {
			t.Fatalf("cell %d still holds %d after a shift-craft", at, cell.Count)
		}
	}
}

func TestShiftCraftStopsAtTheShortestIngredient(t *testing.T) {
	t.Parallel()

	// The stopping condition is "the grid stopped matching", not "a count was
	// reached". An uneven grid is what tells the two apart.
	g := grid3x3(t, `c.. s.. ...`, map[rune]crafting.Cell{
		'c': {ID: coalID, Count: 3},
		's': {ID: stickID, Count: 64},
	})

	result, remaining, _ := crafting.ShiftCraft(registry1_8(t), v1_8.Ingredients{}, g)
	if result.Count != 3*4 {
		t.Fatalf("produced %d torches from a grid with three coal, want %d",
			result.Count, 3*4)
	}
	var left int
	for _, cell := range remaining.Cells {
		if !cell.Empty() {
			left += cell.Count
		}
	}
	if left != 61 {
		t.Fatalf("the grid holds %d items after the drain, want the 61 sticks left", left)
	}
}

func TestShiftCraftingAnEmptyGridProducesNothingRatherThanLooping(t *testing.T) {
	t.Parallel()

	// A drain loop whose stopping condition is wrong on an empty grid does
	// not return a wrong answer, it does not return.
	done := make(chan struct{})
	go func() {
		defer close(done)
		crafting.ShiftCraft(registry1_8(t), v1_8.Ingredients{},
			crafting.Grid{Width: 3, Height: 3, Cells: make([]crafting.Cell, 9)})
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("ShiftCraft on an empty grid did not terminate")
	}
}

// mirrorCase is one row of the committed mirror corpus.
type mirrorCase struct {
	Name          string   `json:"name"`
	Grid          []string `json:"grid"`
	VanillaCrafts bool     `json:"vanillaCrafts"`
	Why           string   `json:"why"`
}

// loadMirrorCorpus reads the corpus the live lane confirmed against both jars.
func loadMirrorCorpus(t *testing.T) []mirrorCase {
	t.Helper()

	content, err := os.ReadFile("testdata/mirror.json")
	if err != nil {
		t.Fatalf("read the mirror corpus: %v", err)
	}
	var corpus struct {
		Source string       `json:"source"`
		Cases  []mirrorCase `json:"cases"`
	}
	if err := json.Unmarshal(content, &corpus); err != nil {
		t.Fatalf("decode the mirror corpus: %v", err)
	}
	if len(corpus.Cases) == 0 {
		t.Fatal("the mirror corpus holds no cases")
	}

	return corpus.Cases
}

func TestMirroringMatchesVanillaRatherThanAnAssumption(t *testing.T) {
	t.Parallel()

	// This package does not state a mirroring rule of its own, because
	// getting it wrong in either direction is silent: a matcher that mirrors
	// too little refuses grids vanilla crafts, and one that mirrors too much
	// crafts tools from upside-down grids. The corpus says what vanilla does
	// — confirmed live against both jars — and this test reads the corpus.
	axeLegend := map[rune]crafting.Cell{
		'p': {ID: planksID, Metadata: 0, Count: 1},
		's': {ID: stickID, Count: 1},
	}

	for _, c := range loadMirrorCorpus(t) {
		t.Run(c.Name, func(t *testing.T) {
			t.Parallel()

			g := grid3x3(t, strings.Join(c.Grid, " "), axeLegend)
			_, ok := crafting.Match(registry1_8(t), v1_8.Ingredients{}, g)
			if ok != c.VanillaCrafts {
				t.Fatalf("the matcher says %v and vanilla says %v: %s",
					ok, c.VanillaCrafts, c.Why)
			}
		})
	}
}
