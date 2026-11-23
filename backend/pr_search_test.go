package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

const searchReply = `{"total_count":2,"incomplete_results":false,"items":[
{"number":7,"title":"Add widgets","state":"open","draft":true,"updated_at":"2026-11-20T10:00:00Z","html_url":"https://github.com/acme/widgets/pull/7","repository_url":"https://api.github.com/repos/acme/widgets","pull_request":{"merged_at":null}},
{"number":3,"title":"Fix gears","state":"closed","draft":false,"updated_at":"2026-11-19T09:00:00Z","html_url":"https://github.com/acme/gears/pull/3","repository_url":"https://api.github.com/repos/acme/gears","pull_request":{"merged_at":"2026-11-19T08:00:00Z"}}]}`

func searchRouter(t *testing.T, github *recordingGitHub) (http.Handler, *http.Cookie) {
	t.Helper()
	auth := newAuthManager(testAuthConfig(), oauthClientGranting(t, `{"access_token":"user-oauth-token","token_type":"bearer","scope":"read:user"}`))
	router := newRouterWithAuth(github.client(), nil, newRateLimiter(100, time.Minute, time.Now), clientIdentity{}, nil, auth)
	return router, signInThroughOAuth(t, router)
}

func searchWith(router http.Handler, session *http.Cookie, query string) *httptest.ResponseRecorder {
	body, _ := json.Marshal(map[string]string{"query": query})
	request := httptest.NewRequest(http.MethodPost, "/api/pr-search", strings.NewReader(string(body)))
	request.Header.Set("Content-Type", "application/json")
	if session != nil {
		request.AddCookie(session)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func TestSearchFindsTheSignedInUsersPublicPullRequests(t *testing.T) {
	var searched *url.URL
	github := &recordingGitHub{respond: func(request *http.Request) *http.Response {
		searched = request.URL
		return oauthResponse(http.StatusOK, searchReply)
	}}
	router, session := searchRouter(t, github)

	response := searchWith(router, session, "  widgets  ")
	if response.Code != http.StatusOK {
		t.Fatalf("search status = %d body=%s", response.Code, response.Body.String())
	}
	if searched == nil || searched.Path != "/search/issues" {
		t.Fatalf("searched %v, want /search/issues", searched)
	}
	if got := searched.Query().Get("q"); got != "is:pr is:public involves:@me widgets" {
		t.Fatalf("search query = %q", got)
	}
	if seen := github.seen(); seen[len(seen)-1] != "Bearer user-oauth-token" {
		t.Fatalf("search authorization = %q, want the user's token", seen)
	}
	if strings.Contains(response.Body.String(), "user-oauth-token") {
		t.Fatal("GitHub access token reached the browser")
	}
	var result struct {
		Partial bool `json:"partial"`
		Items   []struct {
			Repository string    `json:"repository"`
			Number     int       `json:"number"`
			Title      string    `json:"title"`
			State      string    `json:"state"`
			UpdatedAt  time.Time `json:"updatedAt"`
			URL        string    `json:"url"`
		} `json:"items"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Partial || len(result.Items) != 2 {
		t.Fatalf("result = %+v", result)
	}
	first, second := result.Items[0], result.Items[1]
	if first.Repository != "acme/widgets" || first.Number != 7 || first.State != "draft" || first.URL != "https://github.com/acme/widgets/pull/7" || first.Title != "Add widgets" {
		t.Fatalf("first item = %+v", first)
	}
	if !first.UpdatedAt.Equal(time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)) {
		t.Fatalf("first updatedAt = %v", first.UpdatedAt)
	}
	if second.Repository != "acme/gears" || second.State != "merged" {
		t.Fatalf("second item = %+v", second)
	}
}

func TestSearchStates(t *testing.T) {
	for name, tc := range map[string]struct {
		state, mergedAt string
		draft           bool
		want            string
	}{
		"open":   {state: "open", mergedAt: "null", want: "open"},
		"draft":  {state: "open", mergedAt: "null", draft: true, want: "draft"},
		"merged": {state: "closed", mergedAt: `"2026-11-19T08:00:00Z"`, want: "merged"},
		"closed": {state: "closed", mergedAt: "null", want: "closed"},
	} {
		t.Run(name, func(t *testing.T) {
			draft := "false"
			if tc.draft {
				draft = "true"
			}
			reply := `{"total_count":1,"incomplete_results":false,"items":[{"number":7,"title":"t","state":"` + tc.state + `","draft":` + draft + `,"updated_at":"2026-11-20T10:00:00Z","html_url":"https://github.com/acme/widgets/pull/7","repository_url":"https://api.github.com/repos/acme/widgets","pull_request":{"merged_at":` + tc.mergedAt + `}}]}`
			router, session := searchRouter(t, &recordingGitHub{respond: func(*http.Request) *http.Response { return oauthResponse(http.StatusOK, reply) }})
			response := searchWith(router, session, "widgets")
			if !strings.Contains(response.Body.String(), `"state":"`+tc.want+`"`) {
				t.Fatalf("body = %s, want state %s", response.Body.String(), tc.want)
			}
		})
	}
}

func TestSearchMarksAPartialResult(t *testing.T) {
	router, session := searchRouter(t, &recordingGitHub{respond: func(*http.Request) *http.Response {
		return oauthResponse(http.StatusOK, `{"total_count":0,"incomplete_results":true,"items":[]}`)
	}})
	response := searchWith(router, session, "widgets")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"partial":true`) {
		t.Fatalf("search = %d %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"items":[]`) {
		t.Fatalf("empty result must be a list: %s", response.Body.String())
	}
}

func TestSearchNeedsTheUsersGitHubToken(t *testing.T) {
	github := &recordingGitHub{}
	router := newRouterWithAuth(github.client(), nil, newRateLimiter(10, time.Minute, time.Now), clientIdentity{}, nil, nil)
	response := searchWith(router, nil, "widgets")
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("search without a user token = %d %s", response.Code, response.Body.String())
	}
	if len(github.seen()) != 0 {
		t.Fatal("a search without a user token reached GitHub")
	}
}

func TestSearchRejectsShortOrLongQueries(t *testing.T) {
	github := &recordingGitHub{}
	router, session := searchRouter(t, github)
	before := len(github.seen())
	for _, query := range []string{"", "ab", "  ab  ", strings.Repeat("x", 257)} {
		if response := searchWith(router, session, query); response.Code != http.StatusBadRequest {
			t.Fatalf("query %q status = %d", query, response.Code)
		}
	}
	if len(github.seen()) != before {
		t.Fatal("an invalid query reached GitHub")
	}
}

func TestSearchRejectsItemsThatDoNotNameTheirPullRequest(t *testing.T) {
	for name, item := range map[string]string{
		"html url is not a pull request": `{"number":7,"title":"t","state":"open","updated_at":"2026-11-20T10:00:00Z","html_url":"https://github.com/acme/widgets/issues/7","repository_url":"https://api.github.com/repos/acme/widgets","pull_request":{}}`,
		"number disagrees":               `{"number":8,"title":"t","state":"open","updated_at":"2026-11-20T10:00:00Z","html_url":"https://github.com/acme/widgets/pull/7","repository_url":"https://api.github.com/repos/acme/widgets","pull_request":{}}`,
		"repository disagrees":           `{"number":7,"title":"t","state":"open","updated_at":"2026-11-20T10:00:00Z","html_url":"https://github.com/acme/widgets/pull/7","repository_url":"https://api.github.com/repos/acme/other","pull_request":{}}`,
		"not a pull request":             `{"number":7,"title":"t","state":"open","updated_at":"2026-11-20T10:00:00Z","html_url":"https://github.com/acme/widgets/pull/7","repository_url":"https://api.github.com/repos/acme/widgets"}`,
		"unknown state":                  `{"number":7,"title":"t","state":"weird","updated_at":"2026-11-20T10:00:00Z","html_url":"https://github.com/acme/widgets/pull/7","repository_url":"https://api.github.com/repos/acme/widgets","pull_request":{}}`,
		"missing update time":            `{"number":7,"title":"t","state":"open","html_url":"https://github.com/acme/widgets/pull/7","repository_url":"https://api.github.com/repos/acme/widgets","pull_request":{}}`,
	} {
		reply := `{"total_count":1,"incomplete_results":false,"items":[` + item + `]}`
		t.Run(name, func(t *testing.T) {
			router, session := searchRouter(t, &recordingGitHub{respond: func(*http.Request) *http.Response { return oauthResponse(http.StatusOK, reply) }})
			if response := searchWith(router, session, "widgets"); response.Code != http.StatusBadGateway {
				t.Fatalf("search = %d %s, want 502", response.Code, response.Body.String())
			}
		})
	}
}

func TestSearchReportsGitHubRateLimitsAsErrors(t *testing.T) {
	router, session := searchRouter(t, &recordingGitHub{respond: func(*http.Request) *http.Response {
		response := oauthResponse(http.StatusForbidden, `{"message":"API rate limit exceeded"}`)
		response.Header.Set("Retry-After", "30")
		return response
	}})
	response := searchWith(router, session, "widgets")
	if response.Code != http.StatusTooManyRequests || response.Header().Get("Retry-After") == "" {
		t.Fatalf("search = %d %s", response.Code, response.Body.String())
	}
}

func TestSearchesStayUnderGitHubsSearchLimitPerAccount(t *testing.T) {
	github := &recordingGitHub{respond: func(*http.Request) *http.Response { return oauthResponse(http.StatusOK, searchReply) }}
	router, session := searchRouter(t, github)
	for i := 0; i < githubSearchesPerMinute; i++ {
		if response := searchWith(router, session, "widgets"); response.Code != http.StatusOK {
			t.Fatalf("search %d = %d %s", i+1, response.Code, response.Body.String())
		}
	}
	before := len(github.seen())
	response := searchWith(router, session, "widgets")
	if response.Code != http.StatusTooManyRequests || response.Header().Get("Retry-After") == "" {
		t.Fatalf("search over the limit = %d %s", response.Code, response.Body.String())
	}
	if len(github.seen()) != before {
		t.Fatal("a search over the limit reached GitHub")
	}
}

func TestParsePullRequestReferenceAcceptsShorthand(t *testing.T) {
	tests := []struct {
		input       string
		owner, repo string
		number      int
		valid       bool
	}{
		{input: "acme/widgets#7", owner: "acme", repo: "widgets", number: 7, valid: true},
		{input: " acme/my.repo_name-2#12 ", owner: "acme", repo: "my.repo_name-2", number: 12, valid: true},
		{input: "https://github.com/acme/widgets/pull/7", owner: "acme", repo: "widgets", number: 7, valid: true},
		{input: "acme/widgets#0"},
		{input: "acme/widgets"},
		{input: "acme#7"},
		{input: "acme/wid gets#7"},
		{input: "../widgets#7"},
		{input: "acme/..#7"},
	}
	for _, tt := range tests {
		owner, repo, number, err := parsePullRequestReference(tt.input)
		if (err == nil) != tt.valid {
			t.Fatalf("%q: err = %v, want valid %v", tt.input, err, tt.valid)
		}
		if tt.valid && (owner != tt.owner || repo != tt.repo || number != tt.number) {
			t.Fatalf("%q = %s/%s#%d", tt.input, owner, repo, number)
		}
	}
}

func TestImportAcceptsOwnerRepoNumberShorthand(t *testing.T) {
	var paths []string
	github := &recordingGitHub{respond: func(request *http.Request) *http.Response {
		paths = append(paths, request.URL.Path)
		return oauthResponse(http.StatusNotFound, `{"message":"Not Found"}`)
	}}
	router := newRouterWithAuth(github.client(), nil, newRateLimiter(10, time.Minute, time.Now), clientIdentity{}, nil, nil)
	request := httptest.NewRequest(http.MethodPost, "/api/import", strings.NewReader(`{"url":"acme/widgets#7"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound || len(paths) == 0 || paths[len(paths)-1] != "/repos/acme/widgets/pulls/7" {
		t.Fatalf("shorthand import = %d %s, GitHub paths %v", response.Code, response.Body.String(), paths)
	}
}
