package domain

import (
	"errors"
	"regexp"
	"strings"
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
}

type QodoReviewFinding struct {
	ID          string
	Title       string
	Description string
	GitSHA      string
	FilePath    string
	StartLine   int
	EndLine     int
	CodeSnippet string
	Evidence    *QodoFindingEvidence
}

type QodoFindingEvidence struct {
	Explanation string
	Citations   []QodoFindingCitation
}

type QodoFindingCitation struct {
	SourceType  string
	Source      string
	Text        string
	Context     string
	HasMetadata bool
}

type QodoReviewRun struct {
	ID        string
	Status    string
	CommitSHA string
}

// QodoReviewEvidenceCount scores retained source evidence for domain-level
// comparisons. A citation itself is useful evidence even when sparse.
func QodoReviewEvidenceCount(findings []QodoReviewFinding) int {
	count := 0
	for _, finding := range findings {
		for _, value := range []string{finding.Description, finding.FilePath, finding.CodeSnippet} {
			if value != "" {
				count++
			}
		}
		if finding.StartLine != 0 {
			count++
		}
		if finding.EndLine != 0 {
			count++
		}
		if finding.Evidence == nil {
			continue
		}
		if finding.Evidence.Explanation != "" {
			count++
		}
		for _, citation := range finding.Evidence.Citations {
			count++
			for _, value := range []string{citation.SourceType, citation.Source, citation.Text, citation.Context} {
				if value != "" {
					count++
				}
			}
			if citation.HasMetadata {
				count++
			}
		}
	}
	return count
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
