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

	"github.com/felixgeelhaar/ghostline/backend/internal/domain"
)

const (
	qodoMCPURL           = "https://sdk.qodo.ai/v1/tools/mcp/"
	qodoToolName         = "get-pr-review-findings"
	maxQodoAPIKeyLength  = 2048
	maxQodoRequestBytes  = 8 << 10
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
	Warnings      []string               `json:"warnings,omitempty"`
}

type qodoReviewDataWire struct {
	ReviewSession      *qodoReviewSession     `json:"review_session"`
	CamelReviewSession *qodoReviewSession     `json:"reviewSession"`
	Findings           []json.RawMessage      `json:"findings"`
	CamelFindings      []qodoManagedFinding   `json:"camelFindings"`
	ReviewRuns         []qodoManagedReviewRun `json:"review_runs"`
	CamelReviewRuns    []qodoManagedReviewRun `json:"reviewRuns"`
	Warnings           []string               `json:"warnings"`
}

func (data *qodoReviewData) UnmarshalJSON(payload []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(payload, &fields); err != nil {
		return err
	}
	if fields == nil {
		return errors.New("Qodo review response must be an object")
	}
	knownFields := []string{"review_session", "reviewSession", "findings", "camelFindings", "review_runs", "reviewRuns"}
	recognized := false
	for _, name := range knownFields {
		if _, ok := fields[name]; ok {
			recognized = true
			break
		}
	}
	if !recognized {
		return errors.New("Qodo review response did not contain recognized review fields")
	}
	findings, hasFindings := fields["findings"]
	if !hasFindings || bytes.Equal(bytes.TrimSpace(findings), []byte("null")) {
		findings, hasFindings = fields["camelFindings"]
	}
	runs, hasRuns := fields["review_runs"]
	if !hasRuns || bytes.Equal(bytes.TrimSpace(runs), []byte("null")) {
		runs, hasRuns = fields["reviewRuns"]
	}
	if !hasFindings || bytes.Equal(bytes.TrimSpace(findings), []byte("null")) {
		return errors.New("Qodo review response must contain a findings array")
	}
	if !hasRuns || bytes.Equal(bytes.TrimSpace(runs), []byte("null")) {
		return errors.New("Qodo review response must contain a review-runs array")
	}

	var normalized qodoReviewData
	var wire qodoReviewDataWire
	if err := json.Unmarshal(payload, &wire); err != nil {
		return err
	}
	reviewSession, err := mergeQodoReviewSessions(wire.CamelReviewSession, wire.ReviewSession)
	if err != nil {
		return err
	}
	normalized.ReviewSession = reviewSession
	normalized.Warnings = append([]string(nil), wire.Warnings...)
	normalized.Findings = make([]qodoManagedFinding, 0, len(wire.Findings)+len(wire.CamelFindings))
	for _, raw := range wire.Findings {
		var finding qodoManagedFinding
		if err := json.Unmarshal(raw, &finding); err != nil {
			return err
		}
		normalized.Findings = append(normalized.Findings, finding)
	}
	normalized.Findings = append(normalized.Findings, wire.CamelFindings...)
	normalized.Findings, err = deduplicateQodoFindings(normalized.Findings)
	if err != nil {
		return err
	}
	normalized.ReviewRuns = make([]qodoManagedReviewRun, 0, len(wire.ReviewRuns)+len(wire.CamelReviewRuns))
	normalized.ReviewRuns = append(normalized.ReviewRuns, wire.ReviewRuns...)
	normalized.ReviewRuns = append(normalized.ReviewRuns, wire.CamelReviewRuns...)
	normalized.ReviewRuns, err = deduplicateQodoReviewRuns(normalized.ReviewRuns)
	if err != nil {
		return err
	}
	*data = normalized
	return nil
}

func deduplicateQodoFindings(values []qodoManagedFinding) ([]qodoManagedFinding, error) {
	indexes := make(map[string]int, len(values))
	result := make([]qodoManagedFinding, 0, len(values))
	for _, value := range values {
		if index, exists := indexes[value.ID]; exists {
			merged, err := mergeQodoFindings(result[index], value)
			if err != nil {
				return nil, fmt.Errorf("Qodo returned a conflicting duplicate finding %q: %w", value.ID, err)
			}
			result[index] = merged
			continue
		}
		indexes[value.ID] = len(result)
		result = append(result, value)
	}
	return result, nil
}

func deduplicateQodoReviewRuns(values []qodoManagedReviewRun) ([]qodoManagedReviewRun, error) {
	seen := make(map[string]qodoManagedReviewRun, len(values))
	result := make([]qodoManagedReviewRun, 0, len(values))
	for _, value := range values {
		if prior, exists := seen[value.ID]; exists {
			merged, err := mergeQodoReviewRuns(prior, value)
			if err != nil {
				return nil, fmt.Errorf("Qodo returned a conflicting duplicate review run %q: %w", value.ID, err)
			}
			seen[value.ID] = merged
			for i := range result {
				if result[i].ID == value.ID {
					result[i] = merged
					break
				}
			}
			continue
		}
		seen[value.ID] = value
		result = append(result, value)
	}
	return result, nil
}

func mergeQodoFindings(left, right qodoManagedFinding) (qodoManagedFinding, error) {
	merged := left
	gitSHA, err := mergeFindingRevisionAliases("finding revisions", merged.GitSHA, right.GitSHA)
	if err != nil {
		return qodoManagedFinding{}, err
	}
	merged.GitSHA = gitSHA
	mergeString := func(field string, a, b *string) error {
		value, err := mergeFindingStringAliases(field, *a, *b)
		if err == nil {
			*a = value
		}
		return err
	}
	for _, item := range []struct {
		field string
		a, b  *string
	}{
		{"titles", &merged.Title, &right.Title}, {"descriptions", &merged.Description, &right.Description}, {"categories", &merged.Category, &right.Category}, {"action levels", &merged.ActionLevel, &right.ActionLevel}, {"attribution statuses", &merged.AttributionStatus, &right.AttributionStatus}, {"statuses", &merged.Status, &right.Status}, {"resolution reasons", &merged.ResolutionReason, &right.ResolutionReason}, {"review-run IDs", &merged.ReviewRunID, &right.ReviewRunID}, {"file paths", &merged.FilePath, &right.FilePath}, {"diff sides", &merged.Side, &right.Side}, {"code snippets", &merged.CodeSnippet, &right.CodeSnippet},
	} {
		if err := mergeString(item.field, item.a, item.b); err != nil {
			return qodoManagedFinding{}, err
		}
	}
	merged.StartLine, err = mergeFindingIntAliases("start lines", merged.StartLine, right.StartLine)
	if err != nil {
		return qodoManagedFinding{}, err
	}
	merged.EndLine, err = mergeFindingIntAliases("end lines", merged.EndLine, right.EndLine)
	if err != nil {
		return qodoManagedFinding{}, err
	}
	if right.Evidence != nil {
		if merged.Evidence == nil {
			merged.Evidence = right.Evidence
		} else {
			explanation, mergeErr := mergeFindingStringAliases("evidence explanations", merged.Evidence.Explanation, right.Evidence.Explanation)
			if mergeErr != nil {
				return qodoManagedFinding{}, mergeErr
			}
			merged.Evidence.Explanation = explanation
			for _, citation := range right.Evidence.Citations {
				encoded, _ := json.Marshal(citation)
				duplicate := false
				for _, current := range merged.Evidence.Citations {
					currentEncoded, _ := json.Marshal(current)
					if bytes.Equal(encoded, currentEncoded) {
						duplicate = true
						break
					}
				}
				if !duplicate {
					merged.Evidence.Citations = append(merged.Evidence.Citations, citation)
				}
			}
		}
	}
	return merged, nil
}

func mergeQodoReviewRuns(left, right qodoManagedReviewRun) (qodoManagedReviewRun, error) {
	status, err := mergeFindingStringAliases("review-run statuses", left.Status, right.Status)
	if err != nil {
		return qodoManagedReviewRun{}, err
	}
	sha, err := mergeRevisionAliases(left.CommitSHA, right.CommitSHA)
	if err != nil {
		return qodoManagedReviewRun{}, err
	}
	startedAt, err := mergeQodoTimes("review-run start times", left.StartedAt, right.StartedAt)
	if err != nil {
		return qodoManagedReviewRun{}, err
	}
	return qodoManagedReviewRun{ID: left.ID, Status: status, CommitSHA: sha, StartedAt: startedAt}, nil
}

func mergeQodoReviewSessions(preferred, fallback *qodoReviewSession) (*qodoReviewSession, error) {
	if preferred == nil {
		return fallback, nil
	}
	if fallback == nil {
		return preferred, nil
	}

	merged := *preferred
	if merged.Status == "" {
		merged.Status = fallback.Status
	} else if fallback.Status != "" && !strings.EqualFold(merged.Status, fallback.Status) {
		return nil, errors.New("Qodo returned conflicting review-session statuses")
	}
	if merged.CommitSHA == "" {
		merged.CommitSHA = fallback.CommitSHA
	} else if fallback.CommitSHA != "" && !strings.EqualFold(merged.CommitSHA, fallback.CommitSHA) {
		return nil, errors.New("Qodo returned conflicting review-session revisions")
	}
	if merged.StartedAt == nil {
		merged.StartedAt = fallback.StartedAt
	} else if fallback.StartedAt != nil && !merged.StartedAt.Equal(*fallback.StartedAt) {
		return nil, errors.New("Qodo returned conflicting review-session start times")
	}
	return &merged, nil
}

type qodoReviewSession struct {
	Status    string     `json:"status"`
	CommitSHA string     `json:"commitSha,omitempty"`
	StartedAt *time.Time `json:"startedAt,omitempty"`
}

func (session *qodoReviewSession) UnmarshalJSON(payload []byte) error {
	type plain qodoReviewSession
	var decoded plain
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return err
	}
	var snake struct {
		CommitSHA string     `json:"commit_sha"`
		StartedAt *time.Time `json:"started_at"`
	}
	if err := json.Unmarshal(payload, &snake); err != nil {
		return err
	}
	commitSHA, err := mergeRevisionAliases(decoded.CommitSHA, snake.CommitSHA)
	if err != nil {
		return err
	}
	decoded.CommitSHA = commitSHA
	decoded.StartedAt, err = mergeQodoTimes("review-session start times", decoded.StartedAt, snake.StartedAt)
	if err != nil {
		return err
	}
	*session = qodoReviewSession(decoded)
	return nil
}

func mergeQodoTimes(field string, preferred, fallback *time.Time) (*time.Time, error) {
	if preferred != nil && fallback != nil && !preferred.Equal(*fallback) {
		return nil, fmt.Errorf("Qodo returned conflicting %s", field)
	}
	if preferred != nil {
		return preferred, nil
	}
	return fallback, nil
}

func mergeRevisionAliases(preferred, fallback string) (string, error) {
	if preferred != "" && fallback != "" && !strings.EqualFold(preferred, fallback) {
		return "", errors.New("Qodo returned conflicting revision fields")
	}
	if preferred != "" {
		return preferred, nil
	}
	return fallback, nil
}

func mergeAttributionStatusAliases(preferred, fallback string) (string, error) {
	if preferred != "" && fallback != "" && !strings.EqualFold(preferred, fallback) {
		return "", errors.New("Qodo returned conflicting attribution statuses")
	}
	if preferred != "" {
		return preferred, nil
	}
	return fallback, nil
}

func mergeFindingStringAliases(field string, values ...string) (string, error) {
	return domain.MergeQodoStringAliases("finding "+field, values...)
}

// mergeFindingRevisionAliases treats hexadecimal SHA casing as equivalent while
// preserving the first source value for display and provenance.
func mergeFindingRevisionAliases(field, preferred, fallback string) (string, error) {
	if preferred != "" && fallback != "" && !strings.EqualFold(preferred, fallback) {
		return "", fmt.Errorf("Qodo returned conflicting %s", field)
	}
	if preferred != "" {
		return preferred, nil
	}
	return fallback, nil
}

func mergeFindingIntAliases(field string, values ...int) (int, error) {
	return domain.MergeQodoIntAliases("finding "+field, values...)
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
	CodeSnippet       string               `json:"codeSnippet,omitempty"`
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

func (citation *qodoFindingCitation) UnmarshalJSON(payload []byte) error {
	if bytes.Equal(bytes.TrimSpace(payload), []byte("null")) {
		return errors.New("Qodo returned a null citation")
	}
	type plain qodoFindingCitation
	var decoded plain
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return err
	}
	var snake struct {
		SourceType string `json:"source_type"`
	}
	if err := json.Unmarshal(payload, &snake); err != nil {
		return err
	}
	*citation = qodoFindingCitation(decoded)
	sourceType, err := mergeFindingStringAliases("citation source types", citation.SourceType, snake.SourceType)
	if err != nil {
		return err
	}
	citation.SourceType = sourceType
	return nil
}

func (finding *qodoManagedFinding) UnmarshalJSON(payload []byte) error {
	type plain qodoManagedFinding
	var decoded plain
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return err
	}
	var snake struct {
		ActionLevel       string `json:"action_level"`
		AttributionStatus string `json:"attribution_status"`
		GitSHA            string `json:"git_sha"`
		ResolutionReason  string `json:"resolution_reason"`
		ReviewRunID       string `json:"review_run_id"`
		Location          struct {
			FilePath       string `json:"file_path"`
			CamelFilePath  string `json:"filePath"`
			StartLine      int    `json:"start_line"`
			CamelStartLine int    `json:"startLine"`
			EndLine        int    `json:"end_line"`
			CamelEndLine   int    `json:"endLine"`
			Side           string `json:"side"`
			CodeSnippet    string `json:"code_snippet"`
			CamelSnippet   string `json:"codeSnippet"`
		} `json:"location"`
	}
	if err := json.Unmarshal(payload, &snake); err != nil {
		return err
	}
	*finding = qodoManagedFinding(decoded)
	actionLevel, err := mergeFindingStringAliases("action levels", finding.ActionLevel, snake.ActionLevel)
	if err != nil {
		return err
	}
	finding.ActionLevel = actionLevel
	attributionStatus, err := mergeAttributionStatusAliases(finding.AttributionStatus, snake.AttributionStatus)
	if err != nil {
		return err
	}
	finding.AttributionStatus = attributionStatus
	gitSHA, err := mergeRevisionAliases(finding.GitSHA, snake.GitSHA)
	if err != nil {
		return err
	}
	finding.GitSHA = gitSHA
	resolutionReason, err := mergeFindingStringAliases("resolution reasons", finding.ResolutionReason, snake.ResolutionReason)
	if err != nil {
		return err
	}
	finding.ResolutionReason = resolutionReason
	reviewRunID, err := mergeFindingStringAliases("review-run IDs", finding.ReviewRunID, snake.ReviewRunID)
	if err != nil {
		return err
	}
	finding.ReviewRunID = reviewRunID
	filePath, err := mergeFindingStringAliases("file paths", finding.FilePath, snake.Location.FilePath, snake.Location.CamelFilePath)
	if err != nil {
		return err
	}
	finding.FilePath = filePath
	startLine, err := mergeFindingIntAliases("start lines", finding.StartLine, snake.Location.StartLine, snake.Location.CamelStartLine)
	if err != nil {
		return err
	}
	finding.StartLine = startLine
	endLine, err := mergeFindingIntAliases("end lines", finding.EndLine, snake.Location.EndLine, snake.Location.CamelEndLine)
	if err != nil {
		return err
	}
	finding.EndLine = endLine
	side, err := mergeFindingStringAliases("diff sides", finding.Side, snake.Location.Side)
	if err != nil {
		return err
	}
	finding.Side = side
	codeSnippet, err := mergeFindingStringAliases("code snippets", finding.CodeSnippet, snake.Location.CodeSnippet, snake.Location.CamelSnippet)
	if err != nil {
		return err
	}
	finding.CodeSnippet = codeSnippet
	return nil
}

type qodoManagedReviewRun struct {
	ID        string     `json:"id"`
	Status    string     `json:"status"`
	CommitSHA string     `json:"commitSha,omitempty"`
	StartedAt *time.Time `json:"startedAt,omitempty"`
}

func (run *qodoManagedReviewRun) UnmarshalJSON(payload []byte) error {
	type plain qodoManagedReviewRun
	var decoded plain
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return err
	}
	var snake struct {
		CommitSHA string     `json:"commit_sha"`
		StartedAt *time.Time `json:"started_at"`
	}
	if err := json.Unmarshal(payload, &snake); err != nil {
		return err
	}
	commitSHA, err := mergeRevisionAliases(decoded.CommitSHA, snake.CommitSHA)
	if err != nil {
		return err
	}
	decoded.CommitSHA = commitSHA
	decoded.StartedAt, err = mergeQodoTimes("review-run start times", decoded.StartedAt, snake.StartedAt)
	if err != nil {
		return err
	}
	*run = qodoManagedReviewRun(decoded)
	return nil
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
	structuredContent := bytes.TrimSpace(toolResult.StructuredContent)
	if len(structuredContent) > 0 && !bytes.Equal(structuredContent, []byte("null")) {
		if err := json.Unmarshal(structuredContent, &data); err != nil {
			return empty, errors.New("Qodo returned structured review data in an unsupported format")
		}
	} else {
		decoded := false
		incompleteCandidates := false
		var candidateValidationError error
		for _, block := range toolResult.Content {
			if block.Type != "text" {
				continue
			}
			var candidate qodoReviewData
			if err := json.Unmarshal([]byte(block.Text), &candidate); err != nil {
				incompleteCandidates = true
				continue
			}
			if err := validateQodoReviewData(candidate); err != nil {
				incompleteCandidates = true
				candidateValidationError = err
				continue
			}
			if !decoded {
				data = candidate
				decoded = true
				continue
			}
			merged, err := mergeQodoReviewData(data, candidate)
			if err != nil {
				return empty, errors.New("Qodo returned conflicting review data across text blocks")
			}
			data = merged
		}
		if decoded && incompleteCandidates {
			data.Warnings = append(data.Warnings, "Qodo returned an additional text block that could not be interpreted as structured review data; the displayed review may be incomplete.")
		}
		if !decoded {
			if candidateValidationError != nil {
				return empty, candidateValidationError
			}
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

func mergeQodoReviewData(first, second qodoReviewData) (qodoReviewData, error) {
	merged := qodoReviewData{Warnings: append(append([]string(nil), first.Warnings...), second.Warnings...)}
	var err error
	merged.ReviewSession, err = mergeQodoReviewSessions(first.ReviewSession, second.ReviewSession)
	if err != nil {
		return qodoReviewData{}, err
	}
	merged.Findings, err = deduplicateQodoFindings(append(append([]qodoManagedFinding(nil), first.Findings...), second.Findings...))
	if err != nil {
		return qodoReviewData{}, err
	}
	merged.ReviewRuns, err = deduplicateQodoReviewRuns(append(append([]qodoManagedReviewRun(nil), first.ReviewRuns...), second.ReviewRuns...))
	if err != nil {
		return qodoReviewData{}, err
	}
	return merged, nil
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
	contentType := response.Header.Get("Content-Type")
	var envelope mcpEnvelope
	if strings.Contains(contentType, "text/event-stream") {
		envelope, err = decodeMCPEnvelopeStream(io.LimitReader(response.Body, maxQodoResponseBytes+1), contentType, expectedID)
	} else {
		var data []byte
		data, err = io.ReadAll(io.LimitReader(response.Body, maxQodoResponseBytes+1))
		if err == nil && len(data) > maxQodoResponseBytes {
			err = errors.New("Qodo review response exceeded the size limit")
		}
		if err == nil {
			envelope, err = decodeMCPEnvelope(data, contentType, expectedID)
		}
	}
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

func decodeMCPEnvelope(data []byte, contentType string, expectedID int64) (mcpEnvelope, error) {
	if strings.Contains(contentType, "text/event-stream") {
		return decodeMCPEnvelopeStream(bytes.NewReader(data), contentType, expectedID)
	}
	var envelope mcpEnvelope
	err := decodeFirstMCPJSON(data, &envelope)
	return envelope, err
}

func decodeMCPEnvelopeStream(reader io.Reader, contentType string, expectedID int64) (mcpEnvelope, error) {
	var empty mcpEnvelope
	if !strings.Contains(contentType, "text/event-stream") {
		return empty, errors.New("MCP stream decoder requires an event-stream response")
	}
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), maxQodoResponseBytes)
	var eventData []string
	var consumed int
	for scanner.Scan() {
		line := scanner.Text()
		consumed += len(line) + 1
		if consumed > maxQodoResponseBytes {
			return empty, errors.New("Qodo review response exceeded the size limit")
		}
		if line == "" {
			if envelope, ok := matchingMCPEvent(eventData, expectedID); ok {
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
		return empty, err
	}
	if envelope, ok := matchingMCPEvent(eventData, expectedID); ok {
		return envelope, nil
	}
	return empty, errors.New("SSE response had no matching JSON-RPC response")
}

func matchingMCPEvent(eventData []string, expectedID int64) (mcpEnvelope, bool) {
	var candidate mcpEnvelope
	if err := decodeFirstMCPJSON([]byte(strings.Join(eventData, "\n")), &candidate); err != nil {
		return mcpEnvelope{}, false
	}
	var responseID int64
	if json.Unmarshal(candidate.ID, &responseID) != nil || responseID != expectedID {
		return mcpEnvelope{}, false
	}
	return candidate, true
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
	snapshot := domain.QodoReviewSnapshot{}
	if data.ReviewSession != nil {
		snapshot.ReviewSession = &domain.QodoReviewSession{Status: data.ReviewSession.Status, CommitSHA: data.ReviewSession.CommitSHA}
	}
	for _, finding := range data.Findings {
		var evidence *domain.QodoFindingEvidence
		if finding.Evidence != nil {
			evidence = &domain.QodoFindingEvidence{Explanation: finding.Evidence.Explanation}
			for _, citation := range finding.Evidence.Citations {
				evidence.Citations = append(evidence.Citations, domain.QodoFindingCitation{
					SourceType: citation.SourceType, Source: citation.Source, Text: citation.Text,
					Context: citation.Context, HasMetadata: len(citation.Metadata) > 0,
				})
			}
		}
		snapshot.Findings = append(snapshot.Findings, domain.QodoReviewFinding{
			ID: finding.ID, Title: finding.Title, Description: finding.Description, GitSHA: finding.GitSHA,
			FilePath: finding.FilePath, StartLine: finding.StartLine, EndLine: finding.EndLine,
			CodeSnippet: finding.CodeSnippet, Evidence: evidence,
		})
	}
	for _, run := range data.ReviewRuns {
		snapshot.ReviewRuns = append(snapshot.ReviewRuns, domain.QodoReviewRun{ID: run.ID, Status: run.Status, CommitSHA: run.CommitSHA})
	}
	return domain.ValidateQodoReviewSnapshot(snapshot)
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
