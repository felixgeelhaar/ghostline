package domain

import "strings"

// GitHubAccountAllowlist contains the GitHub logins that may use a hosted
// Ghostline instance. Login matching follows GitHub's case-insensitive names.
type GitHubAccountAllowlist map[string]struct{}

func NewGitHubAccountAllowlist(logins []string) GitHubAccountAllowlist {
	allowed := make(GitHubAccountAllowlist, len(logins))
	for _, login := range logins {
		login = strings.ToLower(strings.TrimSpace(login))
		if login != "" {
			allowed[login] = struct{}{}
		}
	}
	return allowed
}

func (a GitHubAccountAllowlist) Allows(login string) bool {
	login = strings.ToLower(strings.TrimSpace(login))
	if login == "" {
		return false
	}
	_, ok := a[login]
	return ok
}
