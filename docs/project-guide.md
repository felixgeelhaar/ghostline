# Ghostline project guide

## Short specification

Ghostline is a change-centric engineering control room for engineers using coding agents. Its current slice imports a **public GitHub pull request** and presents the change’s metadata, a source-labeled activity index of commit and published Qodo comment timestamps, revision-linked GitHub checks and check-run line annotations, Nox findings from the matching GitHub Actions report artifact, and review details. Nox results are accepted only when the artifact belongs to a workflow run for the requested PR revision; Ghostline shows the report source and expiry. Ghostline requires the run's PR base SHA and a matching workflow file there; a missing base SHA (as for fork PRs), an unreadable base workflow, or a mismatch leaves the report untrusted. A matching workflow is only corroborating context because GitHub does not document that field as immutable execution provenance. For GitHub check runs it preserves the source title and shows a bounded text preview of the source summary; oversized previews are labeled, and the linked GitHub check remains the source for full output. This preview is check output; Nox findings come from the report artifact described below. It compares an explicit revision marker in a published Qodo comment with the current PR head; “current” means that comment names the head SHA, not that Ghostline found the latest complete review run. Repeated Qodo finding titles can be traced across comments when their revision markers support it. It also checks issue references in the PR description and highlights issues that remain open after the PR is merged. Warden runs local developer gates and Nox scans; repository CI also runs Nox. Ghostline ingests Nox SARIF only from the matching workflow artifact, as described below.

The screenshot below shows the running app after importing Ghostline PR #56 at its earlier head `685decd`. The displayed Qodo comment, marked revision, and finding link to GitHub; this is live repository evidence captured for the feature, not a replay.

![Ghostline showing published Qodo comment coverage for the current PR head, with links to the marked revision and source comment](assets/qodo-head-comment-coverage.jpg)

The check list below shows GitHub's Nox check-run title and a short summary preview for PR #56 head `46f6276`. The preview remains linked to its GitHub source; it is not a full Nox report or an independent claim that the repository has no security findings.

![Ghostline showing revision-bound GitHub checks and the Nox check-run summary for PR #56](assets/nox-check-summary.jpg)

The Nox report panel links results from an artifact produced by this repository’s `.github/workflows/nox.yml` for the exact matched public PR revision. Before accepting the artifact, Ghostline verifies that this workflow file has the same Git blob SHA at the scanned revision and the pull request base, and that the pull request does not change any other file that steers the scan (the workflow, `.nox.yaml`, anything under `.nox/` (including the baseline), any `.gitignore`, and the `nox-results` scan output path). It then verifies GitHub’s artifact SHA-256 digest, requires the workflow's `scanned-commit.txt` record to name the scanned revision, and displays the scan revision and report expiry. If the workflow or a scan-control file changed, or either cannot be verified, Ghostline labels the report unavailable. Other workflows and private PRs are not accepted as Nox evidence. Active and SARIF-suppressed results are counted separately; suppressed results remain visible with their source kind and justification when supplied. Suppression does not mean a finding was fixed or verified. An unavailable or over-limit lookup is presented as unavailable evidence, not as a clean scan.

The screenshot below shows that panel with a live report for this pull request's head revision `1e2f56c`, fetched from the Security workflow artifact. [Evidence notes](feature-evidence/nox-report-live-1e2f56c.md).

![Ghostline's Nox report panel showing the Security workflow report for revision 1e2f56c](feature-evidence/nox-report-live-1e2f56c.png)

The product helps an engineer inspect what changed, understand which revision review feedback and checks describe, and decide what to inspect next. GitHub remains authoritative for pull-request facts; Qodo remains the review source. Ghostline does not run agents, repair code, prove that a finding was fixed, or replace CI, Qodo, Nox, or Warden. It does not scan a developer’s local filesystem. Structured Qodo findings can be fetched for one imported public PR using a Qodo API key supplied for that request from a secure origin; Ghostline does not persist the key. Local demo mode has no login. Protected mode supports GitHub OAuth, signed 12-hour sessions, and an allowlist of immutable numeric GitHub account IDs; logout revokes the session in the current backend process, and the OAuth token is discarded after identity retrieval. OAuth state and revoked sessions are process-local, so protected mode currently requires a single backend instance.

Evidence labels and links must preserve source and revision. Commit timestamps are commit metadata, not push or review times. A later commit in the PR list is only a candidate to inspect; ordering alone does not establish that it repaired a finding. If evidence is missing or unavailable, the UI should say so.

## Architecture overview

Ghostline is a small Go API and Vue single-page application. It has no database or background worker; importing a PR fetches a current view from GitHub.

```text
Browser (Vue 3 + TypeScript)
  presentation: App.vue, ReviewCard.vue, DeepTrace.vue
  application: fetchGhostlineSession, importPullRequest, fetchRevisionChecks, fetchQodoReviewSession, fetchCheckAnnotations
  domain: pullRequest, checks, reviews, trace, change activity, referenced issues
          │ /auth/*, /api/session, /api/import, /api/checks, /api/check-annotations, /api/nox-report, /api/qodo-review
          ▼
Go + Gin API
  GitHub OAuth callback, signed session, account-ID allowlist middleware
  HTTP handlers and GitHub response normalization
  GitHub client, referenced-issue lookup, check aggregation and annotation paging, Nox artifact/SARIF parsing, Qodo comment parsing
  fixed-endpoint Qodo MCP client
          ├─ HTTPS GitHub REST API → GitHub: pull requests, commits, checks, check annotations, Actions runs/artifacts, published comments, referenced issues
          └─ HTTPS Qodo MCP → Qodo: structured findings and review runs
```

The frontend validates API payloads with Zod at the boundary in `frontend/src/domain`. Keep business rules in domain modules, request orchestration in application modules, and HTTP or rendering details at the edges. In the backend, domain concepts such as the canonical repository identity, Nox workflow/scanner acceptance, and normalized Nox finding interpretation live in `backend/internal/domain`; HTTP handling, GitHub calls, and orchestration stay in the small files under `backend`. Put new business rules in the domain package, and avoid adding infrastructure layers without a concrete need.

For code changes, preserve these domain boundaries and use test-driven development: write or update a focused failing test for the behavior first, then implement the smallest change that makes it pass.

The main request is `POST /api/import`: validate the submitted PR URL, fetch PR metadata and commits, fetch checks for the PR head, recheck the head SHA to avoid mixing snapshots after a force-push, fetch published Qodo issue comments, look up up to 20 issue references from the PR description, normalize the response, and render the source-labeled activity index and review evidence. Issue references are limited to `#N` and same-repository GitHub issue URLs; returned issues are validated against their repository and number. Failed or capped lookups are shown as unavailable. A merged PR with an open referenced issue is flagged with links to the PR and issue; Ghostline does not determine whether the issue’s work was delivered. The activity index orders commit metadata times, check-run start/completion/update times, commit-status creation/update times, and Qodo comment times while preserving each source's timestamp label and native check/status ID. Check entries are GitHub's latest records for the focused revision at fetch time, not a complete run history. The timeline does not treat sequence as causation. Check-run titles and bounded summaries stay attached to their source-linked check. `POST /api/checks` fetches checks for a focused full commit SHA. `POST /api/check-annotations` fetches one GitHub annotation page for an identified check run. `POST /api/qodo-review` sends the imported PR URL and a one-request Qodo key to Qodo's fixed MCP endpoint, then validates and normalizes the structured result. The key is never written to storage or returned to the browser by the API. `POST /api/nox-report` accepts a PR URL and full revision SHA, confirms that the revision belongs to a public PR, finds a matching `pull_request` run from `.github/workflows/nox.yml` and its `nox-results` artifact, verifies that the workflow file has the same Git blob SHA at the scanned revision and PR base, checks run/SHA identity and the mandatory published SHA-256 digest, then parses bounded Nox SARIF 2.1.0 without extracting files. GitHub request volume is capped at 32 API calls per report lookup, and workflow/artifact lookup is capped at ten pages of 100 records. If a request or history cap prevents a complete search, the response says the inspection was incomplete rather than claiming the report is missing. It presents findings with the exact SHA, workflow run, and artifact retention dates. Missing, expired, oversized, mismatched, unsupported, or inaccessible reports are explicitly unavailable; Ghostline does not describe them as clean. Checks, annotations, Nox reports, and review sessions are labeled with the revision their source returned.

For a check run, `POST /api/check-annotations` verifies that the target repository is public, confirms the run's head SHA matches the requested revision, and retrieves GitHub's paginated annotations. The response links annotations to their source file and line range on that exact commit. Pagination follows GitHub's `Link` header so a full final page does not imply another page. A check run without a usable source URL can still return its annotations.

The [check-run annotations screenshot](evidence/check-run-annotations.md) shows this panel using a clearly labeled recorded replay. It displays three sampled annotations from a public GitHub Nox run; it is not a live scan or a live GitHub fetch.

## Setup and verification

Requirements for a local run: Go 1.27.4 or newer, Node.js 22.12 or newer, and npm.

Run the API from the repository root:

```sh
go run ./backend
```

By default it listens at `http://localhost:8080` (`HOST` and `PORT` can override the bind address and port). The frontend's development proxy targets that address.

In another terminal, run the frontend:

```sh
cd frontend
npm ci
npm run dev
```

Open the URL Vite prints. The frontend proxies `/api` requests to `http://localhost:8080`. Public GitHub API access works without credentials and uses GitHub's public API quota. If `GITHUB_TOKEN` is configured, it is used only for repository visibility metadata; pull-request data requests remain unauthenticated and the token does not raise their quota. The one exception is the Nox report artifact download: GitHub requires authentication for it even on public repositories, so after the visibility check confirms the repository is public, Ghostline downloads it with the token, which needs read access to Actions artifacts. Without that access Ghostline marks the report unavailable, not clean. Never commit or paste a token into a report.

Local mode leaves sign-in disabled. Protected OAuth mode is supported only when a trusted HTTPS ingress serves Ghostline at the same public origin as `GITHUB_OAUTH_CALLBACK_URL`; direct access to the local HTTP Vite URL is rejected by the same-origin check. Configure `GHOSTLINE_AUTH_MODE=required`, GitHub OAuth client ID and secret, the matching HTTPS callback, a 32-byte `GHOSTLINE_SESSION_SECRET`, and one or more numeric IDs in `GHOSTLINE_ALLOWED_GITHUB_IDS`. If the ingress proxies requests to Vite, set `GHOSTLINE_TRUSTED_PROXY_CIDRS` to its peer CIDRs and configure it to overwrite `X-Real-IP`. Local Vite proxies `/api` and `/auth` to the backend.

To run the local Vite development stack in containers:

```sh
docker compose up --build
```

Open `http://localhost:5173` for the unauthenticated local demo. Compose exposes the frontend on network interfaces by default; use `GHOSTLINE_BIND_ADDRESS` to select a bind address. Compose forwards `GITHUB_TOKEN` from the environment to the backend when set. The token is used only for repository visibility metadata; public PR, check, review-comment, annotation, workflow-run, and referenced-issue data are fetched without credentials; only the Nox artifact download uses it, after the visibility check. If a repository becomes private after the metadata check, GitHub rejects the later unauthenticated data request. Never commit or paste a token into a report. This stack runs Vite and is intended for local or trusted demos.

For the production-built, same-origin container, follow the [hosted deployment guide](hosted-deployment.md). It requires protected GitHub sign-in and an HTTPS ingress; it remains a single-instance deployment because authentication and rate-limit state are held in process memory.

Run the project checks:

```sh
go test ./... -count=1
go vet ./...
cd frontend && npm ci && npm test && npm run build
```

Warden configures pre-commit and pre-push gates in `.warden.yaml`. It runs the Go checks, frontend verification, and tracked-repository Nox scan. Install Warden and Nox as described in the [README](../README.md#repository-checks), then enable hooks in a fresh checkout with `warden init --hooks=pre-commit,pre-push`.

For detailed Qodo setup and review-evidence semantics, see [Qodo workflow](qodo-workflow.md). Repository rules for agents are in [AGENTS.md](../AGENTS.md).

The intended ticket-to-merge checks, review decisions, and a verified example are documented in [factory verification](factory-verification.md).

For current trust boundaries, abuse cases, and hosting limitations, see the [threat model](threat-model.md).

## Feature evidence

A statusless Qodo run remains visible with an explicit `STATUS NOT RETURNED` label in the [feature screenshot](feature-evidence/qodo-run-missing-status.png). This is a prepared replay fixture, not a live Qodo response.

The running change activity index, imported from Ghostline PR #49, is shown in [this screenshot](feature-evidence/change-activity-index.jpg). The revision-linked GitHub check events are shown in [this screenshot](feature-evidence/revision-linked-check-activity.jpg), imported from Ghostline PR #50 at revision `286dcc7`. The empty-check state is shown in [this screenshot](feature-evidence/activity-index-check-coverage.jpg), captured from public PR `octocat/Hello-World#1` when GitHub reported no check runs or commit statuses for the displayed revision. Qodo activity was unavailable at capture time and the UI says so. These views show current records or fetch-time availability; they do not represent complete event history.

The current source-backed review timeline, imported from Ghostline PR #50 at revision `4bae19d`, is shown in [this screenshot](feature-evidence/activity-index-review-timeline.jpg). It shows observed commit and published Qodo comment events; it does not imply that event order proves a repair relationship. Parser-incompleteness and partial-check-placement warnings are edge states covered by tests, not present in this PR's current records.

The [live referenced-issues panel](feature-evidence/referenced-issue-live-2026-11-13.jpg) shows issue states imported from a public pull request. This example’s referenced issues are now closed; the merged/open warning is covered by UI tests using explicit fixtures.

The [production-mode sign-in screen](feature-evidence/hosted-production-sign-in.jpg) is served by the Go process from the built Vue bundle. It was captured from a local production-configured run with test OAuth settings; no GitHub sign-in was completed. The production Docker image itself is built and smoke-tested by the `Production container smoke test` CI job.
