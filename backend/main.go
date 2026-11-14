package main

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/felixgeelhaar/ghostline/backend/internal/domain"
	"github.com/gin-gonic/gin"
)

//go:embed all:web
var frontendAssets embed.FS

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
	Base struct {
		Repo struct {
			Name     string `json:"name"`
			FullName string `json:"full_name"`
			Owner    struct {
				Login string `json:"login"`
			} `json:"owner"`
		} `json:"repo"`
	} `json:"base"`
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
		identity, err := domain.NewRepositoryIdentity(rawPR.Base.Repo.Owner.Login, rawPR.Base.Repo.Name)
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": "GitHub returned incomplete repository identity for this pull request."})
			return
		}
		path = fmt.Sprintf("/repos/%s/%s/pulls/%d", url.PathEscape(identity.Owner), url.PathEscape(identity.Name), rawPR.Number)
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
		checks := fetchRevisionChecks(c.Request.Context(), a.gh, identity.Owner, identity.Name, rawPR.Head.SHA)

		// GitHub requests are separate snapshots. Verify the head after fetching the
		// timeline and checks so a force push cannot pair a new timeline with old checks.
		var verifiedPR githubPR
		if err := a.gh.get(c.Request.Context(), path, &verifiedPR); err != nil {
			respondGitHubError(c, err)
			return
		}
		verifiedIdentity, err := domain.NewRepositoryIdentity(verifiedPR.Base.Repo.Owner.Login, verifiedPR.Base.Repo.Name)
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": "GitHub returned incomplete repository identity for this pull request."})
			return
		}
		if verifiedPR.Head.SHA != rawPR.Head.SHA || !identity.Equal(verifiedIdentity) {
			if attempt+1 < maxSnapshotAttempts {
				path = fmt.Sprintf("/repos/%s/%s/pulls/%d", url.PathEscape(verifiedIdentity.Owner), url.PathEscape(verifiedIdentity.Name), verifiedPR.Number)
				continue
			}
			c.JSON(http.StatusConflict, gin.H{"error": "This pull request changed while its timeline and checks were loading. Please import it again."})
			return
		}
		rawPR = verifiedPR
		identity = verifiedIdentity
		created, _ := time.Parse(time.RFC3339, rawPR.CreatedAt)
		updated, _ := time.Parse(time.RFC3339, rawPR.UpdatedAt)
		result := pullRequest{Repository: identity.FullName(), Owner: identity.Owner, Repo: identity.Name, Number: rawPR.Number, Title: rawPR.Title, State: rawPR.State, URL: rawPR.HTMLURL, CreatedAt: created, UpdatedAt: updated, Commits: make([]commit, 0, len(rawCommits)), Checks: checks, Reviews: fetchQodoReviews(c.Request.Context(), a.gh, identity, rawPR.Number)}
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

func (a api) loadRevisionChecks(c *gin.Context) {
	var body struct {
		URL string `json:"url"`
		SHA string `json:"sha"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Provide a GitHub pull request URL and a full commit SHA."})
		return
	}
	owner, repo, _, err := parsePRURL(body.URL)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if !fullSHA.MatchString(body.SHA) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "The revision must be a full 40-character commit SHA."})
		return
	}
	c.JSON(http.StatusOK, fetchRevisionChecks(c.Request.Context(), a.gh, owner, repo, body.SHA))
}

type revisionComparison struct {
	BaseSHA        string         `json:"baseSha"`
	HeadSHA        string         `json:"headSha"`
	CompareURL     string         `json:"compareUrl"`
	Status         string         `json:"status"`
	AheadBy        int            `json:"aheadBy"`
	BehindBy       int            `json:"behindBy"`
	TotalCommits   int            `json:"totalCommits"`
	Files          []comparedFile `json:"files"`
	FilesTruncated bool           `json:"filesTruncated"`
}

const githubCompareFileLimit = 300

type comparedFile struct {
	Path      string `json:"path"`
	Status    string `json:"status"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	Changes   int    `json:"changes"`
	URL       string `json:"url"`
}

func (a api) compareRevisions(c *gin.Context) {
	var body struct {
		URL     string `json:"url"`
		BaseSHA string `json:"baseSha"`
		HeadSHA string `json:"headSha"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Provide a GitHub pull request URL and two full commit SHAs."})
		return
	}
	owner, repo, _, err := parsePRURL(body.URL)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if !fullSHA.MatchString(body.BaseSHA) || !fullSHA.MatchString(body.HeadSHA) || strings.EqualFold(body.BaseSHA, body.HeadSHA) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Choose two different full 40-character commit SHAs."})
		return
	}
	path := fmt.Sprintf("/repos/%s/%s/compare/%s...%s?per_page=100", url.PathEscape(owner), url.PathEscape(repo), strings.ToLower(body.BaseSHA), strings.ToLower(body.HeadSHA))
	var raw struct {
		Status       string `json:"status"`
		AheadBy      int    `json:"ahead_by"`
		BehindBy     int    `json:"behind_by"`
		TotalCommits int    `json:"total_commits"`
		HTMLURL      string `json:"html_url"`
		Base         struct {
			SHA string `json:"sha"`
		} `json:"base_commit"`
		Head struct {
			SHA string `json:"sha"`
		} `json:"head_commit"`
		Files []struct {
			Filename  string `json:"filename"`
			Status    string `json:"status"`
			Additions int    `json:"additions"`
			Deletions int    `json:"deletions"`
			Changes   int    `json:"changes"`
			BlobURL   string `json:"blob_url"`
		} `json:"files"`
	}
	if err := a.gh.get(c.Request.Context(), path, &raw); err != nil {
		respondGitHubError(c, err)
		return
	}
	// GitHub's documented compare response includes base_commit but does not
	// include head_commit. The requested compare URL anchors the head SHA; when
	// GitHub does return a head_commit, still reject a mismatch.
	if !strings.EqualFold(raw.Base.SHA, body.BaseSHA) || (raw.Head.SHA != "" && !strings.EqualFold(raw.Head.SHA, body.HeadSHA)) {
		c.JSON(http.StatusBadGateway, gin.H{"error": "GitHub returned a comparison for different revisions. Please retry."})
		return
	}
	expectedComparePath := fmt.Sprintf("/%s/%s/compare/%s...%s", owner, repo, strings.ToLower(body.BaseSHA), strings.ToLower(body.HeadSHA))
	if !validGitHubSourceURL(raw.HTMLURL, expectedComparePath) {
		c.JSON(http.StatusBadGateway, gin.H{"error": "GitHub returned an invalid comparison source link."})
		return
	}
	if raw.Status != "ahead" && raw.Status != "behind" && raw.Status != "identical" && raw.Status != "diverged" {
		c.JSON(http.StatusBadGateway, gin.H{"error": "GitHub returned an unrecognized comparison status."})
		return
	}
	if raw.AheadBy < 0 || raw.BehindBy < 0 || raw.TotalCommits < 0 {
		c.JSON(http.StatusBadGateway, gin.H{"error": "GitHub returned invalid comparison counts."})
		return
	}
	files := make([]comparedFile, 0, len(raw.Files))
	for _, file := range raw.Files {
		if strings.TrimSpace(file.Filename) == "" || file.Additions < 0 || file.Deletions < 0 || file.Changes < 0 {
			c.JSON(http.StatusBadGateway, gin.H{"error": "GitHub returned invalid changed-file details."})
			return
		}
		if !validGitHubSourceURL(file.BlobURL, fmt.Sprintf("/%s/%s/blob/", owner, repo)) {
			c.JSON(http.StatusBadGateway, gin.H{"error": "GitHub returned an invalid changed-file source link."})
			return
		}
		files = append(files, comparedFile{Path: file.Filename, Status: file.Status, Additions: file.Additions, Deletions: file.Deletions, Changes: file.Changes, URL: file.BlobURL})
	}
	c.JSON(http.StatusOK, revisionComparison{
		BaseSHA: strings.ToLower(body.BaseSHA), HeadSHA: strings.ToLower(body.HeadSHA), CompareURL: raw.HTMLURL,
		Status: raw.Status, AheadBy: raw.AheadBy, BehindBy: raw.BehindBy, TotalCommits: raw.TotalCommits, Files: files,
		FilesTruncated: len(files) >= githubCompareFileLimit,
	})
}

func validGitHubSourceURL(raw, expectedPath string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || !strings.EqualFold(u.Host, "github.com") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	path := strings.ToLower(u.EscapedPath())
	expected := strings.ToLower(expectedPath)
	if strings.HasSuffix(expected, "/") {
		if !strings.HasPrefix(path, expected) {
			return false
		}
		remainder := strings.TrimPrefix(path, expected)
		parts := strings.Split(strings.Trim(remainder, "/"), "/")
		return len(parts) >= 2 && fullSHA.MatchString(parts[0])
	}
	return path == expected
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
	return newRouterWithLimiter(gh, newRateLimiter(defaultRateLimitRequests, defaultRateLimitWindow, time.Now), clientIdentity{})
}

func newRouterWithLimiter(gh *githubClient, limiter *rateLimiter, identity clientIdentity) *gin.Engine {
	return newRouterWithAuth(gh, limiter, identity, authSettings{Now: time.Now})
}

func newProductionRouter(gh *githubClient, limiter *rateLimiter, identity clientIdentity, settings authSettings, assets fs.FS) *gin.Engine {
	r := newRouterWithAuth(gh, limiter, identity, settings)
	if assets == nil {
		return r
	}
	static, err := fs.Sub(assets, "web")
	if err != nil {
		// Unit tests may provide an already-rooted asset filesystem.
		static = assets
	}
	index, err := fs.ReadFile(static, "index.html")
	if err != nil {
		// `go run ./backend` is an API-only local development mode.
		return r
	}
	r.NoRoute(func(c *gin.Context) {
		if c.Request.URL.Path == "/api" || strings.HasPrefix(c.Request.URL.Path, "/api/") || c.Request.URL.Path == "/auth" || strings.HasPrefix(c.Request.URL.Path, "/auth/") {
			c.JSON(http.StatusNotFound, gin.H{"error": "Not found."})
			return
		}
		if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
			c.Status(http.StatusNotFound)
			return
		}
		name := strings.TrimPrefix(strings.TrimPrefix(c.Request.URL.Path, "/"), "/")
		if name != "." && name != "" {
			if body, readErr := fs.ReadFile(static, name); readErr == nil {
				contentType := mime.TypeByExtension(filepath.Ext(name))
				if contentType == "" {
					switch filepath.Ext(name) {
					case ".js":
						contentType = "text/javascript; charset=utf-8"
					case ".css":
						contentType = "text/css; charset=utf-8"
					case ".svg":
						contentType = "image/svg+xml"
					case ".woff2":
						contentType = "font/woff2"
					}
				}
				if contentType != "" {
					c.Header("Content-Type", contentType)
				}
				if strings.HasPrefix(name, "assets/") {
					c.Header("Cache-Control", "public, max-age=31536000, immutable")
				}
				c.Data(http.StatusOK, contentType, body)
				return
			}
			if strings.HasPrefix(name, "assets/") || filepath.Ext(name) != "" {
				http.NotFound(c.Writer, c.Request)
				return
			}
		}
		c.Data(http.StatusOK, "text/html; charset=utf-8", index)
	})
	return r
}

func main() {
	cfg, err := loadRateLimitConfig(os.Getenv)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	auth, err := loadAuthConfig(os.Getenv)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	r := newProductionRouter(newGitHubClient(), newRateLimiter(cfg.Requests, cfg.Window, time.Now), cfg.Identity, auth, frontendAssets)
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
