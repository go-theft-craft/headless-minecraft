package world

import (
	"errors"
	"maps"
	"slices"

	"github.com/go-theft-craft/headless-minecraft/event"
)

// ErrUnknownSequence reports a confirmation or rejection for a click nobody
// made. On protocol 47 it means the transaction sequence has drifted, and
// every later confirmation would answer the wrong click; failing loudly beats
// accumulating a silent offset.
var ErrUnknownSequence = errors.New("world: no such pending click")

// ErrOutOfOrder reports a confirmation for a later click while an earlier one
// is unanswered. Unlike an unknown sequence — which can be a click that
// predates a reconnect — this one cannot be shrugged off: the skipped click's
// prediction would stand unexamined forever, so the pendings as a whole can
// no longer be trusted.
var ErrOutOfOrder = errors.New("world: click confirmed out of order")

// The container domain records the menu the server actually opened, and never
// predicts one from the block a caller clicked.
//
// A slot's item is kept as the protocol decoded it, in an `any`, for the same
// reason entity equipment is: an item stack's wire shape differs completely
// between the two protocols and modelling it here would mean a third item
// model to keep in step with both. Semantic layouts — which slot of a furnace
// is the output — are M9's container drivers.

const (
	// maxContainers bounds the menus one connection may accumulate. A server
	// reuses container IDs and a caller has one menu open at a time; this is
	// generous, and a server that trips it is misbehaving.
	maxContainers = 64
	// maxSlots bounds one menu's slots. Vanilla's largest is under a hundred;
	// a modded menu is larger, and this is not a number a peer should choose.
	maxSlots = 1024
	// maxProperties bounds one menu's properties.
	maxProperties = 64
	// maxRecipes bounds the recipe book.
	maxRecipes = 8192
	// maxPending bounds the clicks awaiting confirmation. These are the
	// client's own rather than the peer's, so the bound is a backstop against
	// a confirmation path that stopped resolving them, not against a hostile
	// server.
	maxPending = 256
)

// playerContainerID is the menu every session always has: the player's own
// inventory. No packet opens it, so it is created on first use rather than
// waiting for an OpenWindow that never comes.
const playerContainerID int32 = 0

// container is one open menu's state.
type container struct {
	id          int32
	menuType    string
	title       string
	slotCount   int32
	entityID    int32
	entityKnown bool

	stateID    int32
	stateKnown bool

	slots        map[int32]any
	properties   map[int32]int32
	droppedSlots int
}

// ContainerView is one menu in a snapshot. Its maps are owned copies.
type ContainerView struct {
	ContainerID int32
	MenuType    string
	Title       string
	SlotCount   int32
	EntityID    int32
	EntityKnown bool

	// StateID is 775's inventory synchronization counter. StateKnown is false
	// on protocol 47, and it is exposed as optional rather than defaulting to
	// zero because zero is a valid state ID.
	StateID    int32
	StateKnown bool

	// Slots is addressed by slot index. An item is the protocol's own decoded
	// stack, kept as sent.
	Slots      map[int32]any
	Properties map[int32]int32

	DroppedSlots int
}

// Containers is every menu the server has open, plus the cursor.
type Containers struct {
	open   map[int32]*container
	cursor any
	// cursorHeld is separate from cursor because a nil item is what an empty
	// cursor decodes to, and "empty" and "never mentioned" are different.
	cursorHeld bool
	cursorSent bool

	// pending are the clicks the server has not answered, oldest first. Each
	// carries what the affected slots held before it, because that is the
	// only way to roll back on protocol 47.
	pending        []Pending
	droppedPending int

	dropped int

	recipes        map[int32]bool
	recipesKnown   bool
	declaredCount  int
	droppedRecipes int
}

// ContainersView is the container half of a snapshot.
type ContainersView struct {
	Open map[int32]ContainerView
	// PendingClicks is how many clicks await the server's answer. A caller
	// that reads slots while this is non-zero is reading predictions.
	PendingClicks int
	// DroppedPending counts clicks refused by the pending bound.
	DroppedPending int
	// Cursor is the stack held on the cursor. Held reports whether it holds
	// anything, and Known reports whether the server has ever mentioned it.
	Cursor       any
	CursorHeld   bool
	CursorKnown  bool
	DroppedMenus int

	// Recipes is the recipe book's ID set. RecipesKnown is false for a whole
	// protocol 47 session, which has no recipe packet at all — an empty set
	// there means "never sent", not "no recipes".
	Recipes        map[int32]bool
	RecipesKnown   bool
	DeclaredCount  int
	DroppedRecipes int
}

// Get returns one menu, or false when it is not open.
func (v ContainersView) Get(id int32) (ContainerView, bool) {
	c, ok := v.Open[id]

	return c, ok
}

func newContainers() *Containers {
	return &Containers{open: make(map[int32]*container), recipes: make(map[int32]bool)}
}

func (s *Containers) view() ContainersView {
	open := make(map[int32]ContainerView, len(s.open))
	for id, c := range s.open {
		open[id] = ContainerView{
			ContainerID: c.id, MenuType: c.menuType, Title: c.title,
			SlotCount: c.slotCount, EntityID: c.entityID, EntityKnown: c.entityKnown,
			StateID: c.stateID, StateKnown: c.stateKnown,
			Slots:        maps.Clone(c.slots),
			Properties:   maps.Clone(c.properties),
			DroppedSlots: c.droppedSlots,
		}
	}

	return ContainersView{
		Open:           open,
		Cursor:         s.cursor,
		CursorHeld:     s.cursorHeld,
		CursorKnown:    s.cursorSent,
		DroppedMenus:   s.dropped,
		PendingClicks:  len(s.pending),
		DroppedPending: s.droppedPending,

		Recipes:        maps.Clone(s.recipes),
		RecipesKnown:   s.recipesKnown,
		DeclaredCount:  s.declaredCount,
		DroppedRecipes: s.droppedRecipes,
	}
}

// track returns a menu's state, creating it when the client has no open packet
// for it.
//
// Container 0 is the player's own inventory and no packet ever opens it, so
// creating on demand is not a workaround here — it is how that menu exists at
// all. Slot packets also arrive for menus a reconnecting client never saw
// opened, and dropping them would lose what the server said.
func (s *Containers) track(id int32) *container {
	c, ok := s.open[id]
	if !ok {
		if len(s.open) >= maxContainers {
			s.dropped++

			return nil
		}
		c = &container{
			id:         id,
			slots:      make(map[int32]any),
			properties: make(map[int32]int32),
		}
		s.open[id] = c
	}

	return c
}

// Opened records the server opening a menu. It replaces whatever was open
// under that ID: a server reuses container IDs, and carrying the old menu's
// slots into a new menu would report a chest's contents as a furnace's.
func (s *Containers) Opened(c *event.Collector, opened event.ContainerOpened) {
	delete(s.open, opened.ContainerID)

	menu := s.track(opened.ContainerID)
	if menu == nil {
		return
	}
	menu.menuType, menu.title = opened.MenuType, opened.Title
	menu.slotCount = opened.SlotCount
	menu.entityID, menu.entityKnown = opened.EntityID, opened.EntityKnown

	event.Emit(c, opened)
}

// Closed releases a menu. Closing one that was never open is not an error: a
// server closes a menu the caller closed first.
//
// The player's own inventory is released like any other menu, because the
// server does close container 0 and the slots it held are no longer described.
func (s *Containers) Closed(c *event.Collector, id int32) {
	if _, ok := s.open[id]; !ok {
		return
	}
	delete(s.open, id)

	// Its pending clicks go with it. Their windows no longer exist to roll
	// back, and a confirmation for a closed window answers nothing.
	kept := s.pending[:0]
	for _, p := range s.pending {
		if p.Window != id {
			kept = append(kept, p)
		}
	}
	s.pending = kept

	event.Emit(c, event.ContainerClosed{ContainerID: id})
}

// SlotsChanged merges slots by index, bounded.
//
// A packet carrying one slot must not clear the others, which is what makes
// SetSlot and WindowItems the same mutator: a full list is a merge that
// happens to name every index.
func (s *Containers) SlotsChanged(
	c *event.Collector,
	id int32,
	items map[int32]any,
	stateID int32,
	stateKnown bool,
) {
	menu := s.track(id)
	if menu == nil {
		return
	}
	if stateKnown {
		menu.stateID, menu.stateKnown = stateID, true
	}

	slots := make([]int32, 0, len(items))
	for slot, item := range items {
		if _, existing := menu.slots[slot]; !existing && len(menu.slots) >= maxSlots {
			menu.droppedSlots++

			continue
		}
		menu.slots[slot] = item
		slots = append(slots, slot)
	}
	if len(slots) == 0 && !stateKnown {
		return
	}
	slices.Sort(slots)

	event.Emit(c, event.ContainerSlotsChanged{
		ContainerID: id, Slots: slots,
		StateID: menu.stateID, StateKnown: menu.stateKnown,
		Dropped: menu.droppedSlots,
	})
}

// PropertyChanged records one of a menu's properties — a furnace's burn time,
// a brewing stand's progress.
func (s *Containers) PropertyChanged(c *event.Collector, id int32, property int32, value int32) {
	menu := s.track(id)
	if menu == nil {
		return
	}
	if _, existing := menu.properties[property]; !existing && len(menu.properties) >= maxProperties {
		return
	}
	menu.properties[property] = value

	event.Emit(c, event.ContainerSlotsChanged{
		ContainerID: id, Properties: []int32{property},
		StateID: menu.stateID, StateKnown: menu.stateKnown,
	})
}

// PlayerSlotChanged records a slot of the player's own inventory, which
// protocol 775 addresses directly rather than through a menu.
func (s *Containers) PlayerSlotChanged(c *event.Collector, slot int32, item any, held bool) {
	value := item
	if !held {
		value = nil
	}
	s.SlotsChanged(c, playerContainerID, map[int32]any{slot: value}, 0, false)
}

// CursorChanged records the stack held on the cursor, which belongs to no
// menu.
func (s *Containers) CursorChanged(c *event.Collector, item any, held bool) {
	s.cursor, s.cursorHeld, s.cursorSent = item, held, true
	if !held {
		s.cursor = nil
	}

	event.Emit(c, event.ContainerCursorChanged{Empty: !held})
}

// RecipesChanged records the recipe book gaining or losing recipes.
func (s *Containers) RecipesChanged(c *event.Collector, added, removed []int32, replaced bool) {
	s.recipesKnown = true
	if replaced {
		clear(s.recipes)
	}
	for _, id := range removed {
		delete(s.recipes, id)
	}

	kept := make([]int32, 0, len(added))
	for _, id := range added {
		if !s.recipes[id] && len(s.recipes) >= maxRecipes {
			s.droppedRecipes++

			continue
		}
		s.recipes[id] = true
		kept = append(kept, id)
	}

	event.Emit(c, event.ContainerRecipesChanged{
		Added: kept, Removed: slices.Clone(removed), Replaced: replaced,
	})
}

// RecipesDeclared records a full recipe declaration. The recipes themselves
// are wire structures M9 models; what is kept here is that they arrived and
// how many.
func (s *Containers) RecipesDeclared(c *event.Collector, count int) {
	s.recipesKnown, s.declaredCount = true, count

	event.Emit(c, event.ContainerRecipesChanged{Declared: count})
}

// TradesChanged records a villager's trade list. The trades are wire
// structures, so the snapshot records the menu's trading scalars and the count.
func (s *Containers) TradesChanged(c *event.Collector, trades event.ContainerTradesChanged) {
	s.track(trades.ContainerID)

	event.Emit(c, trades)
}

// CraftResponse records the server answering a recipe-placement request.
func (s *Containers) CraftResponse(c *event.Collector, id int32) {
	event.Emit(c, event.ContainerCraftResponse{ContainerID: id})
}
