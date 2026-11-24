package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/felixgeelhaar/ghostline/backend/internal/domain"
)

// A pull request with more comments than Ghostline reads gets a reason the
// reader can act on, not advice to shrink someone else's comment history.
func TestQodoCommentLimitReasonSaysWhatWasReadAndWhereToLook(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		comments := make([]string, 100)
		for i := range comments {
			comments[i] = fmt.Sprintf(`{"id":%d,"body":"+1","user":{"login":"someone"}}`, i+1)
		}
		_, _ = w.Write([]byte("[" + strings.Join(comments, ",") + "]"))
	}))
	defer server.Close()
	repo, err := domain.NewRepositoryIdentity("octocat", "Hello-World")
	if err != nil {
		t.Fatal(err)
	}

	set := fetchQodoReviews(context.Background(), &githubClient{baseURL: server.URL, http: server.Client()}, repo, 1)

	want := "This pull request has more than 500 comments, and Ghostline reads only the first 500, so no Qodo reviews are shown rather than a partial list. Open the pull request on GitHub for its reviews."
	if set.Availability != reviewsUnavailable || set.Reason != want {
		t.Fatalf("reason = %q, want %q", set.Reason, want)
	}
}
