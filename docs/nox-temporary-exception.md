# Temporary Nox release exception

**Approved:** 2026-11-13 (amended 2026-11-17 and 2026-11-24)
**Review by:** 2026-12-01 for the Node OpenSSL entries (see the 2026-11-24 amendment)
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

## Amendment: 2026-11-24, Node OpenSSL renewed

The review date of 2026-11-20 passed with the exit criteria unmet: `node:22-bookworm-slim` (22.23.5), `node:24-bookworm-slim` (24.22.3) and `node:26-bookworm-slim` (26.13.1) all still report `process.versions.openssl` = 3.5.9 on 2026-11-24. The same day Nox reported three more OpenSSL 3.5.9 advisories for `frontend/Dockerfile`. The maintainer renewed the Node OpenSSL exception through **2026-12-01** and extended it to them ([record](https://github.com/felixgeelhaar/ghostline/issues/45#issuecomment-7140671045)):

| Fingerprint | Advisory | Location |
|---|---|---|
| `40dc13192ffa4feaa2b86d279ea3f4b468fe018fd88adbdbb1220ead3eca5ce6` | CVE-2026-121482 (high), OpenSSL 3.5.9 | `frontend/Dockerfile` |
| `e939dc4b185eb989a8d591dc046b0d26101984fecfcba91f2b832cd7e1b295d1` | CVE-2026-107494 (high), OpenSSL 3.5.9 | `frontend/Dockerfile` |
| `1ffe1dd3fb73e19929b154bd31d668c919dbf61848f01bbfb6e987b6915dbe83` | CVE-2026-84785 (medium), OpenSSL 3.5.9, added later the same day ([#145](https://github.com/felixgeelhaar/ghostline/issues/145)) | `frontend/Dockerfile` |

The six earlier Node OpenSSL entries carry the renewal in their baseline reasons. In the production `Dockerfile`, Node runs only in the `frontend-build` stage and the runtime image is `FROM scratch`; `frontend/Dockerfile` is the local Compose development image. Reachability is undetermined. This renewal does not cover the picomatch entry from 2026-11-17, whose review date also passed; that needs its own decision.

## Exception boundary

The original four OpenSSL fingerprints and the three added on 2026-11-17 are baselined in `.nox/baseline.json`; the OpenSSL entries, with the three added on 2026-11-24, are renewed through 2026-12-01. Nox baselines do not expire automatically. The maintainer must review this record and issue #45 by the review date, remove the nine Node OpenSSL baseline entries as soon as a supported runtime with OpenSSL 3.5.10 or later is available, and seek a new explicit decision before extending the exception. OpenSSL findings in the production builder are not baselined.

The exception does not cover findings other than the ten listed above (nine OpenSSL, one picomatch); the two zlib entries and the eight Alpine OpenSSL entries are recorded in their own sections. Keep Nox and Warden enabled and preserve the scan output with the release evidence.

## Exit criteria

- A frontend-compatible, supported Node runtime includes OpenSSL 3.5.10 or later, or no longer includes the affected bundled OpenSSL.
- The image digest is pinned and verified.
- A fresh tracked Nox scan no longer reports the nine OpenSSL advisories.
- The nine OpenSSL entries are removed from `.nox/baseline.json`.
- For picomatch: a patched release is available, the lockfile is updated, a fresh tracked Nox scan no longer reports GHSA-fc5q-39xc-vjpf, and its baseline entry is removed.
