package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// Config names the deployment to check and what it must show. ExpectVersion is
// the release the deploy manifest pins; ExpectApp is the OAuth app name GitHub
// shows on its sign-in page.
type Config struct {
	Base          string
	GitHub        string
	ExpectVersion string
	ExpectApp     string
}

// Step is one assertion that passed, with what it observed.
type Step struct {
	Name   string
	Detail string
}

// StepError names the first assertion that failed.
type StepError struct {
	Step string
	Err  error
}

func (e *StepError) Error() string { return e.Step + ": " + e.Err.Error() }
func (e *StepError) Unwrap() error { return e.Err }

func asStepError(err error, target **StepError) bool { return errors.As(err, target) }

const maxBody = 4 << 20

var bundlePattern = regexp.MustCompile(`<script[^>]+src="(/assets/index-[A-Za-z0-9_-]+\.js)"`)

func (c Config) validate() error {
	if c.ExpectVersion == "" || c.ExpectApp == "" {
		return errors.New("an expected release version and app name are required")
	}
	for name, raw := range map[string]string{"base": c.Base, "github": c.GitHub} {
		u, err := url.Parse(raw)
		if err != nil || u.Host == "" || (u.Path != "" && u.Path != "/") || u.RawQuery != "" {
			return fmt.Errorf("%s %q must be an origin such as https://host", name, raw)
		}
		if u.Scheme != "https" && !(u.Scheme == "http" && isLoopback(u.Hostname())) {
			return fmt.Errorf("%s %q must use https", name, raw)
		}
	}
	return nil
}

func isLoopback(host string) bool {
	ip := net.ParseIP(host)
	return host == "localhost" || (ip != nil && ip.IsLoopback())
}

// Run checks, in order, what a signed-out user relies on: the server answers,
// it serves the pinned release's UI, it reports no session, its API refuses an
// anonymous import, the sign-in button hands off to GitHub with this
// deployment's callback, and GitHub recognises the OAuth app. It stops at the
// first failure and never signs in.
func Run(ctx context.Context, rt http.RoundTripper, cfg Config) ([]Step, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	base := strings.TrimSuffix(cfg.Base, "/")
	github := strings.TrimSuffix(cfg.GitHub, "/")
	noRedirect := &http.Client{Transport: rt, Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	follow := &http.Client{Transport: rt, Timeout: 20 * time.Second}

	var steps []Step
	var handoff string
	checks := []struct {
		name string
		run  func() (string, error)
	}{
		{"health", func() (string, error) { return checkHealth(ctx, noRedirect, base) }},
		{"release UI", func() (string, error) { return checkReleaseUI(ctx, noRedirect, base, cfg.ExpectVersion) }},
		{"signed-out session", func() (string, error) { return checkSession(ctx, noRedirect, base) }},
		{"API refuses anonymous import", func() (string, error) { return checkAnonymousImport(ctx, noRedirect, base) }},
		{"sign-in hand-off", func() (string, error) {
			target, detail, err := checkHandoff(ctx, noRedirect, base, github)
			handoff = target
			return detail, err
		}},
		{"GitHub shows the app", func() (string, error) { return checkGitHubPage(ctx, follow, handoff, cfg.ExpectApp) }},
	}
	for _, check := range checks {
		detail, err := check.run()
		if err != nil {
			return steps, &StepError{Step: check.name, Err: err}
		}
		steps = append(steps, Step{Name: check.name, Detail: detail})
	}
	return steps, nil
}

func fetch(ctx context.Context, client *http.Client, method, target, origin string, body io.Reader) (*http.Response, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("User-Agent", "ghostline-signin-monitor")
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, nil, fmt.Errorf("read %s: %w", target, err)
	}
	return resp, data, nil
}

func checkHealth(ctx context.Context, client *http.Client, base string) (string, error) {
	resp, body, err := fetch(ctx, client, http.MethodGet, base+"/api/health", "", nil)
	if err != nil {
		return "", err
	}
	var got struct {
		Status string `json:"status"`
	}
	if resp.StatusCode != http.StatusOK || json.Unmarshal(body, &got) != nil || got.Status != "ok" {
		return "", fmt.Errorf("GET /api/health = %d %s, want 200 {\"status\":\"ok\"}", resp.StatusCode, snippet(body))
	}
	return "200 status ok", nil
}

func checkReleaseUI(ctx context.Context, client *http.Client, base, version string) (string, error) {
	resp, page, err := fetch(ctx, client, http.MethodGet, base+"/", "", nil)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(page), `id="app"`) || !strings.Contains(string(page), "<title>Ghostline") {
		return "", fmt.Errorf(`GET / = %d without the Ghostline title and id="app" root`, resp.StatusCode)
	}
	match := bundlePattern.FindSubmatch(page)
	if match == nil {
		return "", errors.New("GET / names no /assets/index-*.js bundle")
	}
	bundlePath := string(match[1])
	resp, bundle, err := fetch(ctx, client, http.MethodGet, base+bundlePath, "", nil)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GET %s = %d, want 200", bundlePath, resp.StatusCode)
	}
	if !carriesString(string(bundle), version) {
		return "", fmt.Errorf("%s does not carry the pinned release %s", bundlePath, version)
	}
	return fmt.Sprintf("%s carries %s", bundlePath, version), nil
}

func checkSession(ctx context.Context, client *http.Client, base string) (string, error) {
	resp, body, err := fetch(ctx, client, http.MethodGet, base+"/api/session", "", nil)
	if err != nil {
		return "", err
	}
	var got struct {
		AuthRequired  *bool `json:"authRequired"`
		Authenticated *bool `json:"authenticated"`
	}
	if resp.StatusCode != http.StatusOK || json.Unmarshal(body, &got) != nil || got.AuthRequired == nil || got.Authenticated == nil {
		return "", fmt.Errorf("GET /api/session = %d %s, want 200 with authRequired and authenticated", resp.StatusCode, snippet(body))
	}
	if !*got.AuthRequired {
		return "", errors.New("authRequired is false: production must require GitHub sign-in")
	}
	if *got.Authenticated {
		return "", errors.New("authenticated is true for a request without a session cookie")
	}
	return "authRequired true, authenticated false", nil
}

func checkAnonymousImport(ctx context.Context, client *http.Client, base string) (string, error) {
	resp, body, err := fetch(ctx, client, http.MethodPost, base+"/api/import", "", strings.NewReader(`{}`))
	if err != nil {
		return "", err
	}
	var got struct {
		Code string `json:"code"`
	}
	if resp.StatusCode != http.StatusUnauthorized || json.Unmarshal(body, &got) != nil || got.Code != "AUTH_REQUIRED" {
		return "", fmt.Errorf("POST /api/import without a session = %d %s, want 401 AUTH_REQUIRED", resp.StatusCode, snippet(body))
	}
	return "401 AUTH_REQUIRED", nil
}

// checkHandoff presses the sign-in button: a same-origin POST /auth/login must
// redirect to GitHub's authorize page with this deployment's callback.
func checkHandoff(ctx context.Context, client *http.Client, base, github string) (string, string, error) {
	resp, _, err := fetch(ctx, client, http.MethodPost, base+"/auth/login", base, nil)
	if err != nil {
		return "", "", err
	}
	if resp.StatusCode != http.StatusFound {
		return "", "", fmt.Errorf("POST /auth/login = %d, want 302 to GitHub", resp.StatusCode)
	}
	location := resp.Header.Get("Location")
	target, err := url.Parse(location)
	if err != nil || target.Scheme+"://"+target.Host != github || target.Path != "/login/oauth/authorize" {
		host := location
		if err == nil {
			host = target.Host
		}
		return "", "", fmt.Errorf("POST /auth/login redirects to %s, want %s/login/oauth/authorize", host, github)
	}
	q := target.Query()
	if q.Get("client_id") == "" {
		return "", "", errors.New("authorize URL has no client_id")
	}
	if got, want := q.Get("redirect_uri"), base+"/auth/callback"; got != want {
		return "", "", fmt.Errorf("authorize URL redirect_uri = %q, want %q", got, want)
	}
	if q.Get("state") == "" || q.Get("code_challenge") == "" || q.Get("code_challenge_method") != "S256" {
		return "", "", errors.New("authorize URL lacks state or an S256 code challenge")
	}
	return location, fmt.Sprintf("302 to %s/login/oauth/authorize, client_id %s, callback %s", github, q.Get("client_id"), q.Get("redirect_uri")), nil
}

// checkGitHubPage opens the authorize URL signed out. GitHub sends an unknown
// client_id to an error page; for a valid app it asks the user to sign in "to
// continue to" the app's name.
func checkGitHubPage(ctx context.Context, client *http.Client, target, app string) (string, error) {
	resp, page, err := fetch(ctx, client, http.MethodGet, target, "", nil)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub authorize page = %d, want 200", resp.StatusCode)
	}
	want := "to continue to <strong>" + app + "</strong>"
	if !strings.Contains(string(page), want) {
		return "", fmt.Errorf("GitHub's sign-in page does not say %q", "to continue to "+app)
	}
	return fmt.Sprintf("200, asks to sign in to continue to %s", app), nil
}

// carriesString reports whether the bundle holds value as a whole JavaScript
// string literal in any quote style; Vite emits template literals.
func carriesString(bundle, value string) bool {
	for _, quote := range []string{`"`, "'", "`"} {
		if strings.Contains(bundle, quote+value+quote) {
			return true
		}
	}
	return false
}

func snippet(body []byte) string {
	s := strings.Join(strings.Fields(string(body)), " ")
	if len(s) > 120 {
		s = s[:120] + "…"
	}
	return s
}
