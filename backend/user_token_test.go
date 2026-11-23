package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/felixgeelhaar/ghostline/backend/internal/domain"
)

func hostedAuthEnv(overrides map[string]string) map[string]string {
	env := map[string]string{
		"GHOSTLINE_AUTH_MODE": "required", "GITHUB_OAUTH_CLIENT_ID": "client-id",
		"GITHUB_OAUTH_CLIENT_SECRET": "client-secret", "GITHUB_OAUTH_CALLBACK_URL": "https://ghostline.example/auth/callback",
		"GHOSTLINE_SESSION_SECRET": strings.Repeat("s", 32),
	}
	for key, value := range overrides {
		env[key] = value
	}
	return env
}

func TestLoadAuthConfigOpenSignin(t *testing.T) {
	tests := []struct {
		name  string
		env   map[string]string
		valid bool
		open  bool
	}{
		{name: "open sign-in needs no allowlist", env: hostedAuthEnv(map[string]string{"GHOSTLINE_OPEN_SIGNIN": "true"}), valid: true, open: true},
		{name: "open sign-in keeps an allowlist for switching back", env: hostedAuthEnv(map[string]string{"GHOSTLINE_OPEN_SIGNIN": "true", "GHOSTLINE_ALLOWED_GITHUB_IDS": "42"}), valid: true, open: true},
		{name: "explicitly closed still needs an allowlist", env: hostedAuthEnv(map[string]string{"GHOSTLINE_OPEN_SIGNIN": "false"})},
		{name: "closed by default", env: hostedAuthEnv(map[string]string{"GHOSTLINE_ALLOWED_GITHUB_IDS": "42"}), valid: true},
		{name: "unknown flag value is rejected", env: hostedAuthEnv(map[string]string{"GHOSTLINE_OPEN_SIGNIN": "yes", "GHOSTLINE_ALLOWED_GITHUB_IDS": "42"})},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := loadAuthConfig(func(key string) string { return tc.env[key] })
			if (err == nil) != tc.valid {
				t.Fatalf("loadAuthConfig() error = %v, want valid %v", err, tc.valid)
			}
			if err == nil && cfg.OpenSignin != tc.open {
				t.Fatalf("OpenSignin = %v, want %v", cfg.OpenSignin, tc.open)
			}
		})
	}
}

func TestOpenSigninAcceptsAnyGitHubAccount(t *testing.T) {
	config := testAuthConfig()
	config.OpenSignin = true
	config.AllowedIDs = map[int64]struct{}{}
	auth := newAuthManager(config, nil)
	stranger, err := domain.NewGitHubIdentity(99, "someone-else")
	if err != nil {
		t.Fatal(err)
	}
	cookie, err := auth.newSessionCookie(stranger, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	router := newRouterWithAuth(&githubClient{baseURL: "https://api.github.com", http: http.DefaultClient}, nil, newRateLimiter(10, time.Minute, time.Now), clientIdentity{}, nil, auth)

	session := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/session", nil)
	request.AddCookie(cookie)
	router.ServeHTTP(session, request)
	if !strings.Contains(session.Body.String(), `"authenticated":true`) {
		t.Fatalf("open sign-in session = %s", session.Body.String())
	}

	imported := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/api/import", strings.NewReader(`{"url":"invalid"}`))
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(cookie)
	router.ServeHTTP(imported, request)
	if imported.Code != http.StatusBadRequest {
		t.Fatalf("open sign-in API status = %d, want request validation 400; body=%s", imported.Code, imported.Body.String())
	}
}

// signInThroughOAuth runs the login and callback against a fake GitHub and returns
// the Ghostline session cookie.
func signInThroughOAuth(t *testing.T, router http.Handler) *http.Cookie {
	t.Helper()
	login := httptest.NewRecorder()
	loginRequest := httptest.NewRequest(http.MethodPost, "/auth/login", nil)
	loginRequest.Header.Set("Origin", "https://ghostline.example")
	router.ServeHTTP(login, loginRequest)
	if login.Code != http.StatusFound {
		t.Fatalf("login status = %d", login.Code)
	}
	authorization, err := url.Parse(login.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	callback := httptest.NewRequest(http.MethodGet, "/auth/callback?state="+url.QueryEscape(authorization.Query().Get("state"))+"&code=one-time-code", nil)
	callback.AddCookie(login.Result().Cookies()[0])
	completed := httptest.NewRecorder()
	router.ServeHTTP(completed, callback)
	if completed.Code != http.StatusSeeOther {
		t.Fatalf("callback status = %d body=%s", completed.Code, completed.Body.String())
	}
	for _, cookie := range completed.Result().Cookies() {
		if cookie.Name == sessionCookieName {
			return cookie
		}
	}
	t.Fatal("callback set no session cookie")
	return nil
}

func oauthClientGranting(t *testing.T, tokenResponse string) *http.Client {
	return &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		switch request.URL.String() {
		case "https://github.com/login/oauth/access_token":
			return oauthResponse(http.StatusOK, tokenResponse), nil
		case "https://api.github.com/user":
			return oauthResponse(http.StatusOK, `{"id":42,"login":"Felix-Dev"}`), nil
		default:
			t.Fatalf("unexpected OAuth destination %s", request.URL)
			return nil, nil
		}
	})}
}

// recordingGitHub answers every GitHub API call with 404 and records the
// Authorization header each call carried.
type recordingGitHub struct {
	mu             sync.Mutex
	authorizations []string
	respond        func(*http.Request) *http.Response
}

func (r *recordingGitHub) client() *githubClient {
	return &githubClient{baseURL: "https://api.github.test", http: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		r.mu.Lock()
		r.authorizations = append(r.authorizations, request.Header.Get("Authorization"))
		r.mu.Unlock()
		if r.respond != nil {
			return r.respond(request), nil
		}
		return oauthResponse(http.StatusNotFound, `{"message":"Not Found"}`), nil
	})}}
}

func (r *recordingGitHub) seen() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.authorizations...)
}

func importWithSession(router http.Handler, session *http.Cookie, remoteAddr string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "/api/import", strings.NewReader(`{"url":"https://github.com/acme/widgets/pull/7"}`))
	request.Header.Set("Content-Type", "application/json")
	if remoteAddr != "" {
		request.RemoteAddr = remoteAddr
	}
	if session != nil {
		request.AddCookie(session)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func TestImportUsesTheSignedInUsersGitHubToken(t *testing.T) {
	github := &recordingGitHub{}
	auth := newAuthManager(testAuthConfig(), oauthClientGranting(t, `{"access_token":"user-oauth-token","token_type":"bearer","scope":"read:user"}`))
	router := newRouterWithAuth(github.client(), nil, newRateLimiter(10, time.Minute, time.Now), clientIdentity{}, nil, auth)

	session := signInThroughOAuth(t, router)
	if strings.Contains(session.Value, "user-oauth-token") {
		t.Fatal("GitHub access token was written into the session cookie")
	}
	response := importWithSession(router, session, "")
	if response.Code != http.StatusNotFound {
		t.Fatalf("import status = %d body=%s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "user-oauth-token") {
		t.Fatal("GitHub access token reached the browser")
	}
	seen := github.seen()
	if len(seen) == 0 || seen[0] != "Bearer user-oauth-token" {
		t.Fatalf("GitHub data request authorization = %q, want the user's token", seen)
	}
}

func TestDataRequestsStayAnonymousWithoutAUserToken(t *testing.T) {
	github := &recordingGitHub{}
	router := newRouterWithAuth(github.client(), nil, newRateLimiter(10, time.Minute, time.Now), clientIdentity{}, nil, nil)
	importWithSession(router, nil, "")
	seen := github.seen()
	if len(seen) == 0 || seen[0] != "" {
		t.Fatalf("GitHub data request authorization = %q, want none", seen)
	}
}

func TestTokenWithBroaderScopeIsNotKept(t *testing.T) {
	for _, scope := range []string{"repo,read:user", "read:user, read:org", "public_repo"} {
		t.Run(scope, func(t *testing.T) {
			github := &recordingGitHub{}
			auth := newAuthManager(testAuthConfig(), oauthClientGranting(t, `{"access_token":"broad-token","token_type":"bearer","scope":"`+scope+`"}`))
			router := newRouterWithAuth(github.client(), nil, newRateLimiter(10, time.Minute, time.Now), clientIdentity{}, nil, auth)
			session := signInThroughOAuth(t, router)
			importWithSession(router, session, "")
			for _, authorization := range github.seen() {
				if authorization != "" {
					t.Fatalf("a token granted %q was used for GitHub data: %q", scope, authorization)
				}
			}
		})
	}
}

func TestGitHubReportingBroaderScopesForAUserTokenStopsTheRequest(t *testing.T) {
	github := &recordingGitHub{respond: func(*http.Request) *http.Response {
		response := oauthResponse(http.StatusOK, `{"number":7}`)
		response.Header.Set("X-OAuth-Scopes", "repo, read:user")
		return response
	}}
	auth := newAuthManager(testAuthConfig(), oauthClientGranting(t, `{"access_token":"user-oauth-token","token_type":"bearer","scope":"read:user"}`))
	router := newRouterWithAuth(github.client(), nil, newRateLimiter(10, time.Minute, time.Now), clientIdentity{}, nil, auth)
	session := signInThroughOAuth(t, router)
	response := importWithSession(router, session, "")
	if response.Code != http.StatusBadGateway || !strings.Contains(response.Body.String(), "Sign out and sign in again") {
		t.Fatalf("broader-scope response = %d %s", response.Code, response.Body.String())
	}
	if len(github.seen()) != 1 {
		t.Fatalf("GitHub calls after the scope mismatch = %d, want 1", len(github.seen()))
	}
}

func TestSignOutAndExpiryDropTheStoredUserToken(t *testing.T) {
	now := time.Now()
	auth := newAuthManager(testAuthConfig(), oauthClientGranting(t, `{"access_token":"user-oauth-token","token_type":"bearer","scope":"read:user"}`))
	auth.now = func() time.Time { return now }
	router := newRouterWithAuth((&recordingGitHub{}).client(), nil, newRateLimiter(10, time.Minute, time.Now), clientIdentity{}, nil, auth)

	session := signInThroughOAuth(t, router)
	if auth.storedUserTokens() != 1 {
		t.Fatalf("stored tokens after sign-in = %d, want 1", auth.storedUserTokens())
	}
	logout := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	logout.Header.Set("Origin", "https://ghostline.example")
	logout.AddCookie(session)
	router.ServeHTTP(httptest.NewRecorder(), logout)
	if auth.storedUserTokens() != 0 {
		t.Fatalf("stored tokens after sign-out = %d, want 0", auth.storedUserTokens())
	}

	signInThroughOAuth(t, router)
	now = now.Add(defaultSessionLifetime + time.Second)
	if token := auth.userTokenFor("any"); token != "" {
		t.Fatal("unknown session returned a token")
	}
	if auth.storedUserTokens() != 0 {
		t.Fatalf("stored tokens after expiry = %d, want 0", auth.storedUserTokens())
	}
}

func TestAPIRequestsAreLimitedPerGitHubAccount(t *testing.T) {
	auth := newAuthManager(testAuthConfig(), nil)
	cookie, err := auth.newSessionCookie(testIdentity(), time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	router := newRouterWithAuth((&recordingGitHub{}).client(), nil, newRateLimiter(1, time.Minute, time.Now), clientIdentity{}, nil, auth)
	if first := importWithSession(router, cookie, "192.0.2.1:1000"); first.Code == http.StatusTooManyRequests {
		t.Fatalf("first request was limited: %s", first.Body.String())
	}
	second := importWithSession(router, cookie, "198.51.100.7:1000")
	if second.Code != http.StatusTooManyRequests || second.Header().Get("Retry-After") == "" || !strings.Contains(second.Body.String(), "this GitHub account") {
		t.Fatalf("second account request from a new address = %d %s", second.Code, second.Body.String())
	}
}

func TestUserTokenRateLimitNamesGitHubsResetTime(t *testing.T) {
	reset := time.Now().Add(10 * time.Minute).Truncate(time.Second)
	github := &recordingGitHub{respond: func(*http.Request) *http.Response {
		response := oauthResponse(http.StatusForbidden, `{"message":"API rate limit exceeded"}`)
		response.Header.Set("X-RateLimit-Remaining", "0")
		response.Header.Set("X-RateLimit-Reset", strconv.FormatInt(reset.Unix(), 10))
		return response
	}}
	auth := newAuthManager(testAuthConfig(), oauthClientGranting(t, `{"access_token":"user-oauth-token","token_type":"bearer","scope":"read:user"}`))
	router := newRouterWithAuth(github.client(), nil, newRateLimiter(10, time.Minute, time.Now), clientIdentity{}, nil, auth)
	session := signInThroughOAuth(t, router)
	response := importWithSession(router, session, "")
	want := reset.UTC().Format("15:04 UTC")
	if response.Code != http.StatusTooManyRequests || !strings.Contains(response.Body.String(), "your GitHub account") || !strings.Contains(response.Body.String(), want) {
		t.Fatalf("rate-limited response = %d %s, want reset %s", response.Code, response.Body.String(), want)
	}
	if response.Header().Get("Retry-After") == "" {
		t.Fatal("rate-limited response has no Retry-After")
	}
	if len(github.seen()) != 1 {
		t.Fatalf("GitHub calls = %d, want 1 (no retry with another token)", len(github.seen()))
	}
}
