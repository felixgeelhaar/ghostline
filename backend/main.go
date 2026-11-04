package main

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

type pullRequest struct {
	Repository string         `json:"repository"`
	Owner      string         `json:"owner"`
	Repo       string         `json:"repo"`
	Number     int            `json:"number"`
	Title      string         `json:"title"`
	State      string         `json:"state"`
	URL        string         `json:"url"`
	CreatedAt  time.Time      `json:"createdAt"`
	UpdatedAt  time.Time      `json:"updatedAt"`
	Commits    []commit       `json:"commits"`
	Checks     revisionChecks `json:"checks"`
	Reviews    qodoReviewSet  `json:"qodoReviews"`
}

type commit struct {
	SHA         string    `json:"sha"`
	ShortSHA    string    `json:"shortSha"`
	Message     string    `json:"message"`
	Author      string    `json:"author"`
	CommittedAt time.Time `json:"committedAt"`
	URL         string    `json:"url"`
}

type githubPR struct {
	Number    int    `json:"number"`
	Title     string `json:"title"`
	State     string `json:"state"`
	HTMLURL   string `json:"html_url"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
	Head      struct {
		SHA string `json:"sha"`
	} `json:"head"`
}

type githubCommit struct {
	SHA     string `json:"sha"`
	HTMLURL string `json:"html_url"`
	Commit  struct {
		Message string `json:"message"`
		Author  struct {
			Name string `json:"name"`
			Date string `json:"date"`
		} `json:"author"`
	} `json:"commit"`
	Author *struct {
		Login string `json:"login"`
	} `json:"author"`
}

var prPath = regexp.MustCompile(`^/([^/]+)/([^/]+)/pull/(\d+)/?$`)

func parsePRURL(raw string) (string, string, int, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" || !strings.EqualFold(u.Host, "github.com") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", "", 0, errors.New("Enter a public GitHub pull request URL, such as https://github.com/owner/repo/pull/123.")
	}
	matches := prPath.FindStringSubmatch(u.Path)
	if matches == nil {
		return "", "", 0, errors.New("That URL does not look like a GitHub pull request. Use a URL ending in /owner/repo/pull/number.")
	}
	n, err := strconv.Atoi(matches[3])
	if err != nil || n < 1 {
		return "", "", 0, errors.New("The pull request number must be a positive integer.")
	}
	return matches[1], matches[2], n, nil
}

type api struct{ gh *githubClient }

func (a api) importPR(c *gin.Context) {
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
	path := fmt.Sprintf("/repos/%s/%s/pulls/%d", url.PathEscape(owner), url.PathEscape(repo), number)
	const maxSnapshotAttempts = 2
	for attempt := 0; attempt < maxSnapshotAttempts; attempt++ {
		var rawPR githubPR
		if err := a.gh.get(c.Request.Context(), path, &rawPR); err != nil {
			respondGitHubError(c, err)
			return
		}
		var rawCommits []githubCommit
		for page := 1; ; page++ {
			var batch []githubCommit
			if err := a.gh.get(c.Request.Context(), fmt.Sprintf("%s/commits?per_page=100&page=%d", path, page), &batch); err != nil {
				respondGitHubError(c, err)
				return
			}
			rawCommits = append(rawCommits, batch...)
			if len(batch) < 100 {
				break
			}
		}
		checks := fetchRevisionChecks(c.Request.Context(), a.gh, owner, repo, rawPR.Head.SHA)

		// GitHub requests are separate snapshots. Verify the head after fetching the
		// timeline and checks so a force push cannot pair a new timeline with old checks.
		var verifiedPR githubPR
		if err := a.gh.get(c.Request.Context(), path, &verifiedPR); err != nil {
			respondGitHubError(c, err)
			return
		}
		if verifiedPR.Head.SHA != rawPR.Head.SHA {
			if attempt+1 < maxSnapshotAttempts {
				continue
			}
			c.JSON(http.StatusConflict, gin.H{"error": "This pull request changed while its timeline and checks were loading. Please import it again."})
			return
		}
		rawPR = verifiedPR
		created, _ := time.Parse(time.RFC3339, rawPR.CreatedAt)
		updated, _ := time.Parse(time.RFC3339, rawPR.UpdatedAt)
		result := pullRequest{Repository: owner + "/" + repo, Owner: owner, Repo: repo, Number: rawPR.Number, Title: rawPR.Title, State: rawPR.State, URL: rawPR.HTMLURL, CreatedAt: created, UpdatedAt: updated, Commits: make([]commit, 0, len(rawCommits)), Checks: checks, Reviews: fetchQodoReviews(c.Request.Context(), a.gh, owner, repo, number)}
		for _, item := range rawCommits {
			date, _ := time.Parse(time.RFC3339, item.Commit.Author.Date)
			author := item.Commit.Author.Name
			if item.Author != nil && item.Author.Login != "" {
				author = item.Author.Login
			}
			sha := item.SHA
			short := sha
			if len(short) > 7 {
				short = short[:7]
			}
			result.Commits = append(result.Commits, commit{SHA: sha, ShortSHA: short, Message: strings.SplitN(item.Commit.Message, "\n", 2)[0], Author: author, CommittedAt: date, URL: item.HTMLURL})
		}
		c.JSON(http.StatusOK, result)
		return
	}
}

func respondGitHubError(c *gin.Context, err error) {
	var statusErr statusError
	switch {
	case errors.Is(err, errNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "Pull request not found. Check the URL and confirm the repository is public."})
	case errors.Is(err, errRateLimit):
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "GitHub’s public API rate limit has been reached. Try again later or configure GITHUB_TOKEN."})
	case errors.As(err, &statusErr) && statusErr.code >= 500:
		c.JSON(http.StatusBadGateway, gin.H{"error": "GitHub is having trouble right now. Try again shortly."})
	default:
		c.JSON(http.StatusBadGateway, gin.H{"error": "Could not reach GitHub. Check your connection and try again."})
	}
}

func newRouter(gh *githubClient) *gin.Engine {
	r := gin.Default()
	r.GET("/api/health", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })
	r.POST("/api/import", api{gh: gh}.importPR)
	return r
}

func main() {
	r := newRouter(newGitHubClient())
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	host := os.Getenv("HOST")
	if host == "" {
		host = "127.0.0.1"
	}
	if err := r.Run(host + ":" + port); err != nil {
		panic(err)
	}
}
