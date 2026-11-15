package domain

import "testing"

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

func TestParseNoxFindingRejectsUnsafePathsAndSuppressedResults(t *testing.T) {
	finding, include := ParseNoxFinding(NoxSARIFFinding{RuleID: "SEC-1", Path: "../../secret", Line: 10})
	if !include || finding.Path != "" || finding.Line != 0 {
		t.Fatalf("unsafe location should be omitted: %+v, include=%v", finding, include)
	}
	if _, include := ParseNoxFinding(NoxSARIFFinding{Suppressed: true}); include {
		t.Fatal("suppressed result should not be presented as an active finding")
	}
}

func TestParseNoxFindingUsesSARIFRuleDefinitionWhenResultOmitsRuleID(t *testing.T) {
	finding, include := ParseNoxFinding(NoxSARIFFinding{RuleDefinitionID: "SEC-2", RuleName: "Rule fallback"})
	if !include || finding.RuleID != "SEC-2" || finding.Title != "Rule fallback" {
		t.Fatalf("SARIF rule definition fallback was lost: %+v, include=%v", finding, include)
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

func TestIsNoxScannerName(t *testing.T) {
	if !IsNoxScannerName("Nox Security Scanner") || IsNoxScannerName("CodeQL") {
		t.Fatal("scanner identity check did not distinguish Nox from another scanner")
	}
}
