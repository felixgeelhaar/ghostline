package main

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/felixgeelhaar/ghostline/backend/internal/domain"
)

const (
	referencedIssuesAvailable   = "available"
	referencedIssuesUnavailable = "unavailable"
	referencedIssueOpen         = "open"
	referencedIssueClosed       = "closed"
	referencedIssueUnavailable  = "unavailable"
	maxReferencedIssues         = 20
)

type referencedIssue struct {
	Number int    `json:"number"`
	URL    string `json:"url"`
	State  string `json:"state"`
}

type referencedIssueSet struct {
	Availability string            `json:"availability"`
	Items        []referencedIssue `json:"items"`
	Reason       string            `json:"reason,omitempty"`
}

type githubIssue struct {
	State       string    `json:"state"`
	HTMLURL     string    `json:"html_url"`
	PullRequest *struct{} `json:"pull_request"`
}

func fetchReferencedIssues(ctx context.Context, gh *githubClient, owner, repo, body string) referencedIssueSet {
	numbers := domain.ReferencedIssueNumbers(body, owner, repo)
	result := referencedIssueSet{Availability: referencedIssuesAvailable, Items: make([]referencedIssue, 0, len(numbers))}
	if len(numbers) > maxReferencedIssues {
		result.Availability = referencedIssuesUnavailable
		result.Reason = fmt.Sprintf("GitHub could not confirm every reference; only the first %d issue references are checked.", maxReferencedIssues)
		numbers = numbers[:maxReferencedIssues]
	}
	for _, number := range numbers {
		item := referencedIssue{Number: number, URL: fmt.Sprintf("https://github.com/%s/%s/issues/%d", owner, repo, number), State: referencedIssueUnavailable}
		var issue githubIssue
		path := fmt.Sprintf("/repos/%s/%s/issues/%d", url.PathEscape(owner), url.PathEscape(repo), number)
		if err := gh.get(ctx, path, &issue); err != nil {
			result.Availability = referencedIssuesUnavailable
			result.Reason = "GitHub could not confirm the state of every referenced issue."
			result.Items = append(result.Items, item)
			continue
		}
		if issue.PullRequest != nil {
			continue
		}
		if !matchesReferencedIssueURL(issue.HTMLURL, owner, repo, number) || (issue.State != referencedIssueOpen && issue.State != referencedIssueClosed) {
			result.Availability = referencedIssuesUnavailable
			result.Reason = "GitHub returned incomplete or mismatched data for a referenced issue."
			result.Items = append(result.Items, item)
			continue
		}
		item.State = issue.State
		item.URL = issue.HTMLURL
		result.Items = append(result.Items, item)
	}
	return result
}

func matchesReferencedIssueURL(value, owner, repo string, number int) bool {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || !strings.EqualFold(parsed.Host, "github.com") || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return false
	}
	expected := fmt.Sprintf("/%s/%s/issues/%d", owner, repo, number)
	return strings.EqualFold(strings.TrimSuffix(parsed.Path, "/"), expected)
}
