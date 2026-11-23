package main

import (
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"
)

// completeSignIn mirrors a successful callback: keep the token as pending,
// issue the session, then commit the token (which trims older sessions).
func completeSignIn(t *testing.T, auth *authManager, sessionID string, accountID int64, token string, expiresAt time.Time) {
	t.Helper()
	if err := auth.storeUserToken(sessionID, accountID, token, expiresAt); err != nil {
		t.Fatalf("sign-in %s was refused: %v", sessionID, err)
	}
	auth.commitUserToken(sessionID, accountID)
}

// With open sign-in any account can sign in again and again; each sign-in keeps
// a token for 12 hours. One account must not be able to fill the shared store.
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

// Sign-ins within the same clock tick share an expiry; the order they were
// kept in, not map order, decides which are newest.
func TestSignInsWithTheSameExpiryKeepTheNewest(t *testing.T) {
	auth := newAuthManager(testAuthConfig(), nil)
	later := auth.now().Add(time.Hour)
	total := maxUserTokensPerAccount + 2
	for i := 0; i < total; i++ {
		completeSignIn(t, auth, fmt.Sprintf("session-%d", i), 42, fmt.Sprintf("token-%d", i), later)
	}
	for i := total - maxUserTokensPerAccount; i < total; i++ {
		if got := auth.userTokenFor(fmt.Sprintf("session-%d", i)); got != fmt.Sprintf("token-%d", i) {
			t.Fatalf("newest session %d lost its token (holds %q)", i, got)
		}
	}
}

func TestOneAccountCannotLockOthersOutOfSignIn(t *testing.T) {
	auth := newAuthManager(testAuthConfig(), nil)
	later := auth.now().Add(time.Hour)
	for i := 0; i < maxStoredUserTokens; i++ {
		completeSignIn(t, auth, fmt.Sprintf("flood-%d", i), 42, "t", later)
	}
	if err := auth.storeUserToken("someone-else", 7, "their-token", later); err != nil {
		t.Fatalf("another account was refused after one account signed in repeatedly: %v", err)
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
	later := auth.now().Add(time.Hour)
	for i := 0; i < maxUserTokensPerAccount; i++ {
		completeSignIn(t, auth, fmt.Sprintf("session-%d", i), 42, "t", later)
	}
	if err := auth.storeUserToken("canceled", 42, "t", later); err != nil {
		t.Fatal(err)
	}
	auth.dropUserToken("canceled")
	for i := 0; i < maxUserTokensPerAccount; i++ {
		if auth.userTokenFor(fmt.Sprintf("session-%d", i)) == "" {
			t.Fatalf("existing session %d lost its token to a canceled sign-in", i)
		}
	}
}

// Another callback's trim must never remove a token whose session is still
// being issued, or that session would start without one.
func TestPendingSignInKeepsItsTokenWhileOthersComplete(t *testing.T) {
	auth := newAuthManager(testAuthConfig(), nil)
	later := auth.now().Add(time.Hour)
	if err := auth.storeUserToken("slow", 42, "slow-token", later); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < maxUserTokensPerAccount+3; i++ {
		completeSignIn(t, auth, fmt.Sprintf("fast-%d", i), 42, "t", later)
	}
	if auth.userTokenFor("slow") != "slow-token" {
		t.Fatal("a pending sign-in lost its token to other sign-ins of the same account")
	}
}

// Overlapping callbacks of one account are bounded before any is committed,
// so they cannot fill the store either.
func TestOneAccountsOverlappingSignInsAreBounded(t *testing.T) {
	auth := newAuthManager(testAuthConfig(), nil)
	later := auth.now().Add(time.Hour)
	for i := 0; i < maxPendingSignInsPerAccount; i++ {
		if err := auth.storeUserToken(fmt.Sprintf("pending-%d", i), 42, "t", later); err != nil {
			t.Fatalf("pending sign-in %d refused: %v", i, err)
		}
	}
	if err := auth.storeUserToken("one-too-many", 42, "t", later); !errors.Is(err, errSignInInProgress) {
		t.Fatalf("store = %v, want errSignInInProgress", err)
	}
	if err := auth.storeUserToken("someone-else", 7, "t", later); err != nil {
		t.Fatalf("another account was refused: %v", err)
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

func TestOAuthCallbackRefusesWhileTooManySignInsAreInProgress(t *testing.T) {
	auth := newAuthManager(testAuthConfig(), oauthClientGranting(t, `{"access_token":"user-oauth-token","token_type":"bearer","scope":"read:user"}`))
	later := auth.now().Add(time.Hour)
	for i := 0; i < maxPendingSignInsPerAccount; i++ {
		if err := auth.storeUserToken(fmt.Sprintf("pending-%d", i), 42, "t", later); err != nil { // 42 is the fake OAuth identity
			t.Fatal(err)
		}
	}
	router := newRouterWithAuth((&recordingGitHub{}).client(), nil, newRateLimiter(1000, time.Minute, time.Now), clientIdentity{}, nil, auth)
	completed := runOAuthCallback(t, router)
	if completed.Code != http.StatusTooManyRequests {
		t.Fatalf("callback = %d %s, want 429", completed.Code, completed.Body.String())
	}
	assertNoSessionCookie(t, completed)
}
