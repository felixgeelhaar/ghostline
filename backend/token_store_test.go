package main

import (
	"fmt"
	"testing"
	"time"
)

// With open sign-in any account can sign in again and again; each sign-in keeps
// a token for 12 hours. One account must not be able to fill the shared store.
func TestRepeatedSignInsByOneAccountKeepOnlyItsNewestTokens(t *testing.T) {
	auth := newAuthManager(testAuthConfig(), nil)
	start := auth.now()
	total := maxUserTokensPerAccount + 3
	for i := 0; i < total; i++ {
		if !auth.storeUserToken(fmt.Sprintf("session-%d", i), 42, fmt.Sprintf("token-%d", i), start.Add(time.Hour+time.Duration(i)*time.Minute)) {
			t.Fatalf("sign-in %d was refused", i)
		}
	}
	if got := auth.storedUserTokens(); got != maxUserTokensPerAccount {
		t.Fatalf("account holds %d tokens, want %d", got, maxUserTokensPerAccount)
	}
	for i := 0; i < total-maxUserTokensPerAccount; i++ {
		if auth.userTokenFor(fmt.Sprintf("session-%d", i)) != "" {
			t.Fatalf("oldest session %d kept its token", i)
		}
	}
	for i := total - maxUserTokensPerAccount; i < total; i++ {
		if got := auth.userTokenFor(fmt.Sprintf("session-%d", i)); got != fmt.Sprintf("token-%d", i) {
			t.Fatalf("newest session %d holds %q", i, got)
		}
	}
}

func TestOneAccountCannotLockOthersOutOfSignIn(t *testing.T) {
	auth := newAuthManager(testAuthConfig(), nil)
	later := auth.now().Add(time.Hour)
	for i := 0; i < maxStoredUserTokens; i++ {
		auth.storeUserToken(fmt.Sprintf("flood-%d", i), 42, "t", later)
	}
	if !auth.storeUserToken("someone-else", 7, "their-token", later) {
		t.Fatal("another account was refused after one account signed in repeatedly")
	}
	if auth.userTokenFor("someone-else") != "their-token" {
		t.Fatal("the other account's token was not kept")
	}
}

func TestSignInsOfDifferentAccountsDoNotEvictEachOther(t *testing.T) {
	auth := newAuthManager(testAuthConfig(), nil)
	later := auth.now().Add(time.Hour)
	auth.storeUserToken("alice", 1, "alice-token", later)
	for i := 0; i < maxUserTokensPerAccount+2; i++ {
		auth.storeUserToken(fmt.Sprintf("bob-%d", i), 2, "bob-token", later)
	}
	if auth.userTokenFor("alice") != "alice-token" {
		t.Fatal("another account's sign-ins evicted alice's token")
	}
}
