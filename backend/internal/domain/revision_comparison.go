package domain

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

var (
	ErrComparisonRevisionsNotAncestor = errors.New("candidate comparison requires the earlier revision to be an ancestor of the candidate revision")
	ErrInvalidRevisionComparison      = errors.New("GitHub returned invalid revision comparison evidence")
	ErrUnrecognizedComparisonStatus   = errors.New("GitHub returned an unrecognized comparison status")
	ErrInvalidComparisonCounts        = errors.New("GitHub returned invalid comparison counts")
	ErrInvalidComparisonSourceLink    = errors.New("GitHub returned an invalid comparison source link")
	ErrInvalidChangedFileDetails      = errors.New("GitHub returned invalid changed-file details")
	ErrInvalidChangedFileSourceLink   = errors.New("GitHub returned an invalid changed-file source link")
)

var comparisonFullSHA = regexp.MustCompile(`^[0-9a-fA-F]{40}$`)

// RevisionComparisonSource is the untrusted comparison response returned by GitHub.
type RevisionComparisonSource struct {
	Status       string                         `json:"status"`
	AheadBy      int                            `json:"ahead_by"`
	BehindBy     int                            `json:"behind_by"`
	TotalCommits int                            `json:"total_commits"`
	HTMLURL      string                         `json:"html_url"`
	Base         RevisionComparisonCommitSource `json:"base_commit"`
	Head         RevisionComparisonCommitSource `json:"head_commit"`
	Files        []RevisionComparisonFileSource `json:"files"`
}

type RevisionComparisonCommitSource struct {
	SHA string `json:"sha"`
}

type RevisionComparisonFileSource struct {
	Path      string `json:"filename"`
	Status    string `json:"status"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	Changes   int    `json:"changes"`
	BlobURL   string `json:"blob_url"`
}

// RevisionComparison is validated, source-linked candidate diff context.
type RevisionComparison struct {
	BaseSHA        string                   `json:"baseSha"`
	HeadSHA        string                   `json:"headSha"`
	CompareURL     string                   `json:"compareUrl"`
	Status         string                   `json:"status"`
	AheadBy        int                      `json:"aheadBy"`
	BehindBy       int                      `json:"behindBy"`
	TotalCommits   int                      `json:"totalCommits"`
	Files          []RevisionComparisonFile `json:"files"`
	FilesTruncated bool                     `json:"filesTruncated"`
}

type RevisionComparisonFile struct {
	Path      string `json:"path"`
	Status    string `json:"status"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	Changes   int    `json:"changes"`
	URL       string `json:"url"`
}

const GitHubCompareFileLimit = 300

// NewRevisionComparison validates GitHub's response against the requested revisions
// before returning any data for display. The compare endpoint's three-dot diff is
// only unambiguous for this UI when base is an ancestor of head.
func NewRevisionComparison(owner, repo, baseSHA, headSHA string, source RevisionComparisonSource) (RevisionComparison, error) {
	if !comparisonFullSHA.MatchString(baseSHA) || !comparisonFullSHA.MatchString(headSHA) || strings.EqualFold(baseSHA, headSHA) {
		return RevisionComparison{}, ErrInvalidRevisionComparison
	}
	if !strings.EqualFold(source.Base.SHA, baseSHA) || (source.Head.SHA != "" && !strings.EqualFold(source.Head.SHA, headSHA)) {
		return RevisionComparison{}, ErrInvalidRevisionComparison
	}
	if source.Status != "ahead" && source.Status != "behind" && source.Status != "identical" && source.Status != "diverged" {
		return RevisionComparison{}, ErrUnrecognizedComparisonStatus
	}
	if source.Status != "ahead" || source.BehindBy != 0 {
		return RevisionComparison{}, ErrComparisonRevisionsNotAncestor
	}
	if source.AheadBy < 0 || source.BehindBy < 0 || source.TotalCommits < 0 {
		return RevisionComparison{}, ErrInvalidComparisonCounts
	}
	expectedComparePath := fmt.Sprintf("/%s/%s/compare/%s...%s", owner, repo, strings.ToLower(baseSHA), strings.ToLower(headSHA))
	if !validComparisonURL(source.HTMLURL, expectedComparePath) {
		return RevisionComparison{}, ErrInvalidComparisonSourceLink
	}

	files := make([]RevisionComparisonFile, 0, len(source.Files))
	for _, file := range source.Files {
		if strings.TrimSpace(file.Path) == "" || strings.HasPrefix(file.Path, "/") || file.Additions < 0 || file.Deletions < 0 || file.Changes < 0 {
			return RevisionComparison{}, ErrInvalidChangedFileDetails
		}
		revisionSHA := headSHA
		switch file.Status {
		case "added", "modified", "renamed", "copied", "changed", "unchanged":
		case "removed":
			revisionSHA = baseSHA
		default:
			return RevisionComparison{}, ErrInvalidChangedFileDetails
		}
		if !validComparisonBlobURL(file.BlobURL, owner, repo, revisionSHA, file.Path) {
			return RevisionComparison{}, ErrInvalidChangedFileSourceLink
		}
		files = append(files, RevisionComparisonFile{
			Path: file.Path, Status: file.Status, Additions: file.Additions,
			Deletions: file.Deletions, Changes: file.Changes, URL: file.BlobURL,
		})
	}

	return RevisionComparison{
		BaseSHA: strings.ToLower(baseSHA), HeadSHA: strings.ToLower(headSHA), CompareURL: source.HTMLURL,
		Status: source.Status, AheadBy: source.AheadBy, BehindBy: source.BehindBy, TotalCommits: source.TotalCommits,
		Files: files, FilesTruncated: len(files) >= GitHubCompareFileLimit,
	}, nil
}

func validComparisonURL(raw, expectedPath string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || !strings.EqualFold(u.Host, "github.com") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	return strings.EqualFold(u.Path, expectedPath)
}

func validComparisonBlobURL(raw, owner, repo, revisionSHA, filePath string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || !strings.EqualFold(u.Host, "github.com") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	prefix := fmt.Sprintf("/%s/%s/blob/", owner, repo)
	path := u.Path
	if len(path) <= len(prefix) || !strings.EqualFold(path[:len(prefix)], prefix) {
		return false
	}
	revisionAndFile := path[len(prefix):]
	separator := strings.IndexByte(revisionAndFile, '/')
	if separator != 40 || !strings.EqualFold(revisionAndFile[:separator], revisionSHA) {
		return false
	}
	return revisionAndFile[separator+1:] == filePath
}
