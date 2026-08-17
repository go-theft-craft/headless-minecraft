package event

// The container domain is the menu the server actually opened.
//
// It never predicts one. A caller that clicks a chest gets a container when
// the server says so, described by what the server sent — an ID, a menu type,
// a title, and a list of raw slots. Semantic layouts, container drivers, and
// "the output slot of a furnace" are M9; inventing them here would mean
// guessing a menu from the block that was clicked, which is the one thing this
// package refuses to do.

// ContainerOpened reports the server opening a menu.
type ContainerOpened struct {
	Stamp

	ContainerID int32
	// MenuType is the server's own menu identifier, kept as sent. Protocol 47
	// names it and 775 numbers it into the session's menu registry, and
	// neither is mapped onto a vanilla name a modded server may not mean.
	MenuType string
	// Title is the title protocol 47 sent, as sent. It is empty on 775, which
	// sends a chat component instead — rendering one is a presentation
	// decision this library does not make for a consumer, and a caller that
	// wants the text renders it from the raw packet.
	Title string
	// SlotCount is protocol 47's declared size. 775 does not send one, and the
	// slot list that follows is the only statement of size there.
	SlotCount int32
	// EntityID names the entity a menu belongs to, for the menus that have one
	// — a horse, a villager. EntityKnown reports whether this menu named one.
	EntityID    int32
	EntityKnown bool
}

func (ContainerOpened) Name() Name     { return NameContainerOpened }
func (ContainerOpened) Domain() Domain { return DomainContainers }

// ContainerClosed reports a menu closing. Everything the client knew about it
// is released with it.
type ContainerClosed struct {
	Stamp

	ContainerID int32
}

func (ContainerClosed) Name() Name     { return NameContainerClosed }
func (ContainerClosed) Domain() Domain { return DomainContainers }

// ContainerSlotsChanged reports slots or properties in one menu changing.
//
// Properties ride this event because the taxonomy declares no name of their
// own and a property is part of a menu's contents — a furnace's burn time, a
// brewing stand's progress. Slots names the slot indices that changed and
// Properties the property indices.
type ContainerSlotsChanged struct {
	Stamp

	ContainerID int32
	Slots       []int32
	Properties  []int32
	// StateID is the synchronization counter 775 attaches to every inventory
	// change. StateKnown is false on protocol 47, which has no such counter,
	// and zero is a valid state ID so the two cannot be told apart otherwise.
	StateID    int32
	StateKnown bool
	// Dropped counts slots refused by the container's bound.
	Dropped int
}

func (ContainerSlotsChanged) Name() Name     { return NameContainerSlotsChanged }
func (ContainerSlotsChanged) Domain() Domain { return DomainContainers }

// ContainerCursorChanged reports the stack held on the cursor changing.
//
// The cursor is not a slot in any menu, which is why it is its own event.
// Protocol 775 sends a packet for it; protocol 47 addresses it as slot -1 of
// container -1, and that special case is the kind that silently corrupts an
// inventory model when it is missed.
type ContainerCursorChanged struct {
	Stamp

	// Empty reports that the cursor is now holding nothing.
	Empty bool
}

func (ContainerCursorChanged) Name() Name     { return NameContainerCursorChanged }
func (ContainerCursorChanged) Domain() Domain { return DomainContainers }

// ContainerRecipesChanged reports the server's recipe book changing.
//
// Protocol 47 has no recipe packet of any kind — 1.8 has no recipe book — so
// this never fires there, and the snapshot reports the recipe set as never
// supplied rather than as empty.
type ContainerRecipesChanged struct {
	Stamp

	Added   []int32
	Removed []int32
	// Declared counts the recipes a full declaration carried. The recipes
	// themselves are wire structures this milestone does not model; a caller
	// that needs one reads the raw packet until M9's crafting plans land.
	Declared int
	// Replaced reports that the server replaced the book rather than adding to
	// it.
	Replaced bool
}

func (ContainerRecipesChanged) Name() Name     { return NameContainerRecipesChanged }
func (ContainerRecipesChanged) Domain() Domain { return DomainContainers }

// ContainerTradesChanged reports a villager's trade list.
//
// Count is how many trades arrived; the trades themselves are wire structures
// M9 models. Protocol 47 sends trades over a plugin channel rather than a
// packet, so this never fires there.
type ContainerTradesChanged struct {
	Stamp

	ContainerID   int32
	Count         int
	VillagerLevel int32
	Experience    int32
	Regular       bool
	CanRestock    bool
}

func (ContainerTradesChanged) Name() Name     { return NameContainerTradesChanged }
func (ContainerTradesChanged) Domain() Domain { return DomainContainers }

// ContainerCraftResponse reports the server answering a recipe-placement
// request. Protocol 47 has no such packet.
type ContainerCraftResponse struct {
	Stamp

	ContainerID int32
}

func (ContainerCraftResponse) Name() Name     { return NameContainerCraftResponse }
func (ContainerCraftResponse) Domain() Domain { return DomainContainers }
