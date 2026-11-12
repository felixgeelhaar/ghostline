package domain

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var (
	bareIssueReference = regexp.MustCompile(`#([1-9][0-9]*)`)
	issueURLReference  = regexp.MustCompile(`(?i)https://github\.com/([A-Za-z0-9_.-]+)/([A-Za-z0-9_.-]+)/issues/([1-9][0-9]*)/?`)
	markdownLink       = regexp.MustCompile(`\[([^\]]*)\]\(([^)]*)\)`)
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
	bareText := []byte(body)
	for _, match := range markdownLink.FindAllStringSubmatchIndex(body, -1) {
		for i := match[2]; i < match[3]; i++ {
			if bareText[i] != '\n' && bareText[i] != '\r' {
				bareText[i] = ' '
			}
		}
	}
	for _, match := range bareIssueReference.FindAllStringSubmatchIndex(string(bareText), -1) {
		start, end := match[0], match[1]
		if (start > 0 && (isIssueTokenByte(body[start-1]) || body[start-1] == '/')) || (end < len(body) && isIssueTokenByte(body[end])) {
			continue
		}
		if number, err := strconv.Atoi(body[match[2]:match[3]]); err == nil {
			references = append(references, positionedIssueReference{position: start, number: number})
		}
	}
	for _, match := range issueURLReference.FindAllStringSubmatchIndex(body, -1) {
		start, end := match[0], match[1]
		if start > 0 && (isIssueTokenByte(body[start-1]) || strings.ContainsRune("./-", rune(body[start-1]))) {
			continue
		}
		if end < len(body) && isIssueTokenByte(body[end]) {
			continue
		}
		identity := RepositoryIdentity{Owner: body[match[2]:match[3]], Name: body[match[4]:match[5]]}
		if !repository.Equal(identity) {
			continue
		}
		if number, err := strconv.Atoi(body[match[6]:match[7]]); err == nil {
			references = append(references, positionedIssueReference{position: start, number: number})
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

func isIssueTokenByte(value byte) bool {
	return value >= 'A' && value <= 'Z' || value >= 'a' && value <= 'z' || value >= '0' && value <= '9' || value == '_'
}
