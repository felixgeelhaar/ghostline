package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"unicode/utf8"
)

const (
	checksAvailable      = "available"
	checksUnavailable    = "unavailable"
	kindCheckRun         = "check_run"
	kindCommitStatus     = "commit_status"
	checksPageSize       = 100
	maxCheckSummaryRunes = 2000
	maxCheckSummaryBytes = 64 * 1024
)

var (
	fullSHA             = regexp.MustCompile(`^[0-9a-f]{40}$`)
	errRevisionMismatch = errors.New("GitHub returned checks for a different revision")
)

// revisionChecks holds only the checks GitHub reports for the requested commit.
// The revision may be the PR head or a focused historical commit.
type revisionChecks struct {
	RevisionSHA      string  `json:"revisionSha"`
	RevisionShortSHA string  `json:"revisionShortSha"`
	RevisionURL      string  `json:"revisionUrl"`
	Availability     string  `json:"availability"`
	Reason           string  `json:"reason,omitempty"`
	Items            []check `json:"items"`
}

type check struct {
	ID                     int64  `json:"id,omitempty"`
	Name                   string `json:"name"`
	Kind                   string `json:"kind"`
	Source                 string `json:"source,omitempty"`
	Status                 string `json:"status"`
	Conclusion             string `json:"conclusion,omitempty"`
	URL                    string `json:"url,omitempty"`
	StartedAt              string `json:"startedAt,omitempty"`
	CompletedAt            string `json:"completedAt,omitempty"`
	CreatedAt              string `json:"createdAt,omitempty"`
	UpdatedAt              string `json:"updatedAt,omitempty"`
	OutputTitle            string `json:"outputTitle,omitempty"`
	OutputSummary          string `json:"outputSummary,omitempty"`
	OutputSummaryTruncated bool   `json:"outputSummaryTruncated,omitempty"`
}

type githubCheckRuns struct {
	TotalCount int `json:"total_count"`
	CheckRuns  []struct {
		ID          int64   `json:"id"`
		Name        string  `json:"name"`
		HeadSHA     string  `json:"head_sha"`
		Status      string  `json:"status"`
		Conclusion  *string `json:"conclusion"`
		HTMLURL     string  `json:"html_url"`
		StartedAt   *string `json:"started_at"`
		CompletedAt *string `json:"completed_at"`
		UpdatedAt   *string `json:"updated_at"`
		App         *struct {
			Name string `json:"name"`
		} `json:"app"`
		Output struct {
			Title   string `json:"title"`
			Summary string `json:"summary"`
		} `json:"output"`
	} `json:"check_runs"`
}

type githubCombinedStatus struct {
	SHA        string `json:"sha"`
	TotalCount int    `json:"total_count"`
	Statuses   []struct {
		ID        int64  `json:"id"`
		Context   string `json:"context"`
		State     string `json:"state"`
		TargetURL string `json:"target_url"`
		CreatedAt string `json:"created_at"`
		UpdatedAt string `json:"updated_at"`
	} `json:"statuses"`
}

func fetchRevisionChecks(ctx context.Context, gh *githubClient, owner, repo, sha string) revisionChecks {
	result := revisionChecks{RevisionSHA: sha, Availability: checksUnavailable, Items: []check{}}
	if !fullSHA.MatchString(sha) {
		result.RevisionSHA = ""
		result.Reason = "GitHub did not identify this revision, so its checks cannot be shown."
		return result
	}
	result.RevisionShortSHA = sha[:7]
	result.RevisionURL = fmt.Sprintf("https://github.com/%s/%s/commit/%s", url.PathEscape(owner), url.PathEscape(repo), sha)
	commitPath := fmt.Sprintf("/repos/%s/%s/commits/%s", url.PathEscape(owner), url.PathEscape(repo), sha)

	runs, err := fetchCheckRuns(ctx, gh, commitPath, sha)
	if err != nil {
		result.Reason = checksUnavailableReason(err)
		return result
	}
	statuses, err := fetchCommitStatuses(ctx, gh, commitPath, sha)
	if err != nil {
		result.Reason = checksUnavailableReason(err)
		return result
	}
	items := make([]check, 0, len(runs)+len(statuses))
	items = append(items, runs...)
	items = append(items, statuses...)
	sort.SliceStable(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	result.Availability = checksAvailable
	result.Items = items
	return result
}

func fetchCheckRuns(ctx context.Context, gh *githubClient, commitPath, sha string) ([]check, error) {
	var items []check
	remainingSummaryBytes := maxCheckSummaryBytes
	for page := 1; ; page++ {
		var batch githubCheckRuns
		if err := gh.get(ctx, fmt.Sprintf("%s/check-runs?filter=latest&per_page=%d&page=%d", commitPath, checksPageSize, page), &batch); err != nil {
			return nil, err
		}
		for _, run := range batch.CheckRuns {
			if run.HeadSHA != sha {
				return nil, errRevisionMismatch
			}
			summary, summaryTruncated, summaryBytes := boundedCheckSummary(run.Output.Summary, remainingSummaryBytes)
			remainingSummaryBytes -= summaryBytes
			item := check{ID: run.ID, Name: run.Name, Kind: kindCheckRun, Status: run.Status, URL: safeURL(run.HTMLURL), StartedAt: stringValue(run.StartedAt), CompletedAt: stringValue(run.CompletedAt), UpdatedAt: stringValue(run.UpdatedAt), OutputTitle: run.Output.Title, OutputSummary: summary, OutputSummaryTruncated: summaryTruncated}
			if run.Conclusion != nil {
				item.Conclusion = *run.Conclusion
			}
			if run.App != nil {
				item.Source = run.App.Name
			}
			items = append(items, item)
		}
		if len(batch.CheckRuns) < checksPageSize || len(items) >= batch.TotalCount {
			return items, nil
		}
	}
}

// boundedCheckSummary limits both one summary and the combined summary data
// returned for a revision. The linked GitHub check remains the source for full output.
func boundedCheckSummary(summary string, remainingBytes int) (string, bool, int) {
	if summary == "" {
		return "", false, 0
	}

	runes := 0
	usedBytes := 0
	for index, r := range summary {
		runeBytes := utf8.RuneLen(r)
		if runes >= maxCheckSummaryRunes || usedBytes+runeBytes > remainingBytes {
			return summary[:index], true, usedBytes
		}
		runes++
		usedBytes += runeBytes
	}
	return summary, false, usedBytes
}

func fetchCommitStatuses(ctx context.Context, gh *githubClient, commitPath, sha string) ([]check, error) {
	var items []check
	for page := 1; ; page++ {
		var batch githubCombinedStatus
		if err := gh.get(ctx, fmt.Sprintf("%s/status?per_page=%d&page=%d", commitPath, checksPageSize, page), &batch); err != nil {
			return nil, err
		}
		if batch.SHA != sha {
			return nil, errRevisionMismatch
		}
		for _, status := range batch.Statuses {
			// Commit statuses report a single state; pending is the only non-final one.
			item := check{ID: status.ID, Name: status.Context, Kind: kindCommitStatus, Status: "pending", URL: safeURL(status.TargetURL), CreatedAt: status.CreatedAt, UpdatedAt: status.UpdatedAt}
			if status.State != "pending" {
				item.Status = "completed"
				item.Conclusion = status.State
			}
			items = append(items, item)
		}
		if len(batch.Statuses) < checksPageSize || len(items) >= batch.TotalCount {
			return items, nil
		}
	}
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// safeURL keeps only http(s) links; commit status target URLs are set by third parties.
func safeURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return ""
	}
	return u.String()
}

func checksUnavailableReason(err error) string {
	var statusErr statusError
	switch {
	case errors.Is(err, errRevisionMismatch):
		return "GitHub returned checks for a different commit than the pull request's head, so they are not shown."
	case errors.Is(err, errRateLimit):
		return "GitHub’s API rate limit was reached before checks could be loaded. Try again later or configure GITHUB_TOKEN."
	case errors.Is(err, errNotFound):
		return "GitHub did not return checks for the head commit."
	case errors.As(err, &statusErr) && statusErr.code == http.StatusForbidden:
		return "GitHub denied access to the head commit's checks."
	default:
		return "Checks could not be loaded from GitHub. Try importing again."
	}
}
