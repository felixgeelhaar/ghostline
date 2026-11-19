# Temporary Nox release exception

**Approved:** 2026-11-13 (amended 2026-11-17)
**Review by:** 2026-11-20
**Tracked follow-up:** [Issue #45](https://github.com/felixgeelhaar/ghostline/issues/45)

## Accepted interim risk

Nox reports four medium-severity advisories for OpenSSL 3.5.9 bundled with the frontend's Node runtime: CVE-2026-95691, CVE-2026-95692, CVE-2026-88823, and CVE-2026-88822. The findings identify an affected version, but Nox cannot establish whether Ghostline reaches the vulnerable code paths. This exception records unresolved risk; it does not mean the advisories are fixed or proven unreachable.

The frontend development image was pinned to `node:22.23.5-alpine3.24` when this exception was created. Nox scanned that image and still reported OpenSSL 3.5.9. Updating the operating-system OpenSSL package alone does not replace the OpenSSL bundled in Node. The frontend development image and production build now use the pinned `node:26.13.1-bookworm-slim` digest, the current Node LTS line. A fresh tracked Nox scan must establish whether this runtime clears the OpenSSL advisories; retain the exception until the scan confirms that it does. The final production image is built from `scratch` and does not include the Node builder image. The production builder's OpenSSL findings remain visible in Nox and are not covered by this exception.

## Go builder image and the zlib alert

The Node images moved to the pinned `node:26.13.1-bookworm-slim` digest, and a tracked Nox scan no longer reports the Alpine zlib alert for them. The Go images stay on the pinned `golang:1.27.4-alpine` digest. A Debian `golang:1.27.4-bookworm` builder was tried, but a tracked Nox scan with vulnerability lookups available reported 229 critical or high Debian package advisories in it, against none for the Alpine image.

Nox still reports ALPINE-CVE-2026-4738 (medium, zlib 1.3.2-r1) for the Alpine Go image in `Dockerfile` and `Dockerfile.dev`. In `Dockerfile` it is the build stage only: the statically linked binary is copied into a `scratch` image that contains no zlib. `Dockerfile.dev` runs from that image, so it installs `zlib>=1.4-r0` and fails the build if the fixed package is unavailable; Nox inventories the base image, not the built one. The maintainer decided on PR #62 (2026-11-18) to baseline both findings in `.nox/baseline.json`: the `Dockerfile` entry keeps the fingerprint already approved on `main`, and `Dockerfile.dev` gets its own. Remove both when the pinned Go Alpine digest ships zlib 1.4-r0 or later. No supported `golang:1.27.4-alpine` digest did on 2026-11-18.

## Amendment: 2026-11-17

The maintainer explicitly extended this exception ([decision](https://github.com/felixgeelhaar/ghostline/issues/45#issuecomment-6960376515)) after Nox reported three more advisories that have no available fix:

| Fingerprint | Advisory | Location | Status on 2026-11-17 |
|---|---|---|---|
| `6d7de04ef2452a7d66a9403cc17b38d572628df7cfcba7b84d6247e68e253540` | CVE-2026-124709 (high), OpenSSL 3.5.9 | `frontend/Dockerfile` | No supported Node release (22.23.5, 24.22.3, 26.13.1) and no Alpine v3.24 `libssl3` package ships OpenSSL 3.5.10 |
| `95bf7d85016061671297507f50d7548e02acb780b350df0bc615d0767279122e` | CVE-2026-35190 (medium), OpenSSL 3.5.9 | `frontend/Dockerfile` | Same as above |
| `7bfac57a77d7f0d83c2bb31b2c0bbf1b8dde061a0c1b5152318becb5fa1176b5` | GHSA-fc5q-39xc-vjpf (high), picomatch 4.0.7 | `frontend/package-lock.json` | No patched picomatch release; the affected range is `<= 4.0.7` |

The two OpenSSL advisories join the original four under the same boundary and review date. picomatch is a build- and dev-time dependency of vite, vitest, tinyglobby, fdir and @vue/language-core. It matches glob patterns from repository configuration, not request input. Reachability with attacker-controlled patterns is not established. As with the original four, the amendment records accepted risk; it does not mean the advisories are fixed or unreachable.

## Amendment: 2026-11-18, Alpine OpenSSL in the Go image

Since the evening of 2026-11-18, Nox reports four high Alpine advisories for OpenSSL 3.5.9-r0 in the pinned `golang:1.27.4-alpine` image: ALPINE-CVE-2026-95691, ALPINE-CVE-2026-95692, ALPINE-CVE-2026-88823 and ALPINE-CVE-2026-107496. Each appears in `Dockerfile` and `Dockerfile.dev`, so there are 8 findings. Alpine v3.24 ships the fix in 3.5.9-r1, but no `golang` Alpine digest includes it yet.

In `Dockerfile` the image is the backend build stage only: the `CGO_ENABLED=0` binary is copied into a `scratch` image that contains no OpenSSL. `Dockerfile.dev` runs from the image, so it installs `libssl3` and `libcrypto3` 3.5.9-r1 or later and fails the build if they are unavailable. Nox inventories the base image, not the built one, so the findings remain.

The maintainer decided to baseline the 8 fingerprints in `.nox/baseline.json`, tracked in [issue #78](https://github.com/felixgeelhaar/ghostline/issues/78), with a review date of 2026-11-25. Remove them when the pinned Go Alpine digest ships OpenSSL 3.5.9-r1 or later. This records accepted risk; it does not mean the advisories are fixed in the base image.

## Exception boundary

The original four OpenSSL fingerprints and the three added on 2026-11-17 are baselined in `.nox/baseline.json` through 2026-11-20 to unblock the current release. Nox baselines do not expire automatically. The maintainer must review this record and issue #45 by the review date, remove the six OpenSSL baseline entries as soon as a supported runtime with OpenSSL 3.5.10 or later is available, and seek a new explicit decision before extending the exception. OpenSSL findings in the production builder are not baselined.

The exception does not cover findings other than the seven listed above; the two zlib entries and the eight Alpine OpenSSL entries are recorded in their own sections. Keep Nox and Warden enabled and preserve the scan output with the release evidence.

## Exit criteria

- A frontend-compatible, supported Node runtime includes OpenSSL 3.5.10 or later, or no longer includes the affected bundled OpenSSL.
- The image digest is pinned and verified.
- A fresh tracked Nox scan no longer reports the six OpenSSL advisories.
- The six OpenSSL entries are removed from `.nox/baseline.json`.
- For picomatch: a patched release is available, the lockfile is updated, a fresh tracked Nox scan no longer reports GHSA-fc5q-39xc-vjpf, and its baseline entry is removed.
