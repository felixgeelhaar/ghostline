package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/felixgeelhaar/ghostline/backend/internal/domain"
)

type readinessReason struct {
	Subject   string `json:"subject"`
	Outcome   string `json:"outcome"`
	Message   string `json:"message"`
	SourceURL string `json:"sourceUrl,omitempty"`
}

type readinessFindings struct {
	High   int `json:"high"`
	Medium int `json:"medium"`
	Low    int `json:"low"`
}

// readinessResponse is the verdict for one pull request's head. Every reason
// describes evidence about HeadSHA and nothing else.
type readinessResponse struct {
	Repository   string             `json:"repository"`
	Number       int                `json:"number"`
	HeadSHA      string             `json:"headSha"`
	HeadShortSHA string             `json:"headShortSha"`
	HeadURL      string             `json:"headUrl"`
	Verdict      string             `json:"verdict"`
	Reasons      []readinessReason  `json:"reasons"`
	Findings     *readinessFindings `json:"findings,omitempty"`
}

type githubReadinessPR struct {
	Number int    `json:"number"`
	State  string `json:"state"`
	Draft  bool   `json:"draft"`
	Head   struct {
		SHA string `json:"sha"`
	} `json:"head"`
	Base struct {
		Ref  string `json:"ref"`
		Repo struct {
			Name  string `json:"name"`
			Owner struct {
				Login string `json:"login"`
			} `json:"owner"`
		} `json:"repo"`
	} `json:"base"`
}

type githubBranchProtection struct {
	Protection *struct {
		RequiredStatusChecks *struct {
			Contexts []string `json:"contexts"`
			Checks   []struct {
				Context string `json:"context"`
				AppID   *int64 `json:"app_id"`
			} `json:"checks"`
		} `json:"required_status_checks"`
	} `json:"protection"`
}

type githubAllCheckRuns struct {
	TotalCount int `json:"total_count"`
	CheckRuns  []struct {
		ID         int64   `json:"id"`
		Name       string  `json:"name"`
		HeadSHA    string  `json:"head_sha"`
		Status     string  `json:"status"`
		Conclusion *string `json:"conclusion"`
		HTMLURL    string  `json:"html_url"`
		App        *struct {
			ID int64 `json:"id"`
		} `json:"app"`
	} `json:"check_runs"`
}

const maxReadinessCheckRunPages = 10

func (a api) loadReadiness(c *gin.Context) {
	var body struct {
		URL string `json:"url"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || strings.TrimSpace(body.URL) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Enter a GitHub pull request URL."})
		return
	}
	owner, repo, number, err := parsePRURL(body.URL)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ctx := c.Request.Context()
	if err := ensurePublicBeforeDataReads(ctx, a.gh, owner, repo); err != nil {
		respondGitHubError(c, err)
		return
	}
	pr, identity, err := fetchReadinessPR(ctx, a.gh, owner, repo, number)
	if err != nil {
		respondGitHubError(c, err)
		return
	}
	if !identity.Equal(domain.RepositoryIdentity{Owner: owner, Name: repo}) {
		if err := ensurePublicBeforeDataReads(ctx, a.gh, identity.Owner, identity.Name); err != nil {
			respondGitHubError(c, err)
			return
		}
	}
	if !fullSHA.MatchString(pr.Head.SHA) {
		c.JSON(http.StatusBadGateway, gin.H{"error": "GitHub did not identify the pull request's head commit, so no readiness evidence can be tied to it."})
		return
	}
	defaultBranch, err := fetchDefaultBranch(ctx, a.gh, identity)
	if err != nil {
		respondGitHubError(c, err)
		return
	}

	evidence := gatherReadinessEvidence(ctx, a.gh, identity, pr, defaultBranch)

	// GitHub requests are separate snapshots. A verdict is only returned when the
	// head did not move while its evidence was being read.
	verified, verifiedIdentity, err := fetchReadinessPR(ctx, a.gh, identity.Owner, identity.Name, pr.Number)
	if err != nil {
		respondGitHubError(c, err)
		return
	}
	verifiedDefault, err := fetchDefaultBranch(ctx, a.gh, identity)
	if err != nil {
		respondGitHubError(c, err)
		return
	}
	if !sameReadinessSnapshot(pr, verified) || !verifiedIdentity.Equal(identity) || verifiedDefault != defaultBranch {
		c.JSON(http.StatusConflict, gin.H{"error": "This pull request changed while its readiness was being checked. Check it again."})
		return
	}

	result := domain.EvaluateReadiness(evidence)
	response := readinessResponse{Repository: identity.FullName(), Number: pr.Number, HeadSHA: result.HeadSHA, Verdict: string(result.Verdict), Reasons: make([]readinessReason, 0, len(result.Reasons))}
	if fullSHA.MatchString(result.HeadSHA) {
		response.HeadShortSHA = result.HeadSHA[:7]
		response.HeadURL = fmt.Sprintf("https://github.com/%s/%s/commit/%s", url.PathEscape(identity.Owner), url.PathEscape(identity.Name), result.HeadSHA)
	}
	for _, reason := range result.Reasons {
		response.Reasons = append(response.Reasons, readinessReason{Subject: reason.Subject, Outcome: string(reason.Outcome), Message: reason.Message, SourceURL: safeURL(reason.SourceURL)})
	}
	if result.Findings != nil {
		response.Findings = &readinessFindings{High: result.Findings.High, Medium: result.Findings.Medium, Low: result.Findings.Low}
	}
	c.JSON(http.StatusOK, response)
}

// sameReadinessSnapshot reports whether every pull-request fact the verdict uses is
// unchanged: the head, whether it is open or a draft, and the branch it targets.
func sameReadinessSnapshot(before, after githubReadinessPR) bool {
	return before.Head.SHA == after.Head.SHA && before.State == after.State && before.Draft == after.Draft && before.Base.Ref == after.Base.Ref
}

var errDefaultBranchUnknown = errors.New("GitHub did not report the repository's default branch")

func fetchDefaultBranch(ctx context.Context, gh *githubClient, repo domain.RepositoryIdentity) (string, error) {
	var repository struct {
		DefaultBranch string `json:"default_branch"`
	}
	if err := gh.get(ctx, fmt.Sprintf("/repos/%s/%s", url.PathEscape(repo.Owner), url.PathEscape(repo.Name)), &repository); err != nil {
		return "", err
	}
	if repository.DefaultBranch == "" {
		return "", errDefaultBranchUnknown
	}
	return repository.DefaultBranch, nil
}

func fetchReadinessPR(ctx context.Context, gh *githubClient, owner, repo string, number int) (githubReadinessPR, domain.RepositoryIdentity, error) {
	var pr githubReadinessPR
	if err := gh.get(ctx, fmt.Sprintf("/repos/%s/%s/pulls/%d", url.PathEscape(owner), url.PathEscape(repo), number), &pr); err != nil {
		return pr, domain.RepositoryIdentity{}, err
	}
	identity, err := domain.NewRepositoryIdentity(pr.Base.Repo.Owner.Login, pr.Base.Repo.Name)
	if err != nil {
		return pr, identity, errRepositoryVisibilityUnknown
	}
	return pr, identity, nil
}

// gatherReadinessEvidence reads every source for the head. A source that cannot be read
// becomes an explicit gap in the evidence, never a passing result.
func gatherReadinessEvidence(ctx context.Context, gh *githubClient, repo domain.RepositoryIdentity, pr githubReadinessPR, defaultBranch string) domain.ReadinessEvidence {
	head := pr.Head.SHA
	repoPath := fmt.Sprintf("/repos/%s/%s", url.PathEscape(repo.Owner), url.PathEscape(repo.Name))
	evidence := domain.ReadinessEvidence{HeadSHA: head, Open: pr.State == "open", Draft: pr.Draft, BaseRef: pr.Base.Ref, DefaultBranch: defaultBranch}
	if !fullSHA.MatchString(head) {
		return evidence
	}

	var comparison struct {
		BehindBy *int   `json:"behind_by"`
		HTMLURL  string `json:"html_url"`
	}
	if err := gh.get(ctx, fmt.Sprintf("%s/compare/%s...%s", repoPath, url.PathEscape(defaultBranch), head), &comparison); err != nil || comparison.BehindBy == nil {
		evidence.BehindBy = domain.BehindUnknown
	} else {
		evidence.BehindBy = *comparison.BehindBy
		evidence.CompareURL = comparison.HTMLURL
	}

	evidence.RequiredChecks, evidence.RequiredChecksKnown = fetchRequiredChecks(ctx, gh, repoPath, defaultBranch)
	runs, err := fetchAllCheckRunsForHead(ctx, gh, repoPath, head)
	if err == nil {
		evidence.CheckRuns = runs
		var statuses []check
		statuses, err = fetchCommitStatuses(ctx, gh, repoPath+"/commits/"+head, head)
		for _, status := range statuses {
			state := status.Conclusion
			if status.Status != "completed" {
				state = "pending"
			}
			evidence.Statuses = append(evidence.Statuses, domain.CommitStatusEvidence{Context: status.Name, State: state, UpdatedAt: status.UpdatedAt, URL: status.URL})
		}
	}
	if err != nil {
		evidence.CheckEvidenceUnavailableReason = checksUnavailableReason(err)
	}

	reviews := fetchQodoReviews(ctx, gh, repo, pr.Number)
	if reviews.Availability != reviewsAvailable {
		evidence.ReviewUnavailableReason = reviews.Reason
		return evidence
	}
	evidence.Review, evidence.SummaryNamesHead = domain.SelectQodoReviewEvidence(qodoReviewCandidates(reviews.Items), head)
	return evidence
}

func fetchRequiredChecks(ctx context.Context, gh *githubClient, repoPath, branch string) ([]domain.RequiredCheck, bool) {
	var branchData githubBranchProtection
	if err := gh.get(ctx, fmt.Sprintf("%s/branches/%s", repoPath, url.PathEscape(branch)), &branchData); err != nil {
		return nil, false
	}
	if branchData.Protection == nil || branchData.Protection.RequiredStatusChecks == nil {
		return nil, true
	}
	statusChecks := branchData.Protection.RequiredStatusChecks
	required := make([]domain.RequiredCheck, 0, len(statusChecks.Contexts))
	if len(statusChecks.Checks) > 0 {
		for _, item := range statusChecks.Checks {
			requirement := domain.RequiredCheck{Context: item.Context}
			if item.AppID != nil {
				requirement.AppID = *item.AppID
			}
			required = append(required, requirement)
		}
		return required, true
	}
	for _, name := range statusChecks.Contexts {
		required = append(required, domain.RequiredCheck{Context: name})
	}
	return required, true
}

// fetchAllCheckRunsForHead reads every run on the head, not only the latest per name,
// so an earlier failure on the same commit stays visible.
func fetchAllCheckRunsForHead(ctx context.Context, gh *githubClient, repoPath, head string) ([]domain.CheckRunEvidence, error) {
	runs := make([]domain.CheckRunEvidence, 0)
	for page := 1; page <= maxReadinessCheckRunPages; page++ {
		var batch githubAllCheckRuns
		if err := gh.get(ctx, fmt.Sprintf("%s/commits/%s/check-runs?filter=all&per_page=%d&page=%d", repoPath, head, checksPageSize, page), &batch); err != nil {
			return nil, err
		}
		for _, run := range batch.CheckRuns {
			if run.HeadSHA != head {
				return nil, errRevisionMismatch
			}
			if run.ID <= 0 {
				return nil, errCheckRunWithoutID
			}
			evidence := domain.CheckRunEvidence{ID: run.ID, Name: run.Name, Status: run.Status, URL: run.HTMLURL}
			if run.Conclusion != nil {
				evidence.Conclusion = *run.Conclusion
			}
			if run.App != nil {
				evidence.AppID = run.App.ID
			}
			runs = append(runs, evidence)
		}
		if len(batch.CheckRuns) < checksPageSize || len(runs) >= batch.TotalCount {
			return runs, nil
		}
	}
	return nil, errTooManyCheckRuns
}

var errTooManyCheckRuns = errors.New("the head has more check runs than Ghostline reads")

// qodoReviewCandidates hands the parsed Qodo comments to the domain readiness rules.
func qodoReviewCandidates(reviews []qodoReview) []domain.QodoReviewCandidate {
	candidates := make([]domain.QodoReviewCandidate, 0, len(reviews))
	for _, review := range reviews {
		counts := review.severityCounts
		candidates = append(candidates, domain.QodoReviewCandidate{CommentID: review.ID, Summary: review.Kind == "summary", CommitSHA: review.CommitSHA, URL: review.URL, UpdatedAt: review.updatedAt, High: counts.High, Medium: counts.Medium, Low: counts.Low, CountsReadable: counts.Readable})
	}
	return candidates
}
