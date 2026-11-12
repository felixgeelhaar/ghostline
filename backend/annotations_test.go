package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestLoadCheckAnnotationsReturnsRevisionBoundGitHubEvidence(t *testing.T) {
	requests := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.RequestURI())
		switch r.URL.Path {
		case "/repos/acme/app":
			if r.Header.Get("Authorization") != "Bearer test-token" {
				t.Errorf("visibility request Authorization = %q", r.Header.Get("Authorization"))
			}
			_, _ = w.Write([]byte(`{"private":false}`))
		case "/repos/acme/app/check-runs/314":
			if got := r.Header.Get("Authorization"); got != "" {
				t.Errorf("check-run request unexpectedly used server token: %q", got)
			}
			_, _ = w.Write([]byte(`{"id":314,"head_sha":"` + headSHA + `","html_url":"https://github.com/acme/app/actions/runs/314"}`))
		case "/repos/acme/app/check-runs/314/annotations":
			if got := r.Header.Get("Authorization"); got != "" {
				t.Errorf("annotation request unexpectedly used server token: %q", got)
			}
			if r.URL.Query().Get("per_page") != "100" || r.URL.Query().Get("page") != "1" {
				t.Errorf("annotation query = %s, want page 1 at 100 per page", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`[{"path":"backend/payments.go","start_line":12,"end_line":14,"annotation_level":"warning","title":"Possible replay","message":"The idempotency key is not checked before charging.","raw_details":"inspect the transaction boundary"}]`))
		default:
			t.Errorf("unexpected GitHub request: %s", r.URL.RequestURI())
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	gin.SetMode(gin.TestMode)
	router := newRouter(&githubClient{baseURL: server.URL, token: "test-token", http: server.Client()})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/check-annotations", strings.NewReader(`{"url":"https://github.com/acme/app/pull/7","sha":"`+headSHA+`","checkRunId":314,"page":1}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var got checkAnnotationPage
	if err := json.Unmarshal(recorder.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode annotation response: %v", err)
	}
	if got.RevisionSHA != headSHA || got.CheckRunID != 314 || got.Page != 1 || got.HasMore || len(got.Items) != 1 {
		t.Fatalf("annotation page = %+v", got)
	}
	annotation := got.Items[0]
	if annotation.Path != "backend/payments.go" || annotation.StartLine != 12 || annotation.EndLine != 14 || annotation.Level != "warning" || annotation.Title != "Possible replay" || annotation.Message == "" || annotation.Details != "inspect the transaction boundary" {
		t.Fatalf("annotation = %+v", annotation)
	}
	if annotation.SourceURL != "https://github.com/acme/app/blob/"+headSHA+"/backend/payments.go#L12-L14" {
		t.Fatalf("source URL = %q", annotation.SourceURL)
	}
	if !strings.Contains(got.CheckRunURL, "/actions/runs/314") {
		t.Fatalf("check run URL = %q", got.CheckRunURL)
	}
	if len(requests) != 3 || requests[0] != "GET /repos/acme/app" || requests[1] != "GET /repos/acme/app/check-runs/314" || !strings.HasPrefix(requests[2], "GET /repos/acme/app/check-runs/314/annotations?") {
		t.Fatalf("GitHub requests = %v", requests)
	}
}

func TestLoadCheckAnnotationsRejectsPrivateRepositoryBeforeFetchingRun(t *testing.T) {
	requests := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.Path)
		if r.URL.Path != "/repos/acme/app" {
			t.Errorf("private repository request escaped visibility check: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"private":true}`))
	}))
	defer server.Close()

	gin.SetMode(gin.TestMode)
	router := newRouter(&githubClient{baseURL: server.URL, token: "test-token", http: server.Client()})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/check-annotations", strings.NewReader(`{"url":"https://github.com/acme/app/pull/7","sha":"`+headSHA+`","checkRunId":314,"page":1}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNotFound || len(requests) != 1 {
		t.Fatalf("private repository status=%d requests=%v body=%s", recorder.Code, requests, recorder.Body.String())
	}
}

func TestLoadCheckAnnotationsUsesGitHubPaginationLink(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/acme/app":
			_, _ = w.Write([]byte(`{"private":false}`))
		case "/repos/acme/app/check-runs/314":
			_, _ = w.Write([]byte(`{"id":314,"head_sha":"` + headSHA + `"}`))
		case "/repos/acme/app/check-runs/314/annotations":
			if r.URL.Query().Get("page") == "1" {
				w.Header().Set("Link", `<https://api.github.com/repos/acme/app/check-runs/314/annotations?per_page=100&page=2>; rel="next"`)
			}
			items := make([]githubCheckAnnotation, annotationPageSize)
			for i := range items {
				items[i] = githubCheckAnnotation{Path: "main.go", StartLine: i + 1, EndLine: i + 1, AnnotationLevel: "warning", Message: "finding"}
			}
			_ = json.NewEncoder(w).Encode(items)
		default:
			t.Errorf("unexpected GitHub request: %s", r.URL.RequestURI())
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	gh := &githubClient{baseURL: server.URL, http: server.Client()}
	first, err := loadCheckAnnotations(t.Context(), gh, "acme", "app", headSHA, 314, 1)
	if err != nil {
		t.Fatal(err)
	}
	second, err := loadCheckAnnotations(t.Context(), gh, "acme", "app", headSHA, 314, 21)
	if err != nil {
		t.Fatal(err)
	}
	if !first.HasMore || second.HasMore || len(second.Items) != annotationPageSize {
		t.Fatalf("page continuation flags: first=%v second=%v items=%d", first.HasMore, second.HasMore, len(second.Items))
	}
}

func TestHasNextLinkMatchesRelationToken(t *testing.T) {
	tests := []struct {
		name    string
		header  string
		expects bool
	}{
		{name: "quoted next", header: `<https://api.github.com/x?page=2>; rel="next"`, expects: true},
		{name: "unquoted next", header: `<https://api.github.com/x?page=2>; rel=next`, expects: true},
		{name: "next in relation list", header: `<https://api.github.com/x?page=2>; rel="prev next"`, expects: true},
		{name: "comma in quoted title", header: `<https://api.github.com/x?page=1>; title="one, two"; rel="prev", <https://api.github.com/x?page=2>; rel="next"`, expects: true},
		{name: "comma in URI", header: `<https://api.github.com/x?labels=a,b>; rel="next"`, expects: true},
		{name: "relation text in quoted semicolon value", header: `<https://api.github.com/x?page=2>; title="a; rel=next"; rel="prev"`, expects: false},
		{name: "not a next token", header: `<https://api.github.com/x?page=2>; rel="next-page"`, expects: false},
		{name: "no next link", header: `<https://api.github.com/x?page=1>; rel="prev"`, expects: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasNextLink([]string{tt.header}); got != tt.expects {
				t.Errorf("hasNextLink() = %v, want %v", got, tt.expects)
			}
		})
	}
}

func TestLoadCheckAnnotationsRejectsRunFromDifferentRevision(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/acme/app" {
			_, _ = w.Write([]byte(`{"private":false}`))
			return
		}
		if r.URL.Path != "/repos/acme/app/check-runs/314" {
			t.Errorf("unexpected GitHub request: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"id":314,"head_sha":"` + olderSHA + `","html_url":"https://github.com/acme/app/actions/runs/314"}`))
	}))
	defer server.Close()

	gin.SetMode(gin.TestMode)
	router := newRouter(&githubClient{baseURL: server.URL, http: server.Client()})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/check-annotations", strings.NewReader(`{"url":"https://github.com/acme/app/pull/7","sha":"`+headSHA+`","checkRunId":314,"page":1}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusConflict, recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), "items") {
		t.Fatalf("mismatched run returned annotation data: %s", recorder.Body.String())
	}
}

func TestLoadCheckAnnotationsRejectsInvalidRunIDAndPage(t *testing.T) {
	for _, body := range []string{
		`{"url":"https://github.com/acme/app/pull/7","sha":"` + headSHA + `","checkRunId":0,"page":1}`,
		`{"url":"https://github.com/acme/app/pull/7","sha":"` + headSHA + `","checkRunId":314,"page":0}`,
		`{"url":"https://github.com/acme/app/pull/7","sha":"bad","checkRunId":314,"page":1}`,
	} {
		gin.SetMode(gin.TestMode)
		router := newRouter(&githubClient{baseURL: "http://127.0.0.1:1", http: http.DefaultClient})
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/api/check-annotations", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusBadRequest {
			t.Errorf("body %s: status = %d, want %d", body, recorder.Code, http.StatusBadRequest)
		}
	}
}

func TestAnnotationSourceURLRejectsUnsafePaths(t *testing.T) {
	for _, name := range []string{"../secret", "a/../../secret", "/absolute", `a\\b`, ""} {
		if got := annotationSourceURL("acme", "app", headSHA, name, 1, 1); got != "" {
			t.Errorf("annotationSourceURL(%q) = %q, want empty", name, got)
		}
	}
}
