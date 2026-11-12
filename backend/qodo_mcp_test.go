package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestQodoReviewClientReadsStructuredReviewThroughMCP(t *testing.T) {
	const prURL = "https://github.com/acme/payments/pull/42"
	var requests []string
	server := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		if r.Header.Get("Authorization") != "Bearer qodo-secret" {
			t.Errorf("authorization = %q, want per-request Qodo bearer", r.Header.Get("Authorization"))
		}
		if r.URL.Path != "/v1/tools/mcp/" {
			t.Errorf("request escaped fixed Qodo MCP endpoint: %s", r.URL.Path)
		}
		var request struct {
			ID     int64          `json:"id"`
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		switch request.Method {
		case "initialize":
			w.Header().Set("Mcp-Session-Id", "test-session")
			writeMCPResult(t, w, request.ID, map[string]any{"protocolVersion": "2025-03-26", "capabilities": map[string]any{}, "serverInfo": map[string]any{"name": "qodo", "version": "test"}})
		case "notifications/initialized":
			if r.Header.Get("Mcp-Session-Id") != "test-session" {
				t.Errorf("initialized session header = %q", r.Header.Get("Mcp-Session-Id"))
			}
			w.WriteHeader(http.StatusAccepted)
		case "tools/call":
			if r.Header.Get("Mcp-Session-Id") != "test-session" {
				t.Errorf("tool session header = %q", r.Header.Get("Mcp-Session-Id"))
			}
			if request.Params["name"] != "get-pr-review-findings" {
				t.Errorf("tool name = %v", request.Params["name"])
			}
			args, _ := request.Params["arguments"].(map[string]any)
			if args["pr_url"] != prURL || args["extended"] != true {
				t.Errorf("tool arguments = %#v", args)
			}
			data := map[string]any{
				"review_session": map[string]any{"status": "completed", "commit_sha": strings.Repeat("a", 40), "started_at": "2026-11-12T09:00:00Z"},
				"findings":       []any{map[string]any{"id": "f-1", "title": "Handle replay safely", "description": "Repeated requests may apply twice.", "category": "Correctness", "action_level": "action_required", "attribution_status": "attributed", "git_sha": strings.Repeat("a", 40), "status": "implemented", "review_run_id": "r-2", "location": map[string]any{"file_path": "backend/replay.go", "start_line": 18, "end_line": 20, "side": "RIGHT", "code_snippet": "if replayed {\n\treturn nil\n}"}, "evidence": map[string]any{"explanation": "The reviewer cited the request handler.", "citations": []any{map[string]any{"source_type": "code", "source": "backend/replay.go", "text": "apply(request)", "context": "inside the replay handler", "metadata": map[string]any{"line": 19}}}}}},
				"review_runs":    []any{map[string]any{"id": "r-2", "status": "completed", "commit_sha": strings.Repeat("b", 40), "started_at": "2026-11-12T10:00:00Z"}},
			}
			text, err := json.Marshal(mustJSON(t, data))
			if err != nil {
				t.Fatal(err)
			}
			invalidText, err := json.Marshal(mustJSON(t, map[string]any{
				"findings": []any{
					map[string]any{"id": "invalid-1", "title": "Invalid SHA", "git_sha": "not-a-commit-sha"},
					map[string]any{"id": "invalid-2", "title": "Another invalid candidate"},
				},
				"review_runs": []any{},
			}))
			if err != nil {
				t.Fatal(err)
			}
			emptyText, err := json.Marshal(`{"review_session":null,"findings":[],"review_runs":[]}`)
			if err != nil {
				t.Fatal(err)
			}
			partialText, err := json.Marshal(`{"findings":[{"id":"f-0","title":"Preserve partial finding"}],"review_runs":[{"id":"r-1","status":"completed"}]}`)
			if err != nil {
				t.Fatal(err)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":{"structuredContent":null   ,"content":[{"type":"text","text":%s},{"type":"text","text":%s},{"type":"text","text":%s},{"type":"text","text":%s}]}}`, request.ID, emptyText, invalidText, partialText, text)
		default:
			t.Errorf("unexpected MCP method %q", request.Method)
			http.Error(w, "unexpected method", http.StatusBadRequest)
		}
	})

	client := qodoReviewClient{endpoint: "https://qodo.test/v1/tools/mcp/", http: &http.Client{Transport: qodoRoundTripperFunc(func(r *http.Request) (*http.Response, error) {
		recorder := httptest.NewRecorder()
		server.ServeHTTP(recorder, r)
		return recorder.Result(), nil
	})}}
	result, err := client.fetch(context.Background(), "qodo-secret", prURL)
	if err != nil {
		t.Fatal(err)
	}
	if result.ReviewSession.Status != "completed" || result.ReviewSession.CommitSHA != strings.Repeat("a", 40) {
		t.Fatalf("session = %+v", result.ReviewSession)
	}
	if len(result.Findings) != 2 || result.Findings[0].ID != "f-0" || result.Findings[1].ID != "f-1" || result.Findings[1].ReviewRunID != "r-2" || result.Findings[1].Status != "implemented" {
		t.Fatalf("findings = %+v", result.Findings)
	}
	if result.Findings[1].FilePath != "backend/replay.go" || result.Findings[1].Side != "RIGHT" || result.Findings[1].CodeSnippet != "if replayed {\n\treturn nil\n}" || result.Findings[1].Evidence == nil || len(result.Findings[1].Evidence.Citations) != 1 || result.Findings[1].Evidence.Citations[0].SourceType != "code" {
		t.Fatalf("finding evidence = %+v", result.Findings[1])
	}
	if len(result.ReviewRuns) != 2 || result.ReviewRuns[0].ID != "r-1" || result.ReviewRuns[1].CommitSHA != strings.Repeat("b", 40) {
		t.Fatalf("review runs = %+v", result.ReviewRuns)
	}
	if len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], "may be incomplete") && !strings.Contains(result.Warnings[0], "displayed review may be incomplete") {
		t.Fatalf("malformed companion response was not surfaced: %+v", result.Warnings)
	}
	if strings.Join(requests, ",") != "POST /v1/tools/mcp/,POST /v1/tools/mcp/,POST /v1/tools/mcp/" {
		t.Fatalf("MCP exchange = %v", requests)
	}
}

func TestQodoReviewDataFailedDecodeDoesNotLeakSessionIntoLaterResponse(t *testing.T) {
	var data qodoReviewData
	malformed := `{"review_session":{"status":"completed","commit_sha":"` + strings.Repeat("a", 40) + `"},"findings":[{"id":42,"title":"Malformed finding"}],"review_runs":[]}`
	if err := json.Unmarshal([]byte(malformed), &data); err == nil {
		t.Fatal("malformed finding was accepted")
	}
	if data.ReviewSession != nil {
		t.Fatalf("failed decode partially mutated review session: %+v", data.ReviewSession)
	}
	if err := json.Unmarshal([]byte(`{"findings":[],"review_runs":[]}`), &data); err != nil {
		t.Fatal(err)
	}
	if data.ReviewSession != nil {
		t.Fatalf("session from failed response leaked into later response: %+v", data.ReviewSession)
	}
}

func TestMergeQodoReviewDataCombinesDuplicateFindingEvidenceAndRejectsConflicts(t *testing.T) {
	first := qodoReviewData{Findings: []qodoManagedFinding{{ID: "f-1", Title: "Guard retries"}}, ReviewRuns: []qodoManagedReviewRun{}}
	second := qodoReviewData{Findings: []qodoManagedFinding{{
		ID: "f-1", Title: "Guard retries",
		Evidence: &qodoFindingEvidence{Citations: []qodoFindingCitation{{Source: "backend/retry.go", Text: "retry()"}}},
	}}, ReviewRuns: []qodoManagedReviewRun{}}
	merged, err := mergeQodoReviewData(first, second)
	if err != nil {
		t.Fatal(err)
	}
	if len(merged.Findings) != 1 || merged.Findings[0].Evidence == nil || len(merged.Findings[0].Evidence.Citations) != 1 {
		t.Fatalf("complementary finding evidence was not merged: %+v", merged.Findings)
	}
	second.Findings[0].Title = "Different finding identity"
	if _, err := mergeQodoReviewData(first, second); err == nil {
		t.Fatal("conflicting duplicate finding was silently discarded")
	}
}

func TestMergeQodoReviewDataAcceptsCaseInsensitiveDuplicateFindingRevisions(t *testing.T) {
	sha := strings.Repeat("a", 40)
	first := qodoReviewData{
		Findings:   []qodoManagedFinding{{ID: "f-1", Title: "Guard retries", GitSHA: sha}},
		ReviewRuns: []qodoManagedReviewRun{},
	}
	second := qodoReviewData{
		Findings:   []qodoManagedFinding{{ID: "f-1", Title: "Guard retries", GitSHA: strings.ToUpper(sha)}},
		ReviewRuns: []qodoManagedReviewRun{},
	}

	merged, err := mergeQodoReviewData(first, second)
	if err != nil {
		t.Fatalf("equivalent SHA casing was treated as conflicting: %v", err)
	}
	if len(merged.Findings) != 1 || merged.Findings[0].GitSHA != sha {
		t.Fatalf("merged findings = %+v; want one finding preserving the first source revision", merged.Findings)
	}
}

func TestQodoReviewEndpointReturnsExplicitNoSessionState(t *testing.T) {
	router := newRouterWithQodoAndLimiter(&githubClient{baseURL: "https://api.github.com", http: http.DefaultClient}, qodoSourceFunc(func(context.Context, string, string) (qodoReviewData, error) {
		return qodoReviewData{Availability: "available", Source: "Qodo Agentic Toolbox", ReviewSession: nil, Findings: []qodoManagedFinding{}, ReviewRuns: []qodoManagedReviewRun{}}, nil
	}), newRateLimiter(defaultRateLimitRequests, defaultRateLimitWindow, time.Now), clientIdentity{})
	req := httptest.NewRequest(http.MethodPost, "/api/qodo-review", strings.NewReader(`{"url":"https://github.com/acme/payments/pull/42"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://ghostline.example")
	req.Header.Set("X-Qodo-API-Key", "qodo-secret")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), `"reviewSession":null`) {
		t.Fatalf("status = %d, body = %s; want successful explicit no-session response", res.Code, res.Body.String())
	}
}

func TestQodoReviewEndpointRejectsOversizedRequestBody(t *testing.T) {
	fetched := false
	router := newRouterWithQodoAndLimiter(&githubClient{baseURL: "https://api.github.com", http: http.DefaultClient}, qodoSourceFunc(func(context.Context, string, string) (qodoReviewData, error) {
		fetched = true
		return qodoReviewData{}, nil
	}), newRateLimiter(defaultRateLimitRequests, defaultRateLimitWindow, time.Now), clientIdentity{})
	body := `{"url":"https://github.com/acme/payments/pull/42","padding":"` + strings.Repeat("x", 16<<10) + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/qodo-review", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://ghostline.example")
	req.Header.Set("X-Qodo-API-Key", "qodo-secret")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s; want oversized request rejection", res.Code, res.Body.String())
	}
	if fetched {
		t.Fatal("Qodo source was called for an oversized request")
	}
}

func TestQodoReviewDataRejectsUnrecognizedEmptyObject(t *testing.T) {
	var data qodoReviewData
	if err := json.Unmarshal([]byte(`{}`), &data); err == nil {
		t.Fatal("empty Qodo object was accepted as an available review")
	}

	// An explicit null session is a valid response shape and must remain distinct
	// from an unrecognized or malformed object.
	if err := json.Unmarshal([]byte(`{"review_session":null,"findings":[],"review_runs":[]}`), &data); err != nil {
		t.Fatalf("explicit no-session response was rejected: %v", err)
	}
	if err := validateQodoReviewData(data); err != nil {
		t.Fatalf("explicit no-session response did not validate: %v", err)
	}
}

func TestQodoReviewDataPreservesCamelCaseFindingDetails(t *testing.T) {
	var data qodoReviewData
	payload := `{"reviewSession":{"status":"completed"},"findings":[{"id":"f-1","title":"Keep details","actionLevel":"action_required","attributionStatus":"attributed","gitSha":"` + strings.Repeat("a", 40) + `","reviewRunId":"r-1","location":{"filePath":"backend/main.go","startLine":12,"endLine":14,"side":"RIGHT","codeSnippet":"if ready {"},"evidence":{"citations":[{"sourceType":"code"}]}}],"reviewRuns":[]}`
	if err := json.Unmarshal([]byte(payload), &data); err != nil {
		t.Fatal(err)
	}
	if len(data.Findings) != 1 {
		t.Fatalf("findings = %+v", data.Findings)
	}
	finding := data.Findings[0]
	if finding.ActionLevel != "action_required" || finding.AttributionStatus != "attributed" || finding.GitSHA != strings.Repeat("a", 40) || finding.ReviewRunID != "r-1" || finding.FilePath != "backend/main.go" || finding.StartLine != 12 || finding.EndLine != 14 || finding.Side != "RIGHT" || finding.CodeSnippet != "if ready {" {
		t.Fatalf("camel-case finding details lost: %+v", finding)
	}
	if finding.Evidence == nil || len(finding.Evidence.Citations) != 1 || finding.Evidence.Citations[0].SourceType != "code" {
		t.Fatalf("camel-case evidence lost: %+v", finding.Evidence)
	}
}

func TestQodoReviewDataPrefersNonEmptyCanonicalFindings(t *testing.T) {
	var data qodoReviewData
	payload := `{"findings":[{"id":"f-1","title":"Canonical finding","evidence":{"explanation":"Keep citation context"}}],"camelFindings":[],"review_runs":[],"reviewRuns":[]}`
	if err := json.Unmarshal([]byte(payload), &data); err != nil {
		t.Fatal(err)
	}
	if len(data.Findings) != 1 || data.Findings[0].ID != "f-1" || data.Findings[0].Evidence == nil || data.Findings[0].Evidence.Explanation != "Keep citation context" {
		t.Fatalf("canonical finding or evidence was dropped: %+v", data.Findings)
	}
}

func TestQodoReviewDataFallsBackToCamelFindingsWhenCanonicalArrayIsEmpty(t *testing.T) {
	var data qodoReviewData
	payload := `{"findings":[],"camelFindings":[{"id":"f-2","title":"Alternate finding"}],"review_runs":[],"reviewRuns":[]}`
	if err := json.Unmarshal([]byte(payload), &data); err != nil {
		t.Fatal(err)
	}
	if len(data.Findings) != 1 || data.Findings[0].ID != "f-2" {
		t.Fatalf("non-empty alternate findings were dropped: %+v", data.Findings)
	}
}

func TestQodoReviewDataPrefersNonEmptyCanonicalReviewRuns(t *testing.T) {
	var data qodoReviewData
	sha := strings.Repeat("b", 40)
	payload := `{"review_session":null,"findings":[],"review_runs":[{"id":"run-1","status":"completed","commit_sha":"` + sha + `"}],"reviewRuns":[]}`
	if err := json.Unmarshal([]byte(payload), &data); err != nil {
		t.Fatal(err)
	}
	if len(data.ReviewRuns) != 1 || data.ReviewRuns[0].ID != "run-1" || data.ReviewRuns[0].CommitSHA != sha {
		t.Fatalf("canonical review run or revision was dropped: %+v", data.ReviewRuns)
	}
}

func TestQodoReviewDataPreservesEntriesFromBothSupportedArrayNames(t *testing.T) {
	var data qodoReviewData
	payload := `{"findings":[{"id":"f-1","title":"Canonical finding"}],"camelFindings":[{"id":"f-2","title":"Alternate finding"}],"review_runs":[{"id":"r-1","status":"completed"}],"reviewRuns":[{"id":"r-2","status":"running"}]}`
	if err := json.Unmarshal([]byte(payload), &data); err != nil {
		t.Fatal(err)
	}
	if len(data.Findings) != 2 || data.Findings[0].ID != "f-1" || data.Findings[1].ID != "f-2" {
		t.Fatalf("entries from both finding arrays were not preserved: %+v", data.Findings)
	}
	if len(data.ReviewRuns) != 2 || data.ReviewRuns[0].ID != "r-1" || data.ReviewRuns[1].ID != "r-2" {
		t.Fatalf("entries from both review-run arrays were not preserved: %+v", data.ReviewRuns)
	}
}

func TestQodoReviewDataDeduplicatesMatchingNativeIDsAcrossAliases(t *testing.T) {
	var data qodoReviewData
	payload := `{"findings":[{"id":"f-1","title":"Finding"}],"camelFindings":[{"id":"f-1","title":"Finding"},{"id":"f-2","title":"Other"}],"review_runs":[{"id":"r-1","status":"completed"}],"reviewRuns":[{"id":"r-1","status":"completed"},{"id":"r-2","status":"running"}]}`
	if err := json.Unmarshal([]byte(payload), &data); err != nil {
		t.Fatal(err)
	}
	if len(data.Findings) != 2 || len(data.ReviewRuns) != 2 {
		t.Fatalf("duplicate native IDs were not collapsed: findings=%+v runs=%+v", data.Findings, data.ReviewRuns)
	}
}

func TestQodoReviewDataRejectsConflictingDuplicateNativeIDs(t *testing.T) {
	var data qodoReviewData
	payload := `{"findings":[{"id":"f-1","title":"First"}],"camelFindings":[{"id":"f-1","title":"Different"}],"review_runs":[],"reviewRuns":[]}`
	if err := json.Unmarshal([]byte(payload), &data); err == nil || !strings.Contains(err.Error(), "conflicting duplicate finding") {
		t.Fatalf("error = %v; want conflicting duplicate finding", err)
	}
}

func TestQodoReviewDataMergesComplementaryDuplicateNativeIDs(t *testing.T) {
	var data qodoReviewData
	payload := `{"warnings":["source warning"],"findings":[{"id":"f-1","title":"Finding"}],"camelFindings":[{"id":"f-1","title":"Finding","description":"Additional source detail"}],"review_runs":[{"id":"r-1","status":"completed"}],"reviewRuns":[{"id":"r-1","status":"completed","commitSha":"` + strings.Repeat("a", 40) + `"}]}`
	if err := json.Unmarshal([]byte(payload), &data); err != nil {
		t.Fatal(err)
	}
	if len(data.Findings) != 1 || data.Findings[0].Description != "Additional source detail" {
		t.Fatalf("complementary finding was not merged: %+v", data.Findings)
	}
	if len(data.ReviewRuns) != 1 || data.ReviewRuns[0].CommitSHA != strings.Repeat("a", 40) {
		t.Fatalf("complementary review run was not merged: %+v", data.ReviewRuns)
	}
	if len(data.Warnings) != 1 || data.Warnings[0] != "source warning" {
		t.Fatalf("source warnings were lost: %+v", data.Warnings)
	}
}

func TestQodoReviewDataFallsBackWhenCanonicalArraysAreNull(t *testing.T) {
	var data qodoReviewData
	payload := `{"findings":null,"camelFindings":[{"id":"f-2","title":"Alternate finding"}],"review_runs":null,"reviewRuns":[{"id":"r-2","status":"completed"}]}`
	if err := json.Unmarshal([]byte(payload), &data); err != nil {
		t.Fatal(err)
	}
	if len(data.Findings) != 1 || data.Findings[0].ID != "f-2" {
		t.Fatalf("alternate findings were not used when canonical value was null: %+v", data.Findings)
	}
	if len(data.ReviewRuns) != 1 || data.ReviewRuns[0].ID != "r-2" {
		t.Fatalf("alternate review runs were not used when canonical value was null: %+v", data.ReviewRuns)
	}
}

func TestQodoReviewDataNormalizesSnakeCaseRevisionFieldsInCamelContainers(t *testing.T) {
	sha := strings.Repeat("a", 40)
	var data qodoReviewData
	payload := `{"reviewSession":{"status":"completed","commit_sha":"` + sha + `","started_at":"2026-11-12T09:00:00Z"},"findings":[],"reviewRuns":[{"id":"run-1","status":"completed","commit_sha":"` + sha + `","started_at":"2026-11-12T09:01:00Z"}]}`
	if err := json.Unmarshal([]byte(payload), &data); err != nil {
		t.Fatal(err)
	}
	if data.ReviewSession == nil || data.ReviewSession.CommitSHA != sha || data.ReviewSession.StartedAt == nil || data.ReviewSession.StartedAt.IsZero() {
		t.Fatalf("review session revision fields were lost: %+v", data.ReviewSession)
	}
	if len(data.ReviewRuns) != 1 || data.ReviewRuns[0].CommitSHA != sha || data.ReviewRuns[0].StartedAt == nil || data.ReviewRuns[0].StartedAt.IsZero() {
		t.Fatalf("review run revision fields were lost: %+v", data.ReviewRuns)
	}
}

func TestQodoReviewDataNormalizesCamelCaseRevisionFieldsInSnakeContainers(t *testing.T) {
	sha := strings.Repeat("c", 40)
	payload := `{"review_session":{"status":"completed","commitSha":"` + sha + `","startedAt":"2026-11-12T09:00:00Z"},"findings":[],"review_runs":[{"id":"run-1","status":"completed","commitSha":"` + sha + `","startedAt":"2026-11-12T09:01:00Z"}]}`
	var data qodoReviewData
	if err := json.Unmarshal([]byte(payload), &data); err != nil {
		t.Fatal(err)
	}
	if data.ReviewSession == nil || data.ReviewSession.CommitSHA != sha || data.ReviewSession.StartedAt == nil || data.ReviewSession.StartedAt.IsZero() {
		t.Fatalf("mixed-case review session revision fields were lost: %+v", data.ReviewSession)
	}
	if len(data.ReviewRuns) != 1 || data.ReviewRuns[0].CommitSHA != sha || data.ReviewRuns[0].StartedAt == nil || data.ReviewRuns[0].StartedAt.IsZero() {
		t.Fatalf("mixed-case review run revision fields were lost: %+v", data.ReviewRuns)
	}
}

func TestQodoReviewDataCombinesBothSessionRepresentations(t *testing.T) {
	sha := strings.Repeat("d", 40)
	payload := `{"review_session":{"status":"completed","commit_sha":"` + sha + `"},"reviewSession":{"status":"completed"},"findings":[],"review_runs":[]}`
	var data qodoReviewData
	if err := json.Unmarshal([]byte(payload), &data); err != nil {
		t.Fatal(err)
	}
	if data.ReviewSession == nil || data.ReviewSession.CommitSHA != sha {
		t.Fatalf("revision from the other supported session representation was lost: %+v", data.ReviewSession)
	}
}

func TestQodoReviewDataRejectsConflictingSessionRevisions(t *testing.T) {
	payload := `{"review_session":{"status":"completed","commit_sha":"` + strings.Repeat("a", 40) + `"},"reviewSession":{"status":"completed","commitSha":"` + strings.Repeat("b", 40) + `"},"findings":[],"review_runs":[]}`
	var data qodoReviewData
	if err := json.Unmarshal([]byte(payload), &data); err == nil || !strings.Contains(err.Error(), "conflicting review-session revisions") {
		t.Fatalf("error = %v; want conflicting revision rejection", err)
	}
}

func TestQodoManagedRecordsRejectConflictingRevisionAliases(t *testing.T) {
	shaA, shaB := strings.Repeat("a", 40), strings.Repeat("b", 40)
	tests := []struct {
		name    string
		payload string
		decode  func([]byte) error
	}{
		{
			name:    "review session",
			payload: `{"status":"completed","commitSha":"` + shaA + `","commit_sha":"` + shaB + `"}`,
			decode: func(payload []byte) error {
				var value qodoReviewSession
				return json.Unmarshal(payload, &value)
			},
		},
		{
			name:    "finding",
			payload: `{"id":"finding-1","title":"A finding","gitSha":"` + shaA + `","git_sha":"` + shaB + `"}`,
			decode: func(payload []byte) error {
				var value qodoManagedFinding
				return json.Unmarshal(payload, &value)
			},
		},
		{
			name:    "review run",
			payload: `{"id":"run-1","status":"completed","commitSha":"` + shaA + `","commit_sha":"` + shaB + `"}`,
			decode: func(payload []byte) error {
				var value qodoManagedReviewRun
				return json.Unmarshal(payload, &value)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.decode([]byte(test.payload))
			if err == nil || !strings.Contains(err.Error(), "conflicting revision fields") {
				t.Fatalf("decode error = %v; want conflicting revision fields", err)
			}
		})
	}
}

func TestQodoFindingRejectsConflictingAttributionStatusAliases(t *testing.T) {
	payload := `{"id":"finding-1","title":"Guard retries","attributionStatus":"implemented","attribution_status":"pending"}`
	var finding qodoManagedFinding
	err := json.Unmarshal([]byte(payload), &finding)
	if err == nil || !strings.Contains(err.Error(), "conflicting attribution statuses") {
		t.Fatalf("decode error = %v; want conflicting attribution statuses", err)
	}
}

func TestQodoManagedRecordRejectsConflictingMetadataAliases(t *testing.T) {
	tests := []struct {
		name, payload string
		decode        func([]byte) error
	}{
		{"action level", `{"id":"f","title":"x","actionLevel":"action_required","action_level":"suggestion"}`, func(b []byte) error { var x qodoManagedFinding; return json.Unmarshal(b, &x) }},
		{"resolution reason", `{"id":"f","title":"x","resolutionReason":"fixed","resolution_reason":"dismissed"}`, func(b []byte) error { var x qodoManagedFinding; return json.Unmarshal(b, &x) }},
		{"citation source type", `{"id":"f","title":"x","evidence":{"citations":[{"sourceType":"code","source_type":"docs"}]}}`, func(b []byte) error { var x qodoManagedFinding; return json.Unmarshal(b, &x) }},
		{"session start time", `{"status":"done","startedAt":"2026-11-12T09:00:00Z","started_at":"2026-11-12T10:00:00Z"}`, func(b []byte) error { var x qodoReviewSession; return json.Unmarshal(b, &x) }},
		{"run start time", `{"id":"r","status":"done","startedAt":"2026-11-12T09:00:00Z","started_at":"2026-11-12T10:00:00Z"}`, func(b []byte) error { var x qodoManagedReviewRun; return json.Unmarshal(b, &x) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.decode([]byte(test.payload)); err == nil {
				t.Fatal("conflicting aliases accepted")
			}
		})
	}
}

func TestQodoFindingRejectsNullCitation(t *testing.T) {
	payload := `{"id":"finding-1","title":"Guard retries","evidence":{"citations":[null]}}`
	var finding qodoManagedFinding
	if err := json.Unmarshal([]byte(payload), &finding); err == nil {
		t.Fatal("null citation was accepted as source evidence")
	}
}

func TestQodoFindingRejectsConflictingReviewRunAndLocationAliases(t *testing.T) {
	tests := []struct {
		name    string
		payload string
	}{
		{
			name:    "review run ID",
			payload: `{"id":"finding-1","title":"Guard retries","reviewRunId":"run-camel","review_run_id":"run-snake"}`,
		},
		{
			name:    "file path",
			payload: `{"id":"finding-1","title":"Guard retries","location":{"file_path":"backend/one.go","filePath":"backend/two.go"}}`,
		},
		{
			name:    "start line",
			payload: `{"id":"finding-1","title":"Guard retries","location":{"start_line":12,"startLine":13}}`,
		},
		{
			name:    "end line",
			payload: `{"id":"finding-1","title":"Guard retries","location":{"end_line":14,"endLine":15}}`,
		},
		{
			name:    "code snippet",
			payload: `{"id":"finding-1","title":"Guard retries","location":{"code_snippet":"first()","codeSnippet":"second()"}}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var finding qodoManagedFinding
			if err := json.Unmarshal([]byte(test.payload), &finding); err == nil || !strings.Contains(err.Error(), "conflicting finding") {
				t.Fatalf("decode error = %v; want conflicting finding fields", err)
			}
		})
	}
}

func TestQodoReviewDataOmitsUnreportedReviewTimes(t *testing.T) {
	var data qodoReviewData
	payload := `{"review_session":{"status":"completed"},"findings":[],"review_runs":[{"id":"run-1","status":"completed"}]}`
	if err := json.Unmarshal([]byte(payload), &data); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "0001-01-01T00:00:00Z") || strings.Contains(string(encoded), "startedAt") {
		t.Fatalf("unreported Qodo start times were serialized as evidence: %s", encoded)
	}
}

func TestDecodeMCPEnvelopeSelectsMatchingSSERequestID(t *testing.T) {
	data := []byte("event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"progress\":true}}\n\nevent: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":2,\"result\":{\"review\":\"complete\"}}\n\n")
	envelope, err := decodeMCPEnvelope(data, "text/event-stream", 2)
	if err != nil {
		t.Fatal(err)
	}
	var id int64
	if err := json.Unmarshal(envelope.ID, &id); err != nil {
		t.Fatal(err)
	}
	if id != 2 || string(envelope.Result) != `{"review":"complete"}` {
		t.Fatalf("selected event = id %d, result %s; want id 2 review result", id, envelope.Result)
	}
}

func TestDecodeMCPEnvelopeStreamReturnsBeforeStreamCloses(t *testing.T) {
	reader, writer := io.Pipe()
	defer reader.Close()
	go func() {
		_, _ = io.WriteString(writer, "event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":2,\"result\":{\"review\":\"complete\"}}\n\n")
	}()
	done := make(chan error, 1)
	go func() {
		envelope, err := decodeMCPEnvelopeStream(reader, "text/event-stream", 2)
		if err == nil && string(envelope.Result) != `{"review":"complete"}` {
			err = fmt.Errorf("unexpected result: %s", envelope.Result)
		}
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("decoder waited for the open SSE body to close")
	}
	_ = writer.Close()
}

func TestQodoReviewDataRejectsNullFindingOrRunArrays(t *testing.T) {
	for _, payload := range []string{
		`{"review_session":null,"findings":null,"review_runs":[]}`,
		`{"review_session":null,"findings":[],"review_runs":null}`,
	} {
		var data qodoReviewData
		if err := json.Unmarshal([]byte(payload), &data); err == nil {
			t.Fatalf("malformed Qodo review arrays accepted: %s", payload)
		}
	}
}

func TestQodoReviewDataRejectsInvalidFindingLineNumbers(t *testing.T) {
	for _, location := range []string{
		`"start_line":-1`,
		`"end_line":-2`,
		`"start_line":12,"end_line":11`,
	} {
		payload := `{"review_session":null,"findings":[{"id":"f-1","title":"Bad location","location":{"file_path":"backend/main.go",` + location + `}}],"review_runs":[]}`
		var data qodoReviewData
		if err := json.Unmarshal([]byte(payload), &data); err != nil {
			t.Fatalf("unmarshal unexpectedly failed for %s: %v", location, err)
		}
		if err := validateQodoReviewData(data); err == nil {
			t.Errorf("invalid Qodo finding location was accepted: %s", location)
		}
	}
}

func TestQodoReviewEndpointLogsFailureWithoutLeakingCredential(t *testing.T) {
	var logs strings.Builder
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	secret := "qodo-secret-value"
	router := newRouterWithQodoLogger(&githubClient{baseURL: "https://api.github.com", http: http.DefaultClient}, qodoSourceFunc(func(context.Context, string, string) (qodoReviewData, error) {
		return qodoReviewData{}, errors.New("upstream rejected credential " + secret)
	}), newRateLimiter(defaultRateLimitRequests, defaultRateLimitWindow, time.Now), clientIdentity{}, logger)
	req := httptest.NewRequest(http.MethodPost, "/api/qodo-review", strings.NewReader(`{"url":"https://github.com/acme/payments/pull/42"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://ghostline.example")
	req.Header.Set("X-Qodo-API-Key", secret)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
	if !strings.Contains(logs.String(), "Qodo structured review request failed") {
		t.Fatalf("server log did not record the Qodo failure: %s", logs.String())
	}
	if strings.Contains(logs.String(), secret) || strings.Contains(res.Body.String(), secret) {
		t.Fatal("Qodo credential leaked into logs or response")
	}
}

func TestQodoReviewEndpointRequiresKeyAndSecureOrigin(t *testing.T) {
	gh := &githubClient{baseURL: "https://api.github.com", http: http.DefaultClient}
	router := newRouterWithQodoAndLimiter(gh, qodoSourceFunc(func(context.Context, string, string) (qodoReviewData, error) {
		return qodoReviewData{Availability: "available", ReviewSession: &qodoReviewSession{Status: "completed"}, Findings: []qodoManagedFinding{}, ReviewRuns: []qodoManagedReviewRun{}}, nil
	}), newRateLimiter(defaultRateLimitRequests, defaultRateLimitWindow, time.Now), clientIdentity{})
	for _, tt := range []struct {
		name, key, origin string
		want              int
	}{
		{"missing key", "", "https://ghostline.example", http.StatusUnauthorized},
		{"insecure origin", "qodo-secret", "http://192.168.1.20:5173", http.StatusForbidden},
		{"secure origin", "qodo-secret", "https://ghostline.example", http.StatusOK},
		{"localhost origin", "qodo-secret", "http://localhost:5173", http.StatusOK},
	} {
		t.Run(tt.name, func(t *testing.T) {
			body := `{"url":"https://github.com/acme/payments/pull/42"}`
			req := httptest.NewRequest(http.MethodPost, "/api/qodo-review", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			if tt.key != "" {
				req.Header.Set("X-Qodo-API-Key", tt.key)
			}
			if tt.origin != "" {
				req.Header.Set("Origin", tt.origin)
			}
			res := httptest.NewRecorder()
			router.ServeHTTP(res, req)
			if res.Code != tt.want {
				t.Fatalf("status = %d, body = %s, want %d", res.Code, res.Body.String(), tt.want)
			}
		})
	}
}

type qodoSourceFunc func(context.Context, string, string) (qodoReviewData, error)

func (source qodoSourceFunc) fetch(ctx context.Context, key, prURL string) (qodoReviewData, error) {
	return source(ctx, key, prURL)
}

func TestQodoReviewEndpointRejectsNonPRAndOverlongCredential(t *testing.T) {
	router := newRouter(&githubClient{baseURL: "https://api.github.com", http: http.DefaultClient})
	for _, tt := range []struct {
		name, body, key string
	}{
		{"invalid URL", `{"url":"https://github.com/acme/payments/issues/42"}`, "key"},
		{"oversized key", `{"url":"https://github.com/acme/payments/pull/42"}`, strings.Repeat("x", maxQodoAPIKeyLength+1)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/qodo-review", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Origin", "https://ghostline.example")
			req.Header.Set("X-Qodo-API-Key", tt.key)
			res := httptest.NewRecorder()
			router.ServeHTTP(res, req)
			if res.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
			}
			if strings.Contains(res.Body.String(), tt.key) {
				t.Fatal("error response echoed the credential")
			}
		})
	}
}

func writeMCPResult(t *testing.T, w http.ResponseWriter, id int64, result any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

type qodoRoundTripperFunc func(*http.Request) (*http.Response, error)

func (f qodoRoundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}
