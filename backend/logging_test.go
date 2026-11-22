package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"regexp"
	"strings"
	"syscall"
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
		{name: "refused redirect", status: 302, failure: "status", level: "WARN"},
		{name: "server token rejected", status: 401, failure: "status", level: "WARN"},
		{name: "quota or permission refusal", status: 403, failure: "status", level: "WARN"},
		{name: "rate limited", status: 429, failure: "status", level: "WARN"},
		{name: "validation refusal", status: 422, failure: "status", level: "WARN"},
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
		{name: "storage body cut short", storage: func() (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(io.MultiReader(strings.NewReader("PK"), errReader{})), Header: http.Header{}}, nil
		}, want: map[string]any{"upstream": "artifact-storage", "upstream_status": float64(200), "failure": "transport", "path": ""}},
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

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("connection reset by peer") }

// A redirect between GitHub hosts is still GitHub: its failure names the API endpoint.
func TestArtifactRedirectWithinGitHubNamesGitHub(t *testing.T) {
	transport := roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host == "api.github.com" {
			return &http.Response{StatusCode: http.StatusFound, Body: io.NopCloser(strings.NewReader("")), Header: http.Header{"Location": []string{"https://github.com/acme/app/suites/1/artifacts/9"}}, Request: r}, nil
		}
		return &http.Response{StatusCode: http.StatusBadGateway, Body: io.NopCloser(strings.NewReader("")), Header: http.Header{}, Request: r}, nil
	})
	var buf bytes.Buffer
	ctx := withRequestLogger(context.Background(), jsonTestLogger(&buf))
	client := &githubClient{baseURL: "https://api.github.com", http: &http.Client{Transport: transport}}
	if _, err := client.downloadGitHubArtifact(ctx, "/repos/acme/app/actions/artifacts/9/zip", 1<<20); err == nil {
		t.Fatal("download succeeded")
	}
	line := findLog(t, logLines(t, &buf), "upstream request failed")
	if line["upstream"] != "github" || line["path"] != "/repos/acme/app/actions/artifacts/9/zip" || line["upstream_status"] != float64(502) {
		t.Fatalf("line = %v, want the GitHub endpoint", line)
	}
}

func TestOAuthUpstreamFailuresAreLogged(t *testing.T) {
	tests := []struct {
		name    string
		call    func(context.Context, *authManager) error
		respond func(*http.Request) (*http.Response, error)
		want    map[string]any
	}{
		{name: "token exchange status", call: func(ctx context.Context, m *authManager) error {
			_, err := m.exchangeCode(ctx, "code-secret", "verifier-secret")
			return err
		}, respond: func(r *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusServiceUnavailable, Body: io.NopCloser(strings.NewReader("")), Header: http.Header{}, Request: r}, nil
		}, want: map[string]any{"upstream": "github", "path": "/login/oauth/access_token", "upstream_status": float64(503), "failure": "status"}},
		{name: "identity transport", call: func(ctx context.Context, m *authManager) error {
			_, err := m.fetchIdentity(ctx, "token-secret")
			return err
		}, respond: func(*http.Request) (*http.Response, error) {
			return nil, errors.New("dial tcp: connection refused")
		}, want: map[string]any{"upstream": "github", "path": "/user", "failure": "transport"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			ctx := withRequestLogger(context.Background(), jsonTestLogger(&buf).With("request_id", "r-3"))
			auth := newAuthManager(testAuthConfig(), &http.Client{Transport: roundTripperFunc(tt.respond)})
			if err := tt.call(ctx, auth); err == nil {
				t.Fatal("call succeeded")
			}
			line := findLog(t, logLines(t, &buf), "upstream request failed")
			for key, want := range tt.want {
				if line[key] != want {
					t.Fatalf("%s = %v, want %v; line %v", key, line[key], want, line)
				}
			}
			if line["request_id"] != "r-3" {
				t.Fatalf("line has no request ID: %v", line)
			}
			for _, secret := range []string{"code-secret", "verifier-secret", "token-secret", testAuthConfig().ClientSecret} {
				if strings.Contains(buf.String(), secret) {
					t.Fatalf("log contains %q: %s", secret, buf.String())
				}
			}
		})
	}
}

// Qodo can reject a call inside a successful HTTP response; that is an upstream failure too.
func TestQodoRPCErrorIsLoggedAsUpstreamFailure(t *testing.T) {
	transport := roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Request: r,
			Body: io.NopCloser(strings.NewReader(`{"jsonrpc":"2.0","id":1,"error":{"code":-32001,"message":"workspace detail-secret"}}`))}, nil
	})
	var buf bytes.Buffer
	ctx := withRequestLogger(context.Background(), jsonTestLogger(&buf))
	client := &qodoReviewClient{endpoint: "https://qodo.test/v1/tools/mcp/", http: &http.Client{Transport: transport}, logger: slog.New(slog.DiscardHandler)}
	if _, err := client.fetch(ctx, "qodo-key", "https://github.com/acme/payments/pull/42"); err == nil {
		t.Fatal("fetch succeeded")
	}
	line := findLog(t, logLines(t, &buf), "upstream request failed")
	if line["upstream"] != "qodo" || line["failure"] != "rpc_error" || line["upstream_status"] != float64(200) || line["path"] != "/v1/tools/mcp/" {
		t.Fatalf("line = %v", line)
	}
	if strings.Contains(buf.String(), "detail-secret") || strings.Contains(buf.String(), "qodo-key") {
		t.Fatalf("log contains Qodo response details or the key: %s", buf.String())
	}
}

func TestBodyFailureSeparatesStallsFromMalformedReplies(t *testing.T) {
	expired, cancel := context.WithCancel(context.Background())
	cancel()
	tests := []struct {
		name string
		ctx  context.Context
		err  error
		want string
	}{
		{name: "body cut short", ctx: context.Background(), err: io.ErrUnexpectedEOF, want: "transport"},
		{name: "connection reset", ctx: context.Background(), err: &net.OpError{Op: "read", Net: "tcp", Err: syscall.ECONNRESET}, want: "transport"},
		{name: "client timeout mid-body", ctx: context.Background(), err: timeoutError{}, want: "deadline"},
		{name: "request canceled", ctx: expired, err: errors.New("read: use of closed connection"), want: "canceled"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := bodyFailure(tt.ctx, tt.err); got != tt.want {
				t.Fatalf("bodyFailure = %q, want %q", got, tt.want)
			}
		})
	}
}

type timeoutError struct{}

func (timeoutError) Error() string   { return "Client.Timeout exceeded while reading body" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }

type errReaderWith struct{ err error }

func (e errReaderWith) Read([]byte) (int, error) { return 0, e.err }

// A GitHub body that stops mid-read is a transport failure; one that arrived whole but
// holds truncated JSON is malformed.
func TestGitHubBodyFailuresAreClassified(t *testing.T) {
	tests := []struct {
		name string
		body func() io.Reader
		want string
	}{
		{name: "body cut short", body: func() io.Reader {
			return io.MultiReader(strings.NewReader(`{"number":`), errReaderWith{io.ErrUnexpectedEOF})
		}, want: "transport"},
		{name: "truncated JSON", body: func() io.Reader { return strings.NewReader(`{"number":`) }, want: "invalid_response"},
		{name: "not JSON", body: func() io.Reader { return strings.NewReader(`<html>`) }, want: "invalid_response"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transport := roundTripperFunc(func(r *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Request: r, Body: io.NopCloser(tt.body())}, nil
			})
			var buf bytes.Buffer
			ctx := withRequestLogger(context.Background(), jsonTestLogger(&buf))
			client := &githubClient{baseURL: "https://api.github.com", http: &http.Client{Transport: transport}}
			if err := client.get(ctx, "/repos/acme/app/pulls/7", &struct{}{}); err == nil {
				t.Fatal("get succeeded")
			}
			if line := findLog(t, logLines(t, &buf), "upstream request failed"); line["failure"] != tt.want {
				t.Fatalf("line = %v, want failure %s", line, tt.want)
			}
		})
	}
}

func TestGitHubDecodeKeepsAcceptingTrailingData(t *testing.T) {
	transport := roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Request: r, Body: io.NopCloser(strings.NewReader("{\"number\":7}\n"))}, nil
	})
	client := &githubClient{baseURL: "https://api.github.com", http: &http.Client{Transport: transport}}
	var got struct{ Number int }
	if err := client.get(context.Background(), "/repos/acme/app/pulls/7", &got); err != nil || got.Number != 7 {
		t.Fatalf("get = %+v, %v", got, err)
	}
}

func TestOAuthBodyFailuresAreClassified(t *testing.T) {
	identity := func(ctx context.Context, m *authManager) error {
		_, err := m.fetchIdentity(ctx, "token-secret")
		return err
	}
	token := func(ctx context.Context, m *authManager) error {
		_, err := m.exchangeCode(ctx, "code-secret", "verifier-secret")
		return err
	}
	tests := []struct {
		name string
		call func(context.Context, *authManager) error
		body func() io.Reader
		want string
	}{
		{name: "identity reply truncated JSON", call: identity, body: func() io.Reader { return strings.NewReader(`{"id":`) }, want: "invalid_response"},
		{name: "identity reply not JSON", call: identity, body: func() io.Reader { return strings.NewReader(`<html>`) }, want: "invalid_response"},
		{name: "identity body cut short", call: identity, body: func() io.Reader {
			return io.MultiReader(strings.NewReader(`{"id":`), errReaderWith{io.ErrUnexpectedEOF})
		}, want: "transport"},
		{name: "token reply cut short", call: token, body: func() io.Reader {
			return io.MultiReader(strings.NewReader(`{"access_token":`), errReaderWith{io.ErrUnexpectedEOF})
		}, want: "transport"},
		{name: "token reply without a token", call: token, body: func() io.Reader { return strings.NewReader(`{"error":"bad_verification_code"}`) }, want: "invalid_response"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			ctx := withRequestLogger(context.Background(), jsonTestLogger(&buf))
			auth := newAuthManager(testAuthConfig(), &http.Client{Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(tt.body()), Request: r}, nil
			})})
			if err := tt.call(ctx, auth); err == nil {
				t.Fatal("call succeeded")
			}
			if line := findLog(t, logLines(t, &buf), "upstream request failed"); line["failure"] != tt.want || line["upstream_status"] != float64(200) {
				t.Fatalf("line = %v, want failure %s", line, tt.want)
			}
		})
	}
}

func TestQodoJSONReplyFailuresAreClassified(t *testing.T) {
	tests := []struct {
		name string
		body func() io.Reader
		want string
	}{
		{name: "body cut short", body: func() io.Reader {
			return io.MultiReader(strings.NewReader(`{"jsonrpc":`), errReaderWith{io.ErrUnexpectedEOF})
		}, want: "transport"},
		{name: "truncated JSON", body: func() io.Reader { return strings.NewReader(`{"jsonrpc":`) }, want: "invalid_response"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transport := roundTripperFunc(func(r *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Request: r, Body: io.NopCloser(tt.body())}, nil
			})
			var buf bytes.Buffer
			ctx := withRequestLogger(context.Background(), jsonTestLogger(&buf))
			client := &qodoReviewClient{endpoint: "https://qodo.test/v1/tools/mcp/", http: &http.Client{Transport: transport}, logger: slog.New(slog.DiscardHandler)}
			if _, err := client.fetch(ctx, "qodo-key", "https://github.com/acme/payments/pull/42"); err == nil {
				t.Fatal("fetch succeeded")
			}
			if line := findLog(t, logLines(t, &buf), "upstream request failed"); line["failure"] != tt.want {
				t.Fatalf("line = %v, want failure %s", line, tt.want)
			}
		})
	}
}

func TestQodoSessionCleanupWarningCarriesTheRequestID(t *testing.T) {
	transport := roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodDelete {
			return &http.Response{StatusCode: http.StatusInternalServerError, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Mcp-Session-Id": []string{"session-1"}, "Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`not json`)), Request: r}, nil
	})
	var buf bytes.Buffer
	ctx := withRequestLogger(context.Background(), jsonTestLogger(&buf).With("request_id", "r-9"))
	client := &qodoReviewClient{endpoint: "https://qodo.test/v1/tools/mcp/", http: &http.Client{Transport: transport}, logger: slog.New(slog.DiscardHandler)}
	if _, err := client.fetch(ctx, "qodo-key", "https://github.com/acme/payments/pull/42"); err == nil {
		t.Fatal("fetch succeeded")
	}
	if line := findLog(t, logLines(t, &buf), "Qodo MCP session cleanup failed"); line["request_id"] != "r-9" {
		t.Fatalf("cleanup warning = %v, want request_id r-9", line)
	}
}

func TestArtifactRedirectToTrailingDotGitHubNamesGitHub(t *testing.T) {
	transport := roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host == "api.github.com" {
			return &http.Response{StatusCode: http.StatusFound, Body: io.NopCloser(strings.NewReader("")), Header: http.Header{"Location": []string{"https://github.com./acme/app/artifacts/9"}}, Request: r}, nil
		}
		return &http.Response{StatusCode: http.StatusBadGateway, Body: io.NopCloser(strings.NewReader("")), Header: http.Header{}, Request: r}, nil
	})
	var buf bytes.Buffer
	ctx := withRequestLogger(context.Background(), jsonTestLogger(&buf))
	client := &githubClient{baseURL: "https://api.github.com", http: &http.Client{Transport: transport}}
	if _, err := client.downloadGitHubArtifact(ctx, "/repos/acme/app/actions/artifacts/9/zip", 1<<20); err == nil {
		t.Fatal("download succeeded")
	}
	if line := findLog(t, logLines(t, &buf), "upstream request failed"); line["upstream"] != "github" {
		t.Fatalf("line = %v, want upstream github", line)
	}
}
