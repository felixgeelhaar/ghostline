package main

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strings"

	"github.com/felixgeelhaar/ghostline/backend/internal/domain"
)

const (
	noxArtifactName      = "nox-results"
	noxSARIFFilename     = "results.sarif"
	noxReportAvailable   = "available"
	noxReportUnavailable = "unavailable"
	maxNoxArtifactBytes  = 10 << 20
	maxNoxSARIFBytes     = 4 << 20
	maxNoxFindings       = 100
	noxPageSize          = 100
	maxNoxPages          = 10
	noxWorkflowPath      = ".github/workflows/nox.yml"
)

type noxReport struct {
	Availability   string       `json:"availability"`
	Reason         string       `json:"reason,omitempty"`
	RevisionSHA    string       `json:"revisionSha"`
	RunID          int64        `json:"runId,omitempty"`
	RunName        string       `json:"runName,omitempty"`
	RunConclusion  string       `json:"runConclusion,omitempty"`
	RunURL         string       `json:"runUrl,omitempty"`
	ArtifactID     int64        `json:"artifactId,omitempty"`
	ArtifactURL    string       `json:"artifactUrl,omitempty"`
	ArtifactDigest string       `json:"artifactDigest,omitempty"`
	CreatedAt      string       `json:"createdAt,omitempty"`
	ExpiresAt      string       `json:"expiresAt,omitempty"`
	TotalCount     int          `json:"totalCount"`
	Truncated      bool         `json:"truncated"`
	Findings       []noxFinding `json:"findings"`
}

type noxFinding struct {
	RuleID      string `json:"ruleId"`
	Title       string `json:"title"`
	Severity    string `json:"severity"`
	Message     string `json:"message"`
	Path        string `json:"path,omitempty"`
	Line        int    `json:"line,omitempty"`
	Fingerprint string `json:"fingerprint,omitempty"`
}

func noxFindingFromDomain(finding domain.NoxFinding) noxFinding {
	return noxFinding{RuleID: finding.RuleID, Title: finding.Title, Severity: finding.Severity, Message: finding.Message, Path: finding.Path, Line: finding.Line, Fingerprint: finding.Fingerprint}
}

type workflowRunsResponse struct {
	WorkflowRuns []struct {
		ID         int64  `json:"id"`
		Path       string `json:"path"`
		Name       string `json:"name"`
		Event      string `json:"event"`
		HeadSHA    string `json:"head_sha"`
		Status     string `json:"status"`
		Conclusion string `json:"conclusion"`
		HTMLURL    string `json:"html_url"`
		CreatedAt  string `json:"created_at"`
	} `json:"workflow_runs"`
}

type workflowArtifactsResponse struct {
	Artifacts []struct {
		ID          int64  `json:"id"`
		Name        string `json:"name"`
		Size        int64  `json:"size_in_bytes"`
		URL         string `json:"url"`
		Digest      string `json:"digest"`
		Expired     bool   `json:"expired"`
		CreatedAt   string `json:"created_at"`
		ExpiresAt   string `json:"expires_at"`
		WorkflowRun struct {
			ID      int64  `json:"id"`
			HeadSHA string `json:"head_sha"`
		} `json:"workflow_run"`
	} `json:"artifacts"`
}

type sarifLog struct {
	Version string     `json:"version"`
	Runs    []sarifRun `json:"runs"`
}

type sarifRun struct {
	Tool struct {
		Driver struct {
			Name  string      `json:"name"`
			Rules []sarifRule `json:"rules"`
		} `json:"driver"`
	} `json:"tool"`
	Results []sarifResult `json:"results"`
}

type sarifRule struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	ShortDescription struct {
		Text string `json:"text"`
	} `json:"shortDescription"`
	Properties struct {
		Severity string `json:"severity"`
	} `json:"properties"`
}

type sarifResult struct {
	RuleID    string `json:"ruleId"`
	RuleIndex *int   `json:"ruleIndex"`
	Level     string `json:"level"`
	Message   struct {
		Text     string `json:"text"`
		Markdown string `json:"markdown"`
	} `json:"message"`
	Suppressions []json.RawMessage `json:"suppressions"`
	Locations    []struct {
		PhysicalLocation struct {
			ArtifactLocation struct {
				URI string `json:"uri"`
			} `json:"artifactLocation"`
			Region struct {
				StartLine int `json:"startLine"`
			} `json:"region"`
		} `json:"physicalLocation"`
	} `json:"locations"`
	PartialFingerprints map[string]string `json:"partialFingerprints"`
}

func loadNoxReport(ctx context.Context, gh *githubClient, owner, repo string, number int, sha string) noxReport {
	result := noxReport{Availability: noxReportUnavailable, RevisionSHA: sha, Findings: []noxFinding{}}
	if !fullSHA.MatchString(sha) {
		result.Reason = "Choose a full commit revision from this pull request."
		return result
	}

	prPath := fmt.Sprintf("/repos/%s/%s/pulls/%d", url.PathEscape(owner), url.PathEscape(repo), number)
	var pr githubPR
	if err := gh.get(ctx, prPath, &pr); err != nil {
		result.Reason = noxUnavailableReason(err)
		return result
	}
	identity, err := domain.NewRepositoryIdentity(pr.Base.Repo.Owner.Login, pr.Base.Repo.Name)
	if err != nil || pr.Number != number {
		result.Reason = "GitHub did not confirm the pull request identity."
		return result
	}
	owner, repo = identity.Owner, identity.Name
	if pr.Base.Repo.Private {
		result.Reason = "Security reports are only available for public pull requests."
		return result
	}

	if pr.Head.SHA != sha {
		found, err := pullRequestContainsRevision(ctx, gh, owner, repo, number, sha)
		if err != nil {
			result.Reason = noxUnavailableReason(err)
			return result
		}
		if !found {
			result.Reason = "This revision is not part of the imported pull request."
			return result
		}
	}

	matchingRuns := make([]struct {
		ID         int64  `json:"id"`
		Path       string `json:"path"`
		Name       string `json:"name"`
		Event      string `json:"event"`
		HeadSHA    string `json:"head_sha"`
		Status     string `json:"status"`
		Conclusion string `json:"conclusion"`
		HTMLURL    string `json:"html_url"`
		CreatedAt  string `json:"created_at"`
	}, 0)
	workflowHistoryTruncated := false
	for page := 1; page <= maxNoxPages; page++ {
		var runs workflowRunsResponse
		runsPath := fmt.Sprintf("/repos/%s/%s/actions/workflows/nox.yml/runs?head_sha=%s&event=pull_request&per_page=%d&page=%d", url.PathEscape(owner), url.PathEscape(repo), url.QueryEscape(sha), noxPageSize, page)
		if err := gh.get(ctx, runsPath, &runs); err != nil {
			result.Reason = noxUnavailableReason(err)
			return result
		}
		for _, run := range runs.WorkflowRuns {
			if run.HeadSHA == sha && run.Event == "pull_request" && isNoxWorkflowPath(run.Path) {
				matchingRuns = append(matchingRuns, run)
			}
		}
		if len(runs.WorkflowRuns) < noxPageSize {
			break
		}
		if page == maxNoxPages {
			workflowHistoryTruncated = true
		}
	}
	sort.SliceStable(matchingRuns, func(i, j int) bool { return matchingRuns[i].CreatedAt > matchingRuns[j].CreatedAt })
	if len(matchingRuns) == 0 {
		if workflowHistoryTruncated {
			result.Reason = "GitHub has more workflow history than Ghostline can inspect for this revision. Narrow the revision or try again after older runs expire."
		} else {
			result.Reason = "GitHub has no pull-request workflow run for this revision."
		}
		return result
	}

	var artifact *struct {
		ID          int64  `json:"id"`
		Name        string `json:"name"`
		Size        int64  `json:"size_in_bytes"`
		URL         string `json:"url"`
		Digest      string `json:"digest"`
		Expired     bool   `json:"expired"`
		CreatedAt   string `json:"created_at"`
		ExpiresAt   string `json:"expires_at"`
		WorkflowRun struct {
			ID      int64  `json:"id"`
			HeadSHA string `json:"head_sha"`
		} `json:"workflow_run"`
	}
	var selectedRun *struct {
		ID         int64  `json:"id"`
		Path       string `json:"path"`
		Name       string `json:"name"`
		Event      string `json:"event"`
		HeadSHA    string `json:"head_sha"`
		Status     string `json:"status"`
		Conclusion string `json:"conclusion"`
		HTMLURL    string `json:"html_url"`
		CreatedAt  string `json:"created_at"`
	}
	for i := range matchingRuns {
		run := &matchingRuns[i]
		artifactHistoryTruncated := false
		for page := 1; page <= maxNoxPages; page++ {
			var response workflowArtifactsResponse
			artifactsPath := fmt.Sprintf("/repos/%s/%s/actions/runs/%d/artifacts?name=%s&per_page=%d&page=%d", url.PathEscape(owner), url.PathEscape(repo), run.ID, url.QueryEscape(noxArtifactName), noxPageSize, page)
			if err := gh.get(ctx, artifactsPath, &response); err != nil {
				result.Reason = noxUnavailableReason(err)
				return result
			}
			for j := range response.Artifacts {
				candidate := &response.Artifacts[j]
				if candidate.Name != noxArtifactName || candidate.WorkflowRun.HeadSHA != sha || candidate.WorkflowRun.ID != run.ID {
					continue
				}
				if artifact == nil || candidate.CreatedAt > artifact.CreatedAt {
					artifact = candidate
					selectedRun = run
				}
			}
			if len(response.Artifacts) < noxPageSize {
				break
			}
			if page == maxNoxPages && artifact == nil {
				artifactHistoryTruncated = true
			}
		}
		if artifactHistoryTruncated && artifact == nil {
			result.Reason = "GitHub has more artifact history than Ghostline can inspect for this revision."
			return result
		}
		if artifact != nil {
			break // Runs are newest first; a later run may not replace a missing artifact.
		}
	}
	if artifact == nil || selectedRun == nil {
		result.Reason = "No retained Nox report was published for this revision."
		return result
	}
	result.RunID = selectedRun.ID
	result.RunName = selectedRun.Name
	result.RunConclusion = selectedRun.Conclusion
	result.RunURL = githubRunURL(owner, repo, selectedRun.ID)
	result.ArtifactID = artifact.ID
	result.ArtifactURL = result.RunURL
	result.ArtifactDigest = artifact.Digest
	result.CreatedAt = artifact.CreatedAt
	result.ExpiresAt = artifact.ExpiresAt
	if artifact.Expired {
		result.Reason = "The Nox report has expired from GitHub Actions storage."
		return result
	}
	if artifact.Size < 0 || artifact.Size > maxNoxArtifactBytes {
		result.Reason = "GitHub’s Nox artifact is too large for Ghostline to inspect safely."
		return result
	}

	archivePath := fmt.Sprintf("/repos/%s/%s/actions/artifacts/%d/zip", url.PathEscape(owner), url.PathEscape(repo), artifact.ID)
	archive, err := gh.downloadGitHubArtifact(ctx, archivePath, maxNoxArtifactBytes)
	if err != nil {
		result.Reason = noxUnavailableReason(err)
		return result
	}
	digest := strings.TrimPrefix(artifact.Digest, "sha256:")
	if len(digest) != sha256.Size*2 {
		result.Reason = "GitHub did not provide a verifiable SHA-256 digest for the Nox artifact."
		return result
	}
	actual := sha256.Sum256(archive)
	if !strings.EqualFold(digest, hex.EncodeToString(actual[:])) {
		result.Reason = "GitHub’s Nox artifact did not match its published SHA-256 digest."
		return result
	}
	sarif, err := readNoxSARIFArtifact(archive)
	if err != nil {
		result.Reason = "GitHub’s Nox artifact could not be read as a supported SARIF report."
		return result
	}
	parsed, err := parseNoxSARIF(sarif, sha, selectedRun.ID, result.RunURL, artifact.CreatedAt, artifact.ExpiresAt)
	if err != nil {
		result.Reason = "GitHub’s Nox artifact did not contain a valid Nox SARIF report."
		return result
	}
	parsed.RunName = selectedRun.Name
	parsed.RunConclusion = selectedRun.Conclusion
	parsed.ArtifactID = artifact.ID
	parsed.ArtifactURL = result.ArtifactURL
	parsed.ArtifactDigest = artifact.Digest
	return parsed
}

func pullRequestContainsRevision(ctx context.Context, gh *githubClient, owner, repo string, number int, sha string) (bool, error) {
	base := fmt.Sprintf("/repos/%s/%s/pulls/%d/commits", url.PathEscape(owner), url.PathEscape(repo), number)
	for page := 1; ; page++ {
		var commits []struct {
			SHA string `json:"sha"`
		}
		if err := gh.get(ctx, fmt.Sprintf("%s?per_page=%d&page=%d", base, noxPageSize, page), &commits); err != nil {
			return false, err
		}
		for _, commit := range commits {
			if commit.SHA == sha {
				return true, nil
			}
		}
		if len(commits) < noxPageSize {
			return false, nil
		}
	}
}

func isNoxWorkflowPath(value string) bool {
	return value == noxWorkflowPath || strings.HasPrefix(value, noxWorkflowPath+"@")
}

func readNoxSARIFArtifact(data []byte) ([]byte, error) {
	if len(data) == 0 || len(data) > maxNoxArtifactBytes {
		return nil, errors.New("artifact size is invalid")
	}
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	var report *zip.File
	for _, file := range reader.File {
		if path.Base(file.Name) != noxSARIFFilename {
			continue
		}
		if report != nil {
			return nil, errors.New("artifact contains more than one SARIF report")
		}
		if file.UncompressedSize64 > maxNoxSARIFBytes {
			return nil, errors.New("SARIF report is too large")
		}
		report = file
	}
	if report == nil {
		return nil, errors.New("SARIF report is missing")
	}
	file, err := report.Open()
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err = io.ReadAll(io.LimitReader(file, maxNoxSARIFBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxNoxSARIFBytes {
		return nil, errors.New("SARIF report is too large")
	}
	return data, nil
}

func parseNoxSARIF(data []byte, sha string, runID int64, runURL, createdAt, expiresAt string) (noxReport, error) {
	var log sarifLog
	if !fullSHA.MatchString(sha) || len(data) == 0 || len(data) > maxNoxSARIFBytes {
		return noxReport{}, errors.New("report revision or size is invalid")
	}
	if err := json.Unmarshal(data, &log); err != nil {
		return noxReport{}, err
	}
	if log.Version != "2.1.0" || len(log.Runs) == 0 {
		return noxReport{}, errors.New("unsupported or empty SARIF log")
	}
	report := noxReport{Availability: noxReportAvailable, RevisionSHA: sha, RunID: runID, RunURL: safeURL(runURL), CreatedAt: createdAt, ExpiresAt: expiresAt, Findings: []noxFinding{}}
	for _, run := range log.Runs {
		if !strings.Contains(strings.ToLower(run.Tool.Driver.Name), "nox") {
			return noxReport{}, errors.New("artifact contains a report from a different scanner")
		}
		for _, result := range run.Results {
			var rule sarifRule
			if result.RuleIndex != nil && *result.RuleIndex >= 0 && *result.RuleIndex < len(run.Tool.Driver.Rules) {
				rule = run.Tool.Driver.Rules[*result.RuleIndex]
			}
			value := domain.NoxSARIFFinding{RuleID: result.RuleID, RuleDefinitionID: rule.ID, RuleName: rule.Name, RuleTitle: rule.ShortDescription.Text, RuleSeverity: rule.Properties.Severity, ResultLevel: result.Level, Message: result.Message.Text, Markdown: result.Message.Markdown, Fingerprints: result.PartialFingerprints, Suppressed: len(result.Suppressions) > 0}
			if len(result.Locations) > 0 {
				location := result.Locations[0].PhysicalLocation
				value.Path = location.ArtifactLocation.URI
				value.Line = location.Region.StartLine
			}
			finding, include := domain.ParseNoxFinding(value)
			if !include {
				continue
			}
			report.TotalCount++
			if len(report.Findings) >= maxNoxFindings {
				report.Truncated = true
				continue
			}
			report.Findings = append(report.Findings, noxFindingFromDomain(finding))
		}
	}
	if len(log.Runs) == 0 {
		return noxReport{}, errors.New("SARIF contains no runs")
	}
	return report, nil
}

func githubRunURL(owner, repo string, runID int64) string {
	return fmt.Sprintf("https://github.com/%s/%s/actions/runs/%d", url.PathEscape(owner), url.PathEscape(repo), runID)
}

func noxUnavailableReason(err error) string {
	if errors.Is(err, errNotFound) {
		return "GitHub did not return the pull request or its workflow evidence."
	}
	if errors.Is(err, errRateLimit) {
		return "GitHub’s API rate limit was reached before the Nox report could be loaded. Try again later or configure GITHUB_TOKEN."
	}
	var statusErr statusError
	if errors.As(err, &statusErr) {
		if statusErr.code == http.StatusUnauthorized {
			return "GitHub requires a server token with Actions read access to download this workflow artifact."
		}
		if statusErr.code == http.StatusForbidden {
			return "GitHub denied access to this Nox workflow artifact."
		}
	}
	return "The Nox report could not be loaded from GitHub. Try again later."
}

func (g *githubClient) downloadGitHubArtifact(ctx context.Context, endpoint string, maxBytes int64) ([]byte, error) {
	if !strings.HasPrefix(endpoint, "/repos/") || strings.Contains(endpoint, "..") {
		return nil, errors.New("invalid artifact endpoint")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, g.baseURL+endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "Ghostline")
	if g.token != "" {
		req.Header.Set("Authorization", "Bearer "+g.token)
	}
	client := *g.http
	previousCheck := client.CheckRedirect
	client.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if len(via) >= 5 || next.URL.Scheme != "https" || !allowedArtifactHost(next.URL.Hostname()) {
			return http.ErrUseLastResponse
		}
		if previousCheck != nil {
			return previousCheck(next, via)
		}
		return nil
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		if response.StatusCode == http.StatusNotFound {
			return nil, errNotFound
		}
		if response.StatusCode == http.StatusTooManyRequests || response.StatusCode == http.StatusForbidden && (response.Header.Get("X-RateLimit-Remaining") == "0" || response.Header.Get("Retry-After") != "") {
			return nil, errRateLimit
		}
		return nil, statusError{code: response.StatusCode}
	}
	if response.ContentLength > maxBytes {
		return nil, errors.New("artifact exceeds size limit")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, errors.New("artifact exceeds size limit")
	}
	return data, nil
}

func allowedArtifactHost(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	return host == "github.com" || host == "api.github.com" || strings.HasSuffix(host, ".githubusercontent.com") || strings.HasSuffix(host, ".blob.core.windows.net")
}
