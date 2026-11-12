package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

const currentPRSHA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const previousPRSHA = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
const unrelatedSHA = "cccccccccccccccccccccccccccccccccccccccc"

func TestQodoMCPClientFetchesExtendedFindingsAndRuns(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != http.MethodPost || r.URL.Path != "/v1/tools/mcp/" {
			t.Errorf("request = %s %s, want POST to the fixed MCP path", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-personal-key" {
			t.Errorf("authorization header was not forwarded as a bearer token")
		}
		if r.Header.Get("MCP-Protocol-Version") != "2025-03-26" && calls > 1 {
			t.Errorf("request %d protocol version = %q", calls, r.Header.Get("MCP-Protocol-Version"))
		}
		var request struct {
			ID     any            `json:"id"`
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		switch request.Method {
		case "initialize":
			w.Header().Set("Mcp-Session-Id", "session-123")
			_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": map[string]any{"protocolVersion": "2025-03-26", "capabilities": map[string]any{}, "serverInfo": map[string]any{"name": "qodo", "version": "test"}}})
		case "notifications/initialized":
			if r.Header.Get("Mcp-Session-Id") != "session-123" {
				t.Errorf("initialized notification omitted MCP session")
			}
			w.WriteHeader(http.StatusAccepted)
		case "tools/call":
			if r.Header.Get("Mcp-Session-Id") != "session-123" {
				t.Errorf("tool call omitted MCP session")
			}
			if request.Params["name"] != "get-pr-review-findings" {
				t.Errorf("tool name = %v", request.Params["name"])
			}
			args, _ := request.Params["arguments"].(map[string]any)
			if args["pr_url"] != "https://github.com/acme/app/pull/7" || args["extended"] != true {
				t.Errorf("tool arguments = %#v", args)
			}
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			body, _ := json.Marshal(map[string]any{
				"jsonrpc": "2.0", "id": request.ID,
				"result": map[string]any{"structuredContent": structuredQodoFixture()},
			})
			_, _ = fmt.Fprintf(w, "event: message\ndata: %s\n\n", body)
		default:
			t.Errorf("unexpected MCP method %q", request.Method)
			http.Error(w, "unexpected method", http.StatusBadRequest)
		}
	}))
	defer server.Close()

	client := qodoMCPClient{endpoint: server.URL + "/v1/tools/mcp/", http: server.Client()}
	result, err := client.fetchPRReview(t.Context(), "test-personal-key", "https://github.com/acme/app/pull/7")
	if err != nil {
		t.Fatalf("fetchPRReview() error = %v", err)
	}
	if calls != 3 {
		t.Fatalf("MCP requests = %d, want initialize, initialized, and tool call", calls)
	}
	if result.ReviewSession.ID != "run-current" || len(result.Findings) != 1 || len(result.ReviewRuns) != 2 {
		t.Fatalf("decoded Qodo result = %+v", result)
	}
	finding := result.Findings[0]
	if finding.ID != "finding-uuid-1" || finding.GitSHA != currentPRSHA || finding.Status != "open" || len(finding.Locations) != 1 || len(finding.Citations) != 1 || len(finding.ReviewRunIDs) != 1 || finding.ReviewRunIDs[0] != "run-current" {
		t.Fatalf("finding evidence was not preserved: %+v", finding)
	}
}

func TestClassifyQodoRevisionEvidence(t *testing.T) {
	commits := []string{previousPRSHA, currentPRSHA}
	tests := []struct {
		name string
		sha  string
		want string
	}{
		{name: "current", sha: currentPRSHA, want: "current"},
		{name: "stale PR revision", sha: previousPRSHA, want: "stale"},
		{name: "unknown revision", sha: unrelatedSHA, want: "unknown"},
		{name: "missing revision", sha: "", want: "missing"},
		{name: "malformed revision", sha: "abc", want: "unknown"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := classifyQodoRevision(test.sha, currentPRSHA, commits); got != test.want {
				t.Fatalf("classifyQodoRevision(%q) = %q, want %q", test.sha, got, test.want)
			}
		})
	}
}

func structuredQodoFixture() map[string]any {
	return map[string]any{
		"review_session": map[string]any{"run_id": "run-current", "status": "completed", "commit_sha": currentPRSHA, "started_at": "2026-11-12T10:00:00Z", "review_complete": true},
		"findings": []any{map[string]any{
			"id": "finding-uuid-1", "title": "Reject stale ownership", "description": "The owner check can use stale state.", "category": "bug", "action_level": "action_required", "attribution_status": "unclassified", "status": "open", "git_sha": currentPRSHA, "review_run_id": "run-current",
			"locations": []any{map[string]any{"file_path": "backend/cache.go", "start_line": 31, "end_line": 32, "side": "RIGHT", "code_snippet": "if owner == current {"}},
			"evidence":  map[string]any{"citations": []any{map[string]any{"source_type": "pull_request", "source": "https://github.com/acme/app/pull/7", "citation": "The ownership value may be stale."}}},
		}},
		"review_runs": []any{
			map[string]any{"run_id": "run-current", "status": "completed", "commit_sha": currentPRSHA, "started_at": "2026-11-12T10:00:00Z", "review_complete": true},
			map[string]any{"run_id": "run-previous", "status": "completed", "commit_sha": previousPRSHA, "started_at": "2026-11-11T10:00:00Z", "review_complete": true},
		},
	}
}

func TestParseMCPResponseRejectsToolErrorsAndMalformedData(t *testing.T) {
	for name, response := range map[string]string{
		"tool error":                `{"jsonrpc":"2.0","id":2,"result":{"isError":true,"content":[{"type":"text","text":"failed"}]}}`,
		"missing structured fields": `{"jsonrpc":"2.0","id":2,"result":{"structuredContent":{"unexpected":true}}}`,
		"invalid JSON":              `not-json`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseQodoToolResult([]byte(response)); err == nil {
				t.Fatal("parseQodoToolResult() unexpectedly succeeded")
			}
		})
	}
}

func TestQodoReviewKeyValidation(t *testing.T) {
	for _, key := range []string{"", strings.Repeat("k", maxQodoAPIKeyLength+1), "contains\rlinefeed"} {
		if err := validateQodoAPIKey(key); err == nil {
			t.Fatalf("validateQodoAPIKey(%q) unexpectedly succeeded", key)
		}
	}
	if err := validateQodoAPIKey("personal-api-key"); err != nil {
		t.Fatalf("valid key rejected: %v", err)
	}
}

func TestQodoRequestKeyFeatureDefaultsOffInProduction(t *testing.T) {
	for _, test := range []struct {
		name string
		env  map[string]string
		want bool
	}{
		{name: "local default", env: map[string]string{}, want: true},
		{name: "production default", env: map[string]string{"GHOSTLINE_ENV": "production"}, want: false},
		{name: "explicit production opt in", env: map[string]string{"GHOSTLINE_ENV": "production", "GHOSTLINE_QODO_REQUEST_KEY_ENABLED": "true"}, want: true},
		{name: "explicit disable", env: map[string]string{"GHOSTLINE_QODO_REQUEST_KEY_ENABLED": "false"}, want: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := qodoRequestKeyEnabled(func(key string) string { return test.env[key] }); got != test.want {
				t.Fatalf("qodoRequestKeyEnabled() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestLoadStructuredQodoReviewUsesRequestKeyAndDoesNotFillMissingFindingRevision(t *testing.T) {
	var qodoToken string
	qodoServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		qodoToken = r.Header.Get("Authorization")
		var request struct {
			ID     int            `json:"id"`
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode Qodo request: %v", err)
			return
		}
		switch request.Method {
		case "initialize":
			_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": map[string]any{"protocolVersion": qodoMCPVersion, "capabilities": map[string]any{}, "serverInfo": map[string]any{"name": "qodo", "version": "test"}}})
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "tools/call":
			result := map[string]any{"structuredContent": map[string]any{
				"review_session": map[string]any{"run_id": "run-current", "status": "completed", "commit_sha": currentPRSHA},
				"findings":       []any{map[string]any{"id": "finding-current", "title": "Current finding", "git_sha": currentPRSHA, "review_run_id": "run-current"}, map[string]any{"id": "finding-missing-sha", "title": "Revision missing", "review_run_id": "run-current"}},
				"review_runs":    []any{map[string]any{"run_id": "run-current", "commit_sha": currentPRSHA, "status": "completed"}},
			}}
			_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result})
		default:
			t.Errorf("unexpected method %q", request.Method)
		}
	}))
	defer qodoServer.Close()

	githubServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/acme/app/pulls/7":
			_ = json.NewEncoder(w).Encode(map[string]any{"number": 7, "head": map[string]string{"sha": currentPRSHA}, "base": map[string]any{"repo": map[string]any{"name": "app", "owner": map[string]string{"login": "acme"}}}})
		case "/repos/acme/app/pulls/7/commits":
			_, _ = fmt.Fprintf(w, `[{"sha":%q},{"sha":%q}]`, previousPRSHA, currentPRSHA)
		default:
			http.NotFound(w, r)
		}
	}))
	defer githubServer.Close()

	gin.SetMode(gin.TestMode)
	router := newRouterWithQodo(
		&githubClient{baseURL: githubServer.URL, http: githubServer.Client()},
		newRateLimiter(defaultRateLimitRequests, defaultRateLimitWindow, time.Now), clientIdentity{},
		authSettings{Now: time.Now}, &qodoMCPClient{endpoint: qodoServer.URL + "/v1/tools/mcp/", http: qodoServer.Client()}, true,
	)
	request := httptest.NewRequest(http.MethodPost, "/api/qodo/reviews", strings.NewReader(`{"url":"https://github.com/acme/app/pull/7"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Qodo-API-Key", "personal-api-key")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if recorder.Header().Get("Cache-Control") != "no-store" || qodoToken != "Bearer personal-api-key" {
		t.Fatalf("response cache policy %q or Qodo auth forwarding %q was incorrect", recorder.Header().Get("Cache-Control"), qodoToken)
	}
	var got qodoStructuredReviewSet
	if err := json.Unmarshal(recorder.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.CurrentRevisionSHA != currentPRSHA || got.Findings[0].RevisionStatus != "current" || got.Findings[1].RevisionStatus != "missing" || got.Findings[1].GitSHA != "" {
		t.Fatalf("revision evidence = %+v", got)
	}
}

func TestLoadStructuredQodoReviewRejectsDisabledOrMissingKeyBeforeFetching(t *testing.T) {
	for _, test := range []struct {
		name    string
		enabled bool
		key     string
		want    int
	}{
		{name: "disabled", enabled: false, key: "personal-key", want: http.StatusNotFound},
		{name: "missing key", enabled: true, key: "", want: http.StatusBadRequest},
	} {
		t.Run(test.name, func(t *testing.T) {
			gh := &githubClient{baseURL: "http://127.0.0.1:1", http: http.DefaultClient}
			router := newRouterWithQodo(gh, newRateLimiter(30, time.Minute, time.Now), clientIdentity{}, authSettings{Now: time.Now}, nil, test.enabled)
			request := httptest.NewRequest(http.MethodPost, "/api/qodo/reviews", strings.NewReader(`{"url":"https://github.com/acme/app/pull/7"}`))
			request.Header.Set("Content-Type", "application/json")
			if test.key != "" {
				request.Header.Set("X-Qodo-API-Key", test.key)
			}
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)
			if recorder.Code != test.want {
				t.Fatalf("status = %d, body=%s, want %d", recorder.Code, recorder.Body.String(), test.want)
			}
		})
	}
}

func TestLoadStructuredQodoReviewRejectsPRHeadChangeDuringQodoLookup(t *testing.T) {
	changedSHA := "dddddddddddddddddddddddddddddddddddddddd"
	var prReads int
	githubServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/acme/app/pulls/7":
			prReads++
			headSHA := currentPRSHA
			if prReads > 1 {
				headSHA = changedSHA
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"number": 7, "head": map[string]string{"sha": headSHA}, "base": map[string]any{"repo": map[string]any{"name": "app", "owner": map[string]string{"login": "acme"}}}})
		case "/repos/acme/app/pulls/7/commits":
			_, _ = fmt.Fprintf(w, `[{"sha":%q},{"sha":%q}]`, previousPRSHA, currentPRSHA)
		default:
			http.NotFound(w, r)
		}
	}))
	defer githubServer.Close()

	qodoServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			ID     int    `json:"id"`
			Method string `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode Qodo request: %v", err)
			return
		}
		switch request.Method {
		case "initialize":
			w.Header().Set("Mcp-Session-Id", "session-race")
			_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": map[string]any{"protocolVersion": qodoMCPVersion, "capabilities": map[string]any{}, "serverInfo": map[string]any{"name": "qodo", "version": "test"}}})
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "tools/call":
			_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": map[string]any{"structuredContent": map[string]any{"findings": []any{}, "review_runs": []any{}}}})
		default:
			t.Errorf("unexpected Qodo method %q", request.Method)
		}
	}))
	defer qodoServer.Close()

	gin.SetMode(gin.TestMode)
	router := newRouterWithQodo(
		&githubClient{baseURL: githubServer.URL, http: githubServer.Client()},
		newRateLimiter(defaultRateLimitRequests, time.Minute, time.Now), clientIdentity{}, authSettings{Now: time.Now},
		&qodoMCPClient{endpoint: qodoServer.URL, http: qodoServer.Client()}, true,
	)
	request := httptest.NewRequest(http.MethodPost, "/api/qodo/reviews", strings.NewReader(`{"url":"https://github.com/acme/app/pull/7"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Qodo-API-Key", "personal-api-key")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, body=%s, want conflict after PR head changed during lookup", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "changed while Qodo review data was loading") {
		t.Fatalf("conflict response did not explain how to recover: %s", recorder.Body.String())
	}
}
