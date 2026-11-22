# Single-instance hosted deployment

Ghostline can run as one production container behind an HTTPS ingress. The image builds the Vue application and serves its static files from the Go/Gin service, so the browser, API, and GitHub OAuth callback share one origin. This is a deployable container, not a Ghostline-managed hosting service.

## Configure

1. Create a GitHub OAuth App for the exact public site origin. Set its callback to `https://your-host/auth/callback`.
2. Copy `hosted.env.example` to `.env.hosted` and replace its non-secret values. Set `GHOSTLINE_ALLOWED_GITHUB_IDS` to the numeric GitHub IDs allowed to use this instance. Do not put OAuth secrets in `.env.hosted` or commit the file.
3. Provide `GHOSTLINE_SESSION_SECRET` and `GITHUB_OAUTH_CLIENT_SECRET` from the host secret manager as environment variables to Docker Compose. Compose fails early if either is absent. The session secret must contain at least 32 random bytes.
4. If an ingress sits in front of the container, configure it to terminate HTTPS and overwrite the configured client-IP header and `X-Request-ID`. Set `RATE_LIMIT_CLIENT_IP_HEADER` to that header and `RATE_LIMIT_TRUSTED_PROXIES` to the exact TCP peer address seen by the container. With a host ingress and Docker port publishing, inspect the Compose network gateway and use that gateway address as a `/32` CIDR; do not assume the ingress's public or private address reaches the container as its TCP peer. Leave both empty for direct connections without a proxy.
5. Keep the container port bound to loopback and let the ingress be the public entry point. Set `GHOSTLINE_BIND_ADDRESS` only to an interface appropriate for the deployment.

Start and inspect the service:

```sh
(
  export GHOSTLINE_SESSION_SECRET="$(secret-manager read ghostline/session-secret)"
  export GITHUB_OAUTH_CLIENT_SECRET="$(secret-manager read ghostline/github-oauth-client-secret)"
  docker compose --env-file .env.hosted -f compose.hosted.yml up --build -d &&
    docker compose --env-file .env.hosted -f compose.hosted.yml ps
)
```

Every Compose command for this file, including `ps`, `logs`, and `down`, needs both secrets because Compose interpolates the whole file first. The subshell keeps them out of `.env.hosted` and out of your interactive shell.

Stop it with:

```sh
(
  export GHOSTLINE_SESSION_SECRET="$(secret-manager read ghostline/session-secret)"
  export GITHUB_OAUTH_CLIENT_SECRET="$(secret-manager read ghostline/github-oauth-client-secret)"
  docker compose --env-file .env.hosted -f compose.hosted.yml down
)
```

The app fails at startup unless production mode, required sign-in, the built frontend, an HTTPS callback, a 32-byte session secret, OAuth credentials, and at least one allowed GitHub account ID are configured. `/api` access requires GitHub sign-in. The container runs non-root from a minimal scratch image, drops Linux capabilities, and has a read-only filesystem. Docker Compose and the image health check call the unauthenticated `/api/health` endpoint; this confirms the HTTP process responds, not application correctness or revision validity.

## Timeouts and shutdown

The server sets explicit connection limits. Each can be changed with a Go duration such as `30s`, the first four in `.env.hosted`; the app refuses to start with a value that is not a positive duration.

| Setting | Default | Limits |
|---|---|---|
| `GHOSTLINE_READ_HEADER_TIMEOUT` | `5s` | Time to send the request headers; cuts off slow-header (slowloris) clients. Must not exceed the read timeout. |
| `GHOSTLINE_READ_TIMEOUT` | `15s` | Time to send the whole request. API bodies are at most a few kilobytes. Must be at least 5s shorter than the write timeout, which starts once the headers are read. |
| `GHOSTLINE_WRITE_TIMEOUT` | `90s` | Time to answer. It suits the slowest handler, the Nox report, which reads several GitHub API pages and downloads an artifact of up to 10 MiB. At least `10s`. |
| `GHOSTLINE_IDLE_TIMEOUT` | `60s` | How long a keep-alive connection may stay idle. |
| `GHOSTLINE_SHUTDOWN_GRACE` | `25s` | How long in-flight requests may run after SIGTERM or SIGINT. The hosted deployment sets it in `compose.hosted.yml`, next to the container stop timeout. |

Request headers are limited to 64 KiB. Each request's work, including its GitHub and Qodo calls, stops 5 seconds before the write timeout so the handler can still answer. A request that fails because a call was cut off returns `504` with a message, not a dropped connection. A pull-request import keeps its partial results: a part whose GitHub call was cut off is shown as unavailable, as it is for any other GitHub failure.

On SIGTERM or SIGINT the server stops accepting connections, lets in-flight requests finish within the grace period, then exits 0. A second signal, or the end of the grace period, closes the remaining connections and exits 1. Docker sends SIGKILL when its stop timeout ends, so the stop timeout must be longer than the grace period. Docker's default of 10 seconds would kill the process mid-shutdown. `compose.hosted.yml` therefore sets both values side by side, `GHOSTLINE_SHUTDOWN_GRACE: 25s` and `stop_grace_period: 30s`, and a test keeps the stop timeout at least 5 seconds longer. Change them together in that file; `.env.hosted` cannot override the grace period, because Compose's `environment` takes precedence over `env_file`. When you run the image another way, give it a matching stop timeout, for example `docker run --stop-timeout 30`.

## Logs

With `GHOSTLINE_ENV=production` the service writes JSON logs to stderr, one object per line, ready for a log collector. Otherwise it writes readable `key=value` text. Read them with `docker compose … logs`.

Every request gets a request ID, which the response returns as `X-Request-ID`. When the TCP peer is one of `RATE_LIMIT_TRUSTED_PROXIES`, an inbound `X-Request-ID` is kept, so the ingress's ID follows the request. As with the client-IP header, configure the ingress to set or overwrite `X-Request-ID`. An ingress that passes a client's header through lets the client choose the ID it is logged under. The value must be 1–128 letters, digits or `._:-`; any other value, or one from an untrusted peer, is replaced with a new ID.

Each request writes one access line:

```json
{"level":"INFO","msg":"request","request_id":"3f9c…","method":"POST","route":"/api/import","status":200,"duration_ms":412.7}
```

`route` is the route template, never the raw path. Requests that match no API route, including the frontend's static files, log `unmatched`. Query strings, headers, cookies, tokens and Qodo keys are never logged. The health check calls `/api/health` every 10 seconds, so expect its line too.

A failed GitHub or Qodo call writes an `upstream request failed` line with the same `request_id`. The line gives `upstream` (`github`, `qodo`, or `artifact-storage` when an artifact download fails after GitHub redirects it), the API `path` without its query (empty for artifact storage, whose URLs are signed), `upstream_status` when a response arrived, and `failure` (`status`, `invalid_response`, `rpc_error` for a call Qodo rejects inside a successful response, `transport`, `deadline` or `canceled`). GitHub sign-in calls are included. It never includes a response body or a transport error's text, which can contain signed artifact download URLs. A 4xx response logs at `INFO`, because some lookups expect a 404; every other failure, including a 5xx, an unreadable response or a failure to connect, logs at `WARN`. To follow one request, filter on its `request_id`.

## Operating limits

This deployment is intentionally a single instance. OAuth state, sessions, logout revocations, and request-rate windows are held in process memory; do not scale replicas or restart during an OAuth callback. Restarting clears revocations, so a copied session cookie can remain valid until its 12-hour expiry. Shared session and rate-limit storage are future work.

Ghostline currently imports public pull requests. Do not configure a GitHub token with private-repository access, and do not put a shared Qodo key into the server. Users provide a Qodo key for the request that needs it. Configure ingress-level abuse controls, TLS, secret rotation, backups for deployment configuration, and monitoring appropriate to your environment. A successful `/api/health` response confirms the HTTP process responds; it does not establish application correctness or validate imported revisions.

The development stack remains `docker compose up --build` and serves Vite on port 5173. Use `compose.hosted.yml` for the production-built single-container variant.
