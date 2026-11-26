# Docker Compose

Moved from the README.

```sh
docker compose up --build
```

Open http://localhost:5173 (or set `GHOSTLINE_PORT` if that port is occupied). Compose binds the frontend to all network interfaces by default, so other devices on the LAN can use `http://<host-lan-ip>:5173`; set `GHOSTLINE_BIND_ADDRESS` to a specific interface address to restrict access. The development frontend proxies API requests to the backend container. Compose forwards `GITHUB_TOKEN` from the environment to the backend for repository visibility checks and, once a repository is confirmed public, for downloading its `nox-results` artifact (see [Configuration](configuration.md#github-token); displaying Nox reports needs a token with Actions read access). Other public pull-request data requests remain unauthenticated and use GitHub's public API quota.

## Protected hosted-mode sign-in

Compose defaults to an unauthenticated local demo. Protected-mode sign-in requires a trusted HTTPS ingress whose public origin matches the origin in `GITHUB_OAUTH_CALLBACK_URL`. The ingress must route `/auth/*` and `/api/*` to Compose and overwrite `X-Real-IP` with the client address. Browsing directly to Compose over `http://localhost:5173` or a LAN IP is intentionally rejected by the same-origin check and is not a supported protected-mode setup. Configure these variables in the environment running Compose:

| Variable | Purpose |
| --- | --- |
| `GHOSTLINE_AUTH_MODE` | Set to `required` to protect all `/api` routes. |
| `GITHUB_OAUTH_CLIENT_ID` / `GITHUB_OAUTH_CLIENT_SECRET` | GitHub OAuth App credentials. |
| `GITHUB_OAUTH_CALLBACK_URL` | Exact HTTPS callback, for example `https://ghostline.example/auth/callback`. |
| `GHOSTLINE_SESSION_SECRET` | Random secret of at least 32 bytes used to sign state and sessions. |
| `GHOSTLINE_ALLOWED_GITHUB_IDS` | Comma-separated numeric GitHub user IDs allowed to sign in. Required unless open sign-in is on. |
| `GHOSTLINE_OPEN_SIGNIN` | `true` lets any GitHub account sign in and ignores the allowlist. Default `false`. |
| `GHOSTLINE_TRUSTED_PROXY_CIDRS` | Vite development proxy only: optional CIDRs for ingress peers trusted to supply `X-Real-IP`. The production Go service uses `RATE_LIMIT_CLIENT_IP_HEADER` and `RATE_LIMIT_TRUSTED_PROXIES`, documented in the hosted deployment guide. |

The callback exchanges the temporary OAuth code, reads the account identity, and discards the GitHub token. Sessions last 12 hours. Use a secret manager for deployment credentials. This two-container Compose stack remains a Vite development environment for trusted demos. For the production-built single-container setup, see the [hosted deployment guide](hosted-deployment.md). See the [threat model](threat-model.md) for limits that still apply to either deployment.

The protected-mode sign-in screen is shown in this [working-app screenshot](feature-evidence/hosted-github-sign-in.jpg); completing sign-in requires configured GitHub OAuth credentials.
