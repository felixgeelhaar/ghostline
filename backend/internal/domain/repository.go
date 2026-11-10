package domain

import (
	"errors"
	"strings"
)

var ErrInvalidRepositoryIdentity = errors.New("repository owner and name are required")

// RepositoryIdentity is the canonical identity used to correlate a pull
// request with its source repository across GitHub snapshots.
type RepositoryIdentity struct {
	Owner string
	Name  string
}

func NewRepositoryIdentity(owner, name string) (RepositoryIdentity, error) {
	owner = strings.TrimSpace(owner)
	name = strings.TrimSpace(name)
	if owner == "" || name == "" {
		return RepositoryIdentity{}, ErrInvalidRepositoryIdentity
	}
	return RepositoryIdentity{Owner: owner, Name: name}, nil
}

func (r RepositoryIdentity) FullName() string {
	return r.Owner + "/" + r.Name
}

func (r RepositoryIdentity) Equal(other RepositoryIdentity) bool {
	return strings.EqualFold(r.Owner, other.Owner) && strings.EqualFold(r.Name, other.Name)
}
