package domain

import "testing"

func TestGitHubAccountAllowlistMatchesLoginCaseInsensitively(t *testing.T) {
	allowed := NewGitHubAccountAllowlist([]string{"FelixGeelhaar"})
	if !allowed.Allows("felixgeelhaar") {
		t.Fatal("allowlist should match GitHub logins without regard to case")
	}
	if allowed.Allows("someone-else") {
		t.Fatal("allowlist should reject an unlisted GitHub login")
	}
	if allowed.Allows(" ") {
		t.Fatal("allowlist should reject an empty GitHub login")
	}
}
