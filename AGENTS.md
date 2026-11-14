# Ghostline Agent Instructions

## Project

Ghostline is a change-centric engineering control room. It imports public GitHub pull requests and displays their metadata, commits, and source-linked Qodo review feedback in a Vue timeline backed by a small Go/Gin API. Review findings retain the source comment and inspected revision where available; later PR commits are only candidate context, not proof of repairs. The timeline is based on commit data, not a complete pull-request event history. Keep evidence tied to the source and revision that actually produced it.

## Setup and verification

- Run locally: `go run ./backend`; in another terminal run `cd frontend && npm ci && npm run dev`.
- Run with Docker Compose only after configuring the required hosted settings. Follow [the README's Compose setup](README.md#docker-compose-hosted-style-run), including GitHub OAuth credentials, an allowed GitHub login, a session signing key, and the proxy client-IP header plus exact trusted-proxy CIDR; then run `docker compose up --build`.
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
6. **Show the working software before release.** For each release, run the app and include screenshot evidence of the working software in the release notes or delivery. Capture the actual release candidate so users can see what they are getting.
7. **One day, one pull request.** Keep each day's hackathon work in its own focused pull request. Applies to the daily build and delivery workflow; separate PRs make each day's scope, review, checks, and evidence easy to follow.
8. **Bind async results to the item that asked for them.** Applies to frontend actions that fetch, copy, or report something for a displayed pull request or revision. Take the repository and revision from the imported pull request or the clicked item, never from editable input such as the import field. Apply a result only while its request is still the latest for that view (for example, a request counter checked after `await`), and test the case where an earlier request settles last. Otherwise an edited field or a quick second click shows one item's data under another.
