# Ghostline

**See the work. Trace the decisions. Take control.**

[![CI](https://github.com/felixgeelhaar/ghostline/actions/workflows/ci.yml/badge.svg)](https://github.com/felixgeelhaar/ghostline/actions/workflows/ci.yml) [![Security scan](https://github.com/felixgeelhaar/ghostline/actions/workflows/nox.yml/badge.svg)](https://github.com/felixgeelhaar/ghostline/actions/workflows/nox.yml)

Ghostline is a change-centric engineering control room. The first slice imports a public GitHub pull request and shows its commit timeline, with commit timestamps clearly distinguished from push or pull-request event chronology. Checks belong in repository CI, where their results can be tied to the revision they actually scanned; Ghostline does not scan a developer's local filesystem.

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

This repository's `.warden.yaml` configures Warden developer gates: Go tests and a tracked-repository Nox scan before commits, then Go tests, `go vet`, frontend tests/build, and another Nox scan before pushes. The Warden steps call Nox directly. Install Warden (for example, `go install go.klarlabs.de/warden@v0.46.1`) and Nox 1.57.0 from the [official release](https://github.com/Nox-HQ/nox/releases/tag/v1.57.0), placing its `nox` binary on `PATH`. Then run `warden init --hooks=pre-commit,pre-push` in a fresh checkout to activate the hooks. Nox uses the committed `.nox/baseline.json` to report known findings and fail on new ones. Warden displays scan coverage warnings; CI enforces the documented coverage-gap policy. These hooks help contributors catch issues early. Hosted security and validation evidence should come from repository CI checks attached to a commit; the Day 1 Ghostline UI does not yet import or display those check runs.

Pull requests also run GitHub Actions CI for Go tests/vetting and frontend tests/builds. Actions run with read-only repository permissions and are pinned to commit SHAs.

The `Security scan` GitHub Actions workflow runs the pinned Nox marketplace action against pull request revisions and pushes to `main`, annotates findings on the check, posts inline finding comments and a summary issue comment on same-repository pull requests, attests and retains generated reports with the workflow run, and fails on findings outside the committed Nox baseline. Fork pull requests remain read-only and show findings in their check output and report artifact. Incomplete scans fail except for Nox's currently unsupported OSV advisory lookup for Docker base images; that specific coverage gap remains visible in the report and workflow summary.

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
