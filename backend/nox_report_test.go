package main

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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

func TestParseNoxSARIFRejectsOtherScannerAndUnsafeLocations(t *testing.T) {
	const sha = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	for name, data := range map[string][]byte{
		"non-Nox scanner": []byte(`{"version":"2.1.0","runs":[{"tool":{"driver":{"name":"CodeQL"}},"results":[]}]}`),
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

func TestParseNoxSARIFDoesNotCountSuppressedResultsAsFindings(t *testing.T) {
	const sha = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	data := []byte(`{"version":"2.1.0","runs":[{"tool":{"driver":{"name":"Nox"}},"results":[{"ruleId":"SEC-001","message":{"text":"Known baseline item."},"suppressions":[{"kind":"external","justification":"Accepted baseline"}]}]}]}`)
	report, err := parseNoxSARIF(data, sha, 1, "https://github.com/acme/app/actions/runs/1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if report.TotalCount != 0 || len(report.Findings) != 0 {
		t.Fatalf("suppressed finding counted as active: %+v", report)
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
				map[string]any{"id": 12, "path": noxWorkflowPath, "name": "Security", "event": "pull_request", "head_sha": "dddddddddddddddddddddddddddddddddddddddd", "html_url": "https://github.com/acme/app/actions/runs/12", "created_at": "2026-11-15T13:59:00Z"},
				map[string]any{"id": 11, "path": noxWorkflowPath + "@refs/pull/7/merge", "name": "Security", "event": "pull_request", "head_sha": sha, "conclusion": "success", "html_url": "https://github.com/acme/app/actions/runs/11", "created_at": "2026-11-15T13:58:00Z"},
			}})
		case "/repos/acme/app/actions/runs/11/artifacts":
			_ = json.NewEncoder(w).Encode(map[string]any{"artifacts": []any{map[string]any{
				"id": 44, "name": noxArtifactName, "size_in_bytes": len(archive), "digest": "sha256:" + hex.EncodeToString(digest[:]),
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
	if report.RunURL != "https://github.com/acme/app/actions/runs/11" || report.ExpiresAt == "" || artifactRequests != 1 {
		t.Fatalf("missing source provenance or artifact was not fetched once: %+v, downloads=%d", report, artifactRequests)
	}
}

func TestLoadNoxReportRejectsArtifactWithDifferentRevisionProvenance(t *testing.T) {
	const sha = "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/acme/app/pulls/7":
			_ = json.NewEncoder(w).Encode(map[string]any{"number": 7, "head": map[string]string{"sha": sha}, "base": map[string]any{"repo": map[string]any{"name": "app", "owner": map[string]string{"login": "acme"}}}})
		case "/repos/acme/app/actions/workflows/nox.yml/runs":
			_ = json.NewEncoder(w).Encode(map[string]any{"workflow_runs": []any{map[string]any{"id": 20, "path": noxWorkflowPath, "event": "pull_request", "head_sha": sha, "created_at": "2026-11-15T13:00:00Z"}}})
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
				map[string]any{"id": 12, "path": "attacker.yml", "event": "pull_request", "head_sha": sha, "html_url": "https://github.com/acme/app/actions/runs/12", "created_at": "2026-11-15T13:59:00Z"},
				map[string]any{"id": 11, "path": ".github/workflows/nox.yml", "event": "pull_request", "head_sha": sha, "html_url": "https://github.com/acme/app/actions/runs/11", "created_at": "2026-11-15T13:58:00Z"},
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
			_ = json.NewEncoder(w).Encode(map[string]any{"workflow_runs": []any{map[string]any{"id": 11, "path": ".github/workflows/nox.yml", "event": "pull_request", "head_sha": sha, "created_at": "2026-11-15T13:58:00Z"}}})
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
				runs[index] = map[string]any{"id": index + 100, "path": "other.yml", "event": "pull_request", "head_sha": sha, "created_at": "2026-11-15T13:58:00Z"}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"workflow_runs": runs})
		case r.URL.Path == "/repos/acme/app/actions/workflows/nox.yml/runs" && r.URL.Query().Get("page") == "2":
			requestedPageTwo = true
			_ = json.NewEncoder(w).Encode(map[string]any{"workflow_runs": []any{map[string]any{"id": 11, "path": noxWorkflowPath, "event": "pull_request", "head_sha": sha, "created_at": "2026-11-15T13:57:00Z"}}})
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
