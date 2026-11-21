package domain

import "testing"

func TestReadQodoSeverityCounts(t *testing.T) {
	tests := []struct {
		name  string
		facts QodoReviewCounts
		want  QodoSeverityCounts
	}{
		{name: "groups by severity", facts: QodoReviewCounts{Groups: []QodoSeverityGroup{{"High", 1}, {"medium", 2}, {"LOW", 3}, {"low", 1}}},
			want: QodoSeverityCounts{High: 1, Medium: 2, Low: 4, Readable: true}},
		{name: "no groups but the review states no open findings", facts: QodoReviewCounts{StatesNoOpenFindings: true},
			want: QodoSeverityCounts{Readable: true}},
		{name: "no groups and no statement", facts: QodoReviewCounts{}, want: QodoSeverityCounts{}},
		{name: "a group naming another severity", facts: QodoReviewCounts{Groups: []QodoSeverityGroup{{"critical", 1}, {"low", 1}}},
			want: QodoSeverityCounts{Low: 1}},
		{name: "a group whose count could not be read", facts: QodoReviewCounts{Groups: []QodoSeverityGroup{{"high", -1}}},
			want: QodoSeverityCounts{}},
		{name: "a nonzero tally carries no severity", facts: QodoReviewCounts{Groups: []QodoSeverityGroup{{"low", 1}}, Tallies: []QodoFindingTally{{"Rule violations", 1}}},
			want: QodoSeverityCounts{Low: 1}},
		{name: "zero tallies are fine", facts: QodoReviewCounts{StatesNoOpenFindings: true, Tallies: []QodoFindingTally{{"Bugs", 0}}},
			want: QodoSeverityCounts{Readable: true}},
		{name: "an unreadable tally", facts: QodoReviewCounts{StatesNoOpenFindings: true, Tallies: []QodoFindingTally{{"Bugs", -1}}},
			want: QodoSeverityCounts{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ReadQodoSeverityCounts(tt.facts)
			if got != tt.want {
				t.Fatalf("ReadQodoSeverityCounts = %+v, want %+v", got, tt.want)
			}
		})
	}
}
