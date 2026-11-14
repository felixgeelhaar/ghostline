package domain

import (
	"errors"
	"regexp"
	"strings"
	"time"
)

var fullGitSHA = regexp.MustCompile(`^[0-9a-f]{40}$`)

// QodoReviewSnapshot contains the source facts needed to validate a structured
// Qodo response. Transport-specific evidence remains in the API layer.
type QodoReviewSnapshot struct {
	ReviewSession *QodoReviewSession
	Findings      []QodoReviewFinding
	ReviewRuns    []QodoReviewRun
}

type QodoReviewSession struct {
	Status    string
	CommitSHA string
	StartedAt *time.Time
}

type QodoReviewFinding struct {
	ID                string
	Title             string
	Description       string
	Category          string
	ActionLevel       string
	AttributionStatus string
	GitSHA            string
	Status            string
	ResolutionReason  string
	ReviewRunID       string
	FilePath          string
	StartLine         int
	EndLine           int
	Side              string
	CodeSnippet       string
	Evidence          *QodoFindingEvidence
}

type QodoFindingEvidence struct {
	Explanation string
	Citations   []QodoFindingCitation
}

type QodoFindingCitation struct {
	SourceType string
	Source     string
	Text       string
	Context    string
	Metadata   string
}

type QodoReviewRun struct {
	ID        string
	Status    string
	CommitSHA string
	StartedAt *time.Time
}

// MergeQodoReviewSessions merges aliases and duplicate session records while
// rejecting contradictory source facts.
func MergeQodoReviewSessions(preferred, fallback *QodoReviewSession) (*QodoReviewSession, error) {
	if preferred == nil {
		return fallback, nil
	}
	if fallback == nil {
		return preferred, nil
	}
	status := preferred.Status
	if status == "" {
		status = fallback.Status
	} else if fallback.Status != "" && !strings.EqualFold(status, fallback.Status) {
		return nil, errors.New("Qodo returned conflicting review-session statuses")
	}
	sha, err := mergeQodoRevisions("review-session revisions", preferred.CommitSHA, fallback.CommitSHA)
	if err != nil {
		return nil, err
	}
	startedAt, err := mergeQodoTimes("review-session start times", preferred.StartedAt, fallback.StartedAt)
	if err != nil {
		return nil, err
	}
	return &QodoReviewSession{Status: status, CommitSHA: sha, StartedAt: startedAt}, nil
}

// MergeQodoReviewFindings applies the domain identity and conflict policy to
// findings from one or more representations of a Qodo response.
func MergeQodoReviewFindings(values []QodoReviewFinding) ([]QodoReviewFinding, error) {
	indexes := make(map[string]int, len(values))
	result := make([]QodoReviewFinding, 0, len(values))
	for _, value := range values {
		if index, exists := indexes[value.ID]; exists {
			merged, err := mergeQodoFinding(result[index], value)
			if err != nil {
				return nil, errors.New("Qodo returned a conflicting duplicate finding " + value.ID + ": " + err.Error())
			}
			result[index] = merged
			continue
		}
		indexes[value.ID] = len(result)
		result = append(result, value)
	}
	return result, nil
}

// MergeQodoReviewRuns applies the domain identity and conflict policy to runs.
func MergeQodoReviewRuns(values []QodoReviewRun) ([]QodoReviewRun, error) {
	indexes := make(map[string]int, len(values))
	result := make([]QodoReviewRun, 0, len(values))
	for _, value := range values {
		if index, exists := indexes[value.ID]; exists {
			merged, err := mergeQodoReviewRun(result[index], value)
			if err != nil {
				return nil, errors.New("Qodo returned a conflicting duplicate review run " + value.ID + ": " + err.Error())
			}
			result[index] = merged
			continue
		}
		indexes[value.ID] = len(result)
		result = append(result, value)
	}
	return result, nil
}

func mergeQodoFinding(left, right QodoReviewFinding) (QodoReviewFinding, error) {
	merged := left
	var err error
	merged.GitSHA, err = mergeQodoRevisions("finding revisions", left.GitSHA, right.GitSHA)
	if err != nil {
		return QodoReviewFinding{}, err
	}
	for _, pair := range []struct {
		name string
		dst  *string
		src  string
	}{
		{"titles", &merged.Title, right.Title}, {"descriptions", &merged.Description, right.Description},
		{"categories", &merged.Category, right.Category}, {"action levels", &merged.ActionLevel, right.ActionLevel},
		{"resolution reasons", &merged.ResolutionReason, right.ResolutionReason}, {"review-run IDs", &merged.ReviewRunID, right.ReviewRunID},
		{"file paths", &merged.FilePath, right.FilePath}, {"diff sides", &merged.Side, right.Side}, {"code snippets", &merged.CodeSnippet, right.CodeSnippet},
	} {
		*pair.dst, err = MergeQodoStringAliases("finding "+pair.name, *pair.dst, pair.src)
		if err != nil {
			return QodoReviewFinding{}, err
		}
	}
	merged.AttributionStatus, err = mergeQodoEnumAliases("finding attribution statuses", merged.AttributionStatus, right.AttributionStatus)
	if err != nil {
		return QodoReviewFinding{}, err
	}
	merged.Status, err = mergeQodoEnumAliases("finding statuses", merged.Status, right.Status)
	if err != nil {
		return QodoReviewFinding{}, err
	}
	merged.StartLine, err = MergeQodoIntAliases("finding start lines", merged.StartLine, right.StartLine)
	if err != nil {
		return QodoReviewFinding{}, err
	}
	merged.EndLine, err = MergeQodoIntAliases("finding end lines", merged.EndLine, right.EndLine)
	if err != nil {
		return QodoReviewFinding{}, err
	}
	if right.Evidence != nil {
		if merged.Evidence == nil {
			merged.Evidence = right.Evidence
		} else {
			merged.Evidence = &QodoFindingEvidence{Explanation: merged.Evidence.Explanation, Citations: append([]QodoFindingCitation(nil), merged.Evidence.Citations...)}
			merged.Evidence.Explanation, err = MergeQodoStringAliases("finding evidence explanations", merged.Evidence.Explanation, right.Evidence.Explanation)
			if err != nil {
				return QodoReviewFinding{}, err
			}
			seen := make(map[QodoFindingCitation]struct{}, len(merged.Evidence.Citations)+len(right.Evidence.Citations))
			for _, citation := range merged.Evidence.Citations {
				seen[citation] = struct{}{}
			}
			for _, citation := range right.Evidence.Citations {
				if _, duplicate := seen[citation]; duplicate {
					continue
				}
				seen[citation] = struct{}{}
				merged.Evidence.Citations = append(merged.Evidence.Citations, citation)
			}
		}
	}
	return merged, nil
}

func mergeQodoReviewRun(left, right QodoReviewRun) (QodoReviewRun, error) {
	status, err := mergeQodoEnumAliases("review-run statuses", left.Status, right.Status)
	if err != nil {
		return QodoReviewRun{}, err
	}
	sha, err := mergeQodoRevisions("review-run revisions", left.CommitSHA, right.CommitSHA)
	if err != nil {
		return QodoReviewRun{}, err
	}
	startedAt, err := mergeQodoTimes("review-run start times", left.StartedAt, right.StartedAt)
	if err != nil {
		return QodoReviewRun{}, err
	}
	return QodoReviewRun{ID: left.ID, Status: status, CommitSHA: sha, StartedAt: startedAt}, nil
}

func mergeQodoEnumAliases(field string, preferred, fallback string) (string, error) {
	if preferred != "" && fallback != "" && !strings.EqualFold(preferred, fallback) {
		return "", errors.New("Qodo returned conflicting " + field)
	}
	if preferred != "" {
		return preferred, nil
	}
	return fallback, nil
}

func mergeQodoRevisions(field, preferred, fallback string) (string, error) {
	if preferred != "" && fallback != "" && !strings.EqualFold(preferred, fallback) {
		return "", errors.New("Qodo returned conflicting " + field)
	}
	if preferred != "" {
		return preferred, nil
	}
	return fallback, nil
}

func mergeQodoTimes(field string, preferred, fallback *time.Time) (*time.Time, error) {
	if preferred != nil && fallback != nil && !preferred.Equal(*fallback) {
		return nil, errors.New("Qodo returned conflicting " + field)
	}
	if preferred != nil {
		return preferred, nil
	}
	return fallback, nil
}

// MergeQodoStringAliases accepts matching or single-source values and rejects
// conflicting non-empty aliases.
func MergeQodoStringAliases(field string, values ...string) (string, error) {
	merged := ""
	for _, value := range values {
		if value == "" {
			continue
		}
		if merged != "" && merged != value {
			return "", errors.New("Qodo returned conflicting " + field)
		}
		merged = value
	}
	return merged, nil
}

// MergeQodoIntAliases accepts matching or single-source positive values.
func MergeQodoIntAliases(field string, values ...int) (int, error) {
	merged := 0
	for _, value := range values {
		if value == 0 {
			continue
		}
		if merged != 0 && merged != value {
			return 0, errors.New("Qodo returned conflicting " + field)
		}
		merged = value
	}
	return merged, nil
}

// ValidateQodoReviewSnapshot enforces the minimum identity and revision
// invariants for Qodo review data before it is exposed to callers.
func ValidateQodoReviewSnapshot(snapshot QodoReviewSnapshot) error {
	if session := snapshot.ReviewSession; session != nil {
		if strings.TrimSpace(session.Status) == "" {
			return errors.New("Qodo review session has no status")
		}
		if session.CommitSHA != "" && !fullGitSHA.MatchString(strings.ToLower(session.CommitSHA)) {
			return errors.New("Qodo review session contained an invalid commit SHA")
		}
	}
	for _, finding := range snapshot.Findings {
		if strings.TrimSpace(finding.ID) == "" || strings.TrimSpace(finding.Title) == "" {
			return errors.New("Qodo returned a finding without its native ID or title")
		}
		if finding.GitSHA != "" && !fullGitSHA.MatchString(strings.ToLower(finding.GitSHA)) {
			return errors.New("Qodo finding contained an invalid commit SHA")
		}
		if finding.StartLine < 0 || finding.EndLine < 0 {
			return errors.New("Qodo finding contained a negative source line")
		}
		if finding.StartLine > 0 && finding.EndLine > 0 && finding.EndLine < finding.StartLine {
			return errors.New("Qodo finding contained an end line before its start line")
		}
	}
	for _, run := range snapshot.ReviewRuns {
		if strings.TrimSpace(run.ID) == "" || strings.TrimSpace(run.Status) == "" {
			return errors.New("Qodo returned a review run without its native ID or status")
		}
		if run.CommitSHA != "" && !fullGitSHA.MatchString(strings.ToLower(run.CommitSHA)) {
			return errors.New("Qodo review run contained an invalid commit SHA")
		}
	}
	return nil
}
