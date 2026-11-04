package main

import (
	"context"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const (
	reviewsAvailable    = "available"
	reviewsUnavailable  = "unavailable"
	maxQodoCommentPages = 5
)

type qodoReviewSet struct {
	Availability string       `json:"availability"`
	Reason       string       `json:"reason,omitempty"`
	Items        []qodoReview `json:"items"`
}

type qodoReview struct {
	ID            int64     `json:"id"`
	Kind          string    `json:"kind"`
	Summary       string    `json:"summary,omitempty"`
	FindingTitles []string  `json:"findingTitles"`
	CommitSHA     string    `json:"commitSha,omitempty"`
	CommitURL     string    `json:"commitUrl,omitempty"`
	CreatedAt     time.Time `json:"createdAt"`
	URL           string    `json:"url"`
}

type githubUser struct {
	Login string `json:"login"`
}

type githubIssueComment struct {
	ID        int64      `json:"id"`
	User      githubUser `json:"user"`
	CreatedAt time.Time  `json:"created_at"`
	HTMLURL   string     `json:"html_url"`
	Body      string     `json:"body"`
}

var (
	qodoSummaryHeading  = regexp.MustCompile(`(?is)<h3>\s*PR Summary by Qodo\s*</h3>(.*?)(?:<img[^>]*(?:Grey Divider|light-grey-line)[^>]*>|<details\b|$)`)
	qodoFindingsHeading = regexp.MustCompile(`(?is)<h3>\s*Code Review by Qodo\s*</h3>`)
	qodoFindingTitle    = regexp.MustCompile(`(?is)<summary>\s*\d+\.\s*(.*?)</summary>`)
	qodoCommitMarker    = regexp.MustCompile(`(?i)<!--\s*(https://github\.com/[^\s]+/commit/([0-9a-f]{40}))\s*-->`)
	htmlTag             = regexp.MustCompile(`(?s)<[^>]*>`)
)

func parseQodoReviews(comments []githubIssueComment) []qodoReview {
	reviews := make([]qodoReview, 0)
	for _, comment := range comments {
		if !isQodoReviewer(comment.User.Login) {
			continue
		}

		review := qodoReview{ID: comment.ID, CreatedAt: comment.CreatedAt, URL: comment.HTMLURL, FindingTitles: []string{}}
		if matches := qodoSummaryHeading.FindStringSubmatch(comment.Body); matches != nil {
			review.Kind = "summary"
			review.Summary = cleanQodoText(matches[1])
		} else if qodoFindingsHeading.MatchString(comment.Body) {
			review.Kind = "findings"
			for _, match := range qodoFindingTitle.FindAllStringSubmatch(comment.Body, -1) {
				if title := cleanQodoText(match[1]); title != "" {
					review.FindingTitles = append(review.FindingTitles, title)
				}
			}
		} else {
			continue
		}

		if match := qodoCommitMarker.FindStringSubmatch(comment.Body); match != nil {
			review.CommitURL = match[1]
			review.CommitSHA = strings.ToLower(match[2])
		}
		reviews = append(reviews, review)
	}
	return reviews
}

func isQodoReviewer(login string) bool {
	switch strings.ToLower(login) {
	case "qodo-code-review", "qodo-code-review[bot]", "qodo-free-for-open-source-projects[bot]":
		return true
	default:
		return false
	}
}

func cleanQodoText(value string) string {
	value = htmlTag.ReplaceAllString(value, " ")
	value = html.UnescapeString(value)
	return strings.Join(strings.Fields(value), " ")
}

func fetchQodoReviews(ctx context.Context, gh *githubClient, owner, repo string, number int) qodoReviewSet {
	reviews := make([]qodoReview, 0)
	for page := 1; page <= maxQodoCommentPages; page++ {
		var batch []githubIssueComment
		path := fmt.Sprintf("/repos/%s/%s/issues/%d/comments?per_page=100&page=%d", url.PathEscape(owner), url.PathEscape(repo), number, page)
		if err := gh.get(ctx, path, &batch); err != nil {
			return qodoReviewSet{Availability: reviewsUnavailable, Reason: "Qodo review comments could not be loaded from GitHub.", Items: []qodoReview{}}
		}
		reviews = append(reviews, parseQodoReviews(batch)...)
		if len(batch) < 100 {
			return qodoReviewSet{Availability: reviewsAvailable, Items: reviews}
		}
	}
	return qodoReviewSet{Availability: reviewsUnavailable, Reason: "GitHub returned more comments than Ghostline scans for Qodo reviews; re-import after the comment history is reduced or use a supported Qodo review source.", Items: []qodoReview{}}
}
