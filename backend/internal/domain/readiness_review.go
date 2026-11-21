package domain

import "time"

// QodoReviewCandidate is one Qodo comment as the readiness rules see it. Summary marks
// Qodo's PR summary, which names a commit but is not a code review.
type QodoReviewCandidate struct {
	CommentID      int64
	Summary        bool
	CommitSHA      string
	URL            string
	UpdatedAt      time.Time
	High           int
	Medium         int
	Low            int
	CountsReadable bool
}

// SelectQodoReviewEvidence picks the most recently updated Qodo code review. Qodo edits
// one comment in place, so creation order alone does not identify the current review.
// It also reports whether a summary names the head, which explains a review that lags it.
func SelectQodoReviewEvidence(reviews []QodoReviewCandidate, head string) (*QodoReviewEvidence, bool) {
	var latest *QodoReviewCandidate
	summaryNamesHead := false
	for i := range reviews {
		review := &reviews[i]
		if review.Summary {
			summaryNamesHead = summaryNamesHead || review.CommitSHA == head
			continue
		}
		// GitHub timestamps have one-second resolution; on a tie the later comment wins.
		if latest == nil || review.UpdatedAt.After(latest.UpdatedAt) || review.UpdatedAt.Equal(latest.UpdatedAt) && review.CommentID > latest.CommentID {
			latest = review
		}
	}
	if latest == nil {
		return nil, summaryNamesHead
	}
	return &QodoReviewEvidence{CommitSHA: latest.CommitSHA, URL: latest.URL, High: latest.High, Medium: latest.Medium, Low: latest.Low, CountsReadable: latest.CountsReadable}, summaryNamesHead
}
