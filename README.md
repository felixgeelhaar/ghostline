# Ghostline

**See the work. Trace the decisions. Take control.**

[![CI](https://github.com/felixgeelhaar/ghostline/actions/workflows/ci.yml/badge.svg)](https://github.com/felixgeelhaar/ghostline/actions/workflows/ci.yml) [![Security](https://github.com/felixgeelhaar/ghostline/actions/workflows/nox.yml/badge.svg)](https://github.com/felixgeelhaar/ghostline/actions/workflows/nox.yml)

Ghostline is a change-centric engineering control room. The first slice imports a public GitHub pull request and shows its commit timeline, with commit timestamps clearly distinguished from push or pull-request event chronology. It also shows the checks GitHub reports for the pull request's head commit — check runs and commit statuses, each with its status and GitHub link — and says explicitly when that commit has no checks or when they could not be loaded. Checks from earlier commits are never shown as validation of the head revision. Checks belong in repository CI, where their results can be tied to the revision they actually scanned; Ghostline does not scan a developer's local filesystem.

## Run locally

Requirements: Go 1.27.3+ and Node.js 22.12+.

```sh
go run ./backend
```

In another terminal:

```sh
cd frontend
npm ci
npm run dev
```

Open the URL printed by Vite. Public GitHub API requests work without a token; set `GITHUB_TOKEN` to increase the unauthenticated rate limit.

## Repository checks

This repository's `.warden.yaml` configures Warden developer gates: Go tests and a tracked-repository Nox scan before commits, then Go tests, `go vet`, frontend tests/build, and another Nox scan before pushes. The Warden steps call Nox directly. Install Warden (for example, `go install go.klarlabs.de/warden@v0.46.1`) and Nox 1.63.0, the version the CI security scan uses, from the [official release](https://github.com/Nox-HQ/nox/releases/tag/v1.63.0), placing its `nox` binary on `PATH`. Then run `warden init --hooks=pre-commit,pre-push` in a fresh checkout to activate the hooks. Nox uses the committed `.nox/baseline.json` to report known findings and fail on new ones. Warden displays scan coverage warnings; CI checks coverage with `nox ci-gate`. These hooks help contributors catch issues early. Hosted security and validation evidence should come from repository CI checks attached to a commit; Ghostline displays those checks only for the commit they ran against.

Pull requests also run GitHub Actions CI for Go tests/vetting and frontend tests/builds. Actions run with read-only repository permissions and are pinned to commit SHAs.

The `Security` GitHub Actions workflow calls the shared `klarlabs-studio/.github` Nox workflow, pinned to a reviewed commit, against pull request revisions and pushes to `main`. It installs a checksum-verified Nox 1.63.0, scans the repository, annotates findings on pull requests, and uploads SARIF to code scanning on a best-effort basis. `nox ci-gate` then fails the check on a missing report, a required analyzer that did not run, any code-security finding (HARDEN, PERM, CRYPTO, MEMSAFE), net-new critical or high findings above the committed `.nox/baseline.json`, or a baseline that no longer matches any finding. New medium and low findings are reported but do not fail the check. Reports are kept as a `nox-results` workflow artifact for 7 days; they are not attested, so treat them as the run's output rather than signed provenance.

## Docker Compose

```sh
docker compose up --build
```

Open http://localhost:5173 (or set `GHOSTLINE_PORT` if that port is occupied). Compose binds the frontend to all network interfaces by default, so other devices on the LAN can use `http://<host-lan-ip>:5173`; set `GHOSTLINE_BIND_ADDRESS` to a specific interface address to restrict access. The development frontend proxies API requests to the backend container.

## Verify

```sh
go test ./...
cd frontend && npm test && npm run build
```

The frontend is organized by domain, application use cases, and presentation. Zod validates API responses at the boundary.

See [Contributing](CONTRIBUTING.md) for the local workflow and [Security](SECURITY.md) for private vulnerability reporting.
