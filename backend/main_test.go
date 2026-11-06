package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
