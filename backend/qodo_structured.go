package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

const (
	qodoMCPURL           = "https://sdk.qodo.ai/v1/tools/mcp/"
	qodoMCPVersion       = "2025-03-26"
	maxQodoAPIKeyLength  = 4096
	maxQodoMCPResponse   = 2 << 20
	maxQodoPRCommitPages = 50
)

var qodoFullSHA = regexp.MustCompile(`^[0-9a-fA-F]{40}$`)

type qodoMCPClient struct {
	endpoint string
	http     *http.Client
}

func newQodoMCPClient() *qodoMCPClient {
	return &qodoMCPClient{
		endpoint: qodoMCPURL,
		http: &http.Client{
			Timeout: 45 * time.Second,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

type qodoStructuredReviewSet struct {
	Provider           string                    `json:"provider"`
	SourceType         string                    `json:"sourceType"`
	CurrentRevisionSHA string                    `json:"currentRevisionSha"`
	ReviewSession      *qodoStructuredReviewRun  `json:"reviewSession,omitempty"`
	Findings           []qodoStructuredFinding   `json:"findings"`
	ReviewRuns         []qodoStructuredReviewRun `json:"reviewRuns"`
}

type qodoStructuredFinding struct {
	ID                string                   `json:"id"`
	Title             string                   `json:"title"`
	Description       string                   `json:"description,omitempty"`
	Category          string                   `json:"category,omitempty"`
	ActionLevel       string                   `json:"actionLevel,omitempty"`
	AttributionStatus string                   `json:"attributionStatus,omitempty"`
	Status            string                   `json:"status,omitempty"`
	GitSHA            string                   `json:"gitSha,omitempty"`
	RevisionStatus    string                   `json:"revisionStatus"`
	ReviewRunIDs      []string                 `json:"reviewRunIds"`
	Locations         []qodoStructuredLocation `json:"locations"`
	Citations         []qodoStructuredCitation `json:"citations"`
}

type qodoStructuredLocation struct {
	FilePath    string `json:"filePath"`
	StartLine   *int   `json:"startLine,omitempty"`
	EndLine     *int   `json:"endLine,omitempty"`
	Side        string `json:"side,omitempty"`
	CodeSnippet string `json:"codeSnippet,omitempty"`
}

type qodoStructuredCitation struct {
	SourceType string `json:"sourceType,omitempty"`
	Source     string `json:"source,omitempty"`
	Citation   string `json:"citation,omitempty"`
}

type qodoStructuredReviewRun struct {
	ID             string `json:"id"`
	Status         string `json:"status,omitempty"`
	CommitSHA      string `json:"commitSha,omitempty"`
	RevisionStatus string `json:"revisionStatus"`
	StartedAt      string `json:"startedAt,omitempty"`
	ReviewEffort   string `json:"reviewEffort,omitempty"`
	ReviewComplete *bool  `json:"reviewComplete,omitempty"`
}

func (client *qodoMCPClient) fetchPRReview(ctx context.Context, apiKey, prURL string) (qodoToolResult, error) {
	if err := validateQodoAPIKey(apiKey); err != nil {
		return qodoToolResult{}, err
	}
	if client == nil || client.endpoint == "" || client.http == nil {
		return qodoToolResult{}, errors.New("Qodo review integration is unavailable")
	}

	_, sessionID, err := client.postJSONRPC(ctx, apiKey, "", 1, "initialize", map[string]any{
		"protocolVersion": qodoMCPVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]string{"name": "ghostline", "version": "1.0"},
	})
	if err != nil {
		return qodoToolResult{}, err
	}
	if _, err := client.postNotification(ctx, apiKey, sessionID, "notifications/initialized"); err != nil {
		return qodoToolResult{}, err
	}
	response, _, err := client.postJSONRPC(ctx, apiKey, sessionID, 2, "tools/call", map[string]any{
		"name":      "get-pr-review-findings",
		"arguments": map[string]any{"pr_url": prURL, "extended": true},
	})
	if err != nil {
		return qodoToolResult{}, err
	}
	return parseQodoToolResult(response)
}

type qodoRPCEnvelope struct {
	ID     json.RawMessage `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

type qodoToolResult struct {
	ReviewSession *qodoStructuredReviewRun
	Findings      []qodoStructuredFinding
	ReviewRuns    []qodoStructuredReviewRun
}

func (client *qodoMCPClient) postJSONRPC(ctx context.Context, apiKey, sessionID string, id int, method string, params any) ([]byte, string, error) {
	requestBody, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	if err != nil {
		return nil, sessionID, errors.New("Could not prepare the Qodo request.")
	}
	body, responseSession, err := client.post(ctx, apiKey, sessionID, requestBody, true)
	if err != nil {
		return nil, sessionID, err
	}
	var envelope qodoRPCEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil || len(envelope.Result) == 0 || !rpcIDMatches(envelope.ID, id) {
		return nil, sessionID, errors.New("Qodo returned an invalid MCP response.")
	}
	if envelope.Error != nil {
		return nil, sessionID, errors.New("Qodo could not return structured review data.")
	}
	return body, responseSession, nil
}

func (client *qodoMCPClient) postNotification(ctx context.Context, apiKey, sessionID, method string) ([]byte, error) {
	body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": method})
	if err != nil {
		return nil, errors.New("Could not prepare the Qodo request.")
	}
	response, _, err := client.post(ctx, apiKey, sessionID, body, false)
	return response, err
}

func (client *qodoMCPClient) post(ctx context.Context, apiKey, sessionID string, body []byte, expectsResult bool) ([]byte, string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, client.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, sessionID, errors.New("Could not prepare the Qodo request.")
	}
	request.Header.Set("Authorization", "Bearer "+apiKey)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	if sessionID != "" {
		request.Header.Set("Mcp-Session-Id", sessionID)
		request.Header.Set("MCP-Protocol-Version", qodoMCPVersion)
	}
	response, err := client.http.Do(request)
	if err != nil {
		return nil, sessionID, errors.New("Could not reach Qodo. Check the key and try again.")
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusAccepted && !expectsResult {
		return nil, response.Header.Get("Mcp-Session-Id"), nil
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, sessionID, errors.New("Qodo could not return structured review data.")
	}
	limited := io.LimitReader(response.Body, maxQodoMCPResponse+1)
	responseBody, err := io.ReadAll(limited)
	if err != nil || len(responseBody) > maxQodoMCPResponse {
		return nil, sessionID, errors.New("Qodo returned a response that could not be read safely.")
	}
	nextSession := response.Header.Get("Mcp-Session-Id")
	if nextSession == "" {
		nextSession = sessionID
	}
	if strings.HasPrefix(strings.ToLower(response.Header.Get("Content-Type")), "text/event-stream") {
		responseBody, err = qodoSSEMessage(responseBody)
		if err != nil {
			return nil, nextSession, err
		}
	}
	return responseBody, nextSession, nil
}

func qodoSSEMessage(body []byte) ([]byte, error) {
	scanner := bufio.NewScanner(bytes.NewReader(body))
	scanner.Buffer(make([]byte, 4096), maxQodoMCPResponse)
	var data []string
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data:") {
			data = append(data, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	if scanner.Err() != nil {
		return nil, errors.New("Qodo returned an unreadable event stream.")
	}
	for _, message := range data {
		var envelope qodoRPCEnvelope
		if json.Unmarshal([]byte(message), &envelope) == nil && (len(envelope.Result) > 0 || envelope.Error != nil) {
			return []byte(message), nil
		}
	}
	return nil, errors.New("Qodo returned an incomplete event stream.")
}

func rpcIDMatches(raw json.RawMessage, expected int) bool {
	var got int
	return json.Unmarshal(raw, &got) == nil && got == expected
}

func parseQodoToolResult(raw []byte) (qodoToolResult, error) {
	var envelope qodoRPCEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil || len(envelope.Result) == 0 {
		return qodoToolResult{}, errors.New("Qodo returned malformed structured review data.")
	}
	var result struct {
		IsError           bool            `json:"isError"`
		StructuredContent json.RawMessage `json:"structuredContent"`
		Content           []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(envelope.Result, &result); err != nil || result.IsError {
		return qodoToolResult{}, errors.New("Qodo could not return structured review data.")
	}
	data := result.StructuredContent
	if len(data) == 0 || string(data) == "null" {
		for _, item := range result.Content {
			if item.Type == "text" && json.Valid([]byte(item.Text)) {
				data = []byte(item.Text)
				break
			}
		}
	}
	if len(data) == 0 {
		return qodoToolResult{}, errors.New("Qodo returned no structured review evidence.")
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		return qodoToolResult{}, errors.New("Qodo returned malformed structured review data.")
	}
	sessionRaw, hasSession := lookupRaw(top, "review_session", "reviewSession")
	findingsRaw, hasFindings := lookupRaw(top, "findings", "current_findings", "currentFindings")
	runsRaw, hasRuns := lookupRaw(top, "review_runs", "reviewRuns")
	if !hasSession && !hasFindings && !hasRuns {
		return qodoToolResult{}, errors.New("Qodo returned review data in an unsupported format.")
	}
	out := qodoToolResult{Findings: []qodoStructuredFinding{}, ReviewRuns: []qodoStructuredReviewRun{}}
	if hasSession && string(sessionRaw) != "null" {
		run, err := parseQodoRun(sessionRaw)
		if err != nil {
			return qodoToolResult{}, err
		}
		out.ReviewSession = &run
	}
	if hasFindings {
		var items []json.RawMessage
		if err := json.Unmarshal(findingsRaw, &items); err != nil || items == nil {
			return qodoToolResult{}, errors.New("Qodo returned malformed finding data.")
		}
		for _, item := range items {
			finding, err := parseQodoFinding(item)
			if err != nil {
				return qodoToolResult{}, err
			}
			out.Findings = append(out.Findings, finding)
		}
	}
	if hasRuns {
		if err := json.Unmarshal(runsRaw, &out.ReviewRuns); err != nil || out.ReviewRuns == nil {
			return qodoToolResult{}, errors.New("Qodo returned malformed review-run data.")
		}
		for i := range out.ReviewRuns {
			run, err := parseQodoRun(runsRawElement(runsRaw, i))
			if err != nil {
				return qodoToolResult{}, err
			}
			out.ReviewRuns[i] = run
		}
	}
	return out, nil
}

func runsRawElement(raw json.RawMessage, index int) json.RawMessage {
	var items []json.RawMessage
	if json.Unmarshal(raw, &items) != nil || index < 0 || index >= len(items) {
		return nil
	}
	return items[index]
}

func parseQodoFinding(raw json.RawMessage) (qodoStructuredFinding, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return qodoStructuredFinding{}, errors.New("Qodo returned malformed finding data.")
	}
	finding := qodoStructuredFinding{
		ID:                scalarField(fields, "id"),
		Title:             scalarField(fields, "title"),
		Description:       scalarField(fields, "description"),
		Category:          scalarField(fields, "category"),
		ActionLevel:       scalarField(fields, "action_level", "actionLevel"),
		AttributionStatus: scalarField(fields, "attribution_status", "attributionStatus"),
		Status:            scalarField(fields, "status", "lifecycle_status", "lifecycleStatus", "lifecycle"),
		GitSHA:            scalarField(fields, "git_sha", "gitSha"),
		ReviewRunIDs:      []string{},
		Locations:         []qodoStructuredLocation{},
		Citations:         []qodoStructuredCitation{},
	}
	if runID, ok := lookupRaw(fields, "review_run_id", "reviewRunId"); ok {
		if id := scalarString(runID); id != "" {
			finding.ReviewRunIDs = append(finding.ReviewRunIDs, id)
		}
	}
	if runIDs, ok := lookupRaw(fields, "review_run_ids", "reviewRunIds"); ok {
		var values []json.RawMessage
		if json.Unmarshal(runIDs, &values) != nil {
			return qodoStructuredFinding{}, errors.New("Qodo returned malformed finding-to-run links.")
		}
		for _, value := range values {
			if id := scalarString(value); id != "" && !containsQodoString(finding.ReviewRunIDs, id) {
				finding.ReviewRunIDs = append(finding.ReviewRunIDs, id)
			}
		}
	}
	if locationsRaw, ok := lookupRaw(fields, "locations"); ok && string(locationsRaw) != "null" {
		var values []map[string]json.RawMessage
		if json.Unmarshal(locationsRaw, &values) != nil {
			return qodoStructuredFinding{}, errors.New("Qodo returned malformed finding locations.")
		}
		for _, value := range values {
			location := qodoStructuredLocation{
				FilePath:    scalarField(value, "file_path", "filePath"),
				Side:        scalarField(value, "side"),
				CodeSnippet: scalarField(value, "code_snippet", "codeSnippet"),
			}
			location.StartLine = integerField(value, "start_line", "startLine", "line_start", "lineStart", "line")
			location.EndLine = integerField(value, "end_line", "endLine", "line_end", "lineEnd")
			finding.Locations = append(finding.Locations, location)
		}
	}
	if evidence, ok := lookupRaw(fields, "evidence"); ok && string(evidence) != "null" {
		var evidenceFields map[string]json.RawMessage
		if json.Unmarshal(evidence, &evidenceFields) != nil {
			return qodoStructuredFinding{}, errors.New("Qodo returned malformed finding citations.")
		}
		if citationsRaw, ok := lookupRaw(evidenceFields, "citations"); ok {
			var values []map[string]json.RawMessage
			if json.Unmarshal(citationsRaw, &values) != nil {
				return qodoStructuredFinding{}, errors.New("Qodo returned malformed finding citations.")
			}
			for _, value := range values {
				finding.Citations = append(finding.Citations, qodoStructuredCitation{
					SourceType: scalarField(value, "source_type", "sourceType"),
					Source:     scalarField(value, "source"),
					Citation:   scalarField(value, "citation", "text"),
				})
			}
		}
	}
	if finding.ID == "" || finding.Title == "" {
		return qodoStructuredFinding{}, errors.New("Qodo returned a finding without its native ID or title.")
	}
	if finding.GitSHA != "" && !qodoFullSHA.MatchString(finding.GitSHA) {
		// Preserve the source value, but classify malformed or unknown evidence honestly.
		finding.RevisionStatus = "unknown"
	}
	return finding, nil
}

func parseQodoRun(raw json.RawMessage) (qodoStructuredReviewRun, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return qodoStructuredReviewRun{}, errors.New("Qodo returned malformed review-run data.")
	}
	var complete *bool
	if value, ok := lookupRaw(fields, "review_complete", "reviewComplete"); ok {
		var parsed bool
		if json.Unmarshal(value, &parsed) == nil {
			complete = &parsed
		}
	}
	run := qodoStructuredReviewRun{
		ID:             scalarField(fields, "run_id", "runId", "id"),
		Status:         scalarField(fields, "status"),
		CommitSHA:      scalarField(fields, "commit_sha", "commitSha", "git_sha", "gitSha"),
		StartedAt:      scalarField(fields, "started_at", "startedAt"),
		ReviewEffort:   scalarField(fields, "review_effort", "reviewEffort"),
		ReviewComplete: complete,
	}
	if run.ID == "" {
		return run, errors.New("Qodo returned a review run without its native ID.")
	}
	return run, nil
}

func scalarField(fields map[string]json.RawMessage, keys ...string) string {
	value, _ := lookupRaw(fields, keys...)
	return scalarString(value)
}

func integerField(fields map[string]json.RawMessage, keys ...string) *int {
	value, ok := lookupRaw(fields, keys...)
	if !ok {
		return nil
	}
	var number int
	if json.Unmarshal(value, &number) != nil || number < 1 {
		return nil
	}
	return &number
}

func containsQodoString(items []string, value string) bool {
	for _, item := range items {
		if item == value {
			return true
		}
	}
	return false
}

func lookupRaw(fields map[string]json.RawMessage, keys ...string) (json.RawMessage, bool) {
	for _, key := range keys {
		if value, ok := fields[key]; ok {
			return value, true
		}
	}
	return nil, false
}

func scalarString(raw json.RawMessage) string {
	var value string
	if json.Unmarshal(raw, &value) == nil {
		return value
	}
	var number json.Number
	if json.Unmarshal(raw, &number) == nil {
		return number.String()
	}
	return ""
}

func validateQodoAPIKey(key string) error {
	if strings.TrimSpace(key) == "" || len(key) > maxQodoAPIKeyLength || strings.ContainsAny(key, "\r\n\x00") {
		return errors.New("Enter a valid Qodo API key for this request.")
	}
	return nil
}

func classifyQodoRevision(sha, current string, commits []string) string {
	if strings.TrimSpace(sha) == "" {
		return "missing"
	}
	if !qodoFullSHA.MatchString(sha) {
		return "unknown"
	}
	sha = strings.ToLower(sha)
	if sha == strings.ToLower(current) {
		return "current"
	}
	for _, commit := range commits {
		if sha == strings.ToLower(commit) {
			return "stale"
		}
	}
	return "unknown"
}
