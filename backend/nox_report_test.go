package main

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/felixgeelhaar/ghostline/backend/internal/domain"
)

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
	archive := makeNoxArchive(t, `{"version":"2.1.0","runs":[{"tool":{"driver":{"name":"Nox"}},"results":[]}]}`)
	digest := sha256.Sum256(archive)
	var artifactRequests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/acme/app/pulls/7":
			_ = json.NewEncoder(w).Encode(map[string]any{"number": 7, "head": map[string]string{"sha": sha}, "base": map[string]any{"repo": map[string]any{"name": "app", "owner": map[string]string{"login": "acme"}}}})
		case "/repos/acme/app/actions/workflows/nox.yml/runs":
			if r.URL.Query().Get("head_sha") != sha || r.URL.Query().Get("event") != "pull_request" {
				t.Errorf("workflow query not scoped to requested revision and PR event: %s", r.URL.RawQuery)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"workflow_runs": []any{
				map[string]any{"id": 12, "path": domain.NoxSecurityWorkflowPath, "name": "Security", "event": "pull_request", "head_sha": "dddddddddddddddddddddddddddddddddddddddd", "html_url": "https://github.com/acme/app/actions/runs/12", "created_at": "2026-11-15T13:59:00Z", "pull_requests": []any{map[string]any{"number": 7}}},
				map[string]any{"id": 11, "path": domain.NoxSecurityWorkflowPath + "@refs/pull/7/merge", "name": "Security", "event": "pull_request", "head_sha": sha, "conclusion": "success", "html_url": "https://github.com/acme/app/actions/runs/11", "created_at": "2026-11-15T13:58:00Z", "pull_requests": []any{map[string]any{"number": 7}}},
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
	gh := &githubClient{baseURL: srv.URL, http: srv.Client()}
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
			_ = json.NewEncoder(w).Encode(map[string]any{"number": 7, "head": map[string]string{"sha": sha}, "base": map[string]any{"repo": map[string]any{"name": "app", "owner": map[string]string{"login": "acme"}}}})
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

	report := loadNoxReport(context.Background(), &githubClient{baseURL: server.URL, http: server.Client()}, "acme", "app", 7, sha)
	if report.Availability != noxReportUnavailable || report.Reason != "GitHub has no pull-request workflow run for this revision." || artifactRequests != 0 {
		t.Fatalf("another pull request's run was attributed to this PR: %+v, artifact requests=%d", report, artifactRequests)
	}
}

func TestLoadNoxReportFallsBackToOlderRetainedArtifactWhenNewestExpired(t *testing.T) {
	const sha = "cccccccccccccccccccccccccccccccccccccccc"
	archive := makeNoxArchive(t, `{"version":"2.1.0","runs":[{"tool":{"driver":{"name":"Nox"}},"results":[]}]}`)
	digest := sha256.Sum256(archive)
	var downloadedArtifactID string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/acme/app/pulls/7":
			_ = json.NewEncoder(w).Encode(map[string]any{"number": 7, "head": map[string]string{"sha": sha}, "base": map[string]any{"repo": map[string]any{"name": "app", "owner": map[string]string{"login": "acme"}}}})
		case "/repos/acme/app/actions/workflows/nox.yml/runs":
			_ = json.NewEncoder(w).Encode(map[string]any{"workflow_runs": []any{
				map[string]any{"id": 22, "path": domain.NoxSecurityWorkflowPath, "event": "pull_request", "head_sha": sha, "created_at": "2026-11-15T14:00:00Z", "pull_requests": []any{map[string]any{"number": 7}}, "html_url": "https://github.com/acme/app/actions/runs/test"},
				map[string]any{"id": 21, "path": domain.NoxSecurityWorkflowPath, "event": "pull_request", "head_sha": sha, "created_at": "2026-11-15T13:00:00Z", "pull_requests": []any{map[string]any{"number": 7}}, "html_url": "https://github.com/acme/app/actions/runs/test"},
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

	report := loadNoxReport(context.Background(), &githubClient{baseURL: srv.URL, http: srv.Client()}, "acme", "app", 7, sha)
	if report.Availability != noxReportAvailable || report.RunID != 21 || report.ArtifactID != 121 || downloadedArtifactID != "121" {
		t.Fatalf("newest expired report hid the older retained report: %+v, downloaded artifact=%q", report, downloadedArtifactID)
	}
}

func TestLoadNoxReportFallsBackToOlderRetainedArtifactWhenNewestInvalid(t *testing.T) {
	const sha = "cccccccccccccccccccccccccccccccccccccccc"
	archive := makeNoxArchive(t, `{"version":"2.1.0","runs":[{"tool":{"driver":{"name":"Nox"}},"results":[]}]}`)
	digest := sha256.Sum256(archive)
	var downloaded []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/acme/app/pulls/7":
			_ = json.NewEncoder(w).Encode(map[string]any{"number": 7, "head": map[string]string{"sha": sha}, "base": map[string]any{"repo": map[string]any{"name": "app", "owner": map[string]string{"login": "acme"}}}})
		case "/repos/acme/app/actions/workflows/nox.yml/runs":
			_ = json.NewEncoder(w).Encode(map[string]any{"workflow_runs": []any{
				map[string]any{"id": 22, "path": domain.NoxSecurityWorkflowPath, "event": "pull_request", "head_sha": sha, "created_at": "2026-11-15T14:00:00Z", "pull_requests": []any{map[string]any{"number": 7}}, "html_url": "https://github.com/acme/app/actions/runs/test"},
				map[string]any{"id": 21, "path": domain.NoxSecurityWorkflowPath, "event": "pull_request", "head_sha": sha, "created_at": "2026-11-15T13:00:00Z", "pull_requests": []any{map[string]any{"number": 7}}, "html_url": "https://github.com/acme/app/actions/runs/test"},
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

	report := loadNoxReport(context.Background(), &githubClient{baseURL: srv.URL, http: srv.Client()}, "acme", "app", 7, sha)
	if report.Availability != noxReportAvailable || report.RunID != 21 || report.ArtifactID != 121 || len(downloaded) != 2 || downloaded[0] != "122" || downloaded[1] != "121" {
		t.Fatalf("newest invalid report hid the older valid report: %+v, downloads=%v", report, downloaded)
	}
}

func TestLoadNoxReportFallsBackWhenNewestArtifactDisappears(t *testing.T) {
	const sha = "cccccccccccccccccccccccccccccccccccccccc"
	archive := makeNoxArchive(t, `{"version":"2.1.0","runs":[{"tool":{"driver":{"name":"Nox"}},"results":[]}]}`)
	digest := sha256.Sum256(archive)
	var downloaded []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/acme/app/pulls/7":
			_ = json.NewEncoder(w).Encode(map[string]any{"number": 7, "head": map[string]string{"sha": sha}, "base": map[string]any{"repo": map[string]any{"name": "app", "owner": map[string]string{"login": "acme"}}}})
		case "/repos/acme/app/actions/workflows/nox.yml/runs":
			_ = json.NewEncoder(w).Encode(map[string]any{"workflow_runs": []any{
				map[string]any{"id": 22, "path": domain.NoxSecurityWorkflowPath, "event": "pull_request", "head_sha": sha, "created_at": "2026-11-15T14:00:00Z", "pull_requests": []any{map[string]any{"number": 7}}, "html_url": "https://github.com/acme/app/actions/runs/test"},
				map[string]any{"id": 21, "path": domain.NoxSecurityWorkflowPath, "event": "pull_request", "head_sha": sha, "created_at": "2026-11-15T13:00:00Z", "pull_requests": []any{map[string]any{"number": 7}}, "html_url": "https://github.com/acme/app/actions/runs/test"},
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

	report := loadNoxReport(context.Background(), &githubClient{baseURL: srv.URL, http: srv.Client()}, "acme", "app", 7, sha)
	if report.Availability != noxReportAvailable || report.RunID != 21 || report.ArtifactID != 121 || len(downloaded) != 2 || downloaded[0] != "122" || downloaded[1] != "121" {
		t.Fatalf("disappeared artifact prevented fallback to an older valid report: %+v, downloads=%v", report, downloaded)
	}
}

func TestLoadNoxReportDownloadsNewestValidReportBeforeInspectingOlderRuns(t *testing.T) {
	const sha = "dddddddddddddddddddddddddddddddddddddddd"
	archive := makeNoxArchive(t, `{"version":"2.1.0","runs":[{"tool":{"driver":{"name":"Nox"}},"results":[]}]}`)
	digest := sha256.Sum256(archive)
	artifactLists := make([]string, 0)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/acme/app/pulls/7":
			_ = json.NewEncoder(w).Encode(map[string]any{"number": 7, "head": map[string]string{"sha": sha}, "base": map[string]any{"repo": map[string]any{"name": "app", "owner": map[string]string{"login": "acme"}}}})
		case "/repos/acme/app/actions/workflows/nox.yml/runs":
			runs := make([]any, 31)
			for index := range runs {
				id := 100 - index
				runs[index] = map[string]any{"id": id, "path": domain.NoxSecurityWorkflowPath, "event": "pull_request", "head_sha": sha, "created_at": fmt.Sprintf("2026-11-15T%02d:00:00Z", 23-index), "pull_requests": []any{map[string]any{"number": 7}}, "html_url": "https://github.com/acme/app/actions/runs/test"}
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

	report := loadNoxReport(context.Background(), &githubClient{baseURL: srv.URL, http: srv.Client()}, "acme", "app", 7, sha)
	if report.Availability != noxReportAvailable || report.RunID != 100 || len(artifactLists) != 1 || artifactLists[0] != "/repos/acme/app/actions/runs/100/artifacts" {
		t.Fatalf("older runs were inspected before the newest valid report was downloaded: %+v, artifact queries=%v", report, artifactLists)
	}
}

func TestLoadNoxReportRejectsArtifactWithDifferentRevisionProvenance(t *testing.T) {
	const sha = "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/acme/app/pulls/7":
			_ = json.NewEncoder(w).Encode(map[string]any{"number": 7, "head": map[string]string{"sha": sha}, "base": map[string]any{"repo": map[string]any{"name": "app", "owner": map[string]string{"login": "acme"}}}})
		case "/repos/acme/app/actions/workflows/nox.yml/runs":
			_ = json.NewEncoder(w).Encode(map[string]any{"workflow_runs": []any{map[string]any{"id": 20, "path": domain.NoxSecurityWorkflowPath, "event": "pull_request", "head_sha": sha, "created_at": "2026-11-15T13:00:00Z", "pull_requests": []any{map[string]any{"number": 7}}, "html_url": "https://github.com/acme/app/actions/runs/test"}}})
		case "/repos/acme/app/actions/runs/20/artifacts":
			_ = json.NewEncoder(w).Encode(map[string]any{"artifacts": []any{map[string]any{"id": 45, "name": noxArtifactName, "workflow_run": map[string]any{"id": 20, "head_sha": "ffffffffffffffffffffffffffffffffffffffff"}}}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	report := loadNoxReport(context.Background(), &githubClient{baseURL: srv.URL, http: srv.Client()}, "acme", "app", 7, sha)
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
		_ = json.NewEncoder(w).Encode(map[string]any{"number": 7, "head": map[string]string{"sha": sha}, "base": map[string]any{"repo": map[string]any{"name": "app", "private": true, "owner": map[string]string{"login": "acme"}}}})
	}))
	defer srv.Close()
	report := loadNoxReport(context.Background(), &githubClient{baseURL: srv.URL, http: srv.Client()}, "acme", "app", 7, sha)
	if report.Availability != noxReportUnavailable || report.Reason != "Security reports are only available for public pull requests." || actionsRequests != 0 {
		t.Fatalf("private report was not rejected before Actions access: %+v, actions requests=%d", report, actionsRequests)
	}
}

func TestLoadNoxReportRequiresTheRepositorySecurityWorkflow(t *testing.T) {
	const sha = "cccccccccccccccccccccccccccccccccccccccc"
	archive := makeNoxArchive(t, `{"version":"2.1.0","runs":[{"tool":{"driver":{"name":"Nox"}},"results":[]}]}`)
	digest := sha256.Sum256(archive)
	var artifactRequests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/acme/app/pulls/7":
			_ = json.NewEncoder(w).Encode(map[string]any{"number": 7, "head": map[string]string{"sha": sha}, "base": map[string]any{"repo": map[string]any{"name": "app", "private": false, "owner": map[string]string{"login": "acme"}}}})
		case "/repos/acme/app/actions/workflows/nox.yml/runs":
			_ = json.NewEncoder(w).Encode(map[string]any{"workflow_runs": []any{
				map[string]any{"id": 12, "path": "attacker.yml", "event": "pull_request", "head_sha": sha, "html_url": "https://github.com/acme/app/actions/runs/12", "created_at": "2026-11-15T13:59:00Z", "pull_requests": []any{map[string]any{"number": 7}}},
				map[string]any{"id": 11, "path": ".github/workflows/nox.yml", "event": "pull_request", "head_sha": sha, "html_url": "https://github.com/acme/app/actions/runs/11", "created_at": "2026-11-15T13:58:00Z", "pull_requests": []any{map[string]any{"number": 7}}},
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
	report := loadNoxReport(context.Background(), &githubClient{baseURL: srv.URL, http: srv.Client()}, "acme", "app", 7, sha)
	if report.Availability != noxReportAvailable || report.RunID != 11 || artifactRequests != 0 {
		t.Fatalf("report did not come from the repository security workflow: %+v, rejected-workflow artifact requests=%d", report, artifactRequests)
	}
}

func TestLoadNoxReportRequiresArtifactDigest(t *testing.T) {
	const sha = "cccccccccccccccccccccccccccccccccccccccc"
	archive := makeNoxArchive(t, `{"version":"2.1.0","runs":[{"tool":{"driver":{"name":"Nox"}},"results":[]}]}`)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/acme/app/pulls/7":
			_ = json.NewEncoder(w).Encode(map[string]any{"number": 7, "head": map[string]string{"sha": sha}, "base": map[string]any{"repo": map[string]any{"name": "app", "owner": map[string]string{"login": "acme"}}}})
		case "/repos/acme/app/actions/workflows/nox.yml/runs":
			_ = json.NewEncoder(w).Encode(map[string]any{"workflow_runs": []any{map[string]any{"id": 11, "path": ".github/workflows/nox.yml", "event": "pull_request", "head_sha": sha, "created_at": "2026-11-15T13:58:00Z", "pull_requests": []any{map[string]any{"number": 7}}, "html_url": "https://github.com/acme/app/actions/runs/test"}}})
		case "/repos/acme/app/actions/runs/11/artifacts":
			_ = json.NewEncoder(w).Encode(map[string]any{"artifacts": []any{map[string]any{"id": 44, "name": noxArtifactName, "workflow_run": map[string]any{"id": 11, "head_sha": sha}}}})
		case "/repos/acme/app/actions/artifacts/44/zip":
			_, _ = w.Write(archive)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	report := loadNoxReport(context.Background(), &githubClient{baseURL: srv.URL, http: srv.Client()}, "acme", "app", 7, sha)
	if report.Availability != noxReportUnavailable || report.Reason != "GitHub did not provide a verifiable SHA-256 digest for the Nox artifact." {
		t.Fatalf("artifact without digest should fail closed: %+v", report)
	}
}

func TestLoadNoxReportFindsSecurityWorkflowBeyondFirstPage(t *testing.T) {
	const sha = "cccccccccccccccccccccccccccccccccccccccc"
	archive := makeNoxArchive(t, `{"version":"2.1.0","runs":[{"tool":{"driver":{"name":"Nox"}},"results":[]}]}`)
	digest := sha256.Sum256(archive)
	var requestedPageTwo bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/acme/app/pulls/7":
			_ = json.NewEncoder(w).Encode(map[string]any{"number": 7, "head": map[string]string{"sha": sha}, "base": map[string]any{"repo": map[string]any{"name": "app", "owner": map[string]string{"login": "acme"}}}})
		case r.URL.Path == "/repos/acme/app/actions/workflows/nox.yml/runs" && r.URL.Query().Get("page") == "1":
			runs := make([]map[string]any, noxPageSize)
			for index := range runs {
				runs[index] = map[string]any{"id": index + 100, "path": "other.yml", "event": "pull_request", "head_sha": sha, "created_at": "2026-11-15T13:58:00Z", "pull_requests": []any{map[string]any{"number": 7}}, "html_url": "https://github.com/acme/app/actions/runs/test"}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"workflow_runs": runs})
		case r.URL.Path == "/repos/acme/app/actions/workflows/nox.yml/runs" && r.URL.Query().Get("page") == "2":
			requestedPageTwo = true
			_ = json.NewEncoder(w).Encode(map[string]any{"workflow_runs": []any{map[string]any{"id": 11, "path": domain.NoxSecurityWorkflowPath, "event": "pull_request", "head_sha": sha, "created_at": "2026-11-15T13:57:00Z", "pull_requests": []any{map[string]any{"number": 7}}, "html_url": "https://github.com/acme/app/actions/runs/test"}}})
		case r.URL.Path == "/repos/acme/app/actions/runs/11/artifacts":
			_ = json.NewEncoder(w).Encode(map[string]any{"artifacts": []any{map[string]any{"id": 44, "name": noxArtifactName, "digest": "sha256:" + hex.EncodeToString(digest[:]), "workflow_run": map[string]any{"id": 11, "head_sha": sha}}}})
		case r.URL.Path == "/repos/acme/app/actions/artifacts/44/zip":
			_, _ = w.Write(archive)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	report := loadNoxReport(context.Background(), &githubClient{baseURL: srv.URL, http: srv.Client()}, "acme", "app", 7, sha)
	if !requestedPageTwo || report.Availability != noxReportAvailable || report.RunID != 11 {
		t.Fatalf("later workflow page was not inspected: %+v, page two=%v", report, requestedPageTwo)
	}
}

func TestLoadNoxReportDoesNotCallCappedArtifactHistoryMissing(t *testing.T) {
	const sha = "cccccccccccccccccccccccccccccccccccccccc"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/acme/app/pulls/7":
			_ = json.NewEncoder(w).Encode(map[string]any{"number": 7, "head": map[string]string{"sha": sha}, "base": map[string]any{"repo": map[string]any{"name": "app", "owner": map[string]string{"login": "acme"}}}})
		case r.URL.Path == "/repos/acme/app/actions/workflows/nox.yml/runs":
			_ = json.NewEncoder(w).Encode(map[string]any{"workflow_runs": []any{map[string]any{"id": 11, "path": domain.NoxSecurityWorkflowPath, "event": "pull_request", "head_sha": sha, "created_at": "2026-11-15T13:58:00Z", "pull_requests": []any{map[string]any{"number": 7}}, "html_url": "https://github.com/acme/app/actions/runs/test"}}})
		case r.URL.Path == "/repos/acme/app/actions/runs/11/artifacts":
			artifacts := make([]map[string]any, noxPageSize)
			for index := range artifacts {
				artifacts[index] = map[string]any{"id": index + 100, "name": noxArtifactName, "workflow_run": map[string]any{"id": 11, "head_sha": "ffffffffffffffffffffffffffffffffffffffff"}}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"artifacts": artifacts})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := &githubClient{baseURL: srv.URL, http: srv.Client()}
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
			_ = json.NewEncoder(w).Encode(map[string]any{"number": 7, "head": map[string]string{"sha": sha}, "base": map[string]any{"repo": map[string]any{"name": "app", "owner": map[string]string{"login": "acme"}}}})
		case "/repos/acme/app/actions/workflows/nox.yml/runs":
			runs := make([]map[string]any, noxPageSize)
			for index := range runs {
				path := "other.yml"
				if r.URL.Query().Get("page") == "1" && index == 0 {
					path = domain.NoxSecurityWorkflowPath
				}
				runs[index] = map[string]any{"id": index + 100, "path": path, "event": "pull_request", "head_sha": sha, "created_at": "2026-11-15T13:58:00Z", "pull_requests": []any{map[string]any{"number": 7}}, "html_url": "https://github.com/acme/app/actions/runs/test"}
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

	report := loadNoxReport(context.Background(), &githubClient{baseURL: server.URL, http: server.Client()}, "acme", "app", 7, sha)
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
			_ = json.NewEncoder(w).Encode(map[string]any{"number": 7, "head": map[string]string{"sha": sha}, "base": map[string]any{"repo": map[string]any{"name": "app", "owner": map[string]string{"login": "acme"}}}})
		case r.URL.Path == "/repos/acme/app/actions/workflows/nox.yml/runs":
			runs := make([]map[string]any, noxPageSize)
			for index := range runs {
				runs[index] = map[string]any{"id": index + 100, "path": domain.NoxSecurityWorkflowPath, "event": "pull_request", "head_sha": sha, "created_at": "2026-11-15T13:58:00Z", "pull_requests": []any{map[string]any{"number": 7}}, "html_url": "https://github.com/acme/app/actions/runs/test"}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"workflow_runs": runs})
		case strings.Contains(r.URL.Path, "/actions/runs/") && strings.HasSuffix(r.URL.Path, "/artifacts"):
			_ = json.NewEncoder(w).Encode(map[string]any{"artifacts": []any{}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := &githubClient{baseURL: srv.URL, http: srv.Client()}
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
			_ = json.NewEncoder(w).Encode(map[string]any{"number": 7, "head": map[string]string{"sha": currentSHA}, "base": map[string]any{"repo": map[string]any{"name": "app", "owner": map[string]string{"login": "acme"}}}})
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

	client := &githubClient{baseURL: srv.URL, http: srv.Client()}
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
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	file, err := writer.Create(noxSARIFFilename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fmt.Fprint(file, sarif); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}
