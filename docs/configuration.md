# Configuration

Moved from the README. Ghostline's backend reads these settings from the environment.

## GitHub token

Public GitHub API requests work without a token. If `GITHUB_TOKEN` is configured, Ghostline uses it only to check repository visibility; pull-request, commit, check, and annotation data are fetched without credentials. The token does not raise the quota for those public-data requests. The one exception is a Nox report: GitHub requires authentication to download an Actions artifact even from a public repository, so after the visibility check confirms the repository is public, Ghostline downloads the `nox-results` artifact with the token. That token needs read access to Actions artifacts. Without it, Ghostline reports the Nox evidence as unavailable rather than clean.

## Request rate limit

`POST /api/import`, `POST /api/checks`, `POST /api/check-annotations`, `POST /api/nox-report`, and `POST /api/qodo-review` share one per-client budget so a single caller cannot use up the GitHub API quota for everyone else. The limiter is in-memory and fixed-window: the window starts at a client's first request, and the count and any recorded clients are lost when the backend restarts or if you run more than one instance. `POST /api/pr-search` also counts against this budget and is further limited to 14 searches per one-minute window per GitHub account, so even bursts on either side of a window boundary stay below GitHub's search limit of 30 a minute. A request over the limit receives HTTP 429, a `Retry-After` header in seconds, and a JSON `{"error": "..."}` body; the UI shows when to retry. `GET /api/health` is not limited.

| Variable | Default | Meaning |
| --- | --- | --- |
| `RATE_LIMIT_REQUESTS` | `30` | Requests allowed per client per window (positive integer). |
| `RATE_LIMIT_WINDOW` | `1m` | Window length as a Go duration, such as `30s` or `5m`. |
| `RATE_LIMIT_CLIENT_IP_HEADER` | unset | Header carrying exactly one client IP, set by a proxy you operate. |
| `RATE_LIMIT_TRUSTED_PROXIES` | unset | Comma-separated CIDR ranges of proxies allowed to set that header. Required together with the header. |

Invalid values stop the backend at startup with an error. By default a client is its TCP peer address and forwarded headers are ignored, because callers can forge them. The header is read only when the request arrives from a trusted proxy, and only when it holds a single IP; forwarding chains such as a multi-hop `X-Forwarded-For` fall back to the peer address. The proxy must overwrite the header with the address of the connection it received, not append to it.

The Vite proxy used for local development and Docker Compose does this: it sets `X-Ghostline-Client-IP` to each browser's address, replacing any value the browser sent. Docker Compose configures the backend to trust that header from private network ranges, since the backend is not published and only the frontend container can reach it. When running `go run ./backend` and `npm run dev` directly, every request comes from `127.0.0.1` and shares one budget unless you set both variables.

This limit does not replace authentication, does not protect or meter a `GITHUB_TOKEN`, and does not stop a distributed caller using many addresses. GitHub's own 429 responses are reported separately, without a retry time.
