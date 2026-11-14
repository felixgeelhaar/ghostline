package main

import (
	"bytes"
	"io"
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
		{name: "hosted auth configured", env: map[string]string{
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
	if _, ok := manager.sessionFromCookie(cookie.Value); ok {
		t.Fatal("expired session was accepted")
	}
	cookie, err = manager.newSessionCookie(testIdentity(), manager.now().Add(timeHour))
	if err != nil {
		t.Fatal(err)
	}
	tampered := cookie.Value[:len(cookie.Value)-1] + "x"
	if _, ok := manager.sessionFromCookie(tampered); ok {
		t.Fatal("tampered session was accepted")
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

	session, err := auth.newSessionCookie(testIdentity(), time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
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

func TestGitHubOAuthCallbackIssuesAllowlistedSessionAndDiscardsToken(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		switch request.URL.String() {
		case "https://github.com/login/oauth/access_token":
			if err := request.ParseForm(); err != nil {
				t.Fatal(err)
			}
			if request.Form.Get("client_secret") != "client-secret" || request.Form.Get("code_verifier") == "" {
				t.Fatalf("token request omitted secret or PKCE verifier: %v", request.Form)
			}
			return oauthResponse(http.StatusOK, `{"access_token":"temporary-oauth-token","token_type":"bearer"}`), nil
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
	router := newRouterWithAuth(&githubClient{baseURL: "https://api.github.com", http: http.DefaultClient}, nil, newRateLimiter(20, time.Minute, time.Now), clientIdentity{}, nil, auth)

	login := httptest.NewRecorder()
	router.ServeHTTP(login, httptest.NewRequest(http.MethodGet, "/auth/login", nil))
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
	if completed.Code != http.StatusSeeOther || completed.Header().Get("Location") != "/" {
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

func TestGitHubOAuthRejectsStateMismatchAndNonAllowlistedUser(t *testing.T) {
	for _, tc := range []struct {
		name  string
		login string
		state string
		want  int
	}{
		{name: "state mismatch", login: "Felix-Dev", state: "wrong", want: http.StatusUnauthorized},
		{name: "user not allowlisted", login: "other-user", state: "matching", want: http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				if request.URL.Host == "github.com" {
					return oauthResponse(http.StatusOK, `{"access_token":"temporary-token","token_type":"bearer"}`), nil
				}
				return oauthResponse(http.StatusOK, `{"id":43,"login":"`+tc.login+`"}`), nil
			})}
			auth := newAuthManager(testAuthConfig(), client)
			router := newRouterWithAuth(&githubClient{baseURL: "https://api.github.com", http: http.DefaultClient}, nil, newRateLimiter(20, time.Minute, time.Now), clientIdentity{}, nil, auth)
			pending := oauthState{State: "matching", Verifier: "verifier", ExpiresAt: time.Now().Add(time.Minute).Unix()}
			value, err := auth.sign("oauth-state", pending)
			if err != nil {
				t.Fatal(err)
			}
			request := httptest.NewRequest(http.MethodGet, "/auth/callback?state="+tc.state+"&code=code", nil)
			request.AddCookie(&http.Cookie{Name: oauthCookieName, Value: value})
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != tc.want {
				t.Fatalf("callback status = %d, want %d; body=%s", response.Code, tc.want, response.Body.String())
			}
		})
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

func TestOAuthCallbackIsRateLimitedPerClient(t *testing.T) {
	var tokenRequests int
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		tokenRequests++
		return oauthResponse(http.StatusBadRequest, `{"error":"bad_verification_code"}`), nil
	})}
	auth := newAuthManager(testAuthConfig(), client)
	limiter := newRateLimiter(1, time.Minute, time.Now)
	router := newRouterWithAuth(&githubClient{baseURL: "https://api.github.com", http: http.DefaultClient}, nil, limiter, clientIdentity{}, nil, auth)
	state, err := auth.sign("oauth-state", oauthState{State: "expected", Verifier: "verifier", ExpiresAt: time.Now().Add(time.Minute).Unix()})
	if err != nil {
		t.Fatal(err)
	}
	request := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/auth/callback?state=expected&code=one-time-code", nil)
		req.AddCookie(&http.Cookie{Name: oauthCookieName, Value: state})
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
