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
	qodoComments         string
}

func (f fakeGitHub) serve(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/acme/app/pulls/7", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"number":7,"title":"Add checks","state":"open","html_url":"https://github.com/acme/app/pull/7","created_at":"2026-11-01T10:00:00Z","updated_at":"2026-11-02T10:00:00Z","head":{"sha":"` + f.headSHA + `"},"base":{"repo":{"name":"app","owner":{"login":"acme"}}}}`))
	})
	mux.HandleFunc("/repos/acme/app/pulls/7/commits", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"sha":"` + olderSHA + `","html_url":"https://github.com/acme/app/commit/` + olderSHA + `","commit":{"message":"first","author":{"name":"A","date":"2026-11-01T10:00:00Z"}}},{"sha":"` + headSHA + `","html_url":"https://github.com/acme/app/commit/` + headSHA + `","commit":{"message":"second","author":{"name":"A","date":"2026-11-02T10:00:00Z"}}}]`))
	})
	mux.HandleFunc("/repos/acme/app/issues/7/comments", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("per_page") != "100" {
			t.Errorf("Qodo comments requested without page size: %s", r.URL.RawQuery)
		}
		comments := f.qodoComments
		if comments == "" {
			comments = `[]`
		}
		_, _ = w.Write([]byte(comments))
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

func TestImportIncludesQodoReviewEvidence(t *testing.T) {
	comments := `[{"id":17,"user":{"login":"qodo-code-review[bot]"},"created_at":"2026-11-04T09:00:00Z","html_url":"https://github.com/acme/app/pull/7#issuecomment-17","body":"<h3>Code Review by Qodo</h3><details><summary>1. Reject stale checks</summary></details><!-- https://github.com/acme/app/commit/` + headSHA + ` -->"}]`
	srv := fakeGitHub{headSHA: headSHA, checkRuns: checkRunsJSON(), statuses: noStatuses, qodoComments: comments}.serve(t)

	code, pr := importVia(t, srv)
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if pr.Reviews.Availability != reviewsAvailable || len(pr.Reviews.Items) != 1 || pr.Reviews.UnrecognizedCommentCount != 0 {
		t.Fatalf("Qodo reviews = %+v", pr.Reviews)
	}
	review := pr.Reviews.Items[0]
	if review.Provider != "Qodo" || review.SourceType != "github_issue_comment" || review.SourceAuthor != "qodo-code-review[bot]" || review.Kind != "findings" || review.CommitSHA != headSHA || review.URL != "https://github.com/acme/app/pull/7#issuecomment-17" || len(review.Findings) != 1 || review.Findings[0].Title != "Reject stale checks" || review.Findings[0].SourceURL != review.URL {
		t.Fatalf("unexpected review evidence: %+v", review)
	}
}

func TestImportReportsNoQodoReviewWhenNoCommentsExist(t *testing.T) {
	srv := fakeGitHub{headSHA: headSHA, checkRuns: checkRunsJSON(), statuses: noStatuses}.serve(t)

	code, pr := importVia(t, srv)
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if pr.Reviews.Availability != reviewsAvailable || pr.Reviews.Items == nil || len(pr.Reviews.Items) != 0 || pr.Reviews.UnrecognizedCommentCount != 0 {
		t.Fatalf("Qodo review state = %+v, want available empty evidence", pr.Reviews)
	}
}

func TestImportDistinguishesUnrecognizedQodoCommentsFromNoReview(t *testing.T) {
	comments := `[{"id":19,"user":{"login":"qodo-code-review[bot]"},"created_at":"2026-11-04T09:00:00Z","html_url":"https://github.com/acme/app/pull/7#issuecomment-19","body":"Qodo review output with an unfamiliar format"}]`
	srv := fakeGitHub{headSHA: headSHA, checkRuns: checkRunsJSON(), statuses: noStatuses, qodoComments: comments}.serve(t)

	code, pr := importVia(t, srv)
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if pr.Reviews.Availability != reviewsAvailable || len(pr.Reviews.Items) != 0 || pr.Reviews.UnrecognizedCommentCount != 1 {
		t.Fatalf("Qodo review state = %+v, want one unrecognized Qodo comment", pr.Reviews)
	}
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

func checkRunWithSummary(name, sha, summary string) string {
	run := checkRun(name, sha, "completed", "success")
	encodedSummary, _ := json.Marshal(summary)
	return strings.TrimSuffix(run, "}") + `,"output":{"summary":` + string(encodedSummary) + `}}`
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
	if got.RevisionSHA != headSHA || got.RevisionShortSHA != "aaaaaaa" || got.RevisionURL != "https://github.com/acme/app/commit/"+headSHA {
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

func TestImportPreservesGitHubCheckRunOutput(t *testing.T) {
	const title = "No new alerts in code changed by this pull request"
	const summary = "[View all branch alerts](/acme/app/security/code-scanning?query=pr%3A7+tool%3Anox+is%3Aopen)."
	run := strings.TrimSuffix(checkRun("nox", headSHA, "completed", "success"), "}") +
		`,"output":{"title":"` + title + `","summary":"` + summary + `"}}`
	srv := fakeGitHub{headSHA: headSHA, checkRuns: checkRunsJSON(run), statuses: noStatuses}.serve(t)

	code, pr := importVia(t, srv)
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if len(pr.Checks.Items) != 1 || pr.Checks.Items[0].OutputTitle != title || pr.Checks.Items[0].OutputSummary != summary {
		t.Fatalf("check output = %+v, want GitHub's title and summary preserved", pr.Checks.Items)
	}
}

func TestImportBoundsGitHubCheckRunSummaries(t *testing.T) {
	largeSummary := strings.Repeat("x", maxCheckSummaryRunes+100)
	run := checkRunWithSummary("long-report", headSHA, largeSummary)
	srv := fakeGitHub{headSHA: headSHA, checkRuns: checkRunsJSON(run), statuses: noStatuses}.serve(t)

	code, pr := importVia(t, srv)
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if len(pr.Checks.Items) != 1 {
		t.Fatalf("checks = %+v, want one check", pr.Checks.Items)
	}
	got := pr.Checks.Items[0]
	if len([]rune(got.OutputSummary)) != maxCheckSummaryRunes || !got.OutputSummaryTruncated {
		t.Fatalf("summary length = %d runes, truncated = %t; want %d runes and truncated", len([]rune(got.OutputSummary)), got.OutputSummaryTruncated, maxCheckSummaryRunes)
	}
}

func TestImportCapsTotalGitHubCheckRunSummaryBytes(t *testing.T) {
	runs := make([]string, 40)
	for i := range runs {
		runs[i] = checkRunWithSummary("report-"+itoa(i), headSHA, strings.Repeat("x", maxCheckSummaryRunes))
	}
	srv := fakeGitHub{headSHA: headSHA, checkRuns: checkRunsJSON(runs...), statuses: noStatuses}.serve(t)

	code, pr := importVia(t, srv)
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	totalBytes := 0
	truncated := false
	for _, item := range pr.Checks.Items {
		totalBytes += len(item.OutputSummary)
		truncated = truncated || item.OutputSummaryTruncated
	}
	if totalBytes > maxCheckSummaryBytes {
		t.Fatalf("total summary bytes = %d, want at most %d", totalBytes, maxCheckSummaryBytes)
	}
	if !truncated {
		t.Fatal("expected at least one summary to be marked truncated after the response budget was reached")
	}
}

func TestFocusedRevisionChecksEndpointFetchesRequestedSHA(t *testing.T) {
	requestedSHA := olderSHA
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/acme/app/commits/" + requestedSHA + "/check-runs":
			_, _ = w.Write([]byte(checkRunsJSON(checkRun("historical-build", requestedSHA, "completed", "success"))))
		case "/repos/acme/app/commits/" + requestedSHA + "/status":
			_, _ = w.Write([]byte(`{"sha":"` + requestedSHA + `","total_count":1,"statuses":[{"context":"legacy-ci","state":"failure","target_url":"https://ci.example/run/7"}]}`))
		default:
			t.Errorf("unexpected GitHub request: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	gin.SetMode(gin.TestMode)
	router := newRouter(&githubClient{baseURL: server.URL, http: server.Client()})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/checks", strings.NewReader(`{"url":"https://github.com/acme/app/pull/7","sha":"`+requestedSHA+`"}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)

	var got revisionChecks
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode checks: %v", err)
	}
	if got.RevisionSHA != requestedSHA || got.Availability != checksAvailable || len(got.Items) != 2 {
		t.Fatalf("checks = %+v, want results for requested revision", got)
	}
	if got.Items[0].Name != "historical-build" || got.Items[1].Name != "legacy-ci" {
		t.Fatalf("checks = %+v, want checks from requested revision", got.Items)
	}
}

func TestFocusedRevisionChecksEndpointRejectsInvalidSHA(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := newRouter(&githubClient{baseURL: "https://api.github.com", http: http.DefaultClient})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/checks", strings.NewReader(`{"url":"https://github.com/acme/app/pull/7","sha":"`+"bad"+`"}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
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
		{"malformed head", fakeGitHub{headSHA: "not-a-sha"}, checksUnavailable, "revision"},
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
		_, _ = w.Write([]byte(`{"number":7,"title":"Add checks","state":"open","html_url":"https://github.com/acme/app/pull/7","created_at":"2026-11-01T10:00:00Z","updated_at":"2026-11-02T10:00:00Z","head":{"sha":"` + sha + `"},"base":{"repo":{"name":"app","owner":{"login":"acme"}}}}`))
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
	if pr.Checks.RevisionSHA != newHeadSHA || pr.Commits[len(pr.Commits)-1].SHA != newHeadSHA {
		t.Fatalf("checks and timeline must describe the latest head: checks=%s commits=%+v", pr.Checks.RevisionSHA, pr.Commits)
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
		_, _ = w.Write([]byte(`{"number":7,"title":"Add checks","state":"open","html_url":"https://github.com/acme/app/pull/7","created_at":"2026-11-01T10:00:00Z","updated_at":"2026-11-02T10:00:00Z","head":{"sha":"` + sha + `"},"base":{"repo":{"name":"app","owner":{"login":"acme"}}}}`))
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
