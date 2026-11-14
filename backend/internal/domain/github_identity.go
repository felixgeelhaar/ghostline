package domain

import (
	"fmt"
	"strconv"
	"strings"
)

// GitHubAccountAllowlist contains immutable GitHub account IDs allowed to use
// a hosted Ghostline instance. Usernames are mutable and are never authority.
type GitHubAccountAllowlist map[int64]struct{}

func NewGitHubAccountAllowlist(values []string) (GitHubAccountAllowlist, error) {
	allowed := make(GitHubAccountAllowlist, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		id, err := strconv.ParseInt(value, 10, 64)
		if err != nil || id <= 0 || strconv.FormatInt(id, 10) != value {
			return nil, fmt.Errorf("invalid GitHub account ID %q", value)
		}
		allowed[id] = struct{}{}
	}
	if len(allowed) == 0 {
		return nil, fmt.Errorf("at least one GitHub account ID is required")
	}
	return allowed, nil
}

func (a GitHubAccountAllowlist) Allows(id int64) bool {
	if id <= 0 {
		return false
	}
	_, ok := a[id]
	return ok
}
