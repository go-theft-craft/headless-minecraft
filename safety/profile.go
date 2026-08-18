// Package safety defines construction-time authorization and recovery policy.
package safety

// Scope identifies one class of high-level automation.
type Scope string

const (
	// ScopeObserve permits reading normalized client state.
	ScopeObserve Scope = "observe"
	// ScopeMove permits movement operations.
	ScopeMove Scope = "move"
	// ScopeInventory permits inventory operations.
	ScopeInventory Scope = "inventory"
	// ScopeInteract permits block and entity interaction.
	ScopeInteract Scope = "interact"
	// ScopeAttack permits attacking entities.
	//
	// It is separate from ScopeInteract even though the two are one action on
	// the wire — both protocols carry attack as a mode of the same interact
	// packet. The split is a safety decision rather than a protocol one: an
	// authorization that permits opening a chest is not obviously one that
	// permits attacking a player, and a caller that wanted the first and got
	// the second would have no way to say so.
	//
	// The cost of the split is that a caller wanting both declares both. That
	// was judged the smaller cost while there is one caller; reversing it later
	// would mean widening every authorization that had asked for interact
	// alone.
	ScopeAttack Scope = "attack"
	// ScopeDig permits digging operations.
	ScopeDig Scope = "dig"
	// ScopeBuild permits block placement operations.
	ScopeBuild Scope = "build"
)

// Profile configures operation recovery. Strict is the safe default.
type Profile struct {
	RetryAmbiguous        bool
	Reconnect             bool
	ResumeAfterCorrection bool
	RequireKnownCollision bool
}

// Strict returns conservative recovery settings.
func Strict() Profile {
	return Profile{
		RequireKnownCollision: true,
	}
}
