package main

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/felixgeelhaar/ghostline/backend/internal/domain"
	"github.com/gin-gonic/gin"
)

// openPullLimit bounds one listing to a single GitHub page. Readiness is checked per
// pull request afterwards, and each check costs several GitHub requests, so a longer
// list would only run into the rate limit.
const openPullLimit = 30

var (
	repositoryShorthand = regexp.MustCompile(`^([A-Za-z0-9](?:[A-Za-z0-9-]{0,38}))/([A-Za-z0-9._-]{1,100})$`)
	repositoryURLPath   = regexp.MustCompile(`^/([A-Za-z0-9](?:[A-Za-z0-9-]{0,38}))/([A-Za-z0-9._-]{1,100})/?$`)
)

var errOpenPullUnidentified = errors.New("GitHub returned an open pull request that Ghostline cannot tie to this repository and a head commit.")

// openPullRequest is one open pull request and the head GitHub reported for it when
// the list was read. A readiness verdict for it counts only for that head.
type openPullRequest struct {
	Number       int    `json:"number"`
	Title        string `json:"title"`
	Draft        bool   `json:"draft"`
	Author       string `json:"author"`
	HeadSHA      string `json:"headSha"`
	HeadShortSHA string `json:"headShortSha"`
	URL          string `json:"url"`
}

// openPullsResponse is one page of a repository's open pull requests. Complete is
// false when GitHub has more open pull requests than this page holds.
type openPullsResponse struct {
	Repository string            `json:"repository"`
	Complete   bool              `json:"complete"`
	Limit      int               `json:"limit"`
	Items      []openPullRequest `json:"items"`
}

type githubOpenPull struct {
	Number  int    `json:"number"`
	Title   string `json:"title"`
	State   string `json:"state"`
	Draft   bool   `json:"draft"`
	HTMLURL string `json:"html_url"`
	User    struct {
		Login string `json:"login"`
	} `json:"user"`
	Head struct {
		SHA string `json:"sha"`
	} `json:"head"`
	Base struct {
		Repo struct {
			Name  string `json:"name"`
			Owner struct {
				Login string `json:"login"`
			} `json:"owner"`
		} `json:"repo"`
	} `json:"base"`
}

// parseRepositoryReference accepts owner/repo or https://github.com/owner/repo.
func parseRepositoryReference(raw string) (domain.RepositoryIdentity, error) {
	invalid := errors.New("Enter a public GitHub repository as owner/repo or https://github.com/owner/repo.")
	trimmed := strings.TrimSpace(raw)
	matches := repositoryShorthand.FindStringSubmatch(trimmed)
	if matches == nil {
		u, err := url.Parse(trimmed)
		if err != nil || u.Scheme != "https" || !strings.EqualFold(u.Host, "github.com") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return domain.RepositoryIdentity{}, invalid
		}
		if matches = repositoryURLPath.FindStringSubmatch(u.Path); matches == nil {
			return domain.RepositoryIdentity{}, invalid
		}
	}
	if matches[2] == "." || matches[2] == ".." {
		return domain.RepositoryIdentity{}, invalid
	}
	return domain.NewRepositoryIdentity(matches[1], matches[2])
}

// respondOpenPullsError names the repository when GitHub cannot find it. A private
// repository answers exactly like a missing one, as everywhere else in Ghostline.
func respondOpenPullsError(c *gin.Context, err error) {
	if errors.Is(err, errNotFound) || errors.Is(err, errPrivateRepository) {
		c.JSON(http.StatusNotFound, gin.H{"error": "Repository not found. Check the name and confirm the repository is public."})
		return
	}
	respondGitHubError(c, err)
}

// listOpenPulls reads one page of a public repository's open pull requests. It reports
// each one's current head and nothing else; readiness is checked per pull request.
func (a api) listOpenPulls(c *gin.Context) {
	var body struct {
		Repository string `json:"repository"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Enter a public GitHub repository as owner/repo."})
		return
	}
	repo, err := parseRepositoryReference(body.Repository)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ctx := c.Request.Context()
	if err := ensurePublicBeforeDataReads(ctx, a.gh, repo.Owner, repo.Name); err != nil {
		respondOpenPullsError(c, err)
		return
	}
	var raw []githubOpenPull
	path := fmt.Sprintf("/repos/%s/%s/pulls?state=open&sort=created&direction=desc&per_page=%d&page=1", url.PathEscape(repo.Owner), url.PathEscape(repo.Name), openPullLimit)
	hasMore, err := a.gh.getWithNext(ctx, path, &raw)
	if err != nil {
		respondOpenPullsError(c, err)
		return
	}
	response := openPullsResponse{Repository: repo.FullName(), Complete: !hasMore && len(raw) <= openPullLimit, Limit: openPullLimit, Items: make([]openPullRequest, 0, len(raw))}
	for _, item := range raw {
		pull, err := openPullFromGitHub(repo, item)
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
			return
		}
		response.Items = append(response.Items, pull)
	}
	c.JSON(http.StatusOK, response)
}

// openPullFromGitHub keeps a pull request only when GitHub ties it to this repository,
// names its full head commit and links it at its canonical address.
func openPullFromGitHub(repo domain.RepositoryIdentity, item githubOpenPull) (openPullRequest, error) {
	base, err := domain.NewRepositoryIdentity(item.Base.Repo.Owner.Login, item.Base.Repo.Name)
	if err != nil || !base.Equal(repo) || item.Number < 1 || item.State != "open" || !fullSHA.MatchString(item.Head.SHA) {
		return openPullRequest{}, errOpenPullUnidentified
	}
	canonical := fmt.Sprintf("https://github.com/%s/%s/pull/%d", repo.Owner, repo.Name, item.Number)
	if !strings.EqualFold(item.HTMLURL, canonical) {
		return openPullRequest{}, errOpenPullUnidentified
	}
	return openPullRequest{Number: item.Number, Title: item.Title, Draft: item.Draft, Author: item.User.Login, HeadSHA: item.Head.SHA, HeadShortSHA: item.Head.SHA[:7], URL: item.HTMLURL}, nil
}
