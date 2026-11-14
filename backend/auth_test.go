package main

import (
	"bytes"
	"encoding/json"
	"errors"
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

func TestLoadAuthConfigRequiresCompleteHostedCredentials(t *testing.T) {
	_, productionErr := loadAuthConfig(func(key string) string {
		if key == "GHOSTLINE_ENV" {
			return "production"
		}
		return ""
	})
	if productionErr == nil || !strings.Contains(productionErr.Error(), "requires GHOSTLINE_AUTH_MODE=github") {
		t.Fatalf("production error = %v, want authentication-required startup error", productionErr)
	}

	_, err := loadAuthConfig(func(key string) string {
		if key == "GHOSTLINE_AUTH_MODE" {
			return "github"
		}
		return ""
	})
	if err == nil || !strings.Contains(err.Error(), "GITHUB_OAUTH_CLIENT_ID") {
		t.Fatalf("error = %v, want missing OAuth configuration", err)
	}
}

func TestLoadAuthConfigRejectsInvalidCookieSecuritySetting(t *testing.T) {
	values := validGitHubAuthEnvironment()
	values["GHOSTLINE_SESSION_COOKIE_SECURE"] = "sometimes"
	_, err := loadAuthConfig(func(key string) string { return values[key] })
	if err == nil || !strings.Contains(err.Error(), "must be true or false") {
		t.Fatalf("error = %v, want invalid cookie-security setting", err)
	}
}

func TestLoadAuthConfigRequiresSecureHostedCookiesAndAllowedUsers(t *testing.T) {
	values := map[string]string{
		"GHOSTLINE_AUTH_MODE":             "github",
		"GITHUB_OAUTH_CLIENT_ID":          "client-id",
		"GITHUB_OAUTH_CLIENT_SECRET":      "client-secret",
		"GITHUB_OAUTH_CALLBACK_URL":       "https://ghostline.example/auth/github/callback",
		"GHOSTLINE_SESSION_SIGNING_KEY":   strings.Repeat("k", 32),
		"GHOSTLINE_ALLOWED_GITHUB_USERS":  "FelixGeelhaar",
		"GHOSTLINE_SESSION_COOKIE_SECURE": "false",
	}
	_, err := loadAuthConfig(func(key string) string { return values[key] })
	if err == nil || !strings.Contains(err.Error(), "secure cookies") {
		t.Fatalf("error = %v, want secure-cookie configuration error", err)
	}
	delete(values, "GHOSTLINE_SESSION_COOKIE_SECURE")
	delete(values, "GHOSTLINE_ALLOWED_GITHUB_USERS")
	_, err = loadAuthConfig(func(key string) string { return values[key] })
	if err == nil || !strings.Contains(err.Error(), "GHOSTLINE_ALLOWED_GITHUB_USERS") {
		t.Fatalf("error = %v, want allowlist configuration error", err)
	}
}

func TestAuthStatusAndProtectedAPI(t *testing.T) {
	settings := testAuthSettings()
	router := newRouterWithAuth(&githubClient{}, newRateLimiter(30, time.Minute, time.Now), clientIdentity{}, settings)
	status := httptest.NewRecorder()
	router.ServeHTTP(status, httptest.NewRequest(http.MethodGet, "/api/auth/status", nil))
	if status.Code != http.StatusOK {
		t.Fatalf("auth status = %d, body = %s", status.Code, status.Body.String())
	}
	var response struct {
		Enabled bool    `json:"enabled"`
		Login   *string `json:"login"`
	}
	if err := json.Unmarshal(status.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !response.Enabled || response.Login != nil {
		t.Fatalf("auth status = %+v, want enabled without a session", response)
	}

	protected := httptest.NewRecorder()
	router.ServeHTTP(protected, httptest.NewRequest(http.MethodPost, "/api/import", strings.NewReader(`{"url":"https://github.com/acme/repo/pull/1"}`)))
	if protected.Code != http.StatusUnauthorized {
		t.Fatalf("protected API status = %d, want 401", protected.Code)
	}
	comparison := httptest.NewRecorder()
	comparisonRequest := httptest.NewRequest(http.MethodPost, "/api/compare", strings.NewReader(`{"url":"https://github.com/acme/repo/pull/1","baseSha":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","headSha":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}`))
	router.ServeHTTP(comparison, comparisonRequest)
	if comparison.Code != http.StatusUnauthorized {
		t.Fatalf("protected comparison status = %d, want 401", comparison.Code)
	}
}

func TestGitHubOAuthCallbackCreatesAllowlistedSession(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/login/oauth/access_token":
			body, err := io.ReadAll(r.Body)
			form, parseErr := url.ParseQuery(string(body))
			if err != nil || parseErr != nil || form.Get("code") != "oauth-code" || form.Get("client_id") != "client-id" {
				t.Errorf("token request form = %v, read error = %v, parse error = %v", form, err, parseErr)
			}
			return oauthResponse(r, `{"access_token":"ephemeral-token","token_type":"bearer"}`), nil
		case "/user":
			if r.Header.Get("Authorization") != "Bearer ephemeral-token" {
				t.Errorf("authorization = %q", r.Header.Get("Authorization"))
			}
			return oauthResponse(r, `{"login":"FelixGeelhaar"}`), nil
		default:
			return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("not found")), Header: make(http.Header), Request: r}, nil
		}
	})}

	settings := testAuthSettings()
	settings.CallbackURL = "https://ghostline.example/auth/github/callback"
	settings.AuthorizeURL = "https://provider.invalid/login/oauth/authorize"
	settings.TokenURL = "https://provider.invalid/login/oauth/access_token"
	settings.UserURL = "https://provider.invalid/user"
	settings.HTTPClient = client
	router := newRouterWithAuth(&githubClient{}, newRateLimiter(30, time.Minute, time.Now), clientIdentity{}, settings)

	login := httptest.NewRecorder()
	router.ServeHTTP(login, httptest.NewRequest(http.MethodGet, "/auth/github/login", nil))
	if login.Code != http.StatusFound {
		t.Fatalf("login status = %d, body = %s", login.Code, login.Body.String())
	}
	stateCookie := cookieNamed(t, login.Result().Cookies(), oauthStateCookieName)
	authorizeURL, err := url.Parse(login.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	state := authorizeURL.Query().Get("state")
	if state == "" || state != stateCookie.Value {
		t.Fatalf("authorization state %q does not match state cookie %q", state, stateCookie.Value)
	}

	callback := httptest.NewRequest(http.MethodGet, "/auth/github/callback?code=oauth-code&state="+url.QueryEscape(state), nil)
	callback.AddCookie(stateCookie)
	result := httptest.NewRecorder()
	router.ServeHTTP(result, callback)
	if result.Code != http.StatusSeeOther || result.Header().Get("Location") != "/" {
		t.Fatalf("callback = %d %q, body = %s", result.Code, result.Header().Get("Location"), result.Body.String())
	}
	sessionCookie := cookieNamed(t, result.Result().Cookies(), sessionCookieName)
	if sessionCookie.Value == "" || !sessionCookie.HttpOnly || !sessionCookie.Secure {
		t.Fatalf("session cookie is missing or insecure: %+v", sessionCookie)
	}

	status := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/auth/status", nil)
	request.AddCookie(sessionCookie)
	router.ServeHTTP(status, request)
	var authStatus struct {
		Enabled bool   `json:"enabled"`
		Login   string `json:"login"`
	}
	if err := json.Unmarshal(status.Body.Bytes(), &authStatus); err != nil {
		t.Fatal(err)
	}
	if status.Code != http.StatusOK || authStatus.Login != "FelixGeelhaar" {
		t.Fatalf("status = %d, %+v", status.Code, authStatus)
	}
}

func TestOAuthHTTPClientRejectsRedirects(t *testing.T) {
	client := &http.Client{CheckRedirect: rejectOAuthRedirect}
	err := client.CheckRedirect(httptest.NewRequest(http.MethodGet, "https://attacker.example/collect", nil), nil)
	if !errors.Is(err, http.ErrUseLastResponse) {
		t.Fatalf("redirect result = %v, want redirect rejected", err)
	}
}

func TestRouterLogsOAuthCallbackPathWithoutQuerySecrets(t *testing.T) {
	previousWriter := gin.DefaultWriter
	var logs bytes.Buffer
	gin.DefaultWriter = &logs
	defer func() { gin.DefaultWriter = previousWriter }()

	router := newGhostlineRouter()
	router.GET("/auth/github/callback", func(c *gin.Context) { c.Status(http.StatusOK) })
	request := httptest.NewRequest(http.MethodGet, "/auth/github/callback?code=oauth-secret&state=state-secret", nil)
	router.ServeHTTP(httptest.NewRecorder(), request)

	if strings.Contains(logs.String(), "oauth-secret") || strings.Contains(logs.String(), "state-secret") {
		t.Fatalf("router log leaked OAuth query secrets: %q", logs.String())
	}
}

func TestGitHubOAuthRejectsWrongStateAndNonAllowlistedUser(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/token" {
			return oauthResponse(r, `{"access_token":"ephemeral-token"}`), nil
		}
		return oauthResponse(r, `{"login":"someone-else"}`), nil
	})}
	settings := testAuthSettings()
	settings.AuthorizeURL = "https://provider.invalid/authorize"
	settings.TokenURL = "https://provider.invalid/token"
	settings.UserURL = "https://provider.invalid/user"
	settings.HTTPClient = client
	router := newRouterWithAuth(&githubClient{}, newRateLimiter(30, time.Minute, time.Now), clientIdentity{}, settings)

	badState := httptest.NewRequest(http.MethodGet, "/auth/github/callback?code=oauth-code&state=attacker", nil)
	badState.AddCookie(&http.Cookie{Name: oauthStateCookieName, Value: "victim"})
	badStateResult := httptest.NewRecorder()
	router.ServeHTTP(badStateResult, badState)
	if badStateResult.Code != http.StatusBadRequest {
		t.Fatalf("wrong-state callback status = %d, want 400", badStateResult.Code)
	}

	callback := httptest.NewRequest(http.MethodGet, "/auth/github/callback?code=oauth-code&state=valid-state", nil)
	callback.AddCookie(&http.Cookie{Name: oauthStateCookieName, Value: "valid-state"})
	result := httptest.NewRecorder()
	router.ServeHTTP(result, callback)
	if result.Code != http.StatusForbidden {
		t.Fatalf("non-allowlisted callback status = %d, want 403", result.Code)
	}
	for _, cookie := range result.Result().Cookies() {
		if cookie.Name == sessionCookieName && cookie.MaxAge > 0 {
			t.Fatalf("non-allowlisted user received a live session cookie: %+v", cookie)
		}
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

func oauthResponse(request *http.Request, body string) *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{"Content-Type": []string{"application/json"}}, Request: request}
}

func TestSessionSignatureAndExpiry(t *testing.T) {
	settings := testAuthSettings()
	now := time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)
	settings.Now = func() time.Time { return now }
	token, err := signSession(settings, "FelixGeelhaar")
	if err != nil {
		t.Fatal(err)
	}
	if user := verifySession(settings, token); user != "FelixGeelhaar" {
		t.Fatalf("verified login = %q", user)
	}
	if user := verifySession(settings, token+"x"); user != "" {
		t.Fatalf("tampered session accepted for %q", user)
	}
	now = now.Add(sessionLifetime + time.Second)
	if user := verifySession(settings, token); user != "" {
		t.Fatalf("expired session accepted for %q", user)
	}
}

func testAuthSettings() authSettings {
	return authSettings{
		Enabled:      true,
		ClientID:     "client-id",
		ClientSecret: "client-secret",
		CallbackURL:  "http://localhost/auth/github/callback",
		SigningKey:   []byte(strings.Repeat("k", 32)),
		AllowedUsers: domain.NewGitHubAccountAllowlist([]string{"felixgeelhaar"}),
		CookieSecure: true,
		Now:          time.Now,
		HTTPClient:   &http.Client{Timeout: 10 * time.Second, CheckRedirect: rejectOAuthRedirect},
		AuthorizeURL: "https://github.com/login/oauth/authorize",
		TokenURL:     "https://github.com/login/oauth/access_token",
		UserURL:      "https://api.github.com/user",
	}
}

func validGitHubAuthEnvironment() map[string]string {
	return map[string]string{
		"GHOSTLINE_AUTH_MODE":            "github",
		"GITHUB_OAUTH_CLIENT_ID":         "client-id",
		"GITHUB_OAUTH_CLIENT_SECRET":     "client-secret",
		"GITHUB_OAUTH_CALLBACK_URL":      "https://ghostline.example/auth/github/callback",
		"GHOSTLINE_SESSION_SIGNING_KEY":  strings.Repeat("k", 32),
		"GHOSTLINE_ALLOWED_GITHUB_USERS": "FelixGeelhaar",
	}
}

func cookieNamed(t *testing.T, cookies []*http.Cookie, name string) *http.Cookie {
	t.Helper()
	for _, cookie := range cookies {
		if cookie.Name == name {
			return cookie
		}
	}
	t.Fatalf("cookie %q not found", name)
	return nil
}
