package main

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The release workflow publishes images, so its supply-chain rules (issue #98) are tested:
// every action is pinned to a full commit SHA, the image job holds exactly the four
// permissions it needs, and only the job that drafts the release may write contents.
func TestReleaseWorkflowKeepsItsSupplyChainRules(t *testing.T) {
	raw, err := os.ReadFile("../.github/workflows/release.yml")
	if err != nil {
		t.Fatal(err)
	}
	workflow := string(raw)

	uses := regexp.MustCompile(`(?m)^\s*(?:-\s*)?uses:\s*(\S+)`).FindAllStringSubmatch(workflow, -1)
	if len(uses) == 0 {
		t.Fatal("release workflow uses no actions")
	}
	pinned := regexp.MustCompile(`^[\w.-]+/[\w./-]+@[0-9a-f]{40}$`)
	for _, use := range uses {
		if !pinned.MatchString(use[1]) {
			t.Errorf("action %q is not pinned to a full commit SHA", use[1])
		}
	}

	if !regexp.MustCompile(`(?m)^on:\n  push:\n    tags: \["v\*"\]\n`).MatchString(workflow) {
		t.Error("release workflow must run only for v* tag pushes")
	}
	if !regexp.MustCompile(`(?m)^permissions:\n  contents: read\n`).MatchString(workflow) {
		t.Error("workflow-level permissions must be contents: read")
	}

	jobs := jobBlocks(workflow)
	if got, want := permissionsOf(jobs["image"]), "attestations: write,contents: read,id-token: write,packages: write"; got != want {
		t.Errorf("image job permissions = %q, want %q", got, want)
	}
	if got := permissionsOf(jobs["draft-release"]); got != "contents: write" {
		t.Errorf("draft-release job permissions = %q, want only contents: write", got)
	}
	if strings.Contains(jobs["draft-release"], "uses:") {
		t.Error("draft-release, which can write contents, must not run third-party actions")
	}
	if strings.Count(jobs["draft-release"], "--signer-workflow felixgeelhaar/ghostline/.github/workflows/release.yml") != 2 {
		t.Error("the release notes' verify commands must pin the signing workflow, not just the owner")
	}
	if !strings.Contains(jobs["draft-release"], "--draft") {
		t.Error("the release must be created as a draft for a person to publish")
	}
	if !strings.Contains(workflow, "type=raw,value=latest,enable=${{ steps.version.outputs.latest == 'true' }}") {
		t.Error("latest must follow the version check, not just the tag name")
	}
	if !regexp.MustCompile(`(?m)^concurrency:\n  group: release\n  cancel-in-progress: false\n`).MatchString(workflow) {
		t.Error("releases must run one at a time in a single group, so latest cannot move backwards")
	}
	if !strings.Contains(jobs["image"], "needs: verify") || permissionsOf(jobs["verify"]) != "contents: read" {
		t.Error("the image job must wait for a verify job that holds only contents: read")
	}
	for _, check := range []string{"go vet ./...", "go test ./... -count=1", "npm test", "merge-base --is-ancestor"} {
		if !strings.Contains(jobs["verify"], check) {
			t.Errorf("verify job does not run %q", check)
		}
	}
	if !strings.Contains(jobs["image"], "imagetools inspect") {
		t.Error("the image job must refuse to overwrite an already published version tag")
	}
}

// jobBlocks splits the jobs: section into each job's lines, keyed by job ID.
func jobBlocks(workflow string) map[string]string {
	blocks := map[string]string{}
	section := workflow[strings.Index(workflow, "\njobs:\n")+len("\njobs:\n"):]
	header := regexp.MustCompile(`(?m)^  ([\w-]+):\n`)
	matches := header.FindAllStringSubmatchIndex(section, -1)
	for i, m := range matches {
		end := len(section)
		if i+1 < len(matches) {
			end = matches[i+1][0]
		}
		blocks[section[m[2]:m[3]]] = section[m[1]:end]
	}
	return blocks
}

// permissionsOf returns a job's permissions block as sorted "scope: level" pairs.
func permissionsOf(job string) string {
	block := regexp.MustCompile(`(?m)^    permissions:\n((?:      [\w-]+: \w+\n)+)`).FindStringSubmatch(job)
	if block == nil {
		return ""
	}
	var pairs []string
	for _, line := range strings.Split(strings.TrimSpace(block[1]), "\n") {
		pairs = append(pairs, strings.TrimSpace(line))
	}
	sort.Strings(pairs)
	return strings.Join(pairs, ",")
}
