package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/felixgeelhaar/ghostline/backend/internal/domain"
)

var acmeApp = domain.RepositoryIdentity{Owner: "acme", Name: "app"}

func TestParseQodoKeepsCommitMarkersOnlyForTheImportedRepository(t *testing.T) {
	tests := []struct {
		name    string
		marker  string
		wantSHA string
		wantURL string
	}{
		{"same repository", "https://github.com/acme/app/commit/" + headSHA, headSHA, "https://github.com/acme/app/commit/" + headSHA},
		{"same repository, different case", "https://github.com/ACME/App/commit/" + headSHA, headSHA, "https://github.com/ACME/App/commit/" + headSHA},
		{"another repository", "https://github.com/acme/other/commit/" + headSHA, "", ""},
		{"another owner", "https://github.com/someone/app/commit/" + headSHA, "", ""},
		{"another host", "https://evil.example/acme/app/commit/" + headSHA, "", ""},
		{"extra path segment", "https://github.com/acme/app/extra/commit/" + headSHA, "", ""},
		{"encoded separator", "https://github.com/acme%2Fother/app/commit/" + headSHA, "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			comments := []githubIssueComment{{ID: 30, User: githubUser{Login: "qodo-code-review[bot]"}, HTMLURL: "https://github.com/acme/app/pull/7#issuecomment-30", Body: `<h3>Code Review by Qodo</h3><p>Great, no issues found!</p><!-- ` + tt.marker + ` -->`}}

			got, unrecognized := parseQodoReviews(comments, acmeApp)
			if unrecognized != 0 || len(got) != 1 {
				t.Fatalf("the review itself must be kept: reviews=%+v unrecognized=%d", got, unrecognized)
			}
			if got[0].CommitSHA != tt.wantSHA || got[0].CommitURL != tt.wantURL {
				t.Fatalf("commit evidence = (%q, %q), want (%q, %q)", got[0].CommitSHA, got[0].CommitURL, tt.wantSHA, tt.wantURL)
			}
		})
	}
}

func TestParseQodoReviewComments(t *testing.T) {
	comments := []githubIssueComment{
		{ID: 17, User: githubUser{Login: "qodo-code-review"}, CreatedAt: time.Date(2026, 11, 4, 9, 0, 0, 0, time.UTC), HTMLURL: "https://github.com/acme/app/pull/7#issuecomment-17", Body: `<h3>Code Review by Qodo</h3><br/><details><summary><img alt="Medium"><img alt="Remediation recommended">View medium (1)</summary><details><summary>  1.  Reject stale checks <code>🐞 Bug</code><code>⛨ Security</code><code>⭐ New</code></summary><br/><details><summary>Description</summary><br/><pre>Checks from the previous revision can appear current.</pre></details></details><details><summary>2. Empty findings</summary></details></details><!-- https://github.com/acme/app/commit/` + headSHA + ` -->`},
		{ID: 18, User: githubUser{Login: "qodo-free-for-open-source-projects[bot]"}, CreatedAt: time.Date(2026, 11, 4, 9, 1, 0, 0, time.UTC), HTMLURL: "https://github.com/acme/app/pull/7#issuecomment-18", Body: `<h3>PR Summary by Qodo</h3>Keep the latest revision aligned with <code>headSha</code> after import.<img src="https://www.qodo.ai/wp-content/uploads/2025/11/light-grey-line.svg" alt="decorative"><code>✨ Enhancement</code><!-- https://github.com/acme/app/commit/` + olderSHA + ` -->`},
		{ID: 19, User: githubUser{Login: "github-actions[bot]"}, Body: `<h3>Code Review by Qodo</h3><summary>1. Do not include</summary>`},
		{ID: 20, User: githubUser{Login: "felixgeelhaar"}, Body: `<h3>PR Summary by Qodo</h3>Do not include`},
		{ID: 21, User: githubUser{Login: "qodo-code-review[bot]"}, Body: "/agentic_review"},
	}

	got, unrecognized := parseQodoReviews(comments, acmeApp)
	if len(got) != 2 {
		t.Fatalf("got %d Qodo review comments, want 2", len(got))
	}
	if unrecognized != 1 {
		t.Fatalf("unrecognized Qodo comments = %d, want 1", unrecognized)
	}
	if got[0].ID != 17 || got[0].Kind != "findings" || got[0].CommitSHA != headSHA || got[0].CommitURL != "https://github.com/acme/app/commit/"+headSHA || got[0].URL != comments[0].HTMLURL {
		t.Fatalf("unexpected findings evidence: %+v", got[0])
	}
	if len(got[0].Findings) != 2 || got[0].Findings[0].Title != "Reject stale checks" || got[0].Findings[1].Title != "Empty findings" {
		t.Fatalf("findings = %#v", got[0].Findings)
	}
	firstFinding := got[0].Findings[0]
	if firstFinding.ID != "17:1" || strings.Join(firstFinding.Categories, ",") != "🐞 Bug,⛨ Security" || firstFinding.Lifecycle != "⭐ New" || firstFinding.Severity != "Medium" || firstFinding.ActionLevel != "Remediation recommended" || firstFinding.Description != "Checks from the previous revision can appear current." || firstFinding.SourceURL != comments[0].HTMLURL {
		t.Fatalf("unexpected structured finding: %+v", firstFinding)
	}
	if got[1].Kind != "summary" || got[1].Summary != "Keep the latest revision aligned with headSha after import." || got[1].CommitSHA != olderSHA {
		t.Fatalf("unexpected summary evidence: %+v", got[1])
	}
}

func TestParseQodoCleanReviewAsNoOpenFindings(t *testing.T) {
	comments := []githubIssueComment{{ID: 26, User: githubUser{Login: "qodo-code-review[bot]"}, HTMLURL: "https://github.com/acme/app/pull/7#issuecomment-26", Body: `<h3>Code Review by Qodo</h3><p>Nice, no open findings for this group</p><!-- https://github.com/acme/app/commit/` + headSHA + ` -->`}}

	got, unrecognized := parseQodoReviews(comments, acmeApp)
	if unrecognized != 0 || len(got) != 1 || got[0].Kind != "no_open_findings" || got[0].CommitSHA != headSHA || len(got[0].Findings) != 0 {
		t.Fatalf("clean review was not preserved as source-backed no-open-findings evidence: reviews=%+v unrecognized=%d", got, unrecognized)
	}
}

func TestParseQodoDoesNotMarkUnreadableFindingGroupAsClean(t *testing.T) {
	comments := []githubIssueComment{{ID: 27, User: githubUser{Login: "qodo-code-review[bot]"}, HTMLURL: "https://github.com/acme/app/pull/7#issuecomment-27", Body: `<h3>Code Review by Qodo</h3><p>Nice, no open findings for this group</p><details><summary>View medium (1)</summary><details><summary>Concern: stale result details</summary></details></details>`}}

	got, unrecognized := parseQodoReviews(comments, acmeApp)
	if unrecognized != 0 || len(got) != 1 || got[0].Kind != "findings" {
		t.Fatalf("a non-empty, unparsed finding group must not look clean: reviews=%+v unrecognized=%d", got, unrecognized)
	}
}

func TestParseQodoReportsPartiallyUnparsedFindings(t *testing.T) {
	comments := []githubIssueComment{{
		ID: 28, User: githubUser{Login: "qodo-code-review[bot]"}, HTMLURL: "https://github.com/acme/app/pull/7#issuecomment-28",
		Body: `<h3>Code Review by Qodo</h3><details><summary>View medium (2)</summary><details><summary>1. Keep revision links scoped <code>🐞 Bug</code></summary><details><summary>Description</summary><pre>Use the imported revision.</pre></details></details><details><summary>2.</summary></details></details>`,
	}}

	got, unrecognized := parseQodoReviews(comments, acmeApp)
	if unrecognized != 0 || len(got) != 1 {
		t.Fatalf("review was not preserved: reviews=%+v unrecognized=%d", got, unrecognized)
	}
	if got[0].Kind != "findings" || len(got[0].Findings) != 1 || got[0].Findings[0].Title != "Keep revision links scoped" || got[0].UnparsedFindingCount != 1 {
		t.Fatalf("parsed and unparsed findings = %+v", got[0])
	}
}

func TestQodoSummaryStopsAtDividerSourceEvenWithoutDescriptiveAlt(t *testing.T) {
	comments := []githubIssueComment{{ID: 25, User: githubUser{Login: "qodo-free-for-open-source-projects[bot]"}, Body: `<h3>PR Summary by Qodo</h3>Keep checks revision-specific.<img src="https://www.qodo.ai/light-grey-line.svg" alt="divider"><code>Post-divider category</code>`}}
	got, _ := parseQodoReviews(comments, acmeApp)
	if len(got) != 1 || got[0].Summary != "Keep checks revision-specific." {
		t.Fatalf("summary crossed divider or was not parsed: %+v", got)
	}
}

func TestFetchQodoReviewsStopsAtCommentBudgetAndReportsUnavailable(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Query().Get("per_page") != "100" || r.URL.Query().Get("page") == "" {
			t.Errorf("unexpected request: %s", r.URL.String())
		}
		comments := make([]githubIssueComment, 100)
		for i := range comments {
			comments[i] = githubIssueComment{ID: int64(i + 1), User: githubUser{Login: "someone"}}
		}
		_ = json.NewEncoder(w).Encode(comments)
	}))
	defer srv.Close()

	result := fetchQodoReviews(t.Context(), &githubClient{baseURL: srv.URL, http: srv.Client()}, acmeApp, 7)
	if requests != maxQodoCommentPages || result.Availability != reviewsUnavailable || len(result.Items) != 0 {
		t.Fatalf("requests=%d result=%+v", requests, result)
	}
}

func TestFetchQodoReviewsMarksUnavailableOnGitHubError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/acme/app/issues/7/comments" || r.URL.Query().Get("per_page") != "100" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.String())
		}
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	result := fetchQodoReviews(t.Context(), &githubClient{baseURL: srv.URL, http: srv.Client()}, acmeApp, 7)
	if result.Availability != reviewsUnavailable || !strings.Contains(result.Reason, "could not be loaded") || len(result.Items) != 0 {
		t.Fatalf("unexpected unavailable result: %+v", result)
	}
}
