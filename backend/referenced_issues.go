package main

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/felixgeelhaar/ghostline/backend/internal/domain"
)

const (
	maxReferencedIssueLookups = 20
	issueLookupWorkers        = 5
	issueLookupTimeout        = 12 * time.Second
)

type githubIssue struct {
	Number      int    `json:"number"`
	Title       string `json:"title"`
	State       string `json:"state"`
	HTMLURL     string `json:"html_url"`
	PullRequest *struct {
		URL string `json:"url"`
	} `json:"pull_request"`
}

func fetchReferencedIssues(parent context.Context, gh *githubClient, repository domain.RepositoryIdentity, body string) []referencedIssue {
	numbers := domain.ParseIssueReferences(body, repository)
	if len(numbers) == 0 {
		return []referencedIssue{}
	}
	results := make([]referencedIssue, len(numbers))
	for index, number := range numbers {
		results[index] = referencedIssue{
			Number: number, Title: fmt.Sprintf("Issue #%d", number), Availability: "unavailable",
			Reason: "Issue details could not be loaded.", URL: canonicalIssueURL(repository, number),
		}
		if index >= maxReferencedIssueLookups {
			results[index].Reason = "The lookup limit for this import was reached."
		}
	}
	limit := len(numbers)
	if limit > maxReferencedIssueLookups {
		limit = maxReferencedIssueLookups
	}
	ctx, cancel := context.WithTimeout(parent, issueLookupTimeout)
	defer cancel()
	jobs := make(chan int)
	var workers sync.WaitGroup
	workerCount := min(issueLookupWorkers, limit)
	workers.Add(workerCount)
	for range workerCount {
		go func() {
			defer workers.Done()
			for index := range jobs {
				results[index] = fetchReferencedIssue(ctx, gh, repository, numbers[index])
			}
		}()
	}

sendJobs:
	for index := 0; index < limit; index++ {
		select {
		case jobs <- index:
		case <-ctx.Done():
			break sendJobs
		}
	}
	close(jobs)
	workers.Wait()
	return results
}

func fetchReferencedIssue(ctx context.Context, gh *githubClient, repository domain.RepositoryIdentity, number int) referencedIssue {
	result := referencedIssue{
		Number: number, Title: fmt.Sprintf("Issue #%d", number), Availability: "unavailable",
		Reason: "Issue details could not be loaded.", URL: canonicalIssueURL(repository, number),
	}
	path := fmt.Sprintf("/repos/%s/%s/issues/%d", url.PathEscape(repository.Owner), url.PathEscape(repository.Name), number)
	var issue githubIssue
	if err := gh.get(ctx, path, &issue); err != nil {
		return result
	}
	if issue.PullRequest != nil {
		result.Reason = "GitHub identifies this reference as a pull request, not an issue."
		return result
	}
	state := strings.ToLower(issue.State)
	if issue.Number != number || strings.TrimSpace(issue.Title) == "" || (state != "open" && state != "closed") || !matchesIssueURL(issue.HTMLURL, repository, number) {
		result.Reason = "GitHub returned issue details that did not match the requested issue."
		return result
	}
	result.Title = issue.Title
	result.Availability = "available"
	result.State = state
	result.Reason = ""
	return result
}

func canonicalIssueURL(repository domain.RepositoryIdentity, number int) string {
	return fmt.Sprintf("https://github.com/%s/%s/issues/%d", repository.Owner, repository.Name, number)
}

func matchesIssueURL(value string, repository domain.RepositoryIdentity, number int) bool {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || !strings.EqualFold(parsed.Host, "github.com") || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return false
	}
	expected := fmt.Sprintf("/%s/%s/issues/%d", repository.Owner, repository.Name, number)
	return strings.EqualFold(strings.TrimSuffix(parsed.Path, "/"), expected)
}
