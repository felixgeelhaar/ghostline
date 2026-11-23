package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// countingReader records how many bytes a handler pulled from a request body.
type countingReader struct {
	r    io.Reader
	read int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.read += int64(n)
	return n, err
}

// oversizedJSON is a syntactically valid JSON body larger than any API request needs.
func oversizedJSON() []byte {
	return []byte(`{"url":"https://github.com/o/r/pull/1","pad":"` + strings.Repeat("a", 4*maxAPIRequestBytes) + `"}`)
}

var jsonAPIEndpoints = []string{"/api/import", "/api/checks", "/api/readiness", "/api/check-annotations", "/api/nox-report", "/api/qodo-review", "/api/pr-search"}

func TestAPIRefusesADeclaredOversizedBody(t *testing.T) {
	for _, path := range jsonAPIEndpoints {
		t.Run(path, func(t *testing.T) {
			var githubCalls atomic.Int32
			github := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				githubCalls.Add(1)
				http.Error(w, "unexpected", http.StatusTeapot)
			}))
			defer github.Close()
			router := newRouter(&githubClient{baseURL: github.URL, http: github.Client()})

			body := &countingReader{r: bytes.NewReader(oversizedJSON())}
			req := httptest.NewRequest(http.MethodPost, path, body)
			req.ContentLength = int64(len(oversizedJSON()))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != http.StatusRequestEntityTooLarge {
				t.Fatalf("status = %d, want 413; body %s", rec.Code, rec.Body.String())
			}
			if body.read != 0 {
				t.Fatalf("handler read %d bytes of a body it should refuse unread", body.read)
			}
			if githubCalls.Load() != 0 {
				t.Fatalf("GitHub was called %d times for a refused request", githubCalls.Load())
			}
		})
	}
}

func TestAPIStopsReadingAnUndeclaredOversizedBody(t *testing.T) {
	for _, path := range jsonAPIEndpoints {
		t.Run(path, func(t *testing.T) {
			var githubCalls atomic.Int32
			github := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				githubCalls.Add(1)
				http.Error(w, "unexpected", http.StatusTeapot)
			}))
			defer github.Close()
			router := newRouter(&githubClient{baseURL: github.URL, http: github.Client()})

			body := &countingReader{r: bytes.NewReader(oversizedJSON())}
			req := httptest.NewRequest(http.MethodPost, path, body)
			req.ContentLength = -1 // chunked upload: the size is not declared up front
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code < 400 || rec.Code >= 500 {
				t.Fatalf("status = %d, want a 4xx refusal; body %s", rec.Code, rec.Body.String())
			}
			// MaxBytesReader may read one buffer past the limit before it reports the overflow.
			if body.read > 2*maxAPIRequestBytes {
				t.Fatalf("handler read %d bytes; the limit is %d", body.read, maxAPIRequestBytes)
			}
			if githubCalls.Load() != 0 {
				t.Fatalf("GitHub was called %d times for a refused request", githubCalls.Load())
			}
		})
	}
}

func TestAPIStillAcceptsAnOrdinaryRequest(t *testing.T) {
	github := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
	}))
	defer github.Close()
	router := newRouter(&githubClient{baseURL: github.URL, http: github.Client()})

	req := httptest.NewRequest(http.MethodPost, "/api/import", strings.NewReader(`{"url":"https://github.com/o/r/pull/1"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code == http.StatusRequestEntityTooLarge || rec.Code == http.StatusBadRequest {
		t.Fatalf("an ordinary import was refused before reaching GitHub: %d %s", rec.Code, rec.Body.String())
	}
}

func TestRefusedOversizedRequestsStillCountTowardTheRateLimit(t *testing.T) {
	github := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
	}))
	defer github.Close()
	limiter := newRateLimiter(1, time.Minute, time.Now)
	router := newRouterWithLimiter(&githubClient{baseURL: github.URL, http: github.Client()}, limiter, clientIdentity{})

	oversized := httptest.NewRequest(http.MethodPost, "/api/import", bytes.NewReader(oversizedJSON()))
	oversized.Header.Set("Content-Type", "application/json")
	first := httptest.NewRecorder()
	router.ServeHTTP(first, oversized)
	if first.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("first status = %d, want 413", first.Code)
	}

	ordinary := httptest.NewRequest(http.MethodPost, "/api/import", strings.NewReader(`{"url":"https://github.com/o/r/pull/1"}`))
	ordinary.Header.Set("Content-Type", "application/json")
	second := httptest.NewRecorder()
	router.ServeHTTP(second, ordinary)
	if second.Code != http.StatusTooManyRequests {
		t.Fatalf("second status = %d, want 429: the refused oversized request must use up the allowance", second.Code)
	}
}
