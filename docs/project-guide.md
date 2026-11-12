# Ghostline project guide

## Short specification

Ghostline is a change-centric engineering control room for engineers using coding agents. Its current slice imports a **public GitHub pull request** and presents the change’s metadata, a source-labeled activity index of commit and published Qodo comment timestamps, revision-linked GitHub checks, and review details. Repeated Qodo finding titles can be traced across comments when their revision markers support it. Warden runs local developer gates and Nox scans; repository CI also runs Nox. Ghostline does not currently ingest Nox reports.

The product helps an engineer inspect what changed, understand which revision review feedback and checks describe, and decide what to inspect next. GitHub remains authoritative for pull-request facts; Qodo remains the review source. Ghostline does not run agents, repair code, prove that a finding was fixed, or replace CI, Qodo, Nox, or Warden. It does not scan a developer’s local filesystem.

Evidence labels and links must preserve source and revision. Commit timestamps are commit metadata, not push or review times. A later commit in the PR list is only a candidate to inspect; ordering alone does not establish that it repaired a finding. If evidence is missing or unavailable, the UI should say so.

## Architecture overview

Ghostline is a small Go API and Vue single-page application. It has no database or background worker; importing a PR fetches a current view from GitHub.

```text
Browser (Vue 3 + TypeScript)
  presentation: App.vue, ReviewCard.vue, DeepTrace.vue
  application: auth status, importPullRequest, fetchRevisionChecks
  domain: pullRequest, checks, reviews, trace
          │ /api/import, /api/checks
          ▼
Go + Gin API
  optional GitHub identity gate and signed session cookie
  HTTP handlers and GitHub response normalization
  GitHub client, check aggregation, Qodo comment parsing
          │ HTTPS GitHub REST API
          ▼
GitHub: OAuth identity, pull request, commits, checks, published issue comments
```

The frontend validates API payloads with Zod at the boundary in `frontend/src/domain`. Keep business rules in domain modules, request orchestration in application modules, and HTTP or rendering details at the edges. In the backend, domain concepts such as the canonical repository identity live in `backend/internal/domain`; HTTP handling, GitHub calls, and orchestration stay in the small files under `backend`. Put new business rules in the domain package, and avoid adding infrastructure layers without a concrete need.

For code changes, preserve these domain boundaries and use test-driven development: write or update a focused failing test for the behavior first, then implement the smallest change that makes it pass.

The main request is `POST /api/import`: validate the submitted PR URL, fetch PR metadata and commits, fetch checks for the PR head, recheck the head SHA to avoid mixing snapshots after a force-push, fetch published Qodo issue comments, normalize the response, and render the activity index and review evidence. The activity index orders commit metadata times, check-run start/completion/update times, commit-status creation/update times, and Qodo comment times while preserving each source's timestamp label and native check/status ID. Check entries are GitHub's latest records for the focused revision at fetch time, not a complete check-run history. The timeline does not treat sequence as causation. `POST /api/checks` fetches checks for a focused full commit SHA. Checks are always labeled with the revision GitHub returned and shown in a revision-scoped section.

When `GHOSTLINE_AUTH_MODE=github`, `/auth/github/login` starts an OAuth identity check and `/auth/github/callback` verifies the GitHub login against `GHOSTLINE_ALLOWED_GITHUB_USERS`, then issues a stateless signed eight-hour cookie. The token used to call GitHub's `/user` endpoint is not persisted. The auth status endpoint is public so the Vue app can render the right gate; imports and check lookups require a valid session. In production, set `GHOSTLINE_ENV=production`, which makes startup reject auth-off configuration. The local default remains unauthenticated for development.

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

Open the URL Vite prints. The frontend proxies `/api` requests to `http://localhost:8080`. Public GitHub API access works without credentials but has a low rate limit; export `GITHUB_TOKEN` before starting the local Go API to raise it. Never commit or paste a token into a report.

To run both services in containers:

```sh
docker compose up --build
```

Open `http://localhost:5173`, or the configured `GHOSTLINE_PORT`. Compose exposes the frontend on network interfaces by default; use `GHOSTLINE_BIND_ADDRESS` to select a bind address. This is a Vite development server intended for local/LAN demos, not public hosting. For a local OAuth demo, create a GitHub OAuth App with a localhost callback and set the environment variables in the README; `GHOSTLINE_SESSION_COOKIE_SECURE=false` is allowed only for localhost HTTP. The Compose backend does not currently receive `GITHUB_TOKEN`, so use the local API run above when authenticated GitHub API access is needed.

Run the project checks:

```sh
go test ./... -count=1
go vet ./...
cd frontend && npm ci && npm test && npm run build
```

Warden configures pre-commit and pre-push gates in `.warden.yaml`. It runs the Go checks, frontend verification, and tracked-repository Nox scan. Install Warden and Nox as described in the [README](../README.md#repository-checks), then enable hooks in a fresh checkout with `warden init --hooks=pre-commit,pre-push`.

For detailed Qodo setup and review-evidence semantics, see [Qodo workflow](qodo-workflow.md). Repository rules for agents are in [AGENTS.md](../AGENTS.md).

For current trust boundaries, abuse cases, and hosting limitations, see the [threat model](threat-model.md).

## Feature evidence

The hosted GitHub sign-in gate is shown in [this screenshot](feature-evidence/hosted-github-sign-in.jpg), captured from the running app with a signed-out session. The OAuth provider exchange itself was not exercised with a real GitHub OAuth App in this local capture.

The running change activity index, imported from Ghostline PR #49, is shown in [this screenshot](feature-evidence/change-activity-index.jpg). The revision-linked GitHub check events are shown in [this screenshot](feature-evidence/revision-linked-check-activity.jpg), imported from Ghostline PR #50 at revision `286dcc7`. The empty-check state is shown in [this screenshot](feature-evidence/activity-index-check-coverage.jpg), captured from public PR `octocat/Hello-World#1` when GitHub reported no check runs or commit statuses for the displayed revision. Qodo activity was unavailable at capture time and the UI says so. These views show current records or fetch-time availability; they do not represent complete event history.

The current source-backed review timeline, imported from Ghostline PR #50 at revision `4bae19d`, is shown in [this screenshot](feature-evidence/activity-index-review-timeline.jpg). It shows observed commit and published Qodo comment events; it does not imply that event order proves a repair relationship. Parser-incompleteness and partial-check-placement warnings are edge states covered by tests, not present in this PR's current records.
