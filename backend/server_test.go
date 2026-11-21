package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestLoadServerConfigDefaults(t *testing.T) {
	cfg, err := loadServerConfig(func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	want := serverConfig{
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      90 * time.Second,
		IdleTimeout:       60 * time.Second,
		ShutdownGrace:     25 * time.Second,
	}
	if cfg != want {
		t.Fatalf("defaults = %+v, want %+v", cfg, want)
	}
}

func TestLoadServerConfigReadsAndRejectsValues(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		check   func(serverConfig) bool
		wantErr string
	}{
		{name: "overrides", env: map[string]string{"GHOSTLINE_READ_HEADER_TIMEOUT": "2s", "GHOSTLINE_READ_TIMEOUT": "10s", "GHOSTLINE_WRITE_TIMEOUT": "2m", "GHOSTLINE_IDLE_TIMEOUT": "30s", "GHOSTLINE_SHUTDOWN_GRACE": "8s"},
			check: func(c serverConfig) bool {
				return c.ReadHeaderTimeout == 2*time.Second && c.ReadTimeout == 10*time.Second && c.WriteTimeout == 2*time.Minute && c.IdleTimeout == 30*time.Second && c.ShutdownGrace == 8*time.Second
			}},
		{name: "not a duration", env: map[string]string{"GHOSTLINE_READ_TIMEOUT": "15"}, wantErr: `GHOSTLINE_READ_TIMEOUT must be a positive duration such as 15s, got "15"`},
		{name: "zero", env: map[string]string{"GHOSTLINE_IDLE_TIMEOUT": "0s"}, wantErr: "GHOSTLINE_IDLE_TIMEOUT must be a positive duration"},
		{name: "negative", env: map[string]string{"GHOSTLINE_SHUTDOWN_GRACE": "-1s"}, wantErr: "GHOSTLINE_SHUTDOWN_GRACE must be a positive duration"},
		{name: "header timeout longer than read timeout", env: map[string]string{"GHOSTLINE_READ_HEADER_TIMEOUT": "20s"}, wantErr: "GHOSTLINE_READ_HEADER_TIMEOUT (20s) must not exceed GHOSTLINE_READ_TIMEOUT (15s)"},
		{name: "a request body could still arrive after the answer is due", env: map[string]string{"GHOSTLINE_READ_TIMEOUT": "30s", "GHOSTLINE_WRITE_TIMEOUT": "10s"}, wantErr: "GHOSTLINE_READ_TIMEOUT (30s) must be at most GHOSTLINE_WRITE_TIMEOUT (10s) minus 5s"},
		{name: "read timeout just fits", env: map[string]string{"GHOSTLINE_READ_TIMEOUT": "10s", "GHOSTLINE_WRITE_TIMEOUT": "15s"},
			check: func(c serverConfig) bool { return c.ReadTimeout == 10*time.Second && c.WriteTimeout == 15*time.Second }},
		{name: "write timeout leaves no room to answer", env: map[string]string{"GHOSTLINE_WRITE_TIMEOUT": "5s"}, wantErr: "GHOSTLINE_WRITE_TIMEOUT must be at least 10s"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := loadServerConfig(func(key string) string { return tt.env[key] })
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want it to contain %q", err, tt.wantErr)
				}
				return
			}
			if err != nil || !tt.check(cfg) {
				t.Fatalf("cfg = %+v, err = %v", cfg, err)
			}
		})
	}
}

func TestNewHTTPServerAppliesTheLimits(t *testing.T) {
	cfg := serverConfig{ReadHeaderTimeout: 1 * time.Second, ReadTimeout: 2 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 4 * time.Second, ShutdownGrace: time.Second}
	srv := newHTTPServer("127.0.0.1:0", http.NotFoundHandler(), cfg)
	if srv.ReadHeaderTimeout != cfg.ReadHeaderTimeout || srv.ReadTimeout != cfg.ReadTimeout || srv.WriteTimeout != cfg.WriteTimeout || srv.IdleTimeout != cfg.IdleTimeout {
		t.Fatalf("server timeouts = %v %v %v %v", srv.ReadHeaderTimeout, srv.ReadTimeout, srv.WriteTimeout, srv.IdleTimeout)
	}
	if srv.MaxHeaderBytes != maxHeaderBytes {
		t.Fatalf("MaxHeaderBytes = %d, want %d", srv.MaxHeaderBytes, maxHeaderBytes)
	}
}

func TestRequestsGetADeadlineBeforeTheWriteTimeout(t *testing.T) {
	cfg := serverConfig{ReadHeaderTimeout: time.Second, ReadTimeout: time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: time.Second, ShutdownGrace: time.Second}
	var remaining time.Duration
	srv := newHTTPServer("127.0.0.1:0", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		deadline, ok := r.Context().Deadline()
		if !ok {
			t.Error("request has no deadline")
			return
		}
		remaining = time.Until(deadline)
	}), cfg)
	srv.Handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	if want := cfg.WriteTimeout - requestDeadlineMargin; remaining > want || remaining < want-time.Second {
		t.Fatalf("request deadline in %v, want about %v", remaining, want)
	}
}

func TestGitHubDeadlineAnswersGatewayTimeout(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	respondGitHubError(c, context.DeadlineExceeded)
	if recorder.Code != http.StatusGatewayTimeout || !strings.Contains(recorder.Body.String(), "GitHub took too long") {
		t.Fatalf("got %d %s", recorder.Code, recorder.Body.String())
	}
}

// startServer runs serveUntilSignal on a loopback listener and returns its address,
// the signal channel it watches, and a channel carrying its result.
func startServer(t *testing.T, handler http.Handler, cfg serverConfig) (string, chan os.Signal, chan error) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := newHTTPServer(listener.Addr().String(), handler, cfg)
	signals := make(chan os.Signal, 2)
	result := make(chan error, 1)
	go func() {
		result <- serveUntilSignal(srv, listener, signals, cfg.ShutdownGrace, slog.New(slog.DiscardHandler))
	}()
	t.Cleanup(func() { _ = srv.Close() })
	return listener.Addr().String(), signals, result
}

func testServerConfig() serverConfig {
	return serverConfig{ReadHeaderTimeout: 200 * time.Millisecond, ReadTimeout: time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: time.Second, ShutdownGrace: 5 * time.Second}
}

func TestSlowRequestHeaderIsCutOff(t *testing.T) {
	// ReadTimeout also bounds header reads, so keep it well above ReadHeaderTimeout
	// to show that the header timeout is what closes the connection.
	cfg := testServerConfig()
	cfg.ReadTimeout = 5 * time.Second
	address, _, _ := startServer(t, http.NotFoundHandler(), cfg)
	conn, err := net.Dial("tcp", address)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := io.WriteString(conn, "GET / HTTP/1.1\r\nHost: ghostline\r\nX-Slow: "); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(4 * time.Second))
	started := time.Now()
	_, err = io.ReadAll(conn)
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		t.Fatal("server kept the connection with an unfinished header open")
	}
	if elapsed := time.Since(started); elapsed > 1500*time.Millisecond {
		t.Fatalf("connection closed after %v, want about the 200ms header timeout", elapsed)
	}
}

// blockingHandler answers once release closes, and reports each request on started.
func blockingHandler(started chan<- struct{}, release <-chan struct{}) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started <- struct{}{}
		select {
		case <-release:
			_, _ = io.WriteString(w, "finished")
		case <-r.Context().Done():
		}
	})
}

func TestShutdownLetsAnInFlightRequestFinish(t *testing.T) {
	started, release := make(chan struct{}, 1), make(chan struct{})
	address, signals, result := startServer(t, blockingHandler(started, release), testServerConfig())

	response := make(chan string, 1)
	go func() {
		resp, err := http.Get("http://" + address + "/")
		if err != nil {
			response <- "error: " + err.Error()
			return
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		response <- resp.Status + " " + string(body)
	}()
	<-started
	signals <- syscall.SIGTERM

	// The listener closes at once, so no new connection is accepted.
	deadline := time.Now().Add(2 * time.Second)
	for {
		conn, err := net.DialTimeout("tcp", address, 100*time.Millisecond)
		if err != nil {
			break
		}
		conn.Close()
		if time.Now().After(deadline) {
			t.Fatal("server still accepts connections after SIGTERM")
		}
		time.Sleep(20 * time.Millisecond)
	}

	close(release)
	if got := <-response; got != "200 OK finished" {
		t.Fatalf("in-flight response = %q, want 200 OK finished", got)
	}
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("serveUntilSignal = %v, want nil for a clean shutdown", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("server did not exit after its last request finished")
	}
}

func TestShutdownGraceDeadlineForcesExit(t *testing.T) {
	cfg := testServerConfig()
	cfg.ShutdownGrace = 200 * time.Millisecond
	started := make(chan struct{}, 1)
	address, signals, result := startServer(t, blockingHandler(started, make(chan struct{})), cfg)
	go func() {
		if resp, err := http.Get("http://" + address + "/"); err == nil {
			resp.Body.Close()
		}
	}()
	<-started
	signals <- syscall.SIGTERM
	select {
	case err := <-result:
		if !errors.Is(err, errShutdownDeadline) {
			t.Fatalf("serveUntilSignal = %v, want errShutdownDeadline", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("server outlived its shutdown grace period")
	}
}

func TestSecondSignalForcesExit(t *testing.T) {
	cfg := testServerConfig()
	cfg.ShutdownGrace = time.Minute
	started := make(chan struct{}, 1)
	address, signals, result := startServer(t, blockingHandler(started, make(chan struct{})), cfg)
	go func() {
		if resp, err := http.Get("http://" + address + "/"); err == nil {
			resp.Body.Close()
		}
	}()
	<-started
	signals <- syscall.SIGTERM
	signals <- syscall.SIGINT
	select {
	case err := <-result:
		if !errors.Is(err, errShutdownForced) {
			t.Fatalf("serveUntilSignal = %v, want errShutdownForced", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("a second signal did not force exit")
	}
}

func TestServeErrorIsReturned(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	listener.Close()
	srv := newHTTPServer(listener.Addr().String(), http.NotFoundHandler(), testServerConfig())
	if err := serveUntilSignal(srv, listener, make(chan os.Signal), time.Second, slog.New(slog.DiscardHandler)); err == nil {
		t.Fatal("serveUntilSignal on a closed listener returned nil")
	}
}

// The hosted container must give the process longer than its shutdown grace period
// before Docker sends SIGKILL; Docker's default stop timeout is only 10s.
func TestHostedComposeStopTimeoutCoversShutdownGrace(t *testing.T) {
	compose, err := os.ReadFile("../compose.hosted.yml")
	if err != nil {
		t.Fatal(err)
	}
	match := regexp.MustCompile(`(?m)^\s+stop_grace_period:\s*(\S+)\s*$`).FindSubmatch(compose)
	if match == nil {
		t.Fatal("compose.hosted.yml sets no stop_grace_period")
	}
	stop, err := time.ParseDuration(string(match[1]))
	if err != nil {
		t.Fatal(err)
	}
	cfg, _ := loadServerConfig(func(string) string { return "" })
	if stop < cfg.ShutdownGrace+5*time.Second {
		t.Fatalf("stop_grace_period %s leaves less than 5s beyond the %s shutdown grace", stop, cfg.ShutdownGrace)
	}
}

// requestWithDeadline is a JSON POST whose context ends soon, like a request whose
// server-side deadline (see newHTTPServer) passes while the handler waits upstream.
func requestWithDeadline(t *testing.T, path, body string) *http.Request {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	t.Cleanup(cancel)
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)).WithContext(ctx)
	request.Header.Set("Content-Type", "application/json")
	return request
}

func TestAnnotationDeadlineAnswersGatewayTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/acme/app" {
			_, _ = w.Write([]byte(`{"private":false}`))
			return
		}
		<-r.Context().Done()
	}))
	defer server.Close()
	gin.SetMode(gin.TestMode)
	router := newRouter(&githubClient{baseURL: server.URL, token: "test-token", http: server.Client()})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, requestWithDeadline(t, "/api/check-annotations", `{"url":"https://github.com/acme/app/pull/7","sha":"`+headSHA+`","checkRunId":314,"page":1}`))
	if recorder.Code != http.StatusGatewayTimeout || !strings.Contains(recorder.Body.String(), "GitHub took too long") {
		t.Fatalf("got %d %s, want 504 naming the GitHub timeout", recorder.Code, recorder.Body.String())
	}
}

func TestQodoDeadlineAnswersGatewayTimeout(t *testing.T) {
	router := newRouterWithQodoAndLimiter(&githubClient{baseURL: "https://api.github.com", http: http.DefaultClient}, qodoSourceFunc(func(ctx context.Context, _, _ string) (qodoReviewData, error) {
		<-ctx.Done()
		return qodoReviewData{}, fmt.Errorf("call Qodo: %w", ctx.Err())
	}), newRateLimiter(defaultRateLimitRequests, defaultRateLimitWindow, time.Now), clientIdentity{})
	request := requestWithDeadline(t, "/api/qodo-review", `{"url":"https://github.com/acme/payments/pull/42"}`)
	request.Header.Set("Origin", "https://ghostline.example")
	request.Header.Set("X-Qodo-API-Key", "qodo-secret")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusGatewayTimeout || !strings.Contains(recorder.Body.String(), "Qodo took too long") {
		t.Fatalf("got %d %s, want 504 naming the Qodo timeout", recorder.Code, recorder.Body.String())
	}
}
