package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/felixgeelhaar/ghostline/backend/internal/domain"
)

// signedInSessionCookie is a session as a completed sign-in leaves it: a signed
// cookie plus the user's GitHub token held in server memory.
func signedInSessionCookie(t *testing.T, m *authManager, identity domain.GitHubIdentity) *http.Cookie {
	t.Helper()
	expiry := m.now().Add(time.Hour)
	value, id, err := m.newSession(identity, expiry)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.storeUserToken(id, identity.ID, "user-oauth-token", expiry); err != nil {
		t.Fatalf("token store refused the test token: %v", err)
	}
	m.commitUserToken(id, identity.ID)
	return &http.Cookie{Name: sessionCookieName, Value: value}
}

func sessionStatus(router http.Handler, cookie *http.Cookie) string {
	request := httptest.NewRequest(http.MethodGet, "/api/session", nil)
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response.Body.String()
}

// After a restart (or a dropped token) the signed cookie is still valid, but the
// token it relied on is gone. Reads must not fall back to the shared anonymous quota.
func TestSessionWithoutItsGitHubTokenMustSignInAgain(t *testing.T) {
	github := &recordingGitHub{}
	auth := newAuthManager(testAuthConfig(), nil)
	router := newRouterWithAuth(github.client(), nil, newRateLimiter(10, time.Minute, time.Now), clientIdentity{}, nil, auth)
	cookie, err := auth.newSessionCookie(testIdentity(), time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}

	response := importWithSession(router, cookie, "")
	if response.Code != http.StatusUnauthorized || !strings.Contains(response.Body.String(), `"code":"AUTH_REQUIRED"`) || !strings.Contains(response.Body.String(), "Sign in with GitHub again") {
		t.Fatalf("import without a stored token = %d %s", response.Code, response.Body.String())
	}
	if seen := github.seen(); len(seen) != 0 {
		t.Fatalf("GitHub was called %d times without the user's token", len(seen))
	}
	if status := sessionStatus(router, cookie); strings.Contains(status, `"authenticated":true`) {
		t.Fatalf("session without a stored token reported as signed in: %s", status)
	}
}

func TestRestartAsksEverySessionToSignInAgain(t *testing.T) {
	github := &recordingGitHub{}
	before := newAuthManager(testAuthConfig(), oauthClientGranting(t, `{"access_token":"user-oauth-token","token_type":"bearer","scope":"read:user"}`))
	session := signInThroughOAuth(t, newRouterWithAuth(github.client(), nil, newRateLimiter(10, time.Minute, time.Now), clientIdentity{}, nil, before))

	after := newAuthManager(testAuthConfig(), nil) // same secret, empty token store
	router := newRouterWithAuth(github.client(), nil, newRateLimiter(10, time.Minute, time.Now), clientIdentity{}, nil, after)
	if response := importWithSession(router, session, ""); response.Code != http.StatusUnauthorized {
		t.Fatalf("import after restart = %d %s, want 401", response.Code, response.Body.String())
	}
	if len(github.seen()) != 0 {
		t.Fatal("GitHub was called anonymously after the restart")
	}
}

func TestSignInIsRefusedWhenTheTokenStoreIsFull(t *testing.T) {
	auth := newAuthManager(testAuthConfig(), oauthClientGranting(t, `{"access_token":"user-oauth-token","token_type":"bearer","scope":"read:user"}`))
	later := auth.now().Add(time.Hour)
	for i := 0; i < maxStoredUserTokens; i++ {
		auth.userTokens[strings.Repeat("x", 8)+string(rune('a'+i%26))+time.Duration(i).String()] = storedUserToken{accountID: int64(1000 + i), token: "t", expiresAt: later}
	}
	router := newRouterWithAuth((&recordingGitHub{}).client(), nil, newRateLimiter(10, time.Minute, time.Now), clientIdentity{}, nil, auth)
	completed := runOAuthCallback(t, router)
	if completed.Code != http.StatusServiceUnavailable {
		t.Fatalf("callback with a full token store = %d %s", completed.Code, completed.Body.String())
	}
	assertNoSessionCookie(t, completed)
}

// The user's read:user token cannot read private repositories, so a request that
// carries it needs no visibility check with the shared server token.
func TestVisibilityCheckDoesNotSpendTheServerTokenForSignedInUsers(t *testing.T) {
	github := &recordingGitHub{}
	client := github.client()
	client.token = "server-token"
	auth := newAuthManager(testAuthConfig(), nil)
	router := newRouterWithAuth(client, nil, newRateLimiter(10, time.Minute, time.Now), clientIdentity{}, nil, auth)
	importWithSession(router, signedInSessionCookie(t, auth, testIdentity()), "")
	seen := github.seen()
	if len(seen) == 0 {
		t.Fatal("GitHub was not called")
	}
	for _, authorization := range seen {
		if authorization != "Bearer user-oauth-token" {
			t.Fatalf("GitHub call carried %q, want only the user's token", authorization)
		}
	}
}

func TestVisibilityCheckStillUsesTheServerTokenWithoutSignIn(t *testing.T) {
	github := &recordingGitHub{}
	client := github.client()
	client.token = "server-token"
	router := newRouterWithAuth(client, nil, newRateLimiter(10, time.Minute, time.Now), clientIdentity{}, nil, nil)
	importWithSession(router, nil, "")
	if seen := github.seen(); len(seen) == 0 || seen[0] != "Bearer server-token" {
		t.Fatalf("visibility check without sign-in carried %q, want the server token", seen)
	}
}
