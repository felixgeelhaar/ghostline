package domain

import (
	"net/url"
	"path"
	"sort"
	"strings"
)

const NoxSecurityWorkflowPath = ".github/workflows/nox.yml"
const MaxNoxSuppressionsPerFinding = 20
const MaxNoxFindings = 100

// NoxSuppression preserves one source-provided SARIF suppression record.
type NoxSuppression struct {
	Status        string
	Kind          string
	Justification string
}

// IsNoxWorkflowRun reports whether GitHub identified this run as the repository's
// Nox pull-request workflow for the requested revision.
func IsNoxWorkflowRun(workflowPath, event, runSHA, requestedSHA string) bool {
	workflowMatches := workflowPath == NoxSecurityWorkflowPath ||
		(strings.HasPrefix(workflowPath, NoxSecurityWorkflowPath+"@") && len(workflowPath) > len(NoxSecurityWorkflowPath)+1)
	return workflowMatches && event == "pull_request" && runSHA != "" && runSHA == requestedSHA
}

// IsNoxWorkflowRunForPullRequest also verifies that GitHub associates the run
// with the requested pull request. A shared commit SHA alone is not PR identity.
func IsNoxWorkflowRunForPullRequest(workflowPath, event, runSHA, requestedSHA string, pullRequestNumbers []int, requestedNumber int) bool {
	if requestedNumber < 1 || !IsNoxWorkflowRun(workflowPath, event, runSHA, requestedSHA) {
		return false
	}
	for _, number := range pullRequestNumbers {
		if number == requestedNumber {
			return true
		}
	}
	return false
}

// IsNoxScannerName checks whether a SARIF run identifies Nox as its scanner.
func IsNoxScannerName(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "nox", "nox security scanner":
		return true
	default:
		return false
	}
}

// NoxArtifactCandidate contains only source identity needed to choose a report
// for a requested revision. Transport-specific artifact fields stay at the edge.
type NoxArtifactCandidate struct {
	Name            string
	ArtifactID      int64
	ArtifactRunID   int64
	ArtifactHeadSHA string
	CreatedAt       string
	Expired         bool
	RunID           int64
	RunHeadSHA      string
}

// IsNoxArtifactForRevision validates that GitHub ties an artifact and its
// workflow run to the same requested revision, including expired artifacts.
func IsNoxArtifactForRevision(candidate NoxArtifactCandidate, artifactName, revisionSHA string) bool {
	return candidate.Name == artifactName && candidate.ArtifactID > 0 && candidate.RunID > 0 &&
		candidate.ArtifactRunID == candidate.RunID && candidate.RunHeadSHA == revisionSHA &&
		candidate.ArtifactHeadSHA == revisionSHA
}

// SelectNoxArtifactsForRevision keeps artifacts whose GitHub run and artifact
// both identify the requested revision, newest first.
func SelectNoxArtifactsForRevision(candidates []NoxArtifactCandidate, artifactName, revisionSHA string) []NoxArtifactCandidate {
	selected := make([]NoxArtifactCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if !IsNoxArtifactForRevision(candidate, artifactName, revisionSHA) || candidate.Expired {
			continue
		}
		selected = append(selected, candidate)
	}
	sort.SliceStable(selected, func(i, j int) bool { return selected[i].CreatedAt > selected[j].CreatedAt })
	return selected
}

// NoxFinding is a source-backed finding extracted from a Nox SARIF result.
type NoxFinding struct {
	RuleID                   string
	Title                    string
	Severity                 string
	Message                  string
	Path                     string
	Line                     int
	Fingerprint              string
	Suppressed               bool
	SuppressionKind          string
	SuppressionJustification string
	Suppressions             []NoxSuppression
	SuppressionsTruncated    bool
}

// NoxReport is the domain representation of one Nox result set.
type NoxReport struct {
	RevisionSHA     string
	Findings        []NoxFinding
	TotalCount      int
	ActiveCount     int
	SuppressedCount int
	Truncated       bool
}

// NoxSARIFFinding preserves normalized finding data from SARIF.
type NoxSARIFFinding struct {
	RuleID                   string
	RuleDefinitionID         string
	RuleName                 string
	RuleTitle                string
	RuleSeverity             string
	ResultLevel              string
	Message                  string
	Markdown                 string
	Path                     string
	Line                     int
	Fingerprints             map[string]string
	Suppressed               bool
	SuppressionKind          string
	SuppressionJustification string
	Suppressions             []NoxSuppression
	SuppressionsTruncated    bool
}

// ParseNoxFinding converts one Nox SARIF result to Ghostline's domain model.
func ParseNoxFinding(value NoxSARIFFinding) (NoxFinding, bool) {
	path := SafeNoxPath(value.Path)
	line := 0
	if path != "" && value.Line > 0 {
		line = value.Line
	}
	finding := NoxFinding{
		RuleID:                   BoundedNoxText(value.RuleID, 200),
		Title:                    BoundedNoxText(value.RuleTitle, 1000),
		Severity:                 NoxSeverity(value.ResultLevel),
		Message:                  BoundedNoxText(value.Message, 2000),
		Path:                     path,
		Line:                     line,
		Suppressed:               value.Suppressed || HasAcceptedNoxSuppression(value.Suppressions),
		SuppressionKind:          BoundedNoxText(value.SuppressionKind, 100),
		SuppressionJustification: BoundedNoxText(value.SuppressionJustification, 1000),
		SuppressionsTruncated:    value.SuppressionsTruncated || len(value.Suppressions) > MaxNoxSuppressionsPerFinding,
	}
	if finding.RuleID == "" {
		finding.RuleID = BoundedNoxText(value.RuleDefinitionID, 200)
	}
	if finding.Title == "" {
		finding.Title = BoundedNoxText(value.RuleName, 1000)
	}
	for _, suppression := range value.Suppressions {
		if suppression.Status != "accepted" {
			continue
		}
		if finding.SuppressionKind == "" {
			finding.SuppressionKind = BoundedNoxText(suppression.Kind, 100)
		}
		if finding.SuppressionJustification == "" {
			finding.SuppressionJustification = BoundedNoxText(suppression.Justification, 1000)
		}
		if finding.SuppressionKind != "" && finding.SuppressionJustification != "" {
			break
		}
	}
	if len(value.Suppressions) > MaxNoxSuppressionsPerFinding {
		value.Suppressions = value.Suppressions[:MaxNoxSuppressionsPerFinding]
	}
	finding.Suppressions = make([]NoxSuppression, 0, len(value.Suppressions))
	for _, suppression := range value.Suppressions {
		finding.Suppressions = append(finding.Suppressions, NoxSuppression{
			Status: BoundedNoxText(suppression.Status, 100), Kind: BoundedNoxText(suppression.Kind, 100),
			Justification: BoundedNoxText(suppression.Justification, 1000),
		})
	}
	if finding.Message == "" {
		finding.Message = BoundedNoxText(value.Markdown, 2000)
	}
	if finding.Title == "" {
		finding.Title = finding.RuleID
	}
	if finding.RuleID == "" {
		finding.RuleID = "unclassified"
	}
	if value.RuleSeverity != "" {
		finding.Severity = NoxSeverity(value.RuleSeverity)
	}
	for _, key := range []string{"primaryLocationLineHash", "primaryLocationLineHash/v1", "noxFingerprint"} {
		if fingerprint := value.Fingerprints[key]; fingerprint != "" {
			finding.Fingerprint = BoundedNoxText(fingerprint, 200)
			break
		}
	}
	return finding, true
}

// SummarizeNoxFindings keeps the visible result list bounded while preserving
// exact report counts and suppression classifications from every finding.
func SummarizeNoxFindings(findings []NoxFinding) NoxReport {
	report := NoxReport{Findings: make([]NoxFinding, 0, min(len(findings), MaxNoxFindings))}
	for _, finding := range findings {
		report.TotalCount++
		if finding.Suppressed {
			report.SuppressedCount++
		} else {
			report.ActiveCount++
		}
		if len(report.Findings) < MaxNoxFindings {
			report.Findings = append(report.Findings, finding)
		}
	}
	report.Truncated = report.TotalCount > len(report.Findings)
	return report
}

// HasAcceptedNoxSuppression reports whether SARIF records an accepted suppression.
func HasAcceptedNoxSuppression(suppressions []NoxSuppression) bool {
	for _, suppression := range suppressions {
		if suppression.Status == "accepted" {
			return true
		}
	}
	return false
}

// NoxSeverity normalizes SARIF severity labels for the interface.
func NoxSeverity(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "critical":
		return "critical"
	case "high", "error":
		return "high"
	case "medium", "moderate", "warning":
		return "medium"
	case "low", "note":
		return "low"
	case "info", "informational", "none":
		return "info"
	default:
		return "unknown"
	}
}

// SafeNoxPath returns a repository-relative path or empty when SARIF supplied
// an absolute or traversal path.
func SafeNoxPath(value string) string {
	if value == "" || strings.Contains(value, "\\") {
		return ""
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.IsAbs() || parsed.Host != "" || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || parsed.Opaque != "" || strings.HasPrefix(parsed.Path, "/") || strings.Contains(parsed.Path, "\\") {
		return ""
	}
	for _, segment := range strings.Split(parsed.Path, "/") {
		if segment == ".." || segment == "." {
			return ""
		}
	}
	clean := path.Clean(parsed.Path)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return ""
	}
	return clean
}

// BoundedNoxText limits untrusted scanner output by rune count.
func BoundedNoxText(value string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	if len(value) <= maxRunes*4 {
		count := 0
		for index := range value {
			if count == maxRunes {
				return value[:index]
			}
			count++
		}
		return value
	}
	runes := []rune(value)
	if len(runes) > maxRunes {
		return string(runes[:maxRunes])
	}
	return value
}
