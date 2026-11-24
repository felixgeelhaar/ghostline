package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// GitHub answers 404 for a workflow's runs when the repository has no such
// workflow file. The pull request itself was found, so the reason must not say
// GitHub failed to return it.
func TestLoadNoxReportSaysWhenTheRepositoryHasNoSecurityWorkflow(t *testing.T) {
	const sha = "7044a8a032e85b6ab611033b2ac8af7ce85805b2"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/octocat/Hello-World/pulls/1":
			_ = json.NewEncoder(w).Encode(map[string]any{"number": 1, "head": map[string]string{"sha": sha}, "base": map[string]any{"sha": strings.Repeat("b", 40), "repo": map[string]any{"name": "Hello-World", "owner": map[string]string{"login": "octocat"}}}})
		default:
			http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
		}
	}))
	defer server.Close()

	report := loadNoxReport(context.Background(), noxTestGitHubClient(server), "octocat", "Hello-World", 1, sha)
	want := "GitHub reports no workflow at .github/workflows/nox.yml in octocat/Hello-World, so Ghostline found no security report for this revision."
	if report.Availability != noxReportUnavailable || report.Reason != want {
		t.Fatalf("reason = %q (availability %q), want %q", report.Reason, report.Availability, want)
	}
}

// A missing pull request still reads as before.
func TestLoadNoxReportStillSaysWhenThePullRequestIsMissing(t *testing.T) {
	const sha = "7044a8a032e85b6ab611033b2ac8af7ce85805b2"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
	}))
	defer server.Close()

	report := loadNoxReport(context.Background(), noxTestGitHubClient(server), "octocat", "Hello-World", 1, sha)
	if report.Reason != "GitHub did not return the pull request or its workflow evidence." {
		t.Fatalf("reason = %q", report.Reason)
	}
}
