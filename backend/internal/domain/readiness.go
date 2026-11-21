package domain

import (
	"fmt"
	"regexp"
	"sort"
)

// ReadinessOutcome is the verdict for one requirement or for the whole pull request.
type ReadinessOutcome string

const (
	ReadinessPass       ReadinessOutcome = "pass"
	ReadinessFail       ReadinessOutcome = "fail"
	ReadinessIncomplete ReadinessOutcome = "incomplete"
)

var readinessFullSHA = regexp.MustCompile(`^[0-9a-f]{40}$`)

// BehindUnknown marks a head whose distance from the default branch could not be read.
const BehindUnknown = -1

// QodoReviewEvidence is the latest Qodo code review on the pull request. CommitSHA is
// the reviewed commit named by the review's marker for this repository, or empty.
type QodoReviewEvidence struct {
	CommitSHA      string
	URL            string
	High           int
	Medium         int
	Low            int
	CountsReadable bool
}

// RequiredCheck is one check branch protection requires. AppID 0 means any producer,
// including a commit status; otherwise only a check run from that GitHub App counts.
type RequiredCheck struct {
	Context string
	AppID   int64
}

// CheckRunEvidence is one check run on the head. GitHub assigns increasing IDs, so the
// highest ID is the latest run, including a rerun that has not started yet.
type CheckRunEvidence struct {
	ID         int64
	Name       string
	AppID      int64
	Status     string
	Conclusion string
	URL        string
}

// CommitStatusEvidence is one commit status on the head.
type CommitStatusEvidence struct {
	Context   string
	State     string
	UpdatedAt string
	URL       string
}

// ReadinessEvidence is everything the verdict may use. Every field describes the
// pull request's current head; callers must not mix in evidence about other commits.
type ReadinessEvidence struct {
	HeadSHA       string
	Open          bool
	Draft         bool
	BaseRef       string
	DefaultBranch string
	BehindBy      int
	CompareURL    string
	Review        *QodoReviewEvidence
	// ReviewUnavailableReason is set when Qodo's comments could not be read at all,
	// which is different from a pull request that has no Qodo review.
	ReviewUnavailableReason string
	SummaryNamesHead        bool
	RequiredChecksKnown     bool
	// CheckEvidenceUnavailableReason is set when the head's check runs or statuses
	// could not be read, or GitHub returned them for another commit.
	CheckEvidenceUnavailableReason string
	RequiredChecks                 []RequiredCheck
	CheckRuns                      []CheckRunEvidence
	Statuses                       []CommitStatusEvidence
}

// ReadinessReason explains one requirement. Subject identifies it: "pull-request",
// "branch", "qodo", "required-checks" or "check:<context>".
type ReadinessReason struct {
	Subject   string
	Outcome   ReadinessOutcome
	Message   string
	SourceURL string
}

// ReadinessFindings are Qodo's finding counts for the head, when they could be read.
type ReadinessFindings struct {
	High   int
	Medium int
	Low    int
}

// Readiness is the verdict for the head. It is PASS only when every requirement
// passed, FAIL when any requirement failed, and INCOMPLETE otherwise.
type Readiness struct {
	Verdict  ReadinessOutcome
	HeadSHA  string
	Reasons  []ReadinessReason
	Findings *ReadinessFindings
}

// EvaluateReadiness decides whether the pull request's current head is ready to merge,
// using only evidence about that head. A missing or unreadable source never counts
// as passing.
func EvaluateReadiness(e ReadinessEvidence) Readiness {
	result := Readiness{HeadSHA: e.HeadSHA, Reasons: []ReadinessReason{}}
	add := func(subject string, outcome ReadinessOutcome, sourceURL, format string, args ...any) {
		result.Reasons = append(result.Reasons, ReadinessReason{Subject: subject, Outcome: outcome, Message: fmt.Sprintf(format, args...), SourceURL: sourceURL})
	}

	if !readinessFullSHA.MatchString(e.HeadSHA) {
		add("pull-request", ReadinessIncomplete, "", "GitHub did not identify the pull request's head commit, so no evidence can be tied to it.")
		result.Verdict = ReadinessIncomplete
		return result
	}
	if !e.Open {
		add("pull-request", ReadinessIncomplete, "", "The pull request is not open.")
	}
	if e.Draft {
		add("pull-request", ReadinessIncomplete, "", "The pull request is a draft; Qodo does not review drafts.")
	}

	evaluateBranch(e, add)
	result.Findings = evaluateQodoReview(e, add)
	evaluateRequiredChecks(e, add)

	result.Verdict = ReadinessPass
	for _, reason := range result.Reasons {
		switch reason.Outcome {
		case ReadinessFail:
			result.Verdict = ReadinessFail
		case ReadinessIncomplete:
			if result.Verdict == ReadinessPass {
				result.Verdict = ReadinessIncomplete
			}
		}
	}
	return result
}

type addReason func(subject string, outcome ReadinessOutcome, sourceURL, format string, args ...any)

func evaluateBranch(e ReadinessEvidence, add addReason) {
	if e.BaseRef != e.DefaultBranch {
		add("branch", ReadinessIncomplete, "", "The pull request targets %s, not %s, the repository's default branch.", e.BaseRef, e.DefaultBranch)
		return
	}
	if e.BehindBy < 0 {
		add("branch", ReadinessIncomplete, "", "Ghostline could not compare the head with %s.", e.DefaultBranch)
		return
	}
	if e.BehindBy > 0 {
		add("branch", ReadinessIncomplete, e.CompareURL, "The branch is behind %s by %d %s. Updating it creates a new head that needs fresh evidence.", e.DefaultBranch, e.BehindBy, plural(e.BehindBy, "commit", "commits"))
		return
	}
	add("branch", ReadinessPass, e.CompareURL, "The branch is up to date with %s.", e.DefaultBranch)
}

func evaluateQodoReview(e ReadinessEvidence, add addReason) *ReadinessFindings {
	review := e.Review
	switch {
	case review == nil && e.ReviewUnavailableReason != "":
		add("qodo", ReadinessIncomplete, "", "%s", e.ReviewUnavailableReason)
		return nil
	case review == nil:
		add("qodo", ReadinessIncomplete, "", "No Qodo code review was found on this pull request.")
		return nil
	case review.CommitSHA == "":
		add("qodo", ReadinessIncomplete, review.URL, "The latest Qodo code review names no commit of this repository.")
		return nil
	case review.CommitSHA != e.HeadSHA && e.SummaryNamesHead:
		add("qodo", ReadinessIncomplete, review.URL, "The latest Qodo code review covers %s, not the head; only Qodo's PR summary names the head. A branch update that only merges the default branch can cause this.", review.CommitSHA[:7])
		return nil
	case review.CommitSHA != e.HeadSHA:
		add("qodo", ReadinessIncomplete, review.URL, "The latest Qodo code review covers %s, not the head.", review.CommitSHA[:7])
		return nil
	case !review.CountsReadable:
		add("qodo", ReadinessIncomplete, review.URL, "Ghostline could not read the finding counts in Qodo's review of the head; read it on GitHub.")
		return nil
	}
	findings := &ReadinessFindings{High: review.High, Medium: review.Medium, Low: review.Low}
	if blocking := review.High + review.Medium; blocking > 0 {
		add("qodo", ReadinessFail, review.URL, "Qodo reports %d High or Medium %s on the head.", blocking, plural(blocking, "finding", "findings"))
		return findings
	}
	if review.Low > 0 {
		add("qodo", ReadinessPass, review.URL, "Qodo's review covers the head with no High or Medium findings; %d Low %s to report to the engineer.", review.Low, plural(review.Low, "finding", "findings"))
		return findings
	}
	add("qodo", ReadinessPass, review.URL, "Qodo's review covers the head with no open findings.")
	return findings
}

func evaluateRequiredChecks(e ReadinessEvidence, add addReason) {
	if !e.RequiredChecksKnown {
		add("required-checks", ReadinessIncomplete, "", "Ghostline could not read which checks %s requires.", e.DefaultBranch)
		return
	}
	if e.CheckEvidenceUnavailableReason != "" {
		add("required-checks", ReadinessIncomplete, "", "%s", e.CheckEvidenceUnavailableReason)
		return
	}
	if len(e.RequiredChecks) == 0 {
		add("required-checks", ReadinessIncomplete, "", "%s requires no status checks, so Ghostline cannot tell which checks must pass.", e.DefaultBranch)
		return
	}
	for _, required := range e.RequiredChecks {
		evaluateRequiredCheck(required, e, add)
	}
}

func evaluateRequiredCheck(required RequiredCheck, e ReadinessEvidence, add addReason) {
	subject := "check:" + required.Context
	runs := make([]CheckRunEvidence, 0)
	for _, run := range e.CheckRuns {
		if run.Name == required.Context && (required.AppID == 0 || run.AppID == required.AppID) {
			runs = append(runs, run)
		}
	}
	sort.SliceStable(runs, func(i, j int) bool { return runs[i].ID < runs[j].ID })

	if len(runs) == 0 {
		// A commit status carries no app, so it can only satisfy a requirement that names none.
		if required.AppID == 0 {
			if status, ok := latestStatus(e.Statuses, required.Context); ok {
				add(subject, statusOutcome(status.State), status.URL, "%s: %s.", required.Context, status.State)
				return
			}
		}
		add(subject, ReadinessIncomplete, "", "%s: missing on the head.", required.Context)
		return
	}

	latest := runs[len(runs)-1]
	if latest.Status != "completed" {
		add(subject, ReadinessIncomplete, latest.URL, "%s: %s.", required.Context, latest.Status)
		return
	}
	if !passingConclusion(latest.Conclusion) {
		add(subject, ReadinessFail, latest.URL, "%s: %s.", required.Context, latest.Conclusion)
		return
	}
	// docs/factory-verification.md: a run that reported a result stays a real result even
	// if a rerun on the same head passed. Only a run that ended without a result, because
	// of the runner or the network, may be rerun, and only once. GitHub records that cause
	// only for a job that never started; any other no-result run needs the engineer.
	var noResult []CheckRunEvidence
	unknownCause := ""
	for _, earlier := range runs[:len(runs)-1] {
		switch {
		case earlier.Status != "completed" || passingConclusion(earlier.Conclusion):
		case noResultConclusion(earlier.Conclusion):
			noResult = append(noResult, earlier)
		case unknownCauseConclusion(earlier.Conclusion):
			if unknownCause == "" {
				unknownCause = earlier.Conclusion
			}
		default:
			add(subject, ReadinessFail, earlier.URL, "%s: an earlier run on this head concluded %s; a passing rerun does not clear it.", required.Context, earlier.Conclusion)
			return
		}
	}
	switch {
	case unknownCause != "":
		add(subject, ReadinessIncomplete, latest.URL, "%s: an earlier run on this head ended %s; GitHub does not show whether the runner or the code caused it, so the engineer decides.", required.Context, unknownCause)
	case len(noResult) > 1:
		add(subject, ReadinessIncomplete, latest.URL, "%s: %d earlier runs on this head ended with no result; only one rerun is allowed, so the engineer decides.", required.Context, len(noResult))
	case len(noResult) == 1:
		add(subject, ReadinessPass, latest.URL, "%s: %s on a rerun; an earlier run ended %s with no result.", required.Context, latest.Conclusion, noResult[0].Conclusion)
	default:
		add(subject, ReadinessPass, latest.URL, "%s: %s.", required.Context, latest.Conclusion)
	}
}

// noResultConclusion reports a run that provably produced no result: the job never
// started, so the checked code cannot have run.
func noResultConclusion(conclusion string) bool {
	return conclusion == "startup_failure"
}

// unknownCauseConclusion reports a run that ended without a result for a reason GitHub
// does not record: a person may have cancelled a hang, or the code under test may have
// timed out or stopped reporting, as well as the runner or the network.
func unknownCauseConclusion(conclusion string) bool {
	switch conclusion {
	case "cancelled", "timed_out", "stale":
		return true
	}
	return false
}

func latestStatus(statuses []CommitStatusEvidence, context string) (CommitStatusEvidence, bool) {
	var latest CommitStatusEvidence
	found := false
	for _, status := range statuses {
		if status.Context == context && (!found || status.UpdatedAt > latest.UpdatedAt) {
			latest, found = status, true
		}
	}
	return latest, found
}

func statusOutcome(state string) ReadinessOutcome {
	switch state {
	case "success":
		return ReadinessPass
	case "pending":
		return ReadinessIncomplete
	default:
		return ReadinessFail
	}
}

func passingConclusion(conclusion string) bool {
	return conclusion == "success" || conclusion == "skipped" || conclusion == "neutral"
}

func plural(count int, one, many string) string {
	if count == 1 {
		return one
	}
	return many
}
