# Temporary Nox release exception

**Approved:** 2026-11-13 (amended 2026-11-17)
**Review by:** 2026-11-20
**Tracked follow-up:** [Issue #45](https://github.com/felixgeelhaar/ghostline/issues/45)

## Accepted interim risk

Nox reports four medium-severity advisories for OpenSSL 3.5.9 bundled with the frontend's Node runtime: CVE-2026-95691, CVE-2026-95692, CVE-2026-88823, and CVE-2026-88822. The findings identify an affected version, but Nox cannot establish whether Ghostline reaches the vulnerable code paths. This exception records unresolved risk; it does not mean the advisories are fixed or proven unreachable.

The frontend image is pinned to `node:22.23.5-alpine3.24`. Nox scanned that image and still reported OpenSSL 3.5.9. Updating Alpine's system OpenSSL package alone does not replace the OpenSSL bundled in Node.

## Amendment: 2026-11-17

The maintainer explicitly extended this exception after Nox reported three more advisories that have no available fix:

| Fingerprint | Advisory | Location | Status on 2026-11-17 |
|---|---|---|---|
| `6d7de04ef2452a7d66a9403cc17b38d572628df7cfcba7b84d6247e68e253540` | CVE-2026-124709 (high), OpenSSL 3.5.9 | `frontend/Dockerfile` | No supported Node release (22.23.5, 24.22.3, 26.13.1) and no Alpine v3.24 `libssl3` package ships OpenSSL 3.5.10 |
| `95bf7d85016061671297507f50d7548e02acb780b350df0bc615d0767279122e` | CVE-2026-35190 (medium), OpenSSL 3.5.9 | `frontend/Dockerfile` | Same as above |
| `7bfac57a77d7f0d83c2bb31b2c0bbf1b8dde061a0c1b5152318becb5fa1176b5` | GHSA-fc5q-39xc-vjpf (high), picomatch 4.0.7 | `frontend/package-lock.json` | No patched picomatch release; the affected range is `<= 4.0.7` |

The two OpenSSL advisories join the original four under the same boundary and review date. picomatch is a build- and dev-time dependency of vite, vitest, tinyglobby, fdir and @vue/language-core. It matches glob patterns from repository configuration, not request input. Reachability with attacker-controlled patterns is not established. As with the original four, the amendment records accepted risk; it does not mean the advisories are fixed or unreachable.

## Exception boundary

The original four fingerprints and the three added on 2026-11-17 are baselined in `.nox/baseline.json` through 2026-11-20 to unblock the current release. Nox baselines do not expire automatically. The maintainer must review this record and issue #45 by the review date, remove the six OpenSSL baseline entries as soon as a supported runtime with OpenSSL 3.5.10 or later is available, and seek a new explicit decision before extending the exception.

The exception does not cover findings other than the seven listed above. Keep Nox and Warden enabled and preserve the scan output with the release evidence.

## Exit criteria

- A frontend-compatible, supported Node runtime includes OpenSSL 3.5.10 or later, or no longer includes the affected bundled OpenSSL.
- The image digest is pinned and verified.
- A fresh tracked Nox scan no longer reports the six OpenSSL advisories.
- The six OpenSSL entries are removed from `.nox/baseline.json`.
- For picomatch: a patched release is available, the lockfile is updated, a fresh tracked Nox scan no longer reports GHSA-fc5q-39xc-vjpf, and its baseline entry is removed.
