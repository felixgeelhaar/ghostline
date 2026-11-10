package main

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"golang.org/x/net/html"

	"github.com/felixgeelhaar/ghostline/backend/internal/domain"
)

const (
	reviewsAvailable    = "available"
	reviewsUnavailable  = "unavailable"
	maxQodoCommentPages = 5
)

type qodoReviewSet struct {
	Availability             string       `json:"availability"`
	Reason                   string       `json:"reason,omitempty"`
	UnrecognizedCommentCount int          `json:"unrecognizedCommentCount"`
	Items                    []qodoReview `json:"items"`
}

type qodoReview struct {
	ID           int64         `json:"id"`
	Provider     string        `json:"provider"`
	SourceType   string        `json:"sourceType"`
	SourceAuthor string        `json:"sourceAuthor"`
	Kind         string        `json:"kind"`
	Summary      string        `json:"summary,omitempty"`
	Findings     []qodoFinding `json:"findings"`
	CommitSHA    string        `json:"commitSha,omitempty"`
	CommitURL    string        `json:"commitUrl,omitempty"`
	CreatedAt    time.Time     `json:"createdAt"`
	URL          string        `json:"url"`
}

type qodoFinding struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Categories  []string `json:"categories"`
	Lifecycle   string   `json:"lifecycle,omitempty"`
	Severity    string   `json:"severity,omitempty"`
	ActionLevel string   `json:"actionLevel,omitempty"`
	Description string   `json:"description,omitempty"`
	SourceURL   string   `json:"sourceUrl"`
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
	qodoCommitMarker        = regexp.MustCompile(`(?i)<!--\s*(https://github\.com/([A-Za-z0-9_.-]+)/([A-Za-z0-9_.-]+)/commit/([0-9a-f]{40}))\s*-->`)
	qodoFindingOrdinal      = regexp.MustCompile(`^\s*\d+\.\s*`)
	qodoFindingHeadingText  = regexp.MustCompile(`^\s*\d+\.`)
	qodoNonzeroFindingGroup = regexp.MustCompile(`(?i)^view\s+.+\(\s*[1-9]\d*\s*\)$`)
)

// parseQodoReviews extracts Qodo reviews from issue comments. A review's commit
// marker is kept only when it names the imported repository; a marker for any
// other repository is dropped, so the review shows no reviewed revision rather
// than another repository's commit.
func parseQodoReviews(comments []githubIssueComment, repo domain.RepositoryIdentity) ([]qodoReview, int) {
	reviews := make([]qodoReview, 0)
	unrecognized := 0
	for _, comment := range comments {
		if !isQodoReviewer(comment.User.Login) {
			continue
		}

		doc, err := html.Parse(strings.NewReader(comment.Body))
		if err != nil {
			unrecognized++
			continue
		}
		heading := findNode(doc, func(node *html.Node) bool {
			return node.Type == html.ElementNode && node.Data == "h3"
		})
		if heading == nil {
			unrecognized++
			continue
		}
		headingText := cleanQodoText(nodeText(heading))
		review := qodoReview{
			ID: comment.ID, Provider: "Qodo", SourceType: "github_issue_comment", SourceAuthor: comment.User.Login,
			CreatedAt: comment.CreatedAt, URL: comment.HTMLURL, Findings: []qodoFinding{},
		}
		switch headingText {
		case "PR Summary by Qodo":
			review.Kind = "summary"
			review.Summary = qodoSummaryText(heading)
		case "Code Review by Qodo":
			review.Findings = qodoFindings(doc, comment.ID, comment.HTMLURL)
			if len(review.Findings) == 0 && qodoReportsNoOpenFindings(nodeText(doc)) && !qodoHasNonzeroFindingGroup(doc) {
				review.Kind = "no_open_findings"
			} else {
				review.Kind = "findings"
			}
		default:
			unrecognized++
			continue
		}

		if match := qodoCommitMarker.FindStringSubmatch(comment.Body); match != nil &&
			repo.Equal(domain.RepositoryIdentity{Owner: match[2], Name: match[3]}) {
			review.CommitURL = match[1]
			review.CommitSHA = strings.ToLower(match[4])
		}
		reviews = append(reviews, review)
	}
	return reviews, unrecognized
}

func qodoHasNonzeroFindingGroup(root *html.Node) bool {
	return findNode(root, func(node *html.Node) bool {
		return node.Type == html.ElementNode && node.Data == "summary" && qodoNonzeroFindingGroup.MatchString(nodeText(node))
	}) != nil
}

func qodoReportsNoOpenFindings(value string) bool {
	text := strings.ToLower(cleanQodoText(value))
	return strings.Contains(text, "no open findings for this group") || strings.Contains(text, "great, no issues found")
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
	return strings.Join(strings.Fields(value), " ")
}

func findNode(root *html.Node, predicate func(*html.Node) bool) *html.Node {
	if predicate(root) {
		return root
	}
	for child := root.FirstChild; child != nil; child = child.NextSibling {
		if found := findNode(child, predicate); found != nil {
			return found
		}
	}
	return nil
}

func nodeText(root *html.Node) string {
	var text strings.Builder
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.TextNode {
			text.WriteString(node.Data)
			text.WriteByte(' ')
			return
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	return cleanQodoText(text.String())
}

func qodoSummaryText(heading *html.Node) string {
	var text strings.Builder
	for node := heading.NextSibling; node != nil; node = node.NextSibling {
		if node.Type == html.ElementNode && (node.Data == "details" || node.Data == "img" && isQodoDivider(node)) {
			break
		}
		text.WriteString(nodeText(node))
		text.WriteByte(' ')
	}
	return cleanQodoText(text.String())
}

func isQodoDivider(node *html.Node) bool {
	alt := strings.ToLower(attribute(node, "alt"))
	src := strings.ToLower(attribute(node, "src"))
	return strings.Contains(alt, "grey divider") || strings.Contains(src, "light-grey-line")
}

func qodoFindings(root *html.Node, commentID int64, sourceURL string) []qodoFinding {
	findings := make([]qodoFinding, 0)
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode && node.Data == "summary" && qodoFindingHeadingText.MatchString(nodeText(node)) {
			if finding := qodoFindingFromSummary(node, commentID, len(findings)+1, sourceURL); finding.Title != "" {
				findings = append(findings, finding)
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	return findings
}

func qodoFindingFromSummary(summary *html.Node, commentID int64, ordinal int, sourceURL string) qodoFinding {
	var title strings.Builder
	codes := make([]string, 0)
	var readHeading func(*html.Node)
	readHeading = func(node *html.Node) {
		if node.Type == html.TextNode {
			title.WriteString(node.Data)
			return
		}
		if node.Type == html.ElementNode && node.Data == "code" {
			codes = append(codes, nodeText(node))
			return
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			readHeading(child)
		}
	}
	readHeading(summary)
	finding := qodoFinding{ID: fmt.Sprintf("%d:%d", commentID, ordinal), Title: qodoFindingOrdinal.ReplaceAllString(cleanQodoText(title.String()), ""), Categories: []string{}, SourceURL: sourceURL}
	for _, code := range codes {
		value := cleanQodoText(code)
		switch {
		case strings.Contains(strings.ToLower(value), "new"), strings.Contains(strings.ToLower(value), "existing"), strings.Contains(strings.ToLower(value), "resolved"):
			finding.Lifecycle = value
		default:
			if !containsString(finding.Categories, value) {
				finding.Categories = append(finding.Categories, value)
			}
		}
	}
	if detail := ancestorFindingDetails(summary); detail != nil {
		finding.Description = findingDescription(detail)
		finding.Severity, finding.ActionLevel = findingLabels(summary)
	}
	return finding
}

func containsString(values []string, value string) bool {
	for _, existing := range values {
		if existing == value {
			return true
		}
	}
	return false
}

func ancestorFindingDetails(node *html.Node) *html.Node {
	for parent := node.Parent; parent != nil; parent = parent.Parent {
		if parent.Type == html.ElementNode && parent.Data == "details" {
			return parent
		}
	}
	return nil
}

func findingDescription(detail *html.Node) string {
	description := findNode(detail, func(node *html.Node) bool {
		return node.Type == html.ElementNode && node.Data == "summary" && strings.EqualFold(nodeText(node), "Description")
	})
	if description == nil || description.Parent == nil {
		return ""
	}
	pre := findNode(description.Parent, func(node *html.Node) bool { return node.Type == html.ElementNode && node.Data == "pre" })
	if pre == nil {
		return ""
	}
	return nodeText(pre)
}

func findingLabels(summary *html.Node) (string, string) {
	for parent := summary.Parent; parent != nil; parent = parent.Parent {
		if parent.Type != html.ElementNode || parent.Data != "details" {
			continue
		}
		severity, actionLevel := "", ""
		var walk func(*html.Node)
		walk = func(node *html.Node) {
			if node.Type == html.ElementNode && node.Data == "img" {
				label := strings.TrimSpace(attribute(node, "alt"))
				if strings.EqualFold(label, "Low") || strings.EqualFold(label, "Medium") || strings.EqualFold(label, "High") || strings.EqualFold(label, "Critical") {
					severity = label
				} else if strings.Contains(strings.ToLower(label), "remediation") || strings.Contains(strings.ToLower(label), "informational") {
					actionLevel = label
				}
			}
			for child := node.FirstChild; child != nil; child = child.NextSibling {
				walk(child)
			}
		}
		walk(parent)
		if severity != "" || actionLevel != "" {
			return severity, actionLevel
		}
	}
	return "", ""
}

func attribute(node *html.Node, name string) string {
	for _, attr := range node.Attr {
		if strings.EqualFold(attr.Key, name) {
			return attr.Val
		}
	}
	return ""
}

func fetchQodoReviews(ctx context.Context, gh *githubClient, repo domain.RepositoryIdentity, number int) qodoReviewSet {
	reviews := make([]qodoReview, 0)
	unrecognized := 0
	for page := 1; page <= maxQodoCommentPages; page++ {
		var batch []githubIssueComment
		path := fmt.Sprintf("/repos/%s/%s/issues/%d/comments?per_page=100&page=%d", url.PathEscape(repo.Owner), url.PathEscape(repo.Name), number, page)
		if err := gh.get(ctx, path, &batch); err != nil {
			return qodoReviewSet{Availability: reviewsUnavailable, Reason: "Qodo review comments could not be loaded from GitHub.", Items: []qodoReview{}}
		}
		parsed, unrecognizedBatch := parseQodoReviews(batch, repo)
		reviews = append(reviews, parsed...)
		unrecognized += unrecognizedBatch
		if len(batch) < 100 {
			return qodoReviewSet{Availability: reviewsAvailable, UnrecognizedCommentCount: unrecognized, Items: reviews}
		}
	}
	return qodoReviewSet{Availability: reviewsUnavailable, Reason: "GitHub returned more comments than Ghostline scans for Qodo reviews; re-import after the comment history is reduced or use a supported Qodo review source.", Items: []qodoReview{}}
}
