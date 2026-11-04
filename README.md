# Ghostline

**See the work. Trace the decisions. Take control.**

Ghostline is a change-centric engineering control room. The first slice imports a public GitHub pull request and shows its commit timeline, with commit timestamps clearly distinguished from push or pull-request event chronology. Checks belong in repository CI, where their results can be tied to the revision they actually scanned; Ghostline does not scan a developer's local filesystem.

## Run locally

Requirements: Go 1.26+ and Node.js 20+.

```sh
go run ./backend
```

In another terminal:

```sh
cd frontend
npm install
npm run dev
```

Open the URL printed by Vite. Public GitHub API requests work without a token; set `GITHUB_TOKEN` to increase the unauthenticated rate limit.

## Repository checks

This repository's `.warden.yaml` configures Warden developer gates: Go tests before commits, then Go tests, `go vet`, and the frontend test/build before pushes. These hooks help contributors catch issues early. Hosted security and validation evidence should come from repository CI checks attached to a commit; the Day 1 Ghostline UI does not yet import or display those check runs.

The `Security scan` GitHub Actions workflow runs Nox against pull request revisions, pushes to `main`, and manual dispatches. It verifies the pinned scanner archive and retains generated reports with the workflow run. Findings are reported but do not currently fail the check; scanner execution errors do.

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
