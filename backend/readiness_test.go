package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

const readinessHead = "cccccccccccccccccccccccccccccccccccccccc"

// readinessGitHub serves one public pull request for the readiness endpoint. Each
// field overrides one GitHub response; the zero value is a PR that is ready to merge.
type readinessGitHub struct {
	headAfterEvaluation  string
	stateAfterEvaluation string
	baseRef              string
	behindBy             int
	protection           string
	checkRuns            string
	statuses             string
	qodoComments         string
	commentsCode         int
	prReads              atomic.Int32
}

func cleanQodoReview(sha string) string {
	return `<h3>Code Review by Qodo</h3><p>Nice, no open findings for this group</p><p>Nice, no open findings for this group</p><p>Nice, no open findings for this group</p><!-- https://github.com/acme/app/commit/` + sha + ` -->`
}

func qodoCommentsJSON(t *testing.T, bodies ...string) string {
	t.Helper()
	type comment struct {
		ID        int64             `json:"id"`
		User      map[string]string `json:"user"`
		CreatedAt string            `json:"created_at"`
		UpdatedAt string            `json:"updated_at"`
		HTMLURL   string            `json:"html_url"`
		Body      string            `json:"body"`
	}
	comments := make([]comment, 0, len(bodies))
	for i, body := range bodies {
		comments = append(comments, comment{ID: int64(100 + i), User: map[string]string{"login": "qodo-code-review[bot]"}, CreatedAt: "2026-11-21T09:00:00Z", UpdatedAt: "2026-11-21T09:00:00Z", HTMLURL: "https://github.com/acme/app/pull/7#issuecomment-" + string(rune('a'+i)), Body: body})
	}
	out, err := json.Marshal(comments)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func (f *readinessGitHub) serve(t *testing.T) *httptest.Server {
	t.Helper()
	or := func(value, fallback string) string {
		if value == "" {
			return fallback
		}
		return value
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/acme/app", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"name":"app","private":false,"default_branch":"main","owner":{"login":"acme"}}`))
	})
	mux.HandleFunc("/repos/acme/app/pulls/7", func(w http.ResponseWriter, r *http.Request) {
		head := readinessHead
		if f.prReads.Add(1) > 1 && f.headAfterEvaluation != "" {
			head = f.headAfterEvaluation
		}
		state := "open"
		if f.prReads.Load() > 1 && f.stateAfterEvaluation != "" {
			state = f.stateAfterEvaluation
		}
		_, _ = w.Write([]byte(`{"number":7,"title":"Ready?","state":"` + state + `","draft":false,"html_url":"https://github.com/acme/app/pull/7","head":{"sha":"` + head + `"},"base":{"ref":"` + or(f.baseRef, "main") + `","repo":{"name":"app","owner":{"login":"acme"}}}}`))
	})
	mux.HandleFunc("/repos/acme/app/compare/main..."+readinessHead, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"behind_by": f.behindBy, "html_url": "https://github.com/acme/app/compare/main..." + readinessHead})
	})
	mux.HandleFunc("/repos/acme/app/branches/main", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(or(f.protection, `{"name":"main","protected":true,"protection":{"required_status_checks":{"contexts":["Go checks","warden/gate"],"checks":[{"context":"Go checks","app_id":15368},{"context":"warden/gate","app_id":null}]}}}`)))
	})
	mux.HandleFunc("/repos/acme/app/commits/"+readinessHead+"/check-runs", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("filter") != "all" {
			t.Errorf("readiness must read every run on the head, got %q", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(or(f.checkRuns, `{"total_count":1,"check_runs":[{"id":10,"name":"Go checks","head_sha":"`+readinessHead+`","status":"completed","conclusion":"success","html_url":"https://github.com/acme/app/runs/10","app":{"id":15368,"name":"GitHub Actions"}}]}`)))
	})
	mux.HandleFunc("/repos/acme/app/commits/"+readinessHead+"/status", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(or(f.statuses, `{"sha":"`+readinessHead+`","total_count":1,"statuses":[{"id":1,"context":"warden/gate","state":"success","updated_at":"2026-11-21T10:00:00Z"}]}`)))
	})
	mux.HandleFunc("/repos/acme/app/issues/7/comments", func(w http.ResponseWriter, r *http.Request) {
		if f.commentsCode != 0 {
			w.WriteHeader(f.commentsCode)
			return
		}
		_, _ = w.Write([]byte(or(f.qodoComments, qodoCommentsJSON(t, cleanQodoReview(readinessHead)))))
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

type readinessBody struct {
	Repository string `json:"repository"`
	Number     int    `json:"number"`
	HeadSHA    string `json:"headSha"`
	HeadURL    string `json:"headUrl"`
	Verdict    string `json:"verdict"`
	Reasons    []struct {
		Subject   string `json:"subject"`
		Outcome   string `json:"outcome"`
		Message   string `json:"message"`
		SourceURL string `json:"sourceUrl"`
	} `json:"reasons"`
	Findings *struct {
		High   int `json:"high"`
		Medium int `json:"medium"`
		Low    int `json:"low"`
	} `json:"findings"`
	Error string `json:"error"`
}

func postReadiness(t *testing.T, fake *readinessGitHub) (int, readinessBody) {
	t.Helper()
	server := fake.serve(t)
	router := newRouter(&githubClient{baseURL: server.URL, http: server.Client()})
	request := httptest.NewRequest(http.MethodPost, "/api/readiness", strings.NewReader(`{"url":"https://github.com/acme/app/pull/7"}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	var body readinessBody
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode %q: %v", recorder.Body.String(), err)
	}
	return recorder.Code, body
}

func reasonFor(body readinessBody, subject string) (string, string, bool) {
	for _, reason := range body.Reasons {
		if reason.Subject == subject {
			return reason.Outcome, reason.Message, true
		}
	}
	return "", "", false
}

func TestReadinessPassesWhenEveryRequirementCoversTheHead(t *testing.T) {
	code, body := postReadiness(t, &readinessGitHub{})
	if code != http.StatusOK || body.Verdict != "pass" {
		t.Fatalf("got %d %q, want 200 pass; body %+v", code, body.Verdict, body)
	}
	if body.Repository != "acme/app" || body.Number != 7 || body.HeadSHA != readinessHead || body.HeadURL != "https://github.com/acme/app/commit/"+readinessHead {
		t.Fatalf("identity = %+v", body)
	}
	for _, subject := range []string{"branch", "qodo", "check:Go checks", "check:warden/gate"} {
		if outcome, _, ok := reasonFor(body, subject); !ok || outcome != "pass" {
			t.Fatalf("reason %q = %q (present %v)", subject, outcome, ok)
		}
	}
	if body.Findings == nil || body.Findings.High+body.Findings.Medium+body.Findings.Low != 0 {
		t.Fatalf("findings = %+v, want zero counts", body.Findings)
	}
}

func TestReadinessFailsOnAMediumQodoFindingGroup(t *testing.T) {
	review := `<h3>Code Review by Qodo</h3><details><summary><img src="https://img.shields.io/badge/Medium-634FD1" alt="Remediation recommended"> View medium (1)</summary><details><summary>1. Stale checks <code>🐞 Bug</code></summary></details></details><p>Nice, no open findings for this group</p><!-- https://github.com/acme/app/commit/` + readinessHead + ` -->`
	code, body := postReadiness(t, &readinessGitHub{qodoComments: qodoCommentsJSON(t, review)})
	if code != http.StatusOK || body.Verdict != "fail" || body.Findings == nil || body.Findings.Medium != 1 {
		t.Fatalf("got %d %q findings %+v, want fail with medium=1", code, body.Verdict, body.Findings)
	}
}

func TestReadinessReportsLowFindingsWithoutBlocking(t *testing.T) {
	review := `<h3>Code Review by Qodo</h3><p>Nice, no open findings for this group</p><p>Nice, no open findings for this group</p><details><summary>View low (2)</summary></details><!-- https://github.com/acme/app/commit/` + readinessHead + ` -->`
	_, body := postReadiness(t, &readinessGitHub{qodoComments: qodoCommentsJSON(t, review)})
	if body.Verdict != "pass" || body.Findings == nil || body.Findings.Low != 2 {
		t.Fatalf("got %q findings %+v, want pass with low=2", body.Verdict, body.Findings)
	}
}

func TestReadinessTreatsAnUnknownFindingGroupAsUnreadable(t *testing.T) {
	review := `<h3>Code Review by Qodo</h3><details><summary>View critical (1)</summary></details><p>Nice, no open findings for this group</p><!-- https://github.com/acme/app/commit/` + readinessHead + ` -->`
	_, body := postReadiness(t, &readinessGitHub{qodoComments: qodoCommentsJSON(t, review)})
	if outcome, _, _ := reasonFor(body, "qodo"); body.Verdict != "incomplete" || outcome != "incomplete" {
		t.Fatalf("got %q / qodo %q, want incomplete", body.Verdict, outcome)
	}
}

func TestReadinessTreatsTallyFindingsWithoutSeverityAsUnreadable(t *testing.T) {
	review := `<h3>Code Review by Qodo</h3><code>🐞 Bugs (0)</code><code>📘 Rule violations (1)</code><h3>Great, no issues found!</h3><!-- https://github.com/acme/app/commit/` + readinessHead + ` -->`
	_, body := postReadiness(t, &readinessGitHub{qodoComments: qodoCommentsJSON(t, review)})
	if body.Verdict != "incomplete" {
		t.Fatalf("verdict = %q, want incomplete", body.Verdict)
	}
}

func TestReadinessUsesTheMostRecentlyUpdatedQodoReview(t *testing.T) {
	older := cleanQodoReview(olderSHA)
	comments := qodoCommentsJSON(t, cleanQodoReview(readinessHead), older)
	// The comment naming the head was edited last, so it is the current review.
	comments = strings.Replace(comments, `"updated_at":"2026-11-21T09:00:00Z","html_url":"https://github.com/acme/app/pull/7#issuecomment-a"`, `"updated_at":"2026-11-21T11:00:00Z","html_url":"https://github.com/acme/app/pull/7#issuecomment-a"`, 1)
	_, body := postReadiness(t, &readinessGitHub{qodoComments: comments})
	if outcome, message, _ := reasonFor(body, "qodo"); outcome != "pass" {
		t.Fatalf("qodo = %q (%s), want pass from the most recently updated review", outcome, message)
	}
}

func TestReadinessFailsOnAnEarlierFailedRunOfTheHead(t *testing.T) {
	runs := `{"total_count":2,"check_runs":[` +
		`{"id":10,"name":"Go checks","head_sha":"` + readinessHead + `","status":"completed","conclusion":"success","app":{"id":15368}},` +
		`{"id":5,"name":"Go checks","head_sha":"` + readinessHead + `","status":"completed","conclusion":"failure","app":{"id":15368}}]}`
	_, body := postReadiness(t, &readinessGitHub{checkRuns: runs})
	if outcome, message, _ := reasonFor(body, "check:Go checks"); body.Verdict != "fail" || outcome != "fail" {
		t.Fatalf("got %q / %q (%s), want fail", body.Verdict, outcome, message)
	}
}

func TestReadinessIgnoresASameNamedRunFromAnotherApp(t *testing.T) {
	runs := `{"total_count":1,"check_runs":[{"id":10,"name":"Go checks","head_sha":"` + readinessHead + `","status":"completed","conclusion":"success","app":{"id":999}}]}`
	_, body := postReadiness(t, &readinessGitHub{checkRuns: runs})
	if _, message, _ := reasonFor(body, "check:Go checks"); body.Verdict != "incomplete" || !strings.Contains(message, "missing on the head") {
		t.Fatalf("got %q (%s), want incomplete: missing", body.Verdict, message)
	}
}

func TestReadinessRejectsCheckRunsForAnotherCommit(t *testing.T) {
	runs := `{"total_count":1,"check_runs":[{"id":10,"name":"Go checks","head_sha":"` + olderSHA + `","status":"completed","conclusion":"success","app":{"id":15368}}]}`
	_, body := postReadiness(t, &readinessGitHub{checkRuns: runs})
	if outcome, _, _ := reasonFor(body, "required-checks"); body.Verdict != "incomplete" || outcome != "incomplete" {
		t.Fatalf("got %q / required-checks %q, want incomplete", body.Verdict, outcome)
	}
}

func TestReadinessIsIncompleteWhenBehindTheDefaultBranch(t *testing.T) {
	_, body := postReadiness(t, &readinessGitHub{behindBy: 4})
	if _, message, _ := reasonFor(body, "branch"); body.Verdict != "incomplete" || !strings.Contains(message, "behind main by 4") {
		t.Fatalf("got %q (%s)", body.Verdict, message)
	}
}

func TestReadinessIsIncompleteForAnUnprotectedDefaultBranch(t *testing.T) {
	_, body := postReadiness(t, &readinessGitHub{protection: `{"name":"main","protected":false}`})
	if _, message, _ := reasonFor(body, "required-checks"); body.Verdict != "incomplete" || !strings.Contains(message, "requires no status checks") {
		t.Fatalf("got %q (%s)", body.Verdict, message)
	}
}

func TestReadinessReportsUnavailableQodoComments(t *testing.T) {
	_, body := postReadiness(t, &readinessGitHub{commentsCode: http.StatusBadGateway})
	if _, message, _ := reasonFor(body, "qodo"); body.Verdict != "incomplete" || !strings.Contains(message, "could not be loaded") {
		t.Fatalf("got %q (%s)", body.Verdict, message)
	}
}

func TestReadinessRefusesAVerdictWhenTheHeadMoves(t *testing.T) {
	code, body := postReadiness(t, &readinessGitHub{headAfterEvaluation: olderSHA})
	if code != http.StatusConflict || body.Verdict != "" {
		t.Fatalf("got %d verdict %q, want 409 with no verdict", code, body.Verdict)
	}
}

func TestReadinessRejectsAnInvalidURL(t *testing.T) {
	router := newRouter(&githubClient{baseURL: "http://127.0.0.1:1", http: http.DefaultClient})
	request := httptest.NewRequest(http.MethodPost, "/api/readiness", strings.NewReader(`{"url":"https://example.com/acme/app/pull/7"}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("code = %d, want 400", recorder.Code)
	}
}

func TestReadinessRefusesAVerdictWhenThePullRequestClosesDuringEvaluation(t *testing.T) {
	code, body := postReadiness(t, &readinessGitHub{stateAfterEvaluation: "closed"})
	if code != http.StatusConflict || body.Verdict != "" {
		t.Fatalf("got %d verdict %q, want 409 with no verdict", code, body.Verdict)
	}
}
