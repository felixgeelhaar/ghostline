package domain

import "testing"

func TestMergeQodoAliasesFillsMissingValuesAndRejectsConflicts(t *testing.T) {
	got, err := MergeQodoStringAliases("status", "", "completed")
	if err != nil || got != "completed" {
		t.Fatalf("merge = %q, %v", got, err)
	}
	if _, err := MergeQodoStringAliases("status", "completed", "failed"); err == nil {
		t.Fatal("conflicting aliases accepted")
	}
	gotInt, err := MergeQodoIntAliases("line", 0, 18)
	if err != nil || gotInt != 18 {
		t.Fatalf("integer merge = %d, %v", gotInt, err)
	}
	if _, err := MergeQodoIntAliases("line", 18, 19); err == nil {
		t.Fatal("conflicting integer aliases accepted")
	}
}
