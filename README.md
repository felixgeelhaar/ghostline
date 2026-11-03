# Ghostline

**See the work. Trace the decisions. Take control.**

Ghostline is a change-centric engineering control room. The first slice imports a public GitHub pull request and shows its commit timeline, with commit timestamps clearly distinguished from push or pull-request event chronology. Optional local Nox and Warden scans are shown as separate repository evidence.

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

## Repository scans

Enter the absolute path to a local checkout in the app. Nox is called as `nox scan <path> --format json --output <temporary-directory> --offline`; its report is read and the temporary output is removed. Warden runs `warden run pre-push` in that repository, using its `.warden.yaml` gate configuration. Set `WARDEN_COMMAND` to select the binary or `WARDEN_HOOK=pre-commit` to run the faster gate. Scanners are never represented as having run when the executable is missing. These local results are not linked to a PR revision.

This repository’s `.warden.yaml` runs Go tests at pre-commit and runs Go tests, `go vet`, and the frontend test/build at pre-push. To install native hooks after the repository has its first commit, run `warden init --hooks=pre-commit,pre-push`.

## Docker Compose

```sh
docker compose up --build
```

Open http://localhost:5173 (or set `GHOSTLINE_PORT` if that port is occupied). The development frontend proxies API requests to the backend container. The backend image includes Nox v1.57.0 and Warden v0.46.1; both Linux release archives are checksum verified during build. By default, Compose mounts this checkout at `/scan-target`. Enter `/scan-target` in the app to run Nox and the Warden pre-push gate against it. To inspect another local checkout, set `SCAN_REPOSITORY_PATH` to its absolute host path before starting Compose. Warden executes the commands configured in the target repository’s `.warden.yaml`.

## Verify

```sh
go test ./...
cd frontend && npm test && npm run build
```

The frontend is organized by domain, application use cases, and presentation. Zod validates API responses at the boundary.
