package behaviour

import (
	"fmt"
	"strings"

	"github.com/go-theft-craft/headless-minecraft/safety"
)

// RequireScopes reports whether an authorization covers everything a behaviour
// needs, and names everything it does not.
//
// It names every missing scope rather than the first. A caller fixing an
// authorization wants the whole list: finding out one scope at a time, one
// construction at a time, is the same work spread over as many edits as there
// are scopes.
//
// It is checked at construction and not per tick. That is the client's own rule
// — components are selected and validated before network work begins — and the
// reason for it is that a behaviour which discovered on tick four hundred that
// it may not dig has already walked the bot somewhere it should not be.
func RequireScopes(authorization safety.Authorization, endpoint string, scopes ...safety.Scope) error {
	var missing []string
	for _, scope := range scopes {
		if !authorization.Allows(endpoint, scope) {
			missing = append(missing, string(scope))
		}
	}
	if len(missing) == 0 {
		return nil
	}

	return fmt.Errorf("%w: this behaviour needs %s at %s",
		safety.ErrUnauthorized, strings.Join(missing, ", "), endpoint)
}
