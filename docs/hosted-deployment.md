# Single-instance hosted deployment

Ghostline can run as one production container behind an HTTPS ingress. The image builds the Vue application and serves its static files from the Go/Gin service, so the browser, API, and GitHub OAuth callback share one origin. This is a deployable container, not a Ghostline-managed hosting service.

## Configure

1. Create a GitHub OAuth App for the exact public site origin. Set its callback to `https://your-host/auth/callback`.
2. Copy `.env.hosted.example` to `.env.hosted` and replace every placeholder. Set `GHOSTLINE_ALLOWED_GITHUB_IDS` to the numeric GitHub IDs allowed to use this instance. Keep OAuth credentials in the host's secret manager; do not commit `.env.hosted`.
3. Provide `GHOSTLINE_SESSION_SECRET` from the host secret manager as an environment variable to Docker Compose. Compose fails early if it is absent. The value must contain at least 32 random bytes.
4. If an ingress sits in front of the container, configure it to terminate HTTPS and overwrite the configured client-IP header. Set `RATE_LIMIT_CLIENT_IP_HEADER` to that header and `RATE_LIMIT_TRUSTED_PROXIES` to the exact TCP peer address seen by the container. With a host ingress and Docker port publishing, inspect the Compose network gateway and use that gateway address as a `/32` CIDR; do not assume the ingress's public or private address reaches the container as its TCP peer. Leave both empty for direct connections without a proxy.
5. Keep the container port bound to loopback and let the ingress be the public entry point. Set `GHOSTLINE_BIND_ADDRESS` only to an interface appropriate for the deployment.

Start and inspect the service:

```sh
GHOSTLINE_SESSION_SECRET="$(secret-manager read ghostline/session-secret)" docker compose --env-file .env.hosted -f compose.hosted.yml up --build -d
docker compose --env-file .env.hosted -f compose.hosted.yml ps
```

Stop it with:

```sh
GHOSTLINE_SESSION_SECRET="$(secret-manager read ghostline/session-secret)" docker compose --env-file .env.hosted -f compose.hosted.yml down
```

The app fails at startup unless production mode, required sign-in, the built frontend, an HTTPS callback, a 32-byte session secret, OAuth credentials, and at least one allowed GitHub account ID are configured. `/api` access requires GitHub sign-in. The container runs non-root from a minimal scratch image, drops Linux capabilities, and has a read-only filesystem. Docker Compose and the image health check call the unauthenticated `/api/health` endpoint; this confirms the HTTP process responds, not application correctness or revision validity.

## Operating limits

This deployment is intentionally a single instance. OAuth state, sessions, logout revocations, and request-rate windows are held in process memory; do not scale replicas or restart during an OAuth callback. Restarting clears revocations, so a copied session cookie can remain valid until its 12-hour expiry. Shared session/rate-limit storage and graceful operational controls are future work.

Ghostline currently imports public pull requests. Do not configure a GitHub token with private-repository access, and do not put a shared Qodo key into the server. Users provide a Qodo key for the request that needs it. Configure ingress-level abuse controls, TLS, secret rotation, backups for deployment configuration, and monitoring appropriate to your environment. A successful `/api/health` response confirms the HTTP process responds; it does not establish application correctness or validate imported revisions.

The development stack remains `docker compose up --build` and serves Vite on port 5173. Use `compose.hosted.yml` for the production-built single-container variant.
