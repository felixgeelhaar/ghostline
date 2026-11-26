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
		{name: "earlier action_required run still counts as a result", change: func(e *ReadinessEvidence) {
			e.CheckRuns = append([]CheckRunEvidence{{ID: 5, Name: "Go checks", AppID: actionsID, Status: "completed", Conclusion: "action_required"}}, e.CheckRuns...)
		}, want: ReadinessFail, wantMessage: "an earlier run on this head concluded action_required"},
		{name: "a cancelled run may have been a hang in the code", change: func(e *ReadinessEvidence) {
			e.CheckRuns = append([]CheckRunEvidence{{ID: 5, Name: "Go checks", AppID: actionsID, Status: "completed", Conclusion: "cancelled"}}, e.CheckRuns...)
		}, want: ReadinessIncomplete, wantMessage: "Go checks: an earlier run on this head ended cancelled; GitHub does not show whether the runner or the code caused it, so the engineer decides."},
		{name: "a stale run's cause is unknown", change: func(e *ReadinessEvidence) {
			e.CheckRuns = append([]CheckRunEvidence{{ID: 5, Name: "Go checks", AppID: actionsID, Status: "completed", Conclusion: "stale"}}, e.CheckRuns...)
		}, want: ReadinessIncomplete, wantMessage: "Go checks: an earlier run on this head ended stale; GitHub does not show whether the runner or the code caused it, so the engineer decides."},
		{name: "a timed out run may be a hang in the code", change: func(e *ReadinessEvidence) {
			e.CheckRuns = append([]CheckRunEvidence{{ID: 5, Name: "Go checks", AppID: actionsID, Status: "completed", Conclusion: "timed_out"}}, e.CheckRuns...)
		}, want: ReadinessIncomplete, wantMessage: "Go checks: an earlier run on this head ended timed_out; GitHub does not show whether the runner or the code caused it, so the engineer decides."},
		{name: "a reported failure outranks a timeout", change: func(e *ReadinessEvidence) {
			e.CheckRuns = append([]CheckRunEvidence{
				{ID: 4, Name: "Go checks", AppID: actionsID, Status: "completed", Conclusion: "timed_out"},
				{ID: 5, Name: "Go checks", AppID: actionsID, Status: "completed", Conclusion: "failure"},
			}, e.CheckRuns...)
		}, want: ReadinessFail, wantMessage: "an earlier run on this head concluded failure"},
		{name: "one rerun after a startup failure", change: func(e *ReadinessEvidence) {
			e.CheckRuns = append([]CheckRunEvidence{{ID: 5, Name: "Go checks", AppID: actionsID, Status: "completed", Conclusion: "startup_failure"}}, e.CheckRuns...)
		}, want: ReadinessPass, wantMessage: "Go checks: success on a rerun; an earlier run ended startup_failure with no result."},
		{name: "a reported failure is not cleared by a later no-result run and pass", change: func(e *ReadinessEvidence) {
			e.CheckRuns = append([]CheckRunEvidence{
				{ID: 4, Name: "Go checks", AppID: actionsID, Status: "completed", Conclusion: "failure"},
				{ID: 5, Name: "Go checks", AppID: actionsID, Status: "completed", Conclusion: "cancelled"},
			}, e.CheckRuns...)
		}, want: ReadinessFail, wantMessage: "an earlier run on this head concluded failure"},
		{name: "more than one rerun needs the engineer", change: func(e *ReadinessEvidence) {
			e.CheckRuns = append([]CheckRunEvidence{
				{ID: 4, Name: "Go checks", AppID: actionsID, Status: "completed", Conclusion: "startup_failure"},
				{ID: 5, Name: "Go checks", AppID: actionsID, Status: "completed", Conclusion: "startup_failure"},
			}, e.CheckRuns...)
		}, want: ReadinessIncomplete, wantMessage: "Go checks: 2 earlier runs on this head ended with no result; only one rerun is allowed, so the engineer decides."},
		{name: "queued rerun is the latest run", change: func(e *ReadinessEvidence) {
			e.CheckRuns = append(e.CheckRuns, CheckRunEvidence{ID: 90, Name: "Go checks", AppID: actionsID, Status: "queued"})
		}, want: ReadinessIncomplete, wantMessage: "Go checks: queued"},
		{name: "same name from another app does not count", change: func(e *ReadinessEvidence) { e.CheckRuns[0].AppID = 999 }, want: ReadinessIncomplete, wantMessage: "Go checks: missing on the head"},
		{name: "a status cannot satisfy an app-bound check", change: func(e *ReadinessEvidence) {
			e.CheckRuns = nil
			e.Statuses = append(e.Statuses, CommitStatusEvidence{Context: "Go checks", State: "success"})
		}, want: ReadinessIncomplete, wantMessage: "Go checks: missing on the head"},
		{name: "an earlier run without a conclusion is not a reported failure", change: func(e *ReadinessEvidence) {
			e.CheckRuns = append([]CheckRunEvidence{{ID: 5, Name: "Go checks", AppID: actionsID, Status: "completed"}}, e.CheckRuns...)
		}, want: ReadinessIncomplete, wantMessage: "Go checks: an earlier run on this head ended without a conclusion; GitHub does not show whether the runner or the code caused it, so the engineer decides."},
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

// A merged or closed pull request has nothing left to merge, so its readiness is not
// applicable, whatever its evidence says. One reason says why; no other requirement
// is reported, because none of them can make the pull request mergeable.
func TestEvaluateReadinessDoesNotApplyToAPullRequestThatIsNotOpen(t *testing.T) {
	tests := []struct {
		name        string
		change      func(*ReadinessEvidence)
		wantMessage string
	}{
		{name: "merged", change: func(e *ReadinessEvidence) { e.Open, e.Merged = false, true }, wantMessage: "The pull request is already merged. Merge readiness applies only to an open pull request."},
		{name: "closed without merging", change: func(e *ReadinessEvidence) { e.Open = false }, wantMessage: "The pull request was closed without being merged. Merge readiness applies only to an open pull request."},
		{name: "merged with stale evidence", change: func(e *ReadinessEvidence) {
			e.Open, e.Merged, e.BehindBy, e.Review = false, true, 3, nil
		}, wantMessage: "already merged"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			evidence := readyEvidence()
			tt.change(&evidence)
			got := EvaluateReadiness(evidence)
			if got.Verdict != ReadinessNotApplicable {
				t.Fatalf("verdict = %q, want %q; reasons: %+v", got.Verdict, ReadinessNotApplicable, got.Reasons)
			}
			if len(got.Reasons) != 1 || got.Reasons[0].Subject != "pull-request" || got.Reasons[0].Outcome != ReadinessNotApplicable || !strings.Contains(got.Reasons[0].Message, tt.wantMessage) {
				t.Fatalf("reasons = %+v, want one pull-request reason containing %q", got.Reasons, tt.wantMessage)
			}
			if got.HeadSHA != readyHead || got.Findings != nil {
				t.Fatalf("head = %q, findings = %+v; want the head and no findings", got.HeadSHA, got.Findings)
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

// A reason the engineer must decide links the run they need to inspect, not the passing rerun.
func TestEvaluateReadinessLinksTheEarlierRunTheEngineerDecides(t *testing.T) {
	tests := []struct {
		name    string
		earlier []CheckRunEvidence
		want    string
	}{
		{name: "cancelled", earlier: []CheckRunEvidence{{ID: 5, Conclusion: "cancelled", URL: "https://github.com/acme/app/runs/5"}}, want: "https://github.com/acme/app/runs/5"},
		{name: "timed out after a cancel", earlier: []CheckRunEvidence{
			{ID: 4, Conclusion: "cancelled", URL: "https://github.com/acme/app/runs/4"},
			{ID: 5, Conclusion: "timed_out", URL: "https://github.com/acme/app/runs/5"},
		}, want: "https://github.com/acme/app/runs/4"},
		{name: "two runs that never started", earlier: []CheckRunEvidence{
			{ID: 4, Conclusion: "startup_failure", URL: "https://github.com/acme/app/runs/4"},
			{ID: 5, Conclusion: "startup_failure", URL: "https://github.com/acme/app/runs/5"},
		}, want: "https://github.com/acme/app/runs/5"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			evidence := readyEvidence()
			for _, run := range tt.earlier {
				run.Name, run.AppID, run.Status = "Go checks", actionsID, "completed"
				evidence.CheckRuns = append(evidence.CheckRuns, run)
			}
			for _, reason := range EvaluateReadiness(evidence).Reasons {
				if reason.Subject == "check:Go checks" {
					if reason.Outcome != ReadinessIncomplete || reason.SourceURL != tt.want {
						t.Fatalf("reason = %+v, want incomplete linking %s", reason, tt.want)
					}
					return
				}
			}
			t.Fatal("no reason for Go checks")
		})
	}
}
