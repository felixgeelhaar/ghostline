package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/felixgeelhaar/ghostline/backend/internal/domain"
	"github.com/gin-gonic/gin"
)

const (
	defaultSessionLifetime = 12 * time.Hour
	oauthStateLifetime     = 10 * time.Minute
	maxOAuthResponseBytes  = 1 << 20
	maxOAuthCodeBytes      = 256
)

type authConfig struct {
	Required     bool
	ClientID     string
	ClientSecret string
	CallbackURL  string
	SessionKey   []byte
	AllowedIDs   map[int64]struct{}
}

func loadAuthConfig(getenv func(string) string) (authConfig, error) {
	mode := strings.ToLower(strings.TrimSpace(getenv("GHOSTLINE_AUTH_MODE")))
	if mode == "" || mode == "disabled" {
		if strings.EqualFold(strings.TrimSpace(getenv("GHOSTLINE_ENV")), "production") {
			return authConfig{}, errors.New("GHOSTLINE_AUTH_MODE=required when GHOSTLINE_ENV=production")
		}
		return authConfig{}, nil
	}
	if mode != "required" {
		return authConfig{}, fmt.Errorf("GHOSTLINE_AUTH_MODE must be 'disabled' or 'required', got %q", mode)
	}
	cfg := authConfig{Required: true}
	cfg.ClientID = strings.TrimSpace(getenv("GITHUB_OAUTH_CLIENT_ID"))
	cfg.ClientSecret = strings.TrimSpace(getenv("GITHUB_OAUTH_CLIENT_SECRET"))
	cfg.CallbackURL = strings.TrimSpace(getenv("GITHUB_OAUTH_CALLBACK_URL"))
	cfg.SessionKey = []byte(getenv("GHOSTLINE_SESSION_SECRET"))
	if cfg.ClientID == "" || cfg.ClientSecret == "" {
		return authConfig{}, errors.New("GITHUB_OAUTH_CLIENT_ID and GITHUB_OAUTH_CLIENT_SECRET are required when GitHub sign-in is required")
	}
	callback, err := url.Parse(cfg.CallbackURL)
	if err != nil || callback.Scheme != "https" || callback.Host == "" || callback.User != nil || callback.RawQuery != "" || callback.Fragment != "" || callback.Path != "/auth/callback" {
		return authConfig{}, errors.New("GITHUB_OAUTH_CALLBACK_URL must be an HTTPS URL ending in /auth/callback without query or fragment")
	}
	if len(cfg.SessionKey) < 32 {
		return authConfig{}, errors.New("GHOSTLINE_SESSION_SECRET must contain at least 32 bytes")
	}
	cfg.AllowedIDs = make(map[int64]struct{})
	for _, rawID := range strings.Split(getenv("GHOSTLINE_ALLOWED_GITHUB_IDS"), ",") {
		rawID = strings.TrimSpace(rawID)
		if rawID == "" {
			continue
		}
		id, err := strconv.ParseInt(rawID, 10, 64)
		if err != nil || id <= 0 {
			return authConfig{}, fmt.Errorf("GHOSTLINE_ALLOWED_GITHUB_IDS contains invalid GitHub user ID %q", rawID)
		}
		cfg.AllowedIDs[id] = struct{}{}
	}
	if len(cfg.AllowedIDs) == 0 {
		return authConfig{}, errors.New("GHOSTLINE_ALLOWED_GITHUB_IDS must include at least one numeric GitHub user ID when sign-in is required")
	}
	return cfg, nil
}

type authSession struct {
	ID        string                `json:"id"`
	Identity  domain.GitHubIdentity `json:"identity"`
	ExpiresAt int64                 `json:"expires_at"`
}

type oauthState struct {
	State     string `json:"state"`
	Verifier  string `json:"verifier"`
	ExpiresAt int64  `json:"expires_at"`
}

type authManager struct {
	cfg          authConfig
	client       *http.Client
	now          func() time.Time
	random       io.Reader
	logger       *slog.Logger
	pendingMu    sync.Mutex
	pendingOAuth map[string]*pendingOAuth
	sessionMu    sync.Mutex
	revoked      map[[32]byte]time.Time
}

type pendingOAuth struct {
	expiresAt  time.Time
	processing bool
	canceled   bool
}

func newAuthManager(cfg authConfig, client *http.Client) *authManager {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	return &authManager{cfg: cfg, client: client, now: time.Now, random: rand.Reader, logger: slog.Default(), pendingOAuth: make(map[string]*pendingOAuth), revoked: make(map[[32]byte]time.Time)}
}

func (m *authManager) authorizationURL(state, verifier string) (string, error) {
	callback, err := url.Parse(m.cfg.CallbackURL)
	if err != nil {
		return "", err
	}
	query := url.Values{
		"client_id":             {m.cfg.ClientID},
		"redirect_uri":          {m.cfg.CallbackURL},
		"scope":                 {"read:user"},
		"state":                 {state},
		"code_challenge":        {pkceChallenge(verifier)},
		"code_challenge_method": {"S256"},
		"allow_signup":          {"false"},
	}
	callback.Scheme = "https"
	callback.Host = "github.com"
	callback.Path = "/login/oauth/authorize"
	callback.RawQuery = query.Encode()
	callback.Fragment = ""
	return callback.String(), nil
}

func (m *authManager) login(c *gin.Context) {
	if !m.cfg.Required {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	if !m.sameOrigin(c.GetHeader("Origin")) {
		c.AbortWithStatus(http.StatusForbidden)
		return
	}
	state, err := randomToken(m.random, 32)
	if err != nil {
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}
	verifier, err := randomToken(m.random, 48)
	if err != nil {
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}
	expiresAt := m.now().Add(oauthStateLifetime)
	value, err := m.sign("oauth-state", oauthState{State: state, Verifier: verifier, ExpiresAt: expiresAt.Unix()})
	if err != nil {
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}
	target, err := m.authorizationURL(state, verifier)
	if err != nil {
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}
	m.registerOAuthState(state, expiresAt)
	cookieName := oauthCookieNameForState(state)
	http.SetCookie(c.Writer, &http.Cookie{Name: cookieName, Value: value, Path: "/", MaxAge: int(oauthStateLifetime.Seconds()), HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
	c.Redirect(http.StatusFound, target)
}

func (m *authManager) callback(c *gin.Context) {
	if !m.cfg.Required {
		c.String(http.StatusUnauthorized, "GitHub sign-in was not completed. Return to Ghostline and try again.")
		return
	}
	queryState := c.Query("state")
	if !validOAuthState(queryState) {
		c.String(http.StatusUnauthorized, "GitHub sign-in could not be verified. Return to Ghostline and try again.")
		return
	}
	cookieName := oauthCookieNameForState(queryState)
	stateCookie, err := c.Request.Cookie(cookieName)
	if err != nil {
		c.String(http.StatusUnauthorized, "GitHub sign-in expired. Return to Ghostline and try again.")
		return
	}
	var pending oauthState
	if !m.verify(stateCookie.Value, "oauth-state", &pending) || pending.ExpiresAt <= m.now().Unix() || !constantEqual(queryState, pending.State) {
		c.String(http.StatusUnauthorized, "GitHub sign-in could not be verified. Return to Ghostline and try again.")
		return
	}
	if !m.beginOAuthCallback(pending.State) {
		http.SetCookie(c.Writer, expiredCookie(cookieName))
		c.String(http.StatusUnauthorized, "GitHub sign-in expired. Return to Ghostline and try again.")
		return
	}
	defer m.finishOAuthCallback(pending.State)
	http.SetCookie(c.Writer, expiredCookie(cookieName))
	if c.Query("error") != "" {
		c.String(http.StatusUnauthorized, "GitHub sign-in was not completed. Return to Ghostline and try again.")
		return
	}
	code := c.Query("code")
	if code == "" || len(code) > maxOAuthCodeBytes {
		c.String(http.StatusBadRequest, "GitHub did not provide a valid sign-in code.")
		return
	}
	token, err := m.exchangeCode(c.Request.Context(), code, pending.Verifier)
	if err != nil {
		requestLogger(c.Request.Context(), m.logger).Error("GitHub OAuth callback failed", "stage", "token_exchange", "error", err)
		c.String(http.StatusBadGateway, "Ghostline could not complete GitHub sign-in. Try again shortly.")
		return
	}
	identity, err := m.fetchIdentity(c.Request.Context(), token)
	if err != nil {
		requestLogger(c.Request.Context(), m.logger).Error("GitHub OAuth callback failed", "stage", "identity_lookup", "error", err)
		c.String(http.StatusBadGateway, "Ghostline could not verify the GitHub account. Try again shortly.")
		return
	}
	if !domain.GitHubIdentityAllowed(identity.ID, m.cfg.AllowedIDs) {
		c.String(http.StatusForbidden, "This GitHub account is not on the Ghostline access list.")
		return
	}
	session, err := m.newSessionValue(identity, m.now().Add(defaultSessionLifetime))
	if err != nil {
		c.String(http.StatusInternalServerError, "Ghostline could not create a sign-in session.")
		return
	}
	if !m.completeOAuthCallback(pending.State, c, session) {
		c.String(http.StatusUnauthorized, "GitHub sign-in was canceled. Return to Ghostline and try again.")
		return
	}
	return
}

func (m *authManager) session(c *gin.Context) {
	result := gin.H{"authRequired": m.cfg.Required, "authenticated": !m.cfg.Required}
	if m.cfg.Required {
		if cookie, err := c.Request.Cookie(sessionCookieName); err == nil {
			if session, ok := m.sessionFromCookie(cookie.Value); ok && domain.GitHubIdentityAllowed(session.Identity.ID, m.cfg.AllowedIDs) {
				result["authenticated"] = true
				result["login"] = session.Identity.Login
			}
		}
	}
	c.JSON(http.StatusOK, result)
}

func (m *authManager) logout(c *gin.Context) {
	if !m.sameOrigin(c.GetHeader("Origin")) {
		c.AbortWithStatus(http.StatusForbidden)
		return
	}
	for _, cookie := range c.Request.Cookies() {
		if !strings.HasPrefix(cookie.Name, oauthCookiePrefix) {
			continue
		}
		var pending oauthState
		if m.verify(cookie.Value, "oauth-state", &pending) && cookie.Name == oauthCookieNameForState(pending.State) {
			m.cancelOAuthState(pending.State)
		}
		http.SetCookie(c.Writer, expiredCookie(cookie.Name))
	}
	if cookie, err := c.Request.Cookie(sessionCookieName); err == nil {
		m.revokeSession(cookie.Value)
	}
	http.SetCookie(c.Writer, expiredCookie(sessionCookieName))
	c.Status(http.StatusNoContent)
}

func (m *authManager) registerOAuthState(state string, expiresAt time.Time) {
	m.pendingMu.Lock()
	defer m.pendingMu.Unlock()
	now := m.now()
	for key, pending := range m.pendingOAuth {
		if !pending.processing && !pending.expiresAt.After(now) {
			delete(m.pendingOAuth, key)
		}
	}
	m.pendingOAuth[state] = &pendingOAuth{expiresAt: expiresAt}
}

func (m *authManager) beginOAuthCallback(state string) bool {
	m.pendingMu.Lock()
	defer m.pendingMu.Unlock()
	pending, ok := m.pendingOAuth[state]
	if !ok || pending.canceled || pending.processing || !pending.expiresAt.After(m.now()) {
		return false
	}
	pending.processing = true
	return true
}

func (m *authManager) finishOAuthCallback(state string) {
	m.pendingMu.Lock()
	delete(m.pendingOAuth, state)
	m.pendingMu.Unlock()
}

func (m *authManager) cancelOAuthState(state string) {
	m.pendingMu.Lock()
	defer m.pendingMu.Unlock()
	if pending, ok := m.pendingOAuth[state]; ok {
		if pending.processing {
			pending.canceled = true
		} else {
			delete(m.pendingOAuth, state)
		}
	}
}

func (m *authManager) completeOAuthCallback(state string, c *gin.Context, session string) bool {
	m.pendingMu.Lock()
	defer m.pendingMu.Unlock()
	pending, ok := m.pendingOAuth[state]
	if !ok || !pending.processing || pending.canceled || !pending.expiresAt.After(m.now()) {
		return false
	}
	http.SetCookie(c.Writer, &http.Cookie{Name: sessionCookieName, Value: session, Path: "/", MaxAge: int(defaultSessionLifetime.Seconds()), HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
	c.Redirect(http.StatusSeeOther, "/?ghostline_auth=complete")
	return true
}

func (m *authManager) middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !m.cfg.Required {
			c.Next()
			return
		}
		cookie, err := c.Request.Cookie(sessionCookieName)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": "AUTH_REQUIRED", "error": "Sign in with GitHub to use Ghostline."})
			return
		}
		session, ok := m.sessionFromCookie(cookie.Value)
		if !ok || !domain.GitHubIdentityAllowed(session.Identity.ID, m.cfg.AllowedIDs) {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": "AUTH_REQUIRED", "error": "Your Ghostline session expired. Sign in with GitHub again."})
			return
		}
		c.Set("ghostline.github_identity", session.Identity)
		c.Next()
	}
}

func (m *authManager) exchangeCode(ctx context.Context, code, verifier string) (string, error) {
	form := url.Values{
		"client_id":     {m.cfg.ClientID},
		"client_secret": {m.cfg.ClientSecret},
		"code":          {code},
		"redirect_uri":  {m.cfg.CallbackURL},
		"code_verifier": {verifier},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://github.com/login/oauth/access_token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := m.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("OAuth token endpoint returned HTTP %d", resp.StatusCode)
	}
	var result struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		Error       string `json:"error"`
	}
	if err := decodeLimitedJSON(resp.Body, &result); err != nil || result.Error != "" || result.AccessToken == "" || !strings.EqualFold(result.TokenType, "bearer") {
		return "", errors.New("OAuth token response was incomplete")
	}
	return result.AccessToken, nil
}

func (m *authManager) fetchIdentity(ctx context.Context, token string) (domain.GitHubIdentity, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/user", nil)
	if err != nil {
		return domain.GitHubIdentity{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	resp, err := m.client.Do(req)
	if err != nil {
		return domain.GitHubIdentity{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return domain.GitHubIdentity{}, fmt.Errorf("GitHub user endpoint returned HTTP %d", resp.StatusCode)
	}
	var result struct {
		ID    int64  `json:"id"`
		Login string `json:"login"`
	}
	if err := decodeLimitedJSON(resp.Body, &result); err != nil {
		return domain.GitHubIdentity{}, err
	}
	return domain.NewGitHubIdentity(result.ID, result.Login)
}

func decodeLimitedJSON(body io.Reader, target any) error {
	data, err := io.ReadAll(io.LimitReader(body, maxOAuthResponseBytes+1))
	if err != nil {
		return err
	}
	if len(data) > maxOAuthResponseBytes {
		return errors.New("OAuth response was too large")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return errors.New("OAuth response contained trailing JSON")
	}
	return nil
}

func (m *authManager) newSessionCookie(identity domain.GitHubIdentity, expiry time.Time) (*http.Cookie, error) {
	value, err := m.newSessionValue(identity, expiry)
	if err != nil {
		return nil, err
	}
	return &http.Cookie{Name: sessionCookieName, Value: value, Path: "/", HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode}, nil
}

func (m *authManager) newSessionValue(identity domain.GitHubIdentity, expiry time.Time) (string, error) {
	id, err := randomToken(m.random, 32)
	if err != nil {
		return "", err
	}
	return m.sign("session", authSession{ID: id, Identity: identity, ExpiresAt: expiry.Unix()})
}

func (m *authManager) sessionFromCookie(value string) (authSession, bool) {
	var session authSession
	if !m.verify(value, "session", &session) || session.ID == "" || session.ExpiresAt <= m.now().Unix() {
		return authSession{}, false
	}
	if m.isSessionRevoked(value) {
		return authSession{}, false
	}
	identity, err := domain.NewGitHubIdentity(session.Identity.ID, session.Identity.Login)
	if err != nil {
		return authSession{}, false
	}
	session.Identity = identity
	return session, true
}

func (m *authManager) revokeSession(value string) {
	var session authSession
	if !m.verify(value, "session", &session) || session.ID == "" || session.ExpiresAt <= m.now().Unix() || !domain.GitHubIdentityAllowed(session.Identity.ID, m.cfg.AllowedIDs) {
		return
	}
	digest := sha256.Sum256([]byte(value))
	expiresAt := time.Unix(session.ExpiresAt, 0)
	m.sessionMu.Lock()
	defer m.sessionMu.Unlock()
	now := m.now()
	for token, expiry := range m.revoked {
		if !expiry.After(now) {
			delete(m.revoked, token)
		}
	}
	m.revoked[digest] = expiresAt
}

func (m *authManager) isSessionRevoked(value string) bool {
	digest := sha256.Sum256([]byte(value))
	m.sessionMu.Lock()
	defer m.sessionMu.Unlock()
	expiresAt, ok := m.revoked[digest]
	if ok && !expiresAt.After(m.now()) {
		delete(m.revoked, digest)
		return false
	}
	return ok
}

func (m *authManager) sign(purpose string, value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	encoded := base64.RawURLEncoding.EncodeToString(data)
	message := purpose + "." + encoded
	mac := hmac.New(sha256.New, m.cfg.SessionKey)
	_, _ = mac.Write([]byte(message))
	return message + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func (m *authManager) verify(value, purpose string, target any) bool {
	if len(value) > 4096 {
		return false
	}
	parts := strings.Split(value, ".")
	if len(parts) != 3 || parts[0] != purpose {
		return false
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, m.cfg.SessionKey)
	_, _ = mac.Write([]byte(parts[0] + "." + parts[1]))
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return false
	}
	data, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return false
	}
	return json.Unmarshal(data, target) == nil
}

func (m *authManager) sameOrigin(origin string) bool {
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return false
	}
	callback, err := url.Parse(m.cfg.CallbackURL)
	if err != nil || parsed.Hostname() == "" || callback.Hostname() == "" || !strings.EqualFold(parsed.Scheme, callback.Scheme) || !strings.EqualFold(parsed.Hostname(), callback.Hostname()) {
		return false
	}
	return effectivePort(parsed) == effectivePort(callback)
}

func effectivePort(parsed *url.URL) string {
	if port := parsed.Port(); port != "" {
		return port
	}
	switch strings.ToLower(parsed.Scheme) {
	case "https":
		return "443"
	case "http":
		return "80"
	default:
		return ""
	}
}

func randomToken(source io.Reader, size int) (string, error) {
	data := make([]byte, size)
	if _, err := io.ReadFull(source, data); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func pkceChallenge(verifier string) string {
	digest := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(digest[:])
}

func constantEqual(left, right string) bool {
	return hmac.Equal([]byte(left), []byte(right))
}

const (
	oauthCookiePrefix = "__Host-ghostline_oauth_"
	sessionCookieName = "__Host-ghostline_session"
)

func oauthCookieNameForState(state string) string { return oauthCookiePrefix + state }

func validOAuthState(state string) bool {
	if len(state) != 43 {
		return false
	}
	for _, char := range state {
		if !((char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || char == '-' || char == '_') {
			return false
		}
	}
	return true
}

func expiredCookie(name string) *http.Cookie {
	return &http.Cookie{Name: name, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode}
}
