package main

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/felixgeelhaar/ghostline/backend/internal/domain"
	"github.com/gin-gonic/gin"
)

func TestLoadAuthConfig(t *testing.T) {
	tests := []struct {
		name     string
		env      map[string]string
		valid    bool
		required bool
	}{
		{name: "disabled local demo", env: map[string]string{}, valid: true},
		{name: "production rejects disabled auth", env: map[string]string{"GHOSTLINE_ENV": "production"}},
		{name: "hosted auth configured", env: map[string]string{
			"GHOSTLINE_ENV":       "production",
			"GHOSTLINE_AUTH_MODE": "required", "GITHUB_OAUTH_CLIENT_ID": "client-id",
			"GITHUB_OAUTH_CLIENT_SECRET": "client-secret", "GITHUB_OAUTH_CALLBACK_URL": "https://ghostline.example/auth/callback",
			"GHOSTLINE_SESSION_SECRET": strings.Repeat("s", 32), "GHOSTLINE_ALLOWED_GITHUB_IDS": "42, 43",
		}, valid: true, required: true},
		{name: "missing OAuth secret", env: map[string]string{
			"GHOSTLINE_AUTH_MODE": "required", "GITHUB_OAUTH_CLIENT_ID": "client-id",
			"GITHUB_OAUTH_CALLBACK_URL": "https://ghostline.example/auth/callback",
			"GHOSTLINE_SESSION_SECRET":  strings.Repeat("s", 32), "GHOSTLINE_ALLOWED_GITHUB_IDS": "42",
		}},
		{name: "short signing secret", env: map[string]string{
			"GHOSTLINE_AUTH_MODE": "required", "GITHUB_OAUTH_CLIENT_ID": "client-id",
			"GITHUB_OAUTH_CLIENT_SECRET": "client-secret", "GITHUB_OAUTH_CALLBACK_URL": "https://ghostline.example/auth/callback",
			"GHOSTLINE_SESSION_SECRET": "too-short", "GHOSTLINE_ALLOWED_GITHUB_IDS": "42",
		}},
		{name: "insecure hosted callback", env: map[string]string{
			"GHOSTLINE_AUTH_MODE": "required", "GITHUB_OAUTH_CLIENT_ID": "client-id",
			"GITHUB_OAUTH_CLIENT_SECRET": "client-secret", "GITHUB_OAUTH_CALLBACK_URL": "http://ghostline.example/auth/callback",
			"GHOSTLINE_SESSION_SECRET": strings.Repeat("s", 32), "GHOSTLINE_ALLOWED_GITHUB_IDS": "42",
		}},
		{name: "invalid account in allowlist", env: map[string]string{
			"GHOSTLINE_AUTH_MODE": "required", "GITHUB_OAUTH_CLIENT_ID": "client-id",
			"GITHUB_OAUTH_CLIENT_SECRET": "client-secret", "GITHUB_OAUTH_CALLBACK_URL": "https://ghostline.example/auth/callback",
			"GHOSTLINE_SESSION_SECRET": strings.Repeat("s", 32), "GHOSTLINE_ALLOWED_GITHUB_IDS": "../admin",
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := loadAuthConfig(func(key string) string { return tc.env[key] })
			if (err == nil) != tc.valid {
				t.Fatalf("loadAuthConfig() error = %v, want valid %v", err, tc.valid)
			}
			if err == nil && cfg.Required != tc.required {
				t.Fatalf("Required = %v, want %v", cfg.Required, tc.required)
			}
			if err == nil && cfg.Required {
				if _, ok := cfg.AllowedIDs[42]; !ok {
					t.Fatal("GitHub account ID was not loaded")
				}
				if cfg.CallbackURL != "https://ghostline.example/auth/callback" {
					t.Fatalf("callback URL = %q", cfg.CallbackURL)
				}
			}
		})
	}
}

func TestOAuthAuthorizationURLUsesStateAndPKCE(t *testing.T) {
	manager := newAuthManager(testAuthConfig(), nil)
	state := "state-value"
	verifier := "verifier-value"
	target, err := manager.authorizationURL(state, verifier)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(target)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	if parsed.Scheme != "https" || parsed.Host != "github.com" || parsed.Path != "/login/oauth/authorize" {
		t.Fatalf("authorization endpoint = %s", target)
	}
	if query.Get("state") != state || query.Get("code_challenge_method") != "S256" || query.Get("code_challenge") == "" {
		t.Fatalf("OAuth request omitted state or PKCE parameters: %s", target)
	}
	if query.Get("scope") != "read:user" || query.Get("redirect_uri") != manager.cfg.CallbackURL {
		t.Fatalf("OAuth request asked for unexpected access: %s", target)
	}
}

func TestSignedSessionRejectsTamperingAndExpiry(t *testing.T) {
	manager := newAuthManager(testAuthConfig(), nil)
	cookie, err := manager.newSessionCookie(testIdentity(), manager.now().Add(-1))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := manager.parseSessionCookie(cookie.Value); ok {
		t.Fatal("expired session was accepted")
	}
	cookie, err = manager.newSessionCookie(testIdentity(), manager.now().Add(timeHour))
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(cookie.Value, ".")
	if len(parts) != 3 || parts[1] == "" {
		t.Fatalf("session cookie has unexpected format: %q", cookie.Value)
	}
	first := byte('a')
	if parts[1][0] == first {
		first = 'b'
	}
	parts[1] = string(first) + parts[1][1:]
	tampered := strings.Join(parts, ".")
	if _, ok := manager.parseSessionCookie(tampered); ok {
		t.Fatal("tampered session was accepted")
	}
}

func TestSessionCookiesAreUniqueForTheSameIdentityAndExpiry(t *testing.T) {
	manager := newAuthManager(testAuthConfig(), nil)
	expiry := manager.now().Add(timeHour)
	first, err := manager.newSessionCookie(testIdentity(), expiry)
	if err != nil {
		t.Fatal(err)
	}
	second, err := manager.newSessionCookie(testIdentity(), expiry)
	if err != nil {
		t.Fatal(err)
	}
	if first.Value == second.Value {
		t.Fatal("separate sign-in sessions received the same signed cookie")
	}
}

const timeHour = time.Hour

func testAuthConfig() authConfig {
	return authConfig{
		Required: true, ClientID: "client-id", ClientSecret: "client-secret",
		CallbackURL: "https://ghostline.example/auth/callback", SessionKey: []byte(strings.Repeat("s", 32)),
		AllowedIDs: map[int64]struct{}{42: {}},
	}
}

func testIdentity() domain.GitHubIdentity {
	identity, _ := domain.NewGitHubIdentity(42, "Felix-Dev")
	return identity
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

func oauthResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

func TestProtectedAPIRequiresGitHubSession(t *testing.T) {
	auth := newAuthManager(testAuthConfig(), nil)
	limiter := newRateLimiter(20, time.Minute, time.Now)
	router := newRouterWithAuth(&githubClient{baseURL: "https://api.github.com", http: http.DefaultClient}, nil, limiter, clientIdentity{}, nil, auth)

	request := httptest.NewRequest(http.MethodPost, "/api/import", strings.NewReader(`{"url":"invalid"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous API status = %d, want 401; body=%s", response.Code, response.Body.String())
	}

	session := signedInSessionCookie(t, auth, testIdentity())
	request = httptest.NewRequest(http.MethodPost, "/api/import", strings.NewReader(`{"url":"invalid"}`))
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(session)
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("authenticated API status = %d, want request validation status 400; body=%s", response.Code, response.Body.String())
	}
}

func TestSessionEndpointHidesAccountsRemovedFromTheAllowlist(t *testing.T) {
	config := testAuthConfig()
	config.AllowedIDs = map[int64]struct{}{43: {}}
	auth := newAuthManager(config, nil)
	sessionCookie, err := auth.newSessionCookie(testIdentity(), time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	router := newRouterWithAuth(&githubClient{baseURL: "https://api.github.com", http: http.DefaultClient}, nil, newRateLimiter(10, time.Minute, time.Now), clientIdentity{}, nil, auth)
	request := httptest.NewRequest(http.MethodGet, "/api/session", nil)
	request.AddCookie(sessionCookie)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || strings.Contains(response.Body.String(), `"authenticated":true`) || strings.Contains(response.Body.String(), "Felix-Dev") {
		t.Fatalf("removed account session response = %d %s", response.Code, response.Body.String())
	}
}

func TestGitHubOAuthCallbackIssuesAllowlistedSessionWithoutTheTokenInTheCookie(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		switch request.URL.String() {
		case "https://github.com/login/oauth/access_token":
			if err := request.ParseForm(); err != nil {
				t.Fatal(err)
			}
			if request.Form.Get("client_secret") != "client-secret" || request.Form.Get("code_verifier") == "" {
				t.Fatalf("token request omitted secret or PKCE verifier: %v", request.Form)
			}
			return oauthResponse(http.StatusOK, `{"access_token":"temporary-oauth-token","token_type":"bearer","scope":"read:user"}`), nil
		case "https://api.github.com/user":
			if request.Header.Get("Authorization") != "Bearer temporary-oauth-token" {
				t.Fatalf("GitHub identity request authorization = %q", request.Header.Get("Authorization"))
			}
			return oauthResponse(http.StatusOK, `{"id":42,"login":"Felix-Dev"}`), nil
		default:
			t.Fatalf("unexpected OAuth destination %s", request.URL)
			return nil, nil
		}
	})}
	auth := newAuthManager(testAuthConfig(), client)
	router := newRouterWithAuth(&githubClient{baseURL: "https://api.github.com", http: http.DefaultClient}, nil, newRateLimiter(1, time.Minute, time.Now), clientIdentity{}, nil, auth)

	login := httptest.NewRecorder()
	loginRequest := httptest.NewRequest(http.MethodPost, "/auth/login", nil)
	loginRequest.Header.Set("Origin", "https://ghostline.example")
	router.ServeHTTP(login, loginRequest)
	if login.Code != http.StatusFound {
		t.Fatalf("login status = %d", login.Code)
	}
	stateCookie := login.Result().Cookies()[0]
	if !stateCookie.HttpOnly || !stateCookie.Secure || stateCookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("OAuth state cookie flags = %+v", stateCookie)
	}
	authorization, err := url.Parse(login.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	query := authorization.Query()
	if query.Get("scope") != "read:user" || query.Get("code_challenge_method") != "S256" || query.Get("state") == "" {
		t.Fatalf("OAuth authorization request has invalid scope, state or PKCE: %s", authorization)
	}

	callback := httptest.NewRequest(http.MethodGet, "/auth/callback?state="+url.QueryEscape(query.Get("state"))+"&code=one-time-code", nil)
	callback.AddCookie(stateCookie)
	completed := httptest.NewRecorder()
	router.ServeHTTP(completed, callback)
	if completed.Code != http.StatusSeeOther || completed.Header().Get("Location") != "/?ghostline_auth=complete" {
		t.Fatalf("callback status=%d redirect=%q body=%s", completed.Code, completed.Header().Get("Location"), completed.Body.String())
	}
	var sessionCookie *http.Cookie
	for _, cookie := range completed.Result().Cookies() {
		if cookie.Name == sessionCookieName {
			sessionCookie = cookie
		}
	}
	if sessionCookie == nil || !sessionCookie.HttpOnly || !sessionCookie.Secure || sessionCookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("secure session cookie missing or misconfigured: %+v", sessionCookie)
	}
	if strings.Contains(sessionCookie.Value, "temporary-oauth-token") {
		t.Fatal("GitHub access token was retained in the Ghostline session")
	}

	sessionResponse := httptest.NewRecorder()
	sessionRequest := httptest.NewRequest(http.MethodGet, "/api/session", nil)
	sessionRequest.AddCookie(sessionCookie)
	router.ServeHTTP(sessionResponse, sessionRequest)
	if sessionResponse.Code != http.StatusOK || !strings.Contains(sessionResponse.Body.String(), `"login":"Felix-Dev"`) {
		t.Fatalf("session response status=%d body=%s", sessionResponse.Code, sessionResponse.Body.String())
	}
}

func TestSignInCanCompleteFromTwoTabs(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Host == "github.com" {
			return oauthResponse(http.StatusOK, `{"access_token":"temporary-token","token_type":"bearer","scope":"read:user"}`), nil
		}
		return oauthResponse(http.StatusOK, `{"id":42,"login":"Felix-Dev"}`), nil
	})}
	auth := newAuthManager(testAuthConfig(), client)
	router := newRouterWithAuth(&githubClient{baseURL: "https://api.github.com", http: http.DefaultClient}, nil, newRateLimiter(20, time.Minute, time.Now), clientIdentity{}, nil, auth)
	startLogin := func(cookie *http.Cookie) (string, *http.Cookie) {
		request := httptest.NewRequest(http.MethodPost, "/auth/login", nil)
		request.Header.Set("Origin", "https://ghostline.example")
		if cookie != nil {
			request.AddCookie(cookie)
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusFound {
			t.Fatalf("login status=%d, want 302", response.Code)
		}
		authorization, err := url.Parse(response.Header().Get("Location"))
		if err != nil {
			t.Fatal(err)
		}
		return authorization.Query().Get("state"), response.Result().Cookies()[0]
	}
	firstState, firstCookie := startLogin(nil)
	secondState, secondCookie := startLogin(firstCookie)
	if firstState == secondState || firstCookie.Name == secondCookie.Name {
		t.Fatal("separate sign-in tabs did not receive independent OAuth state cookies")
	}
	complete := func(state string, cookie *http.Cookie) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodGet, "/auth/callback?state="+url.QueryEscape(state)+"&code=one-time-code", nil)
		request.AddCookie(cookie)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		return response
	}
	first := complete(firstState, firstCookie)
	second := complete(secondState, secondCookie)
	if first.Code != http.StatusSeeOther || second.Code != http.StatusSeeOther {
		t.Fatalf("two-tab callback statuses = %d, %d; want 303 for both", first.Code, second.Code)
	}
	firstSession, secondSession := "", ""
	for _, cookie := range first.Result().Cookies() {
		if cookie.Name == sessionCookieName {
			firstSession = cookie.Value
		}
	}
	for _, cookie := range second.Result().Cookies() {
		if cookie.Name == sessionCookieName {
			secondSession = cookie.Value
		}
	}
	if firstSession == "" || secondSession == "" || firstSession == secondSession {
		t.Fatal("separate sign-ins did not receive distinct session cookies")
	}
}

func TestGitHubOAuthRejectsStateMismatchAndNonAllowlistedUser(t *testing.T) {
	for _, tc := range []struct {
		name     string
		login    string
		mismatch bool
		want     int
	}{
		{name: "state mismatch", login: "Felix-Dev", mismatch: true, want: http.StatusUnauthorized},
		{name: "user not allowlisted", login: "other-user", want: http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				if request.URL.Host == "github.com" {
					return oauthResponse(http.StatusOK, `{"access_token":"temporary-token","token_type":"bearer","scope":"read:user"}`), nil
				}
				return oauthResponse(http.StatusOK, `{"id":43,"login":"`+tc.login+`"}`), nil
			})}
			auth := newAuthManager(testAuthConfig(), client)
			router := newRouterWithAuth(&githubClient{baseURL: "https://api.github.com", http: http.DefaultClient}, nil, newRateLimiter(20, time.Minute, time.Now), clientIdentity{}, nil, auth)
			pendingState, err := randomToken(auth.random, 32)
			if err != nil {
				t.Fatal(err)
			}
			pending := oauthState{State: pendingState, Verifier: "verifier", ExpiresAt: time.Now().Add(time.Minute).Unix()}
			value, err := auth.sign("oauth-state", pending)
			if err != nil {
				t.Fatal(err)
			}
			auth.registerOAuthState(pending.State, time.Unix(pending.ExpiresAt, 0))
			queryState := pending.State
			if tc.mismatch {
				queryState = strings.Repeat("x", 43)
			}
			request := httptest.NewRequest(http.MethodGet, "/auth/callback?state="+queryState+"&code=code", nil)
			request.AddCookie(&http.Cookie{Name: oauthCookieNameForState(pending.State), Value: value})
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != tc.want {
				t.Fatalf("callback status = %d, want %d; body=%s", response.Code, tc.want, response.Body.String())
			}
			if tc.name == "state mismatch" {
				for _, cookie := range response.Result().Cookies() {
					if cookie.Name == oauthCookieNameForState(pending.State) && cookie.MaxAge < 0 {
						t.Fatal("invalid callback cleared the pending OAuth state cookie")
					}
				}
			}
		})
	}
}

func TestCrossSiteLoginCannotReplacePendingOAuthState(t *testing.T) {
	auth := newAuthManager(testAuthConfig(), nil)
	router := newRouterWithAuth(&githubClient{baseURL: "https://api.github.com", http: http.DefaultClient}, nil, newRateLimiter(10, time.Minute, time.Now), clientIdentity{}, nil, auth)
	start := httptest.NewRequest(http.MethodPost, "/auth/login", nil)
	start.Header.Set("Origin", "https://ghostline.example")
	started := httptest.NewRecorder()
	router.ServeHTTP(started, start)
	if started.Code != http.StatusFound {
		t.Fatalf("same-origin login status = %d, want %d", started.Code, http.StatusFound)
	}
	stateCookie := started.Result().Cookies()[0]

	attack := httptest.NewRequest(http.MethodPost, "/auth/login", nil)
	attack.Header.Set("Origin", "https://attacker.example")
	attack.AddCookie(stateCookie)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, attack)
	if response.Code != http.StatusForbidden {
		t.Fatalf("cross-site login status = %d, want %d", response.Code, http.StatusForbidden)
	}
	for _, cookie := range response.Result().Cookies() {
		if strings.HasPrefix(cookie.Name, oauthCookiePrefix) {
			t.Fatalf("cross-site login changed pending OAuth cookie: %+v", cookie)
		}
	}
}

func TestAnonymousProtectedRequestsConsumeRateLimitBudget(t *testing.T) {
	auth := newAuthManager(testAuthConfig(), nil)
	router := newRouterWithAuth(&githubClient{baseURL: "https://api.github.com", http: http.DefaultClient}, nil, newRateLimiter(1, time.Minute, time.Now), clientIdentity{}, nil, auth)
	request := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/import", strings.NewReader(`{"url":"invalid"}`))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "203.0.113.44:40000"
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		return response
	}
	if response := request(); response.Code != http.StatusUnauthorized {
		t.Fatalf("first anonymous request status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
	if response := request(); response.Code != http.StatusTooManyRequests {
		t.Fatalf("second anonymous request status = %d, want %d", response.Code, http.StatusTooManyRequests)
	}
}

func TestLogoutRequiresTheHostedOrigin(t *testing.T) {
	auth := newAuthManager(testAuthConfig(), nil)
	router := newRouterWithAuth(&githubClient{baseURL: "https://api.github.com", http: http.DefaultClient}, nil, newRateLimiter(20, time.Minute, time.Now), clientIdentity{}, nil, auth)
	for _, tc := range []struct {
		origin string
		want   int
	}{{origin: "https://ghostline.example", want: http.StatusNoContent}, {origin: "https://attacker.example", want: http.StatusForbidden}, {origin: "null", want: http.StatusForbidden}} {
		request := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
		request.Header.Set("Origin", tc.origin)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != tc.want {
			t.Fatalf("logout origin %q: status=%d, want %d", tc.origin, response.Code, tc.want)
		}
	}
}

func TestLogoutExpiresSessionAndPendingOAuthState(t *testing.T) {
	auth := newAuthManager(testAuthConfig(), nil)
	router := newRouterWithAuth(&githubClient{baseURL: "https://api.github.com", http: http.DefaultClient}, nil, newRateLimiter(20, time.Minute, time.Now), clientIdentity{}, nil, auth)
	request := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	request.Header.Set("Origin", "https://ghostline.example")
	state, err := randomToken(auth.random, 32)
	if err != nil {
		t.Fatal(err)
	}
	stateValue, err := auth.sign("oauth-state", oauthState{State: state, Verifier: "verifier", ExpiresAt: time.Now().Add(time.Minute).Unix()})
	if err != nil {
		t.Fatal(err)
	}
	auth.registerOAuthState(state, time.Now().Add(time.Minute))
	request.AddCookie(&http.Cookie{Name: oauthCookieNameForState(state), Value: stateValue})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("logout status = %d, want %d", response.Code, http.StatusNoContent)
	}
	expired := map[string]bool{}
	for _, cookie := range response.Result().Cookies() {
		if cookie.MaxAge < 0 {
			expired[cookie.Name] = true
		}
	}
	if !expired[sessionCookieName] || !expired[oauthCookieNameForState(state)] {
		t.Fatalf("logout did not expire both session and pending OAuth cookies: %+v", response.Result().Cookies())
	}
}

func TestLogoutRevokesCopiedSignedSession(t *testing.T) {
	auth := newAuthManager(testAuthConfig(), nil)
	router := newRouterWithAuth(&githubClient{baseURL: "https://api.github.com", http: http.DefaultClient}, nil, newRateLimiter(20, time.Minute, time.Now), clientIdentity{}, nil, auth)
	session := signedInSessionCookie(t, auth, testIdentity())
	logout := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	logout.Header.Set("Origin", "https://ghostline.example")
	logout.AddCookie(session)
	logoutResponse := httptest.NewRecorder()
	router.ServeHTTP(logoutResponse, logout)
	if logoutResponse.Code != http.StatusNoContent {
		t.Fatalf("logout status=%d, want 204", logoutResponse.Code)
	}
	protected := httptest.NewRequest(http.MethodPost, "/api/import", strings.NewReader(`{"url":"invalid"}`))
	protected.Header.Set("Content-Type", "application/json")
	protected.AddCookie(session)
	protectedResponse := httptest.NewRecorder()
	router.ServeHTTP(protectedResponse, protected)
	if protectedResponse.Code != http.StatusUnauthorized {
		t.Fatalf("copied signed session status=%d, want 401; body=%s", protectedResponse.Code, protectedResponse.Body.String())
	}
}

func TestSameOriginNormalizesDefaultHTTPSPort(t *testing.T) {
	cfg := testAuthConfig()
	cfg.CallbackURL = "https://ghostline.example:443/auth/callback"
	manager := newAuthManager(cfg, nil)
	if !manager.sameOrigin("https://ghostline.example") {
		t.Fatal("browser origin without explicit default port should match configured callback")
	}
	if manager.sameOrigin("https://ghostline.example:444") {
		t.Fatal("non-default port should not match configured HTTPS origin")
	}
}

func TestOAuthCallbackIsRateLimitedPerClient(t *testing.T) {
	var tokenRequests int
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		tokenRequests++
		return oauthResponse(http.StatusBadRequest, `{"error":"bad_verification_code"}`), nil
	})}
	auth := newAuthManager(testAuthConfig(), client)
	limiter := newRateLimiter(1, time.Minute, time.Now)
	router := newRouterWithAuth(&githubClient{baseURL: "https://api.github.com", http: http.DefaultClient}, nil, limiter, clientIdentity{}, nil, auth)
	stateValue, err := randomToken(auth.random, 32)
	if err != nil {
		t.Fatal(err)
	}
	state, err := auth.sign("oauth-state", oauthState{State: stateValue, Verifier: "verifier", ExpiresAt: time.Now().Add(time.Minute).Unix()})
	if err != nil {
		t.Fatal(err)
	}
	auth.registerOAuthState(stateValue, time.Now().Add(time.Minute))
	request := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/auth/callback?state="+url.QueryEscape(stateValue)+"&code=one-time-code", nil)
		req.AddCookie(&http.Cookie{Name: oauthCookieNameForState(stateValue), Value: state})
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		return response
	}
	if response := request(); response.Code != http.StatusBadGateway {
		t.Fatalf("first callback status = %d, want %d", response.Code, http.StatusBadGateway)
	}
	response := request()
	if response.Code != http.StatusTooManyRequests || response.Header().Get("Retry-After") == "" {
		t.Fatalf("repeated callback status=%d retry-after=%q, want rate limit", response.Code, response.Header().Get("Retry-After"))
	}
	if tokenRequests != 1 {
		t.Fatalf("token endpoint requests = %d, want 1 after callback rate limit", tokenRequests)
	}
}

func TestLogoutCancelsOAuthCallbackAlreadyInFlight(t *testing.T) {
	tokenRequestStarted := make(chan struct{})
	allowTokenResponse := make(chan struct{})
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Host == "github.com" {
			close(tokenRequestStarted)
			<-allowTokenResponse
			return oauthResponse(http.StatusOK, `{"access_token":"temporary-token","token_type":"bearer","scope":"read:user"}`), nil
		}
		return oauthResponse(http.StatusOK, `{"id":42,"login":"Felix-Dev"}`), nil
	})}
	auth := newAuthManager(testAuthConfig(), client)
	router := newRouterWithAuth(&githubClient{baseURL: "https://api.github.com", http: http.DefaultClient}, nil, newRateLimiter(20, time.Minute, time.Now), clientIdentity{}, nil, auth)

	login := httptest.NewRecorder()
	loginRequest := httptest.NewRequest(http.MethodPost, "/auth/login", nil)
	loginRequest.Header.Set("Origin", "https://ghostline.example")
	router.ServeHTTP(login, loginRequest)
	if login.Code != http.StatusFound {
		t.Fatalf("login status=%d, want 302", login.Code)
	}
	stateCookie := login.Result().Cookies()[0]
	authorization, err := url.Parse(login.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	state := authorization.Query().Get("state")
	callback := httptest.NewRequest(http.MethodGet, "/auth/callback?state="+url.QueryEscape(state)+"&code=one-time-code", nil)
	callback.AddCookie(stateCookie)
	callbackResponse := httptest.NewRecorder()
	callbackDone := make(chan struct{})
	go func() {
		router.ServeHTTP(callbackResponse, callback)
		close(callbackDone)
	}()
	select {
	case <-tokenRequestStarted:
	case <-time.After(time.Second):
		t.Fatal("callback did not reach the token exchange")
	}

	logout := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	logout.Header.Set("Origin", "https://ghostline.example")
	logout.AddCookie(stateCookie)
	logoutResponse := httptest.NewRecorder()
	router.ServeHTTP(logoutResponse, logout)
	if logoutResponse.Code != http.StatusNoContent {
		t.Fatalf("logout status=%d, want 204", logoutResponse.Code)
	}
	close(allowTokenResponse)
	select {
	case <-callbackDone:
	case <-time.After(time.Second):
		t.Fatal("OAuth callback did not finish")
	}
	if callbackResponse.Code != http.StatusUnauthorized {
		t.Fatalf("callback status=%d, want cancellation 401; body=%s", callbackResponse.Code, callbackResponse.Body.String())
	}
	for _, cookie := range callbackResponse.Result().Cookies() {
		if cookie.Name == sessionCookieName && cookie.MaxAge >= 0 {
			t.Fatal("canceled callback issued a session cookie")
		}
	}
}

func TestOAuthCallbackLogsSafeFailureStage(t *testing.T) {
	var output bytes.Buffer
	auth := newAuthManager(testAuthConfig(), &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("upstream unavailable")
	})})
	auth.logger = slog.New(slog.NewTextHandler(&output, nil))
	stateValue, err := randomToken(auth.random, 32)
	if err != nil {
		t.Fatal(err)
	}
	state := oauthState{State: stateValue, Verifier: "verifier", ExpiresAt: time.Now().Add(time.Minute).Unix()}
	value, err := auth.sign("oauth-state", state)
	if err != nil {
		t.Fatal(err)
	}
	auth.registerOAuthState(state.State, time.Unix(state.ExpiresAt, 0))
	request := httptest.NewRequest(http.MethodGet, "/auth/callback?state="+url.QueryEscape(stateValue)+"&code=code-secret", nil)
	request.AddCookie(&http.Cookie{Name: oauthCookieNameForState(state.State), Value: value})
	response := httptest.NewRecorder()
	newRouterWithAuth(&githubClient{baseURL: "https://api.github.com", http: http.DefaultClient}, nil, newRateLimiter(10, time.Minute, time.Now), clientIdentity{}, auth.logger, auth).ServeHTTP(response, request)
	if response.Code != http.StatusBadGateway {
		t.Fatalf("callback status=%d, want 502", response.Code)
	}
	logged := output.String()
	if !strings.Contains(logged, "token_exchange") || !strings.Contains(logged, "upstream unavailable") {
		t.Fatalf("callback failure log omitted stage or cause: %s", logged)
	}
	for _, secret := range []string{"code-secret", stateValue, "verifier", "temporary-token"} {
		if strings.Contains(logged, secret) {
			t.Fatalf("callback log contains OAuth secret %q: %s", secret, logged)
		}
	}
}

func TestOAuthCallbackQueryIsNotWrittenToAccessLogs(t *testing.T) {
	previousWriter := gin.DefaultWriter
	previousMode := gin.Mode()
	var logs bytes.Buffer
	gin.DefaultWriter = &logs
	defer func() {
		gin.DefaultWriter = previousWriter
		gin.SetMode(previousMode)
	}()
	gin.SetMode(gin.TestMode)
	auth := newAuthManager(testAuthConfig(), nil)
	router := newRouterWithAuth(&githubClient{baseURL: "https://api.github.com", http: http.DefaultClient}, nil, newRateLimiter(10, time.Minute, time.Now), clientIdentity{}, nil, auth)
	request := httptest.NewRequest(http.MethodGet, "/auth/callback?code=one-time-secret&state=state-secret", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if strings.Contains(logs.String(), "one-time-secret") || strings.Contains(logs.String(), "state-secret") {
		t.Fatalf("OAuth callback query was written to access logs: %s", logs.String())
	}
}

func TestHealthProbeIsNotWrittenToAccessLogs(t *testing.T) {
	previousWriter := gin.DefaultWriter
	previousMode := gin.Mode()
	var logs bytes.Buffer
	gin.DefaultWriter = &logs
	defer func() {
		gin.DefaultWriter = previousWriter
		gin.SetMode(previousMode)
	}()
	gin.SetMode(gin.TestMode)

	router := newRouterWithAuth(&githubClient{baseURL: "https://api.github.com", http: http.DefaultClient}, nil, newRateLimiter(10, time.Minute, time.Now), clientIdentity{}, nil, newAuthManager(authConfig{}, nil))
	request := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("health status = %d, want 200", response.Code)
	}
	if strings.Contains(logs.String(), "/api/health") {
		t.Fatalf("health probe was written to access logs: %s", logs.String())
	}
}
