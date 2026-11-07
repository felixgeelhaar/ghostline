package domain

import "testing"

func TestNewRepositoryIdentity(t *testing.T) {
	identity, err := NewRepositoryIdentity(" acme ", "ghostline ")
	if err != nil || identity.FullName() != "acme/ghostline" {
		t.Fatalf("identity = %+v, error = %v", identity, err)
	}
	if !identity.Equal(RepositoryIdentity{Owner: "ACME", Name: "Ghostline"}) {
		t.Fatal("repository identity equality should follow GitHub's case-insensitive names")
	}
	if _, err := NewRepositoryIdentity("acme", " "); err != ErrInvalidRepositoryIdentity {
		t.Fatalf("empty repository name error = %v", err)
	}
}
