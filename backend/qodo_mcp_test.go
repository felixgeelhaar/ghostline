package main

import (
	"context"
	"encoding/json"
	"errors"
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
				"findings":       []any{map[string]any{"id": "f-1", "title": "Handle replay safely", "description": "Repeated requests may apply twice.", "category": "Correctness", "action_level": "action_required", "attribution_status": "attributed", "git_sha": strings.Repeat("a", 40), "status": "implemented", "review_run_id": "r-2", "location": map[string]any{"file_path": "backend/replay.go", "start_line": 18, "end_line": 20, "side": "RIGHT"}, "evidence": map[string]any{"explanation": "The reviewer cited the request handler.", "citations": []any{map[string]any{"source_type": "code", "source": "backend/replay.go", "text": "apply(request)", "context": "inside the replay handler", "metadata": map[string]any{"line": 19}}}}}},
				"review_runs":    []any{map[string]any{"id": "r-2", "status": "completed", "commit_sha": strings.Repeat("b", 40), "started_at": "2026-11-12T10:00:00Z"}},
			}
			writeMCPResult(t, w, request.ID, map[string]any{"content": []any{map[string]any{"type": "text", "text": mustJSON(t, data)}}})
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
	if len(result.Findings) != 1 || result.Findings[0].ID != "f-1" || result.Findings[0].ReviewRunID != "r-2" || result.Findings[0].Status != "implemented" {
		t.Fatalf("findings = %+v", result.Findings)
	}
	if result.Findings[0].FilePath != "backend/replay.go" || result.Findings[0].Side != "RIGHT" || result.Findings[0].Evidence == nil || len(result.Findings[0].Evidence.Citations) != 1 || result.Findings[0].Evidence.Citations[0].SourceType != "code" {
		t.Fatalf("finding evidence = %+v", result.Findings[0])
	}
	if len(result.ReviewRuns) != 1 || result.ReviewRuns[0].CommitSHA != strings.Repeat("b", 40) {
		t.Fatalf("review runs = %+v", result.ReviewRuns)
	}
	if strings.Join(requests, ",") != "POST /v1/tools/mcp/,POST /v1/tools/mcp/,POST /v1/tools/mcp/" {
		t.Fatalf("MCP exchange = %v", requests)
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
	payload := `{"reviewSession":{"status":"completed"},"findings":[{"id":"f-1","title":"Keep details","actionLevel":"action_required","attributionStatus":"attributed","gitSha":"` + strings.Repeat("a", 40) + `","reviewRunId":"r-1","location":{"filePath":"backend/main.go","startLine":12,"endLine":14,"side":"RIGHT"},"evidence":{"citations":[{"sourceType":"code"}]}}],"reviewRuns":[]}`
	if err := json.Unmarshal([]byte(payload), &data); err != nil {
		t.Fatal(err)
	}
	if len(data.Findings) != 1 {
		t.Fatalf("findings = %+v", data.Findings)
	}
	finding := data.Findings[0]
	if finding.ActionLevel != "action_required" || finding.AttributionStatus != "attributed" || finding.GitSHA != strings.Repeat("a", 40) || finding.ReviewRunID != "r-1" || finding.FilePath != "backend/main.go" || finding.StartLine != 12 || finding.EndLine != 14 || finding.Side != "RIGHT" {
		t.Fatalf("camel-case finding details lost: %+v", finding)
	}
	if finding.Evidence == nil || len(finding.Evidence.Citations) != 1 || finding.Evidence.Citations[0].SourceType != "code" {
		t.Fatalf("camel-case evidence lost: %+v", finding.Evidence)
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
