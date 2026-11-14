package domain

import (
	"strings"
	"testing"
)

func TestValidateQodoReviewSnapshot(t *testing.T) {
	sha := strings.Repeat("a", 40)
	valid := QodoReviewSnapshot{
		ReviewSession: &QodoReviewSession{Status: "completed", CommitSHA: sha},
		Findings:      []QodoReviewFinding{{ID: "finding-1", Title: "Guard retries", GitSHA: sha, StartLine: 12, EndLine: 14}},
		ReviewRuns:    []QodoReviewRun{{ID: "run-1", Status: "completed", CommitSHA: sha}},
	}
	if err := ValidateQodoReviewSnapshot(valid); err != nil {
		t.Fatalf("valid Qodo snapshot rejected: %v", err)
	}

	tests := []struct {
		name string
		edit func(*QodoReviewSnapshot)
	}{
		{"session without status", func(snapshot *QodoReviewSnapshot) { snapshot.ReviewSession.Status = " " }},
		{"invalid session revision", func(snapshot *QodoReviewSnapshot) { snapshot.ReviewSession.CommitSHA = "short" }},
		{"finding without id", func(snapshot *QodoReviewSnapshot) { snapshot.Findings[0].ID = "" }},
		{"finding with blank id", func(snapshot *QodoReviewSnapshot) { snapshot.Findings[0].ID = " \t " }},
		{"finding without title", func(snapshot *QodoReviewSnapshot) { snapshot.Findings[0].Title = " " }},
		{"invalid finding revision", func(snapshot *QodoReviewSnapshot) { snapshot.Findings[0].GitSHA = "short" }},
		{"negative finding line", func(snapshot *QodoReviewSnapshot) { snapshot.Findings[0].StartLine = -1 }},
		{"end before start", func(snapshot *QodoReviewSnapshot) { snapshot.Findings[0].EndLine = 11 }},
		{"run without id", func(snapshot *QodoReviewSnapshot) { snapshot.ReviewRuns[0].ID = "" }},
		{"run with blank id", func(snapshot *QodoReviewSnapshot) { snapshot.ReviewRuns[0].ID = " \t " }},
		{"run without status", func(snapshot *QodoReviewSnapshot) { snapshot.ReviewRuns[0].Status = " " }},
		{"invalid run revision", func(snapshot *QodoReviewSnapshot) { snapshot.ReviewRuns[0].CommitSHA = "short" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			candidate := QodoReviewSnapshot{
				ReviewSession: &QodoReviewSession{Status: valid.ReviewSession.Status, CommitSHA: valid.ReviewSession.CommitSHA},
				Findings:      append([]QodoReviewFinding(nil), valid.Findings...),
				ReviewRuns:    append([]QodoReviewRun(nil), valid.ReviewRuns...),
			}
			tt.edit(&candidate)
			if err := ValidateQodoReviewSnapshot(candidate); err == nil {
				t.Fatal("invalid Qodo snapshot was accepted")
			}
		})
	}
}

func TestQodoReviewEvidenceCountBelongsToDomain(t *testing.T) {
	findings := []QodoReviewFinding{{
		Description: "Description",
		FilePath:    "backend/handler.go",
		StartLine:   10,
		EndLine:     12,
		CodeSnippet: "return nil",
		Evidence: &QodoFindingEvidence{
			Explanation: "Review rationale",
			Citations: []QodoFindingCitation{{
				SourceType: "code", Source: "backend/handler.go", Text: "call()",
				Context: "handler context", HasMetadata: true,
			}},
		},
	}}
	if got, want := QodoReviewEvidenceCount(findings), 12; got != want {
		t.Fatalf("evidence count = %d, want %d", got, want)
	}
}
