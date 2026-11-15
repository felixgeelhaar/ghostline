# Temporary Nox release exception

**Approved:** 2026-11-13
**Review by:** 2026-11-20
**Tracked follow-up:** [Issue #45](https://github.com/felixgeelhaar/ghostline/issues/45)

## Accepted interim risk

Nox reports four medium-severity advisories for OpenSSL 3.5.9 bundled with the frontend's Node runtime: CVE-2026-95691, CVE-2026-95692, CVE-2026-88823, and CVE-2026-88822. The findings identify an affected version, but Nox cannot establish whether Ghostline reaches the vulnerable code paths. This exception records unresolved risk; it does not mean the advisories are fixed or proven unreachable.

The frontend development image was pinned to `node:22.23.5-alpine3.24` when this exception was created. Nox scanned that image and still reported OpenSSL 3.5.9. Updating the operating-system OpenSSL package alone does not replace the OpenSSL bundled in Node. The frontend development image and production build now use the pinned `node:26.13.1-bookworm-slim` image. This is the current Node LTS line. Whether it resolves the OpenSSL advisories is not yet established; keep the exception until a fresh tracked Nox scan confirms the findings are gone. The final production image is built from `scratch` and does not include the Node builder image. The production builder's OpenSSL findings remain visible in Nox and are not covered by this exception.

## Base image change for the zlib alert

The Alpine builder images were replaced with pinned Debian Bookworm images after Nox reported a zlib alert against their base-image metadata. The referenced CVE is in GDAL's embedded zlib copy, rather than the system zlib library; the Debian Security Tracker identifies the affected component and package context ([CVE-2026-4738](https://security-tracker.debian.org/tracker/CVE-2026-4738)). A tracked Nox scan of the pinned Debian images no longer reported the zlib alert. This change removes the zlib baselines instead of treating a source-scanner limitation as accepted risk.

## Exception boundary

The four OpenSSL fingerprints in `frontend/Dockerfile` are baselined in `.nox/baseline.json` through 2026-11-20 to unblock the current release. Nox baselines do not expire automatically. The maintainer must review this record and issue #45 by the review date, remove all four entries as soon as a supported runtime with OpenSSL 3.5.10 or later is available, and seek a new explicit decision before extending the exception. OpenSSL findings in the production builder are not baselined.

The exception does not cover new findings or other dependency advisories. Keep Nox and Warden enabled and preserve the scan output with the release evidence.

## Exit criteria

- A frontend-compatible, supported Node runtime includes OpenSSL 3.5.10 or later, or no longer includes the affected bundled OpenSSL.
- The image digest is pinned and verified.
- A fresh tracked Nox scan no longer reports these four advisories.
- The four OpenSSL entries are removed from `.nox/baseline.json`.
