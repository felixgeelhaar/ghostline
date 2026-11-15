# Temporary Nox release exception

**Approved:** 2026-11-13
**Review by:** 2026-11-20
**Tracked follow-up:** [Issue #45](https://github.com/felixgeelhaar/ghostline/issues/45)

## Accepted interim risk

Nox reports four medium-severity advisories for OpenSSL 3.5.9 bundled with the frontend's Node runtime: CVE-2026-95691, CVE-2026-95692, CVE-2026-88823, and CVE-2026-88822. The findings identify an affected version, but Nox cannot establish whether Ghostline reaches the vulnerable code paths. This exception records unresolved risk; it does not mean the advisories are fixed or proven unreachable.

The frontend development image was pinned to `node:22.23.5-alpine3.24` when this exception was created. Nox scanned that image and still reported OpenSSL 3.5.9. Updating Alpine's system OpenSSL package alone does not replace the OpenSSL bundled in Node. Both the frontend development image and production frontend build now use the exact `node:24.22.3-alpine3.23` digest. The newer supported Node LTS release still bundles OpenSSL 3.5.9, so the same exception covers the development image while the production builder findings remain visible. The final production image is built from `scratch` and does not include the Node builder image.

## Observed Alpine zlib findings

Nox reports zlib 1.3.2-r1 from the pinned Alpine 3.23 `FROM` images, even though the Dockerfiles upgrade zlib to 1.4-r0 before project build commands run. The production Go image copies only the built binary, CA certificates, and frontend assets into its `scratch` runtime; the Node production stage is a builder. The frontend development Dockerfile produces a running Alpine image, while the backend development Dockerfile also retains Alpine. These zlib findings are not baselined: they remain visible as scanner evidence about the pinned base images, and the Docker build assertions separately verify the package installed in the resulting images. Do not treat either check as proof that the upstream base-image digest itself is fixed.

## Exception boundary

The four fingerprints in `frontend/Dockerfile` are baselined in `.nox/baseline.json` through 2026-11-20 to unblock the current release. The production `Dockerfile` frontend-build stage reports the same four OpenSSL advisories and remains unbaselined, so those findings stay visible until Nox can assess the final runtime or the Node runtime is updated. Nox baselines do not expire automatically. The maintainer must review this record and issue #45 by the review date, remove any baseline entries as soon as a supported runtime with OpenSSL 3.5.10 or later is available, and seek a new explicit decision before extending the exception.

The exception does not cover new findings or other dependency advisories. Keep Nox and Warden enabled and preserve the scan output with the release evidence.

## Exit criteria

- A frontend-compatible, supported Node runtime includes OpenSSL 3.5.10 or later, or no longer includes the affected bundled OpenSSL.
- The image digest is pinned and verified.
- A fresh tracked Nox scan no longer reports these four advisories.
- The four OpenSSL entries are removed from `.nox/baseline.json`.
