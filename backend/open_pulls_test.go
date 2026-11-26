package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const (
	openHeadA = "1111111111111111111111111111111111111111"
	openHeadB = "2222222222222222222222222222222222222222"
)

func openPull(number int, head, owner, name string, draft bool) map[string]any {
	return map[string]any{
		"number": number, "title": "Change " + string(rune('0'+number)), "draft": draft, "state": "open",
		"html_url": "https://github.com/" + owner + "/" + name + "/pull/" + string(rune('0'+number)),
		"user":     map[string]any{"login": "octo"},
		"head":     map[string]any{"sha": head},
		"base":     map[string]any{"ref": "main", "repo": map[string]any{"name": name, "owner": map[string]any{"login": owner}}},
	}
}

type openPullsBody struct {
	Repository string `json:"repository"`
	Complete   bool   `json:"complete"`
	Limit      int    `json:"limit"`
	Items      []struct {
		Number       int    `json:"number"`
		Title        string `json:"title"`
		Draft        bool   `json:"draft"`
		Author       string `json:"author"`
		HeadSHA      string `json:"headSha"`
		HeadShortSHA string `json:"headShortSha"`
		URL          string `json:"url"`
	} `json:"items"`
	Error string `json:"error"`
}

func postOpenPulls(t *testing.T, repository string, handler http.HandlerFunc) (int, openPullsBody) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/acme/app/pulls", handler)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	router := newRouter(&githubClient{baseURL: server.URL, http: server.Client()})
	payload, _ := json.Marshal(map[string]string{"repository": repository})
	request := httptest.NewRequest(http.MethodPost, "/api/open-pulls", strings.NewReader(string(payload)))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	var body openPullsBody
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode %q: %v", recorder.Body.String(), err)
	}
	return recorder.Code, body
}

func TestOpenPullsListsEachOpenPullRequestWithItsHead(t *testing.T) {
	code, body := postOpenPulls(t, "acme/app", func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		if query.Get("state") != "open" || query.Get("per_page") != "30" || query.Get("page") != "1" {
			t.Errorf("query = %q, want one page of up to 30 open pull requests", r.URL.RawQuery)
		}
		_ = json.NewEncoder(w).Encode([]any{openPull(2, openHeadB, "acme", "app", true), openPull(1, openHeadA, "acme", "app", false)})
	})
	if code != http.StatusOK || body.Repository != "acme/app" || !body.Complete || body.Limit != 30 || len(body.Items) != 2 {
		t.Fatalf("got %d %+v, want two items in a complete list for acme/app", code, body)
	}
	first := body.Items[0]
	if first.Number != 2 || first.HeadSHA != openHeadB || first.HeadShortSHA != "2222222" || !first.Draft || first.Author != "octo" || first.URL != "https://github.com/acme/app/pull/2" {
		t.Fatalf("first item = %+v", first)
	}
}

func TestOpenPullsAcceptsARepositoryURL(t *testing.T) {
	code, body := postOpenPulls(t, "https://github.com/acme/app", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[]`))
	})
	if code != http.StatusOK || body.Repository != "acme/app" || !body.Complete || body.Items == nil || len(body.Items) != 0 {
		t.Fatalf("got %d %+v, want an empty complete list", code, body)
	}
}

// More open pull requests than one page: the list says it is partial.
func TestOpenPullsMarksAListWithMorePagesAsPartial(t *testing.T) {
	code, body := postOpenPulls(t, "acme/app", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Link", `<https://api.github.com/repos/acme/app/pulls?state=open&page=2>; rel="next"`)
		_ = json.NewEncoder(w).Encode([]any{openPull(1, openHeadA, "acme", "app", false)})
	})
	if code != http.StatusOK || body.Complete || len(body.Items) != 1 {
		t.Fatalf("got %d %+v, want a partial list", code, body)
	}
}

func TestOpenPullsRejectsInputThatIsNotARepository(t *testing.T) {
	for _, input := range []string{"", "acme", "acme/app#1", "https://github.com/acme/app/pull/1", "http://github.com/acme/app", "https://example.com/acme/app", "acme/..", "acme/app/extra"} {
		t.Run(input, func(t *testing.T) {
			code, body := postOpenPulls(t, input, func(w http.ResponseWriter, r *http.Request) {
				t.Errorf("GitHub must not be called for %q", input)
			})
			if code != http.StatusBadRequest || body.Error == "" {
				t.Fatalf("got %d %+v, want 400 with an error", code, body)
			}
		})
	}
}

// GitHub must describe each pull request well enough to tie it to this repository and a head.
func TestOpenPullsRejectsAnItemItCannotTieToTheRepositoryOrAHead(t *testing.T) {
	tests := map[string]map[string]any{
		"short head":       openPull(1, "abc", "acme", "app", false),
		"other repository": openPull(1, openHeadA, "acme", "other", false),
		"no number": func() map[string]any {
			item := openPull(1, openHeadA, "acme", "app", false)
			item["number"] = 0
			return item
		}(),
		"foreign html link": func() map[string]any {
			item := openPull(1, openHeadA, "acme", "app", false)
			item["html_url"] = "https://evil.example/acme/app/pull/1"
			return item
		}(),
		"not open": func() map[string]any {
			item := openPull(1, openHeadA, "acme", "app", false)
			item["state"] = "closed"
			return item
		}(),
	}
	for name, item := range tests {
		t.Run(name, func(t *testing.T) {
			code, body := postOpenPulls(t, "acme/app", func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewEncoder(w).Encode([]any{item})
			})
			if code != http.StatusBadGateway || len(body.Items) != 0 {
				t.Fatalf("got %d %+v, want 502 and no items", code, body)
			}
		})
	}
}

func TestOpenPullsReportsAnUnknownRepository(t *testing.T) {
	code, body := postOpenPulls(t, "acme/app", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	if code != http.StatusNotFound || body.Error != "Repository not found. Check the name and confirm the repository is public." {
		t.Fatalf("got %d %q, want 404 naming the repository", code, body.Error)
	}
}
