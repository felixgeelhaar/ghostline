package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// deployment is a fake Ghostline plus a fake GitHub sign-in page. Each test
// breaks one thing a signed-out user relies on and expects the check to name it.
type deployment struct {
	health        string
	indexHTML     string
	bundle        string
	session       string
	importStatus  int
	importBody    string
	loginStatus   int
	loginLocation func(base, github string) string
	githubPage    string
	githubStatus  int
	seenOrigin    string
}

func healthyDeployment() *deployment {
	return &deployment{
		health:       `{"status":"ok"}`,
		indexHTML:    `<!doctype html><html><head><title>Ghostline — Change Control</title><script type="module" crossorigin src="/assets/index-Abc123.js"></script></head><body><div id="app"></div></body></html>`,
		bundle:       "setup(e){let t=ub(`v0.1.0-rc.12`),n=N(``)",
		session:      `{"authRequired":true,"authenticated":false}`,
		importStatus: http.StatusUnauthorized,
		importBody:   `{"code":"AUTH_REQUIRED","error":"Sign in with GitHub to use Ghostline."}`,
		loginStatus:  http.StatusFound,
		loginLocation: func(base, github string) string {
			q := url.Values{"client_id": {"Ov23liTest"}, "redirect_uri": {base + "/auth/callback"}, "scope": {"read:user"}, "state": {"s"}, "code_challenge": {"c"}, "code_challenge_method": {"S256"}, "allow_signup": {"false"}}
			return github + "/login/oauth/authorize?" + q.Encode()
		},
		githubPage:   `<p>Sign in to GitHub to continue to <strong>Ghostline</strong></p>`,
		githubStatus: http.StatusOK,
	}
}

func (d *deployment) start(t *testing.T) (string, string) {
	t.Helper()
	github := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/login/oauth/authorize" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(d.githubStatus)
		fmt.Fprint(w, d.githubPage)
	}))
	t.Cleanup(github.Close)
	var app *httptest.Server
	app = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/health":
			fmt.Fprint(w, d.health)
		case r.Method == http.MethodGet && r.URL.Path == "/":
			fmt.Fprint(w, d.indexHTML)
		case r.Method == http.MethodGet && r.URL.Path == "/assets/index-Abc123.js":
			fmt.Fprint(w, d.bundle)
		case r.Method == http.MethodGet && r.URL.Path == "/api/session":
			fmt.Fprint(w, d.session)
		case r.Method == http.MethodPost && r.URL.Path == "/api/import":
			w.WriteHeader(d.importStatus)
			fmt.Fprint(w, d.importBody)
		case r.Method == http.MethodPost && r.URL.Path == "/auth/login":
			d.seenOrigin = r.Header.Get("Origin")
			if d.loginStatus == http.StatusFound {
				w.Header().Set("Location", d.loginLocation(app.URL, github.URL))
			}
			w.WriteHeader(d.loginStatus)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(app.Close)
	return app.URL, github.URL
}

func runAgainst(t *testing.T, d *deployment, adjust func(*Config)) ([]Step, error) {
	t.Helper()
	base, github := d.start(t)
	cfg := Config{Base: base, GitHub: github, ExpectVersion: "v0.1.0-rc.12", ExpectApp: "Ghostline"}
	if adjust != nil {
		adjust(&cfg)
	}
	return Run(context.Background(), http.DefaultTransport, cfg)
}

func TestHealthyDeploymentPassesEveryStep(t *testing.T) {
	d := healthyDeployment()
	steps, err := runAgainst(t, d, nil)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	want := []string{"health", "release UI", "signed-out session", "API refuses anonymous import", "sign-in hand-off", "GitHub shows the app"}
	if len(steps) != len(want) {
		t.Fatalf("steps = %v, want %v", steps, want)
	}
	for i, step := range steps {
		if step.Name != want[i] || step.Detail == "" {
			t.Errorf("step %d = %+v, want name %q with a detail", i, step, want[i])
		}
	}
	if d.seenOrigin == "" || !strings.HasPrefix(d.seenOrigin, "http://127.0.0.1") {
		t.Errorf("login Origin = %q, want the deployment's own origin", d.seenOrigin)
	}
}

func TestBrokenDeploymentNamesTheFailingStep(t *testing.T) {
	cases := []struct {
		name     string
		breakIt  func(*deployment)
		adjust   func(*Config)
		wantStep string
		wantText string
	}{
		{"health is down", func(d *deployment) { d.health = `{"status":"degraded"}` }, nil, "health", `"degraded"`},
		{"UI is not the deployed release", func(d *deployment) { d.bundle = `const v="v0.1.0-rc.11";` }, nil, "release UI", "v0.1.0-rc.12"},
		{"UI carries a longer release that starts the same", func(d *deployment) { d.bundle = "ub(`v0.1.0-rc.120`)" }, nil, "release UI", "v0.1.0-rc.12"},
		{"page has no app root", func(d *deployment) {
			d.indexHTML = `<html><title>Ghostline</title><script src="/assets/index-Abc123.js"></script></html>`
		}, nil, "release UI", `id="app"`},
		{"session reports signed in", func(d *deployment) { d.session = `{"authRequired":true,"authenticated":true}` }, nil, "signed-out session", "authenticated"},
		{"sign-in turned off", func(d *deployment) { d.session = `{"authRequired":false,"authenticated":true}` }, nil, "signed-out session", "authRequired"},
		{"API answers anonymous callers", func(d *deployment) { d.importStatus = http.StatusOK; d.importBody = `{}` }, nil, "API refuses anonymous import", "200"},
		{"API fails instead of refusing", func(d *deployment) { d.importStatus = http.StatusBadGateway; d.importBody = `bad gateway` }, nil, "API refuses anonymous import", "502"},
		{"login refuses", func(d *deployment) { d.loginStatus = http.StatusForbidden }, nil, "sign-in hand-off", "403"},
		{"login sends users elsewhere", func(d *deployment) {
			d.loginLocation = func(base, github string) string { return "https://example.com/login/oauth/authorize?client_id=x" }
		}, nil, "sign-in hand-off", "example.com"},
		{"callback points at another host", func(d *deployment) {
			healthy := d.loginLocation
			d.loginLocation = func(base, github string) string {
				return strings.Replace(healthy(base, github), url.QueryEscape(base+"/auth/callback"), url.QueryEscape("https://other.example/auth/callback"), 1)
			}
		}, nil, "sign-in hand-off", "redirect_uri"},
		{"no client id", func(d *deployment) {
			healthy := d.loginLocation
			d.loginLocation = func(base, github string) string {
				return strings.Replace(healthy(base, github), "client_id=Ov23liTest", "client_id=", 1)
			}
		}, nil, "sign-in hand-off", "client_id"},
		{"GitHub does not know the app", func(d *deployment) { d.githubStatus = http.StatusNotFound; d.githubPage = "Not Found" }, nil, "GitHub shows the app", "404"},
		{"GitHub shows another app", func(d *deployment) { d.githubPage = `to continue to <strong>Other</strong>` }, nil, "GitHub shows the app", "Ghostline"},
		{"controlled failure: an app name GitHub never shows", func(*deployment) {}, func(c *Config) { c.ExpectApp = "Ghostline controlled failure" }, "GitHub shows the app", "Ghostline controlled failure"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := healthyDeployment()
			tc.breakIt(d)
			steps, err := runAgainst(t, d, tc.adjust)
			if err == nil {
				t.Fatalf("Run() passed, want %q to fail", tc.wantStep)
			}
			var failure *StepError
			if !asStepError(err, &failure) || failure.Step != tc.wantStep {
				t.Fatalf("error = %v, want a failure in step %q", err, tc.wantStep)
			}
			if !strings.Contains(err.Error(), tc.wantText) {
				t.Errorf("error = %q, want it to mention %q", err, tc.wantText)
			}
			for _, step := range steps {
				if step.Name == tc.wantStep {
					t.Errorf("failed step %q is also reported as passed", tc.wantStep)
				}
			}
		})
	}
}

func TestConfigRejectsUnsafeTargets(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
	}{
		{"no version", Config{Base: "https://ghostline.example", GitHub: "https://github.com", ExpectApp: "Ghostline"}},
		{"no app", Config{Base: "https://ghostline.example", GitHub: "https://github.com", ExpectVersion: "v1"}},
		{"base with a path", Config{Base: "https://ghostline.example/app", GitHub: "https://github.com", ExpectVersion: "v1", ExpectApp: "Ghostline"}},
		{"plain http production", Config{Base: "http://ghostline.example", GitHub: "https://github.com", ExpectVersion: "v1", ExpectApp: "Ghostline"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.cfg.validate(); err == nil {
				t.Errorf("validate(%+v) = nil, want an error", tc.cfg)
			}
		})
	}
}
