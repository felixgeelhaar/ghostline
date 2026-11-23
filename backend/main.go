package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/felixgeelhaar/ghostline/backend/internal/domain"
	"github.com/gin-gonic/gin"
)

type pullRequest struct {
	Repository       string             `json:"repository"`
	Owner            string             `json:"owner"`
	Repo             string             `json:"repo"`
	Number           int                `json:"number"`
	Title            string             `json:"title"`
	State            string             `json:"state"`
	Merged           bool               `json:"merged"`
	URL              string             `json:"url"`
	CreatedAt        time.Time          `json:"createdAt"`
	UpdatedAt        time.Time          `json:"updatedAt"`
	Commits          []commit           `json:"commits"`
	Checks           revisionChecks     `json:"checks"`
	Reviews          qodoReviewSet      `json:"qodoReviews"`
	ReferencedIssues referencedIssueSet `json:"referencedIssues"`
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
	Body      string `json:"body"`
	Merged    bool   `json:"merged"`
	HTMLURL   string `json:"html_url"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
	Head      struct {
		SHA string `json:"sha"`
	} `json:"head"`
	Base struct {
		SHA  string `json:"sha"`
		Repo struct {
			Name     string `json:"name"`
			FullName string `json:"full_name"`
			Private  bool   `json:"private"`
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

type api struct {
	gh     *githubClient
	qodo   qodoReviewSource
	logger *slog.Logger
}

func (a api) importPR(c *gin.Context) {
	var body struct {
		URL string `json:"url"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || strings.TrimSpace(body.URL) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Enter a GitHub pull request URL."})
		return
	}
	owner, repo, number, err := parsePullRequestReference(body.URL)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := ensurePublicBeforeDataReads(c.Request.Context(), a.gh, owner, repo); err != nil {
		respondGitHubError(c, err)
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
		if !identity.Equal(domain.RepositoryIdentity{Owner: owner, Name: repo}) {
			if err := ensurePublicBeforeDataReads(c.Request.Context(), a.gh, identity.Owner, identity.Name); err != nil {
				respondGitHubError(c, err)
				return
			}
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
		result := pullRequest{Repository: identity.FullName(), Owner: identity.Owner, Repo: identity.Name, Number: rawPR.Number, Title: rawPR.Title, State: rawPR.State, Merged: rawPR.Merged, URL: rawPR.HTMLURL, CreatedAt: created, UpdatedAt: updated, Commits: make([]commit, 0, len(rawCommits)), Checks: checks, Reviews: fetchQodoReviews(c.Request.Context(), a.gh, identity, rawPR.Number), ReferencedIssues: fetchReferencedIssues(c.Request.Context(), a.gh, identity.Owner, identity.Name, rawPR.Body)}
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
	if err := ensurePublicBeforeDataReads(c.Request.Context(), a.gh, owner, repo); err != nil {
		respondGitHubError(c, err)
		return
	}
	c.JSON(http.StatusOK, fetchRevisionChecks(c.Request.Context(), a.gh, owner, repo, body.SHA))
}

func (a api) loadNoxReport(c *gin.Context) {
	var body struct {
		URL string `json:"url"`
		SHA string `json:"sha"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxAPIRequestBytes)
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Provide a GitHub pull request URL and a full commit SHA."})
		return
	}
	owner, repo, number, err := parsePRURL(body.URL)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if !fullSHA.MatchString(body.SHA) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "The revision must be a full 40-character commit SHA."})
		return
	}
	c.JSON(http.StatusOK, loadNoxReport(c.Request.Context(), a.gh, owner, repo, number, body.SHA))
}

func (a api) loadQodoReview(c *gin.Context) {
	apiKey := c.GetHeader("X-Qodo-API-Key")
	if apiKey == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Enter a Qodo API key to load structured review data."})
		return
	}
	if !secureCredentialOrigin(c.GetHeader("Origin")) {
		c.JSON(http.StatusForbidden, gin.H{"error": "Qodo keys can only be sent from a secure Ghostline origin or localhost."})
		return
	}
	if len(apiKey) > maxQodoAPIKeyLength || strings.ContainsAny(apiKey, "\r\n") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "The Qodo API key is invalid or too long."})
		return
	}
	var body struct {
		URL string `json:"url"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxQodoRequestBytes)
	if err := c.ShouldBindJSON(&body); err != nil || strings.TrimSpace(body.URL) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Provide a GitHub pull request URL."})
		return
	}
	if _, _, _, err := parsePRURL(body.URL); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if a.qodo == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Structured Qodo review access is not configured on this Ghostline instance."})
		return
	}
	result, err := a.qodo.fetch(c.Request.Context(), apiKey, strings.TrimSpace(body.URL))
	if err != nil {
		logger := requestLogger(c.Request.Context(), a.logger)
		attrs := []any{"pull_request", strings.TrimSpace(body.URL), "cause", strings.ReplaceAll(err.Error(), apiKey, "[redacted]")}
		if serviceErr := (qodoServiceError{}); errors.As(err, &serviceErr) {
			attrs = append(attrs, "upstream_status", serviceErr.StatusCode())
		}
		logger.Error("Qodo structured review request failed", attrs...)
		// The request deadline (see newHTTPServer) ended before Qodo answered.
		if errors.Is(err, context.DeadlineExceeded) {
			c.JSON(http.StatusGatewayTimeout, gin.H{"error": "Qodo took too long to answer, so the request was stopped. Try again shortly."})
			return
		}
		var serviceErr qodoServiceError
		if errors.As(err, &serviceErr) {
			switch serviceErr.StatusCode() {
			case http.StatusUnauthorized:
				c.JSON(http.StatusUnauthorized, gin.H{"error": "Qodo rejected this API key; it may be expired or revoked. Create a fresh personal Qodo key, then retry."})
				return
			case http.StatusForbidden:
				c.JSON(http.StatusForbidden, gin.H{"error": "Qodo denied this key's access to the workspace. Check the key's workspace permissions and the pull request's workspace."})
				return
			case http.StatusTooManyRequests:
				c.JSON(http.StatusTooManyRequests, gin.H{"error": "Qodo is rate-limiting structured review requests. Try again later."})
				return
			}
		}
		c.JSON(http.StatusBadGateway, gin.H{"error": "Qodo structured review data could not be loaded. Check the key, workspace access, and pull request, then retry."})
		return
	}
	c.JSON(http.StatusOK, result)
}

func respondGitHubError(c *gin.Context, err error) {
	var statusErr statusError
	switch {
	// The request deadline (see newHTTPServer) ended before GitHub answered.
	case errors.Is(err, context.DeadlineExceeded):
		c.JSON(http.StatusGatewayTimeout, gin.H{"error": "GitHub took too long to answer, so the request was stopped. Try again shortly."})
	case errors.Is(err, errDefaultBranchUnknown):
		c.JSON(http.StatusBadGateway, gin.H{"error": "GitHub did not report the repository's default branch, so readiness cannot be checked."})
	case errors.Is(err, errRepositoryVisibilityUnknown):
		c.JSON(http.StatusBadGateway, gin.H{"error": "Could not confirm that this repository is public; GitHub data was not loaded."})
	// A private repository answers exactly like a missing one: the server token
	// can see it, and a distinct reply would let callers probe for its existence.
	case errors.Is(err, errNotFound), errors.Is(err, errPrivateRepository):
		c.JSON(http.StatusNotFound, gin.H{"error": "Pull request not found. Check the URL and confirm the repository is public."})
	case errors.Is(err, errServerTokenRejected):
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "The server’s GitHub token was rejected, so repository visibility could not be checked. Replace the configured token."})
	case errors.Is(err, errServerTokenRateLimit):
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "The server’s GitHub token has reached its rate limit, so repository visibility could not be checked. Try again later."})
	case errors.Is(err, errUserTokenScope):
		c.JSON(http.StatusBadGateway, gin.H{"error": "GitHub reported that your sign-in grants more than public read access, so Ghostline did not use the reply and now reads GitHub without it. Try again."})
	case errors.Is(err, errRateLimit):
		message, wait := githubRateLimitMessage(err, "GitHub’s public API rate limit has been reached. Try again later.")
		if wait > 0 {
			c.Header("Retry-After", strconv.Itoa(retryAfterSeconds(wait)))
		}
		c.JSON(http.StatusTooManyRequests, gin.H{"error": message})
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
	return newRouterWithQodoAndLimiter(gh, nil, limiter, identity)
}

func newRouterWithQodoAndLimiter(gh *githubClient, qodo qodoReviewSource, limiter *rateLimiter, identity clientIdentity) *gin.Engine {
	return newRouterWithQodoLogger(gh, qodo, limiter, identity, slog.Default())
}

func newRouterWithQodoLogger(gh *githubClient, qodo qodoReviewSource, limiter *rateLimiter, identity clientIdentity, logger *slog.Logger) *gin.Engine {
	return newRouterWithAuth(gh, qodo, limiter, identity, logger, nil)
}

func newRouterWithAuth(gh *githubClient, qodo qodoReviewSource, limiter *rateLimiter, identity clientIdentity, logger *slog.Logger, auth *authManager) *gin.Engine {
	// OAuth callback URLs contain one-time credentials in their query string.
	// Keep that path out of access logs while retaining request logging elsewhere.
	if logger == nil {
		logger = slog.Default()
	}
	r := gin.New()
	r.Use(requestLogging(logger, identity), gin.Recovery())
	r.GET("/api/health", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })
	if auth == nil {
		auth = newAuthManager(authConfig{}, nil)
	}
	r.GET("/api/session", auth.session)
	oauthLoginLimiter := newRateLimiter(limiter.limit, limiter.window, limiter.now)
	oauthCallbackLimiter := newRateLimiter(limiter.limit, limiter.window, limiter.now)
	r.POST("/auth/login", rateLimitMiddleware(oauthLoginLimiter, identity), auth.login)
	r.GET("/auth/callback", rateLimitMiddleware(oauthCallbackLimiter, identity), auth.callback)
	r.POST("/auth/logout", auth.logout)
	accountLimiter := newRateLimiter(limiter.limit, limiter.window, limiter.now)
	// The body limit runs after both rate limits, so a refused oversized request still uses up allowance.
	limited := r.Group("/api", rateLimitMiddleware(limiter, identity), auth.middleware(), accountRateLimitMiddleware(accountLimiter), limitRequestBody(maxAPIRequestBytes))
	client := api{gh: gh, qodo: qodo, logger: logger}
	limited.POST("/import", client.importPR)
	limited.POST("/checks", client.loadRevisionChecks)
	limited.POST("/nox-report", client.loadNoxReport)
	limited.POST("/qodo-review", client.loadQodoReview)
	limited.POST("/check-annotations", client.loadCheckAnnotations)
	limited.POST("/readiness", client.loadReadiness)
	searchLimiter := newRateLimiter(githubSearchesPerWindow, time.Minute, limiter.now)
	limited.POST("/pr-search", client.searchPullRequests(searchLimiter))
	return r
}

func main() {
	if len(os.Args) == 2 && os.Args[1] == "healthcheck" {
		if err := runContainerHealthcheck(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	logger := newLogger(os.Getenv, os.Stderr)
	slog.SetDefault(logger)

	cfg, err := loadRateLimitConfig(os.Getenv)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	serverCfg, err := loadServerConfig(os.Getenv)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	auth, err := loadAuthConfig(os.Getenv)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	staticDirectory, err := loadStaticSiteConfig(os.Getenv)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	r := newRouterWithAuth(newGitHubClient(), newQodoReviewClient(), newRateLimiter(cfg.Requests, cfg.Window, time.Now), cfg.Identity, logger, newAuthManager(auth, nil))
	if err := registerStaticSite(r, staticDirectory); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	host := os.Getenv("HOST")
	if host == "" {
		host = "127.0.0.1"
	}
	address := joinHostPort(host, port)
	listener, err := net.Listen("tcp", address)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	signals := make(chan os.Signal, 2)
	signal.Notify(signals, syscall.SIGTERM, os.Interrupt)
	logger.Info("serving HTTP", "address", listener.Addr().String())
	if err := serveUntilSignal(newHTTPServer(address, r, serverCfg), listener, signals, serverCfg.ShutdownGrace, logger); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
