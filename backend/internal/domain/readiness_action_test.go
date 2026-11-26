package domain

import "testing"

// The next action names what the head is waiting for, taken from the first failed
// requirement or, when none failed, the first one with missing evidence.
func TestEvaluateReadinessNamesTheNextAction(t *testing.T) {
	tests := []struct {
		name   string
		change func(*ReadinessEvidence)
		want   string
	}{
		{name: "ready", change: func(*ReadinessEvidence) {}, want: "ready: the engineer decides whether to merge"},
		{name: "merged", change: func(e *ReadinessEvidence) { e.Open, e.Merged = false, true }, want: "nothing to merge: already merged"},
		{name: "closed", change: func(e *ReadinessEvidence) { e.Open = false }, want: "nothing to merge: closed"},
		{name: "no head", change: func(e *ReadinessEvidence) { e.HeadSHA = "abc" }, want: "re-check: GitHub named no head commit"},
		{name: "draft", change: func(e *ReadinessEvidence) { e.Draft = true }, want: "mark ready for review"},
		{name: "other base", change: func(e *ReadinessEvidence) { e.BaseRef = "release" }, want: "retarget to main"},
		{name: "behind", change: func(e *ReadinessEvidence) { e.BehindBy = 3 }, want: "update branch: 3 behind main"},
		{name: "behind unknown", change: func(e *ReadinessEvidence) { e.BehindBy = BehindUnknown }, want: "re-check the branch position"},
		{name: "reviews unreadable", change: func(e *ReadinessEvidence) {
			e.Review = nil
			e.ReviewUnavailableReason = "Qodo review comments could not be loaded from GitHub."
		}, want: "read Qodo's comments on GitHub"},
		{name: "no review", change: func(e *ReadinessEvidence) { e.Review = nil }, want: "request a Qodo review of abcdef1"},
		{name: "review names no commit", change: func(e *ReadinessEvidence) { e.Review.CommitSHA = "" }, want: "waiting on Qodo review of abcdef1"},
		{name: "review of an older head", change: func(e *ReadinessEvidence) { e.Review.CommitSHA = olderHead }, want: "waiting on Qodo review of abcdef1"},
		{name: "summary only", change: func(e *ReadinessEvidence) { e.Review.CommitSHA = olderHead; e.SummaryNamesHead = true }, want: "waiting on Qodo review of abcdef1"},
		{name: "counts unreadable", change: func(e *ReadinessEvidence) { e.Review.CountsReadable = false }, want: "read Qodo's review of abcdef1 on GitHub"},
		{name: "qodo findings", change: func(e *ReadinessEvidence) { e.Review.High = 1; e.Review.Medium = 1 }, want: "fix 2 High or Medium Qodo findings"},
		{name: "one qodo finding", change: func(e *ReadinessEvidence) { e.Review.Medium = 1 }, want: "fix 1 High or Medium Qodo finding"},
		{name: "required checks unreadable", change: func(e *ReadinessEvidence) { e.RequiredChecksKnown = false }, want: "re-check: required checks unreadable"},
		{name: "check evidence unreadable", change: func(e *ReadinessEvidence) { e.CheckEvidenceUnavailableReason = "x" }, want: "re-check the head's checks"},
		{name: "nothing required", change: func(e *ReadinessEvidence) { e.RequiredChecks = nil }, want: "require status checks on main"},
		{name: "check missing", change: func(e *ReadinessEvidence) { e.CheckRuns = nil }, want: "Go checks missing on head"},
		{name: "check running", change: func(e *ReadinessEvidence) { e.CheckRuns[0].Status = "in_progress"; e.CheckRuns[0].Conclusion = "" }, want: "wait for Go checks"},
		{name: "check failed", change: func(e *ReadinessEvidence) { e.CheckRuns[0].Conclusion = "failure" }, want: "fix Go checks (failure)"},
		{name: "earlier run failed", change: func(e *ReadinessEvidence) {
			e.CheckRuns = append([]CheckRunEvidence{{ID: 5, Name: "Go checks", AppID: actionsID, Status: "completed", Conclusion: "failure"}}, e.CheckRuns...)
		}, want: "fix Go checks (an earlier run on this head concluded failure)"},
		{name: "earlier run cancelled", change: func(e *ReadinessEvidence) {
			e.CheckRuns = append([]CheckRunEvidence{{ID: 5, Name: "Go checks", AppID: actionsID, Status: "completed", Conclusion: "cancelled"}}, e.CheckRuns...)
		}, want: "decide on the earlier run of Go checks (cancelled)"},
		{name: "several reruns", change: func(e *ReadinessEvidence) {
			e.CheckRuns = append([]CheckRunEvidence{
				{ID: 4, Name: "Go checks", AppID: actionsID, Status: "completed", Conclusion: "startup_failure"},
				{ID: 5, Name: "Go checks", AppID: actionsID, Status: "completed", Conclusion: "startup_failure"},
			}, e.CheckRuns...)
		}, want: "decide on the reruns of Go checks"},
		{name: "the latest run never started", change: func(e *ReadinessEvidence) { e.CheckRuns[0].Conclusion = "startup_failure" }, want: "rerun Go checks: the job never started"},
		{name: "the rerun never started either", change: func(e *ReadinessEvidence) {
			e.CheckRuns[0].Conclusion = "startup_failure"
			e.CheckRuns = append([]CheckRunEvidence{{ID: 5, Name: "Go checks", AppID: actionsID, Status: "completed", Conclusion: "startup_failure"}}, e.CheckRuns...)
		}, want: "decide on the reruns of Go checks"},
		{name: "an earlier failure outlasts a rerun that never started", change: func(e *ReadinessEvidence) {
			e.CheckRuns[0].Conclusion = "startup_failure"
			e.CheckRuns = append([]CheckRunEvidence{{ID: 5, Name: "Go checks", AppID: actionsID, Status: "completed", Conclusion: "failure"}}, e.CheckRuns...)
		}, want: "fix Go checks (an earlier run on this head concluded failure)"},
		{name: "the latest run was cancelled", change: func(e *ReadinessEvidence) { e.CheckRuns[0].Conclusion = "cancelled" }, want: "decide on the latest run of Go checks (cancelled)"},
		{name: "the latest run timed out", change: func(e *ReadinessEvidence) { e.CheckRuns[0].Conclusion = "timed_out" }, want: "decide on the latest run of Go checks (timed_out)"},
		{name: "an earlier cancelled run before a run that never started", change: func(e *ReadinessEvidence) {
			e.CheckRuns[0].Conclusion = "startup_failure"
			e.CheckRuns = append([]CheckRunEvidence{{ID: 5, Name: "Go checks", AppID: actionsID, Status: "completed", Conclusion: "cancelled"}}, e.CheckRuns...)
		}, want: "decide on the earlier run of Go checks (cancelled)"},
		{name: "a running rerun does not hide an earlier failure", change: func(e *ReadinessEvidence) {
			e.CheckRuns = append(e.CheckRuns, CheckRunEvidence{ID: 90, Name: "Go checks", AppID: actionsID, Status: "in_progress"})
			e.CheckRuns[0].Conclusion = "failure"
		}, want: "fix Go checks (an earlier run on this head concluded failure)"},
		{name: "a completed run without a conclusion", change: func(e *ReadinessEvidence) { e.CheckRuns[0].Conclusion = "" }, want: "decide on the latest run of Go checks (no conclusion)"},
		{name: "a running rerun after a cancelled run", change: func(e *ReadinessEvidence) {
			e.CheckRuns[0].Conclusion = "cancelled"
			e.CheckRuns = append(e.CheckRuns, CheckRunEvidence{ID: 90, Name: "Go checks", AppID: actionsID, Status: "in_progress"})
		}, want: "decide on the earlier run of Go checks (cancelled)"},
		{name: "a running run after two that never started", change: func(e *ReadinessEvidence) {
			e.CheckRuns[0].Conclusion = "startup_failure"
			e.CheckRuns = append(e.CheckRuns,
				CheckRunEvidence{ID: 20, Name: "Go checks", AppID: actionsID, Status: "completed", Conclusion: "startup_failure"},
				CheckRunEvidence{ID: 90, Name: "Go checks", AppID: actionsID, Status: "in_progress"})
		}, want: "decide on the reruns of Go checks"},
		{name: "the one allowed rerun is running", change: func(e *ReadinessEvidence) {
			e.CheckRuns[0].Conclusion = "startup_failure"
			e.CheckRuns = append(e.CheckRuns, CheckRunEvidence{ID: 90, Name: "Go checks", AppID: actionsID, Status: "queued"})
		}, want: "wait for Go checks"},
		{name: "a missing conclusion does not hide an earlier failure", change: func(e *ReadinessEvidence) {
			e.CheckRuns[0].Conclusion = ""
			e.CheckRuns = append([]CheckRunEvidence{{ID: 5, Name: "Go checks", AppID: actionsID, Status: "completed", Conclusion: "failure"}}, e.CheckRuns...)
		}, want: "fix Go checks (an earlier run on this head concluded failure)"},
		{name: "an earlier run without a conclusion before a passing run", change: func(e *ReadinessEvidence) {
			e.CheckRuns = append([]CheckRunEvidence{{ID: 5, Name: "Go checks", AppID: actionsID, Status: "completed"}}, e.CheckRuns...)
		}, want: "decide on the earlier run of Go checks (no conclusion)"},
		{name: "an earlier run without a conclusion before a running one", change: func(e *ReadinessEvidence) {
			e.CheckRuns[0].Conclusion = ""
			e.CheckRuns = append(e.CheckRuns, CheckRunEvidence{ID: 90, Name: "Go checks", AppID: actionsID, Status: "in_progress"})
		}, want: "decide on the earlier run of Go checks (no conclusion)"},
		{name: "status pending", change: func(e *ReadinessEvidence) { e.Statuses[0].State = "pending" }, want: "wait for warden/gate"},
		{name: "status failed", change: func(e *ReadinessEvidence) { e.Statuses[0].State = "error" }, want: "fix warden/gate (error)"},
		{name: "a failure outranks an earlier gap", change: func(e *ReadinessEvidence) { e.BehindBy = 2; e.Review.High = 1 }, want: "fix 1 High or Medium Qodo finding"},
		{name: "the first gap leads", change: func(e *ReadinessEvidence) { e.BehindBy = 2; e.Review = nil }, want: "update branch: 2 behind main"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			evidence := readyEvidence()
			tt.change(&evidence)
			got := EvaluateReadiness(evidence)
			if got.NextAction != tt.want {
				t.Fatalf("next action = %q, want %q; reasons: %+v", got.NextAction, tt.want, got.Reasons)
			}
		})
	}
}

// Every requirement that did not pass says what would move it; a passed one does not.
func TestEveryUnmetReasonHasAnAction(t *testing.T) {
	evidence := readyEvidence()
	evidence.BehindBy, evidence.Review, evidence.CheckRuns = 1, nil, nil
	for _, reason := range EvaluateReadiness(evidence).Reasons {
		if (reason.Outcome == ReadinessPass) != (reason.Action == "") {
			t.Fatalf("reason %+v: a passed reason has no action, every other reason has one", reason)
		}
	}
}

// The verdict, source and action for a required check agree: a reported result fails it
// and links that run; a run without a result leaves it incomplete.
func TestRequiredCheckOutcomeMatchesItsAction(t *testing.T) {
	failed := CheckRunEvidence{ID: 5, Name: "Go checks", AppID: actionsID, Status: "completed", Conclusion: "failure", URL: "https://github.com/acme/app/runs/5"}
	tests := []struct {
		name    string
		runs    []CheckRunEvidence
		want    ReadinessOutcome
		wantURL string
	}{
		{name: "latest never started", runs: []CheckRunEvidence{{ID: 10, Name: "Go checks", AppID: actionsID, Status: "completed", Conclusion: "startup_failure", URL: "https://github.com/acme/app/runs/10"}}, want: ReadinessIncomplete, wantURL: "https://github.com/acme/app/runs/10"},
		{name: "latest cancelled", runs: []CheckRunEvidence{{ID: 10, Name: "Go checks", AppID: actionsID, Status: "completed", Conclusion: "cancelled", URL: "https://github.com/acme/app/runs/10"}}, want: ReadinessIncomplete, wantURL: "https://github.com/acme/app/runs/10"},
		{name: "latest without a conclusion", runs: []CheckRunEvidence{{ID: 10, Name: "Go checks", AppID: actionsID, Status: "completed", URL: "https://github.com/acme/app/runs/10"}}, want: ReadinessIncomplete, wantURL: "https://github.com/acme/app/runs/10"},
		{name: "a rerun is running after a failure", runs: []CheckRunEvidence{failed, {ID: 10, Name: "Go checks", AppID: actionsID, Status: "in_progress", URL: "https://github.com/acme/app/runs/10"}}, want: ReadinessFail, wantURL: failed.URL},
		{name: "a rerun never started after a failure", runs: []CheckRunEvidence{failed, {ID: 10, Name: "Go checks", AppID: actionsID, Status: "completed", Conclusion: "startup_failure", URL: "https://github.com/acme/app/runs/10"}}, want: ReadinessFail, wantURL: failed.URL},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			evidence := readyEvidence()
			evidence.CheckRuns = tt.runs
			got := EvaluateReadiness(evidence)
			for _, reason := range got.Reasons {
				if reason.Subject != "check:Go checks" {
					continue
				}
				if reason.Outcome != tt.want || reason.SourceURL != tt.wantURL || reason.Action == "" {
					t.Fatalf("reason = %+v, want %s linking %s with an action", reason, tt.want, tt.wantURL)
				}
				return
			}
			t.Fatalf("no Go checks reason in %+v", got.Reasons)
		})
	}
}
