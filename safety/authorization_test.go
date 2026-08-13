package safety

import "testing"

func TestAuthorizationBindsEndpointAndScope(t *testing.T) {
	t.Parallel()

	authorization, err := Authorize("Example.org:25565", ScopeObserve, ScopeMove)
	if err != nil {
		t.Fatalf("Authorize() error = %v", err)
	}
	if !authorization.Allows("example.org:25565", ScopeMove) {
		t.Fatal("Allows() = false for declared endpoint and scope")
	}
	if authorization.Allows("example.org:25566", ScopeMove) {
		t.Fatal("Allows() = true for a different endpoint")
	}
	if authorization.Allows("example.org:25565", ScopeInventory) {
		t.Fatal("Allows() = true for an undeclared scope")
	}
}

func TestAuthorizeRejectsMissingScope(t *testing.T) {
	t.Parallel()

	if _, err := Authorize("example.org:25565"); err == nil {
		t.Fatal("Authorize() error = nil, want an error")
	}
}
