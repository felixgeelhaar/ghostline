package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestImportCapsReferencedIssueLookupsAndMarksCoverageIncomplete(t *testing.T) {
	const sha = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	var body strings.Builder
	for number := 1; number <= maxReferencedIssues+1; number++ {
		fmt.Fprintf(&body, "Refs #%d ", number)
	}
	var issueLookups int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/acme/app/pulls/7":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"number": 7, "title": "Change", "state": "closed", "merged": true, "body": body.String(),
				"html_url": "https://github.com/acme/app/pull/7", "created_at": "2026-11-01T10:00:00Z", "updated_at": "2026-11-02T10:00:00Z",
				"head": map[string]string{"sha": sha}, "base": map[string]any{"repo": map[string]any{"name": "app", "owner": map[string]string{"login": "acme"}}},
			})
		case r.URL.Path == "/repos/acme/app/pulls/7/commits":
			_, _ = w.Write([]byte("[]"))
		case strings.HasPrefix(r.URL.Path, "/repos/acme/app/issues/") && !strings.HasSuffix(r.URL.Path, "/comments"):
			issueLookups++
			var number int
			_, _ = fmt.Sscanf(strings.TrimPrefix(r.URL.Path, "/repos/acme/app/issues/"), "%d", &number)
			_ = json.NewEncoder(w).Encode(map[string]any{"state": "closed", "html_url": fmt.Sprintf("https://github.com/acme/app/issues/%d", number)})
		case r.URL.Path == "/repos/acme/app/commits/"+sha+"/check-runs":
			_, _ = w.Write([]byte(`{"total_count":0,"check_runs":[]}`))
		case r.URL.Path == "/repos/acme/app/commits/"+sha+"/status":
			_, _ = w.Write([]byte(`{"sha":"` + sha + `","total_count":0,"statuses":[]}`))
		case r.URL.Path == "/repos/acme/app/issues/7/comments":
			_, _ = w.Write([]byte("[]"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	code, pr := importVia(t, server)
	if code != http.StatusOK {
		t.Fatalf("import status = %d", code)
	}
	if pr.ReferencedIssues.Availability != referencedIssuesUnavailable || !strings.Contains(pr.ReferencedIssues.Reason, "only the first 20") {
		t.Fatalf("referenced issue coverage = %+v, want incomplete coverage with a reason", pr.ReferencedIssues)
	}
	if len(pr.ReferencedIssues.Items) != maxReferencedIssues || issueLookups != maxReferencedIssues {
		t.Fatalf("returned %d issues after %d lookups, want %d", len(pr.ReferencedIssues.Items), issueLookups, maxReferencedIssues)
	}
}

func TestImportReportsReferencedIssueState(t *testing.T) {
	tests := []struct {
		name           string
		body           string
		merged         bool
		issueStatus    int
		issueState     string
		wantAvailable  string
		wantIssueState string
		wantIssueCount int
		wantMerged     bool
	}{
		{name: "merged PR with open issue", body: "Resolves #12", merged: true, issueState: "open", wantAvailable: referencedIssuesAvailable, wantIssueState: "open", wantIssueCount: 1, wantMerged: true},
		{name: "merged PR with closed issue", body: "Fixes #12", merged: true, issueState: "closed", wantAvailable: referencedIssuesAvailable, wantIssueState: "closed", wantIssueCount: 1, wantMerged: true},
		{name: "other repository reference ignored", body: "See https://github.com/elsewhere/app/issues/12", merged: true, wantAvailable: referencedIssuesAvailable, wantIssueCount: 0, wantMerged: true},
		{name: "unmerged PR preserves open state", body: "Refs #12", issueState: "open", wantAvailable: referencedIssuesAvailable, wantIssueState: "open", wantIssueCount: 1},
		{name: "failed lookup remains visible as unavailable", body: "Refs #12", issueStatus: http.StatusForbidden, wantAvailable: referencedIssuesUnavailable, wantIssueState: referencedIssueUnavailable, wantIssueCount: 1},
		{name: "pull request reference is not listed as an issue", body: "See #12", issueState: "open", wantAvailable: referencedIssuesAvailable, wantIssueCount: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			const sha = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
			var issueLookups int
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/repos/acme/app/pulls/7":
					_ = json.NewEncoder(w).Encode(map[string]any{
						"number": 7, "title": "Change", "state": map[bool]string{true: "closed", false: "open"}[tt.merged], "merged": tt.merged,
						"body": tt.body, "html_url": "https://github.com/acme/app/pull/7", "created_at": "2026-11-01T10:00:00Z", "updated_at": "2026-11-02T10:00:00Z",
						"head": map[string]string{"sha": sha}, "base": map[string]any{"repo": map[string]any{"name": "app", "owner": map[string]string{"login": "acme"}}},
					})
				case "/repos/acme/app/pulls/7/commits":
					_, _ = w.Write([]byte("[]"))
				case "/repos/acme/app/issues/12":
					issueLookups++
					if tt.issueStatus != 0 {
						w.WriteHeader(tt.issueStatus)
						return
					}
					issue := map[string]any{"state": tt.issueState, "html_url": "https://github.com/acme/app/issues/12"}
					if tt.name == "pull request reference is not listed as an issue" {
						issue["pull_request"] = map[string]any{"url": "https://api.github.com/repos/acme/app/pulls/12"}
					}
					_ = json.NewEncoder(w).Encode(issue)
				case "/repos/acme/app/commits/" + sha + "/check-runs":
					_, _ = w.Write([]byte(`{"total_count":0,"check_runs":[]}`))
				case "/repos/acme/app/commits/" + sha + "/status":
					_, _ = w.Write([]byte(`{"sha":"` + sha + `","total_count":0,"statuses":[]}`))
				case "/repos/acme/app/issues/7/comments":
					_, _ = w.Write([]byte("[]"))
				default:
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer server.Close()

			code, pr := importVia(t, server)
			if code != http.StatusOK {
				t.Fatalf("import status = %d", code)
			}
			if pr.Merged != tt.wantMerged || pr.ReferencedIssues.Availability != tt.wantAvailable || len(pr.ReferencedIssues.Items) != tt.wantIssueCount {
				t.Fatalf("merged=%v referenced issues=%+v", pr.Merged, pr.ReferencedIssues)
			}
			if tt.wantIssueCount > 0 {
				issue := pr.ReferencedIssues.Items[0]
				if issue.Number != 12 || issue.URL != "https://github.com/acme/app/issues/12" || issue.State != tt.wantIssueState {
					t.Fatalf("referenced issue = %+v", issue)
				}
			}
			if tt.name == "other repository reference ignored" && issueLookups != 0 {
				t.Fatalf("looked up an issue from another repository: %d requests", issueLookups)
			}
			if tt.wantIssueCount > 0 && issueLookups != 1 {
				t.Fatalf("issue lookup count = %d, want 1", issueLookups)
			}
		})
	}
}

func TestImportIncludesReferencedIssueAvailabilityReason(t *testing.T) {
	const sha = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/acme/app/pulls/7":
			_, _ = w.Write([]byte(`{"number":7,"title":"Change","state":"closed","merged":true,"body":"Refs #12","html_url":"https://github.com/acme/app/pull/7","created_at":"2026-11-01T10:00:00Z","updated_at":"2026-11-02T10:00:00Z","head":{"sha":"` + sha + `"},"base":{"repo":{"name":"app","owner":{"login":"acme"}}}}`))
		case "/repos/acme/app/pulls/7/commits":
			_, _ = w.Write([]byte("[]"))
		case "/repos/acme/app/issues/12":
			w.WriteHeader(http.StatusForbidden)
		case "/repos/acme/app/commits/" + sha + "/check-runs":
			_, _ = w.Write([]byte(`{"total_count":0,"check_runs":[]}`))
		case "/repos/acme/app/commits/" + sha + "/status":
			_, _ = w.Write([]byte(`{"sha":"` + sha + `","total_count":0,"statuses":[]}`))
		case "/repos/acme/app/issues/7/comments":
			_, _ = w.Write([]byte("[]"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	_, pr := importVia(t, server)
	if pr.ReferencedIssues.Reason == "" || !strings.Contains(pr.ReferencedIssues.Reason, "could not confirm") {
		t.Fatalf("referenced issue unavailability reason = %q", pr.ReferencedIssues.Reason)
	}
}
