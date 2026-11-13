# Temporary Nox release exception

**Approved:** 2026-11-13
**Review by:** 2026-11-20
**Tracked follow-up:** [Issue #45](https://github.com/felixgeelhaar/ghostline/issues/45)

## Accepted interim risk

Nox reports four medium-severity advisories for OpenSSL 3.5.9 bundled with the frontend's Node runtime: CVE-2026-95691, CVE-2026-95692, CVE-2026-88823, and CVE-2026-88822. The findings identify an affected version, but Nox cannot establish whether Ghostline reaches the vulnerable code paths. This exception records unresolved risk; it does not mean the advisories are fixed or proven unreachable.

The frontend image is pinned to `node:22.23.5-alpine3.24`. Nox scanned that image and still reported OpenSSL 3.5.9. Updating Alpine's system OpenSSL package alone does not replace the OpenSSL bundled in Node.

## Exception boundary

The four fingerprints are baselined in `.nox/baseline.json` through 2026-11-20 to unblock the current release. Nox baselines do not expire automatically. The maintainer must review this record and issue #45 by the review date, remove the four baseline entries as soon as a supported runtime with OpenSSL 3.5.10 or later is available, and seek a new explicit decision before extending the exception.

The exception does not cover new findings or other dependency advisories. Keep Nox and Warden enabled and preserve the scan output with the release evidence.

## Exit criteria

- A frontend-compatible, supported Node runtime includes OpenSSL 3.5.10 or later, or no longer includes the affected bundled OpenSSL.
- The image digest is pinned and verified.
- A fresh tracked Nox scan no longer reports these four advisories.
- The four OpenSSL entries are removed from `.nox/baseline.json`.
