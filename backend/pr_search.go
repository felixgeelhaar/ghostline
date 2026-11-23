package main

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/felixgeelhaar/ghostline/backend/internal/domain"
	"github.com/gin-gonic/gin"
)

// githubSearchesPerWindow keeps a user below GitHub's search API limit of 30
// requests a minute. The limiter's window is fixed, so a burst at the end of one
// window and another as the next opens can land within one minute: 2 × 14 = 28.
const githubSearchesPerWindow = 14

const (
	minSearchQueryLength = 3
	maxSearchQueryLength = 256
	searchResultLimit    = 10
	// maxSearchRequestBytes fits a 256-character query of 4-byte characters in JSON.
	maxSearchRequestBytes = 4 << 10
)

var errSearchSignInRequired = errors.New("search needs the signed-in user's GitHub token")

var (
	shorthandReference = regexp.MustCompile(`^([A-Za-z0-9](?:[A-Za-z0-9-]{0,38}))/([A-Za-z0-9._-]{1,100})#(\d+)$`)
	searchRepoURLPath  = regexp.MustCompile(`/repos/([^/]+)/([^/]+)$`)
)

// parsePullRequestReference accepts a pull request URL or the owner/repo#number
// shorthand GitHub uses to cross-reference pull requests.
func parsePullRequestReference(raw string) (string, string, int, error) {
	trimmed := strings.TrimSpace(raw)
	matches := shorthandReference.FindStringSubmatch(trimmed)
	if matches == nil {
		return parsePRURL(trimmed)
	}
	if matches[2] == "." || matches[2] == ".." {
		return "", "", 0, errors.New("That repository name is not valid.")
	}
	n, err := strconv.Atoi(matches[3])
	if err != nil || n < 1 {
		return "", "", 0, errors.New("The pull request number must be a positive integer.")
	}
	return matches[1], matches[2], n, nil
}

type pullRequestSuggestion struct {
	Repository string `json:"repository"`
	Owner      string `json:"owner"`
	Repo       string `json:"repo"`
	Number     int    `json:"number"`
	Title      string `json:"title"`
	// State is open, draft, merged or closed.
	State string `json:"state"`
	// UpdatedAt is when GitHub last changed the pull request; it says nothing about
	// when it was pushed or reviewed.
	UpdatedAt time.Time `json:"updatedAt"`
	URL       string    `json:"url"`
}

type githubSearchItem struct {
	Number        int    `json:"number"`
	Title         string `json:"title"`
	State         string `json:"state"`
	Draft         bool   `json:"draft"`
	UpdatedAt     string `json:"updated_at"`
	HTMLURL       string `json:"html_url"`
	RepositoryURL string `json:"repository_url"`
	PullRequest   *struct {
		MergedAt *string `json:"merged_at"`
	} `json:"pull_request"`
}

type githubSearchReply struct {
	IncompleteResults bool               `json:"incomplete_results"`
	Items             []githubSearchItem `json:"items"`
}

// searchPullRequests finds public pull requests that involve the signed-in user.
// GitHub stays the source: nothing is stored between searches.
func (a api) searchPullRequests(limiter *rateLimiter) gin.HandlerFunc {
	return func(c *gin.Context) {
		// A search is a few words; refuse a large body before decoding it.
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxSearchRequestBytes)
		var body struct {
			Query string `json:"query"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Enter text to search your pull requests."})
			return
		}
		query := strings.Join(strings.Fields(body.Query), " ")
		// Count characters, as the browser does, not UTF-8 bytes.
		if n := utf8.RuneCountInString(query); n < minSearchQueryLength || n > maxSearchQueryLength {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Search with %d to %d characters.", minSearchQueryLength, maxSearchQueryLength)})
			return
		}
		terms := plainSearchTerms(query)
		if terms == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Enter words to search your pull requests."})
			return
		}
		value, _ := c.Get(githubIdentityKey)
		identity, signedIn := value.(domain.GitHubIdentity)
		if !signedIn || githubUserToken(c.Request.Context()).token == "" {
			respondSearchError(c, errSearchSignInRequired)
			return
		}
		allowed, retryAfter := limiter.allow("github:" + strconv.FormatInt(identity.ID, 10))
		if !allowed {
			seconds := retryAfterSeconds(retryAfter)
			c.Header("Retry-After", strconv.Itoa(seconds))
			c.JSON(http.StatusTooManyRequests, gin.H{"error": fmt.Sprintf("Too many searches from this GitHub account. Try again in %d seconds.", seconds)})
			return
		}
		// A read:user token cannot see private repositories, and is:public keeps the
		// search to public ones even if the token ever could.
		q := "is:pr is:public involves:@me " + terms
		path := fmt.Sprintf("/search/issues?q=%s&sort=updated&order=desc&per_page=%d", url.QueryEscape(q), searchResultLimit)
		var reply githubSearchReply
		if err := a.gh.get(c.Request.Context(), path, &reply); err != nil {
			respondSearchError(c, err)
			return
		}
		items := make([]pullRequestSuggestion, 0, len(reply.Items))
		for _, raw := range reply.Items {
			item, err := suggestionFromSearch(raw)
			if err != nil {
				c.JSON(http.StatusBadGateway, gin.H{"error": "GitHub returned a search result that does not identify its pull request."})
				return
			}
			items = append(items, item)
		}
		c.JSON(http.StatusOK, gin.H{"items": items, "partial": reply.IncompleteResults})
	}
}

// plainSearchTerms quotes each word so GitHub reads it as text: operators such as
// OR or NOT, and qualifiers such as author: or -is:public, cannot widen the fixed
// filters to pull requests that do not involve the user.
func plainSearchTerms(query string) string {
	strip := strings.NewReplacer(`"`, "", `\`, "")
	var terms []string
	for _, word := range strings.Fields(query) {
		if word = strip.Replace(word); word != "" {
			terms = append(terms, `"`+word+`"`)
		}
	}
	return strings.Join(terms, " ")
}

func respondSearchError(c *gin.Context, err error) {
	if errors.Is(err, errSearchSignInRequired) {
		c.JSON(http.StatusUnauthorized, gin.H{"code": "SEARCH_SIGNIN_REQUIRED", "error": "Sign in with GitHub again to search your pull requests, or paste a pull request URL."})
		return
	}
	var status statusError
	if errors.As(err, &status) {
		switch status.code {
		case http.StatusUnauthorized:
			// GitHub no longer accepts the stored token (revoked or expired): stop using it.
			if credential := githubUserToken(c.Request.Context()); credential.revoke != nil {
				credential.revoke()
			}
			respondSearchError(c, errSearchSignInRequired)
			return
		case http.StatusUnprocessableEntity:
			c.JSON(http.StatusBadRequest, gin.H{"error": "GitHub could not run this search. Try fewer or shorter words, or paste a pull request URL."})
			return
		}
	}
	respondGitHubError(c, err)
}

// suggestionFromSearch validates one search item: its page URL, repository and
// number must agree, so a picked suggestion imports exactly the pull request shown.
func suggestionFromSearch(raw githubSearchItem) (pullRequestSuggestion, error) {
	invalid := errors.New("search item does not identify a pull request")
	if raw.PullRequest == nil {
		return pullRequestSuggestion{}, invalid
	}
	owner, repo, number, err := parsePRURL(raw.HTMLURL)
	if err != nil || number != raw.Number {
		return pullRequestSuggestion{}, invalid
	}
	repoURL, err := url.Parse(raw.RepositoryURL)
	if err != nil {
		return pullRequestSuggestion{}, invalid
	}
	repoPath := searchRepoURLPath.FindStringSubmatch(repoURL.Path)
	if repoPath == nil || !strings.EqualFold(repoPath[1], owner) || !strings.EqualFold(repoPath[2], repo) {
		return pullRequestSuggestion{}, invalid
	}
	updated, err := time.Parse(time.RFC3339, raw.UpdatedAt)
	if err != nil {
		return pullRequestSuggestion{}, invalid
	}
	var state string
	switch {
	case raw.State == "open" && raw.Draft:
		state = "draft"
	case raw.State == "open":
		state = "open"
	case raw.State == "closed" && raw.PullRequest.MergedAt != nil:
		state = "merged"
	case raw.State == "closed":
		state = "closed"
	default:
		return pullRequestSuggestion{}, invalid
	}
	return pullRequestSuggestion{
		Repository: owner + "/" + repo, Owner: owner, Repo: repo, Number: number,
		Title: raw.Title, State: state, UpdatedAt: updated, URL: raw.HTMLURL,
	}, nil
}
