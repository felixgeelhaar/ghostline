package main

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	defaultRateLimitRequests = 30
	defaultRateLimitWindow   = time.Minute
)

var headerName = regexp.MustCompile(`^[A-Za-z0-9-]+$`)

// rateLimitConfig is the validated startup configuration for per-client limiting.
type rateLimitConfig struct {
	Requests int
	Window   time.Duration
	// ClientIPHeader names a header set by a proxy you operate. Empty means the
	// TCP peer address is the only client identity; forwarded headers are ignored.
	ClientIPHeader string
}

func loadRateLimitConfig(getenv func(string) string) (rateLimitConfig, error) {
	cfg := rateLimitConfig{Requests: defaultRateLimitRequests, Window: defaultRateLimitWindow}
	if raw := strings.TrimSpace(getenv("RATE_LIMIT_REQUESTS")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			return rateLimitConfig{}, fmt.Errorf("RATE_LIMIT_REQUESTS must be a positive integer, got %q", raw)
		}
		cfg.Requests = n
	}
	if raw := strings.TrimSpace(getenv("RATE_LIMIT_WINDOW")); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil || d <= 0 {
			return rateLimitConfig{}, fmt.Errorf("RATE_LIMIT_WINDOW must be a positive duration such as 1m, got %q", raw)
		}
		cfg.Window = d
	}
	if raw := strings.TrimSpace(getenv("RATE_LIMIT_CLIENT_IP_HEADER")); raw != "" {
		if !headerName.MatchString(raw) {
			return rateLimitConfig{}, errors.New("RATE_LIMIT_CLIENT_IP_HEADER must be a valid HTTP header name")
		}
		cfg.ClientIPHeader = raw
	}
	return cfg, nil
}

type clientWindow struct {
	count   int
	resetAt time.Time
}

// rateLimiter is an in-memory fixed-window limiter keyed by client identity.
// State is per process and is lost on restart.
type rateLimiter struct {
	mu        sync.Mutex
	limit     int
	window    time.Duration
	now       func() time.Time
	clients   map[string]*clientWindow
	nextSweep time.Time
}

func newRateLimiter(limit int, window time.Duration, now func() time.Time) *rateLimiter {
	return &rateLimiter{limit: limit, window: window, now: now, clients: map[string]*clientWindow{}}
}

// allow records a request and reports whether it is within the limit. When it is
// not, retryAfter is the time until the client's window resets.
func (l *rateLimiter) allow(client string) (allowed bool, retryAfter time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	l.sweep(now)
	w, ok := l.clients[client]
	if !ok || !now.Before(w.resetAt) {
		w = &clientWindow{resetAt: now.Add(l.window)}
		l.clients[client] = w
	}
	if w.count >= l.limit {
		return false, w.resetAt.Sub(now)
	}
	w.count++
	return true, 0
}

// sweep drops expired windows so memory stays bounded by active clients.
func (l *rateLimiter) sweep(now time.Time) {
	if now.Before(l.nextSweep) {
		return
	}
	for key, w := range l.clients {
		if !now.Before(w.resetAt) {
			delete(l.clients, key)
		}
	}
	l.nextSweep = now.Add(l.window)
}

func (l *rateLimiter) size() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.clients)
}

// clientKey derives the rate-limit identity. Forwarded headers are spoofable by
// callers, so they are only read when an operator names a trusted header, and
// then only the rightmost entry (appended by the nearest proxy) is used.
func clientKey(r *http.Request, trustedHeader string) string {
	if trustedHeader != "" {
		values := r.Header.Values(trustedHeader)
		if len(values) > 0 {
			parts := strings.Split(values[len(values)-1], ",")
			if ip := net.ParseIP(strings.TrimSpace(parts[len(parts)-1])); ip != nil {
				return ip.String()
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func rateLimitMiddleware(l *rateLimiter, trustedHeader string) gin.HandlerFunc {
	return func(c *gin.Context) {
		allowed, retryAfter := l.allow(clientKey(c.Request, trustedHeader))
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
