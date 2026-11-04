# Contributing

Ghostline is an early-stage Go and Vue application. Keep changes small, preserve source-backed evidence, and avoid describing inferred relationships as facts.

## Development setup

- Go 1.27.3 or newer
- Node.js 22.12 or newer
- Docker Compose (optional)

Install the frontend dependencies and run the checks before opening a pull request:

```sh
cd frontend
npm ci
npm test
npm run build
cd ..
go test ./...
go vet ./...
```

Warden hooks run Go tests and a tracked-repository Nox scan before commits, then the full verification set and another Nox scan before pushes. Pull requests run the same checks in GitHub Actions, plus the Nox repository scan.

## Structure and tests

Keep business rules in `frontend/src/domain`, orchestration in `frontend/src/application`, and UI code in `frontend/src/presentation`. Add or update focused tests with behavior changes. Keep network and GitHub API details outside the domain model where practical.

Open pull requests against `main` and describe behavior, validation performed, and any known limitations. Do not include secrets, local environment files, or generated scan reports in commits.
