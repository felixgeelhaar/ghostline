package domain

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var githubLoginPattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,37}[A-Za-z0-9])?(?:_[A-Za-z0-9]{3,8})?$`)

// ValidGitHubLogin accepts GitHub usernames, including Enterprise Managed User
// usernames with the underscore and enterprise shortcode suffix.
func ValidGitHubLogin(login string) bool {
	return len(login) > 0 && len(login) <= 39 && githubLoginPattern.MatchString(login)
}

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
