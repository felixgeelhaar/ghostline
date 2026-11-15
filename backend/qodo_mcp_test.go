package main

import (
	"bytes"
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
	var cleanupLogs bytes.Buffer
	cleanupStatus := http.StatusNoContent
	server := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		if r.Header.Get("Authorization") != "Bearer qodo-secret" {
			t.Errorf("authorization = %q, want per-request Qodo bearer", r.Header.Get("Authorization"))
		}
		if r.URL.Path != "/v1/tools/mcp/" {
			t.Errorf("request escaped fixed Qodo MCP endpoint: %s", r.URL.Path)
		}
		if r.Method == http.MethodDelete {
			if r.Header.Get("Mcp-Session-Id") != "test-session" {
				t.Errorf("cleanup session header = %q", r.Header.Get("Mcp-Session-Id"))
			}
			w.WriteHeader(cleanupStatus)
			return
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
	})}, logger: slog.New(slog.NewTextHandler(&cleanupLogs, nil))}
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
	cleanupStatus = http.StatusInternalServerError
	secondResult, err := client.fetch(context.Background(), "qodo-secret", prURL)
	if err != nil {
		t.Fatalf("session cleanup failure discarded valid review data: %v", err)
	}
	if len(secondResult.Warnings) != 1 || !strings.Contains(secondResult.Warnings[0], "may be incomplete") {
		t.Fatalf("session cleanup failure changed review completeness warnings: %+v", secondResult.Warnings)
	}
	if !strings.Contains(cleanupLogs.String(), "Qodo MCP session cleanup failed") || !strings.Contains(cleanupLogs.String(), "HTTP 500") {
		t.Fatalf("cleanup failure was not recorded in server logs: %s", cleanupLogs.String())
	}
	if strings.Contains(cleanupLogs.String(), "qodo-secret") || strings.Contains(cleanupLogs.String(), "test-session") {
		t.Fatalf("cleanup log exposed a credential or session identifier: %s", cleanupLogs.String())
	}
	logsBeforeNotFound := cleanupLogs.String()

	cleanupStatus = http.StatusNotFound
	thirdResult, err := client.fetch(context.Background(), "qodo-secret", prURL)
	if err != nil {
		t.Fatalf("already-closed session discarded valid review data: %v", err)
	}
	if len(thirdResult.Warnings) != 1 || !strings.Contains(thirdResult.Warnings[0], "may be incomplete") {
		t.Fatalf("already-closed session changed review completeness warnings: %+v", thirdResult.Warnings)
	}
	if cleanupLogs.String() != logsBeforeNotFound {
		t.Fatalf("already-closed session was logged as a cleanup failure: %s", cleanupLogs.String())
	}
	if strings.Join(requests, ",") != "POST /v1/tools/mcp/,POST /v1/tools/mcp/,POST /v1/tools/mcp/,DELETE /v1/tools/mcp/,POST /v1/tools/mcp/,POST /v1/tools/mcp/,POST /v1/tools/mcp/,DELETE /v1/tools/mcp/,POST /v1/tools/mcp/,POST /v1/tools/mcp/,POST /v1/tools/mcp/,DELETE /v1/tools/mcp/" {
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

func TestQodoReviewDataRejectsRepeatedJSONFields(t *testing.T) {
	tests := []struct {
		name    string
		payload string
	}{
		{
			name:    "findings",
			payload: `{"findings":[],"findings":[{"id":"hidden","title":"Hidden finding"}],"review_runs":[]}`,
		},
		{
			name:    "warnings",
			payload: `{"findings":[],"review_runs":[],"warnings":["partial"],"warnings":[]}`,
		},
		{
			name:    "truncation flag",
			payload: `{"findings":[],"review_runs":[],"qar_operation_result_truncated":true,"qar_operation_result_truncated":false}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var data qodoReviewData
			if err := json.Unmarshal([]byte(test.payload), &data); err == nil || !strings.Contains(err.Error(), "duplicate JSON object key") {
				t.Fatalf("decode error = %v; want duplicate JSON object key", err)
			}
		})
	}
}

func TestQodoReviewDataRejectsCaseInsensitiveRepeatedFields(t *testing.T) {
	tests := []struct {
		name    string
		payload string
	}{
		{
			name:    "findings",
			payload: `{"findings":[{"id":"visible","title":"Visible finding"}],"Findings":[],"review_runs":[]}`,
		},
		{
			name:    "truncation flag",
			payload: `{"findings":[],"review_runs":[],"qar_operation_result_truncated":true,"QAR_OPERATION_RESULT_TRUNCATED":false}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var data qodoReviewData
			if err := json.Unmarshal([]byte(test.payload), &data); err == nil || !strings.Contains(err.Error(), "ambiguous JSON object keys") {
				t.Fatalf("decode error = %v; want ambiguous JSON object keys", err)
			}
		})
	}
}

func TestQodoReviewDataPreservesCaseSensitiveCitationMetadataKeys(t *testing.T) {
	payload := `{"findings":[{"id":"f-1","title":"Keep metadata","evidence":{"citations":[{"metadata":{"key":1,"Key":2}}]}}],"review_runs":[]}`
	var data qodoReviewData
	if err := json.Unmarshal([]byte(payload), &data); err != nil {
		t.Fatalf("case-sensitive citation metadata keys were rejected: %v", err)
	}
	metadata := string(data.Findings[0].Evidence.Citations[0].Metadata)
	if !strings.Contains(metadata, `"key":1`) || !strings.Contains(metadata, `"Key":2`) {
		t.Fatalf("citation metadata keys were not preserved: %s", metadata)
	}
}

func TestQodoReviewClientAcceptsIntegerValuedDecimalResponseID(t *testing.T) {
	client := qodoReviewClient{
		endpoint: "https://qodo.test/v1/tools/mcp/",
		http: &http.Client{Transport: qodoRoundTripperFunc(func(request *http.Request) (*http.Response, error) {
			recorder := httptest.NewRecorder()
			recorder.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(recorder, `{"jsonrpc":"2.0","id":2.0,"result":{"ok":true}}`)
			return recorder.Result(), nil
		})},
	}
	if _, _, err := client.post(context.Background(), "qodo-secret", "", map[string]any{"method": "tools/call"}, 2, true); err != nil {
		t.Fatalf("integer-valued decimal response id was rejected: %v", err)
	}
}

func TestQodoReviewClientRejectsNonIntegerResponseID(t *testing.T) {
	client := qodoReviewClient{
		endpoint: "https://qodo.test/v1/tools/mcp/",
		http: &http.Client{Transport: qodoRoundTripperFunc(func(request *http.Request) (*http.Response, error) {
			recorder := httptest.NewRecorder()
			recorder.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(recorder, `{"jsonrpc":"2.0","id":2.5,"result":{"ok":true}}`)
			return recorder.Result(), nil
		})},
	}
	if _, _, err := client.post(context.Background(), "qodo-secret", "", map[string]any{"method": "tools/call"}, 2, true); err == nil {
		t.Fatal("non-integer response id was accepted")
	}
}

func TestMCPResponseIDMatchesRejectsHugeExponentWithoutExpansion(t *testing.T) {
	if mcpResponseIDMatches(json.RawMessage(`1e1000000000`), 1) {
		t.Fatal("out-of-range response id matched")
	}
	if mcpResponseIDMatches(json.RawMessage(`1e-1000000000`), 0) {
		t.Fatal("fractional response id matched zero")
	}
	if !mcpResponseIDMatches(json.RawMessage(`0e1000000000`), 0) {
		t.Fatal("zero response id with a large exponent did not match zero")
	}
}

func TestJSONNumberAsInt64(t *testing.T) {
	tests := []struct {
		name  string
		raw   string
		want  int64
		valid bool
	}{
		{name: "integer", raw: "2", want: 2, valid: true},
		{name: "decimal integer", raw: "2.0", want: 2, valid: true},
		{name: "negative scale", raw: "20e-1", want: 2, valid: true},
		{name: "positive scale", raw: "2e1", want: 20, valid: true},
		{name: "fractional", raw: "2.5"},
		{name: "fraction below one", raw: "2e-1"},
		{name: "int64 maximum", raw: "9223372036854775807", want: 9223372036854775807, valid: true},
		{name: "int64 minimum", raw: "-9223372036854775808", want: -9223372036854775808, valid: true},
		{name: "int64 overflow", raw: "9223372036854775808"},
		{name: "leading zero", raw: "02"},
		{name: "missing exponent", raw: "2e"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, valid := jsonNumberAsInt64(test.raw)
			if got != test.want || valid != test.valid {
				t.Fatalf("jsonNumberAsInt64(%q) = (%d, %t), want (%d, %t)", test.raw, got, valid, test.want, test.valid)
			}
		})
	}
}

func TestMergeQodoReviewDataCombinesDuplicateFindingEvidenceAndRejectsConflicts(t *testing.T) {
	citation := qodoFindingCitation{Source: "backend/retry.go", Text: "retry()"}
	first := qodoReviewData{Findings: []qodoManagedFinding{{ID: "f-1", Title: "Guard retries", Evidence: &qodoFindingEvidence{Citations: []qodoFindingCitation{citation}}}}, ReviewRuns: []qodoManagedReviewRun{}}
	second := qodoReviewData{Findings: []qodoManagedFinding{{
		ID: "f-1", Title: "Guard retries",
		Evidence: &qodoFindingEvidence{Citations: []qodoFindingCitation{citation, citation}},
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

func TestDeduplicateQodoFindingsMergesRepeatedIDsInOriginalOrder(t *testing.T) {
	values := []qodoManagedFinding{
		{ID: "f-1", Title: "First"},
		{ID: "f-2", Title: "Second"},
		{ID: "f-1", Title: "First", Evidence: &qodoFindingEvidence{Explanation: "first evidence"}},
		{ID: "f-2", Title: "Second", FilePath: "backend/second.go"},
	}

	merged, err := deduplicateQodoFindings(values)
	if err != nil {
		t.Fatal(err)
	}
	if len(merged) != 2 || merged[0].ID != "f-1" || merged[1].ID != "f-2" {
		t.Fatalf("finding order = %+v; want f-1 then f-2", merged)
	}
	if merged[0].Evidence == nil || merged[0].Evidence.Explanation != "first evidence" || merged[1].FilePath != "backend/second.go" {
		t.Fatalf("duplicate details were not merged in place: %+v", merged)
	}
}

func TestDeduplicateQodoReviewRunsMergesRepeatedIDsInOriginalOrder(t *testing.T) {
	values := []qodoManagedReviewRun{
		{ID: "r-1", Status: "completed"},
		{ID: "r-2", Status: "completed"},
		{ID: "r-1", Status: "completed", CommitSHA: strings.Repeat("a", 40)},
	}
	merged, err := deduplicateQodoReviewRuns(values)
	if err != nil {
		t.Fatal(err)
	}
	if len(merged) != 2 || merged[0].ID != "r-1" || merged[1].ID != "r-2" || merged[0].CommitSHA != strings.Repeat("a", 40) {
		t.Fatalf("merged runs = %+v", merged)
	}
}

func TestMergeQodoReviewDataDeduplicatesRepeatedWarnings(t *testing.T) {
	merged, err := mergeQodoReviewData(
		qodoReviewData{Warnings: []string{"partial response"}},
		qodoReviewData{Warnings: []string{"partial response", "other warning"}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(merged.Warnings, "|") != "partial response|other warning" {
		t.Fatalf("warnings = %#v", merged.Warnings)
	}
}

func TestParseQodoToolResultKeepsValidTextWhenStructuredContentIsInvalid(t *testing.T) {
	textBlock := `{"findings":[{"id":"f-1","title":"Keep this finding"}],"review_runs":[]}`
	got, err := parseQodoToolResult(qodoToolResult{
		StructuredContent: json.RawMessage(`{"findings":[`),
		Content:           []qodoToolContent{{Type: "text", Text: textBlock}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Findings) != 1 || got.Findings[0].ID != "f-1" || len(got.Warnings) != 1 {
		t.Fatalf("parsed result = %+v; want finding and partial-data warning", got)
	}
}

func TestParseQodoToolResultMergesCompanionFindingsWithEmptyStructuredContent(t *testing.T) {
	got, err := parseQodoToolResult(qodoToolResult{
		StructuredContent: json.RawMessage(`{"review_session":null,"findings":[],"review_runs":[]}`),
		Content:           []qodoToolContent{{Type: "text", Text: `{"findings":[{"id":"f-1","title":"Companion finding"}],"review_runs":[]}`}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Findings) != 1 || got.Findings[0].ID != "f-1" {
		t.Fatalf("companion finding was lost: %+v", got.Findings)
	}
}

func TestParseQodoToolResultRejectsConflictingAliasesInCompanionData(t *testing.T) {
	got, err := parseQodoToolResult(qodoToolResult{
		StructuredContent: json.RawMessage(`{"review_session":null,"findings":[{"id":"f-1","title":"Valid"}],"review_runs":[]}`),
		Content:           []qodoToolContent{{Type: "text", Text: `{"findings":[{"id":"f-2","title":"Conflicting","git_sha":"` + strings.Repeat("a", 40) + `","gitSha":"` + strings.Repeat("b", 40) + `"}],"review_runs":[]}`}},
	})
	if err == nil || !strings.Contains(err.Error(), "conflicting") {
		t.Fatalf("result = %+v, error = %v; want conflicting-data error", got, err)
	}
}

func TestParseQodoToolResultDoesNotHideConflictBehindLaterInvalidCompanion(t *testing.T) {
	got, err := parseQodoToolResult(qodoToolResult{
		StructuredContent: json.RawMessage(`{"review_session":null,"findings":[{"id":"f-1","title":"Valid"}],"review_runs":[]}`),
		Content: []qodoToolContent{
			{Type: "text", Text: `{"findings":[{"id":"f-2","title":"Conflicting","git_sha":"` + strings.Repeat("a", 40) + `","gitSha":"` + strings.Repeat("b", 40) + `"}],"review_runs":[]}`},
			{Type: "text", Text: `{"findings":[{"id":"","title":"Invalid companion"}],"review_runs":[]}`},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "conflicting") {
		t.Fatalf("result = %+v, error = %v; want earlier conflicting revision rejection", got, err)
	}
}

func TestParseQodoToolResultMarksMalformedReviewCandidateIncomplete(t *testing.T) {
	valid := `{"findings":[{"id":"f-1","title":"Valid finding"}],"review_runs":[]}`
	malformed := `{"findings":[],"review_runs":null}`
	got, err := parseQodoToolResult(qodoToolResult{
		StructuredContent: json.RawMessage(valid),
		Content:           []qodoToolContent{{Type: "text", Text: malformed}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Findings) != 1 || len(got.Warnings) == 0 {
		t.Fatalf("parsed result = %+v; want valid finding and incomplete-data warning", got)
	}
}

func TestParseQodoToolResultIgnoresNonReviewProseWhenStructuredDataIsValid(t *testing.T) {
	got, err := parseQodoToolResult(qodoToolResult{
		StructuredContent: json.RawMessage(`{"findings":[],"review_runs":[]}`),
		Content:           []qodoToolContent{{Type: "text", Text: "Review data follows in structured content."}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Warnings) != 0 {
		t.Fatalf("warnings = %#v; prose should not imply incomplete review data", got.Warnings)
	}
}

func TestQodoReviewDataUnmarshalFiltersBlankWarnings(t *testing.T) {
	var got qodoReviewData
	err := json.Unmarshal([]byte(`{"findings":[],"review_runs":[],"warnings":["  ","source warning",""]}`), &got)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got.Warnings, "|") != "source warning" {
		t.Fatalf("warnings = %#v", got.Warnings)
	}
}

func TestQodoReviewDataWarnsWhenQodoTruncatesExtendedResult(t *testing.T) {
	var got qodoReviewData
	err := json.Unmarshal([]byte(`{"findings":[],"review_runs":[],"qar_operation_result_truncated":true}`), &got)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Warnings) != 1 || !strings.Contains(got.Warnings[0], "truncated") || !strings.Contains(got.Warnings[0], "incomplete") {
		t.Fatalf("warnings = %#v; want explicit truncated-review warning", got.Warnings)
	}
}

func BenchmarkDeduplicateQodoFindingsRepeatedIDs(b *testing.B) {
	const distinctFindings = 5000
	values := make([]qodoManagedFinding, 0, distinctFindings*2)
	for i := 0; i < distinctFindings; i++ {
		values = append(values, qodoManagedFinding{ID: fmt.Sprintf("f-%d", i), Title: "finding"})
	}
	for i := 0; i < distinctFindings; i++ {
		values = append(values, qodoManagedFinding{ID: fmt.Sprintf("f-%d", i), Title: "finding", FilePath: "backend/example.go"})
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := deduplicateQodoFindings(values); err != nil {
			b.Fatal(err)
		}
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

func TestQodoReviewEndpointOmitsMissingSessionStatus(t *testing.T) {
	const sha = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	router := newRouterWithQodoAndLimiter(&githubClient{baseURL: "https://api.github.com", http: http.DefaultClient}, qodoSourceFunc(func(context.Context, string, string) (qodoReviewData, error) {
		return qodoReviewData{
			Availability: "available", Source: "Qodo Agentic Toolbox",
			ReviewSession: &qodoReviewSession{CommitSHA: sha},
			Findings:      []qodoManagedFinding{{ID: "finding-1", Title: "Preserve review data", GitSHA: sha}},
			ReviewRuns: []qodoManagedReviewRun{
				{ID: "run-1", Status: "completed", CommitSHA: sha},
				{ID: "run-2", CommitSHA: sha},
			},
		}, nil
	}), newRateLimiter(defaultRateLimitRequests, defaultRateLimitWindow, time.Now), clientIdentity{})
	req := httptest.NewRequest(http.MethodPost, "/api/qodo-review", strings.NewReader(`{"url":"https://github.com/acme/payments/pull/42"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://ghostline.example")
	req.Header.Set("X-Qodo-API-Key", "qodo-secret")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s; want successful response", res.Code, res.Body.String())
	}
	if strings.Contains(res.Body.String(), `"status":""`) {
		t.Fatalf("missing Qodo status was serialized as an empty string: %s", res.Body.String())
	}
	if !strings.Contains(res.Body.String(), `"commitSha":"`+sha+`"`) {
		t.Fatalf("session revision was lost while omitting status: %s", res.Body.String())
	}
	for _, expected := range []string{`"id":"finding-1"`, `"id":"run-1"`, `"id":"run-2"`} {
		if !strings.Contains(res.Body.String(), expected) {
			t.Fatalf("valid Qodo evidence %s was lost while omitting status: %s", expected, res.Body.String())
		}
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

func TestParseQodoToolResultWarnsForMalformedCamelFindingsCandidate(t *testing.T) {
	result, err := parseQodoToolResult(qodoToolResult{Content: []qodoToolContent{
		{Type: "text", Text: `{"camelFindings":[{"id":"hidden","title":"Candidate without runs"}]}`},
		{Type: "text", Text: `{"findings":[],"review_runs":[]}`},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Warnings) == 0 {
		t.Fatal("malformed camelFindings representation was silently ignored")
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
			name:    "finding conflicting revisions survive unrelated malformed fields",
			payload: `{"id":"finding-1","title":"A finding","gitSha":"` + shaA + `","git_sha":"` + shaB + `","action_level":17}`,
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

func TestDecodeMCPEnvelopeRejectsRepeatedJSONRPCResponseID(t *testing.T) {
	payload := []byte(`{"jsonrpc":"2.0","id":1,"id":2,"result":{"ok":true}}`)
	if _, err := decodeMCPEnvelope(payload, "application/json", 2); err == nil {
		t.Fatal("response with repeated JSON-RPC ids was accepted")
	}
}

func TestDecodeMCPEnvelopePreservesLargeValidMetadataNumbers(t *testing.T) {
	payload := []byte(`{"jsonrpc":"2.0","id":2,"result":{"citation":{"metadata":{"value":1e400}}}}`)
	var envelope mcpEnvelope
	if err := decodeFirstMCPJSON(payload, &envelope); err != nil {
		t.Fatalf("valid metadata number was rejected: %v", err)
	}
	if !strings.Contains(string(envelope.Result), `1e400`) {
		t.Fatalf("result metadata number was changed: %s", envelope.Result)
	}
}

func TestDecodeMCPEnvelopeSelectsIntegerValuedDecimalSSERequestID(t *testing.T) {
	data := []byte("event: message\ndata: " + `{"jsonrpc":"2.0","id":2.0,"result":{"review":"complete"}}` + "\n\n")
	envelope, err := decodeMCPEnvelope(data, "text/event-stream", 2)
	if err != nil {
		t.Fatalf("integer-valued decimal SSE response id was rejected: %v", err)
	}
	if string(envelope.Result) != `{"review":"complete"}` {
		t.Fatalf("result = %s", envelope.Result)
	}
}

func TestDecodeMCPEnvelopeAcceptsCaseInsensitiveEventStreamMediaType(t *testing.T) {
	data := []byte("event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":2,\"result\":{\"review\":\"complete\"}}\n\n")
	if _, err := decodeMCPEnvelope(data, "Text/Event-Stream; charset=utf-8", 2); err != nil {
		t.Fatalf("valid event stream media type rejected: %v", err)
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

func TestDecodeMCPEnvelopeStreamAcceptsCRLFEventBoundaries(t *testing.T) {
	data := []byte("event: message\r\ndata: {\"jsonrpc\":\"2.0\",\"id\":2,\"result\":{\"review\":\"complete\"}}\r\n\r\nevent: keepalive\r\n\r\n")
	envelope, err := decodeMCPEnvelopeStream(bytes.NewReader(data), "text/event-stream", 2)
	if err != nil {
		t.Fatalf("valid CRLF event stream rejected: %v", err)
	}
	if string(envelope.Result) != `{"review":"complete"}` {
		t.Fatalf("result = %s", envelope.Result)
	}
}

func TestDecodeMCPEnvelopeStreamAcceptsStandaloneCREventBoundaries(t *testing.T) {
	data := []byte("event: message\rdata: {\"jsonrpc\":\"2.0\",\"id\":2,\"result\":{\"review\":\"complete\"}}\r\revent: keepalive\r\r")
	envelope, err := decodeMCPEnvelopeStream(bytes.NewReader(data), "text/event-stream", 2)
	if err != nil {
		t.Fatalf("valid CR-delimited event stream rejected: %v", err)
	}
	if string(envelope.Result) != `{"review":"complete"}` {
		t.Fatalf("result = %s", envelope.Result)
	}
}

func TestDecodeMCPEnvelopeStreamReturnsOnStandaloneCRBeforeStreamCloses(t *testing.T) {
	reader, writer := io.Pipe()
	defer reader.Close()
	go func() {
		_, _ = io.WriteString(writer, "event: message\rdata: {\"jsonrpc\":\"2.0\",\"id\":2,\"result\":{\"review\":\"complete\"}}\r\r")
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
		t.Fatal("decoder waited for the open CR-delimited SSE body to close")
	}
	_ = writer.Close()
}

func TestDecodeMCPEnvelopeStreamAcceptsCRLFSeparatedAcrossReads(t *testing.T) {
	reader, writer := io.Pipe()
	defer reader.Close()
	go func() {
		_, _ = io.WriteString(writer, "event: message\r")
		_, _ = io.WriteString(writer, "\ndata: {\"jsonrpc\":\"2.0\",\"id\":2,\"result\":{\"review\":\"complete\"}}\r")
		_, _ = io.WriteString(writer, "\n\r\n")
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

func TestQodoReviewClientClosesSessionWhenInitializationResponseIsInvalid(t *testing.T) {
	var cleanupRequests int
	client := qodoReviewClient{
		endpoint: "https://qodo.test/v1/tools/mcp/",
		http: &http.Client{Transport: qodoRoundTripperFunc(func(request *http.Request) (*http.Response, error) {
			recorder := httptest.NewRecorder()
			if request.Method == http.MethodDelete {
				cleanupRequests++
				if request.Header.Get("Mcp-Session-Id") != "new-session" {
					t.Errorf("cleanup session = %q, want new-session", request.Header.Get("Mcp-Session-Id"))
				}
				recorder.WriteHeader(http.StatusNoContent)
				return recorder.Result(), nil
			}
			recorder.Header().Set("Content-Type", "application/json")
			recorder.Header().Set("Mcp-Session-Id", "new-session")
			_, _ = io.WriteString(recorder, `{"jsonrpc":`)
			return recorder.Result(), nil
		})},
	}

	if _, err := client.fetch(context.Background(), "qodo-secret", "https://github.com/acme/payments/pull/42"); err == nil {
		t.Fatal("invalid initialization response was accepted")
	}
	if cleanupRequests != 1 {
		t.Fatalf("session cleanup requests = %d, want 1 after initialization decode failure", cleanupRequests)
	}
}

func TestQodoReviewClientRejectsUnsupportedNegotiatedProtocolVersion(t *testing.T) {
	var methods []string
	client := qodoReviewClient{
		endpoint: "https://qodo.test/v1/tools/mcp/",
		http: &http.Client{Transport: qodoRoundTripperFunc(func(request *http.Request) (*http.Response, error) {
			recorder := httptest.NewRecorder()
			if request.Method == http.MethodDelete {
				methods = append(methods, "DELETE")
				recorder.WriteHeader(http.StatusNoContent)
				return recorder.Result(), nil
			}
			var body struct {
				Method string `json:"method"`
				ID     int64  `json:"id"`
			}
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				return nil, err
			}
			methods = append(methods, body.Method)
			recorder.Header().Set("Content-Type", "application/json")
			recorder.Header().Set("Mcp-Session-Id", "new-session")
			_, _ = fmt.Fprintf(recorder, `{"jsonrpc":"2.0","id":%d,"result":{"protocolVersion":"2099-01-01","capabilities":{},"serverInfo":{"name":"qodo","version":"test"}}}`, body.ID)
			return recorder.Result(), nil
		})},
	}

	_, err := client.fetch(context.Background(), "qodo-secret", "https://github.com/acme/payments/pull/42")
	if err == nil || !strings.Contains(err.Error(), "unsupported Qodo MCP protocol version") {
		t.Fatalf("fetch error = %v; want unsupported protocol version error", err)
	}
	if strings.Join(methods, ",") != "initialize,DELETE" {
		t.Fatalf("MCP requests after unsupported negotiation = %v; want initialize then cleanup only", methods)
	}
}

func TestDecodeMCPEnvelopeStreamCountsCRLFBytesAgainstLimit(t *testing.T) {
	var data strings.Builder
	data.Grow(maxQodoResponseBytes + 128)
	for range maxQodoResponseBytes / 4 {
		data.WriteString(":x\r\n")
	}
	data.WriteString("data: {\"jsonrpc\":\"2.0\",\"id\":2,\"result\":{\"review\":\"complete\"}}\r\n\r\n")

	_, err := decodeMCPEnvelopeStream(strings.NewReader(data.String()), "text/event-stream", 2)
	if err == nil || !strings.Contains(err.Error(), "size limit") {
		t.Fatalf("oversized CRLF response error = %v; want size-limit error", err)
	}
}

func TestDecodeMCPEnvelopeStreamCountsStandaloneCRBytesAgainstLimit(t *testing.T) {
	var data strings.Builder
	data.Grow(maxQodoResponseBytes + 128)
	for range maxQodoResponseBytes / 3 {
		data.WriteString(":x\r")
	}
	data.WriteString("data: {\"jsonrpc\":\"2.0\",\"id\":2,\"result\":{\"review\":\"complete\"}}\r\r")

	_, err := decodeMCPEnvelopeStream(strings.NewReader(data.String()), "text/event-stream", 2)
	if err == nil || !strings.Contains(err.Error(), "size limit") {
		t.Fatalf("oversized CR response error = %v; want size-limit error", err)
	}
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

func TestQodoReviewEndpointExplainsUpstreamAuthenticationFailures(t *testing.T) {
	const secret = "qodo-secret-value"
	for _, tt := range []struct {
		name       string
		status     int
		wantStatus int
		wantText   string
	}{
		{"expired or revoked key", http.StatusUnauthorized, http.StatusUnauthorized, "may be expired or revoked"},
		{"workspace access denied", http.StatusForbidden, http.StatusForbidden, "workspace permissions"},
		{"rate limited", http.StatusTooManyRequests, http.StatusTooManyRequests, "Try again later"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			router := newRouterWithQodoAndLimiter(&githubClient{baseURL: "https://api.github.com", http: http.DefaultClient}, qodoSourceFunc(func(context.Context, string, string) (qodoReviewData, error) {
				return qodoReviewData{}, qodoServiceError{status: tt.status}
			}), newRateLimiter(defaultRateLimitRequests, defaultRateLimitWindow, time.Now), clientIdentity{})
			req := httptest.NewRequest(http.MethodPost, "/api/qodo-review", strings.NewReader(`{"url":"https://github.com/acme/payments/pull/42"}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Origin", "https://ghostline.example")
			req.Header.Set("X-Qodo-API-Key", secret)
			res := httptest.NewRecorder()
			router.ServeHTTP(res, req)

			if res.Code != tt.wantStatus {
				t.Fatalf("status = %d, body = %s; want %d", res.Code, res.Body.String(), tt.wantStatus)
			}
			if !strings.Contains(res.Body.String(), tt.wantText) {
				t.Fatalf("body = %s; want guidance containing %q", res.Body.String(), tt.wantText)
			}
			if strings.Contains(res.Body.String(), secret) {
				t.Fatal("response exposed the Qodo credential")
			}
		})
	}
}

func TestQodoReviewClientPreservesAuthenticationStatusWithoutUpstreamBody(t *testing.T) {
	const secret = "qodo-secret-value"
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusTooManyRequests} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			client := &qodoReviewClient{
				endpoint: "https://qodo.test/v1/tools/mcp/",
				http: &http.Client{Transport: qodoRoundTripperFunc(func(r *http.Request) (*http.Response, error) {
					if r.Header.Get("Authorization") != "Bearer "+secret {
						t.Errorf("authorization header was not forwarded to the fixed Qodo endpoint")
					}
					return &http.Response{
						StatusCode: status,
						Body:       io.NopCloser(strings.NewReader("upstream detail contains private data")),
						Header:     make(http.Header),
						Request:    r,
					}, nil
				})},
			}
			_, err := client.fetch(context.Background(), secret, "https://github.com/acme/payments/pull/42")
			var serviceErr qodoServiceError
			if !errors.As(err, &serviceErr) || serviceErr.StatusCode() != status {
				t.Fatalf("error = %v; want an upstream status error with code %d", err, status)
			}
			if strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "upstream detail") {
				t.Fatalf("error exposed a credential or upstream response body: %v", err)
			}
		})
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
