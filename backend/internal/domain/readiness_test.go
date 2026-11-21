package domain

import (
	"strings"
	"testing"
)

const (
	readyHead = "abcdef1111111111111111111111111111111111"
	olderHead = "2222222222222222222222222222222222222222"
	actionsID = int64(15368)
)

// readyEvidence is a PR that passes every rule; each case changes one thing.
func readyEvidence() ReadinessEvidence {
	return ReadinessEvidence{
		HeadSHA: readyHead, Open: true, BaseRef: "main", DefaultBranch: "main", BehindBy: 0,
		CompareURL:          "https://github.com/acme/app/compare/main..." + readyHead,
		Review:              &QodoReviewEvidence{CommitSHA: readyHead, URL: "https://github.com/acme/app/pull/7#issuecomment-1", CountsReadable: true},
		RequiredChecksKnown: true,
		RequiredChecks: []RequiredCheck{
			{Context: "Go checks", AppID: actionsID},
			{Context: "warden/gate"},
		},
		CheckRuns: []CheckRunEvidence{
			{ID: 10, Name: "Go checks", AppID: actionsID, Status: "completed", Conclusion: "success", URL: "https://github.com/acme/app/runs/10"},
		},
		Statuses: []CommitStatusEvidence{
			{Context: "warden/gate", State: "success", UpdatedAt: "2026-11-21T10:00:00Z"},
		},
	}
}

func TestEvaluateReadiness(t *testing.T) {
	tests := []struct {
		name        string
		change      func(*ReadinessEvidence)
		want        ReadinessOutcome
		wantMessage string
	}{
		{name: "everything covers the head", change: func(*ReadinessEvidence) {}, want: ReadinessPass},
		{name: "head is not a full SHA", change: func(e *ReadinessEvidence) { e.HeadSHA = "abc" }, want: ReadinessIncomplete, wantMessage: "GitHub did not identify the pull request's head commit"},
		{name: "closed PR", change: func(e *ReadinessEvidence) { e.Open = false }, want: ReadinessIncomplete, wantMessage: "not open"},
		{name: "draft PR", change: func(e *ReadinessEvidence) { e.Draft = true }, want: ReadinessIncomplete, wantMessage: "draft"},
		{name: "targets another branch", change: func(e *ReadinessEvidence) { e.BaseRef = "release" }, want: ReadinessIncomplete, wantMessage: "targets release, not main"},
		{name: "behind the default branch", change: func(e *ReadinessEvidence) { e.BehindBy = 3 }, want: ReadinessIncomplete, wantMessage: "behind main by 3"},
		{name: "behind by one commit", change: func(e *ReadinessEvidence) { e.BehindBy = 1 }, want: ReadinessIncomplete, wantMessage: "behind main by 1 commit."},
		{name: "behind count unknown", change: func(e *ReadinessEvidence) { e.BehindBy = BehindUnknown }, want: ReadinessIncomplete, wantMessage: "could not compare the head with main"},
		{name: "check evidence unreadable", change: func(e *ReadinessEvidence) {
			e.CheckEvidenceUnavailableReason = "GitHub returned checks for a different commit than the pull request's head."
		}, want: ReadinessIncomplete, wantMessage: "different commit"},
		{name: "no Qodo review", change: func(e *ReadinessEvidence) { e.Review = nil }, want: ReadinessIncomplete, wantMessage: "No Qodo code review"},
		{name: "reviews could not be loaded", change: func(e *ReadinessEvidence) {
			e.Review = nil
			e.ReviewUnavailableReason = "Qodo review comments could not be loaded from GitHub."
		}, want: ReadinessIncomplete, wantMessage: "Qodo review comments could not be loaded from GitHub."},
		{name: "review names no commit", change: func(e *ReadinessEvidence) { e.Review.CommitSHA = "" }, want: ReadinessIncomplete, wantMessage: "names no commit"},
		{name: "review covers an older head", change: func(e *ReadinessEvidence) { e.Review.CommitSHA = olderHead }, want: ReadinessIncomplete, wantMessage: "covers 2222222, not the head"},
		{name: "only the summary names the head", change: func(e *ReadinessEvidence) { e.Review.CommitSHA = olderHead; e.SummaryNamesHead = true }, want: ReadinessIncomplete, wantMessage: "only Qodo's PR summary names the head"},
		{name: "unreadable finding counts", change: func(e *ReadinessEvidence) { e.Review.CountsReadable = false }, want: ReadinessIncomplete, wantMessage: "could not read"},
		{name: "medium finding", change: func(e *ReadinessEvidence) { e.Review.Medium = 1 }, want: ReadinessFail, wantMessage: "1 High or Medium"},
		{name: "high finding", change: func(e *ReadinessEvidence) { e.Review.High = 2 }, want: ReadinessFail, wantMessage: "2 High or Medium"},
		{name: "low findings are reported, not blocking", change: func(e *ReadinessEvidence) { e.Review.Low = 2 }, want: ReadinessPass, wantMessage: "2 Low findings to report"},
		{name: "required checks unreadable", change: func(e *ReadinessEvidence) { e.RequiredChecksKnown = false }, want: ReadinessIncomplete, wantMessage: "could not read which checks"},
		{name: "no required checks", change: func(e *ReadinessEvidence) { e.RequiredChecks = nil }, want: ReadinessIncomplete, wantMessage: "requires no status checks"},
		{name: "required check missing", change: func(e *ReadinessEvidence) { e.CheckRuns = nil }, want: ReadinessIncomplete, wantMessage: "Go checks: missing on the head"},
		{name: "required check failed", change: func(e *ReadinessEvidence) { e.CheckRuns[0].Conclusion = "failure" }, want: ReadinessFail, wantMessage: "Go checks: failure"},
		{name: "required check still running", change: func(e *ReadinessEvidence) { e.CheckRuns[0].Status = "in_progress"; e.CheckRuns[0].Conclusion = "" }, want: ReadinessIncomplete, wantMessage: "Go checks: in_progress"},
		{name: "skipped counts as passed", change: func(e *ReadinessEvidence) { e.CheckRuns[0].Conclusion = "skipped" }, want: ReadinessPass},
		{name: "earlier failure on the same head", change: func(e *ReadinessEvidence) {
			e.CheckRuns = append([]CheckRunEvidence{{ID: 5, Name: "Go checks", AppID: actionsID, Status: "completed", Conclusion: "failure"}}, e.CheckRuns...)
		}, want: ReadinessFail, wantMessage: "an earlier run on this head concluded failure"},
		{name: "queued rerun is the latest run", change: func(e *ReadinessEvidence) {
			e.CheckRuns = append(e.CheckRuns, CheckRunEvidence{ID: 90, Name: "Go checks", AppID: actionsID, Status: "queued"})
		}, want: ReadinessIncomplete, wantMessage: "Go checks: queued"},
		{name: "same name from another app does not count", change: func(e *ReadinessEvidence) { e.CheckRuns[0].AppID = 999 }, want: ReadinessIncomplete, wantMessage: "Go checks: missing on the head"},
		{name: "a status cannot satisfy an app-bound check", change: func(e *ReadinessEvidence) {
			e.CheckRuns = nil
			e.Statuses = append(e.Statuses, CommitStatusEvidence{Context: "Go checks", State: "success"})
		}, want: ReadinessIncomplete, wantMessage: "Go checks: missing on the head"},
		{name: "status missing", change: func(e *ReadinessEvidence) { e.Statuses = nil }, want: ReadinessIncomplete, wantMessage: "warden/gate: missing on the head"},
		{name: "status pending", change: func(e *ReadinessEvidence) { e.Statuses[0].State = "pending" }, want: ReadinessIncomplete, wantMessage: "warden/gate: pending"},
		{name: "status failed", change: func(e *ReadinessEvidence) { e.Statuses[0].State = "failure" }, want: ReadinessFail, wantMessage: "warden/gate: failure"},
		{name: "newest status decides", change: func(e *ReadinessEvidence) {
			e.Statuses = []CommitStatusEvidence{
				{Context: "warden/gate", State: "success", UpdatedAt: "2026-11-21T10:00:00Z"},
				{Context: "warden/gate", State: "pending", UpdatedAt: "2026-11-21T09:00:00Z"},
			}
		}, want: ReadinessPass},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			evidence := readyEvidence()
			tt.change(&evidence)
			got := EvaluateReadiness(evidence)
			if got.Verdict != tt.want {
				t.Fatalf("verdict = %q, want %q; reasons: %+v", got.Verdict, tt.want, got.Reasons)
			}
			if tt.wantMessage != "" && !hasReasonContaining(got.Reasons, tt.wantMessage) {
				t.Fatalf("no reason contains %q; reasons: %+v", tt.wantMessage, got.Reasons)
			}
		})
	}
}

func TestEvaluateReadinessReportsTheHeadAndEveryRequirement(t *testing.T) {
	got := EvaluateReadiness(readyEvidence())
	if got.HeadSHA != readyHead {
		t.Fatalf("HeadSHA = %q, want %q", got.HeadSHA, readyHead)
	}
	for _, subject := range []string{"branch", "qodo", "check:Go checks", "check:warden/gate"} {
		found := false
		for _, reason := range got.Reasons {
			if reason.Subject == subject {
				found = true
				if reason.Outcome != ReadinessPass {
					t.Fatalf("reason %q outcome = %q, want pass", subject, reason.Outcome)
				}
			}
		}
		if !found {
			t.Fatalf("no reason for %q; reasons: %+v", subject, got.Reasons)
		}
	}
}

func TestEvaluateReadinessLinksEachReasonToItsSource(t *testing.T) {
	got := EvaluateReadiness(readyEvidence())
	want := map[string]string{
		"branch":          "https://github.com/acme/app/compare/main..." + readyHead,
		"qodo":            "https://github.com/acme/app/pull/7#issuecomment-1",
		"check:Go checks": "https://github.com/acme/app/runs/10",
	}
	for _, reason := range got.Reasons {
		if url, ok := want[reason.Subject]; ok && reason.SourceURL != url {
			t.Fatalf("reason %q SourceURL = %q, want %q", reason.Subject, reason.SourceURL, url)
		}
	}
}

func hasReasonContaining(reasons []ReadinessReason, text string) bool {
	for _, reason := range reasons {
		if strings.Contains(reason.Message, text) {
			return true
		}
	}
	return false
}
