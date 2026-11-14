package domain

import "testing"

func TestGitHubAccountAllowlistUsesImmutableAccountIDs(t *testing.T) {
	allowed, err := NewGitHubAccountAllowlist([]string{"6020564", " 6020564 "})
	if err != nil {
		t.Fatal(err)
	}
	if !allowed.Allows(6020564) {
		t.Fatal("allowlist should accept an explicitly configured account ID")
	}
	if allowed.Allows(6020565) || allowed.Allows(0) {
		t.Fatal("allowlist should reject unlisted or invalid account IDs")
	}
}

func TestGitHubAccountAllowlistRejectsInvalidIDs(t *testing.T) {
	for _, input := range [][]string{{"felixgeelhaar"}, {"+42"}, {"042"}, {"0"}, {"-1"}, {"9223372036854775808"}, {" "}} {
		if _, err := NewGitHubAccountAllowlist(input); err == nil {
			t.Errorf("NewGitHubAccountAllowlist(%q) succeeded, want invalid-ID error", input)
		}
	}
}

func TestValidGitHubLogin(t *testing.T) {
	for _, login := range []string{"FelixGeelhaar", "mona-cat_octo", "octo_admin", "a12345678"} {
		if !ValidGitHubLogin(login) {
			t.Errorf("ValidGitHubLogin(%q) = false, want true", login)
		}
	}

	for _, login := range []string{"", "-mona", "mona-", "_octo", "mona-cat_oc", "mona-cat_123456789", "mona_cat_octo", "mona-cat_oct!"} {
		if ValidGitHubLogin(login) {
			t.Errorf("ValidGitHubLogin(%q) = true, want false", login)
		}
	}
}
