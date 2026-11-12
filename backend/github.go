package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

var (
	errNotFound                    = errors.New("not found")
	errRateLimit                   = errors.New("rate limit")
	errPrivateRepository           = errors.New("private repository is not supported")
	errRepositoryVisibilityUnknown = errors.New("repository visibility is missing")
)

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
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, g.baseURL+path, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "Ghostline-Day1")
	if g.token != "" {
		req.Header.Set("Authorization", "Bearer "+g.token)
	}
	resp, err := g.http.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusNotFound {
			return false, errNotFound
		}
		if resp.StatusCode == http.StatusTooManyRequests || (resp.StatusCode == http.StatusForbidden && (resp.Header.Get("X-RateLimit-Remaining") == "0" || resp.Header.Get("Retry-After") != "")) {
			return false, errRateLimit
		}
		return false, statusError{code: resp.StatusCode}
	}
	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		return false, err
	}
	return hasNextLink(resp.Header.Values("Link")), nil
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
	path := fmt.Sprintf("/repos/%s/%s", url.PathEscape(owner), url.PathEscape(repo))
	var metadata struct {
		Private *bool `json:"private"`
	}
	if err := gh.get(ctx, path, &metadata); err != nil {
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
