package main

import (
	"errors"
	"testing"
)

func TestParsePRURL(t *testing.T) {
	tests := []struct {
		name, input         string
		wantOwner, wantRepo string
		wantNumber          int
		wantErr             bool
	}{
		{"valid", "https://github.com/acme/ghostline/pull/42", "acme", "ghostline", 42, false},
		{"trailing slash", "https://github.com/acme/ghostline/pull/42/", "acme", "ghostline", 42, false},
		{"wrong host", "https://example.com/acme/ghostline/pull/42", "", "", 0, true},
		{"wrong route", "https://github.com/acme/ghostline/issues/42", "", "", 0, true},
		{"query", "https://github.com/acme/ghostline/pull/42?x=1", "", "", 0, true},
		{"zero", "https://github.com/acme/ghostline/pull/0", "", "", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner, repo, number, err := parsePRURL(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && (owner != tt.wantOwner || repo != tt.wantRepo || number != tt.wantNumber) {
				t.Fatalf("got %s/%s#%d", owner, repo, number)
			}
		})
	}
}

func TestWardenStatus(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		output string
		want   string
	}{
		{name: "clean gate", want: "passed"},
		{name: "failed checks", err: errors.New("exit status 1"), output: "step failed", want: "failed"},
		{name: "checks passed without published provenance", err: errors.New("exit status 4"), output: "warden: checks passed; provenance incomplete; git push stopped", want: "passed_unpublished"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := wardenStatus(tt.err, tt.output); got != tt.want {
				t.Fatalf("wardenStatus() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNoxStatus(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		output string
		status string
		count  int
	}{
		{name: "clean report", output: `{"findings":[]}`, status: "complete"},
		{name: "report with findings", err: errors.New("exit status 1"), output: `{"findings":[{},{}]}`, status: "findings", count: 2},
		{name: "null findings is malformed", output: `{"findings":null}`, status: "error"},
		{name: "scanner failed without report", err: errors.New("exit status 2"), output: "invalid configuration", status: "error"},
		{name: "scanner failed with empty report", err: errors.New("exit status 2"), output: `{"findings":[]}`, status: "error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, count := noxStatus([]byte(tt.output), tt.err)
			if status != tt.status || count != tt.count {
				t.Fatalf("noxStatus() = (%q, %d), want (%q, %d)", status, count, tt.status, tt.count)
			}
		})
	}
}
