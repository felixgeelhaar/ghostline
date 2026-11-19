# Nox report feature evidence

Captured on 2026-11-19 from the running Ghostline app (Go backend and Vue dev server from the PR 65 branch) with headless Chrome. The app imported public pull request felixgeelhaar/ghostline#65 live from GitHub and loaded the Nox report for PR head `825d9ed`. The backend used a GitHub token from its environment; it checked that the repository is public before it downloaded the Security workflow artifact.

What the screenshot shows:

- The report is bound to revision `825d9ed`, the head GitHub reported at fetch time. It names the source workflow (Security), its outcome (success), and the artifact's creation and expiry times.
- The 226 active results are the unsuppressed results in the artifact's SARIF file:
  - 224 are SARIF `note` results: dependency notes that Nox recorded at info level for the container base images. Ghostline shows SARIF `note` as low severity, and each title still says "reported at info".
  - 2 are SARIF `warning` results, shown as medium. They are ALPINE-CVE-2026-88822 in OpenSSL for `Dockerfile` and `Dockerfile.dev`, tracked in issues #80 and #81.
- The screenshot shows only the top of the finding list.

What the screenshot does not show:

- It does not show that the findings are valid, that the scan was complete, or that the revision is safe. The report describes only the inspected revision and the artifact it came from.
- It shows one revision. A later commit on the PR is not covered by this report.
- The run behind this report predates #82. At that time the Security workflow checked out GitHub's merge preview of `825d9ed` with `main`, not `825d9ed` itself, so the scanned tree may differ from the revision the panel names. The panel's layout and labels are what this screenshot shows. Since #82, the workflow scans the PR head commit.
