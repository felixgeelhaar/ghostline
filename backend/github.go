package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"
)

var (
	errNotFound  = errors.New("not found")
	errRateLimit = errors.New("rate limit")
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
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, g.baseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "Ghostline-Day1")
	if g.token != "" {
		req.Header.Set("Authorization", "Bearer "+g.token)
	}
	resp, err := g.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusNotFound {
			return errNotFound
		}
		if resp.StatusCode == http.StatusTooManyRequests || (resp.StatusCode == http.StatusForbidden && resp.Header.Get("X-RateLimit-Remaining") == "0") {
			return errRateLimit
		}
		return statusError{code: resp.StatusCode}
	}
	return json.NewDecoder(resp.Body).Decode(target)
}
