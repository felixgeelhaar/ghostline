package domain

import "testing"

func TestGitHubIdentityRequiresPositiveNativeIDAndLogin(t *testing.T) {
	for _, tc := range []struct {
		name  string
		id    int64
		login string
		valid bool
	}{
		{name: "valid", id: 42, login: "Felix-Dev", valid: true},
		{name: "zero id", id: 0, login: "Felix-Dev"},
		{name: "invalid login", id: 42, login: "../admin"},
		{name: "empty login", id: 42},
	} {
		t.Run(tc.name, func(t *testing.T) {
			identity, err := NewGitHubIdentity(tc.id, tc.login)
			if (err == nil) != tc.valid {
				t.Fatalf("NewGitHubIdentity() error = %v, want valid %v", err, tc.valid)
			}
			if tc.valid && (identity.ID != tc.id || identity.Login != tc.login) {
				t.Fatalf("identity = %+v", identity)
			}
		})
	}
}

func TestAllowedGitHubIdentityUsesStableNativeID(t *testing.T) {
	allowed := map[int64]struct{}{42: {}}
	if !GitHubIdentityAllowed(42, allowed) {
		t.Fatal("expected matching GitHub account ID")
	}
	if GitHubIdentityAllowed(43, allowed) {
		t.Fatal("unexpected allowlist match")
	}
}
