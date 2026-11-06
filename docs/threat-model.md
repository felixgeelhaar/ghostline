# Ghostline threat model

**Scope:** the current public pull-request viewer and its Docker Compose demonstration stack. This is a snapshot of the code and deployment configuration, not a claim that Ghostline is ready for public production hosting. Review it when authentication, private-repository support, agent control, or deployment architecture changes.

## System and trust boundaries

Ghostline accepts a GitHub pull-request URL in an unauthenticated browser, sends it to an unauthenticated Go API, and fetches pull-request data, commits, checks, and published Qodo comments from the fixed `https://api.github.com` origin. The API uses an optional `GITHUB_TOKEN` when configured. The Vue application validates API responses with Zod. No database or agent execution is part of this slice.

Trust boundaries:

1. **Browser to Ghostline API:** requests and submitted URLs are user-controlled. There is no product login, authorization, or application-level rate limit.
2. **Ghostline API to GitHub:** GitHub is the source for displayed facts. Responses, including repository-authored text and third-party check metadata, remain untrusted input. A configured token is a server-side credential.
3. **Build and deployment pipeline:** source, dependencies, container images, GitHub Actions, and deployment configuration determine what code and privileges reach users.
4. **Engineer to coding agent:** Deep Trace can produce copyable context. It does not execute an agent, and copied text may still influence the engineer’s separate agent session.

## Threats and current treatment

| Threat | Current controls | Remaining exposure |
| --- | --- | --- |
| **Public API abuse or GitHub quota exhaustion** | GitHub calls have a 12-second timeout. URL input is limited to HTTPS `github.com` pull-request paths. | API endpoints are unauthenticated and have no per-client rate limiting. An internet-facing instance can be used to consume GitHub’s public quota or a configured token’s quota. Add deployment-level access control and rate limits before public hosting. |
| **Private repository data exposed through a server token** | The current Compose stack does not pass `GITHUB_TOKEN` to the backend; the product scope is public PRs. | If an operator configures a token that can read private repositories, unauthenticated callers may cause the API to fetch data using that token. Do not deploy a privileged token with this public API. Private-repository support requires caller identity, repository authorization, and explicit token scoping first. |
| **Server-side request forgery** | Input must use HTTPS, the exact `github.com` host, and a PR path; outbound requests use the fixed GitHub API origin and a bounded timeout. | Keep outbound destinations fixed if integrations are added. Do not turn imported URLs or returned links into server-side fetch targets. |
| **Misleading or hostile imported content** | GitHub remains the source of truth; response schemas are checked at the frontend boundary, Qodo content is parsed into text, Vue interpolation escapes text, and external check links are restricted to HTTP(S). Check results carry their revision SHA. | PR titles, commit messages, review findings, check names, and links can still mislead users. Treat them as untrusted display data. A requested SHA is validated as a full SHA, but the checks endpoint does not independently prove that every requested SHA belongs to the supplied PR; preserve that limitation in claims and UI. |
| **Stale or overstated verification evidence** | Import rechecks the PR head after fetching commits and checks; check responses are verified against the requested SHA and labeled with that revision. | A successful check only records what GitHub reported for that revision. It does not establish correctness or validate later commits. Never infer a repair or causal relationship from timeline order. |
| **Prompt injection through review content** | Deep Trace labels imported findings as untrusted evidence and instructs a copied prompt not to follow instructions embedded in findings. Ghostline does not execute the prompt or control an agent. | An engineer may still paste hostile content into an agent. Keep source attribution and the untrusted-content boundary visible; do not automatically forward findings to agents without a separate threat review. |
| **Compromised build dependency or workflow** | CI actions and container base images are pinned to digests/SHAs. Build/test CI uses read-only repository permissions. The Nox workflow has explicit reporting permissions, and GitHub downgrades tokens for fork pull requests; PR comments are enabled only for same-repository pull requests. Nox scans repository dependencies and reports findings. | Scanning is not prevention or runtime monitoring. The Nox workflow needs write access to publish findings, so keep that workflow and its pinned action under review and scope permissions to reporting needs. Treat scan coverage gaps as unresolved evidence rather than a clean bill of health. |
| **Unintended network exposure of the demo** | `GHOSTLINE_BIND_ADDRESS` can restrict the published frontend bind address; the backend port is not published by Compose. Containers run as non-root users. | Compose defaults the frontend to `0.0.0.0` and uses Vite’s development server. This stack is for local/LAN demonstration, not direct public hosting. Use a production frontend server and add authentication, rate limiting, TLS termination, and operational monitoring before exposing a hosted instance. |

## Hosting boundary

Until access control and abuse protections exist, run the Compose stack only in a trusted local or LAN environment and restrict its bind address when needed. Do not supply a GitHub token with private-repository access to an instance reachable by untrusted users. A hosted deployment must replace the development server and establish who may use the API, what repositories they may query, how requests are limited, and how credentials are stored and rotated.

Warden runs local contributor gates; Nox scans repository artifacts; GitHub Actions records hosted checks. These controls help assess the source and build. They do not secure a running Ghostline service or prove that imported code is safe.

## Review triggers

Reassess this model before adding private repositories, user accounts, write access to GitHub, agent execution or takeover, persistent storage, additional outbound integrations, or an internet-facing deployment. Record the changed trust boundary, the new abuse cases, and the evidence used to verify mitigations.
