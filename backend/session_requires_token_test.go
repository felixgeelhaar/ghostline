package main

import "testing"

// Sign-out ends a session only by dropping its token, so the one function that
// accepts a session cookie must refuse a cookie whose token is gone. A route
// that used it could otherwise accept signed-out cookies until they expire.
func TestSessionFromCookieRefusesASessionWithoutItsToken(t *testing.T) {
	auth := newAuthManager(testAuthConfig(), nil)
	cookie := signedInSessionCookie(t, auth, testIdentity())

	session, token, ok := auth.sessionFromCookie(cookie.Value)
	if !ok {
		t.Fatal("a signed-in session was refused")
	}
	if token != "user-oauth-token" {
		t.Fatalf("token = %q, want the session's held token", token)
	}

	auth.dropUserToken(session.ID)

	if _, _, ok := auth.sessionFromCookie(cookie.Value); ok {
		t.Fatal("a session whose token was dropped was still accepted")
	}
}

// An account removed from the allowlist must lose its sessions at the same place.
func TestSessionFromCookieRefusesAnAccountNoLongerAllowed(t *testing.T) {
	auth := newAuthManager(testAuthConfig(), nil)
	cookie := signedInSessionCookie(t, auth, testIdentity())

	auth.cfg.AllowedIDs = map[int64]struct{}{7: {}}

	if _, _, ok := auth.sessionFromCookie(cookie.Value); ok {
		t.Fatal("a session of an account no longer on the allowlist was accepted")
	}
}
