# Cluster deployment

Ghostline runs on the maintainer's k3s cluster at <https://ghostline.felixgeelhaar.de>, delivered by [rollops](https://go.klarlabs.de/rollops). rollopsd watches this repository's `.rollops/` directory and applies the Deployment, Service, Traefik middlewares and Ingress it defines. `deploy/kubernetes/ghostline.yaml` is the same set of manifests, plus the Namespace and its NetworkPolicy, for `kubectl apply` when rollopsd is down. `backend/deploy_manifest_test.go` fails CI when the two copies differ.

The general production rules in [hosted-deployment.md](hosted-deployment.md) apply here too: one instance, sign-in required, and secrets only through the environment.

## How it runs

| Concern | Setting |
|---|---|
| Image | A release digest, such as `ghcr.io/felixgeelhaar/ghostline:v0.1.0-rc.1@sha256:1bde…`. rollopsd opens a pull request for each newer release (`imagePolicy: minor`, `writeback: pull-request`) that rewrites the tracked `image:` line in `.rollops/deploy-ghostline.yaml`, and a person merges it. rollopsd renders that image over the manifest's. The break-glass copy can lag, so after a break-glass apply, set the tracked image: `kubectl -n ghostline set image deployment/ghostline ghostline=<tracked image>`. |
| Instances | `replicas: 1` with a `Recreate` rollout. Sessions, OAuth state and rate windows live in memory, so two pods must never run at once. |
| Shutdown | `GHOSTLINE_SHUTDOWN_GRACE=25s` within `terminationGracePeriodSeconds: 30`. |
| TLS | cert-manager `letsencrypt-prod`; HTTP redirects to HTTPS; Traefik adds HSTS and a referrer policy. |
| Client IP | The app trusts Traefik's `X-Real-Ip` from the pod network `10.42.0.0/16`. The NetworkPolicy admits only Traefik's pods (`kube-system`, `app.kubernetes.io/name: traefik`), on port 8080 and on cert-manager's HTTP-01 solver port 8089, so no other pod can choose a rate-limit identity. |
| Request IDs | Traefik forwards a client's `X-Request-ID` unchanged, so the `edge-headers` middleware removes it and the app generates one. |
| Resources | Requests 25m CPU and 64 MiB; memory limit 384 MiB. The nodes run at 70–85% memory, so a one-time ResourceQuota and LimitRange also bound the whole namespace. |
| Security | Non-root user 65532, read-only root filesystem, no capabilities, `RuntimeDefault` seccomp, no service-account token. |

## One-time setup

1. **DNS:** point `ghostline.felixgeelhaar.de` at the cluster, as the other hosts are.
2. **GitHub OAuth App:** set the homepage to `https://ghostline.felixgeelhaar.de` and the callback to `https://ghostline.felixgeelhaar.de/auth/callback`.
3. **Namespace and policy** (engineer approval):

   ```sh
   kubectl apply -f deploy/kubernetes/ghostline.yaml -l ghostline.felixgeelhaar.de/setup=one-time
   ```

   This applies only the Namespace, NetworkPolicy, ResourceQuota and LimitRange, the documents carrying that label. rollopsd applies the rest.
4. **Secret.** The engineer creates it. Values go through stdin, never in command arguments or shell history; read each one from the secret manager:

   ```sh
   {
     printf 'GHOSTLINE_SESSION_SECRET=%s\n' "$(openssl rand -base64 48 | tr -d '\n')"
     printf 'GITHUB_OAUTH_CLIENT_ID=%s\n' "$(secret-manager read ghostline/github-oauth-client-id)"
     printf 'GITHUB_OAUTH_CLIENT_SECRET=%s\n' "$(secret-manager read ghostline/github-oauth-client-secret)"
     printf 'GHOSTLINE_ALLOWED_GITHUB_IDS=%s\n' "$(secret-manager read ghostline/allowed-github-ids)"
     printf 'GITHUB_TOKEN=%s\n' "$(secret-manager read ghostline/github-token)"
   } | kubectl -n ghostline create secret generic ghostline-env --from-env-file=/dev/stdin
   ```

   `printf` is a shell builtin, so the values never appear in a process list. Use a fine-grained `GITHUB_TOKEN` with read-only access to public repositories, and comma-separate multiple GitHub IDs.

5. **rollopsd access:** give the fg GitHub App (installation for `felixgeelhaar`) access to `felixgeelhaar/ghostline`. Then add this entry to the `rollopsd-watch` ConfigMap in `rollops-system`:

   ```json
   {"name":"ghostline","url":"https://github.com/felixgeelhaar/ghostline","branch":"main","path":".rollops","githubAppId":"4330615","githubInstallationId":"147373546","githubAppPrivateKeyFile":"/etc/rollops/git/github-app-fg.pem"}
   ```

## Check it

```sh
kubectl -n ghostline rollout status deployment/ghostline
curl -fsS https://ghostline.felixgeelhaar.de/api/health
curl -sI http://ghostline.felixgeelhaar.de | grep -i location    # https redirect
```

Sign in with an allowed GitHub account and import a public pull request. The screenshot of that working page is the AGENTS.md rule 6 evidence for the release.
