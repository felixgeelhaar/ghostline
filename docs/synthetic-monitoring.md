# Synthetic monitoring

`.github/workflows/monitor.yml` checks the production deployment (https://ghostline.felixgeelhaar.de) every 30 minutes, at minutes 7 and 37. GitHub may start scheduled runs late. It runs `tools/signin-monitor`.

## What it checks

Every Ghostline capability needs a GitHub sign-in. The server holds sessions in memory, so a rollout drops them. The monitor therefore holds no credential and never signs in. It checks what every signed-out user relies on, in order, and stops at the first failure:

| Step | Assertion |
| --- | --- |
| health | `GET /api/health` is 200 `{"status":"ok"}` |
| release UI | `GET /` has the Ghostline title and `id="app"`, and its `/assets/index-*.js` bundle carries the release pinned in `.rollops/deploy-ghostline.yaml` |
| signed-out session | `GET /api/session` says `authRequired: true, authenticated: false` |
| API refuses anonymous import | `POST /api/import` without a session is 401 `AUTH_REQUIRED`, not 200 and not 5xx |
| sign-in hand-off | the sign-in button's same-origin `POST /auth/login` is a 302 to `https://github.com/login/oauth/authorize`, with a client ID, this deployment's `/auth/callback`, a state and an S256 challenge |
| GitHub shows the app | GitHub's page for that URL, opened signed out, asks to sign in "to continue to **Ghostline**". An unknown client ID gets an error page instead |

Each run creates one OAuth state on the server. It expires unused, like an abandoned sign-in.

What it does not check: anything after sign-in (import, readiness, Qodo or Nox loading), the GitHub API quota, or a real browser rendering the page. A pass means a signed-out user can reach the release UI and start signing in. It does not mean the signed-in features work.

## Alerts

When a run fails, the workflow opens an issue titled **Synthetic monitor: signed-out journey failing**, or comments on the open one, with the run link and the check output. GitHub notifies the repository's watchers. The next passing run with the real assertion comments and closes the issue.

To test the alert route without touching the app, run the workflow manually with an `expect_app` name GitHub never shows:

```sh
gh workflow run monitor.yml -R felixgeelhaar/ghostline -f expect_app="Ghostline controlled failure"
```

That run fails at the last step and raises the alert. A manual run with the default `Ghostline`, or the next scheduled run, closes it.

## First response

1. **Read the failing step in the issue.** The steps run in order, so the first failure narrows the cause.
2. **health or release UI:** check whether a rollout is in progress or failed: `kubectl --context felixgeelhaar -n ghostline rollout status deploy/ghostline`, then the pod's events and logs. A release mismatch right after a deploy PR merges usually means rollopsd hasn't applied it yet. If it persists, the running image differs from the pin.
3. **signed-out session or anonymous import:** treat it as a security incident. Production stopped requiring sign-in, or the API answers anonymous callers. Check `GHOSTLINE_AUTH_MODE` and `GHOSTLINE_ENV` in the running pod before anything else.
4. **sign-in hand-off or GitHub shows the app:** check the OAuth client settings in the deployment's secret and the GitHub OAuth app (client ID and callback URL). Then check githubstatus.com. Users can't sign in until this is fixed.
5. **The check itself failed** (no output, or setup failed): read the run log. It is a monitor problem, not an app problem, but the deployment went unchecked for that run.

GitHub turns off scheduled workflows in a repository with no activity for 60 days. Re-enable it under Actions if that happens.
