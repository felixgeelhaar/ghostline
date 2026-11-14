package domain

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var (
	issueReferencePattern = regexp.MustCompile(`(?:^|[^\w])#([1-9][0-9]*)\b`)
	issueURLPattern       = regexp.MustCompile(`(?i)https://github\.com/([^/\s?#]+)/([^/\s?#]+)/issues/([1-9][0-9]*)([^[:alnum:]_/-]|$)`)
	urlTokenPattern       = regexp.MustCompile(`(?i)https?://[^\s<>()]+`)
)

type issueReference struct {
	position int
	number   int
}

// ReferencedIssueNumbers extracts unique issue references from a pull request
// description. Full GitHub URLs are accepted only for the same repository.
func ReferencedIssueNumbers(body, owner, repository string) []int {
	references := make([]issueReference, 0)
	urlSpans := urlTokenPattern.FindAllStringIndex(body, -1)
	for _, match := range issueReferencePattern.FindAllStringSubmatchIndex(body, -1) {
		if insideURL(match[2], urlSpans) {
			continue
		}
		number, err := strconv.Atoi(body[match[2]:match[3]])
		if err == nil {
			references = append(references, issueReference{position: match[0], number: number})
		}
	}
	for _, match := range issueURLPattern.FindAllStringSubmatchIndex(body, -1) {
		if !strings.EqualFold(body[match[2]:match[3]], owner) || !strings.EqualFold(body[match[4]:match[5]], repository) {
			continue
		}
		number, err := strconv.Atoi(body[match[6]:match[7]])
		if err == nil {
			references = append(references, issueReference{position: match[0], number: number})
		}
	}
	sort.SliceStable(references, func(i, j int) bool { return references[i].position < references[j].position })

	seen := make(map[int]struct{}, len(references))
	numbers := make([]int, 0, len(references))
	for _, reference := range references {
		if _, ok := seen[reference.number]; ok {
			continue
		}
		seen[reference.number] = struct{}{}
		numbers = append(numbers, reference.number)
	}
	return numbers
}

func insideURL(position int, spans [][]int) bool {
	for _, span := range spans {
		if position >= span[0] && position < span[1] {
			return true
		}
	}
	return false
}
