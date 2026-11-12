package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	qodoMCPURL           = "https://sdk.qodo.ai/v1/tools/mcp/"
	qodoToolName         = "get-pr-review-findings"
	maxQodoAPIKeyLength  = 2048
	maxQodoResponseBytes = 2 << 20
)

type qodoReviewSource interface {
	fetch(context.Context, string, string) (qodoReviewData, error)
}

type qodoReviewClient struct {
	endpoint string
	http     *http.Client
}

type qodoReviewData struct {
	Availability  string                 `json:"availability"`
	Source        string                 `json:"source"`
	ReviewSession *qodoReviewSession     `json:"reviewSession"`
	Findings      []qodoManagedFinding   `json:"findings"`
	ReviewRuns    []qodoManagedReviewRun `json:"reviewRuns"`
}

type qodoReviewDataWire struct {
	ReviewSession *struct {
		Status    string    `json:"status"`
		CommitSHA string    `json:"commit_sha"`
		StartedAt time.Time `json:"started_at"`
	} `json:"review_session"`
	CamelReviewSession *qodoReviewSession `json:"reviewSession"`
	Findings           []struct {
		ID                string `json:"id"`
		Title             string `json:"title"`
		Description       string `json:"description"`
		Category          string `json:"category"`
		ActionLevel       string `json:"action_level"`
		AttributionStatus string `json:"attribution_status"`
		GitSHA            string `json:"git_sha"`
		Status            string `json:"status"`
		ResolutionReason  string `json:"resolution_reason"`
		ReviewRunID       string `json:"review_run_id"`
		Evidence          *struct {
			Explanation string `json:"explanation"`
			Citations   []struct {
				SourceType string          `json:"source_type"`
				Source     string          `json:"source"`
				Text       string          `json:"text"`
				Context    string          `json:"context"`
				Metadata   json.RawMessage `json:"metadata"`
			} `json:"citations"`
		} `json:"evidence"`
		Location struct {
			FilePath  string `json:"file_path"`
			StartLine int    `json:"start_line"`
			EndLine   int    `json:"end_line"`
			Side      string `json:"side"`
		} `json:"location"`
	} `json:"findings"`
	CamelFindings []qodoManagedFinding `json:"camelFindings"`
	ReviewRuns    []struct {
		ID        string    `json:"id"`
		Status    string    `json:"status"`
		CommitSHA string    `json:"commit_sha"`
		StartedAt time.Time `json:"started_at"`
	} `json:"review_runs"`
	CamelReviewRuns []qodoManagedReviewRun `json:"reviewRuns"`
}

func (data *qodoReviewData) UnmarshalJSON(payload []byte) error {
	var wire qodoReviewDataWire
	if err := json.Unmarshal(payload, &wire); err != nil {
		return err
	}
	if wire.CamelReviewSession != nil {
		data.ReviewSession = wire.CamelReviewSession
	} else if wire.ReviewSession != nil {
		data.ReviewSession = &qodoReviewSession{Status: wire.ReviewSession.Status, CommitSHA: wire.ReviewSession.CommitSHA, StartedAt: wire.ReviewSession.StartedAt}
	}
	data.Findings = wire.CamelFindings
	if data.Findings == nil {
		data.Findings = make([]qodoManagedFinding, 0, len(wire.Findings))
		for _, finding := range wire.Findings {
			var evidence *qodoFindingEvidence
			if finding.Evidence != nil {
				evidence = &qodoFindingEvidence{Explanation: finding.Evidence.Explanation, Citations: make([]qodoFindingCitation, 0, len(finding.Evidence.Citations))}
				for _, citation := range finding.Evidence.Citations {
					evidence.Citations = append(evidence.Citations, qodoFindingCitation{SourceType: citation.SourceType, Source: citation.Source, Text: citation.Text, Context: citation.Context, Metadata: citation.Metadata})
				}
			}
			data.Findings = append(data.Findings, qodoManagedFinding{
				ID: finding.ID, Title: finding.Title, Description: finding.Description, Category: finding.Category,
				ActionLevel: finding.ActionLevel, AttributionStatus: finding.AttributionStatus, GitSHA: finding.GitSHA,
				Status: finding.Status, ResolutionReason: finding.ResolutionReason, ReviewRunID: finding.ReviewRunID,
				Evidence: evidence, FilePath: finding.Location.FilePath, StartLine: finding.Location.StartLine, EndLine: finding.Location.EndLine, Side: finding.Location.Side,
			})
		}
	}
	data.ReviewRuns = wire.CamelReviewRuns
	if data.ReviewRuns == nil {
		data.ReviewRuns = make([]qodoManagedReviewRun, 0, len(wire.ReviewRuns))
		for _, run := range wire.ReviewRuns {
			data.ReviewRuns = append(data.ReviewRuns, qodoManagedReviewRun{ID: run.ID, Status: run.Status, CommitSHA: run.CommitSHA, StartedAt: run.StartedAt})
		}
	}
	return nil
}

type qodoReviewSession struct {
	Status    string    `json:"status"`
	CommitSHA string    `json:"commitSha,omitempty"`
	StartedAt time.Time `json:"startedAt,omitempty"`
}

type qodoManagedFinding struct {
	ID                string               `json:"id"`
	Title             string               `json:"title"`
	Description       string               `json:"description,omitempty"`
	Category          string               `json:"category,omitempty"`
	ActionLevel       string               `json:"actionLevel,omitempty"`
	AttributionStatus string               `json:"attributionStatus,omitempty"`
	GitSHA            string               `json:"gitSha,omitempty"`
	Status            string               `json:"status,omitempty"`
	ResolutionReason  string               `json:"resolutionReason,omitempty"`
	ReviewRunID       string               `json:"reviewRunId,omitempty"`
	Evidence          *qodoFindingEvidence `json:"evidence,omitempty"`
	FilePath          string               `json:"filePath,omitempty"`
	StartLine         int                  `json:"startLine,omitempty"`
	EndLine           int                  `json:"endLine,omitempty"`
	Side              string               `json:"side,omitempty"`
}

type qodoFindingEvidence struct {
	Explanation string                `json:"explanation,omitempty"`
	Citations   []qodoFindingCitation `json:"citations,omitempty"`
}

type qodoFindingCitation struct {
	SourceType string          `json:"sourceType,omitempty"`
	Source     string          `json:"source,omitempty"`
	Text       string          `json:"text,omitempty"`
	Context    string          `json:"context,omitempty"`
	Metadata   json.RawMessage `json:"metadata,omitempty"`
}

type qodoManagedReviewRun struct {
	ID        string    `json:"id"`
	Status    string    `json:"status"`
	CommitSHA string    `json:"commitSha,omitempty"`
	StartedAt time.Time `json:"startedAt,omitempty"`
}

type mcpEnvelope struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func newQodoReviewClient() *qodoReviewClient {
	return &qodoReviewClient{
		endpoint: qodoMCPURL,
		http: &http.Client{
			Timeout: 20 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

func (client *qodoReviewClient) fetch(ctx context.Context, apiKey, prURL string) (qodoReviewData, error) {
	var empty qodoReviewData
	if client == nil || client.http == nil || client.endpoint == "" {
		return empty, errors.New("Qodo review source is not configured")
	}
	if len(apiKey) == 0 || len(apiKey) > maxQodoAPIKeyLength || strings.ContainsAny(apiKey, "\r\n") {
		return empty, errors.New("invalid Qodo API key")
	}
	if _, _, _, err := parsePRURL(prURL); err != nil {
		return empty, err
	}

	endpoint, err := url.Parse(client.endpoint)
	if err != nil || endpoint.Scheme != "https" && endpoint.Host != "127.0.0.1" && endpoint.Host != "localhost" {
		return empty, errors.New("Qodo MCP endpoint must use HTTPS")
	}
	if endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" {
		return empty, errors.New("invalid Qodo MCP endpoint")
	}

	initialize := map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "initialize",
		"params": map[string]any{
			"protocolVersion": "2025-03-26", "capabilities": map[string]any{},
			"clientInfo": map[string]string{"name": "ghostline", "version": "0.1.0"},
		},
	}
	initResponse, sessionID, err := client.post(ctx, apiKey, "", initialize, 1, true)
	if err != nil {
		return empty, err
	}
	var initResult struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if err := json.Unmarshal(initResponse.Result, &initResult); err != nil || initResult.ProtocolVersion == "" {
		return empty, errors.New("Qodo MCP initialization response was invalid")
	}
	if err := client.notifyInitialized(ctx, apiKey, sessionID); err != nil {
		return empty, err
	}

	toolCall := map[string]any{
		"jsonrpc": "2.0", "id": 2, "method": "tools/call",
		"params": map[string]any{
			"name":      qodoToolName,
			"arguments": map[string]any{"pr_url": prURL, "extended": true},
		},
	}
	response, _, err := client.post(ctx, apiKey, sessionID, toolCall, 2, true)
	if err != nil {
		return empty, err
	}
	var toolResult struct {
		IsError           bool            `json:"isError"`
		StructuredContent json.RawMessage `json:"structuredContent"`
		Content           []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(response.Result, &toolResult); err != nil {
		return empty, errors.New("Qodo MCP returned an invalid tool result")
	}
	if toolResult.IsError {
		return empty, errors.New("Qodo could not retrieve structured review data for this pull request")
	}
	var data qodoReviewData
	if len(toolResult.StructuredContent) > 0 && string(toolResult.StructuredContent) != "null" {
		if err := json.Unmarshal(toolResult.StructuredContent, &data); err != nil {
			return empty, errors.New("Qodo returned structured review data in an unsupported format")
		}
	} else {
		decoded := false
		for _, block := range toolResult.Content {
			if block.Type != "text" {
				continue
			}
			if err := json.Unmarshal([]byte(block.Text), &data); err == nil {
				decoded = true
				break
			}
		}
		if !decoded {
			return empty, errors.New("Qodo did not return structured review data")
		}
	}
	if err := validateQodoReviewData(data); err != nil {
		return empty, err
	}
	data.Availability = "available"
	data.Source = "Qodo Agentic Toolbox · get-pr-review-findings"
	return data, nil
}

func (client *qodoReviewClient) notifyInitialized(ctx context.Context, apiKey, sessionID string) error {
	message := map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"}
	_, _, err := client.post(ctx, apiKey, sessionID, message, 0, false)
	return err
}

func (client *qodoReviewClient) post(ctx context.Context, apiKey, sessionID string, message any, expectedID int64, expectResult bool) (mcpEnvelope, string, error) {
	var empty mcpEnvelope
	body, err := json.Marshal(message)
	if err != nil {
		return empty, "", errors.New("could not encode Qodo MCP request")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, client.endpoint, bytes.NewReader(body))
	if err != nil {
		return empty, "", errors.New("could not prepare Qodo MCP request")
	}
	request.Header.Set("Authorization", "Bearer "+apiKey)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	if sessionID != "" {
		request.Header.Set("Mcp-Session-Id", sessionID)
	}
	response, err := client.http.Do(request)
	if err != nil {
		return empty, "", errors.New("could not connect to Qodo review service")
	}
	defer response.Body.Close()
	newSessionID := response.Header.Get("Mcp-Session-Id")
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return empty, newSessionID, fmt.Errorf("Qodo review service returned HTTP %d", response.StatusCode)
	}
	if !expectResult {
		return empty, newSessionID, nil
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxQodoResponseBytes+1))
	if err != nil || len(data) > maxQodoResponseBytes {
		return empty, newSessionID, errors.New("Qodo review response could not be read safely")
	}
	envelope, err := decodeMCPEnvelope(data, response.Header.Get("Content-Type"))
	if err != nil {
		return empty, newSessionID, errors.New("Qodo MCP response was not valid JSON-RPC")
	}
	if envelope.Error != nil {
		return empty, newSessionID, fmt.Errorf("Qodo review service reported an MCP error (%d)", envelope.Error.Code)
	}
	var responseID int64
	if err := json.Unmarshal(envelope.ID, &responseID); err != nil || responseID != expectedID {
		return empty, newSessionID, errors.New("Qodo MCP response did not match the request")
	}
	return envelope, newSessionID, nil
}

func decodeMCPEnvelope(data []byte, contentType string) (mcpEnvelope, error) {
	var envelope mcpEnvelope
	if strings.Contains(contentType, "text/event-stream") {
		scanner := bufio.NewScanner(bytes.NewReader(data))
		scanner.Buffer(make([]byte, 4096), maxQodoResponseBytes)
		var eventData []string
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				if err := decodeFirstMCPJSON([]byte(strings.Join(eventData, "\n")), &envelope); err == nil {
					return envelope, nil
				}
				eventData = nil
				continue
			}
			if value, ok := strings.CutPrefix(line, "data:"); ok {
				eventData = append(eventData, strings.TrimSpace(value))
			}
		}
		if err := scanner.Err(); err != nil {
			return envelope, err
		}
		if err := decodeFirstMCPJSON([]byte(strings.Join(eventData, "\n")), &envelope); err == nil {
			return envelope, nil
		}
		return envelope, errors.New("SSE response had no JSON-RPC result")
	}
	err := decodeFirstMCPJSON(data, &envelope)
	return envelope, err
}

func decodeFirstMCPJSON(data []byte, envelope *mcpEnvelope) error {
	if len(bytes.TrimSpace(data)) == 0 {
		return errors.New("empty MCP response")
	}
	if err := json.Unmarshal(data, envelope); err != nil {
		return err
	}
	if envelope.JSONRPC != "2.0" || len(envelope.Result) == 0 && envelope.Error == nil {
		return errors.New("incomplete JSON-RPC response")
	}
	return nil
}

func validateQodoReviewData(data qodoReviewData) error {
	if data.ReviewSession != nil && strings.TrimSpace(data.ReviewSession.Status) == "" {
		return errors.New("Qodo review session has no status")
	}
	if data.ReviewSession != nil && data.ReviewSession.CommitSHA != "" && !fullSHA.MatchString(strings.ToLower(data.ReviewSession.CommitSHA)) {
		return errors.New("Qodo review session contained an invalid commit SHA")
	}
	for _, finding := range data.Findings {
		if finding.ID == "" || strings.TrimSpace(finding.Title) == "" {
			return errors.New("Qodo returned a finding without its native ID or title")
		}
		if finding.GitSHA != "" && !fullSHA.MatchString(strings.ToLower(finding.GitSHA)) {
			return errors.New("Qodo finding contained an invalid commit SHA")
		}
	}
	for _, run := range data.ReviewRuns {
		if run.ID == "" || strings.TrimSpace(run.Status) == "" {
			return errors.New("Qodo returned a review run without its native ID or status")
		}
		if run.CommitSHA != "" && !fullSHA.MatchString(strings.ToLower(run.CommitSHA)) {
			return errors.New("Qodo review run contained an invalid commit SHA")
		}
	}
	return nil
}

func secureCredentialOrigin(raw string) bool {
	origin, err := url.Parse(raw)
	if err != nil || origin.User != nil || origin.RawQuery != "" || origin.Fragment != "" || origin.Path != "" {
		return false
	}
	host := strings.ToLower(origin.Hostname())
	if origin.Scheme == "https" && host != "" {
		return true
	}
	if origin.Scheme != "http" {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
