package main

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/felixgeelhaar/ghostline/backend/internal/domain"
)

const noxTrustedWorkflowBlobSHA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

type noxWorkflowTestTransport struct {
	fallback http.RoundTripper
}

func (transport noxWorkflowTestTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.URL.Path == "/repos/acme/app/contents/.github/workflows/nox.yml" {
		return &http.Response{
			// GitHub reports a user token's scopes on every reply.
			StatusCode: http.StatusOK, Header: http.Header{"X-Oauth-Scopes": {"read:user"}}, Body: io.NopCloser(strings.NewReader(`{"sha":"` + noxTrustedWorkflowBlobSHA + `"}`)), Request: request,
		}, nil
	}
	response, err := transport.fallback.RoundTrip(request)
	// Unless a test serves its own comparison, the pull request changed no
	// scan-control files.
	if err == nil && response.StatusCode == http.StatusNotFound && strings.Contains(request.URL.Path, "/compare/") {
		_ = response.Body.Close()
		return &http.Response{
			StatusCode: http.StatusOK, Header: http.Header{"X-Oauth-Scopes": {"read:user"}}, Body: io.NopCloser(strings.NewReader(`{"files":[]}`)), Request: request,
		}, nil
	}
	return response, err
}

func noxTestGitHubClient(server *httptest.Server) *githubClient {
	client := server.Client()
	fallback := client.Transport
	if fallback == nil {
		fallback = http.DefaultTransport
	}
	client.Transport = noxWorkflowTestTransport{fallback: fallback}
	return &githubClient{baseURL: server.URL, http: client}
}

func TestParseNoxSARIFBindsFindingsToReportRevision(t *testing.T) {
	const sha = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	data := []byte(`{"version":"2.1.0","runs":[{"tool":{"driver":{"name":"Nox","rules":[{"id":"SEC-001","name":"Unsafe input","shortDescription":{"text":"Validate input"},"properties":{"severity":"HIGH"}}]}},"results":[{"ruleId":"SEC-001","ruleIndex":0,"level":"error","message":{"text":"Untrusted input reaches a command."},"locations":[{"physicalLocation":{"artifactLocation":{"uri":"backend/server.go"},"region":{"startLine":42}}}],"partialFingerprints":{"primaryLocationLineHash":"abc"}}]}]}`)
	report, err := parseNoxSARIF(data, sha, 123, "https://github.com/acme/app/actions/runs/123", "2026-11-15T13:58:17Z", "2026-11-22T13:58:16Z")
	if err != nil {
		t.Fatal(err)
	}
	if report.Availability != noxReportAvailable || report.RevisionSHA != sha || report.RunID != 123 || report.TotalCount != 1 {
		t.Fatalf("unexpected report provenance: %+v", report)
	}
	if got := report.Findings[0]; got.RuleID != "SEC-001" || got.Title != "Validate input" || got.Severity != "high" || got.Path != "backend/server.go" || got.Line != 42 || got.Fingerprint != "abc" {
		t.Fatalf("unexpected finding: %+v", got)
	}
}

func TestParseNoxSARIFBoundsMemoryForLargeResultArrays(t *testing.T) {
	const sha = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const resultCount = 250_000
	var data bytes.Buffer
	data.WriteString(`{"version":"2.1.0","runs":[{"tool":{"driver":{"name":"Nox"}},"results":[`)
	for index := 0; index < resultCount; index++ {
		if index > 0 {
			data.WriteByte(',')
		}
		data.WriteString(`{}`)
	}
	data.WriteString(`]}]}`)

	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	report, err := parseNoxSARIF(data.Bytes(), sha, 1, "https://github.com/acme/app/actions/runs/1", "", "")
	runtime.ReadMemStats(&after)
	if err != nil {
		t.Fatal(err)
	}
	if report.TotalCount != resultCount || len(report.Findings) != domain.MaxNoxFindings || !report.Truncated {
		t.Fatalf("large SARIF report was not summarized with bounded findings: total=%d visible=%d truncated=%v", report.TotalCount, len(report.Findings), report.Truncated)
	}
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 96<<20 {
		t.Fatalf("large SARIF parsing allocated %d MiB; expected bounded parsing below 96 MiB", allocated>>20)
	}
}

func TestParseNoxSARIFPreservesScannerExecutionStatus(t *testing.T) {
	const sha = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	for _, test := range []struct {
		name string
		runs string
		want *bool
	}{
		{name: "completed", runs: `[{"tool":{"driver":{"name":"Nox"}},"invocations":[{"executionSuccessful":true}],"results":[]}]`, want: boolPointer(true)},
		{name: "incomplete", runs: `[{"tool":{"driver":{"name":"Nox"}},"invocations":[{"executionSuccessful":false}],"results":[]}]`, want: boolPointer(false)},
		{name: "unknown when omitted", runs: `[{"tool":{"driver":{"name":"Nox"}},"results":[]}]`},
		{name: "failed invocation remains failed when followed by success", runs: `[{"tool":{"driver":{"name":"Nox"}},"invocations":[{"executionSuccessful":false},{"executionSuccessful":true}],"results":[]}]`, want: boolPointer(false)},
		{name: "failed later run keeps aggregate failed", runs: `[{"tool":{"driver":{"name":"Nox"}},"results":[],"invocations":[{"executionSuccessful":true}]},{"tool":{"driver":{"name":"Nox"}},"results":[],"invocations":[{"executionSuccessful":false}]}]`, want: boolPointer(false)},
	} {
		t.Run(test.name, func(t *testing.T) {
			data := `{"version":"2.1.0","runs":` + test.runs + `}`
			report, err := parseNoxSARIF([]byte(data), sha, 1, "https://github.com/acme/app/actions/runs/1", "", "")
			if err != nil {
				t.Fatal(err)
			}
			if test.want == nil && report.ScanSuccessful != nil || test.want != nil && (report.ScanSuccessful == nil || *report.ScanSuccessful != *test.want) {
				t.Fatalf("scan status = %v, want %v", report.ScanSuccessful, test.want)
			}
		})
	}
}

func boolPointer(value bool) *bool { return &value }

func TestParseNoxSARIFFindsRuleMetadataWhenRuleIndexIsOmitted(t *testing.T) {
	const sha = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	data := []byte(`{"version":"2.1.0","runs":[{"tool":{"driver":{"name":"Nox","rules":[{"id":"SEC-002","name":"Unsafe configuration","shortDescription":{"text":"Review exposed configuration"},"properties":{"severity":"critical"}}]}},"results":[{"ruleId":"SEC-002","level":"warning","message":{"text":"A configuration value is exposed."}}]}]}`)
	report, err := parseNoxSARIF(data, sha, 123, "https://github.com/acme/app/actions/runs/123", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Findings) != 1 {
		t.Fatalf("expected one finding, got %+v", report.Findings)
	}
	if got := report.Findings[0]; got.RuleID != "SEC-002" || got.Title != "Review exposed configuration" || got.Severity != "critical" {
		t.Fatalf("rule metadata was not resolved by ruleId: %+v", got)
	}
}

func TestParseNoxSARIFUsesRuleIndexWhenResultOmitsRuleID(t *testing.T) {
	const sha = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	data := []byte(`{"version":"2.1.0","runs":[{"tool":{"driver":{"name":"Nox","rules":[{"id":"SEC-003","name":"Missing id","shortDescription":{"text":"Indexed metadata"},"properties":{"severity":"critical"}}]}},"results":[{"ruleIndex":0,"level":"warning","message":{"text":"Indexed result."}}]}]}`)
	report, err := parseNoxSARIF(data, sha, 123, "https://github.com/acme/app/actions/runs/123", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if got := report.Findings[0]; got.RuleID != "SEC-003" || got.Title != "Indexed metadata" || got.Severity != "critical" {
		t.Fatalf("ruleIndex metadata was not applied: %+v", got)
	}
}

func TestSARIFRuleLookupUsesIndexedMetadataAndFallsBackByID(t *testing.T) {
	rules := []sarifRule{
		{ID: "SEC-001", Name: "First definition"},
		{ID: "SEC-001", Name: "Duplicate definition"},
		{ID: "SEC-002", Name: "Second definition"},
	}
	byID := indexSARIFrules(rules)
	index := 2
	if got := sarifRuleForResult(sarifResult{RuleID: "SEC-002", RuleIndex: &index}, rules, byID); got.Name != "Second definition" {
		t.Fatalf("valid rule index returned %+v", got)
	}
	index = 1
	if got := sarifRuleForResult(sarifResult{RuleID: "SEC-001", RuleIndex: &index}, rules, byID); got.Name != "Duplicate definition" {
		t.Fatalf("valid rule index should take precedence over ID fallback: %+v", got)
	}
	if got := sarifRuleForResult(sarifResult{RuleID: "SEC-001"}, rules, byID); got.Name != "First definition" {
		t.Fatalf("ID fallback should preserve the first rule definition: %+v", got)
	}
}

func TestParseNoxSARIFRejectsOtherScannerAndUnsafeLocations(t *testing.T) {
	const sha = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	for name, data := range map[string][]byte{
		"non-Nox scanner": []byte(`{"version":"2.1.0","runs":[{"tool":{"driver":{"name":"CodeQL"}},"results":[]}]}`),
		"null results":    []byte(`{"version":"2.1.0","runs":[{"tool":{"driver":{"name":"nox"}},"results":null}]}`),
		"missing results": []byte(`{"version":"2.1.0","runs":[{"tool":{"driver":{"name":"nox"}}}]}`),
		"bad version":     []byte(`{"version":"1.0.0","runs":[]}`),
		"bad JSON":        []byte(`{`),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseNoxSARIF(data, sha, 1, "https://github.com/acme/app/actions/runs/1", "", ""); err == nil {
				t.Fatal("expected the report to be rejected")
			}
		})
	}

	data := []byte(`{"version":"2.1.0","runs":[{"tool":{"driver":{"name":"nox"}},"results":[{"ruleId":"X","message":{"text":"finding"},"locations":[{"physicalLocation":{"artifactLocation":{"uri":"../../outside.txt"},"region":{"startLine":0}}}]}]}]}`)
	report, err := parseNoxSARIF(data, sha, 1, "https://github.com/acme/app/actions/runs/1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if report.Findings[0].Path != "" || report.Findings[0].Line != 0 {
		t.Fatalf("unsafe location should be omitted: %+v", report.Findings[0])
	}
}

func TestDownloadGitHubArtifactRestrictsRedirectsAndDoesNotForwardToken(t *testing.T) {
	for name, input := range map[string]struct {
		location    string
		wantAllowed bool
	}{
		"GitHub artifact host":       {"https://objects.githubusercontent.com/archive.zip", true},
		"nested blob host":           {"https://ghostline.blob.core.windows.net/archive.zip", true},
		"host suffix spoof":          {"https://objects.githubusercontent.com.attacker.example/archive.zip", false},
		"plain HTTP":                 {"http://objects.githubusercontent.com/archive.zip", false},
		"unapproved redirect domain": {"https://attacker.example/archive.zip", false},
	} {
		t.Run(name, func(t *testing.T) {
			calls := 0
			forwardedAuthorization := ""
			transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
				calls++
				if calls == 1 {
					if request.Header.Get("Authorization") != "Bearer test-token" {
						t.Fatal("initial GitHub API request did not carry its configured token")
					}
					return &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": []string{input.location}}, Body: io.NopCloser(strings.NewReader("")), Request: request}, nil
				}
				forwardedAuthorization = request.Header.Get("Authorization")
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("archive")), Request: request}, nil
			})
			client := &githubClient{baseURL: "https://api.github.com", token: "test-token", http: &http.Client{Transport: transport}}
			data, err := client.downloadGitHubArtifact(context.Background(), "/repos/acme/app/actions/artifacts/4/zip", 1024)
			if input.wantAllowed {
				if err != nil || string(data) != "archive" || calls != 2 {
					t.Fatalf("approved redirect did not complete: data=%q calls=%d err=%v", data, calls, err)
				}
				if forwardedAuthorization != "" {
					t.Fatalf("GitHub bearer token was forwarded to artifact host: %q", forwardedAuthorization)
				}
				return
			}
			if err == nil || calls != 1 {
				t.Fatalf("unapproved redirect was followed: calls=%d err=%v", calls, err)
			}
		})
	}
}

func TestParseNoxSARIFKeepsSuppressedResultsVisibleAndSeparate(t *testing.T) {
	const sha = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	data := []byte(`{"version":"2.1.0","runs":[{"tool":{"driver":{"name":"Nox"}},"results":[{"ruleId":"SEC-001","message":{"text":"Known baseline item."},"suppressions":[{"kind":"external","status":"accepted","justification":"Accepted baseline"},{"kind":"inSource","status":"accepted","justification":"Second accepted reason"}]}]}]}`)
	report, err := parseNoxSARIF(data, sha, 1, "https://github.com/acme/app/actions/runs/1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if report.TotalCount != 1 || report.ActiveCount != 0 || report.SuppressedCount != 1 || len(report.Findings) != 1 {
		t.Fatalf("suppressed result should remain visible and separately counted: %+v", report)
	}
	finding := report.Findings[0]
	if !finding.Suppressed || finding.SuppressionKind != "external" || finding.SuppressionJustification != "Accepted baseline" || len(finding.Suppressions) != 2 || finding.Suppressions[1].Justification != "Second accepted reason" {
		t.Fatalf("suppression details were not preserved: %+v", finding)
	}
}

func TestParseNoxSARIFDoesNotTreatRejectedOrUnderReviewSuppressionsAsAccepted(t *testing.T) {
	const sha = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	data := []byte(`{"version":"2.1.0","runs":[{"tool":{"driver":{"name":"Nox"}},"results":[{"ruleId":"REJECTED","message":{"text":"Rejected suppression."},"suppressions":[{"kind":"external","status":"rejected","justification":"Do not suppress"}]},{"ruleId":"REVIEW","message":{"text":"Pending suppression."},"suppressions":[{"kind":"external","status":"underReview","justification":"Discussing it"}]},{"ruleId":"ACCEPTED","message":{"text":"Accepted suppression."},"suppressions":[{"kind":"external","status":"accepted","justification":"Approved"}]}]}]}`)
	report, err := parseNoxSARIF(data, sha, 1, "https://github.com/acme/app/actions/runs/1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if report.TotalCount != 3 || report.ActiveCount != 2 || report.SuppressedCount != 1 || report.Findings[0].Suppressed || report.Findings[1].Suppressed || !report.Findings[2].Suppressed {
		t.Fatalf("only accepted suppressions should count as suppressed: %+v", report)
	}
}

func TestReadNoxSARIFFromArtifactRejectsOversizeAndDuplicateReports(t *testing.T) {
	makeZip := func(t *testing.T, entries map[string]string) []byte {
		t.Helper()
		var buffer bytes.Buffer
		writer := zip.NewWriter(&buffer)
		for name, contents := range entries {
			file, err := writer.Create(name)
			if err != nil {
				t.Fatal(err)
			}
			_, _ = file.Write([]byte(contents))
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		return buffer.Bytes()
	}

	valid := makeZip(t, map[string]string{"nox-results/results.sarif": `{"version":"2.1.0","runs":[{"tool":{"driver":{"name":"nox"}},"results":[]}]}`})
	if _, err := readNoxSARIFArtifact(valid); err != nil {
		t.Fatalf("valid artifact rejected: %v", err)
	}
	duplicate := makeZip(t, map[string]string{"one/results.sarif": `{}`, "two/results.sarif": `{}`})
	if _, err := readNoxSARIFArtifact(duplicate); err == nil {
		t.Fatal("expected duplicate reports to be rejected")
	}
	oversize := makeZip(t, map[string]string{"results.sarif": string(bytes.Repeat([]byte("x"), maxNoxSARIFBytes+1))})
	if _, err := readNoxSARIFArtifact(oversize); err == nil {
		t.Fatal("expected oversized report to be rejected")
	}
}

func TestNoxReportJSONUsesStableFieldNames(t *testing.T) {
	report := noxReport{Availability: noxReportUnavailable, RevisionSHA: "", Findings: []noxFinding{}}
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(encoded, &got); err != nil {
		t.Fatal(err)
	}
	if got["availability"] != noxReportUnavailable || got["findings"] == nil {
		t.Fatalf("unexpected JSON response: %s", encoded)
	}
}

func TestLoadNoxReportUsesOnlyArtifactForExactPullRequestRevision(t *testing.T) {
	const sha = "cccccccccccccccccccccccccccccccccccccccc"
	archive := makeNoxArchiveFor(t, sha, `{"version":"2.1.0","runs":[{"tool":{"driver":{"name":"Nox"}},"results":[]}]}`)
	digest := sha256.Sum256(archive)
	var artifactRequests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/acme/app/pulls/7":
			// The PR base has moved since this workflow run. Trust is checked
			// against the run's associated base revision, not this current value.
			_ = json.NewEncoder(w).Encode(map[string]any{"number": 7, "head": map[string]string{"sha": sha}, "base": map[string]any{"sha": strings.Repeat("c", 40), "repo": map[string]any{"name": "app", "owner": map[string]string{"login": "acme"}}}})
		case "/repos/acme/app/actions/workflows/nox.yml/runs":
			if r.URL.Query().Get("head_sha") != sha || r.URL.Query().Get("event") != "pull_request" {
				t.Errorf("workflow query not scoped to requested revision and PR event: %s", r.URL.RawQuery)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"workflow_runs": []any{
				map[string]any{"id": 12, "path": domain.NoxSecurityWorkflowPath, "name": "Security", "event": "pull_request", "head_sha": "dddddddddddddddddddddddddddddddddddddddd", "html_url": "https://github.com/acme/app/actions/runs/12", "created_at": "2026-11-15T13:59:00Z", "pull_requests": []any{map[string]any{"number": 7, "base": map[string]string{"sha": strings.Repeat("b", 40)}}}},
				map[string]any{"id": 11, "path": domain.NoxSecurityWorkflowPath + "@refs/pull/7/merge", "name": "Security", "event": "pull_request", "head_sha": sha, "conclusion": "success", "html_url": "https://github.com/acme/app/actions/runs/11", "created_at": "2026-11-15T13:58:00Z", "pull_requests": []any{map[string]any{"number": 7, "base": map[string]string{"sha": strings.Repeat("b", 40)}}}},
			}})
		case "/repos/acme/app/actions/runs/11/artifacts":
			_ = json.NewEncoder(w).Encode(map[string]any{"artifacts": []any{map[string]any{
				"id": 44, "name": noxArtifactName, "size_in_bytes": len(archive), "digest": "sha256:" + hex.EncodeToString(digest[:]),
				"url":        "https://api.github.com/repos/acme/app/actions/artifacts/44",
				"created_at": "2026-11-15T13:58:17Z", "expires_at": "2026-11-22T13:58:16Z",
				"workflow_run": map[string]any{"id": 11, "head_sha": sha},
			}}})
		case "/repos/acme/app/actions/artifacts/44/zip":
			artifactRequests++
			_, _ = w.Write(archive)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	gh := noxTestGitHubClient(srv)
	report := loadNoxReport(context.Background(), gh, "acme", "app", 7, sha)
	if report.Availability != noxReportAvailable || report.RevisionSHA != sha || report.RunID != 11 || report.RunConclusion != "success" || report.TotalCount != 0 {
		t.Fatalf("unexpected Nox report: %+v", report)
	}
	if report.RunURL != "https://github.com/acme/app/actions/runs/11" || report.ArtifactURL != "https://api.github.com/repos/acme/app/actions/artifacts/44" || report.ExpiresAt == "" || artifactRequests != 1 {
		t.Fatalf("missing source provenance or artifact was not fetched once: %+v, downloads=%d", report, artifactRequests)
	}
}

func TestLoadNoxReportDoesNotAttributeSharedCommitRunToAnotherPullRequest(t *testing.T) {
	const sha = "cccccccccccccccccccccccccccccccccccccccc"
	var artifactRequests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/acme/app/pulls/7":
			// The current base now contains the changed workflow, but this old
			// run records the earlier trusted base and must remain unavailable.
			_ = json.NewEncoder(w).Encode(map[string]any{"number": 7, "head": map[string]string{"sha": sha}, "base": map[string]any{"sha": strings.Repeat("c", 40), "repo": map[string]any{"name": "app", "owner": map[string]string{"login": "acme"}}}})
		case "/repos/acme/app/actions/workflows/nox.yml/runs":
			_ = json.NewEncoder(w).Encode(map[string]any{"workflow_runs": []any{map[string]any{
				"id": 29, "path": domain.NoxSecurityWorkflowPath, "event": "pull_request", "head_sha": sha,
				"created_at": "2026-11-15T14:00:00Z", "html_url": "https://github.com/acme/app/actions/runs/29",
				"pull_requests": []any{map[string]any{"number": 8}},
			}}})
		case "/repos/acme/app/actions/runs/29/artifacts":
			artifactRequests++
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	report := loadNoxReport(context.Background(), noxTestGitHubClient(server), "acme", "app", 7, sha)
	if report.Availability != noxReportUnavailable || report.Reason != "GitHub has no pull-request workflow run for this revision." || artifactRequests != 0 {
		t.Fatalf("another pull request's run was attributed to this PR: %+v, artifact requests=%d", report, artifactRequests)
	}
}

func TestLoadNoxReportExplainsRunsGitHubDoesNotLinkToThePullRequest(t *testing.T) {
	const sha = "cccccccccccccccccccccccccccccccccccccccc"
	var artifactRequests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/acme/app/pulls/7":
			_ = json.NewEncoder(w).Encode(map[string]any{"number": 7, "head": map[string]string{"sha": sha}, "base": map[string]any{"sha": strings.Repeat("c", 40), "repo": map[string]any{"name": "app", "owner": map[string]string{"login": "acme"}}}})
		case "/repos/acme/app/actions/workflows/nox.yml/runs":
			// GitHub leaves pull_requests empty for runs from forks.
			_ = json.NewEncoder(w).Encode(map[string]any{"workflow_runs": []any{map[string]any{
				"id": 29, "path": domain.NoxSecurityWorkflowPath, "event": "pull_request", "head_sha": sha,
				"created_at": "2026-11-15T14:00:00Z", "html_url": "https://github.com/acme/app/actions/runs/29",
				"pull_requests": []any{},
			}}})
		case "/repos/acme/app/actions/runs/29/artifacts":
			artifactRequests++
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	report := loadNoxReport(context.Background(), noxTestGitHubClient(server), "acme", "app", 7, sha)
	want := "GitHub ran the Security workflow for this revision but did not link the run to a pull request, so its report cannot be attributed to this pull request and is not trusted. GitHub leaves runs unlinked, for example, when the pull request comes from a fork."
	if report.Availability != noxReportUnavailable || report.Reason != want || artifactRequests != 0 {
		t.Fatalf("an unlinked run was reported as missing or trusted: %+v, artifact requests=%d", report, artifactRequests)
	}
}

func TestLoadNoxReportFallsBackToOlderRetainedArtifactWhenNewestExpired(t *testing.T) {
	const sha = "cccccccccccccccccccccccccccccccccccccccc"
	archive := makeNoxArchiveFor(t, sha, `{"version":"2.1.0","runs":[{"tool":{"driver":{"name":"Nox"}},"results":[]}]}`)
	digest := sha256.Sum256(archive)
	var downloadedArtifactID string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/acme/app/pulls/7":
			_ = json.NewEncoder(w).Encode(map[string]any{"number": 7, "head": map[string]string{"sha": sha}, "base": map[string]any{"sha": strings.Repeat("b", 40), "repo": map[string]any{"name": "app", "owner": map[string]string{"login": "acme"}}}})
		case "/repos/acme/app/actions/workflows/nox.yml/runs":
			_ = json.NewEncoder(w).Encode(map[string]any{"workflow_runs": []any{
				map[string]any{"id": 22, "path": domain.NoxSecurityWorkflowPath, "event": "pull_request", "head_sha": sha, "created_at": "2026-11-15T14:00:00Z", "pull_requests": []any{map[string]any{"number": 7, "base": map[string]string{"sha": strings.Repeat("b", 40)}}}, "html_url": "https://github.com/acme/app/actions/runs/test"},
				map[string]any{"id": 21, "path": domain.NoxSecurityWorkflowPath, "event": "pull_request", "head_sha": sha, "created_at": "2026-11-15T13:00:00Z", "pull_requests": []any{map[string]any{"number": 7, "base": map[string]string{"sha": strings.Repeat("b", 40)}}}, "html_url": "https://github.com/acme/app/actions/runs/test"},
			}})
		case "/repos/acme/app/actions/runs/22/artifacts":
			_ = json.NewEncoder(w).Encode(map[string]any{"artifacts": []any{map[string]any{
				"id": 122, "name": noxArtifactName, "expired": true, "created_at": "2026-11-15T14:00:17Z",
				"workflow_run": map[string]any{"id": 22, "head_sha": sha},
			}}})
		case "/repos/acme/app/actions/runs/21/artifacts":
			_ = json.NewEncoder(w).Encode(map[string]any{"artifacts": []any{map[string]any{
				"id": 121, "name": noxArtifactName, "size_in_bytes": len(archive), "digest": "sha256:" + hex.EncodeToString(digest[:]),
				"created_at": "2026-11-15T13:00:17Z", "expires_at": "2026-11-22T13:00:16Z",
				"workflow_run": map[string]any{"id": 21, "head_sha": sha},
			}}})
		case "/repos/acme/app/actions/artifacts/121/zip":
			downloadedArtifactID = "121"
			_, _ = w.Write(archive)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	report := loadNoxReport(context.Background(), noxTestGitHubClient(srv), "acme", "app", 7, sha)
	if report.Availability != noxReportAvailable || report.RunID != 21 || report.ArtifactID != 121 || downloadedArtifactID != "121" {
		t.Fatalf("newest expired report hid the older retained report: %+v, downloaded artifact=%q", report, downloadedArtifactID)
	}
}

func TestLoadNoxReportFallsBackToOlderRetainedArtifactWhenNewestInvalid(t *testing.T) {
	const sha = "cccccccccccccccccccccccccccccccccccccccc"
	archive := makeNoxArchiveFor(t, sha, `{"version":"2.1.0","runs":[{"tool":{"driver":{"name":"Nox"}},"results":[]}]}`)
	digest := sha256.Sum256(archive)
	var downloaded []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/acme/app/pulls/7":
			_ = json.NewEncoder(w).Encode(map[string]any{"number": 7, "head": map[string]string{"sha": sha}, "base": map[string]any{"sha": strings.Repeat("b", 40), "repo": map[string]any{"name": "app", "owner": map[string]string{"login": "acme"}}}})
		case "/repos/acme/app/actions/workflows/nox.yml/runs":
			_ = json.NewEncoder(w).Encode(map[string]any{"workflow_runs": []any{
				map[string]any{"id": 22, "path": domain.NoxSecurityWorkflowPath, "event": "pull_request", "head_sha": sha, "created_at": "2026-11-15T14:00:00Z", "pull_requests": []any{map[string]any{"number": 7, "base": map[string]string{"sha": strings.Repeat("b", 40)}}}, "html_url": "https://github.com/acme/app/actions/runs/test"},
				map[string]any{"id": 21, "path": domain.NoxSecurityWorkflowPath, "event": "pull_request", "head_sha": sha, "created_at": "2026-11-15T13:00:00Z", "pull_requests": []any{map[string]any{"number": 7, "base": map[string]string{"sha": strings.Repeat("b", 40)}}}, "html_url": "https://github.com/acme/app/actions/runs/test"},
			}})
		case "/repos/acme/app/actions/runs/22/artifacts":
			_ = json.NewEncoder(w).Encode(map[string]any{"artifacts": []any{map[string]any{
				"id": 122, "name": noxArtifactName, "size_in_bytes": len(archive), "digest": "sha256:" + strings.Repeat("0", sha256.Size*2),
				"created_at": "2026-11-15T14:00:17Z", "workflow_run": map[string]any{"id": 22, "head_sha": sha},
			}}})
		case "/repos/acme/app/actions/runs/21/artifacts":
			_ = json.NewEncoder(w).Encode(map[string]any{"artifacts": []any{map[string]any{
				"id": 121, "name": noxArtifactName, "size_in_bytes": len(archive), "digest": "sha256:" + hex.EncodeToString(digest[:]),
				"created_at": "2026-11-15T13:00:17Z", "workflow_run": map[string]any{"id": 21, "head_sha": sha},
			}}})
		case "/repos/acme/app/actions/artifacts/122/zip", "/repos/acme/app/actions/artifacts/121/zip":
			id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/repos/acme/app/actions/artifacts/"), "/zip")
			downloaded = append(downloaded, id)
			_, _ = w.Write(archive)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	report := loadNoxReport(context.Background(), noxTestGitHubClient(srv), "acme", "app", 7, sha)
	if report.Availability != noxReportAvailable || report.RunID != 21 || report.ArtifactID != 121 || len(downloaded) != 2 || downloaded[0] != "122" || downloaded[1] != "121" {
		t.Fatalf("newest invalid report hid the older valid report: %+v, downloads=%v", report, downloaded)
	}
}

func TestLoadNoxReportFallsBackToOlderArtifactAfterTemporaryDownloadFailure(t *testing.T) {
	const sha = "cccccccccccccccccccccccccccccccccccccccc"
	archive := makeNoxArchiveFor(t, sha, `{"version":"2.1.0","runs":[{"tool":{"driver":{"name":"Nox"}},"results":[]}]}`)
	digest := sha256.Sum256(archive)
	var downloaded []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/acme/app/pulls/7":
			_ = json.NewEncoder(w).Encode(map[string]any{"number": 7, "head": map[string]string{"sha": sha}, "base": map[string]any{"sha": strings.Repeat("b", 40), "repo": map[string]any{"name": "app", "owner": map[string]string{"login": "acme"}}}})
		case "/repos/acme/app/actions/workflows/nox.yml/runs":
			_ = json.NewEncoder(w).Encode(map[string]any{"workflow_runs": []any{
				map[string]any{"id": 22, "path": domain.NoxSecurityWorkflowPath, "event": "pull_request", "head_sha": sha, "created_at": "2026-11-15T14:00:00Z", "pull_requests": []any{map[string]any{"number": 7, "base": map[string]string{"sha": strings.Repeat("b", 40)}}}, "html_url": "https://github.com/acme/app/actions/runs/test"},
				map[string]any{"id": 21, "path": domain.NoxSecurityWorkflowPath, "event": "pull_request", "head_sha": sha, "created_at": "2026-11-15T13:00:00Z", "pull_requests": []any{map[string]any{"number": 7, "base": map[string]string{"sha": strings.Repeat("b", 40)}}}, "html_url": "https://github.com/acme/app/actions/runs/test"},
			}})
		case "/repos/acme/app/actions/runs/22/artifacts", "/repos/acme/app/actions/runs/21/artifacts":
			runID, artifactID, createdAt := int64(22), int64(122), "2026-11-15T14:00:17Z"
			if strings.Contains(r.URL.Path, "/21/") {
				runID, artifactID, createdAt = 21, 121, "2026-11-15T13:00:17Z"
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"artifacts": []any{map[string]any{
				"id": artifactID, "name": noxArtifactName, "size_in_bytes": len(archive), "digest": "sha256:" + hex.EncodeToString(digest[:]),
				"created_at": createdAt, "workflow_run": map[string]any{"id": runID, "head_sha": sha},
			}}})
		case "/repos/acme/app/actions/artifacts/122/zip":
			downloaded = append(downloaded, "122")
			w.WriteHeader(http.StatusServiceUnavailable)
		case "/repos/acme/app/actions/artifacts/121/zip":
			downloaded = append(downloaded, "121")
			_, _ = w.Write(archive)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	report := loadNoxReport(context.Background(), noxTestGitHubClient(srv), "acme", "app", 7, sha)
	if report.Availability != noxReportAvailable || report.RunID != 21 || report.ArtifactID != 121 || len(downloaded) != 2 || downloaded[0] != "122" || downloaded[1] != "121" {
		t.Fatalf("temporary failure on newest report hid an older valid report: %+v, downloads=%v", report, downloaded)
	}
}

func TestLoadNoxReportFallsBackWhenNewestArtifactDisappears(t *testing.T) {
	const sha = "cccccccccccccccccccccccccccccccccccccccc"
	archive := makeNoxArchiveFor(t, sha, `{"version":"2.1.0","runs":[{"tool":{"driver":{"name":"Nox"}},"results":[]}]}`)
	digest := sha256.Sum256(archive)
	var downloaded []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/acme/app/pulls/7":
			_ = json.NewEncoder(w).Encode(map[string]any{"number": 7, "head": map[string]string{"sha": sha}, "base": map[string]any{"sha": strings.Repeat("b", 40), "repo": map[string]any{"name": "app", "owner": map[string]string{"login": "acme"}}}})
		case "/repos/acme/app/actions/workflows/nox.yml/runs":
			_ = json.NewEncoder(w).Encode(map[string]any{"workflow_runs": []any{
				map[string]any{"id": 22, "path": domain.NoxSecurityWorkflowPath, "event": "pull_request", "head_sha": sha, "created_at": "2026-11-15T14:00:00Z", "pull_requests": []any{map[string]any{"number": 7, "base": map[string]string{"sha": strings.Repeat("b", 40)}}}, "html_url": "https://github.com/acme/app/actions/runs/test"},
				map[string]any{"id": 21, "path": domain.NoxSecurityWorkflowPath, "event": "pull_request", "head_sha": sha, "created_at": "2026-11-15T13:00:00Z", "pull_requests": []any{map[string]any{"number": 7, "base": map[string]string{"sha": strings.Repeat("b", 40)}}}, "html_url": "https://github.com/acme/app/actions/runs/test"},
			}})
		case "/repos/acme/app/actions/runs/22/artifacts", "/repos/acme/app/actions/runs/21/artifacts":
			runID := int64(22)
			artifactID := int64(122)
			createdAt := "2026-11-15T14:00:17Z"
			if strings.Contains(r.URL.Path, "/21/") {
				runID, artifactID, createdAt = 21, 121, "2026-11-15T13:00:17Z"
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"artifacts": []any{map[string]any{
				"id": artifactID, "name": noxArtifactName, "size_in_bytes": len(archive), "digest": "sha256:" + hex.EncodeToString(digest[:]),
				"created_at": createdAt, "workflow_run": map[string]any{"id": runID, "head_sha": sha},
			}}})
		case "/repos/acme/app/actions/artifacts/122/zip":
			downloaded = append(downloaded, "122")
			http.NotFound(w, r)
		case "/repos/acme/app/actions/artifacts/121/zip":
			downloaded = append(downloaded, "121")
			_, _ = w.Write(archive)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	report := loadNoxReport(context.Background(), noxTestGitHubClient(srv), "acme", "app", 7, sha)
	if report.Availability != noxReportAvailable || report.RunID != 21 || report.ArtifactID != 121 || len(downloaded) != 2 || downloaded[0] != "122" || downloaded[1] != "121" {
		t.Fatalf("disappeared artifact prevented fallback to an older valid report: %+v, downloads=%v", report, downloaded)
	}
}

func TestLoadNoxReportDownloadsNewestValidReportBeforeInspectingOlderRuns(t *testing.T) {
	const sha = "dddddddddddddddddddddddddddddddddddddddd"
	archive := makeNoxArchiveFor(t, sha, `{"version":"2.1.0","runs":[{"tool":{"driver":{"name":"Nox"}},"results":[]}]}`)
	digest := sha256.Sum256(archive)
	artifactLists := make([]string, 0)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/acme/app/pulls/7":
			_ = json.NewEncoder(w).Encode(map[string]any{"number": 7, "head": map[string]string{"sha": sha}, "base": map[string]any{"sha": strings.Repeat("b", 40), "repo": map[string]any{"name": "app", "owner": map[string]string{"login": "acme"}}}})
		case "/repos/acme/app/actions/workflows/nox.yml/runs":
			runs := make([]any, 31)
			for index := range runs {
				id := 100 - index
				runs[index] = map[string]any{"id": id, "path": domain.NoxSecurityWorkflowPath, "event": "pull_request", "head_sha": sha, "created_at": fmt.Sprintf("2026-11-15T%02d:00:00Z", 23-index), "pull_requests": []any{map[string]any{"number": 7, "base": map[string]string{"sha": strings.Repeat("b", 40)}}}, "html_url": "https://github.com/acme/app/actions/runs/test"}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"workflow_runs": runs})
		case "/repos/acme/app/actions/runs/100/artifacts":
			artifactLists = append(artifactLists, r.URL.Path)
			_ = json.NewEncoder(w).Encode(map[string]any{"artifacts": []any{map[string]any{
				"id": 900, "name": noxArtifactName, "size_in_bytes": len(archive), "digest": "sha256:" + hex.EncodeToString(digest[:]),
				"created_at": "2026-11-15T23:00:17Z", "workflow_run": map[string]any{"id": 100, "head_sha": sha},
			}}})
		case "/repos/acme/app/actions/artifacts/900/zip":
			_, _ = w.Write(archive)
		default:
			if strings.Contains(r.URL.Path, "/artifacts") {
				artifactLists = append(artifactLists, r.URL.Path)
			}
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	report := loadNoxReport(context.Background(), noxTestGitHubClient(srv), "acme", "app", 7, sha)
	if report.Availability != noxReportAvailable || report.RunID != 100 || len(artifactLists) != 1 || artifactLists[0] != "/repos/acme/app/actions/runs/100/artifacts" {
		t.Fatalf("older runs were inspected before the newest valid report was downloaded: %+v, artifact queries=%v", report, artifactLists)
	}
}

func TestLoadNoxReportRejectsArtifactWithDifferentRevisionProvenance(t *testing.T) {
	const sha = "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/acme/app/pulls/7":
			_ = json.NewEncoder(w).Encode(map[string]any{"number": 7, "head": map[string]string{"sha": sha}, "base": map[string]any{"sha": strings.Repeat("b", 40), "repo": map[string]any{"name": "app", "owner": map[string]string{"login": "acme"}}}})
		case "/repos/acme/app/actions/workflows/nox.yml/runs":
			_ = json.NewEncoder(w).Encode(map[string]any{"workflow_runs": []any{map[string]any{"id": 20, "path": domain.NoxSecurityWorkflowPath, "event": "pull_request", "head_sha": sha, "created_at": "2026-11-15T13:00:00Z", "pull_requests": []any{map[string]any{"number": 7, "base": map[string]string{"sha": strings.Repeat("b", 40)}}}, "html_url": "https://github.com/acme/app/actions/runs/test"}}})
		case "/repos/acme/app/actions/runs/20/artifacts":
			_ = json.NewEncoder(w).Encode(map[string]any{"artifacts": []any{map[string]any{"id": 45, "name": noxArtifactName, "workflow_run": map[string]any{"id": 20, "head_sha": "ffffffffffffffffffffffffffffffffffffffff"}}}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	report := loadNoxReport(context.Background(), noxTestGitHubClient(srv), "acme", "app", 7, sha)
	if report.Availability != noxReportUnavailable || report.Reason != "No retained Nox report was published for this revision." {
		t.Fatalf("mismatched artifact should not be presented as evidence: %+v", report)
	}
}

func TestLoadNoxReportRejectsPrivatePullRequestBeforeReadingActions(t *testing.T) {
	const sha = "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	var actionsRequests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/actions/") {
			actionsRequests++
		}
		if r.URL.Path != "/repos/acme/app/pulls/7" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"number": 7, "head": map[string]string{"sha": sha}, "base": map[string]any{"sha": strings.Repeat("b", 40), "repo": map[string]any{"name": "app", "private": true, "owner": map[string]string{"login": "acme"}}}})
	}))
	defer srv.Close()
	report := loadNoxReport(context.Background(), noxTestGitHubClient(srv), "acme", "app", 7, sha)
	if report.Availability != noxReportUnavailable || report.Reason != "Security reports are only available for public pull requests." || actionsRequests != 0 {
		t.Fatalf("private report was not rejected before Actions access: %+v, actions requests=%d", report, actionsRequests)
	}
}

func TestLoadNoxReportRequiresTheRepositorySecurityWorkflow(t *testing.T) {
	const sha = "cccccccccccccccccccccccccccccccccccccccc"
	archive := makeNoxArchiveFor(t, sha, `{"version":"2.1.0","runs":[{"tool":{"driver":{"name":"Nox"}},"results":[]}]}`)
	digest := sha256.Sum256(archive)
	var artifactRequests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/acme/app/pulls/7":
			_ = json.NewEncoder(w).Encode(map[string]any{"number": 7, "head": map[string]string{"sha": sha}, "base": map[string]any{"sha": strings.Repeat("b", 40), "repo": map[string]any{"name": "app", "private": false, "owner": map[string]string{"login": "acme"}}}})
		case "/repos/acme/app/actions/workflows/nox.yml/runs":
			_ = json.NewEncoder(w).Encode(map[string]any{"workflow_runs": []any{
				map[string]any{"id": 12, "path": "attacker.yml", "event": "pull_request", "head_sha": sha, "html_url": "https://github.com/acme/app/actions/runs/12", "created_at": "2026-11-15T13:59:00Z", "pull_requests": []any{map[string]any{"number": 7, "base": map[string]string{"sha": strings.Repeat("b", 40)}}}},
				map[string]any{"id": 11, "path": ".github/workflows/nox.yml", "event": "pull_request", "head_sha": sha, "html_url": "https://github.com/acme/app/actions/runs/11", "created_at": "2026-11-15T13:58:00Z", "pull_requests": []any{map[string]any{"number": 7, "base": map[string]string{"sha": strings.Repeat("b", 40)}}}},
			}})
		case "/repos/acme/app/actions/runs/11/artifacts":
			_ = json.NewEncoder(w).Encode(map[string]any{"artifacts": []any{map[string]any{"id": 44, "name": noxArtifactName, "digest": "sha256:" + hex.EncodeToString(digest[:]), "workflow_run": map[string]any{"id": 11, "head_sha": sha}}}})
		case "/repos/acme/app/actions/runs/12/artifacts":
			artifactRequests++
			_ = json.NewEncoder(w).Encode(map[string]any{"artifacts": []any{map[string]any{"id": 45, "name": noxArtifactName, "digest": "sha256:" + hex.EncodeToString(digest[:]), "workflow_run": map[string]any{"id": 12, "head_sha": sha}}}})
		case "/repos/acme/app/actions/artifacts/44/zip", "/repos/acme/app/actions/artifacts/45/zip":
			_, _ = w.Write(archive)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	report := loadNoxReport(context.Background(), noxTestGitHubClient(srv), "acme", "app", 7, sha)
	if report.Availability != noxReportAvailable || report.RunID != 11 || artifactRequests != 0 {
		t.Fatalf("report did not come from the repository security workflow: %+v, rejected-workflow artifact requests=%d", report, artifactRequests)
	}
}

func TestLoadNoxReportRejectsWorkflowMismatchWithAssociatedBase(t *testing.T) {
	const sha = "cccccccccccccccccccccccccccccccccccccccc"
	var artifactRequests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/acme/app/pulls/7":
			_ = json.NewEncoder(w).Encode(map[string]any{"number": 7, "head": map[string]string{"sha": sha}, "base": map[string]any{"sha": strings.Repeat("b", 40), "repo": map[string]any{"name": "app", "owner": map[string]string{"login": "acme"}}}})
		case "/repos/acme/app/contents/.github/workflows/nox.yml":
			blob := strings.Repeat("a", 40)
			if r.URL.Query().Get("ref") == sha {
				blob = strings.Repeat("c", 40)
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"sha": blob})
		case "/repos/acme/app/actions/workflows/nox.yml/runs":
			_ = json.NewEncoder(w).Encode(map[string]any{"workflow_runs": []any{map[string]any{"id": 11, "path": domain.NoxSecurityWorkflowPath, "event": "pull_request", "head_sha": sha, "created_at": "2026-11-15T13:58:00Z", "pull_requests": []any{map[string]any{"number": 7, "base": map[string]string{"sha": strings.Repeat("b", 40)}}}}}})
		case "/repos/acme/app/actions/runs/11/artifacts":
			artifactRequests++
			_ = json.NewEncoder(w).Encode(map[string]any{"artifacts": []any{map[string]any{"id": 44, "name": noxArtifactName}}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	report := loadNoxReport(context.Background(), &githubClient{baseURL: server.URL, http: server.Client()}, "acme", "app", 7, sha)
	if report.Availability != noxReportUnavailable || !strings.Contains(report.Reason, "workflow definition differs") || artifactRequests != 0 {
		t.Fatalf("an observed workflow mismatch must not supply scan evidence: %+v, artifact requests=%d", report, artifactRequests)
	}
}

func TestLoadNoxReportRequiresArtifactDigest(t *testing.T) {
	const sha = "cccccccccccccccccccccccccccccccccccccccc"
	archive := makeNoxArchiveFor(t, sha, `{"version":"2.1.0","runs":[{"tool":{"driver":{"name":"Nox"}},"results":[]}]}`)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/acme/app/pulls/7":
			_ = json.NewEncoder(w).Encode(map[string]any{"number": 7, "head": map[string]string{"sha": sha}, "base": map[string]any{"sha": strings.Repeat("b", 40), "repo": map[string]any{"name": "app", "owner": map[string]string{"login": "acme"}}}})
		case "/repos/acme/app/actions/workflows/nox.yml/runs":
			_ = json.NewEncoder(w).Encode(map[string]any{"workflow_runs": []any{map[string]any{"id": 11, "path": ".github/workflows/nox.yml", "event": "pull_request", "head_sha": sha, "created_at": "2026-11-15T13:58:00Z", "pull_requests": []any{map[string]any{"number": 7, "base": map[string]string{"sha": strings.Repeat("b", 40)}}}, "html_url": "https://github.com/acme/app/actions/runs/test"}}})
		case "/repos/acme/app/actions/runs/11/artifacts":
			_ = json.NewEncoder(w).Encode(map[string]any{"artifacts": []any{map[string]any{"id": 44, "name": noxArtifactName, "workflow_run": map[string]any{"id": 11, "head_sha": sha}}}})
		case "/repos/acme/app/actions/artifacts/44/zip":
			_, _ = w.Write(archive)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	report := loadNoxReport(context.Background(), noxTestGitHubClient(srv), "acme", "app", 7, sha)
	if report.Availability != noxReportUnavailable || report.Reason != "GitHub did not provide a verifiable SHA-256 digest for the Nox artifact." {
		t.Fatalf("artifact without digest should fail closed: %+v", report)
	}
}

func TestLoadNoxReportFindsSecurityWorkflowBeyondFirstPage(t *testing.T) {
	const sha = "cccccccccccccccccccccccccccccccccccccccc"
	archive := makeNoxArchiveFor(t, sha, `{"version":"2.1.0","runs":[{"tool":{"driver":{"name":"Nox"}},"results":[]}]}`)
	digest := sha256.Sum256(archive)
	var requestedPageTwo bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/acme/app/pulls/7":
			_ = json.NewEncoder(w).Encode(map[string]any{"number": 7, "head": map[string]string{"sha": sha}, "base": map[string]any{"sha": strings.Repeat("b", 40), "repo": map[string]any{"name": "app", "owner": map[string]string{"login": "acme"}}}})
		case r.URL.Path == "/repos/acme/app/actions/workflows/nox.yml/runs" && r.URL.Query().Get("page") == "1":
			runs := make([]map[string]any, noxPageSize)
			w.Header().Set("Link", `<https://api.github.com/next>; rel="next"`) // a full page with more to follow
			for index := range runs {
				runs[index] = map[string]any{"id": index + 100, "path": "other.yml", "event": "pull_request", "head_sha": sha, "created_at": "2026-11-15T13:58:00Z", "pull_requests": []any{map[string]any{"number": 7, "base": map[string]string{"sha": strings.Repeat("b", 40)}}}, "html_url": "https://github.com/acme/app/actions/runs/test"}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"workflow_runs": runs})
		case r.URL.Path == "/repos/acme/app/actions/workflows/nox.yml/runs" && r.URL.Query().Get("page") == "2":
			requestedPageTwo = true
			_ = json.NewEncoder(w).Encode(map[string]any{"workflow_runs": []any{map[string]any{"id": 11, "path": domain.NoxSecurityWorkflowPath, "event": "pull_request", "head_sha": sha, "created_at": "2026-11-15T13:57:00Z", "pull_requests": []any{map[string]any{"number": 7, "base": map[string]string{"sha": strings.Repeat("b", 40)}}}, "html_url": "https://github.com/acme/app/actions/runs/test"}}})
		case r.URL.Path == "/repos/acme/app/actions/runs/11/artifacts":
			_ = json.NewEncoder(w).Encode(map[string]any{"artifacts": []any{map[string]any{"id": 44, "name": noxArtifactName, "digest": "sha256:" + hex.EncodeToString(digest[:]), "workflow_run": map[string]any{"id": 11, "head_sha": sha}}}})
		case r.URL.Path == "/repos/acme/app/actions/artifacts/44/zip":
			_, _ = w.Write(archive)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	report := loadNoxReport(context.Background(), noxTestGitHubClient(srv), "acme", "app", 7, sha)
	if !requestedPageTwo || report.Availability != noxReportAvailable || report.RunID != 11 {
		t.Fatalf("later workflow page was not inspected: %+v, page two=%v", report, requestedPageTwo)
	}
}

func TestLoadNoxReportDoesNotCallCappedArtifactHistoryMissing(t *testing.T) {
	const sha = "cccccccccccccccccccccccccccccccccccccccc"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/acme/app/pulls/7":
			_ = json.NewEncoder(w).Encode(map[string]any{"number": 7, "head": map[string]string{"sha": sha}, "base": map[string]any{"sha": strings.Repeat("b", 40), "repo": map[string]any{"name": "app", "owner": map[string]string{"login": "acme"}}}})
		case r.URL.Path == "/repos/acme/app/actions/workflows/nox.yml/runs":
			_ = json.NewEncoder(w).Encode(map[string]any{"workflow_runs": []any{map[string]any{"id": 11, "path": domain.NoxSecurityWorkflowPath, "event": "pull_request", "head_sha": sha, "created_at": "2026-11-15T13:58:00Z", "pull_requests": []any{map[string]any{"number": 7, "base": map[string]string{"sha": strings.Repeat("b", 40)}}}, "html_url": "https://github.com/acme/app/actions/runs/test"}}})
		case r.URL.Path == "/repos/acme/app/actions/runs/11/artifacts":
			artifacts := make([]map[string]any, noxPageSize)
			w.Header().Set("Link", `<https://api.github.com/next>; rel="next"`) // a full page with more to follow
			for index := range artifacts {
				artifacts[index] = map[string]any{"id": index + 100, "name": noxArtifactName, "workflow_run": map[string]any{"id": 11, "head_sha": "ffffffffffffffffffffffffffffffffffffffff"}}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"artifacts": artifacts})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := noxTestGitHubClient(srv)
	report := loadNoxReport(context.Background(), client, "acme", "app", 7, sha)
	if report.Availability != noxReportUnavailable || report.Reason != noxInspectionIncompleteReason {
		t.Fatalf("capped artifact history was presented as a definitive miss: %+v", report)
	}
}

func TestLoadNoxReportDoesNotCallTruncatedWorkflowHistoryMissing(t *testing.T) {
	const sha = "cccccccccccccccccccccccccccccccccccccccc"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/acme/app/pulls/7":
			_ = json.NewEncoder(w).Encode(map[string]any{"number": 7, "head": map[string]string{"sha": sha}, "base": map[string]any{"sha": strings.Repeat("b", 40), "repo": map[string]any{"name": "app", "owner": map[string]string{"login": "acme"}}}})
		case "/repos/acme/app/actions/workflows/nox.yml/runs":
			runs := make([]map[string]any, noxPageSize)
			w.Header().Set("Link", `<https://api.github.com/next>; rel="next"`) // a full page with more to follow
			for index := range runs {
				path := "other.yml"
				if r.URL.Query().Get("page") == "1" && index == 0 {
					path = domain.NoxSecurityWorkflowPath
				}
				runs[index] = map[string]any{"id": index + 100, "path": path, "event": "pull_request", "head_sha": sha, "created_at": "2026-11-15T13:58:00Z", "pull_requests": []any{map[string]any{"number": 7, "base": map[string]string{"sha": strings.Repeat("b", 40)}}}, "html_url": "https://github.com/acme/app/actions/runs/test"}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"workflow_runs": runs})
		case "/repos/acme/app/actions/runs/100/artifacts":
			_ = json.NewEncoder(w).Encode(map[string]any{"artifacts": []any{map[string]any{
				"id": 1000, "name": noxArtifactName, "expired": true,
				"workflow_run": map[string]any{"id": 100, "head_sha": sha},
			}}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	report := loadNoxReport(context.Background(), noxTestGitHubClient(server), "acme", "app", 7, sha)
	if report.Availability != noxReportUnavailable || report.Reason != noxInspectionIncompleteReason {
		t.Fatalf("truncated workflow history was presented as a definitive miss: %+v", report)
	}
}

func TestLoadNoxReportCapsTotalGitHubRequests(t *testing.T) {
	const sha = "cccccccccccccccccccccccccccccccccccccccc"
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		switch {
		case r.URL.Path == "/repos/acme/app/pulls/7":
			_ = json.NewEncoder(w).Encode(map[string]any{"number": 7, "head": map[string]string{"sha": sha}, "base": map[string]any{"sha": strings.Repeat("b", 40), "repo": map[string]any{"name": "app", "owner": map[string]string{"login": "acme"}}}})
		case r.URL.Path == "/repos/acme/app/actions/workflows/nox.yml/runs":
			runs := make([]map[string]any, noxPageSize)
			for index := range runs {
				runs[index] = map[string]any{"id": index + 100, "path": domain.NoxSecurityWorkflowPath, "event": "pull_request", "head_sha": sha, "created_at": "2026-11-15T13:58:00Z", "pull_requests": []any{map[string]any{"number": 7, "base": map[string]string{"sha": strings.Repeat("b", 40)}}}, "html_url": "https://github.com/acme/app/actions/runs/test"}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"workflow_runs": runs})
		case strings.Contains(r.URL.Path, "/actions/runs/") && strings.HasSuffix(r.URL.Path, "/artifacts"):
			_ = json.NewEncoder(w).Encode(map[string]any{"artifacts": []any{}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := noxTestGitHubClient(srv)
	report := loadNoxReport(context.Background(), client, "acme", "app", 7, sha)
	if requests > maxNoxGitHubRequests {
		t.Fatalf("report lookup used %d GitHub requests; limit is %d", requests, maxNoxGitHubRequests)
	}
	if report.Availability != noxReportUnavailable || report.Reason != noxInspectionIncompleteReason {
		t.Fatalf("budget exhaustion was presented as a definitive miss: %+v", report)
	}
}

func TestLoadNoxReportDoesNotSayRevisionIsAbsentAfterCommitHistoryCap(t *testing.T) {
	const currentSHA = "dddddddddddddddddddddddddddddddddddddddd"
	const requestedSHA = "cccccccccccccccccccccccccccccccccccccccc"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/acme/app/pulls/7":
			_ = json.NewEncoder(w).Encode(map[string]any{"number": 7, "head": map[string]string{"sha": currentSHA}, "base": map[string]any{"sha": strings.Repeat("b", 40), "repo": map[string]any{"name": "app", "owner": map[string]string{"login": "acme"}}}})
		case r.URL.Path == "/repos/acme/app/pulls/7/commits":
			commits := make([]map[string]any, noxPageSize)
			for index := range commits {
				commits[index] = map[string]any{"sha": currentSHA}
			}
			_ = json.NewEncoder(w).Encode(commits)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := noxTestGitHubClient(srv)
	report := loadNoxReport(context.Background(), client, "acme", "app", 7, requestedSHA)
	if report.Availability != noxReportUnavailable || report.Reason != noxInspectionIncompleteReason {
		t.Fatalf("capped commit history was presented as a definitive revision mismatch: %+v", report)
	}
}

func TestNoxUnavailableReasonExplainsMissingActionsReadAccess(t *testing.T) {
	got := noxUnavailableReason(statusError{code: http.StatusUnauthorized})
	want := "GitHub requires a server token with Actions read access to download this workflow artifact."
	if got != want {
		t.Fatalf("401 reason = %q, want %q", got, want)
	}
}

func makeNoxArchive(t *testing.T, sarif string) []byte {
	t.Helper()
	return makeNoxArchiveEntries(t, map[string]string{noxSARIFFilename: sarif})
}

// makeNoxArchiveFor adds the workflow's record of the scanned commit.
func makeNoxArchiveFor(t *testing.T, scannedCommit, sarif string) []byte {
	t.Helper()
	return makeNoxArchiveEntries(t, map[string]string{noxSARIFFilename: sarif, noxScannedCommitFilename: scannedCommit + "\n"})
}

func makeNoxArchiveEntries(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for name, contents := range entries {
		file, err := writer.Create("nox-results/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fmt.Fprint(file, contents); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func TestLoadNoxReportDownloadsWithTokenOnlyAfterConfirmingPublicRepository(t *testing.T) {
	const sha = "cccccccccccccccccccccccccccccccccccccccc"
	archive := makeNoxArchiveFor(t, sha, `{"version":"2.1.0","runs":[{"tool":{"driver":{"name":"Nox"}},"results":[]}]}`)
	digest := sha256.Sum256(archive)
	// A signed-in user's token cannot download artifacts, so the server token
	// still does, and the visibility check must run even when the user has a token.
	for _, tc := range []struct {
		name          string
		visibility    string
		userToken     string
		wantAvailable bool
		wantDownloads int
	}{
		{"public repository", `{"private":false}`, "", true, 1},
		{"private repository", `{"private":true}`, "", false, 0},
		{"visibility unknown", `{}`, "", false, 0},
		{"signed in, public repository", `{"private":false}`, "user-token", true, 1},
		{"signed in, private repository", `{"private":true}`, "user-token", false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var downloads int
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				authorized := r.Header.Get("Authorization") == "Bearer server-token"
				w.Header().Set("X-OAuth-Scopes", "read:user")
				switch r.URL.Path {
				case "/repos/acme/app":
					if !authorized {
						t.Error("visibility check did not use the server token")
					}
					_, _ = w.Write([]byte(tc.visibility))
					return
				case "/repos/acme/app/actions/artifacts/44/zip":
					if !authorized {
						t.Error("artifact download did not use the server token")
					}
					downloads++
					_, _ = w.Write(archive)
					return
				}
				if authorized {
					t.Errorf("server token sent on a data request: %s", r.URL.Path)
				}
				switch r.URL.Path {
				case "/repos/acme/app/pulls/7":
					_ = json.NewEncoder(w).Encode(map[string]any{"number": 7, "head": map[string]string{"sha": sha}, "base": map[string]any{"sha": strings.Repeat("b", 40), "repo": map[string]any{"name": "app", "owner": map[string]string{"login": "acme"}}}})
				case "/repos/acme/app/actions/workflows/nox.yml/runs":
					_ = json.NewEncoder(w).Encode(map[string]any{"workflow_runs": []any{
						map[string]any{"id": 11, "path": domain.NoxSecurityWorkflowPath, "name": "Security", "event": "pull_request", "head_sha": sha, "conclusion": "success", "html_url": "https://github.com/acme/app/actions/runs/11", "created_at": "2026-11-15T13:58:00Z", "pull_requests": []any{map[string]any{"number": 7, "base": map[string]string{"sha": strings.Repeat("b", 40)}}}},
					}})
				case "/repos/acme/app/actions/runs/11/artifacts":
					_ = json.NewEncoder(w).Encode(map[string]any{"artifacts": []any{map[string]any{
						"id": 44, "name": noxArtifactName, "size_in_bytes": len(archive), "digest": "sha256:" + hex.EncodeToString(digest[:]),
						"url":        "https://api.github.com/repos/acme/app/actions/artifacts/44",
						"created_at": "2026-11-15T13:58:17Z", "expires_at": "2099-11-22T13:58:16Z",
						"workflow_run": map[string]any{"id": 11, "head_sha": sha},
					}}})
				default:
					http.NotFound(w, r)
				}
			}))
			defer srv.Close()
			gh := noxTestGitHubClient(srv)
			gh.token = "server-token"
			ctx := context.Background()
			if tc.userToken != "" {
				ctx = withGitHubUserToken(ctx, tc.userToken, nil)
			}
			report := loadNoxReport(ctx, gh, "acme", "app", 7, sha)
			if (report.Availability == noxReportAvailable) != tc.wantAvailable || downloads != tc.wantDownloads {
				t.Fatalf("availability=%s reason=%q downloads=%d, want available=%v downloads=%d", report.Availability, report.Reason, downloads, tc.wantAvailable, tc.wantDownloads)
			}
		})
	}
}

func TestParseNoxSARIFAppliesEverySuppressionRecordBeyondRetainedOnes(t *testing.T) {
	const sha = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	records := make([]string, 0, domain.MaxNoxSuppressionsPerFinding+1)
	for i := 0; i < domain.MaxNoxSuppressionsPerFinding; i++ {
		records = append(records, `{"kind":"external","status":"accepted","justification":"Approved"}`)
	}
	records = append(records, `{"kind":"external","status":"rejected","justification":"Rejected later"}`)
	data := []byte(`{"version":"2.1.0","runs":[{"tool":{"driver":{"name":"Nox"}},"results":[{"ruleId":"SEC-001","message":{"text":"Rejected after many approvals."},"suppressions":[` + strings.Join(records, ",") + `]}]}]}`)
	report, err := parseNoxSARIF(data, sha, 1, "https://github.com/acme/app/actions/runs/1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if report.ActiveCount != 1 || report.SuppressedCount != 0 || report.Findings[0].Suppressed || !report.Findings[0].SuppressionsTruncated {
		t.Fatalf("a rejected record beyond the retained ones must keep the result active: %+v", report)
	}
}

func TestParseNoxSARIFFollowsSARIFSuppressionStatusRules(t *testing.T) {
	const sha = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	data := []byte(`{"version":"2.1.0","runs":[{"tool":{"driver":{"name":"Nox"}},"results":[` +
		`{"ruleId":"NO-STATUS","message":{"text":"In-source suppression without a status."},"suppressions":[{"kind":"inSource"}]},` +
		`{"ruleId":"MIXED","message":{"text":"Accepted and rejected."},"suppressions":[{"kind":"external","status":"accepted"},{"kind":"external","status":"rejected"}]}` +
		`]}]}`)
	report, err := parseNoxSARIF(data, sha, 1, "https://github.com/acme/app/actions/runs/1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if !report.Findings[0].Suppressed || report.Findings[1].Suppressed || report.ActiveCount != 1 || report.SuppressedCount != 1 {
		t.Fatalf("a missing status counts as accepted and any rejected record keeps a result active: %+v", report)
	}
}

func TestLoadNoxReportRejectsReportWhenBaseWorkflowCannotBeVerified(t *testing.T) {
	const sha = "cccccccccccccccccccccccccccccccccccccccc"
	baseSHA := strings.Repeat("b", 40)
	archive := makeNoxArchiveFor(t, sha, `{"version":"2.1.0","runs":[{"tool":{"driver":{"name":"Nox"}},"results":[]}]}`)
	digest := sha256.Sum256(archive)
	for _, tc := range []struct {
		name         string
		pullRequests []any
		baseWorkflow func(http.ResponseWriter)
	}{
		{"workflow added by the pull request", []any{map[string]any{"number": 7, "base": map[string]string{"sha": baseSHA}}}, func(w http.ResponseWriter) { w.WriteHeader(http.StatusNotFound) }},
		{"malformed base blob SHA", []any{map[string]any{"number": 7, "base": map[string]string{"sha": baseSHA}}}, func(w http.ResponseWriter) { _, _ = w.Write([]byte(`{"sha":"not-a-sha"}`)) }},
		{"no base SHA from GitHub", []any{map[string]any{"number": 7}}, func(w http.ResponseWriter) { t.Error("base workflow requested without a base SHA") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var downloads int
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/repos/acme/app/pulls/7":
					_ = json.NewEncoder(w).Encode(map[string]any{"number": 7, "head": map[string]string{"sha": sha}, "base": map[string]any{"sha": baseSHA, "repo": map[string]any{"name": "app", "owner": map[string]string{"login": "acme"}}}})
				case "/repos/acme/app/contents/" + domain.NoxSecurityWorkflowPath:
					if r.URL.Query().Get("ref") == sha {
						_, _ = w.Write([]byte(`{"sha":"` + noxTrustedWorkflowBlobSHA + `"}`))
						return
					}
					tc.baseWorkflow(w)
				case "/repos/acme/app/actions/workflows/nox.yml/runs":
					_ = json.NewEncoder(w).Encode(map[string]any{"workflow_runs": []any{
						map[string]any{"id": 11, "path": domain.NoxSecurityWorkflowPath, "name": "Security", "event": "pull_request", "head_sha": sha, "conclusion": "success", "html_url": "https://github.com/acme/app/actions/runs/11", "created_at": "2026-11-15T13:58:00Z", "pull_requests": tc.pullRequests},
					}})
				case "/repos/acme/app/actions/runs/11/artifacts":
					_ = json.NewEncoder(w).Encode(map[string]any{"artifacts": []any{map[string]any{
						"id": 44, "name": noxArtifactName, "size_in_bytes": len(archive), "digest": "sha256:" + hex.EncodeToString(digest[:]),
						"url": "https://api.github.com/repos/acme/app/actions/artifacts/44", "created_at": "2026-11-15T13:58:17Z", "expires_at": "2099-11-22T13:58:16Z",
						"workflow_run": map[string]any{"id": 11, "head_sha": sha},
					}}})
				case "/repos/acme/app/actions/artifacts/44/zip":
					downloads++
					_, _ = w.Write(archive)
				default:
					http.NotFound(w, r)
				}
			}))
			defer srv.Close()
			report := loadNoxReport(context.Background(), &githubClient{baseURL: srv.URL, http: srv.Client()}, "acme", "app", 7, sha)
			if report.Availability == noxReportAvailable || downloads != 0 {
				t.Fatalf("unverified workflow accepted: availability=%s reason=%q downloads=%d", report.Availability, report.Reason, downloads)
			}
		})
	}
}

// noxScenario serves one PR revision with configurable workflow runs; each run
// has its own base SHA, workflow blob at that base, artifact listing and archive.
type noxScenarioRun struct {
	id             int64
	created        string
	baseSHA        string
	baseBlob       string
	listStatus     int
	downloadStatus int
}

func newNoxScenario(t *testing.T, sha string, runsPages [][]noxScenarioRun) (*githubClient, *int) {
	t.Helper()
	archive := makeNoxArchiveFor(t, sha, `{"version":"2.1.0","runs":[{"tool":{"driver":{"name":"Nox"}},"results":[]}]}`)
	digest := sha256.Sum256(archive)
	byID := map[int64]noxScenarioRun{}
	blobByRef := map[string]string{sha: noxTrustedWorkflowBlobSHA}
	for _, page := range runsPages {
		for _, run := range page {
			byID[run.id] = run
			blobByRef[run.baseSHA] = run.baseBlob
		}
	}
	downloads := 0
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/acme/app/pulls/7":
			_ = json.NewEncoder(w).Encode(map[string]any{"number": 7, "head": map[string]string{"sha": sha}, "base": map[string]any{"sha": strings.Repeat("b", 40), "repo": map[string]any{"name": "app", "owner": map[string]string{"login": "acme"}}}})
		case r.URL.Path == "/repos/acme/app/contents/"+domain.NoxSecurityWorkflowPath:
			_, _ = w.Write([]byte(`{"sha":"` + blobByRef[r.URL.Query().Get("ref")] + `"}`))
		case r.URL.Path == "/repos/acme/app/actions/workflows/nox.yml/runs":
			page := 1
			_, _ = fmt.Sscanf(r.URL.Query().Get("page"), "%d", &page)
			if page < len(runsPages) {
				w.Header().Set("Link", fmt.Sprintf(`<%s%s?page=%d>; rel="next"`, srv.URL, r.URL.Path, page+1))
			}
			runs := []any{}
			if page <= len(runsPages) {
				for _, run := range runsPages[page-1] {
					runs = append(runs, map[string]any{"id": run.id, "path": domain.NoxSecurityWorkflowPath, "name": "Security", "event": "pull_request", "head_sha": sha, "conclusion": "success", "html_url": fmt.Sprintf("https://github.com/acme/app/actions/runs/%d", run.id), "created_at": run.created, "pull_requests": []any{map[string]any{"number": 7, "base": map[string]string{"sha": run.baseSHA}}}})
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"workflow_runs": runs})
		case strings.HasPrefix(r.URL.Path, "/repos/acme/app/actions/runs/") && strings.HasSuffix(r.URL.Path, "/artifacts"):
			var id int64
			_, _ = fmt.Sscanf(strings.TrimPrefix(r.URL.Path, "/repos/acme/app/actions/runs/"), "%d", &id)
			run := byID[id]
			if run.listStatus != 0 {
				w.WriteHeader(run.listStatus)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"artifacts": []any{map[string]any{
				"id": id * 10, "name": noxArtifactName, "size_in_bytes": len(archive), "digest": "sha256:" + hex.EncodeToString(digest[:]),
				"url": fmt.Sprintf("https://api.github.com/repos/acme/app/actions/artifacts/%d", id*10), "created_at": run.created, "expires_at": "2099-11-22T13:58:16Z",
				"workflow_run": map[string]any{"id": id, "head_sha": sha},
			}}})
		case strings.HasPrefix(r.URL.Path, "/repos/acme/app/actions/artifacts/") && strings.HasSuffix(r.URL.Path, "/zip"):
			var artifactID int64
			_, _ = fmt.Sscanf(strings.TrimPrefix(r.URL.Path, "/repos/acme/app/actions/artifacts/"), "%d", &artifactID)
			if status := byID[artifactID/10].downloadStatus; status != 0 {
				w.WriteHeader(status)
				return
			}
			downloads++
			_, _ = w.Write(archive)
		case strings.HasPrefix(r.URL.Path, "/repos/acme/app/compare/"):
			_, _ = w.Write([]byte(`{"files":[]}`)) // the PR changed no scan-control files
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return &githubClient{baseURL: srv.URL, http: srv.Client()}, &downloads
}

func TestLoadNoxReportFollowsNextLinkPastShortPage(t *testing.T) {
	const sha = "cccccccccccccccccccccccccccccccccccccccc"
	base := strings.Repeat("b", 40)
	gh, _ := newNoxScenario(t, sha, [][]noxScenarioRun{{}, {{id: 11, created: "2026-11-15T13:58:00Z", baseSHA: base, baseBlob: noxTrustedWorkflowBlobSHA}}})
	report := loadNoxReport(context.Background(), gh, "acme", "app", 7, sha)
	if report.Availability != noxReportAvailable || report.RunID != 11 {
		t.Fatalf("report on the page after a short page was missed: %+v", report)
	}
}

func TestLoadNoxReportFallsBackToOlderRunWhenArtifactListingFails(t *testing.T) {
	const sha = "cccccccccccccccccccccccccccccccccccccccc"
	base := strings.Repeat("b", 40)
	gh, _ := newNoxScenario(t, sha, [][]noxScenarioRun{{
		{id: 12, created: "2026-11-15T14:00:00Z", baseSHA: base, baseBlob: noxTrustedWorkflowBlobSHA, listStatus: http.StatusBadGateway},
		{id: 11, created: "2026-11-15T13:58:00Z", baseSHA: base, baseBlob: noxTrustedWorkflowBlobSHA},
	}})
	report := loadNoxReport(context.Background(), gh, "acme", "app", 7, sha)
	if report.Availability != noxReportAvailable || report.RunID != 11 {
		t.Fatalf("older valid report was not inspected after a temporary listing failure: %+v", report)
	}
}

func TestLoadNoxReportReasonMatchesTheReportedArtifact(t *testing.T) {
	const sha = "cccccccccccccccccccccccccccccccccccccccc"
	gh, _ := newNoxScenario(t, sha, [][]noxScenarioRun{{
		{id: 12, created: "2026-11-15T14:00:00Z", baseSHA: strings.Repeat("d", 40), baseBlob: strings.Repeat("e", 40)},
		{id: 11, created: "2026-11-15T13:58:00Z", baseSHA: strings.Repeat("b", 40), baseBlob: noxTrustedWorkflowBlobSHA, downloadStatus: http.StatusBadGateway},
	}})
	report := loadNoxReport(context.Background(), gh, "acme", "app", 7, sha)
	if report.Availability == noxReportAvailable || report.RunID != 11 || strings.Contains(report.Reason, "differs from the base") {
		t.Fatalf("reason does not describe the reported run's download failure: run=%d reason=%q", report.RunID, report.Reason)
	}
}

func TestLoadNoxReportUsesValidArtifactFromFullFinalArtifactPage(t *testing.T) {
	const sha = "cccccccccccccccccccccccccccccccccccccccc"
	archive := makeNoxArchiveFor(t, sha, `{"version":"2.1.0","runs":[{"tool":{"driver":{"name":"Nox"}},"results":[]}]}`)
	digest := sha256.Sum256(archive)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/acme/app/pulls/7":
			_ = json.NewEncoder(w).Encode(map[string]any{"number": 7, "head": map[string]string{"sha": sha}, "base": map[string]any{"sha": strings.Repeat("b", 40), "repo": map[string]any{"name": "app", "owner": map[string]string{"login": "acme"}}}})
		case r.URL.Path == "/repos/acme/app/actions/workflows/nox.yml/runs":
			_ = json.NewEncoder(w).Encode(map[string]any{"workflow_runs": []any{map[string]any{"id": 11, "path": domain.NoxSecurityWorkflowPath, "event": "pull_request", "head_sha": sha, "created_at": "2026-11-15T13:58:00Z", "pull_requests": []any{map[string]any{"number": 7, "base": map[string]string{"sha": strings.Repeat("b", 40)}}}, "html_url": "https://github.com/acme/app/actions/runs/test"}}})
		case r.URL.Path == "/repos/acme/app/actions/runs/11/artifacts":
			artifacts := make([]map[string]any, noxPageSize)
			w.Header().Set("Link", `<https://api.github.com/next>; rel="next"`) // every page is full with more to follow
			for index := range artifacts {
				artifacts[index] = map[string]any{"id": index + 100, "name": noxArtifactName, "workflow_run": map[string]any{"id": 11, "head_sha": "ffffffffffffffffffffffffffffffffffffffff"}}
			}
			if r.URL.Query().Get("page") == strconv.Itoa(maxNoxPages) {
				artifacts[0] = map[string]any{"id": 44, "name": noxArtifactName, "digest": "sha256:" + hex.EncodeToString(digest[:]), "workflow_run": map[string]any{"id": 11, "head_sha": sha}}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"artifacts": artifacts})
		case r.URL.Path == "/repos/acme/app/actions/artifacts/44/zip":
			_, _ = w.Write(archive)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	report := loadNoxReport(context.Background(), noxTestGitHubClient(srv), "acme", "app", 7, sha)
	if report.Availability != noxReportAvailable || report.ArtifactID != 44 {
		t.Fatalf("valid artifact on the last inspected page was reported unavailable: %+v", report)
	}
}

func TestParseNoxSARIFDoesNotTreatAbsentResultsAsACleanScan(t *testing.T) {
	const sha = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	for _, test := range []struct{ name, runs string }{
		{name: "absent", runs: `[{"tool":{"driver":{"name":"Nox"}}}]`},
		{name: "null", runs: `[{"tool":{"driver":{"name":"Nox"}},"results":null}]`},
		{name: "absent beside a run with results", runs: `[{"tool":{"driver":{"name":"Nox"}},"results":[]},{"tool":{"driver":{"name":"Nox"}}}]`},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := parseNoxSARIF([]byte(`{"version":"2.1.0","runs":`+test.runs+`}`), sha, 1, "", "", ""); err == nil {
				t.Fatal("a run without a results array was accepted; SARIF 2.1.0 §3.14.23 defaults it to null (analysis did not run)")
			}
		})
	}
	if _, err := parseNoxSARIF([]byte(`{"version":"2.1.0","runs":[{"tool":{"driver":{"name":"Nox"}},"results":[]}]}`), sha, 1, "", "", ""); err != nil {
		t.Fatalf("an empty results array is a completed scan with no results: %v", err)
	}
}

// truncatedArtifactRunServer serves two matching runs: run 12 (newer) whose
// artifact history fills every inspected page, and run 11 (older) with a valid
// report. lastPageArtifact, when set, is placed on run 12's final page.
func truncatedArtifactRunServer(t *testing.T, sha string, lastPageArtifact map[string]any) *httptest.Server {
	t.Helper()
	archive := makeNoxArchiveFor(t, sha, `{"version":"2.1.0","runs":[{"tool":{"driver":{"name":"Nox"}},"results":[]}]}`)
	digest := sha256.Sum256(archive)
	run := func(id int, createdAt string) map[string]any {
		return map[string]any{"id": id, "path": domain.NoxSecurityWorkflowPath, "event": "pull_request", "head_sha": sha, "created_at": createdAt, "pull_requests": []any{map[string]any{"number": 7, "base": map[string]string{"sha": strings.Repeat("b", 40)}}}, "html_url": "https://github.com/acme/app/actions/runs/test"}
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/acme/app/pulls/7":
			_ = json.NewEncoder(w).Encode(map[string]any{"number": 7, "head": map[string]string{"sha": sha}, "base": map[string]any{"sha": strings.Repeat("b", 40), "repo": map[string]any{"name": "app", "owner": map[string]string{"login": "acme"}}}})
		case r.URL.Path == "/repos/acme/app/actions/workflows/nox.yml/runs":
			_ = json.NewEncoder(w).Encode(map[string]any{"workflow_runs": []any{run(12, "2026-11-15T14:00:00Z"), run(11, "2026-11-15T13:00:00Z")}})
		case r.URL.Path == "/repos/acme/app/actions/runs/12/artifacts":
			artifacts := make([]map[string]any, noxPageSize)
			w.Header().Set("Link", `<https://api.github.com/next>; rel="next"`)
			for index := range artifacts {
				artifacts[index] = map[string]any{"id": index + 100, "name": noxArtifactName, "workflow_run": map[string]any{"id": 12, "head_sha": "ffffffffffffffffffffffffffffffffffffffff"}}
			}
			if lastPageArtifact != nil && r.URL.Query().Get("page") == strconv.Itoa(maxNoxPages) {
				artifacts[0] = lastPageArtifact
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"artifacts": artifacts})
		case r.URL.Path == "/repos/acme/app/actions/runs/11/artifacts":
			_ = json.NewEncoder(w).Encode(map[string]any{"artifacts": []any{map[string]any{"id": 44, "name": noxArtifactName, "digest": "sha256:" + hex.EncodeToString(digest[:]), "workflow_run": map[string]any{"id": 11, "head_sha": sha}}}})
		case strings.HasPrefix(r.URL.Path, "/repos/acme/app/actions/artifacts/") && strings.HasSuffix(r.URL.Path, "/zip"):
			_, _ = w.Write(archive)
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestLoadNoxReportDoesNotSkipPastTruncatedNewerRunToOlderReport(t *testing.T) {
	const sha = "cccccccccccccccccccccccccccccccccccccccc"
	srv := truncatedArtifactRunServer(t, sha, nil)
	defer srv.Close()

	report := loadNoxReport(context.Background(), noxTestGitHubClient(srv), "acme", "app", 7, sha)
	if report.Availability != noxReportUnavailable || report.Reason != noxInspectionIncompleteReason || report.ArtifactID != 0 {
		t.Fatalf("an older run's report was shown although the newer run's artifacts were not fully inspected: %+v", report)
	}
}

func TestLoadNoxReportKeepsCandidateReasonWithItsProvenanceWhenHistoryIsTruncated(t *testing.T) {
	const sha = "cccccccccccccccccccccccccccccccccccccccc"
	badDigest := map[string]any{"id": 77, "name": noxArtifactName, "digest": "sha256:" + strings.Repeat("0", 64), "workflow_run": map[string]any{"id": 12, "head_sha": sha}}
	srv := truncatedArtifactRunServer(t, sha, badDigest)
	defer srv.Close()

	report := loadNoxReport(context.Background(), noxTestGitHubClient(srv), "acme", "app", 7, sha)
	if report.Availability != noxReportUnavailable || report.ArtifactID != 77 || report.Reason == "" || report.Reason == noxInspectionIncompleteReason {
		t.Fatalf("the shown artifact's provenance was not paired with its own failure reason: %+v", report)
	}
}

func TestParseNoxSARIFSkipsPassingEvaluations(t *testing.T) {
	const sha = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	data := `{"version":"2.1.0","runs":[{"tool":{"driver":{"name":"Nox"}},"results":[
		{"ruleId":"SEC-1","kind":"pass","level":"none","message":{"text":"Checked"}},
		{"ruleId":"SEC-2","kind":"notApplicable","level":"none","message":{"text":"Not applicable"}},
		{"ruleId":"SEC-3","level":"warning","message":{"text":"Real finding"}}]}]}`
	report, err := parseNoxSARIF([]byte(data), sha, 1, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if report.TotalCount != 1 || report.ActiveCount != 1 || len(report.Findings) != 1 || report.Findings[0].RuleID != "SEC-3" {
		t.Fatalf("passing evaluations were counted as findings: %+v", report)
	}
}

// scanControlScenario serves one matching run with a valid report. compare and
// noxConfig, when set, answer the comparison and the .nox.yaml lookup.
func scanControlScenario(t *testing.T, sha string, compare func(http.ResponseWriter), noxConfig string) *httptest.Server {
	t.Helper()
	archive := makeNoxArchiveFor(t, sha, `{"version":"2.1.0","runs":[{"tool":{"driver":{"name":"Nox"}},"results":[]}]}`)
	digest := sha256.Sum256(archive)
	base := strings.Repeat("b", 40)
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/acme/app/pulls/7":
			_ = json.NewEncoder(w).Encode(map[string]any{"number": 7, "head": map[string]string{"sha": sha}, "base": map[string]any{"sha": base, "repo": map[string]any{"name": "app", "owner": map[string]string{"login": "acme"}}}})
		case r.URL.Path == "/repos/acme/app/actions/workflows/nox.yml/runs":
			_ = json.NewEncoder(w).Encode(map[string]any{"workflow_runs": []any{map[string]any{"id": 11, "path": domain.NoxSecurityWorkflowPath, "event": "pull_request", "head_sha": sha, "created_at": "2026-11-15T13:58:00Z", "pull_requests": []any{map[string]any{"number": 7, "base": map[string]string{"sha": base}}}, "html_url": "https://github.com/acme/app/actions/runs/test"}}})
		case r.URL.Path == "/repos/acme/app/compare/"+base+"..."+sha && compare != nil:
			compare(w)
		case r.URL.Path == "/repos/acme/app/contents/.nox.yaml" && noxConfig != "":
			if r.URL.Query().Get("ref") != sha {
				t.Errorf(".nox.yaml was read at %q, not the scanned revision", r.URL.Query().Get("ref"))
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"type": "file", "encoding": "base64", "content": base64.StdEncoding.EncodeToString([]byte(noxConfig))})
		case r.URL.Path == "/repos/acme/app/actions/runs/11/artifacts":
			_ = json.NewEncoder(w).Encode(map[string]any{"artifacts": []any{map[string]any{"id": 44, "name": noxArtifactName, "digest": "sha256:" + hex.EncodeToString(digest[:]), "workflow_run": map[string]any{"id": 11, "head_sha": sha}}}})
		case r.URL.Path == "/repos/acme/app/actions/artifacts/44/zip":
			_, _ = w.Write(archive)
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestLoadNoxReportRejectsReportWhenPullRequestChangesScanControlFile(t *testing.T) {
	const sha = "cccccccccccccccccccccccccccccccccccccccc"
	for _, test := range []struct{ name, file, previous string }{
		{name: "baseline", file: ".nox/baseline.json"},
		{name: "project config", file: ".nox.yaml"},
		{name: "nested gitignore", file: "backend/.gitignore"},
		{name: "renamed away from baseline", file: "docs/old-baseline.json", previous: ".nox/baseline.json"},
	} {
		t.Run(test.name, func(t *testing.T) {
			srv := scanControlScenario(t, sha, func(w http.ResponseWriter) {
				_ = json.NewEncoder(w).Encode(map[string]any{"files": []any{map[string]any{"filename": "README.md"}, map[string]any{"filename": test.file, "previous_filename": test.previous}}})
			}, "")
			defer srv.Close()
			report := loadNoxReport(context.Background(), noxTestGitHubClient(srv), "acme", "app", 7, sha)
			named := test.file
			if test.previous != "" {
				named = test.previous
			}
			if report.Availability != noxReportUnavailable || !strings.Contains(report.Reason, named) || !strings.Contains(report.Reason, "not trusted") {
				t.Fatalf("a report from a PR that changed %s was accepted: %+v", named, report)
			}
		})
	}
}

func TestLoadNoxReportRejectsReportWhenScanControlChangesCannotBeVerified(t *testing.T) {
	const sha = "cccccccccccccccccccccccccccccccccccccccc"
	for _, test := range []struct {
		name    string
		compare func(http.ResponseWriter)
	}{
		{name: "comparison unavailable", compare: func(w http.ResponseWriter) { http.Error(w, "boom", http.StatusUnprocessableEntity) }},
		{name: "file list may be truncated", compare: func(w http.ResponseWriter) {
			files := make([]map[string]any, noxCompareFileLimit)
			for index := range files {
				files[index] = map[string]any{"filename": fmt.Sprintf("src/file-%d.go", index)}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"files": files})
		}},
		{name: "file list missing", compare: func(w http.ResponseWriter) { _, _ = w.Write([]byte(`{}`)) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			srv := scanControlScenario(t, sha, test.compare, "")
			defer srv.Close()
			report := loadNoxReport(context.Background(), noxTestGitHubClient(srv), "acme", "app", 7, sha)
			if report.Availability != noxReportUnavailable || !strings.Contains(report.Reason, "not trusted") {
				t.Fatalf("an unverifiable comparison was treated as trusted: %+v", report)
			}
		})
	}
}

func TestLoadNoxReportRejectsReportWhenConfigMovesBaseline(t *testing.T) {
	const sha = "cccccccccccccccccccccccccccccccccccccccc"
	srv := scanControlScenario(t, sha, nil, "policy:\n  baseline_path: security/accepted.json\n")
	defer srv.Close()
	report := loadNoxReport(context.Background(), noxTestGitHubClient(srv), "acme", "app", 7, sha)
	if report.Availability != noxReportUnavailable || !strings.Contains(report.Reason, "baseline") {
		t.Fatalf("a report whose baseline lives outside the checked files was accepted: %+v", report)
	}
}

func TestLoadNoxReportAcceptsReportWhenNoScanControlFileChanged(t *testing.T) {
	const sha = "cccccccccccccccccccccccccccccccccccccccc"
	srv := scanControlScenario(t, sha, func(w http.ResponseWriter) {
		_ = json.NewEncoder(w).Encode(map[string]any{"files": []any{map[string]any{"filename": "backend/main.go"}}})
	}, "scan:\n  exclude: []\n")
	defer srv.Close()
	report := loadNoxReport(context.Background(), noxTestGitHubClient(srv), "acme", "app", 7, sha)
	if report.Availability != noxReportAvailable {
		t.Fatalf("a report from a PR that changed only application code was rejected: %+v", report)
	}
}

func TestLoadNoxReportRequiresArtifactToRecordTheScannedRevision(t *testing.T) {
	const sha = "cccccccccccccccccccccccccccccccccccccccc"
	const sarif = `{"version":"2.1.0","runs":[{"tool":{"driver":{"name":"Nox"}},"results":[]}]}`
	for _, test := range []struct {
		name    string
		archive func(t *testing.T) []byte
		want    string
	}{
		{name: "record missing", archive: func(t *testing.T) []byte { return makeNoxArchive(t, sarif) }, want: "does not record which commit"},
		{name: "merge preview scanned", archive: func(t *testing.T) []byte { return makeNoxArchiveFor(t, strings.Repeat("d", 40), sarif) }, want: "different commit"},
		{name: "record unreadable", archive: func(t *testing.T) []byte { return makeNoxArchiveFor(t, "not-a-sha", sarif) }, want: "does not record which commit"},
	} {
		t.Run(test.name, func(t *testing.T) {
			archive := test.archive(t)
			digest := sha256.Sum256(archive)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/repos/acme/app/pulls/7":
					_ = json.NewEncoder(w).Encode(map[string]any{"number": 7, "head": map[string]string{"sha": sha}, "base": map[string]any{"sha": strings.Repeat("b", 40), "repo": map[string]any{"name": "app", "owner": map[string]string{"login": "acme"}}}})
				case "/repos/acme/app/actions/workflows/nox.yml/runs":
					_ = json.NewEncoder(w).Encode(map[string]any{"workflow_runs": []any{map[string]any{"id": 11, "path": domain.NoxSecurityWorkflowPath, "event": "pull_request", "head_sha": sha, "created_at": "2026-11-15T13:58:00Z", "pull_requests": []any{map[string]any{"number": 7, "base": map[string]string{"sha": strings.Repeat("b", 40)}}}, "html_url": "https://github.com/acme/app/actions/runs/test"}}})
				case "/repos/acme/app/actions/runs/11/artifacts":
					_ = json.NewEncoder(w).Encode(map[string]any{"artifacts": []any{map[string]any{"id": 44, "name": noxArtifactName, "digest": "sha256:" + hex.EncodeToString(digest[:]), "workflow_run": map[string]any{"id": 11, "head_sha": sha}}}})
				case "/repos/acme/app/actions/artifacts/44/zip":
					_, _ = w.Write(archive)
				default:
					http.NotFound(w, r)
				}
			}))
			defer srv.Close()
			report := loadNoxReport(context.Background(), noxTestGitHubClient(srv), "acme", "app", 7, sha)
			if report.Availability != noxReportUnavailable || !strings.Contains(report.Reason, test.want) || report.ArtifactID != 44 {
				t.Fatalf("report without a matching scanned-commit record was accepted or misattributed: %+v", report)
			}
		})
	}
}
