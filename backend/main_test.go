package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/felixgeelhaar/ghostline/backend/internal/domain"
)

func TestProductionRouterServesFrontendAndKeepsAPIAvailable(t *testing.T) {
	assets := fstest.MapFS{
		"web/index.html":    &fstest.MapFile{Data: []byte("<!doctype html><div id=app>Ghostline</div>")},
		"web/assets/app.js": &fstest.MapFile{Data: []byte("console.log('ghostline')")},
	}
	router := newProductionRouter(&githubClient{}, newRateLimiter(defaultRateLimitRequests, defaultRateLimitWindow, time.Now), clientIdentity{}, authSettings{Now: time.Now}, assets)

	for _, path := range []string{"/", "/deep-link"} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "id=app") {
			t.Fatalf("GET %s = %d, body %q; want SPA index", path, recorder.Code, recorder.Body.String())
		}
	}

	asset := httptest.NewRecorder()
	router.ServeHTTP(asset, httptest.NewRequest(http.MethodGet, "/assets/app.js", nil))
	if asset.Code != http.StatusOK || !strings.Contains(asset.Body.String(), "ghostline") || !strings.HasPrefix(asset.Header().Get("Content-Type"), "text/javascript") || asset.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" {
		t.Fatalf("asset response = %d, content-type %q, body %q", asset.Code, asset.Header().Get("Content-Type"), asset.Body.String())
	}

	missingAsset := httptest.NewRecorder()
	router.ServeHTTP(missingAsset, httptest.NewRequest(http.MethodGet, "/assets/missing.js", nil))
	if missingAsset.Code != http.StatusNotFound || strings.Contains(missingAsset.Body.String(), "id=app") {
		t.Fatalf("missing asset response = %d, %q; want an asset 404, not SPA fallback", missingAsset.Code, missingAsset.Body.String())
	}

	missingAPI := httptest.NewRecorder()
	router.ServeHTTP(missingAPI, httptest.NewRequest(http.MethodGet, "/api/not-found", nil))
	if missingAPI.Code != http.StatusNotFound || strings.Contains(missingAPI.Body.String(), "id=app") {
		t.Fatalf("missing API response = %d, %q; want API 404, not SPA fallback", missingAPI.Code, missingAPI.Body.String())
	}

	unknownWrite := httptest.NewRecorder()
	router.ServeHTTP(unknownWrite, httptest.NewRequest(http.MethodPost, "/unknown-action", nil))
	if unknownWrite.Code != http.StatusNotFound || strings.Contains(unknownWrite.Body.String(), "id=app") {
		t.Fatalf("unknown write response = %d, %q; want 404, not SPA fallback", unknownWrite.Code, unknownWrite.Body.String())
	}

	health := httptest.NewRecorder()
	router.ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/api/health", nil))
	if health.Code != http.StatusOK {
		t.Fatalf("API health status = %d, want 200", health.Code)
	}
}

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

func TestCompareRevisionsReturnsSourceLinkedFileSummaryForRequestedSHAs(t *testing.T) {
	baseSHA := strings.Repeat("a", 40)
	headSHA := strings.Repeat("b", 40)
	var requestedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPath = r.URL.RequestURI()
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "ahead", "ahead_by": 1, "behind_by": 0, "total_commits": 1,
			"html_url": "https://github.com/acme/app/compare/" + baseSHA + "..." + headSHA,
			// The compare endpoint's documented response does not include head_commit.
			"base_commit": map[string]string{"sha": baseSHA},
			"files":       []map[string]any{{"filename": "backend/retry.go", "status": "modified", "additions": 4, "deletions": 1, "changes": 5, "blob_url": "https://github.com/acme/app/blob/" + headSHA + "/backend/retry.go"}},
		})
	}))
	defer srv.Close()
	request := httptest.NewRequest(http.MethodPost, "/api/compare", strings.NewReader(`{"url":"https://github.com/acme/app/pull/7","baseSha":"`+baseSHA+`","headSha":"`+headSHA+`"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	newRouter(&githubClient{baseURL: srv.URL, http: srv.Client()}).ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if !strings.Contains(requestedPath, "/repos/acme/app/compare/"+baseSHA+"..."+headSHA) {
		t.Fatalf("GitHub path = %q, want requested revision pair", requestedPath)
	}
	var result domain.RevisionComparison
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.BaseSHA != baseSHA || result.HeadSHA != headSHA || result.CompareURL == "" || len(result.Files) != 1 || result.Files[0].Path != "backend/retry.go" || result.Files[0].URL == "" {
		t.Fatalf("comparison response lost revision or source identity: %+v", result)
	}
}

func TestCompareRevisionsFlagsGitHubChangedFileLimit(t *testing.T) {
	baseSHA := strings.Repeat("a", 40)
	headSHA := strings.Repeat("b", 40)
	files := make([]map[string]any, 300)
	for i := range files {
		files[i] = map[string]any{"filename": fmt.Sprintf("file-%d.go", i), "status": "modified", "additions": 1, "deletions": 0, "changes": 1, "blob_url": "https://github.com/acme/app/blob/" + headSHA + fmt.Sprintf("/file-%d.go", i)}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "ahead", "ahead_by": 1, "behind_by": 0, "total_commits": 1,
			"html_url":    "https://github.com/acme/app/compare/" + baseSHA + "..." + headSHA,
			"base_commit": map[string]string{"sha": baseSHA}, "files": files,
		})
	}))
	defer srv.Close()
	request := httptest.NewRequest(http.MethodPost, "/api/compare", strings.NewReader(`{"url":"https://github.com/acme/app/pull/7","baseSha":"`+baseSHA+`","headSha":"`+headSHA+`"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	newRouter(&githubClient{baseURL: srv.URL, http: srv.Client()}).ServeHTTP(response, request)
	var result domain.RevisionComparison
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusOK || len(result.Files) != 300 || !result.FilesTruncated {
		t.Fatalf("comparison = status %d, files %d, truncated %t", response.Code, len(result.Files), result.FilesTruncated)
	}
}

func TestCompareRevisionsReportsInvalidChangedFileDetails(t *testing.T) {
	baseSHA := strings.Repeat("a", 40)
	headSHA := strings.Repeat("b", 40)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "ahead", "ahead_by": 1, "behind_by": 0, "total_commits": 1,
			"html_url":    "https://github.com/acme/app/compare/" + baseSHA + "..." + headSHA,
			"base_commit": map[string]string{"sha": baseSHA},
			"files":       []map[string]any{{"filename": "backend/retry.go", "status": "modified", "additions": -1, "deletions": 0, "changes": 1, "blob_url": "https://github.com/acme/app/blob/" + headSHA + "/backend/retry.go"}},
		})
	}))
	defer srv.Close()
	request := httptest.NewRequest(http.MethodPost, "/api/compare", strings.NewReader(`{"url":"https://github.com/acme/app/pull/7","baseSha":"`+baseSHA+`","headSha":"`+headSHA+`"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	newRouter(&githubClient{baseURL: srv.URL, http: srv.Client()}).ServeHTTP(response, request)
	if response.Code != http.StatusBadGateway || !strings.Contains(response.Body.String(), "invalid changed-file details") {
		t.Fatalf("comparison error = status %d, body %s", response.Code, response.Body.String())
	}
}

func TestCompareRevisionsRejectsNonlinearRevisionPair(t *testing.T) {
	baseSHA := strings.Repeat("a", 40)
	headSHA := strings.Repeat("b", 40)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "diverged", "ahead_by": 2, "behind_by": 1, "total_commits": 2,
			"html_url":    "https://github.com/acme/app/compare/" + baseSHA + "..." + headSHA,
			"base_commit": map[string]string{"sha": baseSHA},
			"files":       []map[string]any{{"filename": "backend/retry.go", "status": "modified", "additions": 4, "deletions": 1, "changes": 5, "blob_url": "https://github.com/acme/app/blob/" + headSHA + "/backend/retry.go"}},
		})
	}))
	defer srv.Close()
	request := httptest.NewRequest(http.MethodPost, "/api/compare", strings.NewReader(`{"url":"https://github.com/acme/app/pull/7","baseSha":"`+baseSHA+`","headSha":"`+headSHA+`"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	newRouter(&githubClient{baseURL: srv.URL, http: srv.Client()}).ServeHTTP(response, request)
	if response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), "ancestor") {
		t.Fatalf("comparison error = status %d, body %s", response.Code, response.Body.String())
	}
}

func TestCompareRevisionsRejectsChangedFileLinkForDifferentRevision(t *testing.T) {
	baseSHA := strings.Repeat("a", 40)
	headSHA := strings.Repeat("b", 40)
	otherSHA := strings.Repeat("c", 40)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "ahead", "ahead_by": 1, "behind_by": 0, "total_commits": 1,
			"html_url":    "https://github.com/acme/app/compare/" + baseSHA + "..." + headSHA,
			"base_commit": map[string]string{"sha": baseSHA},
			"files":       []map[string]any{{"filename": "backend/retry.go", "status": "modified", "additions": 4, "deletions": 1, "changes": 5, "blob_url": "https://github.com/acme/app/blob/" + otherSHA + "/backend/retry.go"}},
		})
	}))
	defer srv.Close()
	request := httptest.NewRequest(http.MethodPost, "/api/compare", strings.NewReader(`{"url":"https://github.com/acme/app/pull/7","baseSha":"`+baseSHA+`","headSha":"`+headSHA+`"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	newRouter(&githubClient{baseURL: srv.URL, http: srv.Client()}).ServeHTTP(response, request)
	if response.Code != http.StatusBadGateway || !strings.Contains(response.Body.String(), "invalid changed-file source link") {
		t.Fatalf("comparison error = status %d, body %s", response.Code, response.Body.String())
	}
}

func TestCompareRevisionsRejectsChangedFileLinkForDifferentPath(t *testing.T) {
	baseSHA := strings.Repeat("a", 40)
	headSHA := strings.Repeat("b", 40)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "ahead", "ahead_by": 1, "behind_by": 0, "total_commits": 1,
			"html_url":    "https://github.com/acme/app/compare/" + baseSHA + "..." + headSHA,
			"base_commit": map[string]string{"sha": baseSHA},
			"files":       []map[string]any{{"filename": "backend/retry.go", "status": "modified", "additions": 4, "deletions": 1, "changes": 5, "blob_url": "https://github.com/acme/app/blob/" + headSHA + "/backend/other.go"}},
		})
	}))
	defer srv.Close()
	request := httptest.NewRequest(http.MethodPost, "/api/compare", strings.NewReader(`{"url":"https://github.com/acme/app/pull/7","baseSha":"`+baseSHA+`","headSha":"`+headSHA+`"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	newRouter(&githubClient{baseURL: srv.URL, http: srv.Client()}).ServeHTTP(response, request)
	if response.Code != http.StatusBadGateway || !strings.Contains(response.Body.String(), "invalid changed-file source link") {
		t.Fatalf("comparison error = status %d, body %s", response.Code, response.Body.String())
	}
}

func TestCompareRevisionsGivesSpecificErrorForMissingRevision(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()
	baseSHA := strings.Repeat("a", 40)
	headSHA := strings.Repeat("b", 40)
	request := httptest.NewRequest(http.MethodPost, "/api/compare", strings.NewReader(`{"url":"https://github.com/acme/app/pull/7","baseSha":"`+baseSHA+`","headSha":"`+headSHA+`"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	newRouter(&githubClient{baseURL: srv.URL, http: srv.Client()}).ServeHTTP(response, request)
	if response.Code != http.StatusNotFound || !strings.Contains(response.Body.String(), "requested revisions") || strings.Contains(response.Body.String(), "Pull request not found") {
		t.Fatalf("comparison error = status %d, body %s", response.Code, response.Body.String())
	}
}

func TestCompareRevisionsRejectsInvalidOrMismatchedSHAs(t *testing.T) {
	baseSHA := strings.Repeat("a", 40)
	headSHA := strings.Repeat("b", 40)
	serverCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serverCalls++
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "ahead", "html_url": "https://github.com/acme/app/compare/" + baseSHA + "..." + headSHA,
			"base_commit": map[string]string{"sha": baseSHA}, "head_commit": map[string]string{"sha": strings.Repeat("c", 40)},
		})
	}))
	defer srv.Close()
	router := newRouter(&githubClient{baseURL: srv.URL, http: srv.Client()})
	for _, body := range []string{
		`{"url":"https://github.com/acme/app/pull/7","baseSha":"short","headSha":"` + headSHA + `"}`,
		`{"url":"https://github.com/acme/app/pull/7","baseSha":"` + baseSHA + `","headSha":"` + baseSHA + `"}`,
	} {
		request := httptest.NewRequest(http.MethodPost, "/api/compare", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("invalid comparison status = %d, body = %s", response.Code, response.Body.String())
		}
	}
	if serverCalls != 0 {
		t.Fatalf("GitHub called %d times for invalid comparisons", serverCalls)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/compare", strings.NewReader(`{"url":"https://github.com/acme/app/pull/7","baseSha":"`+baseSHA+`","headSha":"`+headSHA+`"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusBadGateway {
		t.Fatalf("mismatched GitHub comparison status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestValidGitHubSourceURLRequiresExpectedRepositoryAndRevision(t *testing.T) {
	base := strings.Repeat("a", 40)
	head := strings.Repeat("b", 40)
	tests := []struct {
		name, raw, expected string
		want                bool
	}{
		{"comparison", "https://github.com/acme/app/compare/" + base + "..." + head, "/acme/app/compare/" + base + "..." + head, true},
		{"different repository", "https://github.com/other/app/compare/" + base + "..." + head, "/acme/app/compare/" + base + "..." + head, false},
		{"wrong revision pair", "https://github.com/acme/app/compare/" + base + "..." + strings.Repeat("c", 40), "/acme/app/compare/" + base + "..." + head, false},
		{"file at full commit", "https://github.com/acme/app/blob/" + head + "/backend/retry.go", "/acme/app/blob/", true},
		{"file from another repo", "https://github.com/other/app/blob/" + head + "/backend/retry.go", "/acme/app/blob/", false},
		{"file without full revision", "https://github.com/acme/app/blob/main/backend/retry.go", "/acme/app/blob/", false},
		{"non-GitHub host", "https://github.example/acme/app/compare/" + base + "..." + head, "/acme/app/compare/" + base + "..." + head, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := validGitHubSourceURL(test.raw, test.expected); got != test.want {
				t.Fatalf("validGitHubSourceURL(%q, %q) = %t, want %t", test.raw, test.expected, got, test.want)
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
