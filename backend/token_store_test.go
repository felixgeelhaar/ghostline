package main

import (
	"fmt"
	"net/http"
	"testing"
	"time"
)

// With open sign-in any account can sign in again and again; each sign-in keeps
// a token for 12 hours. One account must not be able to fill the shared store.
// completeSignIn mirrors a successful callback: keep the token, issue the
// session, then trim the account's older tokens.
func completeSignIn(t *testing.T, auth *authManager, sessionID string, accountID int64, token string, expiresAt time.Time) {
	t.Helper()
	if !auth.storeUserToken(sessionID, accountID, token, expiresAt) {
		t.Fatalf("sign-in %s was refused", sessionID)
	}
	auth.trimAccountTokens(accountID)
}

func TestRepeatedSignInsByOneAccountKeepOnlyItsNewestTokens(t *testing.T) {
	auth := newAuthManager(testAuthConfig(), nil)
	start := auth.now()
	total := maxUserTokensPerAccount + 3
	for i := 0; i < total; i++ {
		completeSignIn(t, auth, fmt.Sprintf("session-%d", i), 42, fmt.Sprintf("token-%d", i), start.Add(time.Hour+time.Duration(i)*time.Minute))
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
		completeSignIn(t, auth, fmt.Sprintf("flood-%d", i), 42, "t", later)
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
	completeSignIn(t, auth, "alice", 1, "alice-token", later)
	for i := 0; i < maxUserTokensPerAccount+2; i++ {
		completeSignIn(t, auth, fmt.Sprintf("bob-%d", i), 2, "bob-token", later)
	}
	if auth.userTokenFor("alice") != "alice-token" {
		t.Fatal("another account's sign-ins evicted alice's token")
	}
}

// A sign-in that is canceled after its token was kept must not cost the
// account one of its existing sessions.
func TestCanceledSignInKeepsTheAccountsOtherSessions(t *testing.T) {
	auth := newAuthManager(testAuthConfig(), nil)
	start := auth.now()
	for i := 0; i < maxUserTokensPerAccount; i++ {
		completeSignIn(t, auth, fmt.Sprintf("session-%d", i), 42, "t", start.Add(time.Hour+time.Duration(i)*time.Minute))
	}
	// The callback keeps the new token, then the session cannot be issued.
	if !auth.storeUserToken("canceled", 42, "t", start.Add(2*time.Hour)) {
		t.Fatal("token store refused the new sign-in")
	}
	auth.dropUserToken("canceled")
	for i := 0; i < maxUserTokensPerAccount; i++ {
		if auth.userTokenFor(fmt.Sprintf("session-%d", i)) == "" {
			t.Fatalf("existing session %d lost its token to a canceled sign-in", i)
		}
	}
}

func TestOAuthCallbackKeepsOnlyTheAccountsNewestTokens(t *testing.T) {
	auth := newAuthManager(testAuthConfig(), oauthClientGranting(t, `{"access_token":"user-oauth-token","token_type":"bearer","scope":"read:user"}`))
	router := newRouterWithAuth((&recordingGitHub{}).client(), nil, newRateLimiter(1000, time.Minute, time.Now), clientIdentity{}, nil, auth)
	for i := 0; i < maxUserTokensPerAccount+2; i++ {
		if completed := runOAuthCallback(t, router); completed.Code != http.StatusSeeOther {
			t.Fatalf("sign-in %d = %d %s", i, completed.Code, completed.Body.String())
		}
	}
	if got := auth.storedUserTokens(); got != maxUserTokensPerAccount {
		t.Fatalf("one account holds %d tokens after repeated sign-ins, want %d", got, maxUserTokensPerAccount)
	}
}
