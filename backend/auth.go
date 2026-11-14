package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/felixgeelhaar/ghostline/backend/internal/domain"
	"github.com/gin-gonic/gin"
)

const (
	oauthStateCookieName = "ghostline_oauth_state"
	sessionCookieName    = "ghostline_session"
	sessionLifetime      = 8 * time.Hour
	oauthStateLifetime   = 5 * time.Minute
	githubAuthorizeURL   = "https://github.com/login/oauth/authorize"
	githubTokenURL       = "https://github.com/login/oauth/access_token"
	githubUserURL        = "https://api.github.com/user"
)

var githubLoginPattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,37}[A-Za-z0-9])?$`)

type authSettings struct {
	Enabled      bool
	ClientID     string
	ClientSecret string
	CallbackURL  string
	SigningKey   []byte
	AllowedUsers domain.GitHubAccountAllowlist
	CookieSecure bool
	Now          func() time.Time
	HTTPClient   *http.Client
	AuthorizeURL string
	TokenURL     string
	UserURL      string
}

type sessionClaims struct {
	Login     string `json:"login"`
	ExpiresAt int64  `json:"expires_at"`
}

type githubOAuthToken struct {
	AccessToken string `json:"access_token"`
	Error       string `json:"error"`
}

type githubOAuthUser struct {
	Login string `json:"login"`
}

func loadAuthConfig(getenv func(string) string) (authSettings, error) {
	mode := strings.ToLower(strings.TrimSpace(getenv("GHOSTLINE_AUTH_MODE")))
	environment := strings.ToLower(strings.TrimSpace(getenv("GHOSTLINE_ENV")))
	if environment == "production" && mode != "github" {
		return authSettings{}, errors.New("GHOSTLINE_ENV=production requires GHOSTLINE_AUTH_MODE=github")
	}
	if mode == "" || mode == "off" {
		return authSettings{Now: time.Now, HTTPClient: &http.Client{Timeout: 10 * time.Second}, AuthorizeURL: githubAuthorizeURL, TokenURL: githubTokenURL, UserURL: githubUserURL}, nil
	}
	if mode != "github" {
		return authSettings{}, fmt.Errorf("GHOSTLINE_AUTH_MODE must be empty, off, or github, got %q", mode)
	}

	settings := authSettings{
		Enabled:      true,
		ClientID:     strings.TrimSpace(getenv("GITHUB_OAUTH_CLIENT_ID")),
		ClientSecret: getenv("GITHUB_OAUTH_CLIENT_SECRET"),
		CallbackURL:  strings.TrimSpace(getenv("GITHUB_OAUTH_CALLBACK_URL")),
		SigningKey:   []byte(getenv("GHOSTLINE_SESSION_SIGNING_KEY")),
		AllowedUsers: make(domain.GitHubAccountAllowlist),
		CookieSecure: true,
		Now:          time.Now,
		HTTPClient:   &http.Client{Timeout: 10 * time.Second, CheckRedirect: rejectOAuthRedirect},
		AuthorizeURL: githubAuthorizeURL,
		TokenURL:     githubTokenURL,
		UserURL:      githubUserURL,
	}
	for _, required := range []struct{ name, value string }{
		{"GITHUB_OAUTH_CLIENT_ID", settings.ClientID},
		{"GITHUB_OAUTH_CLIENT_SECRET", settings.ClientSecret},
		{"GITHUB_OAUTH_CALLBACK_URL", settings.CallbackURL},
	} {
		if required.value == "" {
			return authSettings{}, fmt.Errorf("%s is required when GHOSTLINE_AUTH_MODE=github", required.name)
		}
	}
	if len(settings.SigningKey) < 32 {
		return authSettings{}, errors.New("GHOSTLINE_SESSION_SIGNING_KEY must contain at least 32 bytes when GitHub sign-in is enabled")
	}
	if err := validateOAuthCallbackURL(settings.CallbackURL); err != nil {
		return authSettings{}, err
	}
	secureCookieSetting := strings.ToLower(strings.TrimSpace(getenv("GHOSTLINE_SESSION_COOKIE_SECURE")))
	if secureCookieSetting != "" && secureCookieSetting != "true" && secureCookieSetting != "false" {
		return authSettings{}, errors.New("GHOSTLINE_SESSION_COOKIE_SECURE must be true or false")
	}
	if secureCookieSetting == "false" {
		callback, _ := url.Parse(settings.CallbackURL)
		if callback.Scheme != "http" || (callback.Hostname() != "localhost" && callback.Hostname() != "127.0.0.1") {
			return authSettings{}, errors.New("GitHub sign-in requires secure cookies; disable them only for an HTTP localhost callback")
		}
		settings.CookieSecure = false
	}
	for _, login := range strings.Split(getenv("GHOSTLINE_ALLOWED_GITHUB_USERS"), ",") {
		login = strings.TrimSpace(login)
		if login == "" {
			continue
		}
		if !validGitHubLogin(login) {
			return authSettings{}, fmt.Errorf("GHOSTLINE_ALLOWED_GITHUB_USERS contains invalid GitHub login %q", login)
		}
		settings.AllowedUsers[strings.ToLower(login)] = struct{}{}
	}
	if len(settings.AllowedUsers) == 0 {
		return authSettings{}, errors.New("GHOSTLINE_ALLOWED_GITHUB_USERS must list at least one GitHub login when GitHub sign-in is enabled")
	}
	return settings, nil
}

func validateOAuthCallbackURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "/auth/github/callback" {
		return errors.New("GITHUB_OAUTH_CALLBACK_URL must be an absolute URL ending in /auth/github/callback with no query or fragment")
	}
	if u.Scheme == "https" && u.Hostname() != "" {
		return nil
	}
	if u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1") {
		return nil
	}
	return errors.New("GITHUB_OAUTH_CALLBACK_URL must use HTTPS (HTTP is allowed only for localhost)")
}

func validGitHubLogin(login string) bool {
	return len(login) > 0 && len(login) <= 39 && githubLoginPattern.MatchString(login)
}

func newRouterWithAuth(gh *githubClient, limiter *rateLimiter, identity clientIdentity, settings authSettings) *gin.Engine {
	r := newGhostlineRouter()
	r.GET("/api/health", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })
	auth := githubAuth{settings: settings}
	r.GET("/api/auth/status", auth.status)
	authRateLimit := rateLimitMiddleware(limiter, identity)
	r.GET("/auth/github/login", authRateLimit, auth.login)
	r.GET("/auth/github/callback", authRateLimit, auth.callback)
	r.POST("/auth/logout", auth.logout)
	limited := r.Group("/api", auth.middleware(), authenticatedRateLimitMiddleware(limiter, identity))
	limited.POST("/import", api{gh: gh}.importPR)
	limited.POST("/checks", api{gh: gh}.loadRevisionChecks)
	limited.POST("/compare", api{gh: gh}.compareRevisions)
	return r
}

func newGhostlineRouter() *gin.Engine {
	r := gin.New()
	r.Use(gin.LoggerWithConfig(gin.LoggerConfig{SkipQueryString: true, Formatter: func(params gin.LogFormatterParams) string {
		return fmt.Sprintf("[GIN] %v | %3d | %13v | %15s | %-7s %s\n", params.TimeStamp.Format("2006/01/02 - 15:04:05"), params.StatusCode, params.Latency, params.ClientIP, params.Method, params.Path)
	}}))
	r.Use(gin.Recovery())
	return r
}

func rejectOAuthRedirect(_ *http.Request, _ []*http.Request) error {
	return http.ErrUseLastResponse
}

type githubAuth struct{ settings authSettings }

func (a githubAuth) status(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	login := a.currentLogin(c.Request)
	c.JSON(http.StatusOK, gin.H{"enabled": a.settings.Enabled, "login": nullableLogin(login)})
}

func nullableLogin(login string) any {
	if login == "" {
		return nil
	}
	return login
}

func (a githubAuth) middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !a.settings.Enabled {
			c.Next()
			return
		}
		login := a.currentLogin(c.Request)
		if login == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Sign in with GitHub to continue."})
			return
		}
		c.Set("authenticatedGitHubLogin", strings.ToLower(login))
		c.Next()
	}
}

func (a githubAuth) currentLogin(r *http.Request) string {
	if !a.settings.Enabled {
		return ""
	}
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return ""
	}
	login := verifySession(a.settings, cookie.Value)
	if !a.settings.AllowedUsers.Allows(login) {
		return ""
	}
	return login
}

func (a githubAuth) login(c *gin.Context) {
	if !a.settings.Enabled {
		c.JSON(http.StatusNotFound, gin.H{"error": "GitHub sign-in is not enabled."})
		return
	}
	stateBytes := make([]byte, 32)
	if _, err := rand.Read(stateBytes); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not start GitHub sign-in."})
		return
	}
	state := base64.RawURLEncoding.EncodeToString(stateBytes)
	setCookie(c, &http.Cookie{Name: oauthStateCookieName, Value: state, Path: "/auth/github/callback", MaxAge: int(oauthStateLifetime.Seconds()), HttpOnly: true, Secure: a.settings.CookieSecure, SameSite: http.SameSiteLaxMode})
	target, err := url.Parse(a.settings.AuthorizeURL)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "GitHub sign-in is not configured correctly."})
		return
	}
	query := target.Query()
	query.Set("client_id", a.settings.ClientID)
	query.Set("redirect_uri", a.settings.CallbackURL)
	query.Set("scope", "read:user")
	query.Set("state", state)
	target.RawQuery = query.Encode()
	c.Redirect(http.StatusFound, target.String())
}

func (a githubAuth) callback(c *gin.Context) {
	if !a.settings.Enabled {
		c.JSON(http.StatusNotFound, gin.H{"error": "GitHub sign-in is not enabled."})
		return
	}
	stateCookie, err := c.Request.Cookie(oauthStateCookieName)
	state := c.Query("state")
	if err != nil || state == "" || len(state) > 128 || subtle.ConstantTimeCompare([]byte(stateCookie.Value), []byte(state)) != 1 {
		clearCookie(c, oauthStateCookieName, "/auth/github/callback", a.settings.CookieSecure)
		c.JSON(http.StatusBadRequest, gin.H{"error": "GitHub sign-in state did not match. Start sign-in again."})
		return
	}
	clearCookie(c, oauthStateCookieName, "/auth/github/callback", a.settings.CookieSecure)
	if c.Query("error") != "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "GitHub sign-in was cancelled."})
		return
	}
	code := c.Query("code")
	if code == "" || len(code) > 2048 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "GitHub did not return a valid sign-in code."})
		return
	}
	token, err := a.exchangeCode(c.Request, code)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "Could not complete GitHub sign-in. Try again."})
		return
	}
	login, err := a.fetchGitHubLogin(c.Request, token)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "Could not verify the GitHub account. Try again."})
		return
	}
	if !a.settings.AllowedUsers.Allows(login) {
		clearCookie(c, sessionCookieName, "/", a.settings.CookieSecure)
		c.JSON(http.StatusForbidden, gin.H{"error": "This GitHub account is not on this Ghostline instance’s access list."})
		return
	}
	session, err := signSession(a.settings, login)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not create a Ghostline session."})
		return
	}
	setCookie(c, &http.Cookie{Name: sessionCookieName, Value: session, Path: "/", MaxAge: int(sessionLifetime.Seconds()), HttpOnly: true, Secure: a.settings.CookieSecure, SameSite: http.SameSiteLaxMode})
	c.Redirect(http.StatusSeeOther, "/")
}

func (a githubAuth) exchangeCode(r *http.Request, code string) (string, error) {
	form := url.Values{"client_id": {a.settings.ClientID}, "client_secret": {a.settings.ClientSecret}, "code": {code}, "redirect_uri": {a.settings.CallbackURL}}
	request, err := http.NewRequestWithContext(r.Context(), http.MethodPost, a.settings.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := a.httpClient().Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("GitHub token endpoint returned %d", response.StatusCode)
	}
	var token githubOAuthToken
	if err := json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&token); err != nil {
		return "", err
	}
	if token.Error != "" || token.AccessToken == "" {
		return "", errors.New("GitHub did not issue an access token")
	}
	return token.AccessToken, nil
}

func (a githubAuth) fetchGitHubLogin(r *http.Request, token string) (string, error) {
	request, err := http.NewRequestWithContext(r.Context(), http.MethodGet, a.settings.UserURL, nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	response, err := a.httpClient().Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("GitHub user endpoint returned %d", response.StatusCode)
	}
	var user githubOAuthUser
	if err := json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&user); err != nil {
		return "", err
	}
	if !validGitHubLogin(user.Login) {
		return "", errors.New("GitHub returned an invalid user login")
	}
	return user.Login, nil
}

func (a githubAuth) httpClient() *http.Client {
	if a.settings.HTTPClient != nil {
		return a.settings.HTTPClient
	}
	return &http.Client{Timeout: 10 * time.Second}
}

func (a githubAuth) logout(c *gin.Context) {
	clearCookie(c, sessionCookieName, "/", a.settings.CookieSecure)
	c.Redirect(http.StatusSeeOther, "/")
}

func signSession(settings authSettings, login string) (string, error) {
	now := time.Now
	if settings.Now != nil {
		now = settings.Now
	}
	encoded, err := json.Marshal(sessionClaims{Login: login, ExpiresAt: now().Add(sessionLifetime).Unix()})
	if err != nil {
		return "", err
	}
	payload := base64.RawURLEncoding.EncodeToString(encoded)
	mac := hmac.New(sha256.New, settings.SigningKey)
	_, _ = mac.Write([]byte(payload))
	signature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return payload + "." + signature, nil
}

func verifySession(settings authSettings, token string) string {
	if len(settings.SigningKey) < 32 || len(token) > 2048 {
		return ""
	}
	payload, signature, ok := strings.Cut(token, ".")
	if !ok || payload == "" || signature == "" || strings.Contains(signature, ".") {
		return ""
	}
	provided, err := base64.RawURLEncoding.DecodeString(signature)
	if err != nil {
		return ""
	}
	mac := hmac.New(sha256.New, settings.SigningKey)
	_, _ = mac.Write([]byte(payload))
	if !hmac.Equal(provided, mac.Sum(nil)) {
		return ""
	}
	decoded, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return ""
	}
	var claims sessionClaims
	if err := json.Unmarshal(decoded, &claims); err != nil || !validGitHubLogin(claims.Login) {
		return ""
	}
	now := time.Now
	if settings.Now != nil {
		now = settings.Now
	}
	if claims.ExpiresAt <= now().Unix() || claims.ExpiresAt > now().Add(sessionLifetime+time.Minute).Unix() {
		return ""
	}
	return claims.Login
}

func setCookie(c *gin.Context, cookie *http.Cookie) {
	http.SetCookie(c.Writer, cookie)
}

func clearCookie(c *gin.Context, name, path string, secure bool) {
	http.SetCookie(c.Writer, &http.Cookie{Name: name, Value: "", Path: path, MaxAge: -1, Expires: time.Unix(1, 0), HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode})
}

func authenticatedRateLimitMiddleware(limiter *rateLimiter, identity clientIdentity) gin.HandlerFunc {
	return func(c *gin.Context) {
		key := c.GetString("authenticatedGitHubLogin")
		if key == "" {
			key = clientKey(c.Request, identity)
		} else {
			key = "github:" + key
		}
		allowed, retryAfter := limiter.allow(key)
		if allowed {
			c.Next()
			return
		}
		seconds := int((retryAfter + time.Second - 1) / time.Second)
		if seconds < 1 {
			seconds = 1
		}
		c.Header("Retry-After", strconv.Itoa(seconds))
		c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"error": fmt.Sprintf("Too many requests from this client. Try again in %d seconds.", seconds)})
	}
}
