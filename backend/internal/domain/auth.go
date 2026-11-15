package domain

import (
	"errors"
	"regexp"
)

var githubLoginPattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,37}[A-Za-z0-9])?$`)

// GitHubIdentity contains only the stable public identity needed for Ghostline
// access control. OAuth access tokens are deliberately not part of this model.
type GitHubIdentity struct {
	ID    int64
	Login string
}

func NewGitHubIdentity(id int64, login string) (GitHubIdentity, error) {
	if id <= 0 || !githubLoginPattern.MatchString(login) {
		return GitHubIdentity{}, errors.New("GitHub returned an invalid user identity")
	}
	return GitHubIdentity{ID: id, Login: login}, nil
}

func GitHubIdentityAllowed(id int64, allowed map[int64]struct{}) bool {
	_, ok := allowed[id]
	return ok
}
