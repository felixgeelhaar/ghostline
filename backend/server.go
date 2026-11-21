package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	// maxHeaderBytes covers the session cookie and ordinary browser headers.
	maxHeaderBytes = 64 << 10
	// requestDeadlineMargin is the time a handler keeps, after its context expires,
	// to write an error before the server's write deadline closes the connection.
	requestDeadlineMargin = 5 * time.Second
	minWriteTimeout       = 2 * requestDeadlineMargin
)

var (
	errShutdownDeadline = errors.New("shutdown grace period ended before in-flight requests finished")
	errShutdownForced   = errors.New("a second signal forced shutdown before in-flight requests finished")
)

// serverConfig holds the HTTP server's connection limits and shutdown grace period.
type serverConfig struct {
	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	ShutdownGrace     time.Duration
}

// loadServerConfig reads the server limits from the environment. The 90s write
// timeout suits the slowest handler, the Nox report, which reads several GitHub
// API pages and downloads an artifact of up to 10 MiB.
func loadServerConfig(getenv func(string) string) (serverConfig, error) {
	cfg := serverConfig{
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      90 * time.Second,
		IdleTimeout:       60 * time.Second,
		ShutdownGrace:     25 * time.Second,
	}
	for _, setting := range []struct {
		name  string
		value *time.Duration
	}{
		{"GHOSTLINE_READ_HEADER_TIMEOUT", &cfg.ReadHeaderTimeout},
		{"GHOSTLINE_READ_TIMEOUT", &cfg.ReadTimeout},
		{"GHOSTLINE_WRITE_TIMEOUT", &cfg.WriteTimeout},
		{"GHOSTLINE_IDLE_TIMEOUT", &cfg.IdleTimeout},
		{"GHOSTLINE_SHUTDOWN_GRACE", &cfg.ShutdownGrace},
	} {
		raw := strings.TrimSpace(getenv(setting.name))
		if raw == "" {
			continue
		}
		d, err := time.ParseDuration(raw)
		if err != nil || d <= 0 {
			return serverConfig{}, fmt.Errorf("%s must be a positive duration such as 15s, got %q", setting.name, raw)
		}
		*setting.value = d
	}
	if cfg.ReadHeaderTimeout > cfg.ReadTimeout {
		return serverConfig{}, fmt.Errorf("GHOSTLINE_READ_HEADER_TIMEOUT (%s) must not exceed GHOSTLINE_READ_TIMEOUT (%s)", cfg.ReadHeaderTimeout, cfg.ReadTimeout)
	}
	if cfg.WriteTimeout < minWriteTimeout {
		return serverConfig{}, fmt.Errorf("GHOSTLINE_WRITE_TIMEOUT must be at least %s so a handler has time to answer, got %s", minWriteTimeout, cfg.WriteTimeout)
	}
	// The write deadline starts once the headers are read, so a body still arriving
	// near it leaves the handler no time to answer.
	if cfg.ReadTimeout > cfg.WriteTimeout-requestDeadlineMargin {
		return serverConfig{}, fmt.Errorf("GHOSTLINE_READ_TIMEOUT (%s) must be at most GHOSTLINE_WRITE_TIMEOUT (%s) minus %s, so a request is read before its answer is due", cfg.ReadTimeout, cfg.WriteTimeout, requestDeadlineMargin)
	}
	return cfg, nil
}

// newHTTPServer serves handler with the configured limits. Each request's context
// ends requestDeadlineMargin before the write timeout, so outgoing GitHub calls stop
// in time for the handler to report the failure instead of losing the connection.
func newHTTPServer(address string, handler http.Handler, cfg serverConfig) *http.Server {
	requestTimeout := cfg.WriteTimeout - requestDeadlineMargin
	return &http.Server{
		Addr: address,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
			defer cancel()
			handler.ServeHTTP(w, r.WithContext(ctx))
		}),
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
		ReadTimeout:       cfg.ReadTimeout,
		WriteTimeout:      cfg.WriteTimeout,
		IdleTimeout:       cfg.IdleTimeout,
		MaxHeaderBytes:    maxHeaderBytes,
	}
}

// serveUntilSignal serves until the first signal, then stops accepting connections
// and waits up to grace for in-flight requests. It returns nil after a clean
// shutdown; a second signal or the end of the grace period closes the remaining
// connections and returns an error.
func serveUntilSignal(srv *http.Server, listener net.Listener, signals <-chan os.Signal, grace time.Duration, logger *slog.Logger) error {
	served := make(chan error, 1)
	go func() { served <- srv.Serve(listener) }()

	var received os.Signal
	select {
	case err := <-served:
		return err
	case received = <-signals:
	}
	logger.Info("shutting down", "signal", received.String(), "grace", grace.String())

	ctx, cancel := context.WithTimeout(context.Background(), grace)
	defer cancel()
	shutdown := make(chan error, 1)
	go func() { shutdown <- srv.Shutdown(ctx) }()

	select {
	case err := <-shutdown:
		if errors.Is(err, context.DeadlineExceeded) {
			_ = srv.Close()
			return errShutdownDeadline
		}
		return err
	case <-signals:
		_ = srv.Close()
		return errShutdownForced
	}
}
