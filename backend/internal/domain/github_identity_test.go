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
