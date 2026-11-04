# Ghostline Day 1 delivery evidence

Status: the implementation PR is public; Qodo review and hackathon-channel publication are pending.

## Demonstrated

- Started the Vue frontend and Go/Gin backend with Docker Compose at `http://localhost:49080` (`GHOSTLINE_PORT=49080 docker compose up -d --build`).
- Health endpoint returned `{"status":"ok"}`.
- Imported the real public pull request [gin-gonic/gin#4354](https://github.com/gin-gonic/gin/pull/4354): “refactor(tree): replace string('/') with \"/\" in node.insertChild”. The API returned the repository, PR number, title, closed state, one commit, committed timestamp, and source URL. The timestamp is labeled as a commit timestamp in the UI.
- Ran `go test ./...`, the frontend Vitest suite (4 tests), and the frontend production build successfully.
- Configured Warden in `.warden.yaml`: Go tests run before commits; Go tests, `go vet`, and frontend tests/build run before pushes. Compose health checks gate frontend startup on backend readiness.
- Added `.github/workflows/nox.yml` to scan each pull request revision and `main` pushes, retaining Nox reports as workflow artifacts. Findings are visible without blocking merges; an incomplete scan fails. The first hosted run is pending publication of this workflow.
- A local Warden pre-push run passed `go-test`, `go-vet`, and `frontend-verify`. Local runs are contributor feedback only; they are not hosted, revision-linked evidence for a pull request.
- An earlier local Nox experiment reported 19 findings, then 17 after Compose health checks were added. Advisory lookup was skipped in offline mode. These exploratory results are not presented as hosted scan results or as evidence about a particular PR revision.
- After publishing `main`, GitHub reported 19 Dependabot alerts in Go modules. The implementation PR updates Gin and the affected Go modules above the patched versions; Go tests and `go vet` pass on the updated branch. `npm audit` reports zero vulnerabilities. Recheck the default branch after the PR is reviewed and merged; do not treat the current main-branch alerts as resolved yet.

## Review observation

The useful engineering decision was to keep contributor gates separate from hosted evidence. A local check can help its author, but only a repository CI result associated with the relevant commit can tell other contributors which revision was checked. Ghostline's current Day 1 UI does not yet import GitHub check runs.

## Still required for the hackathon submission

- Run and record a genuine Qodo review of that PR, including one concrete observation about its feedback.
- Publish the hackathon introduction and this delivery evidence to the required hackathon destination.

No Qodo review or public submission is claimed by this document yet.
