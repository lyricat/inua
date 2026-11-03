package ui

import "testing"

func TestLoginSessionIDFromEnvironment(t *testing.T) {
	t.Setenv("XDG_SESSION_ID", "c1520071")
	if got := loginSessionID(); got != "c1520071" {
		t.Fatalf("loginSessionID = %q", got)
	}
}

func TestValidSessionIDRejectsPathPieces(t *testing.T) {
	for _, id := range []string{"../x", "a/b", "a b", ""} {
		if validSessionID(id) {
			t.Errorf("validSessionID(%q) = true", id)
		}
	}
}
