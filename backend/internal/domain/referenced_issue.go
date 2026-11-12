package domain

import (
	"regexp"
	"sort"
	"strconv"
)

var (
	bareIssueReference = regexp.MustCompile(`(?:^|[^A-Za-z0-9_/#])#([1-9][0-9]*)(?:$|[^A-Za-z0-9_])`)
	issueURLReference  = regexp.MustCompile(`(?i)(?:^|[^A-Za-z0-9_./-])https://github\.com/([A-Za-z0-9_.-]+)/([A-Za-z0-9_.-]+)/issues/([1-9][0-9]*)(?:$|[^A-Za-z0-9_/-])`)
)

type positionedIssueReference struct {
	position int
	number   int
}

// ParseIssueReferences returns each distinct same-repository issue reference
// in textual order. It intentionally handles only bare #N references and
// canonical GitHub issue URLs; it does not infer ownership from other-repo
// shorthand or treat pull-request URLs as issues.
func ParseIssueReferences(body string, repository RepositoryIdentity) []int {
	if body == "" || repository.Owner == "" || repository.Name == "" {
		return []int{}
	}
	references := make([]positionedIssueReference, 0)
	for _, match := range bareIssueReference.FindAllStringSubmatchIndex(body, -1) {
		if number, err := strconv.Atoi(body[match[2]:match[3]]); err == nil {
			references = append(references, positionedIssueReference{position: match[2], number: number})
		}
	}
	for _, match := range issueURLReference.FindAllStringSubmatchIndex(body, -1) {
		identity := RepositoryIdentity{Owner: body[match[2]:match[3]], Name: body[match[4]:match[5]]}
		if !repository.Equal(identity) {
			continue
		}
		if number, err := strconv.Atoi(body[match[6]:match[7]]); err == nil {
			references = append(references, positionedIssueReference{position: match[6], number: number})
		}
	}
	sort.Slice(references, func(i, j int) bool { return references[i].position < references[j].position })
	result := make([]int, 0, len(references))
	seen := make(map[int]struct{}, len(references))
	for _, reference := range references {
		if _, ok := seen[reference.number]; ok {
			continue
		}
		seen[reference.number] = struct{}{}
		result = append(result, reference.number)
	}
	return result
}
