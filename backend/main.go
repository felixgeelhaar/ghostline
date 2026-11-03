package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

type pullRequest struct {
	Repository string    `json:"repository"`
	Owner      string    `json:"owner"`
	Repo       string    `json:"repo"`
	Number     int       `json:"number"`
	Title      string    `json:"title"`
	State      string    `json:"state"`
	URL        string    `json:"url"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
	Commits    []commit  `json:"commits"`
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

func githubGet(ctx context.Context, path string, target any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com"+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "Ghostline-Day1")
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	client := &http.Client{Timeout: 12 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusNotFound {
			return errors.New("not found")
		}
		if resp.StatusCode == http.StatusForbidden && resp.Header.Get("X-RateLimit-Remaining") == "0" {
			return errors.New("rate limit")
		}
		return fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(target)
}

type scannerResult struct {
	Name       string    `json:"name"`
	Status     string    `json:"status"`
	Summary    string    `json:"summary"`
	Output     string    `json:"output,omitempty"`
	ExitCode   int       `json:"exitCode"`
	FinishedAt time.Time `json:"finishedAt"`
}

func scanRepo(c *gin.Context) {
	var body struct {
		Path string `json:"path"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || strings.TrimSpace(body.Path) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Choose a repository directory to scan."})
		return
	}
	root, err := filepath.Abs(body.Path)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Repository path is invalid."})
		return
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Repository directory was not found."})
		return
	}
	results := []scannerResult{runNox(c.Request.Context(), root), runWarden(c.Request.Context(), root)}
	c.JSON(http.StatusOK, gin.H{"repositoryPath": root, "results": results})
}

func runNox(ctx context.Context, root string) scannerResult {
	started := time.Now()
	bin, err := exec.LookPath("nox")
	if err != nil {
		return scannerResult{Name: "Nox", Status: "unavailable", Summary: "Nox CLI was not found on PATH.", FinishedAt: time.Now()}
	}
	outputDir, tempErr := os.MkdirTemp("", "ghostline-nox-")
	if tempErr != nil {
		return scannerResult{Name: "Nox", Status: "error", Summary: tempErr.Error(), FinishedAt: time.Now()}
	}
	defer os.RemoveAll(outputDir)
	output, err := exec.CommandContext(ctx, bin, "scan", root, "--format", "json", "--output", outputDir, "--offline").CombinedOutput()
	if report, readErr := os.ReadFile(filepath.Join(outputDir, "findings.json")); readErr == nil {
		output = report
	}
	return scannerResult{Name: "Nox", Status: scanStatus(err), Summary: fmt.Sprintf("Completed in %s", time.Since(started).Round(time.Millisecond)), Output: string(output), ExitCode: exitCode(err), FinishedAt: time.Now()}
}

func runWarden(ctx context.Context, root string) scannerResult {
	started := time.Now()
	bin := os.Getenv("WARDEN_COMMAND")
	if bin == "" {
		bin = "warden"
	}
	resolved, err := exec.LookPath(bin)
	if err != nil {
		return scannerResult{Name: "Warden pre-push gate", Status: "unavailable", Summary: "Warden CLI was not found on PATH.", FinishedAt: time.Now()}
	}
	hook := os.Getenv("WARDEN_HOOK")
	if hook == "" {
		hook = "pre-push"
	}
	if hook != "pre-push" && hook != "pre-commit" {
		return scannerResult{Name: "Warden", Status: "error", Summary: "WARDEN_HOOK must be pre-push or pre-commit.", ExitCode: -1, FinishedAt: time.Now()}
	}
	cmd := exec.CommandContext(ctx, resolved, "run", hook)
	cmd.Dir = root
	output, runErr := cmd.CombinedOutput()
	return scannerResult{Name: "Warden " + hook + " gate", Status: scanStatus(runErr), Summary: fmt.Sprintf("Gate finished in %s", time.Since(started).Round(time.Millisecond)), Output: string(output), ExitCode: exitCode(runErr), FinishedAt: time.Now()}
}

func scanStatus(err error) string {
	if err == nil {
		return "complete"
	}
	return "findings_or_error"
}
func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}

func importPR(c *gin.Context) {
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
	var rawPR githubPR
	if err := githubGet(c.Request.Context(), path, &rawPR); err != nil {
		respondGitHubError(c, err)
		return
	}
	var rawCommits []githubCommit
	for page := 1; ; page++ {
		var batch []githubCommit
		if err := githubGet(c.Request.Context(), fmt.Sprintf("%s/commits?per_page=100&page=%d", path, page), &batch); err != nil {
			respondGitHubError(c, err)
			return
		}
		rawCommits = append(rawCommits, batch...)
		if len(batch) < 100 {
			break
		}
	}
	created, _ := time.Parse(time.RFC3339, rawPR.CreatedAt)
	updated, _ := time.Parse(time.RFC3339, rawPR.UpdatedAt)
	result := pullRequest{Repository: owner + "/" + repo, Owner: owner, Repo: repo, Number: rawPR.Number, Title: rawPR.Title, State: rawPR.State, URL: rawPR.HTMLURL, CreatedAt: created, UpdatedAt: updated, Commits: make([]commit, 0, len(rawCommits))}
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
}

func respondGitHubError(c *gin.Context, err error) {
	switch {
	case err.Error() == "not found":
		c.JSON(http.StatusNotFound, gin.H{"error": "Pull request not found. Check the URL and confirm the repository is public."})
	case err.Error() == "rate limit":
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "GitHub’s public API rate limit has been reached. Try again later or configure GITHUB_TOKEN."})
	case strings.Contains(err.Error(), "status 5"):
		c.JSON(http.StatusBadGateway, gin.H{"error": "GitHub is having trouble right now. Try again shortly."})
	default:
		c.JSON(http.StatusBadGateway, gin.H{"error": "Could not reach GitHub. Check your connection and try again."})
	}
}

func main() {
	r := gin.Default()
	r.GET("/api/health", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })
	r.POST("/api/import", importPR)
	r.POST("/api/scan", scanRepo)
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
