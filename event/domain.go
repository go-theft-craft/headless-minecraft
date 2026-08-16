// Package event declares the headless client's event taxonomy.
//
// An event announces what changed in observed state, not which packet
// arrived. Four packets move an entity in protocol 775 and one event reports
// it, so a subscriber written against this taxonomy keeps working when a
// version changes which packet carries a fact.
//
// This package knows nothing about the protocol. It imports no generated code
// and no wire types, which is what lets both the client and the per-version
// adapters depend on it without a cycle.
package event

// Domain groups events by the part of observed state they describe. It is a
// bitmask so a subscriber selects several domains in one value.
type Domain uint

const (
	// DomainSession covers connection lifecycle.
	DomainSession Domain = 1 << iota
	// DomainPlayer covers the local player's own state.
	DomainPlayer
	// DomainWorld covers chunks, blocks, and environment.
	DomainWorld
	// DomainEntities covers tracked entities other than the local player.
	DomainEntities
	// DomainContainers covers open menus, slots, and recipes.
	DomainContainers
	// DomainRegistry covers server-supplied registries, tags, and commands.
	DomainRegistry
	// DomainChat covers chat and presentational UI.
	DomainChat
	// DomainRaw selects undecoded protocol packets. It names no events of its
	// own: a raw delivery carries the packet, not a taxonomy entry.
	DomainRaw
)

// Name is an event's stable identifier. Names are prefixed by domain so a log
// line or a filter expression is readable without a lookup table.
//
// The design called this EventName; inside this package that stutters as
// event.EventName, which the linter refuses and a reader would too.
type Name string

// Event is one immutable observation. Implementations are values, not
// pointers, so a subscriber cannot mutate what another subscriber sees.
type Event interface {
	Name() Name
	Domain() Domain
}
