package domain

import "testing"

func TestReferencedIssueNumbers(t *testing.T) {
	tests := []struct {
		name string
		body string
		want []int
	}{
		{name: "numbers and same repository URLs are unique", body: "Fixes #12 and https://github.com/acme/app/issues/13; also #12", want: []int{12, 13}},
		{name: "other repositories and pull request URLs are ignored", body: "See #8, https://github.com/other/app/issues/9, https://github.com/acme/app/pull/10", want: []int{8}},
		{name: "invalid and embedded numbers are ignored", body: "#0 abc#11 issue 12", want: nil},
		{name: "repository matching is case insensitive", body: "https://github.com/ACME/App/issues/21", want: []int{21}},
		{name: "malformed issue URL suffixes are ignored", body: "https://github.com/acme/app/issues/22extra and https://github.com/acme/app/issues/23/pull/1", want: nil},
		{name: "numeric URL fragments are not issue references", body: "See https://github.com/other/app/issues/9/#10 and https://example.com/docs/#11", want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ReferencedIssueNumbers(tt.body, "acme", "app")
			if len(got) != len(tt.want) {
				t.Fatalf("ReferencedIssueNumbers() = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("ReferencedIssueNumbers() = %v, want %v", got, tt.want)
				}
			}
		})
	}
}
