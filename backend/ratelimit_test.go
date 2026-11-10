package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"reflect"
	"strings"
	"testing"
	"time"
)

type fakeClock struct{ now time.Time }

func (f *fakeClock) Now() time.Time { return f.now }

func TestRateLimiterAllow(t *testing.T) {
	start := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	type step struct {
		advance   time.Duration
		client    string
		wantAllow bool
		wantRetry time.Duration
	}
	tests := []struct {
		name  string
		limit int
		steps []step
	}{
		{"within limit", 2, []step{{0, "a", true, 0}, {time.Second, "a", true, 0}}},
		{"over limit reports time until window reset", 2, []step{
			{0, "a", true, 0}, {0, "a", true, 0}, {10 * time.Second, "a", false, 50 * time.Second},
		}},
		{"window reset admits the client again", 1, []step{
			{0, "a", true, 0}, {30 * time.Second, "a", false, 30 * time.Second}, {30 * time.Second, "a", true, 0},
		}},
		{"distinct clients have independent budgets", 1, []step{
			{0, "a", true, 0}, {0, "b", true, 0}, {0, "a", false, time.Minute},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clock := &fakeClock{now: start}
			limiter := newRateLimiter(tt.limit, time.Minute, clock.Now)
			for i, s := range tt.steps {
				clock.now = clock.now.Add(s.advance)
				allowed, retry := limiter.allow(s.client)
				if allowed != s.wantAllow || retry != s.wantRetry {
					t.Fatalf("step %d: allowed=%v retry=%v, want allowed=%v retry=%v", i, allowed, retry, s.wantAllow, s.wantRetry)
				}
			}
		})
	}
}

func TestRateLimiterDropsExpiredClients(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)}
	limiter := newRateLimiter(1, time.Minute, clock.Now)
	for _, c := range []string{"a", "b", "c"} {
		limiter.allow(c)
	}
	clock.now = clock.now.Add(2 * time.Minute)
	limiter.allow("d")
	if got := limiter.size(); got != 1 {
		t.Fatalf("tracked clients = %d, want 1 after expiry", got)
	}
}

func TestLoadRateLimitConfig(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		want    rateLimitConfig
		wantErr bool
	}{
		{"defaults", map[string]string{}, rateLimitConfig{Requests: 30, Window: time.Minute}, false},
		{"custom", map[string]string{"RATE_LIMIT_REQUESTS": "5", "RATE_LIMIT_WINDOW": "10s", "RATE_LIMIT_CLIENT_IP_HEADER": "X-Ghostline-Client-IP", "RATE_LIMIT_TRUSTED_PROXIES": "10.0.0.0/8, fc00::/7"},
			rateLimitConfig{Requests: 5, Window: 10 * time.Second, Identity: clientIdentity{Header: "X-Ghostline-Client-IP", TrustedProxies: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8"), netip.MustParsePrefix("fc00::/7")}}}, false},
		{"header without trusted proxies", map[string]string{"RATE_LIMIT_CLIENT_IP_HEADER": "X-Ghostline-Client-IP"}, rateLimitConfig{}, true},
		{"trusted proxies without header", map[string]string{"RATE_LIMIT_TRUSTED_PROXIES": "10.0.0.0/8"}, rateLimitConfig{}, true},
		{"invalid trusted proxy", map[string]string{"RATE_LIMIT_CLIENT_IP_HEADER": "X-Ghostline-Client-IP", "RATE_LIMIT_TRUSTED_PROXIES": "10.0.0.0/8,nope"}, rateLimitConfig{}, true},
		{"non numeric requests", map[string]string{"RATE_LIMIT_REQUESTS": "many"}, rateLimitConfig{}, true},
		{"zero requests", map[string]string{"RATE_LIMIT_REQUESTS": "0"}, rateLimitConfig{}, true},
		{"negative requests", map[string]string{"RATE_LIMIT_REQUESTS": "-1"}, rateLimitConfig{}, true},
		{"bad window", map[string]string{"RATE_LIMIT_WINDOW": "soon"}, rateLimitConfig{}, true},
		{"zero window", map[string]string{"RATE_LIMIT_WINDOW": "0s"}, rateLimitConfig{}, true},
		{"invalid header name", map[string]string{"RATE_LIMIT_CLIENT_IP_HEADER": "bad header"}, rateLimitConfig{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := loadRateLimitConfig(func(k string) string { return tt.env[k] })
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestClientKey(t *testing.T) {
	proxy := clientIdentity{Header: "X-Ghostline-Client-IP", TrustedProxies: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}}
	tests := []struct {
		name, remote, headerValue, want string
		identity                        clientIdentity
	}{
		{"remote address only", "203.0.113.9:5555", "", "203.0.113.9", clientIdentity{}},
		{"header ignored unless configured", "203.0.113.9:5555", "198.51.100.1", "203.0.113.9", clientIdentity{}},
		{"trusted proxy header is used", "10.0.0.2:80", "198.51.100.1", "198.51.100.1", proxy},
		{"untrusted peer cannot choose its identity", "203.0.113.9:5555", "198.51.100.1", "203.0.113.9", proxy},
		{"forwarding chain is rejected", "10.0.0.2:80", "6.6.6.6, 198.51.100.1", "10.0.0.2", proxy},
		{"garbage falls back to the peer", "10.0.0.2:80", "not-an-ip", "10.0.0.2", proxy},
		{"absent header falls back to the peer", "10.0.0.2:80", "", "10.0.0.2", proxy},
		{"IPv4-mapped address is normalized", "10.0.0.2:80", "::ffff:198.51.100.1", "198.51.100.1", proxy},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/import", nil)
			req.RemoteAddr = tt.remote
			if tt.headerValue != "" {
				req.Header.Set("X-Ghostline-Client-IP", tt.headerValue)
			}
			if got := clientKey(req, tt.identity); got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRateLimitThroughTrustedProxy(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)}
	identity := clientIdentity{Header: "X-Ghostline-Client-IP", TrustedProxies: []netip.Prefix{netip.MustParsePrefix("172.16.0.0/12")}}
	router := newRouterWithLimiter(&githubClient{baseURL: "http://127.0.0.1:1", http: http.DefaultClient}, newRateLimiter(1, time.Minute, clock.Now), identity)
	post := func(client string) int {
		req := httptest.NewRequest(http.MethodPost, "/api/import", strings.NewReader(`{"url":"https://example.com/x"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Ghostline-Client-IP", client)
		req.RemoteAddr = "172.18.0.3:40000" // the frontend proxy container
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec.Code
	}
	if code := post("192.168.1.10"); code != http.StatusBadRequest {
		t.Fatalf("first browser: status %d, want 400 from the handler", code)
	}
	if code := post("192.168.1.11"); code != http.StatusBadRequest {
		t.Fatalf("second browser behind the same proxy: status %d, want its own budget", code)
	}
	if code := post("192.168.1.10"); code != http.StatusTooManyRequests {
		t.Fatalf("first browser again: status %d, want 429", code)
	}
}

func TestRateLimitedRoutes(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)}
	limiter := newRateLimiter(2, time.Minute, clock.Now)
	router := newRouterWithLimiter(&githubClient{baseURL: "http://127.0.0.1:1", http: http.DefaultClient}, limiter, clientIdentity{})

	post := func(path, remote string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"url":"https://example.com/x"}`))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = remote
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	// Requests within the limit reach the handler (400 for the invalid URL), not the limiter.
	for i := 0; i < 2; i++ {
		if rec := post("/api/import", "203.0.113.9:1000"); rec.Code != http.StatusBadRequest {
			t.Fatalf("request %d: status %d, want 400", i, rec.Code)
		}
	}
	// The budget is shared across /api/import and /api/checks for one client.
	rec := post("/api/checks", "203.0.113.9:1000")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("over limit: status %d, want 429", rec.Code)
	}
	if got := rec.Header().Get("Retry-After"); got != "60" {
		t.Fatalf("Retry-After = %q, want 60", got)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body["error"] == "" {
		t.Fatalf("body %q is not the API error shape: %v", rec.Body.String(), err)
	}
	// Another client is unaffected.
	if rec := post("/api/import", "198.51.100.7:1000"); rec.Code != http.StatusBadRequest {
		t.Fatalf("other client: status %d, want 400", rec.Code)
	}
	// Health stays unlimited.
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
		req.RemoteAddr = "203.0.113.9:1000"
		health := httptest.NewRecorder()
		router.ServeHTTP(health, req)
		if health.Code != http.StatusOK {
			t.Fatalf("health status %d", health.Code)
		}
	}
	// After the window resets the client is admitted again.
	clock.now = clock.now.Add(time.Minute)
	if rec := post("/api/import", "203.0.113.9:1000"); rec.Code != http.StatusBadRequest {
		t.Fatalf("after reset: status %d, want 400", rec.Code)
	}
}
