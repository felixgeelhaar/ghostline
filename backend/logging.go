package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

const requestIDHeader = "X-Request-ID"

// inboundRequestID limits a proxy-supplied ID to a short token, so it cannot add log
// fields or lines.
var inboundRequestID = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)

type requestLoggerKey struct{}

// newLogger writes JSON in production, for log collectors, and readable text otherwise.
func newLogger(getenv func(string) string, w io.Writer) *slog.Logger {
	if strings.EqualFold(strings.TrimSpace(getenv("GHOSTLINE_ENV")), "production") {
		return slog.New(slog.NewJSONHandler(w, nil))
	}
	return slog.New(slog.NewTextHandler(w, nil))
}

// withRequestLogger attaches the request's logger, which carries its request ID.
func withRequestLogger(ctx context.Context, logger *slog.Logger) context.Context {
	return context.WithValue(ctx, requestLoggerKey{}, logger)
}

// requestLogger returns the logger attached to ctx, or fallback outside a request.
func requestLogger(ctx context.Context, fallback *slog.Logger) *slog.Logger {
	if logger, ok := ctx.Value(requestLoggerKey{}).(*slog.Logger); ok {
		return logger
	}
	if fallback != nil {
		return fallback
	}
	return slog.Default()
}

// requestLogging gives each request an ID, echoes it as X-Request-ID, attaches a
// logger carrying it, and writes one access line. The line names the route template,
// never the raw path or query, and no headers, so tokens, cookies and Qodo keys stay out.
func requestLogging(base *slog.Logger, identity clientIdentity) gin.HandlerFunc {
	return func(c *gin.Context) {
		started := time.Now()
		id := requestID(c.Request, identity)
		c.Header(requestIDHeader, id)
		logger := base.With("request_id", id)
		c.Request = c.Request.WithContext(withRequestLogger(c.Request.Context(), logger))

		c.Next()

		route := c.FullPath()
		if route == "" {
			route = "unmatched"
		}
		logger.LogAttrs(c.Request.Context(), slog.LevelInfo, "request",
			slog.String("method", c.Request.Method),
			slog.String("route", route),
			slog.Int("status", c.Writer.Status()),
			slog.Float64("duration_ms", float64(time.Since(started).Microseconds())/1000),
		)
	}
}

// requestID keeps an inbound X-Request-ID only from a trusted proxy, the same rule as
// the client-IP header; otherwise it generates one.
func requestID(r *http.Request, identity clientIdentity) string {
	if peer := peerAddr(r.RemoteAddr); peer.IsValid() && identity.trusts(peer) {
		if values := r.Header.Values(requestIDHeader); len(values) == 1 && inboundRequestID.MatchString(values[0]) {
			return values[0]
		}
	}
	var raw [16]byte
	_, _ = rand.Read(raw[:]) // never returns an error since Go 1.24
	return hex.EncodeToString(raw[:])
}

// logUpstreamFailure records a failed GitHub or Qodo call with the request's ID. It
// logs the API path without its query, the upstream status when one was received, and
// a failure kind; never response bodies, headers or raw transport errors, which can
// carry signed download URLs.
func logUpstreamFailure(ctx context.Context, upstream, path string, status int, failure string) {
	attrs := []slog.Attr{
		slog.String("upstream", upstream),
		slog.String("path", pathWithoutQuery(path)),
		slog.String("failure", failure),
	}
	// Only a 404 is routine (some lookups probe for one). A rejected token, quota or rate
	// limit (401, 403, 429) is an operator problem, like any 5xx or failed read.
	level := slog.LevelWarn
	if status != 0 {
		attrs = append(attrs, slog.Int("upstream_status", status))
		if failure == "status" && status == http.StatusNotFound {
			level = slog.LevelInfo
		}
	}
	requestLogger(ctx, nil).LogAttrs(ctx, level, "upstream request failed", attrs...)
}

// transportFailure classifies an error from sending a request.
func transportFailure(ctx context.Context, err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded):
		return "deadline"
	case errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled):
		return "canceled"
	default:
		return "transport"
	}
}

// bodyFailure classifies an error from reading a response body, not from decoding it:
// the body stopped arriving because of a timeout, cancellation or dropped connection.
// Callers read the body fully before decoding, so a decode error is "invalid_response".
func bodyFailure(ctx context.Context, err error) string {
	if ctx.Err() != nil {
		return transportFailure(ctx, err)
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "deadline"
	}
	return "transport"
}

// readErrorRecorder passes reads through and keeps the first read error other than EOF,
// so a decode failure can be traced to a body that stopped arriving.
type readErrorRecorder struct {
	r   io.Reader
	err error
}

func (rec *readErrorRecorder) Read(p []byte) (int, error) {
	n, err := rec.r.Read(p)
	if err != nil && err != io.EOF && rec.err == nil {
		rec.err = err
	}
	return n, err
}

func pathWithoutQuery(path string) string {
	if i := strings.IndexAny(path, "?#"); i >= 0 {
		return path[:i]
	}
	return path
}
