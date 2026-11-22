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

	if !regexp.MustCompile(`(?m)^on:\n  push:\n    tags: \["v\[0-9\]\+\.\[0-9\]\+\.\[0-9\]\+\*"\]\n`).MatchString(workflow) {
		t.Error("release workflow must run only for vMAJOR.MINOR.PATCH tag pushes")
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
	// The image job lacks the artifact-metadata permission a storage record needs, so every
	// attestation must turn it off; attest-sbom cannot, because it hides that input.
	if strings.Contains(workflow, "actions/attest-sbom@") {
		t.Error("use actions/attest for the SBOM: attest-sbom always creates a storage record")
	}
	if got, want := strings.Count(jobs["image"], "create-storage-record: false"), strings.Count(jobs["image"], "push-to-registry: true"); got != want || want == 0 {
		t.Errorf("%d of %d registry attestations turn off the storage record", got, want)
	}
	if !regexp.MustCompile(`(?m)^concurrency:\n(?:  .*\n)*  queue: max\n`).MatchString(workflow) {
		t.Error("queued tags must wait in order (queue: max), not replace each other")
	}
	// A rerun after a partial failure may continue only for the same commit, and only a
	// definite "not found" may count as an unpublished version.
	// A same-commit rerun reuses the digest it already pushed; rebuilding would give the
	// version a new digest (BuildKit stamps a fresh creation time).
	if !strings.Contains(jobs["image"], "if: steps.published.outputs.digest == ''") {
		t.Error("the build must be skipped when this commit's image is already published")
	}
	if !strings.Contains(jobs["image"], "digest: ${{ steps.build.outputs.digest || steps.published.outputs.digest }}") {
		t.Error("the job must report the existing digest when it skips the build")
	}
	// latest is decided among tags on main only: a tag elsewhere never releases, so it
	// must not hold latest back.
	if !strings.Contains(jobs["image"], "--merged origin/main") {
		t.Error("latest must be decided among tags on main")
	}
	if !strings.Contains(jobs["image"], semverPattern) || !strings.Contains(jobs["image"], "versionsort.suffix=-") {
		t.Error("tag lists must keep only semver tags and sort prereleases before their release")
	}
	if !strings.Contains(jobs["image"], "sha256:[0-9a-f]{64}") {
		t.Error("a reused digest must be checked to be a sha256 digest")
	}
	if !strings.Contains(jobs["draft-release"], `--notes-start-tag "$PREVIOUS"`) || !strings.Contains(jobs["image"], "previous=") {
		t.Error("draft notes must start at the previous tag, which the image job computes")
	}
	for _, guard := range []string{"org.opencontainers.image.revision", `"$GITHUB_SHA"`, "not found", "Could not check"} {
		if !strings.Contains(jobs["image"], guard) {
			t.Errorf("the overwrite guard does not handle %q", guard)
		}
	}
}

// semverPattern is the tag shape the workflow releases: vMAJOR.MINOR.PATCH with an
// optional prerelease suffix.
const semverPattern = `^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$`

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
