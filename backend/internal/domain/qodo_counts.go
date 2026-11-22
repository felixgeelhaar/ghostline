package domain

import "strings"

// QodoSeverityGroup is one "View <severity> (N)" group heading in a Qodo code review.
// A negative Count means the heading's number could not be read.
type QodoSeverityGroup struct {
	Severity string
	Count    int
}

// QodoFindingTally is one "<kind> (N)" tally, such as "Rule violations (1)". It names
// no severity. A negative Count means the number could not be read.
type QodoFindingTally struct {
	Kind  string
	Count int
}

// QodoReviewCounts are the count facts a Qodo code review states, as written.
type QodoReviewCounts struct {
	Groups               []QodoSeverityGroup
	Tallies              []QodoFindingTally
	StatesNoOpenFindings bool
}

// QodoSeverityCounts are a code review's open-finding counts by severity. Readable is
// false when the counts cannot be attributed to High, Medium and Low.
type QodoSeverityCounts struct {
	High     int
	Medium   int
	Low      int
	Readable bool
}

// ReadQodoSeverityCounts attributes a review's counts to High, Medium and Low. It fails
// closed: a group naming another severity, a number that could not be read, a nonzero
// tally (which carries no severity), or a review with neither groups nor a "no open
// findings" statement makes the counts unreadable.
func ReadQodoSeverityCounts(facts QodoReviewCounts) QodoSeverityCounts {
	counts := QodoSeverityCounts{Readable: len(facts.Groups) > 0 || facts.StatesNoOpenFindings}
	for _, group := range facts.Groups {
		switch {
		case group.Count < 0:
			counts.Readable = false
		case strings.EqualFold(group.Severity, "high"):
			counts.High += group.Count
		case strings.EqualFold(group.Severity, "medium"):
			counts.Medium += group.Count
		case strings.EqualFold(group.Severity, "low"):
			counts.Low += group.Count
		default:
			counts.Readable = false
		}
	}
	for _, tally := range facts.Tallies {
		if tally.Count != 0 {
			counts.Readable = false
		}
	}
	return counts
}
