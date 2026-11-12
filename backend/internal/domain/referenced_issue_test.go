package domain

import "testing"

func TestParseIssueReferences(t *testing.T) {
	repo, err := NewRepositoryIdentity("Acme", "payments")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		body string
		want []int
	}{
		{"bare issue references", "Refs #12 and fixes #8.", []int{12, 8}},
		{"same repository issue URLs", "See https://github.com/acme/payments/issues/12 and https://github.com/ACME/PAYMENTS/issues/8", []int{12, 8}},
		{"adjacent references", "#12,#8\nhttps://github.com/acme/payments/issues/7 https://github.com/acme/payments/issues/6", []int{12, 8, 7, 6}},
		{"trailing slash issue URL", "https://github.com/acme/payments/issues/27/", []int{27}},
		{"foreign markdown link label is ignored", "[#27](https://github.com/other/repo/issues/27)", nil},
		{"same repository markdown link destination is found", "[#27](https://github.com/acme/payments/issues/27)", []int{27}},
		{"deduplicates repeated references", "#12 https://github.com/acme/payments/issues/12", []int{12}},
		{"ignores other repositories and PR URLs", "acme/other#5 https://github.com/acme/other/issues/6 https://github.com/acme/payments/pull/7", nil},
		{"ignores issue-looking substrings", "abc#12 /#13 #0", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseIssueReferences(tt.body, repo)
			if len(got) != len(tt.want) {
				t.Fatalf("got %v, want numbers %v", got, tt.want)
			}
			for i, number := range tt.want {
				if got[i] != number {
					t.Errorf("reference[%d] = %d, want %d", i, got[i], number)
				}
			}
		})
	}
}
