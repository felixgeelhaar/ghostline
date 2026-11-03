# Ghostline Day 1 delivery evidence

Status: implementation demo is ready; public submission and Qodo review evidence are pending.

## Demonstrated

- Started the Vue frontend and Go/Gin backend with Docker Compose at `http://localhost:49080` (`GHOSTLINE_PORT=49080 docker compose up -d --build`).
- Health endpoint returned `{"status":"ok"}`.
- Imported the real public pull request [gin-gonic/gin#4354](https://github.com/gin-gonic/gin/pull/4354): “refactor(tree): replace string('/') with \"/\" in node.insertChild”. The API returned the repository, PR number, title, closed state, one commit, committed timestamp, and source URL. The timestamp is labeled as a commit timestamp in the UI.
- Configured Warden in `.warden.yaml`: Go tests run before commits; Go tests, `go vet`, and frontend tests/build run before pushes. The Compose image includes Warden v0.46.1 and Nox v1.57.0, each fetched from a pinned release and checksum-verified.
- Ran the configured Warden pre-push gate in Compose. `go-test`, `go-vet`, and `frontend-verify` passed. Warden wrote local provenance but could not publish it because this checkout has no `origin`; the app reports `passed_unpublished` to preserve that distinction.
- Ran Nox offline against the repository. It emitted 19 findings. Its report explicitly says advisory lookup was skipped offline, so this run does not establish that dependencies have no known advisories. Findings are inspectable in the scan result and remain distinct from PR/revision evidence.

## Review observation

The useful engineering decision from this first scan was to treat validation and provenance publication as separate facts. A successful Warden gate is evidence that its configured checks passed locally, but without a remote the provenance note is not visible to other contributors. Ghostline exposes this as `passed_unpublished` rather than a plain success.

## Still required for the hackathon submission

- Publish the repository and focused implementation PR to the selected GitHub owner.
- Run and record a genuine Qodo review of that PR, including one concrete observation about its feedback.
- Publish the hackathon introduction and this delivery evidence at the required destination.

No Qodo review or public submission is claimed by this document yet.
