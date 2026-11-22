package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"regexp"
	"strings"
	"testing"
	"time"
)

// logLines decodes the JSON log lines written to buf.
func logLines(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var lines []map[string]any
	for _, raw := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if raw == "" {
			continue
		}
		var line map[string]any
		if err := json.Unmarshal([]byte(raw), &line); err != nil {
			t.Fatalf("log line is not JSON: %q", raw)
		}
		lines = append(lines, line)
	}
	return lines
}

func findLog(t *testing.T, lines []map[string]any, msg string) map[string]any {
	t.Helper()
	for _, line := range lines {
		if line["msg"] == msg {
			return line
		}
	}
	t.Fatalf("no %q log line in %v", msg, lines)
	return nil
}

func jsonTestLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(buf, nil))
}

func TestNewLoggerUsesJSONInProduction(t *testing.T) {
	tests := []struct {
		env  string
		json bool
	}{
		{env: "production", json: true},
		{env: " Production ", json: true},
		{env: "", json: false},
		{env: "development", json: false},
	}
	for _, tt := range tests {
		var buf bytes.Buffer
		newLogger(func(key string) string {
			if key == "GHOSTLINE_ENV" {
				return tt.env
			}
			return ""
		}, &buf).Info("hello", "k", "v")
		isJSON := json.Valid(bytes.TrimSpace(buf.Bytes()))
		if isJSON != tt.json {
			t.Fatalf("GHOSTLINE_ENV=%q wrote %q; want JSON = %v", tt.env, buf.String(), tt.json)
		}
		if !tt.json && !strings.Contains(buf.String(), "msg=hello k=v") {
			t.Fatalf("text log = %q", buf.String())
		}
	}
}

var generatedRequestID = regexp.MustCompile(`^[0-9a-f]{32}$`)

func TestAccessLineNamesTheRouteNotThePath(t *testing.T) {
	var buf bytes.Buffer
	router := newRouterWithQodoLogger(&githubClient{baseURL: "https://api.github.com", http: http.DefaultClient}, nil, newRateLimiter(defaultRateLimitRequests, defaultRateLimitWindow, time.Now), clientIdentity{}, jsonTestLogger(&buf))

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/health?access_token=query-secret", nil))
	id := recorder.Header().Get("X-Request-ID")
	if !generatedRequestID.MatchString(id) {
		t.Fatalf("X-Request-ID = %q, want 32 hex characters", id)
	}
	line := findLog(t, logLines(t, &buf), "request")
	if line["method"] != "GET" || line["route"] != "/api/health" || line["status"] != float64(200) || line["request_id"] != id {
		t.Fatalf("access line = %v", line)
	}
	if _, ok := line["duration_ms"].(float64); !ok {
		t.Fatalf("access line has no numeric duration_ms: %v", line)
	}

	buf.Reset()
	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/no/such/route-1234", nil))
	line = findLog(t, logLines(t, &buf), "request")
	if line["route"] != "unmatched" || line["status"] != float64(404) {
		t.Fatalf("unmatched access line = %v", line)
	}
	if strings.Contains(buf.String(), "route-1234") || strings.Contains(buf.String(), "query-secret") {
		t.Fatalf("log contains a raw path or query: %s", buf.String())
	}
}

func TestEachRequestGetsItsOwnID(t *testing.T) {
	router := newRouterWithQodoLogger(&githubClient{baseURL: "https://api.github.com", http: http.DefaultClient}, nil, newRateLimiter(defaultRateLimitRequests, defaultRateLimitWindow, time.Now), clientIdentity{}, slog.New(slog.DiscardHandler))
	seen := map[string]bool{}
	for i := 0; i < 5; i++ {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/health", nil))
		id := recorder.Header().Get("X-Request-ID")
		if seen[id] {
			t.Fatalf("request ID %q repeated", id)
		}
		seen[id] = true
	}
}

func TestInboundRequestIDIsTrustedOnlyFromTrustedProxies(t *testing.T) {
	identity := clientIdentity{Header: "X-Forwarded-For", TrustedProxies: []netip.Prefix{netip.MustParsePrefix("192.0.2.0/24")}}
	tests := []struct {
		name    string
		remote  string
		inbound string
		keep    bool
	}{
		{name: "trusted proxy", remote: "192.0.2.10:4000", inbound: "edge-7f3a.42", keep: true},
		{name: "untrusted peer", remote: "198.51.100.9:4000", inbound: "edge-7f3a.42", keep: false},
		{name: "trusted proxy, value with a newline", remote: "192.0.2.10:4000", inbound: "edge\nmsg=forged", keep: false},
		{name: "trusted proxy, value with spaces", remote: "192.0.2.10:4000", inbound: "edge 1", keep: false},
		{name: "trusted proxy, value too long", remote: "192.0.2.10:4000", inbound: strings.Repeat("a", 129), keep: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			router := newRouterWithQodoLogger(&githubClient{baseURL: "https://api.github.com", http: http.DefaultClient}, nil, newRateLimiter(defaultRateLimitRequests, defaultRateLimitWindow, time.Now), identity, jsonTestLogger(&buf))
			request := httptest.NewRequest(http.MethodGet, "/api/health", nil)
			request.RemoteAddr = tt.remote
			request.Header.Set("X-Request-ID", tt.inbound)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)
			id := recorder.Header().Get("X-Request-ID")
			if tt.keep && id != tt.inbound {
				t.Fatalf("X-Request-ID = %q, want the trusted inbound %q", id, tt.inbound)
			}
			if !tt.keep && !generatedRequestID.MatchString(id) {
				t.Fatalf("X-Request-ID = %q, want a generated ID", id)
			}
			if !tt.keep && strings.Contains(buf.String(), "forged") {
				t.Fatalf("log contains the rejected inbound ID: %s", buf.String())
			}
		})
	}
}

func TestSecretsAreNeverLogged(t *testing.T) {
	var buf bytes.Buffer
	qodo := qodoSourceFunc(func(context.Context, string, string) (qodoReviewData, error) {
		return qodoReviewData{}, qodoServiceError{status: http.StatusUnauthorized}
	})
	router := newRouterWithQodoLogger(&githubClient{baseURL: "https://api.github.com", http: http.DefaultClient}, qodo, newRateLimiter(defaultRateLimitRequests, defaultRateLimitWindow, time.Now), clientIdentity{}, jsonTestLogger(&buf))
	request := httptest.NewRequest(http.MethodPost, "/api/qodo-review?access_token=query-secret", strings.NewReader(`{"url":"https://github.com/acme/payments/pull/42"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://ghostline.example")
	request.Header.Set("Authorization", "Bearer ghp_header-secret")
	request.Header.Set("Cookie", "ghostline_session=cookie-secret")
	request.Header.Set("X-Qodo-API-Key", "qodo-key-secret")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	for _, secret := range []string{"query-secret", "ghp_header-secret", "cookie-secret", "qodo-key-secret"} {
		if strings.Contains(buf.String(), secret) {
			t.Fatalf("log contains %q: %s", secret, buf.String())
		}
	}
	id := recorder.Header().Get("X-Request-ID")
	lines := logLines(t, &buf)
	failure := findLog(t, lines, "Qodo structured review request failed")
	if failure["request_id"] != id || failure["upstream_status"] != float64(401) {
		t.Fatalf("Qodo failure line = %v, want request_id %s and upstream_status 401", failure, id)
	}
	access := findLog(t, lines, "request")
	if access["route"] != "/api/qodo-review" || access["status"] != float64(401) {
		t.Fatalf("access line = %v", access)
	}
}

func TestGitHubFailuresAreLoggedWithTheRequestID(t *testing.T) {
	github := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"message":"body-secret"}`))
	}))
	defer github.Close()
	var buf bytes.Buffer
	router := newRouterWithQodoLogger(&githubClient{baseURL: github.URL, token: "server-token-secret", http: github.Client()}, nil, newRateLimiter(defaultRateLimitRequests, defaultRateLimitWindow, time.Now), clientIdentity{}, jsonTestLogger(&buf))
	request := httptest.NewRequest(http.MethodPost, "/api/import", strings.NewReader(`{"url":"https://github.com/acme/app/pull/7"}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	id := recorder.Header().Get("X-Request-ID")
	failure := findLog(t, logLines(t, &buf), "upstream request failed")
	if failure["request_id"] != id || failure["upstream"] != "github" || failure["upstream_status"] != float64(502) || failure["failure"] != "status" {
		t.Fatalf("GitHub failure line = %v", failure)
	}
	if path, _ := failure["path"].(string); !strings.HasPrefix(path, "/repos/acme/app") || strings.Contains(path, "?") {
		t.Fatalf("logged path = %q, want the API path without a query", path)
	}
	for _, secret := range []string{"body-secret", "server-token-secret"} {
		if strings.Contains(buf.String(), secret) {
			t.Fatalf("log contains %q: %s", secret, buf.String())
		}
	}
}

func TestGitHubTransportFailureLogsAKindNotTheError(t *testing.T) {
	var buf bytes.Buffer
	logger := jsonTestLogger(&buf)
	ctx := withRequestLogger(context.Background(), logger.With("request_id", "r-1"))
	client := &githubClient{baseURL: "http://127.0.0.1:1", http: &http.Client{Timeout: time.Second}}
	if err := client.get(ctx, "/repos/acme/app/pulls/7?signature=sig-secret", &struct{}{}); err == nil {
		t.Fatal("request to a closed port succeeded")
	}
	failure := findLog(t, logLines(t, &buf), "upstream request failed")
	if failure["request_id"] != "r-1" || failure["failure"] != "transport" || failure["path"] != "/repos/acme/app/pulls/7" {
		t.Fatalf("transport failure line = %v", failure)
	}
	if _, ok := failure["upstream_status"]; ok || strings.Contains(buf.String(), "sig-secret") || strings.Contains(buf.String(), "127.0.0.1") {
		t.Fatalf("transport failure logged more than its kind: %s", buf.String())
	}
}

func TestUpstreamFailureLevels(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		failure string
		level   string
	}{
		{name: "expected 404", status: 404, failure: "status", level: "INFO"},
		{name: "server error", status: 502, failure: "status", level: "WARN"},
		{name: "no response", status: 0, failure: "transport", level: "WARN"},
		{name: "undecodable success", status: 200, failure: "invalid_response", level: "WARN"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			ctx := withRequestLogger(context.Background(), jsonTestLogger(&buf))
			logUpstreamFailure(ctx, "github", "/repos/acme/app", tt.status, tt.failure)
			if line := findLog(t, logLines(t, &buf), "upstream request failed"); line["level"] != tt.level {
				t.Fatalf("level = %v, want %s", line["level"], tt.level)
			}
		})
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// After GitHub redirects an artifact download to storage, a storage failure names the
// storage upstream, not the API endpoint, and never the signed URL.
func TestArtifactStorageFailureNamesStorage(t *testing.T) {
	const signed = "https://productionresultssa1.blob.core.windows.net/actions/artifact.zip?sig=signed-secret"
	tests := []struct {
		name    string
		storage func() (*http.Response, error)
		want    map[string]any
	}{
		{name: "storage error status", storage: func() (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusServiceUnavailable, Body: io.NopCloser(strings.NewReader("")), Header: http.Header{}}, nil
		}, want: map[string]any{"upstream": "artifact-storage", "upstream_status": float64(503), "failure": "status", "path": ""}},
		{name: "storage unreachable", storage: func() (*http.Response, error) {
			return nil, errors.New("dial tcp: lookup productionresultssa1.blob.core.windows.net?sig=signed-secret")
		}, want: map[string]any{"upstream": "artifact-storage", "failure": "transport", "path": ""}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transport := roundTripperFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.Host == "api.github.com" {
					return &http.Response{StatusCode: http.StatusFound, Body: io.NopCloser(strings.NewReader("")), Header: http.Header{"Location": []string{signed}}, Request: r}, nil
				}
				response, err := tt.storage()
				if response != nil {
					response.Request = r
				}
				return response, err
			})
			var buf bytes.Buffer
			ctx := withRequestLogger(context.Background(), jsonTestLogger(&buf).With("request_id", "r-2"))
			client := &githubClient{baseURL: "https://api.github.com", token: "server-token", http: &http.Client{Transport: transport}}
			if _, err := client.downloadGitHubArtifact(ctx, "/repos/acme/app/actions/artifacts/9/zip", 1<<20); err == nil {
				t.Fatal("download succeeded")
			}
			line := findLog(t, logLines(t, &buf), "upstream request failed")
			for key, want := range tt.want {
				if line[key] != want {
					t.Fatalf("%s = %v, want %v; line %v", key, line[key], want, line)
				}
			}
			if strings.Contains(buf.String(), "signed-secret") || strings.Contains(buf.String(), "blob.core.windows.net") {
				t.Fatalf("log contains the signed storage URL: %s", buf.String())
			}
		})
	}
}
