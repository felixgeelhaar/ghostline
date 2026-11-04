package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

const headSHA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const olderSHA = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

type fakeGitHub struct {
	headSHA              string
	checkRuns            string
	statuses             string
	checksCode           int
	statusCode           int
	rateLimited          bool
	secondaryRateLimited bool
}

func (f fakeGitHub) serve(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/acme/app/pulls/7", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"number":7,"title":"Add checks","state":"open","html_url":"https://github.com/acme/app/pull/7","created_at":"2026-11-01T10:00:00Z","updated_at":"2026-11-02T10:00:00Z","head":{"sha":"` + f.headSHA + `"}}`))
	})
	mux.HandleFunc("/repos/acme/app/pulls/7/commits", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"sha":"` + olderSHA + `","html_url":"https://github.com/acme/app/commit/` + olderSHA + `","commit":{"message":"first","author":{"name":"A","date":"2026-11-01T10:00:00Z"}}},{"sha":"` + headSHA + `","html_url":"https://github.com/acme/app/commit/` + headSHA + `","commit":{"message":"second","author":{"name":"A","date":"2026-11-02T10:00:00Z"}}}]`))
	})
	mux.HandleFunc("/repos/acme/app/commits/"+f.headSHA+"/check-runs", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("filter") != "latest" {
			t.Errorf("check runs requested without filter=latest: %s", r.URL.RawQuery)
		}
		if f.rateLimited {
			w.Header().Set("X-RateLimit-Remaining", "0")
			w.WriteHeader(http.StatusForbidden)
			return
		}
		if f.secondaryRateLimited {
			w.Header().Set("Retry-After", "30")
			w.WriteHeader(http.StatusForbidden)
			return
		}
		if f.checksCode != 0 {
			w.WriteHeader(f.checksCode)
			return
		}
		_, _ = w.Write([]byte(f.checkRuns))
	})
	mux.HandleFunc("/repos/acme/app/commits/"+f.headSHA+"/status", func(w http.ResponseWriter, r *http.Request) {
		if f.statusCode != 0 {
			w.WriteHeader(f.statusCode)
			return
		}
		_, _ = w.Write([]byte(f.statuses))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func importVia(t *testing.T, srv *httptest.Server) (int, pullRequest) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := newRouter(&githubClient{baseURL: srv.URL, http: srv.Client()})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/import", strings.NewReader(`{"url":"https://github.com/acme/app/pull/7"}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)
	var pr pullRequest
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &pr); err != nil {
			t.Fatalf("decode response: %v", err)
		}
	}
	return rec.Code, pr
}

func checkRunsJSON(runs ...string) string {
	return `{"total_count":` + itoa(len(runs)) + `,"check_runs":[` + strings.Join(runs, ",") + `]}`
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }

func checkRun(name, sha, status, conclusion string) string {
	c := "null"
	if conclusion != "" {
		c = `"` + conclusion + `"`
	}
	return `{"name":"` + name + `","head_sha":"` + sha + `","status":"` + status + `","conclusion":` + c + `,"html_url":"https://github.com/acme/app/runs/1","app":{"name":"GitHub Actions"}}`
}

const noStatuses = `{"sha":"` + headSHA + `","total_count":0,"statuses":[]}`

func TestImportReturnsChecksForHeadCommit(t *testing.T) {
	srv := fakeGitHub{
		headSHA:   headSHA,
		checkRuns: checkRunsJSON(checkRun("go", headSHA, "completed", "success"), checkRun("frontend", headSHA, "in_progress", "")),
		statuses:  `{"sha":"` + headSHA + `","total_count":2,"statuses":[{"context":"warden/gate","state":"pending","target_url":"https://ci.example/1"},{"context":"legacy","state":"failure","target_url":"javascript:alert(1)"}]}`,
	}.serve(t)

	code, pr := importVia(t, srv)
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	got := pr.Checks
	if got.HeadSHA != headSHA || got.HeadShortSHA != "aaaaaaa" || got.HeadURL != "https://github.com/acme/app/commit/"+headSHA {
		t.Fatalf("unexpected head revision: %+v", got)
	}
	if got.Availability != checksAvailable || got.Reason != "" {
		t.Fatalf("availability = %q (%q)", got.Availability, got.Reason)
	}
	want := []check{
		{Name: "frontend", Kind: kindCheckRun, Source: "GitHub Actions", Status: "in_progress", URL: "https://github.com/acme/app/runs/1"},
		{Name: "go", Kind: kindCheckRun, Source: "GitHub Actions", Status: "completed", Conclusion: "success", URL: "https://github.com/acme/app/runs/1"},
		{Name: "legacy", Kind: kindCommitStatus, Status: "completed", Conclusion: "failure"},
		{Name: "warden/gate", Kind: kindCommitStatus, Status: "pending", URL: "https://ci.example/1"},
	}
	if len(got.Items) != len(want) {
		t.Fatalf("items = %+v", got.Items)
	}
	for i := range want {
		if got.Items[i] != want[i] {
			t.Errorf("item %d = %+v, want %+v", i, got.Items[i], want[i])
		}
	}
}

func TestImportReportsMissingAndUnavailableChecks(t *testing.T) {
	tests := []struct {
		name             string
		gh               fakeGitHub
		wantAvailability string
		wantReason       string
	}{
		{"no checks", fakeGitHub{headSHA: headSHA, checkRuns: checkRunsJSON(), statuses: noStatuses}, checksAvailable, ""},
		{"rate limited", fakeGitHub{headSHA: headSHA, rateLimited: true, statuses: noStatuses}, checksUnavailable, "rate limit"},
		{"secondary rate limited", fakeGitHub{headSHA: headSHA, secondaryRateLimited: true, statuses: noStatuses}, checksUnavailable, "rate limit"},
		{"access denied", fakeGitHub{headSHA: headSHA, checksCode: http.StatusForbidden, statuses: noStatuses}, checksUnavailable, "denied access"},
		{"status endpoint down", fakeGitHub{headSHA: headSHA, checkRuns: checkRunsJSON(), statusCode: http.StatusBadGateway}, checksUnavailable, "could not be loaded"},
		{"check run for older commit", fakeGitHub{headSHA: headSHA, checkRuns: checkRunsJSON(checkRun("go", olderSHA, "completed", "success")), statuses: noStatuses}, checksUnavailable, "different commit"},
		{"status for older commit", fakeGitHub{headSHA: headSHA, checkRuns: checkRunsJSON(), statuses: `{"sha":"` + olderSHA + `","total_count":0,"statuses":[]}`}, checksUnavailable, "different commit"},
		{"malformed head", fakeGitHub{headSHA: "not-a-sha"}, checksUnavailable, "head commit"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, pr := importVia(t, tt.gh.serve(t))
			if code != http.StatusOK {
				t.Fatalf("status = %d", code)
			}
			if pr.Checks.Availability != tt.wantAvailability {
				t.Fatalf("availability = %q (%q)", pr.Checks.Availability, pr.Checks.Reason)
			}
			if !strings.Contains(pr.Checks.Reason, tt.wantReason) {
				t.Fatalf("reason = %q, want it to mention %q", pr.Checks.Reason, tt.wantReason)
			}
			if len(pr.Checks.Items) != 0 {
				t.Fatalf("items = %+v, want none", pr.Checks.Items)
			}
			if tt.name == "no checks" && pr.Checks.Items == nil {
				t.Fatal("items serialized as null; want an empty array")
			}
		})
	}
}

func TestImportRetriesWhenPullRequestHeadChanges(t *testing.T) {
	newHeadSHA := "cccccccccccccccccccccccccccccccccccccccc"
	prReads := 0
	commitReads := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/acme/app/pulls/7", func(w http.ResponseWriter, r *http.Request) {
		prReads++
		sha := headSHA
		if prReads >= 2 {
			sha = newHeadSHA
		}
		_, _ = w.Write([]byte(`{"number":7,"title":"Add checks","state":"open","html_url":"https://github.com/acme/app/pull/7","created_at":"2026-11-01T10:00:00Z","updated_at":"2026-11-02T10:00:00Z","head":{"sha":"` + sha + `"}}`))
	})
	mux.HandleFunc("/repos/acme/app/pulls/7/commits", func(w http.ResponseWriter, r *http.Request) {
		commitReads++
		sha := headSHA
		if commitReads >= 2 {
			sha = newHeadSHA
		}
		_, _ = w.Write([]byte(`[{"sha":"` + olderSHA + `","html_url":"https://github.com/acme/app/commit/` + olderSHA + `","commit":{"message":"first","author":{"name":"A","date":"2026-11-01T10:00:00Z"}}},{"sha":"` + sha + `","html_url":"https://github.com/acme/app/commit/` + sha + `","commit":{"message":"head","author":{"name":"A","date":"2026-11-02T10:00:00Z"}}}]`))
	})
	mux.HandleFunc("/repos/acme/app/commits/", func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/repos/acme/app/commits/"), "/")
		sha := parts[0]
		if len(parts) > 1 && parts[1] == "check-runs" {
			_, _ = w.Write([]byte(checkRunsJSON(checkRun("build", sha, "completed", "success"))))
			return
		}
		_, _ = w.Write([]byte(`{"sha":"` + sha + `","total_count":0,"statuses":[]}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	code, pr := importVia(t, srv)
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if pr.Checks.HeadSHA != newHeadSHA || pr.Commits[len(pr.Commits)-1].SHA != newHeadSHA {
		t.Fatalf("checks and timeline must describe the latest head: checks=%s commits=%+v", pr.Checks.HeadSHA, pr.Commits)
	}
	if prReads != 4 || commitReads != 2 {
		t.Fatalf("expected a single retry after head changed; PR reads=%d commit reads=%d", prReads, commitReads)
	}
}

func TestImportReturnsConflictWhenHeadKeepsChanging(t *testing.T) {
	prReads := 0
	commitReads := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/acme/app/pulls/7", func(w http.ResponseWriter, r *http.Request) {
		prReads++
		sha := headSHA
		if prReads%2 == 0 {
			sha = "cccccccccccccccccccccccccccccccccccccccc"
		}
		_, _ = w.Write([]byte(`{"number":7,"title":"Add checks","state":"open","html_url":"https://github.com/acme/app/pull/7","created_at":"2026-11-01T10:00:00Z","updated_at":"2026-11-02T10:00:00Z","head":{"sha":"` + sha + `"}}`))
	})
	mux.HandleFunc("/repos/acme/app/pulls/7/commits", func(w http.ResponseWriter, r *http.Request) {
		commitReads++
		sha := headSHA
		if commitReads == 2 {
			sha = "cccccccccccccccccccccccccccccccccccccccc"
		}
		_, _ = w.Write([]byte(`[{"sha":"` + sha + `","html_url":"https://github.com/acme/app/commit/` + sha + `","commit":{"message":"head","author":{"name":"A","date":"2026-11-02T10:00:00Z"}}}]`))
	})
	mux.HandleFunc("/repos/acme/app/commits/", func(w http.ResponseWriter, r *http.Request) {
		sha := strings.Split(strings.TrimPrefix(r.URL.Path, "/repos/acme/app/commits/"), "/")[0]
		if strings.HasSuffix(r.URL.Path, "/check-runs") {
			_, _ = w.Write([]byte(checkRunsJSON(checkRun("build", sha, "completed", "success"))))
			return
		}
		_, _ = w.Write([]byte(`{"sha":"` + sha + `","total_count":0,"statuses":[]}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	code, _ := importVia(t, srv)
	if code != http.StatusConflict {
		t.Fatalf("status = %d, want %d after two inconsistent snapshots", code, http.StatusConflict)
	}
}
