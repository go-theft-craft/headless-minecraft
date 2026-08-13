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
