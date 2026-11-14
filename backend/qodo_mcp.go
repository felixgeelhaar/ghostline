package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/felixgeelhaar/ghostline/backend/internal/domain"
)

const (
	qodoMCPURL                   = "https://sdk.qodo.ai/v1/tools/mcp/"
	qodoToolName                 = "get-pr-review-findings"
	qodoMCPProtocolVersion       = "2025-03-26"
	maxQodoAPIKeyLength          = 2048
	maxQodoRequestBytes          = 8 << 10
	maxQodoResponseBytes         = 2 << 20
	qodoTruncatedResponseWarning = "Qodo truncated the extended review response; the displayed review may be incomplete."
)

type qodoReviewSource interface {
	fetch(context.Context, string, string) (qodoReviewData, error)
}

type qodoServiceError struct {
	status int
}

func (err qodoServiceError) Error() string {
	return fmt.Sprintf("Qodo review service returned HTTP %d", err.status)
}

func (err qodoServiceError) StatusCode() int {
	return err.status
}

type qodoReviewClient struct {
	endpoint string
	http     *http.Client
	logger   *slog.Logger
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
	ResultTruncated    bool                   `json:"qar_operation_result_truncated"`
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
	for _, warning := range wire.Warnings {
		if warning = strings.TrimSpace(warning); warning != "" {
			normalized.Warnings = append(normalized.Warnings, warning)
		}
	}
	if wire.ResultTruncated {
		normalized.Warnings = append(normalized.Warnings, qodoTruncatedResponseWarning)
	}
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
	findings := make([]domain.QodoReviewFinding, 0, len(values))
	for _, value := range values {
		findings = append(findings, findingToDomain(value))
	}
	merged, err := domain.MergeQodoReviewFindings(findings)
	if err != nil {
		return nil, err
	}
	result := make([]qodoManagedFinding, 0, len(merged))
	for _, value := range merged {
		result = append(result, findingFromDomain(value))
	}
	return result, nil
}

func deduplicateQodoReviewRuns(values []qodoManagedReviewRun) ([]qodoManagedReviewRun, error) {
	runs := make([]domain.QodoReviewRun, 0, len(values))
	for _, value := range values {
		runs = append(runs, domain.QodoReviewRun{ID: value.ID, Status: value.Status, CommitSHA: value.CommitSHA, StartedAt: value.StartedAt})
	}
	merged, err := domain.MergeQodoReviewRuns(runs)
	if err != nil {
		return nil, err
	}
	result := make([]qodoManagedReviewRun, 0, len(merged))
	for _, value := range merged {
		result = append(result, qodoManagedReviewRun{ID: value.ID, Status: value.Status, CommitSHA: value.CommitSHA, StartedAt: value.StartedAt})
	}
	return result, nil
}

func mergeQodoReviewSessions(preferred, fallback *qodoReviewSession) (*qodoReviewSession, error) {
	merged, err := domain.MergeQodoReviewSessions(domainSession(preferred), domainSession(fallback))
	if err != nil {
		return nil, err
	}
	if merged == nil {
		return nil, nil
	}
	return &qodoReviewSession{Status: merged.Status, CommitSHA: merged.CommitSHA, StartedAt: merged.StartedAt}, nil
}

func domainSession(value *qodoReviewSession) *domain.QodoReviewSession {
	if value == nil {
		return nil
	}
	return &domain.QodoReviewSession{Status: value.Status, CommitSHA: value.CommitSHA, StartedAt: value.StartedAt}
}

func findingToDomain(value qodoManagedFinding) domain.QodoReviewFinding {
	result := domain.QodoReviewFinding{
		ID: value.ID, Title: value.Title, Description: value.Description, Category: value.Category,
		ActionLevel: value.ActionLevel, AttributionStatus: value.AttributionStatus, GitSHA: value.GitSHA,
		Status: value.Status, ResolutionReason: value.ResolutionReason, ReviewRunID: value.ReviewRunID,
		FilePath: value.FilePath, StartLine: value.StartLine, EndLine: value.EndLine, Side: value.Side,
		CodeSnippet: value.CodeSnippet,
	}
	if value.Evidence != nil {
		evidence := &domain.QodoFindingEvidence{Explanation: value.Evidence.Explanation}
		for _, citation := range value.Evidence.Citations {
			metadata := string(citation.Metadata)
			if len(citation.Metadata) > 0 {
				var compact bytes.Buffer
				if json.Compact(&compact, citation.Metadata) == nil {
					metadata = compact.String()
				}
			}
			evidence.Citations = append(evidence.Citations, domain.QodoFindingCitation{SourceType: citation.SourceType, Source: citation.Source, Text: citation.Text, Context: citation.Context, Metadata: metadata})
		}
		result.Evidence = evidence
	}
	return result
}

func findingFromDomain(value domain.QodoReviewFinding) qodoManagedFinding {
	result := qodoManagedFinding{
		ID: value.ID, Title: value.Title, Description: value.Description, Category: value.Category,
		ActionLevel: value.ActionLevel, AttributionStatus: value.AttributionStatus, GitSHA: value.GitSHA,
		Status: value.Status, ResolutionReason: value.ResolutionReason, ReviewRunID: value.ReviewRunID,
		FilePath: value.FilePath, StartLine: value.StartLine, EndLine: value.EndLine, Side: value.Side,
		CodeSnippet: value.CodeSnippet,
	}
	if value.Evidence != nil {
		evidence := &qodoFindingEvidence{Explanation: value.Evidence.Explanation}
		for _, citation := range value.Evidence.Citations {
			evidence.Citations = append(evidence.Citations, qodoFindingCitation{SourceType: citation.SourceType, Source: citation.Source, Text: citation.Text, Context: citation.Context, Metadata: json.RawMessage(citation.Metadata)})
		}
		result.Evidence = evidence
	}
	return result
}

type qodoReviewSession struct {
	Status    string     `json:"status,omitempty"`
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
	commitSHA, err := domain.MergeQodoRevisions("revision fields", decoded.CommitSHA, snake.CommitSHA)
	if err != nil {
		return err
	}
	decoded.CommitSHA = commitSHA
	decoded.StartedAt, err = domain.MergeQodoTimes("review-session start times", decoded.StartedAt, snake.StartedAt)
	if err != nil {
		return err
	}
	*session = qodoReviewSession(decoded)
	return nil
}

func mergeFindingStringAliases(field string, values ...string) (string, error) {
	return domain.MergeQodoStringAliases("finding "+field, values...)
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
	attributionStatus, err := domain.MergeQodoEnumAliases("attribution statuses", finding.AttributionStatus, snake.AttributionStatus)
	if err != nil {
		return err
	}
	finding.AttributionStatus = attributionStatus
	gitSHA, err := domain.MergeQodoRevisions("revision fields", finding.GitSHA, snake.GitSHA)
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
	commitSHA, err := domain.MergeQodoRevisions("revision fields", decoded.CommitSHA, snake.CommitSHA)
	if err != nil {
		return err
	}
	decoded.CommitSHA = commitSHA
	decoded.StartedAt, err = domain.MergeQodoTimes("review-run start times", decoded.StartedAt, snake.StartedAt)
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
		logger:   slog.Default(),
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
			"protocolVersion": qodoMCPProtocolVersion, "capabilities": map[string]any{},
			"clientInfo": map[string]string{"name": "ghostline", "version": "0.1.0"},
		},
	}
	initResponse, sessionID, err := client.post(ctx, apiKey, "", initialize, 1, true)
	if sessionID != "" {
		defer func() {
			cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
			defer cancel()
			if cleanupErr := client.closeSession(cleanupCtx, apiKey, sessionID); cleanupErr != nil {
				logger := client.logger
				if logger == nil {
					logger = slog.Default()
				}
				logger.Warn("Qodo MCP session cleanup failed", "cause", cleanupErr)
			}
		}()
	}
	if err != nil {
		return empty, err
	}
	var initResult struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if err := json.Unmarshal(initResponse.Result, &initResult); err != nil || initResult.ProtocolVersion == "" {
		return empty, errors.New("Qodo MCP initialization response was invalid")
	}
	if initResult.ProtocolVersion != qodoMCPProtocolVersion {
		return empty, fmt.Errorf("unsupported Qodo MCP protocol version %q", initResult.ProtocolVersion)
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
	var toolResult qodoToolResult
	if err := json.Unmarshal(response.Result, &toolResult); err != nil {
		return empty, errors.New("Qodo MCP returned an invalid tool result")
	}
	if toolResult.IsError {
		return empty, errors.New("Qodo could not retrieve structured review data for this pull request")
	}
	data, err := parseQodoToolResult(toolResult)
	if err != nil {
		return empty, err
	}
	data.Availability = "available"
	data.Source = "Qodo Agentic Toolbox · get-pr-review-findings"
	return data, nil
}

func (client *qodoReviewClient) closeSession(ctx context.Context, apiKey, sessionID string) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodDelete, client.endpoint, nil)
	if err != nil {
		return errors.New("could not prepare Qodo MCP session cleanup")
	}
	request.Header.Set("Authorization", "Bearer "+apiKey)
	request.Header.Set("Mcp-Session-Id", sessionID)
	request.Header.Set("Accept", "application/json, text/event-stream")
	response, err := client.http.Do(request)
	if err != nil {
		return errors.New("could not close Qodo MCP session")
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound || response.StatusCode == http.StatusMethodNotAllowed || response.StatusCode >= 200 && response.StatusCode < 300 {
		return nil
	}
	return fmt.Errorf("Qodo MCP session cleanup returned HTTP %d", response.StatusCode)
}

type qodoToolResult struct {
	IsError           bool              `json:"isError"`
	StructuredContent json.RawMessage   `json:"structuredContent"`
	Content           []qodoToolContent `json:"content"`
}

type qodoToolContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func parseQodoToolResult(toolResult qodoToolResult) (qodoReviewData, error) {
	var empty qodoReviewData
	var data qodoReviewData
	decoded := false
	incompleteCandidates := false
	var candidateValidationError error
	structuredContent := bytes.TrimSpace(toolResult.StructuredContent)
	if len(structuredContent) > 0 && !bytes.Equal(structuredContent, []byte("null")) {
		var candidate qodoReviewData
		if err := json.Unmarshal(structuredContent, &candidate); err != nil {
			incompleteCandidates = true
			if json.Valid(structuredContent) && looksLikeQodoReviewObject(structuredContent) {
				candidateValidationError = err
			}
		} else if err := validateQodoReviewData(candidate); err != nil {
			incompleteCandidates = true
			candidateValidationError = err
		} else {
			data = candidate
			decoded = true
		}
	}
	for _, block := range toolResult.Content {
		if block.Type != "text" {
			continue
		}
		payload := []byte(block.Text)
		var candidate qodoReviewData
		if err := json.Unmarshal(payload, &candidate); err != nil {
			if json.Valid(payload) && looksLikeQodoReviewObject(payload) {
				incompleteCandidates = true
				candidateValidationError = err
				continue
			}
			if !json.Valid(payload) && looksLikeQodoReviewCandidate(payload) {
				incompleteCandidates = true
			}
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
			return empty, errors.New("Qodo returned conflicting review data across response forms")
		}
		data = merged
	}
	if candidateValidationError != nil && strings.Contains(candidateValidationError.Error(), "conflicting") {
		return empty, candidateValidationError
	}
	if !decoded {
		if candidateValidationError != nil {
			return empty, candidateValidationError
		}
		return empty, errors.New("Qodo did not return structured review data")
	}
	if incompleteCandidates {
		data.Warnings = append(data.Warnings, "Qodo returned a response representation that could not be interpreted as complete structured review data; the displayed review may be incomplete.")
	}
	if err := validateQodoReviewData(data); err != nil {
		return qodoReviewData{}, err
	}
	return data, nil
}

func looksLikeQodoReviewObject(payload []byte) bool {
	var fields map[string]json.RawMessage
	if json.Unmarshal(payload, &fields) != nil {
		return false
	}
	for _, key := range []string{"review_session", "reviewSession", "findings", "camelFindings", "review_runs", "reviewRuns"} {
		if _, ok := fields[key]; ok {
			return true
		}
	}
	return false
}

func looksLikeQodoReviewCandidate(payload []byte) bool {
	if looksLikeQodoReviewObject(payload) {
		return true
	}
	text := string(payload)
	for _, key := range []string{`"review_session"`, `"reviewSession"`, `"findings"`, `"review_runs"`, `"reviewRuns"`} {
		if strings.Contains(text, key) {
			return true
		}
	}
	return false
}

func mergeQodoReviewData(first, second qodoReviewData) (qodoReviewData, error) {
	merged := qodoReviewData{Warnings: deduplicateStrings(append(append([]string(nil), first.Warnings...), second.Warnings...))}
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

func deduplicateStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
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
		return empty, newSessionID, qodoServiceError{status: response.StatusCode}
	}
	if !expectResult {
		return empty, newSessionID, nil
	}
	contentType := response.Header.Get("Content-Type")
	var envelope mcpEnvelope
	if isMCPEventStream(contentType) {
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
	if isMCPEventStream(contentType) {
		return decodeMCPEnvelopeStream(bytes.NewReader(data), contentType, expectedID)
	}
	var envelope mcpEnvelope
	err := decodeFirstMCPJSON(data, &envelope)
	return envelope, err
}

func decodeMCPEnvelopeStream(reader io.Reader, contentType string, expectedID int64) (mcpEnvelope, error) {
	var empty mcpEnvelope
	if !isMCPEventStream(contentType) {
		return empty, errors.New("MCP stream decoder requires an event-stream response")
	}
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), maxQodoResponseBytes)
	var consumed int
	skipLF := false
	scanner.Split(func(data []byte, atEOF bool) (int, []byte, error) {
		start := 0
		if skipLF {
			if len(data) == 0 {
				return 0, nil, nil
			}
			if data[0] == '\n' {
				start = 1
				consumed++
			}
			skipLF = false
		}
		for index := start; index < len(data); index++ {
			switch data[index] {
			case '\n':
				consumed++
				return index + 1, data[start:index], nil
			case '\r':
				delimiterBytes := 1
				if index+1 < len(data) {
					if data[index+1] == '\n' {
						delimiterBytes++
					}
				} else if !atEOF {
					skipLF = true
				}
				consumed += delimiterBytes
				return index + delimiterBytes, data[start:index], nil
			}
		}
		if atEOF && len(data) > start {
			return len(data), data[start:], nil
		}
		if start > 0 {
			return start, nil, nil
		}
		return 0, nil, nil
	})
	var eventData []string
	for scanner.Scan() {
		rawLine := scanner.Text()
		consumed += len(rawLine)
		if consumed > maxQodoResponseBytes {
			return empty, errors.New("Qodo review response exceeded the size limit")
		}
		line := rawLine
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

func isMCPEventStream(contentType string) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	return err == nil && strings.EqualFold(mediaType, "text/event-stream")
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
		snapshot.ReviewSession = &domain.QodoReviewSession{Status: data.ReviewSession.Status, CommitSHA: data.ReviewSession.CommitSHA, StartedAt: data.ReviewSession.StartedAt}
	}
	for _, finding := range data.Findings {
		var evidence *domain.QodoFindingEvidence
		if finding.Evidence != nil {
			evidence = &domain.QodoFindingEvidence{Explanation: finding.Evidence.Explanation}
			for _, citation := range finding.Evidence.Citations {
				evidence.Citations = append(evidence.Citations, domain.QodoFindingCitation{
					SourceType: citation.SourceType, Source: citation.Source, Text: citation.Text,
					Context: citation.Context, Metadata: string(citation.Metadata),
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
