package safety

import (
	"errors"
	"fmt"
	"net"
	"slices"
	"strings"
)

// ErrUnauthorized reports an operation outside its declared endpoint or scope.
var ErrUnauthorized = errors.New("automation is not authorized")

// Authorization declares the endpoint and scopes selected by the application.
// It records intent and does not prove that a remote server permits automation.
type Authorization struct {
	endpoint string
	scopes   []Scope
}

// Authorize validates and records an endpoint-scoped automation declaration.
func Authorize(endpoint string, scopes ...Scope) (Authorization, error) {
	normalized, err := normalizeEndpoint(endpoint)
	if err != nil {
		return Authorization{}, err
	}

	owned := slices.Clone(scopes)
	slices.Sort(owned)
	owned = slices.Compact(owned)
	if len(owned) == 0 {
		return Authorization{}, fmt.Errorf("%w: no scopes declared", ErrUnauthorized)
	}

	return Authorization{endpoint: normalized, scopes: owned}, nil
}

// Allows reports whether the declaration covers an endpoint and scope.
func (a Authorization) Allows(endpoint string, scope Scope) bool {
	normalized, err := normalizeEndpoint(endpoint)
	if err != nil || normalized != a.endpoint {
		return false
	}

	return slices.Contains(a.scopes, scope)
}

// Endpoint returns the normalized authorized endpoint.
func (a Authorization) Endpoint() string {
	return a.endpoint
}

// Scopes returns an owned copy of the authorized scopes.
func (a Authorization) Scopes() []Scope {
	return slices.Clone(a.scopes)
}

func normalizeEndpoint(endpoint string) (string, error) {
	host, port, err := net.SplitHostPort(strings.TrimSpace(endpoint))
	if err != nil || host == "" || port == "" {
		return "", fmt.Errorf("%w: endpoint must use host:port form", ErrUnauthorized)
	}

	return net.JoinHostPort(strings.ToLower(host), port), nil
}
