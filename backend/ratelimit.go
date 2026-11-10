package main

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
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

// clientIdentity decides which address a request is limited as.
type clientIdentity struct {
	// Header names a header carrying exactly one client IP. It is read only
	// when the TCP peer is inside TrustedProxies, so callers cannot pick their
	// own identity; that proxy must overwrite the header, not append to it.
	Header         string
	TrustedProxies []netip.Prefix
}

// rateLimitConfig is the validated startup configuration for per-client limiting.
type rateLimitConfig struct {
	Requests int
	Window   time.Duration
	Identity clientIdentity
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
	identity, err := loadClientIdentity(getenv)
	if err != nil {
		return rateLimitConfig{}, err
	}
	cfg.Identity = identity
	return cfg, nil
}

func loadClientIdentity(getenv func(string) string) (clientIdentity, error) {
	header := strings.TrimSpace(getenv("RATE_LIMIT_CLIENT_IP_HEADER"))
	proxies := strings.TrimSpace(getenv("RATE_LIMIT_TRUSTED_PROXIES"))
	if header == "" && proxies == "" {
		return clientIdentity{}, nil
	}
	if header == "" || proxies == "" {
		return clientIdentity{}, errors.New("RATE_LIMIT_CLIENT_IP_HEADER and RATE_LIMIT_TRUSTED_PROXIES must be set together")
	}
	if !headerName.MatchString(header) {
		return clientIdentity{}, errors.New("RATE_LIMIT_CLIENT_IP_HEADER must be a valid HTTP header name")
	}
	identity := clientIdentity{Header: header}
	for _, raw := range strings.Split(proxies, ",") {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(raw))
		if err != nil {
			return clientIdentity{}, fmt.Errorf("RATE_LIMIT_TRUSTED_PROXIES must be comma-separated CIDR ranges, got %q", strings.TrimSpace(raw))
		}
		identity.TrustedProxies = append(identity.TrustedProxies, prefix.Masked())
	}
	return identity, nil
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

// clientKey derives the rate-limit identity: the TCP peer, or the configured
// header's single IP when the peer is a trusted proxy.
func clientKey(r *http.Request, identity clientIdentity) string {
	peer := peerAddr(r.RemoteAddr)
	if identity.Header != "" && peer.IsValid() && identity.trusts(peer) {
		values := r.Header.Values(identity.Header)
		if len(values) == 1 {
			if addr, err := netip.ParseAddr(strings.TrimSpace(values[0])); err == nil {
				return addr.Unmap().String()
			}
		}
	}
	if peer.IsValid() {
		return peer.String()
	}
	return r.RemoteAddr
}

func (i clientIdentity) trusts(addr netip.Addr) bool {
	for _, prefix := range i.TrustedProxies {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

func peerAddr(remote string) netip.Addr {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		host = remote
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}
	}
	return addr.Unmap()
}

func rateLimitMiddleware(l *rateLimiter, identity clientIdentity) gin.HandlerFunc {
	return func(c *gin.Context) {
		allowed, retryAfter := l.allow(clientKey(c.Request, identity))
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
