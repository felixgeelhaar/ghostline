# Ghostline Agent Instructions

## Project

Ghostline is a change-centric engineering control room. The current slice imports a public GitHub pull request and shows its metadata and commits in a Vue timeline, backed by a small Go/Gin API. The timeline is based on commit data; it is not a complete pull-request event history. Keep evidence tied to the source and revision that actually produced it.

## Setup and verification

- Run locally: `go run ./backend`; in another terminal run `cd frontend && npm ci && npm run dev`.
- Run with Docker Compose: `docker compose up --build`.
- Verify Go: `go test ./... -count=1` and `go vet ./...`.
- Verify frontend: `cd frontend && npm ci && npm test && npm run build`.
- Run the repository security scan: `nox --format all --output .nox/results scan . --tracked-only`.
- The configured Warden pre-commit and pre-push gates run these checks; follow `.warden.yaml` when changing the gate.

## Project rules

1. **A timestamp is not a cause.** Applies to timeline labels, sorting, summaries, and documentation. Commit authored/committed times do not prove when a change was pushed, reviewed, or repaired; do not imply a causal relationship from ordering alone.
2. **Evidence must name its revision and source.** Applies to GitHub checks, review findings, security reports, and UI claims. A check or finding only describes the revision its source actually inspected; never present stale evidence as current.
3. **Keep GitHub as the authority for pull-request facts.** Applies to importing and displaying PR metadata and commits. Preserve native identifiers and source links, validate external responses at the API boundary, and show errors instead of inventing or silently substituting data.
4. **Keep the first slice small and layered.** Applies to backend and frontend changes. Put business concepts in domain code, orchestration in application code, and HTTP/UI details at the edges; avoid generalized adapters, workflow engines, and integrations without a concrete user need.
5. **Keep review and validation tools in their lane.** Applies to Qodo, Nox, Warden, and CI integration. Qodo provides review feedback; Nox reports security findings; Warden runs local developer gates; CI records hosted revision-linked checks. Do not present a tool's result as an independent correctness guarantee or suppress findings without a documented baseline rationale.
