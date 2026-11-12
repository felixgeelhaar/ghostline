package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestParsePRURL(t *testing.T) {
	tests := []struct {
		name, input         string
		wantOwner, wantRepo string
		wantNumber          int
		wantErr             bool
	}{
		{"valid", "https://github.com/acme/ghostline/pull/42", "acme", "ghostline", 42, false},
		{"trailing slash", "https://github.com/acme/ghostline/pull/42/", "acme", "ghostline", 42, false},
		{"wrong host", "https://example.com/acme/ghostline/pull/42", "", "", 0, true},
		{"wrong route", "https://github.com/acme/ghostline/issues/42", "", "", 0, true},
		{"query", "https://github.com/acme/ghostline/pull/42?x=1", "", "", 0, true},
		{"zero", "https://github.com/acme/ghostline/pull/0", "", "", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner, repo, number, err := parsePRURL(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && (owner != tt.wantOwner || repo != tt.wantRepo || number != tt.wantNumber) {
				t.Fatalf("got %s/%s#%d", owner, repo, number)
			}
		})
	}
}

func TestAuthenticatedAPIRejectsPrivateRepository(t *testing.T) {
	var requests []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.Path)
		if r.URL.Path != "/repos/acme/app" {
			t.Errorf("private repository request escaped visibility check: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"private":true}`))
	}))
	defer srv.Close()

	gin.SetMode(gin.TestMode)
	router := newRouter(&githubClient{baseURL: srv.URL, token: "test-token", http: srv.Client()})
	for _, endpoint := range []struct {
		path string
		body string
	}{
		{path: "/api/import", body: `{"url":"https://github.com/acme/app/pull/7"}`},
		{path: "/api/checks", body: `{"url":"https://github.com/acme/app/pull/7","sha":"` + strings.Repeat("a", 40) + `"}`},
	} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, endpoint.path, strings.NewReader(endpoint.body))
		request.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusNotFound {
			t.Errorf("%s status = %d, want 404: %s", endpoint.path, recorder.Code, recorder.Body.String())
		}
	}
	if len(requests) != 2 {
		t.Fatalf("GitHub requests = %v, want only visibility checks", requests)
	}
}

func TestImportDoesNotUseVisibilityTokenForPullRequestData(t *testing.T) {
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		switch r.URL.Path {
		case "/repos/acme/app":
			if r.Header.Get("Authorization") != "Bearer visibility-token" {
				t.Errorf("visibility request Authorization = %q", r.Header.Get("Authorization"))
			}
			_, _ = w.Write([]byte(`{"private":false}`))
		case "/repos/acme/app/pulls/7":
			if got := r.Header.Get("Authorization"); got != "" {
				t.Errorf("pull request data request unexpectedly used server token: %q", got)
			}
			// Simulate a public repository becoming private after visibility was checked.
			http.NotFound(w, r)
		default:
			t.Errorf("unexpected GitHub request: %s", r.URL.RequestURI())
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	gin.SetMode(gin.TestMode)
	router := newRouter(&githubClient{baseURL: server.URL, token: "visibility-token", http: server.Client()})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/import", strings.NewReader(`{"url":"https://github.com/acme/app/pull/7"}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNotFound || strings.Contains(recorder.Body.String(), "private pull request title") {
		t.Fatalf("status/body = %d/%s, want no data after repository became private", recorder.Code, recorder.Body.String())
	}
	if len(requests) != 2 || requests[0] != "GET /repos/acme/app" || requests[1] != "GET /repos/acme/app/pulls/7" {
		t.Fatalf("GitHub requests = %v", requests)
	}
}

func TestImportPRUsesCanonicalRepositoryReturnedByGitHub(t *testing.T) {
	const sha = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	var requests []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.Path)
		switch {
		case r.URL.Path == "/repos/old-owner/old-repo/pulls/12" || r.URL.Path == "/repos/new-owner/new-repo/pulls/12":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"number": 12, "title": "Renamed repository", "state": "open",
				"html_url":   "https://github.com/new-owner/new-repo/pull/12",
				"created_at": "2026-11-06T08:00:00Z", "updated_at": "2026-11-06T08:00:00Z",
				"head": map[string]string{"sha": sha},
				"base": map[string]any{"repo": map[string]any{"name": "new-repo", "owner": map[string]string{"login": "new-owner"}}},
			})
		case r.URL.Path == "/repos/new-owner/new-repo/pulls/12/commits":
			_, _ = w.Write([]byte("[]"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	gh := &githubClient{baseURL: srv.URL, http: srv.Client()}
	router := newRouter(gh)
	request := httptest.NewRequest(http.MethodPost, "/api/import", strings.NewReader(`{"url":"https://github.com/old-owner/old-repo/pull/12"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var result pullRequest
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Repository != "new-owner/new-repo" || result.Owner != "new-owner" || result.Repo != "new-repo" || result.URL != "https://github.com/new-owner/new-repo/pull/12" {
		t.Fatalf("response did not preserve canonical repository identity: %+v", result)
	}
	for _, path := range requests[1:] {
		if strings.Contains(path, "/old-owner/old-repo/") {
			t.Fatalf("follow-up request used stale repository identity: %s", path)
		}
	}
}

func TestImportRetriesWhenRepositoryChangesDuringSnapshot(t *testing.T) {
	const sha = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	var requests []string
	oldPullReads := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.Path)
		switch r.URL.Path {
		case "/repos/old-owner/old-repo/pulls/12":
			oldPullReads++
			owner, repo := "old-owner", "old-repo"
			if oldPullReads == 2 {
				owner, repo = "new-owner", "new-repo"
			}
			writePRResponse(t, w, owner, repo, sha)
		case "/repos/new-owner/new-repo/pulls/12":
			writePRResponse(t, w, "new-owner", "new-repo", sha)
		case "/repos/old-owner/old-repo/pulls/12/commits", "/repos/new-owner/new-repo/pulls/12/commits":
			_, _ = w.Write([]byte("[]"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	router := newRouter(&githubClient{baseURL: srv.URL, http: srv.Client()})
	request := httptest.NewRequest(http.MethodPost, "/api/import", strings.NewReader(`{"url":"https://github.com/old-owner/old-repo/pull/12"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var result pullRequest
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Repository != "new-owner/new-repo" || result.URL != "https://github.com/new-owner/new-repo/pull/12" || result.Checks.RevisionURL != "https://github.com/new-owner/new-repo/commit/"+sha {
		t.Fatalf("response combined stale and canonical repository identity: %+v", result)
	}
	if oldPullReads != 2 {
		t.Fatalf("old repository pull request reads = %d, want two snapshots before retry", oldPullReads)
	}
	foundRetryOnCanonicalPath := false
	for _, path := range requests {
		if path == "/repos/new-owner/new-repo/pulls/12" {
			foundRetryOnCanonicalPath = true
		}
	}
	if !foundRetryOnCanonicalPath {
		t.Fatalf("retry did not use the canonical repository path: %v", requests)
	}
}

func writePRResponse(t *testing.T, w http.ResponseWriter, owner, repo, sha string) {
	t.Helper()
	_ = json.NewEncoder(w).Encode(map[string]any{
		"number": 12, "title": "Renamed repository", "state": "open",
		"html_url":   "https://github.com/" + owner + "/" + repo + "/pull/12",
		"created_at": "2026-11-06T08:00:00Z", "updated_at": "2026-11-06T08:00:00Z",
		"head": map[string]string{"sha": sha},
		"base": map[string]any{"repo": map[string]any{"name": repo, "owner": map[string]string{"login": owner}}},
	})
}
