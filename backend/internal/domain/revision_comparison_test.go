package domain

import (
	"errors"
	"strings"
	"testing"
)

func validRevisionComparisonSource(baseSHA, headSHA, filePath, blobURL string) RevisionComparisonSource {
	return RevisionComparisonSource{
		Status: "ahead", AheadBy: 1, TotalCommits: 1, HTMLURL: "https://github.com/acme/app/compare/" + baseSHA + "..." + headSHA,
		Base:  RevisionComparisonCommitSource{SHA: baseSHA},
		Files: []RevisionComparisonFileSource{{Path: filePath, Status: "modified", Additions: 2, Changes: 2, BlobURL: blobURL}},
	}
}

func TestNewRevisionComparisonRequiresAncestorOrderedPair(t *testing.T) {
	baseSHA, headSHA := strings.Repeat("a", 40), strings.Repeat("b", 40)
	source := validRevisionComparisonSource(baseSHA, headSHA, "src/file.go", "https://github.com/acme/app/blob/"+headSHA+"/src/file.go")
	source.Status, source.AheadBy, source.BehindBy = "diverged", 2, 1
	_, err := NewRevisionComparison("acme", "app", baseSHA, headSHA, source)
	if !errors.Is(err, ErrComparisonRevisionsNotAncestor) {
		t.Fatalf("error = %v, want non-ancestor comparison error", err)
	}
}

func TestNewRevisionComparisonBindsChangedFileLinkToExactRevisionAndPath(t *testing.T) {
	baseSHA, headSHA := strings.Repeat("a", 40), strings.Repeat("b", 40)
	tests := []struct {
		name string
		url  string
	}{
		{"different revision", "https://github.com/acme/app/blob/" + strings.Repeat("c", 40) + "/src/file.go"},
		{"different path", "https://github.com/acme/app/blob/" + headSHA + "/src/other.go"},
		{"different repository prefix", "https://github.com/acme/app-fork/blob/" + headSHA + "/src/file.go"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source := validRevisionComparisonSource(baseSHA, headSHA, "src/file.go", test.url)
			if _, err := NewRevisionComparison("acme", "app", baseSHA, headSHA, source); !errors.Is(err, ErrInvalidChangedFileSourceLink) {
				t.Fatalf("error = %v, want invalid source evidence", err)
			}
		})
	}
}

func TestNewRevisionComparisonUsesBaseLinkForRemovedFiles(t *testing.T) {
	baseSHA, headSHA := strings.Repeat("a", 40), strings.Repeat("b", 40)
	source := validRevisionComparisonSource(baseSHA, headSHA, "src/removed.go", "https://github.com/acme/app/blob/"+baseSHA+"/src/removed.go")
	source.Files[0].Status = "removed"
	comparison, err := NewRevisionComparison("acme", "app", baseSHA, headSHA, source)
	if err != nil || len(comparison.Files) != 1 {
		t.Fatalf("comparison = %+v, error = %v", comparison, err)
	}
}

func TestNewRevisionComparisonPreservesAdditionalGitHubFileStatuses(t *testing.T) {
	baseSHA, headSHA := strings.Repeat("a", 40), strings.Repeat("b", 40)
	for _, status := range []string{"copied", "changed", "unchanged"} {
		t.Run(status, func(t *testing.T) {
			source := validRevisionComparisonSource(baseSHA, headSHA, "src/copied.go", "https://github.com/acme/app/blob/"+headSHA+"/src/copied.go")
			source.Files[0].Status = status
			comparison, err := NewRevisionComparison("acme", "app", baseSHA, headSHA, source)
			if err != nil {
				t.Fatalf("NewRevisionComparison() error = %v", err)
			}
			if len(comparison.Files) != 1 || comparison.Files[0].Status != status {
				t.Fatalf("files = %+v, want status %q preserved", comparison.Files, status)
			}
		})
	}
}
