package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestParseQodoReviewComments(t *testing.T) {
	comments := []githubIssueComment{
		{ID: 17, User: githubUser{Login: "qodo-code-review[bot]"}, CreatedAt: time.Date(2026, 11, 4, 9, 0, 0, 0, time.UTC), HTMLURL: "https://github.com/acme/app/pull/7#issuecomment-17", Body: `<h3>Code Review by Qodo</h3><br/><details><summary>  1.  Reject stale checks <code>🐞 Bug</code></summary><pre>description</pre></details><details><summary>2. Empty findings</summary></details><!-- https://github.com/acme/app/commit/` + headSHA + ` -->`},
		{ID: 18, User: githubUser{Login: "qodo-code-review[bot]"}, CreatedAt: time.Date(2026, 11, 4, 9, 1, 0, 0, time.UTC), HTMLURL: "https://github.com/acme/app/pull/7#issuecomment-18", Body: `<h3>PR Summary by Qodo</h3>Keep the latest revision aligned &amp; visible.<code>🐞 Bug fix</code><!-- https://github.com/acme/app/commit/` + olderSHA + ` -->`},
		{ID: 19, User: githubUser{Login: "github-actions[bot]"}, Body: `<h3>Code Review by Qodo</h3><summary>1. Do not include</summary>`},
		{ID: 20, User: githubUser{Login: "felixgeelhaar"}, Body: `<h3>PR Summary by Qodo</h3>Do not include`},
		{ID: 21, User: githubUser{Login: "qodo-code-review[bot]"}, Body: "/agentic_review"},
	}

	got := parseQodoReviews(comments)
	if len(got) != 2 {
		t.Fatalf("got %d Qodo review comments, want 2", len(got))
	}
	if got[0].ID != 17 || got[0].Kind != "findings" || got[0].CommitSHA != headSHA || got[0].CommitURL != "https://github.com/acme/app/commit/"+headSHA || got[0].URL != comments[0].HTMLURL {
		t.Fatalf("unexpected findings evidence: %+v", got[0])
	}
	if len(got[0].FindingTitles) != 2 || got[0].FindingTitles[0] != "Reject stale checks 🐞 Bug" || got[0].FindingTitles[1] != "Empty findings" {
		t.Fatalf("finding titles = %#v", got[0].FindingTitles)
	}
	if got[1].Kind != "summary" || got[1].Summary != "Keep the latest revision aligned & visible." || got[1].CommitSHA != olderSHA {
		t.Fatalf("unexpected summary evidence: %+v", got[1])
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

	result := fetchQodoReviews(t.Context(), &githubClient{baseURL: srv.URL, http: srv.Client()}, "acme", "app", 7)
	if result.Availability != reviewsUnavailable || !strings.Contains(result.Reason, "could not be loaded") || len(result.Items) != 0 {
		t.Fatalf("unexpected unavailable result: %+v", result)
	}
}
