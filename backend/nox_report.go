package main

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
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
	noxArtifactName  = "nox-results"
	noxSARIFFilename = "results.sarif"
	// Written by the Security workflow: the commit it checked out and scanned.
	noxScannedCommitFilename = "scanned-commit.txt"
	maxNoxScannedCommitBytes = 128
	noxReportAvailable       = "available"
	noxReportUnavailable     = "unavailable"
	maxNoxArtifactBytes      = 10 << 20
	maxNoxSARIFBytes         = 4 << 20
	noxPageSize              = 100
	maxNoxPages              = 10
	maxNoxGitHubRequests     = 32
	// GitHub's compare response lists at most 300 files; a full list may be cut.
	noxCompareFileLimit = 300
)

const noxInspectionIncompleteReason = "GitHub evidence exceeded Ghostline’s inspection limit. The report may exist outside the inspected history; narrow the revision or try again later."

var errNoxInspectionLimit = errors.New("Nox evidence inspection limit reached")

type noxGitHubRequestBudget struct {
	client    *githubClient
	remaining int
}

func (budget *noxGitHubRequestBudget) get(ctx context.Context, requestPath string, target any) error {
	if budget.remaining <= 0 {
		return errNoxInspectionLimit
	}
	budget.remaining--
	return budget.client.get(ctx, requestPath, target)
}

// getWithNext also reports GitHub's rel="next" link, which decides whether
// more pages exist; a short page alone does not prove the listing ended.
func (budget *noxGitHubRequestBudget) getWithNext(ctx context.Context, requestPath string, target any) (bool, error) {
	if budget.remaining <= 0 {
		return false, errNoxInspectionLimit
	}
	budget.remaining--
	return budget.client.getWithNext(ctx, requestPath, target)
}

func (budget *noxGitHubRequestBudget) downloadArtifact(ctx context.Context, requestPath string, maxBytes int64) ([]byte, error) {
	if budget.remaining <= 0 {
		return nil, errNoxInspectionLimit
	}
	budget.remaining--
	return budget.client.downloadGitHubArtifact(ctx, requestPath, maxBytes)
}

// ensurePublic confirms visibility right before the one request that sends
// the server token: GitHub requires authentication to download an artifact.
func (budget *noxGitHubRequestBudget) ensurePublic(ctx context.Context, owner, repo string) error {
	if budget.client.token == "" {
		return nil
	}
	if budget.remaining <= 0 {
		return errNoxInspectionLimit
	}
	budget.remaining--
	return ensurePublicRepository(ctx, budget.client, owner, repo)
}

func noxVisibilityFailureReason(err error) string {
	switch {
	case errors.Is(err, errPrivateRepository), errors.Is(err, errNotFound):
		return "Security reports are only available for public pull requests."
	case errors.Is(err, errRepositoryVisibilityUnknown):
		return "GitHub did not confirm that this repository is public, so the Nox artifact was not downloaded."
	case errors.Is(err, errServerTokenRejected):
		return "The server’s GitHub token was rejected, so the Nox artifact was not downloaded. Replace the configured token."
	case errors.Is(err, errServerTokenRateLimit):
		return "The server’s GitHub token has reached its rate limit, so the Nox artifact was not downloaded. Try again later."
	default:
		return noxRequestFailureReason(err)
	}
}

func noxRequestFailureReason(err error) string {
	if errors.Is(err, errNoxInspectionLimit) {
		return noxInspectionIncompleteReason
	}
	return noxUnavailableReason(err)
}

type noxReport struct {
	Availability    string       `json:"availability"`
	Reason          string       `json:"reason,omitempty"`
	RevisionSHA     string       `json:"revisionSha"`
	RunID           int64        `json:"runId,omitempty"`
	RunName         string       `json:"runName,omitempty"`
	RunStatus       string       `json:"runStatus,omitempty"`
	RunConclusion   string       `json:"runConclusion,omitempty"`
	ScanSuccessful  *bool        `json:"scanSuccessful,omitempty"`
	RunURL          string       `json:"runUrl,omitempty"`
	ArtifactID      int64        `json:"artifactId,omitempty"`
	ArtifactURL     string       `json:"artifactUrl,omitempty"`
	ArtifactDigest  string       `json:"artifactDigest,omitempty"`
	CreatedAt       string       `json:"createdAt,omitempty"`
	ExpiresAt       string       `json:"expiresAt,omitempty"`
	TotalCount      int          `json:"totalCount"`
	ActiveCount     int          `json:"activeCount"`
	SuppressedCount int          `json:"suppressedCount"`
	Truncated       bool         `json:"truncated"`
	Findings        []noxFinding `json:"findings"`
}

type noxFinding struct {
	RuleID                   string           `json:"ruleId"`
	Title                    string           `json:"title"`
	Severity                 string           `json:"severity"`
	Message                  string           `json:"message"`
	Path                     string           `json:"path,omitempty"`
	Line                     int              `json:"line,omitempty"`
	Fingerprint              string           `json:"fingerprint,omitempty"`
	Suppressed               bool             `json:"suppressed"`
	SuppressionKind          string           `json:"suppressionKind,omitempty"`
	SuppressionJustification string           `json:"suppressionJustification,omitempty"`
	Suppressions             []noxSuppression `json:"suppressions"`
	SuppressionsTruncated    bool             `json:"suppressionsTruncated,omitempty"`
}

type noxSuppression struct {
	Status        string `json:"status"`
	Kind          string `json:"kind"`
	Justification string `json:"justification,omitempty"`
}

func noxFindingFromDomain(finding domain.NoxFinding) noxFinding {
	suppressions := make([]noxSuppression, 0, len(finding.Suppressions))
	for _, suppression := range finding.Suppressions {
		suppressions = append(suppressions, noxSuppression{Status: suppression.Status, Kind: suppression.Kind, Justification: suppression.Justification})
	}
	return noxFinding{RuleID: finding.RuleID, Title: finding.Title, Severity: finding.Severity, Message: finding.Message, Path: finding.Path, Line: finding.Line, Fingerprint: finding.Fingerprint, Suppressed: finding.Suppressed, SuppressionKind: finding.SuppressionKind, SuppressionJustification: finding.SuppressionJustification, Suppressions: suppressions, SuppressionsTruncated: finding.SuppressionsTruncated}
}

type workflowRunsResponse struct {
	WorkflowRuns []workflowRun `json:"workflow_runs"`
}

type workflowRun struct {
	ID           int64                    `json:"id"`
	Path         string                   `json:"path"`
	Name         string                   `json:"name"`
	Event        string                   `json:"event"`
	HeadSHA      string                   `json:"head_sha"`
	Status       string                   `json:"status"`
	Conclusion   string                   `json:"conclusion"`
	HTMLURL      string                   `json:"html_url"`
	CreatedAt    string                   `json:"created_at"`
	PullRequests []workflowRunPullRequest `json:"pull_requests"`
}

type workflowRunPullRequest struct {
	Number int `json:"number"`
	Base   struct {
		SHA string `json:"sha"`
	} `json:"base"`
}

type workflowArtifactsResponse struct {
	Artifacts []workflowArtifact `json:"artifacts"`
}

type workflowArtifact struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Size        int64  `json:"size_in_bytes"`
	URL         string `json:"url"`
	HTMLURL     string `json:"html_url"`
	Digest      string `json:"digest"`
	Expired     bool   `json:"expired"`
	CreatedAt   string `json:"created_at"`
	ExpiresAt   string `json:"expires_at"`
	WorkflowRun struct {
		ID      int64  `json:"id"`
		HeadSHA string `json:"head_sha"`
	} `json:"workflow_run"`
}

type noxArtifactCandidate struct {
	artifact workflowArtifact
	run      workflowRun
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
	Invocations []struct {
		ExecutionSuccessful *bool `json:"executionSuccessful"`
	} `json:"invocations"`
	Results *json.RawMessage `json:"results"`
}

type sarifRule struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	ShortDescription struct {
		Text string `json:"text"`
	} `json:"shortDescription"`
	DefaultConfiguration struct {
		Level string `json:"level"`
	} `json:"defaultConfiguration"`
	Properties struct {
		Severity string `json:"severity"`
	} `json:"properties"`
}

type sarifResult struct {
	RuleID    string `json:"ruleId"`
	RuleIndex *int   `json:"ruleIndex"`
	Level     string `json:"level"`
	Kind      string `json:"kind"`
	Message   struct {
		Text     string `json:"text"`
		Markdown string `json:"markdown"`
	} `json:"message"`
	Suppressions []sarifSuppression `json:"suppressions"`
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

type sarifSuppression struct {
	Kind          string `json:"kind"`
	Status        string `json:"status"`
	Justification string `json:"justification"`
}

func loadNoxReport(ctx context.Context, gh *githubClient, owner, repo string, number int, sha string) noxReport {
	result := noxReport{Availability: noxReportUnavailable, RevisionSHA: sha, Findings: []noxFinding{}}
	if !fullSHA.MatchString(sha) {
		result.Reason = "Choose a full commit revision from this pull request."
		return result
	}
	budget := &noxGitHubRequestBudget{client: gh, remaining: maxNoxGitHubRequests}

	prPath := fmt.Sprintf("/repos/%s/%s/pulls/%d", url.PathEscape(owner), url.PathEscape(repo), number)
	var pr githubPR
	if err := budget.get(ctx, prPath, &pr); err != nil {
		result.Reason = noxRequestFailureReason(err)
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
		found, err := pullRequestContainsRevision(ctx, budget, owner, repo, number, sha)
		if err != nil {
			result.Reason = noxRequestFailureReason(err)
			return result
		}
		if !found {
			result.Reason = "This revision is not part of the imported pull request."
			return result
		}
	}

	matchingRuns := make([]workflowRun, 0)
	unlinkedRunFound := false
	workflowHistoryTruncated := false
	for page := 1; page <= maxNoxPages; page++ {
		var runs workflowRunsResponse
		workflowName := strings.TrimPrefix(domain.NoxSecurityWorkflowPath, ".github/workflows/")
		runsPath := fmt.Sprintf("/repos/%s/%s/actions/workflows/%s/runs?head_sha=%s&event=pull_request&per_page=%d&page=%d", url.PathEscape(owner), url.PathEscape(repo), url.PathEscape(workflowName), url.QueryEscape(sha), noxPageSize, page)
		hasNext, err := budget.getWithNext(ctx, runsPath, &runs)
		if err != nil {
			result.Reason = noxRequestFailureReason(err)
			return result
		}
		for _, run := range runs.WorkflowRuns {
			pullRequestNumbers := make([]int, 0, len(run.PullRequests))
			for _, pullRequest := range run.PullRequests {
				pullRequestNumbers = append(pullRequestNumbers, pullRequest.Number)
			}
			if domain.IsNoxWorkflowRunForPullRequest(run.Path, run.Event, run.HeadSHA, sha, pullRequestNumbers, number) {
				matchingRuns = append(matchingRuns, run)
			} else if domain.IsUnlinkedNoxWorkflowRun(run.Path, run.Event, run.HeadSHA, sha, pullRequestNumbers) {
				unlinkedRunFound = true
			}
		}
		if !hasNext {
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
		} else if unlinkedRunFound {
			result.Reason = "GitHub ran the Security workflow for this revision but did not link the run to a pull request, so its report cannot be attributed to this pull request and is not trusted. GitHub leaves runs unlinked, for example, when the pull request comes from a fork."
		} else {
			result.Reason = "GitHub has no pull-request workflow run for this revision."
		}
		return result
	}
	revisionWorkflowSHA, err := noxWorkflowBlobSHA(ctx, budget, owner, repo, sha)
	if err != nil {
		result.Reason = "GitHub could not verify the Nox workflow at the scanned revision."
		return result
	}
	if reason := noxConfigTrustReason(ctx, budget, owner, repo, sha); reason != "" {
		result.Reason = reason
		return result
	}
	scanControlChanges := map[string]noxScanControlResult{}

	var latestExpired *noxArtifactCandidate
	expiredArtifactFound := false
	candidateFailureRecorded := false
runs:
	for i := range matchingRuns {
		run := &matchingRuns[i]
		// GitHub's workflow-run PR association includes a base SHA, but does not
		// promise it is immutable historical execution provenance. Do not use it
		// to claim that the workflow definition was trusted at run time.
		baseSHA := noxWorkflowRunBaseSHA(*run, number)
		// A report is trusted only when the workflow matches the base: a pull
		// request (including one from a fork, where GitHub omits the base SHA)
		// could otherwise edit or add the workflow and publish a clean report.
		if !fullSHA.MatchString(baseSHA) {
			result.Reason = "GitHub did not identify the base revision for this run, so its Nox workflow could not be verified; its report is not trusted."
			continue
		}
		baseWorkflowSHA, baseErr := noxWorkflowBlobSHA(ctx, budget, owner, repo, baseSHA)
		if errors.Is(baseErr, errNoxInspectionLimit) || errors.Is(baseErr, errRateLimit) {
			result.Reason = noxRequestFailureReason(baseErr)
			return result
		}
		if baseErr != nil {
			result.Reason = "Ghostline could not verify the Nox workflow at the base revision associated with this run; its report is not trusted."
			continue
		}
		// A mismatch rejects a changed workflow; a match is still only
		// corroborating context, not attestation.
		if !domain.IsTrustedNoxWorkflowRevision(baseWorkflowSHA, revisionWorkflowSHA) {
			result.Reason = "The Nox workflow definition differs from the base revision associated with this run; its report is not trusted."
			continue
		}
		// Files besides the workflow also steer the scan (configuration,
		// baseline, ignore rules). A PR that changes one cannot vouch for itself.
		control, checked := scanControlChanges[baseSHA]
		if !checked {
			control.path, control.err = noxScanControlChange(ctx, budget, owner, repo, baseSHA, sha)
			scanControlChanges[baseSHA] = control
		}
		if errors.Is(control.err, errNoxInspectionLimit) || errors.Is(control.err, errRateLimit) {
			result.Reason = noxRequestFailureReason(control.err)
			return result
		}
		if control.err != nil {
			result.Reason = "Ghostline could not verify which scan-control files this pull request changes; its report is not trusted."
			continue
		}
		if control.path != "" {
			result.Reason = fmt.Sprintf("This pull request changes %s, which controls what the Nox scan reports; its report is not trusted.", control.path)
			continue
		}
		artifactsForRun := make([]workflowArtifact, 0)
		artifactHistoryTruncated := false
		for page := 1; page <= maxNoxPages; page++ {
			var response workflowArtifactsResponse
			artifactsPath := fmt.Sprintf("/repos/%s/%s/actions/runs/%d/artifacts?name=%s&per_page=%d&page=%d", url.PathEscape(owner), url.PathEscape(repo), run.ID, url.QueryEscape(noxArtifactName), noxPageSize, page)
			hasNext, err := budget.getWithNext(ctx, artifactsPath, &response)
			if err != nil {
				if canTryOlderNoxArtifact(err) {
					// A temporary listing failure must not hide an older run's report.
					if result.Reason == "" {
						result.Reason = noxRequestFailureReason(err)
					}
					continue runs
				}
				result.Reason = noxRequestFailureReason(err)
				return result
			}
			for j := range response.Artifacts {
				candidate := &response.Artifacts[j]
				artifactIdentity := domain.NoxArtifactCandidate{
					Name: candidate.Name, ArtifactID: candidate.ID, ArtifactRunID: candidate.WorkflowRun.ID,
					ArtifactHeadSHA: candidate.WorkflowRun.HeadSHA, RunID: run.ID, RunHeadSHA: run.HeadSHA,
				}
				if !domain.IsNoxArtifactForRevision(artifactIdentity, noxArtifactName, sha) {
					continue
				}
				if candidate.Expired {
					copy := noxArtifactCandidate{artifact: *candidate, run: *run}
					if latestExpired == nil || candidate.CreatedAt > latestExpired.artifact.CreatedAt {
						latestExpired = &copy
					}
					expiredArtifactFound = true
					continue
				}
				artifactsForRun = append(artifactsForRun, *candidate)
			}
			if !hasNext {
				break
			}
			if page == maxNoxPages {
				// Keep the candidates already listed: one of them may be a valid
				// report. The miss is reported as incomplete only if none loads.
				artifactHistoryTruncated = true
			}
		}
		candidates := make([]domain.NoxArtifactCandidate, 0, len(artifactsForRun))
		for _, artifact := range artifactsForRun {
			candidates = append(candidates, domain.NoxArtifactCandidate{
				Name: artifact.Name, ArtifactID: artifact.ID, ArtifactRunID: artifact.WorkflowRun.ID,
				ArtifactHeadSHA: artifact.WorkflowRun.HeadSHA, CreatedAt: artifact.CreatedAt,
				Expired: artifact.Expired, RunID: run.ID, RunHeadSHA: run.HeadSHA,
			})
		}
		selected := domain.SelectNoxArtifactsForRevision(candidates, noxArtifactName, sha)
		for _, selectedCandidate := range selected {
			artifact := findWorkflowArtifact(artifactsForRun, selectedCandidate.ArtifactID)
			candidate := noxArtifactCandidate{artifact: artifact, run: *run}
			parsed, reason, err := loadNoxArtifact(ctx, budget, candidate, owner, repo, sha)
			if err != nil {
				// The reason must describe the artifact whose provenance is shown.
				failureReason := noxRequestFailureReason(err)
				if errors.Is(err, errNotFound) {
					failureReason = "GitHub’s listed Nox artifact disappeared before it could be downloaded."
				}
				if !candidateFailureRecorded {
					setNoxArtifactProvenance(&result, candidate)
					result.Reason = failureReason
					candidateFailureRecorded = true
				}
				if errors.Is(err, errNotFound) {
					continue
				}
				if canTryOlderNoxArtifact(err) {
					continue
				}
				return result
			}
			if reason != "" {
				if !candidateFailureRecorded {
					setNoxArtifactProvenance(&result, candidate)
					result.Reason = reason
					candidateFailureRecorded = true
				}
				continue
			}
			return parsed
		}
		if artifactHistoryTruncated {
			// This run's unlisted artifacts may hold its report; an older run's
			// report must not stand in for it.
			return noxInspectionIncompleteResult(result, candidateFailureRecorded)
		}
	}
	if workflowHistoryTruncated {
		return noxInspectionIncompleteResult(result, candidateFailureRecorded)
	}
	if result.ArtifactID != 0 {
		return result
	}
	if expiredArtifactFound && latestExpired != nil {
		setNoxArtifactProvenance(&result, *latestExpired)
		result.Reason = "The Nox report has expired from GitHub Actions storage."
		return result
	}
	if result.Reason == "" {
		result.Reason = "No retained Nox report was published for this revision."
	}
	return result
}

// noxInspectionIncompleteResult reports an inspection gap without separating a
// shown artifact's provenance from the reason recorded for that artifact.
func noxInspectionIncompleteResult(result noxReport, candidateFailureRecorded bool) noxReport {
	if !candidateFailureRecorded {
		result.Reason = noxInspectionIncompleteReason
	}
	return result
}

func noxWorkflowRunBaseSHA(run workflowRun, pullRequestNumber int) string {
	for _, pullRequest := range run.PullRequests {
		if pullRequest.Number == pullRequestNumber {
			return pullRequest.Base.SHA
		}
	}
	return ""
}

func findWorkflowArtifact(artifacts []workflowArtifact, id int64) workflowArtifact {
	for _, artifact := range artifacts {
		if artifact.ID == id {
			return artifact
		}
	}
	return workflowArtifact{}
}

func canTryOlderNoxArtifact(err error) bool {
	return domain.NoxArtifactFailureAllowsOlderRun(noxArtifactFailure(err))
}

func noxArtifactFailure(err error) domain.NoxArtifactFailure {
	failure := domain.NoxArtifactFailure{
		RateLimited:            errors.Is(err, errRateLimit),
		InspectionLimitReached: errors.Is(err, errNoxInspectionLimit),
	}
	var responseError statusError
	if errors.As(err, &responseError) {
		failure.HTTPStatus = responseError.code
	}
	return failure
}

func noxWorkflowBlobSHA(ctx context.Context, budget *noxGitHubRequestBudget, owner, repo, ref string) (string, error) {
	var content struct {
		SHA string `json:"sha"`
	}
	requestPath := fmt.Sprintf("/repos/%s/%s/contents/%s?ref=%s", url.PathEscape(owner), url.PathEscape(repo), domain.NoxSecurityWorkflowPath, url.QueryEscape(ref))
	if err := budget.get(ctx, requestPath, &content); err != nil {
		return "", err
	}
	if !fullSHA.MatchString(content.SHA) {
		return "", errors.New("GitHub did not return a valid Nox workflow blob SHA")
	}
	return strings.ToLower(content.SHA), nil
}

type noxScanControlResult struct {
	path string
	err  error
}

// noxScanControlChange returns the first scan-control file the pull request
// changes between its base and the scanned revision, including renames.
func noxScanControlChange(ctx context.Context, budget *noxGitHubRequestBudget, owner, repo, baseSHA, sha string) (string, error) {
	var comparison struct {
		Files *[]struct {
			Filename         string `json:"filename"`
			PreviousFilename string `json:"previous_filename"`
		} `json:"files"`
	}
	requestPath := fmt.Sprintf("/repos/%s/%s/compare/%s...%s", url.PathEscape(owner), url.PathEscape(repo), url.PathEscape(baseSHA), url.PathEscape(sha))
	if err := budget.get(ctx, requestPath, &comparison); err != nil {
		return "", err
	}
	if comparison.Files == nil {
		return "", errors.New("GitHub did not list the compared files")
	}
	if len(*comparison.Files) >= noxCompareFileLimit {
		return "", errors.New("GitHub's compared file list may be truncated")
	}
	paths := make([]string, 0, 2*len(*comparison.Files))
	for _, file := range *comparison.Files {
		paths = append(paths, file.Filename)
		if file.PreviousFilename != "" {
			paths = append(paths, file.PreviousFilename)
		}
	}
	changed, _ := domain.NoxScanControlChange(paths)
	return changed, nil
}

// noxConfigTrustReason rejects a report when .nox.yaml at the scanned revision
// points Nox at a baseline outside the files Ghostline checks.
func noxConfigTrustReason(ctx context.Context, budget *noxGitHubRequestBudget, owner, repo, sha string) string {
	var content struct {
		Encoding string `json:"encoding"`
		Content  string `json:"content"`
	}
	requestPath := fmt.Sprintf("/repos/%s/%s/contents/.nox.yaml?ref=%s", url.PathEscape(owner), url.PathEscape(repo), url.QueryEscape(sha))
	err := budget.get(ctx, requestPath, &content)
	if errors.Is(err, errNotFound) {
		return ""
	}
	if errors.Is(err, errNoxInspectionLimit) || errors.Is(err, errRateLimit) {
		return noxRequestFailureReason(err)
	}
	if err != nil || content.Encoding != "base64" {
		return "Ghostline could not read the Nox configuration at the scanned revision; its report is not trusted."
	}
	config, err := base64.StdEncoding.DecodeString(strings.NewReplacer("\n", "", "\r", "").Replace(content.Content))
	if err != nil {
		return "Ghostline could not read the Nox configuration at the scanned revision; its report is not trusted."
	}
	if domain.NoxConfigMovesBaseline(string(config)) {
		return "The Nox configuration at this revision sets a custom baseline_path, which Ghostline cannot verify; its report is not trusted."
	}
	return ""
}

func setNoxArtifactProvenance(report *noxReport, candidate noxArtifactCandidate) {
	report.RunID = candidate.run.ID
	report.RunName = candidate.run.Name
	report.RunStatus = candidate.run.Status
	report.RunConclusion = candidate.run.Conclusion
	report.RunURL = safeGitHubWebURL(candidate.run.HTMLURL)
	report.ArtifactID = candidate.artifact.ID
	report.ArtifactURL = safeGitHubAPIURL(candidate.artifact.URL)
	report.ArtifactDigest = candidate.artifact.Digest
	report.CreatedAt = candidate.artifact.CreatedAt
	report.ExpiresAt = candidate.artifact.ExpiresAt
}

func loadNoxArtifact(ctx context.Context, budget *noxGitHubRequestBudget, candidate noxArtifactCandidate, owner, repo, sha string) (noxReport, string, error) {
	artifact, run := candidate.artifact, candidate.run
	if artifact.Size < 0 || artifact.Size > maxNoxArtifactBytes {
		return noxReport{}, "GitHub’s Nox artifact is too large for Ghostline to inspect safely.", nil
	}
	if err := budget.ensurePublic(ctx, owner, repo); err != nil {
		return noxReport{}, noxVisibilityFailureReason(err), nil
	}
	archivePath := fmt.Sprintf("/repos/%s/%s/actions/artifacts/%d/zip", url.PathEscape(owner), url.PathEscape(repo), artifact.ID)
	archive, err := budget.downloadArtifact(ctx, archivePath, maxNoxArtifactBytes)
	if err != nil {
		return noxReport{}, "", err
	}
	digest := strings.TrimPrefix(artifact.Digest, "sha256:")
	if len(digest) != sha256.Size*2 {
		return noxReport{}, "GitHub did not provide a verifiable SHA-256 digest for the Nox artifact.", nil
	}
	actual := sha256.Sum256(archive)
	if !strings.EqualFold(digest, hex.EncodeToString(actual[:])) {
		return noxReport{}, "GitHub’s Nox artifact did not match its published SHA-256 digest.", nil
	}
	sarif, err := readNoxSARIFArtifact(archive)
	if err != nil {
		return noxReport{}, "GitHub’s Nox artifact could not be read as a supported SARIF report.", nil
	}
	// Nox reports do not name the commit they scanned; the workflow records it.
	scannedCommit, err := readNoxArtifactFile(archive, noxScannedCommitFilename, maxNoxScannedCommitBytes)
	if err != nil || !domain.IsNoxScannedCommitRecord(string(scannedCommit)) {
		return noxReport{}, "The Nox artifact does not record which commit was scanned, so its report cannot be attributed to this revision; it is not trusted.", nil
	}
	if !domain.NoxScannedCommitMatches(string(scannedCommit), sha) {
		return noxReport{}, "The Nox artifact records that a different commit was scanned, so its report does not describe this revision; it is not trusted.", nil
	}
	runURL := safeGitHubWebURL(run.HTMLURL)
	parsed, err := parseNoxSARIF(sarif, sha, run.ID, runURL, artifact.CreatedAt, artifact.ExpiresAt)
	if err != nil {
		return noxReport{}, "GitHub’s Nox artifact did not contain a valid Nox SARIF report.", nil
	}
	parsed.RunName = run.Name
	parsed.RunStatus = run.Status
	parsed.RunConclusion = run.Conclusion
	parsed.ArtifactID = artifact.ID
	parsed.ArtifactURL = safeGitHubAPIURL(artifact.URL)
	parsed.ArtifactDigest = artifact.Digest
	return parsed, "", nil
}

func pullRequestContainsRevision(ctx context.Context, budget *noxGitHubRequestBudget, owner, repo string, number int, sha string) (bool, error) {
	base := fmt.Sprintf("/repos/%s/%s/pulls/%d/commits", url.PathEscape(owner), url.PathEscape(repo), number)
	for page := 1; ; page++ {
		var commits []struct {
			SHA string `json:"sha"`
		}
		if err := budget.get(ctx, fmt.Sprintf("%s?per_page=%d&page=%d", base, noxPageSize, page), &commits); err != nil {
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
		if page == maxNoxPages {
			return false, errNoxInspectionLimit
		}
	}
}

func readNoxSARIFArtifact(data []byte) ([]byte, error) {
	return readNoxArtifactFile(data, noxSARIFFilename, maxNoxSARIFBytes)
}

// readNoxArtifactFile returns the single bounded file with this base name.
func readNoxArtifactFile(data []byte, name string, maxBytes int) ([]byte, error) {
	if len(data) == 0 || len(data) > maxNoxArtifactBytes {
		return nil, errors.New("artifact size is invalid")
	}
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	var match *zip.File
	for _, file := range reader.File {
		if path.Base(file.Name) != name {
			continue
		}
		if match != nil {
			return nil, fmt.Errorf("artifact contains more than one %s", name)
		}
		if file.UncompressedSize64 > uint64(maxBytes) {
			return nil, fmt.Errorf("%s is too large", name)
		}
		match = file
	}
	if match == nil {
		return nil, fmt.Errorf("%s is missing", name)
	}
	file, err := match.Open()
	if err != nil {
		return nil, err
	}
	defer file.Close()
	contents, err := io.ReadAll(io.LimitReader(file, int64(maxBytes)+1))
	if err != nil {
		return nil, err
	}
	if len(contents) > maxBytes {
		return nil, fmt.Errorf("%s is too large", name)
	}
	return contents, nil
}

func parseNoxSARIF(data []byte, sha string, runID int64, runURL, createdAt, expiresAt string) (noxReport, error) {
	var log sarifLog
	if !fullSHA.MatchString(sha) || len(data) == 0 || len(data) > maxNoxSARIFBytes {
		return noxReport{}, errors.New("report revision or size is invalid")
	}
	if err := json.Unmarshal(data, &log); err != nil {
		return noxReport{}, err
	}
	if err := domain.ValidateNoxSARIFLog(log.Version, len(log.Runs)); err != nil {
		return noxReport{}, err
	}
	report := noxReport{Availability: noxReportAvailable, RevisionSHA: sha, RunID: runID, RunURL: safeURL(runURL), CreatedAt: createdAt, ExpiresAt: expiresAt, Findings: []noxFinding{}}
	findingSummary := domain.NewNoxFindingSummaryBuilder()
	for _, run := range log.Runs {
		if err := domain.ValidateNoxSARIFRun(run.Tool.Driver.Name, run.Results != nil); err != nil {
			return noxReport{}, err
		}
		for _, invocation := range run.Invocations {
			if invocation.ExecutionSuccessful != nil && (*invocation.ExecutionSuccessful == false || report.ScanSuccessful == nil) {
				scanSuccessful := *invocation.ExecutionSuccessful
				report.ScanSuccessful = &scanSuccessful
			}
		}
		ruleByID := indexSARIFrules(run.Tool.Driver.Rules)
		resultsDecoder := json.NewDecoder(bytes.NewReader(*run.Results))
		opening, err := resultsDecoder.Token()
		if err != nil || opening != json.Delim('[') {
			return noxReport{}, errors.New("Nox SARIF run results are not an array")
		}
		for resultsDecoder.More() {
			var result sarifResult
			if err := resultsDecoder.Decode(&result); err != nil {
				return noxReport{}, errors.New("Nox SARIF run contains an invalid result")
			}
			rule := sarifRuleForResult(result, run.Tool.Driver.Rules, ruleByID)
			suppressions := make([]domain.NoxSuppression, 0, min(len(result.Suppressions), domain.MaxNoxSuppressionsPerFinding))
			// Only the first records are kept for display, but the domain decides
			// suppression from every record's status.
			statuses := make([]string, 0, len(result.Suppressions))
			for index, suppression := range result.Suppressions {
				statuses = append(statuses, suppression.Status)
				if index < domain.MaxNoxSuppressionsPerFinding {
					suppressions = append(suppressions, domain.NoxSuppression{Status: suppression.Status, Kind: suppression.Kind, Justification: suppression.Justification})
				}
			}
			truncated := len(result.Suppressions) > domain.MaxNoxSuppressionsPerFinding
			value := domain.NoxSARIFFinding{RuleID: result.RuleID, RuleDefinitionID: rule.ID, RuleName: rule.Name, RuleTitle: rule.ShortDescription.Text, RuleSeverity: rule.Properties.Severity, RuleDefaultLevel: rule.DefaultConfiguration.Level, ResultLevel: result.Level, Kind: result.Kind, Message: result.Message.Text, Markdown: result.Message.Markdown, Fingerprints: result.PartialFingerprints, Suppressions: suppressions, SuppressionStatuses: statuses, SuppressionsTruncated: truncated}
			if len(result.Locations) > 0 {
				location := result.Locations[0].PhysicalLocation
				value.Path = location.ArtifactLocation.URI
				value.Line = location.Region.StartLine
			}
			finding, include := domain.ParseNoxFinding(value)
			if !include {
				continue
			}
			findingSummary.Add(finding)
		}
		closing, err := resultsDecoder.Token()
		if err != nil || closing != json.Delim(']') {
			return noxReport{}, errors.New("Nox SARIF run results array is incomplete")
		}
	}
	summary := findingSummary.Build()
	report.TotalCount = summary.TotalCount
	report.ActiveCount = summary.ActiveCount
	report.SuppressedCount = summary.SuppressedCount
	report.Truncated = summary.Truncated
	for _, finding := range summary.Findings {
		report.Findings = append(report.Findings, noxFindingFromDomain(finding))
	}
	return report, nil
}

func indexSARIFrules(rules []sarifRule) map[string]sarifRule {
	byID := make(map[string]sarifRule, len(rules))
	for _, rule := range rules {
		if rule.ID == "" {
			continue
		}
		if _, exists := byID[rule.ID]; !exists {
			byID[rule.ID] = rule
		}
	}
	return byID
}

func sarifRuleForResult(result sarifResult, rules []sarifRule, ruleByID map[string]sarifRule) sarifRule {
	if result.RuleIndex != nil && *result.RuleIndex >= 0 && *result.RuleIndex < len(rules) {
		indexed := rules[*result.RuleIndex]
		if indexed.ID != "" && (result.RuleID == "" || indexed.ID == result.RuleID) {
			return indexed
		}
	}
	if rule, exists := ruleByID[result.RuleID]; exists {
		return rule
	}
	return sarifRule{}
}

func safeGitHubWebURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || !strings.EqualFold(parsed.Host, "github.com") {
		return ""
	}
	return parsed.String()
}

func safeGitHubAPIURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || !strings.EqualFold(parsed.Host, "api.github.com") {
		return ""
	}
	return parsed.String()
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
	redirected := false
	client.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if len(via) >= 5 || next.URL.Scheme != "https" || !allowedArtifactHost(next.URL.Hostname()) {
			return http.ErrUseLastResponse
		}
		if previousCheck != nil {
			if err := previousCheck(next, via); err != nil {
				return err
			}
		}
		// A redirect between GitHub hosts is still GitHub; anything else is storage.
		if host := strings.TrimSuffix(strings.ToLower(next.URL.Hostname()), "."); host != "github.com" && host != "api.github.com" {
			redirected = true
		}
		return nil
	}
	// After GitHub redirects to artifact storage, failures belong to storage. Its URL is
	// signed, so it is never logged, not even its path.
	logFailure := func(status int, failure string) {
		if redirected {
			logUpstreamFailure(ctx, "artifact-storage", "", status, failure)
			return
		}
		logUpstreamFailure(ctx, "github", endpoint, status, failure)
	}
	response, err := client.Do(req)
	if err != nil {
		logFailure(0, transportFailure(ctx, err))
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		logFailure(response.StatusCode, "status")
		if response.StatusCode == http.StatusNotFound {
			return nil, errNotFound
		}
		if response.StatusCode == http.StatusTooManyRequests || response.StatusCode == http.StatusForbidden && (response.Header.Get("X-RateLimit-Remaining") == "0" || response.Header.Get("Retry-After") != "") {
			return nil, errRateLimit
		}
		return nil, statusError{code: response.StatusCode}
	}
	if response.ContentLength > maxBytes {
		logFailure(response.StatusCode, "invalid_response")
		return nil, errors.New("artifact exceeds size limit")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxBytes+1))
	if err != nil {
		logFailure(response.StatusCode, transportFailure(ctx, err))
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		logFailure(response.StatusCode, "invalid_response")
		return nil, errors.New("artifact exceeds size limit")
	}
	return data, nil
}

func allowedArtifactHost(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	return host == "github.com" || host == "api.github.com" || strings.HasSuffix(host, ".githubusercontent.com") || strings.HasSuffix(host, ".blob.core.windows.net")
}
