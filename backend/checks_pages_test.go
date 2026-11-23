package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// endlessChecksGitHub answers every check-run and status page with a full page
// and a total that never runs out, as a revision with a huge history would.
func endlessChecksGitHub(t *testing.T, sha string, endless string, runPages, statusPages *atomic.Int32) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/check-runs"):
			runPages.Add(1)
			n := 0
			if endless == "runs" {
				n = checksPageSize
			}
			runs := make([]map[string]any, n)
			for i := range runs {
				runs[i] = map[string]any{"id": int64(runPages.Load())*1000 + int64(i) + 1, "name": fmt.Sprintf("run-%d", i), "head_sha": sha, "status": "completed"}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"total_count": 1_000_000, "check_runs": runs})
		case strings.HasSuffix(r.URL.Path, "/status"):
			statusPages.Add(1)
			n := 0
			if endless == "statuses" {
				n = checksPageSize
			}
			statuses := make([]map[string]any, n)
			for i := range statuses {
				statuses[i] = map[string]any{"id": int64(statusPages.Load())*1000 + int64(i) + 1, "context": fmt.Sprintf("ci-%d", i), "state": "success"}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"sha": sha, "total_count": 1_000_000, "statuses": statuses})
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestRevisionChecksStopAtAPageLimit(t *testing.T) {
	sha := strings.Repeat("a", 40)
	for _, endless := range []string{"runs", "statuses"} {
		t.Run(endless, func(t *testing.T) {
			var runPages, statusPages atomic.Int32
			github := endlessChecksGitHub(t, sha, endless, &runPages, &statusPages)
			defer github.Close()

			got := fetchRevisionChecks(context.Background(), &githubClient{baseURL: github.URL, http: github.Client()}, "o", "r", sha)

			pages := runPages.Load()
			if endless == "statuses" {
				pages = statusPages.Load()
			}
			if pages > maxRevisionCheckPages {
				t.Fatalf("read %d pages of %s; the limit is %d", pages, endless, maxRevisionCheckPages)
			}
			if got.Availability != checksUnavailable || len(got.Items) != 0 {
				t.Fatalf("availability = %q with %d items; want unavailable and no partial list", got.Availability, len(got.Items))
			}
			if !strings.Contains(got.Reason, "more checks than Ghostline reads") {
				t.Fatalf("reason = %q; want it to say the revision has more checks than Ghostline reads", got.Reason)
			}
		})
	}
}
