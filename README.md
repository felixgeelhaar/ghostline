# Ghostline

**See the work. Trace the decisions. Take control.**

[![CI](https://github.com/felixgeelhaar/ghostline/actions/workflows/ci.yml/badge.svg)](https://github.com/felixgeelhaar/ghostline/actions/workflows/ci.yml) [![Security](https://github.com/felixgeelhaar/ghostline/actions/workflows/nox.yml/badge.svg)](https://github.com/felixgeelhaar/ghostline/actions/workflows/nox.yml)

Ghostline is a change-centric engineering control room. It imports a public GitHub pull request and shows its commit timeline, with commit timestamps clearly distinguished from push or pull-request event chronology. It displays GitHub checks with source-linked line annotations for a selected check run, source-linked Qodo review feedback, and the published Nox SARIF report for the exact pull-request revision. Nox reports are matched through GitHub Actions workflow runs and their `nox-results` artifacts. Ghostline accepts a report only when the workflow file at the scanned revision matches the pull request base, preventing a PR-edited workflow from supplying a fabricated clean report; it shows findings, report availability, expiry, and the source run. It says explicitly when a report is missing or expired and never treats an earlier revision's checks or scan as evidence for the current head.

See the [project guide](docs/project-guide.md) for the product specification, architecture overview, and complete setup path. The [threat model](docs/threat-model.md) records current trust boundaries and hosting limitations. For a production-built single-container deployment, see the [hosted deployment guide](docs/hosted-deployment.md).

The [check-run annotation evidence](docs/evidence/check-run-annotations.md) shows the working panel in a clearly labeled recorded replay.

## Run locally

Requirements: Go 1.27.4+ and Node.js 22.12+.

```sh
go run ./backend
```

In another terminal:

```sh
cd frontend
npm ci
npm run dev
```

Open the URL printed by Vite. Public GitHub API requests work without a token. If `GITHUB_TOKEN` is configured, Ghostline uses it only to check repository visibility; pull-request, commit, check, and annotation data are fetched without credentials. The token does not raise the quota for those public-data requests. The one exception is a Nox report: GitHub requires authentication to download an Actions artifact even from a public repository, so after the visibility check confirms the repository is public, Ghostline downloads the `nox-results` artifact with the token. That token needs read access to Actions artifacts. Without it, Ghostline reports the Nox evidence as unavailable rather than clean.

## Request rate limit

`POST /api/import`, `POST /api/checks`, `POST /api/check-annotations`, `POST /api/nox-report`, and `POST /api/qodo-review` share one per-client budget so a single caller cannot use up the GitHub API quota for everyone else. The limiter is in-memory and fixed-window: the window starts at a client's first request, and the count and any recorded clients are lost when the backend restarts or if you run more than one instance. A request over the limit receives HTTP 429, a `Retry-After` header in seconds, and a JSON `{"error": "..."}` body; the UI shows when to retry. `GET /api/health` is not limited.

| Variable | Default | Meaning |
| --- | --- | --- |
| `RATE_LIMIT_REQUESTS` | `30` | Requests allowed per client per window (positive integer). |
| `RATE_LIMIT_WINDOW` | `1m` | Window length as a Go duration, such as `30s` or `5m`. |
| `RATE_LIMIT_CLIENT_IP_HEADER` | unset | Header carrying exactly one client IP, set by a proxy you operate. |
| `RATE_LIMIT_TRUSTED_PROXIES` | unset | Comma-separated CIDR ranges of proxies allowed to set that header. Required together with the header. |

Invalid values stop the backend at startup with an error. By default a client is its TCP peer address and forwarded headers are ignored, because callers can forge them. The header is read only when the request arrives from a trusted proxy, and only when it holds a single IP; forwarding chains such as a multi-hop `X-Forwarded-For` fall back to the peer address. The proxy must overwrite the header with the address of the connection it received, not append to it.

The Vite proxy used for local development and Docker Compose does this: it sets `X-Ghostline-Client-IP` to each browser's address, replacing any value the browser sent. Docker Compose configures the backend to trust that header from private network ranges, since the backend is not published and only the frontend container can reach it. When running `go run ./backend` and `npm run dev` directly, every request comes from `127.0.0.1` and shares one budget unless you set both variables.

This limit does not replace authentication, does not protect or meter a `GITHUB_TOKEN`, and does not stop a distributed caller using many addresses. GitHub's own 429 responses are reported separately, without a retry time.

## Repository checks

This repository's `.warden.yaml` configures Warden developer gates: Go tests and a tracked-repository Nox scan before commits, then Go tests, `go vet`, frontend tests/build, and another Nox scan before pushes. The Warden steps call Nox directly. Its `trusted_keys` list defines whose signed local validation notes the repository accepts; update that list deliberately when a maintainer signing key changes. Install Warden (for example, `go install go.klarlabs.de/warden@v0.46.1`) and Nox 1.67.0, the version used by the Security workflow, from the [official release](https://github.com/Nox-HQ/nox/releases/tag/v1.67.0), placing its `nox` binary on `PATH`. Then run `warden init --hooks=pre-commit,pre-push` in a fresh checkout to activate the hooks. Nox uses the committed `.nox/baseline.json` to identify accepted findings. Warden removes stale scan output, requires a fresh report, and runs `nox ci-gate`, matching CI policy: any code-security finding and net-new critical/high findings above the baseline block; new medium/low findings remain reported but do not block. Required-analyzer gaps also fail the gate. These hooks help contributors catch issues early. Hosted security and validation evidence should come from repository CI checks attached to a commit; Ghostline displays those checks only for the commit they ran against.

Pull requests also run GitHub Actions CI for Go tests/vetting and frontend tests/builds. Actions run with read-only repository permissions and are pinned to commit SHAs.

The `Security` GitHub Actions workflow runs the SHA-pinned Nox Marketplace action with Nox 1.67.0 on pull request revisions and pushes to `main`. For a pull request it checks out and scans the PR head commit, not GitHub's merge preview, and attributes the code-scanning upload to that commit. It uploads SARIF to code scanning, runs `nox ci-gate`, and keeps the JSON/SARIF output as a `nox-results` artifact for 7 days. The gate fails on a missing report, a required analyzer that did not run, any code-security finding (HARDEN, PERM, CRYPTO, MEMSAFE), net-new critical or high findings above the committed `.nox/baseline.json`, or a baseline entry that no longer matches a finding. Medium and low findings remain visible and reported without blocking the gate. Pushes to `main` also create or reopen one GitHub issue per untracked Nox fingerprint rated medium or above (or without a severity); informational and low findings stay in the report and SARIF only; baseline entries whose reason links an issue are already tracked and do not create duplicates. Each issue links the scan run and the exact scanned commit. Ghostline retrieves these artifacts by exact run and commit SHA, validates the GitHub artifact digest, and accepts Nox SARIF 2.1.0. It accepts a report only when GitHub supplies the run's PR base SHA and the workflow file there matches the scanned revision; a missing base SHA (as for fork PRs), an unreadable base workflow, or a mismatch leaves the report untrusted. It also rejects the report when the pull request changes a file that steers the scan (the workflow, `.nox.yaml`, anything under `.nox/` (including the baseline) and any `.gitignore`), when that cannot be verified, or when `.nox.yaml` moves the baseline. The artifact's `scanned-commit.txt` must name the requested revision; artifacts without it, including those from before the workflow scanned the PR head, are untrusted. The base SHA is not documented as immutable execution provenance; matching workflow content is corroborating context only. Artifacts are not attested, so treat them as run output rather than signed provenance. Ghostline shows the source run, revision, and report retention expiry with findings.

## Docker Compose

```sh
docker compose up --build
```

Open http://localhost:5173 (or set `GHOSTLINE_PORT` if that port is occupied). Compose binds the frontend to all network interfaces by default, so other devices on the LAN can use `http://<host-lan-ip>:5173`; set `GHOSTLINE_BIND_ADDRESS` to a specific interface address to restrict access. The development frontend proxies API requests to the backend container. Compose forwards `GITHUB_TOKEN` from the environment to the backend only for repository visibility checks; public pull-request data requests remain unauthenticated and use GitHub's public API quota.

### Protected hosted-mode sign-in

Compose defaults to an unauthenticated local demo. Protected-mode sign-in requires a trusted HTTPS ingress whose public origin matches the origin in `GITHUB_OAUTH_CALLBACK_URL`. The ingress must route `/auth/*` and `/api/*` to Compose and overwrite `X-Real-IP` with the client address. Browsing directly to Compose over `http://localhost:5173` or a LAN IP is intentionally rejected by the same-origin check and is not a supported protected-mode setup. Configure these variables in the environment running Compose:

| Variable | Purpose |
| --- | --- |
| `GHOSTLINE_AUTH_MODE` | Set to `required` to protect all `/api` routes. |
| `GITHUB_OAUTH_CLIENT_ID` / `GITHUB_OAUTH_CLIENT_SECRET` | GitHub OAuth App credentials. |
| `GITHUB_OAUTH_CALLBACK_URL` | Exact HTTPS callback, for example `https://ghostline.example/auth/callback`. |
| `GHOSTLINE_SESSION_SECRET` | Random secret of at least 32 bytes used to sign state and sessions. |
| `GHOSTLINE_ALLOWED_GITHUB_IDS` | Comma-separated numeric GitHub user IDs allowed to sign in. |
| `GHOSTLINE_TRUSTED_PROXY_CIDRS` | Vite development proxy only: optional CIDRs for ingress peers trusted to supply `X-Real-IP`. The production Go service uses `RATE_LIMIT_CLIENT_IP_HEADER` and `RATE_LIMIT_TRUSTED_PROXIES`, documented in the hosted deployment guide. |

The callback exchanges the temporary OAuth code, reads the account identity, and discards the GitHub token. Sessions last 12 hours. Use a secret manager for deployment credentials. This two-container Compose stack remains a Vite development environment for trusted demos. For the production-built single-container setup, see the [hosted deployment guide](docs/hosted-deployment.md). See the [threat model](docs/threat-model.md) for limits that still apply to either deployment.

The protected-mode sign-in screen is shown in this [working-app screenshot](docs/feature-evidence/hosted-github-sign-in.jpg); completing sign-in requires configured GitHub OAuth credentials.

## Verify

```sh
go test ./...
cd frontend && npm test && npm run build
```

The frontend is organized by domain, application use cases, and presentation. Zod validates API responses at the boundary.

See [Contributing](CONTRIBUTING.md) for the local workflow and [Security](SECURITY.md) for private vulnerability reporting.
