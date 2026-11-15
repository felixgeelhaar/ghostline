package domain

import (
	"strings"
	"testing"
)

func TestParseNoxFindingNormalizesSourceAndSeverity(t *testing.T) {
	finding, include := ParseNoxFinding(NoxSARIFFinding{
		RuleID: "SEC-1", RuleName: "Unsafe input", RuleSeverity: "HIGH", ResultLevel: "error",
		Message: "Validate this input", Path: "backend/server.go", Line: 42,
		Fingerprints: map[string]string{"primaryLocationLineHash": "line-hash"},
	})
	if !include || finding.RuleID != "SEC-1" || finding.Title != "Unsafe input" || finding.Severity != "high" || finding.Path != "backend/server.go" || finding.Line != 42 || finding.Fingerprint != "line-hash" {
		t.Fatalf("unexpected domain finding: %+v, include=%v", finding, include)
	}
}

func TestParseNoxFindingRejectsUnsafePathsAndPreservesSuppression(t *testing.T) {
	finding, include := ParseNoxFinding(NoxSARIFFinding{RuleID: "SEC-1", Path: "../../secret", Line: 10})
	if !include || finding.Path != "" || finding.Line != 0 {
		t.Fatalf("unsafe location should be omitted: %+v, include=%v", finding, include)
	}
	finding, include = ParseNoxFinding(NoxSARIFFinding{RuleID: "SEC-1", Suppressed: true, SuppressionKind: "external", SuppressionJustification: "Accepted baseline", Suppressions: []NoxSuppression{{Status: "accepted", Kind: "external", Justification: "Accepted baseline"}, {Status: "rejected", Kind: "inSource", Justification: "Rejected example"}}})
	if !include || !finding.Suppressed || finding.SuppressionKind != "external" || finding.SuppressionJustification != "Accepted baseline" {
		t.Fatalf("suppression evidence should remain visible: %+v, include=%v", finding, include)
	}
	if len(finding.Suppressions) != 2 || finding.Suppressions[1].Status != "rejected" {
		t.Fatalf("all suppression source details should be retained: %+v", finding.Suppressions)
	}
}

func TestParseNoxFindingDecodesEncodedRepositoryPaths(t *testing.T) {
	for encoded, want := range map[string]string{
		"frontend/src/My%20Component.vue": "frontend/src/My Component.vue",
		"frontend%2Fsrc%2FApp.vue":        "frontend/src/App.vue",
	} {
		finding, include := ParseNoxFinding(NoxSARIFFinding{RuleID: "SEC-1", Path: encoded, Line: 3})
		if !include || finding.Path != want || finding.Line != 3 {
			t.Errorf("encoded path %q = %+v, include=%v; want %q", encoded, finding, include, want)
		}
	}
}

func TestSafeNoxPathRejectsEncodedTraversalAndNonPathComponents(t *testing.T) {
	for _, value := range []string{"%2e%2e/secrets.txt", "src/%2E%2e/secrets.txt", "src/file.go?download=1", "src/file.go#section"} {
		if got := SafeNoxPath(value); got != "" {
			t.Errorf("SafeNoxPath(%q) = %q, want empty", value, got)
		}
	}
}

func TestParseNoxFindingUsesSARIFRuleDefinitionWhenResultOmitsRuleID(t *testing.T) {
	finding, include := ParseNoxFinding(NoxSARIFFinding{RuleDefinitionID: "SEC-2", RuleName: "Rule fallback"})
	if !include || finding.RuleID != "SEC-2" || finding.Title != "Rule fallback" {
		t.Fatalf("SARIF rule definition fallback was lost: %+v, include=%v", finding, include)
	}
}

func TestParseNoxFindingBoundsRuleIdentifiersAndTitles(t *testing.T) {
	long := strings.Repeat("x", 5000)
	finding, include := ParseNoxFinding(NoxSARIFFinding{RuleID: long, RuleTitle: long})
	if !include || len([]rune(finding.RuleID)) > 200 || len([]rune(finding.Title)) > 1000 {
		t.Fatalf("rule metadata was not bounded: id=%d title=%d include=%v", len([]rune(finding.RuleID)), len([]rune(finding.Title)), include)
	}
}

func TestParseNoxFindingCapsSuppressionEvidenceAndMarksTruncation(t *testing.T) {
	suppressions := make([]NoxSuppression, MaxNoxSuppressionsPerFinding+1)
	for i := range suppressions {
		suppressions[i] = NoxSuppression{Status: "accepted", Kind: "external"}
	}
	finding, include := ParseNoxFinding(NoxSARIFFinding{RuleID: "SEC-1", Suppressions: suppressions})
	if !include || !finding.Suppressed || len(finding.Suppressions) != MaxNoxSuppressionsPerFinding || !finding.SuppressionsTruncated {
		t.Fatalf("suppression evidence should be bounded and marked truncated: count=%d truncated=%v suppressed=%v include=%v", len(finding.Suppressions), finding.SuppressionsTruncated, finding.Suppressed, include)
	}
}

func TestNoxWorkflowRunMustMatchTheRepositoryWorkflowAndRevision(t *testing.T) {
	for name, input := range map[string]struct {
		path, event, runSHA, requestedSHA string
		want                              bool
	}{
		"repository workflow path": {".github/workflows/nox.yml", "pull_request", "abc", "abc", true},
		"GitHub ref suffix":        {".github/workflows/nox.yml@refs/pull/7/merge", "pull_request", "abc", "abc", true},
		"different workflow":       {".github/workflows/other.yml", "pull_request", "abc", "abc", false},
		"workflow path prefix":     {".github/workflows/nox.yml.evil", "pull_request", "abc", "abc", false},
		"empty GitHub ref suffix":  {".github/workflows/nox.yml@", "pull_request", "abc", "abc", false},
		"wrong event":              {".github/workflows/nox.yml", "push", "abc", "abc", false},
		"wrong revision":           {".github/workflows/nox.yml", "pull_request", "old", "abc", false},
	} {
		t.Run(name, func(t *testing.T) {
			got := IsNoxWorkflowRun(input.path, input.event, input.runSHA, input.requestedSHA)
			if got != input.want {
				t.Fatalf("IsNoxWorkflowRun(%q, %q, %q, %q) = %v, want %v", input.path, input.event, input.runSHA, input.requestedSHA, got, input.want)
			}
		})
	}
}

func TestNoxWorkflowRunMustBelongToRequestedPullRequest(t *testing.T) {
	for name, input := range map[string]struct {
		prs  []int
		want bool
	}{
		"matching PR":                  {[]int{7}, true},
		"another PR with same SHA":     {[]int{8}, false},
		"multiple PRs including match": {[]int{8, 7}, true},
		"missing PR identity":          {nil, false},
	} {
		t.Run(name, func(t *testing.T) {
			got := IsNoxWorkflowRunForPullRequest(NoxSecurityWorkflowPath, "pull_request", "abc", "abc", input.prs, 7)
			if got != input.want {
				t.Fatalf("IsNoxWorkflowRunForPullRequest(..., %v, 7) = %v, want %v", input.prs, got, input.want)
			}
		})
	}
}

func TestIsNoxScannerName(t *testing.T) {
	for name, input := range map[string]struct {
		value string
		want  bool
	}{
		"Nox display name":              {"Nox Security Scanner", true},
		"Nox executable name":           {"nox", true},
		"case and surrounding spaces":   {"  NOX  ", true},
		"CodeQL":                        {"CodeQL", false},
		"unrelated name containing nox": {"NotNox", false},
		"other scanner mentioning Nox":  {"CodeQL Nox plugin", false},
	} {
		t.Run(name, func(t *testing.T) {
			if got := IsNoxScannerName(input.value); got != input.want {
				t.Fatalf("IsNoxScannerName(%q) = %v, want %v", input.value, got, input.want)
			}
		})
	}
}

func TestSelectNoxArtifactsForRevision(t *testing.T) {
	candidates := []NoxArtifactCandidate{
		{Name: "nox-results", ArtifactID: 1, ArtifactRunID: 10, ArtifactHeadSHA: "abc", RunID: 10, RunHeadSHA: "abc", CreatedAt: "2026-11-15T13:00:00Z"},
		{Name: "nox-results", ArtifactID: 2, ArtifactRunID: 20, ArtifactHeadSHA: "abc", RunID: 20, RunHeadSHA: "abc", CreatedAt: "2026-11-15T14:00:00Z"},
		{Name: "other", ArtifactID: 3, ArtifactRunID: 30, ArtifactHeadSHA: "abc", RunID: 30, RunHeadSHA: "abc", CreatedAt: "2026-11-15T15:00:00Z"},
		{Name: "nox-results", ArtifactID: 4, ArtifactRunID: 40, ArtifactHeadSHA: "old", RunID: 40, RunHeadSHA: "abc", CreatedAt: "2026-11-15T16:00:00Z"},
		{Name: "nox-results", ArtifactID: 5, ArtifactRunID: 51, ArtifactHeadSHA: "abc", RunID: 50, RunHeadSHA: "abc", CreatedAt: "2026-11-15T17:00:00Z"},
		{Name: "nox-results", ArtifactID: 6, ArtifactRunID: 60, ArtifactHeadSHA: "abc", RunID: 60, RunHeadSHA: "abc", Expired: true},
	}
	selected := SelectNoxArtifactsForRevision(candidates, "nox-results", "abc")
	if len(selected) != 2 || selected[0].ArtifactID != 2 || selected[1].ArtifactID != 1 {
		t.Fatalf("unexpected selected artifact candidates: %+v", selected)
	}
}
