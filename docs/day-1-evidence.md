# Ghostline Day 1 delivery evidence

Status: the implementation PR is public; Qodo review and hackathon-channel publication are pending.

## Demonstrated

- Started the Vue frontend and Go/Gin backend with Docker Compose at `http://localhost:49080` (`GHOSTLINE_PORT=49080 docker compose up -d --build`).
- Health endpoint returned `{"status":"ok"}`.
- Imported the real public pull request [gin-gonic/gin#4354](https://github.com/gin-gonic/gin/pull/4354): “refactor(tree): replace string('/') with \"/\" in node.insertChild”. The API returned the repository, PR number, title, closed state, one commit, committed timestamp, and source URL. The timestamp is labeled as a commit timestamp in the UI.
- Ran `go test ./...`, the frontend Vitest suite (4 tests), and the frontend production build successfully.
- Configured Warden in `.warden.yaml`: Go tests run before commits; Go tests, `go vet`, and frontend tests/build run before pushes. The Compose image includes Warden v0.46.1 and Nox v1.57.0, each fetched from a pinned release and checksum-verified. Compose health checks gate frontend startup on backend readiness.
- Ran the configured Warden pre-push gate in Compose. `go-test`, `go-vet`, and `frontend-verify` passed. Before the GitHub repository existed, the local scan correctly reported `passed_unpublished`. After creating the public repository, Warden passed and published provenance for both `main` and `feat/day-1-nox-status` pushes.
- The first Nox run reported 19 findings, including missing Compose service health checks. After adding health checks and making frontend startup wait for the backend, a fresh run reported 17 findings. The API reports `findings` and the count rather than conflating findings with a scanner error. Nox's report says advisory lookup was skipped offline, so this run does not establish that dependencies have no known advisories. Findings are inspectable in the scan result and remain distinct from PR/revision evidence.
- After publishing `main`, GitHub reported 19 Dependabot alerts in Go modules. The implementation PR updates Gin and the affected Go modules above the patched versions; Go tests and `go vet` pass on the updated branch. `npm audit` reports zero vulnerabilities. Recheck the default branch after the PR is reviewed and merged; do not treat the current main-branch alerts as resolved yet.

## Review observation

The useful engineering decision from this first scan was to treat validation and provenance publication as separate facts. A successful Warden gate is evidence that its configured checks passed locally, but without a remote the provenance note is not visible to other contributors. Ghostline exposes this as `passed_unpublished` rather than a plain success.

## Still required for the hackathon submission

- Run and record a genuine Qodo review of that PR, including one concrete observation about its feedback.
- Publish the hackathon introduction and this delivery evidence to the required hackathon destination.

No Qodo review or public submission is claimed by this document yet.
