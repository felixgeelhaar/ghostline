package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

var (
	errNotFound  = errors.New("not found")
	errRateLimit = errors.New("rate limit")
	// errServerTokenRateLimit means the configured server token, used only for
	// visibility checks, is out of quota; the public API limit may still be fine.
	errServerTokenRateLimit = errors.New("server token rate limit")
	// errServerTokenRejected means GitHub refused the configured server token
	// (expired or revoked), so repository visibility cannot be checked.
	errServerTokenRejected         = errors.New("server token rejected")
	errPrivateRepository           = errors.New("private repository is not supported")
	errRepositoryVisibilityUnknown = errors.New("repository visibility is missing")
)

// maxGitHubResponseBytes guards against a pathological GitHub reply. Replies are decoded
// as they stream in, so this is a ceiling, not a buffer. The largest page Ghostline
// reads is 100 issue comments of at most 65,536 characters; even if every character
// were escaped as \uXXXX that is about 39 MB, which fits.
const maxGitHubResponseBytes = 64 << 20

var errGitHubResponseTooLarge = errors.New("GitHub API response exceeded the size limit")

// errUserTokenScope means GitHub reported that the signed-in user's token grants more
// than read:user, so it might read private data. The response is discarded.
var errUserTokenScope = errors.New("user token grants more than read:user")

// githubRateLimitError is a GitHub rate-limit reply. It matches errRateLimit with
// errors.Is, and records whose quota ran out and when GitHub says it resets.
type githubRateLimitError struct {
	userToken bool
	resetAt   time.Time
}

func (e githubRateLimitError) Error() string     { return "GitHub API rate limit reached" }
func (e githubRateLimitError) Is(err error) bool { return err == errRateLimit }

// githubRateLimitMessage describes a rate limit for the person who hit it, naming
// GitHub's reset time when the reply carried one.
func githubRateLimitMessage(err error, fallback string) (string, time.Duration) {
	var limit githubRateLimitError
	if !errors.As(err, &limit) || !limit.userToken {
		return fallback, 0
	}
	if limit.resetAt.IsZero() {
		return "GitHub’s API rate limit for your GitHub account has been reached. Try again later.", 0
	}
	return fmt.Sprintf("GitHub’s API rate limit for your GitHub account has been reached. GitHub resets it at %s.", limit.resetAt.UTC().Format("15:04 UTC")), time.Until(limit.resetAt)
}

type githubUserTokenKey struct{}

// githubUserCredential is the signed-in user's GitHub token for one request, with
// a way to stop using it for the rest of the session.
type githubUserCredential struct {
	token  string
	revoke func()
}

// withGitHubUserToken attaches the signed-in user's GitHub token to a request
// context. Public data reads made with that context use it instead of reading
// anonymously. revoke drops the token from the session.
func withGitHubUserToken(ctx context.Context, token string, revoke func()) context.Context {
	return context.WithValue(ctx, githubUserTokenKey{}, githubUserCredential{token: token, revoke: revoke})
}

func githubUserToken(ctx context.Context) githubUserCredential {
	credential, _ := ctx.Value(githubUserTokenKey{}).(githubUserCredential)
	return credential
}

type statusError struct{ code int }

func (e statusError) Error() string { return fmt.Sprintf("GitHub API returned status %d", e.code) }

type githubClient struct {
	baseURL string
	token   string
	http    *http.Client
}

func newGitHubClient() *githubClient {
	return &githubClient{baseURL: "https://api.github.com", token: os.Getenv("GITHUB_TOKEN"), http: &http.Client{Timeout: 12 * time.Second}}
}

func (g *githubClient) get(ctx context.Context, path string, target any) error {
	_, err := g.getWithNext(ctx, path, target)
	return err
}

func (g *githubClient) getWithNext(ctx context.Context, path string, target any) (bool, error) {
	return g.getWithNextUsingToken(ctx, path, target, false)
}

// getWithServerToken is reserved for repository visibility metadata. Public PR
// data is fetched without the server credential, either anonymously or with the
// signed-in user's read:user token, so a visibility change after this check cannot
// turn Ghostline into a private-data proxy.
func (g *githubClient) getWithServerToken(ctx context.Context, path string, target any) error {
	_, err := g.getWithNextUsingToken(ctx, path, target, true)
	return err
}

func (g *githubClient) getWithNextUsingToken(ctx context.Context, path string, target any, useToken bool) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, g.baseURL+path, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "Ghostline-Day1")
	var user githubUserCredential
	if useToken {
		if g.token != "" {
			req.Header.Set("Authorization", "Bearer "+g.token)
		}
	} else if user = githubUserToken(ctx); user.token != "" {
		req.Header.Set("Authorization", "Bearer "+user.token)
	}
	userToken := user.token
	resp, err := g.http.Do(req)
	if err != nil {
		logUpstreamFailure(ctx, "github", path, 0, transportFailure(ctx, err))
		return false, err
	}
	defer resp.Body.Close()
	if userToken != "" && !publicReadScopes(reportedScopes(resp.Header)) {
		// Stop using this token for the session; later reads fall back to anonymous.
		logUpstreamFailure(ctx, "github", path, resp.StatusCode, "user_token_scope")
		if user.revoke != nil {
			user.revoke()
		}
		return false, errUserTokenScope
	}
	if resp.StatusCode != http.StatusOK {
		logUpstreamFailure(ctx, "github", path, resp.StatusCode, "status")
		if resp.StatusCode == http.StatusNotFound {
			return false, errNotFound
		}
		if resp.StatusCode == http.StatusTooManyRequests || (resp.StatusCode == http.StatusForbidden && (resp.Header.Get("X-RateLimit-Remaining") == "0" || resp.Header.Get("Retry-After") != "")) {
			return false, githubRateLimitError{userToken: userToken != "", resetAt: rateLimitReset(resp.Header)}
		}
		return false, statusError{code: resp.StatusCode}
	}
	// Decode as the body streams in. The recorder tells a body that stopped arriving
	// (transport) from one that arrived but does not decode (invalid_response).
	limited := &io.LimitedReader{R: resp.Body, N: maxGitHubResponseBytes + 1}
	body := &readErrorRecorder{r: limited}
	if err := json.NewDecoder(body).Decode(target); err != nil {
		switch {
		case body.err != nil:
			logUpstreamFailure(ctx, "github", path, resp.StatusCode, bodyFailure(ctx, body.err))
		case limited.N <= 0:
			logUpstreamFailure(ctx, "github", path, resp.StatusCode, "invalid_response")
			return false, errGitHubResponseTooLarge
		default:
			logUpstreamFailure(ctx, "github", path, resp.StatusCode, "invalid_response")
		}
		return false, err
	}
	return hasNextLink(resp.Header.Values("Link")), nil
}

// reportedScopes returns the scopes GitHub says a token has, or missingScope when
// the reply does not say.
func reportedScopes(header http.Header) string {
	values := header.Values("X-OAuth-Scopes")
	if len(values) == 0 {
		return missingScope
	}
	return strings.Join(values, ",")
}

// rateLimitReset reads when GitHub says the request may be retried. Retry-After
// (seconds) answers a secondary limit and wins when present; X-RateLimit-Reset
// (epoch seconds) applies only when the primary quota is exhausted.
func rateLimitReset(header http.Header) time.Time {
	if seconds, err := strconv.Atoi(header.Get("Retry-After")); err == nil && seconds > 0 {
		return time.Now().Add(time.Duration(seconds) * time.Second)
	}
	if header.Get("X-RateLimit-Remaining") != "0" {
		return time.Time{}
	}
	if epoch, err := strconv.ParseInt(header.Get("X-RateLimit-Reset"), 10, 64); err == nil && epoch > 0 {
		return time.Unix(epoch, 0)
	}
	return time.Time{}
}

func hasNextLink(headers []string) bool {
	for _, header := range headers {
		for _, link := range splitLinkValues(header) {
			if linkHasRelation(link, "next") {
				return true
			}
		}
	}
	return false
}

// splitLinkValues splits a Link header at commas that separate link-values,
// while preserving commas inside angle-bracketed URIs and quoted parameters.
func splitLinkValues(header string) []string {
	return splitLinkParts(header, ',')
}

func splitLinkParts(header string, separator rune) []string {
	var values []string
	start := 0
	quoted, escaped, inURI := false, false, false
	for i, r := range header {
		switch {
		case escaped:
			escaped = false
		case quoted && r == '\\':
			escaped = true
		case r == '"':
			quoted = !quoted
		case !quoted && r == '<':
			inURI = true
		case !quoted && r == '>':
			inURI = false
		case !quoted && !inURI && r == separator:
			values = append(values, header[start:i])
			start = i + 1
		}
	}
	return append(values, header[start:])
}

func linkHasRelation(link, relation string) bool {
	parts := splitLinkParts(link, ';')
	for _, param := range parts[1:] {
		key, value, ok := strings.Cut(strings.TrimSpace(param), "=")
		if !ok || !strings.EqualFold(strings.TrimSpace(key), "rel") {
			continue
		}
		value = strings.TrimSpace(value)
		if unquoted, err := strconv.Unquote(value); err == nil {
			value = unquoted
		}
		for _, rel := range strings.Fields(value) {
			if strings.EqualFold(rel, relation) {
				return true
			}
		}
	}
	return false
}

func ensurePublicRepository(ctx context.Context, gh *githubClient, owner, repo string) error {
	// Without a server credential GitHub will not return private repository
	// data. Avoid an extra API call on the public-only unauthenticated path.
	if gh.token == "" {
		return nil
	}
	// The data reads carry the user's read:user token, whose scopes are checked on
	// every reply; it cannot read private repositories. Leave the shared server
	// quota to requests without one.
	if githubUserToken(ctx).token != "" {
		return nil
	}
	path := fmt.Sprintf("/repos/%s/%s", url.PathEscape(owner), url.PathEscape(repo))
	var metadata struct {
		Private *bool `json:"private"`
	}
	if err := gh.getWithServerToken(ctx, path, &metadata); err != nil {
		if errors.Is(err, errRateLimit) {
			return errServerTokenRateLimit
		}
		var statusErr statusError
		if errors.As(err, &statusErr) && statusErr.code == http.StatusUnauthorized {
			return errServerTokenRejected
		}
		return err
	}
	if metadata.Private == nil {
		return errRepositoryVisibilityUnknown
	}
	if *metadata.Private {
		return errPrivateRepository
	}
	return nil
}
