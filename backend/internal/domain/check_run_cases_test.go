package domain

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// sharedCheckRunCases is testdata/check-run-cases.json, the table head-evidence.sh is
// tested against too, so the app and the release gate cannot drift apart.
type sharedCheckRunCases struct {
	About string `json:"about"`
	Check string `json:"check"`
	Cases []struct {
		Name string `json:"name"`
		Runs []struct {
			Status     string  `json:"status"`
			Conclusion *string `json:"conclusion"`
		} `json:"runs"`
		Outcome string `json:"outcome"`
		Message string `json:"message"`
		Source  int    `json:"source"`
	} `json:"cases"`
}

func loadSharedCheckRunCases(t *testing.T) sharedCheckRunCases {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "check-run-cases.json"))
	if err != nil {
		t.Fatalf("read shared check-run cases: %v", err)
	}
	var table sharedCheckRunCases
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&table); err != nil {
		t.Fatalf("parse shared check-run cases: %v", err)
	}
	if table.Check == "" || len(table.Cases) == 0 {
		t.Fatal("shared check-run cases name no check or no cases")
	}
	return table
}

func TestClassifyCheckRunsMatchesTheSharedCases(t *testing.T) {
	table := loadSharedCheckRunCases(t)
	outcomes := map[string]ReadinessOutcome{"pass": ReadinessPass, "fail": ReadinessFail, "incomplete": ReadinessIncomplete}
	for _, tc := range table.Cases {
		t.Run(tc.Name, func(t *testing.T) {
			want, ok := outcomes[tc.Outcome]
			if !ok {
				t.Fatalf("unknown outcome %q", tc.Outcome)
			}
			if len(tc.Runs) == 0 || tc.Source < 0 || tc.Source >= len(tc.Runs) {
				t.Fatalf("case needs runs and a source index within them")
			}
			runs := make([]CheckRunEvidence, len(tc.Runs))
			for i, run := range tc.Runs {
				conclusion := ""
				if run.Conclusion != nil {
					conclusion = *run.Conclusion
				}
				runs[i] = CheckRunEvidence{ID: int64(i + 1), Name: table.Check, Status: run.Status, Conclusion: conclusion, URL: fmt.Sprintf("https://github.com/acme/app/runs/%d", i+1)}
			}

			got := classifyCheckRuns(table.Check, runs[len(runs)-1], runs[:len(runs)-1])

			if got.outcome != want {
				t.Errorf("outcome = %s, want %s (%s)", got.outcome, want, got.message)
			}
			if !strings.Contains(got.message, tc.Message) {
				t.Errorf("message = %q, want it to contain %q", got.message, tc.Message)
			}
			if got.sourceURL != runs[tc.Source].URL {
				t.Errorf("source = %s, want run %d (%s)", got.sourceURL, tc.Source, runs[tc.Source].URL)
			}
		})
	}
}
