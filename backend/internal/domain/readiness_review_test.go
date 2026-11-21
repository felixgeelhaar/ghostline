package domain

import (
	"testing"
	"time"
)

func TestSelectQodoReviewEvidence(t *testing.T) {
	at := func(hour int) time.Time { return time.Date(2026, 11, 21, hour, 0, 0, 0, time.UTC) }
	tests := []struct {
		name        string
		reviews     []QodoReviewCandidate
		wantCommit  string
		wantNone    bool
		wantSummary bool
	}{
		{name: "no reviews", wantNone: true},
		{name: "only a summary that names the head", reviews: []QodoReviewCandidate{{Summary: true, CommitSHA: readyHead, UpdatedAt: at(9)}}, wantNone: true, wantSummary: true},
		{name: "most recently updated code review wins", reviews: []QodoReviewCandidate{
			{CommitSHA: readyHead, UpdatedAt: at(11)},
			{CommitSHA: olderHead, UpdatedAt: at(10)},
		}, wantCommit: readyHead},
		{name: "a newer edit of an older comment wins over a later-created comment", reviews: []QodoReviewCandidate{
			{CommitSHA: olderHead, UpdatedAt: at(12)},
			{CommitSHA: readyHead, UpdatedAt: at(10)},
		}, wantCommit: olderHead},
		{name: "the later comment wins a tie on update time", reviews: []QodoReviewCandidate{
			{CommentID: 9, CommitSHA: readyHead, UpdatedAt: at(11)},
			{CommentID: 3, CommitSHA: olderHead, UpdatedAt: at(11)},
		}, wantCommit: readyHead},
		{name: "a summary is never the code review", reviews: []QodoReviewCandidate{
			{CommitSHA: olderHead, UpdatedAt: at(9)},
			{Summary: true, CommitSHA: readyHead, UpdatedAt: at(12)},
		}, wantCommit: olderHead, wantSummary: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, summaryNamesHead := SelectQodoReviewEvidence(tt.reviews, readyHead)
			if summaryNamesHead != tt.wantSummary {
				t.Fatalf("summaryNamesHead = %v, want %v", summaryNamesHead, tt.wantSummary)
			}
			if tt.wantNone {
				if got != nil {
					t.Fatalf("got %+v, want no review", got)
				}
				return
			}
			if got == nil || got.CommitSHA != tt.wantCommit {
				t.Fatalf("got %+v, want commit %s", got, tt.wantCommit)
			}
		})
	}
}
