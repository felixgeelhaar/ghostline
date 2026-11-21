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

func TestIsTrustedNoxWorkflowRevisionRequiresExactBaseDefinition(t *testing.T) {
	const base = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if !IsTrustedNoxWorkflowRevision(base, base) {
		t.Fatal("unchanged workflow should match the trusted base definition")
	}
	for _, revision := range []string{"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "", "not-a-blob-sha"} {
		if IsTrustedNoxWorkflowRevision(base, revision) {
			t.Errorf("workflow blob %q should not be trusted against base %q", revision, base)
		}
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

func TestParseNoxFindingDoesNotCombineSuppressionMetadataFromDifferentSources(t *testing.T) {
	finding, include := ParseNoxFinding(NoxSARIFFinding{
		RuleID: "SEC-1",
		Suppressions: []NoxSuppression{
			{Status: "accepted", Kind: "external"},
			{Status: "accepted", Justification: "Approved baseline"},
		},
	})
	if !include || !finding.Suppressed {
		t.Fatalf("accepted source records should still mark the result suppressed: %+v, include=%v", finding, include)
	}
	if finding.SuppressionKind != "external" || finding.SuppressionJustification != "" {
		t.Fatalf("summary should preserve one source record without combining another: %+v", finding)
	}
}

func TestParseNoxFindingPreservesOneSidedAcceptedSuppressionDetail(t *testing.T) {
	finding, include := ParseNoxFinding(NoxSARIFFinding{RuleID: "SEC-1", Suppressions: []NoxSuppression{
		{Status: "accepted", Justification: "Temporary exception approved"},
	}})
	if !include || !finding.Suppressed || finding.SuppressionJustification != "Temporary exception approved" {
		t.Fatalf("accepted one-sided suppression detail was lost: %+v, include=%v", finding, include)
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

func TestNoxScanControlChangeNamesFilesThatSteerTheScan(t *testing.T) {
	for _, test := range []struct {
		name    string
		changed []string
		want    string
	}{
		{name: "application code only", changed: []string{"backend/main.go", "docs/nox.md", "frontend/.gitignore.example"}},
		{name: "workflow", changed: []string{"README.md", NoxSecurityWorkflowPath}, want: NoxSecurityWorkflowPath},
		{name: "baseline", changed: []string{".nox/baseline.json"}, want: ".nox/baseline.json"},
		{name: "other nox state", changed: []string{".nox/vex.json"}, want: ".nox/vex.json"},
		{name: "project config", changed: []string{".nox.yaml"}, want: ".nox.yaml"},
		{name: "root gitignore", changed: []string{".gitignore"}, want: ".gitignore"},
		{name: "nested gitignore", changed: []string{"backend/internal/.gitignore"}, want: "backend/internal/.gitignore"},
		{name: "scan output symlink or file", changed: []string{"nox-results"}, want: "nox-results"},
		{name: "planted scan output", changed: []string{"nox-results/results.sarif"}, want: "nox-results/results.sarif"},
		{name: "nested planted scan output", changed: []string{"nox-results/sub/findings.json"}, want: "nox-results/sub/findings.json"},
		{name: "similar names are not scan output", changed: []string{"nox-results.md", "docs/nox-results/notes.md"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, changed := NoxScanControlChange(test.changed)
			if changed != (test.want != "") || got != test.want {
				t.Fatalf("NoxScanControlChange(%v) = %q, %v; want %q", test.changed, got, changed, test.want)
			}
		})
	}
}

func TestNoxConfigCanMoveBaseline(t *testing.T) {
	tests := []struct {
		name   string
		config string
		want   bool
	}{
		{name: "block setting", config: "policy:\n  baseline_path: security/accepted.json\n", want: true},
		{name: "flow setting", config: "policy: {baseline_path: security/accepted.json}\n", want: true},
		{name: "quoted key", config: "policy:\n  \"baseline_path\": security/accepted.json\n", want: true},
		{name: "unparseable config fails closed", config: "policy: [\n", want: true},
		{name: "no setting", config: "scan:\n  exclude: []\n", want: false},
		{name: "comment only", config: "# baseline_path is left at its default\nscan:\n  exclude: []\n", want: false},
		{name: "value mentions the name", config: "scan:\n  exclude: [docs/baseline_path.md]\n", want: false},
		{name: "empty setting", config: "policy:\n  baseline_path: \"\"\n", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NoxConfigMovesBaseline(tt.config); got != tt.want {
				t.Fatalf("NoxConfigMovesBaseline(%q) = %v, want %v", tt.config, got, tt.want)
			}
		})
	}
}

func TestParseNoxFindingAppliesSARIFDefaultLevel(t *testing.T) {
	tests := []struct {
		name  string
		value NoxSARIFFinding
		want  string
	}{
		{name: "no level anywhere defaults to warning", value: NoxSARIFFinding{RuleID: "SEC-1"}, want: "medium"},
		{name: "rule default configuration", value: NoxSARIFFinding{RuleID: "SEC-1", RuleDefaultLevel: "error"}, want: "high"},
		{name: "result level wins over rule default", value: NoxSARIFFinding{RuleID: "SEC-1", ResultLevel: "note", RuleDefaultLevel: "error"}, want: "low"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			finding, ok := ParseNoxFinding(tt.value)
			if !ok || finding.Severity != tt.want {
				t.Fatalf("ParseNoxFinding() severity = %q (included %v), want %q", finding.Severity, ok, tt.want)
			}
		})
	}
}

func TestNoxArtifactFailureAllowsOlderRun(t *testing.T) {
	tests := []struct {
		name    string
		failure NoxArtifactFailure
		want    bool
	}{
		{name: "rate limited", failure: NoxArtifactFailure{RateLimited: true}, want: false},
		{name: "inspection limit", failure: NoxArtifactFailure{InspectionLimitReached: true}, want: false},
		{name: "server error", failure: NoxArtifactFailure{HTTPStatus: 502}, want: true},
		{name: "client error", failure: NoxArtifactFailure{HTTPStatus: 404}, want: false},
		{name: "artifact content rejected", failure: NoxArtifactFailure{}, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NoxArtifactFailureAllowsOlderRun(tt.failure); got != tt.want {
				t.Fatalf("NoxArtifactFailureAllowsOlderRun(%+v) = %v, want %v", tt.failure, got, tt.want)
			}
		})
	}
}

func TestParseNoxFindingSkipsSARIFEvaluationsThatAreNotFindings(t *testing.T) {
	for _, kind := range []string{"pass", "notApplicable"} {
		if _, include := ParseNoxFinding(NoxSARIFFinding{RuleID: "SEC-1", ResultLevel: "none", Kind: kind, Message: "Checked"}); include {
			t.Fatalf("SARIF kind %q was presented as a finding", kind)
		}
	}
	for _, kind := range []string{"", "fail", "review", "open", "informational"} {
		if _, include := ParseNoxFinding(NoxSARIFFinding{RuleID: "SEC-1", ResultLevel: "warning", Kind: kind, Message: "Check"}); !include {
			t.Fatalf("SARIF kind %q was dropped; an omitted kind defaults to fail", kind)
		}
	}
}

func TestParseNoxFindingDecidesSuppressionFromEveryRecordStatus(t *testing.T) {
	retained := []NoxSuppression{{Status: "accepted", Kind: "external", Justification: "baseline"}}
	for _, test := range []struct {
		name     string
		statuses []string
		want     bool
	}{
		{name: "all accepted beyond retained", statuses: []string{"accepted", "", "accepted"}, want: true},
		{name: "rejected beyond retained", statuses: []string{"accepted", "rejected"}},
		{name: "under review beyond retained", statuses: []string{"accepted", "underReview"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			finding, _ := ParseNoxFinding(NoxSARIFFinding{RuleID: "SEC-1", Suppressions: retained, SuppressionStatuses: test.statuses, SuppressionsTruncated: true})
			if finding.Suppressed != test.want {
				t.Fatalf("Suppressed = %v, want %v", finding.Suppressed, test.want)
			}
		})
	}
}

func TestValidateNoxSARIFAcceptsOnlyCompleteNoxRuns(t *testing.T) {
	if err := ValidateNoxSARIFLog("2.1.0", 1); err != nil {
		t.Fatalf("valid log rejected: %v", err)
	}
	for _, test := range []struct {
		version string
		runs    int
	}{{"2.0.0", 1}, {"2.1.0", 0}} {
		if ValidateNoxSARIFLog(test.version, test.runs) == nil {
			t.Fatalf("log version %q with %d runs was accepted", test.version, test.runs)
		}
	}
	if err := ValidateNoxSARIFRun("Nox", true); err != nil {
		t.Fatalf("valid run rejected: %v", err)
	}
	if ValidateNoxSARIFRun("Other", true) == nil {
		t.Fatal("a run from another scanner was accepted")
	}
	if ValidateNoxSARIFRun("Nox", false) == nil {
		t.Fatal("a run without a results array was accepted; SARIF 2.1.0 §3.14.23 defaults it to null")
	}
}

func TestNoxScannedCommitMatches(t *testing.T) {
	sha := strings.Repeat("c", 40)
	for _, test := range []struct {
		record string
		want   bool
	}{
		{record: sha + "\n", want: true},
		{record: strings.ToUpper(sha), want: true},
		{record: strings.Repeat("d", 40)},
		{record: ""},
		{record: sha[:7]},
		{record: sha + "\n" + sha},
	} {
		if got := NoxScannedCommitMatches(test.record, sha); got != test.want {
			t.Fatalf("NoxScannedCommitMatches(%q) = %v, want %v", test.record, got, test.want)
		}
	}
}

func TestParseNoxFindingKeepsSuppressionDetailOffActiveFindings(t *testing.T) {
	tests := []struct {
		name  string
		value NoxSARIFFinding
	}{
		{
			name: "one accepted and one rejected record",
			value: NoxSARIFFinding{RuleID: "SEC-1", Suppressions: []NoxSuppression{
				{Status: "accepted", Kind: "external", Justification: "Tracked elsewhere"},
				{Status: "rejected", Kind: "inSource", Justification: "Not agreed"},
			}},
		},
		{
			name: "caller summary for a result that is not suppressed",
			value: NoxSARIFFinding{RuleID: "SEC-1", SuppressionKind: "external", SuppressionJustification: "Tracked elsewhere",
				SuppressionStatuses: []string{"accepted", "underReview"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			finding, ok := ParseNoxFinding(tt.value)
			if !ok {
				t.Fatal("ParseNoxFinding() skipped a failing result")
			}
			if finding.Suppressed {
				t.Fatal("finding is suppressed, want active")
			}
			if finding.SuppressionKind != "" || finding.SuppressionJustification != "" {
				t.Fatalf("active finding shows suppression detail %q / %q", finding.SuppressionKind, finding.SuppressionJustification)
			}
		})
	}
}

func TestIsUnlinkedNoxWorkflowRun(t *testing.T) {
	sha := strings.Repeat("c", 40)
	if !IsUnlinkedNoxWorkflowRun(NoxSecurityWorkflowPath, "pull_request", sha, sha, nil) {
		t.Fatal("a Security run for this revision without a linked pull request was not recognised")
	}
	if IsUnlinkedNoxWorkflowRun(NoxSecurityWorkflowPath, "pull_request", sha, sha, []int{8}) {
		t.Fatal("a run linked to another pull request was treated as unlinked")
	}
	if IsUnlinkedNoxWorkflowRun(".github/workflows/other.yml", "pull_request", sha, sha, nil) {
		t.Fatal("another workflow's run was treated as an unlinked Security run")
	}
}
