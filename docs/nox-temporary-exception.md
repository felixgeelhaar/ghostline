# Temporary Nox release exception

**Approved:** 2026-11-13
**Review by:** 2026-11-20
**Tracked follow-up:** [Issue #45](https://github.com/felixgeelhaar/ghostline/issues/45)

## Accepted interim risk

Nox reports four medium-severity advisories for OpenSSL 3.5.9 bundled with the frontend's Node runtime: CVE-2026-95691, CVE-2026-95692, CVE-2026-88823, and CVE-2026-88822. The findings identify an affected version, but Nox cannot establish whether Ghostline reaches the vulnerable code paths. This exception records unresolved risk; it does not mean the advisories are fixed or proven unreachable.

The frontend development image was pinned to `node:22.23.5-alpine3.24` when this exception was created. Nox scanned that image and still reported OpenSSL 3.5.9. Updating Alpine's system OpenSSL package alone does not replace the OpenSSL bundled in Node. The frontend development image and both production build definitions now use the pinned `node:24.22.3-alpine3.24` digest. This supported Node LTS release still bundles OpenSSL 3.5.9, so the same temporary exception covers the frontend runtime and production build stage. The final production image is built from `scratch` and does not include the Node builder image.

## Alpine zlib finding in builder images

The pinned Alpine 3.24 `FROM` images contain zlib 1.3.2-r1. Each Dockerfile upgrades it to 1.4-r0 and asserts that exact package before running project build or development commands. The production image copies only the built binary, CA certificates, and frontend assets into its `scratch` runtime. The development images retain Alpine with the upgraded package. Nox's Dockerfile source check reports the package from each `FROM` declaration and cannot verify later build layers, so the zlib entries are baselined with this limitation documented. A passing source scan alone is not proof of the package in a built image; the Docker builds must also pass. Keep issue #58 open until Nox can account for the package upgrade or upstream publishes pinned builder images containing fixed zlib.

## Exception boundary

The four fingerprints in `frontend/Dockerfile` and the same four in the production `Dockerfile` Node build stage are baselined in `.nox/baseline.json` through 2026-11-20 to unblock the current release. Nox cannot establish reachability, even though the production Node stage is not shipped in the final image. Nox baselines do not expire automatically. The maintainer must review this record and issue #45 by the review date, remove all eight OpenSSL baseline entries as soon as a supported runtime with OpenSSL 3.5.10 or later is available, and seek a new explicit decision before extending the exception.

The four zlib occurrences across `Dockerfile`, `Dockerfile.dev`, and `frontend/Dockerfile` are also baselined because Nox attributes each to its `FROM` image rather than the asserted post-upgrade package. This is a source-scanner limitation, not an accepted vulnerable runtime package. Remove these entries when Nox can verify the built image contents or when the pinned upstream images contain fixed zlib.

The exception does not cover new findings or other dependency advisories. Keep Nox and Warden enabled and preserve the scan output with the release evidence.

## Exit criteria

- A frontend-compatible, supported Node runtime includes OpenSSL 3.5.10 or later, or no longer includes the affected bundled OpenSSL.
- The image digest is pinned and verified.
- A fresh tracked Nox scan no longer reports these four advisories.
- The four OpenSSL entries are removed from `.nox/baseline.json`.
