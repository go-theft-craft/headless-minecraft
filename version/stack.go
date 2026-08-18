package version

// Stacks is an adapter that can interpret its own decoded item stacks.
//
// The world keeps items exactly as a protocol decoded them, in an `any`,
// because the two wire shapes share nothing. That makes even "is this slot
// empty" a version question: protocol 47 decodes an empty slot as a value
// whose block ID is -1, and 775 decodes one as nil. A caller that needs the
// answer — the click path deciding whether an outcome is predictable — asks
// the adapter rather than guessing at either shape.
type Stacks interface {
	// StackEmpty reports whether a decoded stack holds nothing. A nil stack
	// is empty on every protocol.
	StackEmpty(stack any) bool
}
