package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// retainedSessionEntries counts what the auth manager keeps in memory per
// session: since sign-out only drops the token, that is the token store.
func retainedSessionEntries(m *authManager) int {
	m.tokenMu.Lock()
	defer m.tokenMu.Unlock()
	return len(m.userTokens)
}

func signOut(t *testing.T, router http.Handler, session *http.Cookie) {
	t.Helper()
	logout := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	logout.Header.Set("Origin", "https://ghostline.example")
	logout.AddCookie(session)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, logout)
	if response.Code != http.StatusNoContent {
		t.Fatalf("logout status = %d, want 204", response.Code)
	}
}

// With open sign-in one account can sign in and out in a loop. Each sign-out must
// free what its session held instead of adding an entry that lives until expiry.
func TestRepeatedSignOutsLeaveNothingBehind(t *testing.T) {
	auth := newAuthManager(testAuthConfig(), nil)
	router := newRouterWithAuth(&githubClient{baseURL: "https://api.github.com", http: http.DefaultClient}, nil, newRateLimiter(10_000, time.Minute, time.Now), clientIdentity{}, nil, auth)

	var signedOut []*http.Cookie
	for range 1000 {
		session := signedInSessionCookie(t, auth, testIdentity())
		signOut(t, router, session)
		signedOut = append(signedOut, session)
	}

	if held := retainedSessionEntries(auth); held != 0 {
		t.Fatalf("auth manager holds %d entries after 1000 sign-outs, want 0", held)
	}
	for _, session := range []*http.Cookie{signedOut[0], signedOut[len(signedOut)-1]} {
		if status := sessionStatus(router, session); !strings.Contains(status, `"authenticated":false`) {
			t.Fatalf("a signed-out session still reports signed in: %s", status)
		}
		protected := httptest.NewRequest(http.MethodPost, "/api/import", strings.NewReader(`{"url":"invalid"}`))
		protected.Header.Set("Content-Type", "application/json")
		protected.AddCookie(session)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, protected)
		if response.Code != http.StatusUnauthorized || !strings.Contains(response.Body.String(), "AUTH_REQUIRED") {
			t.Fatalf("a signed-out session reached the API: %d %s", response.Code, response.Body.String())
		}
	}
}

// Signing out one session must not sign out the account's other sessions.
func TestSignOutEndsOnlyThatSession(t *testing.T) {
	auth := newAuthManager(testAuthConfig(), nil)
	router := newRouterWithAuth(&githubClient{baseURL: "https://api.github.com", http: http.DefaultClient}, nil, newRateLimiter(100, time.Minute, time.Now), clientIdentity{}, nil, auth)
	laptop := signedInSessionCookie(t, auth, testIdentity())
	phone := signedInSessionCookie(t, auth, testIdentity())

	signOut(t, router, laptop)

	if status := sessionStatus(router, laptop); !strings.Contains(status, `"authenticated":false`) {
		t.Fatalf("signed-out session still reports signed in: %s", status)
	}
	if status := sessionStatus(router, phone); !strings.Contains(status, `"authenticated":true`) {
		t.Fatalf("the account's other session was signed out too: %s", status)
	}
}
